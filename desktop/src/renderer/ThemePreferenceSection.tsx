import { useEffect, useState } from "react";
import type { ThemePreference } from "../shared/protocol";
import { applyThemePreference } from "./Theme";

const THEME_OPTIONS: Array<{ value: ThemePreference; label: string }> = [
  { value: "system", label: "跟随系统" },
  { value: "light", label: "亮色" },
  { value: "dark", label: "暗色" },
];

/**
 * 外观 row body: a three-way segmented control (system / light / dark).
 * Reads and persists through window.wuu directly, and applies the choice to
 * <html data-theme>
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

  // Reflect switches made in other windows: the preference is app-global,
  // so the segmented control must follow the main-process broadcast too.
  useEffect(
    () =>
      window.wuu?.onThemePreferenceChange?.((theme) => {
        setPreference(theme);
      }),
    [],
  );

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
