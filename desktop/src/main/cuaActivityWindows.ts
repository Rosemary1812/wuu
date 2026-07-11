import { BrowserWindow, nativeImage, screen } from "electron";
import type { Rectangle } from "electron";
import { fileURLToPath } from "node:url";
import type { ActivitySession, ServerEvent } from "../shared/protocol";
import { renderableFileURL } from "./renderableFileURLs";
import type { WindowRegistry } from "./windowRegistry";

export type ActivityWindowAction = "takeover" | "release" | "stop";

const SNAP_THRESHOLD = 20;
const SNAP_INSET = 8;
const PREVIEW_TARGET_AREA = 380 * 248;
const PREVIEW_MIN_WIDTH = 280;
const PREVIEW_MAX_WIDTH = 480;
const PREVIEW_MIN_HEIGHT = 180;
const PREVIEW_MAX_HEIGHT = 420;

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
  threshold = SNAP_THRESHOLD,
  inset = SNAP_INSET,
): Rectangle {
  const result = { ...bounds };
  const xCandidates = [workArea.x + inset, workArea.x + workArea.width - bounds.width - inset];
  const yCandidates = [workArea.y + inset, workArea.y + workArea.height - bounds.height - inset];

  for (const anchor of anchors) {
    if (rangesOverlap(bounds.y, bounds.y + bounds.height, anchor.y, anchor.y + anchor.height)) {
      xCandidates.push(anchor.x + inset, anchor.x + anchor.width - bounds.width - inset);
    }
    if (rangesOverlap(bounds.x, bounds.x + bounds.width, anchor.x, anchor.x + anchor.width)) {
      yCandidates.push(anchor.y + inset, anchor.y + anchor.height - bounds.height - inset);
    }
  }

  result.x = nearestSnap(bounds.x, xCandidates, threshold);
  result.y = nearestSnap(bounds.y, yCandidates, threshold);
  return result;
}

function nearestSnap(value: number, candidates: number[], threshold: number): number {
  let nearest = value;
  let distance = threshold + 1;
  for (const candidate of candidates) {
    const nextDistance = Math.abs(value - candidate);
    if (nextDistance <= threshold && nextDistance < distance) {
      nearest = candidate;
      distance = nextDistance;
    }
  }
  return nearest;
}

function rangesOverlap(startA: number, endA: number, startB: number, endB: number): boolean {
  return startA < endB && endA > startB;
}

export function activityControlMethod(action: ActivityWindowAction): "activity/takeover" | "activity/release" | "activity/stop" {
  return `activity/${action}`;
}

type ActivityControl = (
  activity: ActivitySession,
  action: ActivityWindowAction,
) => Promise<ActivitySession>;

export class CUAActivityWindowManager {
  private readonly manuallyResizedWindowIDs = new Set<number>();
  private readonly previewState = new Map<string, { ratio: number; target: string }>();

  constructor(
    private readonly registry: WindowRegistry,
    private readonly control: ActivityControl,
  ) {}

  handleServerEvent(event: ServerEvent): void {
    const activity = cuaActivityFromServerEvent(event);
    if (!activity) return;
    this.update(activity);
  }

  update(activity: ActivitySession): void {
    const existing = this.registry.activityWindow(activity.id);
    if (activity.state === "stopped") {
      this.registry.clearActivityWindow(activity.id);
      if (existing && !existing.isDestroyed()) existing.close();
      return;
    }
    const win = existing && !existing.isDestroyed()
      ? existing
      : this.createWindow(activity);
    this.render(win, activity);
  }

  private createWindow(activity: ActivitySession): BrowserWindow {
    const cursor = screen.getCursorScreenPoint();
    const workArea = screen.getDisplayNearestPoint(cursor).workArea;
    const width = 380;
    const height = 248;
    const win = new BrowserWindow({
      width,
      height,
      x: workArea.x + workArea.width - width - 24,
      y: workArea.y + 24,
      minWidth: PREVIEW_MIN_WIDTH,
      minHeight: PREVIEW_MIN_HEIGHT,
      frame: false,
      transparent: true,
      backgroundColor: "#00000000",
      vibrancy: "under-window",
      visualEffectState: "active",
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
    win.setVisibleOnAllWorkspaces(true, { visibleOnFullScreen: true });
    win.webContents.setWindowOpenHandler(() => ({ action: "deny" }));
    win.on("will-resize", () => {
      this.manuallyResizedWindowIDs.add(win.webContents.id);
    });
    win.on("moved", () => {
      if (win.isDestroyed()) return;
      const bounds = win.getBounds();
      const workArea = screen.getDisplayMatching(bounds).workArea;
      const anchors = this.registry.allWindows()
        .filter((candidate) => candidate !== win && !candidate.isDestroyed() && candidate.isVisible())
        .map((candidate) => candidate.getBounds());
      const snapped = snapActivityBounds(bounds, workArea, anchors);
      if (snapped.x !== bounds.x || snapped.y !== bounds.y) {
        win.setBounds(snapped, false);
      }
    });
    win.webContents.on("will-navigate", (navigationEvent, rawURL) => {
      const parsed = activityActionFromURL(rawURL);
      if (!parsed || parsed.activityID !== activity.id) return;
      navigationEvent.preventDefault();
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
    win.on("closed", () => {
      this.manuallyResizedWindowIDs.delete(windowID);
      this.previewState.delete(activity.id);
      this.registry.unregisterWindow(windowID);
    });
    win.once("ready-to-show", () => {
      if (!win.isDestroyed()) win.showInactive();
    });
    return win;
  }

  private render(win: BrowserWindow, activity: ActivitySession): void {
    if (win.isDestroyed()) return;
    this.autoSizeForPreview(win, activity);
    void win.loadURL(`data:text/html;charset=utf-8,${encodeURIComponent(cuaActivityHTML(activity))}`);
  }

  private autoSizeForPreview(win: BrowserWindow, activity: ActivitySession): void {
    const preview = activity.preview?.trim();
    const windowID = win.webContents.id;
    if (!preview?.startsWith("file:") || this.manuallyResizedWindowIDs.has(windowID)) return;
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
    const resized = resizeActivityBounds(bounds, fitActivityPreviewSize(imageSize.width, imageSize.height), workArea);
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
    if (!(["takeover", "release", "stop"] as const).includes(action)) return undefined;
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
    ? `<img src="${escapeHTML(previewURL)}" alt="${target} 最新画面" />`
    : `<div class="glass" role="status" aria-label="正在获取画面"></div>`;
  return `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8" />
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src wuu-file:; style-src 'unsafe-inline'; navigate-to wuu-cua:" />
<meta name="viewport" content="width=device-width,initial-scale=1" />
<style>
:root{color-scheme:light dark;font-family:-apple-system,BlinkMacSystemFont,"SF Pro Text",sans-serif;--ink:#111315;--line:rgba(255,255,255,.48);--glass:rgba(244,246,247,.56);--glass-strong:rgba(255,255,255,.82);--hover:rgba(31,35,40,.08);--danger:#b42318;--danger-soft:rgba(255,240,239,.92);--shadow:0 16px 40px rgba(20,24,28,.18),0 2px 8px rgba(20,24,28,.1)}
@media(prefers-color-scheme:dark){:root{--ink:#f2f3f4;--line:rgba(255,255,255,.16);--glass:rgba(31,35,40,.58);--glass-strong:rgba(42,46,51,.88);--hover:rgba(228,232,235,.12);--danger:#f0705f;--danger-soft:rgba(52,28,24,.92);--shadow:0 16px 40px rgba(0,0,0,.58),0 2px 8px rgba(0,0,0,.42)}}
*{box-sizing:border-box}html,body{width:100%;height:100%;margin:0;background:transparent;overflow:hidden}
.card{position:relative;height:100%;border-radius:14px;overflow:hidden;background:#101214;border:1px solid var(--line);box-shadow:var(--shadow);color:var(--ink);-webkit-app-region:drag}
.preview{position:absolute;inset:0;display:grid;place-items:center;overflow:hidden}.preview img{width:100%;height:100%;object-fit:contain;display:block;pointer-events:none}
.glass{position:absolute;inset:0;background:radial-gradient(circle at 10% 0%,rgba(255,255,255,.62),transparent 44%),radial-gradient(circle at 92% 34%,rgba(255,122,72,.16),transparent 48%),radial-gradient(circle at 52% 112%,rgba(110,170,255,.14),transparent 50%),linear-gradient(145deg,var(--glass-strong),var(--glass));box-shadow:inset 0 1px 0 rgba(255,255,255,.5)}
.actions{position:absolute;z-index:3;top:8px;right:8px;display:flex;align-items:center;gap:5px;padding:4px;border-radius:10px;background:var(--glass-strong);border:1px solid var(--line);box-shadow:0 2px 8px rgba(0,0,0,.14);opacity:0;transform:translateY(-3px);transition:opacity 140ms ease,transform 140ms ease;-webkit-app-region:no-drag}.card:hover .actions,.actions:focus-within{opacity:1;transform:none}.button{height:25px;padding:0 8px;border-radius:7px;display:inline-flex;align-items:center;justify-content:center;text-decoration:none;color:var(--ink);background:transparent;border:0;font-size:11px;font-weight:560}.button:hover{background:var(--hover)}.button.stop{width:25px;padding:0;font-size:15px;font-weight:400}.button.stop:hover{color:var(--danger);background:var(--danger-soft)}
.error{position:absolute;z-index:2;left:8px;right:8px;bottom:8px;padding:8px 10px;border-radius:9px;background:var(--danger-soft);border:1px solid var(--line);font-size:10.5px;color:var(--danger);-webkit-app-region:no-drag}
</style></head><body><section class="card"><div class="preview">${preview}</div><div class="actions">${controls}${actionLink(activity,"stop","停止","stop","×")}</div>${error}</section></body></html>`;
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
