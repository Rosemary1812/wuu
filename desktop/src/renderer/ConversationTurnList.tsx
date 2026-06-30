import { useEffect, useMemo, useState, type ReactNode } from "react";
import type { Turn } from "../shared/protocol";
import { queryTextForUserItem } from "./AppState";
import {
  turnAnchorID,
  turnReplySnippet,
  userMessageAnchorID,
} from "./TurnViewHelpers";

export const TURN_LIST_COLLAPSE_THRESHOLD = 80;
export const TURN_LIST_RECENT_FULL_TURNS = 40;

type ConversationTurnListProps = {
  threadID: string;
  turns: Turn[];
  renderTurn: (turn: Turn) => ReactNode;
  renderBeforeTurns?: ReactNode;
  renderAfterTurn?: (turn: Turn) => ReactNode;
  renderAfterMissingTurn?: ReactNode;
  forcedFullTurnIDs?: Iterable<string>;
};

export function ConversationTurnList({
  threadID,
  turns,
  renderTurn,
  renderBeforeTurns,
  renderAfterTurn,
  renderAfterMissingTurn,
  forcedFullTurnIDs,
}: ConversationTurnListProps): JSX.Element {
  const [expandedTurnIDs, setExpandedTurnIDs] = useState<Set<string>>(
    () => new Set(),
  );
  const forcedFull = useMemo(
    () => new Set(forcedFullTurnIDs ?? []),
    [forcedFullTurnIDs],
  );

  useEffect(() => {
    setExpandedTurnIDs(new Set());
  }, [threadID]);

  useEffect(() => {
    setExpandedTurnIDs((current) => {
      if (current.size === 0) {
        return current;
      }
      const live = new Set(turns.map((turn) => turn.id));
      let changed = false;
      const next = new Set<string>();
      for (const id of current) {
        if (live.has(id)) {
          next.add(id);
        } else {
          changed = true;
        }
      }
      return changed ? next : current;
    });
  }, [turns]);

  const collapseOlderTurns = turns.length > TURN_LIST_COLLAPSE_THRESHOLD;
  const firstRecentFullIndex = Math.max(
    0,
    turns.length - TURN_LIST_RECENT_FULL_TURNS,
  );

  return (
    <>
      {renderBeforeTurns}
      {turns.map((turn, index) => {
        const full =
          !collapseOlderTurns ||
          index >= firstRecentFullIndex ||
          turn.status === "in_progress" ||
          expandedTurnIDs.has(turn.id) ||
          forcedFull.has(turn.id);
        return (
          <TurnListEntry
            key={turn.id}
            turn={turn}
            full={full}
            onExpand={() =>
              setExpandedTurnIDs((current) => {
                if (current.has(turn.id)) {
                  return current;
                }
                const next = new Set(current);
                next.add(turn.id);
                return next;
              })
            }
            renderTurn={renderTurn}
            renderAfterTurn={renderAfterTurn}
          />
        );
      })}
      {renderAfterMissingTurn}
    </>
  );
}

function TurnListEntry({
  turn,
  full,
  onExpand,
  renderTurn,
  renderAfterTurn,
}: {
  turn: Turn;
  full: boolean;
  onExpand: () => void;
  renderTurn: (turn: Turn) => ReactNode;
  renderAfterTurn?: (turn: Turn) => ReactNode;
}): JSX.Element {
  return (
    <>
      {full ? (
        renderTurn(turn)
      ) : (
        <CollapsedTurnView turn={turn} onExpand={onExpand} />
      )}
      {renderAfterTurn?.(turn)}
    </>
  );
}

function CollapsedTurnView({
  turn,
  onExpand,
}: {
  turn: Turn;
  onExpand: () => void;
}): JSX.Element {
  const firstUserItem = turn.items.find(
    (item) => item.type === "user_message" && queryTextForUserItem(item),
  );
  const queryText = firstUserItem
    ? queryTextForUserItem(firstUserItem) ||
      "这一轮没有可预览的用户消息"
    : "这一轮没有可预览的用户消息";
  const reply = turnReplySnippet(turn)?.text;

  return (
    <section
      className="turn turn-collapsed"
      id={turnAnchorID(turn.id)}
      data-turn-id={turn.id}
      data-turn-status={turn.status}
    >
      <button
        className="turn-collapsed-button"
        type="button"
        onClick={onExpand}
        aria-label="展开这一轮对话"
      >
        <span className="turn-collapsed-marker" aria-hidden="true" />
        <span className="turn-collapsed-copy">
          <span
            id={
              firstUserItem
                ? userMessageAnchorID(turn.id, firstUserItem.id)
                : undefined
            }
            className="turn-collapsed-query"
          >
            {compactPreview(queryText)}
          </span>
          {reply ? (
            <span className="turn-collapsed-reply">
              {compactPreview(reply)}
            </span>
          ) : null}
        </span>
        <span className="turn-collapsed-action">展开</span>
      </button>
    </section>
  );
}

function compactPreview(text: string, maxLength = 180): string {
  const compact = text.replace(/\s+/g, " ").trim();
  if (compact.length <= maxLength) {
    return compact;
  }
  return `${compact.slice(0, maxLength - 1)}…`;
}
