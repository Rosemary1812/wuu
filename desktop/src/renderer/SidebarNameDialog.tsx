import { type LucideIcon } from "lucide-react";
import {
  type AnimationEvent,
  type ChangeEvent,
  useCallback,
  useEffect,
  type MouseEvent,
  type ReactElement,
  type ReactNode,
  useRef,
  useState,
} from "react";
import { createPortal } from "react-dom";

export interface SidebarNameDialogProps {
  open: boolean;
  title: string;
  onTitleChange: (title: string) => void;
  onSubmit: () => void;
  onClose: () => void;
  dialogTitle: string;
  dialogTitleId: string;
  fieldLabel: string;
  fieldAriaLabel: string;
  placeholder: string;
  icon: LucideIcon;
  submitLabel: string;
  cancelLabel: string;
  content?: ReactNode;
  submitDisabled?: boolean;
  destructiveAction?: {
    label: string;
    onClick: () => void;
  };
  variant?: "default" | "drawer";
  hideActions?: boolean;
}

// Shared floating name dialog for the sidebar flows that need a single text
// input (for example, renaming a conversation). Same visual shell as
// the conversation-search overlay so the product stays consistent.
export function SidebarNameDialog({
  open,
  title,
  onTitleChange,
  onSubmit,
  onClose,
  dialogTitle,
  dialogTitleId,
  fieldLabel,
  fieldAriaLabel,
  placeholder,
  icon: Icon,
  submitLabel,
  cancelLabel,
  content,
  submitDisabled,
  destructiveAction,
  variant = "default",
  hideActions = false,
}: SidebarNameDialogProps): ReactElement | null {
  const [closing, setClosing] = useState(false);
  const closeTimerRef = useRef<number | null>(null);

  const finishClose = useCallback((): void => {
    if (closeTimerRef.current !== null) {
      window.clearTimeout(closeTimerRef.current);
      closeTimerRef.current = null;
    }
    onClose();
    setClosing(false);
  }, [onClose]);

  const requestClose = useCallback((): void => {
    if (closing) return;
    if (variant !== "drawer") {
      onClose();
      return;
    }
    setClosing(true);
    closeTimerRef.current = window.setTimeout(finishClose, 220);
  }, [closing, finishClose, onClose, variant]);

  useEffect(() => () => {
    if (closeTimerRef.current !== null) window.clearTimeout(closeTimerRef.current);
  }, []);

  useEffect(() => {
    if (!open) {
      return;
    }
    function handleKeyDown(event: KeyboardEvent): void {
      if (event.key !== "Escape") {
        return;
      }
      event.preventDefault();
      requestClose();
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [open, requestClose]);

  if (!open) {
    return null;
  }

  function handleOverlayPointerDown(event: MouseEvent<HTMLDivElement>): void {
    if (event.target === event.currentTarget) {
      requestClose();
    }
  }

  function handleDrawerAnimationEnd(event: AnimationEvent<HTMLFormElement>): void {
    if (closing && event.target === event.currentTarget) finishClose();
  }

  function handleInputChange(event: ChangeEvent<HTMLInputElement>): void {
    onTitleChange(event.currentTarget.value);
  }

  return createPortal(
    <div
      className={`conversation-search-overlay sidebar-name-dialog-overlay${variant === "drawer" ? " sidebar-name-dialog-overlay-drawer" : ""}${closing ? " closing" : ""}`}
      onPointerDown={handleOverlayPointerDown}
    >
      <form
        className={`conversation-search-dialog sidebar-name-dialog${variant === "drawer" ? " sidebar-name-dialog-drawer" : ""}${closing ? " closing" : ""}`}
        role="dialog"
        aria-modal="true"
        aria-labelledby={dialogTitleId}
        onAnimationEnd={handleDrawerAnimationEnd}
        onSubmit={(event) => {
          event.preventDefault();
          onSubmit();
        }}
      >
        <div className="sidebar-name-dialog-header">
          <span className="sidebar-name-dialog-icon" aria-hidden="true">
            <Icon className="icon-lg" />
          </span>
          <h2 id={dialogTitleId} className="sidebar-name-dialog-title">
            {dialogTitle}
          </h2>
        </div>
        {content ?? <label className="sidebar-name-dialog-field">
          <span className="sidebar-name-dialog-label">{fieldLabel}</span>
          <input
            className="sidebar-name-dialog-input"
            value={title}
            aria-label={fieldAriaLabel}
            placeholder={placeholder}
            autoFocus
            onChange={handleInputChange}
            onFocus={(event) => event.currentTarget.select()}
          />
        </label>}
        {!hideActions ? <div className="sidebar-name-dialog-actions">
          {destructiveAction ? (
            <button className="sidebar-name-dialog-destructive" type="button" onClick={destructiveAction.onClick}>
              {destructiveAction.label}
            </button>
          ) : null}
          {destructiveAction ? <span className="sidebar-name-dialog-action-spacer" aria-hidden="true" /> : null}
          <button type="button" onClick={requestClose}>
            {cancelLabel}
          </button>
          <button type="submit" disabled={submitDisabled ?? title.trim().length === 0}>
            {submitLabel}
          </button>
        </div> : null}
      </form>
    </div>,
    document.body,
  );
}
