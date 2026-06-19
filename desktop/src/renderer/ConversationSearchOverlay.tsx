import { Search, X } from "lucide-react";
import type {
  KeyboardEvent as ReactKeyboardEvent,
  RefObject,
} from "react";
import type {
  DesktopProject,
  Thread,
  ThreadSearchResultItem,
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
      </div>
    </div>
  );
}
