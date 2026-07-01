import {
  type KeyboardEvent as ReactKeyboardEvent,
  type MutableRefObject,
  type PointerEvent as ReactPointerEvent,
  type RefObject,
  useCallback,
  useEffect,
  useRef,
  useState
} from "react";

export const SIDEBAR_MOTION_MS = 280;
export const RIGHT_PANEL_MOTION_MS = 280;
export const SIDEBAR_DEFAULT_WIDTH = 326;
export const SIDEBAR_MIN_WIDTH = 240;
export const SIDEBAR_MAX_WIDTH = 520;
const SIDEBAR_STEP = 24;
const SIDEBAR_WIDTH_KEY = "wuu.desktop.sidebarWidth";
const SIDEBAR_COLLAPSED_KEY = "wuu.desktop.sidebarCollapsed";
export const WORKSPACE_RIGHT_PANEL_DEFAULT_WIDTH = 360;
export const WORKSPACE_RIGHT_PANEL_MIN_WIDTH = 300;
export const WORKSPACE_RIGHT_PANEL_MAX_WIDTH = 860;
const WORKSPACE_RIGHT_PANEL_MAIN_MIN_WIDTH = 360;
const WORKSPACE_RIGHT_PANEL_STEP = 32;
const WORKSPACE_RIGHT_PANEL_WIDTH_KEY = "wuu.desktop.workspaceRightPanelWidth";

type SidebarResizeSession = {
  startX: number;
  startWidth: number;
  currentWidth: number;
  allowCollapse: boolean;
  // Tracks whether the sidebar collapsed during the current drag (either at
  // pointerdown or by crossing below SIDEBAR_MIN_WIDTH mid-drag). When true
  // and the user drags back above the threshold without releasing, the move
  // handler must route through applySidebarWidth so the collapsed React state
  // is cleared; otherwise applyLiveSidebarWidth would only mutate the
  // --sidebar-width inline style while the .sidebar-collapsed class stays on
  // .app-shell, leaving a white glass slab with no content visible.
  collapsedDuringDrag: boolean;
};

type RightPanelResizeSession = {
  startX: number;
  startWidth: number;
  currentWidth: number;
};

type WindowPointerResizeController<Session> = {
  resizing: boolean;
  sessionRef: MutableRefObject<Session | null>;
  onMove: (event: PointerEvent, session: Session) => void;
  onEnd: (session: Session | null) => void;
};

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}

function initialSidebarWidth(): number {
  const stored = Number(window.localStorage.getItem(SIDEBAR_WIDTH_KEY));
  if (!Number.isFinite(stored)) {
    return SIDEBAR_DEFAULT_WIDTH;
  }
  return clamp(stored, SIDEBAR_MIN_WIDTH, SIDEBAR_MAX_WIDTH);
}

function initialSidebarCollapsed(): boolean {
  return window.localStorage.getItem(SIDEBAR_COLLAPSED_KEY) === "true";
}

function initialWorkspaceRightPanelWidth(): number {
  const stored = Number(window.localStorage.getItem(WORKSPACE_RIGHT_PANEL_WIDTH_KEY));
  if (!Number.isFinite(stored)) {
    return WORKSPACE_RIGHT_PANEL_DEFAULT_WIDTH;
  }
  return clamp(stored, WORKSPACE_RIGHT_PANEL_MIN_WIDTH, WORKSPACE_RIGHT_PANEL_MAX_WIDTH);
}

function clampWorkspaceRightPanelWidth(width: number, sidebarWidth: number): number {
  const maxForWindow =
    typeof window === "undefined"
      ? WORKSPACE_RIGHT_PANEL_MAX_WIDTH
      : window.innerWidth - sidebarWidth - WORKSPACE_RIGHT_PANEL_MAIN_MIN_WIDTH;
  const maxWidth = Math.max(
    WORKSPACE_RIGHT_PANEL_MIN_WIDTH,
    Math.min(WORKSPACE_RIGHT_PANEL_MAX_WIDTH, maxForWindow)
  );
  return clamp(width, WORKSPACE_RIGHT_PANEL_MIN_WIDTH, maxWidth);
}

function useWindowPointerResize<Session>({
  resizing,
  sessionRef,
  onMove,
  onEnd,
}: WindowPointerResizeController<Session>): void {
  useEffect(() => {
    if (!resizing) {
      return;
    }

    function handlePointerMove(event: PointerEvent): void {
      const session = sessionRef.current;
      if (!session) {
        return;
      }
      onMove(event, session);
    }

    function handlePointerEnd(): void {
      const session = sessionRef.current;
      sessionRef.current = null;
      onEnd(session);
    }

    window.addEventListener("pointermove", handlePointerMove);
    window.addEventListener("pointerup", handlePointerEnd);
    window.addEventListener("pointercancel", handlePointerEnd);
    return () => {
      window.removeEventListener("pointermove", handlePointerMove);
      window.removeEventListener("pointerup", handlePointerEnd);
      window.removeEventListener("pointercancel", handlePointerEnd);
    };
  }, [onEnd, onMove, resizing, sessionRef]);
}

export function useAppLayoutState({
  layoutRootRef,
  onCloseProjectMenu
}: {
  layoutRootRef?: RefObject<HTMLElement | null>;
  onCloseProjectMenu: () => void;
}): {
  sidebarWidth: number;
  sidebarCollapsed: boolean;
  resizingSidebar: boolean;
  sidebarAnimating: boolean;
  workspaceRightPanelWidth: number;
  clampedWorkspaceRightPanelWidth: number;
  resizingRightPanel: boolean;
  rightPanelOpen: boolean;
  rightPanelAnimating: boolean;
  effectiveSidebarWidth: number;
  setRightPanelOpenWithMotion: (open: boolean) => void;
  startSidebarResize: (event: ReactPointerEvent<HTMLDivElement>) => void;
  startSettingsSidebarResize: (event: ReactPointerEvent<HTMLDivElement>) => void;
  startRightPanelResize: (event: ReactPointerEvent<HTMLDivElement>) => void;
  handleRightPanelSeparatorKey: (event: ReactKeyboardEvent<HTMLDivElement>) => void;
  resetWorkspaceRightPanelWidth: () => void;
  toggleSidebar: () => void;
  handleSidebarSeparatorKey: (event: ReactKeyboardEvent<HTMLDivElement>) => void;
  handleSettingsSidebarSeparatorKey: (event: ReactKeyboardEvent<HTMLDivElement>) => void;
  resetSettingsSidebarWidth: () => void;
} {
  const [sidebarWidth, setSidebarWidth] = useState(initialSidebarWidth);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(initialSidebarCollapsed);
  const [resizingSidebar, setResizingSidebar] = useState(false);
  const [sidebarAnimating, setSidebarAnimating] = useState(false);
  const [workspaceRightPanelWidth, setWorkspaceRightPanelWidth] = useState(initialWorkspaceRightPanelWidth);
  const [resizingRightPanel, setResizingRightPanel] = useState(false);
  const [rightPanelOpen, setRightPanelOpen] = useState(false);
  const [rightPanelAnimating, setRightPanelAnimating] = useState(false);
  const resizeSessionRef = useRef<SidebarResizeSession | null>(null);
  const rightPanelResizeSessionRef = useRef<RightPanelResizeSession | null>(null);
  const sidebarMotionTimerRef = useRef<number | undefined>(undefined);
  const rightPanelMotionTimerRef = useRef<number | undefined>(undefined);
  const sidebarLiveFrameRef = useRef<number | undefined>(undefined);
  const pendingLiveSidebarWidthRef = useRef<number | undefined>(undefined);
  const rightPanelLiveFrameRef = useRef<number | undefined>(undefined);
  const pendingLiveWorkspaceRightPanelWidthRef = useRef<number | undefined>(undefined);
  const effectiveSidebarWidth = sidebarCollapsed ? 0 : sidebarWidth;
  const clampedWorkspaceRightPanelWidth = clampWorkspaceRightPanelWidth(
    workspaceRightPanelWidth,
    effectiveSidebarWidth
  );

  const startSidebarMotion = useCallback((): void => {
    if (sidebarMotionTimerRef.current !== undefined) {
      window.clearTimeout(sidebarMotionTimerRef.current);
    }
    setSidebarAnimating(true);
    sidebarMotionTimerRef.current = window.setTimeout(() => {
      sidebarMotionTimerRef.current = undefined;
      setSidebarAnimating(false);
    }, SIDEBAR_MOTION_MS);
  }, []);

  const startRightPanelMotion = useCallback((): void => {
    if (rightPanelMotionTimerRef.current !== undefined) {
      window.clearTimeout(rightPanelMotionTimerRef.current);
    }
    setRightPanelAnimating(true);
    rightPanelMotionTimerRef.current = window.setTimeout(() => {
      rightPanelMotionTimerRef.current = undefined;
      setRightPanelAnimating(false);
    }, RIGHT_PANEL_MOTION_MS);
  }, []);

  const applySettingsSidebarWidth = useCallback((nextWidth: number): void => {
    setSidebarWidth(clamp(nextWidth, SIDEBAR_MIN_WIDTH, SIDEBAR_MAX_WIDTH));
  }, []);

  const writeLiveSidebarWidth = useCallback(
    (nextWidth: number): void => {
      const root = layoutRootRef?.current;
      if (!root) {
        return;
      }
      const clampedWidth = clamp(nextWidth, SIDEBAR_MIN_WIDTH, SIDEBAR_MAX_WIDTH);
      root.style.setProperty("--sidebar-width", `${clampedWidth}px`);
      root.style.setProperty("--sidebar-open-width", `${clampedWidth}px`);
    },
    [layoutRootRef]
  );

  const cancelPendingLiveSidebarWidth = useCallback((): void => {
    if (sidebarLiveFrameRef.current !== undefined) {
      window.cancelAnimationFrame(sidebarLiveFrameRef.current);
      sidebarLiveFrameRef.current = undefined;
    }
    pendingLiveSidebarWidthRef.current = undefined;
  }, []);

  const flushPendingLiveSidebarWidth = useCallback((): void => {
    if (sidebarLiveFrameRef.current !== undefined) {
      window.cancelAnimationFrame(sidebarLiveFrameRef.current);
      sidebarLiveFrameRef.current = undefined;
    }
    const nextWidth = pendingLiveSidebarWidthRef.current;
    pendingLiveSidebarWidthRef.current = undefined;
    if (nextWidth !== undefined) {
      writeLiveSidebarWidth(nextWidth);
    }
  }, [writeLiveSidebarWidth]);

  const applyLiveSidebarWidth = useCallback(
    (nextWidth: number): void => {
      pendingLiveSidebarWidthRef.current = nextWidth;
      if (sidebarLiveFrameRef.current !== undefined) {
        return;
      }
      sidebarLiveFrameRef.current = window.requestAnimationFrame(() => {
        sidebarLiveFrameRef.current = undefined;
        const pendingWidth = pendingLiveSidebarWidthRef.current;
        pendingLiveSidebarWidthRef.current = undefined;
        if (pendingWidth !== undefined) {
          writeLiveSidebarWidth(pendingWidth);
        }
      });
    },
    [writeLiveSidebarWidth]
  );

  const writeLiveWorkspaceRightPanelWidth = useCallback(
    (nextWidth: number): void => {
      const root = layoutRootRef?.current;
      if (!root) {
        return;
      }
      const clampedWidth = clampWorkspaceRightPanelWidth(
        nextWidth,
        effectiveSidebarWidth
      );
      root.style.setProperty("--workspace-right-panel-width", `${clampedWidth}px`);
    },
    [effectiveSidebarWidth, layoutRootRef]
  );

  const cancelPendingLiveWorkspaceRightPanelWidth = useCallback((): void => {
    if (rightPanelLiveFrameRef.current !== undefined) {
      window.cancelAnimationFrame(rightPanelLiveFrameRef.current);
      rightPanelLiveFrameRef.current = undefined;
    }
    pendingLiveWorkspaceRightPanelWidthRef.current = undefined;
  }, []);

  const flushPendingLiveWorkspaceRightPanelWidth = useCallback((): void => {
    if (rightPanelLiveFrameRef.current !== undefined) {
      window.cancelAnimationFrame(rightPanelLiveFrameRef.current);
      rightPanelLiveFrameRef.current = undefined;
    }
    const nextWidth = pendingLiveWorkspaceRightPanelWidthRef.current;
    pendingLiveWorkspaceRightPanelWidthRef.current = undefined;
    if (nextWidth !== undefined) {
      writeLiveWorkspaceRightPanelWidth(nextWidth);
    }
  }, [writeLiveWorkspaceRightPanelWidth]);

  const applyLiveWorkspaceRightPanelWidth = useCallback(
    (nextWidth: number): void => {
      pendingLiveWorkspaceRightPanelWidthRef.current = nextWidth;
      if (rightPanelLiveFrameRef.current !== undefined) {
        return;
      }
      rightPanelLiveFrameRef.current = window.requestAnimationFrame(() => {
        rightPanelLiveFrameRef.current = undefined;
        const pendingWidth = pendingLiveWorkspaceRightPanelWidthRef.current;
        pendingLiveWorkspaceRightPanelWidthRef.current = undefined;
        if (pendingWidth !== undefined) {
          writeLiveWorkspaceRightPanelWidth(pendingWidth);
        }
      });
    },
    [writeLiveWorkspaceRightPanelWidth]
  );

  const applySidebarWidth = useCallback(
    (nextWidth: number): void => {
      if (nextWidth <= SIDEBAR_MIN_WIDTH) {
        if (!sidebarCollapsed && !resizingSidebar) {
          startSidebarMotion();
        }
        onCloseProjectMenu();
        setSidebarCollapsed(true);
        return;
      }
      if (sidebarCollapsed && !resizingSidebar) {
        startSidebarMotion();
      }
      setSidebarCollapsed(false);
      setSidebarWidth(clamp(nextWidth, SIDEBAR_MIN_WIDTH, SIDEBAR_MAX_WIDTH));
    },
    [onCloseProjectMenu, resizingSidebar, sidebarCollapsed, startSidebarMotion]
  );

  const applyWorkspaceRightPanelWidth = useCallback(
    (nextWidth: number): void => {
      setWorkspaceRightPanelWidth(clampWorkspaceRightPanelWidth(nextWidth, effectiveSidebarWidth));
    },
    [effectiveSidebarWidth]
  );

  const setRightPanelOpenWithMotion = useCallback(
    (open: boolean): void => {
      if (rightPanelOpen !== open) {
        startRightPanelMotion();
      }
      setRightPanelOpen(open);
    },
    [rightPanelOpen, startRightPanelMotion]
  );

  useEffect(() => {
    window.localStorage.setItem(SIDEBAR_WIDTH_KEY, String(sidebarWidth));
    window.localStorage.setItem(SIDEBAR_COLLAPSED_KEY, String(sidebarCollapsed));
  }, [sidebarWidth, sidebarCollapsed]);

  useEffect(() => {
    window.localStorage.setItem(WORKSPACE_RIGHT_PANEL_WIDTH_KEY, String(workspaceRightPanelWidth));
  }, [workspaceRightPanelWidth]);

  useEffect(() => {
    return () => {
      if (sidebarMotionTimerRef.current !== undefined) {
        window.clearTimeout(sidebarMotionTimerRef.current);
      }
      if (rightPanelMotionTimerRef.current !== undefined) {
        window.clearTimeout(rightPanelMotionTimerRef.current);
      }
      cancelPendingLiveSidebarWidth();
      cancelPendingLiveWorkspaceRightPanelWidth();
    };
  }, [cancelPendingLiveSidebarWidth, cancelPendingLiveWorkspaceRightPanelWidth]);

  const handleSidebarResizeMove = useCallback(
    (event: PointerEvent, session: SidebarResizeSession): void => {
      const nextWidth = session.startWidth + event.clientX - session.startX;
      session.currentWidth = nextWidth;
      if (session.allowCollapse) {
        if (nextWidth <= SIDEBAR_MIN_WIDTH) {
          cancelPendingLiveSidebarWidth();
          applySidebarWidth(nextWidth);
          session.collapsedDuringDrag = true;
          return;
        }
        if (session.collapsedDuringDrag) {
          // We crossed below SIDEBAR_MIN_WIDTH earlier in this drag and never
          // released. Route through applySidebarWidth so setSidebarCollapsed
          // (false) clears the .sidebar-collapsed class; otherwise the live
          // inline --sidebar-width would expand the sidebar while its content
          // stayed opacity: 0, leaving a white glass slab.
          applySidebarWidth(nextWidth);
          session.collapsedDuringDrag = false;
          return;
        }
        applyLiveSidebarWidth(nextWidth);
        return;
      }
      applySettingsSidebarWidth(nextWidth);
    },
    [
      applyLiveSidebarWidth,
      applySettingsSidebarWidth,
      applySidebarWidth,
      cancelPendingLiveSidebarWidth,
    ]
  );

  const handleSidebarResizeEnd = useCallback(
    (session: SidebarResizeSession | null): void => {
      if (session?.allowCollapse) {
        if (session.currentWidth > SIDEBAR_MIN_WIDTH) {
          flushPendingLiveSidebarWidth();
        } else {
          cancelPendingLiveSidebarWidth();
        }
        applySidebarWidth(session.currentWidth);
      }
      setResizingSidebar(false);
    },
    [applySidebarWidth, cancelPendingLiveSidebarWidth, flushPendingLiveSidebarWidth]
  );

  useWindowPointerResize({
    resizing: resizingSidebar,
    sessionRef: resizeSessionRef,
    onMove: handleSidebarResizeMove,
    onEnd: handleSidebarResizeEnd,
  });

  const handleRightPanelResizeMove = useCallback(
    (event: PointerEvent, session: RightPanelResizeSession): void => {
      const nextWidth = session.startWidth - (event.clientX - session.startX);
      session.currentWidth = nextWidth;
      applyLiveWorkspaceRightPanelWidth(nextWidth);
    },
    [applyLiveWorkspaceRightPanelWidth]
  );

  const handleRightPanelResizeEnd = useCallback((session: RightPanelResizeSession | null): void => {
    if (session) {
      flushPendingLiveWorkspaceRightPanelWidth();
      applyWorkspaceRightPanelWidth(session.currentWidth);
    } else {
      cancelPendingLiveWorkspaceRightPanelWidth();
    }
    setResizingRightPanel(false);
  }, [
    applyWorkspaceRightPanelWidth,
    cancelPendingLiveWorkspaceRightPanelWidth,
    flushPendingLiveWorkspaceRightPanelWidth,
  ]);

  useWindowPointerResize({
    resizing: resizingRightPanel,
    sessionRef: rightPanelResizeSessionRef,
    onMove: handleRightPanelResizeMove,
    onEnd: handleRightPanelResizeEnd,
  });

  useEffect(() => {
    function handleResize(): void {
      setWorkspaceRightPanelWidth((current) =>
        clampWorkspaceRightPanelWidth(current, effectiveSidebarWidth)
      );
    }

    window.addEventListener("resize", handleResize);
    return () => window.removeEventListener("resize", handleResize);
  }, [effectiveSidebarWidth]);

  function startSidebarResize(event: ReactPointerEvent<HTMLDivElement>): void {
    if (event.button !== 0) {
      return;
    }
    event.preventDefault();
    resizeSessionRef.current = {
      startX: event.clientX,
      startWidth: sidebarCollapsed ? 0 : sidebarWidth,
      currentWidth: sidebarCollapsed ? 0 : sidebarWidth,
      allowCollapse: true,
      collapsedDuringDrag: sidebarCollapsed
    };
    onCloseProjectMenu();
    setResizingSidebar(true);
  }

  function startSettingsSidebarResize(event: ReactPointerEvent<HTMLDivElement>): void {
    if (event.button !== 0) {
      return;
    }
    event.preventDefault();
    resizeSessionRef.current = {
      startX: event.clientX,
      startWidth: sidebarWidth,
      currentWidth: sidebarWidth,
      allowCollapse: false,
      // Settings sidebar resizer never collapses, so this flag is never read
      // — handlePointerMove short-circuits on !allowCollapse. Set to false
      // for type clarity.
      collapsedDuringDrag: false
    };
    onCloseProjectMenu();
    setResizingSidebar(true);
  }

  function startRightPanelResize(event: ReactPointerEvent<HTMLDivElement>): void {
    if (event.button !== 0 || !rightPanelOpen) {
      return;
    }
    event.preventDefault();
    rightPanelResizeSessionRef.current = {
      startX: event.clientX,
      startWidth: clampedWorkspaceRightPanelWidth,
      currentWidth: clampedWorkspaceRightPanelWidth
    };
    setResizingRightPanel(true);
  }

  function handleRightPanelSeparatorKey(event: ReactKeyboardEvent<HTMLDivElement>): void {
    if (event.key === "ArrowLeft") {
      event.preventDefault();
      applyWorkspaceRightPanelWidth(workspaceRightPanelWidth + WORKSPACE_RIGHT_PANEL_STEP);
    } else if (event.key === "ArrowRight") {
      event.preventDefault();
      applyWorkspaceRightPanelWidth(workspaceRightPanelWidth - WORKSPACE_RIGHT_PANEL_STEP);
    } else if (event.key === "Home") {
      event.preventDefault();
      applyWorkspaceRightPanelWidth(WORKSPACE_RIGHT_PANEL_MAX_WIDTH);
    } else if (event.key === "End") {
      event.preventDefault();
      applyWorkspaceRightPanelWidth(WORKSPACE_RIGHT_PANEL_MIN_WIDTH);
    }
  }

  function resetWorkspaceRightPanelWidth(): void {
    applyWorkspaceRightPanelWidth(WORKSPACE_RIGHT_PANEL_DEFAULT_WIDTH);
  }

  function toggleSidebar(): void {
    onCloseProjectMenu();
    startSidebarMotion();
    setSidebarCollapsed((collapsed) => !collapsed);
    setSidebarWidth((width) => (width <= SIDEBAR_MIN_WIDTH ? SIDEBAR_DEFAULT_WIDTH : width));
  }

  function handleSidebarSeparatorKey(event: ReactKeyboardEvent<HTMLDivElement>): void {
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      toggleSidebar();
      return;
    }
    if (event.key === "ArrowLeft") {
      event.preventDefault();
      if (sidebarCollapsed) {
        return;
      }
      applySidebarWidth(sidebarWidth - SIDEBAR_STEP);
      return;
    }
    if (event.key === "ArrowRight") {
      event.preventDefault();
      if (sidebarCollapsed) {
        startSidebarMotion();
        setSidebarCollapsed(false);
        setSidebarWidth(SIDEBAR_DEFAULT_WIDTH);
        return;
      }
      applySidebarWidth(sidebarWidth + SIDEBAR_STEP);
    }
  }

  function handleSettingsSidebarSeparatorKey(event: ReactKeyboardEvent<HTMLDivElement>): void {
    if (event.key === "ArrowLeft") {
      event.preventDefault();
      applySettingsSidebarWidth(sidebarWidth - SIDEBAR_STEP);
      return;
    }
    if (event.key === "ArrowRight") {
      event.preventDefault();
      applySettingsSidebarWidth(sidebarWidth + SIDEBAR_STEP);
      return;
    }
    if (event.key === "Home") {
      event.preventDefault();
      applySettingsSidebarWidth(SIDEBAR_MIN_WIDTH);
      return;
    }
    if (event.key === "End") {
      event.preventDefault();
      applySettingsSidebarWidth(SIDEBAR_MAX_WIDTH);
    }
  }

  function resetSettingsSidebarWidth(): void {
    applySettingsSidebarWidth(SIDEBAR_DEFAULT_WIDTH);
  }

  return {
    sidebarWidth,
    sidebarCollapsed,
    resizingSidebar,
    sidebarAnimating,
    workspaceRightPanelWidth,
    clampedWorkspaceRightPanelWidth,
    resizingRightPanel,
    rightPanelOpen,
    rightPanelAnimating,
    effectiveSidebarWidth,
    setRightPanelOpenWithMotion,
    startSidebarResize,
    startSettingsSidebarResize,
    startRightPanelResize,
    handleRightPanelSeparatorKey,
    resetWorkspaceRightPanelWidth,
    toggleSidebar,
    handleSidebarSeparatorKey,
    handleSettingsSidebarSeparatorKey,
    resetSettingsSidebarWidth
  };
}
