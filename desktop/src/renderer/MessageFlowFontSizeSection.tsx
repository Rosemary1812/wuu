import {
  type ChangeEvent,
  type PointerEvent,
  useEffect,
  useState,
} from "react";
import {
  MESSAGE_FLOW_FONT_SIZE_RANGE,
  type MessageFlowFontSize,
} from "../shared/protocol";
import { StreamingMarkdown } from "./StreamingMarkdown";

// Sample reply the Settings preview shows underneath the slider. It is
// rendered through the exact pipeline the conversation pane uses
// (.agent-block > .agent-text > StreamingMarkdown), so the slider
// previews real message-flow typography instead of a look-alike
// placeholder. Mixed CJK/Latin prose, inline code, and a short list
// cover the shapes a typical agent reply takes.
const PREVIEW_SAMPLE_MARKDOWN = [
  "先看一下 README 的目录约定，再读一个相邻页面的 CSS——把改动控制在同一套既有规范里。",
  "",
  "- 改动只落在 `desktop/src/renderer`，不动 Go 核心",
  "- 顺手跑一下单元测试，免得新代码悄悄破坏既有流程",
].join("\n");

const { min, max, step, default: defaultSize } = MESSAGE_FLOW_FONT_SIZE_RANGE;

function clampSize(value: unknown): MessageFlowFontSize {
  if (typeof value !== "number" || !Number.isFinite(value)) {
    return defaultSize;
  }
  return Math.min(max, Math.max(min, value));
}

function formatSize(size: number): string {
  return `${size.toFixed(1)}px`;
}

/**
 * Stamp --conversation-message-font-size on <html> from the chosen px
 * value. The inline style on the document element wins over the
 * `:root` declaration in conversation-shell.css so the change cascades
 * through every message-flow surface (turns.css, chat.css,
 * participants.css, and the settings-page preview). Mirrors the
 * applyThemePreference shape — caller-friendly side-effect helper.
 */
export function applyMessageFlowFontSize(size: MessageFlowFontSize): void {
  const clamped = clampSize(size);
  try {
    document.documentElement.style.setProperty(
      "--conversation-message-font-size",
      `${clamped}px`,
    );
  } catch {
    // Same fall-back story as Theme.ts — losing the inline stamp only
    // costs a one-frame flash at the default size.
  }
}

/**
 * Settings row body for the user-facing message-stream reading size:
 * a slider, the current px value, and a live preview block below that
 * reads the same CSS variables the conversation pane uses. Reads and
 * persists through `window.wuu` directly and applies the choice on
 * every drag tick.
 */
export function MessageFlowFontSizeControl(): JSX.Element {
  const [size, setSize] = useState<MessageFlowFontSize>(() =>
    clampSize(window.wuu?.initialMessageFlowFontSize),
  );

  useEffect(() => {
    let cancelled = false;
    void window.wuu
      ?.getMessageFlowFontSize?.()
      .then((stored) => {
        if (!cancelled) {
          setSize(clampSize(stored));
        }
      })
      .catch(() => {
        // Keep the preload-provided initial value.
      });
    return () => {
      cancelled = true;
    };
  }, []);

  function liveUpdate(value: number): void {
    const clamped = clampSize(value);
    setSize(clamped);
    // Write the CSS variable on every onChange tick so the
    // .message-flow-preview below reacts immediately while the user
    // drags. The actual persistence happens on release
    // (commitOnRelease) so we don't spam the JSON file with each
    // pixel of drag motion.
    applyMessageFlowFontSize(clamped);
  }

  function persist(value: number): void {
    void window.wuu?.setMessageFlowFontSize?.(clampSize(value)).catch(() => {
      // Persistence failure leaves the applied size for this window;
      // the next launch falls back to the stored value.
    });
  }

  return (
    <div className="message-flow-font-size-control">
      <div className="message-flow-font-size-slider-row">
        <input
          type="range"
          min={min}
          max={max}
          step={step}
          value={size}
          onChange={(event: ChangeEvent<HTMLInputElement>) =>
            liveUpdate(Number.parseFloat(event.currentTarget.value))
          }
          onPointerUp={(event: PointerEvent<HTMLInputElement>) =>
            persist(Number.parseFloat(event.currentTarget.value))
          }
          onBlur={(event) =>
            persist(Number.parseFloat(event.currentTarget.value))
          }
          aria-label="消息流字号"
          data-testid="settings-message-flow-font-size-slider"
        />
        <span
          className="message-flow-font-size-value"
          aria-live="polite"
          data-testid="settings-message-flow-font-size-value"
        >
          {formatSize(size)}
        </span>
      </div>
      <div
        className="message-flow-preview"
        aria-label="消息流字号预览"
        data-testid="settings-message-flow-font-size-preview"
      >
        <article className="agent-block">
          <div className="agent-text">
            <StreamingMarkdown
              streamKey="settings-message-flow-font-size-preview"
              initialText={PREVIEW_SAMPLE_MARKDOWN}
              isLive={false}
              phase="final_answer"
            />
          </div>
        </article>
      </div>
    </div>
  );
}
