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
import { PARTICIPANT_ROLES, participantRoleLabel } from "./ParticipantLabels";
import { SelectMenu, type SelectMenuGroup } from "./SelectMenu";
import { translateCurrent, useI18n } from "./i18n";

const AVATAR_MAX_BYTES = 512 * 1024;

type NewParticipantForm = {
  name: string;
  role: string;
  tagline: string;
  model: string;
  // avatarImage is the data URL chosen in this session, if any.
  avatarImage?: string;
  clearAvatarImage: boolean;
};

export interface NewParticipantDialogProps {
  open: boolean;
  participant?: ParticipantProfile;
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
        reject(new Error(translateCurrent("participant.avatar.fileReadFailed")));
      }
    };
    reader.onerror = () => reject(new Error(translateCurrent("participant.avatar.fileReadFailed")));
    reader.readAsDataURL(file);
  });
}

/**
 * Self-contained floating dialog for creating or editing an agent. Both
 * sidebar flows share this surface so profile edits never reopen the legacy
 * right-side ParticipantProfilePanel.
 */
export function NewParticipantDialog({
  open,
  participant,
  providers,
  onSubmit,
  onCreated,
  onClose,
}: NewParticipantDialogProps): ReactElement | null {
  const { locale, t } = useI18n();
  const [form, setForm] = useState<NewParticipantForm>({
    name: "",
    role: "reviewer",
    tagline: "",
    model: "",
    avatarImage: undefined,
    clearAvatarImage: false,
  });
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);
  const [avatarError, setAvatarError] = useState<string | undefined>(undefined);
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  // Monotonic token so a later avatar pick supersedes earlier FileReader
  // results that resolve late (matches ParticipantProfilePanel).
  const avatarReadTokenRef = useRef(0);

  const editing = Boolean(participant);

  // Reset state when the dialog opens or switches to another participant.
  useEffect(() => {
    if (open) {
      setForm({
        name: participant?.name ?? "",
        role: participant?.role ?? "reviewer",
        tagline: participant?.tagline ?? "",
        model: participant?.model ?? "",
        avatarImage: undefined,
        clearAvatarImage: false,
      });
      setSaving(false);
      setError(undefined);
      setAvatarError(undefined);
    }
  }, [open, participant]);

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
    return { value: form.model, label: t("participant.modelUnavailable", { model: form.model }) };
  }, [form.model, locale, providerOptions]);

  const modelGroups = useMemo<SelectMenuGroup[]>(() => {
    const groups: SelectMenuGroup[] = [
      { options: [{ value: "", label: t("participant.followGlobal") }] },
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
  }, [locale, providerOptions, orphanModelOption]);

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
      setAvatarError(t("participant.avatar.tooLarge"));
      return;
    }
    const token = ++avatarReadTokenRef.current;
    try {
      const dataUrl = await readFileAsDataUrl(file);
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
      setAvatarError(t("participant.avatar.readFailed"));
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
    setSaving(true);
    setError(undefined);
    try {
      const result = await onSubmit(params);
      // Dialog closes itself so the parent state machine stays in charge of
      // routing. The parent hands back the saved participant via onCreated so
      // we close on the parent signal rather than guessing the next state.
      const saved: ParticipantProfile | undefined = result ?? undefined;
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
            {editing ? t("participant.dialog.editTitle") : t("participant.dialog.createTitle")}
          </h2>
          <p className="new-participant-subtitle">
            {editing
              ? t("participant.dialog.editDescription")
              : t("participant.dialog.createDescription")}
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
              aria-label={t("participant.avatar.upload")}
              title={t("participant.avatar.upload")}
              onClick={triggerAvatarPicker}
            >
              {form.avatarImage ||
              (participant?.avatar_image && !form.clearAvatarImage) ? (
                <img
                  className="new-participant-avatar-image"
                  src={form.avatarImage ?? participant?.avatar_image}
                  alt={t("participant.avatar.alt")}
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
                <span>{t("participant.avatar.uploadImage")}</span>
              </button>
              {form.avatarImage ||
              (participant?.avatar_image && !form.clearAvatarImage) ? (
                <button
                  type="button"
                  className="new-participant-text-action danger"
                  onClick={clearAvatarImage}
                >
                  <Trash2 aria-hidden="true" />
                  <span>{t("common.remove")}</span>
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
            <span>{t("participant.name")}</span>
            <input
              data-field="name"
              value={form.name}
              autoFocus
              onChange={(event) =>
                updateField("name", event.currentTarget.value)
              }
              placeholder={t("participant.namePlaceholder")}
              onFocus={(event) => event.currentTarget.select()}
            />
          </label>
          <label className="new-participant-field">
            <span>{t("participant.tagline")}</span>
            <input
              data-field="tagline"
              value={form.tagline}
              onChange={(event) =>
                updateField("tagline", event.currentTarget.value)
              }
              placeholder={t("participant.taglinePlaceholder")}
            />
          </label>
          <div className="new-participant-field">
            <span>{t("participant.role")}</span>
            <SelectMenu
              ariaLabel={t("participant.role")}
              dataField="role"
              value={form.role}
              onChange={(next) => updateField("role", next)}
              options={PARTICIPANT_ROLES.map((role) => ({
                value: role,
                label: participantRoleLabel(role),
              }))}
              flip
            />
          </div>
          <div className="new-participant-field">
            <span>{t("participant.model")}</span>
            <SelectMenu
              ariaLabel={t("participant.model")}
              dataField="model"
              value={form.model}
              onChange={(next) => updateField("model", next)}
              groups={modelGroups}
              flip
            />
          </div>
        </div>

        <div className="new-participant-actions">
          <button type="button" onClick={onClose} disabled={saving}>
            {t("common.cancel")}
          </button>
          <button type="submit" disabled={!canSave}>
            {saving ? (
              <Loader2 className="new-participant-spinner" aria-hidden="true" />
            ) : (
              <Save aria-hidden="true" />
            )}
            <span>
              {saving
                ? editing
                  ? t("participant.saving")
                  : t("participant.creating")
                : editing
                  ? t("common.save")
                  : t("common.create")}
            </span>
          </button>
        </div>
      </form>
    </div>,
    document.body,
  );
}
