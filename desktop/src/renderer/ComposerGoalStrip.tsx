import {
  Check,
  Info,
  Loader2,
  MoreHorizontal,
  Pause,
  Pencil,
  Play,
  Target,
  Trash2,
  X,
  type LucideIcon,
} from "lucide-react";
import {
  type KeyboardEvent,
  useEffect,
  useRef,
  useState,
} from "react";
import type { ComposerGoalSummary } from "../shared/protocol";
import {
  FloatingMenuPortal,
  isInsideFloatingMenu,
} from "./ComposerFloatingMenu";

const ACTION_CONFIRM_WINDOW_MS = 3000;

type GoalBusyAction = "edit" | "pause" | "resume" | "clear";
type ConfirmableGoalAction = "clear";
type GoalInfoRow = { label: string; value: string };

export function ComposerGoalStrip({
  summary,
  disabled,
  onEdit,
  onPause,
  onResume,
  onClear,
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
  const [infoOpen, setInfoOpen] = useState(false);
  const [actionsOpen, setActionsOpen] = useState(false);
  const inputRef = useRef<HTMLInputElement | null>(null);
  const controlsRef = useRef<HTMLDivElement | null>(null);
  const infoAnchorRef = useRef<HTMLSpanElement | null>(null);
  const actionsAnchorRef = useRef<HTMLSpanElement | null>(null);
  const infoCloseTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const confirmTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    if (!editing) return;
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
    setInfoOpen(false);
    setActionsOpen(false);
    clearConfirmTimer();
  }, [summary?.id]);

  useEffect(() => {
    if (!infoOpen && !actionsOpen) return;

    function handlePointerDown(event: PointerEvent): void {
      const target = event.target;
      if (target instanceof Node && controlsRef.current?.contains(target)) {
        return;
      }
      if (target instanceof Node && isInsideFloatingMenu(target, "composer-goal")) {
        return;
      }
      closePopovers();
    }

    function handleKeyDown(event: globalThis.KeyboardEvent): void {
      if (event.key === "Escape") closePopovers();
    }

    document.addEventListener("pointerdown", handlePointerDown);
    document.addEventListener("keydown", handleKeyDown);
    return () => {
      document.removeEventListener("pointerdown", handlePointerDown);
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, [actionsOpen, infoOpen]);

  useEffect(() => {
    return () => {
      if (confirmTimerRef.current) {
        clearTimeout(confirmTimerRef.current);
      }
      if (infoCloseTimerRef.current) {
        clearTimeout(infoCloseTimerRef.current);
      }
    };
  }, []);

  if (!summary) return null;

  const activeSummary = summary;
  const displayText = goalStripDisplayText(summary.text);
  const visibleStatus = goalVisibleStatusText(summary);
  const infoRows = goalInfoRows(summary);
  const canPause = summary.can_pause === true;
  const canResume = summary.can_resume === true;
  const canClear = summary.can_clear === true;

  function clearConfirmTimer(): void {
    if (!confirmTimerRef.current) return;
    clearTimeout(confirmTimerRef.current);
    confirmTimerRef.current = null;
  }

  function clearInfoCloseTimer(): void {
    if (!infoCloseTimerRef.current) return;
    clearTimeout(infoCloseTimerRef.current);
    infoCloseTimerRef.current = null;
  }

  function scheduleInfoClose(): void {
    clearInfoCloseTimer();
    infoCloseTimerRef.current = setTimeout(() => {
      setInfoOpen(false);
      infoCloseTimerRef.current = null;
    }, 100);
  }

  function resetConfirmation(): void {
    setConfirmingAction(null);
    clearConfirmTimer();
  }

  function closePopovers(): void {
    clearInfoCloseTimer();
    setInfoOpen(false);
    setActionsOpen(false);
    resetConfirmation();
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
    closePopovers();
    setEditing(true);
  }

  function handleCancelEdit(): void {
    setEditing(false);
    setDraft("");
    setError(null);
  }

  function handlePauseGoal(): void {
    setActionsOpen(false);
    runAction("pause", onPause, "暂停目标失败");
  }

  function handleResumeGoal(): void {
    setActionsOpen(false);
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
    setActionsOpen(false);
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
    <div className="composer-goal-strip" role="status" aria-live="polite">
      <span className="composer-goal-strip-icon" aria-hidden="true">
        <Target className="icon" />
      </span>
      <span className="composer-goal-strip-label">Goal</span>
      <span className="composer-goal-strip-main">
        <span className="composer-goal-strip-text" title={displayText}>
          {displayText}
        </span>
        {visibleStatus ? (
          <span className="composer-goal-strip-state">{visibleStatus}</span>
        ) : null}
      </span>
      <div ref={controlsRef} className="composer-goal-strip-actions">
        <span
          ref={infoAnchorRef}
          className="composer-goal-strip-control-anchor"
          onMouseEnter={() => {
            clearInfoCloseTimer();
            setInfoOpen(true);
          }}
          onMouseLeave={scheduleInfoClose}
        >
          <button
            className="composer-goal-strip-action"
            type="button"
            aria-label="查看目标详情"
            aria-expanded={infoOpen}
            title="目标详情"
            onClick={() => {
              clearInfoCloseTimer();
              setActionsOpen(false);
              setInfoOpen((current) => !current);
            }}
          >
            <Info className="icon-sm" aria-hidden="true" />
          </button>
          {infoOpen ? (
            <FloatingMenuPortal
              anchorRef={infoAnchorRef}
              owner="composer-goal"
              placement="above"
              align="right"
              offset={6}
              width={280}
            >
              <div
                className="composer-goal-strip-info"
                role="tooltip"
                onMouseEnter={clearInfoCloseTimer}
                onMouseLeave={scheduleInfoClose}
              >
                <div className="composer-goal-strip-info-title">目标详情</div>
                <dl className="composer-goal-strip-info-list">
                  {infoRows.map((row) => (
                    <div key={row.label} className="composer-goal-strip-info-row">
                      <dt>{row.label}</dt>
                      <dd>{row.value}</dd>
                    </div>
                  ))}
                </dl>
                <p className="composer-goal-strip-info-note">
                  有效运行时间只统计实际执行，不含暂停或阻塞。
                </p>
              </div>
            </FloatingMenuPortal>
          ) : null}
        </span>
        <span ref={actionsAnchorRef} className="composer-goal-strip-control-anchor">
          <button
            className="composer-goal-strip-action"
            type="button"
            aria-label="目标操作"
            aria-expanded={actionsOpen}
            title="目标操作"
            disabled={disabled || busy !== null}
            onClick={() => {
              setInfoOpen(false);
              setActionsOpen((current) => !current);
            }}
          >
            {busy ? (
              <Loader2
                className="icon-sm composer-goal-strip-spin"
                aria-hidden="true"
              />
            ) : (
              <MoreHorizontal className="icon-sm" aria-hidden="true" />
            )}
          </button>
          {actionsOpen ? (
            <FloatingMenuPortal
              anchorRef={actionsAnchorRef}
              owner="composer-goal"
              placement="above"
              align="right"
              offset={6}
              width={168}
            >
              <div className="composer-goal-strip-menu" role="menu">
                {canPause ? (
                  <GoalMenuButton
                    icon={Pause}
                    label="暂停目标"
                    onClick={handlePauseGoal}
                  />
                ) : null}
                {canResume ? (
                  <GoalMenuButton
                    icon={Play}
                    label="继续目标"
                    onClick={handleResumeGoal}
                  />
                ) : null}
                <GoalMenuButton
                  icon={Pencil}
                  label="编辑目标"
                  onClick={handleStartEdit}
                />
                {canClear ? (
                  <GoalMenuButton
                    danger={confirmingAction === "clear"}
                    icon={Trash2}
                    label={
                      confirmingAction === "clear"
                        ? "再次点击确认清除"
                        : "清除目标"
                    }
                    onClick={() => handleConfirmableGoalAction("clear")}
                  />
                ) : null}
              </div>
            </FloatingMenuPortal>
          ) : null}
        </span>
      </div>
      {error ? (
        <span className="composer-goal-strip-error" role="alert">
          {error}
        </span>
      ) : null}
    </div>
  );
}

function GoalMenuButton({
  icon: Icon,
  label,
  danger = false,
  onClick,
}: {
  icon: LucideIcon;
  label: string;
  danger?: boolean;
  onClick: () => void;
}): JSX.Element {
  return (
    <button
      className={`composer-goal-strip-menu-item${danger ? " danger" : ""}`}
      type="button"
      role="menuitem"
      onClick={onClick}
    >
      <Icon className="icon-sm" aria-hidden="true" />
      <span>{label}</span>
    </button>
  );
}

function goalStripDisplayText(text: string): string {
  const firstLine = text.trim().split(/\r?\n/, 1)[0]?.trim() ?? "";
  return firstLine || "（无目标文本）";
}

function goalStatusKey(summary: ComposerGoalSummary): string {
  return (summary.status || "").trim().toLowerCase();
}

function goalVisibleStatusText(summary: ComposerGoalSummary): string {
  const status = goalStatusKey(summary);
  if (status === "paused") return "已暂停";
  if (status === "blocked") return "已阻塞";
  return "";
}

function goalStatusText(summary: ComposerGoalSummary): string {
  const raw = goalStatusKey(summary);
  switch (raw) {
    case "":
    case "active":
    case "running":
      return "运行中";
    case "paused":
      return "已暂停";
    case "blocked":
      return "已阻塞";
    default:
      return "状态未知";
  }
}

function goalInfoRows(summary: ComposerGoalSummary): GoalInfoRow[] {
  const rows: GoalInfoRow[] = [
    { label: "状态", value: goalStatusText(summary) },
    {
      label: "有效运行",
      value: formatGoalDuration(summary.time_used_seconds ?? 0),
    },
  ];
  if ((summary.goal_turns ?? 0) > 0) {
    rows.push({ label: "回合", value: `${summary.goal_turns} 轮` });
  }
  if ((summary.tokens_used ?? 0) > 0) {
    rows.push({
      label: "Tokens",
      value: formatCompactNumber(summary.tokens_used ?? 0),
    });
  }
  const blocker = summary.blocker?.trim() ?? "";
  if (blocker) {
    rows.push({ label: "阻塞原因", value: blocker });
  }
  const progress = summary.recent_progress?.trim() ?? "";
  if (progress) {
    rows.push({ label: "最近进展", value: progress });
  }
  return rows;
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
