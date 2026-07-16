import { Hash, Plus, UserPlus, X } from "lucide-react";
import { type RefObject, useMemo, useState } from "react";
import type {
  ParticipantProfile,
  ParticipantSummary,
  Thread,
} from "../shared/protocol";
import { DefaultAvatarMark } from "./DefaultAvatar";
import type { EnvironmentPanelMotionState } from "./EnvironmentPanel";
import { participantRoleLabel } from "./ParticipantLabels";
import { resolveLocalizedText, translateCurrent, useI18n } from "./i18n";

export function GroupInfoPanel({
  panelRef,
  motionState,
  thread,
  members,
  participants,
  onClose,
  onAddMember,
  onRemoveMember,
}: {
  panelRef: RefObject<HTMLDivElement | null>;
  motionState: EnvironmentPanelMotionState;
  thread: Thread;
  members: ParticipantSummary[];
  participants: ParticipantProfile[];
  onClose: () => void;
  onAddMember?: (participantID: string) => Promise<void> | void;
  onRemoveMember?: (participantID: string) => Promise<void> | void;
}): JSX.Element {
  const { t, formatNumber } = useI18n();
  const [addOpen, setAddOpen] = useState(false);
  const [addingParticipantID, setAddingParticipantID] = useState<string>();
  const memberIDs = useMemo(
    () => new Set(members.map((member) => member.id)),
    [members],
  );
  const availableParticipants = useMemo(
    () =>
      participants.filter(
        (participant) =>
          participant.kind === "named" && !memberIDs.has(participant.id),
      ),
    [memberIDs, participants],
  );
  const groupName = groupDisplayName(thread);
  const canAddMember =
    Boolean(onAddMember) && availableParticipants.length > 0;
  // Resolve a母体's participant id to a display name so a forked分身 member
  // can be badged "X 的分身". forked_from_id is only an id on the wire; both
  // the member roster and the fuller participant profiles carry names, so a
  // local lookup covers母体s that are (or are not) themselves in this group.
  const resolveMemberName = useMemo(() => {
    const byID = new Map<string, string>();
    for (const participant of participants) {
      if (participant.id) {
        byID.set(participant.id, participant.name.trim() || participant.id);
      }
    }
    for (const member of members) {
      if (member.id && !byID.has(member.id)) {
        byID.set(member.id, member.name.trim() || member.id);
      }
    }
    return (id: string): string => byID.get(id) ?? id;
  }, [members, participants]);

  async function addMember(participantID: string): Promise<void> {
    if (!onAddMember) {
      return;
    }
    setAddingParticipantID(participantID);
    try {
      await onAddMember(participantID);
      setAddOpen(false);
    } finally {
      setAddingParticipantID(undefined);
    }
  }

  return (
    <aside
      className={`environment-panel group-info-panel ${motionState}`}
      ref={panelRef}
      aria-label={t("groupInfo.title")}
      aria-hidden={motionState === "closing" ? true : undefined}
    >
      <div className="environment-panel-header">
        <div className="environment-panel-title">
          <h2>{t("groupInfo.title")}</h2>
        </div>
        <div className="environment-panel-actions">
          <button
            className="icon-button"
            type="button"
            aria-label={t("groupInfo.close")}
            onClick={onClose}
          >
            <X className="icon" />
          </button>
        </div>
      </div>

      <div className="environment-panel-body group-info-panel-body">
        <section className="group-info-summary" aria-label={t("groupInfo.summary")}>
          <div className="group-info-field">
            <span>{t("groupInfo.name")}</span>
            <strong>
              <Hash className="icon" aria-hidden="true" />
              {groupName}
            </strong>
          </div>
        </section>

        <section className="group-info-members" aria-label={t("groupInfo.members")}>
          <div className="group-info-members-heading">
            <div>
              <h3>{t("groupInfo.members")}</h3>
              <span>{t(members.length === 1 ? "groupInfo.memberCountOne" : "groupInfo.memberCount", { count: formatNumber(members.length) })}</span>
            </div>
            {canAddMember ? (
              <button
                className="group-info-add-button"
                type="button"
                aria-label={t("groupInfo.addMember")}
                aria-expanded={addOpen}
                title={t("groupInfo.addMember")}
                onClick={() => setAddOpen((open) => !open)}
              >
                <Plus aria-hidden="true" />
              </button>
            ) : null}
          </div>

          {addOpen ? (
            <div className="group-info-add-list" role="list">
              {availableParticipants.map((participant) => (
                <button
                  className="group-info-add-row"
                  type="button"
                  key={participant.id}
                  disabled={addingParticipantID === participant.id}
                  onClick={() => void addMember(participant.id)}
                >
                  <UserPlus className="icon" aria-hidden="true" />
                  <ParticipantAvatar participant={participant} />
                  <span>
                    <strong>{participant.name}</strong>
                    {participant.role ? <em>{participantRoleLabel(participant.role)}</em> : null}
                  </span>
                </button>
              ))}
            </div>
          ) : null}

          <div className="group-info-member-list" role="list">
            {members.length === 0 ? (
              <div className="group-info-empty">{t("groupInfo.empty")}</div>
            ) : (
              members.map((member) => (
                <div className="group-info-member-row" role="listitem" key={member.id}>
                  <ParticipantAvatar participant={member} busy={member.busy} />
                  <span className="group-info-member-text">
                    <strong>{member.name}</strong>
                    {member.role ? <em>{participantRoleLabel(member.role)}</em> : null}
                    {member.forked_from_id ? (
                      <em className="group-info-fork-badge">
                        {t("participant.profile.forkOf", { name: resolveMemberName(member.forked_from_id) })}
                      </em>
                    ) : null}
                  </span>
                  {onRemoveMember ? (
                    <button
                      className="group-info-remove-button"
                      type="button"
                      aria-label={t("groupInfo.removeMember", { name: member.name })}
                      title={t("groupInfo.removeMember", { name: member.name })}
                      onClick={() => void onRemoveMember(member.id)}
                    >
                      <X aria-hidden="true" />
                    </button>
                  ) : null}
                </div>
              ))
            )}
          </div>
        </section>
      </div>
    </aside>
  );
}

function ParticipantAvatar({
  participant,
  busy,
}: {
  participant: ParticipantSummary | ParticipantProfile;
  // busy comes from the decision-five concurrency lock (member.busy),
  // overlaid onto group-member summaries by the backend. It is a distinct
  // signal from the running-agent set used in chat avatars, so it is read
  // straight off the member here rather than derived. Undefined/false for
  // idle members and for the add-member picker rows.
  busy?: boolean;
}): JSX.Element {
  const { t } = useI18n();
  const name = participant.name.trim() || "Agent";
  const image = participant.avatar_image?.trim() ?? "";
  return (
    <span className="group-info-avatar-cell">
      <span className="group-info-avatar" aria-hidden="true">
        {image ? (
          <img src={image} alt="" />
        ) : (
          <DefaultAvatarMark seed={participant.id || name} kind={participant.kind} />
        )}
      </span>
      {busy ? (
        <span
          className="group-info-avatar-busy"
          role="img"
          aria-label={t("groupInfo.memberBusy", { name })}
          title={t("groupInfo.busy")}
        />
      ) : null}
    </span>
  );
}

function groupDisplayName(thread: Thread): string {
  const title =
    thread.title?.trim() ||
    resolveLocalizedText(thread.preview.trim()) ||
    translateCurrent("groupInfo.untitled");
  return title.replace(/^#/, "");
}
