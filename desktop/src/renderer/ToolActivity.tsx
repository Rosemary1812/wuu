import { type CSSProperties, useEffect, useState } from "react";
import type { ThreadItem } from "../shared/protocol";
import { LightweightStreamingText } from "./LightweightStreamingText";
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
  streaming = false,
}: {
  items: ThreadItem[];
  revealItems?: boolean;
  /**
   * When true, in-progress tool rows fake-stream their summary line at
   * a deliberate cadence. Flips to false the moment an agent_message in
   * the same turn starts streaming so the user's eye is not held on a
   * still-filling title while the body text rushes past underneath.
   */
  streaming?: boolean;
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
          <ToolActivityRow items={[item]} streaming={streaming} />
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
  streaming = false,
}: {
  items: ThreadItem[];
  /**
   * Drives the fake-stream on the summary text. When true, the summary
   * reveals progressively at a deliberate cadence; when false it snaps
   * to the full text. The catch-up signal (turn has an in-progress
   * agent_message) flips this off so the user's eye follows the body
   * text rather than a still-filling title above it.
   */
  streaming?: boolean;
}): JSX.Element {
  const summary = summarizeToolActivity(items);
  const sections = buildToolActivitySections(items);

  // Each section carries both an action verb (title) and a target (detail).
  // Concatenate them so the rendered row reads as "动词 目标" — without
  // this, taking detail alone would drop the verb and surface a bare file
  // name with no hint of what was done to it. Sections without a detail
  // (e.g. "计划") fall back to the title alone. Multiple sections in the
  // same row join with "，".
  const summaryText = sections
    .map((s) => {
      if (s.detail && s.title) return `${s.title} ${s.detail}`;
      return s.detail || s.title;
    })
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
          <LightweightStreamingText
            text={summaryText}
            live={streaming ?? false}
          />
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