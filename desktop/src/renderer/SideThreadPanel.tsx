import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useMemo,
  useRef,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
} from "react";
import { PanelRightClose } from "lucide-react";
import { useAutoFollowScrollContainer } from "./AutoFollowScroll";
import { ConversationTurnList } from "./ConversationTurnList";
import { sideThreadMessagesToTurns } from "./SideThreadTurns";
import {
  SIDE_THREAD_MAX_WIDTH,
  SIDE_THREAD_MIN_WIDTH,
  type SideThreadEntryState,
} from "./SideThreadState";
import { latestAgentMessageItemID, TurnView } from "./TurnView";

const EMPTY_QUICK_PROMPTS = [
  { label: "现在做到哪了？", prompt: "现在做到哪了？" },
  { label: "解释刚才的错误", prompt: "解释刚才出现的错误或工具结果" },
  { label: "当前方案有什么风险？", prompt: "当前方案可能存在哪些风险或后续影响？" },
];

export type SideThreadPanelHandle = {
  focusComposer: () => void;
};

type SideThreadPanelProps = {
  entry: SideThreadEntryState;
  mainThreadId: string;
  mainTaskRunning?: boolean;
  width: number;
  composer: ReactNode;
  cwd?: string;
  onClose: () => void;
  onResizeStart: (event: ReactPointerEvent<HTMLButtonElement>) => void;
  onChangeDraft: (draft: string) => void;
  onOpenFile?: (path: string) => void;
};

export const SideThreadPanel = forwardRef<SideThreadPanelHandle, SideThreadPanelProps>(
  function SideThreadPanel(
    {
      entry,
      mainThreadId,
      mainTaskRunning,
      width,
      composer,
      cwd,
      onClose,
      onResizeStart,
      onChangeDraft,
      onOpenFile,
    },
    ref,
  ) {
    const composerHostRef = useRef<HTMLDivElement | null>(null);
    const bodyScroll = useAutoFollowScrollContainer({ open: true });
    const turns = useMemo(
      () => sideThreadMessagesToTurns(entry.messages),
      [entry.messages],
    );
    const latestTurn = turns.at(-1);
    const latestMessageID = useMemo(
      () => latestAgentMessageItemID(turns),
      [turns],
    );

    const focusComposer = useCallback(() => {
      const host = composerHostRef.current;
      const textarea =
        host?.querySelector<HTMLTextAreaElement>("textarea:not(:disabled)") ??
        host?.querySelector<HTMLTextAreaElement>("textarea");
      textarea?.focus();
    }, []);

    useImperativeHandle(ref, () => ({ focusComposer }), [focusComposer]);

    // Message commits can land without a stream frame (history load, peer
    // windows), so re-anchor on them like the main conversation does for
    // turn snapshots.
    useEffect(() => {
      bodyScroll.scrollToBottom();
    }, [bodyScroll, entry.messages, entry.streaming]);

    const handleStreamFrame = useCallback(() => {
      bodyScroll.scheduleScrollToBottom();
    }, [bodyScroll]);

    const handleQuickPrompt = useCallback(
      (prompt: string) => {
        onChangeDraft(prompt);
        window.requestAnimationFrame(focusComposer);
      },
      [focusComposer, onChangeDraft],
    );

    const isEmpty = turns.length === 0;

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
            {(mainTaskRunning ?? entry.summary?.main_task_summary?.running) ===
            true ? (
              <span
                className="side-thread-panel__status"
                data-status={entry.summary?.status ?? "idle"}
              >
                主任务执行中
              </span>
            ) : null}
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
          ref={bodyScroll.scrollRef}
          className="side-thread-panel__body"
          role="log"
          aria-live="polite"
        >
          {isEmpty ? (
            <SideThreadEmptyState
              disabled={entry.streaming}
              onQuickPrompt={handleQuickPrompt}
            />
          ) : (
            <div className="conversation-width session-flow side-thread-panel__conversation">
              <ConversationTurnList
                threadID={entry.summary?.side_thread_id ?? `side:${mainThreadId}`}
                turns={turns}
                renderTurn={(turn) => (
                  <TurnView
                    turn={turn}
                    cwd={cwd}
                    onOpenFile={onOpenFile}
                    latestAgentMessageID={latestMessageID}
                    onStreamFrame={handleStreamFrame}
                    isLatestTurn={turn.id === latestTurn?.id}
                  />
                )}
              />
            </div>
          )}
        </div>

        {entry.lastError ? (
          <div className="side-thread-panel__error" role="alert">
            {entry.lastError}
          </div>
        ) : null}

        <div ref={composerHostRef} className="side-thread-panel__composer-host">
          {composer}
        </div>
      </aside>
    );
  },
);

function SideThreadEmptyState({
  disabled,
  onQuickPrompt,
}: {
  disabled: boolean;
  onQuickPrompt: (prompt: string) => void;
}) {
  return (
    <div className="side-thread-panel__empty">
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
