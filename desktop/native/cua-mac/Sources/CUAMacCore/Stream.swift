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

    func writeCapture(_ capture: WindowCapture) -> Bool {
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
    let configuration = streamConfiguration(for: window)
    let output = WindowStreamOutput(writer: writer, frame: window.frame)
    let stream = SCStream(filter: SCContentFilter(desktopIndependentWindow: window), configuration: configuration, delegate: output)
    try stream.addStreamOutput(output, type: .screen, sampleHandlerQueue: DispatchQueue(label: "com.blueberrycongee.wuu.cua.capture"))
    stream.startCapture { error in
        guard let error else { return }
        output.lockFailure(error)
    }
    let firstFrameDeadline = Date(timeIntervalSinceNow: 3)
    while !output.receivedFirstFrame(), output.streamFailure() == nil, Date() < firstFrameDeadline {
        try await Task.sleep(for: .milliseconds(50))
    }
    if !output.receivedFirstFrame() {
        try? stream.removeStreamOutput(output, type: .screen)
        var previousCapture: Data?
        var emittedFirstFrame = false
        while true {
            let processID = app.processIdentifier
            let preferredFrame = focusedWindowFrame(processID: processID)
            let capture = try await Task.detached {
                do {
                    return try captureWindowPNG(
                        processID: processID,
                        timeout: 3,
                        preferredWindowFrame: preferredFrame
                    )
                } catch {
                    return try captureForegroundWindowPNG(processID: processID)
                }
            }.value
            if previousCapture != capture.data {
                guard writer.writeCapture(capture) else {
                    throw ComputerError.operationFailed("live window capture could not encode a frame")
                }
                previousCapture = capture.data
                if !emittedFirstFrame {
                    writer.event("started")
                    emittedFirstFrame = true
                }
            }
            try await Task.sleep(for: .milliseconds(125))
        }
    }
    writer.event("started")
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
    while !output.isStopped() {
        try await Task.sleep(for: .milliseconds(750))
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
        if windowChanged {
            try await stream.updateContentFilter(SCContentFilter(desktopIndependentWindow: refreshedWindow))
        }
        if frameChanged {
            try await stream.updateConfiguration(streamConfiguration(for: refreshedWindow))
        }
        window = refreshedWindow
        output.updateFrame(refreshedWindow.frame)
    }
    let streamFailure = output.streamFailure()
    withExtendedLifetime(inputMonitor) {}
    if let eventSource { CFRunLoopRemoveSource(CFRunLoopGetMain(), eventSource, .commonModes) }
    if let streamFailure { throw streamFailure }
    try? await stream.stopCapture()
}
