import AppKit
import CUAMacCore
import Foundation

_ = NSApplication.shared
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
