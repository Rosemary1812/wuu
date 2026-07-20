import type { ParticipantSummary } from "../shared/protocol";
import { DefaultAvatarMark } from "./DefaultAvatar";
import { participantRoleLabel } from "./ParticipantLabels";

export type ParticipantChipProps = {
  /**
   * Wire-level participant identity. When present it wins over the
   * legacy fallback fields below; when undefined the chip renders the
   * legacy `Agent.type` / `task_name` combination so old threads keep
   * a readable label.
   */
  participant?: ParticipantSummary;
  /** Legacy `Agent.type` for threads recorded before participants. */
  fallbackType?: string;
  /** Legacy `Agent.task_name` for threads recorded before participants. */
  fallbackTaskName?: string;
  size?: "sm" | "md";
};

/**
 * Inline identity chip for a conversation participant (subagent).
 * Display-only: avatar + name + role, no interaction. The 16px avatar
 * cell shows the participant's uploaded avatar_image (a data URL the
 * backend embeds into summaries, size-capped — see protocol.ts) and
 * falls back to the name's first letter when no image is available.
 * The role span is omitted when empty, and also suppressed when it
 * would repeat the name verbatim (legacy rows where only `type` is
 * known would otherwise read "explore explore").
 */
export function ParticipantChip({
  participant,
  fallbackType,
  fallbackTaskName,
  size = "md",
}: ParticipantChipProps): JSX.Element {
  const name =
    participant?.name.trim() ||
    fallbackTaskName?.trim() ||
    fallbackType?.trim() ||
    "agent";
  const roleSource = participant
    ? (participant.role?.trim() ?? "")
    : (fallbackType?.trim() ?? "");
  const role = roleSource !== name ? participantRoleLabel(roleSource) : "";
  const avatarImage = participant?.avatar_image?.trim() ?? "";
  const className = `participant-chip${size === "sm" ? " participant-chip--sm" : ""}`;
  return (
    <span className={className} title={role ? `${name} · ${role}` : name}>
      <span className="participant-chip-avatar" aria-hidden="true">
        {avatarImage ? (
          <img src={avatarImage} alt="" />
        ) : (
          <DefaultAvatarMark seed={participant?.id || name} kind={participant?.kind} />
        )}
      </span>
      <span className="participant-chip-name">{name}</span>
      {role ? (
        <>
          <span className="participant-chip-separator" aria-hidden="true">
            ·
          </span>
          <span className="participant-chip-role">{role}</span>
        </>
      ) : null}
    </span>
  );
}
