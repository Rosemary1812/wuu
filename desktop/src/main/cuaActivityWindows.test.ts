import { describe, expect, it } from "vitest";
import type { ActivitySession, ServerEvent } from "../shared/protocol";
import {
  activityActionFromURL,
  activityControlMethod,
  cuaActivityFromServerEvent,
  cuaActivityHTML,
  fitActivityPreviewSize,
  resizeActivityBounds,
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
    expect(html).toContain("停止");
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
    expect(html).toContain("border-radius:22px");
    expect(html).toContain("background:transparent");
    expect(html).toContain("object-fit:cover");
    expect(html).not.toContain("background:#111315");
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
    expect(activityActionFromURL("wuu-cua://action/delete?activity_id=activity-1")).toBeUndefined();
    expect(activityActionFromURL("https://example.test/")).toBeUndefined();
  });

  it("maps window controls onto Activity RPC methods", () => {
    expect(activityControlMethod("takeover")).toBe("activity/takeover");
    expect(activityControlMethod("release")).toBe("activity/release");
    expect(activityControlMethod("stop")).toBe("activity/stop");
  });
});
