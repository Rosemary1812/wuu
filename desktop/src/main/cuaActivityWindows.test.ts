import { describe, expect, it } from "vitest";
import type { ActivitySession, ServerEvent } from "../shared/protocol";
import {
  activityActionFromURL,
  activityActionsHTML,
  activityAspectRatio,
  activityControlMethod,
  activityDockAnchor,
  activityHasVisibleContent,
  activityRenderSignature,
  activityViewState,
  activityVisibleForThread,
  activityWindowCanCreate,
  activityWindowResizeOptions,
  activityWindowStackingOptions,
  cuaActivityFromServerEvent,
  cuaActivityHTML,
  frameStreamRetryDelay,
  fitActivityPreviewSize,
  fitUserResizedPreviewSize,
  resizeActivityBounds,
  shouldScheduleDragSettle,
  snapAnimationDuration,
  snapAnimationProgress,
  snapActivityBounds,
} from "./cuaActivityWindows";

function activity(overrides: Partial<ActivitySession> = {}): ActivitySession {
  return {
    id: "activity-1",
    kind: "cua",
    thread_id: "thread-1",
    workdir: "/repo",
    plugin_id: "cua-mac",
    target: "TextEdit <Draft>",
    state: "active",
    controller: "agent",
    preview: "file:///tmp/preview.png",
    created_at: "2026-07-10T10:00:00Z",
    updated_at: "2026-07-10T10:00:01Z",
    ...overrides,
  };
}

describe("CUA Activity windows", () => {
  it("keeps the PiP above Wuu without floating over other applications", () => {
    const parent = { id: "main-window" } as never;
    expect(activityWindowStackingOptions(parent)).toEqual({
      alwaysOnTop: false,
      parent,
    });
  });

  it("uses native edge resizing within a useful range", () => {
    expect(activityWindowResizeOptions()).toEqual({
      minWidth: 220,
      minHeight: 140,
      maxWidth: 720,
      maxHeight: 800,
      resizable: true,
    });
    expect(activityAspectRatio(230, 408)).toBeCloseTo(230 / 408);
    expect(activityAspectRatio(0, 408)).toBeUndefined();
    expect(fitUserResizedPreviewSize(400, 16 / 9)).toEqual({ width: 400, height: 225 });
    expect(fitUserResizedPreviewSize(400, 230 / 408)).toEqual({ width: 400, height: 710 });
    expect(fitUserResizedPreviewSize(700, 230 / 408)).toEqual({ width: 451, height: 800 });
  });

  it("does not create an orphan PiP after the Wuu window closes", () => {
    expect(activityWindowCanCreate(null)).toBe(false);
    expect(activityWindowCanCreate({ isDestroyed: () => true } as never)).toBe(false);
    expect(activityWindowCanCreate({ isDestroyed: () => false } as never)).toBe(true);
  });

  it("keeps a screen-docked PiP anchored when the Wuu window moves", () => {
    const originalScreen = { x: 1440, y: 0, width: 1200, height: 900 };
    expect(activityDockAnchor(
      { source: "screen", corner: 3, workArea: originalScreen },
      { x: 200, y: 120, width: 1000, height: 700 },
      { x: 0, y: 0, width: 1440, height: 900 },
    )).toEqual(originalScreen);
  });

  it("scopes PiP visibility to the active session", () => {
    expect(activityVisibleForThread("thread-1", "thread-1")).toBe(true);
    expect(activityVisibleForThread("thread-1", "thread-2")).toBe(false);
    expect(activityVisibleForThread("thread-1", undefined)).toBe(false);
  });

  it("shows only the PiP activities owned by the active session", () => {
    const activities = [
      activity({ id: "activity-a", thread_id: "session-a" }),
      activity({ id: "activity-b", thread_id: "session-b" }),
      activity({ id: "activity-c", thread_id: "session-a" }),
    ];
    expect(activities.filter((item) => activityVisibleForThread(item.thread_id, "session-a")).map((item) => item.id))
      .toEqual(["activity-a", "activity-c"]);
    expect(activities.filter((item) => activityVisibleForThread(item.thread_id, "session-b")).map((item) => item.id))
      .toEqual(["activity-b"]);
  });
  it("accepts only CUA lifecycle notifications", () => {
    const event: ServerEvent = {
      workdir: "/repo",
      kind: "notification",
      message: { method: "activity/updated", params: activity() },
    };
    expect(cuaActivityFromServerEvent(event)?.id).toBe("activity-1");
    expect(
      cuaActivityFromServerEvent({
        ...event,
        message: { method: "activity/updated", params: activity({ kind: "browser" }) },
      }),
    ).toBeUndefined();
    expect(
      cuaActivityFromServerEvent({
        workdir: "/repo",
        kind: "server-error",
        message: "nope",
      }),
    ).toBeUndefined();
  });

  it("renders an escaped always-on-top card with safe preview protocol and controls", () => {
    const html = cuaActivityHTML(activity());
    expect(html).toContain("TextEdit &lt;Draft&gt;");
    expect(html).not.toContain("file:///tmp/preview.png");
    expect(html).toMatch(/wuu-file:\/\/local\//);
    expect(html).toContain("接管");
    expect(html).toContain("关闭画中画");
    expect(html).not.toContain("aria-label=\"停止\"");
    expect(html).toContain("prefers-color-scheme:dark");
    expect(html).not.toContain("Agent 正在操作");
    expect(html).not.toContain("Computer Use");
    expect(html).not.toContain("拖动浮窗");
    expect(cuaActivityHTML(activity({ controller: "user", state: "user_controlled" }))).toContain("交还 Agent");
  });

  it("uses glass without visible copy before the first preview", () => {
    const html = cuaActivityHTML(activity({ preview: undefined, state: "starting" }));
    expect(html).toContain('class="glass"');
    expect(html).not.toContain("backdrop-filter");
    expect(html).not.toContain("@keyframes drift");
    expect(html).not.toContain(">正在获取画面<");
    expect(html).not.toContain("正在连接 Mac");
  });

  it("snaps from anywhere on the screen to the nearest corner", () => {
    expect(snapActivityBounds(
      { x: 400, y: 430, width: 380, height: 248 },
      { x: 0, y: 0, width: 1440, height: 900 },
      [],
    )).toEqual({ x: 12, y: 640, width: 380, height: 248 });
  });

  it("settles only an active user drag and keeps programmatic movement out", () => {
    expect(shouldScheduleDragSettle(true, false)).toBe(true);
    expect(shouldScheduleDragSettle(true, true)).toBe(false);
    expect(shouldScheduleDragSettle(false, false)).toBe(false);
  });

  it("uses a zero-velocity magnetic snap curve with distance-aware timing", () => {
    expect(snapAnimationProgress(0)).toBe(0);
    expect(snapAnimationProgress(0.25)).toBeLessThan(0.25);
    expect(snapAnimationProgress(0.5)).toBeCloseTo(0.5);
    expect(snapAnimationProgress(0.75)).toBeGreaterThan(0.75);
    expect(snapAnimationProgress(1)).toBe(1);
    expect(snapAnimationDuration(0)).toBe(220);
    expect(snapAnimationDuration(500)).toBe(330);
    expect(snapAnimationDuration(2000)).toBe(420);
  });

  it("snaps naturally to the four inner corners of the main Wuu window", () => {
    const workArea = { x: 0, y: 0, width: 1800, height: 1100 };
    const wuuWindow = { x: 120, y: 100, width: 1200, height: 800 };
    expect(snapActivityBounds(
      { x: 128, y: 115, width: 380, height: 248 },
      workArea,
      [wuuWindow],
    )).toEqual({ x: 132, y: 112, width: 380, height: 248 });
    expect(snapActivityBounds(
      { x: 925, y: 640, width: 380, height: 248 },
      workArea,
      [wuuWindow],
    )).toEqual({ x: 928, y: 640, width: 380, height: 248 });
    expect(snapActivityBounds(
      { x: 700, y: 500, width: 380, height: 248 },
      workArea,
      [wuuWindow],
    )).toEqual({ x: 928, y: 640, width: 380, height: 248 });
  });

  it("keeps the preview styled as a compact app frame", () => {
    const html = cuaActivityHTML(activity());
    expect(html).toContain("background:transparent");
    expect(html).toContain("object-fit:contain");
    expect(html).not.toContain("background:#111315");
    expect(html).not.toContain(".card{position:relative;height:100%;border-radius");
    expect(html).not.toContain("0 24px 64px");
  });

  it("keeps main-window corner targets inside the active display", () => {
    expect(snapActivityBounds(
      { x: 680, y: 480, width: 380, height: 248 },
      { x: 0, y: 0, width: 1000, height: 700 },
      [{ x: 760, y: 520, width: 180, height: 140 }],
    )).toEqual({ x: 620, y: 452, width: 380, height: 248 });
  });

  it("ignores main-window anchors on another display", () => {
    expect(snapActivityBounds(
      { x: 1970, y: 80, width: 380, height: 248 },
      { x: 1800, y: 0, width: 1200, height: 900 },
      [{ x: 100, y: 100, width: 1200, height: 800 }],
    )).toEqual({ x: 1812, y: 12, width: 380, height: 248 });
  });

  it("fits preview proportions within the compact overlay envelope", () => {
    expect(fitActivityPreviewSize(1920, 1080)).toEqual({ width: 409, height: 230 });
    expect(fitActivityPreviewSize(1100, 1442)).toEqual({ width: 280, height: 367 });
    expect(fitActivityPreviewSize(230, 408)).toEqual({ width: 280, height: 497 });
    expect(fitActivityPreviewSize(1000, 1000)).toEqual({ width: 307, height: 307 });
    expect(fitActivityPreviewSize(0, 0)).toEqual({ width: 380, height: 248 });
  });

  it("keeps automatic resizing anchored to the nearest work-area edges", () => {
    expect(resizeActivityBounds(
      { x: 1012, y: 24, width: 380, height: 248 },
      { width: 280, height: 367 },
      { x: 0, y: 0, width: 1440, height: 900 },
    )).toEqual({ x: 1112, y: 24, width: 280, height: 367 });
  });

  it("parses only explicit Activity control URLs", () => {
    expect(activityActionFromURL("wuu-cua://action/takeover?activity_id=activity-1")).toEqual({
      action: "takeover",
      activityID: "activity-1",
    });
    expect(activityActionFromURL("wuu-cua://action/close?activity_id=activity-1")).toEqual({
      action: "close",
      activityID: "activity-1",
    });
    expect(activityActionFromURL("wuu-cua://action/drag-start?activity_id=activity-1")?.action).toBe("drag-start");
    expect(activityActionFromURL("wuu-cua://action/drag-end?activity_id=activity-1")?.action).toBe("drag-end");
    expect(activityActionFromURL("wuu-cua://action/delete?activity_id=activity-1")).toBeUndefined();
    expect(activityActionFromURL("https://example.test/")).toBeUndefined();
  });

  it("keeps a stable render signature for heartbeat updates while a live stream runs", () => {
    const first = activity({ updated_at: "2026-07-10T10:00:01Z" });
    const later = activity({ updated_at: "2026-07-10T10:00:09Z" });
    expect(activityRenderSignature(later, true)).toBe(activityRenderSignature(first, true));
    expect(activityRenderSignature(later, false)).not.toBe(activityRenderSignature(first, false));
    expect(activityRenderSignature(activity({ controller: "user" }), true))
      .not.toBe(activityRenderSignature(first, true));
    expect(activityRenderSignature(activity({ error: "boom" }), true))
      .toBe(activityRenderSignature(first, true));
    expect(activityRenderSignature(activity({ interaction: { kind: "click", x: 0.5, y: 0.5, revision: 2 } }), true))
      .not.toBe(activityRenderSignature(first, true));
  });

  it("exposes an in-place view state instead of a full page reload", () => {
    const interaction = { kind: "click" as const, x: 0.25, y: 0.75, revision: 42 };
    const state = activityViewState(activity({ error: "boom <late>", interaction }));
    expect(state.actionsHTML).toContain("接管");
    expect(state.actionsHTML).toContain("关闭画中画");
    expect(state.previewURL).toMatch(/^wuu-file:\/\/local\//);
    expect(state.interaction).toEqual(interaction);
    expect(activityViewState(activity({ preview: undefined })).previewURL).toBe("");
    expect(activityActionsHTML(activity({ controller: "user" }))).toContain("交还 Agent");
  });

  it("keeps the PiP hidden until real visual content exists", () => {
    expect(activityHasVisibleContent(activity())).toBe(true);
    expect(activityHasVisibleContent(activity({ preview: undefined, state: "starting" }))).toBe(false);
    expect(activityHasVisibleContent(activity({ preview: "   " }))).toBe(false);
    expect(activityHasVisibleContent(activity({ preview: "https://example.test/p.png" }))).toBe(false);
    expect(activityHasVisibleContent(activity({ preview: undefined, error: "boom" }))).toBe(false);
  });

  it("backs off dead live streams instead of retrying immediately or forever", () => {
    expect(frameStreamRetryDelay(1)).toBe(2000);
    expect(frameStreamRetryDelay(2)).toBe(4000);
    expect(frameStreamRetryDelay(3)).toBe(8000);
    expect(frameStreamRetryDelay(6)).toBe(16000);
    expect(frameStreamRetryDelay(0)).toBe(2000);
  });

  it("ships a patchable document without exposing raw tool errors", () => {
    const html = cuaActivityHTML(activity());
    expect(html).toContain("window.wuuCUAActivity");
    expect(html).toContain("window.wuuCUAStreamUnavailable");
    expect(html).toContain("实时画面暂不可用");
    expect(html).not.toContain('class="error"');
    expect(cuaActivityHTML(activity({ error: "boom" }))).not.toContain("boom");
    expect(html).toContain('class="agent-pointer"');
    expect(html).toContain("cubic-bezier(.22,.8,.24,1)");
  });

  it("maps window controls onto Activity RPC methods", () => {
    expect(activityControlMethod("takeover")).toBe("activity/takeover");
    expect(activityControlMethod("release")).toBe("activity/release");
    expect(activityControlMethod("stop")).toBe("activity/stop");
  });
});
