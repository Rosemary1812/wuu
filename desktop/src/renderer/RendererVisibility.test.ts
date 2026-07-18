import { afterEach, describe, expect, it, vi } from "vitest";
import {
  startRendererVisibilitySync,
  syncRendererVisibility,
} from "./RendererVisibility";

afterEach(() => {
  document.documentElement.removeAttribute("data-renderer-hidden");
  vi.restoreAllMocks();
});

describe("renderer visibility", () => {
  it("stamps hidden state without changing visible documents", () => {
    syncRendererVisibility(document.documentElement, "hidden");
    expect(document.documentElement.hasAttribute("data-renderer-hidden")).toBe(true);

    syncRendererVisibility(document.documentElement, "visible");
    expect(document.documentElement.hasAttribute("data-renderer-hidden")).toBe(false);
  });

  it("tracks document visibility changes for the renderer lifetime", () => {
    const visibilityState = vi
      .spyOn(document, "visibilityState", "get")
      .mockReturnValue("hidden");
    const stop = startRendererVisibilitySync();
    expect(document.documentElement.hasAttribute("data-renderer-hidden")).toBe(true);

    visibilityState.mockReturnValue("visible");
    document.dispatchEvent(new Event("visibilitychange"));
    expect(document.documentElement.hasAttribute("data-renderer-hidden")).toBe(false);

    stop();
  });
});
