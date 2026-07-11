import { describe, expect, it } from "vitest";
import type { ActivitySession, ServerEvent } from "../shared/protocol";
import {
  activityActionFromURL,
  activityControlMethod,
  cuaActivityFromServerEvent,
  cuaActivityHTML,
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

  it("snaps near screen edges with an inset", () => {
    expect(snapActivityBounds(
      { x: 5, y: 650, width: 380, height: 248 },
      { x: 0, y: 0, width: 1440, height: 900 },
      [],
    )).toEqual({ x: 8, y: 644, width: 380, height: 248 });
  });

  it("snaps to overlapping Wuu window inner edges only within range", () => {
    const workArea = { x: 0, y: 0, width: 1800, height: 1100 };
    const wuuWindow = { x: 120, y: 100, width: 1200, height: 800 };
    expect(snapActivityBounds(
      { x: 128, y: 115, width: 380, height: 248 },
      workArea,
      [wuuWindow],
    )).toEqual({ x: 128, y: 108, width: 380, height: 248 });
    expect(snapActivityBounds(
      { x: 700, y: 500, width: 380, height: 248 },
      workArea,
      [wuuWindow],
    )).toEqual({ x: 700, y: 500, width: 380, height: 248 });
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
