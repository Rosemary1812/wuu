import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type MutableRefObject,
  type RefObject,
} from "react";

export const SIDEBAR_DRAWER_HOVER_OPEN_DELAY_MS = 240;

export type SidebarDrawerPhase = "closed" | "open" | "closing";

export type SidebarDrawerStateController = {
  sidebarDrawerPhase: SidebarDrawerPhase;
  sidebarHoverZoneRef: MutableRefObject<HTMLDivElement | null>;
  cancelSidebarDrawerOpen: () => void;
  openSidebarDrawer: () => void;
  openSidebarDrawerNow: () => void;
  scheduleSidebarDrawerOpen: () => void;
  closeSidebarDrawer: () => void;
  syncSidebarDrawerHover: () => void;
};

// The drawer controller serves both shells: the main app (`aside.sidebar`)
// and the settings page (`aside.settings-sidebar`). Pointer-hover checks must
// match either rail, otherwise moving the pointer from the hover zone into
// the revealed drawer counts as "left the drawer" and closes it.
const SIDEBAR_RAIL_SELECTOR = ".sidebar, .settings-sidebar";

export function useSidebarDrawerState({
  appShellRef,
  sidebarCollapsed,
  resizingSidebar,
  activeSessionTabID,
  motionMs,
  hoverOpenDelayMs = SIDEBAR_DRAWER_HOVER_OPEN_DELAY_MS,
}: {
  appShellRef: RefObject<HTMLDivElement | null>;
  sidebarCollapsed: boolean;
  resizingSidebar: boolean;
  activeSessionTabID: string;
  motionMs: number;
  hoverOpenDelayMs?: number;
}): SidebarDrawerStateController {
  const [sidebarDrawerPhase, setSidebarDrawerPhase] =
    useState<SidebarDrawerPhase>("closed");
  const sidebarDrawerOpenTimerRef = useRef<number | undefined>(undefined);
  const sidebarDrawerCloseTimerRef = useRef<number | undefined>(undefined);
  const sidebarHoverZoneRef = useRef<HTMLDivElement | null>(null);
  const sidebarHoverZoneActiveRef = useRef(false);
  const sidebarPointerPositionRef = useRef<{ x: number; y: number } | null>(
    null,
  );
  const sidebarDrawerSyncedSessionRef = useRef<string | undefined>(undefined);
  const sidebarDrawerSuppressedRef = useRef(false);
  const sidebarDrawerExplicitlyOpenedRef = useRef(false);

  const clearSidebarDrawerCloseTimer = useCallback((): void => {
    if (sidebarDrawerCloseTimerRef.current !== undefined) {
      window.clearTimeout(sidebarDrawerCloseTimerRef.current);
      sidebarDrawerCloseTimerRef.current = undefined;
    }
  }, []);

  const clearSidebarDrawerOpenTimer = useCallback((): void => {
    if (sidebarDrawerOpenTimerRef.current !== undefined) {
      window.clearTimeout(sidebarDrawerOpenTimerRef.current);
      sidebarDrawerOpenTimerRef.current = undefined;
    }
  }, []);

  const cancelSidebarDrawerOpen = useCallback((): void => {
    sidebarHoverZoneActiveRef.current = false;
    clearSidebarDrawerOpenTimer();
  }, [clearSidebarDrawerOpenTimer]);

  const rememberSidebarPointerPosition = useCallback((event: Event): void => {
    if (!(event instanceof MouseEvent)) {
      return;
    }
    sidebarPointerPositionRef.current = {
      x: event.clientX,
      y: event.clientY,
    };
  }, []);

  const sidebarDrawerPointerHovered = useCallback((): boolean | undefined => {
    const point = sidebarPointerPositionRef.current;
    if (!point || typeof document.elementFromPoint !== "function") {
      return undefined;
    }
    const target = document.elementFromPoint(point.x, point.y);
    if (!target) {
      return undefined;
    }
    const sidebar = appShellRef.current?.querySelector(SIDEBAR_RAIL_SELECTOR);
    const hoverZone = sidebarHoverZoneRef.current;
    return Boolean(
      (sidebar && sidebar.contains(target)) ||
        (hoverZone && hoverZone.contains(target)),
    );
  }, [appShellRef]);

  const blurSidebarFocus = useCallback((): void => {
    const sidebar = appShellRef.current?.querySelector(SIDEBAR_RAIL_SELECTOR);
    const active = document.activeElement;
    if (sidebar && active instanceof HTMLElement && sidebar.contains(active)) {
      active.blur();
    }
  }, [appShellRef]);

  const openSidebarDrawer = useCallback((): void => {
    if (resizingSidebar || sidebarDrawerSuppressedRef.current) {
      return;
    }
    if (sidebarCollapsed && sidebarDrawerPointerHovered() === false) {
      return;
    }
    clearSidebarDrawerOpenTimer();
    clearSidebarDrawerCloseTimer();
    if (sidebarCollapsed) {
      setSidebarDrawerPhase("open");
    }
  }, [
    clearSidebarDrawerCloseTimer,
    clearSidebarDrawerOpenTimer,
    resizingSidebar,
    sidebarCollapsed,
    sidebarDrawerPointerHovered,
  ]);

  const openSidebarDrawerNow = useCallback((): void => {
    if (resizingSidebar || !sidebarCollapsed) {
      return;
    }
    // This path is triggered by the navigation button inside a globalized
    // workspace, which is outside the drawer. Keep the drawer available
    // while the pointer travels from that button into the revealed sidebar.
    sidebarDrawerExplicitlyOpenedRef.current = true;
    clearSidebarDrawerOpenTimer();
    clearSidebarDrawerCloseTimer();
    setSidebarDrawerPhase("open");
  }, [
    clearSidebarDrawerCloseTimer,
    clearSidebarDrawerOpenTimer,
    resizingSidebar,
    sidebarCollapsed,
  ]);

  const scheduleSidebarDrawerOpen = useCallback((): void => {
    sidebarHoverZoneActiveRef.current = true;
    if (
      resizingSidebar ||
      sidebarDrawerSuppressedRef.current ||
      !sidebarCollapsed
    ) {
      return;
    }
    clearSidebarDrawerOpenTimer();
    sidebarDrawerOpenTimerRef.current = window.setTimeout(() => {
      sidebarDrawerOpenTimerRef.current = undefined;
      if (
        !sidebarHoverZoneActiveRef.current ||
        resizingSidebar ||
        sidebarDrawerSuppressedRef.current ||
        sidebarDrawerPointerHovered() === false
      ) {
        return;
      }
      clearSidebarDrawerCloseTimer();
      setSidebarDrawerPhase("open");
    }, hoverOpenDelayMs);
  }, [
    clearSidebarDrawerCloseTimer,
    clearSidebarDrawerOpenTimer,
    hoverOpenDelayMs,
    resizingSidebar,
    sidebarCollapsed,
    sidebarDrawerPointerHovered,
  ]);

  const closeSidebarDrawer = useCallback((): void => {
    sidebarDrawerExplicitlyOpenedRef.current = false;
    cancelSidebarDrawerOpen();
    clearSidebarDrawerCloseTimer();
    blurSidebarFocus();
    if (!sidebarCollapsed || resizingSidebar) {
      setSidebarDrawerPhase("closed");
      return;
    }
    setSidebarDrawerPhase("closing");
    sidebarDrawerCloseTimerRef.current = window.setTimeout(() => {
      sidebarDrawerCloseTimerRef.current = undefined;
      setSidebarDrawerPhase("closed");
    }, motionMs);
  }, [
    blurSidebarFocus,
    cancelSidebarDrawerOpen,
    clearSidebarDrawerCloseTimer,
    motionMs,
    resizingSidebar,
    sidebarCollapsed,
  ]);

  const syncSidebarDrawerHover = useCallback((): void => {
    if (!sidebarCollapsed || sidebarDrawerPhase !== "open") {
      return;
    }
    if (sidebarDrawerExplicitlyOpenedRef.current) {
      return;
    }
    if (sidebarDrawerPointerHovered() === false) {
      closeSidebarDrawer();
    }
  }, [
    closeSidebarDrawer,
    sidebarCollapsed,
    sidebarDrawerPhase,
    sidebarDrawerPointerHovered,
  ]);

  useEffect(() => {
    function handlePointerEvent(event: Event): void {
      rememberSidebarPointerPosition(event);
      syncSidebarDrawerHover();
    }
    window.addEventListener("pointermove", handlePointerEvent, true);
    window.addEventListener("pointerover", handlePointerEvent, true);
    window.addEventListener("pointerdown", handlePointerEvent, true);
    window.addEventListener("mousemove", handlePointerEvent, true);
    window.addEventListener("mouseover", handlePointerEvent, true);
    window.addEventListener("mousedown", handlePointerEvent, true);
    return () => {
      window.removeEventListener("pointermove", handlePointerEvent, true);
      window.removeEventListener("pointerover", handlePointerEvent, true);
      window.removeEventListener("pointerdown", handlePointerEvent, true);
      window.removeEventListener("mousemove", handlePointerEvent, true);
      window.removeEventListener("mouseover", handlePointerEvent, true);
      window.removeEventListener("mousedown", handlePointerEvent, true);
    };
  }, [rememberSidebarPointerPosition, syncSidebarDrawerHover]);

  useLayoutEffect(() => {
    if (!sidebarCollapsed) {
      sidebarDrawerExplicitlyOpenedRef.current = false;
    }
    if (!sidebarCollapsed && sidebarDrawerPhase !== "closed") {
      cancelSidebarDrawerOpen();
      clearSidebarDrawerCloseTimer();
      setSidebarDrawerPhase("closed");
    }
  }, [
    cancelSidebarDrawerOpen,
    clearSidebarDrawerCloseTimer,
    sidebarCollapsed,
    sidebarDrawerPhase,
  ]);

  useEffect(() => {
    if (!sidebarCollapsed || resizingSidebar) {
      cancelSidebarDrawerOpen();
    }
  }, [cancelSidebarDrawerOpen, resizingSidebar, sidebarCollapsed]);

  useEffect(() => {
    if (!sidebarCollapsed || sidebarDrawerPhase !== "open") {
      return undefined;
    }
    function handleWindowMouseOut(event: MouseEvent): void {
      if (event.relatedTarget === null) {
        closeSidebarDrawer();
      }
    }
    window.addEventListener("mouseout", handleWindowMouseOut);
    window.addEventListener("blur", closeSidebarDrawer);
    return () => {
      window.removeEventListener("mouseout", handleWindowMouseOut);
      window.removeEventListener("blur", closeSidebarDrawer);
    };
  }, [closeSidebarDrawer, sidebarCollapsed, sidebarDrawerPhase]);

  useEffect(() => {
    if (!sidebarCollapsed) {
      return undefined;
    }
    function handleWindowMouseOut(event: MouseEvent): void {
      if (event.relatedTarget === null) {
        cancelSidebarDrawerOpen();
      }
    }
    window.addEventListener("mouseout", handleWindowMouseOut);
    window.addEventListener("blur", cancelSidebarDrawerOpen);
    return () => {
      window.removeEventListener("mouseout", handleWindowMouseOut);
      window.removeEventListener("blur", cancelSidebarDrawerOpen);
    };
  }, [cancelSidebarDrawerOpen, sidebarCollapsed]);

  useEffect(() => {
    if (resizingSidebar) {
      sidebarDrawerSuppressedRef.current = true;
      return undefined;
    }
    if (!sidebarDrawerSuppressedRef.current) {
      return undefined;
    }
    if (!sidebarCollapsed) {
      sidebarDrawerSuppressedRef.current = false;
      return undefined;
    }
    function handlePointerMove(event: PointerEvent): void {
      const zone = sidebarHoverZoneRef.current?.getBoundingClientRect();
      if (!zone || event.clientX > zone.right || event.clientX < zone.left) {
        sidebarDrawerSuppressedRef.current = false;
        window.removeEventListener("pointermove", handlePointerMove);
      }
    }
    window.addEventListener("pointermove", handlePointerMove);
    return () => window.removeEventListener("pointermove", handlePointerMove);
  }, [resizingSidebar, sidebarCollapsed]);

  useEffect(() => {
    if (!resizingSidebar || !sidebarCollapsed) {
      return;
    }
    const sidebar = appShellRef.current?.querySelector(SIDEBAR_RAIL_SELECTOR);
    const active = document.activeElement;
    if (sidebar && active instanceof HTMLElement && sidebar.contains(active)) {
      active.blur();
    }
  }, [appShellRef, resizingSidebar, sidebarCollapsed]);

  useEffect(() => {
    const sessionID = activeSessionTabID;
    if (!sidebarCollapsed || sidebarDrawerPhase !== "open") {
      sidebarDrawerSyncedSessionRef.current = sessionID;
      return;
    }
    if (sidebarDrawerSyncedSessionRef.current === sessionID) {
      return;
    }
    sidebarDrawerSyncedSessionRef.current = sessionID;
    if (sidebarDrawerPointerHovered() === false) {
      closeSidebarDrawer();
    }
  }, [
    activeSessionTabID,
    closeSidebarDrawer,
    sidebarCollapsed,
    sidebarDrawerPhase,
    sidebarDrawerPointerHovered,
  ]);

  useEffect(
    () => () => {
      clearSidebarDrawerOpenTimer();
      clearSidebarDrawerCloseTimer();
    },
    [clearSidebarDrawerCloseTimer, clearSidebarDrawerOpenTimer],
  );

  return {
    sidebarDrawerPhase,
    sidebarHoverZoneRef,
    cancelSidebarDrawerOpen,
    openSidebarDrawer,
    openSidebarDrawerNow,
    scheduleSidebarDrawerOpen,
    closeSidebarDrawer,
    syncSidebarDrawerHover,
  };
}
