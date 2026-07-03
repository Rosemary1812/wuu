import { useEffect, useMemo, useRef } from "react";
import type { ParticipantSummary, Turn } from "../shared/protocol";
import { chatMessagesFromTurns, type ChatMessageRow } from "./AppState";
import { EnvelopeNotice } from "./EnvelopeNotice";
import { RichContent } from "./RichContent";

// Distance (px) from the bottom of the scroll container within which the
// view still counts as "at the bottom" and should auto-follow new rows.
const AUTO_FOLLOW_THRESHOLD_PX = 120;

/**
 * Chat-style message stream for DM and group threads
 * (chat-style-threads-design.md §2, §4). Renders exactly the whitelist
 * produced by chatMessagesFromTurns — user messages, envelope meta rows,
 * and tool-posted participant messages — never the agent's working
 * transcript (thinking, tool calls, plans, final-answer prose).
 */
export function ChatThreadView({
  turns,
  typingParticipants,
}: {
  turns: ReadonlyArray<Pick<Turn, "id" | "items">>;
  typingParticipants: ReadonlyArray<ParticipantSummary>;
}): JSX.Element {
  const rows = useMemo(() => chatMessagesFromTurns(turns), [turns]);
  const containerRef = useRef<HTMLDivElement | null>(null);
  const rowCount = rows.length + typingParticipants.length;

  useEffect(() => {
    const container = containerRef.current;
    if (!container) {
      return;
    }
    const distanceFromBottom =
      container.scrollHeight - container.scrollTop - container.clientHeight;
    if (distanceFromBottom <= AUTO_FOLLOW_THRESHOLD_PX) {
      container.scrollTop = container.scrollHeight;
    }
    // rowCount changes whenever a message or typing indicator is added or
    // removed; that is the only signal that should trigger auto-follow.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rowCount]);

  return (
    <div className="chat-thread" ref={containerRef}>
      {rows.map((row) => (
        <ChatRow key={row.id} row={row} />
      ))}
      {typingParticipants.map((participant) => (
        <ChatTypingRow key={`typing:${participant.id}`} participant={participant} />
      ))}
    </div>
  );
}

function ChatRow({ row }: { row: ChatMessageRow }): JSX.Element {
  if (row.kind === "envelope") {
    return (
      <div className="chat-row chat-row--envelope">
        <EnvelopeNotice meta={row.item.envelope_meta ?? []} text={row.item.text ?? ""} />
      </div>
    );
  }
  if (row.kind === "user") {
    return (
      <div className="chat-row chat-row--user">
        <div className="chat-bubble-group">
          <div className="chat-bubble chat-bubble--user">
            {row.item.text ? (
              <RichContent text={row.item.text} />
            ) : null}
          </div>
        </div>
      </div>
    );
  }
  // participant
  const postKind = row.item.post_kind ?? "result";
  const participant = row.item.participant;
  const name = participant?.name?.trim() || "参与者";
  if (postKind === "decline") {
    const text = (row.item.text ?? "").trim();
    return (
      <div className="chat-row chat-row--decline">
        <div className="chat-decline-line">
          {name} 认为无需回应{text ? `：${text}` : ""}
        </div>
      </div>
    );
  }
  return (
    <div className="chat-row chat-row--participant">
      <ChatAvatar participant={participant} />
      <div className="chat-bubble-group">
        <div className="chat-sender-name">{name}</div>
        <div className="chat-bubble">
          {row.item.text ? <RichContent text={row.item.text} /> : null}
        </div>
      </div>
    </div>
  );
}

function ChatAvatar({
  participant,
}: {
  participant: ParticipantSummary | undefined;
}): JSX.Element {
  const avatarImage = participant?.avatar_image?.trim();
  const emoji = participant?.avatar?.trim();
  const name = participant?.name?.trim() || "参与者";
  return (
    <div className="chat-avatar" aria-hidden="true">
      {avatarImage ? (
        <img src={avatarImage} alt="" />
      ) : emoji ? (
        emoji
      ) : (
        name.charAt(0)
      )}
    </div>
  );
}

function ChatTypingRow({
  participant,
}: {
  participant: ParticipantSummary;
}): JSX.Element {
  const name = participant.name.trim() || "参与者";
  return (
    <div className="chat-row chat-row--participant chat-typing-row" aria-label={`${name} 正在输入`}>
      <ChatAvatar participant={participant} />
      <div className="chat-bubble-group">
        <div className="chat-sender-name">{name}</div>
        <div className="chat-bubble chat-typing-bubble">
          <span className="chat-typing-dot" />
          <span className="chat-typing-dot" />
          <span className="chat-typing-dot" />
        </div>
      </div>
    </div>
  );
}
