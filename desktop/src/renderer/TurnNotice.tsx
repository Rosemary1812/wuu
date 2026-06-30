import type { ThreadItemStatus } from "../shared/protocol";
import type { TurnEventDisplay } from "./TurnEvents";
import type { UserFacingErrorAction, UserFacingErrorDisplay } from "./UserFacingErrors";

export function TurnEventNotice({
  event,
  onAction,
}: {
  event: TurnEventDisplay;
  onAction?: (action: UserFacingErrorAction) => void;
}): JSX.Element {
  if (event.presentation === "context_compaction") {
    return <ContextCompactionNotice text={event.text} reason={event.reason} status={event.status} />;
  }
  return <TurnNotice display={event.notice} onAction={onAction} />;
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
  const actions = display.recommendedActions;
  // The inline notice is intentionally compact: the full explanation moves
  // to the `title` attribute so it is available on hover without taking a
  // second line, while recommended actions stay visible and clickable.
  const hoverText = display.detail
    ? `${display.title} — ${display.detail}`
    : display.title;
  return (
    <aside
      className={`turn-notice turn-event-notice ${display.tone}`}
      role={
        display.tone === "error" || display.tone === "auth" ? "alert" : "status"
      }
      aria-label={hoverText}
      title={hoverText}
    >
      <span className="turn-event-content">
        <strong className="turn-event-title">{display.title}</strong>
        <span className="turn-event-detail">{display.detail}</span>
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
  reason,
  status,
}: {
  text?: string;
  reason?: string;
  status?: ThreadItemStatus;
}): JSX.Element {
  // in_progress reuses the shared live-gray sweep used by active
  // process rows, reasoning labels, and previews. The host itself is a
  // centered compact label — no Archive icon, no detail copy. When the
  // item flips to completed the host swaps to the established icon + copy
  // layout.
  if (status === "in_progress") {
    return (
      <aside
        className="turn-notice turn-event-notice context-compaction-notice is-progress"
        role="status"
        aria-live="polite"
      >
        <span className="turn-event-content">
          <strong className="turn-event-title live-progress-chip">
          正在自动压缩上下文
          </strong>
        </span>
      </aside>
    );
  }
  const detail = contextCompactionDetail(text, reason);
  // The inline notice is a compact label. The full breakdown is moved to
  // the `title` attribute so it is available on hover without taking a
  // second visual line.
  return (
    <aside
      className="turn-notice turn-event-notice context-compaction-notice"
      role="status"
      aria-live="polite"
      title={detail}
    >
      <span className="turn-event-content">
        <strong className="turn-event-title">{contextCompactionTitle(text, reason)}</strong>
        <span className="turn-event-detail">{detail}</span>
      </span>
    </aside>
  );
}

function contextCompactionTitle(text?: string, reason?: string): string {
  const normalized = normalizeContextCompactionText(text);
  if (isFailedCompactNotice(normalized)) {
    return "上下文压缩失败";
  }
  if (isInceptionCompact(reason, normalized)) {
    return "已续接上下文";
  }
  if (isHelpMeCompact(reason, normalized)) {
    return "已合并求助结果";
  }
  return "上下文已压缩";
}

function contextCompactionDetail(text?: string, reason?: string): string {
  const normalized = normalizeContextCompactionText(text);
  if (!normalized) {
    return "已整理较早对话，后续回复会继续沿用保留下来的关键信息。";
  }
  if (isFailedCompactNotice(normalized)) {
    return "自动压缩没有完成，当前对话仍保留原上下文。";
  }
  if (isInceptionCompact(reason, normalized)) {
    return "已生成续接摘要，后续回复会沿用保留下来的任务状态。";
  }
  if (isHelpMeCompact(reason, normalized)) {
    return "已把 HelpMe 恢复结果整理进上下文，后续回复会沿用新的任务状态。";
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

function isFailedCompactNotice(text: string): boolean {
  return /^(?:Context compaction|Proactive compact|Context-overflow compact|Compact) failed\b/i.test(text);
}

function isInceptionCompact(reason: string | undefined, text: string): boolean {
  return reason === "inception" || /^Inception rewrote history\b/i.test(text);
}

function isHelpMeCompact(reason: string | undefined, text: string): boolean {
  return reason === "helpme" || /^HelpMe recovered and compacted history\b/i.test(text);
}

function parseContextCompactionNotice(text: string): string | undefined {
  const match = text.match(
    /^(Recovered from context overflow\s+[—-]\s+compacted|HelpMe recovered and compacted|Inception rewrote|Compacted)\s+history:\s*(\d+)\s*(?:→|->)\s*(\d+)\s+messages(?:\s+\(was\s+~?([^)]+)\))?$/i,
  );
  if (!match) {
    return undefined;
  }
  const [, , before, after, tokens] = match;
  const tokenDetail = tokens ? `，原约 ${tokens.trim()}` : "";
  return `已压缩较早上下文：${before} 条消息整理为 ${after} 条${tokenDetail}。`;
}
