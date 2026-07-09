import type { MutableRefObject, SetStateAction } from "react";
import type { GitCommitResult, GitPullRequestResult } from "../shared/protocol";
import { sameRuntimeContext, type AppState } from "./AppState";
import type { EnvironmentPanelMenu } from "./EnvironmentPanel";

type SetAppState = (update: SetStateAction<AppState>) => void;

export type EnvironmentActionsDeps = {
  getAppState: () => AppState;
  setAppState: SetAppState;
  getAnyThreadIsRunning: () => boolean;
  closeProjectMenus: () => void;
  setEnvironmentPanelOpen: (open: boolean) => void;
  setEnvironmentPanelDismissed: (dismissed: boolean) => void;
  setEnvironmentPanelMenu: (menu: EnvironmentPanelMenu) => void;
  closeRuntimeMenus: () => void;
  getEnvironmentPanelVisible: () => boolean;
  environmentPanelContainsActiveElement: () => boolean;
  focusEnvironmentToggle: () => void;
  gitRefreshTimerRef: MutableRefObject<number | undefined>;
  gitRefreshInFlightRef: MutableRefObject<boolean>;
  gitRefreshQueuedRef: MutableRefObject<boolean>;
};

export type EnvironmentActions = {
  checkoutBranch: (branch: string) => Promise<void>;
  refreshGitStatus: () => Promise<void>;
  scheduleGitStatusRefresh: (delayMs: number) => void;
  createAndCheckoutBranch: (branch: string) => Promise<void>;
  commitEnvironmentChanges: (params: {
    message: string;
    includeUnstaged: boolean;
  }) => Promise<GitCommitResult>;
  createEnvironmentPullRequest: (params: {
    title: string;
    body: string;
    draft: boolean;
  }) => Promise<GitPullRequestResult>;
  toggleEnvironmentPanel: () => void;
  openEnvironmentPanel: () => void;
  closeEnvironmentPanel: (options?: { dismissed?: boolean }) => void;
};

export function createEnvironmentActions(
  deps: EnvironmentActionsDeps,
): EnvironmentActions {
  function setStatus(status: string): void {
    deps.setAppState((current) => ({
      ...current,
      status,
    }));
  }

  async function checkoutBranch(branch: string): Promise<void> {
    if (!branch || deps.getAnyThreadIsRunning()) {
      return;
    }
    deps.closeProjectMenus();
    try {
      const gitStatus = await window.wuu.checkoutGitBranch(branch);
      deps.setAppState((current) => ({
        ...current,
        gitStatus,
        status: current.status === "ready" ? "ready" : current.status,
      }));
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "checkout branch failed");
    }
  }

  async function refreshGitStatus(): Promise<void> {
    const context = deps.getAppState().activeContext;
    if (!context) {
      return;
    }
    if (deps.gitRefreshInFlightRef.current) {
      deps.gitRefreshQueuedRef.current = true;
      return;
    }
    deps.gitRefreshInFlightRef.current = true;
    try {
      const gitStatus = await window.wuu.gitStatus();
      if (!sameRuntimeContext(deps.getAppState().activeContext, context)) {
        return;
      }
      deps.setAppState((current) => ({
        ...current,
        gitStatus,
        status: current.status === "ready" ? "ready" : current.status,
      }));
    } catch (error) {
      if (!sameRuntimeContext(deps.getAppState().activeContext, context)) {
        return;
      }
      setStatus(
        error instanceof Error ? error.message : "refresh git status failed",
      );
    } finally {
      deps.gitRefreshInFlightRef.current = false;
      if (deps.gitRefreshQueuedRef.current) {
        deps.gitRefreshQueuedRef.current = false;
        scheduleGitStatusRefresh(150);
      }
    }
  }

  function scheduleGitStatusRefresh(delayMs: number): void {
    if (!deps.getAppState().activeContext) {
      return;
    }
    if (deps.gitRefreshTimerRef.current !== undefined) {
      window.clearTimeout(deps.gitRefreshTimerRef.current);
    }
    deps.gitRefreshTimerRef.current = window.setTimeout(() => {
      deps.gitRefreshTimerRef.current = undefined;
      void refreshGitStatus();
    }, delayMs);
  }

  async function createAndCheckoutBranch(branch: string): Promise<void> {
    if (!branch || deps.getAnyThreadIsRunning()) {
      return;
    }
    try {
      const result = await window.wuu.createCheckoutGitBranch(branch);
      deps.setAppState((current) => ({
        ...current,
        gitStatus: result.status,
        status: current.status === "ready" ? "ready" : current.status,
      }));
      deps.setEnvironmentPanelMenu(null);
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "create branch failed");
      throw error;
    }
  }

  async function commitEnvironmentChanges(params: {
    message: string;
    includeUnstaged: boolean;
  }): Promise<GitCommitResult> {
    const result = await window.wuu.commitGitChanges({
      message: params.message,
      include_unstaged: params.includeUnstaged,
    });
    deps.setAppState((current) => ({
      ...current,
      gitStatus: result.status,
      status: `已提交 ${result.commit}`,
    }));
    return result;
  }

  async function createEnvironmentPullRequest(params: {
    title: string;
    body: string;
    draft: boolean;
  }): Promise<GitPullRequestResult> {
    const result = await window.wuu.createPullRequest({
      title: params.title,
      body: params.body,
      draft: params.draft,
    });
    deps.setAppState((current) => ({
      ...current,
      gitStatus: result.status,
      status: result.already_exists ? "已有拉取请求" : "已创建拉取请求",
    }));
    return result;
  }

  function toggleEnvironmentPanel(): void {
    if (deps.getEnvironmentPanelVisible()) {
      closeEnvironmentPanel({ dismissed: true });
      return;
    }
    openEnvironmentPanel();
  }

  function openEnvironmentPanel(): void {
    deps.setEnvironmentPanelOpen(true);
    deps.setEnvironmentPanelDismissed(false);
    deps.closeRuntimeMenus();
  }

  function closeEnvironmentPanel({
    dismissed = false,
  }: { dismissed?: boolean } = {}): void {
    restoreEnvironmentPanelFocus();
    deps.setEnvironmentPanelOpen(false);
    if (dismissed) {
      deps.setEnvironmentPanelDismissed(true);
    }
    deps.setEnvironmentPanelMenu(null);
  }

  function restoreEnvironmentPanelFocus(): void {
    if (deps.environmentPanelContainsActiveElement()) {
      deps.focusEnvironmentToggle();
    }
  }

  return {
    checkoutBranch,
    refreshGitStatus,
    scheduleGitStatusRefresh,
    createAndCheckoutBranch,
    commitEnvironmentChanges,
    createEnvironmentPullRequest,
    toggleEnvironmentPanel,
    openEnvironmentPanel,
    closeEnvironmentPanel,
  };
}
