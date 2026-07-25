import { LoaderCircle, Mic, Sparkles, Square } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import type {
  SpeechRecognitionEvent,
  SpeechRecognitionState,
} from "../shared/protocol";
import { useI18n } from "./i18n";
import type { TranslationKey } from "./i18n/resources/zh-CN";
import { useVoiceInputSettings } from "./VoiceInputSettingsState";

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
  polishAvailable,
}: {
  prompt: string;
  setPrompt: (value: string) => void;
  disabled: boolean;
  locale: string;
  polishAvailable: boolean;
}): JSX.Element | null {
  const { t } = useI18n();
  const [phase, setPhaseState] = useState<VoicePhase>("idle");
  const [error, setError] = useState("");
  const { settings, updateSettings } = useVoiceInputSettings();
  const polishEnabled = settings.polish_enabled;
  const phaseRef = useRef<VoicePhase>("idle");
  const basePromptRef = useRef("");
  const transcriptRef = useRef("");
  const finalizingRef = useRef(false);
  const polishEnabledRef = useRef(settings.polish_enabled);
  const polishAvailableRef = useRef(polishAvailable);
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
    if (!polishEnabledRef.current || !polishAvailableRef.current) {
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
    polishAvailableRef.current = polishAvailable;
  }, [polishAvailable, settings.polish_enabled]);

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

  const active =
    phase !== "idle" && phase !== "error" && phase !== "polishing";
  const busy = phase === "polishing";
  const status = voiceStatusMessage(phase, error, t);

  async function start(): Promise<void> {
    basePromptRef.current = prompt;
    transcriptRef.current = "";
    finalizingRef.current = false;
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
    <div className="composer-voice-input">
      <button
        className={`composer-polish-toggle${polishEnabled ? " is-active" : ""}`}
        type="button"
        aria-pressed={polishEnabled}
        disabled={disabled || active || busy || !polishAvailable}
        title={
          polishAvailable
            ? t("composer.voice.polishHint")
            : t("composer.voice.polishUnavailable")
        }
        onClick={() => {
          const next = !settings.polish_enabled;
          polishEnabledRef.current = next;
          void updateSettings({
            ...settings,
            polish_enabled: next,
          }).catch(() => {
            polishEnabledRef.current = settings.polish_enabled;
            setError(t("composer.voice.settingsSaveFailed"));
            setPhase("error");
          });
        }}
      >
        <Sparkles aria-hidden="true" />
        <span>{t("composer.voice.polish")}</span>
      </button>
      {status ? (
        <span
          className={`composer-voice-status${phase === "error" ? " is-error" : ""}`}
          role={phase === "error" ? "alert" : "status"}
          title={status}
        >
          {status}
        </span>
      ) : null}
      <button
        className={`composer-voice-button${active ? " is-active" : ""}`}
        type="button"
        disabled={disabled || busy}
        aria-label={active ? t("composer.voice.stop") : t("composer.voice.start")}
        title={active ? t("composer.voice.stop") : t("composer.voice.startHint")}
        onClick={() => void (active ? stop() : start())}
      >
        {busy ? (
          <LoaderCircle className="composer-voice-spinner" aria-hidden="true" />
        ) : active ? (
          <Square aria-hidden="true" />
        ) : (
          <Mic aria-hidden="true" />
        )}
      </button>
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
