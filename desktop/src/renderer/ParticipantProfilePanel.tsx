import {
  Archive,
  Brain,
  Camera,
  ImagePlus,
  Loader2,
  Save,
  Send,
  Trash2,
  X,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import type {
  MemoryReadResult,
  ParticipantProfile,
  ParticipantSaveParams,
  ProviderSummary,
} from "../shared/protocol";
import { ImagePreviewProvider } from "./ImagePreview";
import { Modal } from "./Modal";
import { RichContent } from "./RichContent";

export const PARTICIPANT_ROLES = [
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

const AVATAR_MAX_BYTES = 512 * 1024;

type ParticipantProfileForm = {
  name: string;
  role: string;
  tagline: string;
  model: string;
  // avatarImage is the data URL chosen in this session, if any.
  avatarImage?: string;
  // clearAvatarImage flags the intent to drop the previously uploaded image.
  clearAvatarImage: boolean;
};

// Read-only mirror of the participant's identity notebook (memory/read).
// The old editable Memory textarea wrote the retired flat-file path; the
// notebook is now managed through 设置 → 记忆 (memory-redesign.md §8).
type MemorySummaryState =
  | { status: "loading" }
  | { status: "ready"; result: MemoryReadResult }
  | { status: "unavailable" }
  | { status: "error"; message: string };

function memoryErrorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

// Same substring check as MemoryPanel: a backend without the memory panel
// service rejects memory/* with an "unknown method" error wrapped by the
// Electron invoke bridge.
function isMemoryBackendMissing(error: unknown): boolean {
  return /unknown method|method not found/i.test(memoryErrorMessage(error));
}

function formFromParticipant(
  participant?: ParticipantProfile,
): ParticipantProfileForm {
  return {
    name: participant?.name ?? "",
    role: participant?.role ?? "reviewer",
    tagline: participant?.tagline ?? "",
    model: participant?.model ?? "",
    avatarImage: undefined,
    clearAvatarImage: false,
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

function readFileAsDataUrl(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      if (typeof reader.result === "string") {
        resolve(reader.result);
      } else {
        reject(new Error("文件读取失败"));
      }
    };
    reader.onerror = () => reject(new Error("文件读取失败"));
    reader.readAsDataURL(file);
  });
}

export function ParticipantProfilePanel({
  participant,
  mode,
  providers,
  loading,
  error,
  saving,
  feedbackSubmitting,
  feedbackReply,
  retiring,
  archived,
  onClose,
  onSave,
  onFeedback,
  onOpenMemoryPanel,
  onRetire,
}: {
  participant?: ParticipantProfile;
  mode: "new" | "edit";
  providers?: ProviderSummary[];
  loading?: boolean;
  error?: string;
  saving?: boolean;
  feedbackSubmitting?: boolean;
  // feedbackReply is the memory manager agent's reply_md for the last
  // submitted feedback, shown as a one-line receipt.
  feedbackReply?: string;
  retiring?: boolean;
  // archived flips on after a successful archive; the panel shows the
  // "已归档" receipt while the parent schedules the close.
  archived?: boolean;
  onClose: () => void;
  onSave: (params: ParticipantSaveParams) => void;
  onFeedback: (text: string) => void;
  // Jumps to 设置 → 记忆 with this participant's notebook preselected.
  onOpenMemoryPanel: (participantId: string) => void;
  onRetire: (participantId: string) => void;
}): JSX.Element {
  const [form, setForm] = useState<ParticipantProfileForm>(() =>
    formFromParticipant(participant),
  );
  const [feedback, setFeedback] = useState("");
  const [avatarError, setAvatarError] = useState<string | undefined>(undefined);
  const [archiveConfirmOpen, setArchiveConfirmOpen] = useState(false);
  const [memorySummary, setMemorySummary] = useState<MemorySummaryState>({
    status: "loading",
  });
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  // Monotonic token used to ignore stale FileReader results when the user
  // rapidly picks multiple files.
  const avatarReadTokenRef = useRef(0);
  const participantID = participant?.id ?? "";

  useEffect(() => {
    setForm(formFromParticipant(participant));
    setFeedback("");
    setAvatarError(undefined);
    setArchiveConfirmOpen(false);
  }, [participant?.id, mode]);

  useEffect(() => {
    if (mode !== "edit" || !participantID) {
      return;
    }
    let cancelled = false;
    setMemorySummary({ status: "loading" });
    void window.wuu
      .readMemoryRaw({ scope: "participant", participant_id: participantID })
      .then((result) => {
        if (!cancelled) {
          setMemorySummary({ status: "ready", result });
        }
      })
      .catch((error: unknown) => {
        if (cancelled) {
          return;
        }
        setMemorySummary(
          isMemoryBackendMissing(error)
            ? { status: "unavailable" }
            : { status: "error", message: memoryErrorMessage(error) },
        );
      });
    return () => {
      cancelled = true;
    };
  }, [mode, participantID]);

  const providerOptions = useMemo(
    () =>
      (providers ?? []).map((provider) => ({
        name: provider.name,
        models: provider.models ?? [],
      })),
    [providers],
  );

  // When the participant pins a model that is no longer offered by any
  // provider, surface it as a disabled-by-default option so the user's pin
  // is visible in the select and only changes on explicit interaction.
  const orphanModelOption = useMemo(() => {
    if (form.model.trim().length === 0) {
      return undefined;
    }
    const known = providerOptions.some((provider) =>
      provider.models.some(
        (model) => `${provider.name}:${model.id}` === form.model,
      ),
    );
    if (known) {
      return undefined;
    }
    return { value: form.model, label: `${form.model}（不可用）` };
  }, [form.model, providerOptions]);

  const trackRecord = participant?.track_record ?? [];
  const panelTitle =
    mode === "new" ? "新建 Agent" : participant?.name || "Agent";
  const canSave =
    form.name.trim().length > 0 && !saving && !loading && !avatarError;
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

  const avatarPreview = form.avatarImage || participant?.avatar_image;
  const showAvatarImage = Boolean(form.avatarImage || participant?.avatar_image);

  function updateField<K extends keyof ParticipantProfileForm>(
    key: K,
    value: ParticipantProfileForm[K],
  ): void {
    setForm((current) => ({ ...current, [key]: value }));
  }

  async function handleAvatarChange(
    event: React.ChangeEvent<HTMLInputElement>,
  ): Promise<void> {
    const file = event.currentTarget.files?.[0];
    // Always reset the input so re-picking the same file fires change.
    event.currentTarget.value = "";
    if (!file) {
      return;
    }
    if (file.size > AVATAR_MAX_BYTES) {
      setAvatarError("头像超过 512KB，请压缩后再试");
      return;
    }
    const token = ++avatarReadTokenRef.current;
    try {
      const dataUrl = await readFileAsDataUrl(file);
      // A later pick has superseded this read; drop the result.
      if (token !== avatarReadTokenRef.current) {
        return;
      }
      setAvatarError(undefined);
      setForm((current) => ({
        ...current,
        avatarImage: dataUrl,
        clearAvatarImage: false,
      }));
    } catch {
      if (token !== avatarReadTokenRef.current) {
        return;
      }
      setAvatarError("读取头像失败");
    }
  }

  function triggerAvatarPicker(): void {
    fileInputRef.current?.click();
  }

  function clearAvatarImage(): void {
    setAvatarError(undefined);
    setForm((current) => ({
      ...current,
      avatarImage: undefined,
      clearAvatarImage: Boolean(participant?.avatar_image),
    }));
  }

  function submitSave(): void {
    if (!canSave) {
      return;
    }
    const params: ParticipantSaveParams = {
      id: participant?.id,
      name: form.name.trim(),
      role: form.role.trim(),
      tagline: form.tagline.trim(),
      model: form.model.trim(),
    };
    if (form.avatarImage) {
      params.avatar_image = form.avatarImage;
    }
    if (form.clearAvatarImage) {
      params.clear_avatar_image = true;
    }
    // The save payload already carries the avatar intent; clear the local
    // markers so a second save without an avatar change does not resend
    // `avatar_image` / `clear_avatar_image`. The parent will re-supply the
    // participant object on success, and the avatar preview falls back to
    // `participant?.avatar_image` when `form.avatarImage` is undefined.
    setForm((current) => ({
      ...current,
      avatarImage: undefined,
      clearAvatarImage: false,
    }));
    onSave(params);
  }

  function submitFeedback(): void {
    const text = feedback.trim();
    if (!canSendFeedback || text.length === 0) {
      return;
    }
    onFeedback(text);
    setFeedback("");
  }

  function confirmArchive(): void {
    if (!participant?.id) {
      return;
    }
    setArchiveConfirmOpen(false);
    onRetire(participant.id);
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
        {archived ? (
          <div
            className="participant-profile-state participant-profile-archived"
            role="status"
          >
            <Archive aria-hidden="true" />
            <span>已归档</span>
          </div>
        ) : loading ? (
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
              aria-labelledby="participant-profile-identity"
            >
              <h3 id="participant-profile-identity">身份</h3>
              <div className="participant-profile-avatar-row">
                <button
                  type="button"
                  className="participant-profile-avatar"
                  aria-label="上传头像"
                  title="上传头像"
                  onClick={triggerAvatarPicker}
                >
                  {showAvatarImage && avatarPreview ? (
                    <img
                      className="participant-profile-avatar-image"
                      src={avatarPreview}
                      alt="头像"
                    />
                  ) : (
                    <Camera
                      className="participant-profile-avatar-placeholder"
                      aria-hidden="true"
                    />
                  )}
                </button>
                <input
                  ref={fileInputRef}
                  type="file"
                  accept="image/png,image/jpeg,image/webp"
                  className="participant-profile-file-input"
                  data-field="avatar-file"
                  onChange={(event) => {
                    void handleAvatarChange(event);
                  }}
                />
                <div className="participant-profile-avatar-actions">
                  <button
                    type="button"
                    className="participant-profile-text-action"
                    onClick={triggerAvatarPicker}
                  >
                    <ImagePlus aria-hidden="true" />
                    <span>上传图片</span>
                  </button>
                  {showAvatarImage ? (
                    <button
                      type="button"
                      className="participant-profile-text-action danger"
                      onClick={clearAvatarImage}
                    >
                      <Trash2 aria-hidden="true" />
                      <span>移除</span>
                    </button>
                  ) : null}
                </div>
              </div>
              {avatarError ? (
                <p className="participant-profile-error" role="alert">
                  {avatarError}
                </p>
              ) : null}
              <label className="participant-profile-field">
                <span>名字</span>
                <input
                  data-field="name"
                  value={form.name}
                  onChange={(event) =>
                    updateField("name", event.currentTarget.value)
                  }
                  placeholder="Noel"
                />
              </label>
              <label className="participant-profile-field">
                <span>一句话介绍</span>
                <input
                  data-field="tagline"
                  value={form.tagline}
                  onChange={(event) =>
                    updateField("tagline", event.currentTarget.value)
                  }
                  placeholder="Find regressions"
                />
              </label>
            </section>

            <section
              className="participant-profile-section"
              aria-labelledby="participant-profile-config"
            >
              <h3 id="participant-profile-config">配置</h3>
              <label className="participant-profile-field">
                <span>角色</span>
                <select
                  data-field="role"
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
                <span>模型</span>
                <select
                  data-field="model"
                  value={form.model}
                  onChange={(event) =>
                    updateField("model", event.currentTarget.value)
                  }
                >
                  <option value="">跟随全局</option>
                  {providerOptions.map((provider) => (
                    <optgroup key={provider.name} label={provider.name}>
                      {provider.models.map((model) => (
                        <option
                          key={`${provider.name}:${model.id}`}
                          value={`${provider.name}:${model.id}`}
                        >
                          {model.display_name ?? model.id}
                        </option>
                      ))}
                    </optgroup>
                  ))}
                  {orphanModelOption ? (
                    <option value={orphanModelOption.value}>
                      {orphanModelOption.label}
                    </option>
                  ) : null}
                </select>
              </label>
            </section>

            {mode === "edit" && participantID ? (
              <section
                className="participant-profile-section"
                aria-labelledby="participant-profile-memory"
              >
                <h3 id="participant-profile-memory">记忆</h3>
                <MemorySummaryView state={memorySummary} />
                <button
                  type="button"
                  className="participant-profile-text-action"
                  onClick={() => onOpenMemoryPanel(participantID)}
                >
                  <Brain aria-hidden="true" />
                  <span>在记忆面板中管理</span>
                </button>
              </section>
            ) : null}

            {mode === "edit" ? (
              <section
                className="participant-profile-section"
                aria-labelledby="participant-profile-track"
              >
                <h3 id="participant-profile-track">任务履历</h3>
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
            ) : null}

            {mode === "edit" ? (
              <section
                className="participant-profile-section"
                aria-labelledby="participant-profile-feedback"
              >
                <h3 id="participant-profile-feedback">反馈</h3>
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
                {feedbackReply ? (
                  <p
                    className="participant-profile-feedback-reply"
                    role="status"
                  >
                    {feedbackReply}
                  </p>
                ) : null}
              </section>
            ) : null}

            {mode === "edit" ? (
              <section
                className="participant-profile-section"
                aria-labelledby="participant-profile-manage"
              >
                <h3 id="participant-profile-manage">管理</h3>
                <div className="participant-profile-reset-actions">
                  <button
                    type="button"
                    className="participant-profile-text-action"
                    disabled={retiring}
                    onClick={() => setArchiveConfirmOpen(true)}
                  >
                    {retiring ? (
                      <Loader2
                        className="participant-profile-spinner"
                        aria-hidden="true"
                      />
                    ) : (
                      <Archive aria-hidden="true" />
                    )}
                    <span>归档此同事</span>
                  </button>
                </div>
              </section>
            ) : null}
          </>
        )}
      </div>
      {archiveConfirmOpen ? (
        <ArchiveConfirmDialog
          onCancel={() => setArchiveConfirmOpen(false)}
          onConfirm={confirmArchive}
        />
      ) : null}
      {archived ? null : (
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
      )}
    </aside>
  );
}

function ArchiveConfirmDialog({
  onCancel,
  onConfirm,
}: {
  onCancel: () => void;
  onConfirm: () => void;
}): JSX.Element {
  return (
    <Modal
      ariaLabel="归档此同事"
      icon={<Archive className="icon-lg" />}
      title="归档此同事"
      subtitle="Ta 的记忆和对话将完整归档，私聊变为只读；之后可以随时复职。"
      onClose={onCancel}
      panelClassName="participant-archive-dialog"
      footer={
        <>
          <button className="secondary-button" type="button" onClick={onCancel}>
            再想想
          </button>
          <button className="primary-button" type="button" onClick={onConfirm}>
            归档
          </button>
        </>
      }
    />
  );
}

function MemorySummaryView({
  state,
}: {
  state: MemorySummaryState;
}): JSX.Element {
  if (state.status === "loading") {
    return <p className="participant-profile-empty">加载中…</p>;
  }
  if (state.status === "unavailable") {
    return <p className="participant-profile-empty">记忆服务尚未就绪。</p>;
  }
  if (state.status === "error") {
    return (
      <p className="participant-profile-error" role="alert">
        {state.message}
      </p>
    );
  }
  if (!state.result.index_md.trim()) {
    return <p className="participant-profile-empty">还没有关于 Ta 的记忆。</p>;
  }
  return (
    <div
      className="participant-profile-memory-summary"
      data-testid="participant-memory-summary"
    >
      <ImagePreviewProvider>
        <RichContent text={state.result.index_md} />
      </ImagePreviewProvider>
    </div>
  );
}
