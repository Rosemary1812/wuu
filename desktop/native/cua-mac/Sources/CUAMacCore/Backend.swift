import AppKit
import ApplicationServices
import CoreGraphics
import Foundation

private final class ApplicationBox: @unchecked Sendable {
    private let lock = NSLock()
    private var application: NSRunningApplication?
    private var error: Error?

    func set(application: NSRunningApplication?, error: Error?) {
        lock.lock()
        self.application = application
        self.error = error
        lock.unlock()
    }

    func get() -> (NSRunningApplication?, Error?) {
        lock.lock()
        defer { lock.unlock() }
        return (application, error)
    }
}

public final class MacComputerBackend: ComputerBackend {
    private let snapshotter = AXSnapshotter()
    private var snapshotProcessID: pid_t?
    private var lastSnapshotText: [pid_t: String] = [:]
    private var lastCaptureGeometry: [pid_t: CaptureGeometry] = [:]
    private var foregroundCaptureProcessIDs = Set<pid_t>()

    public init() {}

    public func perform(_ command: ComputerCommand) throws -> ComputerResult {
        switch command.action {
        case .permissionStatus:
            return permissionStatus()
        case .requestPermissions:
            return requestPermissions()
        case .listApps:
            return listApps()
        default:
            break
        }

        guard let target = command.app?.trimmingCharacters(in: .whitespacesAndNewlines), !target.isEmpty else {
            throw ComputerError.invalidArguments("app is required for \(command.action.rawValue)")
        }
        let app = try resolveApplication(target)
        let axApplication = AXUIElementCreateApplication(app.processIdentifier)
        enableElectronAccessibility(axApplication)

        switch command.action {
        case .observe:
            return try observe(app: app, axApplication: axApplication)
        case .click:
            try click(command, app: app)
        case .drag:
            try drag(command, app: app)
        case .pressKey:
            try pressKey(command, app: app)
        case .scroll:
            try scroll(command, app: app)
        case .setValue:
            try setValue(command, app: app)
        case .typeText:
            try typeText(command, app: app)
        case .selectText:
            try selectText(command, app: app)
        case .performAction:
            try performSecondaryAction(command, app: app)
        case .waitForChange:
            try waitForChange(command, app: app, axApplication: axApplication)
        case .permissionStatus, .requestPermissions, .listApps:
            break
        }
        RunLoop.current.run(until: Date(timeIntervalSinceNow: 0.15))
        return try observe(app: app, axApplication: axApplication)
    }

    private func permissionStatus() -> ComputerResult {
        let accessibility = AXIsProcessTrusted()
        let screenRecording = CGPreflightScreenCaptureAccess()
        let text = "Accessibility: \(accessibility ? "granted" : "missing"); Screen Recording: \(screenRecording ? "granted" : "missing")"
        return ComputerResult(text: text, structured: [
            "accessibility": accessibility,
            "screen_recording": screenRecording,
            "accessibility_settings_url": "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility",
            "screen_recording_settings_url": "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture",
        ])
    }

    private func requestPermissions() -> ComputerResult {
        let options = ["AXTrustedCheckOptionPrompt": true] as CFDictionary
        _ = AXIsProcessTrustedWithOptions(options)
        if !CGPreflightScreenCaptureAccess() {
            _ = CGRequestScreenCaptureAccess()
        }
        return permissionStatus()
    }

    private func listApps() -> ComputerResult {
        let apps = NSWorkspace.shared.runningApplications
            .filter { $0.activationPolicy == .regular && !$0.isTerminated }
            .sorted { ($0.localizedName ?? "") < ($1.localizedName ?? "") }
            .map { app -> [String: Any] in
                [
                    "id": app.bundleIdentifier ?? app.bundleURL?.path ?? String(app.processIdentifier),
                    "displayName": app.localizedName ?? "Unknown",
                    "bundleIdentifier": app.bundleIdentifier ?? "",
                    "path": app.bundleURL?.path ?? "",
                    "processID": Int(app.processIdentifier),
                    "isActive": app.isActive,
                    "isHidden": app.isHidden,
                ]
            }
        let text = apps.map { app in
            "\(app["displayName"] ?? "Unknown")\tbundle=\(app["bundleIdentifier"] ?? "")\tpid=\(app["processID"] ?? 0)"
        }.joined(separator: "\n")
        return ComputerResult(text: text, structured: ["apps": apps])
    }

    private func resolveApplication(_ target: String) throws -> NSRunningApplication {
        let normalized = target.lowercased()
        let running = NSWorkspace.shared.runningApplications.filter { !$0.isTerminated }
        if let exact = running.first(where: {
            $0.bundleIdentifier?.lowercased() == normalized ||
            $0.localizedName?.lowercased() == normalized ||
            $0.bundleURL?.path.lowercased() == normalized
        }) {
            return exact
        }
        guard let url = applicationURL(target) else {
            if let partial = running.first(where: {
                $0.localizedName?.lowercased().contains(normalized) == true
            }) {
                return partial
            }
            throw ComputerError.appNotFound(target)
        }
        if let bundleIdentifier = Bundle(url: url)?.bundleIdentifier,
           let existing = running.first(where: { $0.bundleIdentifier == bundleIdentifier }) {
            return existing
        }
        let semaphore = DispatchSemaphore(value: 0)
        let box = ApplicationBox()
        let configuration = NSWorkspace.OpenConfiguration()
        configuration.activates = false
        NSWorkspace.shared.openApplication(at: url, configuration: configuration) { app, error in
            box.set(application: app, error: error)
            semaphore.signal()
        }
        guard semaphore.wait(timeout: .now() + 12) == .success else {
            throw ComputerError.operationFailed("launching \(target) timed out")
        }
        let (app, error) = box.get()
        if let error { throw ComputerError.operationFailed("launch \(target): \(error.localizedDescription)") }
        guard let app else { throw ComputerError.appNotFound(target) }
        waitForApplicationWindow(app, timeout: 5)
        return app
    }

    private func waitForApplicationWindow(_ app: NSRunningApplication, timeout: TimeInterval) {
        let deadline = Date(timeIntervalSinceNow: timeout)
        let application = AXUIElementCreateApplication(app.processIdentifier)
        repeat {
            var value: CFTypeRef?
            let result = AXUIElementCopyAttributeValue(application, kAXWindowsAttribute as CFString, &value)
            if app.isFinishedLaunching,
               result == .success,
               let windows = value as? [AXUIElement],
               !windows.isEmpty {
                return
            }
            RunLoop.current.run(until: Date(timeIntervalSinceNow: 0.1))
        } while Date() < deadline && !app.isTerminated
    }

    private func applicationURL(_ target: String) -> URL? {
        if target.contains("."), let url = NSWorkspace.shared.urlForApplication(withBundleIdentifier: target) {
            return url
        }
        let expanded = NSString(string: target).expandingTildeInPath
        if FileManager.default.fileExists(atPath: expanded) { return URL(fileURLWithPath: expanded) }
        let name = target.hasSuffix(".app") ? target : target + ".app"
        for root in [
            "/Applications",
            "/System/Applications",
            "/System/Applications/Utilities",
            "/System/Library/CoreServices",
            NSHomeDirectory() + "/Applications",
        ] {
            let path = URL(fileURLWithPath: root).appendingPathComponent(name).path
            if FileManager.default.fileExists(atPath: path) { return URL(fileURLWithPath: path) }
        }
        return nil
    }

    private func enableElectronAccessibility(_ application: AXUIElement) {
        _ = AXUIElementSetAttributeValue(application, "AXManualAccessibility" as CFString, kCFBooleanTrue)
        _ = AXUIElementSetAttributeValue(application, "AXEnhancedUserInterface" as CFString, kCFBooleanTrue)
    }

    private func requireAccessibility() throws {
        guard AXIsProcessTrusted() else {
            throw ComputerError.permissionDenied("Accessibility permission is required for app observation and control")
        }
    }

    private func observe(app: NSRunningApplication, axApplication: AXUIElement) throws -> ComputerResult {
        let accessibility = AXIsProcessTrusted()
        let snapshot: AXSnapshot
        if accessibility {
            snapshot = snapshotter.snapshot(application: axApplication)
        } else {
            snapshotter.clear()
            snapshot = AXSnapshot(
                text: "Accessibility permission is unavailable; use the screenshot and coordinate input.",
                elements: [:]
            )
        }
        snapshotProcessID = app.processIdentifier
        lastSnapshotText[app.processIdentifier] = snapshot.text
        var structured: [String: Any] = [
            "app": app.bundleIdentifier ?? app.localizedName ?? String(app.processIdentifier),
            "display_name": app.localizedName ?? "Unknown",
            "process_id": Int(app.processIdentifier),
            "element_count": snapshot.elements.count,
            "ax_available": accessibility,
            "ax_preferred": accessibility,
        ]
        var screenshot: Data?
        do {
            let capture = try captureWindowWithForegroundFallback(app: app)
            screenshot = capture.data
            lastCaptureGeometry[app.processIdentifier] = capture.geometry
            structured["screenshot"] = [
                "width": capture.geometry.imageWidth,
                "height": capture.geometry.imageHeight,
                "window_frame": [
                    "x": capture.geometry.windowFrame.origin.x,
                    "y": capture.geometry.windowFrame.origin.y,
                    "width": capture.geometry.windowFrame.width,
                    "height": capture.geometry.windowFrame.height,
                ],
                "coordinate_space": "latest_screenshot_pixels",
            ]
        } catch {
            structured["screenshot_error"] = error.localizedDescription
        }
        return ComputerResult(
            text: snapshot.text,
            screenshot: screenshot,
            screenshotMIMEType: screenshot == nil ? nil : "image/png",
            structured: structured
        )
    }

    private func captureWindowWithForegroundFallback(app: NSRunningApplication) throws -> WindowCapture {
        guard #available(macOS 14.0, *) else {
            throw ComputerError.unsupported("window screenshots require macOS 14 or newer")
        }
        if foregroundCaptureProcessIDs.contains(app.processIdentifier) {
            try activate(app)
            return try captureForegroundWindowPNG(processID: app.processIdentifier)
        }
        do {
            return try captureWindowPNG(processID: app.processIdentifier)
        } catch let backgroundError {
            foregroundCaptureProcessIDs.insert(app.processIdentifier)
            try activate(app)
            do {
                return try captureForegroundWindowPNG(processID: app.processIdentifier)
            } catch let foregroundError {
                throw ComputerError.operationFailed(
                    "window screenshot failed in background (\(backgroundError.localizedDescription)) and after foreground retry (\(foregroundError.localizedDescription))"
                )
            }
        }
    }

    private func element(_ command: ComputerCommand, app: NSRunningApplication) throws -> AXUIElement {
        guard snapshotProcessID == app.processIdentifier else {
            throw ComputerError.invalidArguments("observe this app before using an element_id")
        }
        guard let id = command.elementID else {
            throw ComputerError.invalidArguments("element_id is required")
        }
        guard let element = snapshotter.element(id: id) else { throw ComputerError.elementNotFound(id) }
        return element
    }

    private func activate(_ app: NSRunningApplication) throws {
        guard app.activate(options: [.activateAllWindows]) else {
            throw ComputerError.operationFailed("could not activate \(app.localizedName ?? "target app")")
        }
        RunLoop.current.run(until: Date(timeIntervalSinceNow: 0.12))
    }

    private func click(_ command: ComputerCommand, app: NSRunningApplication) throws {
        if command.elementID != nil {
            let target = try element(command, app: app)
            if axActions(target).contains(kAXPressAction as String) {
                try performAXAction(target, action: kAXPressAction as String)
                return
            }
            if let frame = axFrame(target) {
                try activate(app)
                try postClick(point: CGPoint(x: frame.midX, y: frame.midY), button: command.mouseButton, count: command.clickCount ?? 1)
                return
            }
        }
        guard let x = command.x, let y = command.y else {
            throw ComputerError.invalidArguments("click requires element_id or x and y")
        }
        try activate(app)
        try postClick(point: try screenshotPoint(x: x, y: y, app: app), button: command.mouseButton, count: command.clickCount ?? 1)
    }

    private func postClick(point: CGPoint, button name: String?, count: Int) throws {
        let button: CGMouseButton
        let downType: CGEventType
        let upType: CGEventType
        switch name?.lowercased() ?? "left" {
        case "right": button = .right; downType = .rightMouseDown; upType = .rightMouseUp
        case "middle": button = .center; downType = .otherMouseDown; upType = .otherMouseUp
        default: button = .left; downType = .leftMouseDown; upType = .leftMouseUp
        }
        for click in 1...max(1, min(count, 3)) {
            guard let down = CGEvent(mouseEventSource: nil, mouseType: downType, mouseCursorPosition: point, mouseButton: button),
                  let up = CGEvent(mouseEventSource: nil, mouseType: upType, mouseCursorPosition: point, mouseButton: button) else {
                throw ComputerError.operationFailed("could not create mouse event")
            }
            down.setIntegerValueField(.mouseEventClickState, value: Int64(click))
            up.setIntegerValueField(.mouseEventClickState, value: Int64(click))
            down.post(tap: .cghidEventTap)
            up.post(tap: .cghidEventTap)
        }
    }

    private func drag(_ command: ComputerCommand, app: NSRunningApplication) throws {
        guard let x = command.x, let y = command.y, let toX = command.toX, let toY = command.toY else {
            throw ComputerError.invalidArguments("drag requires from_x, from_y, to_x, and to_y")
        }
        try activate(app)
        let start = try screenshotPoint(x: x, y: y, app: app)
        let end = try screenshotPoint(x: toX, y: toY, app: app)
        guard let down = CGEvent(mouseEventSource: nil, mouseType: .leftMouseDown, mouseCursorPosition: start, mouseButton: .left) else {
            throw ComputerError.operationFailed("could not create drag event")
        }
        down.post(tap: .cghidEventTap)
        for step in 1...12 {
            let progress = Double(step) / 12
            let point = CGPoint(
                x: start.x + (end.x - start.x) * progress,
                y: start.y + (end.y - start.y) * progress
            )
            CGEvent(mouseEventSource: nil, mouseType: .leftMouseDragged, mouseCursorPosition: point, mouseButton: .left)?.post(tap: .cghidEventTap)
            usleep(8_000)
        }
        CGEvent(mouseEventSource: nil, mouseType: .leftMouseUp, mouseCursorPosition: end, mouseButton: .left)?.post(tap: .cghidEventTap)
    }

    private func screenshotPoint(x: Double, y: Double, app: NSRunningApplication) throws -> CGPoint {
        guard let geometry = lastCaptureGeometry[app.processIdentifier] else {
            throw ComputerError.invalidArguments("observe the app before using screenshot coordinates")
        }
        return try geometry.screenPoint(x: x, y: y)
    }

    private func pressKey(_ command: ComputerCommand, app: NSRunningApplication) throws {
        guard let key = command.key else { throw ComputerError.invalidArguments("key is required") }
        let chord = try KeyChord.parse(key)
        try activate(app)
        guard let down = CGEvent(keyboardEventSource: nil, virtualKey: chord.keyCode, keyDown: true),
              let up = CGEvent(keyboardEventSource: nil, virtualKey: chord.keyCode, keyDown: false) else {
            throw ComputerError.operationFailed("could not create keyboard event")
        }
        down.flags = chord.modifiers.eventFlags
        up.flags = chord.modifiers.eventFlags
        down.post(tap: .cghidEventTap)
        up.post(tap: .cghidEventTap)
    }

    private func scroll(_ command: ComputerCommand, app: NSRunningApplication) throws {
        let pages = max(1, min(command.pages ?? 1, 20))
        let amount = Int32(pages * 720)
        let direction = command.direction?.lowercased() ?? "down"
        let vertical: Int32 = direction == "up" ? amount : direction == "down" ? -amount : 0
        let horizontal: Int32 = direction == "left" ? amount : direction == "right" ? -amount : 0
        try activate(app)
        if command.elementID != nil,
           let frame = axFrame(try element(command, app: app)) {
            CGEvent(mouseEventSource: nil, mouseType: .mouseMoved, mouseCursorPosition: CGPoint(x: frame.midX, y: frame.midY), mouseButton: .left)?.post(tap: .cghidEventTap)
        }
        guard let event = CGEvent(scrollWheelEvent2Source: nil, units: .pixel, wheelCount: 2, wheel1: vertical, wheel2: horizontal, wheel3: 0) else {
            throw ComputerError.operationFailed("could not create scroll event")
        }
        event.post(tap: .cghidEventTap)
    }

    private func setValue(_ command: ComputerCommand, app: NSRunningApplication) throws {
        let target = try element(command, app: app)
        let value = command.value ?? command.text
        guard let value else { throw ComputerError.invalidArguments("value is required") }
        try setAXValue(target, attribute: kAXValueAttribute as String, value: value as CFString)
    }

    private func typeText(_ command: ComputerCommand, app: NSRunningApplication) throws {
        guard let text = command.text else { throw ComputerError.invalidArguments("text is required") }
        if command.elementID != nil {
            let target = try element(command, app: app)
            _ = AXUIElementSetAttributeValue(target, kAXFocusedAttribute as CFString, kCFBooleanTrue)
        }
        try activate(app)
        let units = Array(text.utf16)
        if units.isEmpty { return }
        guard let down = CGEvent(keyboardEventSource: nil, virtualKey: 0, keyDown: true),
              let up = CGEvent(keyboardEventSource: nil, virtualKey: 0, keyDown: false) else {
            throw ComputerError.operationFailed("could not create text input event")
        }
        units.withUnsafeBufferPointer { buffer in
            guard let baseAddress = buffer.baseAddress else { return }
            down.keyboardSetUnicodeString(stringLength: buffer.count, unicodeString: baseAddress)
            up.keyboardSetUnicodeString(stringLength: buffer.count, unicodeString: baseAddress)
        }
        down.post(tap: .cghidEventTap)
        up.post(tap: .cghidEventTap)
    }

    private func selectText(_ command: ComputerCommand, app: NSRunningApplication) throws {
        let target = try element(command, app: app)
        guard let needle = command.text, !needle.isEmpty else { throw ComputerError.invalidArguments("text is required") }
        guard let fullText = axString(target, kAXValueAttribute as String) else {
            throw ComputerError.unsupported("element has no string AXValue")
        }
        var searchStart = fullText.startIndex
        if let prefix = command.prefix, let range = fullText.range(of: prefix) { searchStart = range.upperBound }
        let suffixBound = command.suffix.flatMap { fullText.range(of: $0, range: searchStart..<fullText.endIndex)?.lowerBound } ?? fullText.endIndex
        guard let range = fullText.range(of: needle, range: searchStart..<suffixBound) else {
            throw ComputerError.invalidArguments("text was not found in the target element")
        }
        let nsRange = NSRange(range, in: fullText)
        var cfRange = CFRange(location: nsRange.location, length: nsRange.length)
        guard let value = AXValueCreate(.cfRange, &cfRange) else {
            throw ComputerError.operationFailed("could not create selected text range")
        }
        try setAXValue(target, attribute: kAXSelectedTextRangeAttribute as String, value: value)
    }

    private func performSecondaryAction(_ command: ComputerCommand, app: NSRunningApplication) throws {
        let target = try element(command, app: app)
        guard let action = command.actionName, !action.isEmpty else { throw ComputerError.invalidArguments("action_name is required") }
        try performAXAction(target, action: action)
    }

    private func waitForChange(_ command: ComputerCommand, app: NSRunningApplication, axApplication: AXUIElement) throws {
        try requireAccessibility()
        let previous = lastSnapshotText[app.processIdentifier] ?? ""
        let timeout = max(0.1, min(command.timeout ?? 5, 30))
        try waitForAccessibilityChange(application: axApplication, processID: app.processIdentifier, timeout: timeout)
        let current = snapshotter.snapshot(application: axApplication).text
        if current == previous {
            throw ComputerError.operationFailed("no accessibility change observed within \(timeout) seconds")
        }
    }
}
