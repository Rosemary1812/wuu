import { act, createElement, type ReactNode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { useConversationScrollState } from "./ConversationScrollState";
import type { Turn } from "../shared/protocol";

function makeLongTurns(): Turn[] {
  return [
    {
      id: "turn-1",
      items: [],
      items_view: "full",
      status: "completed",
    },
  ];
}

type StubbedLayout = {
  scrollHeight: number;
  clientHeight: number;
  scrollTop: number;
};

function stubLayout(node: HTMLElement, opts: Partial<StubbedLayout>): StubbedLayout {
  const layout = {
    scrollHeight: opts.scrollHeight ?? 1000,
    clientHeight: opts.clientHeight ?? 600,
    scrollTop: opts.scrollTop ?? 0,
  };
  Object.defineProperty(node, "scrollHeight", {
    configurable: true,
    get: () => layout.scrollHeight,
  });
  Object.defineProperty(node, "clientHeight", {
    configurable: true,
    get: () => layout.clientHeight,
  });
  Object.defineProperty(node, "scrollTop", {
    configurable: true,
    get: () => layout.scrollTop,
    set: (v: number) => {
      const max = Math.max(0, layout.scrollHeight - layout.clientHeight);
      layout.scrollTop = Math.max(0, Math.min(v, max));
    },
  });
  return layout;
}

function Probe({ activeThreadID }: { activeThreadID?: string }): ReactNode {
  const h = useConversationScrollState({
    activeThreadID,
    activePane: "primary",
    splitConversation: false,
    primaryTurns: makeLongTurns(),
    secondaryTurns: undefined,
    emptyConversation: false,
    previewingLaunch: false,
    initialized: true,
  });
  return createElement("div", {
    ref: (node: HTMLDivElement | null) => {
      h.conversationScrollRef.current = node;
    },
    onScroll: () => h.handleConversationScroll(),
    "data-testid": "scroll-container",
  }, createElement("div", {
    ref: (node: HTMLDivElement | null) => {
      h.scrollContentRef.current = node;
    },
    "data-testid": "scroll-content",
  }));
}

describe("useConversationScrollState — thread scroll snapshots", () => {
  let container: HTMLDivElement;
  let root: Root | null = null;
  let scrollNode: HTMLDivElement | null = null;
  let layout: StubbedLayout | null = null;

  beforeEach(() => {
    container = document.createElement("div");
    document.body.appendChild(container);
  });

  afterEach(() => {
    act(() => {
      root?.unmount();
    });
    root = null;
    scrollNode = null;
    layout = null;
    document.body.removeChild(container);
  });

  function mount(opts: {
    scrollHeight: number;
    clientHeight: number;
    initialScrollTop: number;
    activeThreadID?: string;
  }): HTMLDivElement {
    act(() => {
      root = createRoot(container);
      root.render(
        createElement(Probe, {
          activeThreadID: opts.activeThreadID ?? "thread-1",
        }),
      );
    });

    const node = container.querySelector(
      "[data-testid='scroll-container']",
    ) as HTMLDivElement | null;
    if (!node) throw new Error("Probe did not render");
    scrollNode = node;
    // Install layout stubs after React has finished its mount-time
    // useLayoutEffect, which has already attempted to scroll the
    // unstubbed element. Subsequent scroll events (and our manual
    // dispatchEvent) will read the stubbed values.
    layout = stubLayout(node, {
      scrollHeight: opts.scrollHeight,
      clientHeight: opts.clientHeight,
      scrollTop: opts.initialScrollTop,
    });
    return node;
  }

  function switchThread(activeThreadID?: string): void {
    if (!root) throw new Error("not mounted");
    act(() => {
      root!.render(createElement(Probe, { activeThreadID }));
    });
  }

  function fireScroll(): void {
    if (!scrollNode) throw new Error("not mounted");
    act(() => {
      scrollNode!.dispatchEvent(new Event("scroll", { bubbles: false }));
    });
  }

  function fireUserScroll(): void {
    if (!scrollNode) throw new Error("not mounted");
    act(() => {
      scrollNode!.dispatchEvent(new WheelEvent("wheel", { bubbles: true, deltaY: -80 }));
      scrollNode!.dispatchEvent(new Event("scroll", { bubbles: false }));
    });
  }

  function setScrollTop(top: number): void {
    if (!layout) throw new Error("not mounted");
    layout.scrollTop = top;
  }

  it("snaps a shorter thread to its bottom when switching sessions", () => {
    mount({
      activeThreadID: "thread-long",
      scrollHeight: 2400,
      clientHeight: 600,
      initialScrollTop: 2400 - 600,
    });
    fireScroll();

    if (!layout) throw new Error("not mounted");
    layout.scrollHeight = 900;
    switchThread("thread-short");
    expect(layout.scrollTop).toBe(300);

    fireScroll();
  });

  it("restores a thread's saved away-from-bottom position when switching back", () => {
    mount({
      activeThreadID: "thread-a",
      scrollHeight: 2400,
      clientHeight: 600,
      initialScrollTop: 2400 - 600,
    });
    fireScroll();

    setScrollTop(520);
    fireUserScroll();

    if (!layout) throw new Error("not mounted");
    layout.scrollHeight = 1200;
    switchThread("thread-b");
    expect(layout.scrollTop).toBe(600);
    fireScroll();

    layout.scrollHeight = 2400;
    switchThread("thread-a");
    expect(layout.scrollTop).toBe(520);
    fireScroll();
  });

  it("keeps a thread's scroll snapshot while a non-conversation tab is active", () => {
    mount({
      activeThreadID: "thread-a",
      scrollHeight: 2200,
      clientHeight: 600,
      initialScrollTop: 2200 - 600,
    });
    fireScroll();

    setScrollTop(480);
    fireUserScroll();

    switchThread(undefined);
    if (!layout) throw new Error("not mounted");
    layout.scrollTop = 0;
    fireScroll();

    switchThread("thread-a");
    expect(layout.scrollTop).toBe(480);
    fireScroll();
  });
});

type MockResizeObserverRecord = {
  callback: ResizeObserverCallback;
  observed: Set<Element>;
};

describe("useConversationScrollState — dock composer height", () => {
  let container: HTMLDivElement;
  let root: Root | null = null;
  let resizeObserverGlobal: typeof globalThis & {
    ResizeObserver?: typeof ResizeObserver;
  };
  let realResizeObserver: typeof ResizeObserver | undefined;
  let resizeObservers: MockResizeObserverRecord[];

  beforeEach(() => {
    container = document.createElement("div");
    document.body.appendChild(container);
    resizeObserverGlobal = globalThis as typeof globalThis & {
      ResizeObserver?: typeof ResizeObserver;
    };
    realResizeObserver = resizeObserverGlobal.ResizeObserver;
    resizeObservers = [];

    class MockResizeObserver {
      private readonly record: MockResizeObserverRecord;

      constructor(callback: ResizeObserverCallback) {
        this.record = {
          callback,
          observed: new Set<Element>(),
        };
        resizeObservers.push(this.record);
      }

      observe(target: Element): void {
        this.record.observed.add(target);
      }

      unobserve(target: Element): void {
        this.record.observed.delete(target);
      }

      disconnect(): void {
        this.record.observed.clear();
      }
    }

    resizeObserverGlobal.ResizeObserver =
      MockResizeObserver as typeof ResizeObserver;
  });

  afterEach(() => {
    act(() => {
      root?.unmount();
    });
    root = null;
    if (realResizeObserver) {
      resizeObserverGlobal.ResizeObserver = realResizeObserver;
    } else {
      Reflect.deleteProperty(resizeObserverGlobal, "ResizeObserver");
    }
    document.body.removeChild(container);
  });

  function DockComposerProbe(): ReactNode {
    const h = useConversationScrollState({
      activeThreadID: "thread-1",
      activePane: "primary",
      splitConversation: false,
      primaryTurns: makeLongTurns(),
      secondaryTurns: undefined,
      emptyConversation: false,
      previewingLaunch: false,
      initialized: true,
    });
    return createElement(
      "section",
      {
        ref: (node: HTMLElement | null) => {
          h.conversationPaneRef.current = node;
        },
        "data-testid": "conversation-pane",
      },
      createElement("div", {
        ref: (node: HTMLDivElement | null) => {
          h.conversationScrollRef.current = node;
        },
        "data-testid": "scroll-container",
      }),
      createElement(
        "footer",
        {
          ref: h.dockComposerRef,
          "data-testid": "dock-composer",
        },
        createElement("div", {
          className: "composer-frame",
          "data-testid": "composer-frame",
        }),
      ),
    );
  }

  function mountDockComposerProbe(): {
    pane: HTMLElement;
    dockComposer: HTMLElement;
    frame: HTMLElement;
  } {
    act(() => {
      root = createRoot(container);
      root.render(createElement(DockComposerProbe));
    });

    const pane = container.querySelector<HTMLElement>(
      "[data-testid='conversation-pane']",
    );
    const dockComposer = container.querySelector<HTMLElement>(
      "[data-testid='dock-composer']",
    );
    const frame = container.querySelector<HTMLElement>(
      "[data-testid='composer-frame']",
    );
    if (!pane || !dockComposer || !frame) {
      throw new Error("DockComposerProbe did not render");
    }
    return { pane, dockComposer, frame };
  }

  function stubRectHeight(node: HTMLElement, height: number): void {
    node.getBoundingClientRect = () =>
      ({
        x: 0,
        y: 0,
        top: 0,
        right: 0,
        bottom: height,
        left: 0,
        width: 800,
        height,
        toJSON: () => ({}),
      }) as DOMRect;
  }

  function flushResizeObserversFor(target: Element): void {
    act(() => {
      for (const observer of resizeObservers) {
        if (observer.observed.has(target)) {
          observer.callback([], observer as unknown as ResizeObserver);
        }
      }
    });
  }

  it("includes the expanded composer offset in the dock composer height token", () => {
    const { pane, dockComposer, frame } = mountDockComposerProbe();
    stubRectHeight(dockComposer, 168);

    flushResizeObserversFor(dockComposer);
    expect(pane.style.getPropertyValue("--dock-composer-height")).toBe("168px");

    frame.style.setProperty("--composer-expanded-offset", "284px");
    flushResizeObserversFor(frame);

    expect(pane.style.getPropertyValue("--dock-composer-height")).toBe("452px");
  });
});
