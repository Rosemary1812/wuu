import {
  Check,
  Loader2,
  Pause,
  Pencil,
  Play,
  Target,
  Trash2,
  X
} from "lucide-react";
import {
  type KeyboardEvent,
  useEffect,
  useMemo,
  useRef,
  useState
} from "react";
import type { ComposerGoalSummary } from "../shared/protocol";

const ACTION_CONFIRM_WINDOW_MS = 3000;
const ELAPSED_TICK_MS = 1000;

type GoalBusyAction = "edit" | "pause" | "resume" | "clear";
type ConfirmableGoalAction = "clear";

export function ComposerGoalStrip({
  summary,
  disabled,
  onEdit,
  onPause,
  onResume,
  onClear
}: {
  summary: ComposerGoalSummary | null;
  disabled?: boolean;
  onEdit: (nextText: string) => void | Promise<void>;
  onPause: () => void | Promise<void>;
  onResume: () => void | Promise<void>;
  onClear: () => void | Promise<void>;
}): JSX.Element | null {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState("");
  const [confirmingAction, setConfirmingAction] =
    useState<ConfirmableGoalAction | null>(null);
  const [busy, setBusy] = useState<GoalBusyAction | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [now, setNow] = useState<number>(() => Date.now());
  const inputRef = useRef<HTMLInputElement | null>(null);
  const confirmTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const startedAtMs = useMemo(
    () => parseStartedAt(summary?.started_at),
    [summary?.started_at],
  );

  useEffect(() => {
    if (startedAtMs == null) {
      return;
    }
    setNow(Date.now());
    const intervalID = setInterval(() => {
      setNow(Date.now());
    }, ELAPSED_TICK_MS);
    return () => {
      clearInterval(intervalID);
    };
  }, [startedAtMs]);

  useEffect(() => {
    if (!editing) {
      return;
    }
    setDraft(summary?.text ?? "");
    requestAnimationFrame(() => {
      const node = inputRef.current;
      if (!node) return;
      node.focus();
      const length = node.value.length;
      node.setSelectionRange(length, length);
    });
  }, [editing, summary?.text]);

  useEffect(() => {
    setEditing(false);
    setDraft(summary?.text ?? "");
    setConfirmingAction(null);
    setBusy(null);
    setError(null);
    clearConfirmTimer();
  }, [summary?.id]);

  useEffect(() => {
    return () => {
      if (confirmTimerRef.current) {
        clearTimeout(confirmTimerRef.current);
      }
    };
  }, []);

  if (!summary) {
    return null;
  }
  const activeSummary = summary;
  const displayText = goalStripDisplayText(summary.text);
  const detailText = goalStripDetailText(summary);
  const elapsedElement = renderElapsed(startedAtMs, now);
  const canPause = summary.can_pause === true;
  const canResume = summary.can_resume === true;
  const canClear = summary.can_clear === true;

  function clearConfirmTimer(): void {
    if (confirmTimerRef.current) {
      clearTimeout(confirmTimerRef.current);
      confirmTimerRef.current = null;
    }
  }

  function resetConfirmation(): void {
    setConfirmingAction(null);
    clearConfirmTimer();
  }

  function startConfirmation(action: ConfirmableGoalAction): void {
    setConfirmingAction(action);
    clearConfirmTimer();
    confirmTimerRef.current = setTimeout(() => {
      setConfirmingAction(null);
      confirmTimerRef.current = null;
    }, ACTION_CONFIRM_WINDOW_MS);
  }

  function handleStartEdit(): void {
    if (disabled || busy) return;
    setError(null);
    resetConfirmation();
    setEditing(true);
  }

  function handleCancelEdit(): void {
    setEditing(false);
    setDraft("");
    setError(null);
  }

  function handlePauseGoal(): void {
    runAction("pause", onPause, "暂停目标失败");
  }

  function handleResumeGoal(): void {
    runAction("resume", onResume, "继续目标失败");
  }

  function handleConfirmableGoalAction(action: ConfirmableGoalAction): void {
    if (disabled || busy) return;
    setError(null);
    if (confirmingAction !== action) {
      startConfirmation(action);
      return;
    }
    resetConfirmation();
    runAction(action, onClear, "清除目标失败");
  }

  function runAction(
    action: GoalBusyAction,
    operation: () => void | Promise<void>,
    failureMessage: string,
  ): void {
    if (disabled || busy) return;
    setError(null);
    resetConfirmation();
    setBusy(action);
    void (async () => {
      try {
        await operation();
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : failureMessage);
      } finally {
        setBusy(null);
      }
    })();
  }

  async function handleSubmitEdit(): Promise<void> {
    const next = draft.trim();
    if (!next) {
      setError("目标文本不能为空");
      return;
    }
    if (next === activeSummary.text.trim()) {
      setEditing(false);
      setError(null);
      return;
    }
    setBusy("edit");
    try {
      await onEdit(next);
      setEditing(false);
      setError(null);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "更新目标失败");
    } finally {
      setBusy(null);
    }
  }

  function handleEditKeyDown(event: KeyboardEvent<HTMLInputElement>): void {
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      void handleSubmitEdit();
      return;
    }
    if (event.key === "Escape") {
      event.preventDefault();
      handleCancelEdit();
    }
  }

  if (editing) {
    return (
      <div
        className="composer-goal-strip editing"
        role="group"
        aria-label="编辑当前目标"
      >
        <span className="composer-goal-strip-icon" aria-hidden="true">
          <Target className="icon" />
        </span>
        <span className="composer-goal-strip-label">Goal</span>
        {elapsedElement}
        <input
          ref={inputRef}
          className="composer-goal-strip-input"
          value={draft}
          spellCheck={false}
          disabled={busy === "edit"}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={handleEditKeyDown}
          aria-label="目标文本"
        />
        <div className="composer-goal-strip-actions">
          <button
            className="composer-goal-strip-action"
            type="button"
            aria-label="保存目标"
            title="保存"
            disabled={busy !== null}
            onClick={() => void handleSubmitEdit()}
          >
            {busy === "edit" ? (
              <Loader2
                className="icon-sm composer-goal-strip-spin"
                aria-hidden="true"
              />
            ) : (
              <Check className="icon-sm" aria-hidden="true" />
            )}
          </button>
          <button
            className="composer-goal-strip-action"
            type="button"
            aria-label="取消编辑"
            title="取消编辑"
            disabled={busy !== null}
            onClick={handleCancelEdit}
          >
            <X className="icon-sm" aria-hidden="true" />
          </button>
        </div>
        {error ? (
          <span className="composer-goal-strip-error" role="alert">
            {error}
          </span>
        ) : null}
      </div>
    );
  }

  return (
    <div
      className={
        `composer-goal-strip${
          confirmingAction ? ` confirming-${confirmingAction}` : ""
        }`
      }
      role="status"
      aria-live="polite"
    >
      <span className="composer-goal-strip-icon" aria-hidden="true">
        <Target className="icon" />
      </span>
      <span className="composer-goal-strip-label">Goal</span>
      {elapsedElement}
      <span className="composer-goal-strip-main">
        <span className="composer-goal-strip-text" title={displayText}>
          {displayText}
        </span>
        {detailText ? (
          <span className="composer-goal-strip-detail" title={detailText}>
            {detailText}
          </span>
        ) : null}
      </span>
      <div className="composer-goal-strip-actions">
        {canPause ? (
          <button
            className="composer-goal-strip-action"
            type="button"
            aria-label="暂停目标"
            title="暂停目标"
            disabled={disabled || busy !== null}
            onClick={handlePauseGoal}
          >
            {busy === "pause" ? (
              <Loader2
                className="icon-sm composer-goal-strip-spin"
                aria-hidden="true"
              />
            ) : (
              <Pause className="icon-sm" aria-hidden="true" />
            )}
          </button>
        ) : null}
        {canResume ? (
          <button
            className="composer-goal-strip-action"
            type="button"
            aria-label="继续目标"
            title="继续目标"
            disabled={disabled || busy !== null}
            onClick={handleResumeGoal}
          >
            {busy === "resume" ? (
              <Loader2
                className="icon-sm composer-goal-strip-spin"
                aria-hidden="true"
              />
            ) : (
              <Play className="icon-sm" aria-hidden="true" />
            )}
          </button>
        ) : null}
        <button
          className="composer-goal-strip-action"
          type="button"
          aria-label="编辑目标"
          title="编辑目标"
          disabled={disabled || busy !== null}
          onClick={handleStartEdit}
        >
          <Pencil className="icon-sm" aria-hidden="true" />
        </button>
        {canClear ? (
          <button
            className={
              `composer-goal-strip-action${
                confirmingAction === "clear" ? " danger" : ""
              }`
            }
            type="button"
            aria-label={
              confirmingAction === "clear" ? "再次点击确认清除目标" : "清除目标"
            }
            title={confirmingAction === "clear" ? "再次点击确认" : "清除目标"}
            disabled={disabled || busy !== null}
            onClick={() => handleConfirmableGoalAction("clear")}
          >
            {busy === "clear" ? (
              <Loader2
                className="icon-sm composer-goal-strip-spin"
                aria-hidden="true"
              />
            ) : (
              <Trash2 className="icon-sm" aria-hidden="true" />
            )}
          </button>
        ) : null}
      </div>
      {error ? (
        <span className="composer-goal-strip-error" role="alert">
          {error}
        </span>
      ) : null}
    </div>
  );
}

function goalStripDisplayText(text: string): string {
  const firstLine = text.trim().split(/\r?\n/, 1)[0]?.trim() ?? "";
  return firstLine || "（无目标文本）";
}

function goalStripDetailText(summary: ComposerGoalSummary): string {
  const parts = [
    goalStatusText(summary),
    goalProgressText(summary),
    goalUsageText(summary),
  ].filter((part) => part.length > 0);
  return parts.join(" | ");
}

function goalStatusText(summary: ComposerGoalSummary): string {
  const raw = (summary.stop_reason || summary.status || "")
    .trim()
    .toLowerCase();
  switch (raw) {
    case "":
    case "active":
    case "running":
      return "";
    case "paused":
      return "已暂停";
    case "blocked":
      return "已阻塞";
    case "needs_human":
      return "等待人工";
    default:
      return raw;
  }
}

function goalProgressText(summary: ComposerGoalSummary): string {
  const blocker = summary.blocker?.trim() ?? "";
  if (blocker) {
    const turns = summary.blocker_consecutive_turns ?? 0;
    return turns > 1 ? `${blocker} (${turns} 次)` : blocker;
  }
  return summary.recent_progress?.trim() ?? "";
}

function goalUsageText(summary: ComposerGoalSummary): string {
  const parts: string[] = [];
  if ((summary.goal_turns ?? 0) > 0) {
    parts.push(`${summary.goal_turns} 轮`);
  }
  if ((summary.tokens_used ?? 0) > 0) {
    parts.push(`${formatCompactNumber(summary.tokens_used ?? 0)} tokens`);
  }
  if ((summary.time_used_seconds ?? 0) > 0) {
    parts.push(formatGoalDuration(summary.time_used_seconds ?? 0));
  }
  return parts.join(" / ");
}

function formatCompactNumber(value: number): string {
  return Math.max(0, value).toLocaleString("en-US");
}

function formatGoalDuration(totalSeconds: number): string {
  const seconds = Math.max(0, Math.floor(totalSeconds));
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const remainingSeconds = seconds % 60;
  if (hours > 0) {
    return `${hours} 小时 ${minutes} 分`;
  }
  if (minutes > 0) {
    return `${minutes} 分 ${remainingSeconds} 秒`;
  }
  return `${remainingSeconds} 秒`;
}

// parseStartedAt converts the backend's RFC3339 timestamp into epoch ms.
// Returns null when the value is missing or unparseable so the timer chip
// stays hidden instead of showing "00:00" for goals that pre-date the
// started_at field.
function parseStartedAt(value: string | undefined): number | null {
  if (!value) return null;
  const ms = Date.parse(value);
  return Number.isFinite(ms) ? ms : null;
}

// formatElapsed renders a positive-up elapsed counter. Under one hour it
// uses mm:ss; one hour or more switches to h:mm:ss without zero padding
// the hour so a 12-hour run reads "12:34:56" rather than "012:34:56".
function formatElapsed(ms: number): string {
  const totalSeconds = Math.max(0, Math.floor(ms / 1000));
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  const pad = (n: number) => String(n).padStart(2, "0");
  if (hours > 0) {
    return `${hours}:${pad(minutes)}:${pad(seconds)}`;
  }
  return `${pad(minutes)}:${pad(seconds)}`;
}

function renderElapsed(
  startedAtMs: number | null,
  now: number,
): JSX.Element | null {
  if (startedAtMs == null) return null;
  const elapsedMs = Math.max(0, now - startedAtMs);
  return (
    <span
      className="composer-goal-strip-elapsed"
      title="目标运行时间"
      aria-label={`目标已运行 ${formatElapsedA11y(elapsedMs)}`}
    >
      {formatElapsed(elapsedMs)}
    </span>
  );
}

// formatElapsedA11y renders the elapsed as a Chinese-language phrase for
// screen readers so the tabular "01:05" form doesn't get announced as
// zero-one colon zero-five.
function formatElapsedA11y(ms: number): string {
  const totalSeconds = Math.max(0, Math.floor(ms / 1000));
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  if (hours > 0) {
    return `${hours} 小时 ${minutes} 分 ${seconds} 秒`;
  }
  if (minutes > 0) {
    return `${minutes} 分 ${seconds} 秒`;
  }
  return `${seconds} 秒`;
}
