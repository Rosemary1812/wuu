import {
  ChevronRight,
  RefreshCw,
  Wrench,
} from "lucide-react";
import { useEffect, useId, useMemo, useRef, useState, type ReactNode } from "react";
import type {
  AppLocale,
  ExtensionInventoryRecord,
  ExtensionPackageAction,
  ExtensionPackageUpdateParams,
  RuntimeContext,
  SkillSummary,
} from "../shared/protocol";
import { CatalogSearchField } from "./CatalogSearchField";
import { translateCurrent, useI18n } from "./i18n";
import { Modal } from "./Modal";
import { RichContent } from "./RichContent";

type LoadState = {
  loading: boolean;
  error: string;
  skills: SkillSummary[];
};

type SkillContentState = {
  loading: boolean;
  error: string;
  content: string;
};

type ManagedExtensionPackage = ExtensionInventoryRecord & {
  approval_state?: "official" | "pending" | "granted" | "changed" | "rejected";
  runtime_state?: "inactive" | "starting" | "active" | "failed" | "stopping" | "stopped";
  enabled?: boolean;
  version?: string;
  last_error?: string;
  contributions?: {
    commands?: unknown[];
    settings?: unknown[];
    themes?: unknown[];
  };
};

const initialLoadState: LoadState = {
  loading: true,
  error: "",
  skills: [],
};

export function SkillsCatalog({
  activeContext,
  extensionInventory = [],
  onTrySkill,
  onRefreshCatalog,
  onUpdateExtensionPackage,
}: {
  activeContext?: RuntimeContext;
  extensionInventory?: ExtensionInventoryRecord[];
  onTrySkill?: (skill: SkillSummary) => void;
  onRefreshCatalog?: () => Promise<SkillSummary[] | undefined>;
  onUpdateExtensionPackage?: (update: ExtensionPackageUpdateParams) => Promise<void>;
}): JSX.Element {
  const { locale, t } = useI18n();
  const [state, setState] = useState<LoadState>(initialLoadState);
  const [filter, setFilter] = useState("");
  const [previewSkill, setPreviewSkill] = useState<SkillSummary | null>(null);
  const [packageMutation, setPackageMutation] = useState("");
  const [packageMutationError, setPackageMutationError] = useState("");
  const contextKey = activeContext ? runtimeContextKey(activeContext) : "";
  const contextKeyRef = useRef(contextKey);
  contextKeyRef.current = contextKey;

  useEffect(() => {
    let cancelled = false;
    void loadCatalog(cancelled);
    return () => {
      cancelled = true;
    };

    async function loadCatalog(alreadyCancelled: boolean): Promise<void> {
      if (alreadyCancelled) {
        return;
      }
      setState((current) => ({ ...current, loading: true, error: "" }));
      try {
        const [skillsResult] = await Promise.all([window.wuu.listSkills()]);
        if (cancelled) {
          return;
        }
        setState({
          loading: false,
          error: "",
          skills: skillsResult.skills,
        });
      } catch (error) {
        if (cancelled) {
          return;
        }
        setState({
          loading: false,
          error:
            error instanceof Error
              ? error.message
              : translateCurrent("skills.loadFailed"),
          skills: [],
        });
      }
    }
  }, [contextKey, locale]);

  const visibleSkills = useMemo(() => {
    const query = filter.trim().toLowerCase();
    const items = [...state.skills].sort((left, right) =>
      compareSkills(left, right, locale),
    );
    if (!query) {
      return items;
    }
    return items.filter((skill) =>
      [skill.name, skill.description, skill.when_to_use, skill.source, skill.argument_hint]
        .filter(Boolean)
        .some((value) => value?.toLowerCase().includes(query)),
    );
  }, [filter, locale, state.skills]);

  const officialSkills = useMemo(
    () => visibleSkills.filter((skill) => isBundledSkill(skill.source)),
    [visibleSkills],
  );
  const personalSkills = useMemo(
    () => visibleSkills.filter((skill) => !isBundledSkill(skill.source)),
    [visibleSkills],
  );

  const plugins = useMemo(
    () => extensionInventory.filter((record) => record.kind === "plugin"),
    [extensionInventory],
  );

  const visiblePlugins = useMemo(() => {
    const query = filter.trim().toLowerCase();
    const items = [...plugins].sort((left, right) =>
      left.name.localeCompare(right.name, locale),
    );
    if (!query) {
      return items;
    }
    return items.filter((record) =>
      [record.name, record.description, ...(record.requested_permissions ?? [])]
        .filter(Boolean)
        .some((value) => value?.toLowerCase().includes(query)),
    );
  }, [filter, locale, plugins]);

  async function refreshSkills(): Promise<void> {
    const requestedContextKey = contextKey;
    setState((current) => ({ ...current, loading: true, error: "" }));
    try {
      const skills = onRefreshCatalog
        ? await onRefreshCatalog()
        : (await window.wuu.listSkills()).skills;
      if (!skills || contextKeyRef.current !== requestedContextKey) {
        return;
      }
      setState({
        loading: false,
        error: "",
        skills,
      });
    } catch (error) {
      if (contextKeyRef.current !== requestedContextKey) {
        return;
      }
      setState({
        loading: false,
        error:
          error instanceof Error ? error.message : translateCurrent("skills.loadFailed"),
        skills: [],
      });
    }
  }

  async function updateExtensionPackage(record: ExtensionInventoryRecord, action: ExtensionPackageAction): Promise<void> {
    if (!onUpdateExtensionPackage || packageMutation) {
      return;
    }
    setPackageMutation(`${record.id}:${action}`);
    setPackageMutationError("");
    try {
      await onUpdateExtensionPackage({ id: record.id, fingerprint: record.fingerprint, action });
    } catch (error) {
      setPackageMutationError(error instanceof Error ? error.message : translateCurrent("skills.pluginUpdateFailed"));
    } finally {
      setPackageMutation("");
    }
  }

  return (
    <section className="skills-catalog" aria-label={t("skills.catalogLabel")}>
      <header className="catalog-page-header">
        <div className="catalog-page-title">
          <strong>{t("skills.title")}</strong>
          <span>{t("skills.subtitle")}</span>
        </div>
        <div className="catalog-page-controls">
          <CatalogSearchField
            value={filter}
            placeholder={t("skills.searchPlaceholder")}
            onValueChange={setFilter}
          />
          <button
            className="icon-button catalog-refresh"
            type="button"
            aria-label={t("skills.refresh")}
            onClick={() => void refreshSkills()}
          >
            <RefreshCw className="icon" />
          </button>
        </div>
      </header>

      {state.error ? <div className="skills-catalog-error">{state.error}</div> : null}

      {officialSkills.length > 0 ? (
        <CatalogSection title={t("skills.sectionOfficial")} className="skills-section-official">
          <SkillsList skills={officialSkills} onPreview={setPreviewSkill} />
        </CatalogSection>
      ) : null}

      {personalSkills.length > 0 ? (
        <CatalogSection title={t("skills.sectionPersonal")}>
          <SkillsList skills={personalSkills} onPreview={setPreviewSkill} />
        </CatalogSection>
      ) : null}

      {previewSkill ? (
        <SkillPreviewDialog
          skill={previewSkill}
          onClose={() => setPreviewSkill(null)}
          onTry={() => {
            const skill = previewSkill;
            setPreviewSkill(null);
            onTrySkill?.(skill);
          }}
        />
      ) : null}

      {packageMutationError ? <div className="skills-catalog-error">{packageMutationError}</div> : null}

      {plugins.length > 0 ? (
        <CatalogSection title={t("skills.sectionPlugins")}>
          <div className="skills-list extension-package-list">
            {visiblePlugins.map((record) => {
              const managed = record as ManagedExtensionPackage;
              const primaryAction = extensionPackagePrimaryAction(managed);
              const secondaryAction = extensionPackageSecondaryAction(managed);
              const mutating = packageMutation.startsWith(`${record.id}:`);
              const grantUnavailable = primaryAction === "grant" && !record.fingerprint;
              return (
              <article key={record.id} className="skill-row extension-package-row">
                <SkillArtwork
                  name={record.name}
                  official={record.provenance.official === true}
                  kind="plugin"
                />
                <span className="skill-row-copy">
                  <span className="skill-row-titlebar">
                    <h2>{record.name}</h2>
                    {record.provenance.official ? (
                      <span className="skill-row-tag" title={t("skills.officialPluginTitle")}>
                        {t("skills.official")}
                      </span>
                    ) : null}
                    <span className={`skill-row-tag skill-row-tag-neutral extension-status extension-status-${extensionPackageTone(managed)}`}>
                      {extensionPackageStatusLabel(managed, t)}
                    </span>
                  </span>
                  {record.description ? <p>{record.description}</p> : null}
                  <span className="extension-package-meta">
                    {managed.version ? <span>v{managed.version}</span> : null}
                    <span>{t("skills.pluginScope", { scope: record.provenance.scope })}</span>
                    <span>{extensionContributionSummary(managed, t)}</span>
                  </span>
                  <span className="extension-package-permissions">
                    <strong>{t("skills.pluginPermissions")}</strong>
                    {(record.requested_permissions ?? []).length > 0
                      ? record.requested_permissions?.map((permission) => <code key={permission}>{permission}</code>)
                      : <span>{t("skills.pluginNoPermissions")}</span>}
                  </span>
                  {managed.runtime_state === "failed" && managed.last_error ? (
                    <span className="extension-package-error">{managed.last_error}</span>
                  ) : null}
                </span>
                {onUpdateExtensionPackage ? (
                  <span className="extension-package-actions">
                    <button
                      type="button"
                      className="secondary-button"
                      disabled={Boolean(packageMutation) || grantUnavailable}
                      onClick={() => void updateExtensionPackage(record, primaryAction)}
                    >
                      {mutating ? t("skills.pluginUpdating") : extensionPackageActionLabel(managed, primaryAction, t)}
                    </button>
                    {secondaryAction ? (
                      <button
                        type="button"
                        className="text-button"
                        disabled={Boolean(packageMutation)}
                        onClick={() => void updateExtensionPackage(record, secondaryAction)}
                      >
                        {extensionPackageActionLabel(managed, secondaryAction, t)}
                      </button>
                    ) : null}
                  </span>
                ) : null}
              </article>
              );
            })}
          </div>
        </CatalogSection>
      ) : null}

      {!state.loading && visibleSkills.length === 0 && visiblePlugins.length === 0 ? (
        <div className="skills-empty">
          <Wrench className="icon-xl" />
          <strong>{t("skills.empty")}</strong>
          <span>{filter.trim() ? t("skills.noMatches") : t("skills.noneInRuntime")}</span>
        </div>
      ) : null}
    </section>
  );
}

function extensionPackageApproval(record: ManagedExtensionPackage): NonNullable<ManagedExtensionPackage["approval_state"]> {
  if (record.approval_state) {
    return record.approval_state;
  }
  return record.provenance.official ? "official" : "pending";
}

function extensionPackagePrimaryAction(record: ManagedExtensionPackage): ExtensionPackageAction {
  const approval = extensionPackageApproval(record);
  if (approval === "pending" || approval === "changed" || approval === "rejected") {
    return "grant";
  }
  return record.enabled === false ? "enable" : "disable";
}

function extensionPackageSecondaryAction(record: ManagedExtensionPackage): ExtensionPackageAction | undefined {
  const approval = extensionPackageApproval(record);
  if (approval === "pending" || approval === "changed") {
    return "reject";
  }
  if (approval === "granted") {
    return "revoke";
  }
  return undefined;
}

function extensionPackageTone(record: ManagedExtensionPackage): "good" | "warning" | "danger" | "muted" {
  if (record.runtime_state === "failed" || extensionPackageApproval(record) === "changed") {
    return "danger";
  }
  if (record.runtime_state === "active" || extensionPackageApproval(record) === "official") {
    return "good";
  }
  if (extensionPackageApproval(record) === "pending") {
    return "warning";
  }
  return "muted";
}

function extensionPackageStatusLabel(record: ManagedExtensionPackage, t: ReturnType<typeof useI18n>["t"]): string {
  if (record.runtime_state === "failed") return t("skills.pluginStatusFailed");
  if (record.runtime_state === "starting") return t("skills.pluginStatusStarting");
  if (record.runtime_state === "active") return t("skills.pluginStatusActive");
  switch (extensionPackageApproval(record)) {
    case "official": return t("skills.pluginStatusOfficial");
    case "granted": return record.enabled === false ? t("skills.pluginStatusDisabled") : t("skills.pluginStatusGranted");
    case "changed": return t("skills.pluginStatusChanged");
    case "rejected": return t("skills.pluginStatusRejected");
    case "pending": return t("skills.pluginStatusPending");
  }
}

function extensionPackageActionLabel(
  record: ManagedExtensionPackage,
  action: ExtensionPackageAction,
  t: ReturnType<typeof useI18n>["t"],
): string {
  if (action === "grant") {
    return extensionPackageApproval(record) === "changed" ? t("skills.pluginReauthorize") : t("skills.pluginGrant");
  }
  if (action === "reject") return t("skills.pluginReject");
  if (action === "revoke") return t("skills.pluginRevoke");
  if (action === "enable") return t("skills.pluginEnable");
  return t("skills.pluginDisable");
}

function extensionContributionSummary(record: ManagedExtensionPackage, t: ReturnType<typeof useI18n>["t"]): string {
  const commands = record.contributions?.commands?.length ?? 0;
  const settings = record.contributions?.settings?.length ?? 0;
  const themes = record.contributions?.themes?.length ?? 0;
  return t("skills.pluginContributions", { commands, settings, themes });
}

function CatalogSection({
  title,
  className = "",
  children,
}: {
  title: string;
  className?: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <section className={`skills-section ${className}`.trim()}>
      <div className="skills-section-heading">
        <strong>{title}</strong>
      </div>
      {children}
    </section>
  );
}

function SkillsList({
  skills,
  onPreview,
}: {
  skills: SkillSummary[];
  onPreview: (skill: SkillSummary) => void;
}): JSX.Element {
  const { t } = useI18n();

  return (
    <div className="skills-list">
      {skills.map((skill) => (
        <button
          key={`${skill.source}:${skill.name}`}
          className="skill-row skill-row-button"
          type="button"
          aria-label={t("skills.previewSkill", { name: skill.name })}
          onClick={() => onPreview(skill)}
        >
          <SkillArtwork name={skill.name} official={isBundledSkill(skill.source)} kind="skill" />
          <span className="skill-row-copy">
            <span className="skill-row-titlebar">
              <h2>{skill.name}</h2>
              {pluginSkillID(skill.source) ? (
                <span className="skill-row-tag skill-row-tag-neutral" title={t("skills.pluginSkillTitle")}>
                  {t("skills.pluginTag", { id: pluginSkillID(skill.source) })}
                </span>
              ) : null}
            </span>
            {catalogSkillDescription(skill) ? <p>{catalogSkillDescription(skill)}</p> : null}
          </span>
          <ChevronRight className="skill-row-chevron" aria-hidden="true" />
        </button>
      ))}
    </div>
  );
}

type SkillArtworkVariant =
  | "official-browser"
  | "official-commit"
  | "official-goal"
  | "official-presentation"
  | "official-creator"
  | "official-plugin"
  | "official-default"
  | "custom-plugin"
  | "custom-skill";

type CustomSkillMotif = "orbit" | "ribbon" | "bloom" | "spark";

function SkillArtwork({
  name,
  official,
  kind,
}: {
  name: string;
  official: boolean;
  kind: "skill" | "plugin";
}): JSX.Element {
  const variant = skillArtworkVariant(name, official, kind);
  const customIdentity = variant.startsWith("custom-")
    ? customSkillArtworkIdentity(name)
    : null;
  return (
    <span
      className={[
        "skill-artwork",
        `skill-artwork-${variant}`,
        customIdentity ? `skill-artwork-palette-${customIdentity.palette}` : "",
      ].filter(Boolean).join(" ")}
      data-skill-artwork={variant}
      data-skill-motif={customIdentity?.motif}
      aria-hidden="true"
    >
      {skillArtworkIcon(variant, customIdentity?.motif)}
    </span>
  );
}

function skillArtworkVariant(
  name: string,
  official: boolean,
  kind: "skill" | "plugin",
): SkillArtworkVariant {
  if (!official) {
    return kind === "plugin" ? "custom-plugin" : "custom-skill";
  }
  if (kind === "plugin") {
    return "official-plugin";
  }
  switch (name) {
    case "browser":
      return "official-browser";
    case "commit":
      return "official-commit";
    case "long-running-goal":
      return "official-goal";
    case "pptx-generator":
      return "official-presentation";
    case "skill-creator":
      return "official-creator";
    default:
      return "official-default";
  }
}

function catalogSkillDescription(skill: SkillSummary): string {
  const description = (skill.description || skill.when_to_use || "").trim();
  if (!description) {
    return "";
  }
  const firstSentence = description.match(/^.*?[。！？.!?](?=\s|$)/u)?.[0];
  return firstSentence?.trim() || description;
}

function skillArtworkIcon(
  variant: SkillArtworkVariant,
  customMotif?: CustomSkillMotif,
): JSX.Element {
  switch (variant) {
    case "custom-plugin":
      return <CustomSkillMark kind="plugin" motif={customMotif ?? "orbit"} />;
    case "custom-skill":
      return <CustomSkillMark kind="skill" motif={customMotif ?? "orbit"} />;
    default:
      return <OfficialSkillMark variant={variant} />;
  }
}

function customSkillArtworkIdentity(name: string): {
  palette: number;
  motif: CustomSkillMotif;
} {
  const motifs: CustomSkillMotif[] = ["orbit", "ribbon", "bloom", "spark"];
  const hash = stableSkillHash(name);
  return {
    palette: hash % 8,
    motif: motifs[Math.floor(hash / 8) % motifs.length],
  };
}

function stableSkillHash(name: string): number {
  let hash = 2166136261;
  for (let index = 0; index < name.length; index += 1) {
    hash ^= name.charCodeAt(index);
    hash = Math.imul(hash, 16777619);
  }
  return hash >>> 0;
}

function CustomSkillMark({
  kind,
  motif,
}: {
  kind: "skill" | "plugin";
  motif: CustomSkillMotif;
}): JSX.Element {
  const gradientID = `custom-skill-mark-${useId().replaceAll(":", "")}`;
  const gradient = (
    <defs>
      <linearGradient id={gradientID} x1="4" y1="3" x2="28" y2="29" gradientUnits="userSpaceOnUse">
        <stop className="skill-mark-stop-a" />
        <stop offset="0.56" className="skill-mark-stop-b" />
        <stop offset="1" className="skill-mark-stop-c" />
      </linearGradient>
    </defs>
  );

  if (kind === "plugin") {
    return (
      <svg viewBox="0 0 32 32" focusable="false">
        {gradient}
        <rect x="4" y="4" width="24" height="24" rx="7" fill={`url(#${gradientID})`} />
        <path className="skill-mark-color-c-fill skill-mark-translucent" d="M19 4h9v9.2a4 4 0 0 0-5.2 5.2H19Z" />
        <path className="skill-mark-light-stroke skill-mark-stroke-wide" d="M10 11h4a2.5 2.5 0 1 1 5 0h3v3.5a2.5 2.5 0 1 1 0 5V22h-3.5a2.5 2.5 0 1 1-5 0H10v-3a2.5 2.5 0 1 1 0-5Z" />
      </svg>
    );
  }

  switch (motif) {
    case "orbit":
      return (
        <svg viewBox="0 0 32 32" focusable="false">
          {gradient}
          <rect x="4" y="4" width="24" height="24" rx="8" fill={`url(#${gradientID})`} />
          <circle className="skill-mark-color-c-fill skill-mark-translucent" cx="11" cy="11" r="5" />
          <ellipse className="skill-mark-light-stroke skill-mark-stroke-wide" cx="16" cy="16" rx="9" ry="5" transform="rotate(-28 16 16)" />
          <circle className="skill-mark-light-fill" cx="23.5" cy="11.3" r="2.1" />
          <circle className="skill-mark-light-fill skill-mark-muted" cx="14.5" cy="17" r="2.8" />
        </svg>
      );
    case "ribbon":
      return (
        <svg viewBox="0 0 32 32" focusable="false">
          {gradient}
          <rect x="4" y="4" width="24" height="24" rx="8" fill={`url(#${gradientID})`} />
          <path className="skill-mark-color-c-fill skill-mark-translucent" d="M4 20.5 16.2 5.2 28 8.5v8.2L17.2 28H8Z" />
          <path className="skill-mark-light-stroke skill-mark-stroke-wide" d="m8.5 18 6-8 9 2.5-5.5 9Z" />
          <path className="skill-mark-light-fill skill-mark-muted" d="m14.5 10 3.5 11.5 5.5-9Z" />
        </svg>
      );
    case "bloom":
      return (
        <svg viewBox="0 0 32 32" focusable="false">
          {gradient}
          <circle cx="16" cy="16" r="12" fill={`url(#${gradientID})`} />
          <ellipse className="skill-mark-color-c-fill skill-mark-translucent" cx="16" cy="10.5" rx="4.2" ry="6" />
          <ellipse className="skill-mark-light-fill skill-mark-muted" cx="21.5" cy="16" rx="6" ry="4.2" />
          <ellipse className="skill-mark-color-c-fill skill-mark-translucent" cx="16" cy="21.5" rx="4.2" ry="6" />
          <ellipse className="skill-mark-light-fill skill-mark-muted" cx="10.5" cy="16" rx="6" ry="4.2" />
          <circle className="skill-mark-light-fill" cx="16" cy="16" r="3" />
          <circle className="skill-mark-color-c-fill" cx="16" cy="16" r="1.4" />
        </svg>
      );
    case "spark":
      return (
        <svg viewBox="0 0 32 32" focusable="false">
          {gradient}
          <rect x="4" y="4" width="24" height="24" rx="8" fill={`url(#${gradientID})`} />
          <circle className="skill-mark-color-c-fill skill-mark-translucent" cx="22.5" cy="10" r="5.5" />
          <path className="skill-mark-light-fill" d="m15 7 2.5 6.1L24 16l-6.5 2.9L15 25l-2.5-6.1L6 16l6.5-2.9Z" />
          <path className="skill-mark-color-c-fill" d="m15 11.5 1.1 3.1 3.4 1.4-3.4 1.4-1.1 3.1-1.1-3.1-3.4-1.4 3.4-1.4Z" />
          <circle className="skill-mark-light-fill" cx="24" cy="8" r="1.5" />
        </svg>
      );
  }
}

function OfficialSkillMark({
  variant,
}: {
  variant: Exclude<SkillArtworkVariant, "custom-plugin" | "custom-skill">;
}): JSX.Element {
  const gradientID = `skill-mark-${variant}`;
  const gradient = (
    <defs>
      <linearGradient id={gradientID} x1="4" y1="3" x2="28" y2="29" gradientUnits="userSpaceOnUse">
        <stop className="skill-mark-stop-a" />
        <stop offset="0.56" className="skill-mark-stop-b" />
        <stop offset="1" className="skill-mark-stop-c" />
      </linearGradient>
    </defs>
  );

  switch (variant) {
    case "official-browser":
      return (
        <svg viewBox="0 0 32 32" focusable="false">
          {gradient}
          <rect x="4" y="4" width="24" height="24" rx="7" fill={`url(#${gradientID})`} />
          <path className="skill-mark-color-c-fill skill-mark-translucent" d="M4 4h24v7.5H4Z" />
          <path className="skill-mark-light-stroke" d="M4.5 11.5h23" />
          <circle className="skill-mark-light-fill" cx="8" cy="8" r="1" />
          <circle className="skill-mark-light-fill skill-mark-muted" cx="11.2" cy="8" r="1" />
          <path className="skill-mark-light-fill" d="m14 14 10 4.2-4.1 1.7-1.7 4.1Z" />
          <path className="skill-mark-color-c-fill" d="m18.2 18.2 5.8 0-4.1 1.7Z" />
        </svg>
      );
    case "official-commit":
      return (
        <svg viewBox="0 0 32 32" focusable="false">
          {gradient}
          <circle cx="16" cy="16" r="12" fill={`url(#${gradientID})`} />
          <path className="skill-mark-color-c-fill skill-mark-translucent" d="M16 4a12 12 0 0 1 12 12v2.5L18.5 28H16Z" />
          <path className="skill-mark-light-stroke skill-mark-stroke-wide" d="M10 9v8a5 5 0 0 0 5 5h7" />
          <circle className="skill-mark-light-fill" cx="10" cy="9" r="2.2" />
          <circle className="skill-mark-light-fill" cx="22" cy="22" r="2.2" />
          <circle className="skill-mark-color-c-fill" cx="10" cy="17" r="2.2" />
        </svg>
      );
    case "official-goal":
      return (
        <svg viewBox="0 0 32 32" focusable="false">
          {gradient}
          <circle cx="16" cy="16" r="12" fill={`url(#${gradientID})`} />
          <circle className="skill-mark-color-c-stroke skill-mark-stroke-wide" cx="16" cy="16" r="8" />
          <circle className="skill-mark-light-stroke skill-mark-stroke-wide" cx="16" cy="16" r="3.5" />
          <path className="skill-mark-light-stroke skill-mark-stroke-wide" d="m18.5 13.5 8-8m-4.2.4 4.4-.7-.7 4.4" />
          <circle className="skill-mark-light-fill" cx="16" cy="16" r="1.5" />
        </svg>
      );
    case "official-presentation":
      return (
        <svg viewBox="0 0 32 32" focusable="false">
          {gradient}
          <path d="M7 4h13l7 7v15a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2Z" fill={`url(#${gradientID})`} />
          <path className="skill-mark-color-c-fill" d="M20 4v5a2 2 0 0 0 2 2h5Z" />
          <rect className="skill-mark-light-fill skill-mark-muted" x="8" y="14" width="16" height="10" rx="3" />
          <path className="skill-mark-color-c-stroke skill-mark-stroke-wide" d="M11.5 20.5h9" />
        </svg>
      );
    case "official-creator":
      return (
        <svg viewBox="0 0 32 32" focusable="false">
          {gradient}
          <rect x="4" y="4" width="24" height="24" rx="8" fill={`url(#${gradientID})`} />
          <path className="skill-mark-color-c-fill skill-mark-translucent" d="M17 4h11v13L17 28h-6Z" />
          <path className="skill-mark-light-fill" d="m18 7 1.8 5.2L25 14l-5.2 1.8L18 21l-1.8-5.2L11 14l5.2-1.8Z" />
          <path className="skill-mark-color-c-fill" d="m18 11 1 2 2 1-2 1-1 2-1-2-2-1 2-1Z" />
          <circle className="skill-mark-light-fill skill-mark-muted" cx="9" cy="23" r="1.6" />
        </svg>
      );
    case "official-plugin":
      return (
        <svg viewBox="0 0 32 32" focusable="false">
          {gradient}
          <rect x="4" y="4" width="24" height="24" rx="7" fill={`url(#${gradientID})`} />
          <path className="skill-mark-color-c-fill skill-mark-translucent" d="M19 4h9v9.2a4 4 0 0 0-5.2 5.2H19Z" />
          <path className="skill-mark-light-stroke skill-mark-stroke-wide" d="M10 11h4a2.5 2.5 0 1 1 5 0h3v3.5a2.5 2.5 0 1 1 0 5V22h-3.5a2.5 2.5 0 1 1-5 0H10v-3a2.5 2.5 0 1 1 0-5Z" />
        </svg>
      );
    case "official-default":
      return (
        <svg viewBox="0 0 32 32" focusable="false">
          {gradient}
          <rect x="4" y="4" width="24" height="24" rx="8" fill={`url(#${gradientID})`} />
          <circle className="skill-mark-color-c-fill skill-mark-translucent" cx="22" cy="10" r="5" />
          <path className="skill-mark-light-fill" d="m16 7 2.1 6.2L24 16l-5.9 2.4L16 25l-2.3-6.6L8 16l5.7-2.8Z" />
          <circle className="skill-mark-color-c-fill" cx="16" cy="16" r="2" />
        </svg>
      );
  }
}

function SkillPreviewDialog({
  skill,
  onClose,
  onTry,
}: {
  skill: SkillSummary;
  onClose: () => void;
  onTry: () => void;
}): JSX.Element {
  const { t } = useI18n();
  const [contentState, setContentState] = useState<SkillContentState>({
    loading: true,
    error: "",
    content: "",
  });

  useEffect(() => {
    let cancelled = false;
    setContentState({ loading: true, error: "", content: "" });
    void window.wuu.readSkillContent({ name: skill.name, source: skill.source })
      .then((result) => {
        if (!cancelled) {
          setContentState({ loading: false, error: "", content: stripFrontmatter(result.content) });
        }
      })
      .catch((error) => {
        if (!cancelled) {
          setContentState({
            loading: false,
            error: error instanceof Error ? error.message : translateCurrent("skills.contentUnavailable"),
            content: fallbackSkillContent(skill),
          });
        }
      });
    return () => {
      cancelled = true;
    };
  }, [skill.name, skill.source]);

  return (
    <Modal
      ariaLabel={t("skills.previewLabel", { name: skill.name })}
      icon={
        <span className="skill-preview-icon-title">
          <Wrench className="icon" />
          <span>{t("skills.skillLabel")}</span>
        </span>
      }
      title={skill.name}
      subtitle={skill.description || skill.when_to_use || skill.trigger_condition}
      panelClassName="skill-preview-dialog"
      onClose={onClose}
      footer={skill.user_invocable ? (
        <button className="settings-button settings-button-primary" type="button" onClick={onTry}>
          {t("skills.tryNow")}
        </button>
      ) : undefined}
    >
      <div className="skill-preview-body">
        {contentState.loading ? (
          <p className="skill-preview-loading">{t("skills.loadingContent")}</p>
        ) : null}
        {contentState.error ? (
          <p className="skill-preview-error">{t("skills.contentFallback")}</p>
        ) : null}
        {contentState.content ? <RichContent text={contentState.content} /> : null}
      </div>
    </Modal>
  );
}

function stripFrontmatter(content: string): string {
  return content.replace(/^---\r?\n[\s\S]*?\r?\n---\r?\n?/, "").trim();
}

function fallbackSkillContent(skill: SkillSummary): string {
  return [
    skill.description,
    skill.when_to_use ? `## When to use\n\n${skill.when_to_use}` : "",
    skill.trigger_condition ? `## Trigger condition\n\n${skill.trigger_condition}` : "",
    skill.examples?.length ? `## Examples\n\n${skill.examples.map((item) => `- ${item}`).join("\n")}` : "",
    skill.verification_checklist?.length
      ? `## Verification checklist\n\n${skill.verification_checklist.map((item) => `- ${item}`).join("\n")}`
      : "",
  ].filter(Boolean).join("\n\n");
}

// Skills compiled into the Wuu binary carry source "bundled"; these are the
// first-party skills we ship and curate, so the catalog flags them.
function isBundledSkill(source: string): boolean {
  return source === "bundled";
}

// Skills discovered from a plugin's skills/ directory carry source
// "plugin:<id>" (plugin.SourceLabel); surface the owning plugin on the row.
function pluginSkillID(source: string): string {
  return source.startsWith("plugin:") ? source.slice("plugin:".length) : "";
}

function compareSkills(left: SkillSummary, right: SkillSummary, locale: AppLocale): number {
  const sourceDelta = sourceRank(left.source) - sourceRank(right.source);
  if (sourceDelta !== 0) {
    return sourceDelta;
  }
  return left.name.localeCompare(right.name, locale);
}

function sourceRank(source: string): number {
  switch (source) {
    case "bundled":
      return 0;
    case "project":
      return 1;
    case "user":
      return 2;
    default:
      return 3;
  }
}

function runtimeContextKey(context: RuntimeContext): string {
  return context.kind === "project" ? `project:${context.project_id}` : `no_project:${context.cwd}`;
}
