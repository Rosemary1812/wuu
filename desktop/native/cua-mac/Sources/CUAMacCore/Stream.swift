import AppKit
import ApplicationServices
import AVFoundation
@preconcurrency import CoreMedia
import Darwin
import Foundation
import QuartzCore
@preconcurrency import ScreenCaptureKit

public struct NativePiPConfiguration: Sendable {
    public let activityID: String
    public let target: String
    public let frame: CGRect
    public let processID: pid_t?
    public let windowID: CGWindowID?
    public let parentProcessID: pid_t?

    public init(activityID: String, target: String, frame: CGRect, processID: pid_t? = nil, windowID: CGWindowID? = nil, parentProcessID: pid_t? = nil) {
        self.activityID = activityID
        self.target = target
        self.frame = frame
        self.processID = processID
        self.windowID = windowID
        self.parentProcessID = parentProcessID
    }
}

private final class PiPEventWriter: @unchecked Sendable {
    private let queue = DispatchQueue(label: "com.blueberrycongee.wuu.cua.pip-events")

    func send(_ event: String, fields: [String: Any] = [:]) {
        queue.sync {
            var payload = fields
            payload["event"] = event
            payload["timestamp_ns"] = DispatchTime.now().uptimeNanoseconds
            guard let data = try? JSONSerialization.data(withJSONObject: payload, options: [.sortedKeys]) else { return }
            FileHandle.standardOutput.write(data)
            FileHandle.standardOutput.write(Data([0x0A]))
        }
    }
}

private final class PiPCommandReader: @unchecked Sendable {
    private let lock = NSLock()
    private var buffer = Data()

    func append(_ data: Data) -> [String] {
        lock.lock()
        defer { lock.unlock() }
        buffer.append(data)
        var lines: [String] = []
        while let newline = buffer.firstIndex(of: 0x0A) {
            let line = buffer[..<newline]
            buffer.removeSubrange(...newline)
            if !line.isEmpty { lines.append(String(decoding: line, as: UTF8.self)) }
        }
        return lines
    }
}

private final class CapturedFrame: @unchecked Sendable {
    let sampleBuffer: CMSampleBuffer

    init(_ sampleBuffer: CMSampleBuffer) {
        self.sampleBuffer = sampleBuffer
    }
}

@MainActor
private final class NativePiPLiveView: NSView {
    private let displayLayer = AVSampleBufferDisplayLayer()
    private let pointerLayer = CAShapeLayer()
    private let rippleLayer = CAShapeLayer()
    private var videoSize = CGSize(width: 16, height: 9)
    private var pointerPosition = CGPoint(x: 0.5, y: 0.5)

    override init(frame frameRect: NSRect) {
        super.init(frame: frameRect)
        wantsLayer = true
        layer?.backgroundColor = NSColor.clear.cgColor

        displayLayer.videoGravity = .resizeAspect
        displayLayer.backgroundColor = NSColor.clear.cgColor
        layer?.addSublayer(displayLayer)

        let pointerPath = CGMutablePath()
        // Core Animation uses a bottom-left origin here. Define the cursor in
        // that coordinate space so its tip points toward the upper-left.
        pointerPath.move(to: CGPoint(x: 2, y: 28))
        pointerPath.addLine(to: CGPoint(x: 2, y: 5))
        pointerPath.addLine(to: CGPoint(x: 8, y: 11))
        pointerPath.addLine(to: CGPoint(x: 12, y: 2))
        pointerPath.addLine(to: CGPoint(x: 17, y: 4))
        pointerPath.addLine(to: CGPoint(x: 13, y: 13))
        pointerPath.addLine(to: CGPoint(x: 21, y: 13))
        pointerPath.closeSubpath()
        pointerLayer.path = pointerPath
        pointerLayer.fillColor = NSColor.white.cgColor
        pointerLayer.strokeColor = NSColor.systemOrange.cgColor
        pointerLayer.lineWidth = 2
        pointerLayer.lineJoin = .round
        pointerLayer.bounds = CGRect(x: 0, y: 0, width: 24, height: 30)
        pointerLayer.anchorPoint = CGPoint(x: 2.0 / 24.0, y: 28.0 / 30.0)
        layer?.addSublayer(pointerLayer)

        rippleLayer.path = CGPath(ellipseIn: CGRect(x: 0, y: 0, width: 18, height: 18), transform: nil)
        rippleLayer.fillColor = NSColor.clear.cgColor
        rippleLayer.strokeColor = NSColor.systemOrange.withAlphaComponent(0.9).cgColor
        rippleLayer.lineWidth = 2
        rippleLayer.bounds = CGRect(x: 0, y: 0, width: 18, height: 18)
        rippleLayer.opacity = 0
        layer?.addSublayer(rippleLayer)
    }

    required init?(coder: NSCoder) { nil }

    override func layout() {
        super.layout()
        CATransaction.begin()
        CATransaction.setDisableActions(true)
        displayLayer.frame = bounds
        placePointer()
        CATransaction.commit()
    }

    func enqueue(_ sampleBuffer: CMSampleBuffer) {
        // AVSampleBufferDisplayLayer reads DisplayImmediately from the per-sample
        // attachments array, never from buffer-level attachments. Without a
        // control timebase, a frame only presents when this flag is set on the
        // sample itself; writing it with CMSetAttachment lands it in the wrong
        // bucket, so the layer holds every frame in its queue and stays black
        // while the overlay pointer (a separate CAShapeLayer) still renders.
        if let attachments = CMSampleBufferGetSampleAttachmentsArray(sampleBuffer, createIfNecessary: true),
           CFArrayGetCount(attachments) > 0 {
            let sampleAttachments = unsafeBitCast(CFArrayGetValueAtIndex(attachments, 0), to: CFMutableDictionary.self)
            CFDictionarySetValue(
                sampleAttachments,
                Unmanaged.passUnretained(kCMSampleAttachmentKey_DisplayImmediately).toOpaque(),
                Unmanaged.passUnretained(kCFBooleanTrue).toOpaque()
            )
        }
        if displayLayer.status == .failed { displayLayer.flush() }
        displayLayer.enqueue(sampleBuffer)
        if let imageBuffer = sampleBuffer.imageBuffer {
            let size = CGSize(width: CVPixelBufferGetWidth(imageBuffer), height: CVPixelBufferGetHeight(imageBuffer))
            setVideoSize(size)
        }
    }

    func setVideoSize(_ size: CGSize) {
        guard size.width > 0, size.height > 0 else { return }
        videoSize = size
        needsLayout = true
    }

    func animateInteraction(_ payload: [String: Any]) {
        guard let kind = payload["kind"] as? String,
              let x = payload["x"] as? Double,
              let y = payload["y"] as? Double else { return }
        let destination = CGPoint(
            x: min(1, max(0, payload["to_x"] as? Double ?? x)),
            y: min(1, max(0, payload["to_y"] as? Double ?? y))
        )
        let from = pointerPoint(pointerPosition)
        let to = pointerPoint(destination)
        pointerPosition = destination

        let animation = CAKeyframeAnimation(keyPath: "position")
        animation.values = [from, CGPoint(x: (from.x + to.x) / 2 + 18, y: (from.y + to.y) / 2 + 14), to]
        animation.keyTimes = [0, 0.52, 1]
        animation.duration = 0.52
        animation.timingFunction = CAMediaTimingFunction(controlPoints: 0.22, 0.8, 0.24, 1)
        pointerLayer.add(animation, forKey: "move")
        CATransaction.begin()
        CATransaction.setDisableActions(true)
        pointerLayer.position = to
        rippleLayer.position = to
        CATransaction.commit()

        switch kind {
        case "click": animateClick()
        case "scroll": animateNudge(key: "scroll", y: 8)
        case "type": animatePulse()
        case "drag": animateNudge(key: "drag", y: -4)
        default: break
        }
    }

    private func animateClick() {
        let scale = CABasicAnimation(keyPath: "transform.scale")
        scale.fromValue = 0.35
        scale.toValue = 1.8
        let opacity = CABasicAnimation(keyPath: "opacity")
        opacity.fromValue = 0.95
        opacity.toValue = 0
        let group = CAAnimationGroup()
        group.animations = [scale, opacity]
        group.duration = 0.48
        rippleLayer.add(group, forKey: "click")
    }

    private func animateNudge(key: String, y: CGFloat) {
        let animation = CAKeyframeAnimation(keyPath: "transform.translation.y")
        animation.values = [0, y, 0]
        animation.duration = 0.62
        pointerLayer.add(animation, forKey: key)
    }

    private func animatePulse() {
        let animation = CAKeyframeAnimation(keyPath: "opacity")
        animation.values = [1, 0.45, 1]
        animation.duration = 0.55
        pointerLayer.add(animation, forKey: "type")
    }

    private func presentationRect() -> CGRect {
        AVMakeRect(aspectRatio: videoSize, insideRect: bounds)
    }

    private func pointerPoint(_ normalized: CGPoint) -> CGPoint {
        let rect = presentationRect()
        return CGPoint(x: rect.minX + normalized.x * rect.width, y: rect.maxY - normalized.y * rect.height)
    }

    private func placePointer() {
        pointerLayer.position = pointerPoint(pointerPosition)
        rippleLayer.position = pointerLayer.position
    }
}

/// Container that shows a frosted placeholder (target app icon) the instant the
/// preview is requested, then cross-fades to the live capture once the first
/// real frame is presented. The placeholder communicates *which* app is being
/// observed; it is not a status badge and never carries error text.
@MainActor
private final class NativePiPView: NSView {
    private let frostView = NSVisualEffectView()
    private let iconView = NSImageView()
    private let live = NativePiPLiveView()
    private let closeButton = NSButton()
    private var trackingArea: NSTrackingArea?
    private var isLive = false
    var onClose: (() -> Void)?

    override init(frame frameRect: NSRect) {
        super.init(frame: frameRect)
        wantsLayer = true
        layer?.backgroundColor = NSColor.clear.cgColor
        layer?.cornerRadius = 14
        layer?.masksToBounds = true

        frostView.material = .hudWindow
        frostView.state = .active
        frostView.blendingMode = .behindWindow
        frostView.wantsLayer = true
        addSubview(frostView)

        iconView.imageScaling = .scaleProportionallyUpOrDown
        iconView.contentTintColor = .secondaryLabelColor
        iconView.wantsLayer = true
        iconView.image = NativePiPView.fallbackIcon()
        addSubview(iconView)

        // Live capture sits above the placeholder and starts transparent, so the
        // frosted icon shows through until the first frame reveals it.
        live.alphaValue = 0
        addSubview(live)

        closeButton.title = "×"
        closeButton.isBordered = false
        closeButton.font = .systemFont(ofSize: 17, weight: .regular)
        closeButton.contentTintColor = .labelColor
        closeButton.wantsLayer = true
        closeButton.layer?.backgroundColor = NSColor.windowBackgroundColor.withAlphaComponent(0.88).cgColor
        closeButton.layer?.cornerRadius = 8
        closeButton.alphaValue = 0
        closeButton.target = self
        closeButton.action = #selector(closePressed)
        addSubview(closeButton)

        startPlaceholderPulse()
    }

    required init?(coder: NSCoder) { nil }

    private static func fallbackIcon() -> NSImage? {
        NSImage(systemSymbolName: "macwindow", accessibilityDescription: "preparing live preview")
    }

    func setIcon(_ image: NSImage?) {
        if let image {
            iconView.contentTintColor = nil
            iconView.image = image
        } else {
            iconView.contentTintColor = .secondaryLabelColor
            iconView.image = NativePiPView.fallbackIcon()
        }
    }

    override func layout() {
        super.layout()
        CATransaction.begin()
        CATransaction.setDisableActions(true)
        frostView.frame = bounds
        live.frame = bounds
        let side = max(28, min(bounds.width, bounds.height) * 0.42)
        iconView.frame = CGRect(x: bounds.midX - side / 2, y: bounds.midY - side / 2, width: side, height: side)
        closeButton.frame = CGRect(x: bounds.maxX - 34, y: bounds.maxY - 34, width: 26, height: 26)
        CATransaction.commit()
    }

    private func startPlaceholderPulse() {
        let pulse = CABasicAnimation(keyPath: "opacity")
        pulse.fromValue = 0.85
        pulse.toValue = 0.4
        pulse.duration = 1.35
        pulse.autoreverses = true
        pulse.repeatCount = .infinity
        pulse.timingFunction = CAMediaTimingFunction(name: .easeInEaseOut)
        iconView.layer?.add(pulse, forKey: "pulse")
    }

    func revealLive() {
        guard !isLive else { return }
        isLive = true
        iconView.layer?.removeAnimation(forKey: "pulse")
        NSAnimationContext.runAnimationGroup { context in
            context.duration = 0.28
            live.animator().alphaValue = 1
            iconView.animator().alphaValue = 0
        }
    }

    func showPlaceholder() {
        guard isLive else { return }
        isLive = false
        iconView.alphaValue = 1
        NSAnimationContext.runAnimationGroup { context in
            context.duration = 0.2
            live.animator().alphaValue = 0
        }
        startPlaceholderPulse()
    }

    func enqueue(_ sampleBuffer: CMSampleBuffer) { live.enqueue(sampleBuffer) }
    func setVideoSize(_ size: CGSize) { live.setVideoSize(size) }
    func animateInteraction(_ payload: [String: Any]) { live.animateInteraction(payload) }

    override func updateTrackingAreas() {
        super.updateTrackingAreas()
        if let trackingArea { removeTrackingArea(trackingArea) }
        let next = NSTrackingArea(rect: bounds, options: [.mouseEnteredAndExited, .activeAlways], owner: self)
        addTrackingArea(next)
        trackingArea = next
    }

    override func mouseEntered(with event: NSEvent) {
        NSAnimationContext.runAnimationGroup { _ in closeButton.animator().alphaValue = 1 }
    }

    override func mouseExited(with event: NSEvent) {
        NSAnimationContext.runAnimationGroup { _ in closeButton.animator().alphaValue = 0 }
    }

    @objc private func closePressed() { onClose?() }
}

@MainActor
private final class NativePiPWindowController: NSObject, NSWindowDelegate {
    let panel: NSPanel
    let content: NativePiPView
    private let writer: PiPEventWriter
    private let desktopTop: CGFloat
    private var shown = false
    private var requestedVisible = true
    private var captureAvailable = false

    init(configuration: NativePiPConfiguration, writer: PiPEventWriter) {
        self.writer = writer
        content = NativePiPView(frame: CGRect(origin: .zero, size: configuration.frame.size))
        desktopTop = NSScreen.screens.first?.frame.maxY ?? 0
        let nativeFrame = CGRect(
            x: configuration.frame.minX,
            y: desktopTop - configuration.frame.maxY,
            width: configuration.frame.width,
            height: configuration.frame.height
        )
        panel = NSPanel(
            contentRect: nativeFrame,
            styleMask: [.borderless, .nonactivatingPanel, .resizable, .fullSizeContentView],
            backing: .buffered,
            defer: false
        )
        super.init()
        panel.delegate = self
        panel.contentView = content
        panel.isOpaque = false
        panel.backgroundColor = .clear
        panel.hasShadow = true
        panel.hidesOnDeactivate = false
        panel.becomesKeyOnlyIfNeeded = true
        // The helper runs in a separate process, so it cannot be attached as an
        // NSPanel child of Electron's main window. Keep the preview above normal
        // application windows while its owning activity is visible.
        panel.level = .floating
        panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary]
        panel.isMovableByWindowBackground = true
        panel.minSize = CGSize(width: 220, height: 140)
        panel.maxSize = CGSize(width: 720, height: 800)
        panel.contentAspectRatio = configuration.frame.size
        content.onClose = { [weak self] in
            self?.requestedVisible = false
            self?.writer.send("user_close")
            self?.panel.orderOut(nil)
        }
    }

    func showAfterFirstFrame(videoSize: CGSize) {
        content.setVideoSize(videoSize)
        let ratio = videoSize.width / max(1, videoSize.height)
        let previousRatio = panel.contentAspectRatio.width / max(0.01, panel.contentAspectRatio.height)
        panel.contentAspectRatio = CGSize(width: ratio, height: 1)
        if shown, abs(previousRatio - ratio) > 0.001 {
            resizeForAspectRatio(ratio)
        }
        presentContent(width: Int(videoSize.width), height: Int(videoSize.height), ratio: ratio)
    }

    var hasShownContent: Bool { shown }

    func setVisible(_ visible: Bool) {
        requestedVisible = visible
        // Show the surface immediately so the frosted placeholder appears while
        // capture spins up; visibility is no longer gated on the first frame.
        if visible { panel.orderFrontRegardless() } else { panel.orderOut(nil) }
    }

    func setIcon(_ image: NSImage?) { content.setIcon(image) }

    func markCaptureUnavailable() {
        guard captureAvailable else { return }
        captureAvailable = false
        // Fall back to the frosted placeholder instead of vanishing; a briefly
        // unavailable window should still read as "observing this app".
        content.showPlaceholder()
    }

    func animateInteraction(_ payload: [String: Any]) { content.animateInteraction(payload) }

    func windowWillResize(_ sender: NSWindow, to frameSize: NSSize) -> NSSize {
        let ratio = max(0.01, panel.contentAspectRatio.width / max(0.01, panel.contentAspectRatio.height))
        let widthDriven = CGSize(width: frameSize.width, height: frameSize.width / ratio)
        if widthDriven.height <= panel.maxSize.height { return widthDriven }
        return CGSize(width: frameSize.height * ratio, height: frameSize.height)
    }

    func windowDidResize(_ notification: Notification) { publishGeometry() }
    func windowDidMove(_ notification: Notification) { publishGeometry() }

    private func presentContent(width: Int, height: Int, ratio: CGFloat) {
        captureAvailable = true
        // First real frame: cross-fade the live capture over the placeholder and
        // grow from the fixed placeholder size to the window's true aspect ratio.
        content.revealLive()
        if !shown {
            let fitted = fittedPiPSize(ratio: ratio, preferredWidth: panel.frame.width)
            let current = panel.frame
            panel.setFrame(
                CGRect(x: current.maxX - fitted.width, y: current.maxY - fitted.height, width: fitted.width, height: fitted.height),
                display: true,
                animate: true
            )
            shown = true
            writer.send("ready", fields: ["width": width, "height": height])
        }
        if requestedVisible, !panel.isVisible {
            panel.orderFrontRegardless()
        }
    }

    private func fittedPiPSize(ratio: CGFloat, preferredWidth: CGFloat) -> CGSize {
        var width = preferredWidth
        var height = width / max(0.01, ratio)
        if height > 800 { height = 800; width = height * ratio }
        if height < 140 { height = 140; width = height * ratio }
        if width > 720 { width = 720; height = width / ratio }
        return CGSize(width: max(220, width), height: max(140, height))
    }

    private func resizeForAspectRatio(_ ratio: CGFloat) {
        let current = panel.frame
        var width = current.width
        var height = width / max(0.01, ratio)
        if height > panel.maxSize.height { height = panel.maxSize.height; width = height * ratio }
        if height < panel.minSize.height { height = panel.minSize.height; width = height * ratio }
        if width > panel.maxSize.width { width = panel.maxSize.width; height = width / ratio }
        if width < panel.minSize.width { width = panel.minSize.width; height = width / ratio }
        panel.setFrame(
            CGRect(x: current.maxX - width, y: current.maxY - height, width: width, height: height),
            display: true,
            animate: true
        )
    }

    private func publishGeometry() {
        let frame = panel.frame
        writer.send("geometry", fields: [
            "x": frame.minX,
            "y": desktopTop - frame.maxY,
            "width": frame.width,
            "height": frame.height,
        ])
    }
}

private final class WindowGeometryMonitor: @unchecked Sendable {
    private let lock = NSLock()
    private var dirty = false
    private var observer: AXObserver?
    private let application: AXUIElement
    private var boundWindow: AXUIElement?

    init(processID: pid_t, windowFrame: CGRect) {
        application = AXUIElementCreateApplication(processID)
        var created: AXObserver?
        guard AXObserverCreate(processID, geometryObserverCallback, &created) == .success, let created else { return }
        observer = created
        CFRunLoopAddSource(CFRunLoopGetMain(), AXObserverGetRunLoopSource(created), .commonModes)
        bindWindow(nearestTo: windowFrame)
    }

    deinit {
        if let observer { CFRunLoopRemoveSource(CFRunLoopGetMain(), AXObserverGetRunLoopSource(observer), .commonModes) }
    }

    func handle(_ notification: CFString) {
        markDirty()
    }

    func takeDirty() -> Bool {
        lock.lock(); defer { lock.unlock() }
        let value = dirty
        dirty = false
        return value
    }

    private func markDirty() { lock.withLock { dirty = true } }

    func rebind(nearestTo frame: CGRect) { bindWindow(nearestTo: frame) }

    private func bindWindow(nearestTo frame: CGRect) {
        guard let observer else { return }
        let refcon = Unmanaged.passUnretained(self).toOpaque()
        if let boundWindow {
            AXObserverRemoveNotification(observer, boundWindow, kAXMovedNotification as CFString)
            AXObserverRemoveNotification(observer, boundWindow, kAXResizedNotification as CFString)
            AXObserverRemoveNotification(observer, boundWindow, kAXUIElementDestroyedNotification as CFString)
        }
        var value: CFTypeRef?
        guard AXUIElementCopyAttributeValue(application, kAXWindowsAttribute as CFString, &value) == .success,
              let windows = value as? [AXUIElement],
              let window = windows.compactMap({ item -> (AXUIElement, CGRect)? in
                  guard let candidate = axFrame(item) else { return nil }
                  return (item, candidate)
              }).min(by: { windowFrameDistance($0.1, frame) < windowFrameDistance($1.1, frame) })?.0 else {
            boundWindow = nil
            return
        }
        boundWindow = window
        AXObserverAddNotification(observer, window, kAXMovedNotification as CFString, refcon)
        AXObserverAddNotification(observer, window, kAXResizedNotification as CFString, refcon)
        AXObserverAddNotification(observer, window, kAXUIElementDestroyedNotification as CFString, refcon)
    }
}

private let geometryObserverCallback: AXObserverCallback = { _, _, notification, refcon in
    guard let refcon else { return }
    Unmanaged<WindowGeometryMonitor>.fromOpaque(refcon).takeUnretainedValue().handle(notification)
}

struct CaptureFreshness: Equatable {
    private(set) var hasDisplayedFrame = false
    private(set) var lastCallbackUptime: TimeInterval?
    private(set) var lastHealthyCallbackUptime: TimeInterval?
    private(set) var unavailableSinceUptime: TimeInterval?

    mutating func recordCallback(
        at uptime: TimeInterval,
        displayedFrame: Bool,
        captureUnavailable: Bool
    ) {
        lastCallbackUptime = uptime
        if displayedFrame { hasDisplayedFrame = true }
        if captureUnavailable {
            if unavailableSinceUptime == nil { unavailableSinceUptime = uptime }
        } else {
            lastHealthyCallbackUptime = uptime
            unavailableSinceUptime = nil
        }
    }

    func requiresRecovery(
        at uptime: TimeInterval,
        streamStartedAt: TimeInterval,
        startupTimeout: TimeInterval,
        callbackSilenceTimeout: TimeInterval,
        unavailableStatusTimeout: TimeInterval
    ) -> Bool {
        if let unavailableSinceUptime,
           uptime - unavailableSinceUptime >= unavailableStatusTimeout {
            return true
        }
        guard hasDisplayedFrame else {
            return uptime - streamStartedAt >= startupTimeout
        }
        guard let lastCallbackUptime else {
            return uptime - streamStartedAt >= callbackSilenceTimeout
        }
        return uptime - lastCallbackUptime >= callbackSilenceTimeout
    }
}

@available(macOS 14.0, *)
private final class NativePiPStreamOutput: NSObject, SCStreamOutput, SCStreamDelegate, @unchecked Sendable {
    let controller: NativePiPWindowController
    let writer: PiPEventWriter
    private let lock = NSLock()
    private var stopped = false
    private var freshness = CaptureFreshness()
    private var lastStatus: SCFrameStatus?

    init(controller: NativePiPWindowController, writer: PiPEventWriter) {
        self.controller = controller
        self.writer = writer
    }

    func stream(_ stream: SCStream, didOutputSampleBuffer sampleBuffer: CMSampleBuffer, of type: SCStreamOutputType) {
        guard type == .screen else { return }
        let valid = sampleBuffer.isValid
        let attachments = valid
            ? CMSampleBufferGetSampleAttachmentsArray(sampleBuffer, createIfNecessary: false) as? [[SCStreamFrameInfo: Any]]
            : nil
        let status = (attachments?.first?[.status] as? NSNumber).flatMap { SCFrameStatus(rawValue: $0.intValue) }
        let imageBuffer = valid ? sampleBuffer.imageBuffer : nil
        let captureUnavailable = !valid || status == .blank || status == .suspended || status == .stopped
        let shouldDisplay = imageBuffer != nil && !captureUnavailable
        lock.lock()
        let statusChanged = status != nil && status != lastStatus
        if let status { lastStatus = status }
        if status == .stopped { stopped = true }
        freshness.recordCallback(
            at: ProcessInfo.processInfo.systemUptime,
            displayedFrame: shouldDisplay,
            captureUnavailable: captureUnavailable
        )
        lock.unlock()
        if statusChanged, let status { writer.send("capture_status", fields: ["status": captureStatusName(status)]) }
        guard shouldDisplay, let imageBuffer = sampleBuffer.imageBuffer else { return }
        let size = CGSize(width: CVPixelBufferGetWidth(imageBuffer), height: CVPixelBufferGetHeight(imageBuffer))
        // SCStream invokes this callback on its capture queue. Core Animation
        // layers belong to the main actor, so enqueue and publish readiness in
        // one ordered main-actor operation. Publishing ready before the layer
        // receives the frame creates a visible-but-empty PiP window.
        let frame = CapturedFrame(sampleBuffer)
        Task { @MainActor [weak controller, frame] in
            guard let controller else { return }
            controller.content.enqueue(frame.sampleBuffer)
            controller.showAfterFirstFrame(videoSize: size)
        }
    }

    func stream(_ stream: SCStream, didStopWithError error: Error) {
        lock.lock(); stopped = true; lock.unlock()
        let ns = error as NSError
        pipDebugLog("stream didStopWithError domain=\(ns.domain) code=\(ns.code)")
        writer.send("capture_status", fields: ["status": "error", "message": error.localizedDescription])
    }

    func isStopped() -> Bool { lock.withLock { stopped } }
    func freshnessSnapshot() -> CaptureFreshness { lock.withLock { freshness } }
    func lockFailure(_ error: Error) {
        lock.lock(); stopped = true; lock.unlock()
        writer.send("capture_status", fields: ["status": "error", "message": error.localizedDescription])
    }
}

private extension NSLock {
    func withLock<T>(_ body: () -> T) -> T { lock(); defer { unlock() }; return body() }
}

@available(macOS 14.0, *)
private func captureStatusName(_ status: SCFrameStatus) -> String {
    switch status {
    case .complete, .started: "healthy"
    case .idle: "idle"
    case .blank: "blank"
    case .suspended: "suspended"
    case .stopped: "stopped"
    @unknown default: "suspended"
    }
}

@available(macOS 14.0, *)
private func preferredStreamWindow(in content: SCShareableContent, processID: pid_t) -> SCWindow? {
    let candidates = content.windows.filter {
        $0.owningApplication?.processID == processID && $0.frame.width > 1 && $0.frame.height > 1
    }
    guard !candidates.isEmpty else { return nil }
    let largest = candidates.max { $0.frame.width * $0.frame.height < $1.frame.width * $1.frame.height }
    if let focusedFrame = focusedWindowFrame(processID: processID),
       let focused = candidates.min(by: { windowFrameDistance($0.frame, focusedFrame) < windowFrameDistance($1.frame, focusedFrame) }),
       focused.frame.width >= 120, focused.frame.height >= 80 { return focused }
    return largest
}

private func focusedWindowFrame(processID: pid_t) -> CGRect? {
    let application = AXUIElementCreateApplication(processID)
    var focusedValue: CFTypeRef?
    guard AXUIElementCopyAttributeValue(application, kAXFocusedWindowAttribute as CFString, &focusedValue) == .success,
          let focused = focusedValue as! AXUIElement? else { return nil }
    return axFrame(focused)
}

private func windowFrameDistance(_ left: CGRect, _ right: CGRect) -> CGFloat {
    abs(left.minX - right.minX) + abs(left.minY - right.minY) + abs(left.width - right.width) + abs(left.height - right.height)
}

@available(macOS 14.0, *)
private func streamConfiguration(for window: SCWindow) -> SCStreamConfiguration {
    let configuration = SCStreamConfiguration()
    let scale = window.owningApplication == nil ? 1 : NSScreen.main?.backingScaleFactor ?? 2
    configuration.width = max(1, Int(window.frame.width * scale))
    configuration.height = max(1, Int(window.frame.height * scale))
    configuration.queueDepth = 4
    configuration.showsCursor = false
    configuration.pixelFormat = kCVPixelFormatType_32BGRA
    configuration.ignoreShadowsSingleWindow = true
    return configuration
}

private func pipDebugLog(_ message: String) {
    FileHandle.standardError.write(Data("[pip] \(message)\n".utf8))
}

// Dev probe: report which capture APIs actually work for the target window, so a
// window ScreenCaptureKit refuses (e.g. sharing-restricted) can be told apart
// from a fixable SCStream problem. Logs only; changes no behavior.
@available(macOS 14.0, *)
private func pipProbeCapture(window: SCWindow) {
    if let list = CGWindowListCopyWindowInfo(.optionAll, kCGNullWindowID) as? [[String: Any]],
       let info = list.first(where: { ($0[kCGWindowNumber as String] as? NSNumber)?.uint32Value == window.windowID }) {
        let sharing = (info[kCGWindowSharingState as String] as? NSNumber)?.intValue ?? -1
        pipDebugLog("probe sharingState=\(sharing)")
    } else {
        pipDebugLog("probe sharingState=unknown")
    }
    if let image = CGWindowListCreateImage(.null, .optionIncludingWindow, window.windowID, [.boundsIgnoreFraming, .bestResolution]) {
        pipDebugLog("probe CGWindowListCreateImage ok \(image.width)x\(image.height)")
    } else {
        pipDebugLog("probe CGWindowListCreateImage nil")
    }
    let config = SCStreamConfiguration()
    config.width = max(1, Int(window.frame.width))
    config.height = max(1, Int(window.frame.height))
    SCScreenshotManager.captureImage(contentFilter: SCContentFilter(desktopIndependentWindow: window), configuration: config) { image, error in
        if let error {
            let ns = error as NSError
            pipDebugLog("probe SCScreenshotManager error domain=\(ns.domain) code=\(ns.code)")
        } else if let image {
            pipDebugLog("probe SCScreenshotManager ok \(image.width)x\(image.height)")
        } else {
            pipDebugLog("probe SCScreenshotManager nil")
        }
    }
}

// Guards a one-shot resume so the capture-start completion handler and its
// timeout can race without ever resuming the continuation twice.
private final class CaptureStartGate: @unchecked Sendable {
    private let lock = NSLock()
    private var settled = false
    func settle(_ body: () -> Void) {
        lock.lock(); defer { lock.unlock() }
        if settled { return }
        settled = true
        body()
    }
}

@available(macOS 14.0, *)
private func startWindowStream(window: SCWindow, output: NativePiPStreamOutput, timeout: TimeInterval) async throws -> SCStream {
    let stream = SCStream(
        filter: SCContentFilter(desktopIndependentWindow: window),
        configuration: streamConfiguration(for: window),
        delegate: output
    )
    try stream.addStreamOutput(output, type: .screen, sampleHandlerQueue: DispatchQueue(label: "com.blueberrycongee.wuu.cua.native-pip"))
    // ScreenCaptureKit can report a startup failure through the delegate's
    // didStopWithError instead of resuming startCapture's completion handler,
    // which leaks the async continuation and suspends the caller forever. Drive
    // the completion handler directly and race it against a timeout so a stalled
    // start surfaces as a thrown error the supervision loop can retry.
    let gate = CaptureStartGate()
    let started: Bool = await withCheckedContinuation { (continuation: CheckedContinuation<Bool, Never>) in
        stream.startCapture { error in
            gate.settle { continuation.resume(returning: error == nil) }
        }
        DispatchQueue.global().asyncAfter(deadline: .now() + timeout) {
            gate.settle { continuation.resume(returning: false) }
        }
    }
    guard started else {
        // A merely-slow start can still complete after the timeout fires, so
        // stop the stream — fire-and-forget via the completion handler so we
        // never re-hang on a genuinely stuck one — rather than leaking a stream
        // that keeps capturing with no output attached.
        try? stream.removeStreamOutput(output, type: .screen)
        stream.stopCapture { _ in }
        throw ComputerError.operationFailed("capture failed to start")
    }
    return stream
}

@MainActor
public func runNativePiP(configuration: NativePiPConfiguration) async throws {
    guard CGPreflightScreenCaptureAccess() else {
        throw ComputerError.permissionDenied("Screen Recording permission is required for live window capture")
    }
    guard #available(macOS 14.0, *) else {
        throw ComputerError.unsupported("native picture-in-picture requires macOS 14 or newer")
    }
    let normalized = configuration.target.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
    guard let app = NSWorkspace.shared.runningApplications.first(where: {
        if let processID = configuration.processID { return !$0.isTerminated && $0.processIdentifier == processID }
        return !$0.isTerminated && ($0.bundleIdentifier?.lowercased() == normalized || $0.localizedName?.lowercased() == normalized || $0.bundleURL?.path.lowercased() == normalized)
    }) else { throw ComputerError.appNotFound(configuration.target) }

    let writer = PiPEventWriter()
    let controller = NativePiPWindowController(configuration: configuration, writer: writer)
    controller.setIcon(app.icon)

    // Install the command reader before any capture work. `await` suspends this
    // task rather than blocking the main actor, so even if stream startup stalls
    // (ScreenCaptureKit can leak startCapture's continuation on failure), the
    // coordinator's `visible` command still reaches the main actor and shows the
    // frosted placeholder. It also means `close`/stdin EOF can always terminate
    // the helper, so a stuck capture never leaves an orphan process behind.
    let commandReader = PiPCommandReader()
    FileHandle.standardInput.readabilityHandler = { input in
        let data = input.availableData
        if data.isEmpty { exit(0) }
        for encoded in commandReader.append(data) {
            Task { @MainActor in
                guard let commandData = encoded.data(using: .utf8),
                      let command = try? JSONSerialization.jsonObject(with: commandData) as? [String: Any],
                      let type = command["type"] as? String else { return }
                switch type {
                case "visible": controller.setVisible(command["visible"] as? Bool == true)
                case "interaction": controller.animateInteraction(command)
                case "close": controller.panel.orderOut(nil); exit(0)
                default: break
                }
            }
        }
    }

    let geometryMonitor = WindowGeometryMonitor(processID: app.processIdentifier, windowFrame: .zero)
    let captureStartTimeout: TimeInterval = 3
    let startupTimeout: TimeInterval = 5
    let callbackSilenceTimeout: TimeInterval = 3
    let unavailableStatusTimeout: TimeInterval = 3

    var stream: SCStream?
    var output: NativePiPStreamOutput?
    var trackedWindowID: CGWindowID?
    var trackedFrame: CGRect = .zero
    var streamStartedAt = ProcessInfo.processInfo.systemUptime
    var lastVerifiedCaptureAt = streamStartedAt
    var establishFailures = 0
    var lastHealthyOutput: ObjectIdentifier?
    var lastSafetyRefresh = Date.distantPast
    var didProbe = false

    while !app.isTerminated && processIsAlive(configuration.parentProcessID) {
        // Establish (or re-establish) the capture stream whenever we do not have
        // a live one. Failures are non-fatal: the frosted placeholder stays up
        // and we back off and retry, matching how the reference implementation
        // keeps a placeholder when a capture stream stops with an error.
        if stream == nil || output?.isStopped() == true {
            if let existing = stream, let existingOutput = output {
                try? existing.removeStreamOutput(existingOutput, type: .screen)
                try? await existing.stopCapture()
            }
            stream = nil
            output = nil
            if establishFailures > 0 {
                if ProcessInfo.processInfo.systemUptime - lastVerifiedCaptureAt >= callbackSilenceTimeout {
                    controller.markCaptureUnavailable()
                }
                try await Task.sleep(for: .milliseconds(250 * (1 << min(5, establishFailures))))
            }
            guard let content = try? await SCShareableContent.excludingDesktopWindows(false, onScreenWindowsOnly: false),
                  let target = configuration.windowID.flatMap({ requested in content.windows.first(where: { $0.windowID == requested }) })
                    ?? preferredStreamWindow(in: content, processID: app.processIdentifier) else {
                establishFailures += 1
                continue
            }
            pipDebugLog("resolve window id=\(target.windowID) frame=\(target.frame) title=\(target.title ?? "-")")
            if !didProbe { didProbe = true; pipProbeCapture(window: target) }
            let next = NativePiPStreamOutput(controller: controller, writer: writer)
            do {
                stream = try await startWindowStream(window: target, output: next, timeout: captureStartTimeout)
                output = next
                trackedWindowID = target.windowID
                trackedFrame = target.frame
                geometryMonitor.rebind(nearestTo: target.frame)
                streamStartedAt = ProcessInfo.processInfo.systemUptime
                // Do NOT reset establishFailures here. A stream that starts but
                // never delivers a displayable frame (blank/suspended/DRM window)
                // must let the backoff escalate; the counter resets only when a
                // real frame arrives (freshness.hasDisplayedFrame below).
                pipDebugLog("capture started id=\(target.windowID)")
            } catch {
                establishFailures += 1
                pipDebugLog("capture start failed attempt=\(establishFailures)")
            }
            continue
        }

        guard let activeStream = stream, let activeOutput = output else { continue }
        try await Task.sleep(for: .milliseconds(100))
        let now = ProcessInfo.processInfo.systemUptime
        let freshness = activeOutput.freshnessSnapshot()
        if let healthyAt = freshness.lastHealthyCallbackUptime {
            lastVerifiedCaptureAt = max(lastVerifiedCaptureAt, healthyAt)
        }
        let outputID = ObjectIdentifier(activeOutput)
        if freshness.hasDisplayedFrame, lastHealthyOutput != outputID {
            establishFailures = 0
            lastHealthyOutput = outputID
        }
        let recoveryRequired = freshness.requiresRecovery(
            at: now,
            streamStartedAt: streamStartedAt,
            startupTimeout: startupTimeout,
            callbackSilenceTimeout: callbackSilenceTimeout,
            unavailableStatusTimeout: unavailableStatusTimeout
        )
        if activeOutput.isStopped() || recoveryRequired {
            try? activeStream.removeStreamOutput(activeOutput, type: .screen)
            try? await activeStream.stopCapture()
            if now - lastVerifiedCaptureAt >= callbackSilenceTimeout {
                controller.markCaptureUnavailable()
            }
            establishFailures += 1
            stream = nil
            output = nil
            continue
        }
        if !freshness.hasDisplayedFrame, now - lastVerifiedCaptureAt >= callbackSilenceTimeout {
            controller.markCaptureUnavailable()
        }
        let safetyRefreshDue = Date().timeIntervalSince(lastSafetyRefresh) >= 5
        guard geometryMonitor.takeDirty() || safetyRefreshDue else { continue }
        lastSafetyRefresh = Date()
        guard let refreshed = try? await SCShareableContent.excludingDesktopWindows(false, onScreenWindowsOnly: false),
              let nextWindow = refreshed.windows.first(where: { $0.windowID == (trackedWindowID ?? 0) }) else { continue }
        guard windowFrameDistance(nextWindow.frame, trackedFrame) > 1 else { continue }
        do {
            try await activeStream.updateConfiguration(streamConfiguration(for: nextWindow))
            trackedFrame = nextWindow.frame
        } catch { activeOutput.lockFailure(error) }
    }
    if let stream, let output {
        try? stream.removeStreamOutput(output, type: .screen)
        try? await stream.stopCapture()
    }
}

private func processIsAlive(_ processID: pid_t?) -> Bool {
    guard let processID, processID > 0 else { return true }
    return kill(processID, 0) == 0 || errno == EPERM
}
