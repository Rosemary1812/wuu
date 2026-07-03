import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { RuntimeContext, Thread } from "../shared/protocol";
import {
  activeTurnTokenSpeed,
  activeTurnTokenSpeedSnapshot,
  appendStreamingTokenSample,
  appendTurnTokenSample,
  conversationPaneThreadsByID,
  createThreadSessionTab,
  handleStreamingNotification,
  initialState,
  isScratchThread,
  isThreadUnread,
  latestCompletedTurnID,
  latestContextUsageForThread,
  markThreadTurnsViewed,
  mentionedParticipantIDsFromText,
  openForkThreadAsPrimary,
  queryTextsForThread,
  reduceServerEvent,
  sortThreads,
  summarizeThreadsForSidebar,
  threadBelongsToProject,
  threadProjectPath,
  threadSessionTabID,
  turnStreamStatusForThread,
  type SessionTab,
} from "./AppState";
import { streamTextKey, streamTextStore } from "./StreamText";

function installManualRAF(): {
  flush: () => void;
  restore: () => void;
} {
  const realRAF = window.requestAnimationFrame;
  const pending: FrameRequestCallback[] = [];
  let nextHandle = 1;
  window.requestAnimationFrame = ((cb: FrameRequestCallback) => {
    pending.push(cb);
    return nextHandle++;
  }) as typeof window.requestAnimationFrame;
  return {
    flush: () => {
      const callbacks = pending.splice(0);
      for (const cb of callbacks) {
        cb(performance.now());
      }
    },
    restore: () => {
      window.requestAnimationFrame = realRAF;
    },
  };
}

function handoffText(): string {
  return JSON.stringify({
    author: "/root/helpme_recovery",
    recipient: "/root",
    content: `<subagent_notification>\n${JSON.stringify({
      agent_path: "/root/helpme_recovery",
      status: {
        type: "agent_result",
        agent_id: "worker-1",
        task_name: "helpme_recovery",
        status: "completed"
      }
    })}\n</subagent_notification>`,
    trigger_turn: true
  });
}

function threadWithUserTexts(texts: string[]): Thread {
  return {
    id: "thread-1",
    preview: "preview",
    model_provider: "fake",
    model: "fake-model",
    cwd: "/repo",
    status: "idle",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    turns: [
      {
        id: "turn-1",
        items_view: "full",
        status: "completed",
        items: texts.map((text, index) => ({
          id: `user-${index + 1}`,
          type: "user_message",
          status: "completed",
          role: "user",
          text
        }))
      }
    ]
  };
}

function sessionTabPrompt(
  tabs: SessionTab[],
  tabID: string,
): string | undefined {
  const tab = tabs.find((candidate) => candidate.id === tabID);
  if (!tab || tab.kind === "file" || tab.kind === "skills") {
    return undefined;
  }
  return tab.prompt;
}

describe("AppState server requests", () => {
  it("keeps tool approval requests pending instead of rejecting them", () => {
    const rejectServerRequest = vi.fn();
    Object.defineProperty(window, "wuu", {
      configurable: true,
      value: { rejectServerRequest },
    });

    const next = reduceServerEvent(initialState, {
      kind: "server-request",
      workdir: "/tmp/project",
      message: {
        id: "server-request-1",
        method: "tool/approval/request",
        params: {
          id: "approval-1",
          tool_name: "run_shell",
          risk: "high",
          arguments_preview: "{\"command\":\"printf hi\"}",
          permission: "command.bash",
          permission_patterns: ["printf hi"],
          capability: "command.bash",
          capability_object: "printf hi",
          capability_action: "execute",
          capability_rule: "bash-readonly-echo",
        },
      },
    });

    expect(rejectServerRequest).not.toHaveBeenCalled();
    expect(next.pendingToolApproval?.server_request_id).toBe("server-request-1");
    expect(next.pendingToolApproval?.tool_name).toBe("run_shell");
    expect(next.pendingToolApproval?.permission).toBe("command.bash");
    expect(next.pendingToolApproval?.permission_patterns).toEqual(["printf hi"]);
    expect(next.pendingToolApproval?.capability).toBe("command.bash");
    expect(next.pendingToolApproval?.capability_object).toBe("printf hi");
    expect(next.pendingToolApproval?.capability_action).toBe("execute");
    expect(next.pendingToolApproval?.capability_rule).toBe("bash-readonly-echo");
    expect(next.status).toBe(initialState.status);
  });

  it("does not mark the active thread waiting when a background thread needs approval", () => {
    const rejectServerRequest = vi.fn();
    Object.defineProperty(window, "wuu", {
      configurable: true,
      value: { rejectServerRequest },
    });
    const context: RuntimeContext = {
      kind: "no_project",
      cwd: "/tmp/project",
    };
    const activeThread: Thread = {
      ...threadWithUserTexts(["active prompt"]),
      id: "active-thread",
      preview: "active",
      cwd: context.cwd,
    };
    const backgroundThread: Thread = {
      ...threadWithUserTexts(["background prompt"]),
      id: "background-thread",
      preview: "background",
      cwd: context.cwd,
      status: "in_progress",
      turns: [
        {
          id: "background-turn",
          items_view: "full",
          status: "in_progress",
          items: [
            {
              id: "background-call-item",
              source_id: "background-call",
              type: "tool_call",
              status: "in_progress",
              name: "run_shell",
              arguments: "{\"command\":\"npm install\"}",
            },
          ],
        },
      ],
    };

    const next = reduceServerEvent(
      {
        ...initialState,
        activeContext: context,
        thread: activeThread,
        activePane: "primary",
        activeSessionTabID: threadSessionTabID(activeThread.id),
        sessionTabs: [createThreadSessionTab(activeThread, context)],
        threads: [activeThread, backgroundThread],
        status: "ready",
      },
      {
        kind: "server-request",
        workdir: context.cwd,
        message: {
          id: "server-request-1",
          method: "tool/approval/request",
          params: {
            id: "approval-1",
            tool_name: "run_shell",
            call_id: "background-call",
            risk: "high",
            arguments_preview: "{\"command\":\"npm install\"}",
          },
        },
      },
    );

    expect(rejectServerRequest).not.toHaveBeenCalled();
    expect(next.pendingToolApproval?.call_id).toBe("background-call");
    expect(next.status).toBe("ready");
  });
});

describe("queryTextsForThread", () => {
  it("skips internal agent handoff messages", () => {
    const thread = threadWithUserTexts([handoffText(), "真正的用户问题"]);

    expect(queryTextsForThread(thread)).toEqual(["真正的用户问题"]);
  });
});

describe("summarizeThreadsForSidebar", () => {
  it("keeps sidebar thread data free of turn item payloads", () => {
    const [summary] = summarizeThreadsForSidebar([
      threadWithUserTexts(["secret message body"]),
    ]);

    expect(summary.turn_count).toBe(1);
    expect(summary.turns[0]).toEqual({
      id: "turn-1",
      status: "completed",
      started_at: undefined,
      completed_at: undefined,
      duration_ms: undefined,
    });
    expect(JSON.stringify(summary)).not.toContain("secret message body");
  });

  it("preserves dm_participant_id through summarization", () => {
    const [summary] = summarizeThreadsForSidebar([
      {
        ...threadWithUserTexts(["dm hi"]),
        dm_participant_id: "participant-7",
      },
    ]);

    expect(summary.dm_participant_id).toBe("participant-7");
  });

  it("groups worktree fork sessions by their base repo", () => {
    const project = {
      id: "project-1",
      name: "project",
      path: "/repo/project",
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    };
    const [summary] = summarizeThreadsForSidebar([
      {
        ...threadWithUserTexts(["continue in a worktree"]),
        cwd: "/Users/me/.wuu/worktrees/fork-1/project",
        workspace_kind: "scratch",
        worktree: {
          path: "/Users/me/.wuu/worktrees/fork-1/project",
          base_repo: "/repo/project",
          base_head: "d955824f",
        },
      },
    ]);

    expect(summary.worktree?.base_repo).toBe("/repo/project");
    expect(threadProjectPath(summary)).toBe("/repo/project");
    expect(threadBelongsToProject(summary, project)).toBe(true);
    expect(isScratchThread(summary, [project])).toBe(false);
  });
});

describe("openForkThreadAsPrimary", () => {
  const context: RuntimeContext = {
    kind: "project",
    project_id: "project-1",
    cwd: "/repo",
  };

  it("opens a fork as the primary conversation instead of creating a split", () => {
    const source: Thread = {
      ...threadWithUserTexts(["source prompt"]),
      id: "source-thread",
      preview: "source",
    };
    const fork: Thread = {
      ...threadWithUserTexts(["source prompt"]),
      id: "fork-thread",
      preview: "fork",
      forked_from_id: source.id,
    };

    const next = openForkThreadAsPrimary(
      {
        ...initialState,
        activeContext: context,
        thread: source,
        activePane: "primary",
        activeSessionTabID: threadSessionTabID(source.id),
        sessionTabs: [createThreadSessionTab(source, context)],
        threads: [source],
        status: "ready",
      },
      {
        sourceThread: source,
        forkThread: fork,
        context,
        sourceDraft: {
          prompt: "keep the source draft",
          images: [],
          files: [],
        },
      },
    );

    expect(next.thread?.id).toBe(fork.id);
    expect(next.secondaryThread).toBeUndefined();
    expect(next.activePane).toBe("primary");
    expect(next.activeSessionTabID).toBe(threadSessionTabID(fork.id));
    expect(sessionTabPrompt(next.sessionTabs, threadSessionTabID(source.id))).toBe(
      "keep the source draft",
    );
  });

  it("collapses an existing split and keeps both original pane drafts in tabs", () => {
    const source: Thread = {
      ...threadWithUserTexts(["source prompt"]),
      id: "source-thread",
      preview: "source",
    };
    const secondary: Thread = {
      ...threadWithUserTexts(["secondary prompt"]),
      id: "secondary-thread",
      preview: "secondary",
    };
    const fork: Thread = {
      ...threadWithUserTexts(["source prompt"]),
      id: "fork-thread",
      preview: "fork",
      forked_from_id: source.id,
    };

    const next = openForkThreadAsPrimary(
      {
        ...initialState,
        activeContext: context,
        thread: source,
        secondaryThread: secondary,
        activePane: "secondary",
        activeSessionTabID: threadSessionTabID(secondary.id),
        sessionTabs: [
          createThreadSessionTab(source, context),
          createThreadSessionTab(secondary, context),
        ],
        threads: [source, secondary],
        status: "ready",
      },
      {
        sourceThread: source,
        forkThread: fork,
        context,
        sourceDraft: {
          prompt: "left draft",
          images: [],
          files: [],
        },
        splitDrafts: {
          primary: {
            prompt: "left draft",
            images: [],
            files: [],
          },
          secondary: {
            prompt: "right draft",
            images: [],
            files: [],
          },
        },
      },
    );

    expect(next.thread?.id).toBe(fork.id);
    expect(next.secondaryThread).toBeUndefined();
    expect(next.activePane).toBe("primary");
    expect(
      sessionTabPrompt(next.sessionTabs, threadSessionTabID(source.id)),
    ).toBe("left draft");
    expect(
      sessionTabPrompt(next.sessionTabs, threadSessionTabID(secondary.id)),
    ).toBe("right draft");
  });
});

describe("worktree thread context matching", () => {
  it("applies worktree fork updates while the base repo project is active", () => {
    const worktreeThread: Thread = {
      ...threadWithUserTexts(["continue in worktree"]),
      id: "worktree-thread",
      cwd: "/Users/me/.wuu/worktrees/fork-1/project",
      preview: "before",
      worktree: {
        path: "/Users/me/.wuu/worktrees/fork-1/project",
        base_repo: "/repo",
        base_head: "d955824f",
      },
    };
    const updatedThread: Thread = {
      ...worktreeThread,
      preview: "after",
    };

    const next = reduceServerEvent(
      {
        ...initialState,
        activeContext: {
          kind: "project",
          project_id: "project-1",
          cwd: "/repo",
        },
        thread: worktreeThread,
        threads: [worktreeThread],
        status: "ready",
      },
      {
        kind: "notification",
        workdir: "/repo",
        message: {
          method: "thread/updated",
          params: { thread: updatedThread },
        },
      },
    );

    expect(next.thread?.preview).toBe("after");
  });
});

describe("AppState token usage", () => {
  it("initializes token usage state before the first usage update", () => {
    expect(initialState.turnTokenUsage).toEqual({});
    expect(initialState.turnRequestContext).toEqual({});
    expect(activeTurnTokenSpeed(initialState, "turn-1")).toBe(0);
  });

  it("derives token speed from cumulative output-token samples", () => {
    const first = appendTurnTokenSample(
      initialState,
      "turn-1",
      "thread-1",
      10,
      2,
      0,
      0,
      1_000,
    );
    const second = appendTurnTokenSample(
      first,
      "turn-1",
      "thread-1",
      10,
      22,
      4,
      8,
      2_000,
    );

    expect(activeTurnTokenSpeed(second, "turn-1")).toBe(20);
    expect(activeTurnTokenSpeedSnapshot(second, "turn-1").source).toBe("real");
    expect(second.turnTokenUsage["turn-1"].cacheCreationTokens).toBe(4);
    expect(second.turnTokenUsage["turn-1"].cacheReadTokens).toBe(8);
  });

  it("derives live token speed from streamed model output deltas", () => {
    const first = appendStreamingTokenSample(
      initialState,
      {
        thread_id: "thread-1",
        turn_id: "turn-1",
        delta: "aaaaaaaa",
      },
      1_000,
    );
    const second = appendStreamingTokenSample(
      first,
      {
        thread_id: "thread-1",
        turn_id: "turn-1",
        delta: "bbbbbbbb",
      },
      2_000,
    );

    expect(activeTurnTokenSpeed(second, "turn-1")).toBe(2);
    expect(activeTurnTokenSpeedSnapshot(second, "turn-1").source).toBe("estimated");
    expect(second.turnTokenUsage["turn-1"].outputTokens).toBe(0);
  });

  it("discards estimated samples when real provider usage arrives", () => {
    const estimated = appendStreamingTokenSample(
      initialState,
      {
        thread_id: "thread-1",
        turn_id: "turn-1",
        delta: "aaaaaaaaaaaaaaaa",
      },
      1_000,
    );
    const real = appendTurnTokenSample(
      estimated,
      "turn-1",
      "thread-1",
      10,
      3,
      0,
      0,
      1_500,
    );

    expect(activeTurnTokenSpeedSnapshot(real, "turn-1").source).toBe("real");
    expect(real.turnTokenUsage["turn-1"].samples).toEqual([
      { tokens: 3, at: 1_500 },
    ]);
  });

  it("attaches request context diagnostics to the latest context usage", () => {
    const thread = threadWithUserTexts(["hi"]);
    const stateWithUsage = appendTurnTokenSample(
      {
        ...initialState,
        thread,
      },
      "turn-1",
      "thread-1",
      100,
      10,
      0,
      0,
      1_000,
      200_000,
      "fake-model",
      12_000,
    );
    const next = reduceServerEvent(stateWithUsage, {
      kind: "notification",
      workdir: "/repo",
      message: {
        method: "turn/event",
        params: {
          thread_id: "thread-1",
          turn_id: "turn-1",
          event: {
            request_context: {
              step_index: 0,
              message_count: 8,
              stable_prefix: 5,
              turn_prefix: 6,
              transient_messages: 1,
              hidden_messages: 1,
              tool_count: 14,
              stable_prefix_bytes: 3200,
              turn_prefix_bytes: 4100,
              message_bytes: 9800,
              dynamic_bytes: 1200,
              tool_schema_bytes: 22000,
              prompt_cache_key: "thread-1",
              stable_prefix_hash: "stable",
              turn_prefix_hash: "turn",
              tool_surface_hash: "tools",
            },
          },
        },
      },
    });

    const usage = latestContextUsageForThread(next, thread);
    expect(usage?.requestContext?.stablePrefix).toBe(5);
    expect(usage?.requestContext?.turnPrefix).toBe(6);
    expect(usage?.requestContext?.toolCount).toBe(14);
    expect(usage?.requestContext?.promptCacheKey).toBe("thread-1");
  });

  it("surfaces live stream reconnect attempts as transient turn status", () => {
    const thread: Thread = {
      ...threadWithUserTexts(["hi"]),
      turns: [
        {
          id: "turn-1",
          items_view: "full",
          status: "in_progress",
          items: [],
        },
      ],
    };
    const state = {
      ...initialState,
      thread,
      threads: [thread],
      running: true,
    };

    const reconnecting = reduceServerEvent(state, {
      kind: "notification",
      workdir: "/repo",
      message: {
        method: "turn/event",
        params: {
          thread_id: "thread-1",
          turn_id: "turn-1",
          event: {
            lifecycle: {
              phase: "reconnecting",
              attempt: 2,
              retry_count: 1,
              max_retries: 3,
            },
          },
        },
      },
    });

    expect(turnStreamStatusForThread(reconnecting, reconnecting.thread)).toEqual({
      text: "消息流重连中 1/3",
      liveProgress: true,
    });

    const connected = reduceServerEvent(reconnecting, {
      kind: "notification",
      workdir: "/repo",
      message: {
        method: "turn/event",
        params: {
          thread_id: "thread-1",
          turn_id: "turn-1",
          event: {
            lifecycle: {
              phase: "connected",
              attempt: 2,
            },
          },
        },
      },
    });

    expect(turnStreamStatusForThread(connected, connected.thread)).toBeUndefined();
  });

  it("uses provider transport in stream reconnect status when available", () => {
    const thread: Thread = {
      ...threadWithUserTexts(["hi"]),
      turns: [
        {
          id: "turn-1",
          items_view: "full",
          status: "in_progress",
          items: [],
        },
      ],
    };
    const state = {
      ...initialState,
      thread,
      threads: [thread],
      running: true,
    };

    const withTransport = reduceServerEvent(state, {
      kind: "notification",
      workdir: "/repo",
      message: {
        method: "turn/event",
        params: {
          thread_id: "thread-1",
          turn_id: "turn-1",
          event: {
            provider_state: {
              transport: "http",
            },
          },
        },
      },
    });
    const reconnecting = reduceServerEvent(withTransport, {
      kind: "notification",
      workdir: "/repo",
      message: {
        method: "turn/event",
        params: {
          thread_id: "thread-1",
          turn_id: "turn-1",
          event: {
            lifecycle: {
              phase: "reconnecting",
              retry_count: 2,
              max_retries: 3,
            },
          },
        },
      },
    });

    expect(turnStreamStatusForThread(reconnecting, reconnecting.thread)).toEqual({
      text: "HTTP 消息流重连中 2/3",
      liveProgress: true,
    });
  });

  it("surfaces websocket to http fallback as a static stream status", () => {
    const thread: Thread = {
      ...threadWithUserTexts(["hi"]),
      turns: [
        {
          id: "turn-1",
          items_view: "full",
          status: "in_progress",
          items: [],
        },
      ],
    };
    const state = {
      ...initialState,
      thread,
      threads: [thread],
      running: true,
    };

    const fallback = reduceServerEvent(state, {
      kind: "notification",
      workdir: "/repo",
      message: {
        method: "turn/event",
        params: {
          thread_id: "thread-1",
          turn_id: "turn-1",
          event: {
            provider_state: {
              diagnostic: "provider_transport_failure",
              transport: "http",
              failed_transport: "websocket",
              fallback_transport: "http",
              fallback_active: true,
              transport_failure_phase: "before_message_stream_start",
            },
          },
        },
      },
    });

    expect(turnStreamStatusForThread(fallback, fallback.thread)).toEqual({
      text: "WebSocket 不可用，已切到 HTTP",
      liveProgress: false,
    });
  });

  it("surfaces transport interruption after message stream start", () => {
    const thread: Thread = {
      ...threadWithUserTexts(["hi"]),
      turns: [
        {
          id: "turn-1",
          items_view: "full",
          status: "in_progress",
          items: [],
        },
      ],
    };
    const state = {
      ...initialState,
      thread,
      threads: [thread],
      running: true,
    };

    const interrupted = reduceServerEvent(state, {
      kind: "notification",
      workdir: "/repo",
      message: {
        method: "turn/event",
        params: {
          thread_id: "thread-1",
          turn_id: "turn-1",
          event: {
            provider_state: {
              diagnostic: "provider_transport_failure",
              transport: "websocket",
              failed_transport: "websocket",
              events_emitted: true,
              transport_failure_phase: "after_message_stream_start",
            },
          },
        },
      },
    });

    expect(turnStreamStatusForThread(interrupted, interrupted.thread)).toEqual({
      text: "WebSocket 消息流中断",
      liveProgress: false,
    });
  });

  it("clears stream reconnect status when the turn settles", () => {
    const thread: Thread = {
      ...threadWithUserTexts(["hi"]),
      turns: [
        {
          id: "turn-1",
          items_view: "full",
          status: "in_progress",
          items: [],
        },
      ],
    };
    const state = reduceServerEvent(
      {
        ...initialState,
        thread,
        threads: [thread],
        running: true,
      },
      {
        kind: "notification",
        workdir: "/repo",
        message: {
          method: "turn/event",
          params: {
            thread_id: "thread-1",
            turn_id: "turn-1",
            event: {
              lifecycle: {
                phase: "reconnecting",
                retry_count: 2,
              },
            },
          },
        },
      },
    );

    const settledTurn = {
      ...thread.turns[0],
      status: "failed" as const,
    };
    const settled = reduceServerEvent(state, {
      kind: "notification",
      workdir: "/repo",
      message: {
        method: "turn/error",
        params: {
          thread_id: "thread-1",
          turn: settledTurn,
        },
      },
    });

    expect(turnStreamStatusForThread(settled, settled.thread)).toBeUndefined();
  });
});

describe("AppState stream cache lifecycle", () => {
  afterEach(() => {
    streamTextStore.clearItem("turn-1", "agent-1");
    streamTextStore.clearItem("turn-bg", "agent-bg");
  });

  it("keeps visible text through an empty replace while resetting the next delta base", () => {
    const key = streamTextKey("turn-1", "agent-1", "text");
    const state = {
      ...initialState,
      thread: threadWithUserTexts(["hi"]),
    };

    handleStreamingNotification(
      {
        kind: "notification",
        workdir: "/repo",
        message: {
          method: "item/agentMessage/delta",
          params: {
            thread_id: "thread-1",
            turn_id: "turn-1",
            item_id: "agent-1",
            delta: "stale partial",
          },
        },
      },
      state,
    );
    handleStreamingNotification(
      {
        kind: "notification",
        workdir: "/repo",
        message: {
          method: "item/agentMessage/replace",
          params: {
            thread_id: "thread-1",
            turn_id: "turn-1",
            item_id: "agent-1",
            text: "",
          },
        },
      },
      state,
    );

    expect(streamTextStore.get(key)).toBe("stale partial");

    handleStreamingNotification(
      {
        kind: "notification",
        workdir: "/repo",
        message: {
          method: "item/agentMessage/delta",
          params: {
            thread_id: "thread-1",
            turn_id: "turn-1",
            item_id: "agent-1",
            delta: "fresh answer",
          },
        },
      },
      state,
    );

    expect(streamTextStore.get(key)).toBe("fresh answer");
  });

  it("keeps stream deltas for a known background thread", () => {
    const key = streamTextKey("turn-bg", "agent-bg", "text");
    const activeThread = threadWithUserTexts(["active"]);
    const backgroundThread = {
      ...threadWithUserTexts(["background"]),
      id: "thread-bg",
    };

    const handling = handleStreamingNotification(
      {
        kind: "notification",
        workdir: "/repo",
        message: {
          method: "item/agentMessage/delta",
          params: {
            thread_id: "thread-bg",
            turn_id: "turn-bg",
            item_id: "agent-bg",
            delta: "background text",
          },
        },
      },
      {
        ...initialState,
        thread: activeThread,
        threads: [activeThread, backgroundThread],
      },
    );

    expect(handling).toBe("background-stream");
    expect(streamTextStore.get(key)).toBe("background text");
  });

  it("syncs completed snapshots for a known background thread", () => {
    const raf = installManualRAF();
    const key = streamTextKey("turn-bg", "agent-bg", "text");
    const activeThread = threadWithUserTexts(["active"]);
    const backgroundThread = {
      ...threadWithUserTexts(["background"]),
      id: "thread-bg",
    };
    streamTextStore.set(key, "partial");

    try {
      const handling = handleStreamingNotification(
        {
          kind: "notification",
          workdir: "/repo",
          message: {
            method: "item/completed",
            params: {
              thread_id: "thread-bg",
              turn_id: "turn-bg",
              item: {
                id: "agent-bg",
                type: "agent_message",
                status: "completed",
                text: "partial complete",
              },
            },
          },
        },
        {
          ...initialState,
          thread: activeThread,
          threads: [activeThread, backgroundThread],
        },
      );

      expect(handling).toBe("state");
      expect(streamTextStore.get(key)).toBe("partial complete");
      raf.flush();
      expect(streamTextStore.has(key)).toBe(false);
    } finally {
      raf.restore();
    }
  });

  it("releases completed agent text from the stream cache once a final snapshot exists", () => {
    const raf = installManualRAF();
    const key = streamTextKey("turn-1", "agent-1", "text");
    streamTextStore.set(key, "Final answer");
    try {
      const handling = handleStreamingNotification(
        {
          kind: "notification",
          workdir: "/repo",
          message: {
            method: "item/completed",
            params: {
              thread_id: "thread-1",
              turn_id: "turn-1",
              item: {
                id: "agent-1",
                type: "agent_message",
                status: "completed",
                text: "Final answer",
              },
            },
          },
        },
        {
          ...initialState,
          thread: threadWithUserTexts(["hi"]),
        },
      );

      expect(handling).toBe("state");
      expect(streamTextStore.has(key)).toBe(true);
      raf.flush();
      expect(streamTextStore.has(key)).toBe(false);
    } finally {
      raf.restore();
    }
  });

  it("keeps completed agent text cached while the completed snapshot is empty", () => {
    const raf = installManualRAF();
    const key = streamTextKey("turn-1", "agent-1", "text");
    streamTextStore.set(key, "Final answer");
    try {
      handleStreamingNotification(
        {
          kind: "notification",
          workdir: "/repo",
          message: {
            method: "item/completed",
            params: {
              thread_id: "thread-1",
              turn_id: "turn-1",
              item: {
                id: "agent-1",
                type: "agent_message",
                status: "completed",
                text: "",
              },
            },
          },
        },
        {
          ...initialState,
          thread: threadWithUserTexts(["hi"]),
        },
      );

      raf.flush();
      expect(streamTextStore.has(key)).toBe(true);
      expect(streamTextStore.get(key)).toBe("Final answer");
    } finally {
      raf.restore();
    }
  });

  it("keeps completed agent text cached while the completed snapshot is behind the stream", () => {
    const raf = installManualRAF();
    const key = streamTextKey("turn-1", "agent-1", "text");
    streamTextStore.set(key, "Final answer");
    try {
      handleStreamingNotification(
        {
          kind: "notification",
          workdir: "/repo",
          message: {
            method: "item/completed",
            params: {
              thread_id: "thread-1",
              turn_id: "turn-1",
              item: {
                id: "agent-1",
                type: "agent_message",
                status: "completed",
                text: "Final",
              },
            },
          },
        },
        {
          ...initialState,
          thread: threadWithUserTexts(["hi"]),
        },
      );

      raf.flush();
      expect(streamTextStore.has(key)).toBe(true);
      expect(streamTextStore.get(key)).toBe("Final answer");
    } finally {
      raf.restore();
    }
  });

  it("releases buffered streams when a completed turn carries final item snapshots", () => {
    const textKey = streamTextKey("turn-1", "agent-1", "text");
    const resultKey = streamTextKey("turn-1", "agent-1", "result");
    streamTextStore.set(textKey, "Final answer");
    streamTextStore.set(resultKey, "Tool result");

    reduceServerEvent(
      {
        ...initialState,
        activeContext: { kind: "no_project", cwd: "/repo" },
        thread: threadWithUserTexts(["hi"]),
        threads: [threadWithUserTexts(["hi"])],
      },
      {
        kind: "notification",
        workdir: "/repo",
        message: {
          method: "turn/completed",
          params: {
            thread_id: "thread-1",
            turn: {
              id: "turn-1",
              items_view: "full",
              status: "completed",
              items: [
                {
                  id: "agent-1",
                  type: "agent_message",
                  status: "completed",
                  text: "Final answer",
                  result: "Tool result",
                },
              ],
            },
          },
        },
      },
    );

    expect(streamTextStore.has(textKey)).toBe(false);
    expect(streamTextStore.has(resultKey)).toBe(false);
  });

  it("keeps buffered streams when a completed turn snapshot is behind the stream", () => {
    const textKey = streamTextKey("turn-1", "agent-1", "text");
    const resultKey = streamTextKey("turn-1", "agent-1", "result");
    streamTextStore.set(textKey, "Final answer");
    streamTextStore.set(resultKey, "Tool result");

    reduceServerEvent(
      {
        ...initialState,
        activeContext: { kind: "no_project", cwd: "/repo" },
        thread: threadWithUserTexts(["hi"]),
        threads: [threadWithUserTexts(["hi"])],
      },
      {
        kind: "notification",
        workdir: "/repo",
        message: {
          method: "turn/completed",
          params: {
            thread_id: "thread-1",
            turn: {
              id: "turn-1",
              items_view: "full",
              status: "completed",
              items: [
                {
                  id: "agent-1",
                  type: "agent_message",
                  status: "completed",
                  text: "Final",
                  result: "Tool",
                },
              ],
            },
          },
        },
      },
    );

    expect(streamTextStore.has(textKey)).toBe(true);
    expect(streamTextStore.get(textKey)).toBe("Final answer");
    expect(streamTextStore.has(resultKey)).toBe(true);
    expect(streamTextStore.get(resultKey)).toBe("Tool result");
  });
});

describe("turn token speed", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 0, 1, 0, 0, 0));
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("returns 0 when there are no samples", () => {
    expect(activeTurnTokenSpeed(initialState, "turn-1")).toBe(0);
  });

  it("returns 0 with fewer than two samples", () => {
    const state = appendTurnTokenSample(
      initialState,
      "turn-1",
      "thread-1",
      0,
      100,
      0,
      0,
      Date.now(),
    );
    expect(activeTurnTokenSpeed(state, "turn-1")).toBe(0);
  });

  it("computes tok/s from the oldest to the newest sample", () => {
    let state = appendTurnTokenSample(
      initialState,
      "turn-1",
      "thread-1",
      0,
      100,
      0,
      0,
      Date.now(),
    );
    vi.advanceTimersByTime(500);
    state = appendTurnTokenSample(
      state,
      "turn-1",
      "thread-1",
      0,
      140,
      0,
      0,
      Date.now(),
    );
    expect(activeTurnTokenSpeed(state, "turn-1")).toBeCloseTo(80, 0);
  });

  it("ignores unchanged usage snapshots while tools are running", () => {
    let state = appendTurnTokenSample(
      initialState,
      "turn-1",
      "thread-1",
      0,
      100,
      0,
      0,
      Date.now(),
    );
    vi.advanceTimersByTime(500);
    state = appendTurnTokenSample(
      state,
      "turn-1",
      "thread-1",
      0,
      140,
      0,
      0,
      Date.now(),
    );

    vi.advanceTimersByTime(2500);
    state = appendTurnTokenSample(
      state,
      "turn-1",
      "thread-1",
      20,
      140,
      0,
      0,
      Date.now(),
    );

    const speed = activeTurnTokenSpeedSnapshot(state, "turn-1");
    expect(speed.tokensPerSecond).toBeCloseTo(80, 0);
    expect(speed.sampledAt).toBe(new Date(2026, 0, 1, 0, 0, 0, 500).getTime());
    expect(state.turnTokenUsage["turn-1"].samples).toEqual([
      { tokens: 100, at: new Date(2026, 0, 1, 0, 0, 0).getTime() },
      { tokens: 140, at: new Date(2026, 0, 1, 0, 0, 0, 500).getTime() },
    ]);
  });

  it("drops samples older than the 2s window", () => {
    let state = appendTurnTokenSample(
      initialState,
      "turn-1",
      "thread-1",
      0,
      100,
      0,
      0,
      Date.now(),
    );
    vi.advanceTimersByTime(2500);
    state = appendTurnTokenSample(
      state,
      "turn-1",
      "thread-1",
      0,
      200,
      0,
      0,
      Date.now(),
    );
    expect(state.turnTokenUsage["turn-1"].samples).toHaveLength(1);
    expect(activeTurnTokenSpeed(state, "turn-1")).toBe(0);
  });
});

describe("AppState unread tracking", () => {
  function makeThreadWithTurns(
    threadID: string,
    turns: Array<{
      id: string;
      status: "completed" | "in_progress" | "failed" | "interrupted";
    }>,
  ): Thread {
    return {
      id: threadID,
      preview: "",
      model_provider: "fake",
      model: "fake-model",
      cwd: "/tmp",
      status: "idle",
      created_at: "2026-06-18T00:00:00Z",
      updated_at: "2026-06-18T00:00:00Z",
      turns: turns.map((t) => ({
        id: t.id,
        items: [],
        items_view: "full" as const,
        status: t.status,
      })),
    };
  }

  it("latestCompletedTurnID returns the most recent non-in_progress turn", () => {
    const thread = makeThreadWithTurns("thread-1", [
      { id: "turn-1", status: "completed" },
      { id: "turn-2", status: "completed" },
      { id: "turn-3", status: "completed" },
    ]);
    expect(latestCompletedTurnID(thread)).toBe("turn-3");
  });

  it("latestCompletedTurnID returns undefined when the latest turn is in_progress", () => {
    const thread = makeThreadWithTurns("thread-1", [
      { id: "turn-1", status: "completed" },
      { id: "turn-2", status: "in_progress" },
    ]);
    expect(latestCompletedTurnID(thread)).toBeUndefined();
  });

  it("latestCompletedTurnID returns undefined for an empty thread", () => {
    const thread = makeThreadWithTurns("thread-1", []);
    expect(latestCompletedTurnID(thread)).toBeUndefined();
  });

  it("isThreadUnread returns true for a thread with a new completed turn", () => {
    const thread = makeThreadWithTurns("thread-1", [
      { id: "turn-1", status: "completed" },
    ]);
    expect(isThreadUnread(thread, undefined)).toBe(true);
  });

  it("isThreadUnread returns false when lastViewed matches the latest turn", () => {
    const thread = makeThreadWithTurns("thread-1", [
      { id: "turn-1", status: "completed" },
    ]);
    expect(isThreadUnread(thread, "turn-1")).toBe(false);
  });

  it("isThreadUnread returns false for a running thread", () => {
    const thread = makeThreadWithTurns("thread-1", [
      { id: "turn-1", status: "in_progress" },
    ]);
    expect(isThreadUnread(thread, undefined)).toBe(false);
  });

  it("isThreadUnread returns false for an empty thread", () => {
    const thread = makeThreadWithTurns("thread-1", []);
    expect(isThreadUnread(thread, undefined)).toBe(false);
  });

  it("markThreadTurnsViewed records the latest completed turn ID", () => {
    const thread = makeThreadWithTurns("thread-1", [
      { id: "turn-1", status: "completed" },
    ]);
    const state = {
      ...initialState,
      thread,
      threads: [thread],
    };
    const next = markThreadTurnsViewed(state, "thread-1");
    expect(next.lastViewedTurnByThreadID["thread-1"]).toBe("turn-1");
  });

  it("markThreadTurnsViewed is a no-op when already current", () => {
    const thread = makeThreadWithTurns("thread-1", [
      { id: "turn-1", status: "completed" },
    ]);
    const state = {
      ...initialState,
      thread,
      threads: [thread],
      lastViewedTurnByThreadID: { "thread-1": "turn-1" },
    };
    expect(markThreadTurnsViewed(state, "thread-1")).toBe(state);
  });

  it("markThreadTurnsViewed is a no-op for a running thread", () => {
    const thread = makeThreadWithTurns("thread-1", [
      { id: "turn-1", status: "in_progress" },
    ]);
    const state = {
      ...initialState,
      thread,
      threads: [thread],
    };
    expect(markThreadTurnsViewed(state, "thread-1")).toBe(state);
  });
});

describe("AppState sortThreads (sidebar order)", () => {
  function makeSortableThread(args: {
    id: string;
    createdAt: string;
    updatedAt: string;
    status?: "idle" | "in_progress";
    turns?: Array<{ id: string; status: "completed" | "in_progress" | "failed" | "interrupted" }>;
    archived?: boolean;
    readOnly?: boolean;
  }): Thread {
    return {
      id: args.id,
      preview: "",
      model_provider: "fake",
      model: "fake-model",
      cwd: "/tmp",
      status: args.status ?? "idle",
      created_at: args.createdAt,
      updated_at: args.updatedAt,
      archived: args.archived,
      read_only: args.readOnly,
      turns: (args.turns ?? []).map((turn) => ({
        id: turn.id,
        items: [],
        items_view: "full" as const,
        status: turn.status,
      })),
    };
  }

  it("keeps running threads in created_at order regardless of updated_at jitter", () => {
    // Two in_progress threads. updated_at keeps bumping while the model
    // streams; created_at never changes. The old single-key sort shuffled
    // the rows every time either side streamed a token. The fix pins
    // running threads to a created_at order so clicking one is stable.
    const older = makeSortableThread({
      id: "thread-older",
      createdAt: "2026-06-18T00:00:00Z",
      updatedAt: "2026-06-20T12:00:00Z",
      status: "in_progress",
      turns: [{ id: "turn-1", status: "in_progress" }],
    });
    const newer = makeSortableThread({
      id: "thread-newer",
      createdAt: "2026-06-19T00:00:00Z",
      updatedAt: "2026-06-18T00:00:00Z", // stale; should be ignored while running
      status: "in_progress",
      turns: [{ id: "turn-1", status: "in_progress" }],
    });

    const sorted = sortThreads([older, newer]);
    expect(sorted.map((thread) => thread.id)).toEqual([
      "thread-newer",
      "thread-older",
    ]);

    // Even after flipping updated_at wildly, running order is unchanged.
    const flipped = sortThreads([
      { ...newer, updated_at: "2099-01-01T00:00:00Z" },
      { ...older, updated_at: "1970-01-01T00:00:00Z" },
    ]);
    expect(flipped.map((thread) => thread.id)).toEqual([
      "thread-newer",
      "thread-older",
    ]);
  });

  it("places running threads before settled threads", () => {
    const running = makeSortableThread({
      id: "thread-running",
      createdAt: "2026-06-18T00:00:00Z",
      updatedAt: "2026-06-18T00:00:00Z",
      status: "in_progress",
      turns: [{ id: "turn-1", status: "in_progress" }],
    });
    // Settled thread updated more recently than the running one. It still
    // sits below the running section — recency bubbles within the settled
    // group, not above active conversations.
    const settledRecent = makeSortableThread({
      id: "thread-settled-recent",
      createdAt: "2026-06-17T00:00:00Z",
      updatedAt: "2099-01-01T00:00:00Z",
    });
    const settledOlder = makeSortableThread({
      id: "thread-settled-older",
      createdAt: "2026-06-16T00:00:00Z",
      updatedAt: "2026-06-19T00:00:00Z",
    });

    const sorted = sortThreads([settledOlder, running, settledRecent]);
    expect(sorted.map((thread) => thread.id)).toEqual([
      "thread-running",
      "thread-settled-recent",
      "thread-settled-older",
    ]);
  });

  it("sorts settled threads by updated_at desc", () => {
    const settledA = makeSortableThread({
      id: "thread-a",
      createdAt: "2026-06-15T00:00:00Z",
      updatedAt: "2026-06-15T00:00:00Z",
    });
    const settledB = makeSortableThread({
      id: "thread-b",
      createdAt: "2026-06-15T00:00:00Z",
      updatedAt: "2026-06-20T00:00:00Z",
    });
    const settledC = makeSortableThread({
      id: "thread-c",
      createdAt: "2026-06-15T00:00:00Z",
      updatedAt: "2026-06-17T00:00:00Z",
    });
    const sorted = sortThreads([settledA, settledB, settledC]);
    expect(sorted.map((thread) => thread.id)).toEqual([
      "thread-b",
      "thread-c",
      "thread-a",
    ]);
  });

  it("detects running via any in-progress turn even when thread status is idle", () => {
    // A thread that has just received its first turn but whose own status
    // hasn't been bumped yet must still be treated as running — the
    // streaming output lives in the latest turn.
    const streaming = makeSortableThread({
      id: "thread-streaming",
      createdAt: "2026-06-18T00:00:00Z",
      updatedAt: "2026-06-18T00:00:00Z",
      status: "idle",
      turns: [{ id: "turn-1", status: "in_progress" }],
    });
    const settled = makeSortableThread({
      id: "thread-idle",
      createdAt: "2026-06-17T00:00:00Z",
      updatedAt: "2026-06-20T00:00:00Z",
    });

    const sorted = sortThreads([settled, streaming]);
    expect(sorted.map((thread) => thread.id)).toEqual([
      "thread-streaming",
      "thread-idle",
    ]);
  });

  it("drops archived and read-only threads from the sortable list", () => {
    const archived = makeSortableThread({
      id: "thread-archived",
      createdAt: "2026-06-18T00:00:00Z",
      updatedAt: "2099-01-01T00:00:00Z",
      archived: true,
    });
    const readOnly = makeSortableThread({
      id: "thread-readonly",
      createdAt: "2026-06-18T00:00:00Z",
      updatedAt: "2099-01-01T00:00:00Z",
      readOnly: true,
    });
    const normal = makeSortableThread({
      id: "thread-normal",
      createdAt: "2026-06-18T00:00:00Z",
      updatedAt: "2026-06-19T00:00:00Z",
    });

    const sorted = sortThreads([archived, readOnly, normal]);
    expect(sorted.map((thread) => thread.id)).toEqual(["thread-normal"]);
  });

  it("keeps the active read-only child thread renderable outside the sortable list", () => {
    const child = makeSortableThread({
      id: "child-running",
      createdAt: "2026-06-18T00:00:00Z",
      updatedAt: "2026-06-18T00:00:00Z",
      readOnly: true,
      turns: [{ id: "child-turn-1", status: "in_progress" }],
    });
    const normal = makeSortableThread({
      id: "thread-normal",
      createdAt: "2026-06-18T00:00:00Z",
      updatedAt: "2026-06-19T00:00:00Z",
    });

    const sidebarThreads = sortThreads([normal, child]);
    const renderableThreads = conversationPaneThreadsByID(sidebarThreads, child);

    expect(sidebarThreads.map((thread) => thread.id)).toEqual(["thread-normal"]);
    expect(renderableThreads.get("child-running")?.turns).toHaveLength(1);
  });
});

describe("latestContextUsageForThread", () => {
  function makeThread(args: {
    id?: string;
    model?: string;
    turns?: Array<{
      id: string;
      status?: "in_progress" | "completed" | "failed" | "interrupted";
    }>;
  } = {}): Thread {
    return {
      id: args.id ?? "thread-1",
      preview: "",
      model_provider: "fake",
      model: args.model ?? "fake-model",
      cwd: "/tmp",
      status: "idle",
      created_at: "2026-06-18T00:00:00Z",
      updated_at: "2026-06-18T00:00:00Z",
      turns: (args.turns ?? []).map((t) => ({
        id: t.id,
        items: [],
        items_view: "full" as const,
        status: t.status ?? "completed",
      })),
    };
  }

  it("returns undefined when the thread is undefined", () => {
    expect(latestContextUsageForThread(initialState, undefined)).toBeUndefined();
  });

  it("falls back to the active runtime model when no thread exists yet", () => {
    const result = latestContextUsageForThread(initialState, undefined, {
      model: "gpt-5",
      contextWindowTokens: 400_000,
    });
    expect(result).toEqual({
      turnID: "",
      used: 0,
      window: 400_000,
      inputTokens: 0,
      cacheCreationTokens: 0,
      cacheReadTokens: 0,
    });
  });

  it("returns undefined when no thread exists and runtime ceiling is absent", () => {
    const result = latestContextUsageForThread(initialState, undefined, {
      model: "claude-sonnet-4-5",
    });
    expect(result).toBeUndefined();
  });

  it("returns undefined for an empty thread with an unrecognized model", () => {
    // "fake-model" has no catalog entry — the ring should hide rather
    // than guess a limit.
    const t = makeThread({ turns: [] });
    expect(latestContextUsageForThread(initialState, t)).toBeUndefined();
  });

  it("hides the meter when no runtime ceiling is available and no turn has run", () => {
    const t = makeThread({ model: "claude-sonnet-4-5", turns: [] });
    const result = latestContextUsageForThread(initialState, t);
    expect(result).toBeUndefined();
  });

  it("does not infer a gateway model ceiling from the client", () => {
    const t = makeThread({ model: "anthropic/claude-sonnet-4-5", turns: [] });
    const result = latestContextUsageForThread(initialState, t);
    expect(result).toBeUndefined();
  });

  it("does not treat raw provider usage as retained context", () => {
    let state = initialState;
    state = appendTurnTokenSample(
      state,
      "turn-1",
      "thread-1",
      1_300_000,
      0,
      0,
      0,
      1_000,
      1_000_000,
    );
    const t = makeThread({
      model: "minimax-m3",
      turns: [{ id: "turn-1", status: "completed" }],
    });
    const result = latestContextUsageForThread(state, t, {
      model: "minimax-m3",
      contextWindowTokens: 1_000_000,
    });
    expect(result).toEqual({
      turnID: "",
      used: 0,
      window: 1_000_000,
      inputTokens: 0,
      cacheCreationTokens: 0,
      cacheReadTokens: 0,
    });
  });

  it("returns real usage from the most recent turn that has one", () => {
    let state = initialState;
    state = appendTurnTokenSample(
      state,
      "turn-1",
      "thread-1",
      10,
      0,
      0,
      0,
      1_000,
      200_000,
      undefined,
      10,
    );
    state = appendTurnTokenSample(
      state,
      "turn-2",
      "thread-1",
      20,
      0,
      0,
      0,
      2_000,
      200_000,
      undefined,
      20,
    );
    const t = makeThread({
      turns: [
        { id: "turn-1", status: "completed" },
        { id: "turn-2", status: "completed" },
      ],
    });
    const result = latestContextUsageForThread(state, t);
    expect(result?.turnID).toBe("turn-2");
    expect(result?.used).toBe(20);
    // Real usage wins over the catalog fallback.
    expect(result?.window).toBe(200_000);
  });

  it("uses persisted turn usage after a restart when live usage state is empty", () => {
    const t = makeThread({
      model: "minimax-m3",
      turns: [
        {
          id: "turn-1",
          status: "completed",
        },
      ],
    });
    t.turns[0].input_tokens = 19_600;
    t.turns[0].cache_read_tokens = 113_000;
    t.turns[0].cache_creation_tokens = 0;
    t.turns[0].context_tokens = 88_000;
    t.turns[0].usage_model = "minimax-m3";
    const result = latestContextUsageForThread(initialState, t, {
      model: "minimax-m3",
      contextWindowTokens: 1_000_000,
    });
    expect(result).toEqual({
      turnID: "turn-1",
      used: 88_000,
      window: 1_000_000,
      inputTokens: 19_600,
      cacheCreationTokens: 0,
      cacheReadTokens: 113_000,
    });
  });

  it("walks back to the previous turn when the most recent has no usage", () => {
    // The ring is a passive readout — it must keep showing the last
    // known context after a turn completes. We test that by giving the
    // thread a most-recent turn with no recorded usage, and verifying
    // the selector falls through to the previous one.
    let state = initialState;
    state = appendTurnTokenSample(
      state,
      "turn-1",
      "thread-1",
      10,
      0,
      0,
      0,
      1_000,
      200_000,
      undefined,
      10,
    );
    const t = makeThread({
      turns: [
        { id: "turn-1", status: "completed" },
        { id: "turn-2", status: "completed" },
      ],
    });
    const result = latestContextUsageForThread(state, t);
    expect(result?.turnID).toBe("turn-1");
    expect(result?.used).toBe(10);
  });

  it("prefers real usage over the catalog when both are reachable", () => {
    // Even if the active model is in the catalog, real usage from a
    // previous turn in the same thread wins — the catalog is a fallback
    // for "model known, no usage yet", not a default that overrides
    // observed values.
    let state = initialState;
    state = appendTurnTokenSample(
      state,
      "turn-1",
      "thread-1",
      10,
      0,
      0,
      0,
      1_000,
      200_000,
      undefined,
      10,
    );
    const t = makeThread({
      model: "claude-sonnet-4-5",
      turns: [{ id: "turn-1", status: "completed" }],
    });
    const result = latestContextUsageForThread(state, t);
    expect(result?.turnID).toBe("turn-1");
    expect(result?.used).toBe(10);
  });

  it("ignores stale usage from a previous model after the thread model changes", () => {
    let state = initialState;
    state = appendTurnTokenSample(
      state,
      "turn-1",
      "thread-1",
      80_000,
      0,
      0,
      0,
      1_000,
      200_000,
      "claude-sonnet-4-5",
      80_000,
    );
    const t = makeThread({
      model: "gpt-5",
      turns: [{ id: "turn-1", status: "completed" }],
    });
    const result = latestContextUsageForThread(state, t);
    expect(result).toBeUndefined();
  });
});

describe("mentionedParticipantIDsFromText", () => {
  const roster = [
    { id: "prt-noel", name: "Noel" },
    { id: "prt-noe", name: "Noe" },
    { id: "prt-qing", name: "小青" },
    { id: "prt-blank", name: "  " },
  ];

  it("matches whole-word @Name mentions", () => {
    expect(mentionedParticipantIDsFromText("@Noel 看下这个 PR", roster)).toEqual([
      "prt-noel",
    ]);
  });

  it("prefers the longest name when one is a prefix of another", () => {
    expect(mentionedParticipantIDsFromText("@Noel hi", roster)).toEqual([
      "prt-noel",
    ]);
    expect(mentionedParticipantIDsFromText("@Noe hi", roster)).toEqual([
      "prt-noe",
    ]);
  });

  it("matches CJK names followed by punctuation", () => {
    expect(mentionedParticipantIDsFromText("@小青，帮忙评审", roster)).toEqual([
      "prt-qing",
    ]);
  });

  it("collects multiple distinct mentions without duplicates", () => {
    expect(
      mentionedParticipantIDsFromText("@Noel @小青 @Noel 一起看看", roster),
    ).toEqual(["prt-noel", "prt-qing"]);
  });

  it("returns empty for text without mentions or blank text", () => {
    expect(mentionedParticipantIDsFromText("没有提及任何人", roster)).toEqual([]);
    expect(mentionedParticipantIDsFromText("   ", roster)).toEqual([]);
    expect(mentionedParticipantIDsFromText("mail@Noel.com", roster)).toEqual([]);
  });

  it("ignores blank-named participants", () => {
    expect(mentionedParticipantIDsFromText("@   hello", roster)).toEqual([]);
  });
});
