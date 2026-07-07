import { afterEach, describe, expect, it, vi } from "vitest";
import {
  appliedSkin,
  applySkinPreference,
  applyThemePreference,
  resolveThemePreference,
} from "./Theme";

type MediaListener = (event: { matches: boolean }) => void;

function stubMatchMedia(initialMatches: boolean): {
  fire: (matches: boolean) => void;
  listenerCount: () => number;
} {
  const listeners = new Set<MediaListener>();
  let matches = initialMatches;
  vi.stubGlobal(
    "matchMedia",
    vi.fn(() => ({
      get matches() {
        return matches;
      },
      addEventListener: (_type: string, listener: MediaListener) => {
        listeners.add(listener);
      },
      removeEventListener: (_type: string, listener: MediaListener) => {
        listeners.delete(listener);
      },
    })),
  );
  return {
    fire: (next: boolean) => {
      matches = next;
      for (const listener of listeners) {
        listener({ matches: next });
      }
    },
    listenerCount: () => listeners.size,
  };
}

afterEach(() => {
  vi.unstubAllGlobals();
  delete document.documentElement.dataset.theme;
  delete document.documentElement.dataset.skin;
});

describe("resolveThemePreference", () => {
  it("passes explicit light/dark through", () => {
    stubMatchMedia(true);
    expect(resolveThemePreference("light")).toBe("light");
    expect(resolveThemePreference("dark")).toBe("dark");
  });

  it("resolves system from prefers-color-scheme", () => {
    const media = stubMatchMedia(true);
    expect(resolveThemePreference("system")).toBe("dark");
    media.fire(false);
    expect(resolveThemePreference("system")).toBe("light");
  });
});

describe("applyThemePreference", () => {
  it("stamps the resolved theme on <html>", () => {
    stubMatchMedia(false);
    applyThemePreference("dark");
    expect(document.documentElement.dataset.theme).toBe("dark");
    applyThemePreference("light");
    expect(document.documentElement.dataset.theme).toBe("light");
  });

  it("follows live OS changes while the preference is system", () => {
    const media = stubMatchMedia(false);
    applyThemePreference("system");
    expect(document.documentElement.dataset.theme).toBe("light");

    media.fire(true);
    expect(document.documentElement.dataset.theme).toBe("dark");
    media.fire(false);
    expect(document.documentElement.dataset.theme).toBe("light");
  });

  it("stops following the OS once an explicit theme is applied", () => {
    const media = stubMatchMedia(false);
    applyThemePreference("system");
    expect(media.listenerCount()).toBe(1);

    applyThemePreference("light");
    expect(media.listenerCount()).toBe(0);

    media.fire(true);
    expect(document.documentElement.dataset.theme).toBe("light");
  });
});

describe("applySkinPreference", () => {
  it("stamps the skin without touching the theme", () => {
    stubMatchMedia(false);
    applyThemePreference("dark");
    applySkinPreference("flame");
    expect(appliedSkin()).toBe("flame");
    expect(document.documentElement.dataset.theme).toBe("dark");

    applySkinPreference("work");
    expect(appliedSkin()).toBe("work");
    expect(document.documentElement.dataset.theme).toBe("dark");
  });
});
