import { Check, RefreshCw, Search, Wrench } from "lucide-react";
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
  activeContext
}: {
  activeContext?: RuntimeContext;
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
          <strong>技能</strong>
          <span>通过任务专用技能扩展 Wuu 的能力</span>
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
              <h2>{skill.name}</h2>
              <p>{skill.description || skill.when_to_use || "无描述"}</p>
            </span>
            <Check className="skill-row-check" aria-hidden="true" />
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

function runtimeContextKey(context: RuntimeContext): string {
  return context.kind === "project" ? `project:${context.project_id}` : `no_project:${context.cwd}`;
}
