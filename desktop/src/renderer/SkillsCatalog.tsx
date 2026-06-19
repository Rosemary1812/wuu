import { RefreshCw, Search, Wrench } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import type { RuntimeContext, SkillSummary } from "../shared/protocol";

type LoadState = {
  loading: boolean;
  error: string;
  skills: SkillSummary[];
};

const initialLoadState: LoadState = {
  loading: true,
  error: "",
  skills: []
};

export function SkillsCatalog({
  activeContext,
  onUseSkill
}: {
  activeContext?: RuntimeContext;
  onUseSkill: (name: string) => void;
}): JSX.Element {
  const [state, setState] = useState<LoadState>(initialLoadState);
  const [filter, setFilter] = useState("");
  const contextKey = activeContext ? runtimeContextKey(activeContext) : "";

  useEffect(() => {
    let cancelled = false;
    void loadSkills(cancelled);
    return () => {
      cancelled = true;
    };

    async function loadSkills(alreadyCancelled: boolean): Promise<void> {
      if (alreadyCancelled) {
        return;
      }
      setState((current) => ({ ...current, loading: true, error: "" }));
      try {
        const result = await window.wuu.listSkills();
        if (cancelled) {
          return;
        }
        setState({ loading: false, error: "", skills: result.skills });
      } catch (error) {
        if (cancelled) {
          return;
        }
        setState({
          loading: false,
          error: error instanceof Error ? error.message : "加载 Skills 失败",
          skills: []
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

  async function refreshSkills(): Promise<void> {
    setState((current) => ({ ...current, loading: true, error: "" }));
    try {
      const result = await window.wuu.listSkills();
      setState({ loading: false, error: "", skills: result.skills });
    } catch (error) {
      setState({
        loading: false,
        error: error instanceof Error ? error.message : "加载 Skills 失败",
        skills: []
      });
    }
  }

  return (
    <section className="skills-catalog" aria-label="Skills">
      <header className="skills-catalog-header">
        <div className="skills-catalog-title">
          <Wrench className="icon-lg" />
          <strong>Skills</strong>
          <span>{state.loading ? "加载中" : `${state.skills.length} 项`}</span>
        </div>
        <div className="skills-catalog-controls">
          <label className="skills-search">
            <Search className="icon" />
            <input
              type="search"
              value={filter}
              placeholder="搜索 Skills"
              onChange={(event) => setFilter(event.currentTarget.value)}
            />
          </label>
          <button className="icon-button skills-refresh" type="button" aria-label="刷新 Skills" onClick={() => void refreshSkills()}>
            <RefreshCw className="icon" />
          </button>
        </div>
      </header>

      {state.error ? <div className="skills-catalog-error">{state.error}</div> : null}

      <div className="skills-grid">
        {visibleSkills.map((skill) => (
          <article key={`${skill.source}:${skill.name}`} className="skill-card">
            <div className="skill-card-main">
              <div className="skill-card-title-row">
                <h2>/{skill.name}</h2>
                <span className={`skill-source ${sourceClass(skill.source)}`}>{sourceLabel(skill.source)}</span>
              </div>
              <p>{skill.description || skill.when_to_use || "无描述"}</p>
            </div>
            <div className="skill-card-meta">
              {skill.argument_hint ? <span>{skill.argument_hint}</span> : null}
              {skill.model ? <span>{skill.model}</span> : null}
              {skill.context ? <span>{skill.context}</span> : null}
              {skill.paths?.length ? <span>{skill.paths[0]}</span> : null}
            </div>
            <footer className="skill-card-footer">
              <span className="skill-card-path">{skill.path ? shortPath(skill.path) : sourceLabel(skill.source)}</span>
              <button type="button" onClick={() => onUseSkill(skill.name)}>
                使用
              </button>
            </footer>
          </article>
        ))}
      </div>

      {!state.loading && visibleSkills.length === 0 ? (
        <div className="skills-empty">
          <Wrench className="icon-xl" />
          <strong>暂无 Skills</strong>
          <span>{filter.trim() ? "没有匹配项" : "当前运行时未发现 Skills"}</span>
        </div>
      ) : null}
    </section>
  );
}

function compareSkills(left: SkillSummary, right: SkillSummary): number {
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

function sourceLabel(source: string): string {
  switch (source) {
    case "bundled":
      return "内置";
    case "project":
      return "项目";
    case "user":
      return "用户";
    default:
      return source || "未知";
  }
}

function sourceClass(source: string): string {
  return source === "bundled" || source === "project" || source === "user" ? source : "other";
}

function shortPath(path: string): string {
  const parts = path.split(/[\\/]/).filter(Boolean);
  return parts.slice(-3).join("/");
}

function runtimeContextKey(context: RuntimeContext): string {
  return context.kind === "project" ? `project:${context.project_id}` : `no_project:${context.cwd}`;
}
