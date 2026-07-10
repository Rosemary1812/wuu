import { BrowserWindow, screen } from "electron";
import { fileURLToPath } from "node:url";
import type { ActivitySession, ServerEvent } from "../shared/protocol";
import { renderableFileURL } from "./renderableFileURLs";
import type { WindowRegistry } from "./windowRegistry";

export type ActivityWindowAction = "takeover" | "release" | "stop";

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
    const height = 270;
    const win = new BrowserWindow({
      width,
      height,
      x: workArea.x + workArea.width - width - 24,
      y: workArea.y + 24,
      minWidth: 320,
      minHeight: 220,
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
  const state = activityStateLabel(activity);
  const statusClass = escapeHTML(activity.state);
  const previewURL = activityPreviewURL(activity);
  const controls = activity.controller === "user"
    ? actionLink(activity, "release", "交还 Agent")
    : activity.controller === "agent"
      ? actionLink(activity, "takeover", "接管")
      : "";
  const error = activity.error
    ? `<div class="error">${escapeHTML(activity.error)}</div>`
    : "";
  const preview = previewURL
    ? `<img src="${escapeHTML(previewURL)}" alt="${target} 最新画面" />`
    : `<div class="empty"><span class="pulse"></span>${escapeHTML(state)}</div>`;
  return `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8" />
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src wuu-file:; style-src 'unsafe-inline'; navigate-to wuu-cua:" />
<meta name="viewport" content="width=device-width,initial-scale=1" />
<style>
:root{color-scheme:light dark;font-family:-apple-system,BlinkMacSystemFont,"SF Pro Text",sans-serif}
*{box-sizing:border-box}html,body{width:100%;height:100%;margin:0;background:transparent;overflow:hidden}
.card{height:100%;display:grid;grid-template-rows:46px minmax(0,1fr) 34px;border-radius:18px;overflow:hidden;background:rgba(30,31,33,.94);border:1px solid rgba(255,255,255,.18);box-shadow:0 18px 55px rgba(0,0,0,.34);color:#f5f5f4}
header{display:flex;align-items:center;gap:9px;padding:0 10px 0 14px;-webkit-app-region:drag;background:rgba(255,255,255,.055)}
.dot{width:8px;height:8px;border-radius:50%;background:#65c97a;box-shadow:0 0 0 4px rgba(101,201,122,.13)}
.dot.user_controlled{background:#f0b657;box-shadow:0 0 0 4px rgba(240,182,87,.14)}.dot.error{background:#ef7068}
.title{min-width:0;flex:1}.title strong{display:block;font-size:12.5px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.title span{font-size:10.5px;color:#aeb2b7}
.actions{display:flex;gap:6px;-webkit-app-region:no-drag}.button{height:25px;padding:0 9px;border-radius:8px;display:inline-flex;align-items:center;text-decoration:none;color:#f4f4f3;background:rgba(255,255,255,.11);font-size:11px}.button:hover{background:rgba(255,255,255,.19)}.button.stop{width:25px;padding:0;justify-content:center;color:#ff938c}
.preview{position:relative;background:#111;min-height:0;display:grid;place-items:center}.preview img{width:100%;height:100%;object-fit:contain;display:block}.empty{display:flex;align-items:center;gap:10px;color:#aeb2b7;font-size:12px}.pulse{width:11px;height:11px;border-radius:50%;border:2px solid #72777d;border-top-color:#e6e7e8;animation:spin 1s linear infinite}@keyframes spin{to{transform:rotate(360deg)}}
footer{display:flex;align-items:center;justify-content:space-between;padding:0 13px;color:#aeb2b7;font-size:10.5px;background:rgba(255,255,255,.04)}.brand{color:#d7d9dc}.error{position:absolute;left:10px;right:10px;bottom:8px;padding:7px 9px;border-radius:8px;background:rgba(120,35,35,.92);font-size:10.5px;color:#ffd7d4;z-index:2}
</style></head><body><section class="card"><header><span class="dot ${statusClass}"></span><div class="title"><strong>${target}</strong><span>${escapeHTML(state)}</span></div><div class="actions">${controls}${actionLink(activity,"stop","停止","stop","■")}</div></header><div class="preview">${preview}${error}</div><footer><span class="brand">Wuu Computer Use</span><span>拖动浮窗 · 始终置顶</span></footer></section></body></html>`;
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

function activityStateLabel(activity: ActivitySession): string {
  switch (activity.state) {
  case "starting": return "正在连接 Mac";
  case "active": return "Agent 正在操作";
  case "user_controlled": return "你已接管";
  case "waiting_confirmation": return "等待确认";
  case "error": return "操作遇到问题";
  case "stopped": return "已停止";
  }
}

function escapeHTML(value: string): string {
  return value.replace(/[&<>"']/g, (character) => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
  })[character] ?? character);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
