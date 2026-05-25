import { Archive, ChevronRight, CornerDownRight, Folder, FolderOpen, MessageSquarePlus, Pin } from "lucide-react";
import { Fragment } from "react";
import type { Agent, DesktopProject, Thread } from "../shared/protocol";
import {
  agentLabel,
  agentNestedLabel,
  agentStatusLabel,
  agentStatusTone,
  agentTooltip,
  sortChildAgents
} from "./ThreadAgents";

function projectThreads(threads: Thread[]): Thread[] {
  return threads.filter((thread) => !thread.pinned);
}

export function ProjectList({
  projects,
  activeID,
  pendingProjectID,
  collapsedProjectIDs,
  collapsingProjectIDs,
  threads,
  activeThreadID,
  pendingThreadID,
  pendingAskThreadIDs,
  archiveConfirmThreadID,
  onSelectProject,
  onToggleProjectCollapsed,
  onStartNewThread,
  onSelectThread,
  onSelectChildAgent,
  onToggleThreadPinned,
  onArchiveThread,
  onClearArchiveConfirm
}: {
  projects: DesktopProject[];
  activeID?: string;
  pendingProjectID?: string;
  collapsedProjectIDs: Set<string>;
  collapsingProjectIDs: Set<string>;
  threads: Thread[];
  activeThreadID?: string;
  pendingThreadID?: string;
  pendingAskThreadIDs: Set<string>;
  archiveConfirmThreadID?: string;
  onSelectProject: (id: string) => void;
  onToggleProjectCollapsed: (id: string) => void;
  onStartNewThread: (id: string) => void;
  onSelectThread: (id: string) => void;
  onSelectChildAgent: (agent: Agent) => void;
  onToggleThreadPinned: (thread: Thread) => void;
  onArchiveThread: (thread: Thread) => void;
  onClearArchiveConfirm: (threadID: string) => void;
}): JSX.Element {
  return (
    <div className="projects">
      {projects.map((project) => {
        const pendingProject = pendingProjectID === project.id;
        const activeProject = project.id === activeID;
        const collapsed = collapsedProjectIDs.has(project.id);
        const collapsing = collapsingProjectIDs.has(project.id);
        const expanded = activeProject && !collapsed && !collapsing;
        const threadListMounted = activeProject && (!collapsed || collapsing);
        const projectRowClassName = `project-row ${activeProject ? "active" : ""}${expanded ? " expanded" : ""}${
          pendingProject ? " pending-switch" : ""
        }`;
        return (
          <div key={project.id} className="project-group">
            <button
              className={projectRowClassName}
              aria-label={activeProject ? `${expanded ? "收起" : "展开"} ${project.name} 的会话` : `打开 ${project.name}`}
              aria-current={activeProject ? "page" : undefined}
              aria-expanded={activeProject ? expanded : undefined}
              aria-busy={pendingProject}
              title={activeProject ? (expanded ? "收起会话" : "展开会话") : "打开项目"}
              onClick={() => (activeProject ? onToggleProjectCollapsed(project.id) : onSelectProject(project.id))}
            >
              <ChevronRight className="project-row-chevron" size={15} aria-hidden="true" />
              {expanded ? <FolderOpen size={18} /> : <Folder size={18} />}
              <span>{project.name}</span>
              {pendingProject ? <span className="project-row-loading" aria-hidden="true" /> : null}
            </button>
            <button
              className="project-row-new-thread"
              type="button"
              aria-label={`在 ${project.name} 中新建会话`}
              title="新建会话"
              onClick={() => onStartNewThread(project.id)}
            >
              <MessageSquarePlus size={15} />
            </button>
            {threadListMounted ? (
              <div className={`thread-list-collapse${collapsing ? " closing" : ""}`} aria-hidden={collapsing || undefined}>
                <ThreadList
                  threads={threads}
                  activeID={activeThreadID}
                  pendingThreadID={pendingThreadID}
                  pendingAskThreadIDs={pendingAskThreadIDs}
                  archiveConfirmThreadID={archiveConfirmThreadID}
                  onSelect={onSelectThread}
                  onSelectChildAgent={onSelectChildAgent}
                  onTogglePinned={onToggleThreadPinned}
                  onArchive={onArchiveThread}
                  onClearArchiveConfirm={onClearArchiveConfirm}
                />
              </div>
            ) : null}
          </div>
        );
      })}
    </div>
  );
}

function ThreadList({
  threads,
  activeID,
  pendingThreadID,
  pendingAskThreadIDs,
  archiveConfirmThreadID,
  onSelect,
  onSelectChildAgent,
  onTogglePinned,
  onArchive,
  onClearArchiveConfirm
}: {
  threads: Thread[];
  activeID?: string;
  pendingThreadID?: string;
  pendingAskThreadIDs: Set<string>;
  archiveConfirmThreadID?: string;
  onSelect: (id: string) => void;
  onSelectChildAgent: (agent: Agent) => void;
  onTogglePinned: (thread: Thread) => void;
  onArchive: (thread: Thread) => void;
  onClearArchiveConfirm: (threadID: string) => void;
}): JSX.Element {
  const visibleThreads = projectThreads(threads);
  return (
    <div className="thread-list">
      <ThreadRows
        threads={visibleThreads}
        activeID={activeID}
        pendingThreadID={pendingThreadID}
        pendingAskThreadIDs={pendingAskThreadIDs}
        archiveConfirmThreadID={archiveConfirmThreadID}
        onSelect={onSelect}
        onSelectChildAgent={onSelectChildAgent}
        onTogglePinned={onTogglePinned}
        onArchive={onArchive}
        onClearArchiveConfirm={onClearArchiveConfirm}
      />
    </div>
  );
}

export function PinnedThreadList({
  threads,
  activeID,
  pendingThreadID,
  pendingAskThreadIDs,
  archiveConfirmThreadID,
  onSelect,
  onSelectChildAgent,
  onTogglePinned,
  onArchive,
  onClearArchiveConfirm
}: {
  threads: Thread[];
  activeID?: string;
  pendingThreadID?: string;
  pendingAskThreadIDs: Set<string>;
  archiveConfirmThreadID?: string;
  onSelect: (id: string) => void;
  onSelectChildAgent: (agent: Agent) => void;
  onTogglePinned: (thread: Thread) => void;
  onArchive: (thread: Thread) => void;
  onClearArchiveConfirm: (threadID: string) => void;
}): JSX.Element {
  return (
    <div className="pinned-thread-list">
      <ThreadRows
        threads={threads}
        activeID={activeID}
        pendingThreadID={pendingThreadID}
        pendingAskThreadIDs={pendingAskThreadIDs}
        archiveConfirmThreadID={archiveConfirmThreadID}
        onSelect={onSelect}
        onSelectChildAgent={onSelectChildAgent}
        onTogglePinned={onTogglePinned}
        onArchive={onArchive}
        onClearArchiveConfirm={onClearArchiveConfirm}
      />
    </div>
  );
}

function ThreadRows({
  threads,
  activeID,
  pendingThreadID,
  pendingAskThreadIDs,
  archiveConfirmThreadID,
  onSelect,
  onSelectChildAgent,
  onTogglePinned,
  onArchive,
  onClearArchiveConfirm
}: {
  threads: Thread[];
  activeID?: string;
  pendingThreadID?: string;
  pendingAskThreadIDs: Set<string>;
  archiveConfirmThreadID?: string;
  onSelect: (id: string) => void;
  onSelectChildAgent: (agent: Agent) => void;
  onTogglePinned: (thread: Thread) => void;
  onArchive: (thread: Thread) => void;
  onClearArchiveConfirm: (threadID: string) => void;
}): JSX.Element {
  return (
    <>
      {threads.map((thread) => {
        const archiveConfirming = archiveConfirmThreadID === thread.id;
        const pendingAsk = pendingAskThreadIDs.has(thread.id);
        const pendingSwitch = pendingThreadID === thread.id;
        return (
          <Fragment key={thread.id}>
            <div
              className={`thread-row ${thread.id === activeID ? "active" : ""}${pendingAsk ? " pending-ask" : ""}${
                pendingSwitch ? " pending-switch" : ""
              }`}
              aria-current={thread.id === activeID ? "page" : undefined}
              onMouseLeave={() => onClearArchiveConfirm(thread.id)}
            >
              <button
                className="thread-row-main"
                type="button"
                aria-busy={pendingSwitch}
                onClick={() => onSelect(thread.id)}
              >
                <span className="thread-row-title">{thread.preview || "未命名对话"}</span>
                {pendingAsk ? (
                  <span className="thread-row-ask-badge" title="需要你选择">
                    <MessageSquarePlus size={12} />
                    <span>需选择</span>
                  </span>
                ) : null}
                {pendingSwitch ? <span className="thread-row-loading" aria-hidden="true" /> : null}
              </button>
              <div className="thread-row-actions" aria-label="对话操作">
                <button
                  className={`thread-row-action ${thread.pinned ? "active" : ""}`}
                  type="button"
                  aria-label={thread.pinned ? "取消置顶" : "置顶"}
                  title={thread.pinned ? "取消置顶" : "置顶"}
                  onClick={() => onTogglePinned(thread)}
                >
                  <Pin size={14} />
                </button>
                <button
                  className={`thread-row-action archive ${archiveConfirming ? "confirm" : ""}`}
                  type="button"
                  aria-label={archiveConfirming ? "确认归档" : "归档"}
                  title={archiveConfirming ? "再次点击归档" : "归档"}
                  onClick={() => onArchive(thread)}
                >
                  <Archive size={14} />
                </button>
              </div>
            </div>
            {thread.child_agents?.length ? (
              <ThreadChildAgentRows
                agents={thread.child_agents}
                activeID={activeID}
                pendingThreadID={pendingThreadID}
                onSelect={onSelectChildAgent}
              />
            ) : null}
          </Fragment>
        );
      })}
    </>
  );
}

function ThreadChildAgentRows({
  agents,
  activeID,
  pendingThreadID,
  onSelect
}: {
  agents: Agent[];
  activeID?: string;
  pendingThreadID?: string;
  onSelect: (agent: Agent) => void;
}): JSX.Element {
  return (
    <div className="thread-child-agent-list" aria-label="子任务">
      {sortChildAgents(agents).map((agent) => {
        const status = agentStatusTone(agent.status);
        const label = agentLabel(agent);
        const nestedLabel = agentNestedLabel(agent);
        const active = activeID === agent.id;
        const pendingSwitch = pendingThreadID === agent.id;
        return (
          <button
            key={agent.id}
            className={`thread-child-agent-row ${status}${active ? " active" : ""}${
              pendingSwitch ? " pending-switch" : ""
            }`}
            type="button"
            aria-current={active ? "page" : undefined}
            aria-busy={pendingSwitch}
            aria-label={`${label}，${agentStatusLabel(agent.status)}`}
            title={agentTooltip(agent)}
            onClick={() => onSelect(agent)}
          >
            <CornerDownRight className="thread-child-agent-branch" size={13} />
            <span className="thread-child-agent-name">{label}</span>
            {nestedLabel ? <span className="thread-child-agent-nested">{nestedLabel}</span> : null}
            {pendingSwitch ? (
              <span className="thread-row-loading thread-child-agent-loading" aria-hidden="true" />
            ) : (
              <span className="thread-child-agent-status">{agentStatusLabel(agent.status)}</span>
            )}
          </button>
        );
      })}
    </div>
  );
}
