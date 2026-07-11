import { BrowserWindow, nativeImage, screen } from "electron";
import type { Rectangle } from "electron";
import { fileURLToPath } from "node:url";
import type { ActivitySession, ServerEvent } from "../shared/protocol";
import { renderableFileURL } from "./renderableFileURLs";
import { CUAFrameStream, resolveCUAFrameHelper, type CUAFrameMetadata } from "./cuaFrameStreams";
import type { WindowRegistry } from "./windowRegistry";

export type ActivityControlAction = "takeover" | "release" | "stop";
export type ActivityWindowAction = ActivityControlAction | "close" | "drag-start" | "drag-end";

const SNAP_INSET = 12;
const PREVIEW_TARGET_AREA = 380 * 248;
const PREVIEW_MIN_WIDTH = 280;
const PREVIEW_MAX_WIDTH = 480;
const PREVIEW_MIN_HEIGHT = 180;
const PREVIEW_MAX_HEIGHT = 420;
const MAX_LIVE_CUA_STREAMS = 3;

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
  private readonly dockedCorners = new Map<number, { source: "main" | "screen"; corner: number }>();
  private readonly snapAnimationTokens = new Map<number, number>();
  private readonly frameStreams = new Map<string, { target: string; stream: CUAFrameStream; startedAt: number }>();
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
      if (win && !win.isDestroyed()) this.syncVisibility(win, activity);
    }
  }

  update(activity: ActivitySession): void {
    const existing = this.registry.activityWindow(activity.id);
    if (activity.state === "stopped") {
      this.stopFrameStream(activity.id);
      this.activities.delete(activity.id);
      this.dismissedActivityIDs.delete(activity.id);
      this.pendingActivities.delete(activity.id);
      this.registry.clearActivityWindow(activity.id);
      if (existing && !existing.isDestroyed()) existing.close();
      return;
    }
    this.activities.set(activity.id, activity);
    if (this.dismissedActivityIDs.has(activity.id)) return;
    if (existing && !existing.isDestroyed() && this.draggingWindowIDs.has(existing.webContents.id)) {
      this.pendingActivities.set(activity.id, activity);
      return;
    }
    const win = existing && !existing.isDestroyed()
      ? existing
      : this.createWindow(activity);
    this.syncFrameStream(activity);
    this.render(win, activity);
    this.syncVisibility(win, activity);
  }

  private syncVisibility(win: BrowserWindow, activity: ActivitySession): void {
    if (win.isDestroyed()) return;
    if (activityVisibleForThread(activity.thread_id, this.activeThreadID)) {
      if (!win.isVisible() && this.readyWindowIDs.has(win.webContents.id)) win.showInactive();
    } else if (win.isVisible()) {
      win.hide();
    }
  }

  private createWindow(activity: ActivitySession): BrowserWindow {
    const cursor = screen.getCursorScreenPoint();
    let workArea = screen.getDisplayNearestPoint(cursor).workArea;
    const width = 380;
    const height = 248;
    const mainWindow = this.registry.mainWindow();
    const mainBounds = mainWindow && !mainWindow.isDestroyed() && mainWindow.isVisible()
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
      minWidth: PREVIEW_MIN_WIDTH,
      minHeight: PREVIEW_MIN_HEIGHT,
      frame: false,
      transparent: true,
      backgroundColor: "#00000000",
      hasShadow: false,
      alwaysOnTop: true,
      skipTaskbar: true,
      show: false,
      type: "panel",
      webPreferences: {
        contextIsolation: true,
        nodeIntegration: false,
        sandbox: true,
      },
    });
    win.setAlwaysOnTop(true, "floating");
    win.setHasShadow(false);
    win.setVisibleOnAllWorkspaces(true, { visibleOnFullScreen: true });
    win.webContents.setWindowOpenHandler(() => ({ action: "deny" }));
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
    this.dockedCorners.set(windowID, { source: mainBounds ? "main" : "screen", corner: 1 });
    const syncWithMainWindow = (): void => {
      const docked = this.dockedCorners.get(windowID);
      if (docked?.source !== "main" || win.isDestroyed() || this.draggingWindowIDs.has(windowID) || this.snappingWindowIDs.has(windowID) || !mainWindow || mainWindow.isDestroyed() || !mainWindow.isVisible()) return;
      const anchor = mainWindow.getContentBounds();
      const anchorWorkArea = screen.getDisplayMatching(anchor).workArea;
      const target = cornerPositions(win.getBounds(), anchor, SNAP_INSET, anchorWorkArea)[docked.corner];
      if (!target) return;
      this.cancelSnap(windowID);
      this.setProgrammaticPosition(win, target.x, target.y);
    };
    mainWindow?.on("move", syncWithMainWindow);
    mainWindow?.on("resize", syncWithMainWindow);
    win.on("closed", () => {
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
    const anchor = docked.source === "main" && mainBounds
      ? mainBounds
      : screen.getDisplayMatching(current).workArea;
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
    this.autoSizeForPreview(win, activity);
    void win.loadURL(`data:text/html;charset=utf-8,${encodeURIComponent(cuaActivityHTML(activity))}`);
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
  }

  private publishLiveFrame(activityID: string, path: string, metadata: CUAFrameMetadata): void {
    const win = this.registry.activityWindow(activityID);
    if (!win || win.isDestroyed()) return;
    const revision = metadata.revision ?? Date.now();
    const url = `${renderableFileURL(path)}?v=${revision}`;
    if (typeof metadata.width === "number" && typeof metadata.height === "number") {
      this.autoSizeForLiveFrame(win, activityID, metadata.width, metadata.height);
    }
    void win.webContents.executeJavaScript(`window.wuuCUAFrame?.(${JSON.stringify(url)})`, true).catch(() => undefined);
  }

  private publishStreamError(activityID: string, message: string): void {
    const activity = this.activities.get(activityID);
    const win = this.registry.activityWindow(activityID);
    if (!activity || !win || win.isDestroyed()) return;
    this.render(win, { ...activity, error: message, updated_at: new Date().toISOString() });
  }

  private autoSizeForLiveFrame(win: BrowserWindow, activityID: string, width: number, height: number): void {
    const windowID = win.webContents.id;
    if (width <= 0 || height <= 0 || this.manuallyResizedWindowIDs.has(windowID) || this.draggingWindowIDs.has(windowID)) return;
    const ratio = width / height;
    const previous = this.previewState.get(activityID);
    if (previous && Math.abs(ratio / previous.ratio - 1) <= 0.05) return;
    this.previewState.set(activityID, { ratio, target: this.activities.get(activityID)?.target?.trim() ?? "" });
    const bounds = win.getBounds();
    const size = fitActivityPreviewSize(width, height);
    const resized = this.dockedBoundsForSize(win, size)
      ?? resizeActivityBounds(bounds, size, screen.getDisplayMatching(bounds).workArea);
    if (resized.width !== bounds.width || resized.height !== bounds.height) win.setBounds(resized, false);
  }

  private autoSizeForPreview(win: BrowserWindow, activity: ActivitySession): void {
    const preview = activity.preview?.trim();
    const windowID = win.webContents.id;
    if (!preview?.startsWith("file:") || this.manuallyResizedWindowIDs.has(windowID) || this.draggingWindowIDs.has(windowID)) return;
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
    this.previewState.set(activity.id, { ratio, target });
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

export function cuaActivityHTML(activity: ActivitySession): string {
  const target = escapeHTML(activity.target?.trim() || "Mac App");
  const previewURL = activityPreviewURL(activity);
  const controls = activity.controller === "user"
    ? actionLink(activity, "release", "交还 Agent", "primary")
    : activity.controller === "agent"
      ? actionLink(activity, "takeover", "接管", "primary")
      : "";
  const error = activity.error
    ? `<div class="error">${escapeHTML(activity.error)}</div>`
    : "";
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
.card{position:relative;height:100%;overflow:hidden;background:transparent;color:var(--ink);cursor:grab;user-select:none}.card.is-dragging{cursor:grabbing}
.preview{position:absolute;inset:0;display:grid;place-items:center;overflow:hidden}.preview img{width:100%;height:100%;object-fit:cover;display:block;pointer-events:none}
.glass{position:absolute;inset:0;background:radial-gradient(circle at 10% 0%,rgba(255,255,255,.62),transparent 44%),radial-gradient(circle at 92% 34%,rgba(255,122,72,.16),transparent 48%),radial-gradient(circle at 52% 112%,rgba(110,170,255,.14),transparent 50%),linear-gradient(145deg,var(--glass-strong),var(--glass));box-shadow:inset 0 1px 0 rgba(255,255,255,.5)}
.actions{position:absolute;z-index:3;top:8px;right:8px;display:flex;align-items:center;gap:5px;padding:4px;border-radius:10px;background:var(--glass-strong);border:1px solid var(--line);box-shadow:0 2px 8px rgba(0,0,0,.14);opacity:0;transform:translateY(-3px);transition:opacity 140ms ease,transform 140ms ease;-webkit-app-region:no-drag}.card:hover .actions,.actions:focus-within{opacity:1;transform:none}.button{height:25px;padding:0 8px;border-radius:7px;display:inline-flex;align-items:center;justify-content:center;text-decoration:none;color:var(--ink);background:transparent;border:0;font-size:11px;font-weight:560}.button:hover{background:var(--hover)}.button.stop{width:25px;padding:0;font-size:15px;font-weight:400}.button.stop:hover{color:var(--danger);background:var(--danger-soft)}
.error{position:absolute;z-index:2;left:8px;right:8px;bottom:8px;padding:8px 10px;border-radius:9px;background:var(--danger-soft);border:1px solid var(--line);font-size:10.5px;color:var(--danger);-webkit-app-region:no-drag}
</style></head><body><section class="card"><div class="preview">${preview}</div><div class="actions">${controls}${actionLink(activity,"close","关闭画中画","stop","×")}</div>${error}</section>
<script>
(() => {
  const card = document.querySelector('.card');
  const livePreview = document.querySelector('#live-preview');
  window.wuuCUAFrame = (url) => {
    livePreview.src = url;
    livePreview.hidden = false;
    document.querySelector('.glass')?.remove();
  };
  const activityID = ${JSON.stringify(activity.id)};
  let pointerID = null;
  let offsetX = 0;
  let offsetY = 0;
  const notify = (action) => { location.href = 'wuu-cua://action/' + action + '?activity_id=' + encodeURIComponent(activityID); };
  card.addEventListener('pointerdown', (event) => {
    if (event.button !== 0 || event.target.closest('.actions,.error')) return;
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
