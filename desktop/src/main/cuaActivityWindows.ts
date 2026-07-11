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
    ? actionLink(activity, "release", "交还 Agent", "primary")
    : activity.controller === "agent"
      ? actionLink(activity, "takeover", "接管", "primary")
      : "";
  const error = activity.error
    ? `<div class="error">${escapeHTML(activity.error)}</div>`
    : "";
  const preview = previewURL
    ? `<img src="${escapeHTML(previewURL)}" alt="${target} 最新画面" />`
    : `<div class="empty"><span class="pulse"></span><span>正在获取画面</span></div>`;
  return `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8" />
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src wuu-file:; style-src 'unsafe-inline'; navigate-to wuu-cua:" />
<meta name="viewport" content="width=device-width,initial-scale=1" />
<style>
:root{color-scheme:light dark;font-family:-apple-system,BlinkMacSystemFont,"SF Pro Text",sans-serif;--paper:#fff;--surface:#f7f7f5;--ink:#111315;--ink-soft:#6f7478;--line:rgba(31,35,40,.12);--veil:rgba(255,255,255,.88);--hover:rgba(31,35,40,.07);--accent:#ff3d00;--danger:#b42318;--danger-soft:#fff0ef;--shadow:0 14px 34px rgba(20,24,28,.14),0 2px 8px rgba(20,24,28,.08)}
@media(prefers-color-scheme:dark){:root{--paper:#1d2024;--surface:#24282c;--ink:#f2f3f4;--ink-soft:#989ea3;--line:rgba(228,232,235,.14);--veil:rgba(29,32,36,.88);--hover:rgba(228,232,235,.1);--accent:#ff5a26;--danger:#f0705f;--danger-soft:#341c18;--shadow:0 14px 34px rgba(0,0,0,.55),0 2px 8px rgba(0,0,0,.4)}}
*{box-sizing:border-box}html,body{width:100%;height:100%;margin:0;background:transparent;overflow:hidden}
.card{height:100%;display:grid;grid-template-rows:44px minmax(0,1fr);border-radius:12px;overflow:hidden;background:var(--paper);border:1px solid var(--line);box-shadow:var(--shadow);color:var(--ink)}
header{display:flex;align-items:center;gap:10px;padding:0 8px 0 12px;-webkit-app-region:drag;background:var(--paper);border-bottom:1px solid var(--line)}
.mark{width:18px;height:18px;border-radius:5px;display:grid;place-items:center;background:var(--accent);color:#fff;font-size:10px;font-weight:700;line-height:1;box-shadow:inset 0 0 0 1px rgba(255,255,255,.18)}
.title{min-width:0;flex:1;line-height:1.2}.title strong{display:block;font-size:12px;font-weight:620;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.title span{display:block;margin-top:2px;font-size:10px;color:var(--ink-soft)}
.actions{display:flex;align-items:center;gap:5px;-webkit-app-region:no-drag}.button{height:26px;padding:0 9px;border-radius:8px;display:inline-flex;align-items:center;justify-content:center;text-decoration:none;color:var(--ink);background:transparent;border:1px solid var(--line);font-size:11px;font-weight:560}.button:hover{background:var(--hover)}.button.primary{background:var(--surface)}.button.stop{width:26px;padding:0;border-color:transparent;color:var(--ink-soft);font-size:15px;font-weight:400}.button.stop:hover{color:var(--danger);background:var(--danger-soft)}
.preview{position:relative;background:#101214;min-height:0;display:grid;place-items:center;overflow:hidden}.preview img{width:100%;height:100%;object-fit:contain;display:block}.empty{display:flex;align-items:center;gap:9px;color:#a9aeb3;font-size:11px}.pulse{width:12px;height:12px;border-radius:50%;border:2px solid #565c62;border-top-color:#e4e6e8;animation:spin 1s linear infinite}@keyframes spin{to{transform:rotate(360deg)}}
.status{position:absolute;left:8px;bottom:8px;display:flex;align-items:center;gap:6px;max-width:calc(100% - 16px);height:24px;padding:0 8px;border-radius:7px;background:var(--veil);border:1px solid var(--line);backdrop-filter:blur(12px);color:var(--ink);font-size:10px;font-weight:560;box-shadow:0 1px 3px rgba(0,0,0,.12)}.dot{width:6px;height:6px;flex:none;border-radius:50%;background:#1f9d55}.dot.user_controlled{background:#d09022}.dot.error{background:var(--danger)}.error{position:absolute;left:8px;right:8px;bottom:40px;padding:7px 9px;border-radius:8px;background:var(--danger-soft);border:1px solid var(--line);font-size:10.5px;color:var(--danger);z-index:2}
</style></head><body><section class="card"><header><span class="mark">W</span><div class="title"><strong>${target}</strong><span>Computer Use</span></div><div class="actions">${controls}${actionLink(activity,"stop","停止","stop","×")}</div></header><div class="preview">${preview}${error}<div class="status"><span class="dot ${statusClass}"></span>${escapeHTML(state)}</div></div></section></body></html>`;
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
