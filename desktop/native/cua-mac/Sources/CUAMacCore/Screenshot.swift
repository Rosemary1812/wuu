import AppKit
import CoreGraphics
import Foundation
import ScreenCaptureKit

private final class CaptureBox: @unchecked Sendable {
    private let lock = NSLock()
    private var value: Result<Data, Error>?

    func set(_ result: Result<Data, Error>) {
        lock.lock()
        value = result
        lock.unlock()
    }

    func get() -> Result<Data, Error>? {
        lock.lock()
        defer { lock.unlock() }
        return value
    }
}

func captureWindowPNG(processID: pid_t, timeout: TimeInterval = 8) throws -> Data {
    guard CGPreflightScreenCaptureAccess() else {
        throw ComputerError.permissionDenied("Screen Recording permission is required for window screenshots")
    }
    guard #available(macOS 14.0, *) else {
        throw ComputerError.unsupported("window screenshots require macOS 14 or newer")
    }
    let semaphore = DispatchSemaphore(value: 0)
    let box = CaptureBox()
    SCShareableContent.getExcludingDesktopWindows(false, onScreenWindowsOnly: false) { content, error in
        if let error {
            box.set(.failure(error))
            semaphore.signal()
            return
        }
        guard let window = content?.windows
            .filter({ $0.owningApplication?.processID == processID && $0.frame.width > 1 && $0.frame.height > 1 })
            .max(by: { $0.frame.width * $0.frame.height < $1.frame.width * $1.frame.height }) else {
            box.set(.failure(ComputerError.operationFailed("no capturable window found")))
            semaphore.signal()
            return
        }
        let configuration = SCStreamConfiguration()
        configuration.width = max(1, Int(window.frame.width * 2))
        configuration.height = max(1, Int(window.frame.height * 2))
        configuration.showsCursor = false
        let filter = SCContentFilter(desktopIndependentWindow: window)
        SCScreenshotManager.captureImage(contentFilter: filter, configuration: configuration) { image, captureError in
            if let captureError {
                box.set(.failure(captureError))
            } else if let image,
                      let data = NSBitmapImageRep(cgImage: image).representation(using: .png, properties: [:]) {
                box.set(.success(data))
            } else {
                box.set(.failure(ComputerError.operationFailed("window screenshot produced no image")))
            }
            semaphore.signal()
        }
    }
    guard semaphore.wait(timeout: .now() + timeout) == .success else {
        throw ComputerError.operationFailed("window screenshot timed out")
    }
    return try box.get()?.get() ?? { throw ComputerError.operationFailed("window screenshot did not complete") }()
}
