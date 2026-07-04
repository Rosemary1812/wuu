import { describe, expect, it } from "vitest";
import { resolveWuuCommand } from "./wuuCommand";

describe("resolveWuuCommand (issue #8: never run a workspace-local core)", () => {
  it("ignores the active workspace and falls back to wuu on PATH", () => {
    // Even if /repo/wuu contains bin/wuu or wuu (e.g. an ignored `make build`
    // artifact built for another OS), the resolver never inspects the
    // workspace, so a stale/foreign binary can't be spawned as the core.
    expect(resolveWuuCommand({}, "/repo/wuu", undefined)).toEqual({
      command: "wuu",
      args: [],
      cwd: "/repo/wuu",
    });
    // A discovered source root does NOT enable workspace/go-run on its own.
    expect(resolveWuuCommand({}, "/repo/wuu", "/src/wuu")).toEqual({
      command: "wuu",
      args: [],
      cwd: "/repo/wuu",
    });
  });

  it("honors an explicit WUU_BIN override", () => {
    expect(
      resolveWuuCommand({ WUU_BIN: "/opt/wuu/bin/wuu" }, "/repo/wuu", "/src/wuu"),
    ).toEqual({ command: "/opt/wuu/bin/wuu", args: [], cwd: "/repo/wuu" });
  });

  it("uses go run only when opted in AND a source root is found", () => {
    expect(
      resolveWuuCommand(
        { WUU_DESKTOP_USE_GO_RUN: "1" },
        "/repo/wuu",
        "/src/wuu",
      ),
    ).toEqual({ command: "go", args: ["run", "./cmd/wuu"], cwd: "/src/wuu" });
  });

  it("falls back to PATH when go-run is opted in but no source root exists", () => {
    expect(
      resolveWuuCommand({ WUU_DESKTOP_USE_GO_RUN: "1" }, "/repo/wuu", undefined),
    ).toEqual({ command: "wuu", args: [], cwd: "/repo/wuu" });
  });

  it("prefers WUU_BIN over go run", () => {
    expect(
      resolveWuuCommand(
        { WUU_BIN: "/opt/wuu", WUU_DESKTOP_USE_GO_RUN: "1" },
        "/repo/wuu",
        "/src/wuu",
      ),
    ).toEqual({ command: "/opt/wuu", args: [], cwd: "/repo/wuu" });
  });
});
