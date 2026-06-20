import { describe, expect, it } from "vitest";
import type { ThreadItem } from "../shared/protocol";
import { readableToolActivityCommand } from "./ToolActivity";
import {
  buildToolActivityProcessSegments,
  summarizeToolActivity,
} from "./ToolActivityHelpers";

describe("readableToolActivityCommand", () => {
  it("ignores tool-provided display text and waits for args to parse", () => {
    // The backend ships a preformatted `display.text` ("查看项目目录")
    // with item/started, but it's just a placeholder — once args parse
    // we render the real path. Ignoring display.text keeps the title
    // timing unified (no placeholder → real flicker).
    expect(
      readableToolActivityCommand({
        name: "list_files",
        arguments: undefined,
        display: { kind: "read", text: "查看项目目录" },
      })
    ).toBe("");

    // display.text is also ignored when args parse; we render from args.
    expect(
      readableToolActivityCommand({
        name: "list_files",
        arguments: JSON.stringify({ path: "." }),
        display: { kind: "read", text: "（已忽略）" },
      })
    ).toBe("查看项目目录");
  });

  it("returns empty string when args are missing for known tools", () => {
    // Until args (or result) actually parses, there is nothing to render.
    // The next item/toolCall/delta will reveal the title.
    expect(readableToolActivityCommand({ name: "read_file" })).toBe("");
    expect(readableToolActivityCommand({ name: "bash" })).toBe("");
  });

  it("returns empty string when args are partial JSON", () => {
    // Streaming `item/toolCall/delta` builds up the JSON one chunk at a
    // time; mid-stream it isn't valid JSON yet, so we wait.
    expect(
      readableToolActivityCommand({
        name: "read_file",
        arguments: '{"path":"/foo"',
      })
    ).toBe("");
  });

  it("renders the path once args parse", () => {
    // formatPathTarget collapses multi-segment paths to their basename,
    // so use a single-segment path to assert on the basename directly.
    expect(
      readableToolActivityCommand({
        name: "read_file",
        arguments: JSON.stringify({ path: "bar.ts" }),
      })
    ).toBe("读取 bar.ts");
  });

  it("renders tool calls as process log lines instead of raw JSON", () => {
    expect(
      readableToolActivityCommand({
        name: "list_files",
        arguments: JSON.stringify({ path: "." })
      })
    ).toBe("查看项目目录");

    expect(
      readableToolActivityCommand({
        name: "bash",
        arguments: JSON.stringify({ command: "git status" })
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
        name: "bash",
        arguments: JSON.stringify({ command: "npm run typecheck" })
      })
    ).toBe("运行 npm run typecheck");

    expect(
      readableToolActivityCommand({
        name: "bash",
        arguments: JSON.stringify({ command: "npx vitest run" }),
        display: { capability: "command.bash" }
      })
    ).toBe("运行 npx vitest run — command.bash");
  });

  it("renders apply_patch as a file update tool", () => {
    expect(
      readableToolActivityCommand({
        name: "apply_patch",
        arguments: JSON.stringify({ patch: "*** Begin Patch\n*** End Patch" })
      })
    ).toBe("更新文件");
  });

  it("renders bash background actions from capability metadata", () => {
    expect(
      readableToolActivityCommand({
        name: "bash",
        arguments: JSON.stringify({ action: "start_background", command: "npm run dev" }),
        display: { capability: "command.background" }
      })
    ).toBe("启动 npm run dev — command.background");
  });

  it("summarizes apply_patch result metadata as file updates", () => {
    const summary = summarizeToolActivity([
      {
        id: "tool-1",
        type: "tool_call",
        name: "apply_patch",
        status: "completed",
        result: JSON.stringify({
          changed_files: ["src/app.ts"],
          risk_summary: { added_lines: 3, deleted_lines: 1 }
        })
      } satisfies ThreadItem
    ]);

    expect(summary).toMatchObject({
      kind: "edit",
      text: "已编辑",
      fileName: "app.ts",
      additions: 3,
      deletions: 1,
      running: false,
      failed: false
    });
  });

  it("keeps MCP tool calls raw", () => {
    expect(
      readableToolActivityCommand({
        name: "mcp_docs_search",
        arguments: JSON.stringify({ query: "abc" })
      })
    ).toBe('mcp_docs_search {"query":"abc"}');
  });

  it("appends the capability suffix when display.capability is set", () => {
    expect(
      readableToolActivityCommand({
        name: "bash",
        arguments: JSON.stringify({ command: "npx vitest" }),
        display: { kind: "command", text: "运行 npx vitest", capability: "command.bash" }
      })
    ).toBe("运行 npx vitest — command.bash");
  });

  it("omits the capability suffix when display.capability is missing", () => {
    expect(
      readableToolActivityCommand({
        name: "bash",
        arguments: JSON.stringify({ command: "npx vitest" }),
        display: { kind: "command", text: "运行 npx vitest" }
      })
    ).toBe("运行 npx vitest");
  });
});

describe("buildToolActivityProcessSegments", () => {
  it("turns multiple file reads into a count segment", () => {
    const segments = buildToolActivityProcessSegments([
      {
        id: "tool-1",
        type: "tool_call",
        name: "read_file",
        status: "completed",
        arguments: JSON.stringify({ path: "src/App.tsx" }),
      },
      {
        id: "tool-2",
        type: "tool_call",
        name: "read_file",
        status: "completed",
        arguments: JSON.stringify({ path: "src/turns.css" }),
      },
    ] satisfies ThreadItem[]);

    expect(segments).toMatchObject([
      {
        kind: "read",
        countPrefix: "查看 ",
        count: 2,
        countSuffix: " 个文件",
      },
    ]);
  });

  it("compacts long OR search patterns by common prefix", () => {
    const segments = buildToolActivityProcessSegments([
      {
        id: "tool-1",
        type: "tool_call",
        name: "grep",
        status: "completed",
        arguments: JSON.stringify({
          pattern:
            "WORKSPACE_RIGHT_PANEL_MIN_WIDTH|WORKSPACE_RIGHT_PANEL_MAX_WIDTH|WORKSPACE_RIGHT_PANEL_DEFAULT_WIDTH",
        }),
      },
    ] satisfies ThreadItem[]);

    expect(segments).toMatchObject([
      {
        kind: "search",
        text: "搜索 WORKSPACE_RIGHT_PANEL_*",
      },
    ]);
  });
});
