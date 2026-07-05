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
 * `threadID → windowID` map; popped-out windows also carry an optional
 * `workdir` string for commit 7's same-workdir cascade. Source-window
 * tabs are owned locally by the renderer
 * (`desktop/src/renderer/AppState.sessionTabs`) and existing broadcasts
 * (`emitServerEvent`, `emitTerminalEvent`, `setWindowResizeState`) are
 * dispatched to all registered windows with the renderer filtering by
 * `state.sessionTabs`.
 *
 * Lifecycle: the main process `closed` handler will call
 * `unregisterWindow` once commit 7 wires it. Commit 1 landed the data
 * structures + tests; commit 2 wired `index.ts` to fan events out via
 * `allWindows()` + `broadcastToAll`; commit 2-final migrated the
 * per-window resize listeners onto `attachResizeHandlers`; commit 3
 * added `createPopOutWindow` + `wuu:pop-out-session` IPC + the
 * `sameWorkdirPopOutWindows` lookup the commit 7 cascade will rely on.
 *
 * **§4.1 risk 5 — grep warning**: if you are about to add a
 * `BrowserWindow` reference for an app-menu item, a focused-window
 * dialog, or any "which window hosts X?" branch in `desktop/src/main/`,
 * STOP. This registry is the single source of truth — parallel `Map`s
 * or `BrowserWindow[]` arrays will silently desync. Use
 * `popOutWindowForThread(threadID)` and `windowRegistry.mainWindow()`
 * instead. (Why this lives here: §4.1 risk 5 in
 * `docs/plans/2026-07-05-popout-session-tab.md` flags an app-menu item
 * that follows the active session as the most likely site to open a
 * parallel BrowserWindow map. Keep this comment next to the interface
 * so the warning is impossible to miss when adding methods here.)
 */
export interface WindowRegistry {
  /** Add or replace the entry for `window.webContents.id`. */
  registerWindow(window: BrowserWindow, role: WindowRole, workdir?: string): void;

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

  /**
   * Commit 7 cascade uses this to enumerate popped-out windows in the
   * same workdir when the source window closes. v1 returns only
   * popped-out windows; the main window is excluded.
   */
  sameWorkdirPopOutWindows(workdir: string): BrowserWindow[];
}

interface WindowEntry {
  readonly window: BrowserWindow;
  readonly role: WindowRole;
  readonly workdir?: string;
}

class WindowRegistryImpl implements WindowRegistry {
  private readonly windowsByID = new Map<number, WindowEntry>();
  private readonly threadToWindowID = new Map<string, number>();
  private readonly resizeCleanups = new Map<number, () => void>();

  registerWindow(window: BrowserWindow, role: WindowRole, workdir?: string): void {
    this.windowsByID.set(window.webContents.id, { window, role, workdir });
  }

  unregisterWindow(windowID: number): void {
    const cleanup = this.resizeCleanups.get(windowID);
    if (cleanup) {
      cleanup();
      this.resizeCleanups.delete(windowID);
    }
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

  sameWorkdirPopOutWindows(workdir: string): BrowserWindow[] {
    return [...this.windowsByID.values()]
      .filter((entry) => entry.role === "popped-out" && entry.workdir === workdir)
      .map((entry) => entry.window);
  }

  attachResizeHandlers(window: BrowserWindow, onChange: () => void): void {
    const id = window.webContents.id;
    if (this.resizeCleanups.has(id)) return;  // idempotent — already wired
    const willResizeCb = (): void => onChange();
    const resizeCb = (): void => onChange();
    const resizedCb = (): void => onChange();
    window.on("will-resize", willResizeCb);
    window.on("resize", resizeCb);
    window.on("resized", resizedCb);
    this.resizeCleanups.set(id, () => {
      window.off("will-resize", willResizeCb);
      window.off("resize", resizeCb);
      window.off("resized", resizedCb);
    });
  }
}

/** Construct an empty `WindowRegistry`. */
export function createWindowRegistry(): WindowRegistry {
  return new WindowRegistryImpl();
}
