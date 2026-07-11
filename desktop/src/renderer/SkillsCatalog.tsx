import { AlertTriangle, Bot, Check, Puzzle, RefreshCw, Search, Wrench } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import type {
  AgentTemplateDiagnostic,
  AgentTemplateSummary,
  ExtensionInventoryRecord,
  RuntimeContext,
  SkillSummary
} from "../shared/protocol";

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
          error: error instanceof Error ? error.message : "加载扩展目录失败",
          skills: [],
          agentTemplates: [],
          diagnostics: []
        });
      }
    }
  }, [contextKey]);

  const visibleSkills = useMemo(() => {
    const query = filter.trim().toLowerCase();
    const items = [...state.skills].sort(compareSkills);
    if (!query) {
      return items;
    }
    return items.filter((skill) =>
      [skill.name, skill.description, skill.when_to_use, skill.source, skill.argument_hint]
        .filter(Boolean)
        .some((value) => value?.toLowerCase().includes(query))
    );
  }, [filter, state.skills]);

  const plugins = useMemo(
    () => extensionInventory.filter((record) => record.kind === "plugin"),
    [extensionInventory]
  );

  const visiblePlugins = useMemo(() => {
    const query = filter.trim().toLowerCase();
    const items = [...plugins].sort((left, right) => left.name.localeCompare(right.name));
    if (!query) {
      return items;
    }
    return items.filter((record) =>
      [record.name, record.description, ...(record.requested_permissions ?? [])]
        .filter(Boolean)
        .some((value) => value?.toLowerCase().includes(query))
    );
  }, [filter, plugins]);

  const visibleAgentTemplates = useMemo(() => {
    const query = filter.trim().toLowerCase();
    const items = [...state.agentTemplates].sort(compareAgentTemplates);
    if (!query) {
      return items;
    }
    return items.filter((template) =>
      [template.name, template.description, template.source, template.model, template.permission_mode]
        .filter(Boolean)
        .some((value) => value?.toLowerCase().includes(query))
    );
  }, [filter, state.agentTemplates]);

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
        error: error instanceof Error ? error.message : "加载扩展目录失败",
        skills: [],
        agentTemplates: [],
        diagnostics: []
      });
    }
  }

  return (
    <section className="skills-catalog" aria-label="Skills">
      <header className="skills-catalog-header">
        <div className="skills-catalog-title">
          <strong>技能</strong>
          <span>查看任务技能、已安装插件和可复用的临时子代理模板</span>
        </div>
        <div className="skills-catalog-controls">
          <label className="skills-search">
            <Search className="icon" />
            <input
              type="search"
              value={filter}
              placeholder="搜索技能"
              onChange={(event) => setFilter(event.currentTarget.value)}
            />
          </label>
          <button className="icon-button skills-refresh" type="button" aria-label="刷新 Skills" onClick={() => void refreshSkills()}>
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
        <strong>Installed</strong>
        <span>{state.loading ? "加载中" : `${visibleSkills.length} / ${state.skills.length}`}</span>
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
                  <span className="skill-row-tag" title="随 Wuu 内置分发的官方技能">
                    官方
                  </span>
                ) : null}
                {pluginSkillID(skill.source) ? (
                  <span className="skill-row-tag" title="由插件提供的技能">
                    插件 · {pluginSkillID(skill.source)}
                  </span>
                ) : null}
              </span>
              <p>{skill.description || skill.when_to_use || "无描述"}</p>
            </span>
            <Check className="skill-row-check" aria-hidden="true" />
          </article>
        ))}
      </div>

      {plugins.length > 0 ? (
        <>
          <div className="skills-section-heading">
            <strong>Plugins</strong>
            <span>{state.loading ? "加载中" : `${visiblePlugins.length} / ${plugins.length}`}</span>
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
                      <span className="skill-row-tag" title="随 Wuu 内置分发的官方插件">
                        官方
                      </span>
                    ) : null}
                  </span>
                  <p>{record.description || "无描述"}</p>
                </span>
                <Check className="skill-row-check" aria-hidden="true" />
              </article>
            ))}
          </div>
        </>
      ) : null}

      <div className="skills-section-heading">
        <strong>Agent Templates</strong>
        <span>{state.loading ? "加载中" : `${visibleAgentTemplates.length} / ${state.agentTemplates.length}`}</span>
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
                <span className="skill-row-tag" title="调用时创建临时子代理，不是常驻成员">
                  临时子代理模板
                </span>
              </span>
              <p>{template.description || "无描述"}</p>
            </span>
            <Check className="skill-row-check" aria-hidden="true" />
          </article>
        ))}
      </div>

      {!state.loading &&
      visibleSkills.length === 0 &&
      visiblePlugins.length === 0 &&
      visibleAgentTemplates.length === 0 ? (
        <div className="skills-empty">
          <Wrench className="icon-xl" />
          <strong>暂无 Skills</strong>
          <span>{filter.trim() ? "没有匹配项" : "当前运行时未发现 Skills"}</span>
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

function compareSkills(left: SkillSummary, right: SkillSummary): number {
  const sourceDelta = sourceRank(left.source) - sourceRank(right.source);
  if (sourceDelta !== 0) {
    return sourceDelta;
  }
  return left.name.localeCompare(right.name);
}

function compareAgentTemplates(left: AgentTemplateSummary, right: AgentTemplateSummary): number {
  const sourceDelta = sourceRank(left.source) - sourceRank(right.source);
  if (sourceDelta !== 0) {
    return sourceDelta;
  }
  return left.name.localeCompare(right.name);
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
