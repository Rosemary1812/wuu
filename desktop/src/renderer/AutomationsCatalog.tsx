import { ChevronRight, CircleAlert, Pause, Play, RefreshCw, Trash2, X } from "lucide-react";
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
import { createPortal } from "react-dom";
import type { AutomationTask, AutomationUpdateParams } from "../shared/protocol";
import {
  cronForAutomationSchedule,
  defaultCronForScheduleKind,
  nextAutomationExecution,
  parseAutomationSchedule,
  type AutomationScheduleKind,
  type AutomationScheduleValue,
} from "./AutomationSchedule";
import { CatalogSearchField } from "./CatalogSearchField";
import { translateCurrent, useI18n } from "./i18n";
import { TopNotice } from "./TopNotice";

type Filter = "all" | "active" | "paused";
type Translate = ReturnType<typeof useI18n>["t"];

const AUTOMATION_WEEKDAY_KEYS = [
  "automations.weekday.0",
  "automations.weekday.1",
  "automations.weekday.2",
  "automations.weekday.3",
  "automations.weekday.4",
  "automations.weekday.5",
  "automations.weekday.6",
] as const;

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
  const [saveNotice, setSaveNotice] = useState<{ id: number; message: string } | null>(null);
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
      throw reason;
    }
  }

  const showSaveRejectedNotice = useCallback((): void => {
    setSaveNotice({
      id: Date.now(),
      message: t("automations.changesReverted"),
    });
  }, [t]);

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
              <span
                className={`automation-state${task.paused ? " paused" : ""}`}
                role="img"
                aria-label={task.paused ? t("automations.paused") : t("automations.active")}
              />
              <span className="automation-row-copy">
                <strong>{task.title || task.prompt || task.id}</strong>
                <span>{automationScheduleSummary(task.cron, task.timezone, t)}</span>
              </span>
              <ChevronRight className="automation-row-chevron" aria-hidden="true" />
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
              onSaveRejected={showSaveRejectedNotice} onClose={() => setSelectedID("")} />
          </div>
        </>
      ) : null}
      {saveNotice ? createPortal(
        <TopNotice
          key={saveNotice.id}
          message={saveNotice.message}
          icon={CircleAlert}
          isError
          dismissAriaLabel={t("common.closeNotice")}
          onDismiss={() => setSaveNotice(null)}
        />,
        document.body,
      ) : null}
    </section>
  );
}

type AutomationDraft = {
  title: string;
  prompt: string;
  schedule: string;
  timezone: string;
  mode: "new_thread" | "thread_heartbeat";
  heartbeatThreadID: string;
  recurring: boolean;
};

function draftFromAutomation(task: AutomationTask): AutomationDraft {
  return {
    title: task.title ?? "",
    prompt: task.prompt ?? "",
    schedule: task.cron,
    timezone: task.timezone ?? "",
    mode: task.mode ?? "new_thread",
    heartbeatThreadID: task.heartbeatThreadId ?? "",
    recurring: task.recurring,
  };
}

function draftsMatch(left: AutomationDraft, right: AutomationDraft): boolean {
  return Object.keys(left).every((key) => (
    left[key as keyof AutomationDraft] === right[key as keyof AutomationDraft]
  ));
}

function automationScheduleSummary(cron: string, timezone: string | undefined, t: Translate): string {
  const interval = automationScheduleInterval(cron, t);
  const next = automationNextExecutionText(cron, timezone, t);
  return `${interval} · ${t("automations.nextExecutionShort", { time: next })}`;
}

function automationScheduleInterval(cron: string, t: Translate): string {
  const schedule = parseAutomationSchedule(cron);
  switch (schedule.kind) {
    case "minutes":
      return t("automations.intervalMinutes", { count: schedule.interval });
    case "hourly":
      return t("automations.frequency.hourly");
    case "daily":
      return t("automations.frequency.daily");
    case "weekdays":
      return t("automations.frequency.weekdays");
    case "weekly":
      return t("automations.frequency.weekly");
    case "custom":
      return t("automations.frequency.custom");
  }
}

function automationNextExecutionText(cron: string, timezone: string | undefined, t: Translate): string {
  const next = nextAutomationExecution(cron, timezone);
  if (!next) return t("automations.nextExecutionUnavailable");
  if (next.dayOffset === 0) return t("automations.nextExecutionToday", { time: next.time });
  if (next.dayOffset === 1) return t("automations.nextExecutionTomorrow", { time: next.time });
  return t("automations.nextExecutionWeekday", {
    day: t(AUTOMATION_WEEKDAY_KEYS[next.weekday]),
    time: next.time,
  });
}

function AutomationScheduleEditor({
  schedule,
  timezone,
  onScheduleChange,
  onCommit,
}: {
  schedule: string;
  timezone: string;
  onScheduleChange: (schedule: string, commit: boolean) => void;
  onCommit: () => void;
}): JSX.Element {
  const { t } = useI18n();
  const parsed = parseAutomationSchedule(schedule);
  const [kind, setKind] = useState<AutomationScheduleKind>(parsed.kind);
  const editor: AutomationScheduleValue = kind === "custom"
    ? { ...parsed, kind, cron: schedule }
    : parsed;

  useEffect(() => {
    if (kind !== "custom" && parsed.kind !== "custom" && parsed.kind !== kind) {
      setKind(parsed.kind);
    }
  }, [kind, parsed.kind]);

  function updateCommon(next: AutomationScheduleValue): void {
    onScheduleChange(cronForAutomationSchedule(next), true);
  }

  return (
    <div className="automation-schedule-editor">
      <div className="automation-schedule-controls">
        <label>
          <span>{t("automations.frequency")}</span>
          <select
            className="settings-select"
            value={kind}
            onChange={(event) => {
              const nextKind = event.currentTarget.value as AutomationScheduleKind;
              setKind(nextKind);
              if (nextKind !== "custom") {
                onScheduleChange(defaultCronForScheduleKind(nextKind, schedule), true);
              }
            }}
          >
            {(["minutes", "hourly", "daily", "weekdays", "weekly", "custom"] as const).map((value) => (
              <option key={value} value={value}>{t(`automations.frequency.${value}`)}</option>
            ))}
          </select>
        </label>
        {kind === "minutes" ? (
          <label>
            <span>{t("automations.interval")}</span>
            <select className="settings-select" value={editor.interval} onChange={(event) => {
              updateCommon({ ...editor, kind, interval: Number(event.currentTarget.value) });
            }}>
              {[5, 10, 15, 30].map((value) => (
                <option key={value} value={value}>{t("automations.intervalMinutes", { count: value })}</option>
              ))}
            </select>
          </label>
        ) : null}
        {kind === "hourly" ? (
          <label>
            <span>{t("automations.minuteOfHour")}</span>
            <select className="settings-select" value={editor.minute} onChange={(event) => {
              updateCommon({ ...editor, kind, minute: Number(event.currentTarget.value) });
            }}>
              {Array.from({ length: 60 }, (_, value) => (
                <option key={value} value={value}>{String(value).padStart(2, "0")}</option>
              ))}
            </select>
          </label>
        ) : null}
        {kind === "daily" || kind === "weekdays" || kind === "weekly" ? (
          <label>
            <span>{t("automations.runTime")}</span>
            <input className="settings-input" type="time" value={editor.time} onChange={(event) => {
              updateCommon({ ...editor, kind, time: event.currentTarget.value });
            }} />
          </label>
        ) : null}
        {kind === "weekly" ? (
          <label>
            <span>{t("automations.weekday")}</span>
            <select className="settings-select" value={editor.weekday} onChange={(event) => {
              updateCommon({ ...editor, kind, weekday: Number(event.currentTarget.value) });
            }}>
              {Array.from({ length: 7 }, (_, value) => (
                <option key={value} value={value}>{t(AUTOMATION_WEEKDAY_KEYS[value])}</option>
              ))}
            </select>
          </label>
        ) : null}
        {kind === "custom" ? (
          <label className="automation-schedule-custom">
            <span>{t("automations.scheduleCustom")}</span>
            <input className="settings-input" value={schedule}
              onChange={(event) => onScheduleChange(event.currentTarget.value, false)}
              onBlur={onCommit} />
          </label>
        ) : null}
      </div>
      <dl className="automation-schedule-summary">
        <div>
          <dt>{t("automations.frequency")}</dt>
          <dd>{automationScheduleInterval(schedule, t)}</dd>
        </div>
        <div>
          <dt>{t("automations.nextExecution")}</dt>
          <dd>{automationNextExecutionText(schedule, timezone, t)}</dd>
        </div>
      </dl>
    </div>
  );
}

function AutomationDetail({ task, onUpdate, onRemove, onSaveRejected, onClose }: {
  task: AutomationTask;
  onUpdate: (params: AutomationUpdateParams) => Promise<void>;
  onRemove: (task: AutomationTask) => Promise<void>;
  onSaveRejected: () => void;
  onClose: () => void;
}): JSX.Element {
  const { t } = useI18n();
  const initialDraft = useMemo(() => draftFromAutomation(task), [task.id]);
  const [draft, setDraft] = useState<AutomationDraft>(initialDraft);
  const latestDraftRef = useRef(initialDraft);
  const lastSavedDraftRef = useRef(initialDraft);
  const saveQueueRef = useRef<Promise<boolean>>(Promise.resolve(true));

  function updateDraft(next: AutomationDraft): void {
    latestDraftRef.current = next;
    setDraft(next);
  }

  function persistDraft(candidate = latestDraftRef.current): Promise<boolean> {
    const snapshot = { ...candidate };
    const queued = saveQueueRef.current.then(async () => {
      if (draftsMatch(snapshot, lastSavedDraftRef.current)) return true;
      try {
        await onUpdate({
          id: task.id,
          title: snapshot.title,
          prompt: snapshot.prompt,
          schedule: snapshot.schedule,
          timezone: snapshot.timezone,
          mode: snapshot.mode,
          heartbeat_thread_id: snapshot.heartbeatThreadID,
          recurring: snapshot.recurring,
        });
        lastSavedDraftRef.current = snapshot;
        return true;
      } catch {
        if (draftsMatch(latestDraftRef.current, snapshot)) {
          const fallback = { ...lastSavedDraftRef.current };
          latestDraftRef.current = fallback;
          setDraft(fallback);
        }
        onSaveRejected();
        return false;
      }
    });
    saveQueueRef.current = queued;
    return queued;
  }

  async function closeDetails(): Promise<void> {
    await persistDraft();
    onClose();
  }

  async function togglePaused(): Promise<void> {
    await persistDraft();
    try {
      await onUpdate({ id: task.id, paused: !task.paused });
    } catch {
      onSaveRejected();
    }
  }

  return (
    <div className="automation-detail-form">
      <section className="automation-detail-section">
        <div className="automation-detail-name-row">
          <label>
            <span>{t("automations.name")}</span>
            <input
              className="settings-input"
              value={draft.title}
              onChange={(event) => updateDraft({ ...latestDraftRef.current, title: event.currentTarget.value })}
              onBlur={() => void persistDraft()}
            />
          </label>
        <div className="automation-detail-actions">
          <button className="icon-button" type="button" aria-label={task.paused ? t("automations.resume") : t("automations.pause")}
            onClick={() => void togglePaused()}>
            {task.paused ? <Play className="icon" /> : <Pause className="icon" />}
          </button>
          <button className="icon-button danger" type="button" aria-label={t("automations.delete")}
            onClick={() => void onRemove(task)}><Trash2 className="icon" /></button>
          <button className="icon-button" type="button" aria-label={t("automations.closeDetails")} onClick={() => void closeDetails()}>
            <X className="icon" />
          </button>
        </div>
        </div>
        <label>
          <span>{t("automations.prompt")}</span>
          <textarea
            className="settings-input settings-textarea"
            rows={7}
            value={draft.prompt}
            onChange={(event) => updateDraft({ ...latestDraftRef.current, prompt: event.currentTarget.value })}
            onBlur={() => void persistDraft()}
          />
        </label>
      </section>
      <section className="automation-detail-section">
        <div className="automation-detail-grid">
          <AutomationScheduleEditor
            schedule={draft.schedule}
            timezone={draft.timezone}
            onScheduleChange={(schedule, commit) => {
              const next = { ...latestDraftRef.current, schedule };
              updateDraft(next);
              if (commit) void persistDraft(next);
            }}
            onCommit={() => void persistDraft()}
          />
          <label>
            <span>{t("automations.timezone")}</span>
            <input className="settings-input" value={draft.timezone}
              onChange={(event) => updateDraft({ ...latestDraftRef.current, timezone: event.currentTarget.value })}
              onBlur={() => void persistDraft()} />
          </label>
          <label>
            <span>{t("automations.mode")}</span>
            <select className="settings-select" value={draft.mode} onChange={(event) => {
              const next = { ...latestDraftRef.current, mode: event.currentTarget.value as AutomationDraft["mode"] };
              updateDraft(next);
              void persistDraft(next);
            }}>
              <option value="new_thread">{t("automations.mode.newThread")}</option>
              <option value="thread_heartbeat">{t("automations.mode.heartbeat")}</option>
            </select>
          </label>
          <label className="automation-checkbox">
            <input type="checkbox" checked={draft.recurring} onChange={(event) => {
              const next = { ...latestDraftRef.current, recurring: event.currentTarget.checked };
              updateDraft(next);
              void persistDraft(next);
            }} />
            <span>{t("automations.recurring")}</span>
          </label>
        </div>
        {draft.mode === "thread_heartbeat" ? (
          <label>
            <span>{t("automations.heartbeatThread")}</span>
            <input className="settings-input" value={draft.heartbeatThreadID}
              onChange={(event) => updateDraft({ ...latestDraftRef.current, heartbeatThreadID: event.currentTarget.value })}
              onBlur={() => void persistDraft()} />
          </label>
        ) : null}
      </section>
    </div>
  );
}
