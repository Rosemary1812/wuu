import { describe, expect, it, vi } from "vitest";
import type { RuntimeContext, TerminalSessionStartParams } from "../shared/protocol";

// node-pty loads a platform-specific native addon at import time
// (unixTerminal.ts's top-level loadNativeModule). It only ships prebuilt
// binaries for darwin/win32, so importing the real module crashes outright
// on this linux sandbox — unrelated to the change under test. Stub it out
// so terminalSessions.ts can be imported for its pure resolveTerminalCwd
// seam; TerminalSessionManager.start() itself (the pty.spawn() call) isn't
// exercised here for the same reason.
vi.mock("node-pty", () => ({ spawn: vi.fn() }));

const { resolveTerminalCwd } = await import("./terminalSessions");
describe("resolveTerminalCwd", () => {
  const context: RuntimeContext = {
    kind: "project",
    project_id: "project-1",
    cwd: "/repo/project",
  };

  it("uses the runtime context's cwd when no override is given", () => {
    expect(resolveTerminalCwd(context, {})).toBe("/repo/project");
  });

  it("prefers an explicit override cwd over the runtime context", () => {
    const params: TerminalSessionStartParams = {
      cwd: "/repo/worktrees/fork-1/project",
    };
    expect(resolveTerminalCwd(context, params)).toBe(
      "/repo/worktrees/fork-1/project",
    );
  });

  it("normalizes a relative override to an absolute path", () => {
    const params: TerminalSessionStartParams = { cwd: "relative/worktree" };
    const resolved = resolveTerminalCwd(context, params);
    expect(resolved.endsWith("/relative/worktree")).toBe(true);
    expect(resolved.startsWith("/")).toBe(true);
  });

  it("ignores an empty-string override and falls back to the runtime context", () => {
    expect(resolveTerminalCwd(context, { cwd: "" })).toBe("/repo/project");
  });
});
