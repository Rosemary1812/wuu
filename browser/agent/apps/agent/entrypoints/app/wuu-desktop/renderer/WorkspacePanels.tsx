import { FolderOpen, Plus, ShieldCheck, Terminal, X } from "lucide-react";
import { OverlayScrollbarsComponent } from "overlayscrollbars-react";
import type { GitStatusResult, RuntimeContext } from "../shared/protocol";
import { OVERLAY_SCROLLBAR_OPTIONS } from "./ScrollbarOptions";
import { WorkspaceFilePreview, WorkspaceFileTree } from "./WorkspaceFiles";
import { WorkspaceDiffReview, WorkspaceReviewPanel } from "./WorkspaceReviewPanels";
import { WorkspaceTerminalPanel } from "./WorkspaceTerminalPanel";

export type WorkspacePanelView = "files" | "review" | "terminal";
export type WorkspaceRightPanelView = "tools" | WorkspacePanelView;

export function WorkspaceMainPanel({
  view,
  activeContext,
  gitStatus,
  selectedFilePath,
  onOpenRightPanel
}: {
  view: WorkspacePanelView;
  activeContext?: RuntimeContext;
  gitStatus?: GitStatusResult;
  selectedFilePath?: string;
  onOpenRightPanel: () => void;
}): JSX.Element | null {
  if (view === "files") {
    return (
      <WorkspaceFilePreview
        activeContext={activeContext}
        selectedFilePath={selectedFilePath}
        onOpenRightPanel={onOpenRightPanel}
      />
    );
  }

  if (view === "review") {
    return <WorkspaceDiffReview activeContext={activeContext} gitStatus={gitStatus} />;
  }

  return null;
}

const WORKSPACE_TOOL_ITEMS: Array<{
  id: WorkspacePanelView;
  title: string;
  subtitle: string;
}> = [
  { id: "files", title: "文件", subtitle: "浏览项目文件" },
  { id: "review", title: "审查", subtitle: "查看代码更改" },
  { id: "terminal", title: "终端", subtitle: "运行 shell 命令" }
];

export function WorkspaceRightPanel({
  open,
  present,
  view,
  openTabs,
  activeContext,
  gitStatus,
  selectedFilePath,
  onSelectView,
  onShowTools,
  onCloseTab,
  onOpenFile,
  onClose
}: {
  open: boolean;
  present: boolean;
  view: WorkspaceRightPanelView;
  openTabs: WorkspacePanelView[];
  activeContext?: RuntimeContext;
  gitStatus?: GitStatusResult;
  selectedFilePath?: string;
  onSelectView: (view: WorkspacePanelView) => void;
  onShowTools: () => void;
  onCloseTab: (view: WorkspacePanelView) => void;
  onOpenFile: (path: string) => void;
  onClose: () => void;
}): JSX.Element {
  const detailView = view === "tools" ? undefined : view;

  return (
    <aside
      className={`workspace-right-panel${detailView ? " detail" : " tools"}${detailView === "review" ? " review" : ""}`}
      aria-hidden={!open}
    >
      <div className="workspace-panel-tabbar">
        <div className="workspace-panel-tabs" role="tablist" aria-label="右侧栏工具">
          {openTabs.map((item) => {
            const tool = workspaceToolFor(item);
            const active = item === detailView;
            return (
              <div className={`workspace-tool-tab${active ? " active" : ""}`} key={item}>
                <button
                  className="workspace-tool-tab-main"
                  type="button"
                  role="tab"
                  aria-selected={active}
                  title={tool.title}
                  disabled={!open}
                  onClick={() => onSelectView(item)}
                >
                  <WorkspaceToolIcon view={item} size={16} />
                  <span>{tool.title}</span>
                </button>
                <button
                  className="workspace-tool-tab-close"
                  type="button"
                  aria-label={`关闭${tool.title}`}
                  disabled={!open}
                  onClick={() => onCloseTab(item)}
                >
                  <X size={13} />
                </button>
              </div>
            );
          })}
        </div>
        <button
          className={`icon-button workspace-panel-add${view === "tools" ? " active" : ""}`}
          type="button"
          aria-label="选择工具"
          aria-pressed={view === "tools"}
          disabled={!open}
          onClick={onShowTools}
        >
          <Plus size={19} />
        </button>
        <span className="workspace-panel-tabbar-spacer" />
        <button
          className="icon-button workspace-panel-close"
          type="button"
          aria-label="关闭右侧栏"
          disabled={!open}
          onClick={onClose}
        >
          <X size={17} />
        </button>
      </div>
      {present ? (
        <>
          <div className={`workspace-panel-body${view === "tools" ? " picker" : ""}`}>
            {view === "tools" ? (
              <WorkspaceToolPicker openTabs={openTabs} onSelectTool={onSelectView} />
            ) : view === "files" ? (
              <WorkspaceFileTree
                activeContext={activeContext}
                open={open}
                selectedFilePath={selectedFilePath}
                onOpenFile={onOpenFile}
              />
            ) : view === "review" ? (
              <WorkspaceReviewPanel gitStatus={gitStatus} />
            ) : view === "terminal" ? (
              <WorkspaceTerminalPanel activeContext={activeContext} />
            ) : null}
          </div>
        </>
      ) : null}
    </aside>
  );
}

function WorkspaceToolPicker({
  openTabs,
  onSelectTool
}: {
  openTabs: WorkspacePanelView[];
  onSelectTool: (view: WorkspacePanelView) => void;
}): JSX.Element {
  return (
    <div className="workspace-tool-menu" aria-label="工作区工具">
      {WORKSPACE_TOOL_ITEMS.map((item) => (
        <button
          key={item.id}
          className={`workspace-tool-menu-item${openTabs.includes(item.id) ? " active" : ""}`}
          type="button"
          onClick={() => onSelectTool(item.id)}
        >
          <span className="workspace-tool-menu-icon" aria-hidden="true">
            <WorkspaceToolIcon view={item.id} size={20} />
          </span>
          <span className="workspace-tool-menu-copy">
            <strong>{item.title}</strong>
            <span>{item.subtitle}</span>
          </span>
        </button>
      ))}
    </div>
  );
}

export function WorkspaceBottomPanel({
  open,
  selectedView,
  onSelectTool,
  onClose
}: {
  open: boolean;
  selectedView: WorkspacePanelView;
  onSelectTool: (view: WorkspacePanelView) => void;
  onClose: () => void;
}): JSX.Element {
  return (
    <section className="workspace-bottom-panel" aria-hidden={!open}>
      <div className="workspace-bottom-header">
        <div className="workspace-bottom-title">工具</div>
        <button
          className="icon-button workspace-panel-close"
          type="button"
          aria-label="关闭底部栏"
          disabled={!open}
          onClick={onClose}
        >
          <X size={17} />
        </button>
      </div>
      {open ? (
        <OverlayScrollbarsComponent
          className="workspace-tool-grid"
          aria-label="工作区工具"
          data-overlayscrollbars-initialize
          defer
          options={OVERLAY_SCROLLBAR_OPTIONS}
        >
          {WORKSPACE_TOOL_ITEMS.map((item) => (
            <button
              key={item.id}
              className={`workspace-tool-card${item.id === selectedView ? " active" : ""}`}
              type="button"
              onClick={() => onSelectTool(item.id)}
            >
              <WorkspaceToolIcon view={item.id} size={25} />
              <strong>{item.title}</strong>
              <span>{item.subtitle}</span>
            </button>
          ))}
        </OverlayScrollbarsComponent>
      ) : null}
    </section>
  );
}

export function WorkspaceToolIcon({ view, size }: { view: WorkspacePanelView; size: number }): JSX.Element {
  switch (view) {
    case "files":
      return <FolderOpen size={size} />;
    case "review":
      return <ShieldCheck size={size} />;
    case "terminal":
      return <Terminal size={size} />;
  }
}

function workspaceToolFor(view: WorkspacePanelView): (typeof WORKSPACE_TOOL_ITEMS)[number] {
  return WORKSPACE_TOOL_ITEMS.find((item) => item.id === view) ?? WORKSPACE_TOOL_ITEMS[0];
}

export function workspaceModeTitle(view: WorkspacePanelView): string {
  return view === "files" ? "打开文件" : workspaceToolFor(view).title;
}
