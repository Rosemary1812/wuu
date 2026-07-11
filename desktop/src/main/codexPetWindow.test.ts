import { beforeEach, describe, expect, it, vi } from "vitest";
import type {
  CodexPetHint,
  CodexPetSize,
  CodexPetsSnapshot,
} from "../shared/protocol";
import {
  CodexPetWindowManager,
  codexPetActionFromURL,
  codexPetBoundsForLayout,
  codexPetRenderedSpriteForSize,
  codexPetStateForRuntime,
  codexPetView,
  codexPetWindowHTML,
  selectCodexPetBubbleLayout,
  selectedCodexPet,
} from "./codexPetWindow";

// Hoisted electron mocks for the CodexPetWindowManager integration test.
// vi.hoisted runs before module imports, so the mock function and the
// shared captured-listener map are available when the "electron" factory
// below captures them. We use vi.fn() instead of a class so the
// constructor call itself is a tracked spy (`new` on a vi.fn() records
// the call), and a single shared capturedListeners map keeps the
// listeners in scope across the multiple BrowserWindow instances the
// manager may create.
const codexPetElectronMocks = vi.hoisted(() => {
  const capturedListeners: Record<string, Array<(...args: unknown[]) => void>> = {};
  const loadURL = vi.fn();
  const showInactive = vi.fn();
  const close = vi.fn();
  const isDestroyed = vi.fn(() => false);
  const isVisible = vi.fn(() => false);
  const setWindowOpenHandler = vi.fn();
  const executeJavaScript = vi.fn().mockResolvedValue(undefined);
  const popup = vi.fn();
  const setAlwaysOnTop = vi.fn();
  const setHasShadow = vi.fn();
  const setVisibleOnAllWorkspaces = vi.fn();
  const initialBounds = { x: 0, y: 0, width: 120, height: 128 };
  const boundsStack: Array<{ x: number; y: number; width: number; height: number }> = [initialBounds];
  const setBounds = vi.fn((next: { x: number; y: number; width: number; height: number }) => {
    boundsStack[boundsStack.length - 1] = next;
  });
  const getBounds = vi.fn(() => boundsStack[boundsStack.length - 1]);

  // `new BrowserWindow(options)` becomes a tracked spy call. The impl
  // returns a fresh mock instance each time so per-window state (e.g.
  // the listener maps above) can stay shared across instances.
  const BrowserWindow = vi.fn().mockImplementation(() => ({
    webContents: {
      on: (event: string, listener: (...args: unknown[]) => void) => {
        (capturedListeners[`wc:${event}`] ??= []).push(listener);
      },
      setWindowOpenHandler,
      executeJavaScript,
    },
    on: (event: string, listener: (...args: unknown[]) => void) => {
      (capturedListeners[`win:${event}`] ??= []).push(listener);
    },
    once: (event: string, listener: (...args: unknown[]) => void) => {
      (capturedListeners[`win:${event}:once`] ??= []).push(listener);
    },
    showInactive,
    close,
    isDestroyed,
    isVisible,
    setAlwaysOnTop,
    setHasShadow,
    setVisibleOnAllWorkspaces,
    loadURL,
    setBounds,
    getBounds,
  }));

  return {
    capturedListeners,
    loadURL,
    showInactive,
    close,
    isDestroyed,
    isVisible,
    setWindowOpenHandler,
    executeJavaScript,
    popup,
    setBounds,
    getBounds,
    BrowserWindow,
  };
});

vi.mock("electron", () => ({
  BrowserWindow: codexPetElectronMocks.BrowserWindow,
  Menu: {
    buildFromTemplate: () => ({ popup: codexPetElectronMocks.popup }),
  },
  screen: {
    getPrimaryDisplay: () => ({
      workArea: { x: 0, y: 0, width: 1920, height: 1080 },
    }),
  },
}));

function snapshot(enabled: boolean, selectedID = "alpha"): CodexPetsSnapshot {
  return {
    home: "/tmp/pets",
    enabled,
    selected_id: selectedID,
    errors: [],
    pets: [
      {
        id: "alpha",
        display_name: "Alpha Pet",
        description: "",
        manifest_path: "/tmp/pets/alpha/pet.json",
        spritesheet_path: "/tmp/pets/alpha/spritesheet.webp",
        spritesheet_url: "wuu-file://local/alpha",
      },
    ],
  };
}

const sampleHint: CodexPetHint = {
  thread_id: "thread-42",
  title: "Refactor auth flow",
  status: "running",
  preview: "正在执行 npm test",
  attention: false,
  updated_at: 1700000000000,
};

describe("codexPetStateForRuntime", () => {
  it("maps app runtime state onto Codex Pets atlas states", () => {
    expect(codexPetStateForRuntime({ running: false, status: "ready" }).id).toBe("idle");
    expect(codexPetStateForRuntime({ running: true, status: "正在发送请求" }).id).toBe("running");
    expect(codexPetStateForRuntime({ running: false, status: "等待权限确认" }).id).toBe("review");
    expect(codexPetStateForRuntime({ running: false, status: "send failed" }).id).toBe("failed");
    expect(codexPetStateForRuntime({ running: false, status: "queued" }).id).toBe("waiting");
  });
});

describe("selectedCodexPet", () => {
  it("returns nothing while pets are disabled", () => {
    expect(selectedCodexPet(snapshot(false))).toBeUndefined();
    expect(selectedCodexPet(undefined)).toBeUndefined();
  });

  it("falls back to the first pet when the selection is stale", () => {
    expect(selectedCodexPet(snapshot(true, "missing"))?.id).toBe("alpha");
  });
});

describe("codexPetRenderedSpriteForSize", () => {
  it("returns the base footprint at the default size", () => {
    const rendered = codexPetRenderedSpriteForSize("default");
    expect(rendered.width).toBe(96);
    expect(rendered.height).toBe(104);
    expect(rendered.scale).toBe(0.5);
    expect(rendered.windowWidth).toBe(120);
    expect(rendered.windowHeight).toBe(128);
  });

  it("scales the sprite + window footprint for non-default sizes", () => {
    const small = codexPetRenderedSpriteForSize("small");
    expect(small.width).toBe(72);
    expect(small.height).toBe(78);
    expect(small.scale).toBeCloseTo(0.375);
    expect(small.windowWidth).toBe(96);
    expect(small.windowHeight).toBe(102);

    const large = codexPetRenderedSpriteForSize("large");
    expect(large.width).toBe(144);
    expect(large.height).toBe(156);
    expect(large.scale).toBeCloseTo(0.75);
    expect(large.windowWidth).toBe(168);
    expect(large.windowHeight).toBe(180);

    const xl = codexPetRenderedSpriteForSize("extra-large");
    expect(xl.width).toBe(192);
    expect(xl.height).toBe(208);
    expect(xl.scale).toBe(1.0);
    expect(xl.windowWidth).toBe(216);
    expect(xl.windowHeight).toBe(232);
  });
});

describe("codexPetView", () => {
  it("derives spritesheet animation variables from the atlas state", () => {
    const view = codexPetView(
      snapshot(true).pets[0],
      codexPetStateForRuntime({ running: true, status: "" }),
      null,
      "hidden",
    );
    expect(view.spritesheetURL).toBe("wuu-file://local/alpha");
    expect(view.y).toBe(-1456);
    expect(view.endX).toBe(-1152);
    expect(view.frames).toBe(6);
    expect(view.duration).toBe(1560);
    expect(view.label).toBe("Alpha Pet running");
    expect(view.hint).toBeNull();
    expect(view.layout).toBe("hidden");
  });

  it("carries the hint and layout through to the renderer payload", () => {
    const view = codexPetView(
      snapshot(true).pets[0],
      codexPetStateForRuntime({ running: true, status: "" }),
      sampleHint,
      "above",
    );
    expect(view.hint).toEqual(sampleHint);
    expect(view.layout).toBe("above");
  });

  it("threads the size into the per-size sprite geometry when provided", () => {
    const view = codexPetView(
      snapshot(true).pets[0],
      codexPetStateForRuntime({ running: false, status: "" }),
      null,
      "hidden",
      "large",
    );
    expect(view.size).toBe("large");
    expect(view.spriteWidth).toBe(144);
    expect(view.spriteHeight).toBe(156);
    expect(view.scale).toBeCloseTo(0.75);
  });
});

describe("codexPetWindowHTML", () => {
  const html = codexPetWindowHTML(
    codexPetView(
      snapshot(true).pets[0],
      codexPetStateForRuntime({ running: false, status: "" }),
      null,
      "hidden",
    ),
  );

  it("embeds the spritesheet and animation variables inline", () => {
    expect(html).toContain("wuu-file://local/alpha");
    expect(html).toContain("--pet-frames:6");
    expect(html).toContain("steps(var(--pet-frames))");
    expect(html).toMatch(/\.sprite\{[^}]*flex-shrink:0/);
  });

  it("keeps the window draggable and offers the context menu action", () => {
    expect(html).toContain("pointerdown");
    expect(html).toContain("window.moveTo");
    expect(html).toContain("wuu-pet://action/menu");
  });

  it("locks the page down to spritesheet images and inline assets", () => {
    expect(html).toContain("default-src 'none'");
    expect(html).toContain("img-src wuu-file:");
  });

  it("renders the bubble DOM and jump action when a hint is provided", () => {
    const withHint = codexPetWindowHTML(
      codexPetView(snapshot(true).pets[0], codexPetStateForRuntime({ running: true, status: "" }), sampleHint, "above"),
    );
    expect(withHint).toContain(`data-thread-id="${sampleHint.thread_id}"`);
    expect(withHint).toContain("wuu-pet://action/jump?thread_id=");
    expect(withHint).toContain("-webkit-line-clamp:1");
  });

  it("keeps the live updater text-only so refreshes never touch missing nodes", () => {
    // The bubble markup only renders `.preview`. Earlier revisions still
    // queried `.title` and `.dot` from the live applyHint script; the missing
    // nodes raised TypeError on every wuuPetView push and silently dropped the
    // preview text. Pin the script to the text-only contract so the gap can't
    // sneak back in.
    const withHint = codexPetWindowHTML(
      codexPetView(snapshot(true).pets[0], codexPetStateForRuntime({ running: true, status: "" }), sampleHint, "above"),
    );
    expect(withHint).not.toMatch(/bubble\.querySelector\(['"]\.title['"]\)/);
    expect(withHint).not.toMatch(/bubble\.querySelector\(['"]\.dot['"]\)/);
    expect(withHint).not.toMatch(/STATUS_COLORS/);
    expect(withHint).not.toMatch(/classList\.toggle\(['"]attention['"]/);
    expect(withHint).toMatch(/bubble\.querySelector\(['"]\.preview['"]\)/);
  });

  it("fades the preview out, swaps text, then fades it back in on commentary change", () => {
    // Pin the swap animation CSS so the bubble preview never regresses to a
    // flash on text change. .preview carries the fade-in transition; the
    // .is-swapping modifier sets opacity 0 with a shorter fade-out so the
    // cycle reads as "fade out -> swap -> fade in" (~320ms total). Without
    // these rules the bubble would update instantly and feel jumpy when the
    // first commentary hands off to the second, or when the bubble disappears.
    const withHint = codexPetWindowHTML(
      codexPetView(snapshot(true).pets[0], codexPetStateForRuntime({ running: true, status: "" }), sampleHint, "above"),
    );
    expect(withHint).toMatch(/\.bubble\s+\.preview\s*\{[^}]*transition:\s*opacity\s+180ms\s+ease/);
    expect(withHint).toMatch(/\.bubble\.is-swapping\s+\.preview\s*\{[^}]*opacity:\s*0/);
    expect(withHint).toMatch(/\.bubble\.is-swapping\s+\.preview\s*\{[^}]*transition:\s*opacity\s+140ms\s+ease/);
  });

  it("runs the swap through a state machine so a fast second push doesn't strand mid-fade", () => {
    // The state machine has three contracts: (1) isFirstApply gates the
    // initial render so the inline-rendered text doesn't fade on first
    // mount, (2) cancelSwap clears any pending setTimeout AND removes the
    // .is-swapping class so a back-to-back push doesn't leave the dimmed
    // class lingering over mismatched text, and (3) the same-text branch
    // short-circuits before scheduling a swap. Pin all three so the
    // animation contract can't drift back to a no-fade world.
    const withHint = codexPetWindowHTML(
      codexPetView(snapshot(true).pets[0], codexPetStateForRuntime({ running: true, status: "" }), sampleHint, "above"),
    );
    expect(withHint).toMatch(/isFirstApply\s*=\s*true/);
    expect(withHint).toMatch(/pendingSwap\s*=\s*null/);
    expect(withHint).toMatch(/clearTimeout\(pendingSwap\)/);
    expect(withHint).toMatch(/bubble\.classList\.remove\(['"]is-swapping['"]\)/);
    expect(withHint).toMatch(/bubble\.classList\.add\(['"]is-swapping['"]\)/);
    expect(withHint).toMatch(/if\s*\(oldPreview\s*===\s*newPreview\)/);
  });

  it("hides the bubble whenever the pushed hint has no preview text", () => {
    // The renderer drops the Thread.preview fallback, so a top-ranked
    // thread with no stable agent_message pushes a non-null hint with
    // empty preview here. The window-side applyHint must treat that as
    // "hide" the same way it treats a null hint, so the bubble never
    // surfaces stale text from any source. Pin the show/hide gate on
    // preview emptiness and the trim() so whitespace-only previews also
    // hide.
    const withHint = codexPetWindowHTML(
      codexPetView(snapshot(true).pets[0], codexPetStateForRuntime({ running: true, status: "" }), sampleHint, "above"),
    );
    expect(withHint).toMatch(/trimmedPreview\s*=\s*\(hint\?\.preview\s*\?\?\s*['"]['"]\)/);
    expect(withHint).toMatch(/!\s*hint\s*\|\|\s*trimmedPreview\.length\s*===\s*0/);
    expect(withHint).toMatch(/bubble\.style\.display\s*=\s*['"]none['"]/);
  });

  it("keeps the resize strips invisible hot zones with only the cursor as affordance", () => {
    // The 16px top/bottom strips resize the pet continuously; the
    // ns-resize cursor is the affordance. Earlier revisions painted a
    // pair of dark grip bars into the strip via ::before / ::after —
    // those read as a stray black line floating over the pet, so pin
    // that no pseudo-element chrome comes back.
    const html = codexPetWindowHTML(
      codexPetView(snapshot(true).pets[0], codexPetStateForRuntime({ running: false, status: "" }), null, "hidden"),
    );
    expect(html).toMatch(/\.resize-handle\s*\{[^}]*height:16px/);
    expect(html).toMatch(/\.resize-handle\s*\{[^}]*cursor:ns-resize/);
    expect(html).not.toContain(".resize-handle::before");
    expect(html).not.toContain(".resize-handle::after");
  });

  it("resizes continuously during the drag and commits the final scale on release", () => {
    // The inline script converts each pointermove's vertical delta into a
    // live scale (1px of drag = 1px of rendered sprite height, i.e. delta /
    // CELL_HEIGHT), emits it rAF-throttled via wuu-pet://action/scale, and
    // re-sends the final value with commit=1 on pointerup so the host
    // persists it. Pin the emission URL, the throttle, the commit marker,
    // and that the old snap-to-next-preset flow is gone.
    const html = codexPetWindowHTML(
      codexPetView(snapshot(true).pets[0], codexPetStateForRuntime({ running: false, status: "" }), null, "hidden"),
    );
    expect(html).toContain("wuu-pet://action/scale?value=");
    expect(html).toContain("&commit=1");
    expect(html).toContain("requestAnimationFrame");
    expect(html).toMatch(/liveScale\s*=\s*view\.scale/);
    expect(html).not.toContain("wuu-pet://action/resize?id=");
  });

  it("threads the per-size sprite geometry into the inline CSS variables and styles", () => {
    const html = codexPetWindowHTML(
      codexPetView(
        snapshot(true).pets[0],
        codexPetStateForRuntime({ running: false, status: "" }),
        null,
        "hidden",
        "large",
      ),
    );
    expect(html).toContain("--pet-frame-width:144px");
    expect(html).toContain("--pet-frame-height:156px");
    expect(html).toContain("--pet-scale:0.75");
    expect(html).toMatch(/\.sprite-frame\{[^}]*var\(--pet-frame-width\)/);
    expect(html).toMatch(/\.sprite\{[^}]*scale\(var\(--pet-scale\)\)/);
  });
});

describe("codexPetActionFromURL", () => {
  it("accepts the pet menu action", () => {
    expect(codexPetActionFromURL("wuu-pet://action/menu")).toEqual({ action: "menu" });
    expect(codexPetActionFromURL("wuu-pet://action/close")).toBeUndefined();
    expect(codexPetActionFromURL("wuu-cua://action/menu")).toBeUndefined();
    expect(codexPetActionFromURL("not a url")).toBeUndefined();
  });

  it("accepts the jump action only with a thread_id", () => {
    expect(codexPetActionFromURL("wuu-pet://action/jump?thread_id=abc")).toEqual({
      action: "jump",
      thread_id: "abc",
    });
    expect(codexPetActionFromURL("wuu-pet://action/jump")).toBeUndefined();
  });

  it("parses the continuous scale action with clamping and the commit marker", () => {
    expect(codexPetActionFromURL("wuu-pet://action/scale?value=0.62")).toEqual({
      action: "scale",
      value: 0.62,
      commit: false,
    });
    expect(
      codexPetActionFromURL("wuu-pet://action/scale?value=0.62&commit=1"),
    ).toEqual({ action: "scale", value: 0.62, commit: true });
    // Out-of-range values clamp instead of resizing the pet into
    // uselessness; non-finite / missing values are dropped entirely.
    expect(codexPetActionFromURL("wuu-pet://action/scale?value=99")).toEqual({
      action: "scale",
      value: 1.5,
      commit: false,
    });
    expect(codexPetActionFromURL("wuu-pet://action/scale?value=0")).toEqual({
      action: "scale",
      value: 0.25,
      commit: false,
    });
    expect(codexPetActionFromURL("wuu-pet://action/scale?value=abc")).toBeUndefined();
    expect(codexPetActionFromURL("wuu-pet://action/scale")).toBeUndefined();
  });
});

describe("codexPetBoundsForLayout", () => {
  const anchor = { x: 1000, y: 900 };

  it("places bubble above and centers it horizontally over the sprite", () => {
    const bounds = codexPetBoundsForLayout({ layout: "above", anchor, bubble: { width: 280, height: 80 } });
    expect(bounds.width).toBe(280);
    expect(bounds.height).toBe(208);
    expect(bounds.x).toBe(860); // anchor.x - width/2
    expect(bounds.y).toBe(700); // anchor.y - (height - 8)
  });

  it("places the bubble to the right of the sprite with sprite anchor preserved", () => {
    const bounds = codexPetBoundsForLayout({ layout: "right", anchor, bubble: { width: 280, height: 80 } });
    expect(bounds.width).toBe(400);
    expect(bounds.height).toBe(120);
    expect(bounds.x).toBe(944); // anchor.x - 8 - 48
    expect(bounds.y).toBe(788); // anchor.y - (height - 8)
  });

  it("uses the size's sprite footprint when one is supplied", () => {
    const bounds = codexPetBoundsForLayout({
      layout: "above",
      anchor,
      bubble: { width: 280, height: 80 },
      size: "large",
    });
    // large: spriteW = 144, spriteH = 156 → height = 8 + 80 + 8 + 156 + 8 = 260
    expect(bounds.width).toBe(280); // bubble still drives width
    expect(bounds.height).toBe(260);
  });
});

describe("selectCodexPetBubbleLayout", () => {
  const bubble = { width: 280, height: 80 };

  it("picks 'above' when there is room above the sprite", () => {
    const workArea = { x: 0, y: 0, width: 1920, height: 1080 };
    const anchor = { x: 960, y: 900 };
    const decision = selectCodexPetBubbleLayout({ workArea, anchor, bubble });
    expect(decision.layout).toBe("above");
  });

  it("falls back to 'right' when the sprite sits too close to the top edge", () => {
    const workArea = { x: 0, y: 0, width: 1920, height: 1080 };
    // anchor.y is small enough that 'above' (height 208) overflows the top
    // edge, but 'right' (height 120) still fits centered around the sprite.
    const anchor = { x: 960, y: 150 };
    const decision = selectCodexPetBubbleLayout({ workArea, anchor, bubble });
    expect(decision.layout).toBe("right");
  });

  it("returns 'hidden' when no layout fits inside the workArea", () => {
    const workArea = { x: 0, y: 0, width: 400, height: 60 }; // tiny workArea, nothing fits
    const anchor = { x: 200, y: 50 };
    const decision = selectCodexPetBubbleLayout({ workArea, anchor, bubble });
    expect(decision.layout).toBe("hidden");
    expect(decision.bounds.width).toBe(120);
    expect(decision.bounds.height).toBe(128);
  });

  it("scales the hidden fallback bounds when a non-default size is requested", () => {
    const workArea = { x: 0, y: 0, width: 400, height: 60 };
    const anchor = { x: 200, y: 50 };
    const decision = selectCodexPetBubbleLayout({
      workArea,
      anchor,
      bubble,
      size: "large",
    });
    expect(decision.layout).toBe("hidden");
    // large: windowWidth = 168, windowHeight = 180
    expect(decision.bounds.width).toBe(168);
    expect(decision.bounds.height).toBe(180);
  });
});

function enabledSnapshot(overrides: Partial<CodexPetsSnapshot> = {}): CodexPetsSnapshot {
  return {
    home: "/tmp/pets",
    enabled: true,
    selected_id: "clawd",
    errors: [],
    pets: [
      {
        id: "clawd",
        display_name: "Clawd",
        description: "",
        manifest_path: "/tmp/pets/clawd/pet.json",
        spritesheet_path: "/tmp/pets/clawd/spritesheet.webp",
        spritesheet_url: "wuu-file://local/clawd",
      },
    ],
    ...overrides,
  };
}

describe("CodexPetWindowManager", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    codexPetElectronMocks.isDestroyed.mockReturnValue(false);
    codexPetElectronMocks.isVisible.mockReturnValue(false);
    codexPetElectronMocks.getBounds.mockImplementation(() => ({
      x: 100,
      y: 200,
      width: 120,
      height: 128,
    }));
    codexPetElectronMocks.setBounds.mockImplementation(() => undefined);
    for (const key of Object.keys(codexPetElectronMocks.capturedListeners)) {
      delete codexPetElectronMocks.capturedListeners[key];
    }
  });

  it("surfaces the pet window after did-finish-load when ready-to-show never fires", () => {
    const manager = new CodexPetWindowManager(() => undefined, () => undefined);
    manager.sync(enabledSnapshot());

    // sync creates a BrowserWindow and queues the data: URL load.
    expect(codexPetElectronMocks.BrowserWindow).toHaveBeenCalledTimes(1);
    expect(codexPetElectronMocks.loadURL).toHaveBeenCalledTimes(1);
    // Window starts hidden and is not shown until the load event surfaces it.
    expect(codexPetElectronMocks.showInactive).not.toHaveBeenCalled();

    // Simulate the regression: data: URL ready-to-show never fires, but
    // did-finish-load does. Without the fallback, the window would stay
    // at show: false forever (which is what the user saw after the
    // codex pet migration).
    const didFinishLoad =
      codexPetElectronMocks.capturedListeners["wc:did-finish-load"]?.[0];
    expect(didFinishLoad).toBeDefined();
    didFinishLoad!({});

    // The did-finish-load fallback must surface the window even though
    // ready-to-show never fired.
    expect(codexPetElectronMocks.showInactive).toHaveBeenCalledTimes(1);
  });

  it("destroys the window when the snapshot no longer has an enabled pet", () => {
    const manager = new CodexPetWindowManager(() => undefined, () => undefined);
    manager.sync(enabledSnapshot());
    expect(codexPetElectronMocks.BrowserWindow).toHaveBeenCalledTimes(1);

    // Disabling the pet (e.g. user toggled it off in Settings) tears
    // the window down so it stops eating input on the main window.
    manager.sync({ ...enabledSnapshot(), enabled: false, pets: [] });
    expect(codexPetElectronMocks.close).toHaveBeenCalledTimes(1);
  });

  it("resizes the window when a hint is set and triggers a jump via the jump callback", () => {
    const closeRequested = vi.fn();
    const jumpRequested = vi.fn();
    const manager = new CodexPetWindowManager(closeRequested, jumpRequested);
    manager.sync(enabledSnapshot());
    const didFinishLoad =
      codexPetElectronMocks.capturedListeners["wc:did-finish-load"]?.[0];
    didFinishLoad!({});
    codexPetElectronMocks.setBounds.mockClear();
    codexPetElectronMocks.executeJavaScript.mockClear();

    manager.setHint(sampleHint);

    // With the single-line card-chrome bubble at 280×44 (BUBBLE_HEIGHT —
    // wraps the 12px inner padding plus 1 line of 13/1.45 text), the
    // window height comes out to 8 (BUBBLE_PADDING) + 44 (BUBBLE_HEIGHT) +
    // 8 (BUBBLE_GAP) + 104 (spriteH) + 8 (BUBBLE_PADDING) = 172. The
    // width is `max(spriteW, bubble.width) = max(96, 280) = 280`.
    expect(codexPetElectronMocks.setBounds).toHaveBeenCalledWith(
      expect.objectContaining({ width: 280, height: 172 }),
    );
    // wuuPetView is invoked with the new view carrying the hint.
    const lastCall = codexPetElectronMocks.executeJavaScript.mock.calls.at(-1)?.[0] as string;
    expect(lastCall).toContain("wuuPetView");
    expect(lastCall).toContain("thread-42");

    // Simulate a bubble click: the renderer navigates to wuu-pet://action/jump
    const willNavigate = codexPetElectronMocks.capturedListeners["wc:will-navigate"]?.[0];
    willNavigate!({ preventDefault: vi.fn() }, "wuu-pet://action/jump?thread_id=thread-42");
    expect(jumpRequested).toHaveBeenCalledWith(sampleHint);
  });

  it("resizes the pet window and notifies the host when setSize() picks a non-default size", () => {
    const onSizeChange = vi.fn();
    const manager = new CodexPetWindowManager(
      () => undefined,
      () => undefined,
      onSizeChange,
    );
    manager.sync(enabledSnapshot());
    const didFinishLoad =
      codexPetElectronMocks.capturedListeners["wc:did-finish-load"]?.[0];
    didFinishLoad!({});
    codexPetElectronMocks.setBounds.mockClear();
    codexPetElectronMocks.executeJavaScript.mockClear();

    manager.setSize("large");

    // The window grew to the "large" footprint: 168 × 180.
    expect(codexPetElectronMocks.setBounds).toHaveBeenCalledWith(
      expect.objectContaining({ width: 168, height: 180 }),
    );
    // The in-page sprite geometry also changed (spriteWidth/spriteHeight/scale
    // travel inside the wuuPetView payload; the in-page closure applies them
    // to the CSS custom properties once received). The earlier revision of
    // this test asserted on the executeJavaScript call string, which only
    // embeds the JSON payload now, so check the payload directly.
    const lastCall = codexPetElectronMocks.executeJavaScript.mock.calls.at(
      -1,
    )?.[0] as string;
    expect(lastCall).toContain("wuuPetView");
    expect(lastCall).toContain('"spriteWidth":144');
    expect(lastCall).toContain('"spriteHeight":156');
    expect(lastCall).toContain('"scale":0.75');
    expect(lastCall).toContain('"size":"large"');
    // The host was told so it can persist the choice to desktop-settings.json.
    expect(onSizeChange).toHaveBeenCalledWith("large");
  });

  it("live-resizes on setScale() without notifying the host until commit", () => {
    const onSizeChange = vi.fn();
    const onScaleChange = vi.fn();
    const manager = new CodexPetWindowManager(
      () => undefined,
      () => undefined,
      onSizeChange,
      onScaleChange,
    );
    manager.sync(enabledSnapshot());
    const didFinishLoad =
      codexPetElectronMocks.capturedListeners["wc:did-finish-load"]?.[0];
    didFinishLoad!({});
    codexPetElectronMocks.setBounds.mockClear();
    codexPetElectronMocks.executeJavaScript.mockClear();

    // Mid-drag frame: geometry updates live, no persistence traffic.
    manager.setScale(0.75);
    // scale 0.75 → multiplier 1.5 → sprite 144×156 → window 168×180.
    expect(codexPetElectronMocks.setBounds).toHaveBeenCalledWith(
      expect.objectContaining({ width: 168, height: 180 }),
    );
    const midDragCall = codexPetElectronMocks.executeJavaScript.mock.calls.at(
      -1,
    )?.[0] as string;
    expect(midDragCall).toContain('"scale":0.75');
    expect(onScaleChange).not.toHaveBeenCalled();
    expect(onSizeChange).not.toHaveBeenCalled();

    // Pointerup arrives with commit=true → the host persists the value.
    manager.setScale(0.8, true);
    expect(onScaleChange).toHaveBeenCalledWith(0.8);
  });

  it("clamps out-of-range scales from the startup rehydration path", () => {
    const onScaleChange = vi.fn();
    const manager = new CodexPetWindowManager(
      () => undefined,
      () => undefined,
      undefined,
      onScaleChange,
    );
    // setScale runs before the window exists on startup: it must record
    // the (clamped) scale so createWindow sizes the first BrowserWindow
    // from it instead of the preset.
    manager.setScale(99, true);
    expect(onScaleChange).toHaveBeenCalledWith(1.5);
    manager.sync(enabledSnapshot());
    const options = codexPetElectronMocks.BrowserWindow.mock.calls.at(-1)?.[0] as {
      width: number;
      height: number;
    };
    // scale 1.5 → multiplier 3 → sprite 288×312 → window 312×336.
    expect(options.width).toBe(312);
    expect(options.height).toBe(336);
  });

  it("routes a scale navigation from the pet page through setScale", () => {
    const onScaleChange = vi.fn();
    const manager = new CodexPetWindowManager(
      () => undefined,
      () => undefined,
      undefined,
      onScaleChange,
    );
    manager.sync(enabledSnapshot());
    const didFinishLoad =
      codexPetElectronMocks.capturedListeners["wc:did-finish-load"]?.[0];
    didFinishLoad!({});
    codexPetElectronMocks.setBounds.mockClear();

    const willNavigate =
      codexPetElectronMocks.capturedListeners["wc:will-navigate"]?.[0];
    willNavigate!({ preventDefault: vi.fn() }, "wuu-pet://action/scale?value=0.75");
    expect(codexPetElectronMocks.setBounds).toHaveBeenCalledWith(
      expect.objectContaining({ width: 168, height: 180 }),
    );
    expect(onScaleChange).not.toHaveBeenCalled();

    willNavigate!(
      { preventDefault: vi.fn() },
      "wuu-pet://action/scale?value=0.75&commit=1",
    );
    expect(onScaleChange).toHaveBeenCalledWith(0.75);
  });

  it("is a no-op when setSize() matches the current size", () => {
    const onSizeChange = vi.fn();
    const manager = new CodexPetWindowManager(
      () => undefined,
      () => undefined,
      onSizeChange,
    );
    manager.sync(enabledSnapshot());
    const didFinishLoad =
      codexPetElectronMocks.capturedListeners["wc:did-finish-load"]?.[0];
    didFinishLoad!({});
    codexPetElectronMocks.setBounds.mockClear();
    codexPetElectronMocks.executeJavaScript.mockClear();

    manager.setSize("default");

    expect(onSizeChange).not.toHaveBeenCalled();
    expect(codexPetElectronMocks.setBounds).not.toHaveBeenCalled();
    expect(codexPetElectronMocks.executeJavaScript).not.toHaveBeenCalled();
  });
});
