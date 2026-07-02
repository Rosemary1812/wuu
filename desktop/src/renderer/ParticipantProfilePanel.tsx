import { Loader2, RotateCcw, Save, Send, X } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import type {
  ParticipantProfile,
  ParticipantSaveParams,
} from "../shared/protocol";

export type ParticipantResetScope = "restart" | "session" | "full";

const PARTICIPANT_ROLES = [
  "general-purpose",
  "planner",
  "researcher",
  "worker",
  "reviewer",
  "qa",
  "debugger",
  "integrator",
  "verification",
];

type ParticipantProfileForm = {
  name: string;
  role: string;
  avatar: string;
  tagline: string;
  model: string;
  memory: string;
};

function formFromParticipant(
  participant?: ParticipantProfile,
): ParticipantProfileForm {
  return {
    name: participant?.name ?? "",
    role: participant?.role ?? "reviewer",
    avatar: participant?.avatar ?? "",
    tagline: participant?.tagline ?? "",
    model: participant?.model ?? "",
    memory: participant?.memory ?? "",
  };
}

function timestampLabel(value?: string): string {
  if (!value) {
    return "";
  }
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return value;
  }
  return parsed.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export function ParticipantProfilePanel({
  participant,
  mode,
  loading,
  error,
  saving,
  feedbackSubmitting,
  resettingScope,
  onClose,
  onSave,
  onFeedback,
  onReset,
}: {
  participant?: ParticipantProfile;
  mode: "new" | "edit";
  loading?: boolean;
  error?: string;
  saving?: boolean;
  feedbackSubmitting?: boolean;
  resettingScope?: ParticipantResetScope;
  onClose: () => void;
  onSave: (params: ParticipantSaveParams) => void;
  onFeedback: (text: string) => void;
  onReset: (scope: ParticipantResetScope) => void;
}): JSX.Element {
  const [form, setForm] = useState<ParticipantProfileForm>(() =>
    formFromParticipant(participant),
  );
  const [feedback, setFeedback] = useState("");

  useEffect(() => {
    setForm(formFromParticipant(participant));
    setFeedback("");
  }, [participant?.id, mode]);

  const trackRecord = participant?.track_record ?? [];
  const panelTitle =
    mode === "new" ? "新建 Agent" : participant?.name || "Agent";
  const canSave = form.name.trim().length > 0 && !saving && !loading;
  const canSendFeedback =
    mode === "edit" &&
    feedback.trim().length > 0 &&
    !feedbackSubmitting &&
    !loading;
  const metaLine = useMemo(() => {
    const parts = [form.role, form.model].filter(
      (part) => part.trim().length > 0,
    );
    return parts.join(" · ");
  }, [form.model, form.role]);

  function updateField<K extends keyof ParticipantProfileForm>(
    key: K,
    value: ParticipantProfileForm[K],
  ): void {
    setForm((current) => ({ ...current, [key]: value }));
  }

  function submitSave(): void {
    if (!canSave) {
      return;
    }
    onSave({
      id: participant?.id,
      name: form.name.trim(),
      role: form.role.trim(),
      avatar: form.avatar.trim(),
      tagline: form.tagline.trim(),
      model: form.model.trim(),
      memory: form.memory,
    });
  }

  function submitFeedback(): void {
    const text = feedback.trim();
    if (!canSendFeedback || text.length === 0) {
      return;
    }
    onFeedback(text);
    setFeedback("");
  }

  return (
    <aside className="participant-profile-panel" aria-label="Agent 档案">
      <header className="participant-profile-header">
        <div className="participant-profile-title-group">
          <h2>{panelTitle}</h2>
          {metaLine ? <span>{metaLine}</span> : null}
        </div>
        <button
          type="button"
          className="icon-button participant-profile-icon"
          aria-label="关闭"
          title="关闭"
          onClick={onClose}
        >
          <X aria-hidden="true" />
        </button>
      </header>
      <div className="participant-profile-body">
        {loading ? (
          <div className="participant-profile-state" role="status">
            <Loader2 className="participant-profile-spinner" aria-hidden="true" />
            <span>加载中</span>
          </div>
        ) : (
          <>
            {error ? (
              <div className="participant-profile-state error" role="alert">
                {error}
              </div>
            ) : null}
            <section
              className="participant-profile-section"
              aria-labelledby="participant-profile-basic"
            >
              <h3 id="participant-profile-basic">身份</h3>
              <label className="participant-profile-field">
                <span>名字</span>
                <input
                  value={form.name}
                  onChange={(event) =>
                    updateField("name", event.currentTarget.value)
                  }
                  placeholder="Noel"
                />
              </label>
              <label className="participant-profile-field inline">
                <span>头像</span>
                <input
                  value={form.avatar}
                  onChange={(event) =>
                    updateField("avatar", event.currentTarget.value)
                  }
                  placeholder="头像"
                />
              </label>
              <label className="participant-profile-field">
                <span>角色</span>
                <select
                  value={form.role}
                  onChange={(event) =>
                    updateField("role", event.currentTarget.value)
                  }
                >
                  {PARTICIPANT_ROLES.map((role) => (
                    <option key={role} value={role}>
                      {role}
                    </option>
                  ))}
                </select>
              </label>
              <label className="participant-profile-field">
                <span>一句话</span>
                <input
                  value={form.tagline}
                  onChange={(event) =>
                    updateField("tagline", event.currentTarget.value)
                  }
                  placeholder="Find regressions"
                />
              </label>
              <label className="participant-profile-field">
                <span>模型</span>
                <input
                  value={form.model}
                  onChange={(event) =>
                    updateField("model", event.currentTarget.value)
                  }
                  placeholder="跟随全局"
                />
              </label>
            </section>

            <section
              className="participant-profile-section"
              aria-labelledby="participant-profile-memory"
            >
              <h3 id="participant-profile-memory">Memory</h3>
              <textarea
                className="participant-profile-memory"
                value={form.memory}
                onChange={(event) =>
                  updateField("memory", event.currentTarget.value)
                }
                rows={10}
              />
            </section>

            <section
              className="participant-profile-section"
              aria-labelledby="participant-profile-track"
            >
              <h3 id="participant-profile-track">Track record</h3>
              {trackRecord.length === 0 ? (
                <p className="participant-profile-empty">暂无记录</p>
              ) : (
                <ol className="participant-profile-track-list">
                  {trackRecord.map((entry, index) => (
                    <li
                      key={`${entry.task_id ?? index}-${entry.created_at ?? index}`}
                    >
                      <div className="participant-profile-track-title">
                        {entry.summary || entry.task_id || "任务"}
                      </div>
                      <div className="participant-profile-track-meta">
                        {[entry.outcome, timestampLabel(entry.created_at)]
                          .filter(Boolean)
                          .join(" · ")}
                      </div>
                    </li>
                  ))}
                </ol>
              )}
            </section>

            {mode === "edit" ? (
              <section
                className="participant-profile-section"
                aria-labelledby="participant-profile-feedback"
              >
                <h3 id="participant-profile-feedback">Feedback</h3>
                <textarea
                  className="participant-profile-feedback"
                  value={feedback}
                  onChange={(event) => setFeedback(event.currentTarget.value)}
                  rows={4}
                />
                <button
                  type="button"
                  className="participant-profile-action"
                  disabled={!canSendFeedback}
                  onClick={submitFeedback}
                >
                  {feedbackSubmitting ? (
                    <Loader2
                      className="participant-profile-spinner"
                      aria-hidden="true"
                    />
                  ) : (
                    <Send aria-hidden="true" />
                  )}
                  <span>写入反馈</span>
                </button>
              </section>
            ) : null}

            {mode === "edit" ? (
              <section
                className="participant-profile-section"
                aria-labelledby="participant-profile-reset"
              >
                <h3 id="participant-profile-reset">Reset</h3>
                <div className="participant-profile-reset-actions">
                  {(["restart", "session", "full"] as ParticipantResetScope[]).map(
                    (scope) => (
                      <button
                        key={scope}
                        type="button"
                        className="participant-profile-text-action"
                        disabled={Boolean(resettingScope)}
                        onClick={() => onReset(scope)}
                      >
                        {resettingScope === scope ? (
                          <Loader2
                            className="participant-profile-spinner"
                            aria-hidden="true"
                          />
                        ) : (
                          <RotateCcw aria-hidden="true" />
                        )}
                        <span>{scope}</span>
                      </button>
                    ),
                  )}
                </div>
              </section>
            ) : null}
          </>
        )}
      </div>
      <footer className="participant-profile-footer">
        <button
          type="button"
          className="participant-profile-action primary"
          disabled={!canSave}
          onClick={submitSave}
        >
          {saving ? (
            <Loader2
              className="participant-profile-spinner"
              aria-hidden="true"
            />
          ) : (
            <Save aria-hidden="true" />
          )}
          <span>保存</span>
        </button>
      </footer>
    </aside>
  );
}
