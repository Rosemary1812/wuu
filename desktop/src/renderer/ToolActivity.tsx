import { ChevronDown } from "lucide-react";
import { useEffect, useState, type CSSProperties } from "react";
import type { ThreadItem } from "../shared/protocol";
import {
  activitySummaryText,
  buildToolActivitySections,
  summarizeToolActivity,
  type ToolActivitySection,
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
  collapseWhenIdle = false,
  revealItems = false,
}: {
  items: ThreadItem[];
  collapseWhenIdle?: boolean;
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

  if (items.length <= 1) {
    return (
      <ToolActivityRow items={items} collapseWhenIdle={collapseWhenIdle} />
    );
  }

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
          <ToolActivityRow
            items={[item]}
            collapseWhenIdle={collapseWhenIdle}
          />
        </div>
      ))}
    </div>
  );
}

export function ToolActivityRow({
  items,
  collapseWhenIdle = false,
}: {
  items: ThreadItem[];
  collapseWhenIdle?: boolean;
}): JSX.Element {
  const summary = summarizeToolActivity(items);
  const sections = buildToolActivitySections(items);
  const summaryText = activitySummaryText(sections, summary);
  const shouldExpandForStatus = summary.running || summary.failed;
  const [expanded, setExpanded] = useState(
    !collapseWhenIdle && shouldExpandForStatus,
  );
  const className = `activity-group${expanded ? " expanded" : ""}${summary.running ? " running" : ""}${
    summary.failed ? " failed" : ""
  }`;

  useEffect(() => {
    if (shouldExpandForStatus) {
      setExpanded(true);
      return;
    }
    if (collapseWhenIdle) {
      const timer = window.setTimeout(() => setExpanded(false), 140);
      return () => window.clearTimeout(timer);
    }
    return undefined;
  }, [collapseWhenIdle, shouldExpandForStatus]);

  return (
    <article className={className}>
      <button
        className="activity-row activity-toggle"
        type="button"
        aria-expanded={expanded}
        onClick={() => setExpanded((open) => !open)}
      >
        <span className="activity-copy">
          <span>{summaryText}</span>
          {summary.fileName ? (
            <span className="activity-file">{summary.fileName}</span>
          ) : null}
          {summary.additions > 0 ? (
            <span className="activity-add">+{summary.additions}</span>
          ) : null}
          {summary.deletions > 0 ? (
            <span className="activity-delete">-{summary.deletions}</span>
          ) : null}
        </span>
        <ChevronDown className="activity-chevron" size={13} />
      </button>
      <div className="activity-details" aria-hidden={!expanded}>
        <div className="activity-details-inner">
          {sections.map((section) => (
            <ToolActivitySectionView key={section.id} section={section} />
          ))}
        </div>
      </div>
    </article>
  );
}

function ToolActivitySectionView({
  section,
}: {
  section: ToolActivitySection;
}): JSX.Element {
  return (
    <section className="activity-detail">
      <div className="activity-detail-body">
        <div className="activity-command-list">
          {section.commands.map((command, index) => (
            <div
              className={`activity-command ${command.status}`}
              key={`${section.id}-${index}`}
            >
              {command.text}
            </div>
          ))}
        </div>
        {section.error ? (
          <div className="activity-detail-error">{section.error}</div>
        ) : null}
      </div>
    </section>
  );
}
