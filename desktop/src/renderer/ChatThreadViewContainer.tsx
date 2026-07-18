import { useMemo, type JSX } from "react";
import type {
  ConversationSubthread,
  MessageMarkWire,
  ThreadItem,
  Turn,
} from "../shared/protocol";
import type { TurnStreamStatus } from "./AppState";
import { ChatThreadView } from "./ChatThreadView";
import type { QueuedComposerMessage } from "./ComposerMessages";
import { aggregateMarksBySeq } from "./MessageMarks";
import type { TurnEventDisplay } from "./TurnEvents";
import { useThreadMarks } from "./useThreadMarks";

// Feeds ChatThreadView its live read receipts + reactions. Split out because
// the ChatThreadView render site sits inside a memoized .map (where a hook
// can't go); this container is a stable component, so useThreadMarks lives at
// a valid top level, and ChatThreadView itself stays pure and testable.
export function ChatThreadViewContainer({
  threadID,
  turns,
  cwd,
  marks,
  pendingMessages,
  busyParticipantIDs,
  readerCount,
  resolveParticipantName,
  threadOwnerCandidates,
  subthreadsByAnchor,
  isActive,
  streamStatus,
  turnEvents,
  onOpenSubthread,
  onReact,
}: {
  threadID: string;
  turns: ReadonlyArray<Pick<Turn, "id" | "items">>;
  cwd?: string;
  marks?: readonly MessageMarkWire[];
  pendingMessages?: ReadonlyArray<QueuedComposerMessage>;
  busyParticipantIDs?: ReadonlySet<string>;
  readerCount?: number;
  resolveParticipantName?: (id: string) => string;
  threadOwnerCandidates?: ReadonlyArray<
    import("../shared/protocol").ParticipantSummary
  >;
  subthreadsByAnchor?: ReadonlyMap<string, ConversationSubthread>;
  isActive?: boolean;
  streamStatus?: TurnStreamStatus;
  turnEvents?: ReadonlyArray<{ turnID: string; event: TurnEventDisplay }>;
  onOpenSubthread?: (
    item: ThreadItem,
    threadOwnerParticipantID?: string,
    existingSubthreadID?: string,
  ) => void;
  onReact?: (item: ThreadItem, reaction: string) => void;
}): JSX.Element {
  const loadedMarksBySeq = useThreadMarks(threadID, marks === undefined);
  const providedMarksBySeq = useMemo(
    () => (marks ? aggregateMarksBySeq(marks) : undefined),
    [marks],
  );
  const marksBySeq = providedMarksBySeq ?? loadedMarksBySeq;
  return (
    <ChatThreadView
      turns={turns}
      cwd={cwd}
      pendingMessages={pendingMessages}
      busyParticipantIDs={busyParticipantIDs}
      marksBySeq={marksBySeq}
      readerCount={readerCount}
      resolveParticipantName={resolveParticipantName}
      threadOwnerCandidates={threadOwnerCandidates}
      subthreadsByAnchor={subthreadsByAnchor}
      isActive={isActive}
      streamStatus={streamStatus}
      turnEvents={turnEvents}
      onOpenSubthread={onOpenSubthread}
      onReact={onReact}
    />
  );
}
