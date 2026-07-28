import { readFileSync } from "node:fs";
import { join } from "node:path";
import { wuuHomePath } from "./projects";
import { writeTextFileAtomicSync } from "./atomicFile";

// Desktop-only settings that the Electron main process needs before the
// renderer loads. Persistence mirrors projects.ts: a small JSON file under
// the wuu home directory (~/.wuu), written synchronously on change.

import {
  CODEX_PET_SIZE_DEFAULT,
  CODEX_PET_SIZE_OPTIONS,
  MESSAGE_FLOW_FONT_SIZE_RANGE,
  isLanguagePreference,
  type CodexPetSettings,
  type CodexPetSize,
  type MessageFlowFontSize,
  type LanguagePreference,
  type ThemePreference,
  type VoiceInputSettings,
} from "../shared/protocol";
import type { WindowBounds } from "./windowState";

export type {
  CodexPetSettings,
  MessageFlowFontSize,
  ThemePreference,
  LanguagePreference,
  VoiceInputSettings,
  WindowBounds,
};

export const DEFAULT_MESSAGE_FLOW_FONT_SIZE: MessageFlowFontSize =
  MESSAGE_FLOW_FONT_SIZE_RANGE.default;

function isMessageFlowFontSize(value: unknown): value is MessageFlowFontSize {
  return (
    typeof value === "number" &&
    Number.isFinite(value) &&
    value >= MESSAGE_FLOW_FONT_SIZE_RANGE.min &&
    value <= MESSAGE_FLOW_FONT_SIZE_RANGE.max
  );
}

export type DesktopSettings = {
  // Appearance. "system" follows the OS light/dark preference; the
  // renderer resolves it to a concrete data-theme on <html>.
  theme?: ThemePreference;
  language?: LanguagePreference;
  voice_input?: VoiceInputSettings;
  // User-facing reading size for the message stream, in pixels. The
  // renderer clamps incoming values to MESSAGE_FLOW_FONT_SIZE_RANGE
  // (13–20 step 0.5 default 14) before applying and the IPC boundary
  // keeps the same range check; an out-of-range persisted value here
  // falls back to the default.
  message_flow_font_size?: MessageFlowFontSize;
  // Codex Pets compatibility. The desktop reads local pets from Wuu's own
  // pets directory, with ~/.codex/pets as a compatibility source, and keeps
  // only the UI enablement + selected pet here.
  codex_pet?: CodexPetSettings;
  // Last position + size of the main window as captured on close. The center
  // point is checked against the connected displays at load time (in
  // windowState.loadMainWindowBounds) so an unplugged display is treated as
  // "no saved bounds" rather than "open off-screen".
  main_window_bounds?: WindowBounds;
};

const THEME_PREFERENCES: readonly ThemePreference[] = ["system", "light", "dark"];
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
    if (THEME_PREFERENCES.includes(record.theme as ThemePreference)) {
      settings.theme = record.theme as ThemePreference;
    }
    if (isLanguagePreference(record.language)) {
      settings.language = record.language;
    }
    if (
      typeof record.voice_input === "object" &&
      record.voice_input !== null &&
      !Array.isArray(record.voice_input)
    ) {
      const voiceInput = record.voice_input as Record<string, unknown>;
      settings.voice_input = {
        polish_enabled: voiceInput.polish_enabled === true,
        language: isLanguagePreference(voiceInput.language)
          ? voiceInput.language
          : "system",
      };
    }
    if (
      isMessageFlowFontSize(record.message_flow_font_size)
    ) {
      settings.message_flow_font_size = record.message_flow_font_size;
    }
    if (typeof record.codex_pet === "object" && record.codex_pet !== null && !Array.isArray(record.codex_pet)) {
      const codexPet = record.codex_pet as Record<string, unknown>;
      const sizeRaw = codexPet.size;
      const size =
        typeof sizeRaw === "string" &&
        CODEX_PET_SIZE_OPTIONS.some((option) => option.id === sizeRaw)
          ? (sizeRaw as CodexPetSize)
          : undefined;
      // 连续缩放值：正的有限数即可，精确范围钳制留给
      // CodexPetWindowManager.setScale（那里是所有 scale 输入的汇合点）。
      const scaleRaw = codexPet.scale;
      const scale =
        typeof scaleRaw === "number" && Number.isFinite(scaleRaw) && scaleRaw > 0
          ? scaleRaw
          : undefined;
      settings.codex_pet = {
        enabled: codexPet.enabled === true,
        selected_id: typeof codexPet.selected_id === "string" ? codexPet.selected_id.trim() : "",
        ...(size ? { size } : {}),
        ...(scale !== undefined ? { scale } : {}),
      };
    }
    if (
      typeof record.main_window_bounds === "object" &&
      record.main_window_bounds !== null &&
      !Array.isArray(record.main_window_bounds)
    ) {
      const bounds = record.main_window_bounds as Record<string, unknown>;
      if (
        typeof bounds.x === "number" && Number.isFinite(bounds.x) &&
        typeof bounds.y === "number" && Number.isFinite(bounds.y) &&
        typeof bounds.width === "number" && Number.isFinite(bounds.width) && bounds.width > 0 &&
        typeof bounds.height === "number" && Number.isFinite(bounds.height) && bounds.height > 0
      ) {
        settings.main_window_bounds = {
          x: bounds.x,
          y: bounds.y,
          width: bounds.width,
          height: bounds.height,
        };
      }
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
  writeTextFileAtomicSync(
    filePath,
    `${JSON.stringify(settings, null, 2)}\n`,
  );
}

export function getThemePreference(filePath?: string): ThemePreference {
  return readDesktopSettings(filePath).theme ?? "system";
}

export function setThemePreference(theme: ThemePreference, filePath?: string): void {
  const settings = readDesktopSettings(filePath);
  writeDesktopSettings({ ...settings, theme }, filePath);
}

export function getLanguagePreference(filePath?: string): LanguagePreference {
  return readDesktopSettings(filePath).language ?? "system";
}

export function setLanguagePreference(language: LanguagePreference, filePath?: string): void {
  const settings = readDesktopSettings(filePath);
  writeDesktopSettings({ ...settings, language }, filePath);
}

export function getVoiceInputSettings(filePath?: string): VoiceInputSettings {
  return (
    readDesktopSettings(filePath).voice_input ?? {
      polish_enabled: false,
      language: "system",
    }
  );
}

export function setVoiceInputSettings(
  voiceInput: VoiceInputSettings,
  filePath?: string,
): void {
  const settings = readDesktopSettings(filePath);
  const language = isLanguagePreference(voiceInput.language)
    ? voiceInput.language
    : "system";
  writeDesktopSettings(
    {
      ...settings,
      voice_input: {
        polish_enabled: voiceInput.polish_enabled === true,
        language,
      },
    },
    filePath,
  );
}

export function getMessageFlowFontSize(
  filePath?: string,
): MessageFlowFontSize {
  return (
    readDesktopSettings(filePath).message_flow_font_size ??
    DEFAULT_MESSAGE_FLOW_FONT_SIZE
  );
}

export function setMessageFlowFontSize(
  fontSize: MessageFlowFontSize,
  filePath?: string,
): void {
  const settings = readDesktopSettings(filePath);
  writeDesktopSettings({ ...settings, message_flow_font_size: fontSize }, filePath);
}

export function getCodexPetSettings(filePath?: string): CodexPetSettings {
  const stored = readDesktopSettings(filePath).codex_pet ?? {
    enabled: false,
    selected_id: "",
  };
  // size 是可选字段；缺省时统一回落到 CODEX_PET_SIZE_DEFAULT，让 renderer
  // 和 CodexPetWindowManager 都不必再各自处理 undefined。
  return { ...stored, size: stored.size ?? CODEX_PET_SIZE_DEFAULT };
}

export function setCodexPetSettings(next: CodexPetSettings, filePath?: string): void {
  const settings = readDesktopSettings(filePath);
  // codex_pet 对象整体替换：省略的可选字段（size/scale）即从持久化文件
  // 删除。部分更新（保留旧值）由调用方负责——index.ts 的
  // updateCodexPetSettings 先读旧值合并，要保留的字段总是显式传入；
  // 显式选 size 档位的路径靠"省略 scale 即删除"清掉连续缩放覆盖。
  const nextSize = next.size;
  const nextScale = next.scale;
  writeDesktopSettings({
    ...settings,
    codex_pet: {
      enabled: next.enabled,
      selected_id: next.selected_id.trim(),
      ...(nextSize !== undefined ? { size: nextSize } : {}),
      ...(nextScale !== undefined ? { scale: nextScale } : {}),
    },
  }, filePath);
}

export function getCodexPetSize(filePath?: string): CodexPetSize {
  return getCodexPetSettings(filePath).size ?? CODEX_PET_SIZE_DEFAULT;
}

export function getCodexPetScale(filePath?: string): number | undefined {
  // 连续缩放值优先于 size 档位；损坏的持久化值（非有限数、非正数）当作
  // 未设置，回落到档位。范围钳制交给 CodexPetWindowManager.setScale。
  const scale = getCodexPetSettings(filePath).scale;
  return typeof scale === "number" && Number.isFinite(scale) && scale > 0
    ? scale
    : undefined;
}

export function getMainWindowBounds(filePath?: string): WindowBounds | undefined {
  return readDesktopSettings(filePath).main_window_bounds;
}

export function setMainWindowBounds(bounds: WindowBounds, filePath?: string): void {
  const settings = readDesktopSettings(filePath);
  writeDesktopSettings(
    { ...settings, main_window_bounds: { ...bounds } },
    filePath,
  );
}
