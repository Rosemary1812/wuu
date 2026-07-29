import { ChevronRight, CircleAlert, Pause, Play, Plus, RefreshCw, Trash2, X } from "lucide-react";
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
import type { AutomationTask, AutomationUpdateParams, DesktopProject } from "../shared/protocol";
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
import { MinuteClockPicker } from "./MinuteClockPicker";
import { SelectMenu, type SelectMenuOption } from "./SelectMenu";
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
const AUTOMATION_DETAIL_DEFAULT_WIDTH = 560;
const AUTOMATION_DETAIL_MIN_WIDTH = 480;
const AUTOMATION_DETAIL_MAX_WIDTH = 760;
const AUTOMATION_MASTER_MIN_WIDTH = 340;
const AUTOMATION_DETAIL_RESIZER_WIDTH = 10;
const AUTOMATION_DETAIL_WIDTH_STEP = 32;

const SUPPORTED_AUTOMATION_TIMEZONES = (() => {
  try {
    return Intl.supportedValuesOf("timeZone");
  } catch {
    return [];
  }
})();
const TIMEZONE_NAME_LOCALES = ["zh-CN", "en-US"] as const;
const timezoneNameCache = new Map<string, string>();

function localizedTimezoneName(timezone: string, locale: string): string {
  const cacheKey = `${locale}:${timezone}`;
  const cached = timezoneNameCache.get(cacheKey);
  if (cached !== undefined) return cached;
  try {
    const name = new Intl.DateTimeFormat(locale, {
      timeZone: timezone,
      timeZoneName: "longGeneric",
    }).formatToParts(new Date(0)).find((part) => part.type === "timeZoneName")?.value ?? "";
    timezoneNameCache.set(cacheKey, name);
    return name;
  } catch {
    timezoneNameCache.set(cacheKey, "");
    return "";
  }
}

function automationTimezoneOptions(
  currentTimezone: string,
  localTimezone: string,
  localTimezoneLabel: string,
  locale: string,
): SelectMenuOption[] {
  const orderedTimezones = [
    localTimezone,
    currentTimezone,
    "UTC",
    ...SUPPORTED_AUTOMATION_TIMEZONES,
  ];
  return [...new Set(orderedTimezones.filter(Boolean))].map((timezone) => {
    const localizedName = localizedTimezoneName(timezone, locale);
    const hint = [...new Set([
      timezone === localTimezone ? localTimezoneLabel : "",
      localizedName,
    ].filter(Boolean))].join(" · ");
    return {
      value: timezone,
      label: timezone,
      hint: hint || undefined,
      keywords: TIMEZONE_NAME_LOCALES.map((nameLocale) => (
        localizedTimezoneName(timezone, nameLocale)
      )).filter(Boolean),
    };
  });
}

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
  projects = [],
  activeProjectID = "",
}: {
  onDetailPaneLayoutChange?: (layout: AutomationDetailPaneLayout) => void;
  projects?: DesktopProject[];
  activeProjectID?: string;
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
  const availableProjects = useMemo(() => projects.filter((project) => !project.missing), [projects]);
  // Non-null while the detail pane shows an unsaved "new automation" draft;
  // the task only exists server-side once the draft's create action succeeds.
  const [pendingNew, setPendingNew] = useState<{ workspaceID: string } | null>(null);
  const resizeStartRef = useRef({ x: 0, width: AUTOMATION_DETAIL_DEFAULT_WIDTH });

  async function load(): Promise<void> {
    setLoading(true);
    setError("");
    try {
      const result = await window.wuu.listAutomations();
      const tasks = result.tasks ?? [];
      setTasks(tasks);
      setSelectedID((current) => (tasks.some((task) => task.id === current) ? current : ""));
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
  // Synthetic task carrying the defaults for the unsaved new-automation draft;
  // AutomationDetail seeds its field draft from it. Never rendered in the list.
  const pendingTask: AutomationTask | null = pendingNew === null ? null : {
    id: "__new__",
    title: t("automations.newTitle"),
    prompt: "",
    cron: "0 9 * * 1-5",
    timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
    mode: "new_thread",
    createdAt: 0,
    recurring: true,
    paused: true,
  };
  const detailTask = selected ?? pendingTask;
  const detailOpen = detailTask != null;

  const updateDetailWidth = useCallback((width: number): void => {
    const nextWidth = clampDetailWidth(width, catalogRef.current?.clientWidth);
    setDetailWidth(nextWidth);
    window.localStorage.setItem(AUTOMATION_DETAIL_WIDTH_KEY, String(nextWidth));
  }, []);

  useEffect(() => {
    onDetailPaneLayoutChange?.({
      open: detailOpen,
      reservedWidth: detailOpen ? detailWidth + AUTOMATION_DETAIL_RESIZER_WIDTH : 0,
    });
  }, [detailWidth, onDetailPaneLayoutChange, detailOpen]);

  useEffect(() => {
    const catalog = catalogRef.current;
    if (!catalog || !detailOpen) return;
    const fitDetailToCatalog = (): void => {
      setDetailWidth((current) => {
        const next = clampDetailWidth(current, catalog.clientWidth);
        if (next !== current) {
          window.localStorage.setItem(AUTOMATION_DETAIL_WIDTH_KEY, String(next));
        }
        return next;
      });
    };
    fitDetailToCatalog();
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(fitDetailToCatalog);
    observer.observe(catalog);
    return () => observer.disconnect();
  }, [detailOpen]);

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

  function startCreate(): void {
    if (availableProjects.length === 0) return;
    setSelectedID("");
    const workspaceID = availableProjects.some((project) => project.id === activeProjectID)
      ? activeProjectID
      : availableProjects[0].id;
    setPendingNew({ workspaceID });
  }

  async function createFromDraft(draft: AutomationDraft, workspaceID: string): Promise<boolean> {
    setError("");
    const project = availableProjects.find((candidate) => candidate.id === workspaceID);
    if (!project) {
      setError(t("automations.workspaceRequired"));
      return false;
    }
    try {
      const result = await window.wuu.createAutomation({
        title: draft.title,
        prompt: draft.prompt,
        schedule: draft.schedule,
        timezone: draft.timezone,
        mode: draft.mode,
        heartbeat_thread_id: draft.heartbeatThreadID || undefined,
        workspace_id: project.id,
        workspace_path: project.path,
        recurring: draft.recurring,
        paused: true,
      });
      setTasks((current) => [result.task, ...current]);
      setPendingNew(null);
      setSelectedID(result.task.id);
      return true;
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : translateCurrent("automations.createFailed"));
      return false;
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
      className={`automations-catalog${detailOpen ? " detail-open" : ""}${resizingDetail ? " resizing-detail" : ""}`}
      aria-label={t("automations.title")}
      style={{ "--automation-detail-pane-width": `${detailWidth}px` } as CSSProperties}
    >
      <div className="automations-master">
        <header className="catalog-page-header">
          <div className="automations-heading-row">
            <div className="catalog-page-title">
              <strong>{t("automations.title")}</strong>
              <span>{t("automations.subtitle")}</span>
            </div>
            <div className="automation-create-controls">
              <button className="settings-button catalog-create" type="button"
                aria-label={t("automations.create")}
                disabled={availableProjects.length === 0} onClick={startCreate}>
                <Plus className="icon" aria-hidden="true" />
                <span>{t("automations.create")}</span>
              </button>
            </div>
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
              onClick={() => { setPendingNew(null); setSelectedID(task.id); }}>
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
      {detailTask ? (
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
            <AutomationDetail key={detailTask.id} task={detailTask}
              creating={pendingTask !== null && !selected}
              projects={availableProjects}
              initialWorkspaceID={pendingNew?.workspaceID ?? ""}
              onCreate={createFromDraft}
              onUpdate={update} onRemove={remove}
              onSaveRejected={showSaveRejectedNotice}
              onClose={() => { setPendingNew(null); setSelectedID(""); }} />
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
  recurring,
  onScheduleChange,
  onRecurringChange,
  onCommit,
}: {
  schedule: string;
  timezone: string;
  recurring: boolean;
  onScheduleChange: (schedule: string, commit: boolean) => void;
  onRecurringChange: (recurring: boolean) => void;
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
          <span>{t("automations.executionMode")}</span>
          <SelectMenu
            triggerClassName="settings-select-trigger"
            ariaLabel={t("automations.executionMode")}
            value={recurring ? "recurring" : "once"}
            onChange={(value) => onRecurringChange(value === "recurring")}
            options={[
              { value: "recurring", label: t("automations.executionMode.recurring") },
              { value: "once", label: t("automations.executionMode.once") },
            ]}
          />
        </label>
        <label>
          <span>{t("automations.frequency")}</span>
          <SelectMenu
            triggerClassName="settings-select-trigger"
            ariaLabel={t("automations.frequency")}
            value={kind}
            onChange={(value) => {
              const nextKind = value as AutomationScheduleKind;
              setKind(nextKind);
              if (nextKind !== "custom") {
                onScheduleChange(defaultCronForScheduleKind(nextKind, schedule), true);
              }
            }}
            options={(["minutes", "hourly", "daily", "weekdays", "weekly", "custom"] as const).map((value) => ({
              value,
              label: t(`automations.frequency.${value}`),
            }))}
          />
        </label>
        {kind === "minutes" ? (
          <label>
            <span>{t("automations.interval")}</span>
            <SelectMenu
              triggerClassName="settings-select-trigger"
              ariaLabel={t("automations.interval")}
              value={String(editor.interval)}
              onChange={(value) => updateCommon({ ...editor, kind, interval: Number(value) })}
              options={[5, 10, 15, 30].map((value) => ({
                value: String(value),
                label: t("automations.intervalMinutes", { count: value }),
              }))}
            />
          </label>
        ) : null}
        {kind === "hourly" ? (
          <label>
            <span>{t("automations.executionMinute")}</span>
            <MinuteClockPicker
              minute={editor.minute}
              ariaLabel={t("automations.executionMinute")}
              onChange={(minute) => updateCommon({ ...editor, kind, minute })}
            />
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
            <SelectMenu
              triggerClassName="settings-select-trigger"
              ariaLabel={t("automations.weekday")}
              value={String(editor.weekday)}
              onChange={(value) => updateCommon({ ...editor, kind, weekday: Number(value) })}
              options={Array.from({ length: 7 }, (_, value) => ({
                value: String(value),
                label: t(AUTOMATION_WEEKDAY_KEYS[value]),
              }))}
            />
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

function AutomationDetail({ task, onUpdate, onRemove, onSaveRejected, onClose, creating = false, projects = [], initialWorkspaceID = "", onCreate }: {
  task: AutomationTask;
  onUpdate: (params: AutomationUpdateParams) => Promise<void>;
  onRemove: (task: AutomationTask) => Promise<void>;
  onSaveRejected: () => void;
  onClose: () => void;
  // creating: the task is an unsaved draft. Fields buffer locally, the
  // workspace stays selectable, and nothing persists until the create action
  // runs. After creation the workspace is locked (it owns scheduling, so
  // rebinding would be a migration, not an edit).
  creating?: boolean;
  projects?: DesktopProject[];
  initialWorkspaceID?: string;
  onCreate?: (draft: AutomationDraft, workspaceID: string) => Promise<boolean>;
}): JSX.Element {
  const { locale, t } = useI18n();
  const initialDraft = useMemo(() => draftFromAutomation(task), [task.id]);
  const [draft, setDraft] = useState<AutomationDraft>(initialDraft);
  const latestDraftRef = useRef(initialDraft);
  const lastSavedDraftRef = useRef(initialDraft);
  const saveQueueRef = useRef<Promise<boolean>>(Promise.resolve(true));
  const [workspaceID, setWorkspaceID] = useState(initialWorkspaceID);
  const [submitting, setSubmitting] = useState(false);
  const timezoneOptions = useMemo(() => automationTimezoneOptions(
    draft.timezone,
    Intl.DateTimeFormat().resolvedOptions().timeZone,
    t("automations.localTimezone"),
    locale,
  ), [draft.timezone, locale, t]);

  function updateDraft(next: AutomationDraft): void {
    latestDraftRef.current = next;
    setDraft(next);
  }

  function persistDraft(candidate = latestDraftRef.current): Promise<boolean> {
    if (creating) return Promise.resolve(true);
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
    if (!creating) await persistDraft();
    onClose();
  }

  async function submitCreate(): Promise<void> {
    if (!onCreate || submitting) return;
    setSubmitting(true);
    const created = await onCreate({ ...latestDraftRef.current }, workspaceID);
    if (!created) setSubmitting(false);
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
          {creating ? null : (
            <>
              <button className="icon-button" type="button" aria-label={task.paused ? t("automations.resume") : t("automations.pause")}
                onClick={() => void togglePaused()}>
                {task.paused ? <Play className="icon" /> : <Pause className="icon" />}
              </button>
              <button className="icon-button danger" type="button" aria-label={t("automations.delete")}
                onClick={() => void onRemove(task)}><Trash2 className="icon" /></button>
            </>
          )}
          <button className="icon-button" type="button" aria-label={t("automations.closeDetails")} onClick={() => void closeDetails()}>
            <X className="icon" />
          </button>
        </div>
        </div>
        <label>
          <span>{t("automations.prompt")}</span>
          <textarea
            className="settings-input settings-textarea"
            rows={4}
            value={draft.prompt}
            onChange={(event) => updateDraft({ ...latestDraftRef.current, prompt: event.currentTarget.value })}
            onBlur={() => void persistDraft()}
          />
        </label>
      </section>
      <section className="automation-detail-section">
        <div className="automation-detail-grid">
          <div className="automation-workspace-field">
            <span>{t("automations.workspace")}</span>
            {creating ? (
              <SelectMenu
                value={workspaceID}
                onChange={setWorkspaceID}
                options={projects.map((project) => ({
                  value: project.id,
                  label: project.name,
                  hint: project.path,
                }))}
                placeholder={t("automations.workspaceUnavailable")}
                ariaLabel={t("automations.workspace")}
                triggerClassName="settings-select-trigger"
              />
            ) : (
              <strong title={task.workspacePath}>{task.workspacePath || t("automations.workspaceUnavailable")}</strong>
            )}
          </div>
          <AutomationScheduleEditor
            schedule={draft.schedule}
            timezone={draft.timezone}
            recurring={draft.recurring}
            onScheduleChange={(schedule, commit) => {
              const next = { ...latestDraftRef.current, schedule };
              updateDraft(next);
              if (commit) void persistDraft(next);
            }}
            onRecurringChange={(recurring) => {
              const next = { ...latestDraftRef.current, recurring };
              updateDraft(next);
              void persistDraft(next);
            }}
            onCommit={() => void persistDraft()}
          />
          <label>
            <span>{t("automations.timezone")}</span>
            <SelectMenu
              triggerClassName="settings-select-trigger"
              ariaLabel={t("automations.timezone")}
              value={draft.timezone}
              searchable
              searchPlaceholder={t("automations.timezoneSearch")}
              emptyMessage={t("automations.timezoneNoMatches")}
              options={timezoneOptions}
              onChange={(timezone) => {
                const next = { ...latestDraftRef.current, timezone };
                updateDraft(next);
                void persistDraft(next);
              }}
            />
          </label>
          <label>
            <span>{t("automations.mode")}</span>
            <SelectMenu
              triggerClassName="settings-select-trigger"
              ariaLabel={t("automations.mode")}
              value={draft.mode}
              onChange={(value) => {
                const next = { ...latestDraftRef.current, mode: value as AutomationDraft["mode"] };
                updateDraft(next);
                void persistDraft(next);
              }}
              options={[
                { value: "new_thread", label: t("automations.mode.newThread") },
                { value: "thread_heartbeat", label: t("automations.mode.heartbeat") },
              ]}
            />
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
      {creating ? (
        <section className="automation-detail-section automation-detail-create-bar">
          <button className="settings-button catalog-create" type="button"
            disabled={!workspaceID || submitting}
            onClick={() => void submitCreate()}>
            <Plus className="icon" aria-hidden="true" />
            <span>{t("automations.create")}</span>
          </button>
        </section>
      ) : null}
    </div>
  );
}
