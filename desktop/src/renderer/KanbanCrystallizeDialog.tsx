import {
  Loader2,
  Package,
  Trash2,
  X,
} from "lucide-react";
import {
  useEffect,
  useMemo,
  useState,
  type ChangeEvent,
  type JSX,
  type MouseEvent,
} from "react";
import type {
  KanbanCrystallizeResult,
  KanbanCrystallizeSubtask,
  ParticipantProfile,
} from "../shared/protocol";
import { useI18n } from "./i18n";

export type KanbanCrystallizeDialogProps = {
  threadId: string;
  isOpen: boolean;
  pending: boolean;
  result?: KanbanCrystallizeResult;
  participants: ParticipantProfile[];
  onClose: () => void;
  onSwitchToBoard: () => void;
};

type EditableSubtask = KanbanCrystallizeSubtask & {
  selectedTargetId: string;
};

export function KanbanCrystallizeDialog({
  threadId,
  isOpen,
  pending,
  result,
  participants,
  onClose,
  onSwitchToBoard,
}: KanbanCrystallizeDialogProps): JSX.Element | null {
  const { t } = useI18n();
  const [parentTitle, setParentTitle] = useState(result?.task.title ?? "");
  const [parentBrief, setParentBrief] = useState(result?.task.brief ?? "");
  const [subtasks, setSubtasks] = useState<EditableSubtask[]>([]);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (result) {
      setParentTitle(result.task.title);
      setParentBrief(result.task.brief ?? "");
      setSubtasks(
        result.subtasks.map((subtask) => ({
          ...subtask,
          selectedTargetId:
            subtask.suggested_target_id ??
            participants[0]?.id ??
            "",
        })),
      );
    }
  }, [result, participants]);

  const unassignedOption = useMemo(
    () => ({ value: "", label: t("kanban.crystallize.unassigned") }),
    [t],
  );

  const handleConfirm = async (withDispatch: boolean) => {
    if (!result) return;
    setSubmitting(true);
    try {
      const taskId = result.task.id;
      await window.wuu.kanbanTransitionTask(taskId, "ready");
      const keptSubtasks = subtasks.filter((s) => s.selectedTargetId !== "");
      for (const subtask of keptSubtasks) {
        await window.wuu.kanbanTransitionTask(subtask.id, "ready");
      }
      if (withDispatch) {
        for (const subtask of keptSubtasks) {
          await window.wuu.kanbanDispatchRun({
            thread_id: threadId,
            task_id: subtask.id,
            target_id: subtask.selectedTargetId,
            kind: "execute",
          });
        }
      }
      onSwitchToBoard();
    } catch (error) {
      // eslint-disable-next-line no-console
      console.error("crystallize confirm failed", error);
    } finally {
      setSubmitting(false);
    }
  };

  const updateSubtask = (
    index: number,
    patch: Partial<EditableSubtask>,
  ) => {
    setSubtasks((current) =>
      current.map((subtask, i) => (i === index ? { ...subtask, ...patch } : subtask)),
    );
  };

  const removeSubtask = (index: number) => {
    setSubtasks((current) => current.filter((_, i) => i !== index));
  };

  if (!isOpen) return null;

  return (
    <div className="kanban-crystallize-dialog-overlay" onClick={onClose}>
      <div
        className="kanban-crystallize-dialog"
        role="dialog"
        aria-modal="true"
        aria-label={t("kanban.crystallize.title")}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="kanban-crystallize-dialog-header">
          <h3 className="kanban-crystallize-dialog-title">
            <Package size={20} aria-hidden="true" />
            {t("kanban.crystallize.title")}
          </h3>
          <button
            className="kanban-crystallize-dialog-close"
            type="button"
            onClick={onClose}
            aria-label={t("common.close")}
            disabled={submitting}
          >
            <X size={16} />
          </button>
        </div>

        <div className="kanban-crystallize-dialog-body">
          {pending || !result ? (
            <div className="kanban-crystallize-pending">
              <Loader2 className="kanban-crystallize-pending-spinner" size={32} />
              <span>{t("kanban.crystallize.pending")}</span>
            </div>
          ) : (
            <>
              <div className="kanban-crystallize-section">
                <h4 className="kanban-crystallize-section-title">
                  {t("kanban.crystallize.parentTask")}
                </h4>
                <label className="kanban-crystallize-field-label">
                  {t("kanban.crystallize.titleLabel")}
                </label>
                <input
                  className="kanban-crystallize-input"
                  type="text"
                  value={parentTitle}
                  onChange={(e) => setParentTitle(e.target.value)}
                  disabled={submitting}
                />
                <label className="kanban-crystallize-field-label">
                  {t("kanban.crystallize.briefLabel")}
                </label>
                <textarea
                  className="kanban-crystallize-textarea"
                  value={parentBrief}
                  onChange={(e) => setParentBrief(e.target.value)}
                  disabled={submitting}
                />
              </div>

              <div className="kanban-crystallize-section">
                <h4 className="kanban-crystallize-section-title">
                  {t("kanban.crystallize.subtasks")}
                </h4>
                {subtasks.length === 0 ? (
                  <div className="kanban-crystallize-subtask-empty">
                    {t("kanban.crystallize.noSubtasks")}
                  </div>
                ) : (
                  <div className="kanban-crystallize-subtask-list">
                    {subtasks.map((subtask, index) => (
                      <div key={subtask.id} className="kanban-crystallize-subtask">
                        <div className="kanban-crystallize-subtask-header">
                          <span className="kanban-crystallize-subtask-title">
                            {t("kanban.crystallize.subtaskNumber", { number: index + 1 })}
                          </span>
                          <button
                            className="kanban-crystallize-subtask-remove"
                            type="button"
                            aria-label={t("kanban.crystallize.removeSubtask")}
                            onClick={() => removeSubtask(index)}
                            disabled={submitting}
                          >
                            <Trash2 size={14} />
                          </button>
                        </div>
                        <input
                          className="kanban-crystallize-input"
                          type="text"
                          value={subtask.title}
                          onChange={(e: ChangeEvent<HTMLInputElement>) =>
                            updateSubtask(index, { title: e.target.value })
                          }
                          disabled={submitting}
                        />
                        <textarea
                          className="kanban-crystallize-textarea"
                          value={subtask.brief ?? ""}
                          onChange={(e: ChangeEvent<HTMLTextAreaElement>) =>
                            updateSubtask(index, { brief: e.target.value })
                          }
                          disabled={submitting}
                        />
                        <select
                          className="kanban-crystallize-subtask-select"
                          value={subtask.selectedTargetId}
                          onChange={(e: ChangeEvent<HTMLSelectElement>) =>
                            updateSubtask(index, { selectedTargetId: e.target.value })
                          }
                          disabled={submitting}
                        >
                          <option value="">{unassignedOption.label}</option>
                          {participants.map((p) => (
                            <option key={p.id} value={p.id}>
                              {p.name}
                            </option>
                          ))}
                        </select>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </>
          )}
        </div>

        <div className="kanban-crystallize-dialog-footer">
          <button
            className="kanban-crystallize-button"
            type="button"
            onClick={onClose}
            disabled={submitting}
          >
            {t("common.cancel")}
          </button>
          <button
            className="kanban-crystallize-button"
            type="button"
            disabled={pending || !result || submitting}
            onClick={(e: MouseEvent<HTMLButtonElement>) => {
              e.preventDefault();
              void handleConfirm(false);
            }}
          >
            {t("kanban.crystallize.confirmOnly")}
          </button>
          <button
            className="kanban-crystallize-button kanban-crystallize-button-primary"
            type="button"
            disabled={pending || !result || submitting}
            onClick={(e: MouseEvent<HTMLButtonElement>) => {
              e.preventDefault();
              void handleConfirm(true);
            }}
          >
            {t("kanban.crystallize.confirmAndDispatch")}
          </button>
        </div>
      </div>
    </div>
  );
}
