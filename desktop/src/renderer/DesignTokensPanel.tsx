/**
 * Design tokens panel — a developer "mixer" for live-tweaking the
 * conversation flow's CSS custom properties without editing the
 * stylesheet. Each token is a labeled range slider; the current
 * value is shown next to the label, and overrides persist to
 * localStorage so they survive reloads.
 *
 * Why this exists: the message flow has a lot of coupled spacing
 * tokens (line-height, block gap, font size, flow width, ...) and
 * the right value depends on the user's screen + their reading
 * taste. Hand-tuning each commit is slow and noisy in git. This
 * panel lets the user iterate at the speed of a slider drag and
 * commit only the values that end up feeling right.
 *
 * The panel itself is intentionally minimal: a fixed-position
 * toggle (bottom-right, like Chrome DevTools) and a 320px side
 * drawer. No animations, no fancy chrome — it's a debug tool.
 */
import { useEffect, useState } from "react";
import { RotateCcw, Sliders, X } from "lucide-react";
import {
  clampConversationDesignTokenValue,
  CONVERSATION_DESIGN_TOKENS,
  CONVERSATION_DESIGN_TOKEN_STORAGE_KEY,
  conversationDesignTokenByKey,
  conversationDesignTokenStyleValue,
  LEGACY_CONVERSATION_DESIGN_TOKEN_CSS_VARS,
  LEGACY_CONVERSATION_DESIGN_TOKEN_STORAGE_KEYS,
  type ConversationDesignTokenKey,
} from "./ConversationDesignTokens";
import { useI18n } from "./i18n";

type Overrides = Partial<Record<ConversationDesignTokenKey, number>>;

function normalizeOverrides(parsed: unknown): Overrides {
  if (!parsed || typeof parsed !== "object") {
    return {};
  }
  const source = parsed as Record<string, unknown>;
  const normalized: Overrides = {};
  for (const token of CONVERSATION_DESIGN_TOKENS) {
    const value = source[token.key];
    if (typeof value === "number" && Number.isFinite(value)) {
      normalized[token.key] = clampConversationDesignTokenValue(token, value);
    }
  }
  return normalized;
}

function loadOverrides(): Overrides {
  if (typeof window === "undefined" || !window.localStorage) {
    return {};
  }
  try {
    const raw = window.localStorage.getItem(
      CONVERSATION_DESIGN_TOKEN_STORAGE_KEY,
    );
    if (!raw) return {};
    const parsed: unknown = JSON.parse(raw);
    return normalizeOverrides(parsed);
  } catch {
    /* ignore — corrupted localStorage shouldn't kill the panel */
  }
  return {};
}

function clearLegacyStorage(): void {
  if (typeof window === "undefined" || !window.localStorage) return;
  for (const key of LEGACY_CONVERSATION_DESIGN_TOKEN_STORAGE_KEYS) {
    window.localStorage.removeItem(key);
  }
}

function saveOverrides(overrides: Overrides): void {
  if (typeof window === "undefined" || !window.localStorage) return;
  try {
    window.localStorage.setItem(
      CONVERSATION_DESIGN_TOKEN_STORAGE_KEY,
      JSON.stringify(overrides),
    );
  } catch {
    /* quota or privacy mode — silently drop */
  }
}

function applyToDOM(overrides: Overrides): void {
  const pane = document.querySelector<HTMLElement>(".conversation-pane");
  if (!pane) return;
  for (const [key, value] of Object.entries(overrides)) {
    const token = conversationDesignTokenByKey(key);
    if (token) {
      pane.style.setProperty(
        token.cssVar,
        conversationDesignTokenStyleValue(token, value),
      );
    }
  }
}

function clearFromDOM(): void {
  const pane = document.querySelector<HTMLElement>(".conversation-pane");
  if (!pane) return;
  for (const token of CONVERSATION_DESIGN_TOKENS) {
    pane.style.removeProperty(token.cssVar);
  }
  for (const cssVar of LEGACY_CONVERSATION_DESIGN_TOKEN_CSS_VARS) {
    pane.style.removeProperty(cssVar);
  }
}

export function DesignTokensPanel(): JSX.Element {
  const { t, formatNumber } = useI18n();
  const [open, setOpen] = useState(false);
  const [overrides, setOverrides] = useState<Overrides>({});

  // Hydrate from localStorage on mount and re-apply so the values
  // take effect even if the user navigates between threads (which
  // unmounts the App's children but keeps the renderer alive).
  useEffect(() => {
    clearLegacyStorage();
    clearFromDOM();
    const loaded = loadOverrides();
    if (Object.keys(loaded).length > 0) {
      setOverrides(loaded);
      applyToDOM(loaded);
    }
  }, []);

  const handleChange = (key: string, value: number): void => {
    const token = conversationDesignTokenByKey(key);
    if (!token) return;
    const next: Overrides = {
      ...overrides,
      [token.key]: clampConversationDesignTokenValue(token, value),
    };
    setOverrides(next);
    saveOverrides(next);
    applyToDOM(next);
  };

  const handleReset = (): void => {
    setOverrides({});
    if (typeof window !== "undefined" && window.localStorage) {
      window.localStorage.removeItem(CONVERSATION_DESIGN_TOKEN_STORAGE_KEY);
    }
    clearFromDOM();
  };

  return (
    <>
      <button
        className="design-tokens-toggle"
        type="button"
        onClick={() => setOpen((o) => !o)}
        title={t("designTokens.devTitle")}
        aria-label={t("designTokens.open")}
        aria-expanded={open}
      >
        <Sliders className="icon" />
      </button>
      {open ? (
        <aside
          className="design-tokens-panel"
          role="dialog"
          aria-label={t("designTokens.title")}
        >
          <div className="design-tokens-header">
            <div className="design-tokens-title">
              <Sliders className="icon-sm" />
              <h2>{t("designTokens.title")}</h2>
            </div>
            <button
              className="design-tokens-close"
              type="button"
              onClick={() => setOpen(false)}
              aria-label={t("common.close")}
            >
              <X className="icon-sm" />
            </button>
          </div>
          <div className="design-tokens-body">
            {CONVERSATION_DESIGN_TOKENS.map((token) => {
              const value = overrides[token.key] ?? token.defaultValue;
              return (
                <div className="design-token" key={token.key}>
                  <div className="design-token-label">
                    <span className="design-token-name">{t(token.labelKey)}</span>
                    <span className="design-token-value">
                      {formatNumber(value)}
                      {token.unit}
                    </span>
                  </div>
                  <input
                    type="range"
                    min={token.min}
                    max={token.max}
                    step={token.step}
                    value={value}
                    onChange={(e) =>
                      handleChange(token.key, Number(e.target.value))
                    }
                    aria-label={t(token.labelKey)}
                  />
                </div>
              );
            })}
          </div>
          <div className="design-tokens-footer">
            <button
              className="design-tokens-reset"
              type="button"
              onClick={handleReset}
            >
              <RotateCcw className="icon-xs" />
              {t("common.restoreDefaults")}
            </button>
            <span className="design-tokens-hint">{t("designTokens.storageHint")}</span>
          </div>
        </aside>
      ) : null}
    </>
  );
}
