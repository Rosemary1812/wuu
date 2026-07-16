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
import { conversationSearchVisibleSnippet } from "./ConversationSearchDisplay";
import { primaryShortcutLabel } from "./platform";
import type { ConversationSearchState } from "./ConversationSearchState";
import {
  conversationSearchContextLabel,
  conversationSearchThreadMeta,
} from "./AppState";
import { threadDisplayTitle } from "./ThreadTitles";
import { useI18n } from "./i18n";

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
  onQueryChange: (query: string) => void;
  onClearQuery: () => void;
  onKeyDown: (event: ReactKeyboardEvent<HTMLInputElement>) => void;
  onSelectIndex: (index: number) => void;
  onSelectResult: (result: ThreadSearchResultItem) => void;
}): JSX.Element | null {
  const { t } = useI18n();
  if (!state.open && !state.closing) {
    return null;
  }

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
        aria-label={t("search.conversations")}
        ref={dialogRef}
      >
        <div className="conversation-search-input-wrap">
          <Search className="icon-lg" aria-hidden="true" />
          <input
            ref={inputRef}
            value={state.query}
            placeholder={t("search.placeholder")}
            onChange={(event) => onQueryChange(event.target.value)}
            onKeyDown={onKeyDown}
          />
          {state.query ? (
            <button
              className="conversation-search-clear"
              type="button"
              aria-label={t("search.clear")}
              onClick={onClearQuery}
            >
              <X className="icon" />
            </button>
          ) : (
            <kbd className="conversation-search-shortcut-hint" aria-hidden="true">
              {primaryShortcutLabel("P")}
            </kbd>
          )}
        </div>
        {state.error ? (
          <div className="conversation-search-error">{state.error}</div>
        ) : null}
        <div className="conversation-search-body">
          <div className="conversation-search-results">
            {results.map((result, resultIndex) => {
              const thread = result.thread;
              const title = threadDisplayTitle(thread, threads, t("search.untitledConversation"));
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
                        {primaryShortcutLabel(resultIndex + 1)}
                      </span>
                    ) : null}
                  </span>
                </button>
              );
            })}
            {results.length === 0 ? (
              <div className="conversation-search-empty">
                {state.loading
                  ? t("search.searching")
                  : state.query.trim()
                    ? t("search.noMatches")
                    : t("search.noConversations")}
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
  const { t } = useI18n();
  const idx = Math.max(0, Math.min(selectedIndex, results.length - 1));
  const selectedResult = results[idx];
  const thread = selectedResult?.thread;
  const title = thread ? threadDisplayTitle(thread, threads, t("search.untitledConversation")) : "";
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
      aria-label={t("search.preview")}
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
          {t("search.selectForPreview")}
        </div>
      ) : (
        <>
          <header className="conversation-search-preview-header">
            <h2 className="conversation-search-preview-title" title={title}>
              {title}
            </h2>
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
              {t("search.loadingPreview")}
            </div>
          ) : null}
          {!loadingForSelection && !errorForSelection && !hasTurns ? (
            <div className="conversation-search-preview-empty">
              {t("search.noPreview")}
            </div>
          ) : null}
          {hasTurns ? (
            <div className="conversation-search-preview-turns">
              {turnsForSelection.map((turn) => (
                <PreviewTurnGroup key={turn.id} turn={turn} query={query} />
              ))}
            </div>
          ) : null}
        </>
      )}
    </aside>
  );
}

// A turn in this codebase bundles the user's opening message with every
// agent_message / reasoning / tool_call that followed it (see
// turnsFromPersistedHistoryInScope in appserver/model.go). Rendering one
// row per turn with a single role hides half the conversation: labelling
// the row "助手" when the agent replied swallows the user's query, while
// labelling it "你" hides the agent's final answer. Either way the right
// pane shows a misleading timeline that drops every other user query.
// Emit up to two rows per turn — a "你" row for the user_message and an
// "助手" row for the agent_message — so both sides appear in the order
// they actually happened.
export function PreviewTurnGroup({ turn, query }: { turn: Turn; query: string }): JSX.Element {
  const userText = pickUserText(turn);
  const assistantText = pickAssistantText(turn);
  return (
    <>
      {userText ? (
        <PreviewRow
          key={`${turn.id}:user`}
          role="user"
          text={userText}
          query={query}
        />
      ) : null}
      {assistantText ? (
        <PreviewRow
          key={`${turn.id}:assistant`}
          role="assistant"
          text={assistantText}
          query={query}
        />
      ) : null}
    </>
  );
}

function PreviewRow({
  role,
  text,
  query,
}: {
  role: "user" | "assistant";
  text: string;
  query: string;
}): JSX.Element {
  const oneLineText = oneLinePreviewText(text, query);
  // The row's chrome (right-aligned bubble for user, flush-left plain
  // text for assistant) lives in CSS so this component stays a pure
  // function of (role, text, query). `title` exposes the full
  // untruncated text so users can still read longer rows via tooltip
  // when the inline ellipsis hides the match context.
  return (
    <article
      className={`conversation-search-preview-turn role-${role}`}
      data-role={role}
      title={text || undefined}
    >
      <span className="conversation-search-preview-text">
        {oneLineText}
      </span>
    </article>
  );
}

// Last non-empty user_message text in the turn. Each user_message opens
// its own row in the right pane, mirroring the live conversation surface
// where every user query gets its own bubble.
function pickUserText(turn: Turn): string {
  let final = "";
  for (const item of turn.items) {
    if (item.type !== "user_message") continue;
    const text = (item.text ?? "").trim();
    if (text) final = text;
  }
  return final;
}

// Last non-empty agent_message text in the turn, preferring items tagged
// `phase: "final_answer"` — the same rule
// TurnViewHelpers.explicitFinalAgentMessageItemID uses to identify the
// rendered answer region in the live turn view. An assistant turn can
// carry commentary + final_answer passes interleaved with tool calls,
// and showing the commentary would mislead the search-preview pane.
// When no final_answer item exists yet (streaming in progress, legacy
// items without a phase) fall back to the last non-empty agent_message
// so the preview still surfaces something readable.
//
// Go-side `finalAgentMessageText` (appserver/resident_turn_failure.go)
// intentionally does not filter by phase because the turn-failure path
// only runs once the agent has stopped emitting; the preview can fire
// mid-stream and benefits from a phase-aware fallback.
function pickAssistantText(turn: Turn): string {
  let finalAnswer = "";
  let lastAgentMessage = "";
  for (const item of turn.items) {
    if (item.type !== "agent_message") continue;
    const text = (item.text ?? "").trim();
    if (!text) continue;
    lastAgentMessage = text;
    if (item.phase === "final_answer") finalAnswer = text;
  }
  return finalAnswer || lastAgentMessage;
}

// Pick a single-line window of the turn text that keeps the query match
// visible. A naive "first N chars" slice (the previous behavior) hides the
// match whenever it sits past position N — the exact disambiguation
// problem this pane exists to solve. Falls back to the leading window when
// the turn does not contain the query (e.g. surrounding context turns).
function oneLinePreviewText(
  text: string,
  query: string,
  halfWindow = 110,
): string {
  if (!text) return "";
  const trimmedQuery = query.trim();
  if (!trimmedQuery) return leadingWindow(text, halfWindow * 2);
  const idx = text.toLowerCase().indexOf(trimmedQuery.toLowerCase());
  if (idx < 0) return leadingWindow(text, halfWindow * 2);
  const start = Math.max(0, idx - halfWindow);
  const end = Math.min(text.length, idx + trimmedQuery.length + halfWindow);
  const prefix = start > 0 ? "…" : "";
  const suffix = end < text.length ? "…" : "";
  return prefix + text.slice(start, end).trim() + suffix;
}

function leadingWindow(text: string, length: number): string {
  if (text.length <= length) return text;
  return text.slice(0, length).trimEnd() + "…";
}
