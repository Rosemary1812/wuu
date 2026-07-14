import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type PointerEvent as ReactPointerEvent
} from "react";
import type {
  RuntimeContext,
  SideThreadEvent,
  SideThreadHistoryResult,
  SideThreadOpenResult,
  SideThreadSendParams,
  SideThreadSendResult
} from "../shared/protocol";
import {
  SIDE_THREAD_DEFAULT_WIDTH,
  clampSideThreadWidth,
  createInitialSideThreadStore,
  ensureSideThreadEntry,
  reduceSideThreadStore,
  type SideThreadAction,
  type SideThreadStoreState
} from "./SideThreadState";

export type SideThreadController = {
  entry: SideThreadStoreState["byThread"][string] | undefined;
  width: number;
  open: () => void;
  close: () => void;
  toggle: () => void;
  setDraft: (draft: string) => void;
  sendMessage: (prompt: string) => void;
  interrupt: () => void;
  startResize: (event: ReactPointerEvent<HTMLButtonElement>) => void;
  sendDisabledReason?: string;
};

export type SideThreadControllerOptions = {
  activeThreadId: string | undefined;
  activeContext?: RuntimeContext;
  ipc?: SideThreadIPC;
  disabled?: boolean;
  disabledReason?: string;
};

export type SideThreadIPC = {
  openSideThread: (mainThreadId: string) => Promise<SideThreadOpenResult>;
  getSideThreadHistory: (
    mainThreadId: string
  ) => Promise<SideThreadHistoryResult | null>;
  sendSideThreadMessage: (
    params: SideThreadSendParams
  ) => Promise<SideThreadSendResult>;
  interruptSideThread: (mainThreadId: string) => Promise<{ ok: boolean }>;
  onSideThreadEvent: (handler: (event: SideThreadEvent) => void) => () => void;
};

function defaultIPC(): SideThreadIPC | undefined {
  if (typeof window === "undefined") {
    return undefined;
  }
  const candidate = (window as unknown as { wuu?: Partial<SideThreadIPC> }).wuu;
  if (
    typeof candidate?.openSideThread !== "function" ||
    typeof candidate.getSideThreadHistory !== "function" ||
    typeof candidate.sendSideThreadMessage !== "function" ||
    typeof candidate.interruptSideThread !== "function" ||
    typeof candidate.onSideThreadEvent !== "function"
  ) {
    return undefined;
  }
  return candidate as SideThreadIPC;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

export function useSideThreadController(
  options: SideThreadControllerOptions
): SideThreadController {
  const { activeThreadId, activeContext, ipc, disabled, disabledReason } = options;
  const ipcImpl = ipc ?? defaultIPC();
  const effectiveDisabled = Boolean(disabled || !ipcImpl);
  const effectiveReason = !ipcImpl
    ? "当前版本不支持侧聊"
    : disabled
      ? disabledReason ?? "侧聊暂不可用"
      : undefined;

  const [store, setStore] = useState<SideThreadStoreState>(() =>
    createInitialSideThreadStore(SIDE_THREAD_DEFAULT_WIDTH)
  );
  const storeRef = useRef(store);
  const openGenerationRef = useRef(new Map<string, number>());
  const pendingSendTasksRef = useRef(new Map<string, Promise<void>>());
  const resizeCleanupRef = useRef<(() => void) | undefined>(undefined);
  storeRef.current = store;

  const dispatch = useCallback((action: SideThreadAction) => {
    setStore((previous) => reduceSideThreadStore(previous, action));
  }, []);

  useEffect(() => {
    if (!ipcImpl) {
      return;
    }
    return ipcImpl.onSideThreadEvent((event) => {
      setStore((previous) =>
        reduceSideThreadStore(previous, { type: "applyEvent", event })
      );
    });
  }, [ipcImpl]);

  useEffect(() => {
    if (!activeThreadId) {
      return;
    }
    setStore((previous) =>
      ensureSideThreadEntry(previous, activeThreadId).store
    );
  }, [activeThreadId]);

  useEffect(() => {
    return () => resizeCleanupRef.current?.();
  }, []);

  const open = useCallback(() => {
    if (!activeThreadId) {
      return;
    }
    dispatch({ type: "open", mainThreadId: activeThreadId });
    dispatch({ type: "setError", mainThreadId: activeThreadId, error: undefined });
    if (!ipcImpl || disabled) {
      return;
    }

    const generation = (openGenerationRef.current.get(activeThreadId) ?? 0) + 1;
    openGenerationRef.current.set(activeThreadId, generation);
    const isCurrentRequest = () =>
      openGenerationRef.current.get(activeThreadId) === generation;

    void (async () => {
      try {
        const opened = await ipcImpl.openSideThread(activeThreadId);
        const openedSummary = opened.summary;
        if (!isCurrentRequest() || !openedSummary) {
          return;
        }
        setStore((previous) =>
          reduceSideThreadStore(previous, {
            type: "mergeSummary",
            mainThreadId: activeThreadId,
            summary: openedSummary
          })
        );

        const history = await ipcImpl.getSideThreadHistory(activeThreadId);
        if (!isCurrentRequest() || !history) {
          return;
        }
        await pendingSendTasksRef.current.get(activeThreadId);
        if (!isCurrentRequest()) {
          return;
        }
        setStore((previous) =>
          reduceSideThreadStore(previous, {
            type: "mergeHistory",
            mainThreadId: activeThreadId,
            summary: history.summary,
            messages: history.messages
          })
        );
      } catch (error) {
        if (isCurrentRequest()) {
          dispatch({
            type: "setError",
            mainThreadId: activeThreadId,
            error: errorMessage(error)
          });
        }
      }
    })();
  }, [activeThreadId, disabled, dispatch, ipcImpl]);

  const close = useCallback(() => {
    if (activeThreadId) {
      dispatch({ type: "close", mainThreadId: activeThreadId });
    }
  }, [activeThreadId, dispatch]);

  const toggle = useCallback(() => {
    if (!activeThreadId) {
      return;
    }
    if (storeRef.current.byThread[activeThreadId]?.open) {
      close();
    } else {
      open();
    }
  }, [activeThreadId, close, open]);

  const setDraft = useCallback(
    (draft: string) => {
      if (activeThreadId) {
        dispatch({ type: "setDraft", mainThreadId: activeThreadId, draft });
      }
    },
    [activeThreadId, dispatch]
  );

  const sendMessage = useCallback(
    (prompt: string) => {
      if (!activeThreadId || !activeContext || !ipcImpl || effectiveDisabled) {
        return;
      }
      const trimmed = prompt.trim();
      const currentEntry = storeRef.current.byThread[activeThreadId];
      if (
        !trimmed ||
        currentEntry?.streaming ||
        pendingSendTasksRef.current.has(activeThreadId)
      ) {
        return;
      }

      const optimisticId = `local-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
      const optimisticMessage = {
        id: optimisticId,
        side_thread_id: currentEntry?.summary?.side_thread_id ?? "",
        role: "user" as const,
        text: trimmed,
        created_at: new Date().toISOString()
      };
      setStore((previous) => {
        let next = reduceSideThreadStore(previous, {
          type: "appendMessage",
          mainThreadId: activeThreadId,
          message: optimisticMessage
        });
        next = reduceSideThreadStore(next, {
          type: "setDraft",
          mainThreadId: activeThreadId,
          draft: ""
        });
        next = reduceSideThreadStore(next, {
          type: "setStreaming",
          mainThreadId: activeThreadId,
          streaming: true
        });
        return reduceSideThreadStore(next, {
          type: "setError",
          mainThreadId: activeThreadId,
          error: undefined
        });
      });

      const task = (async () => {
        try {
          const result = await ipcImpl.sendSideThreadMessage({
            main_thread_id: activeThreadId,
            prompt: trimmed
          });
          setStore((previous) => {
            let next = reduceSideThreadStore(previous, {
              type: "mergeSummary",
              mainThreadId: activeThreadId,
              summary: result.summary
            });
            next = reduceSideThreadStore(next, {
              type: "removeMessage",
              mainThreadId: activeThreadId,
              messageId: result.user_message_id
            });
            return reduceSideThreadStore(next, {
              type: "updateMessage",
              mainThreadId: activeThreadId,
              messageId: optimisticId,
              patch: {
                id: result.user_message_id,
                side_thread_id: result.summary.side_thread_id
              }
            });
          });
        } catch (error) {
          setStore((previous) => {
            let next = reduceSideThreadStore(previous, {
              type: "removeMessage",
              mainThreadId: activeThreadId,
              messageId: optimisticId
            });
            next = reduceSideThreadStore(next, {
              type: "setStreaming",
              mainThreadId: activeThreadId,
              streaming: false
            });
            next = reduceSideThreadStore(next, {
              type: "setError",
              mainThreadId: activeThreadId,
              error: errorMessage(error)
            });
            if (!next.byThread[activeThreadId]?.draft) {
              next = reduceSideThreadStore(next, {
                type: "setDraft",
                mainThreadId: activeThreadId,
                draft: trimmed
              });
            }
            return next;
          });
        }
      })();
      pendingSendTasksRef.current.set(activeThreadId, task);
      void task.finally(() => {
        if (pendingSendTasksRef.current.get(activeThreadId) === task) {
          pendingSendTasksRef.current.delete(activeThreadId);
        }
      });
    },
    [activeContext, activeThreadId, effectiveDisabled, ipcImpl]
  );

  const interrupt = useCallback(() => {
    if (!activeThreadId || !ipcImpl) {
      return;
    }
    void ipcImpl.interruptSideThread(activeThreadId).catch((error: unknown) => {
      dispatch({
        type: "setError",
        mainThreadId: activeThreadId,
        error: errorMessage(error)
      });
    });
  }, [activeThreadId, dispatch, ipcImpl]);

  const startResize = useCallback(
    (event: ReactPointerEvent<HTMLButtonElement>) => {
      if (event.button !== 0) {
        return;
      }
      event.preventDefault();
      resizeCleanupRef.current?.();

      const startX = event.clientX;
      const startWidth = storeRef.current.width;
      const pointerId = event.pointerId;
      const target = event.currentTarget;
      const root = document.documentElement;
      target.setPointerCapture?.(pointerId);
      root.classList.add("resizing-side-thread");

      const handleMove = (moveEvent: PointerEvent) => {
        dispatch({
          type: "setWidth",
          width: clampSideThreadWidth(startWidth + startX - moveEvent.clientX)
        });
      };
      const cleanup = () => {
        window.removeEventListener("pointermove", handleMove);
        window.removeEventListener("pointerup", cleanup);
        window.removeEventListener("pointercancel", cleanup);
        root.classList.remove("resizing-side-thread");
        if (target.hasPointerCapture?.(pointerId)) {
          target.releasePointerCapture?.(pointerId);
        }
        if (resizeCleanupRef.current === cleanup) {
          resizeCleanupRef.current = undefined;
        }
      };
      resizeCleanupRef.current = cleanup;
      window.addEventListener("pointermove", handleMove);
      window.addEventListener("pointerup", cleanup);
      window.addEventListener("pointercancel", cleanup);
    },
    [dispatch]
  );

  const entry = useMemo(
    () => (activeThreadId ? store.byThread[activeThreadId] : undefined),
    [activeThreadId, store]
  );

  return {
    entry,
    width: store.width,
    open,
    close,
    toggle,
    setDraft,
    sendMessage,
    interrupt,
    startResize,
    sendDisabledReason:
      effectiveDisabled || !activeContext
        ? effectiveReason ?? "请先选择工作区"
        : undefined
  };
}
