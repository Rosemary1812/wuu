import { afterEach, describe, expect, it, vi } from "vitest";
import type { Rectangle } from "electron";
import type { ActivitySession } from "../shared/protocol";
import type { BrowserInteractionHint, BrowserTabFrame } from "./browserHostWindows";
import {
  BrowserFramePump,
  BrowserPiPSurface,
  browserPiPWindowHTML,
  pipContainRect,
  pipFrameHash,
  pipHostname,
  pipMapPoint,
  type BrowserFrameSource,
  type BrowserPiPWindowHandle,
} from "./browserPiPWindow";
import type { ObservationPiPEventSink } from "./cuaActivityWindows";

afterEach(() => {
  vi.useRealTimers();
});

function png(label: string): Buffer {
  return Buffer.from(`png:${label}`);
}

function frame(label: string): BrowserTabFrame {
  return { png: png(label), width: 1280, height: 800 };
}

describe("browser PiP pure helpers", () => {
  it("hashes frames for change detection", () => {
    expect(pipFrameHash(png("a"))).toBe(pipFrameHash(png("a")));
    expect(pipFrameHash(png("a"))).not.toBe(pipFrameHash(png("b")));
  });

  it("extracts the host for the surface chrome", () => {
    expect(pipHostname("https://docs.example.com/path?q=1")).toBe("docs.example.com");
    expect(pipHostname("http://localhost:3000/app")).toBe("localhost:3000");
    expect(pipHostname("about:blank")).toBe("about:blank");
  });

  it("maps page points through object-fit contain letterboxing", () => {
    // Page 1000x500 into a 260x170 box: width-limited, vertical letterbox.
    const rect = pipContainRect(1000, 500, 260, 170);
    expect(rect.scale).toBeCloseTo(0.26);
    expect(rect.y).toBeCloseTo((170 - 500 * 0.26) / 2);
    const point = pipMapPoint(500, 250, 1000, 500, 260, 170);
    expect(point.x).toBeCloseTo(130);
    expect(point.y).toBeCloseTo(rect.y + 250 * 0.26);
    expect(pipContainRect(0, 500, 260, 170).scale).toBe(0);
  });
});

describe("BrowserFramePump", () => {
  function makePump(overrides: {
    capture?: () => Promise<BrowserTabFrame | undefined>;
    meta?: () => { url: string; title: string } | undefined;
    onFrame?: (frame: BrowserTabFrame & { url: string; title: string }) => void;
    onGone?: () => void;
    onFirstFrame?: () => void;
  }) {
    const frames: Array<BrowserTabFrame & { url: string; title: string }> = [];
    const gone = vi.fn(overrides.onGone ?? (() => undefined));
    const first = vi.fn(overrides.onFirstFrame ?? (() => undefined));
    const capture = vi.fn(overrides.capture ?? (async () => frame("a")));
    const meta = vi.fn(overrides.meta ?? (() => ({ url: "https://a.test/", title: "A" })));
    const pump = new BrowserFramePump(
      { capture, meta },
      {
        onFrame: (f) => {
          frames.push(f);
          overrides.onFrame?.(f);
        },
        onGone: gone,
        onFirstFrame: first,
      },
      100,
    );
    return { pump, frames, gone, first, capture, meta };
  }

  it("captures at the cadence and dedupes identical frames", async () => {
    vi.useFakeTimers();
    const { pump, frames, first, capture } = makePump({});
    pump.start();
    await vi.advanceTimersByTimeAsync(0);
    expect(frames).toHaveLength(1);
    expect(first).toHaveBeenCalledTimes(1);

    // Same pixels + same meta: ticks keep firing but nothing is re-sent.
    await vi.advanceTimersByTimeAsync(300);
    expect(frames).toHaveLength(1);
    expect(capture.mock.calls.length).toBeGreaterThan(1);
    pump.stop();
  });

  it("re-sends when only the page identity changed", async () => {
    vi.useFakeTimers();
    let metaValue = { url: "https://a.test/", title: "A" };
    const { pump, frames } = makePump({ meta: () => metaValue });
    pump.start();
    await vi.advanceTimersByTimeAsync(0);
    metaValue = { url: "https://b.test/", title: "B" };
    await vi.advanceTimersByTimeAsync(100);
    expect(frames.map((f) => f.url)).toEqual(["https://a.test/", "https://b.test/"]);
    pump.stop();
  });

  it("keeps a single capture in flight under slow captures", async () => {
    vi.useFakeTimers();
    let resolveCapture: ((frame: BrowserTabFrame) => void) | undefined;
    const { pump, capture } = makePump({
      capture: () =>
        new Promise<BrowserTabFrame>((resolve) => {
          resolveCapture = resolve;
        }),
    });
    pump.start();
    await vi.advanceTimersByTimeAsync(0);
    await vi.advanceTimersByTimeAsync(500);
    expect(capture).toHaveBeenCalledTimes(1);
    resolveCapture?.(frame("slow"));
    await vi.advanceTimersByTimeAsync(0);
    await vi.advanceTimersByTimeAsync(100);
    expect(capture).toHaveBeenCalledTimes(2);
    pump.stop();
  });

  it("backs off after transient capture failures and recovers", async () => {
    vi.useFakeTimers();
    let failures = 1;
    const { pump, frames, capture } = makePump({
      capture: async () => {
        if (failures > 0) {
          failures -= 1;
          throw new Error("capture failed");
        }
        return frame("ok");
      },
    });
    pump.start();
    await vi.advanceTimersByTimeAsync(0);
    expect(capture).toHaveBeenCalledTimes(1);
    // First failure backs off 2s; a tick before that must not happen.
    await vi.advanceTimersByTimeAsync(1000);
    expect(capture).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(1000);
    expect(capture).toHaveBeenCalledTimes(2);
    expect(frames).toHaveLength(1);
    pump.stop();
  });

  it("reports a gone tab exactly once and stops", async () => {
    vi.useFakeTimers();
    const { pump, gone, capture } = makePump({ capture: async () => undefined });
    pump.start();
    await vi.advanceTimersByTimeAsync(0);
    await vi.advanceTimersByTimeAsync(1000);
    expect(gone).toHaveBeenCalledTimes(1);
    expect(capture).toHaveBeenCalledTimes(1);
    pump.stop();
  });

  it("pauses without capturing and resumes immediately", async () => {
    vi.useFakeTimers();
    const { pump, frames, capture } = makePump({});
    pump.start();
    await vi.advanceTimersByTimeAsync(0);
    pump.pause();
    const callsAtPause = capture.mock.calls.length;
    await vi.advanceTimersByTimeAsync(500);
    expect(capture).toHaveBeenCalledTimes(callsAtPause);
    pump.start();
    await vi.advanceTimersByTimeAsync(0);
    expect(frames.length).toBeGreaterThan(0);
    pump.stop();
  });
});

// ---------------------------------------------------------------------------
// Surface lifecycle.
// ---------------------------------------------------------------------------

class FakePiPWindow implements BrowserPiPWindowHandle {
  destroyed = false;
  visible = false;
  executed: string[] = [];
  loadedURL = "";
  bounds: Rectangle;
  private listeners = new Map<string, Array<(...args: unknown[]) => void>>();
  private wcListeners = new Map<string, Array<(...args: unknown[]) => void>>();

  constructor(bounds: Rectangle) {
    this.bounds = { ...bounds };
  }

  readonly webContents = {
    executeJavaScript: async (code: string): Promise<unknown> => {
      this.executed.push(code);
      return undefined;
    },
    setWindowOpenHandler: () => undefined,
    on: (event: string, listener: (...args: unknown[]) => void): void => {
      const list = this.wcListeners.get(event) ?? [];
      list.push(listener);
      this.wcListeners.set(event, list);
    },
  };

  setAlwaysOnTop(): void {}
  setVisibleOnAllWorkspaces(): void {}
  showInactive(): void {
    this.visible = true;
  }
  hide(): void {
    this.visible = false;
  }
  isDestroyed(): boolean {
    return this.destroyed;
  }
  isVisible(): boolean {
    return this.visible;
  }
  getBounds(): Rectangle {
    return this.bounds;
  }
  on(event: string, listener: (...args: unknown[]) => void): void {
    const list = this.listeners.get(event) ?? [];
    list.push(listener);
    this.listeners.set(event, list);
  }
  async loadURL(url: string): Promise<void> {
    this.loadedURL = url;
  }
  destroy(): void {
    this.destroyed = true;
    this.emit("closed");
  }

  emit(event: string, ...args: unknown[]): void {
    for (const listener of this.listeners.get(event) ?? []) listener(...args);
  }
  emitWebContents(event: string, ...args: unknown[]): void {
    for (const listener of this.wcListeners.get(event) ?? []) listener(...args);
  }
  finishLoad(): void {
    this.emitWebContents("did-finish-load");
  }
}

type FakeSource = BrowserFrameSource & {
  frames: Array<BrowserTabFrame | undefined>;
  emitClosed(): void;
  emitHint(hint: BrowserInteractionHint): void;
  captureCalls: number;
};

function makeSource(): FakeSource {
  const source: FakeSource = {
    frames: [],
    captureCalls: 0,
    capture: async () => {
      source.captureCalls += 1;
      return source.frames.length > 0 ? source.frames.shift() : frame("live");
    },
    meta: () => ({ url: "https://page.test/", title: "Page" }),
    onClosed: (listener) => {
      closedListeners.push(listener);
      return () => undefined;
    },
    onInteraction: (listener) => {
      hintListeners.push(listener);
      return () => {
        const index = hintListeners.indexOf(listener);
        if (index >= 0) hintListeners.splice(index, 1);
      };
    },
    emitClosed: () => {
      for (const listener of closedListeners) listener();
    },
    emitHint: (hint) => {
      for (const listener of [...hintListeners]) listener(hint);
    },
  };
  const closedListeners: Array<() => void> = [];
  const hintListeners: Array<(hint: BrowserInteractionHint) => void> = [];
  return source;
}

function browserActivity(overrides: Partial<ActivitySession> = {}): ActivitySession {
  return {
    id: "activity-1",
    kind: "browser",
    thread_id: "thread-1",
    workdir: "/repo",
    plugin_id: "embedded-browser",
    target: "tab-1",
    state: "background_controlled",
    controller: "agent",
    created_at: "2026-07-17T10:00:00Z",
    updated_at: "2026-07-17T10:00:01Z",
    ...overrides,
  };
}

function makeSurface(source: FakeSource): {
  surface: BrowserPiPSurface;
  sink: ObservationPiPEventSink & { onEvent: ReturnType<typeof vi.fn> };
  windows: FakePiPWindow[];
} {
  const windows: FakePiPWindow[] = [];
  const sink = {
    onEvent: vi.fn(),
    onFailure: vi.fn(),
    onGone: vi.fn(),
  };
  const surface = new BrowserPiPSurface({
    activity: browserActivity(),
    bounds: { x: 100, y: 100, width: 260, height: 170 },
    sink,
    source,
    isPackaged: false,
    createWindow: (bounds) => {
      const win = new FakePiPWindow(bounds);
      windows.push(win);
      return win;
    },
  });
  return { surface, sink: sink as ObservationPiPEventSink & { onEvent: ReturnType<typeof vi.fn> }, windows };
}

describe("BrowserPiPSurface", () => {
  it("presents frames only after the page loaded, then replays the queued frame", async () => {
    vi.useFakeTimers();
    const source = makeSource();
    const { surface, sink, windows } = makeSurface(source);
    surface.start();
    surface.setVisible(true);
    await vi.advanceTimersByTimeAsync(400);
    const win = windows[0];
    // Not loaded yet: frames were captured but nothing reached the page.
    expect(win.executed).toHaveLength(0);
    expect(sink.onEvent).toHaveBeenCalledWith({ event: "ready" });

    win.finishLoad();
    await vi.advanceTimersByTimeAsync(0);
    expect(win.executed.some((code) => code.includes("wuuPipFrame"))).toBe(true);
    expect(win.executed.some((code) => code.includes("wuuPipState"))).toBe(true);
    surface.stop();
  });

  it("pauses the pump while frozen or hidden and resumes on live+visible", async () => {
    vi.useFakeTimers();
    const source = makeSource();
    const { surface } = makeSurface(source);
    surface.start();
    surface.setVisible(true);
    await vi.advanceTimersByTimeAsync(0);
    expect(source.captureCalls).toBeGreaterThan(0);

    surface.setLive(false);
    const callsAtFreeze = source.captureCalls;
    await vi.advanceTimersByTimeAsync(1000);
    expect(source.captureCalls).toBe(callsAtFreeze);

    surface.setLive(true);
    await vi.advanceTimersByTimeAsync(400);
    expect(source.captureCalls).toBeGreaterThan(callsAtFreeze);

    surface.setVisible(false);
    const callsAtHide = source.captureCalls;
    await vi.advanceTimersByTimeAsync(1000);
    expect(source.captureCalls).toBe(callsAtHide);
    surface.stop();
  });

  it("forwards page interaction hints with local revisions and dedupes stale ones", async () => {
    vi.useFakeTimers();
    const source = makeSource();
    const { surface, windows } = makeSurface(source);
    surface.start();
    surface.setVisible(true);
    windows[0].finishLoad();

    source.emitHint({ kind: "click", x: 100, y: 200 });
    source.emitHint({ kind: "scroll", x: 0, y: 0, direction: "down" });
    const interactCalls = windows[0].executed.filter((code) => code.includes("wuuPipInteract"));
    expect(interactCalls).toHaveLength(2);
    expect(interactCalls[0]).toContain('"revision":1');
    expect(interactCalls[1]).toContain('"revision":2');
    expect(interactCalls[1]).toContain('"direction":"down"');

    // A stale protocol interaction (older revision) is ignored.
    surface.animateInteraction({ kind: "click", x: 1, y: 1, revision: 1 });
    expect(windows[0].executed.filter((code) => code.includes("wuuPipInteract"))).toHaveLength(2);
    surface.animateInteraction({ kind: "click", x: 1, y: 1, revision: 3 });
    expect(windows[0].executed.filter((code) => code.includes("wuuPipInteract"))).toHaveLength(3);
    surface.stop();
  });

  it("reports close navigation and geometry to the sink, and tears down on stop", async () => {
    vi.useFakeTimers();
    const source = makeSource();
    const { surface, sink, windows } = makeSurface(source);
    surface.start();
    surface.setVisible(true);
    const win = windows[0];
    win.finishLoad();

    const prevented = vi.fn();
    win.emitWebContents("will-navigate", { preventDefault: prevented }, "wuu-pip://close");
    expect(prevented).toHaveBeenCalled();
    expect(sink.onEvent).toHaveBeenCalledWith({ event: "user_close" });

    win.emit("moved");
    expect(sink.onEvent).toHaveBeenCalledWith(
      expect.objectContaining({ event: "geometry", width: 260, height: 170 }),
    );

    const stopped = vi.fn();
    surface.stop(stopped);
    expect(stopped).toHaveBeenCalledTimes(1);
    expect(win.destroyed).toBe(true);
    source.emitHint({ kind: "click", x: 1, y: 1 });
    expect(win.executed.filter((code) => code.includes("wuuPipInteract"))).toHaveLength(0);
    surface.stop(stopped);
    expect(stopped).toHaveBeenCalledTimes(2);
  });

  it("reports a closed tab as gone", () => {
    vi.useFakeTimers();
    const source = makeSource();
    const { surface, sink } = makeSurface(source);
    surface.start();
    surface.setVisible(true);
    source.emitClosed();
    expect(sink.onGone).toHaveBeenCalledTimes(1);
    surface.stop();
  });

  it("never leaks third-party product names into the surface page", () => {
    const html = browserPiPWindowHTML("example.com");
    expect(html).toContain("wuuPipFrame");
    expect(html).toContain("wuuPipInteract");
    expect(html).toContain("wuu-pip://close");
    expect(html.toLowerCase()).not.toMatch(/chatgpt|openai|codex(?!Pet)/);
  });
});
