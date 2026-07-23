import { AlertTriangle, Bot, Puzzle, RefreshCw, Search, Wrench } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import type {
  AppLocale,
  AgentTemplateDiagnostic,
  AgentTemplateSummary,
  ExtensionInventoryRecord,
  RuntimeContext,
  SkillSummary
} from "../shared/protocol";
import { translateCurrent, useI18n } from "./i18n";

type LoadState = {
  loading: boolean;
  error: string;
  skills: SkillSummary[];
  agentTemplates: AgentTemplateSummary[];
  diagnostics: AgentTemplateDiagnostic[];
};

const initialLoadState: LoadState = {
  loading: true,
  error: "",
  skills: [],
  agentTemplates: [],
  diagnostics: []
};

export function SkillsCatalog({
  activeContext,
  extensionInventory = []
}: {
  activeContext?: RuntimeContext;
  extensionInventory?: ExtensionInventoryRecord[];
}): JSX.Element {
  const { locale, t } = useI18n();
  const [state, setState] = useState<LoadState>(initialLoadState);
  const [filter, setFilter] = useState("");
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
        const [skillsResult, templatesResult] = await Promise.all([
          window.wuu.listSkills(),
          window.wuu.listAgentTemplates()
        ]);
        if (cancelled) {
          return;
        }
        setState({
          loading: false,
          error: "",
          skills: skillsResult.skills,
          agentTemplates: templatesResult.templates,
          diagnostics: templatesResult.diagnostics ?? []
        });
      } catch (error) {
        if (cancelled) {
          return;
        }
        setState({
          loading: false,
          error: error instanceof Error ? error.message : translateCurrent("skills.loadFailed"),
          skills: [],
          agentTemplates: [],
          diagnostics: []
        });
      }
    }
  }, [contextKey, locale]);

  const visibleSkills = useMemo(() => {
    const query = filter.trim().toLowerCase();
    const items = [...state.skills].sort((left, right) => compareSkills(left, right, locale));
    if (!query) {
      return items;
    }
    return items.filter((skill) =>
      [skill.name, skill.description, skill.when_to_use, skill.source, skill.argument_hint]
        .filter(Boolean)
        .some((value) => value?.toLowerCase().includes(query))
    );
  }, [filter, locale, state.skills]);

  const plugins = useMemo(
    () => extensionInventory.filter((record) => record.kind === "plugin"),
    [extensionInventory]
  );

  const visiblePlugins = useMemo(() => {
    const query = filter.trim().toLowerCase();
    const items = [...plugins].sort((left, right) => left.name.localeCompare(right.name, locale));
    if (!query) {
      return items;
    }
    return items.filter((record) =>
      [record.name, record.description, ...(record.requested_permissions ?? [])]
        .filter(Boolean)
        .some((value) => value?.toLowerCase().includes(query))
    );
  }, [filter, locale, plugins]);

  const visibleAgentTemplates = useMemo(() => {
    const query = filter.trim().toLowerCase();
    const items = [...state.agentTemplates].sort((left, right) => compareAgentTemplates(left, right, locale));
    if (!query) {
      return items;
    }
    return items.filter((template) =>
      [template.name, template.description, template.source, template.model, template.permission_mode]
        .filter(Boolean)
        .some((value) => value?.toLowerCase().includes(query))
    );
  }, [filter, locale, state.agentTemplates]);

  async function refreshSkills(): Promise<void> {
    setState((current) => ({ ...current, loading: true, error: "" }));
    try {
      const [skillsResult, templatesResult] = await Promise.all([
        window.wuu.listSkills(),
        window.wuu.listAgentTemplates()
      ]);
      setState({
        loading: false,
        error: "",
        skills: skillsResult.skills,
        agentTemplates: templatesResult.templates,
        diagnostics: templatesResult.diagnostics ?? []
      });
    } catch (error) {
      setState({
        loading: false,
        error: error instanceof Error ? error.message : translateCurrent("skills.loadFailed"),
        skills: [],
        agentTemplates: [],
        diagnostics: []
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
          <button className="icon-button skills-refresh" type="button" aria-label={t("skills.refresh")} onClick={() => void refreshSkills()}>
            <RefreshCw className="icon" />
          </button>
        </div>
      </header>

      {state.error ? <div className="skills-catalog-error">{state.error}</div> : null}

      {state.diagnostics.length > 0 ? (
        <div className="skills-catalog-error">
          <AlertTriangle className="icon-sm" aria-hidden="true" />
          {state.diagnostics.map((diagnostic) => (
            <span key={`${diagnostic.path}:${diagnostic.message}`}>
              {diagnostic.path}: {diagnostic.message}
            </span>
          ))}
        </div>
      ) : null}

      <div className="skills-section-heading">
        <strong>{t("skills.sectionSkills")}</strong>
      </div>

      <div className="skills-list">
        {visibleSkills.map((skill) => (
          <article key={`${skill.source}:${skill.name}`} className="skill-row">
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
              {skill.description || skill.when_to_use ? <p>{skill.description || skill.when_to_use}</p> : null}
            </span>
          </article>
        ))}
      </div>

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

      <div className="skills-section-heading">
        <strong>{t("skills.sectionAgentTemplates")}</strong>
      </div>

      <div className="skills-list">
        {visibleAgentTemplates.map((template) => (
          <article key={`${template.source}:${template.name}`} className="skill-row">
            <span className="skill-row-icon" aria-hidden="true">
              <Bot className="icon" />
            </span>
            <span className="skill-row-copy">
              <span className="skill-row-titlebar">
                <h2>{template.name}</h2>
              </span>
              {template.description ? <p>{template.description}</p> : null}
            </span>
          </article>
        ))}
      </div>

      {!state.loading &&
      visibleSkills.length === 0 &&
      visiblePlugins.length === 0 &&
      visibleAgentTemplates.length === 0 ? (
        <div className="skills-empty">
          <Wrench className="icon-xl" />
          <strong>{t("skills.empty")}</strong>
          <span>{filter.trim() ? t("skills.noMatches") : t("skills.noneInRuntime")}</span>
        </div>
      ) : null}
    </section>
  );
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

function compareAgentTemplates(left: AgentTemplateSummary, right: AgentTemplateSummary, locale: AppLocale): number {
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
