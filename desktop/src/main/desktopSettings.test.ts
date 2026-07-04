import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  getCliAutoInstallEnabled,
  getThemePreference,
  readDesktopSettings,
  setCliAutoInstallEnabled,
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
});
