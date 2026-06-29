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
  const { result, loading, error, title } = entry;
  const categories = result?.categories ?? [];
  const contributing = categories.filter((category) => category.contributes !== false);
  const promptTokens = result?.prompt_tokens ?? contributing.reduce((sum, category) => sum + (category.tokens ?? 0), 0);
  const contextWindow = result?.context_window_tokens ?? 0;
  const inputLimit = result?.input_limit_tokens ?? 0;
  const compactThreshold = result?.compact_threshold_tokens ?? 0;
  const requestWindow = firstPositive(inputLimit, contextWindow);
  const cacheReadTokens = result?.cache_read_tokens ?? 0;
  const retainedTokens = result?.retained_tokens ?? 0;
  const freeTokens = requestWindow > 0 ? Math.max(0, requestWindow - promptTokens) : 0;
  const barTokens = requestWindow > 0 ? requestWindow : promptTokens;
  const headlineTokens = requestWindow > 0
    ? `${formatTokens(promptTokens)} / ${formatTokens(requestWindow)}`
    : formatTokens(promptTokens);
  const headlinePercent = requestWindow > 0 ? formatPercent(promptTokens, requestWindow) : null;
  const runtime = [result?.provider, result?.model].filter(Boolean).join(" · ");
  const summaryMeta = [
    contextWindow > 0 && contextWindow !== requestWindow ? `模型窗口 ${formatTokens(contextWindow)}` : "",
    compactThreshold > 0 ? `压缩线 ${formatTokens(compactThreshold)}` : "",
    freeTokens > 0 ? `请求余量 ${formatTokens(freeTokens)}` : "",
    cacheReadTokens > 0 ? `缓存命中 ${formatTokens(cacheReadTokens)}` : "",
    retainedTokens > 0 ? `保留历史 ${formatTokens(retainedTokens)}` : "",
  ].filter(Boolean);

  return (
    <article className="context-composition-card" aria-label="上下文">
      <div className="context-composition-card-inner">
        <div className="context-composition-header">
          <div className="context-composition-title-block">
            <h2>上下文</h2>
            {title ? <p>{title}</p> : null}
          </div>
          {runtime ? <span className="context-composition-runtime">{runtime}</span> : null}
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
        </div>

        {loading ? <ContextCompositionState text="正在读取上下文记录" /> : null}
        {!loading && error ? <ContextCompositionState tone="error" text={error} /> : null}
        {!loading && !error && result && !result.available ? (
          <ContextCompositionState text={unavailableMessage(result.reason)} />
        ) : null}

        {!loading && !error && result?.available ? (
          <>
            <div className="context-composition-summary">
              <div className="context-composition-headline">
                <strong>{headlineTokens}</strong>
                <span className="context-composition-headline-unit">当前请求 / 输入上限</span>
                {headlinePercent ? (
                  <span className="context-composition-headline-percent">{headlinePercent}</span>
                ) : null}
              </div>
              {summaryMeta.length > 0 ? (
                <div className="context-composition-summary-meta">
                  {summaryMeta.map((item) => (
                    <span key={item}>{item}</span>
                  ))}
                </div>
              ) : null}
            </div>

            <div className="context-composition-bar" aria-label="上下文组成条">
              {contributing.map((category) => (
                <span
                  className={`context-composition-segment tone-${category.tone ?? "default"}`}
                  key={category.id}
                  style={{ width: `${segmentWidth(category.tokens ?? 0, barTokens)}%` }}
                  title={`${category.label}: ${formatTokens(category.tokens ?? 0)}`}
                />
              ))}
              {freeTokens > 0 ? (
                <span
                  className="context-composition-segment free"
                  style={{ width: `${segmentWidth(freeTokens, barTokens)}%` }}
                  title={`剩余: ${formatTokens(freeTokens)}`}
                />
              ) : null}
            </div>

            <div className="context-composition-section">
              <h3>组成</h3>
              <div className="context-category-list">
                {categories.map((category) => (
                  <ContextCategoryRow category={category} promptTokens={promptTokens} key={category.id} />
                ))}
              </div>
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
    <div className={`context-category-row tone-${category.tone ?? "default"}`}>
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
  return Math.max(1, Math.min(100, (tokens / total) * 100));
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

function firstPositive(...values: number[]): number {
  return values.find((value) => value > 0) ?? 0;
}
