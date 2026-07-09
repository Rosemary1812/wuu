import { describe, expect, it, vi } from "vitest";
import type { InitializeResult, RuntimeContext, Thread } from "../shared/protocol";
import {
  createThreadSessionTab,
  initialSplitComposerDrafts,
  initialState,
  threadSessionTabID,
  type AppState,
  type ComposerDraftState,
  type ConversationPaneID,
} from "./AppState";
import { createConversationDemoPaneActions } from "./ConversationDemoPaneActions";
import type { ComposerFile, ComposerImage } from "./ComposerMessages";
import type { EnvironmentPanelMenu } from "./EnvironmentPanel";
import type { WorkspacePanelView } from "./WorkspacePanels";

function projectContext(): RuntimeContext {
  return { kind: "project", project_id: "project-1", cwd: "/tmp/project-1" };
}

function initialized(): InitializeResult {
  return {
    protocol_version: "1",
    provider: "codex",
    model: "gpt-5",
    workspace_root: "/tmp/project-1",
  };
}

function thread(id: string, status: Thread["status"] = "idle"): Thread {
  return {
    id,
    title: id,
    preview: id,
    model_provider: "codex",
    model: "gpt-5",
    cwd: "/tmp/project-1",
    status,
    pinned: false,
    archived: false,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    turns: [],
  };
}

function buildActions({
  initial = {
    ...initialState,
    activeContext: projectContext(),
    initialized: initialized(),
    status: "ready",
  },
}: {
  initial?: AppState;
} = {}) {
  let appState = initial;
  let prompt = "draft";
  let composerImages: ComposerImage[] = [
    { id: "image-1", media_type: "image/png", data: "" },
  ];
  let composerFiles: ComposerFile[] = [
    {
      id: "file-1",
      media_type: "application/pdf",
      data: "",
      filename: "file.pdf",
    },
  ];
  let splitDrafts = initialSplitComposerDrafts();
  let archiveConfirmThreadID: string | undefined = "thread-1";
  let workspaceMode: WorkspacePanelView | undefined = "files";
  let runDebugOpen = true;
  let environmentPanelOpen = false;
  let environmentPanelDismissed = true;
  let environmentPanelMenu: EnvironmentPanelMenu = "branch";
  const localDemoThreadsRef = { current: new Map<string, Thread>() };
  const cancelViewSwitch = vi.fn();
  const moveSplitDraftToGlobalComposer = vi.fn();
  const actions = createConversationDemoPaneActions({
    getAppState: () => appState,
    setAppState: (update) => {
      appState = typeof update === "function" ? update(appState) : update;
    },
    localDemoThreadsRef,
    cancelViewSwitch,
    setArchiveConfirmThreadID: (update) => {
      archiveConfirmThreadID =
        typeof update === "function" ? update(archiveConfirmThreadID) : update;
    },
    setWorkspaceMode: (mode) => {
      workspaceMode = mode;
    },
    setPrompt: (update) => {
      prompt = typeof update === "function" ? update(prompt) : update;
    },
    setComposerImages: (update) => {
      composerImages =
        typeof update === "function" ? update(composerImages) : update;
    },
    setComposerFiles: (update) => {
      composerFiles =
        typeof update === "function" ? update(composerFiles) : update;
    },
    setSplitComposerDrafts: (update) => {
      splitDrafts =
        typeof update === "function" ? update(splitDrafts) : update;
    },
    moveSplitDraftToGlobalComposer,
    setRunDebugOpen: (open) => {
      runDebugOpen = open;
    },
    setEnvironmentPanelOpen: (open) => {
      environmentPanelOpen = open;
    },
    setEnvironmentPanelDismissed: (dismissed) => {
      environmentPanelDismissed = dismissed;
    },
    setEnvironmentPanelMenu: (menu) => {
      environmentPanelMenu = menu;
    },
  });
  return {
    actions,
    getAppState: () => appState,
    getComposerState: () => ({
      prompt,
      composerImages,
      composerFiles,
      splitDrafts,
      archiveConfirmThreadID,
      workspaceMode,
    }),
    getEnvironmentState: () => ({
      runDebugOpen,
      environmentPanelOpen,
      environmentPanelDismissed,
      environmentPanelMenu,
    }),
    localDemoThreadsRef,
    cancelViewSwitch,
    moveSplitDraftToGlobalComposer,
  };
}

describe("createConversationDemoPaneActions", () => {
  it("seeds the agent-tree demo and selects the parent thread", () => {
    const harness = buildActions();

    harness.actions.seedAgentTreeDemo();

    expect(harness.cancelViewSwitch).toHaveBeenCalled();
    expect(harness.getComposerState()).toMatchObject({
      prompt: "",
      composerImages: [],
      composerFiles: [],
      archiveConfirmThreadID: undefined,
      workspaceMode: undefined,
    });
    expect(harness.localDemoThreadsRef.current.size).toBeGreaterThan(1);
    expect(harness.getAppState().thread?.id).toBeTruthy();
    expect(harness.getAppState().activeSessionTabID).toBe(
      threadSessionTabID(harness.getAppState().thread?.id ?? ""),
    );
    expect(harness.getAppState().status).toBe("ready");
  });

  it("seeds the plan fixture and opens the environment debug panel", () => {
    const harness = buildActions();

    harness.actions.seedPlanPanelDebug();

    expect(harness.getAppState().thread?.id).toContain("plan");
    expect(harness.getEnvironmentState()).toEqual({
      runDebugOpen: false,
      environmentPanelOpen: true,
      environmentPanelDismissed: false,
      environmentPanelMenu: null,
    });
  });

  it("activates the secondary pane when it has a thread", () => {
    const secondary = thread("secondary", "in_progress");
    const harness = buildActions({
      initial: {
        ...initialState,
        activeContext: projectContext(),
        initialized: initialized(),
        thread: thread("primary"),
        secondaryThread: secondary,
        activePane: "primary",
      },
    });

    harness.actions.activateConversationPane("secondary");

    expect(harness.getAppState().activePane).toBe("secondary");
    expect(harness.getAppState().activeSessionTabID).toBe(
      threadSessionTabID("secondary"),
    );
    expect(harness.getAppState().running).toBe(true);
  });

  it("keeps primary active when trying to activate an empty secondary pane", () => {
    const harness = buildActions({
      initial: {
        ...initialState,
        activeContext: projectContext(),
        initialized: initialized(),
        thread: thread("primary"),
        activePane: "primary",
      },
    });

    harness.actions.activateConversationPane("secondary");

    expect(harness.getAppState().activePane).toBe("primary");
  });

  it("closes the secondary pane and restores the primary thread tab", () => {
    const harness = buildActions({
      initial: {
        ...initialState,
        activeContext: projectContext(),
        initialized: initialized(),
        thread: thread("primary"),
        secondaryThread: thread("secondary"),
        activePane: "secondary",
      },
    });

    harness.actions.closeConversationPane("secondary");

    expect(harness.moveSplitDraftToGlobalComposer).toHaveBeenCalledWith(
      "primary",
    );
    expect(harness.getAppState().secondaryThread).toBeUndefined();
    expect(harness.getAppState().activePane).toBe("primary");
    expect(harness.getAppState().activeSessionTabID).toBe(
      threadSessionTabID("primary"),
    );
  });

  it("promotes the secondary thread when closing the primary pane", () => {
    const secondary = thread("secondary");
    const harness = buildActions({
      initial: {
        ...initialState,
        activeContext: projectContext(),
        initialized: initialized(),
        thread: thread("primary"),
        secondaryThread: secondary,
        activePane: "primary",
      },
    });

    harness.actions.closeConversationPane("primary");

    expect(harness.moveSplitDraftToGlobalComposer).toHaveBeenCalledWith(
      "secondary",
    );
    expect(harness.getAppState().thread?.id).toBe("secondary");
    expect(harness.getAppState().secondaryThread).toBeUndefined();
  });

  it("opens a fresh draft tab when closing the only primary pane", () => {
    const context = projectContext();
    const primary = thread("primary");
    const harness = buildActions({
      initial: {
        ...initialState,
        activeContext: context,
        initialized: initialized(),
        thread: primary,
        sessionTabs: [createThreadSessionTab(primary, context)],
        activeSessionTabID: threadSessionTabID("primary"),
        activePane: "primary",
      },
    });

    harness.actions.closeConversationPane("primary");

    expect(harness.getAppState().thread).toBeUndefined();
    expect(harness.getAppState().activeSessionTabID).toContain("draft:closed:");
    expect(harness.getAppState().sessionTabs.at(-1)?.kind).toBe("draft");
  });
});
