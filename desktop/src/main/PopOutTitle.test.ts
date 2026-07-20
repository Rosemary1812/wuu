import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const read = (path: string): string =>
  readFileSync(resolve(process.cwd(), path), "utf8");

describe("pop-out title ownership", () => {
  const mainSource = read("src/main/index.ts");
  const appSource = read("src/renderer/App.tsx");

  it("does not race renderer hydration with a main-process thread lookup", () => {
    const start = mainSource.indexOf("function createPopOutWindow");
    const end = mainSource.indexOf("function createWindow", start);
    const createPopOutWindow = mainSource.slice(start, end);
    expect(createPopOutWindow).not.toContain('"thread/list"');
    expect(createPopOutWindow).not.toContain("win.setTitle(");
  });

  it("derives the loaded title from hydrated state and current translations", () => {
    expect(appSource).toContain("const popOutWindowTitle =");
    expect(appSource).toContain('t("tabs.newConversation")');
    expect(appSource).toContain("document.title = `wuu · ${popOutWindowTitle}`");
    expect(appSource).toContain("[poppedOutMode, popOutWindowTitle]");
  });
});
