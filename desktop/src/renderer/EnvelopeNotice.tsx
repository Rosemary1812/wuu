import { ChevronDown, ChevronRight } from "lucide-react";
import { useState } from "react";
import type { EnvelopeMeta } from "../shared/protocol";
import { useI18n } from "./i18n";

/**
 * Collapsed meta row for envelope-injected user messages in a resident
 * agent's DM thread (docs/plans/2026-07-03-resident-named-agents.md
 * §7.3). These messages were routed in from a group thread, so showing
 * them as user bubbles would read as if the user typed them. The row
 * stays on the meta layer (--ink-muted) and expands to the raw
 * envelope text on demand.
 *
 * Two structural envelope facts are surfaced inline (consistency plan
 * 2026-07-04 §1 #12): a "点名" badge when any coalesced record is
 * addressed (the sender @-mentioned this agent in the source thread),
 * and a grey "转发×N" marker when the message arrived through resident
 * relays rather than straight from the source thread (hop 0 = direct
 * user message; each resident re-broadcast increments hop).
 */
export function EnvelopeNotice({
  meta,
  text,
}: {
  meta: EnvelopeMeta;
  text: string;
}): JSX.Element {
  const { t } = useI18n();
  const [expanded, setExpanded] = useState(false);
  const records = meta;
  const titles = [
    ...new Set(
      records
        .map((record) => record.source_thread_title?.trim() ?? "")
        .filter((candidate) => candidate !== ""),
    ),
  ];
  const source =
    titles.length === 1
      ? t("envelope.namedSource", { title: titles[0] })
      : titles.length > 1
        ? t("envelope.multipleConversations")
        : t("envelope.otherConversation");
  const label =
    records.length > 1
      ? t("envelope.messagesReceived", { source, count: records.length })
      : t("envelope.messageReceived", { source });
  const addressed = records.some((record) => record.addressed === true);
  const maxHop = records.reduce(
    (max, record) => Math.max(max, record.hop ?? 0),
    0,
  );
  return (
    <div className="envelope-notice">
      <button
        type="button"
        className="chat-inline-divider envelope-notice-toggle"
        aria-expanded={expanded}
        aria-label={t("envelope.toggle", { label, action: expanded ? t("common.collapse") : t("common.expand") })}
        onClick={() => setExpanded((previous) => !previous)}
      >
        <span className="chat-inline-divider-label envelope-notice-label">
          <span className="envelope-notice-title">{label}</span>
          {addressed ? (
            <span className="envelope-notice-addressed">{t("envelope.addressed")}</span>
          ) : null}
          {maxHop > 0 ? (
            <span className="envelope-notice-hop">{t("envelope.forwarded", { count: maxHop })}</span>
          ) : null}
          {expanded ? (
            <ChevronDown className="envelope-notice-chevron" aria-hidden="true" />
          ) : (
            <ChevronRight className="envelope-notice-chevron" aria-hidden="true" />
          )}
        </span>
      </button>
      {expanded ? <div className="envelope-notice-body">{text}</div> : null}
    </div>
  );
}
