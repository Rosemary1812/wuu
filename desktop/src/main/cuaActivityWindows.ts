import { BrowserWindow, nativeImage, screen } from "electron";
import type { BrowserWindowConstructorOptions, Rectangle } from "electron";
import { fileURLToPath } from "node:url";
import type { ActivitySession, ServerEvent } from "../shared/protocol";
import { renderableFileURL } from "./renderableFileURLs";
import { CUAFrameStream, resolveCUAFrameHelper, type CUAFrameMetadata } from "./cuaFrameStreams";
import type { WindowRegistry } from "./windowRegistry";

export type ActivityControlAction = "takeover" | "release" | "stop";
export type ActivityWindowAction = ActivityControlAction | "close" | "drag-start" | "drag-end";

export type ActivityDockedCorner =
  | { source: "main"; corner: number }
  | { source: "screen"; corner: number; workArea: Rectangle };

const INTERACTION_FEEDBACK_CLASSES = {
  click: "is-clicking",
  drag: "is-dragging",
  scroll: "is-scrolling",
  type: "is-typing",
} as const;

export function activityInteractionFeedbackClass(kind: string): string | undefined {
  return INTERACTION_FEEDBACK_CLASSES[kind as keyof typeof INTERACTION_FEEDBACK_CLASSES];
}

const SNAP_INSET = 12;
const PREVIEW_TARGET_AREA = 380 * 248;
const PREVIEW_MIN_WIDTH = 280;
const PREVIEW_MAX_WIDTH = 480;
const PREVIEW_MIN_HEIGHT = 180;
const PREVIEW_MAX_HEIGHT = 520;
const PREVIEW_RESIZE_MIN_WIDTH = 220;
const PREVIEW_RESIZE_MIN_HEIGHT = 140;
const PREVIEW_RESIZE_MAX_WIDTH = 720;
const PREVIEW_RESIZE_MAX_HEIGHT = 800;
const MAX_LIVE_CUA_STREAMS = 3;
const MAX_LIVE_STREAM_RETRIES = 6;

export function frameStreamRetryDelay(attempt: number): number {
  return Math.min(16_000, 1_000 * 2 ** Math.max(1, attempt));
}

export function shouldScheduleDragSettle(dragging: boolean, snapping: boolean): boolean {
  return dragging && !snapping;
}

export function snapAnimationDuration(distance: number): number {
  return Math.max(220, Math.min(420, 210 + Math.max(0, distance) * 0.24));
}

export function snapAnimationProgress(progress: number): number {
  const t = Math.max(0, Math.min(1, progress));
  return t * t * t * (t * (t * 6 - 15) + 10);
}

export function fitActivityPreviewSize(imageWidth: number, imageHeight: number): { width: number; height: number } {
  if (imageWidth <= 0 || imageHeight <= 0) {
    return { width: 380, height: 248 };
  }
  const ratio = imageWidth / imageHeight;
  let width = Math.sqrt(PREVIEW_TARGET_AREA * ratio);
  let height = width / ratio;
  if (width < PREVIEW_MIN_WIDTH) { width = PREVIEW_MIN_WIDTH; height = width / ratio; }
  if (height < PREVIEW_MIN_HEIGHT) { height = PREVIEW_MIN_HEIGHT; width = height * ratio; }
  if (width > PREVIEW_MAX_WIDTH) { width = PREVIEW_MAX_WIDTH; height = width / ratio; }
  if (height > PREVIEW_MAX_HEIGHT) { height = PREVIEW_MAX_HEIGHT; width = height * ratio; }
  return {
    width: Math.round(Math.max(PREVIEW_MIN_WIDTH, Math.min(PREVIEW_MAX_WIDTH, width))),
    height: Math.round(Math.max(PREVIEW_MIN_HEIGHT, Math.min(PREVIEW_MAX_HEIGHT, height))),
  };
}

export function resizeActivityBounds(
  bounds: Rectangle,
  size: { width: number; height: number },
  workArea: Rectangle,
): Rectangle {
  const leftDistance = Math.abs(bounds.x - workArea.x);
  const rightDistance = Math.abs(workArea.x + workArea.width - (bounds.x + bounds.width));
  const topDistance = Math.abs(bounds.y - workArea.y);
  const bottomDistance = Math.abs(workArea.y + workArea.height - (bounds.y + bounds.height));
  const desiredX = rightDistance < leftDistance ? bounds.x + bounds.width - size.width : bounds.x;
  const desiredY = bottomDistance < topDistance ? bounds.y + bounds.height - size.height : bounds.y;
  return {
    x: Math.max(workArea.x, Math.min(workArea.x + workArea.width - size.width, desiredX)),
    y: Math.max(workArea.y, Math.min(workArea.y + workArea.height - size.height, desiredY)),
    width: size.width,
    height: size.height,
  };
}

export function snapActivityBounds(
  bounds: Rectangle,
  workArea: Rectangle,
  anchors: Rectangle[],
  inset = SNAP_INSET,
): Rectangle {
  for (const anchor of anchors) {
    if (!rectanglesOverlap(anchor, workArea)) continue;
    return nearestCorner(bounds, cornerPositions(bounds, anchor, inset, workArea));
  }
  return nearestCorner(bounds, cornerPositions(bounds, workArea, inset, workArea));
}

function rectanglesOverlap(a: Rectangle, b: Rectangle): boolean {
  return a.x < b.x + b.width && a.x + a.width > b.x
    && a.y < b.y + b.height && a.y + a.height > b.y;
}

function cornerPositions(
  bounds: Rectangle,
  anchor: Rectangle,
  inset: number,
  constraint?: Rectangle,
): Array<{ x: number; y: number }> {
  const horizontalRoom = anchor.width - bounds.width - inset * 2;
  const verticalRoom = anchor.height - bounds.height - inset * 2;
  const rawLeft = horizontalRoom >= 0
    ? anchor.x + inset
    : anchor.x + (anchor.width - bounds.width) / 2;
  const rawRight = horizontalRoom >= 0
    ? anchor.x + anchor.width - bounds.width - inset
    : rawLeft;
  const rawTop = verticalRoom >= 0
    ? anchor.y + inset
    : anchor.y + (anchor.height - bounds.height) / 2;
  const rawBottom = verticalRoom >= 0
    ? anchor.y + anchor.height - bounds.height - inset
    : rawTop;
  const clampX = (value: number): number => constraint
    ? Math.max(constraint.x, Math.min(constraint.x + constraint.width - bounds.width, value))
    : value;
  const clampY = (value: number): number => constraint
    ? Math.max(constraint.y, Math.min(constraint.y + constraint.height - bounds.height, value))
    : value;
  const left = clampX(rawLeft);
  const right = clampX(rawRight);
  const top = clampY(rawTop);
  const bottom = clampY(rawBottom);
  return [
    { x: left, y: top },
    { x: right, y: top },
    { x: left, y: bottom },
    { x: right, y: bottom },
  ];
}

function nearestCorner(
  bounds: Rectangle,
  candidates: Array<{ x: number; y: number }>,
): Rectangle {
  const nearest = candidates[nearestCornerIndex(bounds, candidates)] ?? { x: bounds.x, y: bounds.y };
  return { ...bounds, ...nearest };
}

function nearestCornerIndex(
  bounds: Rectangle,
  candidates: Array<{ x: number; y: number }>,
): number {
  let nearestIndex = 0;
  let distance = Number.POSITIVE_INFINITY;
  for (const [index, candidate] of candidates.entries()) {
    const nextDistance = Math.hypot(bounds.x - candidate.x, bounds.y - candidate.y);
    if (nextDistance < distance) {
      nearestIndex = index;
      distance = nextDistance;
    }
  }
  return nearestIndex;
}

export function activityControlMethod(action: ActivityControlAction): "activity/takeover" | "activity/release" | "activity/stop" {
  return `activity/${action}`;
}

export function activityVisibleForThread(activityThreadID: string, activeThreadID?: string): boolean {
  return activeThreadID !== undefined && activityThreadID === activeThreadID;
}

export function activityWindowStackingOptions(
  parent: BrowserWindow,
): Pick<BrowserWindowConstructorOptions, "alwaysOnTop" | "parent"> {
  return {
    alwaysOnTop: false,
    parent,
  };
}

export function activityWindowResizeOptions(): Pick<
  BrowserWindowConstructorOptions,
  "minWidth" | "minHeight" | "maxWidth" | "maxHeight" | "resizable"
> {
  return {
    minWidth: PREVIEW_RESIZE_MIN_WIDTH,
    minHeight: PREVIEW_RESIZE_MIN_HEIGHT,
    maxWidth: PREVIEW_RESIZE_MAX_WIDTH,
    maxHeight: PREVIEW_RESIZE_MAX_HEIGHT,
    resizable: true,
  };
}

export function activityAspectRatio(width: number, height: number): number | undefined {
  return width > 0 && height > 0 ? width / height : undefined;
}

export function fitUserResizedPreviewSize(width: number, ratio: number): { width: number; height: number } {
  let nextWidth = Math.max(PREVIEW_RESIZE_MIN_WIDTH, Math.min(PREVIEW_RESIZE_MAX_WIDTH, width));
  let nextHeight = nextWidth / ratio;
  if (nextHeight > PREVIEW_RESIZE_MAX_HEIGHT) {
    nextHeight = PREVIEW_RESIZE_MAX_HEIGHT;
    nextWidth = nextHeight * ratio;
  }
  if (nextHeight < PREVIEW_RESIZE_MIN_HEIGHT) {
    nextHeight = PREVIEW_RESIZE_MIN_HEIGHT;
    nextWidth = nextHeight * ratio;
  }
  return { width: Math.round(nextWidth), height: Math.round(nextHeight) };
}

export function activityWindowCanCreate(mainWindow: BrowserWindow | null): mainWindow is BrowserWindow {
  return mainWindow !== null && !mainWindow.isDestroyed();
}

export function activityDockAnchor(
  docked: ActivityDockedCorner,
  mainBounds: Rectangle | undefined,
  fallbackWorkArea: Rectangle,
): Rectangle {
  if (docked.source === "screen") return docked.workArea;
  return mainBounds ?? fallbackWorkArea;
}

type ActivityControl = (
  activity: ActivitySession,
  action: ActivityControlAction,
) => Promise<ActivitySession>;

export class CUAActivityWindowManager {
  private readonly activities = new Map<string, ActivitySession>();
  private readonly manuallyResizedWindowIDs = new Set<number>();
  private readonly previewState = new Map<string, { ratio: number; target: string }>();
  private readonly snappingWindowIDs = new Set<number>();
  private readonly draggingWindowIDs = new Set<number>();
  private readonly readyWindowIDs = new Set<number>();
  private readonly dismissedActivityIDs = new Set<string>();
  private readonly pendingActivities = new Map<string, ActivitySession>();
  private readonly dockedCorners = new Map<number, ActivityDockedCorner>();
  private readonly snapAnimationTokens = new Map<number, number>();
  private readonly frameStreams = new Map<string, { target: string; stream: CUAFrameStream; startedAt: number }>();
  private readonly frameStreamRetries = new Map<string, { attempts: number; timer?: NodeJS.Timeout }>();
  private readonly loadStartedWindowIDs = new Set<number>();
  private readonly loadedWindowIDs = new Set<number>();
  private readonly pendingRenderActivities = new Map<number, ActivitySession>();
  private readonly renderedSignatures = new Map<string, string>();
  private readonly visualReadyActivityIDs = new Set<string>();
  private activeThreadID: string | undefined;

  constructor(
    private readonly registry: WindowRegistry,
    private readonly control: ActivityControl,
  ) {}

  handleServerEvent(event: ServerEvent): void {
    const activity = cuaActivityFromServerEvent(event);
    if (!activity) return;
    this.update(activity);
  }

  setActiveThread(threadID?: string): void {
    this.activeThreadID = threadID?.trim() || undefined;
    for (const [activityID, activity] of this.activities) {
      const win = this.registry.activityWindow(activityID);
      if (win && !win.isDestroyed()) {
        if (activityVisibleForThread(activity.thread_id, this.activeThreadID)) {
          this.syncFrameStream(activity);
        } else {
          this.stopFrameStream(activityID);
        }
        this.syncVisibility(win, activity);
      } else if (activityVisibleForThread(activity.thread_id, this.activeThreadID)) {
        this.update(activity);
      }
    }
  }

  update(activity: ActivitySession): void {
    const existing = this.registry.activityWindow(activity.id);
    if (activity.state === "stopped") {
      this.stopFrameStream(activity.id);
      this.activities.delete(activity.id);
      this.dismissedActivityIDs.delete(activity.id);
      this.pendingActivities.delete(activity.id);
      this.renderedSignatures.delete(activity.id);
      this.visualReadyActivityIDs.delete(activity.id);
      this.registry.clearActivityWindow(activity.id);
      if (existing && !existing.isDestroyed()) existing.close();
      return;
    }
    this.activities.set(activity.id, activity);
    if (activityHasVisibleContent(activity)) this.visualReadyActivityIDs.add(activity.id);
    if (this.dismissedActivityIDs.has(activity.id)) return;
    if (existing && !existing.isDestroyed() && this.draggingWindowIDs.has(existing.webContents.id)) {
      this.pendingActivities.set(activity.id, activity);
      return;
    }
    let win = existing && !existing.isDestroyed() ? existing : undefined;
    if (!win) {
      const mainWindow = this.registry.mainWindow();
      if (!activityWindowCanCreate(mainWindow)) return;
      win = this.createWindow(activity, mainWindow);
    }
    if (activityVisibleForThread(activity.thread_id, this.activeThreadID)) {
      this.syncFrameStream(activity);
    } else {
      this.stopFrameStream(activity.id);
    }
    this.render(win, activity);
    this.syncVisibility(win, activity);
  }

  private syncVisibility(win: BrowserWindow, activity: ActivitySession): void {
    if (win.isDestroyed()) return;
    const showable = activityVisibleForThread(activity.thread_id, this.activeThreadID)
      && this.visualReadyActivityIDs.has(activity.id);
    if (showable) {
      if (!win.isVisible() && this.readyWindowIDs.has(win.webContents.id)) win.showInactive();
    } else if (win.isVisible()) {
      win.hide();
    }
  }

  private createWindow(activity: ActivitySession, mainWindow: BrowserWindow): BrowserWindow {
    const cursor = screen.getCursorScreenPoint();
    let workArea = screen.getDisplayNearestPoint(cursor).workArea;
    const width = 380;
    const height = 248;
    const mainBounds = mainWindow.isVisible()
      ? mainWindow.getContentBounds()
      : undefined;
    if (mainBounds) workArea = screen.getDisplayMatching(mainBounds).workArea;
    const initialBounds = mainBounds
      ? cornerPositions({ x: 0, y: 0, width, height }, mainBounds, SNAP_INSET, workArea)[1]
      : { x: workArea.x + workArea.width - width - 24, y: workArea.y + 24 };
    const win = new BrowserWindow({
      width,
      height,
      x: initialBounds.x,
      y: initialBounds.y,
      ...activityWindowResizeOptions(),
      frame: false,
      transparent: true,
      backgroundColor: "#00000000",
      hasShadow: false,
      ...activityWindowStackingOptions(mainWindow),
      skipTaskbar: true,
      show: false,
      webPreferences: {
        contextIsolation: true,
        nodeIntegration: false,
        sandbox: true,
      },
    });
    win.setHasShadow(false);
    win.webContents.setWindowOpenHandler(() => ({ action: "deny" }));
    win.webContents.on("did-finish-load", () => {
      if (win.isDestroyed()) return;
      this.loadedWindowIDs.add(win.webContents.id);
      const pending = this.pendingRenderActivities.get(win.webContents.id);
      this.pendingRenderActivities.delete(win.webContents.id);
      if (pending) this.applyViewState(win, pending);
    });
    win.on("will-resize", () => {
      this.manuallyResizedWindowIDs.add(win.webContents.id);
    });
    win.on("resized", () => {
      if (this.draggingWindowIDs.has(win.webContents.id)) return;
      const docked = this.dockedBoundsForSize(win, win.getBounds());
      if (docked) this.setProgrammaticPosition(win, docked.x, docked.y);
    });
    win.webContents.on("will-navigate", (navigationEvent, rawURL) => {
      const parsed = activityActionFromURL(rawURL);
      if (!parsed || parsed.activityID !== activity.id) return;
      navigationEvent.preventDefault();
      if (parsed.action === "drag-start") {
        this.cancelSnap(win.webContents.id);
        this.draggingWindowIDs.add(win.webContents.id);
        return;
      }
      if (parsed.action === "drag-end") {
        this.finishUserDrag(win);
        const pending = this.pendingActivities.get(activity.id);
        if (pending) {
          this.pendingActivities.delete(activity.id);
          this.render(win, pending);
        }
        return;
      }
      if (parsed.action === "close") {
        this.dismissedActivityIDs.add(activity.id);
        this.stopFrameStream(activity.id);
        this.registry.clearActivityWindow(activity.id);
        win.close();
        return;
      }
      void this.control(activity, parsed.action)
        .then((updated) => this.update(updated))
        .catch((error) => {
          this.render(win, {
            ...activity,
            state: "error",
            error: error instanceof Error ? error.message : String(error),
            updated_at: new Date().toISOString(),
          });
        });
    });
    const windowID = win.webContents.id;
    this.registry.registerWindow(win, "activity", {
      workdir: activity.workdir,
      threadID: activity.thread_id,
      activityID: activity.id,
    });
    this.registry.setActivityWindow(activity.id, windowID);
    this.dockedCorners.set(windowID, mainBounds
      ? { source: "main", corner: 1 }
      : { source: "screen", corner: 1, workArea });
    const syncWithMainWindow = (): void => {
      const docked = this.dockedCorners.get(windowID);
      if (!docked || win.isDestroyed() || this.draggingWindowIDs.has(windowID) || this.snappingWindowIDs.has(windowID)) return;
      if (docked.source === "main" && (!mainWindow || mainWindow.isDestroyed() || !mainWindow.isVisible())) return;
      const anchor = docked.source === "main" ? mainWindow!.getContentBounds() : docked.workArea;
      const anchorWorkArea = screen.getDisplayMatching(anchor).workArea;
      const target = cornerPositions(win.getBounds(), anchor, SNAP_INSET, anchorWorkArea)[docked.corner];
      if (!target) return;
      this.cancelSnap(windowID);
      this.setProgrammaticPosition(win, target.x, target.y);
    };
    mainWindow?.on("move", syncWithMainWindow);
    mainWindow?.on("resize", syncWithMainWindow);
    win.on("closed", () => {
      this.stopFrameStream(activity.id);
      mainWindow?.removeListener("move", syncWithMainWindow);
      mainWindow?.removeListener("resize", syncWithMainWindow);
      this.manuallyResizedWindowIDs.delete(windowID);
      this.draggingWindowIDs.delete(windowID);
      this.readyWindowIDs.delete(windowID);
      this.snappingWindowIDs.delete(windowID);
      this.pendingActivities.delete(activity.id);
      this.dockedCorners.delete(windowID);
      this.snapAnimationTokens.delete(windowID);
      this.previewState.delete(activity.id);
      this.loadStartedWindowIDs.delete(windowID);
      this.loadedWindowIDs.delete(windowID);
      this.pendingRenderActivities.delete(windowID);
      this.renderedSignatures.delete(activity.id);
      this.visualReadyActivityIDs.delete(activity.id);
      this.registry.unregisterWindow(windowID);
    });
    win.once("ready-to-show", () => {
      if (win.isDestroyed()) return;
      this.readyWindowIDs.add(windowID);
      this.syncVisibility(win, activity);
    });
    return win;
  }

  private finishUserDrag(win: BrowserWindow): void {
    const windowID = win.webContents.id;
    if (win.isDestroyed() || this.snappingWindowIDs.has(windowID) || !this.draggingWindowIDs.has(windowID)) return;
    this.draggingWindowIDs.delete(windowID);
    const bounds = win.getBounds();
    const workArea = screen.getDisplayMatching(bounds).workArea;
    const mainWindow = this.registry.mainWindow();
    const anchors = mainWindow && mainWindow !== win && !mainWindow.isDestroyed() && mainWindow.isVisible()
      ? [mainWindow.getContentBounds()]
      : [];
    const snapped = snapActivityBounds(bounds, workArea, anchors);
    if (anchors.length > 0 && rectanglesOverlap(anchors[0], workArea)) {
      this.dockedCorners.set(
        windowID,
        {
          source: "main",
          corner: nearestCornerIndex(bounds, cornerPositions(bounds, anchors[0], SNAP_INSET, workArea)),
        },
      );
    } else {
      this.dockedCorners.set(windowID, {
        source: "screen",
        corner: nearestCornerIndex(bounds, cornerPositions(bounds, workArea, SNAP_INSET, workArea)),
        workArea,
      });
    }
    if (snapped.x !== bounds.x || snapped.y !== bounds.y) {
      this.animateSnap(win, bounds, snapped);
    }
  }

  private animateSnap(win: BrowserWindow, from: Rectangle, to: Rectangle): void {
    const windowID = win.webContents.id;
    const token = (this.snapAnimationTokens.get(windowID) ?? 0) + 1;
    this.snapAnimationTokens.set(windowID, token);
    this.snappingWindowIDs.add(windowID);
    const startedAt = performance.now();
    const distance = Math.hypot(to.x - from.x, to.y - from.y);
    const duration = snapAnimationDuration(distance);
    const step = (): void => {
      if (win.isDestroyed()) {
        this.snappingWindowIDs.delete(windowID);
        return;
      }
      if (this.snapAnimationTokens.get(windowID) !== token) return;
      const progress = Math.min(1, (performance.now() - startedAt) / duration);
      const eased = snapAnimationProgress(progress);
      win.setPosition(
        Math.round(from.x + (to.x - from.x) * eased),
        Math.round(from.y + (to.y - from.y) * eased),
        false,
      );
      if (progress < 1) {
        setTimeout(step, 16);
      } else {
        this.snappingWindowIDs.delete(windowID);
      }
    };
    step();
  }

  private setProgrammaticPosition(win: BrowserWindow, x: number, y: number): void {
    const windowID = win.webContents.id;
    this.snappingWindowIDs.add(windowID);
    win.setPosition(x, y, false);
    setImmediate(() => this.snappingWindowIDs.delete(windowID));
  }

  private cancelSnap(windowID: number): void {
    this.snapAnimationTokens.set(windowID, (this.snapAnimationTokens.get(windowID) ?? 0) + 1);
    this.snappingWindowIDs.delete(windowID);
  }

  private dockedBoundsForSize(win: BrowserWindow, size: { width: number; height: number }): Rectangle | undefined {
    const docked = this.dockedCorners.get(win.webContents.id);
    if (!docked) return undefined;
    const current = win.getBounds();
    const mainWindow = this.registry.mainWindow();
    const mainBounds = mainWindow && !mainWindow.isDestroyed() && mainWindow.isVisible()
      ? mainWindow.getContentBounds()
      : undefined;
    const anchor = activityDockAnchor(
      docked,
      mainBounds,
      screen.getDisplayMatching(current).workArea,
    );
    const workArea = screen.getDisplayMatching(anchor).workArea;
    const target = cornerPositions(
      { ...current, width: size.width, height: size.height },
      anchor,
      SNAP_INSET,
      workArea,
    )[docked.corner];
    return target ? { ...current, ...target, width: size.width, height: size.height } : undefined;
  }

  private render(win: BrowserWindow, activity: ActivitySession): void {
    if (win.isDestroyed()) return;
    const signature = activityRenderSignature(activity, this.frameStreams.get(activity.id)?.stream.isLive() ?? false);
    if (this.renderedSignatures.get(activity.id) === signature) return;
    this.renderedSignatures.set(activity.id, signature);
    this.autoSizeForPreview(win, activity);
    const windowID = win.webContents.id;
    if (this.loadedWindowIDs.has(windowID)) {
      this.applyViewState(win, activity);
      return;
    }
    if (this.loadStartedWindowIDs.has(windowID)) {
      this.pendingRenderActivities.set(windowID, activity);
      return;
    }
    this.loadStartedWindowIDs.add(windowID);
    void win.loadURL(`data:text/html;charset=utf-8,${encodeURIComponent(cuaActivityHTML(activity))}`);
  }

  private applyViewState(win: BrowserWindow, activity: ActivitySession): void {
    const payload = JSON.stringify(activityViewState(activity));
    void win.webContents.executeJavaScript(`window.wuuCUAActivity?.(${payload})`, true).catch(() => undefined);
  }

  private syncFrameStream(activity: ActivitySession): void {
    if (activity.plugin_id !== "cua-mac") return;
    const target = activity.target?.trim();
    const helper = resolveCUAFrameHelper();
    if (!target || !helper) return;
    const current = this.frameStreams.get(activity.id);
    if (current?.target === target) return;
    this.stopFrameStream(activity.id);
    if (this.frameStreams.size >= MAX_LIVE_CUA_STREAMS) {
      const oldest = [...this.frameStreams.entries()].sort((left, right) => left[1].startedAt - right[1].startedAt)[0];
      if (oldest) this.stopFrameStream(oldest[0]);
    }
    const stream = new CUAFrameStream(
      helper,
      activity.id,
      target,
      (path, metadata) => this.publishLiveFrame(activity.id, path, metadata),
      (message) => this.publishStreamError(activity.id, message),
      () => this.handleDetectedUserInput(activity.id),
    );
    this.frameStreams.set(activity.id, { target, stream, startedAt: Date.now() });
    stream.start();
  }

  private handleDetectedUserInput(activityID: string): void {
    const activity = this.activities.get(activityID);
    if (!activity || activity.controller !== "agent") return;
    void this.control(activity, "takeover")
      .then((updated) => this.update(updated))
      .catch(() => undefined);
  }

  private stopFrameStream(activityID: string): void {
    this.frameStreams.get(activityID)?.stream.stop();
    this.frameStreams.delete(activityID);
    const retry = this.frameStreamRetries.get(activityID);
    if (retry?.timer) clearTimeout(retry.timer);
    this.frameStreamRetries.delete(activityID);
  }

  private scheduleFrameStreamRetry(activityID: string): boolean {
    const current = this.frameStreams.get(activityID);
    if (!current || current.stream.isLive() || !this.activities.has(activityID)) return false;
    const retry = this.frameStreamRetries.get(activityID) ?? { attempts: 0 };
    if (retry.timer || retry.attempts >= MAX_LIVE_STREAM_RETRIES) return false;
    retry.attempts += 1;
    retry.timer = setTimeout(() => {
      retry.timer = undefined;
      const activity = this.activities.get(activityID);
      const entry = this.frameStreams.get(activityID);
      if (!activity || !entry || entry.stream.isLive()) return;
      this.frameStreams.delete(activityID);
      this.syncFrameStream(activity);
    }, frameStreamRetryDelay(retry.attempts));
    retry.timer.unref?.();
    this.frameStreamRetries.set(activityID, retry);
    return true;
  }

  private publishLiveFrame(activityID: string, path: string, metadata: CUAFrameMetadata): void {
    const retry = this.frameStreamRetries.get(activityID);
    if (retry && !retry.timer) this.frameStreamRetries.delete(activityID);
    const win = this.registry.activityWindow(activityID);
    if (!win || win.isDestroyed()) return;
    if (!this.visualReadyActivityIDs.has(activityID)) {
      this.visualReadyActivityIDs.add(activityID);
      const activity = this.activities.get(activityID);
      if (activity) this.syncVisibility(win, activity);
    }
    const revision = metadata.revision ?? Date.now();
    const url = `${renderableFileURL(path)}?v=${revision}`;
    if (typeof metadata.width === "number" && typeof metadata.height === "number") {
      this.autoSizeForLiveFrame(win, activityID, metadata.width, metadata.height);
    }
    void win.webContents.executeJavaScript(
      `window.wuuCUAFrame?.(${JSON.stringify(url)}, ${JSON.stringify(metadata.capture_mode ?? "full_window")})`,
      true,
    ).catch(() => undefined);
  }

  private publishStreamError(activityID: string, _message: string): void {
    if (this.scheduleFrameStreamRetry(activityID)) return;
    const win = this.registry.activityWindow(activityID);
    if (!win || win.isDestroyed()) return;
    void win.webContents.executeJavaScript("window.wuuCUAStreamUnavailable?.()", true).catch(() => undefined);
  }

  private autoSizeForLiveFrame(win: BrowserWindow, activityID: string, width: number, height: number): void {
    const windowID = win.webContents.id;
    const ratio = activityAspectRatio(width, height);
    if (!ratio) return;
    const previous = this.previewState.get(activityID);
    if (previous && Math.abs(ratio / previous.ratio - 1) <= 0.05) return;
    win.setAspectRatio(ratio);
    if (this.draggingWindowIDs.has(windowID)) return;
    this.previewState.set(activityID, { ratio, target: this.activities.get(activityID)?.target?.trim() ?? "" });
    if (this.manuallyResizedWindowIDs.has(windowID)) {
      const bounds = win.getBounds();
      const size = fitUserResizedPreviewSize(bounds.width, ratio);
      const resized = this.dockedBoundsForSize(win, size)
        ?? resizeActivityBounds(bounds, size, screen.getDisplayMatching(bounds).workArea);
      if (resized.width !== bounds.width || resized.height !== bounds.height) win.setBounds(resized, false);
      return;
    }
    const bounds = win.getBounds();
    const size = fitActivityPreviewSize(width, height);
    const resized = this.dockedBoundsForSize(win, size)
      ?? resizeActivityBounds(bounds, size, screen.getDisplayMatching(bounds).workArea);
    if (resized.width !== bounds.width || resized.height !== bounds.height) win.setBounds(resized, false);
  }

  private autoSizeForPreview(win: BrowserWindow, activity: ActivitySession): void {
    const preview = activity.preview?.trim();
    const windowID = win.webContents.id;
    if (!preview?.startsWith("file:")) return;
    let imagePath: string;
    try {
      imagePath = fileURLToPath(preview);
    } catch {
      return;
    }
    const imageSize = nativeImage.createFromPath(imagePath).getSize();
    if (imageSize.width <= 0 || imageSize.height <= 0) return;
    const ratio = imageSize.width / imageSize.height;
    const target = activity.target?.trim() ?? "";
    const previous = this.previewState.get(activity.id);
    const ratioChanged = !previous || Math.abs(ratio / previous.ratio - 1) > 0.05;
    if (!ratioChanged && previous.target === target) return;
    win.setAspectRatio(ratio);
    if (this.draggingWindowIDs.has(windowID)) return;
    this.previewState.set(activity.id, { ratio, target });
    if (this.manuallyResizedWindowIDs.has(windowID)) {
      const bounds = win.getBounds();
      const size = fitUserResizedPreviewSize(bounds.width, ratio);
      const resized = this.dockedBoundsForSize(win, size)
        ?? resizeActivityBounds(bounds, size, screen.getDisplayMatching(bounds).workArea);
      if (resized.width !== bounds.width || resized.height !== bounds.height) win.setBounds(resized, false);
      return;
    }
    const bounds = win.getBounds();
    const workArea = screen.getDisplayMatching(bounds).workArea;
    const size = fitActivityPreviewSize(imageSize.width, imageSize.height);
    const resized = this.dockedBoundsForSize(win, size)
      ?? resizeActivityBounds(bounds, size, workArea);
    if (resized.width !== bounds.width || resized.height !== bounds.height) {
      win.setBounds(resized, false);
    }
  }
}

export function cuaActivityFromServerEvent(event: ServerEvent): ActivitySession | undefined {
  if (event.kind !== "notification" || !event.message.method.startsWith("activity/")) {
    return undefined;
  }
  const value = event.message.params;
  if (!isRecord(value) || value.kind !== "cua") return undefined;
  for (const field of ["id", "thread_id", "workdir", "state", "controller", "created_at", "updated_at"] as const) {
    if (typeof value[field] !== "string" || value[field].trim() === "") return undefined;
  }
  return value as ActivitySession;
}

export function activityActionFromURL(rawURL: string): { action: ActivityWindowAction; activityID: string } | undefined {
  try {
    const url = new URL(rawURL);
    if (url.protocol !== "wuu-cua:" || url.hostname !== "action") return undefined;
    const action = url.pathname.replace(/^\/+/, "") as ActivityWindowAction;
    if (!(["takeover", "release", "stop", "close", "drag-start", "drag-end"] as const).includes(action)) return undefined;
    const activityID = url.searchParams.get("activity_id")?.trim() ?? "";
    return activityID ? { action, activityID } : undefined;
  } catch {
    return undefined;
  }
}

export function activityHasVisibleContent(activity: ActivitySession): boolean {
  return activityPreviewURL(activity) !== undefined;
}

export function activityRenderSignature(activity: ActivitySession, hasLiveStream: boolean): string {
  return JSON.stringify([
    activity.state,
    activity.controller,
    activity.interaction?.revision ?? 0,
    activity.target?.trim() ?? "",
    activity.preview?.trim() ?? "",
    hasLiveStream ? "" : activity.updated_at,
  ]);
}

export function activityViewState(activity: ActivitySession): {
  actionsHTML: string;
  previewURL: string;
  target: string;
  interaction?: ActivitySession["interaction"];
} {
  return {
    actionsHTML: activityActionsHTML(activity),
    previewURL: activityPreviewURL(activity) ?? "",
    target: activity.target?.trim() ?? "",
    interaction: activity.interaction,
  };
}

export function activityActionsHTML(activity: ActivitySession): string {
  const controls = activity.controller === "user"
    ? actionLink(activity, "release", "交还 Agent", "primary")
    : activity.controller === "agent"
      ? actionLink(activity, "takeover", "接管", "primary")
      : "";
  return `${controls}${actionLink(activity, "close", "关闭画中画", "stop", "×")}`;
}

export function cuaActivityHTML(activity: ActivitySession): string {
  const target = escapeHTML(activity.target?.trim() || "Mac App");
  const previewURL = activityPreviewURL(activity);
  const streamStatus = `<div class="stream-status" hidden>实时画面暂不可用</div>`;
  const agentPointer = `<div class="agent-pointer" aria-hidden="true"><svg viewBox="0 0 24 30"><path d="M3 2.5v21.2l5.4-5.1 3.4 8.2 4.1-1.8-3.5-8.1 7.3-.2L3 2.5Z" /></svg><i></i></div>`;
  const preview = previewURL
    ? `<img id="live-preview" src="${escapeHTML(previewURL)}" alt="${target} 实时画面" />`
    : `<div class="glass" role="status" aria-label="正在获取画面"></div><img id="live-preview" hidden alt="${target} 实时画面" />`;
  return `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8" />
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src wuu-file:; style-src 'unsafe-inline'; script-src 'unsafe-inline'; navigate-to wuu-cua:" />
<meta name="viewport" content="width=device-width,initial-scale=1" />
<style>
:root{color-scheme:light dark;font-family:-apple-system,BlinkMacSystemFont,"SF Pro Text",sans-serif;--ink:#111315;--line:rgba(255,255,255,.64);--glass:rgba(244,246,247,.56);--glass-strong:rgba(255,255,255,.82);--hover:rgba(31,35,40,.08);--danger:#b42318;--danger-soft:rgba(255,240,239,.92)}
@media(prefers-color-scheme:dark){:root{--ink:#f2f3f4;--line:rgba(255,255,255,.2);--glass:rgba(31,35,40,.58);--glass-strong:rgba(42,46,51,.88);--hover:rgba(228,232,235,.12);--danger:#f0705f;--danger-soft:rgba(52,28,24,.92)}}
*{box-sizing:border-box}html,body{width:100%;height:100%;margin:0;background:transparent;overflow:hidden}
.card{position:relative;height:100%;overflow:hidden;border-radius:clamp(10px,3.2vw,16px);background:transparent;color:var(--ink);cursor:grab;user-select:none}.card.is-dragging{cursor:grabbing}
.preview{position:absolute;inset:0;display:grid;place-items:center;overflow:hidden;border-radius:inherit}.preview img{width:100%;height:100%;object-fit:contain;display:block;pointer-events:none}
.glass{position:absolute;inset:0;background:radial-gradient(circle at 10% 0%,rgba(255,255,255,.62),transparent 44%),radial-gradient(circle at 92% 34%,rgba(255,122,72,.16),transparent 48%),radial-gradient(circle at 52% 112%,rgba(110,170,255,.14),transparent 50%),linear-gradient(145deg,var(--glass-strong),var(--glass));box-shadow:inset 0 1px 0 rgba(255,255,255,.5)}
.actions{position:absolute;z-index:3;top:8px;right:8px;display:flex;align-items:center;gap:5px;padding:4px;border-radius:10px;background:var(--glass-strong);border:1px solid var(--line);box-shadow:0 2px 8px rgba(0,0,0,.14);opacity:0;transform:translateY(-3px);transition:opacity 140ms ease,transform 140ms ease;-webkit-app-region:no-drag}.card:hover .actions,.actions:focus-within{opacity:1;transform:none}.button{height:25px;padding:0 8px;border-radius:7px;display:inline-flex;align-items:center;justify-content:center;text-decoration:none;color:var(--ink);background:transparent;border:0;font-size:11px;font-weight:560}.button:hover{background:var(--hover)}.button.stop{width:25px;padding:0;font-size:15px;font-weight:400}.button.stop:hover{color:var(--danger);background:var(--danger-soft)}
.stream-status{position:absolute;z-index:2;left:50%;bottom:10px;transform:translateX(-50%);padding:6px 9px;border-radius:8px;background:rgba(30,32,35,.76);color:#fff;font-size:10.5px;white-space:nowrap;-webkit-app-region:no-drag}
.agent-pointer{position:absolute;z-index:4;left:50%;top:50%;width:24px;height:30px;opacity:1;pointer-events:none;filter:drop-shadow(0 3px 5px rgba(0,0,0,.32));transform:translate(-3px,-2px)}.agent-pointer svg{display:block;width:24px;height:30px;overflow:visible}.agent-pointer path{fill:#fff;stroke:#f05a28;stroke-width:2;stroke-linejoin:round}.agent-pointer i{position:absolute;left:3px;top:3px;width:10px;height:10px;border:2px solid rgba(240,90,40,.9);border-radius:50%;opacity:0}.agent-pointer.is-clicking i{animation:agent-click 480ms cubic-bezier(.16,1,.3,1)}.agent-pointer.is-scrolling svg{animation:agent-scroll 520ms ease-in-out}.agent-pointer.is-typing i{opacity:.9;width:3px;height:15px;border:0;border-radius:2px;background:#f05a28;animation:agent-type 620ms ease-in-out}.agent-pointer.is-dragging svg{animation:agent-drag 420ms ease-in-out}@keyframes agent-click{0%{opacity:.9;transform:scale(.35)}100%{opacity:0;transform:scale(3.2)}}@keyframes agent-scroll{0%,100%{transform:translateY(0)}45%{transform:translateY(-7px)}}@keyframes agent-type{0%,100%{opacity:.25;transform:scaleY(.72)}50%{opacity:1;transform:scaleY(1)}}@keyframes agent-drag{0%,100%{transform:scale(1)}45%{transform:scale(.82)}}
</style></head><body><section class="card"><div class="preview">${preview}</div><div class="actions">${activityActionsHTML(activity)}</div>${streamStatus}${agentPointer}</section>
<script>
(() => {
  const card = document.querySelector('.card');
  const livePreview = document.querySelector('#live-preview');
  const actions = document.querySelector('.actions');
  const streamStatus = document.querySelector('.stream-status');
  const agentPointer = document.querySelector('.agent-pointer');
  const interactionFeedbackClasses = ${JSON.stringify(INTERACTION_FEEDBACK_CLASSES)};
  let lastLiveFrameAt = 0;
  let lastInteractionRevision = 0;
  let lastTarget = ${JSON.stringify(activity.target?.trim() ?? "")};
  let pointerPosition = { x: .5, y: .5 };
  const mapPoint = (point) => {
    const imageWidth = livePreview.naturalWidth || innerWidth;
    const imageHeight = livePreview.naturalHeight || innerHeight;
    const scale = Math.min(innerWidth / imageWidth, innerHeight / imageHeight);
    const renderedWidth = imageWidth * scale;
    const renderedHeight = imageHeight * scale;
    return {
      x: (innerWidth - renderedWidth) / 2 + point.x * renderedWidth,
      y: (innerHeight - renderedHeight) / 2 + point.y * renderedHeight,
    };
  };
  const placePointer = () => {
    const point = mapPoint(pointerPosition);
    agentPointer.style.left = point.x + 'px';
    agentPointer.style.top = point.y + 'px';
  };
  addEventListener('resize', placePointer);
  livePreview.addEventListener('load', placePointer);
  window.wuuCUAFrame = (url, captureMode) => {
    lastLiveFrameAt = Date.now();
    livePreview.src = url;
    livePreview.hidden = false;
    streamStatus.textContent = captureMode === 'visible_fallback' ? '当前仅显示屏幕内区域' : '实时画面暂不可用';
    streamStatus.hidden = captureMode !== 'visible_fallback';
    document.querySelector('.glass')?.remove();
  };
  window.wuuCUAStreamUnavailable = () => {
    streamStatus.hidden = false;
  };
  window.wuuCUAActivity = (state) => {
    actions.innerHTML = state.actionsHTML;
    if (state.target !== lastTarget) {
      lastTarget = state.target;
      lastLiveFrameAt = 0;
      livePreview.hidden = true;
      livePreview.removeAttribute('src');
      streamStatus.hidden = true;
      if (!document.querySelector('.glass')) {
        const glass = document.createElement('div');
        glass.className = 'glass';
        glass.setAttribute('role', 'status');
        glass.setAttribute('aria-label', '正在获取画面');
        document.querySelector('.preview').prepend(glass);
      }
      pointerPosition = { x: .5, y: .5 };
      agentPointer.style.left = '50%';
      agentPointer.style.top = '50%';
    }
    if (state.previewURL && Date.now() - lastLiveFrameAt > 2000) {
      livePreview.src = state.previewURL;
      livePreview.hidden = false;
      document.querySelector('.glass')?.remove();
    }
    const interaction = state.interaction;
    if (interaction && interaction.revision !== lastInteractionRevision) {
      lastInteractionRevision = interaction.revision;
      const destination = {
        x: Math.max(0, Math.min(1, interaction.to_x ?? interaction.x)),
        y: Math.max(0, Math.min(1, interaction.to_y ?? interaction.y)),
      };
      agentPointer.getAnimations().forEach((animation) => animation.cancel());
      agentPointer.classList.remove('is-clicking', 'is-scrolling', 'is-typing', 'is-dragging');
      agentPointer.style.opacity = '1';
      const from = mapPoint(pointerPosition);
      const to = mapPoint(destination);
      const entry = mapPoint({ x: interaction.x, y: interaction.y });
      const fromX = from.x;
      const fromY = from.y;
      const toX = to.x;
      const toY = to.y;
      const bend = Math.max(12, Math.min(52, Math.hypot(toX - fromX, toY - fromY) * .18));
      const animation = agentPointer.animate([
        { left: fromX + 'px', top: fromY + 'px', opacity: .35 },
        { left: (interaction.kind === 'drag' ? entry.x : (fromX + toX) / 2 + bend) + 'px', top: (interaction.kind === 'drag' ? entry.y : (fromY + toY) / 2 - bend) + 'px', opacity: 1, offset: .52 },
        { left: toX + 'px', top: toY + 'px', opacity: 1 },
      ], { duration: 520, easing: 'cubic-bezier(.22,.8,.24,1)', fill: 'forwards' });
      pointerPosition = destination;
      animation.onfinish = () => {
        agentPointer.style.left = toX + 'px';
        agentPointer.style.top = toY + 'px';
        const feedbackClass = interactionFeedbackClasses[interaction.kind];
        if (feedbackClass) agentPointer.classList.add(feedbackClass);
        setTimeout(() => {
          agentPointer.classList.remove('is-clicking', 'is-scrolling', 'is-typing', 'is-dragging');
        }, 700);
      };
    }
  };
  const activityID = ${JSON.stringify(activity.id)};
  let pointerID = null;
  let offsetX = 0;
  let offsetY = 0;
  const notify = (action) => { location.href = 'wuu-cua://action/' + action + '?activity_id=' + encodeURIComponent(activityID); };
  card.addEventListener('pointerdown', (event) => {
    if (event.button !== 0 || event.target.closest('.actions')) return;
    pointerID = event.pointerId;
    offsetX = event.screenX - window.screenX;
    offsetY = event.screenY - window.screenY;
    card.setPointerCapture(pointerID);
    card.classList.add('is-dragging');
    notify('drag-start');
    event.preventDefault();
  });
  card.addEventListener('pointermove', (event) => {
    if (event.pointerId !== pointerID) return;
    window.moveTo(Math.round(event.screenX - offsetX), Math.round(event.screenY - offsetY));
  });
  const finish = (event) => {
    if (event.pointerId !== pointerID) return;
    pointerID = null;
    card.classList.remove('is-dragging');
    notify('drag-end');
  };
  card.addEventListener('pointerup', finish);
  card.addEventListener('pointercancel', finish);
})();
</script></body></html>`;
}

function activityPreviewURL(activity: ActivitySession): string | undefined {
  const preview = activity.preview?.trim();
  if (!preview?.startsWith("file:")) return undefined;
  try {
    return `${renderableFileURL(fileURLToPath(preview))}?v=${encodeURIComponent(activity.updated_at)}`;
  } catch {
    return undefined;
  }
}

function actionLink(activity: ActivitySession, action: ActivityWindowAction, label: string, extraClass = "", visibleLabel = label): string {
  const href = `wuu-cua://action/${action}?activity_id=${encodeURIComponent(activity.id)}`;
  return `<a class="button ${extraClass}" href="${escapeHTML(href)}" aria-label="${escapeHTML(label)}">${escapeHTML(visibleLabel)}</a>`;
}

function escapeHTML(value: string): string {
  return value.replace(/[&<>"']/g, (character) => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
  })[character] ?? character);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
