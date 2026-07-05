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

  // M1 sync IPC parity (Plan §7 risk #7 — required by §3 commit 5
  // verification, §2.2 IPC table row 2 sync bootstrap). Every
  // `ipcMain.on("wuu:...")` handler in main must have a matching
  // `ipcRenderer.sendSync("wuu:...")` call in preload. Without this
  // test the team would be free to add sync IPC channels that nobody
  // uses (or vice-versa); commit 5 adds `wuu:pop-out-init` as the
  // first non-theme sync channel.
  it("matches every sync channel between main and preload (M1)", () => {
    const mainSyncChannels = extractChannels(
      source("index.ts"),
      /ipcMain\.on\(\s*["']([^"']+)["']/gs,
    );
    const preloadSyncChannels = extractChannels(
      source("preload.ts"),
      /ipcRenderer\.sendSync\(\s*["']([^"']+)["']/gs,
    );
    // Both sides must agree on the set of `wuu:` sync channels. We
    // ignore non-`wuu:` channels (the renderer may sendSync other
    // channels in the future for third-party integrations) — the
    // parity guarantee is for desktop-owned IPC only.
    const wuuSyncMain = mainSyncChannels.filter((c) => c.startsWith("wuu:")).sort();
    const wuuSyncPreload = preloadSyncChannels.filter((c) => c.startsWith("wuu:")).sort();
    expect(wuuSyncPreload).toEqual(wuuSyncMain);
  });

  // Each sync handler must actually set `event.returnValue` so the
  // renderer's `sendSync` resolves with the right shape (not a
  // returning undefined). A regression where the handler forgets to
  // assign `event.returnValue` would silently return undefined to
  // the renderer; this test catches it by string-searching the source.
  it("every wuu: sync handler sets event.returnValue", () => {
    const index = source("index.ts");
    const syncHandlerMatches = [...index.matchAll(
      /ipcMain\.on\(\s*["'](wuu:[^"']+)["']/gs,
    )];
    for (const match of syncHandlerMatches) {
      const channel = match[1];
      const handlerStart = match.index ?? 0;
      // Find the matching closing `);` for this ipcMain.on(...)
      // call by tracking paren depth. Conservative: scan ahead 4 KB
      // for the close, which is far past any real handler.
      const slice = index.slice(handlerStart, handlerStart + 4096);
      let depth = 0;
      let end = -1;
      for (let i = 0; i < slice.length; i++) {
        const c = slice[i];
        if (c === "(") depth++;
        else if (c === ")") {
          depth--;
          if (depth === 0) {
            end = i;
            break;
          }
        }
      }
      expect(end, `could not find close of ipcMain.on for ${channel}`).toBeGreaterThan(0);
      const handler = slice.slice(0, end);
      expect(
        handler,
        `${channel} handler must set event.returnValue`,
      ).toMatch(/event\.returnValue\s*=/);
    }
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
