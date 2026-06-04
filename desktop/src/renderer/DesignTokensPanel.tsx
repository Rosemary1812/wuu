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
    cssVar: "--conversation-readable-width",
    label: "消息流宽度",
    min: 600,
    max: 1400,
    step: 20,
    unit: "px",
    defaultValue: 860,
  },
  {
    key: "composer-width",
    cssVar: "--conversation-dialog-width",
    label: "输入框宽度",
    min: 600,
    max: 1200,
    step: 20,
    unit: "px",
    defaultValue: 920,
  },
  {
    key: "msg-font-size",
    cssVar: "--conversation-message-font-size",
    label: "正文字号",
    min: 13,
    max: 20,
    step: 0.5,
    unit: "px",
    defaultValue: 16,
  },
  {
    key: "prose-line-height",
    cssVar: "--conversation-prose-line-height",
    label: "正文行高",
    min: 1.3,
    max: 2.2,
    step: 0.05,
    unit: "",
    defaultValue: 1.85,
  },
  {
    key: "prose-block-gap",
    cssVar: "--conversation-prose-block-gap",
    label: "段落间距",
    min: 4,
    max: 40,
    step: 1,
    unit: "px",
    defaultValue: 30,
  },
  {
    key: "meta-line-height",
    cssVar: "--conversation-meta-line-height",
    label: "Meta 行高",
    min: 1.2,
    max: 1.8,
    step: 0.05,
    unit: "",
    defaultValue: 1.4,
  },
  {
    key: "control-line-height",
    cssVar: "--conversation-control-line-height",
    label: "控件行高",
    min: 1.2,
    max: 1.7,
    step: 0.05,
    unit: "",
    defaultValue: 1.5,
  },
  {
    key: "shell-gap",
    cssVar: "--conversation-shell-gap",
    label: "Turn 内间距",
    min: 4,
    max: 32,
    step: 1,
    unit: "px",
    defaultValue: 14,
  },
  {
    key: "turn-gap",
    cssVar: "--conversation-turn-gap",
    label: "Turn 间距",
    min: 8,
    max: 40,
    step: 1,
    unit: "px",
    defaultValue: 30,
  },
  {
    key: "flow-padding",
    cssVar: "--conversation-flow-padding-inline",
    label: "流两侧留白",
    min: 16,
    max: 80,
    step: 4,
    unit: "px",
    defaultValue: 56,
  },
];

const STORAGE_KEY = "wuu:design-tokens";

type Overrides = Record<string, number>;

function loadOverrides(): Overrides {
  if (typeof window === "undefined" || !window.localStorage) {
    return {};
  }
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return {};
    const parsed: unknown = JSON.parse(raw);
    if (parsed && typeof parsed === "object") {
      return parsed as Overrides;
    }
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
        <Sliders size={16} />
      </button>
      {open ? (
        <aside
          className="design-tokens-panel"
          role="dialog"
          aria-label="设计调音台"
        >
          <div className="design-tokens-header">
            <div className="design-tokens-title">
              <Sliders size={14} />
              <h2>设计调音台</h2>
            </div>
            <button
              className="design-tokens-close"
              type="button"
              onClick={() => setOpen(false)}
              aria-label="关闭"
            >
              <X size={14} />
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
              <RotateCcw size={13} />
              恢复默认
            </button>
            <span className="design-tokens-hint">值保存到 localStorage</span>
          </div>
        </aside>
      ) : null}
    </>
  );
}
