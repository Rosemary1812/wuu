import { Puzzle, RefreshCw, Search, Wrench } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import type {
  AppLocale,
  ExtensionInventoryRecord,
  RuntimeContext,
  SkillSummary,
} from "../shared/protocol";
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

const initialLoadState: LoadState = {
  loading: true,
  error: "",
  skills: [],
};

export function SkillsCatalog({
  activeContext,
  extensionInventory = [],
  onTrySkill,
}: {
  activeContext?: RuntimeContext;
  extensionInventory?: ExtensionInventoryRecord[];
  onTrySkill?: (skill: SkillSummary) => void;
}): JSX.Element {
  const { locale, t } = useI18n();
  const [state, setState] = useState<LoadState>(initialLoadState);
  const [filter, setFilter] = useState("");
  const [previewSkill, setPreviewSkill] = useState<SkillSummary | null>(null);
  const contextKey = activeContext ? runtimeContextKey(activeContext) : "";

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
    setState((current) => ({ ...current, loading: true, error: "" }));
    try {
      const [skillsResult] = await Promise.all([window.wuu.listSkills()]);
      setState({
        loading: false,
        error: "",
        skills: skillsResult.skills,
      });
    } catch (error) {
      setState({
        loading: false,
        error:
          error instanceof Error ? error.message : translateCurrent("skills.loadFailed"),
        skills: [],
      });
    }
  }

  return (
    <section className="skills-catalog" aria-label={t("skills.catalogLabel")}>
      <header className="skills-catalog-header">
        <div className="skills-catalog-title">
          <strong>{t("skills.title")}</strong>
        </div>
        <div className="skills-catalog-controls">
          <label className="skills-search">
            <Search className="icon" />
            <input
              type="search"
              value={filter}
              placeholder={t("skills.searchPlaceholder")}
              onChange={(event) => setFilter(event.currentTarget.value)}
            />
          </label>
          <button
            className="icon-button skills-refresh"
            type="button"
            aria-label={t("skills.refresh")}
            onClick={() => void refreshSkills()}
          >
            <RefreshCw className="icon" />
          </button>
        </div>
      </header>

      {state.error ? <div className="skills-catalog-error">{state.error}</div> : null}

      <div className="skills-section-heading">
        <strong>{t("skills.sectionSkills")}</strong>
      </div>

      <div className="skills-list">
        {visibleSkills.map((skill) => (
          <button
            key={`${skill.source}:${skill.name}`}
            className="skill-row skill-row-button"
            type="button"
            aria-label={t("skills.previewSkill", { name: skill.name })}
            onClick={() => setPreviewSkill(skill)}
          >
            <span className="skill-row-icon" aria-hidden="true">
              <Wrench className="icon" />
            </span>
            <span className="skill-row-copy">
              <span className="skill-row-titlebar">
                <h2>{skill.name}</h2>
                {isBundledSkill(skill.source) ? (
                  <span className="skill-row-tag" title={t("skills.officialSkillTitle")}>
                    {t("skills.official")}
                  </span>
                ) : null}
                {pluginSkillID(skill.source) ? (
                  <span className="skill-row-tag" title={t("skills.pluginSkillTitle")}>
                    {t("skills.pluginTag", { id: pluginSkillID(skill.source) })}
                  </span>
                ) : null}
              </span>
              {skill.description || skill.when_to_use ? (
                <p>{skill.description || skill.when_to_use}</p>
              ) : null}
            </span>
          </button>
        ))}
      </div>

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

      {plugins.length > 0 ? (
        <>
          <div className="skills-section-heading">
            <strong>{t("skills.sectionPlugins")}</strong>
          </div>

          <div className="skills-list">
            {visiblePlugins.map((record) => (
              <article key={record.id} className="skill-row">
                <span className="skill-row-icon" aria-hidden="true">
                  <Puzzle className="icon" />
                </span>
                <span className="skill-row-copy">
                  <span className="skill-row-titlebar">
                    <h2>{record.name}</h2>
                    {record.provenance.official ? (
                      <span className="skill-row-tag" title={t("skills.officialPluginTitle")}>
                        {t("skills.official")}
                      </span>
                    ) : null}
                  </span>
                  {record.description ? <p>{record.description}</p> : null}
                </span>
              </article>
            ))}
          </div>
        </>
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
