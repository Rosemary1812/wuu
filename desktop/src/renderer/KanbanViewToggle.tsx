import type { JSX } from "react";
import { useI18n } from "./i18n";

export type KanbanViewMode = "message" | "board";

export type KanbanViewToggleProps = {
  mode: KanbanViewMode;
  onChange: (mode: KanbanViewMode) => void;
};

export function KanbanViewToggle({
  mode,
  onChange,
}: KanbanViewToggleProps): JSX.Element {
  const { t } = useI18n();

  return (
    <div className="kanban-view-toggle" role="tablist" aria-label={t("shell.taskBoard")}>
      <button
        type="button"
        role="tab"
        className={`kanban-view-toggle-button${mode === "message" ? " active" : ""}`}
        aria-selected={mode === "message"}
        onClick={() => onChange("message")}
      >
        {t("shell.viewMessages")}
      </button>
      <button
        type="button"
        role="tab"
        className={`kanban-view-toggle-button${mode === "board" ? " active" : ""}`}
        aria-selected={mode === "board"}
        onClick={() => onChange("board")}
      >
        {t("shell.viewBoard")}
      </button>
    </div>
  );
}
