import { UsersRound } from "lucide-react";
import type { ParticipantSummary } from "../shared/protocol";
import { DefaultAvatarMark } from "./DefaultAvatar";

const MAX_VISIBLE_AVATARS = 3;

/**
 * Compact members affordance on the right edge of the group composer bar:
 * a stack of overlapping avatars plus the member count, nothing else. The
 * full roster and member management live in the group info panel this
 * button opens; live agent status stays on message-flow avatars.
 */
export function GroupMembersCapsule({
  members,
  onOpen,
}: {
  members: ParticipantSummary[];
  onOpen?: () => void;
}): JSX.Element {
  const visibleMembers = members.slice(0, MAX_VISIBLE_AVATARS);
  const hiddenCount = Math.max(0, members.length - visibleMembers.length);
  const names = members.map((member) => member.name.trim()).filter(Boolean);
  const detail =
    names.length > 0
      ? names.slice(0, 4).join("、") + (names.length > 4 ? ` 等 ${names.length} 位` : "")
      : "暂无成员";
  return (
    <button
      className="group-members-capsule"
      type="button"
      aria-label={`群聊成员：${detail}`}
      title="打开群聊信息"
      onClick={onOpen}
    >
      <span className="group-members-capsule-avatars" aria-hidden="true">
        {visibleMembers.length > 0 ? (
          visibleMembers.map((member) => (
            <GroupMemberAvatar key={member.id} member={member} />
          ))
        ) : (
          <span className="group-members-capsule-empty">
            <UsersRound />
          </span>
        )}
        {hiddenCount > 0 ? (
          <span className="group-members-capsule-more">+{hiddenCount}</span>
        ) : null}
      </span>
      <span className="group-members-capsule-count" aria-hidden="true">
        {members.length}
      </span>
    </button>
  );
}

function GroupMemberAvatar({
  member,
}: {
  member: ParticipantSummary;
}): JSX.Element {
  const name = member.name.trim() || "Agent";
  const image = member.avatar_image?.trim() ?? "";
  return (
    <span className="group-members-capsule-avatar">
      {image ? (
        <img src={image} alt="" />
      ) : (
        <DefaultAvatarMark seed={member.id || name} />
      )}
    </span>
  );
}
