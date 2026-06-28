// Client-side mirror of internal/providers/contextwindow.go's catalog.
//
// The Go side stays the source of truth for live usage snapshots: it
// resolves the active model's window at turn-time and emits it on every
// "turn/usage" notification. The renderer only needs a *catalog* window
// to render an empty ring from the moment a model is picked, before any
// turn has run. The renderer has no provider-request context and no
// knowledge of the catwalk curated index, so the right answer is to
// mirror the small substring registry + exact overrides the Go side uses
// for the same purpose.
//
// If a model is genuinely unrecognized here the function returns
// undefined rather than falling back to a 64k default. The 64k fallback
// in Go exists for runtime proactive auto-compact (so unknown BYOK
// models remain reactive-only), not for UI display — the meter should
// hide when it has nothing honest to show.

type ContextWindowEntry = {
  // Lowercase substring match; first hit wins.
  pattern: string;
  size: number;
};

const exactContextWindowOverrides: ContextWindowEntry[] = [
  // MiniMax official docs list MiniMax-M3 at 1M context and M2-series
  // text models at 204,800. Listed first so older catalog snapshots
  // that report MiniMax-M3 as 200k do not win the race.
  { pattern: "minimax-m3", size: 1_000_000 },
  { pattern: "minimax-m2.7-highspeed", size: 204_800 },
  { pattern: "minimax-m2.7", size: 204_800 },
  { pattern: "minimax-m2.5-highspeed", size: 204_800 },
  { pattern: "minimax-m2.5", size: 204_800 },
  { pattern: "minimax-m2.1-highspeed", size: 204_800 },
  { pattern: "minimax-m2.1", size: 204_800 },
  { pattern: "minimax-m2", size: 204_800 },
];

// Order matters: more specific patterns must come before generic ones.
const contextWindowRegistry: ContextWindowEntry[] = [
  // Anthropic Claude 4.x
  { pattern: "claude-opus-4", size: 200_000 },
  { pattern: "claude-sonnet-4", size: 200_000 },
  { pattern: "claude-haiku-4", size: 200_000 },
  // Anthropic Claude 3.x
  { pattern: "claude-3-7-sonnet", size: 200_000 },
  { pattern: "claude-3-5-sonnet", size: 200_000 },
  { pattern: "claude-3-5-haiku", size: 200_000 },
  { pattern: "claude-3-opus", size: 200_000 },
  { pattern: "claude-3-sonnet", size: 200_000 },
  { pattern: "claude-3-haiku", size: 200_000 },
  // Generic Claude catch-alls
  { pattern: "claude-opus", size: 200_000 },
  { pattern: "claude-sonnet", size: 200_000 },
  { pattern: "claude-haiku", size: 200_000 },
  { pattern: "claude", size: 200_000 },
  // OpenAI GPT-5
  { pattern: "gpt-5", size: 400_000 },
  // OpenAI GPT-4
  { pattern: "gpt-4o-mini", size: 128_000 },
  { pattern: "gpt-4o", size: 128_000 },
  { pattern: "gpt-4-turbo", size: 128_000 },
  { pattern: "gpt-4.1", size: 1_000_000 },
  { pattern: "gpt-4-32k", size: 32_000 },
  { pattern: "gpt-4", size: 8_192 },
  // OpenAI o-series
  { pattern: "o3-mini", size: 200_000 },
  { pattern: "o3", size: 200_000 },
  { pattern: "o1-mini", size: 128_000 },
  { pattern: "o1", size: 200_000 },
  // OpenAI GPT-3.5
  { pattern: "gpt-3.5-turbo-16k", size: 16_000 },
  { pattern: "gpt-3.5-turbo", size: 16_000 },
  // DeepSeek
  { pattern: "deepseek-v3", size: 64_000 },
  { pattern: "deepseek-r1", size: 64_000 },
  { pattern: "deepseek-coder", size: 64_000 },
  { pattern: "deepseek-chat", size: 64_000 },
  { pattern: "deepseek", size: 64_000 },
  // Google Gemini
  { pattern: "gemini-2.5-pro", size: 2_000_000 },
  { pattern: "gemini-2.0", size: 2_000_000 },
  { pattern: "gemini-2", size: 2_000_000 },
  { pattern: "gemini-1.5-pro", size: 2_000_000 },
  { pattern: "gemini-1.5-flash", size: 1_000_000 },
  { pattern: "gemini-1.5", size: 1_000_000 },
  { pattern: "gemini-1", size: 32_000 },
  { pattern: "gemini", size: 32_000 },
  // Mistral
  { pattern: "mistral-large", size: 128_000 },
  { pattern: "mistral-medium", size: 32_000 },
  { pattern: "mistral-small", size: 32_000 },
  { pattern: "mistral", size: 32_000 },
  // Meta Llama
  { pattern: "llama-3.3", size: 128_000 },
  { pattern: "llama-3.2", size: 128_000 },
  { pattern: "llama-3.1-405b", size: 128_000 },
  { pattern: "llama-3.1-70b", size: 128_000 },
  { pattern: "llama-3.1", size: 128_000 },
  { pattern: "llama-3", size: 8_192 },
  { pattern: "llama", size: 8_192 },
  // Qwen
  { pattern: "qwen3", size: 128_000 },
  { pattern: "qwen2.5", size: 128_000 },
  { pattern: "qwen", size: 32_000 },
];

export function clientContextWindowFor(model: string): number | undefined {
  if (!model) {
    return undefined;
  }
  const lower = model.toLowerCase().trim();
  // OpenRouter / gateway style: "anthropic/claude-sonnet-4-5". Strip
  // the prefix for matching but keep the original for the exact pass.
  const stripped = (() => {
    const idx = lower.lastIndexOf("/");
    return idx >= 0 ? lower.slice(idx + 1) : lower;
  })();
  // Exact overrides first so specific model docs win over generic
  // substring matches (e.g. "minimax-m3" must resolve to 1M, not 200k).
  for (const entry of exactContextWindowOverrides) {
    if (lower === entry.pattern || stripped === entry.pattern) {
      return entry.size;
    }
  }
  for (const entry of contextWindowRegistry) {
    if (lower.includes(entry.pattern) || stripped.includes(entry.pattern)) {
      return entry.size;
    }
  }
  return undefined;
}