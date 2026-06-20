import {
  AlertCircle,
  ChevronDown,
  ChevronRight,
  Container,
  FileCode,
  FileCog,
  FileJson,
  FileText,
  FileX,
  Folder,
  FolderOpen,
  FolderX,
  ScrollText,
  Terminal,
  type LucideIcon
} from "lucide-react";
import { type CSSProperties, useCallback, useEffect, useRef, useState } from "react";
import type {
  RuntimeContext,
  WorkspaceDirectoryListResult,
  WorkspaceFileReadResult,
  WorkspaceFileTreeEntry
} from "../shared/protocol";
import { desktopApiErrorMessage, formatBytes } from "./WorkspaceReviewHelpers";

type DirectoryLoadState = {
  entries?: WorkspaceFileTreeEntry[];
  loading?: boolean;
  error?: string;
  root?: string;
  truncated?: boolean;
};

type DirectoryLoadMap = Record<string, DirectoryLoadState>;

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
  const [directories, setDirectories] = useState<DirectoryLoadMap>({});
  const [expandedPaths, setExpandedPaths] = useState<Set<string>>(new Set());
  const [search, setSearch] = useState("");
  const directoriesRef = useRef(directories);
  const workspaceRootRef = useRef<string | undefined>(undefined);
  const workspaceRoot = activeContext?.cwd;

  useEffect(() => {
    directoriesRef.current = directories;
  }, [directories]);

  const loadDirectory = useCallback((path: string) => {
    const directoryPath = normalizeDirectoryPath(path);
    const expectedWorkspaceRoot = workspaceRootRef.current;
    setDirectories((current) => ({
      ...current,
      [directoryPath]: {
        ...current[directoryPath],
        loading: true,
        error: undefined
      }
    }));

    void window.wuu
      .listWorkspaceDirectory(directoryPath)
      .then((result: WorkspaceDirectoryListResult) => {
        if (workspaceRootRef.current !== expectedWorkspaceRoot) {
          return;
        }
        const resultPath = normalizeDirectoryPath(result.path);
        setDirectories((current) => ({
          ...current,
          [resultPath]: {
            entries: result.entries,
            loading: false,
            root: result.root,
            truncated: result.truncated
          }
        }));
      })
      .catch((nextError) => {
        if (workspaceRootRef.current !== expectedWorkspaceRoot) {
          return;
        }
        setDirectories((current) => ({
          ...current,
          [directoryPath]: {
            ...current[directoryPath],
            loading: false,
            error: desktopApiErrorMessage(nextError, "读取文件失败")
          }
        }));
      });
  }, []);

  useEffect(() => {
    workspaceRootRef.current = open ? workspaceRoot : undefined;
    if (!open || !workspaceRoot) {
      setDirectories({});
      setExpandedPaths(new Set());
      setSearch("");
      return;
    }

    setDirectories({});
    setExpandedPaths(new Set());
    setSearch("");
    loadDirectory("");
  }, [loadDirectory, open, workspaceRoot]);

  const toggleDirectory = useCallback(
    (path: string) => {
      const directoryPath = normalizeDirectoryPath(path);
      setExpandedPaths((current) => {
        const next = new Set(current);
        if (next.has(directoryPath)) {
          next.delete(directoryPath);
          return next;
        }
        next.add(directoryPath);
        return next;
      });

      const directoryState = directoriesRef.current[directoryPath];
      if (!directoryState?.entries && !directoryState?.loading) {
        loadDirectory(directoryPath);
      }
    },
    [loadDirectory]
  );

  if (!workspaceRoot) {
    return <WorkspacePanelEmpty title="没有项目" description="先选择一个项目。这个面板会显示它的文件。" />;
  }

  const rootDirectory = directories[""] ?? {};

  if (!rootDirectory.entries && !rootDirectory.error) {
    return <WorkspacePanelEmpty title="正在读取文件" description="文件树马上就绪。" />;
  }

  if (rootDirectory.error) {
    return <WorkspacePanelEmpty title="读取失败" description={rootDirectory.error} />;
  }

  if (!rootDirectory.entries || rootDirectory.entries.length === 0) {
    return <WorkspacePanelEmpty title="没有文件" description={formatWorkspaceRoot(workspaceRoot)} />;
  }

  return (
    <div className="workspace-file-panel">
      <div className="workspace-file-meta">
        <span>{formatWorkspaceRoot(rootDirectory.root ?? workspaceRoot)}</span>
        {rootDirectory.truncated ? <small>已截断</small> : null}
      </div>
      <WorkspaceFileTreeView
        directories={directories}
        entries={rootDirectory.entries}
        expandedPaths={expandedPaths}
        selectedFilePath={selectedFilePath}
        search={search}
        onSearchChange={setSearch}
        onToggleDirectory={toggleDirectory}
        onOpenFile={onOpenFile}
        onRetryDirectory={loadDirectory}
      />
    </div>
  );
}

function WorkspaceFileTreeView({
  directories,
  entries,
  expandedPaths,
  selectedFilePath,
  search,
  onSearchChange,
  onToggleDirectory,
  onOpenFile,
  onRetryDirectory
}: {
  directories: DirectoryLoadMap;
  entries: WorkspaceFileTreeEntry[];
  expandedPaths: Set<string>;
  selectedFilePath?: string;
  search: string;
  onSearchChange: (value: string) => void;
  onToggleDirectory: (path: string) => void;
  onOpenFile: (path: string) => void;
  onRetryDirectory: (path: string) => void;
}): JSX.Element {
  const query = search.trim().toLowerCase();
  const visibleEntries = entries.filter((entry) => shouldShowFileTreeEntry(entry, query, directories));

  return (
    <div className="workspace-file-tree-frame">
      <input
        className="workspace-file-search"
        value={search}
        onChange={(event) => onSearchChange(event.currentTarget.value)}
        placeholder="搜索文件"
        spellCheck={false}
        aria-label="搜索文件"
      />
      <div className="workspace-file-tree-scroll">
        <div className="workspace-file-tree-list" role="tree">
          {visibleEntries.length > 0 ? (
            <WorkspaceFileTreeRows
              directories={directories}
              entries={visibleEntries}
              expandedPaths={expandedPaths}
              selectedFilePath={selectedFilePath}
              query={query}
              depth={0}
              onToggleDirectory={onToggleDirectory}
              onOpenFile={onOpenFile}
              onRetryDirectory={onRetryDirectory}
            />
          ) : (
            <div className="workspace-file-tree-status">没有匹配文件</div>
          )}
        </div>
      </div>
    </div>
  );
}

function WorkspaceFileTreeRows({
  directories,
  entries,
  expandedPaths,
  selectedFilePath,
  query,
  depth,
  onToggleDirectory,
  onOpenFile,
  onRetryDirectory
}: {
  directories: DirectoryLoadMap;
  entries: WorkspaceFileTreeEntry[];
  expandedPaths: Set<string>;
  selectedFilePath?: string;
  query: string;
  depth: number;
  onToggleDirectory: (path: string) => void;
  onOpenFile: (path: string) => void;
  onRetryDirectory: (path: string) => void;
}): JSX.Element {
  return (
    <>
      {entries.map((entry) => (
        <WorkspaceFileTreeNode
          key={entry.path}
          directories={directories}
          entry={entry}
          expandedPaths={expandedPaths}
          selectedFilePath={selectedFilePath}
          query={query}
          depth={depth}
          onToggleDirectory={onToggleDirectory}
          onOpenFile={onOpenFile}
          onRetryDirectory={onRetryDirectory}
        />
      ))}
    </>
  );
}

function WorkspaceFileTreeNode({
  directories,
  entry,
  expandedPaths,
  selectedFilePath,
  query,
  depth,
  onToggleDirectory,
  onOpenFile,
  onRetryDirectory
}: {
  directories: DirectoryLoadMap;
  entry: WorkspaceFileTreeEntry;
  expandedPaths: Set<string>;
  selectedFilePath?: string;
  query: string;
  depth: number;
  onToggleDirectory: (path: string) => void;
  onOpenFile: (path: string) => void;
  onRetryDirectory: (path: string) => void;
}): JSX.Element | null {
  if (!shouldShowFileTreeEntry(entry, query, directories)) {
    return null;
  }

  const directoryPath = normalizeDirectoryPath(entry.path);
  const directoryState = entry.kind === "directory" ? directories[directoryPath] : undefined;
  const directoryEntries = directoryState?.entries ?? [];
  const visibleChildren = directoryEntries.filter((child) => shouldShowFileTreeEntry(child, query, directories));
  const isExpanded = expandedPaths.has(directoryPath) || Boolean(query && visibleChildren.length > 0);
  const isHidden = entry.name.startsWith(".");
  const rowStyle = { "--workspace-tree-depth": depth } as CSSProperties;

  if (entry.kind === "file") {
    const fileIcon = fileIconFor(entry.name);
    return (
      <button
        type="button"
        className={`workspace-file-tree-row${selectedFilePath === entry.path ? " selected" : ""}${isHidden ? " hidden" : ""}`}
        style={rowStyle}
        role="treeitem"
        title={entry.path}
        onClick={() => onOpenFile(entry.path)}
      >
        <span className="workspace-file-tree-toggle-spacer" />
        <fileIcon.Icon className={`icon workspace-file-tree-icon ${fileIcon.tone}`} />
        <span className="workspace-file-tree-name">{entry.name}</span>
      </button>
    );
  }

  return (
    <>
      <button
        type="button"
        className={`workspace-file-tree-row${isHidden ? " hidden" : ""}`}
        style={rowStyle}
        role="treeitem"
        aria-expanded={isExpanded}
        title={entry.path}
        onClick={() => onToggleDirectory(directoryPath)}
      >
        <span className="workspace-file-tree-toggle">
          {isExpanded ? <ChevronDown className="icon" /> : <ChevronRight className="icon" />}
        </span>
        {isExpanded ? <FolderOpen className="icon" /> : <Folder className="icon" />}
        <span className="workspace-file-tree-name">{entry.name}</span>
      </button>
      {isExpanded && directoryState?.loading ? (
        <div className="workspace-file-tree-status row" style={{ "--workspace-tree-depth": depth + 1 } as CSSProperties}>
          读取中
        </div>
      ) : null}
      {isExpanded && directoryState?.error ? (
        <button
          type="button"
          className="workspace-file-tree-status row action"
          style={{ "--workspace-tree-depth": depth + 1 } as CSSProperties}
          onClick={() => onRetryDirectory(directoryPath)}
        >
          读取失败，点击重试
        </button>
      ) : null}
      {isExpanded && visibleChildren.length > 0 ? (
        <WorkspaceFileTreeRows
          directories={directories}
          entries={visibleChildren}
          expandedPaths={expandedPaths}
          selectedFilePath={selectedFilePath}
          query={query}
          depth={depth + 1}
          onToggleDirectory={onToggleDirectory}
          onOpenFile={onOpenFile}
          onRetryDirectory={onRetryDirectory}
        />
      ) : null}
      {isExpanded && directoryState?.truncated ? (
        <div className="workspace-file-tree-status row" style={{ "--workspace-tree-depth": depth + 1 } as CSSProperties}>
          已截断
        </div>
      ) : null}
    </>
  );
}

function shouldShowFileTreeEntry(entry: WorkspaceFileTreeEntry, query: string, directories: DirectoryLoadMap): boolean {
  if (!query || entry.name.toLowerCase().includes(query) || entry.path.toLowerCase().includes(query)) {
    return true;
  }
  if (entry.kind !== "directory") {
    return false;
  }

  const directory = directories[normalizeDirectoryPath(entry.path)];
  return Boolean(directory?.entries?.some((child) => shouldShowFileTreeEntry(child, query, directories)));
}

function normalizeDirectoryPath(path: string): string {
  return path.trim().replace(/\\/g, "/").replace(/^\/+/, "").replace(/\/+$/, "");
}

type FileIconTone = "code" | "config" | "doc" | "shell" | "data" | "default";

const FILE_ICON_BY_NAME: ReadonlyArray<readonly [RegExp, LucideIcon, FileIconTone]> = [
  [/^LICENSE(\.|$)/i, ScrollText, "doc"],
  [/^Makefile$/i, FileCog, "config"],
  [/^Dockerfile$/i, Container, "config"],
  [/\.(sh|bash|zsh|fish)$/i, Terminal, "shell"],
  [/\.(ts|tsx|mts|cts|js|jsx|mjs|cjs)$/i, FileCode, "code"],
  [/\.go$/i, FileCode, "code"],
  [/\.py$/i, FileCode, "code"],
  [/\.rs$/i, FileCode, "code"],
  [/\.json$/i, FileJson, "data"],
  [/\.(ya?ml|toml|ini|env|properties)$/i, FileCog, "config"],
  [/\.(mod|sum)$/i, FileCog, "config"],
  [/\.mdx?$/i, FileText, "doc"],
  [/\.gitignore$/i, FileX, "config"]
];

function fileIconFor(name: string): { Icon: LucideIcon; tone: FileIconTone } {
  for (const [pattern, Icon, tone] of FILE_ICON_BY_NAME) {
    if (pattern.test(name)) {
      return { Icon, tone };
    }
  }
  return { Icon: FileText, tone: "default" };
}

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
      <div className="workspace-file-code-scroll">
        <pre className="workspace-file-code">
          <code>{file.text}</code>
        </pre>
      </div>
    </article>
  );
}
