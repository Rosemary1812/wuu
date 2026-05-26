import { describe, expect, it } from "bun:test";
import { readableToolActivityCommand } from "./ToolActivity";

describe("readableToolActivityCommand", () => {
  it("renders tool calls as process log lines instead of raw JSON", () => {
    expect(
      readableToolActivityCommand({
        name: "list_files",
        arguments: JSON.stringify({ path: "." })
      })
    ).toBe("查看 当前目录");

    expect(
      readableToolActivityCommand({
        name: "git",
        arguments: JSON.stringify({ subcommand: "status", args: [] })
      })
    ).toBe("检查 Git 状态");

    expect(
      readableToolActivityCommand({
        name: "glob",
        arguments: JSON.stringify({ pattern: "**/AGENTS.md", path: "." })
      })
    ).toBe("搜索 AGENTS.md");
  });

  it("keeps explicit shell commands readable", () => {
    expect(
      readableToolActivityCommand({
        name: "run_shell",
        arguments: JSON.stringify({ command: "npm run typecheck" })
      })
    ).toBe("运行 npm run typecheck");
  });
});
