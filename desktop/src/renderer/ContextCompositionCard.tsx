import { Info, X } from "lucide-react";
import type { ContextCompositionCategory, ThreadContextCompositionResult } from "../shared/protocol";

export type ContextCompositionEntry = {
  id: string;
  threadID: string;
  afterTurnID?: string;
  title?: string;
  result?: ThreadContextCompositionResult;
  loading: boolean;
  error?: string;
};

export function ContextCompositionCard({
  entry,
  onDismiss,
}: {
  entry: ContextCompositionEntry;
  onDismiss?: (id: string) => void;
}): JSX.Element {
  const { result, loading, error } = entry;
  const categories = result?.categories ?? [];
  const contributing = categories.filter((category) => category.contributes !== false);
  const promptTokens = result?.prompt_tokens ?? contributing.reduce((sum, category) => sum + (category.tokens ?? 0), 0);
  const contextWindow = result?.context_window_tokens ?? 0;
  const cacheReadTokens = result?.cache_read_tokens ?? 0;
  const retainedTokens = result?.retained_tokens ?? 0;
  const barTokens = compositionBarTokens(promptTokens, retainedTokens, contextWindow);
  const headlinePercent = contextWindow > 0 ? formatPercent(retainedTokens, contextWindow) : null;
  const runtime = [result?.provider, result?.model].filter(Boolean).join(" · ");

  return (
    <article className="context-composition-card" aria-label="上下文">
      <div className="context-composition-card-inner">
        {onDismiss ? (
          <button
            className="icon-button context-composition-dismiss"
            type="button"
            aria-label="移除上下文卡片"
            onClick={() => onDismiss(entry.id)}
          >
            <X className="icon" />
          </button>
        ) : null}

        {loading ? <ContextCompositionState text="正在读取上下文记录" /> : null}
        {!loading && error ? <ContextCompositionState tone="error" text={error} /> : null}
        {!loading && !error && result && !result.available ? (
          <ContextCompositionState text={unavailableMessage(result.reason)} />
        ) : null}

        {!loading && !error && result?.available ? (
          <>
            <div className="context-composition-gauge">
              {/* 占用/上限的语义由数字和量表承载，版面上不再用文字复述；
                * 完整说明只保留在悬停提示里。 */}
              <div className="context-composition-headline" title="历史占用 / 上下文上限">
                <strong>
                  {formatTokens(retainedTokens)}
                  {contextWindow > 0 ? (
                    <>
                      <span className="context-composition-headline-divider" aria-hidden="true">
                        /
                      </span>
                      {formatTokens(contextWindow)}
                    </>
                  ) : null}
                </strong>
                {headlinePercent ? (
                  <span className="context-composition-headline-percent">{headlinePercent}</span>
                ) : null}
              </div>

              <div className="context-composition-bar" aria-label="上下文占用组成条">
                {contributing.map((category) => (
                  <span
                    className={`context-composition-segment tone-${category.tone ?? "default"}`}
                    key={category.id}
                    style={{ width: `${segmentWidth(category.tokens ?? 0, barTokens)}%` }}
                    title={`${category.label}: ${formatTokens(category.tokens ?? 0)}`}
                  />
                ))}
              </div>

              {cacheReadTokens > 0 || runtime ? (
                <div className="context-composition-summary-meta">
                  {cacheReadTokens > 0 ? (
                    <span className="context-composition-meta-item">
                      缓存命中 <strong>{formatTokens(cacheReadTokens)}</strong>
                    </span>
                  ) : null}
                  {runtime ? <span className="context-composition-runtime">{runtime}</span> : null}
                </div>
              ) : null}
            </div>

            <div className="context-category-list">
              {categories.map((category) => (
                <ContextCategoryRow category={category} promptTokens={promptTokens} key={category.id} />
              ))}
            </div>
          </>
        ) : null}
      </div>
    </article>
  );
}

function ContextCompositionState({ text, tone = "neutral" }: { text: string; tone?: "neutral" | "error" }): JSX.Element {
  return (
    <div className={`context-composition-state ${tone}`}>
      <Info className="icon" />
      <span>{text}</span>
    </div>
  );
}

function ContextCategoryRow({ category, promptTokens }: { category: ContextCompositionCategory; promptTokens: number }): JSX.Element {
  return (
    <div
      className={`context-category-row tone-${category.tone ?? "default"}${
        category.contributes === false ? " is-excluded" : ""
      }`}
    >
      <span className="context-category-swatch" aria-hidden="true" />
      <span className="context-category-label">{category.label}</span>
      {category.request_only ? <span className="context-category-tag">本次</span> : null}
      <strong className="context-category-tokens">{formatTokens(category.tokens ?? 0)}</strong>
      <span className="context-category-percent">
        {category.contributes === false ? "未计入" : formatPercent(category.tokens ?? 0, promptTokens)}
      </span>
    </div>
  );
}

function unavailableMessage(reason?: string): string {
  switch (reason) {
    case "no_request_yet":
      return "还没有模型请求记录。发送一轮后这里会显示真实请求组成。";
    case "no_trace_path":
      return "当前对话还没有可读取的上下文记录。";
    default:
      return "没有找到上下文记录。";
  }
}

function segmentWidth(tokens: number, total: number): number {
  if (tokens <= 0 || total <= 0) {
    return 0;
  }
  return Math.min(100, (tokens / total) * 100);
}

function compositionBarTokens(promptTokens: number, retainedTokens: number, contextWindow: number): number {
  if (promptTokens <= 0) {
    return 0;
  }
  if (retainedTokens <= 0 || contextWindow <= 0) {
    return promptTokens;
  }
  return (promptTokens * contextWindow) / retainedTokens;
}

function formatPercent(value: number, total: number): string {
  if (value <= 0 || total <= 0) {
    return "0%";
  }
  return `${Math.round((value / total) * 100)}%`;
}

function formatTokens(value: number): string {
  return formatCompactNumber(value);
}

function formatCompactNumber(value: number): string {
  const safe = Math.max(0, Math.round(value));
  if (safe >= 1_000_000) {
    return `${(safe / 1_000_000).toFixed(safe >= 10_000_000 ? 0 : 1)}M`;
  }
  if (safe >= 1_000) {
    return `${(safe / 1_000).toFixed(safe >= 100_000 ? 0 : 1)}k`;
  }
  return safe.toLocaleString();
}
