import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { wuuHomePath } from "./projects";

// Desktop-only settings that the Electron main process needs before the
// renderer loads (e.g. whether to auto-install the wuu CLI symlink at
// startup). Persistence mirrors projects.ts: a small JSON file under the wuu
// home directory (~/.wuu), written synchronously on change.

import type { SkinPreference, ThemePreference } from "../shared/protocol";

export type { SkinPreference, ThemePreference };

export type DesktopSettings = {
  // Auto-install the wuu CLI into ~/.local/bin at app startup. Defaults to
  // true — the CLI is how agents invoke wuu, so it should not depend on the
  // user finding a button in settings.
  cli_auto_install?: boolean;
  // Appearance. "system" follows the OS light/dark preference; the
  // renderer resolves it to a concrete data-theme on <html>.
  theme?: ThemePreference;
  // Skin (theme family). "flame" is the default mascot-warm look;
  // "work" keeps the quiet SaaS look. Stamped as data-skin on <html>.
  skin?: SkinPreference;
};

const THEME_PREFERENCES: readonly ThemePreference[] = ["system", "light", "dark"];
const SKIN_PREFERENCES: readonly SkinPreference[] = ["flame", "work"];

export function desktopSettingsPath(): string {
  return join(wuuHomePath(), "desktop-settings.json");
}

export function readDesktopSettings(filePath: string = desktopSettingsPath()): DesktopSettings {
  try {
    const parsed = JSON.parse(readFileSync(filePath, "utf8")) as unknown;
    if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
      return {};
    }
    const record = parsed as Record<string, unknown>;
    const settings: DesktopSettings = {};
    if (typeof record.cli_auto_install === "boolean") {
      settings.cli_auto_install = record.cli_auto_install;
    }
    if (THEME_PREFERENCES.includes(record.theme as ThemePreference)) {
      settings.theme = record.theme as ThemePreference;
    }
    if (SKIN_PREFERENCES.includes(record.skin as SkinPreference)) {
      settings.skin = record.skin as SkinPreference;
    }
    return settings;
  } catch {
    // Missing or corrupted file → defaults.
    return {};
  }
}

export function writeDesktopSettings(
  settings: DesktopSettings,
  filePath: string = desktopSettingsPath(),
): void {
  mkdirSync(dirname(filePath), { recursive: true });
  writeFileSync(filePath, `${JSON.stringify(settings, null, 2)}\n`);
}

export function getCliAutoInstallEnabled(filePath?: string): boolean {
  return readDesktopSettings(filePath).cli_auto_install ?? true;
}

export function setCliAutoInstallEnabled(enabled: boolean, filePath?: string): void {
  const settings = readDesktopSettings(filePath);
  writeDesktopSettings({ ...settings, cli_auto_install: enabled }, filePath);
}

export function getThemePreference(filePath?: string): ThemePreference {
  return readDesktopSettings(filePath).theme ?? "system";
}

export function setThemePreference(theme: ThemePreference, filePath?: string): void {
  const settings = readDesktopSettings(filePath);
  writeDesktopSettings({ ...settings, theme }, filePath);
}

export function getSkinPreference(filePath?: string): SkinPreference {
  return readDesktopSettings(filePath).skin ?? "flame";
}

export function setSkinPreference(skin: SkinPreference, filePath?: string): void {
  const settings = readDesktopSettings(filePath);
  writeDesktopSettings({ ...settings, skin }, filePath);
}
