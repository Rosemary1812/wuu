import { X } from "lucide-react";
import type { ParticipantSummary } from "../shared/protocol";
import { ParticipantChip } from "./ParticipantChip";

/**
 * Inline member list for group threads (docs/plans/
 * 2026-07-03-resident-named-agents.md §7.4). Members join by being
 * @-mentioned in the composer; this bar is where they become visible
 * and where the user can remove them. Hidden entirely when the thread
 * has no members (normal threads, DMs).
 */
export function ThreadMembersBar({
  members,
  onRemove,
}: {
  members: ParticipantSummary[];
  onRemove?: (participantId: string) => void;
}): JSX.Element | null {
  if (members.length === 0) {
    return null;
  }
  return (
    <div className="thread-members-bar" aria-label="会话成员">
      {members.map((member) => (
        <span className="thread-members-item" key={member.id}>
          <ParticipantChip participant={member} size="sm" />
          {onRemove ? (
            <button
              type="button"
              className="thread-members-remove"
              aria-label={`将 ${member.name} 移出会话`}
              title={`将 ${member.name} 移出会话`}
              onClick={() => onRemove(member.id)}
            >
              <X aria-hidden="true" />
            </button>
          ) : null}
        </span>
      ))}
    </div>
  );
}
