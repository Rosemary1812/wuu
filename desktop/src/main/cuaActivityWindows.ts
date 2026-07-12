import { screen, type Rectangle } from "electron";
import type { ActivitySession, ServerEvent } from "../shared/protocol";
import { CUANativePiP, resolveCUAFrameHelper, type CUANativePiPEvent } from "./cuaFrameStreams";
import type { WindowRegistry } from "./windowRegistry";

export type ActivityControlAction = "takeover" | "release" | "stop";

const PIP_WIDTH = 260;
const PIP_HEIGHT = 170;
const PIP_INSET = 12;

export function activityControlMethod(action: ActivityControlAction): "activity/takeover" | "activity/release" | "activity/stop" {
  switch (action) {
    case "takeover": return "activity/takeover";
    case "release": return "activity/release";
    case "stop": return "activity/stop";
  }
}

export function frameStreamRetryDelay(attempt: number): number {
  return Math.min(16_000, 1_000 * 2 ** Math.max(1, attempt));
}

export function activityVisibleForThread(activityThreadID: string, activeThreadID?: string): boolean {
  return Boolean(activeThreadID && activityThreadID === activeThreadID);
}

export function nativePiPInitialBounds(mainBounds: Rectangle | undefined, workArea: Rectangle): Rectangle {
  if (!mainBounds) {
    return {
      x: workArea.x + workArea.width - PIP_WIDTH - 24,
      y: workArea.y + 24,
      width: PIP_WIDTH,
      height: PIP_HEIGHT,
    };
  }
  const x = Math.min(
    workArea.x + workArea.width - PIP_WIDTH - PIP_INSET,
    Math.max(workArea.x + PIP_INSET, mainBounds.x + mainBounds.width - PIP_WIDTH - PIP_INSET),
  );
  const y = Math.min(
    workArea.y + workArea.height - PIP_HEIGHT - PIP_INSET,
    Math.max(workArea.y + PIP_INSET, mainBounds.y + PIP_INSET),
  );
  return { x, y, width: PIP_WIDTH, height: PIP_HEIGHT };
}

type ActivitySnapshot = (threadID: string) => Promise<ActivitySession[]>;
type PiPHandle = Pick<CUANativePiP, "start" | "setVisible" | "animateInteraction" | "stop">;
type PiPFactory = (activity: ActivitySession, key: string) => PiPHandle | undefined;
type PiPEntry = {
  key: string;
  threadID: string;
  activity: ActivitySession;
  pip: PiPHandle;
  // "preparing" = frosted placeholder shown, no live frame yet.
  // "live" = first real frame presented (native `ready`).
  phase: "preparing" | "live";
};

/**
 * Owns one user-visible CUA observation surface. Activity events update the
 * target, but an individual action ending never owns the PiP lifetime.
 */
export class CUAObservationCoordinator {
  private readonly observations = new Map<string, ActivitySession>();
  private readonly dismissedAt = new Map<string, string>();
  private readonly lastInteractionRevisions = new Map<string, number>();
  private readonly retryAttempts = new Map<string, number>();
  private readonly retryTimers = new Map<string, NodeJS.Timeout>();
  private current: PiPEntry | undefined;
  private replacement: { activity: ActivitySession; key: string } | undefined;
  private replacementInFlight = false;
  private userBounds: Rectangle | undefined;
  private reconcileTimer: NodeJS.Timeout | undefined;
  private reconcileInFlight = false;
  private activeThreadID: string | undefined;

  constructor(
    private readonly registry: WindowRegistry,
    private readonly snapshot?: ActivitySnapshot,
    private readonly pipFactory?: PiPFactory,
  ) {}

  handleServerEvent(event: ServerEvent): void {
    const activity = cuaActivityFromServerEvent(event);
    if (activity) {
      this.update(activity);
      return;
    }
    this.scheduleReconcile();
  }

  setActiveThread(threadID?: string): void {
    this.activeThreadID = threadID?.trim() || undefined;
    if (this.current?.threadID !== this.activeThreadID) this.current?.pip.setVisible(false);
    if (this.replacement?.activity.thread_id !== this.activeThreadID) this.replacement = undefined;
    this.syncActiveObservation();
    this.scheduleReconcile(0);
  }

  update(activity: ActivitySession): void {
    if (activity.kind !== "cua" || activity.plugin_id !== "cua-mac") return;
    // A stopped control lease is not the end of the user's observation. Keep
    // the last target until the session changes, the user closes it, or a new
    // target replaces it.
    if (activity.state === "stopped" || !activity.target?.trim()) return;
    const current = this.observations.get(activity.thread_id);
    if (!current || current.updated_at <= activity.updated_at) {
      this.observations.set(activity.thread_id, activity);
    }
    const dismissed = this.dismissedAt.get(activity.thread_id);
    if (dismissed && dismissed < activity.updated_at) this.dismissedAt.delete(activity.thread_id);
    if (activity.thread_id === this.activeThreadID) this.syncActiveObservation();
  }

  private syncActiveObservation(): void {
    const threadID = this.activeThreadID;
    if (!threadID || this.dismissedAt.has(threadID)) {
      this.current?.pip.setVisible(false);
      return;
    }
    const activity = this.observations.get(threadID);
    if (!activity) {
      this.current?.pip.setVisible(false);
      return;
    }
    const key = observationKey(activity);
    if (this.current?.key === key) {
      this.current.pip.setVisible(true);
      this.animateInteractionIfNew(activity, this.current.pip);
      return;
    }
    if (this.replacement?.key === key) {
      this.replacement.activity = activity;
      return;
    }
    if (this.current || this.replacementInFlight) {
      this.replaceCurrent(activity, key);
      return;
    }
    this.startImmediate(activity, key);
  }

  private startImmediate(activity: ActivitySession, key: string): void {
    const pip = this.spawnPiP(activity, key);
    if (!pip) return;
    this.current = { key, threadID: activity.thread_id, activity, pip, phase: "preparing" };
    pip.start();
    pip.setVisible(true);
    this.animateInteractionIfNew(activity, pip);
  }

  private replaceCurrent(activity: ActivitySession, key: string): void {
    this.replacement = { activity, key };
    if (this.replacementInFlight) return;
    if (!this.current) {
      this.startReplacement();
      return;
    }
    this.stopCurrent();
  }

  private stopCurrent(): void {
    if (this.replacementInFlight) return;
    const outgoing = this.current;
    if (!outgoing) return;
    // replayd groups capture clients by executable path. Two overlapping PiP
    // helpers therefore interrupt one another even when they stream different
    // apps. Every stop path shares this lock so a user close or stream failure
    // cannot let a replacement start before the old process has closed.
    this.replacementInFlight = true;
    this.current = undefined;
    outgoing.pip.stop(() => {
      this.replacementInFlight = false;
      this.startReplacement();
    });
  }

  private startReplacement(): void {
    const replacement = this.replacement;
    this.replacement = undefined;
    if (!replacement) return;
    if (replacement.activity.thread_id !== this.activeThreadID || this.dismissedAt.has(replacement.activity.thread_id)) return;
    this.startImmediate(replacement.activity, replacement.key);
  }

  private spawnPiP(activity: ActivitySession, key: string): PiPHandle | undefined {
    if (this.pipFactory) return this.pipFactory(activity, key);
    const helper = resolveCUAFrameHelper();
    const target = activity.target?.trim();
    if (!helper || !target) return undefined;
    return new CUANativePiP(
      helper,
      activity.thread_id,
      target,
      activity.process_id,
      activity.window_id,
      this.initialBounds(),
      (event) => this.handleNativePiPEvent(key, event),
      () => this.handleNativePiPFailure(key),
    );
  }

  private handleNativePiPEvent(key: string, event: CUANativePiPEvent): void {
    const entry = this.current?.key === key ? this.current : undefined;
    if (!entry) return;
    switch (event.event) {
      case "ready":
        // A placeholder-first current reached its first live frame; the native
        // side already cross-faded from the frosted placeholder to the capture.
        entry.phase = "live";
        this.retryAttempts.delete(entry.threadID);
        return;
      case "user_close":
        this.dismissedAt.set(entry.threadID, new Date().toISOString());
        if (this.current?.key === key) this.stopCurrent();
        return;
      case "user_input":
        return;
      case "geometry":
        if ([event.x, event.y, event.width, event.height].every((value) => typeof value === "number")) {
          this.userBounds = { x: event.x!, y: event.y!, width: event.width!, height: event.height! };
        }
        return;
      case "capture_status":
        return;
    }
  }

  private handleNativePiPFailure(key: string): void {
    const entry = this.current?.key === key ? this.current : undefined;
    if (!entry) return;
    if (this.current?.key === key) this.stopCurrent();
    if (entry.threadID !== this.activeThreadID || this.dismissedAt.has(entry.threadID)) return;
    const attempts = (this.retryAttempts.get(entry.threadID) ?? 0) + 1;
    this.retryAttempts.set(entry.threadID, attempts);
    const timer = setTimeout(() => {
      this.retryTimers.delete(entry.threadID);
      this.syncActiveObservation();
    }, frameStreamRetryDelay(attempts));
    timer.unref?.();
    this.retryTimers.set(entry.threadID, timer);
  }

  private animateInteractionIfNew(activity: ActivitySession, pip: PiPHandle): void {
    const interaction = activity.interaction;
    if (!interaction) return;
    const previous = this.lastInteractionRevisions.get(activity.thread_id);
    if (previous !== undefined && interaction.revision <= previous) return;
    this.lastInteractionRevisions.set(activity.thread_id, interaction.revision);
    pip.animateInteraction(interaction);
  }

  private scheduleReconcile(delay = 150): void {
    if (!this.snapshot || !this.activeThreadID || this.reconcileInFlight || this.reconcileTimer) return;
    this.reconcileTimer = setTimeout(() => {
      this.reconcileTimer = undefined;
      const threadID = this.activeThreadID;
      if (!threadID) return;
      this.reconcileInFlight = true;
      void this.snapshot?.(threadID)
        .then((activities) => {
          if (this.activeThreadID !== threadID) return;
          for (const activity of activities) this.update(activity);
        })
        .catch(() => undefined)
        .finally(() => { this.reconcileInFlight = false; });
    }, delay);
    this.reconcileTimer.unref?.();
  }

  private initialBounds(): Rectangle {
    if (this.userBounds) return this.userBounds;
    const mainWindow = this.registry.mainWindow();
    const mainBounds = mainWindow && !mainWindow.isDestroyed() && mainWindow.isVisible()
      ? mainWindow.getContentBounds()
      : undefined;
    const cursor = screen.getCursorScreenPoint();
    const workArea = mainBounds
      ? screen.getDisplayMatching(mainBounds).workArea
      : screen.getDisplayNearestPoint(cursor).workArea;
    return nativePiPInitialBounds(mainBounds, workArea);
  }
}

export function observationKey(activity: ActivitySession): string {
  // Key the PiP lifecycle by session + target only. A single CUA call emits
  // `started` with no window identity (0/0) and then `updated` with the resolved
  // process/window id; including those here changed the key mid-call and spawned
  // a second helper, so two ScreenCaptureKit streams raced for the same window
  // and tripped "application connection interrupted". One target = one helper =
  // one stream. The exact identity is still passed to the helper as a hint.
  return [activity.thread_id, activity.target?.trim()].join(":");
}

export function cuaActivityFromServerEvent(event: ServerEvent): ActivitySession | undefined {
  if (event.kind !== "notification") return undefined;
  const method = event.message.method;
  if (method !== "activity/started" && method !== "activity/updated" && method !== "activity/control_changed" && method !== "activity/stopped") return undefined;
  const value = event.message.params;
  if (!value || typeof value !== "object" || Array.isArray(value)) return undefined;
  const activity = value as Partial<ActivitySession>;
  if (activity.kind !== "cua" || typeof activity.id !== "string" || typeof activity.thread_id !== "string") return undefined;
  return value as ActivitySession;
}
