import type { ThemePreference } from "../shared/protocol";

/**
 * Renderer-side theme controller.
 *
 * The single source of visual truth is the `data-theme` attribute on
 * <html>: every color in the stylesheets resolves through CSS custom
 * properties, and the theme blocks in base.css key off
 * `:root[data-theme="dark"]`. This module resolves a persisted
 * preference ("system" | "light" | "dark") to a concrete attribute
 * value and keeps it in sync with the OS when the preference is
 * "system".
 *
 * The preload script stamps the attribute once before first paint (so
 * boot doesn't flash light); `applyThemePreference` is the runtime
 * authority from then on.
 */

const DARK_SCHEME_QUERY = "(prefers-color-scheme: dark)";

let systemListenerCleanup: (() => void) | undefined;

export function resolveThemePreference(
  preference: ThemePreference,
): "light" | "dark" {
  if (preference === "system") {
    return typeof window !== "undefined" &&
      window.matchMedia?.(DARK_SCHEME_QUERY).matches
      ? "dark"
      : "light";
  }
  return preference;
}

export function appliedTheme(): string | undefined {
  return document.documentElement.dataset.theme;
}

/**
 * Stamp the resolved theme on <html> and, for "system", follow OS
 * changes live until a different preference is applied.
 */
export function applyThemePreference(preference: ThemePreference): void {
  systemListenerCleanup?.();
  systemListenerCleanup = undefined;

  document.documentElement.dataset.theme = resolveThemePreference(preference);

  if (preference !== "system" || typeof window === "undefined") {
    return;
  }
  const query = window.matchMedia?.(DARK_SCHEME_QUERY);
  if (!query?.addEventListener) {
    return;
  }
  const onChange = (event: MediaQueryListEvent): void => {
    document.documentElement.dataset.theme = event.matches ? "dark" : "light";
  };
  query.addEventListener("change", onChange);
  systemListenerCleanup = () => query.removeEventListener("change", onChange);
}
