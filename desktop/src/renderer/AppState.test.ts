import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type {
  Agent,
  DesktopProject,
  RuntimeContext,
  Thread,
  ThreadItem,
  Turn,
} from "../shared/protocol";
import {
  activePlanUpdateForThread,
  activeThreadForState,
  activeTurnTokenSpeed,
  activeTurnTokenSpeedSnapshot,
  applyLoadedRuntimeWithDraftCarry,
  appendStreamingTokenSample,
  appendTurnTokenSample,
  chatFocusValueForThread,
  chatMessagesFromTurns,
  chatReaderCountForThread,
  composerDraftHasContent,
  conversationPaneThreadsByID,
  conversationSearchContextLabel,
  groupThreadSummaries,
  createDraftSessionTab,
  createThreadSessionTab,
  focusWorkspaceSendValue,
  sessionTabLabel,
  handleStreamingNotification,
  initialState,
  isGroupThread,
  isScratchThread,
  isThreadRunning,
  isThreadUnread,
  latestCompletedTurnID,
  latestIncomingChatMessageSeq,
  overlayMemberBusy,
  latestContextUsageForThread,
  mergeListedThreads,
  markThreadTurnsViewed,
  mentionedParticipantIDsFromText,
  openForkThreadAsPrimary,
  pinnedThreads,
  pinnedThreadSummaries,
  projectThreads,
  queryTextsForThread,
  reconcileResumedThreadTurns,
  reduceServerEvent,
  replyCountBadge,
  resolveThreadRuntimeContext,
  scratchThreadSummaries,
  sortThreads,
  summarizeThreadsForSidebar,
  threadBelongsToProject,
  threadNeedsResumeOnReselect,
  threadProjectPath,
  threadSessionTabID,
  turnStreamStatusForThread,
  workspacePanelContext,
  type AppState,
  type ComposerDraftState,
  type SessionTab,
  type ThreadSummary,
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

describe("activeThreadForState", () => {
  it("follows the visible session tab when the loaded primary thread briefly lags behind", () => {
    const context: RuntimeContext = { kind: "project", project_id: "project-1", cwd: "/repo" };
    const stale = { ...threadWithUserTexts(["settings task"]), id: "thread-settings" };
    const visible = { ...threadWithUserTexts(["NetEase task"]), id: "thread-netease" };
    const state: AppState = {
      ...initialState,
      activeContext: context,
      thread: stale,
      threads: [stale, visible],
      sessionTabs: [createThreadSessionTab(stale, context), createThreadSessionTab(visible, context)],
      activeSessionTabID: threadSessionTabID(visible.id),
    };

    expect(activeThreadForState(state)?.id).toBe(visible.id);
  });

  it("does not fall back to a stale thread while a draft or unresolved thread tab is visible", () => {
    const context: RuntimeContext = { kind: "project", project_id: "project-1", cwd: "/repo" };
    const stale = { ...threadWithUserTexts(["old task"]), id: "thread-old" };
    const draft = createDraftSessionTab("draft:new", context);
    const unresolved = createThreadSessionTab(
      { ...threadWithUserTexts(["new task"]), id: "thread-loading" },
      context,
    );

    expect(activeThreadForState({
      ...initialState,
      activeContext: context,
      thread: stale,
      threads: [stale],
      sessionTabs: [draft],
      activeSessionTabID: draft.id,
    })).toBeUndefined();
    expect(activeThreadForState({
      ...initialState,
      activeContext: context,
      thread: stale,
      threads: [stale],
      sessionTabs: [unresolved],
      activeSessionTabID: unresolved.id,
    })).toBeUndefined();
  });
});

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

describe("AppState protocol normalization", () => {
  it("keeps rendering when an older core starts an empty turn with null items", () => {
    const thread = threadWithUserTexts(["continue"]);
    const next = reduceServerEvent(
      {
        ...initialState,
        activeContext: { kind: "no_project", cwd: "/repo" },
        thread,
        threads: [thread],
      },
      {
        kind: "notification",
        workdir: "/repo",
        message: {
          method: "turn/started",
          params: {
            thread_id: thread.id,
            turn: {
              id: "turn-auto-continue",
              items: null,
              items_view: "full",
              status: "in_progress",
            },
          },
        },
      },
    );

    expect(next.thread?.turns.at(-1)).toMatchObject({
      id: "turn-auto-continue",
      items: [],
    });
    expect(next.running).toBe(true);
  });

  it("normalizes null turn items inside a thread snapshot", () => {
    const current = threadWithUserTexts(["continue"]);
    const next = reduceServerEvent(
      {
        ...initialState,
        activeContext: { kind: "no_project", cwd: "/repo" },
        thread: current,
        threads: [current],
      },
      {
        kind: "notification",
        workdir: "/repo",
        message: {
          method: "thread/updated",
          params: {
            thread: {
              ...current,
              turns: [
                {
                  id: "turn-auto-continue",
                  items: null,
                  items_view: "full",
                  status: "in_progress",
                },
              ],
            },
          },
        },
      },
    );

    expect(next.thread?.turns).toEqual([
      expect.objectContaining({ id: "turn-auto-continue", items: [] }),
    ]);
  });
});

function sessionTabPrompt(
  tabs: SessionTab[],
  tabID: string,
): string | undefined {
  const tab = tabs.find((candidate) => candidate.id === tabID);
  if (
    !tab ||
    tab.kind === "skills" ||
    tab.kind === "board"
  ) {
    return undefined;
  }
  return tab.prompt;
}

describe("AppState server requests", () => {
  it("rejects unsupported server requests", () => {
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
        method: "unsupported/request",
        params: {
          id: "request-1",
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

    expect(rejectServerRequest).toHaveBeenCalledWith(
      "server-request-1",
      "unsupported server request: unsupported/request",
    );
    expect(next).toBe(initialState);
    expect(next.status).toBe(initialState.status);
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

describe("overlayMemberBusy", () => {
  function groupSummary(
    members: Array<{ id: string; busy?: boolean }>,
  ): ReturnType<typeof summarizeThreadsForSidebar>[number] {
    const [summary] = summarizeThreadsForSidebar([
      {
        ...threadWithUserTexts(["group hello"]),
        group: true,
        members: members.map((member) => ({
          id: member.id,
          name: member.id,
          busy: member.busy,
        })),
      } as unknown as Thread,
    ]);
    return summary;
  }

  it("lights a member the live busy set says is running", () => {
    const summary = groupSummary([{ id: "andy", busy: false }]);
    const next = overlayMemberBusy(summary, new Set(["andy"]));
    expect(next.members?.[0]?.busy).toBe(true);
    expect(isThreadRunning(next)).toBe(true);
  });

  it("clears a stale busy=true once the member's turn settled", () => {
    const summary = groupSummary([{ id: "andy", busy: true }]);
    const next = overlayMemberBusy(summary, new Set());
    expect(next.members?.[0]?.busy).toBe(false);
    expect(isThreadRunning(next)).toBe(false);
  });

  it("returns the same reference when nothing changes", () => {
    const summary = groupSummary([
      { id: "andy", busy: true },
      { id: "bob", busy: false },
    ]);
    expect(overlayMemberBusy(summary, new Set(["andy"]))).toBe(summary);
    const memberless = groupSummary([]);
    expect(overlayMemberBusy(memberless, new Set(["andy"]))).toBe(memberless);
  });
});

describe("resolveThreadRuntimeContext", () => {
  const project: DesktopProject = {
    id: "project-1",
    name: "project",
    path: "/repo/project",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };

  it("resolves a project thread to that project's context", () => {
    const thread = threadWithUserTexts(["hello"]);
    thread.cwd = project.path;
    thread.workspace_kind = "project";

    expect(resolveThreadRuntimeContext(thread, [project])).toEqual({
      kind: "project",
      project_id: project.id,
      cwd: project.path,
    });
  });

  it("resolves a scratch thread to a no_project context rooted at its own cwd", () => {
    const thread = threadWithUserTexts(["hello"]);
    thread.cwd = "/Users/me/.wuu/scratch/2026-07-03";
    thread.workspace_kind = "scratch";

    expect(resolveThreadRuntimeContext(thread, [project])).toEqual({
      kind: "no_project",
      cwd: thread.cwd,
    });
  });

  it("resolves a DM thread to a no_project context rooted at its own cwd", () => {
    const thread: Thread = {
      ...threadWithUserTexts(["hi Andy"]),
      cwd: "/Users/me/.wuu/agents/andy/home",
      workspace_kind: "dm",
      dm_participant_id: "andy",
    };

    expect(resolveThreadRuntimeContext(thread, [project])).toEqual({
      kind: "no_project",
      cwd: thread.cwd,
    });
  });

  it("has no registered project to match against and falls back to no_project", () => {
    const thread = threadWithUserTexts(["hello"]);
    thread.cwd = "/Users/me/some/random/cwd";
    thread.workspace_kind = "scratch";

    expect(resolveThreadRuntimeContext(thread, [])).toEqual({
      kind: "no_project",
      cwd: thread.cwd,
    });
  });

  it("prefers a cwd match against a registered project over a stale workspace_kind label, consistent with isScratchThread", () => {
    // Same fixture shape as the "groups worktree fork sessions" case above:
    // cwd belongs to the project, but workspace_kind still says "scratch"
    // (e.g. a legacy thread from before the project was registered at that
    // path). isScratchThread treats the cwd match as authoritative and
    // returns false (not scratch); resolveThreadRuntimeContext must agree
    // and resolve to the project's context rather than no_project.
    const thread = threadWithUserTexts(["continue in a worktree"]);
    thread.cwd = "/Users/me/.wuu/worktrees/fork-1/project";
    thread.workspace_kind = "scratch";
    thread.worktree = {
      path: "/Users/me/.wuu/worktrees/fork-1/project",
      base_repo: project.path,
      base_head: "d955824f",
    };

    expect(isScratchThread(thread, [project])).toBe(false);
    expect(resolveThreadRuntimeContext(thread, [project])).toEqual({
      kind: "project",
      project_id: project.id,
      cwd: project.path,
    });
  });
});

describe("workspacePanelContext", () => {
  const projectContext: RuntimeContext = {
    kind: "project",
    project_id: "project-1",
    cwd: "/repo/project",
  };

  it("returns activeContext unchanged when there is no active thread", () => {
    expect(workspacePanelContext(projectContext, undefined)).toBe(
      projectContext,
    );
  });

  it("returns the very same activeContext reference when the thread's cwd matches it", () => {
    const thread = threadWithUserTexts(["hello"]);
    thread.cwd = projectContext.cwd;

    expect(workspacePanelContext(projectContext, thread)).toBe(projectContext);
  });

  it("returns activeContext unchanged when the thread has no cwd", () => {
    const thread = threadWithUserTexts(["hello"]);
    thread.cwd = "";

    expect(workspacePanelContext(projectContext, thread)).toBe(projectContext);
  });

  it("overrides cwd to the thread's own cwd when it differs, preserving kind/project_id (worktree fork)", () => {
    // Mirrors a thread/fork "worktree" thread: resolveThreadRuntimeContext
    // resolves it to the base project's context (threadProjectPath prefers
    // worktree.base_repo), but the thread itself runs out of the worktree
    // directory. The workspace panel should follow the thread.
    const thread = threadWithUserTexts(["continue in a worktree"]);
    thread.cwd = "/Users/me/.wuu/worktrees/fork-1/project";
    thread.worktree = {
      path: thread.cwd,
      base_repo: projectContext.cwd,
      base_head: "d955824f",
    };

    expect(workspacePanelContext(projectContext, thread)).toEqual({
      kind: "project",
      project_id: projectContext.project_id,
      cwd: thread.cwd,
    });
  });

  it("falls back to a no_project context rooted at the thread's cwd when there is no activeContext", () => {
    const thread = threadWithUserTexts(["hello"]);
    thread.cwd = "/Users/me/.wuu/scratch/2026-07-03";

    expect(workspacePanelContext(undefined, thread)).toEqual({
      kind: "no_project",
      cwd: thread.cwd,
    });
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
              max_attempts: 4,
              retry_count: 1,
              max_retries: 3,
              retry_in_ms: 1500,
            },
          },
        },
      },
    });

    expect(turnStreamStatusForThread(reconnecting, reconnecting.thread)).toEqual({
      text: "消息流暂时中断，约 2 秒后继续（第 2/4 次尝试）",
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
      text: "HTTP 消息流正在恢复（第 3/4 次尝试）",
      liveProgress: true,
    });
  });

  it("explains when automatic replay stops to avoid duplicate tools", () => {
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
                phase: "failed",
                reason:
                  'automatic replay blocked: streamed tool "read_file" may already be running; reconnecting could run it twice',
              },
            },
          },
        },
      },
    );

    expect(turnStreamStatusForThread(state, state.thread)).toEqual({
      text: "为避免工具被重复执行，消息流已停止自动恢复",
      liveProgress: false,
    });
  });

  it.each([
    ["workflow_budget_exceeded", undefined, "本次任务的自动恢复额度已用完"],
    [
      "workflow_cost_indeterminate",
      undefined,
      "存在状态未知且可能已计费的请求，已停止自动恢复",
    ],
    [
      "replay_unsafe",
      "invocation_unknown",
      "工具是否执行成功无法确认，已停止自动恢复以避免重复操作",
    ],
  ])("explains typed terminal recovery state %s", (category, replayReason, text) => {
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
      {...initialState, thread, threads: [thread], running: true},
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
                phase: "failed",
                failure_category: category,
                replay_reason: replayReason,
              },
            },
          },
        },
      },
    );
    expect(turnStreamStatusForThread(state, state.thread)).toEqual({
      text,
      liveProgress: false,
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
    streamTextStore.clearItem("turn-1", "reasoning-1");
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

  it("keeps accumulated reasoning when item/started carries a stale snapshot", () => {
    const key = streamTextKey("turn-1", "reasoning-1", "text");
    const state = {
      ...initialState,
      thread: threadWithUserTexts(["hi"]),
    };
    streamTextStore.set(key, "Let me think about that.");

    for (const text of ["", "Let me think"]) {
      const handling = handleStreamingNotification(
        {
          kind: "notification",
          workdir: "/repo",
          message: {
            method: "item/started",
            params: {
              thread_id: "thread-1",
              turn_id: "turn-1",
              item: {
                id: "reasoning-1",
                type: "reasoning",
                status: "in_progress",
                text,
              },
            },
          },
        },
        state,
      );

      expect(handling).toBe("state");
      expect(streamTextStore.get(key)).toBe("Let me think about that.");
    }
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

  function makeGroupThreadWithMessages(
    threadID: string,
    items: Array<{
      type: "user_message" | "participant_message";
      seq?: number;
    }>,
  ): Thread {
    const base = makeThreadWithTurns(threadID, [
      { id: "turn-1", status: "completed" },
    ]);
    return {
      ...base,
      group: true,
      turns: [
        {
          id: "turn-1",
          items_view: "full" as const,
          status: "completed" as const,
          items: items.map((item, index) => ({
            id: `item-${index + 1}`,
            type: item.type,
            text: `message ${index + 1}`,
            seq: item.seq,
            ...(item.type === "participant_message"
              ? { participant: { id: "andy", name: "Andy", kind: "agent" } }
              : {}),
          })),
        },
      ],
    };
  }

  it("chat-style unread requires an actual incoming message, not a settled turn", () => {
    // A group turn that completed WITHOUT any participant post (e.g. the
    // model settled without using send_message) must not flag unread.
    const silent = makeGroupThreadWithMessages("group-1", [
      { type: "user_message", seq: 1 },
    ]);
    expect(isThreadUnread(silent, undefined)).toBe(false);

    const spoke = makeGroupThreadWithMessages("group-2", [
      { type: "user_message", seq: 1 },
      { type: "participant_message", seq: 2 },
    ]);
    expect(isThreadUnread(spoke, undefined)).toBe(true);
    expect(isThreadUnread(spoke, undefined, 2)).toBe(false);
    expect(isThreadUnread(spoke, undefined, 1)).toBe(true);
  });

  it("chat-style unread flows through ThreadSummary.last_incoming_message_seq", () => {
    const spoke = makeGroupThreadWithMessages("group-1", [
      { type: "participant_message", seq: 5 },
    ]);
    const [summary] = summarizeThreadsForSidebar([spoke]);
    expect(summary.last_incoming_message_seq).toBe(5);
    expect(isThreadUnread(summary, undefined)).toBe(true);
    expect(isThreadUnread(summary, undefined, 5)).toBe(false);
  });

  it("the user's own post never flags a chat thread unread", () => {
    const thread = makeGroupThreadWithMessages("group-1", [
      { type: "participant_message", seq: 3 },
      { type: "user_message", seq: 4 },
    ]);
    // Viewed up to the agent's message (3); the trailing message is the
    // user's own — nothing incoming is pending.
    expect(isThreadUnread(thread, undefined, 3)).toBe(false);
  });

  it("markThreadTurnsViewed advances the chat message offset to the latest post", () => {
    const thread = makeGroupThreadWithMessages("group-1", [
      { type: "participant_message", seq: 3 },
      { type: "user_message", seq: 4 },
    ]);
    const state = {
      ...initialState,
      thread,
      threads: [thread],
    };
    const next = markThreadTurnsViewed(state, "group-1");
    expect(next.lastViewedMessageSeqByThreadID["group-1"]).toBe(4);
    expect(isThreadUnread(thread, undefined, 4)).toBe(false);
  });

  it("latestIncomingChatMessageSeq ignores unpersisted items without seq", () => {
    const thread = makeGroupThreadWithMessages("group-1", [
      { type: "participant_message", seq: 2 },
      { type: "participant_message" },
    ]);
    expect(latestIncomingChatMessageSeq(thread)).toBe(2);
  });
});

describe("chatReaderCountForThread", () => {
  const member = (id: string) => ({ id, name: id, kind: "agent" });

  it("uses the group's own member roster, not the global roster", () => {
    const group = {
      group: true,
      members: [member("andy"), member("bob")],
    };
    expect(chatReaderCountForThread(group, 3)).toBe(2);
  });

  it("counts a DM as exactly one reader", () => {
    expect(chatReaderCountForThread({ dm_participant_id: "andy" }, 3)).toBe(1);
  });

  it("falls back to the roster while a group's members snapshot is missing", () => {
    expect(chatReaderCountForThread({ group: true }, 3)).toBe(3);
    expect(chatReaderCountForThread(undefined, 3)).toBe(3);
  });
});

describe("AppState sortThreads (sidebar order)", () => {
  function makeSortableThread(args: {
    id: string;
    createdAt: string;
    updatedAt: string;
    status?: "idle" | "in_progress";
    turns?: Array<{ id: string; status: "completed" | "in_progress" | "failed" | "interrupted" }>;
    childAgents?: Agent[];
    members?: Thread["members"];
    group?: boolean;
    pinned?: boolean;
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
      child_agents: args.childAgents,
      members: args.members,
      group: args.group,
      pinned: args.pinned,
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

  it("treats a direct running child agent as active sidebar work", () => {
    const groupWithRunningAgent = makeSortableThread({
      id: "group-with-agent",
      createdAt: "2026-06-18T00:00:00Z",
      updatedAt: "2026-06-18T00:00:00Z",
      group: true,
      childAgents: [
        {
          id: "agent-running",
          parent_id: "group-with-agent",
          status: "running",
          task_name: "review",
        },
      ],
    });
    const settledRecent = makeSortableThread({
      id: "thread-settled-recent",
      createdAt: "2026-06-17T00:00:00Z",
      updatedAt: "2099-01-01T00:00:00Z",
    });

    const sorted = sortThreads([settledRecent, groupWithRunningAgent]);
    expect(sorted.map((thread) => thread.id)).toEqual([
      "group-with-agent",
      "thread-settled-recent",
    ]);
    expect(isThreadUnread(groupWithRunningAgent, undefined)).toBe(false);
  });

  it("treats a busy group member as active sidebar work", () => {
    const groupWithBusyMember = makeSortableThread({
      id: "group-with-busy-member",
      createdAt: "2026-06-18T00:00:00Z",
      updatedAt: "2026-06-18T00:00:00Z",
      group: true,
      members: [
        {
          id: "participant-running",
          name: "Runner",
          kind: "named",
          busy: true,
        },
      ],
    });
    const settledRecent = makeSortableThread({
      id: "thread-settled-recent",
      createdAt: "2026-06-17T00:00:00Z",
      updatedAt: "2099-01-01T00:00:00Z",
    });

    expect(sortThreads([settledRecent, groupWithBusyMember]).map((thread) => thread.id)).toEqual([
      "group-with-busy-member",
      "thread-settled-recent",
    ]);
  });

  it("keeps archived and read-only threads in the sortable list so Settings → Archive can show them", () => {
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
    expect(sorted.map((thread) => thread.id).sort()).toEqual([
      "thread-archived",
      "thread-normal",
      "thread-readonly",
    ]);
  });

  it("keeps the active read-only child thread renderable via conversationPaneThreadsByID", () => {
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

    expect(sidebarThreads.map((thread) => thread.id).sort()).toEqual([
      "child-running",
      "thread-normal",
    ]);
    expect(renderableThreads.get("child-running")?.turns).toHaveLength(1);
  });

  it("pinnedThreads and projectThreads hide archived entries but keep read-only ones", () => {
    const pinnedArchived = makeSortableThread({
      id: "pinned-archived",
      createdAt: "2026-06-18T00:00:00Z",
      updatedAt: "2026-06-20T00:00:00Z",
      pinned: true,
      archived: true,
    });
    const pinnedLive = makeSortableThread({
      id: "pinned-live",
      createdAt: "2026-06-18T00:00:00Z",
      updatedAt: "2026-06-21T00:00:00Z",
      pinned: true,
    });
    const projectArchived = makeSortableThread({
      id: "project-archived",
      createdAt: "2026-06-18T00:00:00Z",
      updatedAt: "2026-06-22T00:00:00Z",
      archived: true,
    });
    const projectReadOnly = makeSortableThread({
      id: "project-readonly",
      createdAt: "2026-06-18T00:00:00Z",
      updatedAt: "2026-06-23T00:00:00Z",
      readOnly: true,
    });

    const threads = [
      pinnedArchived,
      pinnedLive,
      projectArchived,
      projectReadOnly,
    ];

    expect(pinnedThreads(threads).map((thread) => thread.id)).toEqual([
      "pinned-live",
    ]);
    expect(projectThreads(threads).map((thread) => thread.id)).toEqual([
      "project-readonly",
    ]);
  });
});

describe("mergeListedThreads", () => {
  it("does not clear known child-agent state when a listed snapshot omits it", () => {
    const current: Thread = {
      ...threadWithUserTexts(["group work"]),
      id: "group-1",
      group: true,
      child_agents: [
        {
          id: "agent-running",
          parent_id: "group-1",
          status: "running",
          task_name: "review",
        },
      ],
    };
    const listed: Thread = {
      ...current,
      turns: [],
      child_agents: undefined,
      updated_at: "2026-01-02T00:00:00Z",
    };

    const [merged] = mergeListedThreads([current], [listed]);

    expect(merged.child_agents?.[0]?.id).toBe("agent-running");
    expect(merged.child_agents?.[0]?.status).toBe("running");
    expect(isThreadUnread(merged, undefined)).toBe(false);
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

  it("prefers the retained estimate over inclusive raw input (A1 normalization)", () => {
    // A live turn/usage sample from an inclusive-input endpoint (MiniMax
    // reports input_tokens including cache_read) carries a large raw input
    // alongside the server-side retained-context estimate. The meter must
    // read the retained estimate (context_tokens = res.ContextTokens), never
    // the raw input, and cache_read stays intact for the hit-rate display.
    let state = initialState;
    state = appendTurnTokenSample(
      state,
      "turn-1",
      "thread-1",
      132_600, // raw input_tokens (inclusive of cache_read)
      2_000,
      0,
      113_000, // cache_read preserved
      1_000,
      1_000_000,
      "minimax-m3",
      88_000, // retained context estimate (res.ContextTokens)
    );
    const t = makeThread({
      model: "minimax-m3",
      turns: [{ id: "turn-1", status: "completed" }],
    });
    const result = latestContextUsageForThread(state, t, {
      model: "minimax-m3",
      contextWindowTokens: 1_000_000,
    });
    expect(result?.turnID).toBe("turn-1");
    expect(result?.used).toBe(88_000);
    expect(result?.used).not.toBe(132_600);
    expect(result?.cacheReadTokens).toBe(113_000);
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

describe("activePlanUpdateForThread", () => {
  // The floating "跳到最新" pill cluster's progress chip must disappear
  // once its turn completes — otherwise it lingers on top of the same
  // turn's now-persistent action row (issue #7). `latestPlanUpdateForThread`
  // itself (used by the environment side panel) stays turn-status-agnostic;
  // this wrapper is the one the pill actually reads.
  function threadWithPlan(turnStatus: Turn["status"]): Thread {
    const planItem: ThreadItem = {
      id: "turn-1-item-1",
      type: "tool_call",
      status: "completed",
      name: "update_plan",
      arguments: JSON.stringify({
        plan: [
          { step: "定位问题", status: "completed" },
          { step: "实现修复", status: "in_progress" },
        ],
      }),
    };
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
          status: turnStatus,
          items: [planItem],
        },
      ],
    };
  }

  it("returns the plan while its turn is still running", () => {
    const thread = threadWithPlan("in_progress");
    expect(activePlanUpdateForThread(thread)?.plan).toEqual([
      { step: "定位问题", status: "completed" },
      { step: "实现修复", status: "in_progress" },
    ]);
  });

  it("hides the plan once the turn completes", () => {
    expect(activePlanUpdateForThread(threadWithPlan("completed"))).toBeUndefined();
  });

  it("hides the plan for a failed or interrupted turn", () => {
    expect(activePlanUpdateForThread(threadWithPlan("failed"))).toBeUndefined();
    expect(activePlanUpdateForThread(threadWithPlan("interrupted"))).toBeUndefined();
  });

  it("returns undefined when there is no thread", () => {
    expect(activePlanUpdateForThread(undefined)).toBeUndefined();
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

describe("chatMessagesFromTurns", () => {
  function turn(id: string, items: ThreadItem[]): Pick<Turn, "id" | "items"> {
    return { id, items };
  }

  it("maps a plain user_message to a user row", () => {
    const item: ThreadItem = { id: "item-1", type: "user_message", text: "hi" };
    const rows = chatMessagesFromTurns([turn("turn-1", [item])]);
    expect(rows).toEqual([
      { kind: "user", id: "turn-1:item-1", turnID: "turn-1", item },
    ]);
  });

  it("maps a user_message with non-empty envelope_meta to an envelope row", () => {
    const item: ThreadItem = {
      id: "item-1",
      type: "user_message",
      text: "hi",
      envelope_meta: [{ source_thread_id: "thread-x", addressed: true, hop: 1 }],
    };
    const rows = chatMessagesFromTurns([turn("turn-1", [item])]);
    expect(rows).toEqual([
      {
        kind: "envelope",
        id: "turn-1:item-1",
        turnID: "turn-1",
        items: [item],
      },
    ]);
  });

  it("coalesces consecutive envelope rows into one visible chat row", () => {
    const first: ThreadItem = {
      id: "item-1",
      type: "user_message",
      text: "first forwarded message",
      envelope_meta: [{ source_thread_id: "thread-x", addressed: true, hop: 0 }],
    };
    const second: ThreadItem = {
      id: "item-2",
      type: "user_message",
      text: "second forwarded message",
      envelope_meta: [{ source_thread_id: "thread-x", addressed: false, hop: 1 }],
    };
    const rows = chatMessagesFromTurns([
      turn("turn-1", [first]),
      turn("turn-2", [second]),
    ]);
    expect(rows).toEqual([
      {
        kind: "envelope",
        id: "turn-1:item-1",
        turnID: "turn-1",
        items: [first, second],
      },
    ]);
  });

  it("starts a new envelope group after a user message", () => {
    const first: ThreadItem = {
      id: "item-1",
      type: "user_message",
      text: "forwarded before query",
      envelope_meta: [{ source_thread_id: "thread-x", addressed: false, hop: 0 }],
    };
    const query: ThreadItem = {
      id: "item-2",
      type: "user_message",
      text: "new query",
    };
    const second: ThreadItem = {
      id: "item-3",
      type: "user_message",
      text: "forwarded after query",
      envelope_meta: [{ source_thread_id: "thread-x", addressed: true, hop: 0 }],
    };
    const rows = chatMessagesFromTurns([turn("turn-1", [first, query, second])]);
    expect(rows).toEqual([
      {
        kind: "envelope",
        id: "turn-1:item-1",
        turnID: "turn-1",
        items: [first],
      },
      { kind: "user", id: "turn-1:item-2", turnID: "turn-1", item: query },
      {
        kind: "envelope",
        id: "turn-1:item-3",
        turnID: "turn-1",
        items: [second],
      },
    ]);
  });

  it("keeps a user_message with an empty envelope_meta array as a user row", () => {
    const item: ThreadItem = {
      id: "item-1",
      type: "user_message",
      text: "hi",
      envelope_meta: [],
    };
    const rows = chatMessagesFromTurns([turn("turn-1", [item])]);
    expect(rows).toEqual([
      { kind: "user", id: "turn-1:item-1", turnID: "turn-1", item },
    ]);
  });

  it("maps a subagent_notification handoff to a system event row", () => {
    // The backend delivers a triggering subagent notification to the resident
    // as a user_message whose text is an InterAgentCommunication envelope
    // wrapping a <subagent_notification> payload. The chat stream renders the
    // lifecycle fact as a system divider, never as a raw JSON bubble.
    const notification: ThreadItem = {
      id: "item-1",
      type: "user_message",
      text: JSON.stringify({
        author: "/root/planner",
        recipient: "/root",
        content: `<subagent_notification>\n${JSON.stringify({
          agent_path: "/root/design_pop_out_session_tab",
          status: {
            type: "agent_result",
            agent_id: "planner-aef57366",
            task_name: "design_pop_out_session_tab",
            status: "cancelled",
          },
        })}\n</subagent_notification>`,
        trigger_turn: true,
      }),
    };
    const realMessage: ThreadItem = {
      id: "item-2",
      type: "user_message",
      text: "重跑一下",
    };
    const rows = chatMessagesFromTurns([
      turn("turn-1", [notification, realMessage]),
    ]);
    expect(rows).toEqual([
      {
        kind: "system",
        id: "turn-1:item-1",
        turnID: "turn-1",
        text: "subagent 任务已取消",
        item: notification,
      },
      { kind: "user", id: "turn-1:item-2", turnID: "turn-1", item: realMessage },
    ]);
  });

  it("maps a participant_message to a participant row", () => {
    const item: ThreadItem = {
      id: "item-1",
      type: "participant_message",
      text: "看这里",
      post_kind: "result",
    };
    const rows = chatMessagesFromTurns([turn("turn-1", [item])]);
    expect(rows).toEqual([
      { kind: "participant", id: "turn-1:item-1", turnID: "turn-1", item },
    ]);
  });

  it("keeps a decline participant_message as a participant row", () => {
    const item: ThreadItem = {
      id: "item-1",
      type: "participant_message",
      text: "无需回应",
      post_kind: "decline",
    };
    const rows = chatMessagesFromTurns([turn("turn-1", [item])]);
    expect(rows).toEqual([
      { kind: "participant", id: "turn-1:item-1", turnID: "turn-1", item },
    ]);
  });

  it("drops agent_message, tool_call, reasoning, and other non-whitelisted items", () => {
    const items: ThreadItem[] = [
      { id: "item-1", type: "agent_message", text: "final answer" },
      { id: "item-2", type: "tool_call", name: "bash" },
      { id: "item-3", type: "reasoning", text: "thinking..." },
      { id: "item-5", type: "context_compaction" },
      { id: "item-6", type: "error" },
    ];
    const rows = chatMessagesFromTurns([turn("turn-1", items)]);
    expect(rows).toEqual([]);
  });

  it("drops top-level task_card items from chat rows", () => {
    const item: ThreadItem = {
      id: "item-1",
      type: "task_card",
      task: { id: "task-1", name: "跑测试", status: "running", reply_count: 3 },
    };
    const rows = chatMessagesFromTurns([turn("turn-1", [item])]);
    expect(rows).toEqual([]);
  });

  it("uses stable `${turn.id}:${item.id}` row ids across multiple turns", () => {
    const rows = chatMessagesFromTurns([
      turn("turn-1", [{ id: "item-1", type: "user_message", text: "a" }]),
      turn("turn-2", [{ id: "item-1", type: "user_message", text: "b" }]),
    ]);
    expect(rows.map((row) => row.id)).toEqual(["turn-1:item-1", "turn-2:item-1"]);
  });

  it("maps an item with focus_meta to a focus row regardless of its type", () => {
    const item: ThreadItem = {
      id: "item-1",
      type: "user_message",
      focus_meta: { kind: "workspace", name: "wuu", root: "/home/user/wuu" },
    };
    const rows = chatMessagesFromTurns([turn("turn-1", [item])]);
    expect(rows).toEqual([
      { kind: "focus", id: "turn-1:item-1", turnID: "turn-1", item },
    ]);
  });

  it("prefers focus_meta over envelope_meta when (implausibly) both are present", () => {
    const item: ThreadItem = {
      id: "item-1",
      type: "user_message",
      envelope_meta: [{ source_thread_id: "thread-x", addressed: true, hop: 1 }],
      focus_meta: { kind: "all" },
    };
    const rows = chatMessagesFromTurns([turn("turn-1", [item])]);
    expect(rows).toEqual([
      { kind: "focus", id: "turn-1:item-1", turnID: "turn-1", item },
    ]);
  });
});

describe("replyCountBadge", () => {
  it("renders small counts verbatim", () => {
    expect(replyCountBadge(0)).toBe("0");
    expect(replyCountBadge(1)).toBe("1");
    expect(replyCountBadge(42)).toBe("42");
    expect(replyCountBadge(99)).toBe("99");
  });

  it("caps counts above 99 at '99+'", () => {
    expect(replyCountBadge(100)).toBe("99+");
    expect(replyCountBadge(1000)).toBe("99+");
  });

  it("clamps negative / fractional garbage defensively", () => {
    expect(replyCountBadge(-5)).toBe("0");
    expect(replyCountBadge(3.9)).toBe("3");
  });
});

describe("chatFocusValueForThread", () => {
  const registered = [{ name: "wuu" }];

  it("returns the empty string (全部工作区) when there is no active thread", () => {
    expect(chatFocusValueForThread(undefined, {}, registered)).toBe("");
  });

  it("falls back to the thread's own focus_workspace when unset in this session", () => {
    expect(
      chatFocusValueForThread(
        { id: "thread-1", focus_workspace: "~" },
        {},
        registered,
      ),
    ).toBe("~");
    expect(
      chatFocusValueForThread(
        { id: "thread-1", focus_workspace: "wuu" },
        {},
        registered,
      ),
    ).toBe("wuu");
  });

  it("defaults to the empty string when the thread has never set a focus", () => {
    expect(chatFocusValueForThread({ id: "thread-1" }, {}, registered)).toBe("");
  });

  it("prefers the in-session override for the thread over the thread's own value", () => {
    expect(
      chatFocusValueForThread(
        { id: "thread-1", focus_workspace: "wuu" },
        { "thread-1": "~" },
        registered,
      ),
    ).toBe("~");
  });

  it("ignores overrides keyed to a different thread", () => {
    expect(
      chatFocusValueForThread(
        { id: "thread-1", focus_workspace: "wuu" },
        { "thread-2": "~" },
        registered,
      ),
    ).toBe("wuu");
  });

  it("falls back to 全部工作区 when the focused project is no longer registered", () => {
    // The DM was focused on "gone", but that workspace has since been removed
    // (or moved away), so it drops out of the roster and the focus resolves to
    // the union — from thread.focus_workspace...
    expect(
      chatFocusValueForThread(
        { id: "thread-1", focus_workspace: "gone" },
        {},
        registered,
      ),
    ).toBe("");
    // ...and from a stale in-session override too.
    expect(
      chatFocusValueForThread(
        { id: "thread-1", focus_workspace: "wuu" },
        { "thread-1": "gone" },
        registered,
      ),
    ).toBe("");
  });
});

describe("focusWorkspaceSendValue", () => {
  it("sends nothing when the chip was never touched this session", () => {
    expect(
      focusWorkspaceSendValue({ focus_workspace: "wuu" }, undefined),
    ).toBeUndefined();
    expect(focusWorkspaceSendValue(undefined, undefined)).toBeUndefined();
  });

  it("sends nothing when the override matches the thread's own value", () => {
    expect(
      focusWorkspaceSendValue({ focus_workspace: "wuu" }, "wuu"),
    ).toBeUndefined();
    // Undefined/absent focus_workspace on the thread is equivalent to "".
    expect(focusWorkspaceSendValue({}, "")).toBeUndefined();
    expect(focusWorkspaceSendValue(undefined, "")).toBeUndefined();
  });

  it("sends the override when it differs from the thread's own value", () => {
    expect(focusWorkspaceSendValue({ focus_workspace: "wuu" }, "~")).toBe("~");
    expect(focusWorkspaceSendValue({}, "~")).toBe("~");
    // Resetting back to "全部工作区" from something else must still send
    // the empty string explicitly — it is a real, intentional change.
    expect(focusWorkspaceSendValue({ focus_workspace: "wuu" }, "")).toBe("");
  });
});

describe("isGroupThread", () => {
  it("returns true when group is true", () => {
    expect(isGroupThread({ group: true })).toBe(true);
  });

  it("returns false when group is false", () => {
    expect(isGroupThread({ group: false })).toBe(false);
  });

  it("returns false when group is undefined", () => {
    expect(isGroupThread({})).toBe(false);
  });
});

describe("scratchThreadSummaries excludes group threads", () => {
  it("drops group threads the same way it drops DM threads", () => {
    const [groupSummary] = summarizeThreadsForSidebar([
      { ...threadWithUserTexts(["群聊消息"]), id: "thread-group", group: true },
    ]);
    const [scratchSummary] = summarizeThreadsForSidebar([
      { ...threadWithUserTexts(["普通会话"]), id: "thread-scratch" },
    ]);
    const results = scratchThreadSummaries(
      [groupSummary, scratchSummary],
      [],
    );
    expect(results.map((thread) => thread.id)).toEqual([scratchSummary.id]);
  });
});

describe("sidebar pin/archive matrix", () => {
  // Mental model under test: pinning MOVES an entity to 置顶 (no
  // duplicate left in its own section); archiving removes it from the
  // sidebar completely (own section AND 置顶).
  function summaries(
    overrides: Array<Partial<Thread> & { id: string }>,
  ): ThreadSummary[] {
    return summarizeThreadsForSidebar(
      overrides.map((entry) => ({
        ...threadWithUserTexts([entry.id]),
        ...entry,
      })),
    );
  }

  it("groupThreadSummaries lists only live, unpinned group threads", () => {
    const all = summaries([
      { id: "group-live", group: true },
      { id: "group-pinned", group: true, pinned: true },
      { id: "group-archived", group: true, archived: true },
      { id: "scratch-live" },
      { id: "dm-live", dm_participant_id: "prt-1" },
    ]);
    expect(groupThreadSummaries(all).map((thread) => thread.id)).toEqual([
      "group-live",
    ]);
  });

  it("pinnedThreadSummaries hosts pinned entities of every kind, never archived ones", () => {
    const all = summaries([
      { id: "scratch-pinned", pinned: true },
      { id: "group-pinned", group: true, pinned: true },
      { id: "dm-pinned", dm_participant_id: "prt-1", pinned: true },
      { id: "scratch-live" },
      { id: "group-archived-pinned", group: true, pinned: true, archived: true },
    ]);
    const pinned = pinnedThreadSummaries(all).map((thread) => thread.id);
    expect(pinned).toContain("scratch-pinned");
    expect(pinned).toContain("group-pinned");
    expect(pinned).toContain("dm-pinned");
    expect(pinned).not.toContain("scratch-live");
    // Archived-but-pinned must not ghost in 置顶.
    expect(pinned).not.toContain("group-archived-pinned");
  });

  it("scratchThreadSummaries drops pinned and archived entries (move semantics)", () => {
    const all = summaries([
      { id: "scratch-live" },
      { id: "scratch-pinned", pinned: true },
      { id: "scratch-archived", archived: true },
    ]);
    expect(
      scratchThreadSummaries(all, []).map((thread) => thread.id),
    ).toEqual(["scratch-live"]);
  });
});

describe("composerDraftHasContent", () => {
  it("is false for a blank draft and true once text, an image, or a file is present", () => {
    expect(
      composerDraftHasContent({ prompt: "", images: [], files: [] }),
    ).toBe(false);
    expect(
      composerDraftHasContent({ prompt: "   ", images: [], files: [] }),
    ).toBe(false);
    expect(
      composerDraftHasContent({ prompt: "hi", images: [], files: [] }),
    ).toBe(true);
    expect(
      composerDraftHasContent({
        prompt: "",
        images: [{ id: "img-1", dataUrl: "data:," }] as never,
        files: [],
      }),
    ).toBe(true);
    expect(
      composerDraftHasContent({
        prompt: "",
        images: [],
        files: [{ id: "file-1", name: "a.txt" }] as never,
      }),
    ).toBe(true);
  });
});

describe("applyLoadedRuntimeWithDraftCarry (R2: draft follows a pill switch)", () => {
  const projectContext: RuntimeContext = {
    kind: "project",
    project_id: "p1",
    cwd: "/repo/p1",
  };
  const noProjectContext: RuntimeContext = {
    kind: "no_project",
    cwd: "/scratch/2026-07-03",
  };

  function draftTab(
    id: string,
    context: RuntimeContext,
    prompt: string,
  ): SessionTab {
    return {
      id,
      kind: "draft",
      context,
      title: "新对话",
      prompt,
      images: [],
      files: [],
      createdAt: 0,
    };
  }

  function draftOf(tab: SessionTab): string | undefined {
    return tab.kind === "draft" || tab.kind === "thread"
      ? tab.prompt
      : undefined;
  }

  it("carries a non-empty draft into the target context's tab, overwriting that tab's own stale content", () => {
    const oldTab = draftTab("draft:old", projectContext, "");
    const staleTargetTab = draftTab(
      "draft:target-stale",
      noProjectContext,
      "an earlier, unrelated draft",
    );
    const current: AppState = {
      ...initialState,
      activeContext: projectContext,
      sessionTabs: [oldTab, staleTargetTab],
      activeSessionTabID: oldTab.id,
    };
    const outgoingDraft: ComposerDraftState = {
      prompt: "hello from the project draft",
      images: [],
      files: [],
    };
    const loadedState: Partial<AppState> = {
      activeContext: noProjectContext,
      thread: undefined,
    };

    const next = applyLoadedRuntimeWithDraftCarry(
      current,
      loadedState,
      outgoingDraft,
    );

    // Lands on the pre-existing draft tab for the target context...
    expect(next.activeSessionTabID).toBe(staleTargetTab.id);
    const resultTargetTab = next.sessionTabs.find(
      (tab) => tab.id === staleTargetTab.id,
    );
    // ...and its content is the carried draft, not its own stale one.
    expect(draftOf(resultTargetTab!)).toBe("hello from the project draft");
    // The tab the user left behind keeps whatever it already had — no
    // duplicate copy of the carried text is written back into it.
    const resultOldTab = next.sessionTabs.find((tab) => tab.id === oldTab.id);
    expect(draftOf(resultOldTab!)).toBe("");
  });

  it("does not carry an empty draft (falls back to persisting + loading normally)", () => {
    const oldTab = draftTab("draft:old", projectContext, "");
    const current: AppState = {
      ...initialState,
      activeContext: projectContext,
      sessionTabs: [oldTab],
      activeSessionTabID: oldTab.id,
    };
    const emptyDraft: ComposerDraftState = {
      prompt: "",
      images: [],
      files: [],
    };
    const loadedState: Partial<AppState> = {
      activeContext: noProjectContext,
      thread: undefined,
    };

    const next = applyLoadedRuntimeWithDraftCarry(
      current,
      loadedState,
      emptyDraft,
    );

    // A brand new draft tab is created for the target context, still empty.
    expect(next.activeSessionTabID).not.toBe(oldTab.id);
    const resultNewTab = next.sessionTabs.find(
      (tab) => tab.id === next.activeSessionTabID,
    );
    expect(draftOf(resultNewTab!)).toBe("");
  });

  it("does not carry when the outgoing tab is a real thread, not a draft", () => {
    const thread: Thread = {
      ...threadWithUserTexts(["reply in progress"]),
      id: "thread-1",
      cwd: projectContext.cwd,
    };
    const threadTab = createThreadSessionTab(thread, projectContext);
    const current: AppState = {
      ...initialState,
      activeContext: projectContext,
      thread,
      sessionTabs: [threadTab],
      activeSessionTabID: threadTab.id,
      threads: [thread],
    };
    const outgoingDraft: ComposerDraftState = {
      prompt: "typed mid-thread, not a fresh draft",
      images: [],
      files: [],
    };
    const loadedState: Partial<AppState> = {
      activeContext: noProjectContext,
      thread: undefined,
    };

    const next = applyLoadedRuntimeWithDraftCarry(
      current,
      loadedState,
      outgoingDraft,
    );

    // The thread tab keeps the in-progress reply (ordinary persist path)...
    const resultThreadTab = next.sessionTabs.find(
      (tab) => tab.id === threadTab.id,
    );
    expect(draftOf(resultThreadTab!)).toBe(
      "typed mid-thread, not a fresh draft",
    );
    // ...and the freshly-created no-project draft tab starts blank, since
    // this isn't the "carry a fresh draft along" case.
    const resultNewTab = next.sessionTabs.find(
      (tab) => tab.id === next.activeSessionTabID,
    );
    expect(draftOf(resultNewTab!)).toBe("");
  });
});

describe("conversationSearchContextLabel (R4: no raw scratch paths in the UI)", () => {
  // A no-project (scratch) thread's cwd is an internal
  // ~/.wuu/scratch/<date> directory (allocateNoProjectCwd in
  // src/main/projects.ts) — not something a user ever named or should see
  // spelled out in the conversation search results.
  const scratchThread: Thread = {
    ...threadWithUserTexts(["scratch prompt"]),
    id: "scratch-thread",
    cwd: "/Users/test/.wuu/scratch/2026-07-03",
  };

  it("shows the project name for a thread that belongs to a registered project", () => {
    const project: DesktopProject = {
      id: "proj-1",
      name: "MyApp",
      path: "/repo/myapp",
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    };
    const thread: Thread = {
      ...threadWithUserTexts(["hi"]),
      id: "project-thread",
      cwd: project.path,
    };
    expect(conversationSearchContextLabel(thread, [project])).toBe("MyApp");
  });

  it("labels a no-project (scratch) thread 无项目 instead of its raw scratch directory name", () => {
    const label = conversationSearchContextLabel(scratchThread, []);
    expect(label).toBe("无项目");
    expect(label).not.toContain("2026-07-03");
    expect(label).not.toContain("scratch");
  });

  it("still says 无项目 when other registered projects exist but none match this thread's cwd", () => {
    const otherProject: DesktopProject = {
      id: "proj-2",
      name: "OtherApp",
      path: "/repo/other",
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    };
    expect(
      conversationSearchContextLabel(scratchThread, [otherProject]),
    ).toBe("无项目");
  });
});

describe("sessionTabLabel (draft tabs read as their workspace)", () => {
  const project: DesktopProject = {
    id: "p1",
    name: "acme-web",
    path: "/repo/acme-web",
    created_at: "2026-07-04T00:00:00.000Z",
    updated_at: "2026-07-04T00:00:00.000Z",
  };
  const state: AppState = { ...initialState, projects: [project] };

  it("labels a project draft tab with the project name", () => {
    const tab = createDraftSessionTab("draft:initial:project:p1", {
      kind: "project",
      project_id: "p1",
      cwd: "/repo/acme-web",
    });
    expect(sessionTabLabel(tab, state)).toBe("acme-web");
  });

  it("labels a no-project draft tab as 对话", () => {
    const tab = createDraftSessionTab("draft:initial:no_project:/scratch", {
      kind: "no_project",
      cwd: "/scratch",
    });
    expect(sessionTabLabel(tab, state)).toBe("对话");
  });

  it("does not surface the typed prompt in the draft label", () => {
    const tab = createDraftSessionTab(
      "draft:initial:project:p1",
      { kind: "project", project_id: "p1", cwd: "/repo/acme-web" },
      { prompt: "please refactor the auth module", images: [], files: [] },
    );
    expect(sessionTabLabel(tab, state)).toBe("acme-web");
  });

  it("falls back to the cwd basename when the project is gone (removed/relocated)", () => {
    const tab = createDraftSessionTab("draft:initial:project:ghost", {
      kind: "project",
      project_id: "ghost",
      cwd: "/repo/orphaned-dir",
    });
    expect(sessionTabLabel(tab, state)).toBe("orphaned-dir");
  });
});

describe("reconcileResumedThreadTurns", () => {
  function threadWithTurnIDs(ids: string[]): Thread {
    return {
      ...threadWithUserTexts([]),
      turns: ids.map((id) => ({
        id,
        items_view: "full",
        status: "completed",
        items: [
          {
            id: `${id}-user`,
            type: "user_message",
            status: "completed",
            role: "user",
            text: `message ${id}`,
          },
        ],
      })),
    };
  }

  it("salvages the client tail when the resumed snapshot is a strict prefix (just-sent turn not yet in the snapshot)", () => {
    // The reported bug: send a message (turn-3 lands only in client state),
    // switch session tabs, switch back. resumeThread returns [turn-1, turn-2]
    // — lagging behind — and the wholesale replace would drop turn-3.
    const resumed = threadWithTurnIDs(["turn-1", "turn-2"]);
    const local = threadWithTurnIDs(["turn-1", "turn-2", "turn-3"]);
    const merged = reconcileResumedThreadTurns(resumed, local);
    expect(merged.turns.map((turn) => turn.id)).toEqual([
      "turn-1",
      "turn-2",
      "turn-3",
    ]);
    // The overlapping prefix uses the server's (fresher) turn objects; only
    // the client's extra tail is carried over verbatim.
    expect(merged.turns[0]).toBe(resumed.turns[0]);
    expect(merged.turns[1]).toBe(resumed.turns[1]);
    expect(merged.turns[2]).toBe(local.turns[2]);
  });

  it("salvages client tail items when a resumed turn snapshot lags behind live participant messages", () => {
    const resumed = threadWithTurnIDs(["turn-1", "turn-2"]);
    const local = threadWithTurnIDs(["turn-1", "turn-2"]);
    const liveParticipant: ThreadItem = {
      id: "turn-2-participant",
      seq: 8,
      type: "participant_message",
      status: "completed",
      role: "participant",
      post_kind: "result",
      text: "我已经回复了",
      participant: {
        id: "prt-andy",
        kind: "named",
        name: "Andy",
      },
    };
    local.turns[1] = {
      ...local.turns[1],
      items: [...local.turns[1].items, liveParticipant],
    };

    const merged = reconcileResumedThreadTurns(resumed, local);

    expect(merged.turns).toHaveLength(2);
    expect(merged.turns[1].items).toEqual([
      resumed.turns[1].items[0],
      liveParticipant,
    ]);
    expect(chatMessagesFromTurns(merged.turns).map((row) => row.kind)).toEqual([
      "user",
      "user",
      "participant",
    ]);
  });

  it("trusts the resumed snapshot when history diverges (edit/fork truncated the tail)", () => {
    // An edit/fork replaces turn-2 with a new turn id and truncates what
    // followed. That is a genuine server-side truncation, not lag, so we must
    // NOT resurrect the client's stale [turn-2, turn-3].
    const resumed = threadWithTurnIDs(["turn-1", "turn-2b"]);
    const local = threadWithTurnIDs(["turn-1", "turn-2", "turn-3"]);
    const merged = reconcileResumedThreadTurns(resumed, local);
    expect(merged).toBe(resumed);
    expect(merged.turns.map((turn) => turn.id)).toEqual(["turn-1", "turn-2b"]);
  });

  it("trusts the resumed snapshot when it is at least as long as the local copy (server is ahead or equal)", () => {
    const resumed = threadWithTurnIDs(["turn-1", "turn-2", "turn-3"]);
    const local = threadWithTurnIDs(["turn-1", "turn-2"]);
    expect(reconcileResumedThreadTurns(resumed, local)).toBe(resumed);
  });

  it("returns the resumed thread unchanged when there is no local copy", () => {
    const resumed = threadWithTurnIDs(["turn-1"]);
    expect(reconcileResumedThreadTurns(resumed, undefined)).toBe(resumed);
  });
});

describe("threadNeedsResumeOnReselect", () => {
  const context: RuntimeContext = {
    kind: "no_project",
    cwd: "/tmp/wuu-scratch",
  };

  it("asks the caller to resume when the active thread is only an empty list snapshot", () => {
    const thread = { ...threadWithUserTexts([]), turns: [] };
    const state: AppState = {
      ...initialState,
      activeContext: context,
      thread,
      threads: [thread],
      sessionTabs: [createThreadSessionTab(thread, context)],
      activeSessionTabID: threadSessionTabID(thread.id),
    };

    expect(threadNeedsResumeOnReselect(state, thread.id)).toBe(true);
  });

  it("does not re-resume an active thread that already has loaded turns", () => {
    const thread = threadWithUserTexts(["hello"]);
    const state: AppState = {
      ...initialState,
      activeContext: context,
      thread,
      threads: [thread],
      sessionTabs: [createThreadSessionTab(thread, context)],
      activeSessionTabID: threadSessionTabID(thread.id),
    };

    expect(threadNeedsResumeOnReselect(state, thread.id)).toBe(false);
  });
});
