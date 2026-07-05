import type { JSX } from "react";
import type {
  ConversationSubthread,
  ThreadItem,
  Turn,
} from "../shared/protocol";
import { ChatThreadView } from "./ChatThreadView";
import type { QueuedComposerMessage } from "./ComposerMessages";
import { useThreadMarks } from "./useThreadMarks";

// Feeds ChatThreadView its live read receipts + reactions. Split out because
// the ChatThreadView render site sits inside a memoized .map (where a hook
// can't go); this container is a stable component, so useThreadMarks lives at
// a valid top level, and ChatThreadView itself stays pure and testable.
export function ChatThreadViewContainer({
  threadID,
  turns,
  pendingMessages,
  busyParticipantIDs,
  readerCount,
  resolveParticipantName,
  subthreadsByAnchor,
  onOpenSubthread,
  onReact,
}: {
  threadID: string;
  turns: ReadonlyArray<Pick<Turn, "id" | "items">>;
  pendingMessages?: ReadonlyArray<QueuedComposerMessage>;
  busyParticipantIDs?: ReadonlySet<string>;
  readerCount?: number;
  resolveParticipantName?: (id: string) => string;
  subthreadsByAnchor?: ReadonlyMap<string, ConversationSubthread>;
  onOpenSubthread?: (item: ThreadItem) => void;
  onReact?: (item: ThreadItem, reaction: string) => void;
}): JSX.Element {
  const marksBySeq = useThreadMarks(threadID, true);
  return (
    <ChatThreadView
      turns={turns}
      pendingMessages={pendingMessages}
      busyParticipantIDs={busyParticipantIDs}
      marksBySeq={marksBySeq}
      readerCount={readerCount}
      resolveParticipantName={resolveParticipantName}
      subthreadsByAnchor={subthreadsByAnchor}
      onOpenSubthread={onOpenSubthread}
      onReact={onReact}
    />
  );
}
