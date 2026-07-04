import { X } from "lucide-react";
import {
  type FormEvent as ReactFormEvent,
  type KeyboardEvent as ReactKeyboardEvent,
  type MouseEvent as ReactMouseEvent,
  type ReactNode,
  useCallback,
  useEffect,
  useRef,
} from "react";

/**
 * Shared chrome for the application's environment-style dialogs
 * (fork picker, git commit/PR, destructive confirmations). Centralizes the
 * backdrop, header, focus, Esc, and backdrop-click dismissal
 * behavior so each dialog only owns its body and footer.
 *
 * Close model:
 * - Pass `onClose` to opt into X button + Escape + backdrop dismissal.
 * - Omit `onClose` to render a non-dismissible dialog.
 * - `closeDisabled` temporarily locks down every close affordance
 *   while an in-flight promise is still resolving.
 *
 * Form model:
 * - By default the panel is a `<div>`. Set `asForm` to render a
 *   `<form>` instead so submit buttons inside `footer` (or anywhere
 *   inside `children`) participate in form submission. Modal calls
 *   `event.preventDefault()` before forwarding submit, mirroring the
 *   previous hand-rolled forms.
 */
export type ModalProps = {
  ariaLabel: string;
  icon: ReactNode;
  title: ReactNode;
  subtitle?: ReactNode;
  onClose?: () => void;
  closeDisabled?: boolean;
  panelClassName?: string;
  initialFocus?: "first-interactive" | "none";
  asForm?: boolean;
  onSubmit?: (event: ReactFormEvent<HTMLFormElement>) => void;
  footer?: ReactNode;
  children?: ReactNode;
};

export function Modal({
  ariaLabel,
  icon,
  title,
  subtitle,
  onClose,
  closeDisabled = false,
  panelClassName,
  initialFocus = "first-interactive",
  asForm = false,
  onSubmit,
  footer,
  children,
}: ModalProps): JSX.Element | null {
  const panelRef = useRef<HTMLElement | null>(null);
  const dismissible = typeof onClose === "function" && !closeDisabled;

  const setPanelRef = useCallback((node: HTMLElement | null) => {
    panelRef.current = node;
  }, []);

  useEffect(() => {
    if (initialFocus !== "first-interactive") {
      return;
    }
    const panel = panelRef.current;
    if (!panel) {
      return;
    }
    // Skip the dialog's own X button (it always lives in the header
    // and has class `icon-button`). Otherwise land on the first
    // usable form control / button inside the panel.
    const target = panel.querySelector<HTMLElement>(
      "input:not([type=hidden]):not(:disabled), " +
        "textarea:not(:disabled), " +
        "select:not(:disabled), " +
        "button:not(.icon-button):not(:disabled)",
    );
    target?.focus();
  }, [initialFocus]);

  useEffect(() => {
    if (!onClose) {
      return;
    }
    function handleKeyDown(event: KeyboardEvent): void {
      if (event.key !== "Escape") {
        return;
      }
      if (closeDisabled) {
        return;
      }
      event.preventDefault();
      onClose?.();
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => {
      window.removeEventListener("keydown", handleKeyDown);
    };
  }, [onClose, closeDisabled]);

  function handleBackdropClick(): void {
    if (!dismissible) {
      return;
    }
    onClose?.();
  }

  function stopBubble(
    event: ReactMouseEvent<HTMLElement> | ReactKeyboardEvent<HTMLElement>,
  ): void {
    // Clicks inside the panel must not reach the backdrop, otherwise
    // backdrop dismissal fires when the user is interacting with the
    // dialog body (selections, copy, button presses that don't stop
    // propagation themselves).
    event.stopPropagation();
  }

  const panelClass = ["environment-dialog", panelClassName]
    .filter(Boolean)
    .join(" ");

  const backdropClass = "modal-backdrop environment-modal-backdrop";

  const panelBody = (
    <>
      <div className="environment-dialog-header">
        <span className="environment-dialog-icon">{icon}</span>
        {onClose ? (
          <button
            className="icon-button"
            type="button"
            aria-label="关闭"
            disabled={closeDisabled}
            onClick={() => {
              if (!closeDisabled) {
                onClose();
              }
            }}
          >
            <X className="icon" />
          </button>
        ) : null}
      </div>
      <h2>{title}</h2>
      {subtitle ? (
        <p className="environment-dialog-subtitle">{subtitle}</p>
      ) : null}
      {children}
      {footer ? <div className="environment-dialog-footer">{footer}</div> : null}
    </>
  );

  const content = (
    <div
      className={backdropClass}
      onClick={handleBackdropClick}
      role="presentation"
    >
      {asForm ? (
        <form
          ref={setPanelRef}
          className={panelClass}
          role="dialog"
          aria-modal="true"
          aria-label={ariaLabel}
          onClick={stopBubble}
          onKeyDown={stopBubble}
          onSubmit={(event) => {
            event.preventDefault();
            onSubmit?.(event);
          }}
        >
          {panelBody}
        </form>
      ) : (
        <div
          ref={setPanelRef}
          className={panelClass}
          role="dialog"
          aria-modal="true"
          aria-label={ariaLabel}
          onClick={stopBubble}
          onKeyDown={stopBubble}
        >
          {panelBody}
        </div>
      )}
    </div>
  );

  return content;
}
