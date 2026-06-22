import {
  Check,
  Loader2,
  Pencil,
  Target,
  X
} from "lucide-react";
import { type KeyboardEvent, useEffect, useRef, useState } from "react";
import type { ComposerGoalSummary } from "../shared/protocol";

const CANCEL_CONFIRM_WINDOW_MS = 3000;

export function ComposerGoalStrip({
  summary,
  disabled,
  onEdit,
  onCancel
}: {
  summary: ComposerGoalSummary | null;
  disabled?: boolean;
  onEdit: (nextText: string) => void | Promise<void>;
  onCancel: () => void | Promise<void>;
}): JSX.Element | null {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState("");
  const [confirmingCancel, setConfirmingCancel] = useState(false);
  const [busy, setBusy] = useState<null | "edit" | "cancel">(null);
  const [error, setError] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement | null>(null);
  const confirmTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

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
    setConfirmingCancel(false);
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

  function clearConfirmTimer(): void {
    if (confirmTimerRef.current) {
      clearTimeout(confirmTimerRef.current);
      confirmTimerRef.current = null;
    }
  }

  function handleStartEdit(): void {
    if (disabled || busy) return;
    setError(null);
    setConfirmingCancel(false);
    clearConfirmTimer();
    setEditing(true);
  }

  function handleCancelEdit(): void {
    setEditing(false);
    setDraft("");
    setError(null);
  }

  function handleCancelGoal(): void {
    if (disabled || busy) return;
    setError(null);
    if (!confirmingCancel) {
      setConfirmingCancel(true);
      clearConfirmTimer();
      confirmTimerRef.current = setTimeout(() => {
        setConfirmingCancel(false);
        confirmTimerRef.current = null;
      }, CANCEL_CONFIRM_WINDOW_MS);
      return;
    }
    clearConfirmTimer();
    setConfirmingCancel(false);
    setBusy("cancel");
    void (async () => {
      try {
        await onCancel();
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : "取消目标失败");
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
              <Loader2 className="icon-sm composer-goal-strip-spin" aria-hidden="true" />
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
      className={`composer-goal-strip${confirmingCancel ? " confirming-cancel" : ""}`}
      role="status"
      aria-live="polite"
    >
      <span className="composer-goal-strip-icon" aria-hidden="true">
        <Target className="icon" />
      </span>
      <span className="composer-goal-strip-label">Goal</span>
      <span className="composer-goal-strip-text" title={displayText}>
        {displayText}
      </span>
      <div className="composer-goal-strip-actions">
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
        <button
          className={`composer-goal-strip-action${confirmingCancel ? " danger" : ""}`}
          type="button"
          aria-label={confirmingCancel ? "再次点击确认取消目标" : "取消目标"}
          title={confirmingCancel ? "再次点击确认" : "取消目标"}
          disabled={disabled || busy !== null}
          onClick={handleCancelGoal}
        >
          {busy === "cancel" ? (
            <Loader2
              className="icon-sm composer-goal-strip-spin"
              aria-hidden="true"
            />
          ) : (
            <X className="icon-sm" aria-hidden="true" />
          )}
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

function goalStripDisplayText(text: string): string {
  const firstLine = text.trim().split(/\r?\n/, 1)[0]?.trim() ?? "";
  return firstLine || "（无目标文本）";
}
