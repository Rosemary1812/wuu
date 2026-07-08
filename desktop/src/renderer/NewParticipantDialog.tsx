import {
  Camera,
  ImagePlus,
  Loader2,
  Save,
  Trash2,
  UserRound,
} from "lucide-react";
import {
  type ChangeEvent,
  type MouseEvent,
  type ReactElement,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { createPortal } from "react-dom";
import type {
  ParticipantProfile,
  ParticipantSaveParams,
  ProviderSummary,
} from "../shared/protocol";
import { PARTICIPANT_ROLES } from "./ParticipantProfilePanel";
import { SelectMenu, type SelectMenuGroup } from "./SelectMenu";

const AVATAR_MAX_BYTES = 512 * 1024;

type NewParticipantForm = {
  name: string;
  role: string;
  tagline: string;
  model: string;
  // avatarImage is the data URL chosen in this session, if any.
  avatarImage?: string;
};

export interface NewParticipantDialogProps {
  open: boolean;
  providers?: ProviderSummary[];
  onSubmit: (
    params: ParticipantSaveParams,
  ) => Promise<ParticipantProfile | void> | ParticipantProfile | void;
  onCreated: (participant: ParticipantProfile) => void;
  onClose: () => void;
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

/**
 * Self-contained floating dialog for creating a new agent. Replaces the
 * two-step flow (SidebarNameDialog → ParticipantProfilePanel) so the user
 * can fill in every field here in one shot. After a successful save the
 * dialog closes and the right-side profile panel does NOT open.
 *
 * The edit-mode ParticipantProfilePanel stays in place for editing
 * existing agents via the right-click menu.
 */
export function NewParticipantDialog({
  open,
  providers,
  onSubmit,
  onCreated,
  onClose,
}: NewParticipantDialogProps): ReactElement | null {
  const [form, setForm] = useState<NewParticipantForm>({
    name: "",
    role: "reviewer",
    tagline: "",
    model: "",
    avatarImage: undefined,
  });
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);
  const [avatarError, setAvatarError] = useState<string | undefined>(undefined);
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  // Monotonic token so a later avatar pick supersedes earlier FileReader
  // results that resolve late (matches ParticipantProfilePanel).
  const avatarReadTokenRef = useRef(0);

  // Reset state when the dialog opens (or after a successful save closes it).
  useEffect(() => {
    if (open) {
      setForm({
        name: "",
        role: "reviewer",
        tagline: "",
        model: "",
        avatarImage: undefined,
      });
      setSaving(false);
      setError(undefined);
      setAvatarError(undefined);
    }
  }, [open]);

  useEffect(() => {
    if (!open) {
      return;
    }
    function handleKeyDown(event: KeyboardEvent): void {
      if (event.key !== "Escape") {
        return;
      }
      if (saving) {
        return;
      }
      event.preventDefault();
      onClose();
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [open, saving, onClose]);

  const providerOptions = useMemo(
    () =>
      (providers ?? []).map((provider) => ({
        name: provider.name,
        models: provider.models ?? [],
      })),
    [providers],
  );

  // Orphan model: if the user somehow has a pinned model no longer offered
  // by any provider, keep it visible. For a brand-new agent this is always
  // undefined; matches ParticipantProfilePanel so both surfaces look the
  // same when switching between create / edit.
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

  const modelGroups = useMemo<SelectMenuGroup[]>(() => {
    const groups: SelectMenuGroup[] = [
      { options: [{ value: "", label: "跟随全局" }] },
      ...providerOptions.map((provider) => ({
        label: provider.name,
        options: provider.models.map((model) => ({
          value: `${provider.name}:${model.id}`,
          label: model.display_name ?? model.id,
        })),
      })),
    ];
    if (orphanModelOption) {
      groups.push({ options: [orphanModelOption] });
    }
    return groups;
  }, [providerOptions, orphanModelOption]);

  const canSave = form.name.trim().length > 0 && !saving && !avatarError;

  function updateField<K extends keyof NewParticipantForm>(
    key: K,
    value: NewParticipantForm[K],
  ): void {
    setForm((current) => ({ ...current, [key]: value }));
  }

  async function handleAvatarChange(
    event: ChangeEvent<HTMLInputElement>,
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
      if (token !== avatarReadTokenRef.current) {
        return;
      }
      setAvatarError(undefined);
      setForm((current) => ({ ...current, avatarImage: dataUrl }));
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
    setForm((current) => ({ ...current, avatarImage: undefined }));
  }

  function handleOverlayPointerDown(event: MouseEvent<HTMLDivElement>): void {
    if (saving) {
      return;
    }
    if (event.target === event.currentTarget) {
      onClose();
    }
  }

  async function handleSubmit(): Promise<void> {
    if (!canSave) {
      return;
    }
    const params: ParticipantSaveParams = {
      name: form.name.trim(),
      role: form.role.trim(),
      tagline: form.tagline.trim(),
      model: form.model.trim(),
    };
    if (form.avatarImage) {
      params.avatar_image = form.avatarImage;
    }
    setSaving(true);
    setError(undefined);
    try {
      const result = await onSubmit(params);
      // Dialog closes itself so the parent state machine stays in charge of
      // routing (no right-side panel for "new" agents). The parent hands back
      // the saved participant via onCreated so we can close on the parent
      // signal rather than guessing the next state.
      const saved: ParticipantProfile | undefined =
        result ?? undefined;
      if (saved) {
        onCreated(saved);
      }
      onClose();
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : String(caught));
      setSaving(false);
    }
  }

  if (!open) {
    return null;
  }

  return createPortal(
    <div
      className="conversation-search-overlay new-participant-overlay"
      onPointerDown={handleOverlayPointerDown}
    >
      <form
        className="conversation-search-dialog new-participant-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="new-participant-dialog-title"
        onSubmit={(event) => {
          event.preventDefault();
          void handleSubmit();
        }}
      >
        <div className="new-participant-header">
          <span className="new-participant-icon" aria-hidden="true">
            <UserRound className="icon-lg" />
          </span>
          <h2
            id="new-participant-dialog-title"
            className="new-participant-title"
          >
            新建 Agent
          </h2>
          <p className="new-participant-subtitle">
            在这里填完所有信息，保存后即可在右侧 Agent 列表里看到 Ta。
          </p>
        </div>

        <div className="new-participant-body">
          {error ? (
            <div className="new-participant-error" role="alert">
              {error}
            </div>
          ) : null}

          <div className="new-participant-avatar-row">
            <button
              type="button"
              className="new-participant-avatar"
              aria-label="上传头像"
              title="上传头像"
              onClick={triggerAvatarPicker}
            >
              {form.avatarImage ? (
                <img
                  className="new-participant-avatar-image"
                  src={form.avatarImage}
                  alt="头像"
                />
              ) : (
                <Camera
                  className="new-participant-avatar-placeholder"
                  aria-hidden="true"
                />
              )}
            </button>
            <input
              ref={fileInputRef}
              type="file"
              accept="image/png,image/jpeg,image/webp"
              className="new-participant-file-input"
              onChange={(event) => {
                void handleAvatarChange(event);
              }}
            />
            <div className="new-participant-avatar-actions">
              <button
                type="button"
                className="new-participant-text-action"
                onClick={triggerAvatarPicker}
              >
                <ImagePlus aria-hidden="true" />
                <span>上传图片</span>
              </button>
              {form.avatarImage ? (
                <button
                  type="button"
                  className="new-participant-text-action danger"
                  onClick={clearAvatarImage}
                >
                  <Trash2 aria-hidden="true" />
                  <span>移除</span>
                </button>
              ) : null}
            </div>
          </div>
          {avatarError ? (
            <p className="new-participant-error" role="alert">
              {avatarError}
            </p>
          ) : null}

          <label className="new-participant-field">
            <span>名字</span>
            <input
              data-field="name"
              value={form.name}
              autoFocus
              onChange={(event) => updateField("name", event.currentTarget.value)}
              placeholder="例如 Noel"
              onFocus={(event) => event.currentTarget.select()}
            />
          </label>
          <label className="new-participant-field">
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
          <div className="new-participant-field">
            <span>角色</span>
            <SelectMenu
              ariaLabel="角色"
              dataField="role"
              value={form.role}
              onChange={(next) => updateField("role", next)}
              options={PARTICIPANT_ROLES.map((role) => ({
                value: role,
                label: role,
              }))}
            />
          </div>
          <div className="new-participant-field">
            <span>模型</span>
            <SelectMenu
              ariaLabel="模型"
              dataField="model"
              value={form.model}
              onChange={(next) => updateField("model", next)}
              groups={modelGroups}
            />
          </div>
        </div>

        <div className="new-participant-actions">
          <button type="button" onClick={onClose} disabled={saving}>
            取消
          </button>
          <button type="submit" disabled={!canSave}>
            {saving ? (
              <Loader2 className="new-participant-spinner" aria-hidden="true" />
            ) : (
              <Save aria-hidden="true" />
            )}
            <span>{saving ? "创建中…" : "创建"}</span>
          </button>
        </div>
      </form>
    </div>,
    document.body,
  );
}