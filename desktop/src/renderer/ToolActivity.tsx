import { type CSSProperties, useEffect, useState } from "react";
import type { ThreadItem } from "../shared/protocol";
import {
  buildToolActivitySections,
  summarizeToolActivity,
} from "./ToolActivityHelpers";
export type { JsonRecord } from "./ToolActivityHelpers";
export {
  isRecord,
  numberValue,
  readableToolActivityCommand,
  readableToolName,
  recordValue,
  stringValue,
} from "./ToolActivityHelpers";

const TOOL_ACTIVITY_REVEAL_INTERVAL_MS = 85;

export function ToolActivityTimeline({
  items,
  revealItems = false,
}: {
  items: ThreadItem[];
  revealItems?: boolean;
}): JSX.Element {
  const [visibleCount, setVisibleCount] = useState(() =>
    revealItems ? Math.min(1, items.length) : items.length,
  );
  const itemSignature = items.map((item) => item.id).join("\u0000");

  useEffect(() => {
    setVisibleCount((current) => {
      if (!revealItems) {
        return items.length;
      }
      if (items.length === 0) {
        return 0;
      }
      return Math.min(Math.max(current, 1), items.length);
    });
  }, [itemSignature, items.length, revealItems]);

  useEffect(() => {
    if (!revealItems || visibleCount >= items.length) {
      return undefined;
    }
    const timer = window.setTimeout(() => {
      setVisibleCount((current) => Math.min(current + 1, items.length));
    }, TOOL_ACTIVITY_REVEAL_INTERVAL_MS);
    return () => window.clearTimeout(timer);
  }, [itemSignature, items.length, revealItems, visibleCount]);

  return (
    <div
      className="activity-timeline"
      data-pending-count={Math.max(0, items.length - visibleCount)}
    >
      {items.slice(0, visibleCount).map((item, index) => (
        <div
          className="activity-timeline-item"
          key={item.id}
          style={{ "--activity-index": index } as CSSProperties}
        >
          <ToolActivityRow items={[item]} />
        </div>
      ))}
    </div>
  );
}

// A single tool activity row is now one line of plain prose plus, when
// there is an error, a single indented error line below it. We no longer
// render a separate collapsible "details" block: in nearly every case
// (list_files, read_file, grep, run_shell with a readable label) the
// toggle summary and the detail command text were the same string, so
// the previous toggle+details pair read as the same tool call shown
// twice. Consolidating to one line removes the duplication; errors get
// the only line below the summary, since they're the one piece of
// information that genuinely is not already in the summary.
export function ToolActivityRow({
  items,
}: {
  items: ThreadItem[];
}): JSX.Element {
  const summary = summarizeToolActivity(items);
  const sections = buildToolActivitySections(items);

  // Section detail wins over title: "查看 docs" is informative, while
  // "查看" alone reads as a fragment waiting for a target. For sections
  // that genuinely have no detail (e.g. "计划"), fall back to the title.
  // Multiple sections of the same row join with "，".
  const summaryText = sections
    .map((s) => s.detail || s.title)
    .filter(Boolean)
    .join("，");

  const errors = sections
    .map((s) => s.error)
    .filter((e): e is string => Boolean(e));

  const className = `activity-group${summary.running ? " running" : ""}${
    summary.failed ? " failed" : ""
  }`;

  return (
    <article className={className}>
      <span className="activity-row activity-summary">
        <span className="activity-copy">
          <span>{summaryText}</span>
          {summary.additions > 0 ? (
            <span className="activity-add">+{summary.additions}</span>
          ) : null}
          {summary.deletions > 0 ? (
            <span className="activity-delete">-{summary.deletions}</span>
          ) : null}
        </span>
      </span>
      {errors.length > 0 ? (
        <div className="activity-errors">
          {errors.map((message, index) => (
            <div className="activity-detail-error" key={`error-${index}`}>
              {message}
            </div>
          ))}
        </div>
      ) : null}
    </article>
  );
}