import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useRef,
  type ChangeEvent,
  type KeyboardEvent as ReactKeyboardEvent,
  type PointerEvent as ReactPointerEvent,
  type UIEvent
} from "react";
import { PanelRightClose, Square } from "lucide-react";
import type { SideThreadMessage, SideThreadSummary } from "../shared/protocol";
import {
  SIDE_THREAD_MAX_WIDTH,
  SIDE_THREAD_MIN_WIDTH,
  type SideThreadEntryState
} from "./SideThreadState";

const EMPTY_QUICK_PROMPTS = [
  { label: "现在做到哪了？", prompt: "现在做到哪了？" },
  { label: "解释刚才的错误", prompt: "解释刚才出现的错误或工具结果" },
  { label: "当前方案有什么风险？", prompt: "当前方案可能存在哪些风险或后续影响？" }
];

function mainTaskLabel(
  summary: SideThreadSummary | null,
  liveRunning?: boolean
): string {
  const running = liveRunning ?? summary?.main_task_summary?.running;
  if (running === true) {
    return "主任务执行中";
  }
  if (running === false) {
    return "主任务未运行";
  }
  return "主任务状态未知";
}

export type SideThreadPanelHandle = {
  focusComposer: () => void;
};

type SideThreadPanelProps = {
  entry: SideThreadEntryState;
  mainThreadId: string;
  mainTaskRunning?: boolean;
  width: number;
  onClose: () => void;
  onResizeStart: (event: ReactPointerEvent<HTMLButtonElement>) => void;
  onSend: (prompt: string) => void;
  onInterrupt: () => void;
  onChangeDraft: (draft: string) => void;
  sendDisabledReason?: string;
};

export const SideThreadPanel = forwardRef<SideThreadPanelHandle, SideThreadPanelProps>(
  function SideThreadPanel(
    {
      entry,
      mainThreadId,
      mainTaskRunning,
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
    const bodyRef = useRef<HTMLDivElement | null>(null);
    const autoFollowRef = useRef(true);

    useImperativeHandle(
      ref,
      () => ({
        focusComposer: () => composerRef.current?.focus()
      }),
      []
    );

    useEffect(() => {
      const body = bodyRef.current;
      if (body && autoFollowRef.current) {
        body.scrollTop = body.scrollHeight;
      }
    }, [entry.messages, entry.streaming]);

    const handleBodyScroll = useCallback((event: UIEvent<HTMLDivElement>) => {
      const body = event.currentTarget;
      autoFollowRef.current =
        body.scrollHeight - body.scrollTop - body.clientHeight < 32;
    }, []);

    const handleDraftChange = useCallback(
      (event: ChangeEvent<HTMLTextAreaElement>) => {
        onChangeDraft(event.target.value);
      },
      [onChangeDraft]
    );

    const submitComposer = useCallback(() => {
      const text = entry.draft.trim();
      if (!text || entry.streaming || sendDisabledReason) {
        return;
      }
      onSend(text);
    }, [entry.draft, entry.streaming, onSend, sendDisabledReason]);

    const handleKeyDown = useCallback(
      (event: ReactKeyboardEvent<HTMLTextAreaElement>) => {
        if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) {
          event.preventDefault();
          submitComposer();
        }
      },
      [submitComposer]
    );

    const handleQuickPrompt = useCallback(
      (prompt: string) => {
        onChangeDraft(prompt);
        composerRef.current?.focus();
      },
      [onChangeDraft]
    );

    const isEmpty = entry.messages.length === 0;

    return (
      <aside
        className="side-thread-panel"
        data-main-thread-id={mainThreadId}
        data-streaming={entry.streaming ? "true" : "false"}
        aria-label="侧聊"
      >
        <button
          type="button"
          className="side-thread-panel__resizer"
          role="separator"
          aria-label="调整侧聊宽度"
          aria-orientation="vertical"
          aria-valuemin={SIDE_THREAD_MIN_WIDTH}
          aria-valuemax={SIDE_THREAD_MAX_WIDTH}
          aria-valuenow={width}
          onPointerDown={onResizeStart}
        />
        <header className="side-thread-panel__header">
          <div className="side-thread-panel__heading">
            <span className="side-thread-panel__title">侧聊</span>
            <span
              className="side-thread-panel__status"
              data-status={entry.summary?.status ?? "idle"}
            >
              {mainTaskLabel(entry.summary, mainTaskRunning)}
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

        <div
          ref={bodyRef}
          className="side-thread-panel__body"
          role="log"
          aria-live="polite"
          onScroll={handleBodyScroll}
        >
          {isEmpty ? (
            <SideThreadEmptyState
              disabled={Boolean(sendDisabledReason) || entry.streaming}
              onQuickPrompt={handleQuickPrompt}
            />
          ) : (
            <ol className="side-thread-panel__messages">
              {entry.messages.map((message) => (
                <SideThreadMessageItem key={message.id} message={message} />
              ))}
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
            value={entry.draft}
            onChange={handleDraftChange}
            onKeyDown={handleKeyDown}
            placeholder="询问当前任务，不会加入主对话"
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
                disabled={Boolean(sendDisabledReason) || !entry.draft.trim()}
                aria-label="发送侧聊消息"
              >
                发送
              </button>
            )}
          </div>
        </footer>
      </aside>
    );
  }
);

function SideThreadEmptyState({
  disabled,
  onQuickPrompt
}: {
  disabled: boolean;
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
              disabled={disabled}
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
  const isFailed = message.status === "failed";
  return (
    <li
      className={`side-thread-panel__message side-thread-panel__message--${message.role}${
        isFailed ? " side-thread-panel__message--failed" : ""
      }`}
      data-status={message.status ?? "completed"}
    >
      <div className="side-thread-panel__message-bubble">
        {message.text || (
          <span className="side-thread-panel__message-placeholder">…</span>
        )}
      </div>
      {message.role === "assistant" && message.status === "streaming" ? (
        <span className="side-thread-panel__streaming-dot" aria-hidden />
      ) : null}
      {isFailed && message.error_message ? (
        <div className="side-thread-panel__message-error">{message.error_message}</div>
      ) : null}
    </li>
  );
}
