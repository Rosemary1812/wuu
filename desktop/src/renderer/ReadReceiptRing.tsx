import type { JSX } from "react";
import type { RingModel, SeenAggregate } from "./MessageMarks";

// A compact read-receipt ring (Feishu-style): a gray track that fills blue as
// members finish reading; a distinct segment renders for members whose turn
// failed, so a ring that never completes reads as "someone crashed" rather
// than "still working". Hover shows the per-member breakdown.
// 2026-07-04-read-receipts-and-reactions.md §6.

const SIZE = 16;
const STROKE = 2.5;
const RADIUS = (SIZE - STROKE) / 2;
const CIRC = 2 * Math.PI * RADIUS;

function names(ids: string[], resolveName?: (id: string) => string): string {
  return ids.map((id) => resolveName?.(id) ?? id).join("、");
}

export function ringTitle(
  ring: RingModel,
  seen: SeenAggregate,
  resolveName?: (id: string) => string,
): string {
  const parts = [`已读 ${ring.completed}/${ring.total}`];
  if (ring.completed > 0) parts[0] += `：${names(seen.completed, resolveName)}`;
  if (ring.inProgress > 0) parts.push(`处理中：${names(seen.inProgress, resolveName)}`);
  if (ring.failed > 0) parts.push(`未完成：${names(seen.failed, resolveName)}`);
  return parts.join(" · ");
}

export function ReadReceiptRing({
  ring,
  seen,
  resolveName,
}: {
  ring: RingModel;
  seen: SeenAggregate;
  resolveName?: (id: string) => string;
}): JSX.Element | null {
  if (ring.total <= 0) {
    return null;
  }
  const done = ring.fraction * CIRC;
  const failed = ring.total > 0 ? (ring.failed / ring.total) * CIRC : 0;
  const title = ringTitle(ring, seen, resolveName);
  const center = SIZE / 2;
  return (
    <span
      className="chat-receipt-ring"
      data-all-seen={ring.allSeen ? "true" : undefined}
      data-failed={ring.anyFailed ? "true" : undefined}
      title={title}
      role="img"
      aria-label={title}
    >
      <svg width={SIZE} height={SIZE} viewBox={`0 0 ${SIZE} ${SIZE}`} aria-hidden="true">
        <circle
          className="chat-receipt-ring-track"
          cx={center}
          cy={center}
          r={RADIUS}
          strokeWidth={STROKE}
          fill="none"
        />
        {done > 0 ? (
          <circle
            className="chat-receipt-ring-done"
            cx={center}
            cy={center}
            r={RADIUS}
            strokeWidth={STROKE}
            fill="none"
            strokeLinecap="round"
            strokeDasharray={`${done} ${CIRC}`}
            transform={`rotate(-90 ${center} ${center})`}
          />
        ) : null}
        {failed > 0 ? (
          <circle
            className="chat-receipt-ring-failed"
            cx={center}
            cy={center}
            r={RADIUS}
            strokeWidth={STROKE}
            fill="none"
            strokeDasharray={`${failed} ${CIRC}`}
            strokeDashoffset={`-${done}`}
            transform={`rotate(-90 ${center} ${center})`}
          />
        ) : null}
      </svg>
    </span>
  );
}
