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

type Token = {
  /** Stable id used as the localStorage key and the slider identity. */
  key: string;
  /** CSS custom property name written to .conversation-pane. */
  cssVar: string;
  /** Human-readable label shown in the panel. */
  label: string;
  min: number;
  max: number;
  step: number;
  /** "" for unitless values (line-height), "px" otherwise. */
  unit: string;
  /** Fallback when no override is stored. Mirrors the value in
   *  styles.css so the slider sits at the right place on first open. */
  defaultValue: number;
};

const TOKENS: Token[] = [
  {
    key: "flow-width",
    cssVar: "--session-outer-width",
    label: "消息流宽度",
    min: 800,
    max: 1280,
    step: 16,
    unit: "px",
    defaultValue: 928,
  },
  {
    key: "message-max-width",
    cssVar: "--conversation-message-max-width",
    label: "消息最大宽度",
    min: 480,
    max: 1080,
    step: 16,
    unit: "px",
    defaultValue: 720,
  },
  {
    // Derived from content-width; the slider here overrides the cascade.
    key: "composer-width",
    cssVar: "--session-composer-width",
    label: "输入框宽度",
    min: 600,
    max: 1200,
    step: 20,
    unit: "px",
    defaultValue: 784,
  },
  {
    key: "composer-radius",
    cssVar: "--session-composer-radius",
    label: "输入框圆角",
    min: 0,
    max: 32,
    step: 1,
    unit: "px",
    defaultValue: 18,
  },
  {
    key: "msg-font-size",
    cssVar: "--conversation-message-font-size",
    label: "正文字号",
    min: 13,
    max: 20,
    step: 0.5,
    unit: "px",
    defaultValue: 14,
  },
  {
    key: "prose-line-height",
    cssVar: "--conversation-prose-line-height",
    label: "正文行高",
    min: 1.55,
    max: 2.1,
    step: 0.02,
    unit: "",
    defaultValue: 1.72,
  },
  {
    key: "prose-block-gap",
    cssVar: "--conversation-prose-block-gap",
    label: "段落间距",
    min: 4,
    max: 48,
    step: 1,
    unit: "px",
    defaultValue: 14,
  },
  {
    key: "meta-line-height",
    cssVar: "--conversation-meta-line-height",
    label: "Meta 行高",
    min: 1.2,
    max: 1.8,
    step: 0.05,
    unit: "",
    defaultValue: 1.8,
  },
  {
    key: "control-line-height",
    cssVar: "--conversation-control-line-height",
    label: "控件行高",
    min: 1.2,
    max: 2.2,
    step: 0.05,
    unit: "",
    defaultValue: 2,
  },
  {
    key: "process-gap",
    cssVar: "--conversation-process-gap",
    label: "Turn 内块间距",
    min: 2,
    max: 24,
    step: 1,
    unit: "px",
    defaultValue: 12,
  },
  {
    key: "message-element-gap",
    cssVar: "--conversation-message-element-gap",
    label: "消息块间距",
    min: 4,
    max: 32,
    step: 1,
    unit: "px",
    defaultValue: 12,
  },
  {
    key: "turn-gap",
    cssVar: "--conversation-turn-gap",
    label: "Turn 间距",
    min: 0,
    max: 48,
    step: 1,
    unit: "px",
    defaultValue: 8,
  },
  {
    key: "flow-padding",
    cssVar: "--session-outer-padding-inline",
    label: "流两侧留白",
    min: 24,
    max: 96,
    step: 4,
    unit: "px",
    defaultValue: 72,
  },
];

const STORAGE_KEY = "wuu:design-tokens:v3";

type Overrides = Record<string, number>;

function normalizeOverrides(parsed: unknown): Overrides {
  if (!parsed || typeof parsed !== "object") {
    return {};
  }
  const source = parsed as Record<string, unknown>;
  const normalized: Overrides = {};
  for (const token of TOKENS) {
    const value = source[token.key];
    if (typeof value === "number" && Number.isFinite(value)) {
      normalized[token.key] = Math.min(token.max, Math.max(token.min, value));
    }
  }
  return normalized;
}

function loadOverrides(): Overrides {
  if (typeof window === "undefined" || !window.localStorage) {
    return {};
  }
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return {};
    const parsed: unknown = JSON.parse(raw);
    return normalizeOverrides(parsed);
  } catch {
    /* ignore — corrupted localStorage shouldn't kill the panel */
  }
  return {};
}

function saveOverrides(overrides: Overrides): void {
  if (typeof window === "undefined" || !window.localStorage) return;
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(overrides));
  } catch {
    /* quota or privacy mode — silently drop */
  }
}

function applyToDOM(overrides: Overrides): void {
  const pane = document.querySelector<HTMLElement>(".conversation-pane");
  if (!pane) return;
  for (const [key, value] of Object.entries(overrides)) {
    const token = TOKENS.find((t) => t.key === key);
    if (token) {
      pane.style.setProperty(token.cssVar, `${value}${token.unit}`);
    }
  }
}

function clearFromDOM(): void {
  const pane = document.querySelector<HTMLElement>(".conversation-pane");
  if (!pane) return;
  for (const token of TOKENS) {
    pane.style.removeProperty(token.cssVar);
  }
}

export function DesignTokensPanel(): JSX.Element {
  const [open, setOpen] = useState(false);
  const [overrides, setOverrides] = useState<Overrides>({});

  // Hydrate from localStorage on mount and re-apply so the values
  // take effect even if the user navigates between threads (which
  // unmounts the App's children but keeps the renderer alive).
  useEffect(() => {
    const loaded = loadOverrides();
    if (Object.keys(loaded).length > 0) {
      setOverrides(loaded);
      applyToDOM(loaded);
    }
  }, []);

  const handleChange = (key: string, value: number): void => {
    const next: Overrides = { ...overrides, [key]: value };
    setOverrides(next);
    saveOverrides(next);
    applyToDOM(next);
  };

  const handleReset = (): void => {
    setOverrides({});
    if (typeof window !== "undefined" && window.localStorage) {
      window.localStorage.removeItem(STORAGE_KEY);
    }
    clearFromDOM();
  };

  return (
    <>
      <button
        className="design-tokens-toggle"
        type="button"
        onClick={() => setOpen((o) => !o)}
        title="设计调音台（开发用）"
        aria-label="打开设计调音台"
        aria-expanded={open}
      >
        <Sliders className="icon" />
      </button>
      {open ? (
        <aside
          className="design-tokens-panel"
          role="dialog"
          aria-label="设计调音台"
        >
          <div className="design-tokens-header">
            <div className="design-tokens-title">
              <Sliders className="icon-sm" />
              <h2>设计调音台</h2>
            </div>
            <button
              className="design-tokens-close"
              type="button"
              onClick={() => setOpen(false)}
              aria-label="关闭"
            >
              <X className="icon-sm" />
            </button>
          </div>
          <div className="design-tokens-body">
            {TOKENS.map((token) => {
              const value = overrides[token.key] ?? token.defaultValue;
              return (
                <div className="design-token" key={token.key}>
                  <div className="design-token-label">
                    <span className="design-token-name">{token.label}</span>
                    <span className="design-token-value">
                      {value}
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
                    aria-label={token.label}
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
              恢复默认
            </button>
            <span className="design-tokens-hint">值保存到 localStorage</span>
          </div>
        </aside>
      ) : null}
    </>
  );
}
