import { afterEach, describe, expect, it, vi } from "vitest";
import type { RuntimeContext } from "../shared/protocol";
import {
  createDraftSessionTab,
  createFileSessionTab,
  emptyComposerDraft,
  initialState,
  type AppState,
  type ComposerDraftState,
  type SessionTab,
} from "./AppState";
import { createSessionTabActions } from "./SessionTabActions";

function projectContext(id = "project-1"): RuntimeContext {
  return { kind: "project", project_id: id, cwd: `/tmp/${id}` };
}

function buildActions({
  initial,
  draft = emptyComposerDraft(),
}: {
  initial: AppState;
  draft?: ComposerDraftState;
}) {
  let appState = initial;
  let currentDraft = draft;
  const setWorkspaceMode = vi.fn();
  const clearPrimaryComposerDraft = vi.fn(() => {
    currentDraft = emptyComposerDraft();
  });
  const restorePrimaryComposerDraft = vi.fn((nextDraft: ComposerDraftState) => {
    currentDraft = {
      prompt: nextDraft.prompt,
      images: [...nextDraft.images],
      files: [...nextDraft.files],
    };
  });
  const resetSplitComposerDrafts = vi.fn();
  const nextDraftSessionTab = vi.fn((context: RuntimeContext): SessionTab =>
    createDraftSessionTab("draft:new", context),
  );
  const selectThread = vi.fn();
  const useNoProject = vi.fn();
  const poppingOutTabIDsRef = { current: new Set<string>() };

  const actions = createSessionTabActions({
    getAppState: () => appState,
    setAppState: (update) => {
      appState = typeof update === "function" ? update(appState) : update;
    },
    getPrimaryComposerDraft: () => currentDraft,
    restorePrimaryComposerDraft,
    clearPrimaryComposerDraft,
    resetSplitComposerDrafts,
    nextDraftSessionTab,
    selectThread,
    useNoProject,
    setArchiveConfirmThreadID: vi.fn(),
    setWorkspaceMode,
    poppingOutTabIDsRef,
    beginViewSwitch: vi.fn(() => 1),
    finishViewSwitch: vi.fn(() => true),
    cancelViewSwitch: vi.fn(),
    loadRuntime: vi.fn(),
    selectRuntimeContext: vi.fn(),
  });

  return {
    actions,
    getAppState: () => appState,
    getCurrentDraft: () => currentDraft,
    clearPrimaryComposerDraft,
    restorePrimaryComposerDraft,
    resetSplitComposerDrafts,
    nextDraftSessionTab,
    selectThread,
    useNoProject,
    setWorkspaceMode,
    poppingOutTabIDsRef,
  };
}

afterEach(() => {
  vi.restoreAllMocks();
  Reflect.deleteProperty(window, "wuu");
});

describe("createSessionTabActions", () => {
  it("starts a new thread by reusing an already blank active draft tab", async () => {
    const context = projectContext();
    const draftTab = createDraftSessionTab("draft:active", context);
    const harness = buildActions({
      initial: {
        ...initialState,
        activeContext: context,
        activeSessionTabID: draftTab.id,
        sessionTabs: [draftTab],
      },
    });

    await harness.actions.startNewThread();

    expect(harness.clearPrimaryComposerDraft).toHaveBeenCalled();
    expect(harness.nextDraftSessionTab).not.toHaveBeenCalled();
    expect(harness.getAppState().activeSessionTabID).toBe(draftTab.id);
    expect(harness.getAppState().thread).toBeUndefined();
  });

  it("selects a file tab in the same context and preserves the outgoing draft", async () => {
    const context = projectContext();
    const draftTab = createDraftSessionTab("draft:active", context);
    const fileTab = createFileSessionTab(context, "/tmp/project-1/README.md");
    const harness = buildActions({
      initial: {
        ...initialState,
        activeContext: context,
        activeSessionTabID: draftTab.id,
        sessionTabs: [draftTab, fileTab],
      },
      draft: { prompt: "keep me", images: [], files: [] },
    });

    await harness.actions.selectSessionTab(fileTab.id);

    expect(harness.setWorkspaceMode).toHaveBeenCalledWith("files");
    expect(harness.resetSplitComposerDrafts).toHaveBeenCalled();
    expect(harness.getAppState().activeSessionTabID).toBe(fileTab.id);
    const persistedDraft = harness.getAppState().sessionTabs[0];
    expect(persistedDraft?.kind).toBe("draft");
    expect(persistedDraft?.kind === "draft" ? persistedDraft.prompt : "").toBe(
      "keep me",
    );
  });

  it("closes the active tab by restoring the fallback draft tab", async () => {
    const context = projectContext();
    const fileTab = createFileSessionTab(context, "/tmp/project-1/README.md");
    const fallbackDraft = createDraftSessionTab("draft:fallback", context, {
      prompt: "restored",
      images: [],
      files: [],
    });
    const harness = buildActions({
      initial: {
        ...initialState,
        activeContext: context,
        activeSessionTabID: fileTab.id,
        sessionTabs: [fileTab, fallbackDraft],
      },
    });

    await harness.actions.closeSessionTab(fileTab.id);

    expect(harness.setWorkspaceMode).toHaveBeenCalledWith(undefined);
    expect(harness.restorePrimaryComposerDraft).toHaveBeenCalledWith({
      prompt: "restored",
      images: [],
      files: [],
    });
    expect(harness.resetSplitComposerDrafts).toHaveBeenCalled();
    expect(harness.getCurrentDraft().prompt).toBe("restored");
    expect(harness.getAppState().activeSessionTabID).toBe(fallbackDraft.id);
  });

  it("reorders session tabs", () => {
    const context = projectContext();
    const first = createDraftSessionTab("draft:first", context);
    const second = createDraftSessionTab("draft:second", context);
    const third = createDraftSessionTab("draft:third", context);
    const harness = buildActions({
      initial: {
        ...initialState,
        activeContext: context,
        activeSessionTabID: first.id,
        sessionTabs: [first, second, third],
      },
    });

    harness.actions.reorderSessionTabs(first.id, third.id);

    expect(harness.getAppState().sessionTabs.map((tab) => tab.id)).toEqual([
      second.id,
      third.id,
      first.id,
    ]);
  });

  it("pops out a draft tab and closes it after the detached window opens", async () => {
    const context = projectContext();
    const activeDraft = createDraftSessionTab("draft:active", context);
    const fallbackDraft = createDraftSessionTab("draft:fallback", context);
    const popOutSession = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(window, "wuu", {
      configurable: true,
      value: { popOutSession },
    });
    const harness = buildActions({
      initial: {
        ...initialState,
        activeContext: context,
        activeSessionTabID: activeDraft.id,
        sessionTabs: [activeDraft, fallbackDraft],
      },
    });

    await harness.actions.popOutSessionTab(activeDraft.id);

    expect(popOutSession).toHaveBeenCalledWith({
      kind: "draft",
      context,
    });
    expect(harness.getAppState().sessionTabs.map((tab) => tab.id)).toEqual([
      fallbackDraft.id,
    ]);
    expect(harness.getAppState().activeSessionTabID).toBe(fallbackDraft.id);
    expect(harness.poppingOutTabIDsRef.current.has(activeDraft.id)).toBe(false);
  });
});
