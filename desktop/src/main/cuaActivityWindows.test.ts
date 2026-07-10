import { describe, expect, it } from "vitest";
import type { ActivitySession, ServerEvent } from "../shared/protocol";
import {
  activityActionFromURL,
  activityControlMethod,
  cuaActivityFromServerEvent,
  cuaActivityHTML,
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
    expect(cuaActivityHTML(activity({ controller: "user", state: "user_controlled" }))).toContain("交还 Agent");
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
