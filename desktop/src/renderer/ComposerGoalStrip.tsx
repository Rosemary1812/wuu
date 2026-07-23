import {
  ChevronDown,
  ChevronUp,
  Loader2,
  MoreHorizontal,
  Pause,
  Pencil,
  Play,
  Target,
  Trash2,
  type LucideIcon,
} from "lucide-react";
import {
  type KeyboardEvent,
  useEffect,
  useRef,
  useState,
} from "react";
import { createPortal } from "react-dom";
import type { ComposerGoalSummary } from "../shared/protocol";
import {
  FloatingMenuPortal,
  isInsideFloatingMenu,
} from "./ComposerFloatingMenu";
import { Modal } from "./Modal";
import { formatCurrentNumber, translateCurrent as translate, useI18n } from "./i18n";

const ACTION_CONFIRM_WINDOW_MS = 3000;

type GoalBusyAction = "edit" | "pause" | "resume" | "clear";
type ConfirmableGoalAction = "clear";
type GoalInfoRow = { label: string; value: string };

export function ComposerGoalStrip({
  summary,
  disabled,
  expanded: controlledExpanded,
  onExpandedChange,
  onEdit,
  onPause,
  onResume,
  onClear,
}: {
  summary: ComposerGoalSummary | null;
  disabled?: boolean;
  expanded?: boolean;
  onExpandedChange?: (expanded: boolean) => void;
  onEdit: (nextText: string) => void | Promise<void>;
  onPause: () => void | Promise<void>;
  onResume: () => void | Promise<void>;
  onClear: () => void | Promise<void>;
}): JSX.Element | null {
  const { t } = useI18n();
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState("");
  const [confirmingAction, setConfirmingAction] =
    useState<ConfirmableGoalAction | null>(null);
  const [busy, setBusy] = useState<GoalBusyAction | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [internalExpanded, setInternalExpanded] = useState(false);
  const expanded = controlledExpanded ?? internalExpanded;
  const [actionsOpen, setActionsOpen] = useState(false);
  const [, setElapsedTick] = useState(0);
  const inputRef = useRef<HTMLTextAreaElement | null>(null);
  const controlsRef = useRef<HTMLDivElement | null>(null);
  const actionsAnchorRef = useRef<HTMLSpanElement | null>(null);
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
    setInternalExpanded(false);
    setActionsOpen(false);
    clearConfirmTimer();
  }, [summary?.id]);

  useEffect(() => {
    if (!actionsOpen) return;

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
  }, [actionsOpen]);

  useEffect(() => {
    if (!expanded || !summary?.running_since) return;
    const timer = window.setInterval(() => {
      setElapsedTick((tick) => tick + 1);
    }, 1000);
    return () => window.clearInterval(timer);
  }, [expanded, summary?.running_since]);

  useEffect(() => {
    return () => {
      if (confirmTimerRef.current) {
        clearTimeout(confirmTimerRef.current);
      }
    };
  }, []);

  if (!summary) return null;

  const activeSummary = summary;
  const displayText = goalStripDisplayText(summary.text);
  const visibleStatus = goalVisibleStatusText(summary);
  const infoRows = goalInfoRows(summary, Date.now());
  const canPause = summary.can_pause === true;
  const canResume = summary.can_resume === true;
  const canClear = summary.can_clear === true;

  function clearConfirmTimer(): void {
    if (!confirmTimerRef.current) return;
    clearTimeout(confirmTimerRef.current);
    confirmTimerRef.current = null;
  }

  function resetConfirmation(): void {
    setConfirmingAction(null);
    clearConfirmTimer();
  }

  function setExpanded(next: boolean): void {
    if (controlledExpanded === undefined) {
      setInternalExpanded(next);
    }
    onExpandedChange?.(next);
  }

  function closePopovers(): void {
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
    runAction("pause", onPause, t("goal.pauseFailed"));
  }

  function handleResumeGoal(): void {
    setActionsOpen(false);
    runAction("resume", onResume, t("goal.resumeFailed"));
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
    runAction(action, onClear, t("goal.clearFailed"));
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
        setExpanded(true);
      } finally {
        setBusy(null);
      }
    })();
  }

  async function handleSubmitEdit(): Promise<void> {
    const next = draft.trim();
    if (!next) {
      setError(t("goal.emptyText"));
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
      setError(cause instanceof Error ? cause.message : t("goal.updateFailed"));
    } finally {
      setBusy(null);
    }
  }

  function handleEditKeyDown(event: KeyboardEvent<HTMLTextAreaElement>): void {
    if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
      event.preventDefault();
      void handleSubmitEdit();
    }
  }

  return (
    <>
      <section
        className={`composer-goal-strip composer-accessory-drawer${expanded ? " expanded" : ""}`}
        role="status"
        aria-live="polite"
      >
        <div className="composer-goal-strip-summary">
          <span className="composer-goal-strip-icon" aria-hidden="true">
            <Target className="icon-sm" />
          </span>
          <span className="composer-goal-strip-main">
            <span className="composer-goal-strip-text" title={displayText}>
              {displayText}
            </span>
            {visibleStatus ? (
              <span className="composer-goal-strip-state">{visibleStatus}</span>
            ) : null}
          </span>
          <div
            ref={controlsRef}
            className="composer-goal-strip-actions composer-input-header-actions"
          >
            <button
              className="composer-goal-strip-action composer-goal-strip-edit composer-input-header-action"
              type="button"
              aria-label={t("goal.edit")}
              title={t("goal.edit")}
              disabled={disabled || busy !== null}
              onClick={handleStartEdit}
            >
              <Pencil className="icon-sm" aria-hidden="true" />
            </button>
            <span ref={actionsAnchorRef} className="composer-goal-strip-control-anchor">
              <button
                className="composer-goal-strip-action composer-input-header-action"
                type="button"
                aria-label={t("goal.actions")}
                aria-expanded={actionsOpen}
                title={t("goal.actions")}
                disabled={disabled || busy !== null}
                onClick={() => setActionsOpen((current) => !current)}
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
                        label={t("goal.pause")}
                        onClick={handlePauseGoal}
                      />
                    ) : null}
                    {canResume ? (
                      <GoalMenuButton
                        icon={Play}
                        label={t("goal.resume")}
                        onClick={handleResumeGoal}
                      />
                    ) : null}
                    {canClear ? (
                      <GoalMenuButton
                        danger={confirmingAction === "clear"}
                        icon={Trash2}
                        label={
                          confirmingAction === "clear"
                            ? t("goal.confirmClear")
                            : t("goal.clear")
                        }
                        onClick={() => handleConfirmableGoalAction("clear")}
                      />
                    ) : null}
                  </div>
                </FloatingMenuPortal>
              ) : null}
            </span>
            <button
              className="composer-goal-strip-action composer-goal-strip-toggle composer-input-header-action"
              type="button"
              aria-controls={`composer-goal-details-${summary.id}`}
              aria-expanded={expanded}
              aria-label={t(expanded ? "goal.collapse" : "goal.expand")}
              title={t(expanded ? "goal.collapse" : "goal.expand")}
              onClick={() => {
                setActionsOpen(false);
                setExpanded(!expanded);
              }}
            >
              {expanded ? (
                <ChevronDown className="icon-sm" aria-hidden="true" />
              ) : (
                <ChevronUp className="icon-sm" aria-hidden="true" />
              )}
            </button>
          </div>
        </div>
        {expanded ? (
          <div
            className="composer-goal-strip-details"
            id={`composer-goal-details-${summary.id}`}
          >
            <p className="composer-goal-strip-full-text">
              {summary.text.trim() || t("goal.noText")}
            </p>
            <dl className="composer-goal-strip-info-list">
              {infoRows.map((row) => (
                <div key={row.label} className="composer-goal-strip-info-row">
                  <dt>{row.label}</dt>
                  <dd>{row.value}</dd>
                </div>
              ))}
            </dl>
            {error && !editing ? (
              <span className="composer-goal-strip-error" role="alert">
                {error}
              </span>
            ) : null}
          </div>
        ) : null}
      </section>
      {editing
        ? createPortal(
            <Modal
              ariaLabel={t("goal.edit")}
              icon={<Target className="icon-lg" />}
              title={t("goal.edit")}
              onClose={handleCancelEdit}
              closeDisabled={busy === "edit"}
              panelClassName="composer-goal-edit-dialog"
              asForm
              onSubmit={() => void handleSubmitEdit()}
              footer={
                <>
                  <button
                    className="secondary-button"
                    type="button"
                    disabled={busy === "edit"}
                    onClick={handleCancelEdit}
                  >
                    {t("common.cancel")}
                  </button>
                  <button
                    className="primary-button composer-goal-edit-save"
                    type="submit"
                    disabled={busy === "edit"}
                  >
                    {busy === "edit" ? (
                      <Loader2
                        className="icon-sm composer-goal-strip-spin"
                        aria-hidden="true"
                      />
                    ) : null}
                    <span>{busy === "edit" ? t("common.saving") : t("common.save")}</span>
                  </button>
                </>
              }
            >
              <label className="composer-goal-edit-field">
                <span>{t("goal.content")}</span>
                <textarea
                  ref={inputRef}
                  className="composer-goal-edit-textarea"
                  value={draft}
                  spellCheck={false}
                  disabled={busy === "edit"}
                  rows={10}
                  onChange={(event) => setDraft(event.target.value)}
                  onKeyDown={handleEditKeyDown}
                  aria-label={t("goal.content")}
                />
              </label>
              {error ? (
                <div className="environment-dialog-error" role="alert">
                  {error}
                </div>
              ) : null}
            </Modal>,
            document.body,
          )
        : null}
    </>
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
  return firstLine || translate("goal.noText");
}

function goalStatusKey(summary: ComposerGoalSummary): string {
  return (summary.status || "").trim().toLowerCase();
}

function goalVisibleStatusText(summary: ComposerGoalSummary): string {
  const status = goalStatusKey(summary);
  if (status === "paused") return translate("goal.paused");
  if (status === "blocked") return translate("goal.blocked");
  return "";
}

function goalStatusText(summary: ComposerGoalSummary): string {
  const raw = goalStatusKey(summary);
  switch (raw) {
    case "":
    case "active":
    case "running":
      return translate("goal.running");
    case "paused":
      return translate("goal.paused");
    case "blocked":
      return translate("goal.blocked");
    default:
      return translate("goal.unknownStatus");
  }
}

function goalInfoRows(summary: ComposerGoalSummary, nowMS: number): GoalInfoRow[] {
  const rows: GoalInfoRow[] = [
    { label: translate("goal.status"), value: goalStatusText(summary) },
    {
      label: translate("goal.elapsed"),
      value: formatGoalDuration(goalRunningSeconds(summary, nowMS)),
    },
  ];
  if ((summary.goal_turns ?? 0) > 0) {
    const turns = summary.goal_turns ?? 0;
    rows.push({
      label: translate("goal.turnsLabel"),
      value: translate(turns === 1 ? "goal.turnCountOne" : "goal.turnCount", { count: formatCurrentNumber(turns) }),
    });
  }
  if ((summary.tokens_used ?? 0) > 0) {
    rows.push({
      label: translate("goal.tokens"),
      value: formatCompactNumber(summary.tokens_used ?? 0),
    });
  }
  const blocker = summary.blocker?.trim() ?? "";
  if (blocker) {
    rows.push({ label: translate("goal.blocker"), value: blocker });
  }
  const progress = summary.recent_progress?.trim() ?? "";
  if (progress) {
    rows.push({ label: translate("goal.recentProgress"), value: progress });
  }
  return rows;
}

function goalRunningSeconds(summary: ComposerGoalSummary, nowMS: number): number {
  const completedSeconds = Math.max(0, summary.time_used_seconds ?? 0);
  const runningSinceMS = Date.parse(summary.running_since ?? "");
  if (!Number.isFinite(runningSinceMS) || nowMS <= runningSinceMS) {
    return completedSeconds;
  }
  return completedSeconds + Math.floor((nowMS - runningSinceMS) / 1000);
}

function formatCompactNumber(value: number): string {
  return formatCurrentNumber(Math.max(0, value));
}

function formatGoalDuration(totalSeconds: number): string {
  const seconds = Math.max(0, Math.floor(totalSeconds));
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const remainingSeconds = seconds % 60;
  if (hours > 0) {
    return `${formatGoalDurationUnit(hours, "hour")} ${formatGoalDurationUnit(minutes, "minute")}`;
  }
  if (minutes > 0) {
    return `${formatGoalDurationUnit(minutes, "minute")} ${formatGoalDurationUnit(remainingSeconds, "second")}`;
  }
  return formatGoalDurationUnit(remainingSeconds, "second");
}

function formatGoalDurationUnit(value: number, unit: "hour" | "minute" | "second"): string {
  const suffix = value === 1 ? "One" : "";
  return translate(`goal.duration.${unit}${suffix}` as
    | "goal.duration.hour"
    | "goal.duration.hourOne"
    | "goal.duration.minute"
    | "goal.duration.minuteOne"
    | "goal.duration.second"
    | "goal.duration.secondOne", { count: formatCurrentNumber(value) });
}
