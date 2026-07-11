import AppKit
import ApplicationServices
import CoreGraphics
import Darwin
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
    private var axRevisions: [pid_t: UInt64] = [:]
    private var visualRevisions: [pid_t: UInt64] = [:]
    private var lastScreenshotData: [pid_t: Data] = [:]
    private var lastCaptureGeometry: [pid_t: CaptureGeometry] = [:]
    private var foregroundCaptureProcessIDs = Set<pid_t>()
    private var concealedWindowOrigins: [pid_t: [CGPoint]] = [:]

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
        let frontmostBefore = NSWorkspace.shared.frontmostApplication?.processIdentifier
        let app = try resolveApplication(target)
        let appActionLock = try AppActionLock.acquire(processID: app.processIdentifier)
        defer { withExtendedLifetime(appActionLock) {} }
        let axApplication = AXUIElementCreateApplication(app.processIdentifier)
        enableElectronAccessibility(axApplication)
        if command.foregroundPolicy == .require {
            try ForegroundInputLock.withLock {
                restoreConcealedWindows(app: app, application: axApplication)
                try activate(app)
            }
        }

        let mechanism: String
        switch command.action {
        case .observe:
            return try observe(command, app: app, axApplication: axApplication)
        case .concealApp:
            return try concealApp(app: app, application: axApplication)
        case .revealApp:
            return try revealApp(app: app, application: axApplication)
        case .click:
            mechanism = try click(command, app: app)
        case .drag:
            try drag(command, app: app)
            mechanism = "foreground_native"
        case .pressKey:
            try pressKey(command, app: app)
            mechanism = "foreground_native"
        case .pressKeys:
            try pressKeys(command, app: app)
            mechanism = "foreground_native"
        case .scroll:
            try scroll(command, app: app)
            mechanism = "foreground_native"
        case .setValue:
            try setValue(command, app: app)
            mechanism = "background_ax"
        case .typeText:
            try typeText(command, app: app)
            mechanism = "foreground_native"
        case .selectText:
            try selectText(command, app: app)
            mechanism = "background_ax"
        case .performAction:
            try performSecondaryAction(command, app: app)
            mechanism = "background_ax"
        case .waitForChange:
            try waitForChange(command, app: app, axApplication: axApplication)
            return try observe(command, app: app, axApplication: axApplication)
        case .sequence:
            throw ComputerError.unsupported("sequence is coordinated by the Wuu runtime")
        case .activateControl:
            try activateControl(command, app: app, application: axApplication)
            mechanism = "background_ax"
        case .permissionStatus, .requestPermissions, .listApps:
            preconditionFailure("global actions returned before target resolution")
        }
        let previousSnapshot = lastSnapshotText[app.processIdentifier] ?? ""
        let settled = settleAccessibility(app: app, application: axApplication, previous: previousSnapshot)
        let frontmostAfter = NSWorkspace.shared.frontmostApplication?.processIdentifier
        let canonicalTarget = app.bundleIdentifier ?? app.bundleURL?.path ?? target
        let changes = snapshotChanges(from: previousSnapshot, to: settled.text)
        return ComputerResult(
            text: "Action \(command.action.rawValue) completed for app=\"\(canonicalTarget)\". Call observe when you need fresh UI state.",
            structured: [
                "action": command.action.rawValue,
                "app": canonicalTarget,
                "display_name": app.localizedName ?? "Unknown",
                "process_id": Int(app.processIdentifier),
                "status": settled.text != previousSnapshot ? "verified" : "unverified",
                "mechanism": mechanism,
                "foreground_changed": frontmostBefore != frontmostAfter,
                "ax_revision": axRevisions[app.processIdentifier] ?? 0,
                "visual_revision": visualRevisions[app.processIdentifier] ?? 0,
                "changes": changes,
            ]
        )
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
        let running = NSWorkspace.shared.runningApplications.filter { !$0.isTerminated && isProcessAlive($0.processIdentifier) }
        let exactMatches = running.filter {
            $0.bundleIdentifier?.lowercased() == normalized ||
            $0.localizedName?.lowercased() == normalized ||
            $0.bundleURL?.path.lowercased() == normalized
        }
        if let exact = preferWindowedInstance(exactMatches) {
            return exact
        }
        guard let url = applicationURL(target) else {
            if let partial = preferWindowedInstance(running.filter({
                $0.localizedName?.lowercased().contains(normalized) == true
            })) {
                return partial
            }
            throw ComputerError.appNotFound(target)
        }
        if let bundleIdentifier = Bundle(url: url)?.bundleIdentifier,
           let existing = preferWindowedInstance(running.filter({ $0.bundleIdentifier == bundleIdentifier })) {
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

    private func isProcessAlive(_ pid: pid_t) -> Bool {
        // kill(pid, 0) probes existence without signalling; ESRCH means the process
        // is gone even if NSWorkspace has not yet dropped its stale record.
        kill(pid, 0) == 0 || errno == EPERM
    }

    private func hasWindow(_ app: NSRunningApplication) -> Bool {
        var value: CFTypeRef?
        let application = AXUIElementCreateApplication(app.processIdentifier)
        guard AXUIElementCopyAttributeValue(application, kAXWindowsAttribute as CFString, &value) == .success,
              let windows = value as? [AXUIElement] else { return false }
        return !windows.isEmpty
    }

    private func preferWindowedInstance(_ candidates: [NSRunningApplication]) -> NSRunningApplication? {
        guard !candidates.isEmpty else { return nil }
        // A windowless instance (e.g. a hidden launch that never created a window)
        // cannot be observed or controlled, so never let it shadow a usable one.
        return candidates.first(where: hasWindow) ?? candidates.first
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

    private func observe(_ command: ComputerCommand, app: NSRunningApplication, axApplication: AXUIElement) throws -> ComputerResult {
        let accessibility = AXIsProcessTrusted()
        let snapshot: AXSnapshot
        let previousText = lastSnapshotText[app.processIdentifier]
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
        if previousText != snapshot.text {
            axRevisions[app.processIdentifier, default: 0] += 1
        }
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
            let capture = try captureWindowWithForegroundFallback(command, app: app)
            screenshot = capture.data
            if lastScreenshotData[app.processIdentifier] != capture.data {
                visualRevisions[app.processIdentifier, default: 0] += 1
                lastScreenshotData[app.processIdentifier] = capture.data
            }
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
                "visible_image_frame": [
                    "x": capture.geometry.visibleImageFrame.origin.x,
                    "y": capture.geometry.visibleImageFrame.origin.y,
                    "width": capture.geometry.visibleImageFrame.width,
                    "height": capture.geometry.visibleImageFrame.height,
                ],
            ]
        } catch {
            structured["screenshot_error"] = error.localizedDescription
        }
        let canonicalTarget = app.bundleIdentifier ?? app.bundleURL?.path ?? String(app.processIdentifier)
        var header = "Target app=\"\(canonicalTarget)\" display_name=\"\(app.localizedName ?? "Unknown")\" pid=\(app.processIdentifier). Reuse this exact app value for follow-up actions."
        if let geometry = lastCaptureGeometry[app.processIdentifier], screenshot != nil {
            header += " Screenshot=\(geometry.imageWidth)×\(geometry.imageHeight) pixels maps to window_frame=(\(Int(geometry.windowFrame.origin.x)),\(Int(geometry.windowFrame.origin.y)),\(Int(geometry.windowFrame.width)),\(Int(geometry.windowFrame.height))). Prefer coordinate_space=\"normalized\" (0-1000) for visual targets so provider image resizing does not affect clicks; use coordinate_space=\"screenshot\" only for original image pixels."
        }
        let changes = previousText.map { snapshotChanges(from: $0, to: snapshot.text) } ?? []
        let returnDiff = previousText != nil && !changes.isEmpty && changes.count <= 120
        structured["ax_revision"] = axRevisions[app.processIdentifier] ?? 0
        structured["visual_revision"] = visualRevisions[app.processIdentifier] ?? 0
        structured["full_snapshot"] = !returnDiff
        structured["changes"] = changes
        let stateText = returnDiff ? "Changes since the previous observe:\n" + changes.joined(separator: "\n") : snapshot.text
        return ComputerResult(
            text: header + "\n" + stateText,
            screenshot: screenshot,
            screenshotMIMEType: screenshot == nil ? nil : "image/png",
            structured: structured
        )
    }

    private func captureWindowWithForegroundFallback(_ command: ComputerCommand, app: NSRunningApplication) throws -> WindowCapture {
        guard #available(macOS 14.0, *) else {
            throw ComputerError.unsupported("window screenshots require macOS 14 or newer")
        }
        if foregroundCaptureProcessIDs.contains(app.processIdentifier) {
            return try withForegroundInput(command, app: app) {
                try captureForegroundWindowPNG(processID: app.processIdentifier)
            }
        }
        do {
            return try captureWindowPNG(processID: app.processIdentifier)
        } catch let backgroundError {
            foregroundCaptureProcessIDs.insert(app.processIdentifier)
            do {
                return try withForegroundInput(command, app: app) {
                    try captureForegroundWindowPNG(processID: app.processIdentifier)
                }
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
        guard let element = snapshotter.element(id: id), let descriptor = snapshotter.descriptor(id: id) else {
            throw ComputerError.elementNotFound(id)
        }
        var role: CFTypeRef?
        if AXUIElementCopyAttributeValue(element, kAXRoleAttribute as CFString, &role) == .success {
            return element
        }
        let application = AXUIElementCreateApplication(app.processIdentifier)
        _ = snapshotter.snapshot(application: application)
        guard let recovered = snapshotter.uniqueElement(matching: descriptor) else {
            throw ComputerError.elementNotFound(id)
        }
        return recovered
    }

    private func settleAccessibility(app: NSRunningApplication, application: AXUIElement, previous: String) -> AXSnapshot {
        guard AXIsProcessTrusted() else {
            return AXSnapshot(text: previous, elements: [:])
        }
        let deadline = Date(timeIntervalSinceNow: 1.2)
        var latest = snapshotter.snapshot(application: application)
        var stableSamples = 0
        while Date() < deadline && stableSamples < 2 {
            RunLoop.current.run(until: Date(timeIntervalSinceNow: 0.08))
            let next = snapshotter.snapshot(application: application)
            stableSamples = next.text == latest.text ? stableSamples + 1 : 0
            latest = next
        }
        if latest.text != previous {
            axRevisions[app.processIdentifier, default: 0] += 1
        }
        lastSnapshotText[app.processIdentifier] = latest.text
        snapshotProcessID = app.processIdentifier
        return latest
    }

    private func snapshotChanges(from previous: String, to current: String) -> [String] {
        guard previous != current else { return [] }
        let old = Set(previous.split(separator: "\n").map { semanticAXLine(String($0)) })
        let new = Set(current.split(separator: "\n").map { semanticAXLine(String($0)) })
        let removed = old.subtracting(new).sorted().map { "- \($0)" }
        let added = new.subtracting(old).sorted().map { "+ \($0)" }
        return Array((removed + added).prefix(240))
    }

    private func semanticAXLine(_ line: String) -> String {
        let trimmed = line.trimmingCharacters(in: .whitespaces)
        guard trimmed.first == "[", let closing = trimmed.firstIndex(of: "]") else { return trimmed }
        return String(trimmed[trimmed.index(after: closing)...]).trimmingCharacters(in: .whitespaces)
    }

    private func activate(_ app: NSRunningApplication) throws {
        app.unhide()
        guard app.activate(options: [.activateAllWindows, .activateIgnoringOtherApps]) else {
            throw ComputerError.operationFailed("could not activate \(app.localizedName ?? "target app")")
        }
        let application = AXUIElementCreateApplication(app.processIdentifier)
        _ = AXUIElementSetAttributeValue(application, kAXFrontmostAttribute as CFString, kCFBooleanTrue)
        var focusedWindow: CFTypeRef?
        if AXUIElementCopyAttributeValue(application, kAXFocusedWindowAttribute as CFString, &focusedWindow) == .success,
           let window = focusedWindow as! AXUIElement? {
            _ = AXUIElementPerformAction(window, kAXRaiseAction as CFString)
        }
        let deadline = Date(timeIntervalSinceNow: 2)
        repeat {
            if app.isActive || NSWorkspace.shared.frontmostApplication?.processIdentifier == app.processIdentifier {
                return
            }
            RunLoop.current.run(until: Date(timeIntervalSinceNow: 0.02))
        } while Date() < deadline && !app.isTerminated
        throw ComputerError.operationFailed("\(app.localizedName ?? "target app") did not become active")
    }

    private func withForegroundInput<T>(_ command: ComputerCommand, app: NSRunningApplication, body: () throws -> T) throws -> T {
        guard command.foregroundPolicy != .avoid else {
            throw ComputerError.requiresForeground(
                "\(command.action.rawValue) needs native mouse or keyboard input for \(app.localizedName ?? "the target app"); retry with foreground_policy=\"allow\" only when foreground control is acceptable"
            )
        }
        return try ForegroundInputLock.withLock {
            restoreConcealedWindows(app: app, application: AXUIElementCreateApplication(app.processIdentifier))
            try activate(app)
            return try body()
        }
    }

    // A rendered window keeps its full Accessibility tree and its ScreenCaptureKit
    // backing surface no matter where it sits, so parking it beyond every display
    // hides it from the user while background control and the live PiP keep working.
    // The Dock icon, Cmd-Tab entry, and Mission Control presence follow the target
    // app's own activation policy, which only that process can change; conceal_app
    // does not claim to remove them.
    private func offscreenOrigin() -> CGPoint {
        let union = NSScreen.screens.reduce(CGRect.zero) { $0.union($1.frame) }
        let width = max(union.width, 4000)
        let height = max(union.height, 4000)
        return CGPoint(x: union.maxX + width, y: union.maxY + height)
    }

    private func appWindows(_ application: AXUIElement) -> [AXUIElement] {
        axElements(application, attribute: kAXWindowsAttribute as String)
    }

    private func setWindowOrigin(_ window: AXUIElement, to origin: CGPoint) -> Bool {
        var point = origin
        guard let value = AXValueCreate(.cgPoint, &point) else { return false }
        return AXUIElementSetAttributeValue(window, kAXPositionAttribute as CFString, value) == .success
    }

    private func concealApp(app: NSRunningApplication, application: AXUIElement) throws -> ComputerResult {
        try requireAccessibility()
        let windows = appWindows(application)
        guard !windows.isEmpty else {
            throw ComputerError.operationFailed("no window to conceal yet; observe the app or wait for its window before concealing")
        }
        let target = offscreenOrigin()
        var origins = concealedWindowOrigins[app.processIdentifier] ?? []
        var concealed = 0
        var clamped = 0
        for (index, window) in windows.enumerated() {
            let current = axFrame(window)?.origin ?? .zero
            // Only record an original that is still on-screen so a re-conceal does not
            // overwrite the saved position with the off-screen one.
            if index < origins.count {
                if current.x < target.x - 1 { origins[index] = current }
            } else {
                origins.append(current)
            }
            guard setWindowOrigin(window, to: target) else { continue }
            if (axFrame(window)?.origin.x ?? current.x) >= target.x - 1 {
                concealed += 1
            } else {
                clamped += 1
            }
        }
        concealedWindowOrigins[app.processIdentifier] = origins
        let canonicalTarget = app.bundleIdentifier ?? app.bundleURL?.path ?? String(app.processIdentifier)
        var text = "Concealed \(concealed) window(s) for app=\"\(canonicalTarget)\" off-screen; Accessibility control and live capture continue."
        if clamped > 0 {
            text += " \(clamped) window(s) refused the off-screen position and stay visible."
        }
        return ComputerResult(text: text, structured: [
            "action": "conceal_app",
            "app": canonicalTarget,
            "process_id": Int(app.processIdentifier),
            "status": clamped == 0 ? "concealed" : "partial",
            "mechanism": "background_ax",
            "foreground_changed": false,
            "concealed_windows": concealed,
            "unsupported_windows": clamped,
        ])
    }

    private func revealApp(app: NSRunningApplication, application: AXUIElement) throws -> ComputerResult {
        try requireAccessibility()
        let restored = restoreConcealedWindows(app: app, application: application)
        let canonicalTarget = app.bundleIdentifier ?? app.bundleURL?.path ?? String(app.processIdentifier)
        return ComputerResult(
            text: "Revealed \(restored) window(s) for app=\"\(canonicalTarget)\" at their original positions.",
            structured: [
                "action": "reveal_app",
                "app": canonicalTarget,
                "process_id": Int(app.processIdentifier),
                "status": "revealed",
                "mechanism": "background_ax",
                "foreground_changed": false,
                "revealed_windows": restored,
            ]
        )
    }

    @discardableResult
    private func restoreConcealedWindows(app: NSRunningApplication, application: AXUIElement) -> Int {
        guard let origins = concealedWindowOrigins[app.processIdentifier] else { return 0 }
        let windows = appWindows(application)
        var restored = 0
        for (index, window) in windows.enumerated() where index < origins.count {
            if setWindowOrigin(window, to: origins[index]) { restored += 1 }
        }
        concealedWindowOrigins[app.processIdentifier] = nil
        return restored
    }

    private func click(_ command: ComputerCommand, app: NSRunningApplication) throws -> String {
        if command.elementID != nil {
            let target = try element(command, app: app)
            if axActions(target).contains(kAXPressAction as String) {
                try performAXAction(target, action: kAXPressAction as String)
                return "background_ax"
            }
            if let frame = axFrame(target) {
                try withForegroundInput(command, app: app) {
                    try postClick(point: CGPoint(x: frame.midX, y: frame.midY), button: command.mouseButton, count: command.clickCount ?? 1)
                }
                return "foreground_native"
            }
        }
        guard let x = command.x, let y = command.y else {
            throw ComputerError.invalidArguments("click requires element_id or x and y")
        }
        let point = try inputPoint(x: x, y: y, coordinateSpace: command.coordinateSpace, app: app)
        try withForegroundInput(command, app: app) {
            try postClick(point: point, button: command.mouseButton, count: command.clickCount ?? 1)
        }
        return "foreground_native"
    }

    private func postClick(point: CGPoint, button name: String?, count: Int) throws {
        let source = syntheticEventSource()
        let button: CGMouseButton
        let downType: CGEventType
        let upType: CGEventType
        switch name?.lowercased() ?? "left" {
        case "right": button = .right; downType = .rightMouseDown; upType = .rightMouseUp
        case "middle": button = .center; downType = .otherMouseDown; upType = .otherMouseUp
        default: button = .left; downType = .leftMouseDown; upType = .leftMouseUp
        }
        markSynthetic(CGEvent(mouseEventSource: source, mouseType: .mouseMoved, mouseCursorPosition: point, mouseButton: button))?.post(tap: .cghidEventTap)
        usleep(20_000)
        for click in 1...max(1, min(count, 3)) {
            guard let down = markSynthetic(CGEvent(mouseEventSource: source, mouseType: downType, mouseCursorPosition: point, mouseButton: button)),
                  let up = markSynthetic(CGEvent(mouseEventSource: source, mouseType: upType, mouseCursorPosition: point, mouseButton: button)) else {
                throw ComputerError.operationFailed("could not create mouse event")
            }
            down.setIntegerValueField(.mouseEventClickState, value: Int64(click))
            up.setIntegerValueField(.mouseEventClickState, value: Int64(click))
            down.post(tap: .cghidEventTap)
            usleep(35_000)
            up.post(tap: .cghidEventTap)
        }
    }

    private func drag(_ command: ComputerCommand, app: NSRunningApplication) throws {
        guard let x = command.x, let y = command.y, let toX = command.toX, let toY = command.toY else {
            throw ComputerError.invalidArguments("drag requires from_x, from_y, to_x, and to_y")
        }
        let start = try inputPoint(x: x, y: y, coordinateSpace: command.coordinateSpace, app: app)
        let end = try inputPoint(x: toX, y: toY, coordinateSpace: command.coordinateSpace, app: app)
        try withForegroundInput(command, app: app) {
            let source = syntheticEventSource()
            guard let down = markSynthetic(CGEvent(mouseEventSource: source, mouseType: .leftMouseDown, mouseCursorPosition: start, mouseButton: .left)) else {
                throw ComputerError.operationFailed("could not create drag event")
            }
            down.post(tap: .cghidEventTap)
            for step in 1...12 {
                let progress = Double(step) / 12
                let point = CGPoint(
                    x: start.x + (end.x - start.x) * progress,
                    y: start.y + (end.y - start.y) * progress
                )
                markSynthetic(CGEvent(mouseEventSource: source, mouseType: .leftMouseDragged, mouseCursorPosition: point, mouseButton: .left))?.post(tap: .cghidEventTap)
                usleep(8_000)
            }
            markSynthetic(CGEvent(mouseEventSource: source, mouseType: .leftMouseUp, mouseCursorPosition: end, mouseButton: .left))?.post(tap: .cghidEventTap)
        }
    }

    private func inputPoint(x: Double, y: Double, coordinateSpace: String?, app: NSRunningApplication) throws -> CGPoint {
        guard let geometry = lastCaptureGeometry[app.processIdentifier] else {
            throw ComputerError.invalidArguments("observe the app before using screenshot coordinates")
        }
        switch coordinateSpace ?? "screenshot" {
        case "normalized":
            return try geometry.screenPoint(normalizedX: x, normalizedY: y)
        case "screenshot":
            return try geometry.screenPoint(x: x, y: y)
        case "screen":
            let point = CGPoint(x: x, y: y)
            guard geometry.windowFrame.contains(point) else {
                throw ComputerError.invalidArguments("screen coordinates must be inside the target window frame")
            }
            return point
        default:
            throw ComputerError.invalidArguments("coordinate_space must be normalized, screenshot, or screen")
        }
    }

    private func pressKey(_ command: ComputerCommand, app: NSRunningApplication) throws {
        guard let key = command.key else { throw ComputerError.invalidArguments("key is required") }
        try withForegroundInput(command, app: app) { try postKey(key) }
    }

    private func pressKeys(_ command: ComputerCommand, app: NSRunningApplication) throws {
        guard let keys = command.keys, !keys.isEmpty else { throw ComputerError.invalidArguments("keys is required") }
        try withForegroundInput(command, app: app) {
            for key in keys {
                try postKey(key)
                usleep(20_000)
            }
        }
    }

    private func postKey(_ key: String) throws {
        let chord = try KeyChord.parse(key)
        let source = syntheticEventSource()
        guard let down = markSynthetic(CGEvent(keyboardEventSource: source, virtualKey: chord.keyCode, keyDown: true)),
              let up = markSynthetic(CGEvent(keyboardEventSource: source, virtualKey: chord.keyCode, keyDown: false)) else {
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
        let frame = try command.elementID.flatMap { _ in axFrame(try element(command, app: app)) }
        try withForegroundInput(command, app: app) {
            if let frame {
                markSynthetic(CGEvent(mouseEventSource: nil, mouseType: .mouseMoved, mouseCursorPosition: CGPoint(x: frame.midX, y: frame.midY), mouseButton: .left))?.post(tap: .cghidEventTap)
            }
            guard let event = markSynthetic(CGEvent(scrollWheelEvent2Source: nil, units: .pixel, wheelCount: 2, wheel1: vertical, wheel2: horizontal, wheel3: 0)) else {
                throw ComputerError.operationFailed("could not create scroll event")
            }
            event.post(tap: .cghidEventTap)
        }
    }

    private func setValue(_ command: ComputerCommand, app: NSRunningApplication) throws {
        let target = try element(command, app: app)
        let value = command.value ?? command.text
        guard let value else { throw ComputerError.invalidArguments("value is required") }
        try setAXValue(target, attribute: kAXValueAttribute as String, value: value as CFString)
    }

    private func typeText(_ command: ComputerCommand, app: NSRunningApplication) throws {
        guard let text = command.text else { throw ComputerError.invalidArguments("text is required") }
        let units = Array(text.utf16)
        if units.isEmpty { return }
        guard let down = markSynthetic(CGEvent(keyboardEventSource: nil, virtualKey: 0, keyDown: true)),
              let up = markSynthetic(CGEvent(keyboardEventSource: nil, virtualKey: 0, keyDown: false)) else {
            throw ComputerError.operationFailed("could not create text input event")
        }
        try withForegroundInput(command, app: app) {
            if command.elementID != nil {
                let target = try element(command, app: app)
                _ = AXUIElementSetAttributeValue(target, kAXFocusedAttribute as CFString, kCFBooleanTrue)
            }
            units.withUnsafeBufferPointer { buffer in
                guard let baseAddress = buffer.baseAddress else { return }
                down.keyboardSetUnicodeString(stringLength: buffer.count, unicodeString: baseAddress)
                up.keyboardSetUnicodeString(stringLength: buffer.count, unicodeString: baseAddress)
            }
            down.post(tap: .cghidEventTap)
            up.post(tap: .cghidEventTap)
        }
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

    private func activateControl(_ command: ComputerCommand, app: NSRunningApplication, application: AXUIElement) throws {
        guard command.title?.isEmpty == false || command.description?.isEmpty == false else {
            throw ComputerError.invalidArguments("activate_control requires title or description")
        }
        _ = snapshotter.snapshot(application: application)
        guard let target = snapshotter.uniqueElement(role: command.role, title: command.title, description: command.description) else {
            throw ComputerError.operationFailed("activate_control selector did not match exactly one element; observe and refine role, title, or description")
        }
        try performAXAction(target, action: kAXPressAction as String)
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
