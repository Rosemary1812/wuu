import { X } from "lucide-react";
import {
  type FormEvent as ReactFormEvent,
  type KeyboardEvent as ReactKeyboardEvent,
  type MouseEvent as ReactMouseEvent,
  type ReactNode,
  type RefObject,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";
import { createPortal } from "react-dom";

/**
 * Shared chrome for the application's environment-style dialogs
 * (fork picker, git commit/PR, tool approval). Centralizes the
 * backdrop, header, focus, Esc, and backdrop-click dismissal
 * behavior so each dialog only owns its body and footer.
 *
 * Close model:
 * - Pass `onClose` to opt into X button + Escape + backdrop dismissal.
 * - Omit `onClose` to render a non-dismissible dialog (e.g. tool
 *   approval, where the user must explicitly approve or deny).
 * - `closeDisabled` temporarily locks down every close affordance
 *   while an in-flight promise is still resolving.
 *
 * Form model:
 * - By default the panel is a `<div>`. Set `asForm` to render a
 *   `<form>` instead so submit buttons inside `footer` (or anywhere
 *   inside `children`) participate in form submission. Modal calls
 *   `event.preventDefault()` before forwarding submit, mirroring the
 *   previous hand-rolled forms.
 *
 * Scoping model:
 * - Pass `hostRef` to render the backdrop into a specific DOM node
 *   (typically the conversation pane) and switch the backdrop to
 *   `position: absolute`. The dim layer then only covers the host
 *   element, leaving the sidebar and any other root-level chrome
 *   completely untouched. Omit `hostRef` to fall back to a global
 *   `position: fixed; inset: 0` backdrop that covers the viewport.
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
  /**
   * When provided, render the backdrop into `hostRef.current` and use
   * `position: absolute` so the dim layer only covers the host box.
   * The host element must be a positioned ancestor — the conversation
   * pane already is. If the ref is not yet attached at render time
   * (e.g. when the dialog first appears before the host has finished
   * mounting), the dialog waits a tick before mounting via a portal.
   */
  hostRef?: RefObject<HTMLElement | null>;
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
  hostRef,
}: ModalProps): JSX.Element | null {
  const panelRef = useRef<HTMLElement | null>(null);
  const [host, setHost] = useState<HTMLElement | null>(null);
  const dismissible = typeof onClose === "function" && !closeDisabled;

  // Resolve `hostRef.current` once it has been attached. React's
  // commit phase attaches refs before useEffect, so by the time this
  // effect runs `hostRef.current` is normally the live DOM node. The
  // setTimeout fallback covers the unusual case where a dialog
  // appears before its host (e.g. conditional render that opens
  // before the host has fully committed).
  useEffect(() => {
    if (!hostRef) {
      setHost(null);
      return;
    }
    if (hostRef.current) {
      setHost(hostRef.current);
      return;
    }
    const id = window.setTimeout(() => {
      if (hostRef.current) {
        setHost(hostRef.current);
      }
    }, 0);
    return () => {
      window.clearTimeout(id);
    };
  }, [hostRef]);

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
  }, [initialFocus, host]);

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

  const backdropClass = [
    "modal-backdrop",
    "environment-modal-backdrop",
    hostRef ? "scoped" : null,
  ]
    .filter(Boolean)
    .join(" ");

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

  if (hostRef) {
    if (!host) {
      return null;
    }
    return createPortal(content, host);
  }
  return content;
}
