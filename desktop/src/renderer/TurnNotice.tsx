import { AlertCircle, Archive, Info, ShieldCheck, Square } from "lucide-react";
import type { ThreadItemStatus, Turn } from "../shared/protocol";
import {
  isCancellationMessage,
  userFacingErrorForMessage,
  type UserFacingErrorAction,
  type UserFacingErrorDisplay,
} from "./UserFacingErrors";

export function turnNoticeDisplay(
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

export function TurnNotice({
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
        <Icon className="icon-sm" />
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

export function ContextCompactionNotice({
  text,
  status,
}: {
  text?: string;
  status?: ThreadItemStatus;
}): JSX.Element {
  // in_progress reuses the shared live-gray sweep used by active
  // process rows, reasoning labels, and previews. The host itself is a
  // centered label flanked by two fading dividers — no Archive icon, no
  // detail copy. When the item flips to completed the host swaps to the
  // established icon + copy layout.
  if (status === "in_progress") {
    return (
      <aside
        className="turn-notice context-compaction-notice is-compacting"
        role="status"
        aria-live="polite"
      >
        <span className="context-compaction-compacting-text">
          正在自动压缩上下文
        </span>
      </aside>
    );
  }
  const detail = contextCompactionDetail(text);
  return (
    <aside
      className="turn-notice context-compaction-notice"
      role="status"
      aria-live="polite"
    >
      <span className="turn-notice-icon" aria-hidden="true">
        <Archive className="icon-sm" />
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
