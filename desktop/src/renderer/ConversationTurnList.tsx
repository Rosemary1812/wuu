import {
  memo,
  useCallback,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import type { Turn } from "../shared/protocol";
import { queryTextForUserItem } from "./AppState";
import {
  turnAnchorID,
  turnReplySnippet,
  userMessageAnchorID,
} from "./TurnViewHelpers";
import { useI18n } from "./i18n";

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

  // Identity-stable expander: TurnListEntry memoizes its children on turn
  // identity, which only works if this callback never changes between renders.
  const expandTurn = useCallback((turnID: string) => {
    setExpandedTurnIDs((current) => {
      if (current.has(turnID)) {
        return current;
      }
      const next = new Set(current);
      next.add(turnID);
      return next;
    });
  }, []);

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
            onExpand={expandTurn}
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
  onExpand: (turnID: string) => void;
  renderTurn: (turn: Turn) => ReactNode;
  renderAfterTurn?: (turn: Turn) => ReactNode;
}): JSX.Element {
  // CollapsedTurnView is memoized on turn identity (untouched turns keep
  // their object across server events); the expand callback must be equally
  // stable or it defeats that memo on every list render.
  const handleExpand = useCallback(() => onExpand(turn.id), [onExpand, turn.id]);
  return (
    <>
      {full ? (
        renderTurn(turn)
      ) : (
        <CollapsedTurnView turn={turn} onExpand={handleExpand} />
      )}
      {renderAfterTurn?.(turn)}
    </>
  );
}

// Memoized on turn identity: server events rebuild the thread object but
// keep untouched turns referentially equal, so hundreds of collapsed rows
// skip re-rendering (and skip re-running their snippet scans) on each event.
const CollapsedTurnView = memo(function CollapsedTurnView({
  turn,
  onExpand,
}: {
  turn: Turn;
  onExpand: () => void;
}): JSX.Element {
  const { t } = useI18n();
  const firstUserItem = turn.items.find(
    (item) => item.type === "user_message" && queryTextForUserItem(item),
  );
  const queryText = firstUserItem
    ? queryTextForUserItem(firstUserItem) ||
      t("turn.noUserPreview")
    : t("turn.noUserPreview");
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
        aria-label={t("turn.expandConversationTurn")}
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
        <span className="turn-collapsed-action">{t("common.expand")}</span>
      </button>
    </section>
  );
});

function compactPreview(text: string, maxLength = 180): string {
  const compact = text.replace(/\s+/g, " ").trim();
  if (compact.length <= maxLength) {
    return compact;
  }
  return `${compact.slice(0, maxLength - 1)}…`;
}
