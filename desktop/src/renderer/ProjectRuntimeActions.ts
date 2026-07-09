import type { SetStateAction } from "react";
import type { DesktopProject } from "../shared/protocol";
import {
  activeSessionTab,
  applyLoadedRuntimeWithDraftCarry,
  composerDraftHasContent,
  emptyComposerDraft,
  ensureSessionTab,
  isAnyThreadRunning,
  persistActiveSessionTabDraft,
  sameRuntimeContext,
  withLoadedRuntimeSessionTab,
  type AppState,
  type ComposerDraftState,
  type SessionTab,
} from "./AppState";
import { loadRuntime as defaultLoadRuntime } from "./RuntimeLoadState";

type SetAppState = (update: SetStateAction<AppState>) => void;

export type ProjectRuntimeActionsDeps = {
  getAppState: () => AppState;
  setAppState: SetAppState;
  getPrimaryComposerDraft: () => ComposerDraftState;
  clearPrimaryComposerDraft: () => void;
  restoreLoadedRuntimeComposerDraft: (
    loadedState: Partial<AppState>,
    carryDraft?: ComposerDraftState,
  ) => void;
  nextDraftSessionTab: (context: NonNullable<AppState["activeContext"]>) => SessionTab;
  closeProjectMenus: () => void;
  setArchiveConfirmThreadID: (threadID: string | undefined) => void;
  setWorkspaceMode: (mode: undefined) => void;
  beginViewSwitch: (kind: "thread" | "project" | "runtime", targetID: string) => number;
  finishViewSwitch: (requestID: number) => boolean;
  cancelViewSwitch: () => void;
  loadRuntime?: typeof defaultLoadRuntime;
};

export type ProjectRuntimeActions = {
  openProject: (projectId: string) => Promise<void>;
  selectProjectForNewThread: (projectId: string) => Promise<void>;
  startNewThreadForProject: (projectId: string) => Promise<void>;
  createBlankProject: () => Promise<void>;
  chooseProjectFolder: () => Promise<void>;
  removeProject: (projectId: string) => Promise<void>;
  relocateProject: (projectId: string) => Promise<void>;
  useNoProject: (fresh: boolean) => Promise<void>;
};

export function createProjectRuntimeActions(
  deps: ProjectRuntimeActionsDeps,
): ProjectRuntimeActions {
  const loadRuntime = deps.loadRuntime ?? defaultLoadRuntime;

  function setStatus(status: string): void {
    deps.setAppState((current) => ({
      ...current,
      status,
    }));
  }

  function switchToFreshDraft(context: NonNullable<AppState["activeContext"]>): void {
    const nextTab = deps.nextDraftSessionTab(context);
    deps.clearPrimaryComposerDraft();
    deps.setAppState((current) => ({
      ...persistActiveSessionTabDraft(current, deps.getPrimaryComposerDraft()),
      thread: undefined,
      secondaryThread: undefined,
      activePane: "primary",
      sessionTabs: ensureSessionTab(current.sessionTabs, nextTab),
      activeSessionTabID: nextTab.id,
      allowThreadAutoActivation: false,
      running: false,
      status: "ready",
    }));
  }

  async function openProject(projectId: string): Promise<void> {
    const currentState = deps.getAppState();
    if (
      projectId === currentState.activeProjectId &&
      currentState.activeContext?.kind === "project"
    ) {
      deps.closeProjectMenus();
      deps.setArchiveConfirmThreadID(undefined);
      deps.setWorkspaceMode(undefined);
      if (currentState.thread || currentState.secondaryThread) {
        switchToFreshDraft(currentState.activeContext);
      }
      return;
    }
    const requestID = deps.beginViewSwitch("project", projectId);
    deps.closeProjectMenus();
    deps.setWorkspaceMode(undefined);
    const outgoingDraft = deps.getPrimaryComposerDraft();
    try {
      const projectState = await window.wuu.selectProject(projectId);
      const loadedState = await loadRuntime(projectState, {
        resumeLatestThread: false,
      });
      if (!deps.finishViewSwitch(requestID)) {
        return;
      }
      deps.restoreLoadedRuntimeComposerDraft(loadedState);
      deps.setAppState((current) => {
        const next = withLoadedRuntimeSessionTab(
          persistActiveSessionTabDraft(current, outgoingDraft),
          loadedState,
        );
        return {
          ...next,
          thread: undefined,
          secondaryThread: undefined,
          activePane: "primary",
          allowThreadAutoActivation: false,
          running: false,
          status: "ready",
        };
      });
    } catch (error) {
      if (!deps.finishViewSwitch(requestID)) {
        return;
      }
      setStatus(error instanceof Error ? error.message : "open project failed");
    }
  }

  async function selectProjectForNewThread(projectId: string): Promise<void> {
    const currentState = deps.getAppState();
    if (
      projectId === currentState.activeProjectId &&
      currentState.activeContext?.kind === "project"
    ) {
      deps.closeProjectMenus();
      deps.setArchiveConfirmThreadID(undefined);
      deps.setWorkspaceMode(undefined);
      if (currentState.thread || currentState.secondaryThread) {
        switchToFreshDraft(currentState.activeContext);
      }
      return;
    }
    if (isAnyThreadRunning(currentState)) {
      deps.closeProjectMenus();
      setStatus("任务运行中，暂不能切换项目");
      return;
    }
    const requestID = deps.beginViewSwitch("project", projectId);
    deps.closeProjectMenus();
    deps.setArchiveConfirmThreadID(undefined);
    deps.setWorkspaceMode(undefined);
    const outgoingDraft = deps.getPrimaryComposerDraft();
    const carryDraft =
      activeSessionTab(currentState)?.kind === "draft" &&
      composerDraftHasContent(outgoingDraft)
        ? outgoingDraft
        : undefined;
    try {
      const projectState = await window.wuu.selectProject(projectId);
      const loadedState = await loadRuntime(projectState, {
        resumeLatestThread: false,
      });
      if (!deps.finishViewSwitch(requestID)) {
        return;
      }
      deps.restoreLoadedRuntimeComposerDraft(loadedState, carryDraft);
      deps.setAppState((current) => {
        const next = applyLoadedRuntimeWithDraftCarry(
          current,
          loadedState,
          outgoingDraft,
        );
        return {
          ...next,
          thread: undefined,
          secondaryThread: undefined,
          activePane: "primary",
          allowThreadAutoActivation: false,
          running: false,
          status: "ready",
        };
      });
    } catch (error) {
      if (!deps.finishViewSwitch(requestID)) {
        return;
      }
      setStatus(error instanceof Error ? error.message : "open project failed");
    }
  }

  async function startNewThreadForProject(projectId: string): Promise<void> {
    deps.cancelViewSwitch();
    deps.closeProjectMenus();
    deps.setArchiveConfirmThreadID(undefined);
    deps.setWorkspaceMode(undefined);
    const currentState = deps.getAppState();
    if (
      projectId === currentState.activeProjectId &&
      currentState.activeContext?.kind === "project"
    ) {
      const draft = deps.getPrimaryComposerDraft();
      const nextTab =
        activeSessionTab(currentState)?.kind === "draft" &&
        !draft.prompt.trim() &&
        draft.images.length === 0 &&
        draft.files.length === 0
          ? activeSessionTab(currentState)
          : deps.nextDraftSessionTab(currentState.activeContext);
      if (!nextTab) {
        return;
      }
      deps.clearPrimaryComposerDraft();
      deps.setAppState((current) => ({
        ...persistActiveSessionTabDraft(current, deps.getPrimaryComposerDraft()),
        thread: undefined,
        secondaryThread: undefined,
        activePane: "primary",
        sessionTabs: ensureSessionTab(current.sessionTabs, nextTab),
        activeSessionTabID: nextTab.id,
        allowThreadAutoActivation: false,
        running: false,
        status: "ready",
      }));
      return;
    }
    const requestID = deps.beginViewSwitch("project", projectId);
    const outgoingDraft = deps.getPrimaryComposerDraft();
    try {
      const projectState = await window.wuu.selectProject(projectId);
      const loadedState = await loadRuntime(projectState, {
        resumeLatestThread: false,
      });
      if (!deps.finishViewSwitch(requestID)) {
        return;
      }
      if (!loadedState.activeContext) {
        return;
      }
      deps.clearPrimaryComposerDraft();
      const nextTab = deps.nextDraftSessionTab(loadedState.activeContext);
      deps.setAppState((current) => {
        const withDraft = persistActiveSessionTabDraft(current, outgoingDraft);
        return {
          ...withDraft,
          ...loadedState,
          thread: undefined,
          secondaryThread: undefined,
          activePane: "primary",
          sessionTabs: ensureSessionTab(withDraft.sessionTabs, nextTab),
          activeSessionTabID: nextTab.id,
          allowThreadAutoActivation: false,
          running: false,
          status: "ready",
        };
      });
    } catch (error) {
      if (!deps.finishViewSwitch(requestID)) {
        return;
      }
      setStatus(error instanceof Error ? error.message : "open project failed");
    }
  }

  async function createBlankProject(): Promise<void> {
    const currentState = deps.getAppState();
    const requestID = deps.beginViewSwitch("runtime", "create-project");
    deps.closeProjectMenus();
    const outgoingDraft = deps.getPrimaryComposerDraft();
    try {
      const projectState = await window.wuu.createBlankProject();
      if (sameRuntimeContext(projectState.active_context, currentState.activeContext)) {
        if (!deps.finishViewSwitch(requestID)) {
          return;
        }
        deps.setAppState((current) => ({
          ...current,
          projects: projectState.projects,
        }));
        return;
      }
      const loadedState = await loadRuntime(projectState);
      if (!deps.finishViewSwitch(requestID)) {
        return;
      }
      deps.restoreLoadedRuntimeComposerDraft(loadedState);
      deps.setAppState((current) =>
        withLoadedRuntimeSessionTab(
          persistActiveSessionTabDraft(current, outgoingDraft),
          loadedState,
        ),
      );
    } catch (error) {
      if (!deps.finishViewSwitch(requestID)) {
        return;
      }
      setStatus(error instanceof Error ? error.message : "create project failed");
    }
  }

  async function chooseProjectFolder(): Promise<void> {
    const currentState = deps.getAppState();
    const requestID = deps.beginViewSwitch("runtime", "choose-project");
    deps.closeProjectMenus();
    const outgoingDraft = deps.getPrimaryComposerDraft();
    try {
      const projectState = await window.wuu.chooseProjectFolder();
      if (sameRuntimeContext(projectState.active_context, currentState.activeContext)) {
        if (!deps.finishViewSwitch(requestID)) {
          return;
        }
        deps.setAppState((current) => ({
          ...current,
          projects: projectState.projects,
        }));
        return;
      }
      const loadedState = await loadRuntime(projectState);
      if (!deps.finishViewSwitch(requestID)) {
        return;
      }
      deps.restoreLoadedRuntimeComposerDraft(loadedState);
      deps.setAppState((current) =>
        withLoadedRuntimeSessionTab(
          persistActiveSessionTabDraft(current, outgoingDraft),
          loadedState,
        ),
      );
    } catch (error) {
      if (!deps.finishViewSwitch(requestID)) {
        return;
      }
      setStatus(error instanceof Error ? error.message : "open folder failed");
    }
  }

  async function offerRemovedProjectStateCleanup(
    removedProject: DesktopProject | undefined,
  ): Promise<void> {
    if (!removedProject) {
      return;
    }
    if (
      !window.confirm(
        "是否同时清理该项目的本地状态（会话/目标/工件）？记忆将保留归档。",
      )
    ) {
      return;
    }
    try {
      await window.wuu.cleanupProjectState(
        removedProject.id,
        removedProject.path,
      );
    } catch (error) {
      setStatus(
        error instanceof Error ? error.message : "cleanup project state failed",
      );
    }
  }

  async function removeProject(projectId: string): Promise<void> {
    const currentState = deps.getAppState();
    const requestID = deps.beginViewSwitch("runtime", "remove-project");
    const outgoingDraft = deps.getPrimaryComposerDraft();
    const removedProject = currentState.projects.find(
      (project) => project.id === projectId,
    );
    try {
      const projectState = await window.wuu.removeProject(projectId);
      if (sameRuntimeContext(projectState.active_context, currentState.activeContext)) {
        if (!deps.finishViewSwitch(requestID)) {
          return;
        }
        deps.setAppState((current) => ({
          ...current,
          projects: projectState.projects,
        }));
        void offerRemovedProjectStateCleanup(removedProject);
        return;
      }
      const loadedState = await loadRuntime(projectState);
      if (!deps.finishViewSwitch(requestID)) {
        return;
      }
      deps.restoreLoadedRuntimeComposerDraft(loadedState);
      deps.setAppState((current) =>
        withLoadedRuntimeSessionTab(
          persistActiveSessionTabDraft(current, outgoingDraft),
          loadedState,
        ),
      );
      void offerRemovedProjectStateCleanup(removedProject);
    } catch (error) {
      if (!deps.finishViewSwitch(requestID)) {
        return;
      }
      setStatus(error instanceof Error ? error.message : "remove workspace failed");
    }
  }

  async function relocateProject(projectId: string): Promise<void> {
    const currentState = deps.getAppState();
    const requestID = deps.beginViewSwitch("runtime", "relocate-project");
    const outgoingDraft = deps.getPrimaryComposerDraft();
    const previousCwd = currentState.activeContext?.cwd;
    const wasActive = currentState.activeProjectId === projectId;
    try {
      const projectState = await window.wuu.relocateProject(projectId);
      const newCwd = projectState.active_context?.cwd;
      if (!wasActive || newCwd === previousCwd) {
        if (!deps.finishViewSwitch(requestID)) {
          return;
        }
        deps.setAppState((current) => ({
          ...current,
          projects: projectState.projects,
        }));
        return;
      }
      const loadedState = await loadRuntime(projectState);
      if (!deps.finishViewSwitch(requestID)) {
        return;
      }
      deps.restoreLoadedRuntimeComposerDraft(loadedState);
      deps.setAppState((current) =>
        withLoadedRuntimeSessionTab(
          persistActiveSessionTabDraft(current, outgoingDraft),
          loadedState,
        ),
      );
    } catch (error) {
      if (!deps.finishViewSwitch(requestID)) {
        return;
      }
      setStatus(
        error instanceof Error ? error.message : "relocate workspace failed",
      );
    }
  }

  async function useNoProject(fresh: boolean): Promise<void> {
    const currentState = deps.getAppState();
    if (!fresh && currentState.activeContext?.kind === "no_project") {
      deps.closeProjectMenus();
      return;
    }
    const requestID = deps.beginViewSwitch(
      "runtime",
      fresh ? "no-project:fresh" : "no-project",
    );
    deps.closeProjectMenus();
    const outgoingDraft = deps.getPrimaryComposerDraft();
    const carryDraft =
      activeSessionTab(currentState)?.kind === "draft" &&
      composerDraftHasContent(outgoingDraft)
        ? outgoingDraft
        : undefined;
    try {
      const projectState = await window.wuu.selectNoProject(fresh);
      const loadedState = await loadRuntime(projectState);
      if (!deps.finishViewSwitch(requestID)) {
        return;
      }
      deps.restoreLoadedRuntimeComposerDraft(loadedState, carryDraft);
      deps.setAppState((current) =>
        applyLoadedRuntimeWithDraftCarry(current, loadedState, outgoingDraft),
      );
    } catch (error) {
      if (!deps.finishViewSwitch(requestID)) {
        return;
      }
      setStatus(error instanceof Error ? error.message : "open no-project failed");
    }
  }

  return {
    openProject,
    selectProjectForNewThread,
    startNewThreadForProject,
    createBlankProject,
    chooseProjectFolder,
    removeProject,
    relocateProject,
    useNoProject,
  };
}
