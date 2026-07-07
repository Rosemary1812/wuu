import { useEffect, useState } from "react";
import type { SkinPreference, ThemePreference } from "../shared/protocol";
import { applySkinPreference, applyThemePreference } from "./Theme";

const THEME_OPTIONS: Array<{ value: ThemePreference; label: string }> = [
  { value: "system", label: "跟随系统" },
  { value: "light", label: "亮色" },
  { value: "dark", label: "暗色" },
];

const SKIN_OPTIONS: Array<{ value: SkinPreference; label: string }> = [
  { value: "flame", label: "火苗" },
  { value: "work", label: "工作" },
];

/**
 * 外观 row body: a three-way segmented control (system / light / dark).
 * Self-contained like CliInstallSection — reads and persists through
 * window.wuu directly, and applies the choice to <html data-theme>
 * immediately so the user sees the switch without a save step.
 */
export function ThemePreferenceControl(): JSX.Element {
  const [preference, setPreference] = useState<ThemePreference>(
    () => window.wuu?.initialThemePreference ?? "system",
  );

  useEffect(() => {
    let cancelled = false;
    void window.wuu
      ?.getThemePreference?.()
      .then((stored) => {
        if (!cancelled && stored) {
          setPreference(stored);
        }
      })
      .catch(() => {
        // Keep the preload-provided initial value.
      });
    return () => {
      cancelled = true;
    };
  }, []);

  function choose(next: ThemePreference): void {
    setPreference(next);
    applyThemePreference(next);
    void window.wuu?.setThemePreference?.(next).catch(() => {
      // Persistence failure leaves the applied theme for this window;
      // the next launch falls back to the stored preference.
    });
  }

  return (
    <div className="theme-segmented" role="group" aria-label="外观主题">
      {THEME_OPTIONS.map((option) => (
        <button
          key={option.value}
          type="button"
          aria-pressed={preference === option.value}
          data-testid={`settings-theme-${option.value}`}
          onClick={() => choose(option.value)}
        >
          {option.label}
        </button>
      ))}
    </div>
  );
}

/**
 * 风格 row body: the skin (theme family) axis, orthogonal to
 * light/dark. Same self-contained persistence pattern as the theme
 * control above.
 */
export function SkinPreferenceControl(): JSX.Element {
  const [preference, setPreference] = useState<SkinPreference>(
    () => window.wuu?.initialSkinPreference ?? "flame",
  );

  useEffect(() => {
    let cancelled = false;
    void window.wuu
      ?.getSkinPreference?.()
      .then((stored) => {
        if (!cancelled && stored) {
          setPreference(stored);
        }
      })
      .catch(() => {
        // Keep the preload-provided initial value.
      });
    return () => {
      cancelled = true;
    };
  }, []);

  function choose(next: SkinPreference): void {
    setPreference(next);
    applySkinPreference(next);
    void window.wuu?.setSkinPreference?.(next).catch(() => {
      // Persistence failure leaves the applied skin for this window;
      // the next launch falls back to the stored preference.
    });
  }

  return (
    <div className="theme-segmented" role="group" aria-label="界面风格">
      {SKIN_OPTIONS.map((option) => (
        <button
          key={option.value}
          type="button"
          aria-pressed={preference === option.value}
          data-testid={`settings-skin-${option.value}`}
          onClick={() => choose(option.value)}
        >
          {option.label}
        </button>
      ))}
    </div>
  );
}
