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
  Save,
  Terminal,
  type LucideIcon
} from "lucide-react";
import { type CSSProperties, type RefObject, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import type {
  RuntimeContext,
  WorkspaceDirectoryListResult,
  WorkspaceFileReadResult,
  WorkspaceFileTreeEntry
} from "../shared/protocol";
import { RichContent } from "./RichContent";
import {
  WorkspaceMonacoEditor,
  type WorkspaceMonacoViewState,
} from "./WorkspaceMonacoEditor";
import { desktopApiErrorMessage } from "./WorkspaceReviewHelpers";

type DirectoryLoadState = {
  entries?: WorkspaceFileTreeEntry[];
  loading?: boolean;
  error?: string;
  root?: string;
  truncated?: boolean;
};

type DirectoryLoadMap = Record<string, DirectoryLoadState>;
type MarkdownMode = "reading" | "source";

export type WorkspaceFileDirtyState = {
  root?: string;
  path?: string;
  dirty: boolean;
};

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
  const [contextMenu, setContextMenu] = useState<{
    x: number;
    y: number;
    entry: WorkspaceFileTreeEntry;
  } | null>(null);
  const directoriesRef = useRef(directories);
  const workspaceRootRef = useRef<string | undefined>(undefined);
  const workspaceRoot = activeContext?.cwd;
  const selectedWorkspaceFilePath = useMemo(
    () => normalizeSelectedWorkspaceFilePath(selectedFilePath, workspaceRoot),
    [selectedFilePath, workspaceRoot]
  );

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
      .listWorkspaceDirectory(directoryPath, expectedWorkspaceRoot)
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
      setContextMenu(null);
      return;
    }

    setDirectories({});
    setExpandedPaths(new Set());
    setSearch("");
    setContextMenu(null);
    loadDirectory("");
  }, [loadDirectory, open, workspaceRoot]);

  useEffect(() => {
    if (!open || !workspaceRoot || !selectedWorkspaceFilePath) {
      return;
    }

    const parentPaths = parentDirectoryPathsForFile(selectedWorkspaceFilePath);
    setSearch("");
    if (parentPaths.length === 0) {
      return;
    }

    setExpandedPaths((current) => {
      let changed = false;
      const next = new Set(current);
      for (const path of parentPaths) {
        if (!next.has(path)) {
          changed = true;
          next.add(path);
        }
      }
      return changed ? next : current;
    });

    for (const path of parentPaths) {
      const directoryState = directoriesRef.current[path];
      if (!directoryState?.entries && !directoryState?.loading) {
        loadDirectory(path);
      }
    }
  }, [loadDirectory, open, selectedWorkspaceFilePath, workspaceRoot]);

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

  // Right-click on any row lands here. Suppress the browser's native menu
  // and stash the mouse position + the entry, so the menu component below
  // can position itself at the cursor.
  const handleContextMenu = useCallback(
    (entry: WorkspaceFileTreeEntry, x: number, y: number) => {
      setContextMenu({ x, y, entry });
    },
    [],
  );
  const closeContextMenu = useCallback(() => {
    setContextMenu(null);
  }, []);

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
        selectedFilePath={selectedWorkspaceFilePath}
        search={search}
        onSearchChange={setSearch}
        onToggleDirectory={toggleDirectory}
        onOpenFile={onOpenFile}
        onContextMenu={handleContextMenu}
        onRetryDirectory={loadDirectory}
      />
      {contextMenu ? (
        <WorkspaceTreeContextMenu
          entry={contextMenu.entry}
          workspaceRoot={rootDirectory.root ?? workspaceRoot ?? ""}
          x={contextMenu.x}
          y={contextMenu.y}
          onClose={closeContextMenu}
        />
      ) : null}
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
  onContextMenu,
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
  onContextMenu: (entry: WorkspaceFileTreeEntry, x: number, y: number) => void;
  onRetryDirectory: (path: string) => void;
}): JSX.Element {
  const query = search.trim().toLowerCase();
  const visibleEntries = entries.filter((entry) => shouldShowFileTreeEntry(entry, query, directories));
  const selectedRowRef = useRef<HTMLButtonElement | null>(null);

  useEffect(() => {
    const row = selectedRowRef.current;
    if (!selectedFilePath || !row) {
      return;
    }
    row.scrollIntoView({ block: "nearest", inline: "nearest" });
  }, [directories, expandedPaths, query, selectedFilePath]);

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
              selectedRowRef={selectedRowRef}
              depth={0}
              onToggleDirectory={onToggleDirectory}
              onOpenFile={onOpenFile}
              onContextMenu={onContextMenu}
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
  selectedRowRef,
  depth,
  onToggleDirectory,
  onOpenFile,
  onContextMenu,
  onRetryDirectory
}: {
  directories: DirectoryLoadMap;
  entries: WorkspaceFileTreeEntry[];
  expandedPaths: Set<string>;
  selectedFilePath?: string;
  query: string;
  selectedRowRef: RefObject<HTMLButtonElement | null>;
  depth: number;
  onToggleDirectory: (path: string) => void;
  onOpenFile: (path: string) => void;
  onContextMenu: (entry: WorkspaceFileTreeEntry, x: number, y: number) => void;
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
          selectedRowRef={selectedRowRef}
          depth={depth}
          onToggleDirectory={onToggleDirectory}
          onOpenFile={onOpenFile}
          onContextMenu={onContextMenu}
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
  selectedRowRef,
  depth,
  onToggleDirectory,
  onOpenFile,
  onContextMenu,
  onRetryDirectory
}: {
  directories: DirectoryLoadMap;
  entry: WorkspaceFileTreeEntry;
  expandedPaths: Set<string>;
  selectedFilePath?: string;
  query: string;
  selectedRowRef: RefObject<HTMLButtonElement | null>;
  depth: number;
  onToggleDirectory: (path: string) => void;
  onOpenFile: (path: string) => void;
  onContextMenu: (entry: WorkspaceFileTreeEntry, x: number, y: number) => void;
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
    const selected = selectedFilePath === entry.path;
    return (
      <button
        type="button"
        className={`workspace-file-tree-row${selected ? " selected" : ""}${isHidden ? " hidden" : ""}`}
        ref={selected ? selectedRowRef : undefined}
        style={rowStyle}
        role="treeitem"
        title={entry.path}
        onClick={() => onOpenFile(entry.path)}
        onContextMenu={(event) => {
          event.preventDefault();
          onContextMenu(entry, event.clientX, event.clientY);
        }}
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
        onContextMenu={(event) => {
          event.preventDefault();
          onContextMenu(entry, event.clientX, event.clientY);
        }}
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
          selectedRowRef={selectedRowRef}
          depth={depth + 1}
          onToggleDirectory={onToggleDirectory}
          onOpenFile={onOpenFile}
          onContextMenu={onContextMenu}
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

function WorkspaceTreeContextMenu({
  entry,
  workspaceRoot,
  x,
  y,
  onClose
}: {
  entry: WorkspaceFileTreeEntry;
  workspaceRoot: string;
  x: number;
  y: number;
  onClose: () => void;
}): JSX.Element {
  const ref = useRef<HTMLDivElement | null>(null);
  // The menu mounts at the cursor, but until React commits the first
  // paint its own size isn't known — measure on the layout effect that
  // runs just before paint, clamp to viewport so the user never sees
  // it spilling off-screen.
  const [position, setPosition] = useState({ x, y });
  useLayoutEffect(() => {
    const menuElement = ref.current;
    if (!menuElement) {
      return;
    }
    const rect = menuElement.getBoundingClientRect();
    const viewportWidth = window.innerWidth;
    const viewportHeight = window.innerHeight;
    const margin = 8;
    const clampedX = Math.max(
      margin,
      Math.min(viewportWidth - rect.width - margin, x),
    );
    const clampedY = Math.max(
      margin,
      Math.min(viewportHeight - rect.height - margin, y),
    );
    if (clampedX !== x || clampedY !== y) {
      setPosition({ x: clampedX, y: clampedY });
    }
  }, [x, y]);

  // Outside click + Escape dismiss. Listener attach is deferred to a
  // setTimeout(0) so the original `contextmenu` event's pointerdown
  // burst doesn't fire dismiss on the same gesture that opened us.
  // Only left clicks dismiss — right clicks land on tree rows whose
  // own `onContextMenu` handler re-opens / re-anchors the menu at the
  // new cursor position; dismissing on right-click would race with
  // that re-open and kill the menu instead of repositioning it.
  useEffect(() => {
    const handlePointerDown = (event: PointerEvent) => {
      if (event.button !== 0) {
        return;
      }
      const menuElement = ref.current;
      if (!menuElement) {
        return;
      }
      if (event.target instanceof Node && menuElement.contains(event.target)) {
        return;
      }
      onClose();
    };
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        onClose();
      }
    };
    let active = true;
    const id = window.setTimeout(() => {
      if (!active) {
        return;
      }
      document.addEventListener("pointerdown", handlePointerDown);
      document.addEventListener("keydown", handleKeyDown);
    }, 0);
    return () => {
      active = false;
      window.clearTimeout(id);
      document.removeEventListener("pointerdown", handlePointerDown);
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, [onClose]);

  // `entry.path` is absolute — it comes from the same main-process dir
  // listing the tree itself renders against. Strip the workspace-root
  // prefix so users get a repo-relative path for tools that take one
  // (CLI flags, CI scripts, commit messages).
  const absolutePath = entry.path;
  const relativePath = useMemo(() => {
    const normalizedRoot = workspaceRoot.replace(/\\/g, "/").replace(/\/$/, "");
    const normalizedPath = absolutePath.replace(/\\/g, "/");
    return normalizedPath.startsWith(`${normalizedRoot}/`)
      ? normalizedPath.slice(normalizedRoot.length + 1)
      : absolutePath;
  }, [absolutePath, workspaceRoot]);

  // Silent failures only — the user already saw the menu close, and a
  // notification toast for a clipboard / reveal failure would be louder
  // than the action they tried. Both outcomes dismiss the menu.
  const copyToClipboard = (value: string) => () => {
    void navigator.clipboard.writeText(value).then(
      () => onClose(),
      () => onClose(),
    );
  };
  const revealInFolder = () => {
    void window.wuu.revealWorkspaceItem(absolutePath).then(
      () => onClose(),
      () => onClose(),
    );
  };

  return (
    <div
      ref={ref}
      className="workspace-tree-context-menu"
      role="menu"
      style={{ left: position.x, top: position.y }}
      onContextMenu={(event) => event.preventDefault()}
    >
      <button
        type="button"
        role="menuitem"
        className="workspace-tree-context-menu-item"
        onClick={copyToClipboard(absolutePath)}
      >
        复制路径
      </button>
      <button
        type="button"
        role="menuitem"
        className="workspace-tree-context-menu-item"
        onClick={copyToClipboard(relativePath)}
      >
        复制相对路径
      </button>
      <button
        type="button"
        role="menuitem"
        className="workspace-tree-context-menu-item"
        onClick={copyToClipboard(entry.name)}
      >
        复制文件名
      </button>
      <button
        type="button"
        role="menuitem"
        className="workspace-tree-context-menu-item"
        onClick={revealInFolder}
      >
        在文件管理器中显示
      </button>
    </div>
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

function normalizeSelectedWorkspaceFilePath(path: string | undefined, workspaceRoot: string | undefined): string | undefined {
  if (!path || !workspaceRoot) {
    return undefined;
  }
  const normalizedPath = normalizePathSeparators(path).replace(/\/+$/, "");
  const normalizedRoot = normalizePathSeparators(workspaceRoot).replace(/\/+$/, "");
  const relativePath = normalizedPath.startsWith(`${normalizedRoot}/`)
    ? normalizedPath.slice(normalizedRoot.length + 1)
    : normalizedPath.startsWith("/")
      ? undefined
      : normalizedPath;
  if (!relativePath) {
    return undefined;
  }
  return normalizeWorkspaceRelativeFilePath(relativePath);
}

function normalizeWorkspaceRelativeFilePath(path: string): string | undefined {
  const value = normalizePathSeparators(path)
    .trim()
    .replace(/^\.\/+/, "")
    .replace(/^\/+/, "")
    .replace(/\/+$/, "");
  if (!value || value.includes("\0") || value.split("/").some((segment) => !segment || segment === "..")) {
    return undefined;
  }
  return value;
}

function parentDirectoryPathsForFile(path: string): string[] {
  const segments = path.split("/").filter(Boolean);
  if (segments.length <= 1) {
    return [];
  }
  const parentPaths: string[] = [];
  for (let index = 1; index < segments.length; index += 1) {
    parentPaths.push(segments.slice(0, index).join("/"));
  }
  return parentPaths;
}

function normalizePathSeparators(path: string): string {
  return path.trim().replace(/\\/g, "/");
}

function normalizeDirectoryPath(path: string): string {
  return normalizePathSeparators(path).replace(/^\/+/, "").replace(/\/+$/, "");
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
  active = true,
  activeContext,
  editorResourceID,
  selectedFilePath,
  onOpenRightPanel,
  onDirtyChange
}: {
  active?: boolean;
  activeContext?: RuntimeContext;
  editorResourceID?: string;
  selectedFilePath?: string;
  onOpenRightPanel: () => void;
  onDirtyChange?: (state: WorkspaceFileDirtyState) => void;
}): JSX.Element {
  const [file, setFile] = useState<WorkspaceFileReadResult | undefined>(undefined);
  const [draftText, setDraftText] = useState("");
  const [baseText, setBaseText] = useState("");
  const [baseMtimeMs, setBaseMtimeMs] = useState<number | undefined>(undefined);
  const [baseSha256, setBaseSha256] = useState("");
  const [saving, setSaving] = useState(false);
  const [saveStatus, setSaveStatus] = useState<"idle" | "saved" | "conflict" | "error">("idle");
  const [saveError, setSaveError] = useState<string | undefined>(undefined);
  const [markdownMode, setMarkdownMode] = useState<MarkdownMode>("reading");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);
  const editorViewStateRef = useRef<WorkspaceMonacoViewState | null>(null);
  const editRevisionRef = useRef(0);
  const selectedWorkspaceFilePath = useMemo(
    () => normalizeSelectedWorkspaceFilePath(selectedFilePath, activeContext?.cwd),
    [activeContext?.cwd, selectedFilePath]
  );

  useEffect(() => {
    if (!selectedWorkspaceFilePath) {
      setFile(undefined);
      setDraftText("");
      setBaseText("");
      setBaseMtimeMs(undefined);
      setBaseSha256("");
      setSaving(false);
      setSaveStatus("idle");
      setSaveError(undefined);
      setMarkdownMode("reading");
      setError(undefined);
      setLoading(false);
      return;
    }

    let cancelled = false;
    editorViewStateRef.current = null;
    setFile(undefined);
    setDraftText("");
    setBaseText("");
    setBaseMtimeMs(undefined);
    setBaseSha256("");
    setSaving(false);
    setSaveStatus("idle");
    setSaveError(undefined);
    setMarkdownMode("reading");
    setLoading(true);
    setError(undefined);
    void window.wuu
      .readWorkspaceFile(selectedWorkspaceFilePath, activeContext?.cwd)
      .then((result) => {
        if (!cancelled) {
          setFile(result);
          setDraftText(result.text ?? "");
          setBaseText(result.text ?? "");
          setBaseMtimeMs(result.mtime_ms);
          setBaseSha256(result.sha256);
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
  }, [activeContext?.cwd, selectedWorkspaceFilePath]);

  const isMarkdown = Boolean(file && isMarkdownPath(file.path));
  const readOnly = Boolean(file?.binary || file?.truncated || file?.text === undefined);
  const dirty = draftText !== baseText;
  const dirtyStatePath = file?.path ?? selectedWorkspaceFilePath;

  useEffect(() => {
    onDirtyChange?.({
      root: activeContext?.cwd,
      path: dirtyStatePath,
      dirty,
    });
  }, [activeContext?.cwd, dirty, dirtyStatePath, onDirtyChange]);

  useEffect(() => {
    return () => {
      onDirtyChange?.({
        root: activeContext?.cwd,
        path: dirtyStatePath,
        dirty: false,
      });
    };
  }, [activeContext?.cwd, dirtyStatePath, onDirtyChange]);

  const handleEditorChange = useCallback((nextText: string) => {
    editRevisionRef.current += 1;
    setDraftText(nextText);
    setSaveStatus("idle");
    setSaveError(undefined);
  }, []);

  const handleSave = useCallback(() => {
    if (!file || readOnly || !dirty || saving || baseMtimeMs === undefined || !baseSha256) {
      return;
    }

    const submittedEditRevision = editRevisionRef.current;
    setSaving(true);
    setSaveError(undefined);
    void window.wuu
      .writeWorkspaceFile(
        {
          path: file.path,
          text: draftText,
          base_mtime_ms: baseMtimeMs,
          base_sha256: baseSha256,
        },
        activeContext?.cwd,
      )
      .then((result) => {
        const nextBaseText = result.file.text ?? "";
        setFile(result.file);
        setBaseText(nextBaseText);
        setBaseMtimeMs(result.file.mtime_ms);
        setBaseSha256(result.file.sha256);
        if (result.status === "saved") {
          if (editRevisionRef.current === submittedEditRevision) {
            setDraftText(nextBaseText);
            setSaveStatus("saved");
          } else {
            setSaveStatus("idle");
          }
          setSaveError(undefined);
          return;
        }
        setSaveStatus("conflict");
        setSaveError("文件已在外部修改。请检查当前编辑内容后再次保存。");
      })
      .catch((nextError) => {
        setSaveStatus("error");
        setSaveError(desktopApiErrorMessage(nextError, "保存失败"));
      })
      .finally(() => {
        setSaving(false);
      });
  }, [activeContext?.cwd, baseMtimeMs, baseSha256, dirty, draftText, file, readOnly, saving]);

  if (!activeContext) {
    return (
      <div className="workspace-main-empty">
        <FolderX size={36} />
        <strong>没有项目</strong>
        <span>先打开一个项目，再浏览文件。</span>
      </div>
    );
  }

  if (!selectedWorkspaceFilePath) {
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
        <span>{selectedWorkspaceFilePath}</span>
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
        <span>{selectedWorkspaceFilePath}</span>
      </div>
    );
  }

  if (file.renderable_url) {
    return (
      <article className="workspace-file-preview">
        <div className="workspace-file-editor-toolbar">
          <div className="workspace-file-editor-left">
            <div className="workspace-file-editor-status">
              <span className="workspace-file-editor-dot readonly" aria-hidden="true" />
              <span>只读</span>
            </div>
          </div>
        </div>
        <div className="workspace-file-image-preview">
          <img src={file.renderable_url} alt={file.path} />
        </div>
      </article>
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

  const statusLabel = readOnly
    ? "只读"
    : saving
      ? "保存中"
      : dirty
        ? "已修改"
        : saveStatus === "saved"
          ? "已保存"
          : "未修改";
  const statusTone = readOnly
    ? "readonly"
    : saveStatus === "conflict" || saveStatus === "error"
      ? "error"
      : dirty
        ? "dirty"
        : "clean";
  const editorModeClass = isMarkdown ? `markdown-${markdownMode}` : "code";

  return (
    <article className="workspace-file-preview">
      <div className="workspace-file-editor-toolbar">
        <div className="workspace-file-editor-left">
          <div className="workspace-file-editor-status">
            <span className={`workspace-file-editor-dot ${statusTone}`} aria-hidden="true" />
            <span>{statusLabel}</span>
            {file.truncated ? <span className="workspace-file-editor-hint">文件较大，已截断</span> : null}
            {saveError ? (
              <span className="workspace-file-editor-error" role="alert">
                {saveError}
              </span>
            ) : null}
          </div>
          {isMarkdown ? (
            <div className="workspace-markdown-mode-switch" role="tablist" aria-label="Markdown 模式">
              {(["reading", "source"] as const).map((mode) => (
                <button
                  key={mode}
                  type="button"
                  className={markdownMode === mode ? "selected" : ""}
                  aria-selected={markdownMode === mode}
                  role="tab"
                  onClick={() => setMarkdownMode(mode)}
                >
                  {markdownModeLabel(mode)}
                </button>
              ))}
            </div>
          ) : null}
        </div>
        <button
          type="button"
          className="workspace-file-save-button"
          disabled={readOnly || saving || !dirty}
          onClick={handleSave}
        >
          <Save className="icon" />
          <span>保存</span>
        </button>
      </div>
      <div className={`workspace-file-editor-scroll ${editorModeClass}`}>
        {isMarkdown && markdownMode === "reading" ? (
          <div className="workspace-markdown-reading">
            <RichContent text={draftText} cwd={activeContext.cwd} />
          </div>
        ) : active ? (
          <WorkspaceMonacoEditor
            initialViewState={editorViewStateRef.current}
            path={file.path}
            resourceID={editorResourceID ?? `${activeContext.cwd}:${file.path}`}
            text={draftText}
            readOnly={readOnly}
            onChange={handleEditorChange}
            onSave={handleSave}
            onViewStateChange={(viewState) => {
              editorViewStateRef.current = viewState;
            }}
          />
        ) : null}
      </div>
    </article>
  );
}

function isMarkdownPath(path: string): boolean {
  return /\.mdx?$/i.test(path);
}

function markdownModeLabel(mode: MarkdownMode): string {
  if (mode === "reading") {
    return "阅读";
  }
  return "源码";
}
