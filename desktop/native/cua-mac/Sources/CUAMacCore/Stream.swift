import AppKit
import ApplicationServices
@preconcurrency import CoreMedia
import Foundation
@preconcurrency import ScreenCaptureKit

private final class FramePipeWriter: @unchecked Sendable {
    private let queue = DispatchQueue(label: "com.blueberrycongee.wuu.cua.frame-writer")
    private let lock = NSLock()
    private var writing = false
    private var revision: UInt64 = 0

    func submit(_ sampleBuffer: CMSampleBuffer, frame: CGRect) {
        lock.lock()
        guard !writing else { lock.unlock(); return }
        writing = true
        lock.unlock()
        queue.async { [weak self] in
            guard let self else { return }
            defer {
                self.lock.lock()
                self.writing = false
                self.lock.unlock()
            }
            guard let imageBuffer = sampleBuffer.imageBuffer else { return }
            // A desktop-independent window stream already contains the complete
            // window. SCStreamFrameInfo.contentRect is expressed in window-space
            // points, while this buffer uses the configured pixel dimensions.
            // Cropping the pixel buffer with that unscaled rectangle truncates the
            // right and bottom of the window, especially when it is partly off-screen.
            let image = CIImage(cvImageBuffer: imageBuffer)
            let context = CIContext(options: [.cacheIntermediates: false])
            guard let colorSpace = CGColorSpace(name: CGColorSpace.sRGB),
                  let data = context.jpegRepresentation(
                    of: image,
                    colorSpace: colorSpace,
                    options: [kCGImageDestinationLossyCompressionQuality as CIImageRepresentationOption: 0.72]
                  ) else { return }
            self.revision += 1
            self.write(metadata: [
                "event": "frame",
                "revision": self.revision,
                "timestamp_ns": DispatchTime.now().uptimeNanoseconds,
                "width": Int(image.extent.width),
                "height": Int(image.extent.height),
                "capture_mode": "full_window",
                "window_frame": ["x": frame.origin.x, "y": frame.origin.y, "width": frame.width, "height": frame.height],
                "mime_type": "image/jpeg",
            ], payload: data)
        }
    }

    func event(_ name: String, message: String? = nil) {
        queue.sync {
            var metadata: [String: Any] = ["event": name, "timestamp_ns": DispatchTime.now().uptimeNanoseconds]
            if let message { metadata["message"] = message }
            write(metadata: metadata, payload: Data())
        }
    }

    func writeCapture(_ capture: WindowCapture, mode: String = "visible_fallback") -> Bool {
        queue.sync {
            guard let bitmap = NSBitmapImageRep(data: capture.data),
                  let data = bitmap.representation(using: .jpeg, properties: [.compressionFactor: 0.72]) else {
                return false
            }
            revision += 1
            write(metadata: [
                "event": "frame",
                "revision": revision,
                "timestamp_ns": DispatchTime.now().uptimeNanoseconds,
                "width": bitmap.pixelsWide,
                "height": bitmap.pixelsHigh,
                "capture_mode": mode,
                "window_frame": [
                    "x": capture.geometry.windowFrame.origin.x,
                    "y": capture.geometry.windowFrame.origin.y,
                    "width": capture.geometry.windowFrame.width,
                    "height": capture.geometry.windowFrame.height,
                ],
                "mime_type": "image/jpeg",
            ], payload: data)
            return true
        }
    }

    private func write(metadata: [String: Any], payload: Data) {
        guard let encoded = try? JSONSerialization.data(withJSONObject: metadata, options: [.sortedKeys]) else { return }
        var header = Data()
        var metadataLength = UInt32(encoded.count).bigEndian
        var payloadLength = UInt32(payload.count).bigEndian
        withUnsafeBytes(of: &metadataLength) { header.append(contentsOf: $0) }
        withUnsafeBytes(of: &payloadLength) { header.append(contentsOf: $0) }
        FileHandle.standardOutput.write(header)
        FileHandle.standardOutput.write(encoded)
        if !payload.isEmpty { FileHandle.standardOutput.write(payload) }
    }
}

private final class UserInputMonitor: @unchecked Sendable {
    let processID: pid_t
    let writer: FramePipeWriter
    private let lock = NSLock()
    private var lastSignal = Date.distantPast

    init(processID: pid_t, writer: FramePipeWriter) {
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
        if shouldSignal { writer.event("user_input") }
    }
}

private let userInputCallback: CGEventTapCallBack = { _, _, event, refcon in
    guard let refcon else { return Unmanaged.passUnretained(event) }
    Unmanaged<UserInputMonitor>.fromOpaque(refcon).takeUnretainedValue().handle(event)
    return Unmanaged.passUnretained(event)
}

@available(macOS 14.0, *)
private final class WindowStreamOutput: NSObject, SCStreamOutput, SCStreamDelegate, @unchecked Sendable {
    let writer: FramePipeWriter
    private let lock = NSLock()
    private var frame: CGRect
    private var stopped = false
    private var firstFrameReceived = false
    private var lastFrameAt: Date?
    private var failure: Error?

    init(writer: FramePipeWriter, frame: CGRect) {
        self.writer = writer
        self.frame = frame
    }

    func stream(_ stream: SCStream, didOutputSampleBuffer sampleBuffer: CMSampleBuffer, of type: SCStreamOutputType) {
        guard type == .screen, sampleBuffer.isValid, sampleBuffer.imageBuffer != nil else { return }
        lock.lock()
        let currentFrame = frame
        firstFrameReceived = true
        lastFrameAt = Date()
        lock.unlock()
        writer.submit(sampleBuffer, frame: currentFrame)
    }

    func stream(_ stream: SCStream, didStopWithError error: Error) {
        lock.lock()
        failure = error
        stopped = true
        lock.unlock()
        CFRunLoopWakeUp(CFRunLoopGetMain())
    }

    func isStopped() -> Bool {
        lock.lock()
        defer { lock.unlock() }
        return stopped
    }

    func receivedFirstFrame() -> Bool {
        lock.lock()
        defer { lock.unlock() }
        return firstFrameReceived
    }

    func secondsSinceLastFrame(now: Date = Date()) -> TimeInterval? {
        lock.lock()
        defer { lock.unlock() }
        return lastFrameAt.map { now.timeIntervalSince($0) }
    }

    func streamFailure() -> Error? {
        lock.lock()
        defer { lock.unlock() }
        return failure
    }

    func lockFailure(_ error: Error) {
        lock.lock()
        failure = error
        stopped = true
        lock.unlock()
    }

    func updateFrame(_ nextFrame: CGRect) {
        lock.lock()
        frame = nextFrame
        lock.unlock()
    }

    func currentFrame() -> CGRect {
        lock.lock()
        defer { lock.unlock() }
        return frame
    }

    // Clear the terminal flags so this same output object can be reattached to a freshly
    // built SCStream after a recoverable failure instead of ending the whole session.
    func resetForRebuild() {
        lock.lock()
        failure = nil
        stopped = false
        firstFrameReceived = false
        lastFrameAt = nil
        lock.unlock()
    }
}

@available(macOS 14.0, *)
private func preferredStreamWindow(in content: SCShareableContent, processID: pid_t) -> SCWindow? {
    let candidates = content.windows.filter {
        $0.owningApplication?.processID == processID && $0.frame.width > 1 && $0.frame.height > 1
    }
    guard !candidates.isEmpty else { return nil }
    let largest = candidates.max { left, right in
        left.frame.width * left.frame.height < right.frame.width * right.frame.height
    }
    if let focusedFrame = focusedWindowFrame(processID: processID),
       let focused = candidates.min(by: { left, right in
            windowFrameDistance(left.frame, focusedFrame) < windowFrameDistance(right.frame, focusedFrame)
       }),
       focused.frame.width >= 120, focused.frame.height >= 80 {
        return focused
    }
    return largest
}

private func focusedWindowFrame(processID: pid_t) -> CGRect? {
    let application = AXUIElementCreateApplication(processID)
    var focusedValue: CFTypeRef?
    guard AXUIElementCopyAttributeValue(application, kAXFocusedWindowAttribute as CFString, &focusedValue) == .success,
          let focused = focusedValue as! AXUIElement? else {
        return nil
    }
    return axFrame(focused)
}

private func windowFrameDistance(_ left: CGRect, _ right: CGRect) -> CGFloat {
    abs(left.minX - right.minX)
        + abs(left.minY - right.minY)
        + abs(left.width - right.width)
        + abs(left.height - right.height)
}

@available(macOS 14.0, *)
private func streamConfiguration(for window: SCWindow) -> SCStreamConfiguration {
    let configuration = SCStreamConfiguration()
    configuration.width = max(1, Int(window.frame.width * 1.25))
    configuration.height = max(1, Int(window.frame.height * 1.25))
    configuration.minimumFrameInterval = CMTime(value: 1, timescale: 12)
    configuration.queueDepth = 2
    configuration.showsCursor = false
    return configuration
}

@available(macOS 14.0, *)
private func startWindowStream(window: SCWindow, output: WindowStreamOutput) throws -> SCStream {
    let stream = SCStream(
        filter: SCContentFilter(desktopIndependentWindow: window),
        configuration: streamConfiguration(for: window),
        delegate: output
    )
    try stream.addStreamOutput(output, type: .screen, sampleHandlerQueue: DispatchQueue(label: "com.blueberrycongee.wuu.cua.capture"))
    stream.startCapture { error in
        guard let error else { return }
        output.lockFailure(error)
    }
    return stream
}

// Pulls one COMPLETE-window frame via SCScreenshotManager(desktopIndependentWindow), which —
// unlike the live SCStream — keeps returning the whole window while it is idle or parked
// fully off-screen. It only runs while the live stream is silent, dedupes identical frames,
// and is tagged "full_window". The legacy on-screen-only captureForegroundWindowPNG path is
// intentionally never used here, so the PiP can no longer show a truncated window.
@available(macOS 14.0, *)
private func runFullWindowHeartbeat(processID: pid_t, output: WindowStreamOutput, writer: FramePipeWriter) async {
    var lastData: Data?
    var idleStreak = 0
    while !Task.isCancelled {
        let streamIsFresh = output.secondsSinceLastFrame().map { $0 <= 0.9 } ?? false
        if streamIsFresh {
            idleStreak = 0
        } else {
            let frame = output.currentFrame()
            // Light path for a live frame: no per-pixel visible-bounds scan and a modest
            // scale, so a persistently idle/off-screen window doesn't burn a CPU core.
            let capture = try? await Task.detached {
                try captureWindowPNG(
                    processID: processID,
                    timeout: 2,
                    preferredWindowFrame: frame,
                    scale: 1.25,
                    computeVisibleBounds: false
                )
            }.value
            if let capture, capture.data != lastData {
                _ = writer.writeCapture(capture, mode: "full_window")
                lastData = capture.data
                idleStreak = 0
            } else {
                idleStreak = min(idleStreak + 1, 6)
            }
        }
        // ~3fps while the window is actively changing off-screen, backing off toward ~1fps
        // once it goes static, so watching stays smooth without a constant full-window pull.
        try? await Task.sleep(for: .milliseconds(300 + idleStreak * 120))
    }
}

public func runWindowFrameStream(target: String) async throws {
    guard CGPreflightScreenCaptureAccess() else {
        throw ComputerError.permissionDenied("Screen Recording permission is required for live window capture")
    }
    guard #available(macOS 14.0, *) else {
        throw ComputerError.unsupported("live window capture requires macOS 14 or newer")
    }
    let normalized = target.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
    guard !normalized.isEmpty else { throw ComputerError.invalidArguments("stream target is required") }
    let app = NSWorkspace.shared.runningApplications.first {
        !$0.isTerminated && (
            $0.bundleIdentifier?.lowercased() == normalized ||
            $0.localizedName?.lowercased() == normalized ||
            $0.bundleURL?.path.lowercased() == normalized
        )
    }
    guard let app else { throw ComputerError.appNotFound(target) }

    let writer = FramePipeWriter()
    let content = try await SCShareableContent.excludingDesktopWindows(false, onScreenWindowsOnly: false)
    guard var window = preferredStreamWindow(in: content, processID: app.processIdentifier) else {
        throw ComputerError.operationFailed("no capturable window found")
    }
    let output = WindowStreamOutput(writer: writer, frame: window.frame)
    var stream = try startWindowStream(window: window, output: output)
    writer.event("started")

    // The live SCStream only fires on content change and can go fully silent for a
    // concealed (off-screen) window. A full-window heartbeat guarantees the PiP keeps
    // showing the COMPLETE window regardless, and liveness is judged by real stream
    // errors — never by frame timing — so an idle window no longer degrades the capture.
    let heartbeatProcessID = app.processIdentifier
    let heartbeat = Task.detached { [output, writer] in
        await runFullWindowHeartbeat(processID: heartbeatProcessID, output: output, writer: writer)
    }
    defer { heartbeat.cancel() }
    let inputMonitor = UserInputMonitor(processID: app.processIdentifier, writer: writer)
    let eventTypes: [CGEventType] = [.leftMouseDown, .rightMouseDown, .otherMouseDown, .keyDown, .scrollWheel]
    let eventMask = eventTypes.reduce(CGEventMask(0)) { $0 | (CGEventMask(1) << CGEventMask($1.rawValue)) }
    let monitorRef = Unmanaged.passUnretained(inputMonitor).toOpaque()
    let eventTap = CGEvent.tapCreate(
        tap: .cgSessionEventTap,
        place: .tailAppendEventTap,
        options: .listenOnly,
        eventsOfInterest: eventMask,
        callback: userInputCallback,
        userInfo: monitorRef
    )
    var eventSource: CFRunLoopSource?
    if let eventTap {
        eventSource = CFMachPortCreateRunLoopSource(kCFAllocatorDefault, eventTap, 0)
        if let eventSource { CFRunLoopAddSource(CFRunLoopGetMain(), eventSource, .commonModes) }
        CGEvent.tapEnable(tap: eventTap, enable: true)
    }
    while !app.isTerminated {
        try await Task.sleep(for: .milliseconds(750))
        if app.isTerminated { break }
        // Only a real stream error tears anything down; rebuild the stream in place while
        // the heartbeat keeps full-window frames flowing. No permanent polling fallback.
        if output.streamFailure() != nil {
            try? stream.removeStreamOutput(output, type: .screen)
            try? await stream.stopCapture()
            // Clear the failure flag only once a fresh stream is actually up; otherwise leave
            // it set so the next tick retries the rebuild instead of driving a dead stream.
            if let rebuilt = try? startWindowStream(window: window, output: output) {
                stream = rebuilt
                output.resetForRebuild()
            }
            continue
        }
        guard let refreshedContent = try? await SCShareableContent.excludingDesktopWindows(false, onScreenWindowsOnly: false) else {
            continue
        }
        let currentWindow = refreshedContent.windows.first(where: { $0.windowID == window.windowID })
        let preferredWindow = preferredStreamWindow(in: refreshedContent, processID: app.processIdentifier)
        let focusedFrame = focusedWindowFrame(processID: app.processIdentifier)
        let focusMovedToPreferred = preferredWindow.map { preferred in
            preferred.windowID != window.windowID
                && focusedFrame.map { windowFrameDistance(preferred.frame, $0) <= 4 } == true
        } ?? false
        let refreshedWindow = focusMovedToPreferred ? preferredWindow : (currentWindow ?? preferredWindow)
        guard let refreshedWindow else { continue }
        let windowChanged = refreshedWindow.windowID != window.windowID
        let frameChanged = windowFrameDistance(refreshedWindow.frame, window.frame) > 1
        guard windowChanged || frameChanged else { continue }
        do {
            if windowChanged {
                try await stream.updateContentFilter(SCContentFilter(desktopIndependentWindow: refreshedWindow))
            }
            if frameChanged {
                try await stream.updateConfiguration(streamConfiguration(for: refreshedWindow))
            }
        } catch {
            // A live reconfigure failure means the stream is unhealthy; fold it into the
            // rebuild path on the next tick instead of killing the whole helper.
            output.lockFailure(error)
            continue
        }
        window = refreshedWindow
        output.updateFrame(refreshedWindow.frame)
    }
    heartbeat.cancel()
    withExtendedLifetime(inputMonitor) {}
    if let eventSource { CFRunLoopRemoveSource(CFRunLoopGetMain(), eventSource, .commonModes) }
    try? await stream.stopCapture()
}
