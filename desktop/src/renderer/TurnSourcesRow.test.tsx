/**
 * Tests for `TurnSourcesRow`. Mirrors the AssistantTurnShell /
 * ToolActivityRow test setup: real React via react-dom/client + act,
 * no @testing-library/react dependency. The component is a pure
 * function of (sources, onOpen), so we exercise:
 *   - empty list returns null
 *   - one button per host, favicon URL scoped per host
 *   - "来源" vs "来源 N" label
 *   - single source → entire pill is a button; click anywhere opens the URL
 *   - multi source → each icon is its own button with the host URL
 *   - more than VISIBLE_SOURCE_LIMIT hosts → "+N" overflow badge
 *     with the rest of the URLs behind it
 *   - explicit `onOpen` prop is the default click handler
 *   - falling back to `window.wuu.openExternal` when no prop is set
 *   - onError swaps the favicon for a first-letter avatar
 *   - accessible name + tooltip carry the full URL (not just host)
 */
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { TurnSource } from "./ToolActivityHelpers";
import { TurnSourcesRow } from "./TurnSourcesRow";
import { hoverTooltipText, unhoverTooltip } from "./tooltipTestUtils";

let mountedRoots: Root[] = [];

afterEach(() => {
  unhoverTooltip();
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

  it("shows a loading skeleton until the favicon has loaded", () => {
    const container = mountInto(<TurnSourcesRow sources={sampleSources} />);
    const avatar = container.querySelector<HTMLElement>(".turn-source-avatar");
    const img = avatar?.querySelector("img");

    expect(avatar?.dataset.loadState).toBe("loading");
    act(() => {
      img?.dispatchEvent(new Event("load"));
    });
    expect(avatar?.dataset.loadState).toBe("loaded");
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

  it("renders the whole single-source pill as a button so clicking anywhere opens the URL", async () => {
    // Single source case: the icon and the label both belong to the
    // same <button>, so a hover-anywhere / click-anywhere affordance
    // is what makes "来源" actually behave like a source link rather
    // than just a passive count.
    const onOpen = vi.fn();
    const container = mountInto(
      <TurnSourcesRow
        sources={[
          {
            url: "https://example.com/article",
            host: "example.com",
            title: "Example article",
            origin: "web_search",
          },
        ]}
        onOpen={onOpen}
      />,
    );
    const pillButton = container.querySelector<HTMLButtonElement>(
      "button.turn-sources-pill.turn-sources-pill-single",
    );
    expect(pillButton).not.toBeNull();
    const iconFrame = pillButton?.querySelector(".turn-source-icon-frame");
    expect(iconFrame).not.toBeNull();
    expect(iconFrame?.querySelector(".turn-source-avatar img")).not.toBeNull();
    // The inert frame gives the favicon the same chrome as the multi-source
    // stack without nesting an invalid button inside the pill button.
    expect(container.querySelector("button.turn-source-icon")).toBeNull();
    expect(pillButton?.getAttribute("aria-label")).toBe(
      "打开 Example article — https://example.com/article",
    );
    expect(pillButton?.getAttribute("title")).toBeNull();
    expect(await hoverTooltipText(pillButton)).toBe(
      "Example article — https://example.com/article",
    );
    act(() => {
      pillButton?.click();
    });
    expect(onOpen).toHaveBeenCalledWith("https://example.com/article");
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

  it("falls back to window.wuu.openExternal from a single-source pill click", () => {
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
    const pillButton = container.querySelector<HTMLButtonElement>(
      "button.turn-sources-pill",
    );
    act(() => {
      pillButton?.click();
    });
    expect(openExternal).toHaveBeenCalledWith("https://example.com/x");
  });

  it("falls back to window.wuu.openExternal from an icon click when no onOpen prop is set", () => {
    const openExternal = vi.fn().mockResolvedValue(undefined);
    (window as unknown as { wuu: { openExternal: typeof openExternal } }).wuu = {
      openExternal,
    };
    const container = mountInto(<TurnSourcesRow sources={sampleSources} />);
    const button = container.querySelector<HTMLButtonElement>(
      "button.turn-source-icon",
    );
    act(() => {
      button?.click();
    });
    expect(openExternal).toHaveBeenCalledWith(
      "https://www.anthropic.com/news/claude-opus-4-7",
    );
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
    expect(
      container.querySelector<HTMLElement>(".turn-source-avatar")?.dataset
        .loadState,
    ).toBe("failed");
    const fallback = container.querySelector(".turn-source-fallback");
    expect(fallback?.textContent).toBe("A");
    // Once failed, the original <img> is gone.
    expect(container.querySelector("img")).toBeNull();
  });

  it("exposes the full URL plus title in aria-label and tooltip when title is present", async () => {
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
    // Single source → the pill itself carries the accessible name.
    const pill = container.querySelector("button.turn-sources-pill");
    expect(pill?.getAttribute("aria-label")).toBe(
      "打开 Claude Opus 4.7 — https://www.anthropic.com/news",
    );
    expect(await hoverTooltipText(pill)).toBe(
      "Claude Opus 4.7 — https://www.anthropic.com/news",
    );
  });

  it("exposes only the URL in aria-label when no title is available", async () => {
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
    const pill = container.querySelector("button.turn-sources-pill");
    expect(pill?.getAttribute("aria-label")).toBe("打开 https://openai.com/c");
    expect(await hoverTooltipText(pill)).toBe("https://openai.com/c");
  });

  it("does not leak the origin field as user-facing text", () => {
    // origin is implementation metadata for collectTurnSources —
    // never expose "web_search" / "web_fetch" strings to the user.
    const container = mountInto(<TurnSourcesRow sources={sampleSources} />);
    expect(container.textContent).not.toMatch(/web_search/);
    expect(container.textContent).not.toMatch(/web_fetch/);
  });

  describe("overflow badge", () => {
    function makeManySources(n: number): TurnSource[] {
      // Distinct hosts across the public web so each becomes its own
      // dedup slot. The host list doesn't need to resolve — the test
      // only cares about how many icons are visible vs hidden.
      const tlds = ["com", "io", "dev", "co", "ai", "app"];
      return Array.from({ length: n }, (_, index) => {
        const tld = tlds[index % tlds.length];
        const slug = `site-${index}`;
        return {
          url: `https://${slug}.example.${tld}/page-${index}`,
          host: `${slug}.example.${tld}`,
          title: `Page ${index}`,
          origin: "web_search",
        } as TurnSource;
      });
    }

    it("renders an overflow badge when there are more than the visible limit", () => {
      const sources = makeManySources(10);
      const container = mountInto(<TurnSourcesRow sources={sources} />);
      // Six host icons are visible; the rest hide behind a "+4" badge.
      const icons = container.querySelectorAll("button.turn-source-icon");
      const realIcons = container.querySelectorAll(
        "button.turn-source-icon:not(.turn-source-overflow-badge)",
      );
      expect(realIcons.length).toBe(6);
      const badge = container.querySelector(
        "button.turn-source-overflow-badge",
      );
      expect(badge).not.toBeNull();
      expect(badge?.textContent).toBe("+4");
      expect(badge?.getAttribute("aria-label")).toBe("查看另外 4 个来源");
      expect(badge?.getAttribute("aria-expanded")).toBe("false");
      expect(badge?.getAttribute("aria-haspopup")).toBe("menu");
      // The pill label still reflects the total so the user knows the
      // full count even when the icon stack is capped.
      expect(container.querySelector(".turn-sources-label")?.textContent).toBe(
        "来源 10",
      );
      // Sanity: 6 real icons + 1 badge = 7 turn-source-icon buttons.
      expect(icons.length).toBe(7);
    });

    it("does not render an overflow badge when sources fit in the visible limit", () => {
      const sources = makeManySources(6);
      const container = mountInto(<TurnSourcesRow sources={sources} />);
      expect(
        container.querySelector("button.turn-source-overflow-badge"),
      ).toBeNull();
      // Label still says "来源 6" — the cap is a layout concern, not a
      // truncation of the displayed total.
      expect(container.querySelector(".turn-sources-label")?.textContent).toBe(
        "来源 6",
      );
    });

    it("opens a popover listing the overflow sources when the badge is clicked", async () => {
      const sources = makeManySources(8);
      const container = mountInto(<TurnSourcesRow sources={sources} />);
      const badge = container.querySelector<HTMLButtonElement>(
        "button.turn-source-overflow-badge",
      );
      expect(badge?.getAttribute("aria-expanded")).toBe("false");
      act(() => {
        badge?.click();
      });
      expect(badge?.getAttribute("aria-expanded")).toBe("true");
      const menu = container.querySelector(
        "ul.turn-source-overflow-list[role=\"menu\"]",
      );
      expect(menu).not.toBeNull();
      // Only the 2 overflow sources land in the menu — the visible 6
      // icons are already in the pill itself, no need to repeat them.
      const items = menu?.querySelectorAll(
        "button.turn-source-overflow-item",
      );
      expect(items?.length).toBe(2);
      // TLD cycle in makeManySources: index 6 picks tlds[6 % 6] = "com",
      // index 7 picks tlds[7 % 6] = "io". The test only needs to prove
      // the menu shows the right URLs and titles in first-seen order;
      // the TLD itself is incidental.
      expect(items?.[0].getAttribute("aria-label")).toBe(
        "打开 Page 6 — https://site-6.example.com/page-6",
      );
      expect(await hoverTooltipText(items?.[0] ?? null)).toBe(
        "Page 6 — https://site-6.example.com/page-6",
      );
      expect(items?.[1].getAttribute("aria-label")).toBe(
        "打开 Page 7 — https://site-7.example.io/page-7",
      );
      // Host appears as a secondary line so users see which domain
      // the entry belongs to without parsing the URL.
      expect(items?.[0].textContent).toContain("site-6.example.com");
    });

    it("routes an overflow item click through the caller-supplied onOpen and closes the popover", () => {
      const onOpen = vi.fn();
      const sources = makeManySources(7);
      const container = mountInto(
        <TurnSourcesRow sources={sources} onOpen={onOpen} />,
      );
      const badge = container.querySelector<HTMLButtonElement>(
        "button.turn-source-overflow-badge",
      );
      act(() => {
        badge?.click();
      });
      const overflowItem = container.querySelector<HTMLButtonElement>(
        "button.turn-source-overflow-item",
      );
      act(() => {
        overflowItem?.click();
      });
      expect(onOpen).toHaveBeenCalledTimes(1);
      expect(onOpen).toHaveBeenCalledWith(
        "https://site-6.example.com/page-6",
      );
      // Popover closed itself after the click — leaving it open would
      // hide the user's view of the conversation they came from.
      expect(
        container.querySelector("ul.turn-source-overflow-list"),
      ).toBeNull();
      expect(badge?.getAttribute("aria-expanded")).toBe("false");
    });

    it("falls back to window.wuu.openExternal for an overflow item when no onOpen is set", () => {
      const openExternal = vi.fn().mockResolvedValue(undefined);
      (
        window as unknown as { wuu: { openExternal: typeof openExternal } }
      ).wuu = { openExternal };
      const sources = makeManySources(7);
      const container = mountInto(<TurnSourcesRow sources={sources} />);
      act(() => {
        container
          .querySelector<HTMLButtonElement>(
            "button.turn-source-overflow-badge",
          )
          ?.click();
      });
      act(() => {
        container
          .querySelector<HTMLButtonElement>(
            "button.turn-source-overflow-item",
          )
          ?.click();
      });
      expect(openExternal).toHaveBeenCalledWith(
        "https://site-6.example.com/page-6",
      );
    });

    it("closes the popover when the user clicks outside", () => {
      const sources = makeManySources(7);
      const container = mountInto(<TurnSourcesRow sources={sources} />);
      act(() => {
        container
          .querySelector<HTMLButtonElement>(
            "button.turn-source-overflow-badge",
          )
          ?.click();
      });
      expect(
        container.querySelector("ul.turn-source-overflow-list"),
      ).not.toBeNull();
      // mousedown on something outside the overflow wrapper — document
      // body is the natural "outside" target in jsdom.
      act(() => {
        document.body.dispatchEvent(new MouseEvent("mousedown", { bubbles: true }));
      });
      expect(
        container.querySelector("ul.turn-source-overflow-list"),
      ).toBeNull();
    });

    it("closes the popover when the user presses Escape", () => {
      const sources = makeManySources(7);
      const container = mountInto(<TurnSourcesRow sources={sources} />);
      act(() => {
        container
          .querySelector<HTMLButtonElement>(
            "button.turn-source-overflow-badge",
          )
          ?.click();
      });
      expect(
        container.querySelector("ul.turn-source-overflow-list"),
      ).not.toBeNull();
      act(() => {
        document.dispatchEvent(
          new KeyboardEvent("keydown", { key: "Escape", bubbles: true }),
        );
      });
      expect(
        container.querySelector("ul.turn-source-overflow-list"),
      ).toBeNull();
    });
  });
});
