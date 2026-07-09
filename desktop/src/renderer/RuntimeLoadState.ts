import type {
  PopOutInitResult,
  ProjectListResult,
  RuntimeContext,
} from "../shared/protocol";
import {
  activeProjectID,
  createDraftSessionTab,
  createThreadSessionTab,
  isThreadRunning,
  reconcileResumedThreadTurns,
  requireThread,
  sortThreads,
  upsertThread,
  type AppState,
} from "./AppState";

export async function loadRuntime(
  projectState: ProjectListResult,
  options: { resumeLatestThread?: boolean } = {},
): Promise<Partial<AppState>> {
  if (!projectState.active_context) {
    return emptyRuntimeState(projectState);
  }
  const resumeLatestThread = options.resumeLatestThread ?? true;
  const initialized = await window.wuu.initialize();
  const listed = await window.wuu.listThreads();
  const listedThreads = sortThreads(listed.threads);
  const defaultThread = resumeLatestThread
    ? (listedThreads.find((candidate) => !candidate.pinned) ?? listedThreads[0])
    : undefined;
  const thread = defaultThread
    ? requireThread(
        await window.wuu.resumeThread(defaultThread.id),
        "resume did not return a thread",
      )
    : undefined;
  return {
    initialized,
    projects: projectState.projects,
    activeContext: projectState.active_context,
    activeProjectId: activeProjectID(projectState.active_context),
    gitStatus: undefined,
    thread,
    secondaryThread: undefined,
    activePane: "primary",
    allowThreadAutoActivation: Boolean(thread),
    threads: thread ? upsertThread(listedThreads, thread) : listedThreads,
    running: isThreadRunning(thread),
    status:
      initialized.status === "needs_setup"
        ? (initialized.issues?.[0]?.message ?? "请在设置中配置模型凭据")
        : "ready",
  };
}

export async function loadPopOutRuntime(
  init: PopOutInitResult,
): Promise<Partial<AppState>> {
  if (!init.kind || !init.context) {
    return { status: "no-runtime" };
  }
  if (init.kind === "draft") {
    const [listedProjects, initialized, listed] = await Promise.all([
      window.wuu.listProjects(),
      window.wuu.initialize(),
      window.wuu.listThreads(),
    ]);
    const listedThreads = sortThreads(listed.threads);
    const tab = createDraftSessionTab("draft:pop-out", init.context);
    return {
      initialized,
      projects: listedProjects.projects,
      activeContext: init.context,
      activeProjectId: activeProjectID(init.context),
      gitStatus: undefined,
      thread: undefined,
      secondaryThread: undefined,
      activePane: "primary",
      allowThreadAutoActivation: false,
      sessionTabs: [tab],
      activeSessionTabID: tab.id,
      threads: listedThreads,
      running: false,
      status: "ready",
    };
  }
  if (!init.threadID) {
    return { status: "no-runtime" };
  }
  const [listedProjects, initialized, listed, resumed] = await Promise.all([
    window.wuu.listProjects(),
    window.wuu.initialize(),
    window.wuu.listThreads(),
    window.wuu.resumeThread(init.threadID),
  ]);
  const listedThreads = sortThreads(listed.threads);
  const thread = reconcileResumedThreadTurns(
    requireThread(resumed, "resume did not return a thread"),
    listedThreads.find((item) => item.id === init.threadID),
  );
  const tab = createThreadSessionTab(thread, init.context);
  return {
    initialized,
    projects: listedProjects.projects,
    activeContext: init.context,
    activeProjectId: activeProjectID(init.context),
    gitStatus: undefined,
    thread,
    secondaryThread: undefined,
    activePane: "primary",
    allowThreadAutoActivation: true,
    sessionTabs: [tab],
    activeSessionTabID: tab.id,
    threads: upsertThread(listedThreads, thread),
    running: isThreadRunning(thread),
    status: "ready",
  };
}

export function emptyRuntimeState(
  projectState: ProjectListResult,
): Partial<AppState> {
  return {
    initialized: undefined,
    projects: projectState.projects,
    activeContext: undefined,
    activeProjectId: undefined,
    gitStatus: undefined,
    thread: undefined,
    secondaryThread: undefined,
    activePane: "primary",
    allowThreadAutoActivation: false,
    threads: [],
    running: false,
    status: "no-runtime",
  };
}

export async function selectRuntimeContext(
  context: RuntimeContext,
): Promise<ProjectListResult> {
  if (context.kind === "project") {
    return window.wuu.selectProject(context.project_id);
  }
  return window.wuu.selectNoProject(false, context.cwd);
}
