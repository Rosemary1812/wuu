import type { ThreadItemStatus } from "../shared/protocol";
import type { TurnEventDisplay } from "./TurnEvents";
import type { UserFacingErrorDisplay, UserFacingErrorTone } from "./UserFacingErrors";
import { translateCurrent as t } from "./i18n";

export type SystemEventDisplay = {
  label: string;
  detail?: string;
  tone?: UserFacingErrorTone;
  state?: "settled" | "in_progress";
};

export function SystemEventNotice({
  event,
  className,
}: {
  event: SystemEventDisplay;
  className?: string;
}): JSX.Element {
  const tone = event.tone ?? "neutral";
  const inProgress = event.state === "in_progress";
  const description = event.detail
    ? `${event.label} — ${event.detail}`
    : event.label;
  return (
    <aside
      className={`turn-notice turn-event-notice ${tone}${inProgress ? " is-progress" : ""}${className ? ` ${className}` : ""}`}
      role={tone === "error" || tone === "auth" ? "alert" : "status"}
      aria-label={description}
      aria-live={inProgress ? "polite" : undefined}
      title={description}
    >
      <span className="turn-event-content">
        <strong
          className={`turn-event-title${inProgress ? " live-progress-chip" : ""}`}
        >
          {event.label}
        </strong>
      </span>
    </aside>
  );
}

export function SystemEventDivider({
  text,
  className,
}: {
  text: string;
  className?: string;
}): JSX.Element {
  return <SystemEventNotice event={{ label: text }} className={className} />;
}

export function TurnEventNotice({
  event,
}: {
  event: TurnEventDisplay;
}): JSX.Element {
  if (event.presentation === "context_compaction") {
    return <ContextCompactionNotice text={event.text} reason={event.reason} status={event.status} />;
  }
  return <TurnNotice display={event.notice} />;
}

export function TurnNotice({
  display,
}: {
  display: UserFacingErrorDisplay;
}): JSX.Element {
  return (
    <SystemEventNotice
      event={{
        label: display.title,
        detail: display.detail,
        tone: display.tone,
      }}
    />
  );
}

export function StreamReconnectNotice({
  text,
}: {
  text: string;
}): JSX.Element {
  return (
    <SystemEventNotice
      event={{ label: text, state: "in_progress" }}
      className="context-compaction-notice"
    />
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
    const title = contextCompactionProgressTitle(text, reason);
    return (
      <SystemEventNotice
        event={{ label: title, state: "in_progress" }}
        className="context-compaction-notice"
      />
    );
  }
  const detail = contextCompactionDetail(text, reason, status);
  // The inline notice is a compact label. The full breakdown is moved to
  // the `title` attribute so it is available on hover without taking a
  // second visual line.
  const label = contextCompactionTitle(text, reason, status);
  const normalized = normalizeContextCompactionText(text);
  const tone = status === "failed" || isFailedCompactNotice(normalized) ? "error" : "neutral";
  return (
    <SystemEventNotice
      event={{ label, detail, tone }}
      className="context-compaction-notice"
    />
  );
}

function contextCompactionProgressTitle(text?: string, reason?: string): string {
  if (isManualCompact(reason, normalizeContextCompactionText(text))) {
    return t("compaction.compacting");
  }
  const normalized = normalizeContextCompactionText(text);
  return normalized || t("compaction.autoCompacting");
}

function contextCompactionTitle(
  text?: string,
  reason?: string,
  status?: ThreadItemStatus,
): string {
  const normalized = normalizeContextCompactionText(text);
  if (status === "failed") {
    return t("compaction.failed");
  }
  if (isFailedCompactNotice(normalized)) {
    return t("compaction.failed");
  }
  if (isUnchangedCompactNotice(normalized)) {
    return t("compaction.notNeeded");
  }
  if (isHelpMeCompact(reason, normalized)) {
    return t("compaction.helpmeComplete");
  }
  if (isManualCompact(reason, normalized)) {
    return t("compaction.manualComplete");
  }
  return t("compaction.complete");
}

function contextCompactionDetail(
  text?: string,
  reason?: string,
  status?: ThreadItemStatus,
): string {
  const normalized = normalizeContextCompactionText(text);
  if (status === "failed") {
    return t("compaction.failedDetail");
  }
  if (!normalized) {
    return t("compaction.completeDetail");
  }
  if (isFailedCompactNotice(normalized)) {
    return t("compaction.failedDetail");
  }
  if (isUnchangedCompactNotice(normalized)) {
    return t("compaction.notNeededDetail");
  }
  if (isHelpMeCompact(reason, normalized)) {
    return t("compaction.helpmeDetail");
  }
  if (/^Compacted history$/i.test(normalized)) {
    return t("compaction.completeDetail");
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
  return /^(?:Manual context compaction|Context compaction|Proactive compact|Context-overflow compact|Compact) failed\b/i.test(
    text,
  );
}

function isUnchangedCompactNotice(text: string): boolean {
  return /^Nothing to compact yet\b/i.test(text);
}

function isManualCompact(reason: string | undefined, text: string): boolean {
  return (
    reason === "manual" ||
    /^Manual(?:ly)?\s+(?:context\s+)?compact/i.test(text)
  );
}

function isHelpMeCompact(reason: string | undefined, text: string): boolean {
  return reason === "helpme" || /^HelpMe recovered and compacted history\b/i.test(text);
}

function parseContextCompactionNotice(text: string): string | undefined {
  const match = text.match(
    /^(Recovered from context overflow\s+[—-]\s+compacted|HelpMe recovered and compacted|Manually compacted|Compacted)\s+history:\s*(\d+)\s*(?:→|->)\s*(\d+)\s+messages(?:\s+\(was\s+~?([^)]+)\))?$/i,
  );
  if (!match) {
    return undefined;
  }
  const [, , before, after, tokens] = match;
  return tokens
    ? t("compaction.summaryWithTokens", { before, after, tokens: tokens.trim() })
    : t("compaction.summary", { before, after });
}
