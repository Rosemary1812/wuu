import { ChevronDown, ChevronUp } from "lucide-react";
import { type ReactNode, useEffect, useMemo, useState } from "react";
import type { ThreadItem, Turn } from "../shared/protocol";
import { useI18n } from "./i18n";

interface WorkspaceDocumentTurnDockProps {
  children: ReactNode;
  statusText?: string;
  turns: Turn[];
}

function latestUserItem(turn: Turn): ThreadItem | undefined {
  for (let index = turn.items.length - 1; index >= 0; index -= 1) {
    const item = turn.items[index];
    if (item.type === "user_message") {
      return item;
    }
  }
  return undefined;
}

function latestAgentText(turn: Turn): string | undefined {
  for (let index = turn.items.length - 1; index >= 0; index -= 1) {
    const item = turn.items[index];
    if (item.type === "agent_message" && item.text?.trim()) {
      return item.text.trim();
    }
  }
  return undefined;
}

function latestUserTurn(turns: Turn[]): Turn | undefined {
  for (let index = turns.length - 1; index >= 0; index -= 1) {
    const turn = turns[index];
    if (latestUserItem(turn)) {
      return turn;
    }
  }
  return undefined;
}

export function WorkspaceDocumentTurnDock({
  children,
  statusText,
  turns,
}: WorkspaceDocumentTurnDockProps): JSX.Element {
  const { t } = useI18n();
  const turn = useMemo(() => latestUserTurn(turns), [turns]);
  const [expanded, setExpanded] = useState(false);

  useEffect(() => {
    setExpanded(false);
  }, [turn?.id]);

  const userItem = turn ? latestUserItem(turn) : undefined;
  const userText =
    userItem?.text?.trim() ||
    (userItem?.images?.length || userItem?.files?.length
      ? t("workspace.documentTurn.attachments")
      : undefined);
  const agentText = turn ? latestAgentText(turn) : undefined;

  if (!turn || !userText) {
    return <div className="workspace-document-turn-dock">{children}</div>;
  }

  const status =
    turn.status === "in_progress"
      ? statusText || t("workspace.documentTurn.running")
      : turn.status === "failed"
        ? t("workspace.documentTurn.failed")
        : turn.status === "interrupted"
          ? t("workspace.documentTurn.interrupted")
          : t("workspace.documentTurn.completed");
  const toggleLabel = expanded
    ? t("workspace.documentTurn.collapse")
    : t("workspace.documentTurn.expand");
  const detailsID = `workspace-document-turn-${turn.id}`;

  return (
    <div className="workspace-document-turn-dock">
      <section
        className={`workspace-document-turn-drawer${expanded ? " expanded" : ""}`}
        data-testid="workspace-document-turn-drawer"
      >
        <button
          type="button"
          className="workspace-document-turn-summary"
          aria-controls={detailsID}
          aria-expanded={expanded}
          aria-label={toggleLabel}
          onClick={() => setExpanded((current) => !current)}
        >
          <span
            className={`workspace-document-turn-status ${turn.status}`}
            aria-hidden="true"
          />
          <span className="workspace-document-turn-status-text">{status}</span>
          <span className="workspace-document-turn-prompt">{userText}</span>
          {expanded ? <ChevronDown size={15} /> : <ChevronUp size={15} />}
        </button>
        {expanded ? (
          <div className="workspace-document-turn-details" id={detailsID}>
            <div className="workspace-document-turn-message user">{userText}</div>
            {agentText ? (
              <div className="workspace-document-turn-message agent">{agentText}</div>
            ) : turn.status === "in_progress" ? (
              <div className="workspace-document-turn-waiting">{status}</div>
            ) : null}
          </div>
        ) : null}
      </section>
      <div className="workspace-document-turn-composer">{children}</div>
    </div>
  );
}
