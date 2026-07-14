import type { BrowserWindow } from "electron";
import type { RuntimeContext } from "../shared/protocol";

/**
 * Roles a window can play in the registry. The renderer is untrusted for
 * routing decisions; the main process uses these roles when deciding what
 * to do with the window on a server event.
 *
 *  - "main"        — the primary application window.
 *  - "popped-out"  — a thread, draft, or subthread in its own window.
 *  - "activity"    — a window dedicated to one background activity.
 */
export type WindowRole = "main" | "popped-out" | "activity";

/**
 * Single source of truth for the live windows the main process created.
 *
 * Window-specific routing and cleanup must resolve owners through this
 * registry. Keeping a parallel BrowserWindow map would let lifecycle state
 * drift and deliver events to the wrong renderer.
 */
export interface WindowRegistry {
  /** Add or replace the entry for `window.webContents.id`. */
  registerWindow(
    window: BrowserWindow,
    role: WindowRole,
    options?: WindowRegistrationOptions,
  ): void;

  /** Drop the entry for `windowID` and any thread mappings pointing at it. */
  unregisterWindow(windowID: number): void;

  /** Return the registered main window, or null when none is registered. */
  mainWindow(): BrowserWindow | null;

  /** Every registered window in registration order. Callers filter destroyed refs themselves. */
  allWindows(): BrowserWindow[];

  /** Registered window for a webContents id, or null after it was removed. */
  windowForID(windowID: number): BrowserWindow | null;

  /** BrowserWindow currently hosting `threadID` (a popped-out window), or null. */
  popOutWindowForThread(threadID: string): BrowserWindow | null;

  /** Associate `threadID` with `windowID` (the popped-out window's `webContents.id`). */
  setThreadWindow(threadID: string, windowID: number): void;

  /** Drop the `threadID → windowID` mapping without touching the window entry. */
  clearThreadWindow(threadID: string): void;

  /** `webContents.id` hosting `threadID`, or undefined if no mapping exists. */
  threadHostWindowID(threadID: string): number | undefined;

  /** Runtime context pinned to a window, or undefined for the global main window. */
  runtimeContextForWindow(windowID: number): RuntimeContext | undefined;

  /** Popped-out thread hosted by a window, or undefined for the main window. */
  threadForWindow(windowID: number): string | undefined;

  /** BrowserWindow currently hosting a popped-out reply subthread (cth), or null. */
  popOutWindowForSubthread(subthreadID: string): BrowserWindow | null;

  /** Associate a reply subthread (cth) id with `windowID`. Kept separate from
   *  the thread map: a subthread window also stores its PARENT threadID for
   *  runtime routing, so reusing setThreadWindow would clobber the parent group
   *  thread's own pop-out dedup mapping. */
  setSubthreadWindow(subthreadID: string, windowID: number): void;

  /** Drop the `subthreadID → windowID` mapping without touching the window entry. */
  clearSubthreadWindow(subthreadID: string): void;

  /** `webContents.id` hosting a reply subthread (cth), or undefined. */
  subthreadHostWindowID(subthreadID: string): number | undefined;

  activityWindow(activityID: string): BrowserWindow | null;
  setActivityWindow(activityID: string, windowID: number): void;
  clearActivityWindow(activityID: string): void;
  activityHostWindowID(activityID: string): number | undefined;

  /** Popped-out reply subthread (cth) hosted by a window, or undefined. */
  subthreadForWindow(windowID: number): string | undefined;

  /**
   * Attach the per-window resize listeners (`will-resize` / `resize` /
   * `resized`) on `window` and run `onChange` for each event.
   */
  attachResizeHandlers(window: BrowserWindow, onChange: () => void): void;

  /** Popped-out windows in the workdir; the main window is excluded. */
  sameWorkdirPopOutWindows(workdir: string): BrowserWindow[];
}

export type WindowRegistrationOptions = {
  workdir?: string;
  runtimeContext?: RuntimeContext;
  threadID?: string;
  subthreadID?: string;
  activityID?: string;
};

interface WindowEntry {
  readonly window: BrowserWindow;
  readonly role: WindowRole;
  readonly workdir?: string;
  readonly runtimeContext?: RuntimeContext;
  readonly threadID?: string;
  readonly subthreadID?: string;
  readonly activityID?: string;
}

class WindowRegistryImpl implements WindowRegistry {
  private readonly windowsByID = new Map<number, WindowEntry>();
  private readonly threadToWindowID = new Map<string, number>();
  private readonly subthreadToWindowID = new Map<string, number>();
  private readonly activityToWindowID = new Map<string, number>();
  private readonly resizeCleanups = new Map<number, () => void>();

  registerWindow(
    window: BrowserWindow,
    role: WindowRole,
    options: WindowRegistrationOptions = {},
  ): void {
    this.windowsByID.set(window.webContents.id, {
      window,
      role,
      workdir: options.workdir,
      runtimeContext: options.runtimeContext,
      threadID: options.threadID,
      subthreadID: options.subthreadID,
      activityID: options.activityID,
    });
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
    for (const [subthreadID, hostedID] of this.subthreadToWindowID) {
      if (hostedID === windowID) {
        this.subthreadToWindowID.delete(subthreadID);
      }
    }
    for (const [activityID, hostedID] of this.activityToWindowID) {
      if (hostedID === windowID) {
        this.activityToWindowID.delete(activityID);
      }
    }
  }

  windowForID(windowID: number): BrowserWindow | null {
    return this.windowsByID.get(windowID)?.window ?? null;
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

  runtimeContextForWindow(windowID: number): RuntimeContext | undefined {
    return this.windowsByID.get(windowID)?.runtimeContext;
  }

  threadForWindow(windowID: number): string | undefined {
    return this.windowsByID.get(windowID)?.threadID;
  }

  popOutWindowForSubthread(subthreadID: string): BrowserWindow | null {
    const id = this.subthreadToWindowID.get(subthreadID);
    if (id === undefined) return null;
    const entry = this.windowsByID.get(id);
    return entry?.window ?? null;
  }

  setSubthreadWindow(subthreadID: string, windowID: number): void {
    this.subthreadToWindowID.set(subthreadID, windowID);
  }

  clearSubthreadWindow(subthreadID: string): void {
    this.subthreadToWindowID.delete(subthreadID);
  }

  subthreadHostWindowID(subthreadID: string): number | undefined {
    return this.subthreadToWindowID.get(subthreadID);
  }

  subthreadForWindow(windowID: number): string | undefined {
    return this.windowsByID.get(windowID)?.subthreadID;
  }

  activityWindow(activityID: string): BrowserWindow | null {
    const id = this.activityToWindowID.get(activityID);
    if (id === undefined) return null;
    return this.windowsByID.get(id)?.window ?? null;
  }

  setActivityWindow(activityID: string, windowID: number): void {
    this.activityToWindowID.set(activityID, windowID);
  }

  clearActivityWindow(activityID: string): void {
    this.activityToWindowID.delete(activityID);
  }

  activityHostWindowID(activityID: string): number | undefined {
    return this.activityToWindowID.get(activityID);
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
