import {
  Fragment,
  memo,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import type { Turn } from "../shared/protocol";
import { queryTextForUserItem } from "./AppState";
import { groupConversationTurns, type TurnGroup } from "./TurnGrouping";
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
  /** Groups of ≥2 turns render through this when provided (subagent
   *  orchestrations read as one shell); single-turn groups always fall
   *  back to renderTurn. */
  renderTurnGroup?: (turns: Turn[]) => ReactNode;
  /** Live hint for the grouping rule: the thread has running child agents,
   *  so the trailing user turn still belongs to the open orchestration. */
  lastGroupOpen?: boolean;
  renderBeforeTurns?: ReactNode;
  renderAfterTurn?: (turn: Turn) => ReactNode;
  renderAfterMissingTurn?: ReactNode;
  forcedFullTurnIDs?: Iterable<string>;
};

export function ConversationTurnList({
  threadID,
  turns,
  renderTurn,
  renderTurnGroup,
  lastGroupOpen,
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
  const groups = useMemo(
    () => groupConversationTurns(turns, { lastGroupOpen }),
    [turns, lastGroupOpen],
  );
  // Group objects are rebuilt on every turns-array change, but the memoized
  // entries below bail out on group identity. Reuse the previous group
  // object when its member turns are referentially untouched — the same
  // identity contract PaneTurnView relies on for single turns.
  const stableGroupsRef = useRef<TurnGroup[]>([]);
  const stableGroups = useMemo(() => {
    const previousByID = new Map(
      stableGroupsRef.current.map((group) => [group.id, group]),
    );
    const next = groups.map((group) => {
      const previous = previousByID.get(group.id);
      if (
        previous &&
        previous.turns.length === group.turns.length &&
        previous.turns.every((turn, index) => turn === group.turns[index])
      ) {
        return previous;
      }
      return group;
    });
    stableGroupsRef.current = next;
    return next;
  }, [groups]);

  useEffect(() => {
    setExpandedTurnIDs(new Set());
  }, [threadID]);

  useEffect(() => {
    setExpandedTurnIDs((current) => {
      if (current.size === 0) {
        return current;
      }
      const live = new Set(
        stableGroups.flatMap((group) => group.turns.map((turn) => turn.id)),
      );
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
  }, [stableGroups]);

  const collapseOlderTurns = stableGroups.length > TURN_LIST_COLLAPSE_THRESHOLD;
  const firstRecentFullIndex = Math.max(
    0,
    stableGroups.length - TURN_LIST_RECENT_FULL_TURNS,
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
      {stableGroups.map((group, index) => {
        const full =
          !collapseOlderTurns ||
          index >= firstRecentFullIndex ||
          group.turns.some(
            (turn) =>
              turn.status === "in_progress" ||
              expandedTurnIDs.has(turn.id) ||
              forcedFull.has(turn.id),
          );
        return (
          <TurnListEntry
            key={group.id}
            group={group}
            full={full}
            onExpand={expandTurn}
            renderTurn={renderTurn}
            renderTurnGroup={renderTurnGroup}
            renderAfterTurn={renderAfterTurn}
          />
        );
      })}
      {renderAfterMissingTurn}
    </>
  );
}

const TurnListEntry = memo(function TurnListEntry({
  group,
  full,
  onExpand,
  renderTurn,
  renderTurnGroup,
  renderAfterTurn,
}: {
  group: TurnGroup;
  full: boolean;
  onExpand: (turnID: string) => void;
  renderTurn: (turn: Turn) => ReactNode;
  renderTurnGroup?: (turns: Turn[]) => ReactNode;
  renderAfterTurn?: (turn: Turn) => ReactNode;
}): JSX.Element {
  // CollapsedTurnView is memoized on group identity (untouched groups keep
  // their member turns referentially equal); the expand callback must be
  // equally stable or it defeats that memo on every list render.
  const handleExpand = useCallback(
    () => onExpand(group.id),
    [onExpand, group.id],
  );
  return (
    <>
      {full ? (
        group.turns.length > 1 && renderTurnGroup ? (
          renderTurnGroup(group.turns)
        ) : (
          // Panes without a group renderer keep the legacy per-turn layout:
          // every member turn still renders, just not merged into one shell.
          group.turns.map((turn) => (
            <Fragment key={turn.id}>{renderTurn(turn)}</Fragment>
          ))
        )
      ) : (
        <CollapsedTurnView group={group} onExpand={handleExpand} />
      )}
      {group.turns.map((turn) => renderAfterTurn?.(turn))}
    </>
  );
});

// Memoized on group identity: server events rebuild the thread object but
// keep untouched turns referentially equal, so hundreds of collapsed rows
// skip re-rendering (and skip re-running their snippet scans) on each event.
const CollapsedTurnView = memo(function CollapsedTurnView({
  group,
  onExpand,
}: {
  group: TurnGroup;
  onExpand: () => void;
}): JSX.Element {
  const { t } = useI18n();
  const first = group.turns[0];
  const last = group.turns[group.turns.length - 1];
  const firstUserItem = first.items.find(
    (item) => item.type === "user_message" && queryTextForUserItem(item),
  );
  const queryText = firstUserItem
    ? queryTextForUserItem(firstUserItem) || t("turn.noUserPreview")
    : t("turn.noUserPreview");
  const reply = turnReplySnippet(last)?.text;

  return (
    <section
      className="turn turn-collapsed"
      id={turnAnchorID(first.id)}
      data-turn-id={first.id}
      data-turn-status={last.status}
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
                ? userMessageAnchorID(first.id, firstUserItem.id)
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
