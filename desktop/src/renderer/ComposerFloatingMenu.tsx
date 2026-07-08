import {
  type CSSProperties,
  type ReactNode,
  type RefObject,
  useLayoutEffect,
  useRef,
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

// Minimum visible height before a flip is preferred. 240px fits ~6
// 36px menu items — anything tighter than this on the requested side
// means the panel would scroll immediately, so flipping to the
// opposite side (when that side has more room) keeps the list
// readable.
const FLIP_MIN_HEIGHT = 240;

export function FloatingMenuPortal({
  anchorRef,
  owner,
  placement,
  align,
  offset = 8,
  crossAxisOffset = 0,
  width,
  // When true, flip to the opposite side of the trigger if the
  // requested placement doesn't have room. Use this for dropdowns
  // whose content height is uncertain (model pickers, tag pickers)
  // and which may sit near a viewport edge inside a modal — without
  // flipping, the panel overflows the viewport bottom and the
  // tail of the list is hidden behind the next layer / window
  // chrome. Defaults to false so existing callers keep their
  // explicit placement.
  flip = false,
  children
}: {
  anchorRef: RefObject<HTMLElement | null>;
  owner: FloatingMenuOwner;
  placement: FloatingMenuPlacement;
  align: FloatingMenuAlign;
  offset?: number;
  crossAxisOffset?: number;
  width: number;
  flip?: boolean;
  children: ReactNode;
}): JSX.Element | null {
  const [resolvedPlacement, setResolvedPlacement] =
    useState<FloatingMenuPlacement>(placement);
  const [style, setStyle] = useState<CSSProperties>({
    position: "fixed",
    visibility: "hidden"
  });
  const layerRef = useRef<HTMLDivElement>(null);

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

      // Auto-flip: if the requested side has less than FLIP_MIN_HEIGHT
      // pixels free AND the opposite side has more room, prefer the
      // opposite side. This keeps dropdowns readable when the trigger
      // is near a viewport edge (e.g. the last field of a centered
      // modal whose list is taller than the space below).
      let actualPlacement: FloatingMenuPlacement = placement;
      if (flip && (placement === "above" || placement === "below")) {
        const spaceBelow = window.innerHeight - rect.bottom - offset;
        const spaceAbove = rect.top - offset;
        if (
          placement === "below" &&
          spaceBelow < FLIP_MIN_HEIGHT &&
          spaceAbove > spaceBelow
        ) {
          actualPlacement = "above";
        } else if (
          placement === "above" &&
          spaceAbove < FLIP_MIN_HEIGHT &&
          spaceBelow > spaceAbove
        ) {
          actualPlacement = "below";
        }
      }
      if (actualPlacement !== resolvedPlacement) {
        setResolvedPlacement(actualPlacement);
      }

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

      // Constrain max-height to the available viewport room on the
      // chosen side. This is the proportional-scaling half of the
      // fix: when the list is taller than the side can show, the
      // panel scrolls internally instead of overflowing the viewport.
      let availableHeight: number;
      if (actualPlacement === "above") {
        nextStyle.bottom = Math.max(
          viewportMargin,
          window.innerHeight - rect.top + offset
        );
        availableHeight = Math.max(80, rect.top - offset - viewportMargin);
      } else if (actualPlacement === "below") {
        nextStyle.top = Math.max(viewportMargin, rect.bottom + offset);
        availableHeight = Math.max(
          80,
          window.innerHeight - rect.bottom - offset - viewportMargin
        );
      } else {
        nextStyle.top = clamp(
          rect.top + rect.height / 2,
          viewportMargin,
          window.innerHeight - viewportMargin
        );
        nextStyle.transform = "translateY(-50%)";
        availableHeight = Math.max(
          80,
          Math.min(window.innerHeight - 2 * viewportMargin, 420)
        );
      }
      nextStyle.maxHeight = `${availableHeight}px`;

      setStyle(nextStyle);
    }

    updatePosition();
    window.addEventListener("resize", updatePosition);
    window.addEventListener("scroll", updatePosition, true);
    return () => {
      window.removeEventListener("resize", updatePosition);
      window.removeEventListener("scroll", updatePosition, true);
    };
  }, [
    align,
    anchorRef,
    crossAxisOffset,
    flip,
    offset,
    placement,
    resolvedPlacement,
    width
  ]);

  return createPortal(
    <div
      ref={layerRef}
      className={`floating-menu-layer floating-menu-${resolvedPlacement}`}
      data-floating-menu-owner={owner}
      style={style}
    >
      {children}
    </div>,
    document.body
  );
}
