import type { BrowserWindow } from "electron";
import { describe, expect, it } from "vitest";
import {
  createWindowRegistry,
  type WindowRegistry,
} from "./windowRegistry";

function makeWindow(
  id: number,
  options: { destroyed?: boolean } = {},
): BrowserWindow {
  const destroyed = options.destroyed ?? false;
  const webContents = {
    id,
    isDestroyed: () => destroyed,
  };
  return {
    webContents,
    isDestroyed: () => destroyed,
  } as unknown as BrowserWindow;
}

describe("windowRegistry", () => {
  it("starts empty", () => {
    const registry: WindowRegistry = createWindowRegistry();
    expect(registry.mainWindow()).toBeNull();
    expect(registry.allWindows()).toEqual([]);
    expect(registry.popOutWindowForThread("t1")).toBeNull();
    expect(registry.threadHostWindowID("t1")).toBeUndefined();
  });

  describe("registerWindow / mainWindow / allWindows", () => {
    it("registers a main window and returns it from mainWindow()", () => {
      const registry = createWindowRegistry();
      const win = makeWindow(1);
      registry.registerWindow(win, "main");
      expect(registry.mainWindow()).toBe(win);
      expect(registry.allWindows()).toEqual([win]);
    });

    it("returns null from mainWindow() when only popped-out windows are registered", () => {
      const registry = createWindowRegistry();
      registry.registerWindow(makeWindow(1), "popped-out");
      expect(registry.mainWindow()).toBeNull();
      expect(registry.allWindows()).toHaveLength(1);
    });

    it("lists main and popped-out windows in allWindows() in registration order", () => {
      const registry = createWindowRegistry();
      const main = makeWindow(1);
      const poppedA = makeWindow(2);
      const poppedB = makeWindow(3);
      registry.registerWindow(main, "main");
      registry.registerWindow(poppedA, "popped-out");
      registry.registerWindow(poppedB, "popped-out");
      expect(registry.mainWindow()).toBe(main);
      expect(registry.allWindows()).toEqual([main, poppedA, poppedB]);
    });

    it("overwrites a previous entry for the same webContents.id", () => {
      const registry = createWindowRegistry();
      const first = makeWindow(1);
      const second = makeWindow(1);
      registry.registerWindow(first, "main");
      registry.registerWindow(second, "main");
      expect(registry.mainWindow()).toBe(second);
      expect(registry.allWindows()).toEqual([second]);
    });
  });

  describe("unregisterWindow", () => {
    it("removes the window from mainWindow() and allWindows()", () => {
      const registry = createWindowRegistry();
      registry.registerWindow(makeWindow(1), "main");
      registry.unregisterWindow(1);
      expect(registry.mainWindow()).toBeNull();
      expect(registry.allWindows()).toEqual([]);
    });

    it("is a no-op for unknown windowIDs", () => {
      const registry = createWindowRegistry();
      const win = makeWindow(1);
      registry.registerWindow(win, "main");
      expect(() => registry.unregisterWindow(999)).not.toThrow();
      expect(registry.mainWindow()).toBe(win);
    });

    it("clears thread → window mappings pointing at the unregistered window", () => {
      const registry = createWindowRegistry();
      const poppedB = makeWindow(3);
      registry.registerWindow(makeWindow(2), "popped-out");
      registry.registerWindow(poppedB, "popped-out");
      registry.setThreadWindow("t-victim", 2);
      registry.setThreadWindow("t-survivor", 3);
      registry.unregisterWindow(2);
      expect(registry.threadHostWindowID("t-victim")).toBeUndefined();
      expect(registry.threadHostWindowID("t-survivor")).toBe(3);
      expect(registry.popOutWindowForThread("t-victim")).toBeNull();
      expect(registry.popOutWindowForThread("t-survivor")).toBe(poppedB);
    });
  });

  describe("setThreadWindow / clearThreadWindow / popOutWindowForThread / threadHostWindowID", () => {
    it("round-trips a thread → window mapping", () => {
      const registry = createWindowRegistry();
      const win = makeWindow(7);
      registry.registerWindow(win, "popped-out");
      registry.setThreadWindow("t-x", 7);
      expect(registry.threadHostWindowID("t-x")).toBe(7);
      expect(registry.popOutWindowForThread("t-x")).toBe(win);
    });

    it("returns null from popOutWindowForThread when the thread is unknown", () => {
      const registry = createWindowRegistry();
      expect(registry.popOutWindowForThread("missing")).toBeNull();
    });

    it("returns null from popOutWindowForThread after the host window is unregistered", () => {
      const registry = createWindowRegistry();
      registry.registerWindow(makeWindow(7), "popped-out");
      registry.setThreadWindow("t-x", 7);
      registry.unregisterWindow(7);
      expect(registry.popOutWindowForThread("t-x")).toBeNull();
    });

    it("clearThreadWindow drops the mapping without touching the window entry", () => {
      const registry = createWindowRegistry();
      const win = makeWindow(7);
      registry.registerWindow(win, "popped-out");
      registry.setThreadWindow("t-x", 7);
      registry.clearThreadWindow("t-x");
      expect(registry.threadHostWindowID("t-x")).toBeUndefined();
      expect(registry.popOutWindowForThread("t-x")).toBeNull();
      expect(registry.allWindows()).toEqual([win]);
    });

    it("distinct threads map to distinct windows and to the same window", () => {
      const registry = createWindowRegistry();
      const a = makeWindow(2);
      const b = makeWindow(3);
      registry.registerWindow(a, "popped-out");
      registry.registerWindow(b, "popped-out");
      registry.setThreadWindow("t-1", 2);
      registry.setThreadWindow("t-2", 2);
      registry.setThreadWindow("t-3", 3);
      expect(registry.popOutWindowForThread("t-1")).toBe(a);
      expect(registry.popOutWindowForThread("t-2")).toBe(a);
      expect(registry.popOutWindowForThread("t-3")).toBe(b);
    });

    it("re-registering the same windowID for the same thread just overwrites", () => {
      const registry = createWindowRegistry();
      const first = makeWindow(5);
      const second = makeWindow(5);
      registry.registerWindow(first, "popped-out");
      registry.registerWindow(second, "popped-out");
      registry.setThreadWindow("t-rereg", 5);
      registry.setThreadWindow("t-rereg", 5);
      expect(registry.popOutWindowForThread("t-rereg")).toBe(second);
    });
  });

  describe("destroyed BrowserWindow", () => {
    it("does not throw on register / unregister / mainWindow / allWindows", () => {
      const registry = createWindowRegistry();
      const win = makeWindow(1, { destroyed: true });
      expect(() => registry.registerWindow(win, "main")).not.toThrow();
      expect(() => registry.unregisterWindow(1)).not.toThrow();
      expect(() => registry.mainWindow()).not.toThrow();
      expect(() => registry.allWindows()).not.toThrow();
    });

    it("returns the destroyed reference so callers can observe isDestroyed()", () => {
      const registry = createWindowRegistry();
      const win = makeWindow(1, { destroyed: true });
      registry.registerWindow(win, "main");
      const returned = registry.mainWindow();
      expect(returned?.isDestroyed()).toBe(true);
      expect(registry.allWindows()[0]?.isDestroyed()).toBe(true);
    });

    it("popOutWindowForThread on a destroyed host returns the destroyed reference", () => {
      const registry = createWindowRegistry();
      const win = makeWindow(7, { destroyed: true });
      registry.registerWindow(win, "popped-out");
      registry.setThreadWindow("t-doomed", 7);
      const returned = registry.popOutWindowForThread("t-doomed");
      expect(returned?.isDestroyed()).toBe(true);
    });
  });
});
