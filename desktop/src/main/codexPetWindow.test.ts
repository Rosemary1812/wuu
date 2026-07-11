import { beforeEach, describe, expect, it, vi } from "vitest";
import type { CodexPetsSnapshot } from "../shared/protocol";
import {
  CodexPetWindowManager,
  codexPetActionFromURL,
  codexPetStateForRuntime,
  codexPetView,
  codexPetWindowHTML,
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

describe("codexPetView", () => {
  it("derives spritesheet animation variables from the atlas state", () => {
    const view = codexPetView(
      snapshot(true).pets[0],
      codexPetStateForRuntime({ running: true, status: "" }),
    );
    expect(view.spritesheetURL).toBe("wuu-file://local/alpha");
    expect(view.y).toBe(-1456);
    expect(view.endX).toBe(-1152);
    expect(view.frames).toBe(6);
    expect(view.duration).toBe(1560);
    expect(view.label).toBe("Alpha Pet running");
  });
});

describe("codexPetWindowHTML", () => {
  const html = codexPetWindowHTML(
    codexPetView(
      snapshot(true).pets[0],
      codexPetStateForRuntime({ running: false, status: "" }),
    ),
  );

  it("embeds the spritesheet and animation variables inline", () => {
    expect(html).toContain("wuu-file://local/alpha");
    expect(html).toContain("--pet-frames:6");
    expect(html).toContain("steps(var(--pet-frames))");
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
});

describe("codexPetActionFromURL", () => {
  it("accepts only the pet menu action", () => {
    expect(codexPetActionFromURL("wuu-pet://action/menu")).toBe("menu");
    expect(codexPetActionFromURL("wuu-pet://action/close")).toBeUndefined();
    expect(codexPetActionFromURL("wuu-cua://action/menu")).toBeUndefined();
    expect(codexPetActionFromURL("not a url")).toBeUndefined();
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
    for (const key of Object.keys(codexPetElectronMocks.capturedListeners)) {
      delete codexPetElectronMocks.capturedListeners[key];
    }
  });

  it("surfaces the pet window after did-finish-load when ready-to-show never fires", () => {
    const manager = new CodexPetWindowManager(() => undefined);
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
    const manager = new CodexPetWindowManager(() => undefined);
    manager.sync(enabledSnapshot());
    expect(codexPetElectronMocks.BrowserWindow).toHaveBeenCalledTimes(1);

    // Disabling the pet (e.g. user toggled it off in Settings) tears
    // the window down so it stops eating input on the main window.
    manager.sync({ ...enabledSnapshot(), enabled: false, pets: [] });
    expect(codexPetElectronMocks.close).toHaveBeenCalledTimes(1);
  });
});
