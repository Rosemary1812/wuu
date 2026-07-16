import { afterEach, describe, expect, it, vi } from "vitest";
import type { GitStatusResult, RuntimeContext } from "../shared/protocol";
import { initialState, type AppState } from "./AppState";
import { createEnvironmentActions } from "./EnvironmentActions";
import type { EnvironmentPanelMenu } from "./EnvironmentPanel";
import { resolveLocalizedText } from "./i18n";

const originalWuu = (window as unknown as { wuu?: unknown }).wuu;

function restoreWuu(): void {
  if (originalWuu === undefined) {
    delete (window as unknown as { wuu?: unknown }).wuu;
    return;
  }
  Object.defineProperty(window, "wuu", {
    configurable: true,
    value: originalWuu,
  });
}

afterEach(() => {
  vi.useRealTimers();
  restoreWuu();
});

function projectContext(): RuntimeContext {
  return { kind: "project", project_id: "project-1", cwd: "/tmp/project-1" };
}

function gitStatus(branch = "main"): GitStatusResult {
  return { is_repo: true, branch, dirty_count: 0 };
}

function installWuuApi(): {
  checkoutGitBranch: ReturnType<typeof vi.fn>;
  gitStatus: ReturnType<typeof vi.fn>;
  commitGitChanges: ReturnType<typeof vi.fn>;
} {
  const checkoutGitBranch = vi.fn().mockResolvedValue(gitStatus("feature"));
  const gitStatusMock = vi.fn().mockResolvedValue(gitStatus("main"));
  const commitGitChanges = vi.fn().mockResolvedValue({
    commit: "abc123",
    status: gitStatus("main"),
  });
  Object.defineProperty(window, "wuu", {
    configurable: true,
    value: {
      checkoutGitBranch,
      gitStatus: gitStatusMock,
      commitGitChanges,
      createCheckoutGitBranch: vi.fn().mockResolvedValue({
        status: gitStatus("feature"),
      }),
      createPullRequest: vi.fn().mockResolvedValue({
        url: "https://example.test/pr/1",
        already_exists: false,
        status: gitStatus("main"),
      }),
    },
  });
  return { checkoutGitBranch, gitStatus: gitStatusMock, commitGitChanges };
}

function buildActions({
  initial = { ...initialState, activeContext: projectContext(), status: "ready" },
  anyThreadIsRunning = false,
  environmentPanelVisible = false,
  panelContainsFocus = false,
}: {
  initial?: AppState;
  anyThreadIsRunning?: boolean;
  environmentPanelVisible?: boolean;
  panelContainsFocus?: boolean;
} = {}) {
  let appState = initial;
  let activeMenu: EnvironmentPanelMenu = null;
  let panelOpen = false;
  let panelDismissed = false;
  const closeProjectMenus = vi.fn();
  const closeRuntimeMenus = vi.fn();
  const focusEnvironmentToggle = vi.fn();
  const actions = createEnvironmentActions({
    getAppState: () => appState,
    setAppState: (update) => {
      appState = typeof update === "function" ? update(appState) : update;
    },
    getAnyThreadIsRunning: () => anyThreadIsRunning,
    closeProjectMenus,
    setEnvironmentPanelOpen: (open) => {
      panelOpen = open;
    },
    setEnvironmentPanelDismissed: (dismissed) => {
      panelDismissed = dismissed;
    },
    setEnvironmentPanelMenu: (menu) => {
      activeMenu = menu;
    },
    closeRuntimeMenus,
    getEnvironmentPanelVisible: () => environmentPanelVisible,
    environmentPanelContainsActiveElement: () => panelContainsFocus,
    focusEnvironmentToggle,
    gitRefreshTimerRef: { current: undefined },
    gitRefreshInFlightRef: { current: false },
    gitRefreshQueuedRef: { current: false },
  });

  return {
    actions,
    getAppState: () => appState,
    getPanelState: () => ({ activeMenu, panelOpen, panelDismissed }),
    closeProjectMenus,
    closeRuntimeMenus,
    focusEnvironmentToggle,
  };
}

describe("createEnvironmentActions", () => {
  it("checks out a branch and keeps ready status unchanged", async () => {
    const api = installWuuApi();
    const harness = buildActions();

    await harness.actions.checkoutBranch("feature");

    expect(api.checkoutGitBranch).toHaveBeenCalledWith("feature");
    expect(harness.closeProjectMenus).toHaveBeenCalled();
    expect(harness.getAppState().gitStatus?.branch).toBe("feature");
    expect(harness.getAppState().status).toBe("ready");
  });

  it("does not check out a branch while a thread is running", async () => {
    const api = installWuuApi();
    const harness = buildActions({ anyThreadIsRunning: true });

    await harness.actions.checkoutBranch("feature");

    expect(api.checkoutGitBranch).not.toHaveBeenCalled();
    expect(harness.closeProjectMenus).not.toHaveBeenCalled();
  });

  it("updates status after committing environment changes", async () => {
    const api = installWuuApi();
    const harness = buildActions();

    await harness.actions.commitEnvironmentChanges({
      message: "commit message",
      includeUnstaged: true,
    });

    expect(api.commitGitChanges).toHaveBeenCalledWith({
      message: "commit message",
      include_unstaged: true,
    });
    expect(resolveLocalizedText(harness.getAppState().status)).toBe("已提交 abc123");
  });

  it("opens and closes the environment panel with focus restoration", () => {
    installWuuApi();
    const harness = buildActions({
      environmentPanelVisible: true,
      panelContainsFocus: true,
    });

    harness.actions.openEnvironmentPanel();
    expect(harness.getPanelState().panelOpen).toBe(true);
    expect(harness.getPanelState().panelDismissed).toBe(false);
    expect(harness.closeRuntimeMenus).toHaveBeenCalled();

    harness.actions.toggleEnvironmentPanel();
    expect(harness.getPanelState()).toEqual({
      activeMenu: null,
      panelOpen: false,
      panelDismissed: true,
    });
    expect(harness.focusEnvironmentToggle).toHaveBeenCalled();
  });
});
