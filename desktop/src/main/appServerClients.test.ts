import { describe, expect, it } from "vitest";
import {
  activityServerRequestRejection,
  appServerExitMessage,
  updateStoppedActivityIDs,
} from "./appServerClients";

describe("appServerExitMessage", () => {
  it("preserves stderr and the exit code", () => {
    expect(appServerExitMessage(1, "parse config: unknown field")).toBe(
      "wuu core exited (code 1): parse config: unknown field",
    );
  });

  it("still reports an exit without stderr", () => {
    expect(appServerExitMessage(null, "")).toBe("wuu core exited");
  });

  it("rejects future plugin bridge requests after their Activity stops", () => {
    const stopped = new Set<string>();
    updateStoppedActivityIDs(stopped, {
      method: "activity/stopped",
      params: { id: "activity-1", thread_id: "thread-1" },
    });
    expect(
      activityServerRequestRejection(
        {
          id: "bridge-1",
          method: "official-plugin/browser-command",
          params: { activity_id: "activity-1", action: "click" },
        },
        stopped,
      ),
    ).toBe("activity activity-1 is stopped");
    expect(
      activityServerRequestRejection(
        {
          id: "bridge-2",
          method: "official-plugin/browser-command",
          params: { activity_id: "activity-2", action: "click" },
        },
        stopped,
      ),
    ).toBeUndefined();
  });
});
