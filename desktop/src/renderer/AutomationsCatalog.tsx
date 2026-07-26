import { Clock3, Pause, Play, RefreshCw, Trash2, X } from "lucide-react";
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type KeyboardEvent as ReactKeyboardEvent,
  type PointerEvent as ReactPointerEvent,
} from "react";
import type { AutomationTask, AutomationUpdateParams } from "../shared/protocol";
import { CatalogSearchField } from "./CatalogSearchField";
import { translateCurrent, useI18n } from "./i18n";

type Filter = "all" | "active" | "paused";

export type AutomationDetailPaneLayout = {
  open: boolean;
  reservedWidth: number;
};

const AUTOMATION_DETAIL_WIDTH_KEY = "wuu.desktop.automationDetailPaneWidth";
const AUTOMATION_DETAIL_DEFAULT_WIDTH = 520;
const AUTOMATION_DETAIL_MIN_WIDTH = 360;
const AUTOMATION_DETAIL_MAX_WIDTH = 760;
const AUTOMATION_MASTER_MIN_WIDTH = 280;
const AUTOMATION_DETAIL_RESIZER_WIDTH = 10;
const AUTOMATION_DETAIL_WIDTH_STEP = 32;

function clampDetailWidth(width: number, containerWidth?: number): number {
  const availableWidth = containerWidth
    ? containerWidth - AUTOMATION_MASTER_MIN_WIDTH - AUTOMATION_DETAIL_RESIZER_WIDTH
    : AUTOMATION_DETAIL_MAX_WIDTH;
  const maxWidth = Math.max(
    AUTOMATION_DETAIL_MIN_WIDTH,
    Math.min(AUTOMATION_DETAIL_MAX_WIDTH, availableWidth),
  );
  return Math.min(maxWidth, Math.max(AUTOMATION_DETAIL_MIN_WIDTH, Math.round(width)));
}

function initialDetailWidth(): number {
  const stored = Number(window.localStorage.getItem(AUTOMATION_DETAIL_WIDTH_KEY));
  return Number.isFinite(stored) && stored >= AUTOMATION_DETAIL_MIN_WIDTH
    ? clampDetailWidth(stored)
    : AUTOMATION_DETAIL_DEFAULT_WIDTH;
}

export function AutomationsCatalog({
  onDetailPaneLayoutChange,
}: {
  onDetailPaneLayoutChange?: (layout: AutomationDetailPaneLayout) => void;
}): JSX.Element {
  const { t } = useI18n();
  const catalogRef = useRef<HTMLElement>(null);
  const [tasks, setTasks] = useState<AutomationTask[]>([]);
  const [selectedID, setSelectedID] = useState("");
  const [filter, setFilter] = useState<Filter>("all");
  const [query, setQuery] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [detailWidth, setDetailWidth] = useState(initialDetailWidth);
  const [resizingDetail, setResizingDetail] = useState(false);
  const resizeStartRef = useRef({ x: 0, width: AUTOMATION_DETAIL_DEFAULT_WIDTH });

  async function load(): Promise<void> {
    setLoading(true);
    setError("");
    try {
      const result = await window.wuu.listAutomations();
      setTasks(result.tasks ?? []);
      setSelectedID((current) => result.tasks.some((task) => task.id === current) ? current : "");
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : translateCurrent("automations.loadFailed"));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => { void load(); }, []);

  const visibleTasks = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return tasks.filter((task) => {
      if (filter === "active" && task.paused) return false;
      if (filter === "paused" && !task.paused) return false;
      return !needle || [task.title, task.prompt, task.cron, task.timezone]
        .some((value) => value?.toLowerCase().includes(needle));
    });
  }, [filter, query, tasks]);
  const selected = tasks.find((task) => task.id === selectedID);

  const updateDetailWidth = useCallback((width: number): void => {
    const nextWidth = clampDetailWidth(width, catalogRef.current?.clientWidth);
    setDetailWidth(nextWidth);
    window.localStorage.setItem(AUTOMATION_DETAIL_WIDTH_KEY, String(nextWidth));
  }, []);

  useEffect(() => {
    onDetailPaneLayoutChange?.({
      open: Boolean(selectedID),
      reservedWidth: selectedID ? detailWidth + AUTOMATION_DETAIL_RESIZER_WIDTH : 0,
    });
  }, [detailWidth, onDetailPaneLayoutChange, selectedID]);

  useEffect(() => {
    if (!resizingDetail) return;
    const handlePointerMove = (event: PointerEvent): void => {
      updateDetailWidth(resizeStartRef.current.width - (event.clientX - resizeStartRef.current.x));
    };
    const handlePointerUp = (): void => setResizingDetail(false);
    window.addEventListener("pointermove", handlePointerMove);
    window.addEventListener("pointerup", handlePointerUp, { once: true });
    return () => {
      window.removeEventListener("pointermove", handlePointerMove);
      window.removeEventListener("pointerup", handlePointerUp);
    };
  }, [resizingDetail, updateDetailWidth]);

  function startDetailResize(event: ReactPointerEvent<HTMLButtonElement>): void {
    event.preventDefault();
    resizeStartRef.current = { x: event.clientX, width: detailWidth };
    setResizingDetail(true);
  }

  function handleDetailResizeKeyDown(event: ReactKeyboardEvent<HTMLButtonElement>): void {
    if (event.key === "ArrowLeft") {
      event.preventDefault();
      updateDetailWidth(detailWidth + AUTOMATION_DETAIL_WIDTH_STEP);
    } else if (event.key === "ArrowRight") {
      event.preventDefault();
      updateDetailWidth(detailWidth - AUTOMATION_DETAIL_WIDTH_STEP);
    } else if (event.key === "Home") {
      event.preventDefault();
      updateDetailWidth(AUTOMATION_DETAIL_MIN_WIDTH);
    } else if (event.key === "End") {
      event.preventDefault();
      updateDetailWidth(AUTOMATION_DETAIL_MAX_WIDTH);
    }
  }

  async function update(params: AutomationUpdateParams): Promise<void> {
    setError("");
    try {
      const result = await window.wuu.updateAutomation(params);
      setTasks((current) => current.map((task) => task.id === result.task.id ? result.task : task));
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : translateCurrent("automations.saveFailed"));
      throw reason;
    }
  }

  async function remove(task: AutomationTask): Promise<void> {
    if (!window.confirm(t("automations.deleteConfirm", { name: task.title || task.prompt || task.id }))) return;
    try {
      await window.wuu.removeAutomation(task.id);
      setTasks((current) => current.filter((candidate) => candidate.id !== task.id));
      setSelectedID("");
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : translateCurrent("automations.deleteFailed"));
    }
  }

  return (
    <section
      ref={catalogRef}
      className={`automations-catalog${selected ? " detail-open" : ""}${resizingDetail ? " resizing-detail" : ""}`}
      aria-label={t("automations.title")}
      style={{ "--automation-detail-pane-width": `${detailWidth}px` } as CSSProperties}
    >
      <div className="automations-master">
        <header className="catalog-page-header">
          <div className="catalog-page-title">
            <strong>{t("automations.title")}</strong>
            <span>{t("automations.subtitle")}</span>
          </div>
          <div className="catalog-page-controls">
            <CatalogSearchField
              value={query}
              placeholder={t("automations.searchPlaceholder")}
              onValueChange={setQuery}
            />
            <button
              className="icon-button catalog-refresh"
              type="button"
              aria-label={t("automations.refresh")}
              onClick={() => void load()}
            >
              <RefreshCw className="icon" />
            </button>
          </div>
        </header>
        <div className="automations-filters" role="tablist" aria-label={t("automations.filterLabel")}>
          {(["all", "active", "paused"] as const).map((value) => (
            <button key={value} className={filter === value ? "active" : ""} type="button"
              role="tab" aria-selected={filter === value} onClick={() => setFilter(value)}>
              {t(`automations.filter.${value}`)}
            </button>
          ))}
        </div>
        {error ? <div className="automations-error">{error}</div> : null}
        <div className="automations-list">
          {loading ? <div className="automations-empty">{t("automations.loading")}</div> : null}
          {!loading && visibleTasks.length === 0 ? <div className="automations-empty">{t("automations.empty")}</div> : null}
          {visibleTasks.map((task) => (
            <button key={task.id} type="button" className={`automation-row${task.id === selectedID ? " selected" : ""}`}
              onClick={() => setSelectedID(task.id)}>
              <span className={`automation-state${task.paused ? " paused" : ""}`}><Clock3 /></span>
              <span className="automation-row-copy">
                <strong>{task.title || task.prompt || task.id}</strong>
                <span>{task.cron} · {task.timezone || t("automations.localTime")}</span>
              </span>
            </button>
          ))}
        </div>
      </div>
      {selected ? (
        <>
          <button
            className="automations-detail-resizer"
            type="button"
            role="separator"
            aria-label={t("automations.resizeDetails")}
            aria-orientation="vertical"
            aria-valuemin={AUTOMATION_DETAIL_MIN_WIDTH}
            aria-valuemax={AUTOMATION_DETAIL_MAX_WIDTH}
            aria-valuenow={detailWidth}
            onPointerDown={startDetailResize}
            onKeyDown={handleDetailResizeKeyDown}
            onDoubleClick={() => updateDetailWidth(AUTOMATION_DETAIL_DEFAULT_WIDTH)}
          />
          <div className="automations-detail">
            <AutomationDetail key={selected.id} task={selected} onUpdate={update} onRemove={remove}
              onClose={() => setSelectedID("")} />
          </div>
        </>
      ) : null}
    </section>
  );
}

function AutomationDetail({ task, onUpdate, onRemove, onClose }: {
  task: AutomationTask;
  onUpdate: (params: AutomationUpdateParams) => Promise<void>;
  onRemove: (task: AutomationTask) => Promise<void>;
  onClose: () => void;
}): JSX.Element {
  const { t } = useI18n();
  const [draft, setDraft] = useState<{
    title: string; prompt: string; schedule: string; timezone: string;
    mode: "new_thread" | "thread_heartbeat"; heartbeatThreadID: string; recurring: boolean;
  }>({
    title: task.title ?? "", prompt: task.prompt ?? "", schedule: task.cron,
    timezone: task.timezone ?? "", mode: task.mode ?? "new_thread",
    heartbeatThreadID: task.heartbeatThreadId ?? "", recurring: task.recurring,
  });
  const [saving, setSaving] = useState(false);

  async function save(): Promise<void> {
    setSaving(true);
    try {
      await onUpdate({ id: task.id, title: draft.title, prompt: draft.prompt,
        schedule: draft.schedule, timezone: draft.timezone,
        mode: draft.mode as "new_thread" | "thread_heartbeat",
        heartbeat_thread_id: draft.heartbeatThreadID, recurring: draft.recurring });
    } finally { setSaving(false); }
  }

  return (
    <div className="automation-detail-form">
      <header className="automation-detail-header">
        <span className={`automation-status-label${task.paused ? " paused" : ""}`}>
          {task.paused ? t("automations.paused") : t("automations.active")}
        </span>
        <div className="automation-detail-actions">
          <button className="icon-button" type="button" aria-label={task.paused ? t("automations.resume") : t("automations.pause")}
            onClick={() => void onUpdate({ id: task.id, paused: !task.paused })}>
            {task.paused ? <Play className="icon" /> : <Pause className="icon" />}
          </button>
          <button className="icon-button danger" type="button" aria-label={t("automations.delete")}
            onClick={() => void onRemove(task)}><Trash2 className="icon" /></button>
          <button className="icon-button" type="button" aria-label={t("automations.closeDetails")} onClick={onClose}>
            <X className="icon" />
          </button>
        </div>
      </header>
      <label><span>{t("automations.name")}</span><input value={draft.title} onChange={(e) => setDraft({ ...draft, title: e.currentTarget.value })} /></label>
      <label><span>{t("automations.prompt")}</span><textarea rows={7} value={draft.prompt} onChange={(e) => setDraft({ ...draft, prompt: e.currentTarget.value })} /></label>
      <div className="automation-detail-grid">
        <label><span>{t("automations.schedule")}</span><input value={draft.schedule} onChange={(e) => setDraft({ ...draft, schedule: e.currentTarget.value })} /></label>
        <label><span>{t("automations.timezone")}</span><input value={draft.timezone} onChange={(e) => setDraft({ ...draft, timezone: e.currentTarget.value })} /></label>
        <label><span>{t("automations.mode")}</span><select value={draft.mode} onChange={(e) => setDraft({ ...draft, mode: e.currentTarget.value as "new_thread" | "thread_heartbeat" })}>
          <option value="new_thread">{t("automations.mode.newThread")}</option><option value="thread_heartbeat">{t("automations.mode.heartbeat")}</option>
        </select></label>
        <label className="automation-checkbox"><input type="checkbox" checked={draft.recurring} onChange={(e) => setDraft({ ...draft, recurring: e.currentTarget.checked })} /><span>{t("automations.recurring")}</span></label>
      </div>
      {draft.mode === "thread_heartbeat" ? <label><span>{t("automations.heartbeatThread")}</span><input value={draft.heartbeatThreadID} onChange={(e) => setDraft({ ...draft, heartbeatThreadID: e.currentTarget.value })} /></label> : null}
      <footer><button className="settings-button settings-button-primary" type="button" disabled={saving} onClick={() => void save()}>{saving ? t("automations.saving") : t("automations.save")}</button></footer>
    </div>
  );
}
