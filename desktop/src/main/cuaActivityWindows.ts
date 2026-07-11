import { BrowserWindow, screen } from "electron";
import type { Rectangle } from "electron";
import { fileURLToPath } from "node:url";
import type { ActivitySession, ServerEvent } from "../shared/protocol";
import { renderableFileURL } from "./renderableFileURLs";
import type { WindowRegistry } from "./windowRegistry";

export type ActivityWindowAction = "takeover" | "release" | "stop";

const SNAP_THRESHOLD = 20;
const SNAP_INSET = 8;

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
      minWidth: 320,
      minHeight: 200,
      frame: false,
      transparent: true,
      backgroundColor: "#00000000",
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
    win.on("moved", () => {
      if (win.isDestroyed()) return;
      const bounds = win.getBounds();
      const workArea = screen.getDisplayMatching(bounds).workArea;
      const anchors = this.registry.allWindows()
        .filter((candidate) => candidate !== win && !candidate.isDestroyed() && candidate.isVisible())
        .map((candidate) => candidate.getBounds());
      const snapped = snapActivityBounds(bounds, workArea, anchors);
      if (snapped.x !== bounds.x || snapped.y !== bounds.y) {
        win.setBounds(snapped, true);
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
      this.registry.unregisterWindow(windowID);
    });
    win.once("ready-to-show", () => {
      if (!win.isDestroyed()) win.showInactive();
    });
    return win;
  }

  private render(win: BrowserWindow, activity: ActivitySession): void {
    if (win.isDestroyed()) return;
    void win.loadURL(`data:text/html;charset=utf-8,${encodeURIComponent(cuaActivityHTML(activity))}`);
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
    : `<div class="glass" role="status" aria-label="正在获取画面"><span></span><span></span><span></span></div>`;
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
.glass{position:absolute;inset:0;overflow:hidden;background:linear-gradient(145deg,var(--glass-strong),var(--glass));backdrop-filter:blur(28px) saturate(150%)}.glass:after{content:"";position:absolute;inset:0;background:linear-gradient(135deg,rgba(255,255,255,.34),transparent 38%,rgba(255,255,255,.1));box-shadow:inset 0 1px 0 rgba(255,255,255,.5)}.glass span{position:absolute;width:54%;aspect-ratio:1;border-radius:50%;filter:blur(30px);opacity:.58;animation:drift 7s ease-in-out infinite alternate}.glass span:nth-child(1){left:-12%;top:-30%;background:rgba(255,255,255,.72)}.glass span:nth-child(2){right:-18%;top:14%;background:rgba(255,122,72,.26);animation-delay:-2s}.glass span:nth-child(3){left:26%;bottom:-44%;background:rgba(110,170,255,.2);animation-delay:-4s}@keyframes drift{to{transform:translate3d(12px,18px,0) scale(1.08)}}
.actions{position:absolute;z-index:3;top:8px;right:8px;display:flex;align-items:center;gap:5px;padding:4px;border-radius:10px;background:var(--glass-strong);border:1px solid var(--line);backdrop-filter:blur(18px) saturate(145%);box-shadow:0 2px 8px rgba(0,0,0,.14);opacity:0;transform:translateY(-3px);transition:opacity 140ms ease,transform 140ms ease;-webkit-app-region:no-drag}.card:hover .actions,.actions:focus-within{opacity:1;transform:none}.button{height:25px;padding:0 8px;border-radius:7px;display:inline-flex;align-items:center;justify-content:center;text-decoration:none;color:var(--ink);background:transparent;border:0;font-size:11px;font-weight:560}.button:hover{background:var(--hover)}.button.stop{width:25px;padding:0;font-size:15px;font-weight:400}.button.stop:hover{color:var(--danger);background:var(--danger-soft)}
.error{position:absolute;z-index:2;left:8px;right:8px;bottom:8px;padding:8px 10px;border-radius:9px;background:var(--danger-soft);border:1px solid var(--line);backdrop-filter:blur(18px);font-size:10.5px;color:var(--danger);-webkit-app-region:no-drag}
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
