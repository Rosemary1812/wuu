import AppKit
import CoreGraphics
import Foundation
@preconcurrency import ScreenCaptureKit

public struct CaptureGeometry: Sendable {
    public let windowFrame: CGRect
    public let imageWidth: Int
    public let imageHeight: Int
    public let visibleImageFrame: CGRect

    public init(windowFrame: CGRect, imageWidth: Int, imageHeight: Int, visibleImageFrame: CGRect? = nil) {
        self.windowFrame = windowFrame
        self.imageWidth = imageWidth
        self.imageHeight = imageHeight
        self.visibleImageFrame = visibleImageFrame ?? CGRect(x: 0, y: 0, width: imageWidth, height: imageHeight)
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

    public func screenPoint(normalizedX x: Double, normalizedY y: Double) throws -> CGPoint {
        guard x >= 0, y >= 0, x <= 1000, y <= 1000 else {
            throw ComputerError.invalidArguments("normalized coordinates must be between 0 and 1000")
        }
        return try screenPoint(
            x: x * Double(imageWidth) / 1000,
            y: y * Double(imageHeight) / 1000
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

func captureWindowPNG(
    processID: pid_t,
    timeout: TimeInterval = 8,
    preferredWindowFrame: CGRect? = nil,
    scale: CGFloat = 2,
    computeVisibleBounds: Bool = true
) throws -> WindowCapture {
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
        let candidates = content?.windows
            .filter({ $0.owningApplication?.processID == processID && $0.frame.width > 1 && $0.frame.height > 1 }) ?? []
        let window = preferredWindowFrame.flatMap { preferred in
            candidates.min { left, right in
                screenshotWindowFrameDistance(left.frame, preferred) < screenshotWindowFrameDistance(right.frame, preferred)
            }
        } ?? candidates.max(by: { $0.frame.width * $0.frame.height < $1.frame.width * $1.frame.height })
        guard let window else {
            box.set(.failure(ComputerError.operationFailed("no capturable window found")))
            semaphore.signal()
            return
        }
        let configuration = SCStreamConfiguration()
        configuration.width = max(1, Int(window.frame.width * scale))
        configuration.height = max(1, Int(window.frame.height * scale))
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
                        imageHeight: image.height,
                        visibleImageFrame: computeVisibleBounds ? visiblePixelBounds(image) : nil
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

private func screenshotWindowFrameDistance(_ left: CGRect, _ right: CGRect) -> CGFloat {
    abs(left.minX - right.minX)
        + abs(left.minY - right.minY)
        + abs(left.width - right.width)
        + abs(left.height - right.height)
}

@available(macOS 14.0, *)
func captureForegroundWindowPNG(processID: pid_t) throws -> WindowCapture {
    guard CGPreflightScreenCaptureAccess() else {
        throw ComputerError.permissionDenied("Screen Recording permission is required for window screenshots")
    }
    guard let windowList = CGWindowListCopyWindowInfo(.optionAll, kCGNullWindowID) as? [[String: Any]] else {
        throw ComputerError.operationFailed("could not list foreground windows")
    }
    let candidates = windowList.compactMap { info -> (id: CGWindowID, frame: CGRect, shareable: Bool)? in
        guard (info[kCGWindowOwnerPID as String] as? NSNumber)?.int32Value == processID,
              (info[kCGWindowLayer as String] as? NSNumber)?.intValue == 0,
              let number = info[kCGWindowNumber as String] as? NSNumber,
              let bounds = info[kCGWindowBounds as String] as? [String: Any],
              let frame = CGRect(dictionaryRepresentation: bounds as CFDictionary),
              frame.width > 1,
              frame.height > 1 else {
            return nil
        }
        let shareable = (info[kCGWindowSharingState as String] as? NSNumber)?.intValue != 0
        return (CGWindowID(number.uint32Value), frame, shareable)
    }
    guard let window = candidates.max(by: { $0.frame.width * $0.frame.height < $1.frame.width * $1.frame.height }) else {
        throw ComputerError.operationFailed("no foreground window found")
    }
    let windowImage = window.shareable
        ? CGWindowListCreateImage(.null, .optionIncludingWindow, window.id, [.boundsIgnoreFraming, .bestResolution])
        : nil
    let captured: (image: CGImage, frame: CGRect)
    if let windowImage {
        captured = (windowImage, window.frame)
    } else {
        return try captureSystemWindowPNG(window.id, frame: window.frame)
    }
    guard let data = NSBitmapImageRep(cgImage: captured.image).representation(using: .png, properties: [:]) else {
        throw ComputerError.operationFailed("foreground window screenshot produced no image")
    }
    return WindowCapture(
        data: data,
        geometry: CaptureGeometry(
            windowFrame: captured.frame,
            imageWidth: captured.image.width,
            imageHeight: captured.image.height,
            visibleImageFrame: visiblePixelBounds(captured.image)
        )
    )
}

private func visiblePixelBounds(_ image: CGImage) -> CGRect {
    let bitmap = NSBitmapImageRep(cgImage: image)
    var minX = image.width
    var minY = image.height
    var maxX = -1
    var maxY = -1
    for y in 0..<image.height {
        for x in 0..<image.width {
            guard let color = bitmap.colorAt(x: x, y: y), color.alphaComponent >= 0.5 else { continue }
            minX = min(minX, x)
            minY = min(minY, y)
            maxX = max(maxX, x)
            maxY = max(maxY, y)
        }
    }
    guard maxX >= minX, maxY >= minY else {
        return CGRect(x: 0, y: 0, width: image.width, height: image.height)
    }
    return CGRect(x: minX, y: minY, width: maxX - minX + 1, height: maxY - minY + 1)
}

private func captureSystemWindowPNG(_ windowID: CGWindowID, frame: CGRect, timeout: TimeInterval = 4) throws -> WindowCapture {
    let path = FileManager.default.temporaryDirectory
        .appendingPathComponent("wuu-cua-\(UUID().uuidString).png")
    defer { try? FileManager.default.removeItem(at: path) }

    let process = Process()
    process.executableURL = URL(fileURLWithPath: "/usr/sbin/screencapture")
    process.arguments = [
        "-x",
        "-o",
        "-l\(windowID)",
        path.path,
    ]
    let finished = DispatchSemaphore(value: 0)
    process.terminationHandler = { _ in finished.signal() }
    do {
        try process.run()
    } catch {
        throw ComputerError.operationFailed("start system screenshot: \(error.localizedDescription)")
    }
    guard finished.wait(timeout: .now() + timeout) == .success else {
        process.terminate()
        throw ComputerError.operationFailed("system screenshot timed out")
    }
    guard process.terminationStatus == 0,
          let data = try? Data(contentsOf: path),
          let image = NSBitmapImageRep(data: data),
          image.pixelsWide > 0,
          image.pixelsHigh > 0 else {
        throw ComputerError.operationFailed("system screenshot produced no image")
    }
    return WindowCapture(
        data: data,
        geometry: CaptureGeometry(
            windowFrame: frame,
            imageWidth: image.pixelsWide,
            imageHeight: image.pixelsHigh
        )
    )
}
