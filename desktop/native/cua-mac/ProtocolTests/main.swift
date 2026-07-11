import CUAMacCore
import Foundation

private enum TestFailure: Error, CustomStringConvertible {
    case assertion(String)
    var description: String {
        switch self {
        case let .assertion(message): message
        }
    }
}

private func expect(_ condition: @autoclosure () -> Bool, _ message: String) throws {
    if !condition() {
        throw TestFailure.assertion(message)
    }
}

private final class FakeBackend: ComputerBackend {
    var commands: [ComputerCommand] = []
    var next = ComputerResult(
        text: "AXApplication id=0 title=TextEdit",
        screenshot: Data("png".utf8),
        screenshotMIMEType: "image/png",
        structured: ["app": "com.apple.TextEdit", "ax_preferred": true]
    )

    func perform(_ command: ComputerCommand) throws -> ComputerResult {
        commands.append(command)
        return next
    }
}

private func testInitializesAndAdvertisesFullComputerTool() throws {
    let server = MCPServer(backend: FakeBackend())
    let initialized = try server.handle([
        "jsonrpc": "2.0",
        "id": 1,
        "method": "initialize",
        "params": ["protocolVersion": "2025-11-25"],
    ])
    try expect(initialized?["id"] as? Int == 1, "initialize id")
    let initResult = initialized?["result"] as? [String: Any]
    try expect(initResult?["protocolVersion"] as? String == "2025-11-25", "protocol version")

    let listed = try server.handle(["jsonrpc": "2.0", "id": 2, "method": "tools/list"])
    let result = listed?["result"] as? [String: Any]
    let tools = result?["tools"] as? [[String: Any]]
    try expect(tools?.count == 1, "one tool")
    try expect(tools?.first?["name"] as? String == "computer", "computer tool name")
    let schema = tools?.first?["inputSchema"] as? [String: Any]
    let properties = schema?["properties"] as? [String: Any]
    let variants = schema?["oneOf"] as? [[String: Any]]
    let action = properties?["action"] as? [String: Any]
    let actions = action?["enum"] as? [String]
    let app = properties?["app"] as? [String: Any]
    try expect(actions == [
        "permission_status", "request_permissions", "list_apps", "observe",
        "click", "drag", "press_key", "press_keys", "scroll", "set_value", "type_text",
        "select_text", "perform_action", "wait_for_change",
        "sequence",
        "activate_control",
        "conceal_app", "reveal_app",
    ], "full computer actions")
    try expect((app?["description"] as? String)?.contains("Required for every action") == true, "app requirement is explicit")
    func requiredFields(for action: String) -> [String] {
        variants?.first(where: { variant in
            let properties = variant["properties"] as? [String: Any]
            let actionProperty = properties?["action"] as? [String: Any]
            return (actionProperty?["enum"] as? [String]) == [action]
        })?["required"] as? [String] ?? []
    }
    try expect(requiredFields(for: "observe").contains("app"), "observe requires app")
    try expect(requiredFields(for: "press_key").contains("key"), "press_key requires key")
    try expect(requiredFields(for: "press_keys").contains("keys"), "press_keys requires keys")
    try expect(requiredFields(for: "type_text").contains("text"), "type_text requires text")
    try expect(requiredFields(for: "perform_action").contains("action_name"), "perform_action requires action_name")
    try expect(requiredFields(for: "conceal_app") == ["app"], "conceal_app requires only app")
    try expect(requiredFields(for: "reveal_app") == ["app"], "reveal_app requires only app")
    let clickVariant = variants?.first(where: { variant in
        let properties = variant["properties"] as? [String: Any]
        let actionProperty = properties?["action"] as? [String: Any]
        return (actionProperty?["enum"] as? [String]) == ["click"]
    })
    let clickAlternatives = clickVariant?["anyOf"] as? [[String: Any]]
    try expect(clickAlternatives?.contains(where: { ($0["required"] as? [String]) == ["element_id"] }) == true, "click accepts element id")
    try expect(clickAlternatives?.contains(where: { ($0["required"] as? [String]) == ["x", "y", "coordinate_space"] }) == true, "click accepts explicit coordinates")
    let coordinateProperty = properties?["coordinate_space"] as? [String: Any]
    try expect((coordinateProperty?["enum"] as? [String])?.contains("normalized") == true, "coordinate space supports resize-independent normalized points")
    let foregroundPolicy = properties?["foreground_policy"] as? [String: Any]
    try expect(foregroundPolicy?["default"] as? String == "avoid", "foreground policy defaults to avoid")
}

private func testPreservesAccessibilityAndScreenshotContent() throws {
    let backend = FakeBackend()
    let server = MCPServer(backend: backend)
    let response = try server.handle([
        "jsonrpc": "2.0", "id": 3, "method": "tools/call",
        "params": ["name": "computer", "arguments": ["action": "observe", "app": "com.apple.TextEdit"]],
    ])
    try expect(backend.commands.count == 1, "backend call count")
    try expect(backend.commands.first?.action == .observe, "observe action")
    try expect(backend.commands.first?.app == "com.apple.TextEdit", "observe app")
    let result = response?["result"] as? [String: Any]
    try expect(result?["isError"] as? Bool == false, "successful result")
    let content = result?["content"] as? [[String: Any]]
    try expect(content?.count == 2, "text and screenshot content")
    try expect(content?.first?["type"] as? String == "text", "text content")
    try expect(content?.last?["type"] as? String == "image", "image content")
    try expect(content?.last?["mimeType"] as? String == "image/png", "image mime")
    try expect(content?.last?["data"] as? String == Data("png".utf8).base64EncodedString(), "image data")
    let structured = result?["structuredContent"] as? [String: Any]
    try expect(structured?["ax_preferred"] as? Bool == true, "structured content")
}

private func testCoordinateAndKeyboardFallbackReachBackend() throws {
    let backend = FakeBackend()
    let server = MCPServer(backend: backend)
    _ = try server.handle([
        "jsonrpc": "2.0", "id": 4, "method": "tools/call",
        "params": ["name": "computer", "arguments": ["action": "click", "app": "com.apple.TextEdit", "x": 100, "y": 200]],
    ])
    _ = try server.handle([
        "jsonrpc": "2.0", "id": 5, "method": "tools/call",
        "params": ["name": "computer", "arguments": ["action": "press_key", "app": "com.apple.TextEdit", "key": "command+a"]],
    ])
    try expect(backend.commands.count == 2, "fallback call count")
    try expect(backend.commands[0].action == .click, "click action")
    try expect(backend.commands[0].x == 100 && backend.commands[0].y == 200, "click coordinates")
    try expect(backend.commands[1].action == .pressKey, "press key action")
    try expect(backend.commands[1].key == "command+a", "key value")
    try expect(backend.commands[0].foregroundPolicy == .avoid, "omitted foreground policy is avoid")
}

private func testForegroundPolicyParsingAndErrors() throws {
    let allowed = try ComputerCommand(arguments: [
        "action": "press_key", "app": "com.apple.TextEdit", "key": "return", "foreground_policy": "allow",
    ])
    try expect(allowed.foregroundPolicy == .allow, "allow foreground policy")

    do {
        _ = try ComputerCommand(arguments: ["action": "press_key", "app": "com.apple.TextEdit", "key": "return", "foreground_policy": "sometimes"])
        throw TestFailure.assertion("invalid foreground policy should fail")
    } catch let error as ComputerError {
        try expect(error.errorDescription?.hasPrefix("invalid_arguments:") == true, "invalid foreground policy error")
    }

    final class ForegroundFailingBackend: ComputerBackend {
        func perform(_ command: ComputerCommand) throws -> ComputerResult {
            throw ComputerError.requiresForeground("native input required")
        }
    }
    let server = MCPServer(backend: ForegroundFailingBackend())
    let response = try server.handle([
        "jsonrpc": "2.0", "id": 8, "method": "tools/call",
        "params": ["name": "computer", "arguments": ["action": "press_key", "app": "com.apple.TextEdit", "key": "return"]],
    ])
    let result = response?["result"] as? [String: Any]
    let structured = result?["structuredContent"] as? [String: Any]
    try expect(result?["isError"] as? Bool == true, "foreground rejection is a tool error")
    try expect(structured?["error_code"] as? String == "requires_foreground", "foreground rejection has a stable code")
}

private func testKeyChordParserSupportsSkyStyleAliases() throws {
    let chord = try KeyChord.parse("super+shift+a")
    try expect(chord.keyCode == 0, "A key code")
    try expect(chord.modifiers.contains(.command), "super maps to command")
    try expect(chord.modifiers.contains(.shift), "shift modifier")
    let enter = try KeyChord.parse("Return")
    try expect(enter.keyCode == 36, "Return key code")
    let multiply = try KeyChord.parse("*")
    try expect(multiply.keyCode == 28 && multiply.modifiers.contains(.shift), "asterisk maps to shift-8")
    let plus = try KeyChord.parse("+")
    try expect(plus.keyCode == 24 && plus.modifiers.contains(.shift), "plus maps to shift-equals")
    let commandPlus = try KeyChord.parse("cmd++")
    try expect(commandPlus.keyCode == 24 && commandPlus.modifiers.contains(.shift) && commandPlus.modifiers.contains(.command), "command-plus keeps both modifiers")
}

private func testNativePermissionAndAppDiscoveryAreInspectable() throws {
    let backend = MacComputerBackend()
    let permission = try backend.perform(ComputerCommand(arguments: ["action": "permission_status"]))
    try expect(permission.structured["accessibility"] is Bool, "accessibility permission status")
    try expect(permission.structured["screen_recording"] is Bool, "screen recording permission status")
    let apps = try backend.perform(ComputerCommand(arguments: ["action": "list_apps"]))
    try expect(apps.structured["apps"] is [[String: Any]], "running apps list")
}

private func testScreenshotCoordinatesMapToGlobalWindowCoordinates() throws {
    let geometry = CaptureGeometry(
        windowFrame: CGRect(x: 100, y: 50, width: 800, height: 600),
        imageWidth: 1600,
        imageHeight: 1200
    )
    let point = try geometry.screenPoint(x: 800, y: 600)
    try expect(point.x == 500 && point.y == 350, "screenshot midpoint maps to window midpoint")
    let normalizedPoint = try geometry.screenPoint(normalizedX: 500, normalizedY: 500)
    try expect(normalizedPoint.x == 500 && normalizedPoint.y == 350, "normalized midpoint maps independently of screenshot resolution")
    let padded = CaptureGeometry(
        windowFrame: CGRect(x: 100, y: 50, width: 800, height: 600),
        imageWidth: 1800,
        imageHeight: 1400,
        visibleImageFrame: CGRect(x: 100, y: 100, width: 1600, height: 1200)
    )
    let paddedPoint = try padded.screenPoint(x: 900, y: 700)
    try expect(paddedPoint.x == 500 && paddedPoint.y == 350, "transparent screenshot padding preserves full-image mapping")
}

do {
    try testInitializesAndAdvertisesFullComputerTool()
    try testPreservesAccessibilityAndScreenshotContent()
    try testCoordinateAndKeyboardFallbackReachBackend()
    try testForegroundPolicyParsingAndErrors()
    try testKeyChordParserSupportsSkyStyleAliases()
    try testNativePermissionAndAppDiscoveryAreInspectable()
    try testScreenshotCoordinatesMapToGlobalWindowCoordinates()
    print("cua-mac protocol tests passed")
} catch {
    FileHandle.standardError.write(Data("cua-mac protocol tests failed: \(error)\n".utf8))
    exit(1)
}
