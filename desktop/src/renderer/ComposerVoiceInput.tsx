import { LoaderCircle, Mic } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import type {
  SpeechRecognitionEvent,
  SpeechRecognitionState,
} from "../shared/protocol";
import { useI18n } from "./i18n";
import type { TranslationKey } from "./i18n/resources/zh-CN";
import { useVoiceInputSettings } from "./VoiceInputSettingsState";

const VOICE_WAVEFORM_BAR_COUNT = 32;
const VOICE_WAVEFORM_FLOOR = 0.08;

function emptyVoiceWaveform(): number[] {
  return Array.from({ length: VOICE_WAVEFORM_BAR_COUNT }, () => VOICE_WAVEFORM_FLOOR);
}

type VoicePhase =
  | "idle"
  | SpeechRecognitionState
  | "polishing"
  | "error";

export function ComposerVoiceInput({
  prompt,
  setPrompt,
  disabled,
  locale,
  onRecordingChange,
}: {
  prompt: string;
  setPrompt: (value: string) => void;
  disabled: boolean;
  locale: string;
  onRecordingChange?: (recording: boolean) => void;
}): JSX.Element | null {
  const { t } = useI18n();
  const [phase, setPhaseState] = useState<VoicePhase>("idle");
  const [error, setError] = useState("");
  const [audioLevels, setAudioLevels] = useState(emptyVoiceWaveform);
  const { settings } = useVoiceInputSettings();
  const phaseRef = useRef<VoicePhase>("idle");
  const basePromptRef = useRef("");
  const transcriptRef = useRef("");
  const finalizingRef = useRef(false);
  const polishEnabledRef = useRef(settings.polish_enabled);
  const supported =
    window.wuu?.platform === "darwin" &&
    typeof window.wuu.startSpeechRecognition === "function";

  function setPhase(next: VoicePhase): void {
    phaseRef.current = next;
    setPhaseState(next);
  }

  function composedPrompt(text: string): string {
    const base = basePromptRef.current;
    if (!base || !text) return `${base}${text}`;
    return `${base}${/\s$/.test(base) ? "" : "\n"}${text}`;
  }

  async function finishTranscript(rawText: string): Promise<void> {
    const text = rawText.trim();
    if (!text || finalizingRef.current) {
      setPhase("idle");
      return;
    }
    finalizingRef.current = true;
    setPrompt(composedPrompt(text));
    if (!polishEnabledRef.current) {
      finalizingRef.current = false;
      setPhase("idle");
      return;
    }
    setPhase("polishing");
    try {
      const result = await window.wuu.polishText(text);
      setPrompt(composedPrompt(result.text.trim() || text));
      setError("");
      setPhase("idle");
    } catch {
      setPrompt(composedPrompt(text));
      setError(t("composer.voice.polishFailed"));
      setPhase("error");
    } finally {
      finalizingRef.current = false;
    }
  }

  useEffect(() => {
    if (!supported) return;
    return window.wuu.onSpeechRecognitionEvent((event) => {
      handleSpeechEvent(event);
    });

    function handleSpeechEvent(event: SpeechRecognitionEvent): void {
      if (event.type === "level") {
        const level = Number.isFinite(event.level)
          ? Math.min(Math.max(event.level, VOICE_WAVEFORM_FLOOR), 1)
          : VOICE_WAVEFORM_FLOOR;
        setAudioLevels((current) => [...current.slice(1), level]);
        return;
      }
      if (event.type === "state") {
        if (event.state === "stopped") {
          if (phaseRef.current !== "polishing") {
            setPhase("idle");
          }
          return;
        }
        setPhase(event.state);
        return;
      }
      if (event.type === "error") {
        setError(voiceErrorMessage(event.code, t));
        setPhase("error");
        return;
      }
      transcriptRef.current = event.text;
      setPrompt(composedPrompt(event.text));
      if (event.is_final) {
        void finishTranscript(event.text);
      }
    }
  }, [supported, t]);

  useEffect(() => {
    polishEnabledRef.current = settings.polish_enabled;
  }, [settings.polish_enabled]);

  const recording =
    phase === "requesting_microphone_permission" ||
    phase === "requesting_speech_permission" ||
    phase === "listening";

  useEffect(() => {
    onRecordingChange?.(recording);
  }, [onRecordingChange, recording]);

  useEffect(
    () => () => onRecordingChange?.(false),
    [onRecordingChange],
  );

  useEffect(
    () => () => {
      if (
        phaseRef.current !== "idle" &&
        phaseRef.current !== "error"
      ) {
        void window.wuu.stopSpeechRecognition();
      }
    },
    [],
  );

  if (!supported) return null;

  const busy = phase === "polishing";
  const status = voiceStatusMessage(phase, error, t);

  async function start(): Promise<void> {
    basePromptRef.current = prompt;
    transcriptRef.current = "";
    finalizingRef.current = false;
    setAudioLevels(emptyVoiceWaveform());
    setError("");
    setPhase("requesting_microphone_permission");
    try {
      const recognitionLocale =
        settings.language === "system" ? locale : settings.language;
      const result = await window.wuu.startSpeechRecognition(recognitionLocale);
      if (!result.ok) {
        setError(voiceErrorMessage(result.error, t));
        setPhase("error");
      }
    } catch {
      setError(t("composer.voice.systemUnavailable"));
      setPhase("error");
    }
  }

  async function stop(): Promise<void> {
    const rawText = transcriptRef.current;
    await window.wuu.stopSpeechRecognition();
    await finishTranscript(rawText);
  }

  return (
    <div className={`composer-voice-input${recording ? " is-recording" : ""}`}>
      {recording ? (
        <button
          className="composer-voice-recording"
          type="button"
          aria-label={t("composer.voice.stop")}
          title={t("composer.voice.stop")}
          disabled={phase !== "listening"}
          onClick={() => void stop()}
        >
          <span className="composer-voice-live-dot" aria-hidden="true" />
          <span className="composer-voice-waveform" aria-hidden="true">
            {audioLevels.map((level, index) => (
              <span
                className="composer-voice-waveform-bar"
                key={index}
                style={{ height: `${Math.round(3 + level * 19)}px` }}
              />
            ))}
          </span>
          <span className="composer-voice-recording-label">{status}</span>
        </button>
      ) : status ? (
        <span
          className={`composer-voice-status${phase === "error" ? " is-error" : ""}`}
          role={phase === "error" ? "alert" : "status"}
          title={status}
        >
          {status}
        </span>
      ) : null}
      {!recording ? (
        <button
          className="composer-voice-button"
          type="button"
          disabled={disabled || busy}
          aria-label={t("composer.voice.start")}
          title={t("composer.voice.startHint")}
          onClick={() => void start()}
        >
          {busy ? (
            <LoaderCircle className="composer-voice-spinner" aria-hidden="true" />
          ) : (
            <Mic aria-hidden="true" />
          )}
        </button>
      ) : null}
    </div>
  );
}

function voiceStatusMessage(
  phase: VoicePhase,
  error: string,
  t: (key: TranslationKey) => string,
): string {
  switch (phase) {
    case "requesting_microphone_permission":
      return t("composer.voice.requestingMicrophone");
    case "requesting_speech_permission":
      return t("composer.voice.requestingSpeech");
    case "listening":
      return t("composer.voice.listening");
    case "polishing":
      return t("composer.voice.polishing");
    case "error":
      return error;
    default:
      return "";
  }
}

function voiceErrorMessage(
  code: string,
  t: (key: TranslationKey) => string,
): string {
  switch (code) {
    case "microphone_permission_denied":
      return t("composer.voice.microphoneDenied");
    case "speech_permission_denied":
    case "speech_permission_restricted":
    case "speech_permission_unavailable":
      return t("composer.voice.speechDenied");
    case "locale_unavailable":
      return t("composer.voice.localeUnavailable");
    case "on_device_unavailable":
      return t("composer.voice.onDeviceUnavailable");
    case "platform_unsupported":
      return t("composer.voice.platformUnsupported");
    default:
      return t("composer.voice.systemUnavailable");
  }
}
