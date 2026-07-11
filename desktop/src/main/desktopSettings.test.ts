import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  getCodexPetSettings,
  getCliAutoInstallEnabled,
  getMainWindowBounds,
  getMessageFlowFontSize,
  getThemePreference,
  readDesktopSettings,
  setCodexPetSettings,
  setCliAutoInstallEnabled,
  setMainWindowBounds,
  setMessageFlowFontSize,
  setThemePreference,
  writeDesktopSettings,
} from "./desktopSettings";

let dir: string;
let file: string;

beforeEach(async () => {
  dir = await mkdtemp(join(tmpdir(), "wuu-desktop-settings-"));
  file = join(dir, "desktop-settings.json");
});

afterEach(async () => {
  await rm(dir, { recursive: true, force: true });
});

describe("desktopSettings", () => {
  it("defaults cli_auto_install to enabled when no file exists", () => {
    expect(getCliAutoInstallEnabled(file)).toBe(true);
  });

  it("round-trips the auto-install flag", () => {
    setCliAutoInstallEnabled(false, file);
    expect(getCliAutoInstallEnabled(file)).toBe(false);
    setCliAutoInstallEnabled(true, file);
    expect(getCliAutoInstallEnabled(file)).toBe(true);
  });

  it("creates parent directories on write", async () => {
    const nested = join(dir, "a", "b", "settings.json");
    writeDesktopSettings({ cli_auto_install: false }, nested);
    expect(JSON.parse(await readFile(nested, "utf8"))).toEqual({ cli_auto_install: false });
  });

  it("falls back to defaults on corrupted or malformed files", async () => {
    await writeFile(file, "{not json");
    expect(getCliAutoInstallEnabled(file)).toBe(true);
    await writeFile(file, JSON.stringify({ cli_auto_install: "yes" }));
    expect(readDesktopSettings(file)).toEqual({});
    expect(getCliAutoInstallEnabled(file)).toBe(true);
  });

  it("preserves unknown-but-valid fields it manages", () => {
    setCliAutoInstallEnabled(false, file);
    const settings = readDesktopSettings(file);
    expect(settings.cli_auto_install).toBe(false);
  });

  it("defaults the theme preference to system", () => {
    expect(getThemePreference(file)).toBe("system");
  });

  it("round-trips the theme preference", () => {
    setThemePreference("dark", file);
    expect(getThemePreference(file)).toBe("dark");
    setThemePreference("light", file);
    expect(getThemePreference(file)).toBe("light");
    setThemePreference("system", file);
    expect(getThemePreference(file)).toBe("system");
  });

  it("rejects unknown theme values on read", async () => {
    await writeFile(file, JSON.stringify({ theme: "sepia" }));
    expect(getThemePreference(file)).toBe("system");
  });

  it("keeps the theme when toggling other settings", () => {
    setThemePreference("dark", file);
    setCliAutoInstallEnabled(false, file);
    expect(getThemePreference(file)).toBe("dark");
    expect(getCliAutoInstallEnabled(file)).toBe(false);
  });

  it("defaults the message-flow font size to 14", () => {
    expect(getMessageFlowFontSize(file)).toBe(14);
  });

  it("round-trips the message-flow font size", () => {
    setMessageFlowFontSize(13, file);
    expect(getMessageFlowFontSize(file)).toBe(13);
    setMessageFlowFontSize(20, file);
    expect(getMessageFlowFontSize(file)).toBe(20);
    setMessageFlowFontSize(15, file);
    expect(getMessageFlowFontSize(file)).toBe(15);
  });

  it("rejects out-of-range message-flow font size values on read", async () => {
    await writeFile(file, JSON.stringify({ message_flow_font_size: 5 }));
    expect(getMessageFlowFontSize(file)).toBe(14);
    await writeFile(file, JSON.stringify({ message_flow_font_size: 100 }));
    expect(getMessageFlowFontSize(file)).toBe(14);
    await writeFile(file, JSON.stringify({ message_flow_font_size: "huge" }));
    expect(getMessageFlowFontSize(file)).toBe(14);
    await writeFile(file, JSON.stringify({ message_flow_font_size: null }));
    expect(getMessageFlowFontSize(file)).toBe(14);
  });

  it("keeps the message-flow font size when toggling other settings", () => {
    setMessageFlowFontSize(16, file);
    setThemePreference("dark", file);
    expect(getMessageFlowFontSize(file)).toBe(16);
    expect(getThemePreference(file)).toBe("dark");
  });

  it("defaults codex pets to disabled with no selected pet", () => {
    expect(getCodexPetSettings(file)).toEqual({ enabled: false, selected_id: "" });
  });

  it("round-trips codex pet settings while preserving other desktop settings", () => {
    setThemePreference("dark", file);
    setCodexPetSettings({ enabled: true, selected_id: "pixel-duck" }, file);
    expect(getCodexPetSettings(file)).toEqual({ enabled: true, selected_id: "pixel-duck" });
    expect(getThemePreference(file)).toBe("dark");

    setCodexPetSettings({ enabled: false, selected_id: "" }, file);
    expect(getCodexPetSettings(file)).toEqual({ enabled: false, selected_id: "" });
    expect(getThemePreference(file)).toBe("dark");
  });

  it("ignores a legacy skin field left in the settings file", async () => {
    // Older builds persisted a `skin` preference; after the skin axis was
    // removed the reader must simply drop the unknown field, not choke.
    await writeFile(file, JSON.stringify({ theme: "dark", skin: "work" }));
    expect(readDesktopSettings(file)).toEqual({ theme: "dark" });
    expect(getThemePreference(file)).toBe("dark");
  });

  it("returns undefined when no main_window_bounds have been saved", () => {
    expect(getMainWindowBounds(file)).toBeUndefined();
  });

  it("round-trips main_window_bounds while preserving other settings", () => {
    setThemePreference("dark", file);
    setMainWindowBounds({ x: 100, y: 200, width: 1280, height: 800 }, file);
    expect(getMainWindowBounds(file)).toEqual({
      x: 100,
      y: 200,
      width: 1280,
      height: 800,
    });
    expect(getThemePreference(file)).toBe("dark");

    setMainWindowBounds({ x: 0, y: 0, width: 880, height: 560 }, file);
    expect(getMainWindowBounds(file)).toEqual({
      x: 0,
      y: 0,
      width: 880,
      height: 560,
    });
    expect(getThemePreference(file)).toBe("dark");
  });

  it("ignores invalid main_window_bounds shapes on read", async () => {
    await writeFile(
      file,
      JSON.stringify({ main_window_bounds: "near the screen" }),
    );
    expect(getMainWindowBounds(file)).toBeUndefined();

    await writeFile(
      file,
      JSON.stringify({
        main_window_bounds: { x: "1", y: 2, width: 100, height: 100 },
      }),
    );
    expect(getMainWindowBounds(file)).toBeUndefined();

    await writeFile(
      file,
      JSON.stringify({
        main_window_bounds: { x: 1, y: 2, width: -1, height: 100 },
      }),
    );
    expect(getMainWindowBounds(file)).toBeUndefined();

    await writeFile(
      file,
      JSON.stringify({ theme: "light", main_window_bounds: { x: 1, y: 2 } }),
    );
    // Partial object (missing width / height) is dropped, theme survives.
    expect(getMainWindowBounds(file)).toBeUndefined();
    expect(getThemePreference(file)).toBe("light");
  });
});
