// 侧聊（SideThread）面板 — 依附主对话的第二列 UI。
//
// 视觉规范：
// - 主对话永远是视觉主体，侧聊更窄、密度更高；
// - 不抢主对话的火苗签名时刻，颜色、间距、字号都收敛到次级档位；
// - 用发丝线与主对话分隔，不加阴影 / 不加圆角大块面；
// - 第一版只展示必要信息：标题 / 主任务状态 / 收起按钮。
//
// 与 ChatThreadView 的差别：这里不复用主对话完整渲染管线（参与
// 者/工具/计划层都隐藏），只渲染纯文本消息 + 流式增量 + 错误。
// SideThread 是独立的 agent 线程，不能让主对话的 UI 元件误以为它
// 属于主线程。

import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useRef,
  useState,
  type ChangeEvent,
  type KeyboardEvent as ReactKeyboardEvent,
  type PointerEvent as ReactPointerEvent
} from "react";
import { Loader2, PanelRightClose, Square } from "lucide-react";
import type {
  SideThreadMessage,
  SideThreadSummary
} from "../shared/protocol";
import type { SideThreadEntryState } from "./SideThreadState";

// 第一版空状态快捷问题。设计 §6 要求按普通侧聊消息发送，不形成
// 单独的功能模式——这里只是预填 composer，然后让用户再点一次发送。
const EMPTY_QUICK_PROMPTS = [
  { label: "现在做到哪了？", prompt: "现在做到哪了？" },
  { label: "解释刚才的错误", prompt: "解释刚才出现的错误或工具结果" },
  { label: "当前方案有什么风险？", prompt: "当前方案可能存在哪些风险或后续影响？" }
];

function summaryMainTaskLabel(summary: SideThreadSummary | null): string {
  const running = summary?.main_task_summary?.running;
  if (running) {
    return "主任务执行中";
  }
  if (!summary) {
    return "主任务就绪";
  }
  switch (summary.status) {
    case "running":
      return "主任务执行中";
    case "completed":
      return "主任务已完成";
    case "failed":
      return "主任务失败";
    case "interrupted":
      return "主任务已停止";
    case "idle":
      return "主任务就绪";
    default:
      // `detached` 走默认兜底；这里不再承诺具体文案，由设计演进。
      return "主任务运行中";
  }
}

export type SideThreadPanelHandle = {
  focusComposer: () => void;
};

type SideThreadPanelProps = {
  entry: SideThreadEntryState;
  mainThreadId: string;
  width: number;
  onClose: () => void;
  onResizeStart: (event: ReactPointerEvent<HTMLButtonElement>) => void;
  onSend: (prompt: string) => void;
  onInterrupt: () => void;
  onChangeDraft: (draft: string) => void;
  // 真正的后端尚未完成时，可用一个 disabledReason 提示用户；主
  // 进程 IPC handlers 到位之前先用这个标志禁用发送。
  sendDisabledReason?: string;
};

export const SideThreadPanel = forwardRef<SideThreadPanelHandle, SideThreadPanelProps>(
  function SideThreadPanel(
    {
      entry,
      mainThreadId,
      width,
      onClose,
      onResizeStart,
      onSend,
      onInterrupt,
      onChangeDraft,
      sendDisabledReason
    },
    ref
  ) {
    const composerRef = useRef<HTMLTextAreaElement | null>(null);
    const messagesEndRef = useRef<HTMLDivElement | null>(null);
    const [pendingQuickPrompt, setPendingQuickPrompt] = useState<string | null>(null);

    useImperativeHandle(
      ref,
      () => ({
        focusComposer: () => {
          composerRef.current?.focus();
        }
      }),
      []
    );

    // 收到新消息或流式增量时自动滚到底。仅在用户没有手动上滚时
    // 跟随——若滚动容器距离底部超过 32px，认为用户在回看历史。
    useEffect(() => {
      const el = messagesEndRef.current;
      if (!el) {
        return;
      }
      const scroller = el.parentElement;
      if (!scroller) {
        return;
      }
      const distanceFromBottom =
        scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight;
      if (distanceFromBottom < 32) {
        scroller.scrollTop = scroller.scrollHeight;
      }
    }, [entry.messages, entry.streaming]);

    const handleDraftChange = useCallback(
      (event: ChangeEvent<HTMLTextAreaElement>) => {
        onChangeDraft(event.target.value);
        // 用户改动了 draft，清掉快捷问题的预填，避免再发送时把它
        // 重复加进 composer。
        setPendingQuickPrompt(null);
      },
      [onChangeDraft]
    );

    const handleKeyDown = useCallback(
      (event: ReactKeyboardEvent<HTMLTextAreaElement>) => {
        if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) {
          event.preventDefault();
          submitComposer();
        }
      },
      // submitComposer 闭包依赖 entry.draft；用 ref 让 handler 永远
      // 读到最新值。
      // eslint-disable-next-line react-hooks/exhaustive-deps
      [entry.draft, entry.streaming, sendDisabledReason]
    );

    const submitComposer = useCallback(() => {
      const text = entry.draft.trim();
      if (!text || sendDisabledReason) {
        return;
      }
      onSend(text);
    }, [entry.draft, sendDisabledReason, onSend]);

    const handleQuickPrompt = useCallback(
      (prompt: string) => {
        setPendingQuickPrompt(prompt);
        onChangeDraft(prompt);
        composerRef.current?.focus();
      },
      [onChangeDraft]
    );

    const isEmpty = entry.messages.length === 0;
    const placeholder = "询问当前任务，不会加入主对话";

    return (
      <aside
        className="side-thread-panel"
        style={{ width: `${width}px` }}
        data-main-thread-id={mainThreadId}
        data-streaming={entry.streaming ? "true" : "false"}
        aria-label="侧聊"
      >
        <button
          type="button"
          className="side-thread-panel__resizer"
          aria-label="调整侧聊宽度"
          onPointerDown={onResizeStart}
        />
        <header className="side-thread-panel__header">
          <div className="side-thread-panel__heading">
            <span className="side-thread-panel__title">侧聊</span>
            <span className="side-thread-panel__status" data-status={entry.summary?.status ?? "idle"}>
              {summaryMainTaskLabel(entry.summary)}
            </span>
          </div>
          <button
            type="button"
            className="side-thread-panel__close"
            onClick={onClose}
            aria-label="收起侧聊"
            title="收起侧聊"
          >
            <PanelRightClose size={16} strokeWidth={1.75} />
          </button>
        </header>

        <div className="side-thread-panel__body" role="log" aria-live="polite">
          {isEmpty ? (
            <SideThreadEmptyState
              disabledReason={sendDisabledReason}
              onQuickPrompt={handleQuickPrompt}
            />
          ) : (
            <ol className="side-thread-panel__messages">
              {entry.messages.map((message) => (
                <SideThreadMessageItem key={message.id} message={message} />
              ))}
              <div ref={messagesEndRef} />
            </ol>
          )}
        </div>

        {entry.lastError ? (
          <div className="side-thread-panel__error" role="alert">
            {entry.lastError}
          </div>
        ) : null}

        <footer className="side-thread-panel__composer">
          <textarea
            ref={composerRef}
            className="side-thread-panel__textarea"
            value={entry.draft || pendingQuickPrompt || ""}
            onChange={handleDraftChange}
            onKeyDown={handleKeyDown}
            placeholder={placeholder}
            rows={2}
            disabled={Boolean(sendDisabledReason)}
            data-testid="side-thread-textarea"
          />
          <div className="side-thread-panel__composer-actions">
            {entry.streaming ? (
              <button
                type="button"
                className="side-thread-panel__interrupt"
                onClick={onInterrupt}
                aria-label="停止侧聊"
              >
                <Square size={14} strokeWidth={2} />
                <span>停止</span>
              </button>
            ) : (
              <button
                type="button"
                className="side-thread-panel__send"
                onClick={submitComposer}
                disabled={Boolean(sendDisabledReason) || !(entry.draft || pendingQuickPrompt || "").trim()}
                aria-label="发送侧聊消息"
              >
                {entry.streaming ? (
                  <Loader2 size={14} strokeWidth={2} className="spin" />
                ) : (
                  <span>发送</span>
                )}
              </button>
            )}
          </div>
        </footer>
      </aside>
    );
  }
);

// ============================================================================
// 子组件
// ============================================================================

function SideThreadEmptyState({
  disabledReason,
  onQuickPrompt
}: {
  disabledReason?: string;
  onQuickPrompt: (prompt: string) => void;
}) {
  return (
    <div className="side-thread-panel__empty">
      <p className="side-thread-panel__empty-copy">
        可以询问当前任务的进度和相关信息。这里的内容不会加入主对话。
      </p>
      <ul className="side-thread-panel__quick-prompts">
        {EMPTY_QUICK_PROMPTS.map((item) => (
          <li key={item.label}>
            <button
              type="button"
              className="side-thread-panel__quick-prompt"
              onClick={() => onQuickPrompt(item.prompt)}
              disabled={Boolean(disabledReason)}
            >
              {item.label}
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}

function SideThreadMessageItem({ message }: { message: SideThreadMessage }) {
  const isUser = message.role === "user";
  const isFailed = message.status === "failed";
  return (
    <li
      className={`side-thread-panel__message side-thread-panel__message--${message.role}${
        isFailed ? " side-thread-panel__message--failed" : ""
      }`}
      data-status={message.status ?? "completed"}
    >
      <div className="side-thread-panel__message-bubble">
        <MessageText text={message.text} />
      </div>
      {!isUser && message.status === "streaming" ? (
        <span className="side-thread-panel__streaming-dot" aria-hidden />
      ) : null}
      {isFailed && message.error_message ? (
        <div className="side-thread-panel__message-error">{message.error_message}</div>
      ) : null}
      {/* 标记 isUser 给测试/未来扩展使用；保留可读性。 */}
      <span className="visually-hidden" data-role={message.role}>
        {isUser ? "user" : "assistant"}
      </span>
    </li>
  );
}

// 极简消息文本渲染：保留换行与空白，链接 / 代码块 / 列表留给后续
// 接 ChatThreadView 渲染管线时再升级。第一版只回答"现在做到哪了"
// 这类问题，纯文本已够用。
function MessageText({ text }: { text: string }) {
  if (!text) {
    return <span className="side-thread-panel__message-placeholder">…</span>;
  }
  return <span>{text}</span>;
}