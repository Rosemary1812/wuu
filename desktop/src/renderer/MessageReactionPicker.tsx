import { useEffect, useRef } from "react";
import { createPortal } from "react-dom";
import { REACTION_EMOJI, REACTION_KEYS, reactionGlyph } from "./MessageMarks";

// Reaction labels for the picker's a11y names. Keys mirror the backend
// vocabulary; the glyph is the swappable skin (MessageMarks.REACTION_EMOJI).
const REACTION_LABEL: Record<string, string> = {
  eyes: "看到了",
  nice: "赞同",
  shrug: "无所谓",
  sideeye: "存疑",
  smug: "得意",
  whoa: "惊讶",
};

/**
 * MessageReactionPicker is the human "贴 emoji" affordance opened from a chat
 * message's right-click menu. It floats a compact row of the shared reaction
 * emojis at fixed (x, y) coords (raw clientX/clientY from the contextmenu
 * event) and calls onPick with the chosen reaction key. It dismisses on
 * selection, Escape, or an outside click — the same lightweight pattern as
 * ThreadContextMenu, kept separate so the picker can lay its emojis out in a
 * row instead of a vertical menu.
 */
export function MessageReactionPicker({
  x,
  y,
  onPick,
  onClose,
}: {
  x: number;
  y: number;
  onPick: (reaction: string) => void;
  onClose: () => void;
}): JSX.Element {
  const pickerRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    function handleMouseDown(event: MouseEvent): void {
      if (pickerRef.current && !pickerRef.current.contains(event.target as Node)) {
        onClose();
      }
    }
    function handleKeyDown(event: KeyboardEvent): void {
      if (event.key === "Escape") {
        onClose();
      }
    }
    document.addEventListener("mousedown", handleMouseDown);
    document.addEventListener("keydown", handleKeyDown);
    return () => {
      document.removeEventListener("mousedown", handleMouseDown);
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, [onClose]);

  return createPortal(
    <div
      ref={pickerRef}
      role="menu"
      className="message-reaction-picker"
      style={{ left: x, top: y }}
      data-testid="message-reaction-picker"
    >
      {REACTION_KEYS.map((key) => (
        <button
          key={key}
          role="menuitem"
          type="button"
          className="message-reaction-picker-item"
          aria-label={REACTION_LABEL[key] ?? key}
          title={REACTION_LABEL[key] ?? key}
          data-reaction={key}
          onClick={() => {
            onPick(key);
            onClose();
          }}
        >
          <span aria-hidden="true">{REACTION_EMOJI[key] ?? reactionGlyph(key)}</span>
        </button>
      ))}
    </div>,
    document.body,
  );
}
