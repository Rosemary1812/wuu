import { Gauge, Info, X } from "lucide-react";
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
  const dynamicCategory = categories.find((category) => category.id === "request_only");
  const cacheReadTokens = result?.cache_read_tokens ?? 0;
  const freeTokens = contextWindow > 0 ? Math.max(0, contextWindow - promptTokens) : 0;
  const barTokens = contextWindow > 0 ? contextWindow : promptTokens;

  return (
    <article className="context-composition-card" aria-label="上下文组成">
      <div className="context-composition-card-inner">
        <div className="context-composition-header">
          <div>
            <span className="context-composition-eyebrow">
              <Gauge aria-hidden="true" />
              /context
            </span>
            <h2>上下文组成</h2>
            <p>{title ? title : "当前对话"} · 最近一次真实请求 · 未进入上下文</p>
          </div>
          {onDismiss ? (
            <button
              className="icon-button context-composition-dismiss"
              type="button"
              aria-label="移除上下文组成"
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
            <div className="context-composition-stats">
              <ContextStat label="请求上下文" value={formatTokenRatio(promptTokens, contextWindow)} detail={formatPercent(promptTokens, contextWindow)} />
              <ContextStat label="保留历史" value={formatTokens(result.retained_tokens ?? 0)} detail="圆环口径" />
              <ContextStat label="缓存读取" value={formatTokens(cacheReadTokens)} detail={formatPercent(cacheReadTokens, promptTokens)} />
              <ContextStat label="动态上下文" value={formatTokens(dynamicCategory?.tokens ?? 0)} detail={dynamicCategory?.request_only ? "只进本次" : "无"} />
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

            <div className="context-composition-meta">
              <span>{runtimeLabel(result)}</span>
              <span>{result.token_estimate_source === "provider_usage" ? "按 provider usage 分摊估算" : "按字节估算"}</span>
              {result.prompt_cache_key ? <span>cache key {result.prompt_cache_key}</span> : null}
            </div>

            <div className="context-composition-section">
              <h3>组成</h3>
              <div className="context-category-list">
                {categories.map((category) => (
                  <ContextCategoryRow category={category} promptTokens={promptTokens} key={category.id} />
                ))}
              </div>
            </div>

            <div className="context-composition-detail-grid">
              <ContextDetailBlock
                title="请求形状"
                rows={[
                  ["消息", String(result.message_count ?? 0)],
                  ["隐藏消息", String(result.hidden_messages ?? 0)],
                  ["工具", String(result.tool_count ?? 0)],
                  ["stable prefix", String(result.stable_prefix ?? 0)],
                  ["turn prefix", String(result.turn_prefix ?? 0)],
                ]}
              />
              <ContextDetailBlock
                title="动态块"
                rows={recordRows(result.block_kind_bytes, (value) => formatBytes(value))}
                empty="无动态块"
              />
              <ContextDetailBlock
                title="segment"
                rows={[
                  ...recordRows(result.segment_counts?.lifecycle),
                  ...recordRows(result.segment_counts?.cache_policy),
                ]}
                empty="无 request-only segment"
              />
              <ContextDetailBlock
                title="system sections"
                rows={(result.system_sections ?? []).map((section) => [
                  section.key,
                  `${formatTokens(section.tokens ?? 0)} · ${formatBytes(section.bytes)}`,
                ])}
                empty="无分段记录"
              />
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

function ContextStat({ label, value, detail }: { label: string; value: string; detail: string }): JSX.Element {
  return (
    <div className="context-composition-stat">
      <span>{label}</span>
      <strong>{value}</strong>
      <small>{detail}</small>
    </div>
  );
}

function ContextCategoryRow({ category, promptTokens }: { category: ContextCompositionCategory; promptTokens: number }): JSX.Element {
  return (
    <div className={`context-category-row tone-${category.tone ?? "default"}`}>
      <span className="context-category-swatch" aria-hidden="true" />
      <div className="context-category-main">
        <strong>{category.label}</strong>
        <span>{category.description}</span>
      </div>
      <div className="context-category-values">
        <strong>{formatTokens(category.tokens ?? 0)}</strong>
        <span>{category.contributes === false ? "未计入" : formatPercent(category.tokens ?? 0, promptTokens)}</span>
      </div>
      <div className="context-category-badges">
        {category.request_only ? <span>request-only</span> : null}
        {category.cache_scope ? <span>{cacheScopeLabel(category.cache_scope)}</span> : null}
        {category.deferred ? <span>按需</span> : null}
      </div>
    </div>
  );
}

function ContextDetailBlock({
  title,
  rows,
  empty,
}: {
  title: string;
  rows: Array<[string, string]>;
  empty?: string;
}): JSX.Element {
  const visibleRows = rows.filter(([key, value]) => key.trim() !== "" && value.trim() !== "" && value !== "0");
  return (
    <div className="context-detail-block">
      <h3>{title}</h3>
      {visibleRows.length > 0 ? (
        visibleRows.map(([key, value]) => (
          <div className="context-detail-row" key={`${title}-${key}`}>
            <span>{key}</span>
            <strong>{value}</strong>
          </div>
        ))
      ) : (
        <p>{empty ?? "无记录"}</p>
      )}
    </div>
  );
}

function recordRows(record: Record<string, number> | undefined, formatValue: (value: number) => string = String): Array<[string, string]> {
  if (!record) {
    return [];
  }
  return Object.entries(record)
    .sort((left, right) => right[1] - left[1])
    .map(([key, value]) => [key, formatValue(value)]);
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

function runtimeLabel(result: ThreadContextCompositionResult): string {
  return [result.provider, result.model, result.turn_id ? `turn ${result.turn_id}` : ""].filter(Boolean).join(" · ") || "未知模型";
}

function cacheScopeLabel(scope: string): string {
  switch (scope) {
    case "stable":
      return "稳定前缀";
    case "turn":
      return "本轮前缀";
    case "volatile":
      return "尾部";
    case "surface":
      return "请求面";
    case "deferred":
      return "不常驻";
    default:
      return scope;
  }
}

function segmentWidth(tokens: number, total: number): number {
  if (tokens <= 0 || total <= 0) {
    return 0;
  }
  return Math.max(1, Math.min(100, (tokens / total) * 100));
}

function formatTokenRatio(used: number, total: number): string {
  if (total > 0) {
    return `${formatTokens(used)} / ${formatTokens(total)}`;
  }
  return formatTokens(used);
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

function formatBytes(value: number): string {
  if (value >= 1024 * 1024) {
    return `${(value / 1024 / 1024).toFixed(1)} MB`;
  }
  if (value >= 1024) {
    return `${(value / 1024).toFixed(1)} KB`;
  }
  return `${Math.max(0, value)} B`;
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
