import { Search, X } from "lucide-react";
import type {
  KeyboardEvent as ReactKeyboardEvent,
  RefObject,
} from "react";
import type {
  DesktopProject,
  Thread,
  ThreadSearchResultItem,
  Turn,
} from "../shared/protocol";
import {
  conversationSearchResultSections,
  conversationSearchStatusText,
  conversationSearchVisibleSnippet,
} from "./ConversationSearchDisplay";
import type { ConversationSearchState } from "./ConversationSearchState";
import {
  conversationSearchContextLabel,
  conversationSearchThreadMeta,
} from "./AppState";
import { threadDisplayTitle } from "./ThreadTitles";

export function ConversationSearchOverlay({
  state,
  results,
  threads,
  projects,
  activeThreadID,
  pendingThreadID,
  dialogRef,
  inputRef,
  onClose,
  onRefresh,
  onQueryChange,
  onClearQuery,
  onKeyDown,
  onSelectIndex,
  onSelectResult,
}: {
  state: ConversationSearchState;
  results: ThreadSearchResultItem[];
  threads: Thread[];
  projects: DesktopProject[];
  activeThreadID?: string;
  pendingThreadID?: string;
  dialogRef: RefObject<HTMLDivElement | null>;
  inputRef: RefObject<HTMLInputElement | null>;
  onClose: () => void;
  onRefresh: () => void;
  onQueryChange: (query: string) => void;
  onClearQuery: () => void;
  onKeyDown: (event: ReactKeyboardEvent<HTMLInputElement>) => void;
  onSelectIndex: (index: number) => void;
  onSelectResult: (result: ThreadSearchResultItem) => void;
}): JSX.Element | null {
  if (!state.open && !state.closing) {
    return null;
  }

  const sections = conversationSearchResultSections(results, state.query);
  const status = conversationSearchStatusText({
    loading: state.loading,
    query: state.query,
    resultCount: results.length,
  });

  return (
    <div
      className={`conversation-search-overlay${state.closing ? " closing" : ""}`}
      onPointerDown={(event) => {
        if (event.target === event.currentTarget) {
          onClose();
        }
      }}
    >
      <div
        className="conversation-search-dialog"
        role="dialog"
        aria-modal="true"
        aria-label="搜索会话"
        ref={dialogRef}
      >
        <div className="conversation-search-input-wrap">
          <Search className="icon-lg" aria-hidden="true" />
          <input
            ref={inputRef}
            value={state.query}
            placeholder="搜索对话内容或提问"
            onChange={(event) => onQueryChange(event.target.value)}
            onKeyDown={onKeyDown}
          />
          {state.query ? (
            <button
              className="conversation-search-clear"
              type="button"
              aria-label="清空搜索"
              onClick={onClearQuery}
            >
              <X className="icon" />
            </button>
          ) : null}
        </div>
        <div
          className={`conversation-search-status${state.loading ? " loading" : ""}`}
        >
          <span className="conversation-search-status-text">{status}</span>
          <button type="button" onClick={onRefresh}>
            刷新
          </button>
        </div>
        {state.error ? (
          <div className="conversation-search-error">{state.error}</div>
        ) : null}
        <div className="conversation-search-body">
          <div className="conversation-search-results">
            {sections.map((section) => (
              <section
                className="conversation-search-section"
                key={section.title}
              >
                <div className="conversation-search-section-title">
                  {section.title}
                </div>
                {section.results.map((result, sectionIndex) => {
                  const resultIndex = section.startIndex + sectionIndex;
                  const thread = result.thread;
                  const title = threadDisplayTitle(
                    thread,
                    threads,
                    "未命名对话",
                  );
                  const active = thread.id === activeThreadID;
                  const pending = pendingThreadID === thread.id;
                  const selected = state.selectedIndex === resultIndex;
                  const contextLabel = conversationSearchContextLabel(
                    thread,
                    projects,
                  );
                  const snippet = conversationSearchVisibleSnippet({
                    query: state.query,
                    snippet: result.snippet,
                    title,
                  });
                  return (
                    <button
                      key={thread.id}
                      className={`conversation-search-result${active ? " active" : ""}${pending ? " pending" : ""}${selected ? " selected" : ""}`}
                      type="button"
                      aria-current={active ? "page" : undefined}
                      aria-selected={selected}
                      onMouseEnter={() => onSelectIndex(resultIndex)}
                      onClick={() => onSelectResult(result)}
                    >
                      <span className="conversation-search-result-main">
                        <span className="conversation-search-result-title">
                          {title}
                        </span>
                        {snippet ? (
                          <span className="conversation-search-result-snippet">
                            {snippet}
                          </span>
                        ) : null}
                      </span>
                      <span className="conversation-search-result-side">
                        <span className="conversation-search-result-context">
                          {contextLabel}
                        </span>
                        <span className="conversation-search-result-meta">
                          {conversationSearchThreadMeta(thread)}
                        </span>
                        {resultIndex < 9 ? (
                          <span className="conversation-search-result-shortcut">
                            ⌘{resultIndex + 1}
                          </span>
                        ) : null}
                      </span>
                    </button>
                  );
                })}
              </section>
            ))}
            {results.length === 0 ? (
              <div className="conversation-search-empty">
                {state.loading
                  ? "正在搜索会话"
                  : state.query.trim()
                    ? "没有匹配的会话"
                    : "暂无会话"}
              </div>
            ) : null}
          </div>
          <ConversationSearchPreview
            results={results}
            threads={threads}
            projects={projects}
            selectedIndex={state.selectedIndex}
            previewThreadID={state.previewedThreadID}
            previewTurns={state.previewedTurns}
            previewLoading={state.previewLoading}
            previewError={state.previewError}
            query={state.query}
          />
        </div>
      </div>
    </div>
  );
}

// ConversationSearchPreview renders the right pane: a one-glance view of the
// currently-selected thread so the user can pick the right result without
// having to open each candidate and bounce out of the search. The pane stays
// in sync with the result list: arrow keys / mouse hover update both. Empty
// / loading / error states are handled inline so the layout never jumps.
function ConversationSearchPreview({
  results,
  threads,
  projects,
  selectedIndex,
  previewThreadID,
  previewTurns,
  previewLoading,
  previewError,
  query,
}: {
  results: ThreadSearchResultItem[];
  threads: Thread[];
  projects: DesktopProject[];
  selectedIndex: number;
  previewThreadID: string;
  previewTurns: Turn[];
  previewLoading: boolean;
  previewError: string;
  query: string;
}): JSX.Element {
  const idx = Math.max(0, Math.min(selectedIndex, results.length - 1));
  const selectedResult = results[idx];
  const thread = selectedResult?.thread;
  const title = thread ? threadDisplayTitle(thread, threads, "未命名对话") : "";
  const contextLabel = thread
    ? conversationSearchContextLabel(thread, projects)
    : "";
  const meta = thread ? conversationSearchThreadMeta(thread) : "";
  const snippet = selectedResult
    ? conversationSearchVisibleSnippet({
        query,
        snippet: selectedResult.snippet,
        title,
      })
    : "";

  // The preview state can briefly describe a thread that the user has already
  // navigated away from (a stale response, or selection moved mid-fetch).
  // Treat any state where previewThreadID no longer matches the selection as
  // "not for this thread" so the pane never paints the wrong content.
  const selectedThreadID = thread?.id ?? "";
  const previewIsForSelection = previewThreadID === selectedThreadID;
  const loadingForSelection =
    previewIsForSelection && previewLoading && previewTurns.length === 0;
  const errorForSelection = previewIsForSelection ? previewError : "";
  const turnsForSelection = previewIsForSelection ? previewTurns : [];
  const hasTurns = turnsForSelection.length > 0;

  return (
    <aside
      className="conversation-search-preview"
      aria-label="会话预览"
      data-state={
        !thread
          ? "empty"
          : loadingForSelection
            ? "loading"
            : errorForSelection
              ? "error"
              : hasTurns
                ? "ready"
                : "no-turns"
      }
    >
      {!thread ? (
        <div className="conversation-search-preview-empty">
          选择一个会话查看预览
        </div>
      ) : (
        <>
          <header className="conversation-search-preview-header">
            <h2 className="conversation-search-preview-title">{title}</h2>
            <div className="conversation-search-preview-meta">
              <span className="conversation-search-preview-context">
                {contextLabel}
              </span>
              <span aria-hidden="true" className="conversation-search-preview-sep">
                ·
              </span>
              <span className="conversation-search-preview-time">{meta}</span>
            </div>
          </header>
          {snippet ? (
            <div className="conversation-search-preview-snippet">{snippet}</div>
          ) : null}
          {errorForSelection ? (
            <div className="conversation-search-preview-error">
              {errorForSelection}
            </div>
          ) : null}
          {loadingForSelection ? (
            <div className="conversation-search-preview-loading">
              加载预览中…
            </div>
          ) : null}
          {!loadingForSelection && !errorForSelection && !hasTurns ? (
            <div className="conversation-search-preview-empty">
              暂无预览
            </div>
          ) : null}
          {hasTurns ? (
            <div className="conversation-search-preview-turns">
              {turnsForSelection.map((turn) => (
                <PreviewTurn key={turn.id} turn={turn} />
              ))}
            </div>
          ) : null}
        </>
      )}
    </aside>
  );
}

function PreviewTurn({ turn }: { turn: Turn }): JSX.Element {
  const role = previewTurnRole(turn);
  const text = previewTurnText(turn);
  return (
    <article
      className={`conversation-search-preview-turn role-${role}`}
      data-role={role}
    >
      <div className="conversation-search-preview-role">
        {previewTurnRoleLabel(role)}
      </div>
      <div className="conversation-search-preview-text">
        {truncatePreviewText(text)}
      </div>
    </article>
  );
}

function previewTurnRole(
  turn: Turn,
): "user" | "assistant" | "system" {
  const first = turn.items[0];
  if (!first) return "system";
  switch (first.type) {
    case "user_message":
      return "user";
    case "agent_message":
    case "reasoning":
      return "assistant";
    default:
      return "system";
  }
}

function previewTurnRoleLabel(
  role: "user" | "assistant" | "system",
): string {
  switch (role) {
    case "user":
      return "你";
    case "assistant":
      return "助手";
    default:
      return "系统";
  }
}

function previewTurnText(turn: Turn): string {
  const parts: string[] = [];
  for (const item of turn.items) {
    const text = (item.text ?? "").trim();
    if (text) parts.push(text);
  }
  return parts.join("\n\n");
}

// Truncate long turns so a single chatty assistant reply does not push the
// other turns off the visible pane. The limit is generous enough to keep a
// real "what was this thread about" answer readable.
function truncatePreviewText(text: string, limit = 400): string {
  if (text.length <= limit) return text;
  return text.slice(0, limit).trimEnd() + "…";
}