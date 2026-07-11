import AppKit
import CUAMacCore
import Foundation

_ = NSApplication.shared
if CommandLine.arguments.count >= 8, CommandLine.arguments[1] == "--native-pip" {
    let activityID = CommandLine.arguments[2]
    let target = CommandLine.arguments[3]
    let values = CommandLine.arguments[4...7].compactMap(Double.init)
    guard values.count == 4 else {
        FileHandle.standardError.write(Data("wuu-cua-mac native PiP failed: invalid initial frame\n".utf8))
        exit(2)
    }
    let frame = CGRect(x: values[0], y: values[1], width: values[2], height: values[3])
    NSApplication.shared.setActivationPolicy(.accessory)
    Task {
        do {
            try await runNativePiP(configuration: NativePiPConfiguration(activityID: activityID, target: target, frame: frame))
            exit(0)
        } catch {
            let message = (error as? LocalizedError)?.errorDescription ?? error.localizedDescription
            FileHandle.standardError.write(Data("wuu-cua-mac native PiP failed: \(message)\n".utf8))
            exit(1)
        }
    }
    NSApplication.shared.run()
    exit(0)
}
let server = MCPServer(backend: MacComputerBackend())

while let line = readLine(strippingNewline: true) {
    guard !line.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { continue }
    do {
        guard let data = line.data(using: .utf8),
              let request = try JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            throw ComputerError.invalidArguments("request must be a JSON object")
        }
        guard let response = try server.handle(request) else { continue }
        let encoded = try JSONSerialization.data(withJSONObject: response, options: [.sortedKeys])
        FileHandle.standardOutput.write(encoded)
        FileHandle.standardOutput.write(Data([0x0A]))
    } catch {
        let message = (error as? LocalizedError)?.errorDescription ?? error.localizedDescription
        let response: [String: Any] = [
            "jsonrpc": "2.0",
            "id": NSNull(),
            "error": ["code": -32700, "message": message],
        ]
        if let encoded = try? JSONSerialization.data(withJSONObject: response, options: [.sortedKeys]) {
            FileHandle.standardOutput.write(encoded)
            FileHandle.standardOutput.write(Data([0x0A]))
        }
    }
}
