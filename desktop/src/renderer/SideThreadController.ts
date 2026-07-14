// SideThreadController — 把侧聊所需的状态、IPC 订阅、交互逻辑封装在一个
// 自定义 hook 里。设计依据：docs/side-thread.md。
//
// 为什么单独成模块而不是直接进 App.tsx？
// - App.tsx 已经 160K 行，再添加 side thread 状态机会让它更失控；
// - 侧聊行为天然独立（与主对话解耦），独立 hook 便于单独单测；
// - 主进程 IPC 接入顺序与渲染端 UI 解耦：先交付 UI 骨架，后端接
//   通前 sendDisabledReason 由 controller 自己打开，防止 UI 假装
//   可用但消息丢了。
//
// V1 范围：单 main thread ↔ 单 side thread，懒初始化，事件回放走
// onSideThreadEvent 订阅；不写主对话历史。

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type {
  SideThreadEvent,
  SideThreadMessage,
  SideThreadSendParams,
  SideThreadSendResult,
  SideThreadSummary,
  RuntimeContext
} from "../shared/protocol";
import {
  SIDE_THREAD_DEFAULT_WIDTH,
  clampSideThreadWidth,
  createInitialSideThreadStore,
  reduceSideThreadStore,
  type SideThreadAction,
  type SideThreadStoreState
} from "./SideThreadState";

// 当主进程 / 后端尚未接好时，让 UI 自己禁用发送、并在错误条上提示，
// 而不是静默丢消息。host/agent 接入后只需把 disableReason 设为 undefined。
export type SideThreadController = {
  store: SideThreadStoreState;
  // 当前主 thread 的视图状态。open=false 时面板完全收起。
  entry: ReturnType<typeof ensureEntry>;
  width: number;
  open: () => void;
  close: () => void;
  toggle: () => void;
  setDraft: (draft: string) => void;
  sendMessage: (prompt: string) => void;
  interrupt: () => void;
  // 拖拽宽度：调用方把 pointerdown 事件传进来，hook 内部管理后续
  // pointermove / pointerup。
  startResize: (event: React.PointerEvent<HTMLButtonElement>) => void;
  // 真正的 IPC 还没接通时，给侧聊面板一个"先别发"的提示语。
  sendDisabledReason?: string;
};

function ensureEntry(store: SideThreadStoreState, mainThreadId: string | undefined) {
  if (!mainThreadId) {
    return undefined;
  }
  return store.byThread[mainThreadId];
}

export type SideThreadControllerOptions = {
  activeThreadId: string | undefined;
  activeContext?: RuntimeContext;
  // 注入 IPC，便于测试或 host 不接 wuu 桥接的场景。
  ipc?: SideThreadIPC;
  // 主进程尚未接好时设为 true：所有 send / open 都被禁用，提示语
  // 由 host 控制。
  disabled?: boolean;
  disabledReason?: string;
};

export type SideThreadIPC = {
  openSideThread: (mainThreadId: string) => Promise<{ summary: SideThreadSummary | null }>;
  getSideThreadHistory: (
    mainThreadId: string
  ) => Promise<{ summary: SideThreadSummary; messages: unknown[] } | null>;
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
  const wuu = (window as unknown as { wuu?: SideThreadIPC }).wuu;
  if (!wuu) {
    return undefined;
  }
  // 只在主进程桥接的 5 个方法都存在时才采用；缺一就视为未接通。
  const candidate = wuu as SideThreadIPC;
  if (
    typeof candidate.openSideThread !== "function" ||
    typeof candidate.getSideThreadHistory !== "function" ||
    typeof candidate.sendSideThreadMessage !== "function" ||
    typeof candidate.interruptSideThread !== "function" ||
    typeof candidate.onSideThreadEvent !== "function"
  ) {
    return undefined;
  }
  return candidate;
}

export function useSideThreadController(
  options: SideThreadControllerOptions
): SideThreadController {
  const { activeThreadId, activeContext, ipc, disabled, disabledReason } = options;
  const ipcImpl = ipc ?? defaultIPC();
  const effectiveDisabled = disabled || !ipcImpl;
  const effectiveReason = !ipcImpl
    ? "后端侧聊能力尚未启用"
    : disabled
      ? disabledReason
      : undefined;

  const [store, setStore] = useState<SideThreadStoreState>(() =>
    createInitialSideThreadStore(SIDE_THREAD_DEFAULT_WIDTH)
  );
  // 总是从 store 取最新值；ref 只用于逃逸回调里访问最新 store。
  const storeRef = useRef(store);
  storeRef.current = store;

  const dispatch = useCallback((action: SideThreadAction) => {
    setStore((prev) => reduceSideThreadStore(prev, action));
  }, []);

  // ============================================================================
  // IPC 订阅：路由后端事件到正确的 main thread 面板
  // ============================================================================

  useEffect(() => {
    if (!ipcImpl) {
      return;
    }
    const dispose = ipcImpl.onSideThreadEvent((event) => {
      setStore((prev) => reduceSideThreadStore(prev, { type: "applyEvent", event }));
    });
    return dispose;
  }, [ipcImpl]);

  // 切换主 thread 时，如果新 thread 还没有 side thread 记录，不要
  // 立刻调用后端 —— 设计 §5 说"打开侧聊本身不必立即创建持久化
  // 对话"。所以这里只保证条目存在，不触发 IPC。
  useEffect(() => {
    if (!activeThreadId) {
      return;
    }
    setStore((prev) =>
      prev.byThread[activeThreadId] ? prev : {
        ...prev,
        byThread: {
          ...prev.byThread,
          [activeThreadId]: {
            open: false,
            summary: null,
            messages: [],
            draft: "",
            streaming: false
          }
        }
      }
    );
  }, [activeThreadId]);

  // ============================================================================
  // 用户操作
  // ============================================================================

  const open = useCallback(() => {
    if (!activeThreadId) {
      return;
    }
    dispatch({ type: "open", mainThreadId: activeThreadId });
    if (ipcImpl) {
      // 异步拉一次后端历史 / summary，但失败也不致命（懒加载）。
      void ipcImpl
        .getSideThreadHistory(activeThreadId)
        .then((result) => {
          if (!result) {
            return;
          }
          // result.messages 由 IPC 协议保证是 SideThreadMessage[]；
          // 这里走一次窄化，避免 unknown[] 污染 reducer 签名。
          const messages = (result.messages ?? []) as SideThreadMessage[];
          setStore((prev) => {
            const next = reduceSideThreadStore(prev, {
              type: "setSummary",
              mainThreadId: result.summary.main_thread_id,
              summary: result.summary
            });
            return messages.length === 0
              ? next
              : reduceSideThreadStore(next, {
                  type: "setHistory",
                  mainThreadId: result.summary.main_thread_id,
                  messages
                });
          });
        })
        .catch((error: unknown) => {
          setStore((prev) =>
            reduceSideThreadStore(prev, {
              type: "setError",
              mainThreadId: activeThreadId,
              error: error instanceof Error ? error.message : String(error)
            })
          );
        });
    }
  }, [activeThreadId, dispatch, ipcImpl]);

  const close = useCallback(() => {
    if (!activeThreadId) {
      return;
    }
    dispatch({ type: "close", mainThreadId: activeThreadId });
  }, [activeThreadId, dispatch]);

  const toggle = useCallback(() => {
    if (!activeThreadId) {
      return;
    }
    const current = storeRef.current.byThread[activeThreadId]?.open ?? false;
    if (current) {
      close();
    } else {
      open();
    }
  }, [activeThreadId, close, dispatch, open]);

  const setDraft = useCallback(
    (draft: string) => {
      if (!activeThreadId) {
        return;
      }
      dispatch({ type: "setDraft", mainThreadId: activeThreadId, draft });
    },
    [activeThreadId, dispatch]
  );

  const sendMessage = useCallback(
    async (prompt: string) => {
      if (!activeThreadId || !ipcImpl) {
        return;
      }
      const trimmed = prompt.trim();
      if (!trimmed) {
        return;
      }
      // 先乐观写入一个 user message，保持 UI 与 IPC 同步期间不闪烁；
      // 真正的 user_message_id 由 sendSideThreadMessage 返回后回填。
      const optimisticId = `local-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
      const optimisticMessage = {
        id: optimisticId,
        side_thread_id: storeRef.current.byThread[activeThreadId]?.summary?.side_thread_id ?? "pending",
        role: "user" as const,
        text: trimmed,
        created_at: new Date().toISOString()
      };
      dispatch({
        type: "appendMessage",
        mainThreadId: activeThreadId,
        message: optimisticMessage
      });
      // 清空草稿。
      dispatch({ type: "setDraft", mainThreadId: activeThreadId, draft: "" });
      dispatch({ type: "setStreaming", mainThreadId: activeThreadId, streaming: true });
      try {
        const result = await ipcImpl.sendSideThreadMessage({
          main_thread_id: activeThreadId,
          prompt: trimmed
        });
        dispatch({
          type: "setSummary",
          mainThreadId: activeThreadId,
          summary: result.summary
        });
        // 用真正的 id 重写 optimistic 消息（仅当存在时）。
        dispatch({
          type: "updateMessage",
          mainThreadId: activeThreadId,
          messageId: optimisticId,
          patch: { id: result.user_message_id, side_thread_id: result.summary.side_thread_id }
        });
      } catch (error: unknown) {
        dispatch({ type: "setStreaming", mainThreadId: activeThreadId, streaming: false });
        dispatch({
          type: "setError",
          mainThreadId: activeThreadId,
          error: error instanceof Error ? error.message : String(error)
        });
      }
    },
    [activeThreadId, dispatch, ipcImpl]
  );

  const interrupt = useCallback(async () => {
    if (!activeThreadId || !ipcImpl) {
      return;
    }
    try {
      await ipcImpl.interruptSideThread(activeThreadId);
    } catch (error: unknown) {
      dispatch({
        type: "setError",
        mainThreadId: activeThreadId,
        error: error instanceof Error ? error.message : String(error)
      });
    }
  }, [activeThreadId, dispatch, ipcImpl]);

  // ============================================================================
  // 拖拽宽度
  // ============================================================================

  const startResize = useCallback(
    (event: React.PointerEvent<HTMLButtonElement>) => {
      if (event.button !== 0) {
        return;
      }
      event.preventDefault();
      const startX = event.clientX;
      const startWidth = storeRef.current.width;
      const target = event.currentTarget;
      target.setPointerCapture?.(event.pointerId);

      const handleMove = (moveEvent: PointerEvent) => {
        // 用户往左拖 = 面板变宽（panel 在主对话右侧）。
        const delta = startX - moveEvent.clientX;
        const next = clampSideThreadWidth(startWidth + delta);
        setStore((prev) => ({ ...prev, width: next }));
      };
      const handleUp = () => {
        window.removeEventListener("pointermove", handleMove);
        window.removeEventListener("pointerup", handleUp);
        window.removeEventListener("pointercancel", handleUp);
      };
      window.addEventListener("pointermove", handleMove);
      window.addEventListener("pointerup", handleUp);
      window.addEventListener("pointercancel", handleUp);
    },
    []
  );

  const entry = useMemo(
    () => ensureEntry(store, activeThreadId),
    [store, activeThreadId]
  );

  return {
    store,
    entry,
    width: store.width,
    open,
    close,
    toggle,
    setDraft,
    sendMessage,
    interrupt,
    startResize,
    // 仅当 activeContext 缺失时禁用；与 /side 斜杠命令本身的
    // needsWorkspace 闸门保持一致。
    sendDisabledReason:
      effectiveDisabled || !activeContext ? effectiveReason ?? "请先选择工作区" : undefined
  };
}
