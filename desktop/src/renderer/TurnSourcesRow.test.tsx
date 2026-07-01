/**
 * Tests for `TurnSourcesRow`. Mirrors the AssistantTurnShell /
 * ToolActivityRow test setup: real React via react-dom/client + act,
 * no @testing-library/react dependency. The component is a pure
 * function of (sources, onOpen), so we exercise:
 *   - empty list returns null
 *   - one button per host, favicon URL scoped per host
 *   - "来源" vs "来源 N" label
 *   - explicit `onOpen` prop is the default click handler
 *   - falling back to `window.wuu.openExternal` when no prop is set
 *   - onError swaps the <img> for a first-letter avatar
 *   - accessible name + tooltip carry title when available
 */
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { TurnSource } from "./ToolActivityHelpers";
import { TurnSourcesRow } from "./TurnSourcesRow";

let mountedRoots: Root[] = [];

afterEach(() => {
  for (const root of mountedRoots) {
    act(() => root.unmount());
  }
  mountedRoots = [];
  vi.restoreAllMocks();
  // Don't leak the stubbed `window.wuu` across cases — the test that
  // needs it re-creates it explicitly so we know when it's set.
  delete (window as { wuu?: unknown }).wuu;
});

// jsdom doesn't implement layout. Stub getBoundingClientRect so React
// doesn't crash on layout queries during render.
beforeAll(() => {
  Element.prototype.getBoundingClientRect = function (): DOMRect {
    return {
      x: 0,
      y: 0,
      top: 0,
      left: 0,
      right: 0,
      bottom: 0,
      width: 0,
      height: 0,
      toJSON() {
        return this;
      },
    } as DOMRect;
  };
});

import { beforeAll } from "vitest";

function mountInto(element: JSX.Element): HTMLElement {
  const container = document.createElement("div");
  document.body.appendChild(container);
  const root = createRoot(container);
  act(() => {
    root.render(element);
  });
  mountedRoots.push(root);
  return container;
}

const sampleSources: TurnSource[] = [
  {
    url: "https://www.anthropic.com/news/claude-opus-4-7",
    host: "anthropic.com",
    title: "Claude Opus 4.7",
    origin: "web_search",
  },
  {
    url: "https://platform.openai.com/docs/models",
    host: "openai.com",
    origin: "web_fetch",
  },
];

describe("TurnSourcesRow", () => {
  it("renders nothing when there are no sources", () => {
    const container = mountInto(<TurnSourcesRow sources={[]} />);
    expect(container.firstChild).toBeNull();
  });

  it("renders one icon button per source with a per-host favicon URL", () => {
    const container = mountInto(<TurnSourcesRow sources={sampleSources} />);
    const buttons = container.querySelectorAll("button.turn-source-icon");
    expect(buttons.length).toBe(2);
    const imgs = Array.from(container.querySelectorAll("img"));
    expect(imgs).toHaveLength(2);
    expect(imgs[0].src).toContain("google.com/s2/favicons");
    expect(imgs[0].src).toContain("domain=anthropic.com");
    expect(imgs[1].src).toContain("domain=openai.com");
  });

  it("labels the pill with the source count when there is more than one", () => {
    const container = mountInto(<TurnSourcesRow sources={sampleSources} />);
    expect(container.querySelector(".turn-sources-label")?.textContent).toBe(
      "来源 2",
    );
  });

  it("collapses to a singular '来源' label for a single source", () => {
    const container = mountInto(
      <TurnSourcesRow
        sources={[
          { url: "https://example.com/x", host: "example.com", origin: "web_search" },
        ]}
      />,
    );
    expect(container.querySelector(".turn-sources-label")?.textContent).toBe(
      "来源",
    );
  });

  it("routes the click through the caller-supplied onOpen when provided", () => {
    const onOpen = vi.fn();
    const container = mountInto(
      <TurnSourcesRow sources={sampleSources} onOpen={onOpen} />,
    );
    const buttons = Array.from(
      container.querySelectorAll<HTMLButtonElement>("button.turn-source-icon"),
    );
    act(() => {
      buttons[1].click();
    });
    expect(onOpen).toHaveBeenCalledTimes(1);
    expect(onOpen).toHaveBeenCalledWith("https://platform.openai.com/docs/models");
  });

  it("falls back to window.wuu.openExternal when no onOpen prop is set", () => {
    const openExternal = vi.fn().mockResolvedValue(undefined);
    (window as unknown as { wuu: { openExternal: typeof openExternal } }).wuu = {
      openExternal,
    };
    const container = mountInto(
      <TurnSourcesRow
        sources={[
          { url: "https://example.com/x", host: "example.com", origin: "web_search" },
        ]}
      />,
    );
    const button = container.querySelector<HTMLButtonElement>(
      "button.turn-source-icon",
    );
    act(() => {
      button?.click();
    });
    expect(openExternal).toHaveBeenCalledWith("https://example.com/x");
  });

  it("falls back to a first-letter avatar when the favicon fails to load", () => {
    const container = mountInto(
      <TurnSourcesRow
        sources={[
          {
            url: "https://www.anthropic.com/news",
            host: "anthropic.com",
            origin: "web_search",
          },
        ]}
      />,
    );
    const img = container.querySelector("img");
    expect(img).not.toBeNull();
    act(() => {
      img?.dispatchEvent(new Event("error"));
    });
    const fallback = container.querySelector(".turn-source-fallback");
    expect(fallback?.textContent).toBe("A");
    // Once failed, the original <img> is gone.
    expect(container.querySelector("img")).toBeNull();
  });

  it("exposes the title + host in aria-label and tooltip when title is present", () => {
    const container = mountInto(
      <TurnSourcesRow
        sources={[
          {
            url: "https://www.anthropic.com/news",
            host: "anthropic.com",
            title: "Claude Opus 4.7",
            origin: "web_search",
          },
        ]}
      />,
    );
    const button = container.querySelector("button.turn-source-icon");
    expect(button?.getAttribute("aria-label")).toBe(
      "打开 Claude Opus 4.7 · anthropic.com",
    );
    expect(button?.getAttribute("title")).toBe("Claude Opus 4.7 · anthropic.com");
  });

  it("shows only the host in aria-label when no title is available", () => {
    const container = mountInto(
      <TurnSourcesRow
        sources={[
          {
            url: "https://openai.com/c",
            host: "openai.com",
            origin: "web_fetch",
          },
        ]}
      />,
    );
    const button = container.querySelector("button.turn-source-icon");
    expect(button?.getAttribute("aria-label")).toBe("打开 openai.com");
    expect(button?.getAttribute("title")).toBe("openai.com");
  });

  it("does not leak the origin field as user-facing text", () => {
    // origin is implementation metadata for collectTurnSources —
    // never expose "web_search" / "web_fetch" strings to the user.
    const container = mountInto(<TurnSourcesRow sources={sampleSources} />);
    expect(container.textContent).not.toMatch(/web_search/);
    expect(container.textContent).not.toMatch(/web_fetch/);
  });
});
