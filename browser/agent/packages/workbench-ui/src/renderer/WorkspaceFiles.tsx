import { preparePresortedFileTreeInput } from "@pierre/trees";
import { FileTree, useFileTree, useFileTreeSelection } from "@pierre/trees/react";
import { AlertCircle, FileText, FolderOpen, FolderX } from "lucide-react";
import { type CSSProperties, memo, useEffect, useMemo, useRef, useState } from "react";
import { OverlayScrollbarsComponent } from "overlayscrollbars-react";
import type { FileTreeListResult, RuntimeContext, WorkspaceFileReadResult } from "../shared/protocol";
import { OVERLAY_SCROLLBAR_OPTIONS } from "./ScrollbarOptions";
import { desktopApiErrorMessage, formatBytes } from "./WorkspaceReviewHelpers";

const WORKSPACE_FILE_TREE_STYLE: CSSProperties = {
  contain: "strict",
  height: "100%",
  minHeight: 0,
  minWidth: 0,
  width: "100%"
};
const WORKSPACE_FILE_TREE_ITEM_HEIGHT = 28;

const WORKSPACE_TREE_CSS = `
  :host {
    --trees-fg-override: #34393d;
    --trees-muted-fg-override: #7a8085;
    --trees-selected-bg-override: #eeeeeb;
    --trees-hover-bg-override: #f4f4f2;
    --trees-border-color-override: transparent;
    font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    font-size: 13px;
    line-height: 1.35;
  }

  button[data-type="item"] {
    border-radius: 7px;
  }
`;

export function WorkspaceFileTree({
  activeContext,
  open,
  selectedFilePath,
  onOpenFile
}: {
  activeContext?: RuntimeContext;
  open: boolean;
  selectedFilePath?: string;
  onOpenFile: (path: string) => void;
}): JSX.Element {
  const [fileTree, setFileTree] = useState<FileTreeListResult | undefined>(undefined);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);
  const workspaceRoot = activeContext?.cwd;

  useEffect(() => {
    if (!open || !workspaceRoot) {
      return;
    }

    let cancelled = false;
    setFileTree(undefined);
    setLoading(true);
    setError(undefined);
    void window.wuu
      .listWorkspaceFiles()
      .then((result) => {
        if (cancelled) {
          return;
        }
        setFileTree(result);
      })
      .catch((nextError) => {
        if (cancelled) {
          return;
        }
        setError(desktopApiErrorMessage(nextError, "读取文件失败"));
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [open, workspaceRoot]);

  if (!workspaceRoot) {
    return <WorkspacePanelEmpty title="没有项目" description="先选择一个项目。这个面板会显示它的文件。" />;
  }

  if (loading && !fileTree) {
    return <WorkspacePanelEmpty title="正在读取文件" description="文件树马上就绪。" />;
  }

  if (error) {
    return <WorkspacePanelEmpty title="读取失败" description={error} />;
  }

  if (!fileTree || fileTree.paths.length === 0) {
    return <WorkspacePanelEmpty title="没有文件" description={formatWorkspaceRoot(workspaceRoot)} />;
  }

  return (
    <div className="workspace-file-panel">
      <div className="workspace-file-meta">
        <span>{formatWorkspaceRoot(fileTree.root)}</span>
        <small>
          {fileTree.paths.length} 项{fileTree.truncated ? "，已截断" : ""}
        </small>
      </div>
      <WorkspaceFileTreeView
        paths={fileTree.paths}
        selectedFilePath={selectedFilePath}
        onOpenFile={onOpenFile}
      />
    </div>
  );
}

const WorkspaceFileTreeView = memo(function WorkspaceFileTreeView({
  paths,
  selectedFilePath,
  onOpenFile
}: {
  paths: string[];
  selectedFilePath?: string;
  onOpenFile: (path: string) => void;
}): JSX.Element {
  const preparedInput = useMemo(() => preparePresortedFileTreeInput(paths), [paths]);
  const { model } = useFileTree({
    flattenEmptyDirectories: false,
    initialExpansion: "closed",
    initialSelectedPaths: selectedFilePath ? [selectedFilePath] : [],
    itemHeight: WORKSPACE_FILE_TREE_ITEM_HEIGHT,
    overscan: 8,
    preparedInput,
    search: true,
    stickyFolders: false,
    unsafeCSS: WORKSPACE_TREE_CSS
  });
  const syncedPathsRef = useRef(paths);
  const selectedPaths = useFileTreeSelection(model);
  const onOpenFileRef = useRef(onOpenFile);

  useEffect(() => {
    onOpenFileRef.current = onOpenFile;
  }, [onOpenFile]);

  useEffect(() => {
    if (paths === syncedPathsRef.current) {
      return;
    }
    model.resetPaths(preparedInput.paths, { preparedInput });
    syncedPathsRef.current = paths;
  }, [model, paths, preparedInput]);

  useEffect(() => {
    const nextPath = selectedPaths[0];
    if (!nextPath || nextPath.endsWith("/")) {
      return;
    }
    onOpenFileRef.current(nextPath);
  }, [selectedPaths]);

  return (
    <div className="workspace-file-tree-frame">
      <FileTree model={model} style={WORKSPACE_FILE_TREE_STYLE} />
    </div>
  );
});

export function WorkspacePanelEmpty({
  title,
  description,
  icon
}: {
  title: string;
  description: string;
  icon?: JSX.Element;
}): JSX.Element {
  return (
    <div className="workspace-panel-empty">
      <div className="workspace-panel-empty-icon">{icon ?? <FolderOpen size={24} />}</div>
      <strong>{title}</strong>
      <span>{description}</span>
    </div>
  );
}

export function formatWorkspaceRoot(root: string): string {
  const segments = root.split(/[\\/]/).filter(Boolean);
  return segments.at(-1) ?? root;
}

export function WorkspaceFilePreview({
  activeContext,
  selectedFilePath,
  onOpenRightPanel
}: {
  activeContext?: RuntimeContext;
  selectedFilePath?: string;
  onOpenRightPanel: () => void;
}): JSX.Element {
  const [file, setFile] = useState<WorkspaceFileReadResult | undefined>(undefined);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);

  useEffect(() => {
    if (!selectedFilePath) {
      setFile(undefined);
      setError(undefined);
      setLoading(false);
      return;
    }

    let cancelled = false;
    setFile(undefined);
    setLoading(true);
    setError(undefined);
    void window.wuu
      .readWorkspaceFile(selectedFilePath)
      .then((result) => {
        if (!cancelled) {
          setFile(result);
        }
      })
      .catch((nextError) => {
        if (!cancelled) {
          setError(desktopApiErrorMessage(nextError, "打开文件失败"));
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [selectedFilePath]);

  if (!activeContext) {
    return (
      <div className="workspace-main-empty">
        <FolderX size={36} />
        <strong>没有项目</strong>
        <span>先打开一个项目，再浏览文件。</span>
      </div>
    );
  }

  if (!selectedFilePath) {
    return (
      <div className="workspace-main-empty">
        <FolderOpen size={38} />
        <strong>打开文件</strong>
        <span>从工作区目录树中选择文件</span>
        <button type="button" onClick={onOpenRightPanel}>
          显示目录树
        </button>
      </div>
    );
  }

  if (loading) {
    return (
      <div className="workspace-main-empty">
        <FileText size={36} />
        <strong>正在打开</strong>
        <span>{selectedFilePath}</span>
      </div>
    );
  }

  if (error) {
    return (
      <div className="workspace-main-empty">
        <AlertCircle size={36} />
        <strong>打开失败</strong>
        <span>{error}</span>
      </div>
    );
  }

  if (!file) {
    return (
      <div className="workspace-main-empty">
        <FileText size={36} />
        <strong>没有内容</strong>
        <span>{selectedFilePath}</span>
      </div>
    );
  }

  if (file.binary) {
    return (
      <div className="workspace-main-empty">
        <FileText size={36} />
        <strong>无法预览</strong>
        <span>{file.path} 是二进制文件。</span>
      </div>
    );
  }

  return (
    <article className="workspace-file-preview">
      <header className="workspace-file-preview-header">
        <div>
          <strong>{file.path}</strong>
          <span>
            {formatBytes(file.size_bytes)}
            {file.truncated ? " · 仅显示前 512 KB" : ""}
          </span>
        </div>
      </header>
      <OverlayScrollbarsComponent
        className="workspace-file-code-scroll"
        data-overlayscrollbars-initialize
        defer
        options={OVERLAY_SCROLLBAR_OPTIONS}
      >
        <pre className="workspace-file-code">
          <code>{file.text}</code>
        </pre>
      </OverlayScrollbarsComponent>
    </article>
  );
}
