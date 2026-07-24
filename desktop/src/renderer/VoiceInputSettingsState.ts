import { useCallback, useEffect, useState } from "react";
import type { VoiceInputSettings } from "../shared/protocol";

export const DEFAULT_VOICE_INPUT_SETTINGS: VoiceInputSettings = {
  polish_enabled: false,
  language: "system",
};

export function useVoiceInputSettings(): {
  settings: VoiceInputSettings;
  updateSettings: (next: VoiceInputSettings) => Promise<void>;
} {
  const [settings, setSettings] = useState<VoiceInputSettings>(
    () =>
      window.wuu?.initialVoiceInputSettings ??
      DEFAULT_VOICE_INPUT_SETTINGS,
  );

  useEffect(() => {
    const api = window.wuu as Partial<typeof window.wuu> | undefined;
    let cancelled = false;
    const unsubscribe =
      typeof api?.onVoiceInputSettingsChange === "function"
        ? api.onVoiceInputSettingsChange((next) => {
            if (!cancelled) setSettings(next);
          })
        : undefined;
    return () => {
      cancelled = true;
      unsubscribe?.();
    };
  }, []);

  const updateSettings = useCallback(
    async (next: VoiceInputSettings): Promise<void> => {
      const api = window.wuu as Partial<typeof window.wuu> | undefined;
      if (typeof api?.updateVoiceInputSettings !== "function") {
        throw new Error("Voice input settings are unavailable");
      }
      const saved = await api.updateVoiceInputSettings(next);
      setSettings(saved);
    },
    [],
  );

  return { settings, updateSettings };
}
