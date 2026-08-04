/// <reference path="../shared/jsx-compat.d.ts" />

import { Fragment, useMemo } from "react";
import type {
  InputFile,
  InputImage,
  ThreadItem,
  Turn,
} from "../shared/protocol";
import {
  buildAssistantTurnDisplay,
  type AssistantTurnDisplay,
  type TurnEntry,
  type TurnProcessPreview,
} from "./AssistantTurnDisplay";
import { useAssistantTurnPresentation } from "./AssistantTurnPresentation";
import { AssistantTurnShell } from "./AssistantTurnShell";
import { ThreadItemView } from "./ThreadItemView";
import { TurnEditSummaryCard } from "./TurnEditSummaryCard";
import type { TurnFileDiffSelection } from "./TurnFileDiffTypes";
import { SystemEventDivider, TurnEventNotice, StreamReconnectNotice } from "./TurnNotice";
import { turnEventForTurn } from "./TurnEvents";
import { isAgentHandoffItem } from "./AgentHandoff";
import { parseTurnTimestampMs } from "./RunDebugPanel";
import {
  messageFlowAgentMessageItemID,
  turnAnchorID,
  turnHasAssistantOutput,
} from "./TurnViewHelpers";
import { TurnView } from "./TurnView";
import { turnsHavePendingSubagents } from "./TurnGrouping";
import type { TurnStreamStatus } from "./AppState";
import type { SubagentChipDisplay } from "./AgentHandoff";
import { translateCurrent } from "./i18n";

export type TurnGroupViewProps = {
  /** One orchestration group (TurnGrouping). Length 1 for ordinary turns. */
  turns: Turn[];
  /** The thread still has running child agents and this is the last group:
   *  the orchestration is between turns, waiting for a completion wake. */
  awaiting?: boolean;
  /** The user stopped this orchestration while it was between wake turns. */
  interrupted?: boolean;
  cwd?: string;
  onOpenFile?: (path: string) => void;
  onOpenAgent?: (agentID: string) => void;
  latestAgentMessageID?: string;
  onStreamFrame: () => void;
  onForkMessage?: (turnID: string, itemID: string) => void;
  onEditMessage?: (turnID: string, item: ThreadItem) => void;
  editingMessage?: { turnID: string; itemID: string; submitting: boolean };
  onCancelEditMessage?: () => void;
  onSubmitEditMessage?: (
    turnID: string,
    item: ThreadItem,
    text: string,
    images: InputImage[],
    files: InputFile[],
  ) => void;
  onCollapseComplete?: () => void;
  onOpenFileDiff?: (selection: TurnFileDiffSelection) => void;
  onOpenRuns?: (turnID: string) => void;
  streamStatus?: TurnStreamStatus;
  isLatestTurn?: boolean;
};

export function TurnGroupView(props: TurnGroupViewProps): JSX.Element {
  const { turns, awaiting, interrupted } = props;
  const first = turns[0];
  const timelinePending = turnsHavePendingSubagents(turns);
  // A single-turn group with no live orchestration renders through the
  // ordinary per-turn path untouched; everything else (merged groups, and
  // the waiting-between-turns state) renders one shell for the whole run.
  // The live child-agent set is authoritative. Persisted or compacted turn
  // history may no longer contain the original spawn_agent item, but the
  // conversation must not expose a completed block while the side panel still
  // reports work in flight.
  const orchestrationLive = Boolean(awaiting) || (timelinePending && !interrupted);
  const orchestrationInterrupted = Boolean(interrupted);
  if (turns.length === 1 && !orchestrationLive && !orchestrationInterrupted && first) {
    const turn = first;
    return (
      <TurnView
        turn={turn}
        cwd={props.cwd}
        onOpenFile={props.onOpenFile}
        onOpenAgent={props.onOpenAgent}
        latestAgentMessageID={props.latestAgentMessageID}
        onStreamFrame={props.onStreamFrame}
        onForkMessage={props.onForkMessage}
        onEditMessage={props.onEditMessage}
        editingMessage={props.editingMessage}
        onCancelEditMessage={props.onCancelEditMessage}
        onSubmitEditMessage={props.onSubmitEditMessage}
        onCollapseComplete={props.onCollapseComplete}
        onOpenFileDiff={props.onOpenFileDiff}
        onOpenRuns={
          props.onOpenRuns ? () => props.onOpenRuns?.(turn.id) : undefined
        }
        streamStatus={props.streamStatus}
        isLatestTurn={props.isLatestTurn}
      />
    );
  }
  return <MergedTurnGroupView {...props} awaiting={orchestrationLive} />;
}

function MergedTurnGroupView({
  turns,
  awaiting,
  interrupted,
  cwd,
  onOpenFile,
  onOpenAgent,
  latestAgentMessageID,
  onStreamFrame,
  onForkMessage,
  onEditMessage,
  editingMessage,
  onCancelEditMessage,
  onSubmitEditMessage,
  onCollapseComplete,
  onOpenFileDiff,
  onOpenRuns,
  streamStatus,
  isLatestTurn,
}: TurnGroupViewProps): JSX.Element {
  const first = turns[0];
  const last = turns[turns.length - 1];
  const anyMemberInProgress = turns.some(
    (turn) => turn.status === "in_progress",
  );
  // The group reads as one live turn while any member runs OR while the
  // orchestration is parked between turns waiting for a completion wake.
  const live = anyMemberInProgress || Boolean(awaiting);
  const closed = !live && !interrupted && last.status === "completed";
  // Only the group's final answer carries the action bar; intermediate
  // answers (the wake-up progress reports) render as plain text.
  const actionableAgentMessageID = closed
    ? messageFlowAgentMessageItemID(last)
    : undefined;

  // The shell sees a synthetic turn: identity of the first, items of all
  // (turnProgressContent reads running tools / latest item), status and
  // timing of the whole run. Per-item rendering still resolves each
  // entry's origin turn via TurnEntry.turn.
  const shellTurn = useMemo<Turn>(() => {
    let durationMs: number | undefined;
    if (!live) {
      const startMs = parseTurnTimestampMs(first.started_at);
      const endMs = parseTurnTimestampMs(last.completed_at);
      if (Number.isFinite(startMs) && Number.isFinite(endMs) && endMs >= startMs) {
        durationMs = endMs - startMs;
      } else {
        const sum = turns.reduce(
          (acc, turn) =>
            acc + (typeof turn.duration_ms === "number" ? turn.duration_ms : 0),
          0,
        );
        durationMs = sum > 0 ? sum : undefined;
      }
    }
    return {
      ...first,
      items: turns.flatMap((turn) => turn.items),
      status: live ? "in_progress" : interrupted ? "interrupted" : last.status,
      started_at: first.started_at,
      completed_at: live ? undefined : last.completed_at,
      duration_ms: durationMs,
      error: live ? undefined : last.error,
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [turns, live, interrupted]);

  const display = useMemo(() => mergeTurnDisplays(turns), [turns]);
  const presented = useAssistantTurnPresentation(shellTurn.id, display);

  const hasMissingReply =
    closed && presented?.missingReplyMessage !== undefined;

  return (
    <section
      className="turn"
      id={turnAnchorID(first.id)}
      data-turn-id={first.id}
      data-turn-status={shellTurn.status}
    >
      {turns.map((member) =>
        member.items
          .filter(
            (item) => item.type === "user_message" && !isAgentHandoffItem(item),
          )
          .map((item) => (
            <ThreadItemView
              key={item.id}
              turnID={member.id}
              turnStatus={member.status}
              item={item}
              cwd={cwd}
              onOpenFile={onOpenFile}
              streaming={false}
              onStreamFrame={onStreamFrame}
              onEditMessage={onEditMessage}
              editing={
                editingMessage?.turnID === member.id &&
                editingMessage.itemID === item.id
              }
              editSubmitting={
                editingMessage?.turnID === member.id &&
                editingMessage.itemID === item.id
                  ? editingMessage.submitting
                  : false
              }
              onCancelEditMessage={onCancelEditMessage}
              onSubmitEditMessage={onSubmitEditMessage}
              onOpenAgent={onOpenAgent}
            />
          )),
      )}
      {presented ? (
        <AssistantTurnShell
          turn={shellTurn}
          display={presented}
          cwd={cwd}
          onOpenFile={onOpenFile}
          onOpenAgent={onOpenAgent}
          actionableAgentMessageID={actionableAgentMessageID}
          latestAgentMessageID={latestAgentMessageID}
          onStreamFrame={onStreamFrame}
          onForkMessage={onForkMessage}
          onOpenRuns={
            onOpenRuns ? () => onOpenRuns(last.id) : undefined
          }
          onCollapseComplete={onCollapseComplete}
          subagentWaiting={Boolean(awaiting)}
        />
      ) : null}
      {turns.map((member) => {
        const event = turnEventForTurn(
          member,
          turnHasAssistantOutput(member),
          member.id === last.id ? hasMissingReply : false,
        );
        return (
          <Fragment key={member.id}>
            <TurnEditSummaryCard
              turn={member}
              cwd={cwd}
              onOpenFile={onOpenFile}
              onOpenFileDiff={onOpenFileDiff}
            />
            {event ? <TurnEventNotice event={event} /> : null}
          </Fragment>
        );
      })}
      {interrupted ? (
        <SystemEventDivider text={translateCurrent("turn.orchestrationPaused")} />
      ) : null}
      {isLatestTurn &&
      last.status === "in_progress" &&
      streamStatus?.liveProgress ? (
        <StreamReconnectNotice text={streamStatus.text} />
      ) : null}
    </section>
  );
}

function mergeTurnDisplays(turns: Turn[]): AssistantTurnDisplay | undefined {
  const entries: TurnEntry[] = [];
  const spawnBatches: Array<{
    entryIndex: number;
    agents: Array<{ aliases: string[]; finished: boolean }>;
    failed: boolean;
  }> = [];
  const chips: SubagentChipDisplay[] = [];
  let hasAnswer = false;
  let latestProcessPreview: TurnProcessPreview | undefined;
  let lastDisplay: AssistantTurnDisplay | undefined;
  for (const turn of turns) {
    const display = buildAssistantTurnDisplay(turn, undefined);
    if (!display) continue;
    lastDisplay = display;
    for (const entry of display.entries) {
      if (isSpawnEntry(entry)) {
        const spawnItems = entry.items ?? [entry.item];
        const identities = spawnItems.map(spawnIdentity);
        const agents = identities.map((identity) => ({
          aliases: identity.aliases,
          finished: false,
        }));
        const singleTarget = identities[0]?.target ?? "";
        entries.push({
          ...entry,
          kind: "subagent_status",
          subagentStatus:
            agents.length === 1
              ? {
                  label: translateCurrent("toolActivity.subtaskTarget", {
                    target: singleTarget,
                  }),
                  outcome: "updated",
                }
              : spawnBatchDisplay(agents.length, 0, false),
          turn,
        });
        spawnBatches.push({
          entryIndex: entries.length - 1,
          agents,
          failed: false,
        });
        continue;
      }
      if (entry.kind === "subagent_status" && entry.subagentStatus) {
        const label = entry.subagentStatus.label;
        const unresolvedAgents = spawnBatches.flatMap((batch) =>
          batch.agents.flatMap((agent) =>
            agent.finished ? [] : [{ batch, agent }],
          ),
        );
        const matched = unresolvedAgents.find(({ agent }) =>
          agent.aliases.some((alias) => alias.length > 0 && label.includes(alias)),
        );
        const resolved = matched ?? (unresolvedAgents.length === 1 ? unresolvedAgents[0] : undefined);
        if (resolved) {
          resolved.agent.finished = true;
          resolved.batch.failed ||= entry.subagentStatus.outcome === "failed";
          const finished = resolved.batch.agents.filter((agent) => agent.finished).length;
          const total = resolved.batch.agents.length;
          entries[resolved.batch.entryIndex] = {
            ...entries[resolved.batch.entryIndex],
            settled: finished === total,
            streaming: false,
            subagentStatus:
              total === 1
                ? entry.subagentStatus
                : spawnBatchDisplay(total, finished, resolved.batch.failed),
          };
          continue;
        }
        // Completion is the result of an earlier spawn tool call, never new
        // assistant content. If projection/compaction removed that spawn,
        // omit the orphan instead of appending a misleading bottom row.
        continue;
      }
      entries.push({
        ...entry,
        position: entry.kind === "subagent_status" ? "process" : entry.position,
        turn,
      });
    }
    chips.push(...display.subagentChips);
    hasAnswer = hasAnswer || display.hasAnswer;
    if (display.latestProcessPreview) {
      latestProcessPreview = display.latestProcessPreview;
    }
  }
  if (!lastDisplay) {
    return undefined;
  }
  return {
    entries,
    hasAnswer,
    subagentChips: chips,
    // Only the last member's missing-reply outcome matters: an
    // intermediate wake turn that produced no final answer simply
    // continued the orchestration.
    missingReplyMessage: lastDisplay.missingReplyMessage,
    latestProcessPreview,
  };
}

function isSpawnEntry(entry: TurnEntry): boolean {
  const items = entry.items ?? [entry.item];
  return (
    items.length > 0 &&
    items.every(
      (item) =>
        (item.type === "tool_call" || item.type === "collab_agent_tool_call") &&
        item.name === "spawn_agent",
    )
  );
}

function spawnBatchDisplay(
  total: number,
  finished: number,
  failed: boolean,
): SubagentChipDisplay {
  if (finished === 0) {
    return {
      label: translateCurrent("process.subagentsDispatched", { count: total }),
      outcome: "updated",
    };
  }
  if (finished < total) {
    return {
      label: translateCurrent("process.subagentsProgress", {
        finished,
        remaining: total - finished,
      }),
      outcome: "updated",
    };
  }
  return {
    label: translateCurrent("process.subagentsFinished", { count: total }),
    outcome: failed ? "failed" : "completed",
  };
}

function spawnIdentity(item: ThreadItem): { aliases: string[]; target: string } {
  const aliases: string[] = [];
  let target = "";
  for (const [raw, keys] of [
    [item.result, ["task_name", "name", "agent_id"]],
    [item.arguments, ["name", "description"]],
  ] as const) {
    if (!raw) continue;
    try {
      const value = JSON.parse(raw) as Record<string, unknown>;
      for (const key of keys) {
        if (typeof value[key] === "string" && value[key]) {
          aliases.push(value[key]);
          if (!target) target = value[key];
        }
      }
    } catch {
      // Malformed tool payloads still render with the generic subtask label.
    }
  }
  return { aliases, target };
}
