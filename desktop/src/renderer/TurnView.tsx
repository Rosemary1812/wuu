/// <reference path="../shared/jsx-compat.d.ts" />

import {
  AlertCircle,
  Archive,
  Brain,
  ChevronDown,
  Info,
  MessageCircle,
  ShieldCheck,
  Square,
} from "lucide-react";
import { Fragment, useEffect, useState } from "react";
import type { ThreadItem, Turn } from "../shared/protocol";
import {
  messageFlowFinalTextIndex,
  messageFlowStatusLabel,
} from "./message-flow-display";
import { agentHandoffDisplay } from "./AgentHandoff";
import {
  buildAssistantTurnDisplay,
  type AssistantTurnDisplay,
  type TurnProcessEntry,
} from "./AssistantTurnDisplay";
import { CollapsibleDetails } from "./CollapsibleMotion";
import { RichContent } from "./RichContent";
import {
  AgentMessageActions,
  MessageCopyButton,
  MessageFileList,
  MessageImageGrid,
} from "./MessageActions";
import {
  debugStreamFieldLength,
  latestDebugItem,
  parseTurnTimestampMs,
} from "./RunDebugPanel";
import { StreamingMarkdown } from "./StreamingMarkdown";
import { streamTextKey, streamTextStore } from "./StreamText";
import { streamFieldValue } from "./ThreadItemText";
import { ToolActivityRow } from "./ToolActivity";
import { formatDuration, useLiveNow } from "./TurnProgress";
import {
  isCancellationMessage,
  userFacingErrorForMessage,
  type UserFacingErrorAction,
  type UserFacingErrorDisplay,
} from "./UserFacingErrors";

type TurnProgressContent = {
  label: string;
  detail?: string;
};

// Anchor IDs used by the input-box query history popover to scroll
// back to a past user message. Kept as plain DOM ids (no hash routing
// involvement) so document.getElementById / scrollIntoView stay cheap.
function turnAnchorID(turnID: string): string {
  return `turn-${turnID}`;
}

function userMessageAnchorID(turnID: string, itemID: string): string {
  return `user-msg-${turnID}-${itemID}`;
}

function userMessageAnchorSelector(turnID: string, itemID: string): string {
  return `#${userMessageAnchorID(turnID, itemID)}`;
}

export function scrollToUserMessage(turnID: string, itemID: string): void {
  if (typeof document === "undefined") {
    return;
  }
  const node = document.querySelector<HTMLElement>(
    userMessageAnchorSelector(turnID, itemID),
  );
  if (!node) {
    return;
  }
  node.scrollIntoView({ behavior: "smooth", block: "start" });
}

function turnNoticeDisplay(
  turn: Turn,
  hasAssistantOutput: boolean,
): UserFacingErrorDisplay | undefined {
  const rawMessage = turn.error?.message;
  const baseDisplay =
    turn.status === "interrupted"
      ? userFacingErrorForMessage("context canceled", "turn")
      : isCancellationMessage((rawMessage ?? "").toLowerCase())
        ? userFacingErrorForMessage(rawMessage, "turn")
        : turn.status === "failed"
          ? userFacingErrorForMessage(rawMessage, "turn")
          : undefined;
  if (!baseDisplay) {
    return undefined;
  }
  if (baseDisplay.category === "cancelled") {
    return {
      ...baseDisplay,
      title: hasAssistantOutput ? "回复已中断" : "已停止",
      detail: hasAssistantOutput
        ? "已保留已生成内容，可以继续发送消息。"
        : "这次请求已停止，没有生成回复内容。",
    };
  }
  return {
    ...baseDisplay,
    detail: hasAssistantOutput
      ? `${baseDisplay.detail} 已保留已生成内容。`
      : baseDisplay.detail,
  };
}

function turnHasAssistantOutput(turn: Turn): boolean {
  return turn.items.some((item) => {
    if (item.type !== "agent_message") {
      return false;
    }
    return streamFieldValue(turn.id, item, "text").trim().length > 0;
  });
}

function TurnNotice({
  display,
  onAction,
}: {
  display: UserFacingErrorDisplay;
  /**
   * Called when the user activates a recommended action. The data
   * layer only declares what the action is; this callback decides
   * what the action does. If omitted, the action renders as a button
   * but does nothing — useful for first-render fallback only.
   */
  onAction?: (action: UserFacingErrorAction) => void;
}): JSX.Element {
  const Icon = turnNoticeIcon(display);
  const actions = display.recommendedActions;
  return (
    <aside
      className={`turn-notice ${display.tone}`}
      role={
        display.tone === "error" || display.tone === "auth" ? "alert" : "status"
      }
    >
      <span className="turn-notice-icon" aria-hidden="true">
        <Icon size={14} />
      </span>
      <span className="turn-notice-copy">
        <strong>{display.title}</strong>
        <span>{display.detail}</span>
        {actions.length > 0 ? (
          <span className="turn-notice-actions">
            {actions.map((action) => (
              <button
                key={action.kind}
                type="button"
                className={
                  action.variant === "secondary"
                    ? "turn-notice-action secondary"
                    : "turn-notice-action"
                }
                onClick={onAction ? () => onAction(action) : undefined}
              >
                {action.label}
              </button>
            ))}
          </span>
        ) : null}
      </span>
    </aside>
  );
}

function ContextCompactionNotice({ text }: { text?: string }): JSX.Element {
  const detail = contextCompactionDetail(text);
  return (
    <aside className="turn-notice context-compaction-notice" role="status">
      <span className="turn-notice-icon" aria-hidden="true">
        <Archive size={14} />
      </span>
      <span className="turn-notice-copy">
        <strong>上下文已压缩</strong>
        <span>{detail}</span>
      </span>
    </aside>
  );
}

function contextCompactionDetail(text?: string): string {
  const normalized = normalizeContextCompactionText(text);
  if (!normalized) {
    return "已整理较早对话，后续回复会继续沿用保留下来的关键信息。";
  }
  if (/^Compacted history$/i.test(normalized)) {
    return "已整理较早对话，后续回复会继续沿用保留下来的关键信息。";
  }
  const compactNotice = parseContextCompactionNotice(normalized);
  if (compactNotice) {
    return compactNotice;
  }
  return normalized.replace(/^上下文已压缩[:：]\s*/, "");
}

function normalizeContextCompactionText(text?: string): string {
  return (text ?? "").trim().replace(/^[✦*•]\s*/, "");
}

function parseContextCompactionNotice(text: string): string | undefined {
  const match = text.match(
    /^(Recovered from context overflow\s+[—-]\s+compacted|Compacted)\s+history:\s*(\d+)\s*(?:→|->)\s*(\d+)\s+messages(?:\s+\(was\s+~?([^)]+)\))?$/i,
  );
  if (!match) {
    return undefined;
  }
  const [, action, before, after, tokens] = match;
  const tokenDetail = tokens ? `，原约 ${tokens.trim()}` : "";
  if (/^Recovered/i.test(action)) {
    return `已从上下文超限中恢复：${before} 条消息整理为 ${after} 条${tokenDetail}。`;
  }
  return `已压缩较早上下文：${before} 条消息整理为 ${after} 条${tokenDetail}。`;
}

function turnNoticeIcon(display: UserFacingErrorDisplay): typeof AlertCircle {
  if (display.category === "cancelled") {
    return Square;
  }
  if (display.tone === "auth") {
    return ShieldCheck;
  }
  if (display.tone === "warning") {
    return Info;
  }
  return AlertCircle;
}

export function TurnView({
  turn,
  cwd,
  latestAgentMessageID,
  onStreamFrame,
  onForkMessage,
  onNoticeAction,
}: {
  turn: Turn;
  cwd?: string;
  latestAgentMessageID?: string;
  onStreamFrame: () => void;
  onForkMessage?: (turnID: string, itemID: string) => void;
  onNoticeAction: (action: UserFacingErrorAction) => void;
}): JSX.Element {
  const actionableAgentMessageID =
    turn.status === "completed"
      ? messageFlowAgentMessageItemID(turn)
      : undefined;

  function renderThreadItem(
    item: ThreadItem,
    streaming: boolean,
    pendingCompanionReasoning?: boolean,
  ): JSX.Element | null {
    return (
      <ThreadItemView
        key={item.id}
        turnID={turn.id}
        turnStatus={turn.status}
        item={item}
        cwd={cwd}
        streaming={streaming}
        pendingCompanionReasoning={pendingCompanionReasoning}
        actionableAgentMessageID={actionableAgentMessageID}
        latestAgentMessageID={latestAgentMessageID}
        onStreamFrame={onStreamFrame}
        onForkMessage={onForkMessage}
        onNoticeAction={onNoticeAction}
      />
    );
  }

  const userItems = turn.items.filter((item) => item.type === "user_message");
  const assistantDisplay = buildAssistantTurnDisplay(
    turn,
    actionableAgentMessageID,
    renderThreadItem,
  );
  const notice = turnNoticeDisplay(turn, turnHasAssistantOutput(turn));

  return (
    <section className="turn" id={turnAnchorID(turn.id)} data-turn-id={turn.id}>
      {userItems.map((item) => renderThreadItem(item, false))}
      {assistantDisplay ? (
        <AssistantTurnShell turn={turn} display={assistantDisplay} />
      ) : null}
      {notice ? <TurnNotice display={notice} onAction={onNoticeAction} /> : null}
    </section>
  );
}

function AssistantTurnShell({
  turn,
  display,
}: {
  turn: Turn;
  display: AssistantTurnDisplay;
}): JSX.Element {
  const hasFront = display.frontEntries.length > 0;
  const hasBody = display.finalAnswerItems.length > 0;
  const className = [
    "assistant-turn-shell",
    hasFront ? " has-front" : "",
    hasBody ? " has-body" : "",
    display.missingReplyMessage ? " missing-reply-turn" : "",
  ]
    .filter(Boolean)
    .join("");

  return (
    <div className={className}>
      {hasFront ? (
        <div className="assistant-turn-front">
          <TurnProcessGroup
            turn={turn}
            entries={display.frontEntries}
            defaultCollapsed={display.frontDefaultCollapsed}
            showTurnStatus
            latestCommentaryPreview={display.latestCommentaryPreview}
          />
        </div>
      ) : null}
      {display.showDivider ? <div className="answer-divider" aria-hidden /> : null}
      <div className="assistant-turn-final">
        {display.finalAnswerItems.map((answer) => (
          <Fragment key={answer.item.id}>{answer.element}</Fragment>
        ))}
      </div>
      {display.missingReplyMessage ? (
        <aside className="turn-notice warning assistant-turn-missing-reply" role="status">
          <span className="turn-notice-icon" aria-hidden="true">
            <Info size={14} />
          </span>
          <span className="turn-notice-copy">
            <strong>没有生成回复</strong>
            <span>{display.missingReplyMessage}</span>
          </span>
        </aside>
      ) : null}
    </div>
  );
}

function TurnProcessGroup({
  turn,
  entries,
  defaultCollapsed,
  showTurnStatus,
  latestCommentaryPreview,
}: {
  turn: Turn;
  entries: TurnProcessEntry[];
  defaultCollapsed: boolean;
  showTurnStatus: boolean;
  latestCommentaryPreview?: string;
}): JSX.Element {
  const [expanded, setExpanded] = useState(!defaultCollapsed);
  const detailsID = `${turn.id}-process-details`;
  const hasDetails = entries.length > 0;
  const hasPreview = Boolean(latestCommentaryPreview);
  const className = `turn-process-group${expanded ? " expanded" : " collapsed"}${
    hasDetails ? "" : " no-details"
  }${hasPreview ? " has-preview" : ""}`;
  const processCount = entries.reduce(
    (total, entry) => total + (entry.count ?? 1),
    0,
  );
  const completedDuration =
    typeof turn.duration_ms === "number" ? turn.duration_ms : undefined;
  const startedAt = parseTurnTimestampMs(turn.started_at);
  const liveDuration =
    showTurnStatus &&
    completedDuration === undefined &&
    turn.status === "in_progress" &&
    Number.isFinite(startedAt);
  const liveNow = useLiveNow(liveDuration);
  const elapsedMs =
    completedDuration ?? (liveDuration ? Math.max(0, liveNow - startedAt) : 0);
  const processLabel = showTurnStatus
    ? turnProgressContent(turn, elapsedMs, turnHasAssistantOutput(turn)).label
    : messageFlowStatusLabel({
        done: true,
        failed: turn.status === "failed",
        hasFinalText: turnHasAssistantOutput(turn),
        locale: "zh",
      });
  const metaParts = turnProcessMetaParts(
    turn,
    processCount,
    elapsedMs,
    showTurnStatus,
  );

  const toggleContent = (
    <>
      {/* Row 1 avatar slot: brain placeholder. Reserved for the
          future mascot character. */}
      <span className="turn-process-avatar" aria-hidden>
        <Brain size={15} />
      </span>
      {/* Row 2 avatar slot: speech-bubble placeholder. Only rendered
          when there's a live commentary preview, so a turn without
          commentary shows only the status row + brain slot. Both
          slots share col 1 of the grid below so the icons line up
          vertically with no indent between rows. */}
      {hasPreview ? (
        <span
          className="turn-process-avatar turn-process-avatar-secondary"
          aria-hidden
        >
          <MessageCircle size={15} />
        </span>
      ) : null}
      <span className="turn-process-header">
        <span className="turn-process-title">{processLabel}</span>
        {metaParts.map((part) => (
          <span className="turn-process-meta" key={part}>
            {part}
          </span>
        ))}
      </span>
      {hasPreview ? (
        <span
          className={`turn-process-preview${
            turn.status === "in_progress" ? " is-live" : ""
          }`}
        >
          <span className="turn-process-preview-text">
            {latestCommentaryPreview}
          </span>
        </span>
      ) : null}
      {hasDetails ? (
        <ChevronDown className="turn-process-chevron" size={15} />
      ) : null}
    </>
  );

  return (
    <div className={className}>
      {hasDetails ? (
        <button
          className="turn-process-toggle"
          type="button"
          aria-expanded={expanded}
          aria-controls={detailsID}
          onClick={() => setExpanded((open) => !open)}
        >
          {toggleContent}
        </button>
      ) : (
        <div className="turn-process-toggle turn-process-toggle-static">
          {toggleContent}
        </div>
      )}
      {hasDetails ? (
        <CollapsibleDetails
          className="turn-process-details"
          id={detailsID}
          expanded={expanded}
          innerClassName="turn-process-stack"
        >
          {entries.map((entry) => entry.element)}
        </CollapsibleDetails>
      ) : null}
    </div>
  );
}

function turnProcessMetaParts(
  turn: Turn,
  processCount: number,
  elapsedMs: number,
  showTurnStatus: boolean,
): string[] {
  const parts: string[] = [];
  if (showTurnStatus && turn.status === "in_progress") {
    parts.push(formatDuration(elapsedMs));
    return parts;
  }
  if (processCount > 0) {
    parts.push(`${processCount} 项`);
  }
  if (showTurnStatus && typeof turn.duration_ms === "number") {
    parts.push(formatDuration(turn.duration_ms));
  }
  return parts;
}

function TurnStatusLine({ turn }: { turn: Turn }): JSX.Element {
  const completedDuration =
    typeof turn.duration_ms === "number" ? turn.duration_ms : undefined;
  const startedAt = parseTurnTimestampMs(turn.started_at);
  const liveDuration =
    completedDuration === undefined &&
    turn.status === "in_progress" &&
    Number.isFinite(startedAt);
  const liveNow = useLiveNow(liveDuration);
  const elapsedMs =
    completedDuration ?? (liveDuration ? Math.max(0, liveNow - startedAt) : 0);
  const content = turnProgressContent(
    turn,
    elapsedMs,
    turnHasAssistantOutput(turn),
  );

  return (
    <div
      className={`turn-progress ${turn.status}`}
      role={liveDuration ? "status" : undefined}
      aria-live={liveDuration ? "polite" : undefined}
    >
      <span className="turn-progress-title">{content.label}</span>
    </div>
  );
}

function turnProgressContent(
  turn: Turn,
  elapsedMs: number,
  hasFinalText: boolean,
): TurnProgressContent {
  if (turn.status === "interrupted") {
    return { label: "已停止", detail: "这次请求已停止" };
  }
  if (turn.status !== "in_progress") {
    return {
      label: messageFlowStatusLabel({
        done: true,
        failed: turn.status === "failed",
        hasFinalText,
        locale: "zh",
      }),
    };
  }

  const runningTool = turn.items.find(
    (item) =>
      (item.type === "tool_call" || item.type === "collab_agent_tool_call") &&
      (item.status ?? "in_progress") === "in_progress",
  );
  if (runningTool) {
    return {
      label: messageFlowStatusLabel({
        done: false,
        failed: false,
        hasFinalText,
        locale: "zh",
      }),
    };
  }

  const latestItem = latestDebugItem(turn);
  if (!latestItem) {
    return {
      label: messageFlowStatusLabel({
        done: false,
        failed: false,
        hasFinalText,
        locale: "zh",
      }),
      detail: waitingDetail(elapsedMs, "已收到请求，正在等待模型回应"),
    };
  }
  if (latestItem.type === "agent_message") {
    const hasText =
      hasFinalText || debugStreamFieldLength(turn.id, latestItem, "text") > 0;
    return {
      label: messageFlowStatusLabel({
        done: false,
        failed: false,
        hasFinalText: hasText,
        locale: "zh",
      }),
      detail: hasText ? undefined : waitingDetail(elapsedMs, "正在组织回答"),
    };
  }
  if (latestItem.type === "reasoning") {
    return {
      label: messageFlowStatusLabel({
        done: false,
        failed: false,
        hasFinalText,
        locale: "zh",
      }),
      detail: waitingDetail(elapsedMs, "正在组织回答"),
    };
  }
  if (
    latestItem.type === "tool_call" ||
    latestItem.type === "collab_agent_tool_call"
  ) {
    return {
      label: messageFlowStatusLabel({
        done: false,
        failed: false,
        hasFinalText,
        locale: "zh",
      }),
    };
  }
  if (latestItem.type === "context_compaction") {
    return {
      label: messageFlowStatusLabel({
        done: false,
        failed: false,
        hasFinalText,
        locale: "zh",
      }),
    };
  }
  if (latestItem.type === "error") {
    return {
      label: messageFlowStatusLabel({
        done: false,
        failed: false,
        hasFinalText,
        locale: "zh",
      }),
    };
  }

  return {
    label: messageFlowStatusLabel({
      done: false,
      failed: false,
      hasFinalText,
      locale: "zh",
    }),
    detail: waitingDetail(elapsedMs, "请求正在处理中"),
  };
}

function waitingDetail(elapsedMs: number, defaultDetail: string): string {
  if (elapsedMs >= 30_000) {
    return "这个请求比平常更久，仍在等待响应";
  }
  if (elapsedMs >= 8_000) {
    return "请求已开始，正在继续处理";
  }
  return defaultDetail;
}

export function latestAgentMessageItemID(turns: Turn[]): string | undefined {
  for (let turnIndex = turns.length - 1; turnIndex >= 0; turnIndex--) {
    const itemID = latestAgentMessageItemIDForTurn(turns[turnIndex]);
    if (itemID) {
      return itemID;
    }
  }
  return undefined;
}

function latestAgentMessageItemIDForTurn(turn: Turn): string | undefined {
  for (let itemIndex = turn.items.length - 1; itemIndex >= 0; itemIndex--) {
    const item = turn.items[itemIndex];
    if (item.type === "agent_message") {
      return item.id;
    }
  }
  return undefined;
}

function messageFlowAgentMessageItemID(turn: Turn): string | undefined {
  const explicitFinalID = explicitFinalAgentMessageItemID(turn);
  if (explicitFinalID) {
    return explicitFinalID;
  }

  const finalIndex = messageFlowFinalTextIndex(turn.items, (item) => {
    if (item.type === "agent_message") {
      return streamFieldValue(turn.id, item, "text").trim().length > 0
        ? "text"
        : "ignore";
    }
    if (
      item.type === "reasoning" ||
      item.type === "tool_call" ||
      item.type === "collab_agent_tool_call" ||
      item.type === "context_compaction"
    ) {
      return "process";
    }
    return "ignore";
  });

  return finalIndex >= 0 ? turn.items[finalIndex]?.id : undefined;
}

function explicitFinalAgentMessageItemID(turn: Turn): string | undefined {
  for (let itemIndex = turn.items.length - 1; itemIndex >= 0; itemIndex--) {
    const item = turn.items[itemIndex];
    if (item.type !== "agent_message" || item.phase !== "final_answer") {
      continue;
    }
    if (streamFieldValue(turn.id, item, "text").trim().length > 0) {
      return item.id;
    }
  }
  return undefined;
}

function ThreadItemView({
  turnID,
  turnStatus,
  item,
  cwd,
  streaming,
  pendingCompanionReasoning,
  actionableAgentMessageID,
  latestAgentMessageID,
  onStreamFrame,
  onForkMessage,
  onNoticeAction,
}: {
  turnID: string;
  turnStatus: Turn["status"];
  item: ThreadItem;
  cwd?: string;
  streaming: boolean;
  pendingCompanionReasoning?: boolean;
  actionableAgentMessageID?: string;
  latestAgentMessageID?: string;
  onStreamFrame: () => void;
  onForkMessage?: (turnID: string, itemID: string) => void;
  onNoticeAction: (action: UserFacingErrorAction) => void;
}): JSX.Element | null {
  switch (item.type) {
    case "user_message": {
      const text = item.text ?? "";
      const handoff = agentHandoffDisplay(text);
      if (handoff) {
        return (
          <div className="agent-handoff-line" role="status">
            {handoff.label}
          </div>
        );
      }
      const copyable = text.trim() !== "";
      return (
        <div
          className={`user-message-block${copyable ? " user-message-block-with-actions" : ""}`}
          id={userMessageAnchorID(turnID, item.id)}
          data-user-message-id={item.id}
          data-turn-id={turnID}
        >
          <div className="message user-message">
            {item.images?.length ? (
              <MessageImageGrid images={item.images} />
            ) : null}
            {item.files?.length ? <MessageFileList files={item.files} /> : null}
            {text ? <RichContent text={text} cwd={cwd} /> : null}
          </div>
          {copyable ? (
            <div
              className="message-actions user-message-actions"
              aria-label="用户消息操作"
            >
              <MessageCopyButton
                getText={() => text}
                className="message-action-button"
                iconSize={15}
              />
            </div>
          ) : null}
        </div>
      );
    }
    case "agent_message": {
      const streamKeyValue = streamTextKey(turnID, item.id, "text");
      const agentText = streamTextStore.has(streamKeyValue)
        ? streamTextStore.get(streamKeyValue)
        : (item.text ?? "");
      const copyable = streaming || agentText.trim() !== "";
      const actionsVisible =
        turnStatus === "completed" &&
        item.id === actionableAgentMessageID &&
        copyable;
      const actionsPersistent =
        actionsVisible && item.id === latestAgentMessageID;
      const reserveActionSlot =
        copyable &&
        (streaming || actionsVisible || item.phase === "final_answer");
      return (
        <article
          className={`agent-block${
            reserveActionSlot
              ? ` agent-block-with-action-slot${actionsVisible ? " agent-actions-available" : ""}${actionsPersistent ? " agent-actions-persistent" : ""}`
              : ""
          }`}
        >
          <div className="agent-text">
            <AgentMessageContent
              turnID={turnID}
              item={item}
              cwd={cwd}
              streaming={streaming}
              pendingCompanionReasoning={pendingCompanionReasoning}
              onStreamFrame={onStreamFrame}
            />
          </div>
          {reserveActionSlot && actionsVisible ? (
            <AgentMessageActions
              getText={() => streamFieldValue(turnID, item, "text")}
              onFork={
                onForkMessage ? () => onForkMessage(turnID, item.id) : undefined
              }
            />
          ) : reserveActionSlot ? (
            <div
              className="message-actions agent-message-actions action-slot-placeholder"
              aria-hidden="true"
            />
          ) : null}
        </article>
      );
    }
    case "reasoning":
      return (
        <article className="reasoning-block">
          <ReasoningContent
            turnID={turnID}
            item={item}
            streaming={streaming}
            onStreamFrame={onStreamFrame}
          />
        </article>
      );
    case "tool_call":
    case "collab_agent_tool_call":
      return <ToolActivityRow items={[item]} />;
    case "context_compaction":
      return <ContextCompactionNotice text={item.text} />;
    case "error":
      return (
        <TurnNotice display={userFacingErrorForMessage(item.error, "turn")} onAction={onNoticeAction} />
      );
    default:
      return null;
  }
}

function AgentMessageContent({
  turnID,
  item,
  cwd,
  streaming,
  pendingCompanionReasoning,
  onStreamFrame,
}: {
  turnID: string;
  item: ThreadItem;
  cwd?: string;
  streaming: boolean;
  /**
   * True when the turn has a reasoning block that the model just finished
   * writing. The first answer item waits a short beat so the reasoning
   * cursor can fully settle before the text cursor starts animating.
   */
  pendingCompanionReasoning?: boolean;
  onStreamFrame: () => void;
}): JSX.Element {
  const streamKeyValue = streamTextKey(turnID, item.id, "text");
  const hasBufferedStream = streamTextStore.has(streamKeyValue);
  const [streamSettled, setStreamSettled] = useState(false);
  // Hold the cursor back when a just-completed reasoning block is still
  // visually settling. The reasoning and text streams are sequential on
  // the wire, but the StreamingMarkdown cursor's "settling" phase and the
  // next text's "streaming" phase can briefly race in the UI.
  const [cursorArmed, setCursorArmed] = useState<boolean>(
    !pendingCompanionReasoning,
  );
  useEffect(() => {
    if (!pendingCompanionReasoning) {
      setCursorArmed(true);
      return;
    }
    // 240ms is enough to let the reasoning cursor finish its tail reveal
    // (it's bound by max cps but typically clears in ~150ms for short
    // reasoning). Tuned by hand; bump up if you can still see overlap.
    const timer = window.setTimeout(() => {
      setCursorArmed(true);
    }, 240);
    return () => {
      window.clearTimeout(timer);
    };
  }, [pendingCompanionReasoning]);
  const liveStream =
    (streaming || hasBufferedStream) && !streamSettled && cursorArmed;

  useEffect(() => {
    setStreamSettled(false);
  }, [streamKeyValue]);

  return (
    <StreamingMarkdown
      streamKey={streamKeyValue}
      initialText={
        hasBufferedStream
          ? streamTextStore.seedValue(streamKeyValue)
          : item.text
      }
      cwd={cwd}
      final={!streaming}
      live={liveStream}
      textKind={item.phase === "commentary" ? "commentary" : "final_answer"}
      onFrame={onStreamFrame}
      onSettled={() => {
        setStreamSettled(true);
        streamTextStore.clearItem(turnID, item.id);
        onStreamFrame();
      }}
    />
  );
}

function ReasoningContent({
  turnID,
  item,
  streaming,
  onStreamFrame,
}: {
  turnID: string;
  item: ThreadItem;
  streaming: boolean;
  onStreamFrame: () => void;
}): JSX.Element {
  const streamKeyValue = streamTextKey(turnID, item.id, "text");
  const hasBufferedStream = streamTextStore.has(streamKeyValue);
  const liveStream = streaming || hasBufferedStream;

  return (
    <StreamingMarkdown
      streamKey={streamKeyValue}
      initialText={
        hasBufferedStream
          ? streamTextStore.seedValue(streamKeyValue)
          : item.text
      }
      className="streaming-markdown rich-content reasoning-stream"
      final={!streaming}
      live={liveStream}
      onFrame={onStreamFrame}
      onSettled={() => {
        streamTextStore.clearItem(turnID, item.id);
        onStreamFrame();
      }}
    />
  );
}
