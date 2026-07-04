import { Hash, Plus, UserPlus, X } from "lucide-react";
import { type RefObject, useMemo, useState } from "react";
import type {
  ParticipantProfile,
  ParticipantSummary,
  Thread,
} from "../shared/protocol";
import type { EnvironmentPanelMotionState } from "./EnvironmentPanel";

export function GroupInfoPanel({
  panelRef,
  motionState,
  thread,
  members,
  busyParticipantIDs,
  participants,
  onClose,
  onAddMember,
  onRemoveMember,
}: {
  panelRef: RefObject<HTMLDivElement | null>;
  motionState: EnvironmentPanelMotionState;
  thread: Thread;
  members: ParticipantSummary[];
  busyParticipantIDs?: ReadonlySet<string>;
  participants: ParticipantProfile[];
  onClose: () => void;
  onAddMember?: (participantID: string) => Promise<void> | void;
  onRemoveMember?: (participantID: string) => Promise<void> | void;
}): JSX.Element {
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
  const allChannel = isAllChannelThread(thread);
  const groupName = groupDisplayName(thread);
  const canAddMember =
    !allChannel && Boolean(onAddMember) && availableParticipants.length > 0;

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
      aria-label="群聊信息"
      aria-hidden={motionState === "closing" ? true : undefined}
    >
      <div className="environment-panel-header">
        <div className="environment-panel-title">
          <h2>群聊信息</h2>
        </div>
        <div className="environment-panel-actions">
          <button
            className="icon-button"
            type="button"
            aria-label="关闭群聊信息"
            onClick={onClose}
          >
            <X className="icon" />
          </button>
        </div>
      </div>

      <div className="environment-panel-body group-info-panel-body">
        <section className="group-info-summary" aria-label="群聊概览">
          <div className="group-info-field">
            <span>群名称</span>
            <strong>
              <Hash className="icon" aria-hidden="true" />
              {groupName}
            </strong>
          </div>
        </section>

        <section className="group-info-members" aria-label="群成员">
          <div className="group-info-members-heading">
            <div>
              <h3>群成员</h3>
              <span>{members.length} 人</span>
            </div>
            {canAddMember ? (
              <button
                className="group-info-add-button"
                type="button"
                aria-label="添加群成员"
                aria-expanded={addOpen}
                title="添加群成员"
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
                    {participant.role ? <em>{participant.role}</em> : null}
                  </span>
                </button>
              ))}
            </div>
          ) : null}

          <div className="group-info-member-list" role="list">
            {members.length === 0 ? (
              <div className="group-info-empty">暂无成员</div>
            ) : (
              members.map((member) => (
                <div className="group-info-member-row" role="listitem" key={member.id}>
                  <ParticipantAvatar
                    participant={member}
                    busy={busyParticipantIDs?.has(member.id) ?? false}
                  />
                  <span className="group-info-member-text">
                    <strong>{member.name}</strong>
                    {member.role ? <em>{member.role}</em> : null}
                  </span>
                  {!allChannel && onRemoveMember ? (
                    <button
                      className="group-info-remove-button"
                      type="button"
                      aria-label={`将 ${member.name} 移出群聊`}
                      title={`将 ${member.name} 移出群聊`}
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
  busy?: boolean;
}): JSX.Element {
  const name = participant.name.trim() || "Agent";
  const image = participant.avatar_image?.trim() ?? "";
  const status = busy === undefined ? undefined : busy ? "busy" : "online";
  return (
    <span
      className="group-info-avatar"
      role={status ? "img" : undefined}
      aria-label={status ? `${name} ${busy ? "正在响应" : "在线"}` : undefined}
      aria-hidden={status ? undefined : true}
    >
      {image ? <img src={image} alt="" /> : name.charAt(0)}
      {status ? (
        <span className="group-info-avatar-status" data-status={status} />
      ) : null}
    </span>
  );
}

function groupDisplayName(thread: Thread): string {
  const title = thread.title?.trim() || thread.preview.trim() || "未命名群聊";
  return title.replace(/^#/, "");
}

function isAllChannelThread(thread: Thread): boolean {
  const title = thread.title?.trim().replace(/^#/, "").toLowerCase();
  return thread.group === true && title === "all";
}
