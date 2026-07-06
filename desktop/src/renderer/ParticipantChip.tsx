import type { ParticipantSummary } from "../shared/protocol";
import { DefaultAvatarMark } from "./DefaultAvatar";

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
  /**
   * Resolves a母体's participant id to a display name so a forked分身 can be
   * badged "X 的分身". forked_from_id is only an id on the wire; when no
   * resolver is supplied the chip degrades to a bare "分身" tag.
   */
  resolveName?: (id: string) => string;
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
  resolveName,
}: ParticipantChipProps): JSX.Element {
  const name =
    participant?.name.trim() ||
    fallbackTaskName?.trim() ||
    fallbackType?.trim() ||
    "agent";
  const roleSource = participant
    ? (participant.role?.trim() ?? "")
    : (fallbackType?.trim() ?? "");
  const role = roleSource !== name ? roleSource : "";
  const avatarImage = participant?.avatar_image?.trim() ?? "";
  const forkedFromID = participant?.forked_from_id?.trim() ?? "";
  // Compact "分身" pill in the tight inline chip; the母体 name rides in the
  // tooltip so the row stays a single line even for long母体 names.
  const forkTooltip = forkedFromID
    ? `${resolveName ? resolveName(forkedFromID) : forkedFromID} 的分身`
    : "";
  const className = `participant-chip${size === "sm" ? " participant-chip--sm" : ""}`;
  return (
    <span className={className} title={role ? `${name} · ${role}` : name}>
      <span className="participant-chip-avatar" aria-hidden="true">
        {avatarImage ? (
          <img src={avatarImage} alt="" />
        ) : (
          <DefaultAvatarMark seed={participant?.id || name} />
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
      {forkedFromID ? (
        <span
          className="participant-chip-fork"
          title={forkTooltip}
          aria-label={forkTooltip}
        >
          分身
        </span>
      ) : null}
    </span>
  );
}
