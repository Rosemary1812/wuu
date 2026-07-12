import { type RefObject, useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";

// Right-click context menu for the composer textarea. Mirrors the visual
// language of WorkspaceTreeContextMenu (file-tree row menu): flat surface,
// soft shadow, no native chrome. Positioning lives in viewport coordinates
// so the menu stays anchored to the cursor even when the textarea itself
// is scrolled inside a tall composer shell.
//
// Dismissal rules:
//   - Left click outside the menu → close. Right click is intentionally
//     NOT a dismiss signal: the right-click gesture that opened us is the
//     same gesture the textarea uses to reopen us, and dismissing on it
//     would race with reopen. Same rule as WorkspaceTreeContextMenu.
//   - Escape → close.
//   - Listeners are attached via setTimeout(0) so the burst of pointer
//     events the right-click itself dispatches doesn't immediately
//     dismiss the menu we just opened.

export function ComposerContextMenu({
  textareaRef,
  x,
  y,
  hasSelection,
  disabled = false,
  onClose,
  onValueChange
}: {
  textareaRef: RefObject<HTMLTextAreaElement | null>;
  x: number;
  y: number;
  hasSelection: boolean;
  disabled?: boolean;
  onClose: () => void;
  onValueChange: (next: string) => void;
}): JSX.Element {
  const ref = useRef<HTMLDivElement | null>(null);
  // The menu mounts at the cursor, but until React commits the first
  // paint its own size isn't known — measure on the layout effect that
  // runs just before paint, clamp to viewport so the user never sees
  // it spilling off-screen.
  const [position, setPosition] = useState({ x, y });
  useLayoutEffect(() => {
    const menuElement = ref.current;
    if (!menuElement) {
      return;
    }
    const rect = menuElement.getBoundingClientRect();
    const viewportWidth = window.innerWidth;
    const viewportHeight = window.innerHeight;
    const margin = 8;
    const clampedX = Math.max(
      margin,
      Math.min(viewportWidth - rect.width - margin, x)
    );
    const clampedY = Math.max(
      margin,
      Math.min(viewportHeight - rect.height - margin, y)
    );
    if (clampedX !== x || clampedY !== y) {
      setPosition({ x: clampedX, y: clampedY });
    }
  }, [x, y]);

  useEffect(() => {
    const handlePointerDown = (event: PointerEvent) => {
      if (event.button !== 0) {
        return;
      }
      const menuElement = ref.current;
      if (!menuElement) {
        return;
      }
      if (event.target instanceof Node && menuElement.contains(event.target)) {
        return;
      }
      onClose();
    };
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        onClose();
      }
    };
    let active = true;
    const id = window.setTimeout(() => {
      if (!active) {
        return;
      }
      document.addEventListener("pointerdown", handlePointerDown);
      document.addEventListener("keydown", handleKeyDown);
    }, 0);
    return () => {
      active = false;
      window.clearTimeout(id);
      document.removeEventListener("pointerdown", handlePointerDown);
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, [onClose]);

  // Read the current selection and value at action time, not at render
  // time — the user can open the menu, then change their selection with
  // the keyboard before clicking an item, and the action should reflect
  // the live state of the textarea. Synchronous action handlers also
  // guarantee the menu closes before React re-renders, so we never
  // observe the textarea mid-update.
  const readSelection = (): {
    textarea: HTMLTextAreaElement;
    start: number;
    end: number;
    value: string;
  } | null => {
    const textarea = textareaRef.current;
    if (!textarea) {
      return null;
    }
    return {
      textarea,
      start: textarea.selectionStart,
      end: textarea.selectionEnd,
      value: textarea.value
    };
  };

  const runCut = (): void => {
    if (disabled) {
      return;
    }
    const selection = readSelection();
    if (!selection || selection.start === selection.end) {
      onClose();
      return;
    }
    const { textarea, start, end, value } = selection;
    const selectedText = value.slice(start, end);
    const nextValue = value.slice(0, start) + value.slice(end);
    onValueChange(nextValue);
    // Best-effort clipboard write; failure (e.g. permission denied) should
    // not block the cut. The text has already been removed from the
    // textarea, and re-inserting it on clipboard failure would be more
    // surprising than simply having the cut take effect.
    void navigator.clipboard.writeText(selectedText);
    // Place the caret at the cut boundary after React re-renders the
    // textarea with the new value.
    requestAnimationFrame(() => {
      textarea.focus();
      textarea.setSelectionRange(start, start);
    });
    onClose();
  };

  const runCopy = (): void => {
    if (disabled) {
      return;
    }
    const selection = readSelection();
    if (!selection || selection.start === selection.end) {
      onClose();
      return;
    }
    const { textarea, start, end, value } = selection;
    const selectedText = value.slice(start, end);
    void navigator.clipboard.writeText(selectedText);
    // Restore focus so the user can keep editing from the same selection
    // after the menu closes.
    textarea.focus();
    onClose();
  };

  const runPaste = async (): Promise<void> => {
    if (disabled) {
      return;
    }
    const selection = readSelection();
    if (!selection) {
      onClose();
      return;
    }
    const { textarea, start, end, value } = selection;
    let clipboardText = "";
    try {
      clipboardText = await navigator.clipboard.readText();
    } catch {
      // Permission denied or no clipboard access — fail closed without
      // touching the textarea.
      onClose();
      return;
    }
    if (clipboardText.length === 0) {
      onClose();
      return;
    }
    const nextValue = value.slice(0, start) + clipboardText + value.slice(end);
    onValueChange(nextValue);
    const caret = start + clipboardText.length;
    requestAnimationFrame(() => {
      textarea.focus();
      textarea.setSelectionRange(caret, caret);
    });
    onClose();
  };

  const runSelectAll = (): void => {
    if (disabled) {
      return;
    }
    const textarea = textareaRef.current;
    if (!textarea) {
      onClose();
      return;
    }
    textarea.focus();
    textarea.select();
    onClose();
  };

  const runDelete = (): void => {
    if (disabled) {
      return;
    }
    const selection = readSelection();
    if (!selection || selection.start === selection.end) {
      onClose();
      return;
    }
    const { textarea, start, end, value } = selection;
    const nextValue = value.slice(0, start) + value.slice(end);
    onValueChange(nextValue);
    requestAnimationFrame(() => {
      textarea.focus();
      textarea.setSelectionRange(start, start);
    });
    onClose();
  };

  // Cut/Copy/Delete need an active selection to do anything meaningful;
  // grey them out when there isn't one. Copy and 全选 are always allowed so
  // users can still extract text from a read-only composer — only the
  // mutating actions (cut/paste/delete) are blocked when the textarea is
  // disabled.
  const canMutate = hasSelection && !disabled;
  const canCopy = hasSelection;

  return createPortal(
    <div
      ref={ref}
      className="composer-context-menu"
      role="menu"
      style={{ left: position.x, top: position.y }}
      onContextMenu={(event) => event.preventDefault()}
    >
      <button
        type="button"
        role="menuitem"
        className="composer-context-menu-item"
        onClick={runCut}
        disabled={!canMutate}
      >
        剪切
      </button>
      <button
        type="button"
        role="menuitem"
        className="composer-context-menu-item"
        onClick={runCopy}
        disabled={!canCopy}
      >
        复制
      </button>
      <button
        type="button"
        role="menuitem"
        className="composer-context-menu-item"
        onClick={() => {
          void runPaste();
        }}
        disabled={disabled}
      >
        粘贴
      </button>
      <button
        type="button"
        role="menuitem"
        className="composer-context-menu-item"
        onClick={runSelectAll}
      >
        全选
      </button>
      <button
        type="button"
        role="menuitem"
        className="composer-context-menu-item"
        onClick={runDelete}
        disabled={!canMutate}
      >
        删除
      </button>
    </div>,
    document.body
  );
}
