import { ChevronDown, ChevronRight, Mail } from "lucide-react";
import { useState } from "react";
import type { EnvelopeMeta } from "../shared/protocol";

/**
 * Collapsed meta row for envelope-injected user messages in a resident
 * agent's DM thread (docs/plans/2026-07-03-resident-named-agents.md
 * §7.3). These messages were routed in from a group thread, so showing
 * them as user bubbles would read as if the user typed them. The row
 * stays on the meta layer (--ink-muted) and expands to the raw
 * envelope text on demand.
 */
export function EnvelopeNotice({
  meta,
  text,
}: {
  meta: EnvelopeMeta;
  text: string;
}): JSX.Element {
  const [expanded, setExpanded] = useState(false);
  const title = meta.source_thread_title?.trim();
  const source = title ? `「${title}」` : "其他会话";
  const count = meta.message_count;
  const label =
    typeof count === "number" && count > 0
      ? `收到来自${source}的 ${count} 条消息`
      : `收到来自${source}的消息`;
  return (
    <div className="envelope-notice">
      <button
        type="button"
        className="envelope-notice-toggle"
        aria-expanded={expanded}
        onClick={() => setExpanded((previous) => !previous)}
      >
        <Mail className="envelope-notice-icon" aria-hidden="true" />
        <span>{label}</span>
        {expanded ? (
          <ChevronDown className="envelope-notice-chevron" aria-hidden="true" />
        ) : (
          <ChevronRight className="envelope-notice-chevron" aria-hidden="true" />
        )}
      </button>
      {expanded ? <div className="envelope-notice-body">{text}</div> : null}
    </div>
  );
}
