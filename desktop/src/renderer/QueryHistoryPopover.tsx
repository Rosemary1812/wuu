/**
 * QueryHistoryPopover — hover-on-input past-query quick jump.
 *
 * Mirrors the ChatGPT "hover the input box, see past queries, click to jump"
 * affordance. Lists the current thread's past user messages in chronological
 * order; clicking one scrolls the corresponding message into view.
 *
 * The popover is meant to be mounted inside `FloatingMenuPortal` (so it can
 * escape the composer overflow context). The parent owns the open/close
 * state and the hover anchor ref; this component is a pure presentational
 * list of past queries.
 */
import type { JSX } from "react";

export type QueryHistoryEntry = {
  turnID: string;
  itemID: string;
  text: string;
};

export type QueryHistoryPopoverProps = {
  entries: QueryHistoryEntry[];
  /**
   * Soft cap on how many entries to render. Anything beyond this is
   * collapsed into a single "+N more" footer. Defaults to no cap so
   * the caller can decide.
   */
  maxItems?: number;
  onSelect: (entry: QueryHistoryEntry) => void;
};

const PREVIEW_MAX_CHARS = 64;

function previewText(text: string): string {
  const trimmed = text.trim();
  if (trimmed.length <= PREVIEW_MAX_CHARS) {
    return trimmed;
  }
  return `${trimmed.slice(0, PREVIEW_MAX_CHARS)}…`;
}

export function QueryHistoryPopover({
  entries,
  maxItems,
  onSelect,
}: QueryHistoryPopoverProps): JSX.Element {
  if (entries.length === 0) {
    return (
      <div className="query-history-popover" role="dialog" aria-label="历史输入">
        <div className="query-history-empty">这个对话里还没有历史输入</div>
      </div>
    );
  }
  const visible = maxItems !== undefined ? entries.slice(0, maxItems) : entries;
  return (
    <div
      className="query-history-popover"
      role="dialog"
      aria-label="历史输入"
    >
      <div className="environment-panel-body query-history-list" role="listbox" aria-label="历史输入列表">
        {visible.map((entry) => (
          <button
            key={`${entry.turnID}:${entry.itemID}`}
            type="button"
            className="environment-row query-history-item"
            role="option"
            aria-selected={false}
            onMouseDown={(event) => event.preventDefault()}
            onClick={() => onSelect(entry)}
          >
            <strong className="query-history-text">{previewText(entry.text)}</strong>
          </button>
        ))}
      </div>
    </div>
  );
}
