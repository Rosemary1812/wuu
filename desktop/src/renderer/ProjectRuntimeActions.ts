import type { SetStateAction } from "react";
import type { DesktopProject, ProjectListResult } from "../shared/protocol";
import {
  activeSessionTab,
  applyLoadedRuntimeWithDraftCarry,
  cloneSessionTabDraft,
  composerDraftHasContent,
  draftSessionTabForContext,
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
  restorePrimaryComposerDraft: (draft: ComposerDraftState) => void;
  clearPrimaryComposerDraft: () => void;
  restoreLoadedRuntimeComposerDraft: (
    loadedState: Partial<AppState>,
    carryDraft?: ComposerDraftState,
  ) => void;
  nextDraftSessionTab: (context: NonNullable<AppState["activeContext"]>) => SessionTab;
  closeProjectMenus: () => void;
  
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

  function activateWorkspaceDraft(
    context: NonNullable<AppState["activeContext"]>,
  ): void {
    const draft = deps.getPrimaryComposerDraft();
    const currentState = deps.getAppState();
    const existingDraft = draftSessionTabForContext(
      currentState.sessionTabs,
      context,
    );
    if (existingDraft) {
      if (existingDraft.id === currentState.activeSessionTabID) {
        return;
      }
      deps.restorePrimaryComposerDraft(cloneSessionTabDraft(existingDraft));
      deps.setAppState((current) => ({
        ...persistActiveSessionTabDraft(current, draft),
        thread: undefined,
        secondaryThread: undefined,
        activePane: "primary",
        activeSessionTabID: existingDraft.id,
        allowThreadAutoActivation: false,
        running: false,
        status: "ready",
      }));
      return;
    }
    const nextTab = deps.nextDraftSessionTab(context);
    deps.clearPrimaryComposerDraft();
    deps.setAppState((current) => {
      const withDraft = persistActiveSessionTabDraft(current, draft);
      return {
        ...withDraft,
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
  }

  async function openProject(projectId: string): Promise<void> {
    const currentState = deps.getAppState();
    if (
      projectId === currentState.activeProjectId &&
      currentState.activeContext?.kind === "project"
    ) {
      deps.closeProjectMenus();
      
      if (currentState.thread || currentState.secondaryThread) {
        activateWorkspaceDraft(currentState.activeContext);
      }
      return;
    }
    const requestID = deps.beginViewSwitch("project", projectId);
    deps.closeProjectMenus();
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

  /**
   * The composer's project picker has one mental model regardless of the
   * destination: "retarget the conversation I'm drafting at that
   * context". Selecting a project and selecting 不使用项目 are the same
   * gesture, so they share this implementation:
   *
   *   - re-selecting the current context returns to its draft page when
   *     a conversation is on screen (and is otherwise a no-op);
   *   - the switch lands on the destination context's draft tab — it
   *     never resumes an existing conversation, because the user is
   *     holding a draft, not asking to reopen history;
   *   - an in-progress draft travels with the user (see
   *     applyLoadedRuntimeWithDraftCarry).
   */
  async function retargetDraftToContext({
    switchKind,
    switchTarget,
    isCurrentContext,
    selectContext,
    failureStatus,
  }: {
    switchKind: "project" | "runtime";
    switchTarget: string;
    isCurrentContext: (state: AppState) => boolean;
    selectContext: () => Promise<ProjectListResult>;
    failureStatus: string;
  }): Promise<void> {
    const currentState = deps.getAppState();
    if (isCurrentContext(currentState)) {
      deps.closeProjectMenus();
      const context = currentState.activeContext;
      if (context && (currentState.thread || currentState.secondaryThread)) {
        activateWorkspaceDraft(context);
      }
      return;
    }
    if (isAnyThreadRunning(currentState)) {
      deps.closeProjectMenus();
      setStatus("任务运行中，暂不能切换项目");
      return;
    }
    const requestID = deps.beginViewSwitch(switchKind, switchTarget);
    deps.closeProjectMenus();

    const outgoingDraft = deps.getPrimaryComposerDraft();
    const carryDraft =
      activeSessionTab(currentState)?.kind === "draft" &&
      composerDraftHasContent(outgoingDraft)
        ? outgoingDraft
        : undefined;
    try {
      const projectState = await selectContext();
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
      setStatus(error instanceof Error ? error.message : failureStatus);
    }
  }

  async function selectProjectForNewThread(projectId: string): Promise<void> {
    await retargetDraftToContext({
      switchKind: "project",
      switchTarget: projectId,
      isCurrentContext: (state) =>
        projectId === state.activeProjectId &&
        state.activeContext?.kind === "project",
      selectContext: () => window.wuu.selectProject(projectId),
      failureStatus: "open project failed",
    });
  }

  async function startNewThreadForProject(projectId: string): Promise<void> {
    deps.cancelViewSwitch();
    deps.closeProjectMenus();
    
    const currentState = deps.getAppState();
    if (
      projectId === currentState.activeProjectId &&
      currentState.activeContext?.kind === "project"
    ) {
      activateWorkspaceDraft(currentState.activeContext);
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
      if (
        draftSessionTabForContext(
          currentState.sessionTabs,
          loadedState.activeContext,
        )
      ) {
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
    // The non-fresh flavor is the composer picker's 不使用项目 entry —
    // the same "retarget my draft" gesture as picking a project, so it
    // shares that path (land on the 对话 draft page, never resume an
    // old conversation). The fresh flavor below is the sidebar's 新对话
    // button: an explicit "start clean" that discards nothing but also
    // carries nothing.
    if (!fresh) {
      await retargetDraftToContext({
        switchKind: "runtime",
        switchTarget: "no-project",
        isCurrentContext: (state) => state.activeContext?.kind === "no_project",
        selectContext: () => window.wuu.selectNoProject(false),
        failureStatus: "open no-project failed",
      });
      return;
    }
    const currentState = deps.getAppState();
    const requestID = deps.beginViewSwitch("runtime", "no-project:fresh");
    deps.closeProjectMenus();
    const outgoingDraft = deps.getPrimaryComposerDraft();
    try {
      const projectState = await window.wuu.selectNoProject(true);
      const loadedState = await loadRuntime(projectState, {
        resumeLatestThread: false,
      });
      if (!deps.finishViewSwitch(requestID)) {
        return;
      }
      if (!loadedState.activeContext) {
        return;
      }
      const existingDraft = draftSessionTabForContext(
        currentState.sessionTabs,
        loadedState.activeContext,
      );
      if (existingDraft) {
        deps.restoreLoadedRuntimeComposerDraft(loadedState);
        deps.setAppState((current) =>
          withLoadedRuntimeSessionTab(
            persistActiveSessionTabDraft(current, outgoingDraft),
            loadedState,
          ),
        );
        return;
      }
      const nextTab = deps.nextDraftSessionTab(loadedState.activeContext);
      deps.clearPrimaryComposerDraft();
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
        };
      });
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
