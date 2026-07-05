import type { BrowserWindow } from "electron";

/**
 * Roles a window can play in the registry. The renderer is untrusted for
 * routing decisions; the main process uses these roles when deciding what
 * to do with the window on a server event.
 *
 *  - "main"        — the primary source window (the one currently assigned to
 *                    `mainWindow` at `desktop/src/main/index.ts`). Owns its
 *                    tabs locally via `state.sessionTabs`; the registry
 *                    never tracks individual tabs for this window.
 *  - "popped-out"  — a session lifted into its own `BrowserWindow`. Each such
 *                    window registers the threads it hosts so the existing
 *                    broadcast path can deliver the right events to it.
 */
export type WindowRole = "main" | "popped-out";

/**
 * Single source of truth for the live windows the main process created.
 *
 * For v1 the registry tracks both the main window and any popped-out
 * windows but only tracks popped-out threads in the inverse
 * `threadID → windowID` map. Source-window tabs are owned locally by the
 * renderer (`desktop/src/renderer/AppState.sessionTabs`) and existing
 * broadcasts (`emitServerEvent`, `emitTerminalEvent`,
 * `setWindowResizeState`) are dispatched to all registered windows with
 * the renderer filtering by `state.sessionTabs`.
 *
 * Lifecycle: the main process `closed` handler will call
 * `unregisterWindow` once commit 7 wires it. Commit 1 only adds the data
 * structures and unit tests; nothing in `desktop/src/main/index.ts`
 * consumes this registry yet, so the diff is observably a no-op.
 */
export interface WindowRegistry {
  /** Add or replace the entry for `window.webContents.id`. */
  registerWindow(window: BrowserWindow, role: WindowRole): void;

  /** Drop the entry for `windowID` and any thread mappings pointing at it. */
  unregisterWindow(windowID: number): void;

  /** Return the registered main window, or null when none is registered. */
  mainWindow(): BrowserWindow | null;

  /** Every registered window in registration order. Callers filter destroyed refs themselves. */
  allWindows(): BrowserWindow[];

  /** BrowserWindow currently hosting `threadID` (a popped-out window), or null. */
  popOutWindowForThread(threadID: string): BrowserWindow | null;

  /** Associate `threadID` with `windowID` (the popped-out window's `webContents.id`). */
  setThreadWindow(threadID: string, windowID: number): void;

  /** Drop the `threadID → windowID` mapping without touching the window entry. */
  clearThreadWindow(threadID: string): void;

  /** `webContents.id` hosting `threadID`, or undefined if no mapping exists. */
  threadHostWindowID(threadID: string): number | undefined;

  /**
   * Attach the per-window resize listeners (`will-resize` / `resize` /
   * `resized`) on `window` and run `onChange` for each event. Replaces
   * the previous `mainWindow.on(...)` inline wiring so commit 3+ can
   * reuse this for popped-out windows created via `createPopOutWindow`.
   */
  attachResizeHandlers(window: BrowserWindow, onChange: () => void): void;
}

interface WindowEntry {
  readonly window: BrowserWindow;
  readonly role: WindowRole;
}

class WindowRegistryImpl implements WindowRegistry {
  private readonly windowsByID = new Map<number, WindowEntry>();
  private readonly threadToWindowID = new Map<string, number>();

  registerWindow(window: BrowserWindow, role: WindowRole): void {
    this.windowsByID.set(window.webContents.id, { window, role });
  }

  unregisterWindow(windowID: number): void {
    this.windowsByID.delete(windowID);
    for (const [threadID, hostedID] of this.threadToWindowID) {
      if (hostedID === windowID) {
        this.threadToWindowID.delete(threadID);
      }
    }
  }

  mainWindow(): BrowserWindow | null {
    for (const entry of this.windowsByID.values()) {
      if (entry.role === "main") {
        return entry.window;
      }
    }
    return null;
  }

  allWindows(): BrowserWindow[] {
    return [...this.windowsByID.values()].map((entry) => entry.window);
  }

  popOutWindowForThread(threadID: string): BrowserWindow | null {
    const id = this.threadToWindowID.get(threadID);
    if (id === undefined) return null;
    const entry = this.windowsByID.get(id);
    return entry?.window ?? null;
  }

  setThreadWindow(threadID: string, windowID: number): void {
    this.threadToWindowID.set(threadID, windowID);
  }

  clearThreadWindow(threadID: string): void {
    this.threadToWindowID.delete(threadID);
  }

  threadHostWindowID(threadID: string): number | undefined {
    return this.threadToWindowID.get(threadID);
  }

  attachResizeHandlers(window: BrowserWindow, onChange: () => void): void {
    window.on("will-resize", () => onChange());
    window.on("resize", () => onChange());
    window.on("resized", () => onChange());
  }
}

/** Construct an empty `WindowRegistry`. */
export function createWindowRegistry(): WindowRegistry {
  return new WindowRegistryImpl();
}
