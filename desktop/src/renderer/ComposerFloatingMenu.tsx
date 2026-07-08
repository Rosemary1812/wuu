import {
  type CSSProperties,
  type ReactNode,
  type RefObject,
  useLayoutEffect,
  useState
} from "react";
import { createPortal } from "react-dom";
import type {
  FloatingMenuAlign,
  FloatingMenuOwner,
  FloatingMenuPlacement
} from "./ComposerTypes";

export function isInsideFloatingMenu(target: Node, owner: FloatingMenuOwner): boolean {
  const element = target instanceof Element ? target : target.parentElement;
  return Boolean(element?.closest('[data-floating-menu-owner="' + owner + '"]'));
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}

export function FloatingMenuPortal({
  anchorRef,
  owner,
  placement,
  align,
  offset = 8,
  crossAxisOffset = 0,
  width,
  children
}: {
  anchorRef: RefObject<HTMLElement | null>;
  owner: FloatingMenuOwner;
  placement: FloatingMenuPlacement;
  align: FloatingMenuAlign;
  offset?: number;
  crossAxisOffset?: number;
  width: number;
  children: ReactNode;
}): JSX.Element | null {
  const [style, setStyle] = useState<CSSProperties>({
    position: "fixed",
    visibility: "hidden"
  });

  useLayoutEffect(() => {
    function updatePosition(): void {
      const anchor = anchorRef.current;
      if (!anchor) {
        return;
      }
      const viewportMargin = 8;
      const rect = anchor.getBoundingClientRect();
      const baseLeft = align === "right" ? rect.right - width : rect.left;
      const maxLeft = Math.max(viewportMargin, window.innerWidth - width - viewportMargin);
      const left = clamp(baseLeft + crossAxisOffset, viewportMargin, maxLeft);
      const nextStyle: CSSProperties = {
        left,
        position: "fixed",
        visibility: "visible",
        // Sit above any modal overlay the floating menu might be opened
        // from. The new-participant dialog (and the shared sidebar-name
        // dialog + conversation-search dialog it reuses) is portaled to
        // body at z-index: 200, so a SelectMenu dropdown portaled from
        // inside that dialog needs to be > 200 to remain visible. 220
        // clears the modal band while staying below anything an app
        // surface might intentionally pin above.
        zIndex: 220,
      };
      if (placement === "above") {
        nextStyle.bottom = Math.max(viewportMargin, window.innerHeight - rect.top + offset);
      } else if (placement === "below") {
        nextStyle.top = Math.max(viewportMargin, rect.bottom + offset);
      } else {
        nextStyle.top = clamp(
          rect.top + rect.height / 2,
          viewportMargin,
          window.innerHeight - viewportMargin
        );
        nextStyle.transform = "translateY(-50%)";
      }
      setStyle(nextStyle);
    }

    updatePosition();
    window.addEventListener("resize", updatePosition);
    window.addEventListener("scroll", updatePosition, true);
    return () => {
      window.removeEventListener("resize", updatePosition);
      window.removeEventListener("scroll", updatePosition, true);
    };
  }, [align, anchorRef, crossAxisOffset, offset, placement, width]);

  return createPortal(
    <div className={`floating-menu-layer floating-menu-${placement}`} data-floating-menu-owner={owner} style={style}>
      {children}
    </div>,
    document.body
  );
}
