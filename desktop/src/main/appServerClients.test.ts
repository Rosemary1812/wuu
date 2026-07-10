import { describe, expect, it } from "vitest";
import {
  activityServerRequestRejection,
  appServerExitMessage,
  AppServerClientPool,
  cuaMacHelperEnvironment,
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

describe("cuaMacHelperEnvironment", () => {
  it("injects the packaged signed helper path on macOS", () => {
    const result = cuaMacHelperEnvironment(
      { HOME: "/Users/test" },
      "/source",
      "/Applications/wuu.app/Contents/Resources",
      "darwin",
      (path) => path === "/Applications/wuu.app/Contents/Resources/bin/wuu-cua-mac",
    );
    expect(result.WUU_CUA_MAC_HELPER).toBe(
      "/Applications/wuu.app/Contents/Resources/bin/wuu-cua-mac",
    );
  });

  it("uses the development helper without replacing an explicit override", () => {
    const discovered = cuaMacHelperEnvironment(
      {},
      "/source",
      undefined,
      "darwin",
      (path) => path === "/source/desktop/build/bin/wuu-cua-mac",
    );
    expect(discovered.WUU_CUA_MAC_HELPER).toBe(
      "/source/desktop/build/bin/wuu-cua-mac",
    );
    const overridden = cuaMacHelperEnvironment(
      { WUU_CUA_MAC_HELPER: "/custom/helper" },
      "/source",
      undefined,
      "darwin",
      () => true,
    );
    expect(overridden.WUU_CUA_MAC_HELPER).toBe("/custom/helper");
  });
});

describe("AppServerClientPool Activity routing", () => {
  it("does not create a new workspace client for an unknown Activity workdir", async () => {
    const pool = new AppServerClientPool(
      () => ({ kind: "no_project", cwd: "/active" }),
      () => "/active",
      () => undefined,
    );
    await expect(
      pool.requestForWorkdir("/missing", "activity/stop", {
        thread_id: "thread-1",
        activity_id: "activity-1",
      }),
    ).rejects.toThrow("activity workspace is no longer connected");
  });
});
