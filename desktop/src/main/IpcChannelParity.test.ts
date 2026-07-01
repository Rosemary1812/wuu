import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const here = dirname(fileURLToPath(import.meta.url));

function source(name: string): string {
  return readFileSync(resolve(here, name), "utf8");
}

function extractChannels(sourceText: string, pattern: RegExp): string[] {
  return [...new Set([...sourceText.matchAll(pattern)].map((match) => match[1]))].sort();
}

function duplicateChannels(channels: string[]): string[] {
  const seen = new Set<string>();
  const duplicates = new Set<string>();
  for (const channel of channels) {
    if (seen.has(channel)) {
      duplicates.add(channel);
    }
    seen.add(channel);
  }
  return [...duplicates].sort();
}

describe("IPC channel parity", () => {
  const mainChannels = extractChannels(
    source("index.ts"),
    /ipcMain\.handle\(\s*["']([^"']+)["']/gs,
  );
  const preloadChannels = extractChannels(
    source("preload.ts"),
    /ipcRenderer\.invoke\(\s*["']([^"']+)["']/gs,
  );

  it("exposes every main-process handler through preload", () => {
    expect(preloadChannels).toEqual(mainChannels);
  });

  it("does not duplicate invoke or handler channels", () => {
    const rawMainChannels = [...source("index.ts").matchAll(/ipcMain\.handle\(\s*["']([^"']+)["']/gs)].map(
      (match) => match[1],
    );
    const rawPreloadChannels = [
      ...source("preload.ts").matchAll(/ipcRenderer\.invoke\(\s*["']([^"']+)["']/gs),
    ].map((match) => match[1]);

    expect(duplicateChannels(rawMainChannels)).toEqual([]);
    expect(duplicateChannels(rawPreloadChannels)).toEqual([]);
  });

  it("forwards all runtime connection fields to the app server", () => {
    const index = source("index.ts");
    const handlerStart = index.indexOf('"wuu:config-model-update"');
    const handlerEnd = index.indexOf('"wuu:config-advanced-update"', handlerStart);
    const handler = index.slice(handlerStart, handlerEnd);

    expect(handler).toContain("auth_token?: string");
    expect(handler).toContain("type?: string");
    expect(handler).toContain("auth_token: connection.auth_token");
    expect(handler).toContain("type: connection.type");
  });
});
