import { afterEach, describe, expect, it } from "vitest";
import {
  mainTranslate,
  resolveMainLocale,
  setMainLocale,
} from "./i18n";

afterEach(() => setMainLocale("zh-CN"));

describe("main process i18n", () => {
  it("resolves explicit and system language preferences", () => {
    expect(resolveMainLocale("zh-CN", "en-GB")).toBe("zh-CN");
    expect(resolveMainLocale("en-US", "zh-Hans-CN")).toBe("en-US");
    expect(resolveMainLocale("system", "zh-Hans-CN")).toBe("zh-CN");
    expect(resolveMainLocale("system", "en-GB")).toBe("en-US");
  });

  it("switches native strings immediately and interpolates values", () => {
    setMainLocale("en-US");
    expect(mainTranslate("chooseExistingFolder")).toBe(
      "Use an existing folder",
    );
    expect(mainTranslate("openConversation", { title: "Review" })).toBe(
      "Open conversation · Review",
    );
  });

  it("localizes native workspace item menu labels", () => {
    expect(mainTranslate("openInApplication", { application: "Cursor" }, "zh-CN"))
      .toBe("在 Cursor 中打开");
    expect(mainTranslate("openWith", {}, "en-US")).toBe("Open With");
  });
});
