import { afterEach, describe, expect, it } from "vitest";
import type { ThreadItem } from "../shared/protocol";
import { readableToolActivityCommand } from "./ToolActivity";
import {
  buildToolActivityProcessSegments,
  buildToolActivitySections,
  collectTurnSources,
  summarizeToolActivity,
} from "./ToolActivityHelpers";
import { setActiveLocale } from "./i18n";

afterEach(() => setActiveLocale("zh-CN"));

describe("readableToolActivityCommand", () => {
  it("uses structured rich tool results when the text projection is not JSON", () => {
    expect(
      readableToolActivityCommand({
        name: "web_fetch",
        arguments: JSON.stringify({ url: "https://request.test" }),
        result: "[image: screenshot (image/png)]",
        result_detail: {
          structured_content: { url: "https://result.test" },
          content: [{ type: "image", mime_type: "image/png", data: "aW1hZ2U=" }],
        },
      }),
    ).toBe("读取网页 https://result.test");
  });
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

  it("localizes generated activity copy without changing command arguments", () => {
    setActiveLocale("en-US");

    expect(
      readableToolActivityCommand({
        name: "bash",
        arguments: JSON.stringify({ command: "npm run typecheck" }),
      }),
    ).toBe("Run npm run typecheck");

    expect(
      summarizeToolActivity([
        {
          id: "tool-1",
          type: "tool_call",
          name: "bash",
          status: "completed",
          arguments: JSON.stringify({ command: "npm run typecheck" }),
        },
      ]).text,
    ).toBe("Ran 1 command");
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

  it("uses action-oriented copy for repeated searches and scheduled tasks", () => {
    const segments = buildToolActivityProcessSegments([
      {
        id: "search-1",
        type: "tool_call",
        name: "grep",
        status: "completed",
        arguments: JSON.stringify({ pattern: "cache_read" }),
      },
      {
        id: "search-2",
        type: "tool_call",
        name: "grep",
        status: "completed",
        arguments: JSON.stringify({ pattern: "cache_write" }),
      },
      {
        id: "search-3",
        type: "tool_call",
        name: "grep",
        status: "completed",
        arguments: JSON.stringify({ pattern: "cache_write" }),
      },
      {
        id: "schedule-1",
        type: "tool_call",
        name: "cron",
        status: "completed",
        arguments: JSON.stringify({ action: "list" }),
      },
    ] satisfies ThreadItem[]);

    expect(segments).toMatchObject([
      {
        kind: "search",
        countPrefix: "搜索 ",
        count: 3,
        countSuffix: " 次",
      },
      {
        kind: "schedule",
        text: "查看定时任务",
      },
    ]);
  });

  it("describes recognizable data-check commands by their purpose", () => {
    const [timeSegment] = buildToolActivityProcessSegments([
      {
        id: "time-1",
        type: "tool_call",
        name: "bash",
        status: "completed",
        arguments: JSON.stringify({ command: "date '+%H:%M'" }),
      },
    ] satisfies ThreadItem[]);
    expect(timeSegment).toMatchObject({
      kind: "command",
      text: "查看本地时间",
    });

    const [databaseSegment] = buildToolActivityProcessSegments([
      {
        id: "database-1",
        type: "tool_call",
        name: "bash",
        status: "completed",
        arguments: JSON.stringify({
          command: "python3 - <<'PY'\nimport sqlite3\nPY",
        }),
      },
    ] satisfies ThreadItem[]);
    expect(databaseSegment).toMatchObject({
      kind: "command",
      text: "操作本地数据库",
    });
  });

  it("does not mislabel commands that merely mention sqlite3 or date", () => {
    const label = (command: string) => {
      const [segment] = buildToolActivityProcessSegments([
        {
          id: `cmd-${command.length}`,
          type: "tool_call",
          name: "bash",
          status: "completed",
          arguments: JSON.stringify({ command }),
        },
      ] satisfies ThreadItem[]);
      return segment;
    };

    expect(label("rm sessions.sqlite3")).toMatchObject({
      kind: "command",
      text: "运行命令",
    });
    expect(label("go test ./internal/sqlite3/...")).toMatchObject({
      kind: "command",
      text: "运行测试",
    });
    expect(label("npm run typecheck && date")).toMatchObject({
      kind: "command",
      text: "检查类型",
    });
    expect(label("sqlite3 state.db 'SELECT 1'")).toMatchObject({
      kind: "command",
      text: "操作本地数据库",
    });
  });
});


describe("context (inception) tool activity", () => {
  const inceptionItem: ThreadItem = {
    id: "item_inception",
    type: "tool_call",
    status: "completed",
    name: "inception",
    arguments: JSON.stringify({ anchor_id: 0, summary: "## Task state\nContinue." }),
    display: {
      kind: "context",
      text: "潜入上下文 · 植入续行摘要",
      capability: "context.rewrite",
    },
  };

  it("builds a context section with the movie-flavored title and checkpoint detail", () => {
    const sections = buildToolActivitySections([inceptionItem]);
    expect(sections).toHaveLength(1);
    expect(sections[0].kind).toBe("context");
    expect(sections[0].title).toBe("潜入上下文 · 植入续行摘要");
    expect(sections[0].detail).toBe("checkpoint #0");
  });

  it("builds a context process segment with the movie-flavored text", () => {
    const [segment] = buildToolActivityProcessSegments([inceptionItem]);
    expect(segment.kind).toBe("context");
    expect(segment.text).toBe("潜入上下文 · 植入续行摘要");
    expect(segment.status).toBe("completed");
  });
});

describe("collectTurnSources", () => {
  it("returns an empty list when the turn has no web_search or web_fetch items", () => {
    expect(
      collectTurnSources([
        {
          id: "tool-1",
          type: "tool_call",
          name: "read_file",
          status: "completed",
          arguments: JSON.stringify({ path: "foo.ts" }),
        } satisfies ThreadItem,
      ]),
    ).toEqual([]);
  });

  it("collects one source per web_search hit with the canonical host", () => {
    expect(
      collectTurnSources([
        {
          id: "ws-1",
          type: "tool_call",
          name: "web_search",
          status: "completed",
          result: JSON.stringify({
            results: [
              {
                url: "https://www.anthropic.com/news/claude-opus-4-7",
                title: "Opus 4.7",
              },
              { url: "https://docs.anthropic.com/api", title: "API docs" },
            ],
          }),
        } satisfies ThreadItem,
      ]),
    ).toEqual([
      {
        url: "https://www.anthropic.com/news/claude-opus-4-7",
        host: "anthropic.com",
        title: "Opus 4.7",
        origin: "web_search",
      },
      {
        url: "https://docs.anthropic.com/api",
        host: "docs.anthropic.com",
        title: "API docs",
        origin: "web_search",
      },
    ]);
  });

  it("captures a web_fetch URL from arguments even when result has none", () => {
    expect(
      collectTurnSources([
        {
          id: "wf-1",
          type: "tool_call",
          name: "web_fetch",
          status: "completed",
          arguments: JSON.stringify({
            url: "https://platform.openai.com/docs/models",
          }),
          result: JSON.stringify({ status_code: 200, text: "..." }),
        } satisfies ThreadItem,
      ]),
    ).toEqual([
      {
        url: "https://platform.openai.com/docs/models",
        host: "platform.openai.com",
        origin: "web_fetch",
      },
    ]);
  });

  it("collapses multiple hits on the same host into a single favicon slot", () => {
    expect(
      collectTurnSources([
        {
          id: "ws-1",
          type: "tool_call",
          name: "web_search",
          status: "completed",
          result: JSON.stringify({
            results: [
              { url: "https://docs.anthropic.com/a", title: "Doc A" },
              { url: "https://docs.anthropic.com/b", title: "Doc B" },
            ],
          }),
        } satisfies ThreadItem,
      ]),
    ).toEqual([
      {
        url: "https://docs.anthropic.com/a",
        host: "docs.anthropic.com",
        title: "Doc A",
        origin: "web_search",
      },
    ]);
  });

  it("upgrades a slot's title when a later hit on the same host provides one", () => {
    expect(
      collectTurnSources([
        {
          id: "wf-1",
          type: "tool_call",
          name: "web_fetch",
          status: "completed",
          arguments: JSON.stringify({
            url: "https://www.anthropic.com/landing",
          }),
        } satisfies ThreadItem,
        {
          id: "ws-1",
          type: "tool_call",
          name: "web_search",
          status: "completed",
          result: JSON.stringify({
            results: [
              {
                url: "https://anthropic.com/news/claude-opus-4-7",
                title: "Claude Opus 4.7 迁移指南",
              },
            ],
          }),
        } satisfies ThreadItem,
      ]),
    ).toEqual([
      {
        url: "https://www.anthropic.com/landing",
        host: "anthropic.com",
        title: "Claude Opus 4.7 迁移指南",
        origin: "web_fetch",
      },
    ]);
  });

  it("preserves first-seen order across the turn", () => {
    const sources = collectTurnSources([
      {
        id: "ws-1",
        type: "tool_call",
        name: "web_search",
        status: "completed",
        result: JSON.stringify({
          results: [{ url: "https://example.com/a" }],
        }),
      } satisfies ThreadItem,
      {
        id: "wf-1",
        type: "tool_call",
        name: "web_fetch",
        status: "completed",
        arguments: JSON.stringify({ url: "https://other.test/page" }),
      } satisfies ThreadItem,
    ]);
    expect(sources.map((source) => source.host)).toEqual([
      "example.com",
      "other.test",
    ]);
  });

  it("skips web_search hits with no parseable URL", () => {
    expect(
      collectTurnSources([
        {
          id: "ws-1",
          type: "tool_call",
          name: "web_search",
          status: "completed",
          result: JSON.stringify({
            results: [
              { title: "no URL" },
              { url: "https://valid.example/x" },
            ],
          }),
        } satisfies ThreadItem,
      ]),
    ).toEqual([
      {
        url: "https://valid.example/x",
        host: "valid.example",
        origin: "web_search",
      },
    ]);
  });

  it("treats collab_agent_tool_call web_search items the same as tool_call", () => {
    expect(
      collectTurnSources([
        {
          id: "ws-collab",
          type: "collab_agent_tool_call",
          name: "web_search",
          status: "completed",
          result: JSON.stringify({
            results: [
              { url: "https://collab.example/x", title: "Collab result" },
            ],
          }),
        } satisfies ThreadItem,
      ]),
    ).toEqual([
      {
        url: "https://collab.example/x",
        host: "collab.example",
        title: "Collab result",
        origin: "web_search",
      },
    ]);
  });
});
