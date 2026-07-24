import AVFoundation
import Foundation
import Speech

private struct Event: Encodable {
    let type: String
    let state: String?
    let text: String?
    let isFinal: Bool?
    let code: String?
    let message: String?

    enum CodingKeys: String, CodingKey {
        case type, state, text, code, message
        case isFinal = "is_final"
    }

    static func state(_ value: String) -> Event {
        Event(type: "state", state: value, text: nil, isFinal: nil, code: nil, message: nil)
    }

    static func result(_ text: String, final: Bool) -> Event {
        Event(type: "result", state: nil, text: text, isFinal: final, code: nil, message: nil)
    }

    static func error(_ code: String, _ message: String) -> Event {
        Event(type: "error", state: nil, text: nil, isFinal: nil, code: code, message: message)
    }
}

private func emit(_ event: Event) {
    guard let data = try? JSONEncoder().encode(event),
          let line = String(data: data, encoding: .utf8) else {
        return
    }
    FileHandle.standardOutput.write(Data((line + "\n").utf8))
}

private final class SpeechSession {
    private let localeIdentifier: String
    private let audioEngine = AVAudioEngine()
    private var recognitionTask: SFSpeechRecognitionTask?
    private var recognitionRequest: SFSpeechAudioBufferRecognitionRequest?
    private var stopped = false

    init(localeIdentifier: String) {
        self.localeIdentifier = localeIdentifier
    }

    func start() {
        guard SFSpeechRecognizer.supportedLocales().contains(where: {
            $0.identifier.caseInsensitiveCompare(localeIdentifier) == .orderedSame
        }) else {
            emit(.error("locale_unavailable", "The selected language is not supported by macOS Speech Recognition."))
            exit(2)
        }

        guard let recognizer = SFSpeechRecognizer(locale: Locale(identifier: localeIdentifier)) else {
            emit(.error("locale_unavailable", "The selected language is not supported by macOS Speech Recognition."))
            exit(2)
        }
        guard recognizer.isAvailable else {
            emit(.error("recognizer_unavailable", "macOS Speech Recognition is currently unavailable."))
            exit(2)
        }
        guard recognizer.supportsOnDeviceRecognition else {
            emit(.error("on_device_unavailable", "On-device speech recognition is unavailable for the selected language."))
            exit(2)
        }

        emit(.state("requesting_speech_permission"))
        SFSpeechRecognizer.requestAuthorization { [weak self] status in
            DispatchQueue.main.async {
                guard let self else { return }
                switch status {
                case .authorized:
                    self.beginRecognition(with: recognizer)
                case .denied:
                    emit(.error("speech_permission_denied", "Speech Recognition permission was denied."))
                    exit(3)
                case .restricted:
                    emit(.error("speech_permission_restricted", "Speech Recognition is restricted on this Mac."))
                    exit(3)
                case .notDetermined:
                    emit(.error("speech_permission_unavailable", "Speech Recognition permission could not be determined."))
                    exit(3)
                @unknown default:
                    emit(.error("speech_permission_unavailable", "Speech Recognition permission is unavailable."))
                    exit(3)
                }
            }
        }
    }

    func stop() {
        guard !stopped else { return }
        stopped = true
        recognitionRequest?.endAudio()
        audioEngine.stop()
        audioEngine.inputNode.removeTap(onBus: 0)
        recognitionTask?.cancel()
        emit(.state("stopped"))
        exit(0)
    }

    private func beginRecognition(with recognizer: SFSpeechRecognizer) {
        let request = SFSpeechAudioBufferRecognitionRequest()
        request.shouldReportPartialResults = true
        request.requiresOnDeviceRecognition = true
        recognitionRequest = request

        let inputNode = audioEngine.inputNode
        let format = inputNode.outputFormat(forBus: 0)
        guard format.sampleRate > 0, format.channelCount > 0 else {
            emit(.error("microphone_unavailable", "No usable microphone input is available."))
            exit(4)
        }
        inputNode.installTap(onBus: 0, bufferSize: 1024, format: format) { buffer, _ in
            request.append(buffer)
        }

        recognitionTask = recognizer.recognitionTask(with: request) { [weak self] result, error in
            guard let self, !self.stopped else { return }
            if let result {
                emit(.result(result.bestTranscription.formattedString, final: result.isFinal))
                if result.isFinal {
                    self.stop()
                    return
                }
            }
            if let error {
                emit(.error("recognition_failed", error.localizedDescription))
                exit(5)
            }
        }

        do {
            audioEngine.prepare()
            try audioEngine.start()
            emit(.state("listening"))
        } catch {
            emit(.error("audio_engine_failed", error.localizedDescription))
            exit(4)
        }
    }
}

let localeFlagIndex = CommandLine.arguments.firstIndex(of: "--locale")
let localeIdentifier = localeFlagIndex.flatMap { index -> String? in
    let valueIndex = CommandLine.arguments.index(after: index)
    return valueIndex < CommandLine.arguments.endIndex ? CommandLine.arguments[valueIndex] : nil
} ?? Locale.current.identifier

private let session = SpeechSession(localeIdentifier: localeIdentifier)
FileHandle.standardInput.readabilityHandler = { handle in
    if !handle.availableData.isEmpty {
        DispatchQueue.main.async {
            session.stop()
        }
    }
}
session.start()
RunLoop.main.run()
