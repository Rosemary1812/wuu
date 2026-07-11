import { useEffect, useState } from "react";
import {
  MESSAGE_FLOW_FONT_SIZE_PX,
  type MessageFlowFontSize,
} from "../shared/protocol";

// Visual labels — kept here so the protocol module stays presentation-free.
// Each step maps to a pixel value via MESSAGE_FLOW_FONT_SIZE_PX, applied
// to --conversation-message-font-size on <html>.
const SIZE_LABELS: Readonly<Record<MessageFlowFontSize, string>> = {
  small: "小",
  medium: "默认",
  large: "大",
};

const ORDERED_SIZES: readonly MessageFlowFontSize[] = [
  "small",
  "medium",
  "large",
];

const DEFAULT_SIZE: MessageFlowFontSize = "medium";

function isMessageFlowFontSize(value: unknown): value is MessageFlowFontSize {
  return (
    value === "small" || value === "medium" || value === "large"
  );
}

/**
 * Stamp --conversation-message-font-size on <html> from the chosen step.
 * The inline style on the document element wins over the `:root`
 * declaration in conversation-shell.css, so the change cascades through
 * every message-flow surface (turns.css, chat.css, participants.css)
 * without touching per-component styles. Mirrors applyThemePreference.
 */
export function applyMessageFlowFontSize(size: MessageFlowFontSize): void {
  try {
    document.documentElement.style.setProperty(
      "--conversation-message-font-size",
      `${MESSAGE_FLOW_FONT_SIZE_PX[size]}px`,
    );
  } catch {
    // Same fall-back story as Theme.ts — losing the inline stamp only
    // costs a one-frame flash at the default size.
  }
}

/**
 * Settings row body for the user-facing message-stream reading size.
 * Self-contained like ThemePreferenceControl — reads and persists
 * through `window.wuu` directly and applies the choice to
 * --conversation-message-font-size immediately so the user sees the
 * switch without a save step.
 */
export function MessageFlowFontSizeControl(): JSX.Element {
  const [size, setSize] = useState<MessageFlowFontSize>(() => {
    const initial = window.wuu?.initialMessageFlowFontSize;
    return isMessageFlowFontSize(initial) ? initial : DEFAULT_SIZE;
  });

  useEffect(() => {
    let cancelled = false;
    void window.wuu
      ?.getMessageFlowFontSize?.()
      .then((stored) => {
        if (!cancelled && isMessageFlowFontSize(stored)) {
          setSize(stored);
        }
      })
      .catch(() => {
        // Keep the preload-provided initial value.
      });
    return () => {
      cancelled = true;
    };
  }, []);

  function choose(next: MessageFlowFontSize): void {
    if (next === size) {
      return;
    }
    setSize(next);
    applyMessageFlowFontSize(next);
    void window.wuu?.setMessageFlowFontSize?.(next).catch(() => {
      // Persistence failure leaves the applied size for this window;
      // the next launch falls back to the stored value.
    });
  }

  return (
    <div
      className="theme-segmented"
      role="group"
      aria-label="消息流字号"
    >
      {ORDERED_SIZES.map((value) => (
        <button
          key={value}
          type="button"
          aria-pressed={size === value}
          data-testid={`settings-message-flow-font-size-${value}`}
          onClick={() => choose(value)}
        >
          {SIZE_LABELS[value]}
        </button>
      ))}
    </div>
  );
}
