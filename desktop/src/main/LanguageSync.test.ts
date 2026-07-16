import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const read = (path: string): string =>
  readFileSync(resolve(process.cwd(), path), "utf8");

const mainSource = read("src/main/index.ts");
const preloadSource = read("src/main/preload.ts");
const providerSource = read("src/renderer/i18n/index.tsx");

describe("multi-window language sync", () => {
  it("broadcasts an explicit preference after persisting it", () => {
    const handlerStart = mainSource.indexOf('"wuu:language-preference-set"');
    expect(handlerStart).toBeGreaterThan(-1);
    const handlerEnd = mainSource.indexOf("});", handlerStart);
    const handler = mainSource.slice(handlerStart, handlerEnd);
    expect(handler).toContain("setLanguagePreference(next)");
    expect(handler).toContain("broadcastLanguagePreference()");
    expect(mainSource).toContain(
      'broadcastToAll("wuu:language-preference-changed", getLanguagePreference())',
    );
  });

  it("exposes a validated preload subscription with cleanup", () => {
    expect(preloadSource).toContain("onLanguagePreferenceChange");
    expect(preloadSource).toContain(
      'ipcRenderer.on("wuu:language-preference-changed"',
    );
    expect(preloadSource).toContain(
      'ipcRenderer.removeListener("wuu:language-preference-changed"',
    );
  });

  it("subscribes every provider instance to the app-global preference", () => {
    expect(providerSource).toContain("onLanguagePreferenceChange?.((next)");
    expect(providerSource).toContain("setPreferenceState(next)");
  });
});
