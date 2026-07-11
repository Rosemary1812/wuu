import AppKit
import ApplicationServices
import AVFoundation
@preconcurrency import CoreMedia
import Foundation
import QuartzCore
@preconcurrency import ScreenCaptureKit

public struct NativePiPConfiguration: Sendable {
    public let activityID: String
    public let target: String
    public let frame: CGRect

    public init(activityID: String, target: String, frame: CGRect) {
        self.activityID = activityID
        self.target = target
        self.frame = frame
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

@MainActor
private final class NativePiPView: NSView {
    private nonisolated(unsafe) let displayLayer = AVSampleBufferDisplayLayer()
    private let pointerLayer = CAShapeLayer()
    private let rippleLayer = CAShapeLayer()
    private let closeButton = NSButton()
    private var trackingArea: NSTrackingArea?
    private var videoSize = CGSize(width: 16, height: 9)
    private var pointerPosition = CGPoint(x: 0.5, y: 0.5)
    var onClose: (() -> Void)?

    override init(frame frameRect: NSRect) {
        super.init(frame: frameRect)
        wantsLayer = true
        layer?.backgroundColor = NSColor.windowBackgroundColor.withAlphaComponent(0.92).cgColor
        layer?.cornerRadius = 14
        layer?.masksToBounds = true

        displayLayer.videoGravity = .resizeAspect
        displayLayer.backgroundColor = NSColor.black.cgColor
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
    }

    required init?(coder: NSCoder) { nil }

    override func layout() {
        super.layout()
        CATransaction.begin()
        CATransaction.setDisableActions(true)
        displayLayer.frame = bounds
        closeButton.frame = CGRect(x: bounds.maxX - 34, y: bounds.maxY - 34, width: 26, height: 26)
        placePointer()
        CATransaction.commit()
    }

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

    nonisolated func enqueue(_ sampleBuffer: CMSampleBuffer) {
        CMSetAttachment(
            sampleBuffer,
            key: kCMSampleAttachmentKey_DisplayImmediately,
            value: kCFBooleanTrue,
            attachmentMode: kCMAttachmentMode_ShouldPropagate
        )
        if displayLayer.status == .failed { displayLayer.flush() }
        displayLayer.enqueue(sampleBuffer)
        if let imageBuffer = sampleBuffer.imageBuffer {
            let size = CGSize(width: CVPixelBufferGetWidth(imageBuffer), height: CVPixelBufferGetHeight(imageBuffer))
            Task { @MainActor [weak self] in self?.setVideoSize(size) }
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

@MainActor
private final class NativePiPWindowController: NSObject, NSWindowDelegate {
    let panel: NSPanel
    let content: NativePiPView
    private let writer: PiPEventWriter
    private var shown = false

    init(configuration: NativePiPConfiguration, writer: PiPEventWriter) {
        self.writer = writer
        content = NativePiPView(frame: CGRect(origin: .zero, size: configuration.frame.size))
        let desktopTop = NSScreen.screens.first?.frame.maxY ?? 0
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
            self?.writer.send("user_close")
            self?.panel.orderOut(nil)
        }
    }

    func showAfterFirstFrame(videoSize: CGSize) {
        content.setVideoSize(videoSize)
        let ratio = videoSize.width / max(1, videoSize.height)
        panel.contentAspectRatio = CGSize(width: ratio, height: 1)
        guard !shown else { return }
        let fitted = fittedPiPSize(ratio: ratio)
        let current = panel.frame
        panel.setFrame(
            CGRect(x: current.maxX - fitted.width, y: current.maxY - fitted.height, width: fitted.width, height: fitted.height),
            display: false
        )
        shown = true
        panel.orderFrontRegardless()
        writer.send("ready", fields: ["width": Int(videoSize.width), "height": Int(videoSize.height)])
    }

    func setVisible(_ visible: Bool) {
        if visible, shown { panel.orderFrontRegardless() } else { panel.orderOut(nil) }
    }

    func animateInteraction(_ payload: [String: Any]) { content.animateInteraction(payload) }

    func windowWillResize(_ sender: NSWindow, to frameSize: NSSize) -> NSSize {
        let ratio = max(0.01, panel.contentAspectRatio.width / max(0.01, panel.contentAspectRatio.height))
        let widthDriven = CGSize(width: frameSize.width, height: frameSize.width / ratio)
        if widthDriven.height <= panel.maxSize.height { return widthDriven }
        return CGSize(width: frameSize.height * ratio, height: frameSize.height)
    }

    private func fittedPiPSize(ratio: CGFloat) -> CGSize {
        let preferredWidth: CGFloat = ratio >= 1.35 ? 409 : (ratio <= 0.75 ? 280 : 307)
        var width = preferredWidth
        var height = width / max(0.01, ratio)
        if height > 800 { height = 800; width = height * ratio }
        if height < 140 { height = 140; width = height * ratio }
        if width > 720 { width = 720; height = width / ratio }
        return CGSize(width: max(220, width), height: max(140, height))
    }
}

private final class WindowGeometryMonitor: @unchecked Sendable {
    private let lock = NSLock()
    private var dirty = false
    private var observer: AXObserver?
    private let application: AXUIElement
    private var focusedWindow: AXUIElement?

    init(processID: pid_t) {
        application = AXUIElementCreateApplication(processID)
        var created: AXObserver?
        guard AXObserverCreate(processID, geometryObserverCallback, &created) == .success, let created else { return }
        observer = created
        let refcon = Unmanaged.passUnretained(self).toOpaque()
        AXObserverAddNotification(created, application, kAXFocusedWindowChangedNotification as CFString, refcon)
        CFRunLoopAddSource(CFRunLoopGetMain(), AXObserverGetRunLoopSource(created), .commonModes)
        refreshFocusedWindow()
    }

    deinit {
        if let observer { CFRunLoopRemoveSource(CFRunLoopGetMain(), AXObserverGetRunLoopSource(observer), .commonModes) }
    }

    func handle(_ notification: CFString) {
        markDirty()
        if notification as String == kAXFocusedWindowChangedNotification {
            DispatchQueue.main.async { [weak self] in self?.refreshFocusedWindow() }
        }
    }

    func takeDirty() -> Bool {
        lock.lock(); defer { lock.unlock() }
        let value = dirty
        dirty = false
        return value
    }

    private func markDirty() { lock.withLock { dirty = true } }

    private func refreshFocusedWindow() {
        guard let observer else { return }
        let refcon = Unmanaged.passUnretained(self).toOpaque()
        if let focusedWindow {
            AXObserverRemoveNotification(observer, focusedWindow, kAXMovedNotification as CFString)
            AXObserverRemoveNotification(observer, focusedWindow, kAXResizedNotification as CFString)
            AXObserverRemoveNotification(observer, focusedWindow, kAXUIElementDestroyedNotification as CFString)
        }
        var value: CFTypeRef?
        guard AXUIElementCopyAttributeValue(application, kAXFocusedWindowAttribute as CFString, &value) == .success,
              let window = value as! AXUIElement? else { focusedWindow = nil; return }
        focusedWindow = window
        AXObserverAddNotification(observer, window, kAXMovedNotification as CFString, refcon)
        AXObserverAddNotification(observer, window, kAXResizedNotification as CFString, refcon)
        AXObserverAddNotification(observer, window, kAXUIElementDestroyedNotification as CFString, refcon)
    }
}

private let geometryObserverCallback: AXObserverCallback = { _, _, notification, refcon in
    guard let refcon else { return }
    Unmanaged<WindowGeometryMonitor>.fromOpaque(refcon).takeUnretainedValue().handle(notification)
}

private final class UserInputMonitor: @unchecked Sendable {
    let processID: pid_t
    let writer: PiPEventWriter
    private let lock = NSLock()
    private var lastSignal = Date.distantPast

    init(processID: pid_t, writer: PiPEventWriter) {
        self.processID = processID
        self.writer = writer
    }

    func handle(_ event: CGEvent) {
        guard event.getIntegerValueField(.eventSourceUserData) != wuuSyntheticEventMarker,
              NSWorkspace.shared.frontmostApplication?.processIdentifier == processID else { return }
        lock.lock()
        let shouldSignal = Date().timeIntervalSince(lastSignal) > 0.5
        if shouldSignal { lastSignal = Date() }
        lock.unlock()
        if shouldSignal { writer.send("user_input") }
    }
}

private let userInputCallback: CGEventTapCallBack = { _, _, event, refcon in
    guard let refcon else { return Unmanaged.passUnretained(event) }
    Unmanaged<UserInputMonitor>.fromOpaque(refcon).takeUnretainedValue().handle(event)
    return Unmanaged.passUnretained(event)
}

@available(macOS 14.0, *)
private final class NativePiPStreamOutput: NSObject, SCStreamOutput, SCStreamDelegate, @unchecked Sendable {
    let controller: NativePiPWindowController
    let writer: PiPEventWriter
    private let lock = NSLock()
    private var stopped = false
    private var receivedFrame = false
    private var lastStatus: SCFrameStatus?

    init(controller: NativePiPWindowController, writer: PiPEventWriter) {
        self.controller = controller
        self.writer = writer
    }

    func stream(_ stream: SCStream, didOutputSampleBuffer sampleBuffer: CMSampleBuffer, of type: SCStreamOutputType) {
        guard type == .screen, sampleBuffer.isValid else { return }
        let attachments = CMSampleBufferGetSampleAttachmentsArray(sampleBuffer, createIfNecessary: false)
            as? [[SCStreamFrameInfo: Any]]
        let status = (attachments?.first?[.status] as? NSNumber).flatMap { SCFrameStatus(rawValue: $0.intValue) }
        lock.lock()
        let statusChanged = status != nil && status != lastStatus
        if let status { lastStatus = status }
        if status == .stopped { stopped = true }
        let shouldDisplay = sampleBuffer.imageBuffer != nil && (status == nil || status == .complete || status == .started)
        if shouldDisplay { receivedFrame = true }
        lock.unlock()
        if statusChanged, let status { writer.send("capture_status", fields: ["status": captureStatusName(status)]) }
        guard shouldDisplay, let imageBuffer = sampleBuffer.imageBuffer else { return }
        controller.content.enqueue(sampleBuffer)
        let size = CGSize(width: CVPixelBufferGetWidth(imageBuffer), height: CVPixelBufferGetHeight(imageBuffer))
        Task { @MainActor [weak controller] in controller?.showAfterFirstFrame(videoSize: size) }
    }

    func stream(_ stream: SCStream, didStopWithError error: Error) {
        lock.lock(); stopped = true; lock.unlock()
        writer.send("capture_status", fields: ["status": "error", "message": error.localizedDescription])
    }

    func isStopped() -> Bool { lock.withLock { stopped } }
    func hasFrame() -> Bool { lock.withLock { receivedFrame } }
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
    return configuration
}

@available(macOS 14.0, *)
private func startWindowStream(window: SCWindow, output: NativePiPStreamOutput) throws -> SCStream {
    let stream = SCStream(
        filter: SCContentFilter(desktopIndependentWindow: window),
        configuration: streamConfiguration(for: window),
        delegate: output
    )
    try stream.addStreamOutput(output, type: .screen, sampleHandlerQueue: DispatchQueue(label: "com.blueberrycongee.wuu.cua.native-pip"))
    stream.startCapture { error in if let error { output.lockFailure(error) } }
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
        !$0.isTerminated && ($0.bundleIdentifier?.lowercased() == normalized || $0.localizedName?.lowercased() == normalized || $0.bundleURL?.path.lowercased() == normalized)
    }) else { throw ComputerError.appNotFound(configuration.target) }

    let writer = PiPEventWriter()
    let controller = NativePiPWindowController(configuration: configuration, writer: writer)
    let geometryMonitor = WindowGeometryMonitor(processID: app.processIdentifier)
    let content = try await SCShareableContent.excludingDesktopWindows(false, onScreenWindowsOnly: false)
    guard var window = preferredStreamWindow(in: content, processID: app.processIdentifier) else {
        throw ComputerError.operationFailed("no capturable window found")
    }
    var output = NativePiPStreamOutput(controller: controller, writer: writer)
    var stream = try startWindowStream(window: window, output: output)
    var streamStartedAt = Date()
    var consecutiveStreamFailures = 0

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

    let inputMonitor = UserInputMonitor(processID: app.processIdentifier, writer: writer)
    let eventMask = [CGEventType.leftMouseDown, .rightMouseDown, .otherMouseDown, .keyDown, .scrollWheel]
        .reduce(CGEventMask(0)) { $0 | (CGEventMask(1) << CGEventMask($1.rawValue)) }
    let eventTap = CGEvent.tapCreate(
        tap: .cgSessionEventTap, place: .tailAppendEventTap, options: .listenOnly,
        eventsOfInterest: eventMask, callback: userInputCallback,
        userInfo: Unmanaged.passUnretained(inputMonitor).toOpaque()
    )
    let eventSource = eventTap.map { CFMachPortCreateRunLoopSource(kCFAllocatorDefault, $0, 0) }
    if let eventTap, let eventSource {
        CFRunLoopAddSource(CFRunLoopGetMain(), eventSource, .commonModes)
        CGEvent.tapEnable(tap: eventTap, enable: true)
    }

    var lastSafetyRefresh = Date.distantPast
    while !app.isTerminated {
        try await Task.sleep(for: .milliseconds(100))
        if output.hasFrame() { consecutiveStreamFailures = 0 }
        if !output.hasFrame(), Date().timeIntervalSince(streamStartedAt) >= 5 {
            try? stream.removeStreamOutput(output, type: .screen)
            try? await stream.stopCapture()
            throw ComputerError.operationFailed("native window capture did not produce a first frame")
        }
        if output.isStopped() {
            consecutiveStreamFailures += 1
            try? stream.removeStreamOutput(output, type: .screen)
            try? await stream.stopCapture()
            if consecutiveStreamFailures >= 4 {
                throw ComputerError.operationFailed("native window capture repeatedly stopped")
            }
            let retryDelay = min(2_000, 250 * (1 << consecutiveStreamFailures))
            try await Task.sleep(for: .milliseconds(retryDelay))
            let next = NativePiPStreamOutput(controller: controller, writer: writer)
            do {
                stream = try startWindowStream(window: window, output: next)
                output = next
                streamStartedAt = Date()
            } catch {
                if consecutiveStreamFailures >= 3 { throw error }
            }
            continue
        }
        let safetyRefreshDue = Date().timeIntervalSince(lastSafetyRefresh) >= 5
        guard geometryMonitor.takeDirty() || safetyRefreshDue else { continue }
        lastSafetyRefresh = Date()
        guard let refreshed = try? await SCShareableContent.excludingDesktopWindows(false, onScreenWindowsOnly: false),
              let nextWindow = refreshed.windows.first(where: { $0.windowID == window.windowID }) ?? preferredStreamWindow(in: refreshed, processID: app.processIdentifier) else { continue }
        let changedWindow = nextWindow.windowID != window.windowID
        let changedFrame = windowFrameDistance(nextWindow.frame, window.frame) > 1
        guard changedWindow || changedFrame else { continue }
        do {
            if changedWindow { try await stream.updateContentFilter(SCContentFilter(desktopIndependentWindow: nextWindow)) }
            if changedFrame { try await stream.updateConfiguration(streamConfiguration(for: nextWindow)) }
            window = nextWindow
        } catch { output.lockFailure(error) }
    }
    if let eventSource { CFRunLoopRemoveSource(CFRunLoopGetMain(), eventSource, .commonModes) }
    try? await stream.stopCapture()
}
