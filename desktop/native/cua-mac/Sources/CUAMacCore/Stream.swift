import AppKit
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
                "width": CVPixelBufferGetWidth(imageBuffer),
                "height": CVPixelBufferGetHeight(imageBuffer),
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
    let frame: CGRect
    private let lock = NSLock()
    private var stopped = false

    init(writer: FramePipeWriter, frame: CGRect) {
        self.writer = writer
        self.frame = frame
    }

    func stream(_ stream: SCStream, didOutputSampleBuffer sampleBuffer: CMSampleBuffer, of type: SCStreamOutputType) {
        guard type == .screen, sampleBuffer.isValid else { return }
        writer.submit(sampleBuffer, frame: frame)
    }

    func stream(_ stream: SCStream, didStopWithError error: Error) {
        writer.event("error", message: error.localizedDescription)
        lock.lock()
        stopped = true
        lock.unlock()
        CFRunLoopWakeUp(CFRunLoopGetMain())
    }

    func isStopped() -> Bool {
        lock.lock()
        defer { lock.unlock() }
        return stopped
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
    guard let window = content.windows
        .filter({ $0.owningApplication?.processID == app.processIdentifier && $0.frame.width > 1 && $0.frame.height > 1 })
        .max(by: { $0.frame.width * $0.frame.height < $1.frame.width * $1.frame.height }) else {
        throw ComputerError.operationFailed("no capturable window found")
    }
    let configuration = SCStreamConfiguration()
    configuration.width = max(1, Int(window.frame.width * 1.25))
    configuration.height = max(1, Int(window.frame.height * 1.25))
    configuration.minimumFrameInterval = CMTime(value: 1, timescale: 12)
    configuration.queueDepth = 2
    configuration.showsCursor = false
    let output = WindowStreamOutput(writer: writer, frame: window.frame)
    let stream = SCStream(filter: SCContentFilter(desktopIndependentWindow: window), configuration: configuration, delegate: output)
    try stream.addStreamOutput(output, type: .screen, sampleHandlerQueue: DispatchQueue(label: "com.blueberrycongee.wuu.cua.capture"))
    try await stream.startCapture()
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
        try await Task.sleep(for: .milliseconds(250))
    }
    withExtendedLifetime(inputMonitor) {}
    if let eventSource { CFRunLoopRemoveSource(CFRunLoopGetMain(), eventSource, .commonModes) }
    try? await stream.stopCapture()
}
