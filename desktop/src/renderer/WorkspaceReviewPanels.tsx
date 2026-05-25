import {
  AlertCircle,
  Check,
  ChevronRight,
  FileText,
  Folder,
  FolderOpen,
  FolderX,
  GitBranch,
  RefreshCw,
  Search
} from "lucide-react";
import {
  type CSSProperties,
  type KeyboardEvent as ReactKeyboardEvent,
  type PointerEvent as ReactPointerEvent,
  useEffect,
  useMemo,
  useRef,
  useState
} from "react";
import { OverlayScrollbarsComponent } from "overlayscrollbars-react";
import type { GitChangeFile, GitChangesResult, GitFileDiffResult, GitStatusResult, RuntimeContext } from "../shared/protocol";
import { OVERLAY_SCROLLBAR_OPTIONS } from "./ScrollbarOptions";
import { formatWorkspaceRoot } from "./WorkspaceFiles";
import {
  buildGitChangeTree,
  collectGitChangeTreeDirectoryPaths,
  desktopApiErrorMessage,
  desktopApiSupportsGitReview,
  filterGitChangeFiles,
  gitChangeFilePathLabel,
  gitChangeStatusDescription,
  gitChangeStatusLabel,
  gitDiffDisplayLines,
  gitPathAncestors,
  summarizeGitChangeFiles,
  type GitChangeTreeNode
} from "./WorkspaceReviewHelpers";

const WORKSPACE_REVIEW_TREE_DEFAULT_WIDTH = 280;
const WORKSPACE_REVIEW_TREE_MIN_WIDTH = 240;
const WORKSPACE_REVIEW_TREE_MAX_WIDTH = 520;
const WORKSPACE_REVIEW_DIFF_MIN_WIDTH = 140;
const WORKSPACE_REVIEW_TREE_STEP = 24;
const WORKSPACE_REVIEW_TREE_WIDTH_KEY = "wuu.desktop.reviewTreeWidth";

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}

function initialWorkspaceReviewTreeWidth(): number {
  const stored = Number(window.localStorage.getItem(WORKSPACE_REVIEW_TREE_WIDTH_KEY));
  if (!Number.isFinite(stored)) {
    return WORKSPACE_REVIEW_TREE_DEFAULT_WIDTH;
  }
  return clamp(stored, WORKSPACE_REVIEW_TREE_MIN_WIDTH, WORKSPACE_REVIEW_TREE_MAX_WIDTH);
}

function clampWorkspaceReviewTreeWidth(width: number, panelWidth = Number.POSITIVE_INFINITY): number {
  if (!Number.isFinite(panelWidth)) {
    return clamp(width, WORKSPACE_REVIEW_TREE_MIN_WIDTH, WORKSPACE_REVIEW_TREE_MAX_WIDTH);
  }
  const maxForPanel = Math.max(
    WORKSPACE_REVIEW_TREE_MIN_WIDTH,
    Math.min(WORKSPACE_REVIEW_TREE_MAX_WIDTH, panelWidth - WORKSPACE_REVIEW_DIFF_MIN_WIDTH)
  );
  return clamp(width, WORKSPACE_REVIEW_TREE_MIN_WIDTH, maxForPanel);
}

export function WorkspaceReviewPanel({ gitStatus }: { gitStatus?: GitStatusResult }): JSX.Element {
  const panelRef = useRef<HTMLDivElement>(null);
  const splitResizeRef = useRef<{ startX: number; startTreeWidth: number } | null>(null);
  const [changes, setChanges] = useState<GitChangesResult | undefined>(undefined);
  const [selectedPath, setSelectedPath] = useState<string | undefined>(undefined);
  const [fileDiff, setFileDiff] = useState<GitFileDiffResult | undefined>(undefined);
  const [loadingChanges, setLoadingChanges] = useState(false);
  const [loadingDiff, setLoadingDiff] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);
  const [treeQuery, setTreeQuery] = useState("");
  const [expandedPaths, setExpandedPaths] = useState<Set<string>>(() => new Set());
  const [treePaneWidth, setTreePaneWidth] = useState(initialWorkspaceReviewTreeWidth);
  const [resizingSplit, setResizingSplit] = useState(false);
  const files = changes?.files ?? [];
  const filteredFiles = useMemo(() => filterGitChangeFiles(files, treeQuery), [files, treeQuery]);
  const treeNodes = useMemo(() => buildGitChangeTree(filteredFiles), [filteredFiles]);
  const selectedFile = files.find((file) => file.path === selectedPath);
  const panelStyle = {
    "--workspace-review-tree-width": `${treePaneWidth}px`
  } as CSSProperties;

  useEffect(() => {
    let cancelled = false;
    setChanges(undefined);
    setSelectedPath(undefined);
    setFileDiff(undefined);
    if (!desktopApiSupportsGitReview()) {
      setError("审查接口还没被当前窗口加载。请重启桌面端后再试。");
      setLoadingChanges(false);
      return;
    }
    setLoadingChanges(true);
    setError(undefined);
    void window.wuu
      .listGitChanges()
      .then((result) => {
        if (cancelled) {
          return;
        }
        setChanges(result);
        setExpandedPaths(new Set(collectGitChangeTreeDirectoryPaths(buildGitChangeTree(result.files))));
        setSelectedPath((current) => {
          if (current && result.files.some((file) => file.path === current)) {
            return current;
          }
          return result.files[0]?.path;
        });
      })
      .catch((nextError) => {
        if (!cancelled) {
          setError(desktopApiErrorMessage(nextError, "读取变更失败"));
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoadingChanges(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    if (!selectedPath) {
      setFileDiff(undefined);
      setLoadingDiff(false);
      return;
    }
    setExpandedPaths((current) => {
      const next = new Set(current);
      for (const ancestor of gitPathAncestors(selectedPath)) {
        next.add(ancestor);
      }
      return next;
    });
  }, [selectedPath]);

  useEffect(() => {
    if (!selectedPath) {
      return;
    }
    let cancelled = false;
    setFileDiff(undefined);
    if (!desktopApiSupportsGitReview()) {
      setError("审查接口还没被当前窗口加载。请重启桌面端后再试。");
      setLoadingDiff(false);
      return;
    }
    setLoadingDiff(true);
    setError(undefined);
    void window.wuu
      .readGitFileDiff(selectedPath)
      .then((result) => {
        if (!cancelled) {
          setFileDiff(result);
        }
      })
      .catch((nextError) => {
        if (!cancelled) {
          setError(desktopApiErrorMessage(nextError, "读取 diff 失败"));
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoadingDiff(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [selectedPath]);

  useEffect(() => {
    window.localStorage.setItem(WORKSPACE_REVIEW_TREE_WIDTH_KEY, String(treePaneWidth));
  }, [treePaneWidth]);

  useEffect(() => {
    const root = document.documentElement;
    root.classList.toggle("resizing-review-split", resizingSplit);
    if (!resizingSplit) {
      return () => root.classList.remove("resizing-review-split");
    }

    function handlePointerMove(event: PointerEvent): void {
      const session = splitResizeRef.current;
      if (!session) {
        return;
      }
      const panelWidth = panelRef.current?.getBoundingClientRect().width;
      setTreePaneWidth(
        clampWorkspaceReviewTreeWidth(session.startTreeWidth - (event.clientX - session.startX), panelWidth)
      );
    }

    function handlePointerUp(): void {
      splitResizeRef.current = null;
      setResizingSplit(false);
    }

    window.addEventListener("pointermove", handlePointerMove);
    window.addEventListener("pointerup", handlePointerUp);
    window.addEventListener("pointercancel", handlePointerUp);
    return () => {
      root.classList.remove("resizing-review-split");
      window.removeEventListener("pointermove", handlePointerMove);
      window.removeEventListener("pointerup", handlePointerUp);
      window.removeEventListener("pointercancel", handlePointerUp);
    };
  }, [resizingSplit]);

  function toggleTreePath(path: string): void {
    setExpandedPaths((current) => {
      const next = new Set(current);
      if (next.has(path)) {
        next.delete(path);
      } else {
        next.add(path);
      }
      return next;
    });
  }

  function resizeTreePaneBy(delta: number): void {
    const panelWidth = panelRef.current?.getBoundingClientRect().width;
    setTreePaneWidth((current) => clampWorkspaceReviewTreeWidth(current + delta, panelWidth));
  }

  function startReviewSplitResize(event: ReactPointerEvent<HTMLDivElement>): void {
    if (event.button !== 0) {
      return;
    }
    event.preventDefault();
    splitResizeRef.current = {
      startX: event.clientX,
      startTreeWidth: treePaneWidth
    };
    setResizingSplit(true);
  }

  function handleReviewSplitKeyDown(event: ReactKeyboardEvent<HTMLDivElement>): void {
    if (event.key === "ArrowLeft") {
      event.preventDefault();
      resizeTreePaneBy(WORKSPACE_REVIEW_TREE_STEP);
    } else if (event.key === "ArrowRight") {
      event.preventDefault();
      resizeTreePaneBy(-WORKSPACE_REVIEW_TREE_STEP);
    } else if (event.key === "Home") {
      event.preventDefault();
      resizeTreePaneBy(WORKSPACE_REVIEW_TREE_MAX_WIDTH);
    } else if (event.key === "End") {
      event.preventDefault();
      resizeTreePaneBy(-WORKSPACE_REVIEW_TREE_MAX_WIDTH);
    }
  }

  if (loadingChanges && !changes) {
    return (
      <div className="workspace-main-empty">
        <GitBranch size={36} />
        <strong>正在读取变更</strong>
        <span>正在检查当前工作区的本地代码差异。</span>
      </div>
    );
  }

  if (error && !changes) {
    return (
      <div className="workspace-main-empty">
        <AlertCircle size={36} />
        <strong>读取失败</strong>
        <span>{error}</span>
      </div>
    );
  }

  if (changes && !changes.is_repo) {
    return (
      <div className="workspace-main-empty">
        <FolderX size={36} />
        <strong>不是 Git 仓库</strong>
        <span>当前项目没有可查看的 Git 变更。</span>
      </div>
    );
  }

  if (changes && files.length === 0) {
    return (
      <div className="workspace-main-empty">
        <Check size={36} />
        <strong>工作区干净</strong>
        <span>当前没有待审查的代码差异。</span>
      </div>
    );
  }

  return (
    <div
      className={`workspace-review-panel${selectedFile ? " has-diff" : ""}${
        resizingSplit ? " resizing-split" : ""
      }`}
      aria-label="审查变更"
      ref={panelRef}
      style={panelStyle}
    >
      {selectedFile ? (
        <WorkspaceReviewDiffPeekPanel
          file={selectedFile}
          fileDiff={fileDiff}
          loading={loadingDiff}
          error={error}
          branch={gitStatus?.branch}
        />
      ) : null}
      {selectedFile ? (
        <div
          className="workspace-review-resizer"
          role="separator"
          aria-label="调整 diff 和文件树宽度"
          aria-orientation="vertical"
          aria-valuemin={WORKSPACE_REVIEW_TREE_MIN_WIDTH}
          aria-valuemax={WORKSPACE_REVIEW_TREE_MAX_WIDTH}
          aria-valuenow={Math.round(treePaneWidth)}
          tabIndex={0}
          onPointerDown={startReviewSplitResize}
          onKeyDown={handleReviewSplitKeyDown}
        />
      ) : null}
      <div className="workspace-review-tree-pane">
        <GitChangeTreePanel
          files={filteredFiles}
          nodes={treeNodes}
          selectedPath={selectedPath}
          expandedPaths={expandedPaths}
          query={treeQuery}
          onQueryChange={setTreeQuery}
          onSelectFile={setSelectedPath}
          onTogglePath={toggleTreePath}
        />
        {error && !selectedFile ? <div className="workspace-review-overlay error">{error}</div> : null}
      </div>
    </div>
  );
}

function WorkspaceReviewDiffPeekPanel({
  file,
  fileDiff,
  loading,
  error,
  branch
}: {
  file: GitChangeFile;
  fileDiff?: GitFileDiffResult;
  loading: boolean;
  error?: string;
  branch?: string;
}): JSX.Element {
  const diffLines = useMemo(() => (fileDiff?.patch ? gitDiffDisplayLines(fileDiff.patch) : []), [fileDiff?.patch]);
  return (
    <section className="workspace-review-diff-panel workspace-diff-detail" aria-label={`${file.path} 的代码差异`}>
      <div className="workspace-diff-detail-header">
        <div>
          <strong>{gitChangeFilePathLabel(file)}</strong>
          <span>
            {branch ?? "当前分支"} · {gitChangeStatusDescription(file)}
          </span>
        </div>
      </div>
      {error ? <div className="workspace-diff-error">{error}</div> : null}
      {loading ? (
        <div className="workspace-diff-empty">正在读取 diff...</div>
      ) : fileDiff?.binary ? (
        <div className="workspace-diff-empty">这是二进制文件，无法显示文本 diff。</div>
      ) : fileDiff?.patch ? (
        <OverlayScrollbarsComponent
          className="workspace-diff-code-scroll"
          data-overlayscrollbars-initialize
          defer
          options={OVERLAY_SCROLLBAR_OPTIONS}
        >
          <pre className="workspace-diff-code" aria-label={`${fileDiff.path} 的代码差异`}>
            {diffLines.map((line, index) => (
              <span className={`workspace-diff-line ${line.kind}`} key={`${index}:${line.content.slice(0, 24)}`}>
                <span className="workspace-diff-line-number">{line.oldLine ?? ""}</span>
                <span className="workspace-diff-line-number">{line.newLine ?? ""}</span>
                <span className="workspace-diff-line-code">{line.content || " "}</span>
              </span>
            ))}
          </pre>
        </OverlayScrollbarsComponent>
      ) : (
        <div className="workspace-diff-empty">没有可显示的文本 diff。</div>
      )}
      {fileDiff?.truncated ? <div className="workspace-diff-truncated">diff 太大，已截断预览。</div> : null}
    </section>
  );
}

export function WorkspaceDiffReview({
  activeContext,
  gitStatus
}: {
  activeContext?: RuntimeContext;
  gitStatus?: GitStatusResult;
}): JSX.Element {
  const [changes, setChanges] = useState<GitChangesResult | undefined>(undefined);
  const [selectedPath, setSelectedPath] = useState<string | undefined>(undefined);
  const [fileDiff, setFileDiff] = useState<GitFileDiffResult | undefined>(undefined);
  const [loadingChanges, setLoadingChanges] = useState(false);
  const [loadingDiff, setLoadingDiff] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);
  const [refreshVersion, setRefreshVersion] = useState(0);
  const [treeQuery, setTreeQuery] = useState("");
  const [expandedPaths, setExpandedPaths] = useState<Set<string>>(() => new Set());
  const workspaceRoot = activeContext?.cwd;
  const files = changes?.files ?? [];
  const totals = useMemo(() => summarizeGitChangeFiles(files), [files]);
  const filteredFiles = useMemo(() => filterGitChangeFiles(files, treeQuery), [files, treeQuery]);
  const treeNodes = useMemo(() => buildGitChangeTree(filteredFiles), [filteredFiles]);
  const diffLines = useMemo(() => (fileDiff?.patch ? gitDiffDisplayLines(fileDiff.patch) : []), [fileDiff?.patch]);
  const selectedFile = files.find((file) => file.path === selectedPath);
  const branchLabel = gitStatus?.is_repo ? gitStatus.branch ?? "detached" : "非 Git 仓库";
  const upstreamLabel = gitStatus?.upstream;

  useEffect(() => {
    if (!workspaceRoot) {
      setChanges(undefined);
      setSelectedPath(undefined);
      setFileDiff(undefined);
      setError(undefined);
      setLoadingChanges(false);
      return;
    }

    let cancelled = false;
    setChanges(undefined);
    setSelectedPath(undefined);
    setFileDiff(undefined);
    if (!desktopApiSupportsGitReview()) {
      setError("审查接口还没被当前窗口加载。请重启桌面端后再试。");
      setLoadingChanges(false);
      return;
    }
    setLoadingChanges(true);
    setError(undefined);
    void window.wuu
      .listGitChanges()
      .then((result) => {
        if (cancelled) {
          return;
        }
        setChanges(result);
        setExpandedPaths(new Set(collectGitChangeTreeDirectoryPaths(buildGitChangeTree(result.files))));
        setSelectedPath((current) => {
          if (current && result.files.some((file) => file.path === current)) {
            return current;
          }
          return result.files[0]?.path;
        });
      })
      .catch((nextError) => {
        if (!cancelled) {
          setError(desktopApiErrorMessage(nextError, "读取变更失败"));
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoadingChanges(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [workspaceRoot, refreshVersion]);

  useEffect(() => {
    if (!selectedPath) {
      return;
    }
    setExpandedPaths((current) => {
      const next = new Set(current);
      for (const ancestor of gitPathAncestors(selectedPath)) {
        next.add(ancestor);
      }
      return next;
    });
  }, [selectedPath]);

  useEffect(() => {
    if (!workspaceRoot || !selectedPath) {
      setFileDiff(undefined);
      setLoadingDiff(false);
      return;
    }

    let cancelled = false;
    setFileDiff(undefined);
    if (!desktopApiSupportsGitReview()) {
      setError("审查接口还没被当前窗口加载。请重启桌面端后再试。");
      setLoadingDiff(false);
      return;
    }
    setLoadingDiff(true);
    setError(undefined);
    void window.wuu
      .readGitFileDiff(selectedPath)
      .then((result) => {
        if (!cancelled) {
          setFileDiff(result);
        }
      })
      .catch((nextError) => {
        if (!cancelled) {
          setError(desktopApiErrorMessage(nextError, "读取 diff 失败"));
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoadingDiff(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [workspaceRoot, selectedPath, refreshVersion]);

  function toggleTreePath(path: string): void {
    setExpandedPaths((current) => {
      const next = new Set(current);
      if (next.has(path)) {
        next.delete(path);
      } else {
        next.add(path);
      }
      return next;
    });
  }

  if (!activeContext) {
    return (
      <div className="workspace-main-empty">
        <FolderX size={36} />
        <strong>没有项目</strong>
        <span>先打开一个项目，再查看本地变更。</span>
      </div>
    );
  }

  if (loadingChanges && !changes) {
    return (
      <div className="workspace-main-empty">
        <GitBranch size={36} />
        <strong>正在读取变更</strong>
        <span>{formatWorkspaceRoot(workspaceRoot ?? "")}</span>
      </div>
    );
  }

  if (error && !changes) {
    return (
      <div className="workspace-main-empty">
        <AlertCircle size={36} />
        <strong>读取失败</strong>
        <span>{error}</span>
      </div>
    );
  }

  if (changes && !changes.is_repo) {
    return (
      <div className="workspace-main-empty">
        <FolderX size={36} />
        <strong>不是 Git 仓库</strong>
        <span>当前项目没有可查看的 Git 变更。</span>
      </div>
    );
  }

  if (changes && files.length === 0) {
    return (
      <div className="workspace-main-empty">
        <Check size={36} />
        <strong>工作区干净</strong>
        <span>当前没有未提交的代码差异。</span>
        <button type="button" onClick={() => setRefreshVersion((version) => version + 1)}>
          刷新
        </button>
      </div>
    );
  }

  return (
    <article className="workspace-diff-review">
      <header className="workspace-diff-header">
        <div className="workspace-diff-title">
          <strong>审查</strong>
          <span>
            {branchLabel}
            {upstreamLabel ? (
              <>
                <span className="workspace-diff-branch-arrow">-&gt;</span>
                {upstreamLabel}
              </>
            ) : null}
          </span>
        </div>
        <div className="workspace-diff-summary">
          <span>{files.length} 个文件</span>
          <span className="additions">+{totals.additions.toLocaleString()}</span>
          <span className="deletions">-{totals.deletions.toLocaleString()}</span>
          <button
            className="icon-button"
            type="button"
            aria-label="刷新变更"
            title="刷新变更"
            disabled={loadingChanges || loadingDiff}
            onClick={() => setRefreshVersion((version) => version + 1)}
          >
            <RefreshCw size={16} />
          </button>
        </div>
      </header>
      <div className="workspace-diff-content">
        <section className="workspace-diff-detail">
          <div className="workspace-diff-detail-header">
            <div>
              <strong>{selectedFile ? gitChangeFilePathLabel(selectedFile) : "选择文件"}</strong>
              <span>{selectedFile ? gitChangeStatusDescription(selectedFile) : "从左侧选择一个变更文件"}</span>
            </div>
          </div>
          {error ? <div className="workspace-diff-error">{error}</div> : null}
          {loadingDiff ? (
            <div className="workspace-diff-empty">正在读取 diff...</div>
          ) : fileDiff?.binary ? (
            <div className="workspace-diff-empty">这是二进制文件，无法显示文本 diff。</div>
          ) : fileDiff?.patch ? (
            <OverlayScrollbarsComponent
              className="workspace-diff-code-scroll"
              data-overlayscrollbars-initialize
              defer
              options={OVERLAY_SCROLLBAR_OPTIONS}
            >
              <pre className="workspace-diff-code" aria-label={`${fileDiff.path} 的代码差异`}>
                {diffLines.map((line, index) => (
                  <span
                    className={`workspace-diff-line ${line.kind}`}
                    key={`${index}:${line.content.slice(0, 24)}`}
                  >
                    <span className="workspace-diff-line-number">{line.oldLine ?? ""}</span>
                    <span className="workspace-diff-line-number">{line.newLine ?? ""}</span>
                    <span className="workspace-diff-line-code">{line.content || " "}</span>
                  </span>
                ))}
              </pre>
            </OverlayScrollbarsComponent>
          ) : (
            <div className="workspace-diff-empty">没有可显示的文本 diff。</div>
          )}
          {fileDiff?.truncated ? <div className="workspace-diff-truncated">diff 太大，已截断预览。</div> : null}
        </section>
        <GitChangeTreePanel
          files={filteredFiles}
          nodes={treeNodes}
          selectedPath={selectedPath}
          expandedPaths={expandedPaths}
          query={treeQuery}
          onQueryChange={setTreeQuery}
          onSelectFile={setSelectedPath}
          onTogglePath={toggleTreePath}
        />
      </div>
    </article>
  );
}

function GitChangeTreePanel({
  files,
  nodes,
  selectedPath,
  expandedPaths,
  query,
  onQueryChange,
  onSelectFile,
  onTogglePath
}: {
  files: GitChangeFile[];
  nodes: GitChangeTreeNode[];
  selectedPath?: string;
  expandedPaths: Set<string>;
  query: string;
  onQueryChange: (value: string) => void;
  onSelectFile: (path: string) => void;
  onTogglePath: (path: string) => void;
}): JSX.Element {
  const forceExpanded = query.trim().length > 0;
  const totals = summarizeGitChangeFiles(files);
  return (
    <aside className="workspace-diff-tree" aria-label="变更文件树">
      <div className="workspace-diff-tree-header">
        <div>
          <strong>文件</strong>
          <span>
            {forceExpanded ? `${files.length} 个匹配` : `${files.length} 个文件`}
            {files.length > 0 ? (
              <>
                {" "}
                <span className="additions">+{totals.additions.toLocaleString()}</span>{" "}
                <span className="deletions">-{totals.deletions.toLocaleString()}</span>
              </>
            ) : null}
          </span>
        </div>
      </div>
      <label className="workspace-diff-search">
        <Search size={16} />
        <input
          value={query}
          placeholder="筛选文件..."
          onChange={(event) => onQueryChange(event.currentTarget.value)}
        />
      </label>
      <OverlayScrollbarsComponent
        className="workspace-diff-tree-scroll"
        data-overlayscrollbars-initialize
        defer
        options={OVERLAY_SCROLLBAR_OPTIONS}
      >
        {nodes.length === 0 ? (
          <div className="workspace-diff-tree-empty">没有匹配文件</div>
        ) : (
          <div className="workspace-diff-tree-list">
            {nodes.map((node) => (
              <GitChangeTreeNodeView
                key={node.id}
                node={node}
                depth={0}
                forceExpanded={forceExpanded}
                selectedPath={selectedPath}
                expandedPaths={expandedPaths}
                onSelectFile={onSelectFile}
                onTogglePath={onTogglePath}
              />
            ))}
          </div>
        )}
      </OverlayScrollbarsComponent>
    </aside>
  );
}

function GitChangeTreeNodeView({
  node,
  depth,
  forceExpanded,
  selectedPath,
  expandedPaths,
  onSelectFile,
  onTogglePath
}: {
  node: GitChangeTreeNode;
  depth: number;
  forceExpanded: boolean;
  selectedPath?: string;
  expandedPaths: Set<string>;
  onSelectFile: (path: string) => void;
  onTogglePath: (path: string) => void;
}): JSX.Element {
  const indentation = { paddingLeft: `${10 + depth * 18}px` } as CSSProperties;
  if (node.kind === "directory") {
    const expanded = forceExpanded || expandedPaths.has(node.path);
    return (
      <div className="workspace-diff-tree-node">
        <button
          className="workspace-diff-tree-row directory"
          type="button"
          style={indentation}
          aria-expanded={expanded}
          onClick={() => onTogglePath(node.path)}
        >
          <ChevronRight className="workspace-diff-tree-chevron" size={15} />
          {expanded ? <FolderOpen size={16} /> : <Folder size={16} />}
          <span className="workspace-diff-tree-name">{node.name}</span>
          <span className="workspace-diff-tree-count">{node.fileCount}</span>
        </button>
        {expanded ? (
          <div className="workspace-diff-tree-children">
            {node.children.map((child) => (
              <GitChangeTreeNodeView
                key={child.id}
                node={child}
                depth={depth + 1}
                forceExpanded={forceExpanded}
                selectedPath={selectedPath}
                expandedPaths={expandedPaths}
                onSelectFile={onSelectFile}
                onTogglePath={onTogglePath}
              />
            ))}
          </div>
        ) : null}
      </div>
    );
  }

  const file = node.file;
  const selected = file?.path === selectedPath;
  return (
    <button
      className={`workspace-diff-tree-row file${selected ? " active" : ""}`}
      type="button"
      style={indentation}
      aria-pressed={selected}
      onClick={() => {
        if (file) {
          onSelectFile(file.path);
        }
      }}
    >
      <span className="workspace-diff-tree-spacer" />
      <FileText size={16} />
      <span className="workspace-diff-tree-name">{node.name}</span>
      {file ? <GitChangeFileStats file={file} /> : null}
    </button>
  );
}

function GitChangeFileStats({ file }: { file: GitChangeFile }): JSX.Element {
  return (
    <span className="workspace-diff-tree-stats">
      <span className={`workspace-diff-file-status ${file.status}`}>{gitChangeStatusLabel(file.status)}</span>
      {file.binary ? (
        <span className="workspace-diff-tree-binary">binary</span>
      ) : (
        <>
          <span className="additions">+{file.additions.toLocaleString()}</span>
          <span className="deletions">-{file.deletions.toLocaleString()}</span>
        </>
      )}
    </span>
  );
}
