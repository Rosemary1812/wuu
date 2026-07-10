import AppKit
import CoreGraphics
import Foundation
@preconcurrency import ScreenCaptureKit

public struct CaptureGeometry: Sendable {
    public let windowFrame: CGRect
    public let imageWidth: Int
    public let imageHeight: Int

    public init(windowFrame: CGRect, imageWidth: Int, imageHeight: Int) {
        self.windowFrame = windowFrame
        self.imageWidth = imageWidth
        self.imageHeight = imageHeight
    }

    public func screenPoint(x: Double, y: Double) throws -> CGPoint {
        guard imageWidth > 0, imageHeight > 0 else {
            throw ComputerError.operationFailed("screenshot geometry is invalid")
        }
        guard x >= 0, y >= 0, x <= Double(imageWidth), y <= Double(imageHeight) else {
            throw ComputerError.invalidArguments("coordinates must be inside the latest screenshot (\(imageWidth)×\(imageHeight))")
        }
        return CGPoint(
            x: windowFrame.origin.x + x * windowFrame.width / Double(imageWidth),
            y: windowFrame.origin.y + y * windowFrame.height / Double(imageHeight)
        )
    }
}

struct WindowCapture {
    let data: Data
    let geometry: CaptureGeometry
}

private final class CaptureBox: @unchecked Sendable {
    private let lock = NSLock()
    private var value: Result<WindowCapture, Error>?

    func set(_ result: Result<WindowCapture, Error>) {
        lock.lock()
        value = result
        lock.unlock()
    }

    func get() -> Result<WindowCapture, Error>? {
        lock.lock()
        defer { lock.unlock() }
        return value
    }
}

func captureWindowPNG(processID: pid_t, timeout: TimeInterval = 8) throws -> WindowCapture {
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
        let windowFrame = window.frame
        SCScreenshotManager.captureImage(contentFilter: filter, configuration: configuration) { image, captureError in
            if let captureError {
                box.set(.failure(captureError))
            } else if let image,
                      let data = NSBitmapImageRep(cgImage: image).representation(using: .png, properties: [:]) {
                box.set(.success(WindowCapture(
                    data: data,
                    geometry: CaptureGeometry(
                        windowFrame: windowFrame,
                        imageWidth: image.width,
                        imageHeight: image.height
                    )
                )))
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
