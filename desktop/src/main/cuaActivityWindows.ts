import { screen, type Rectangle } from "electron";
import type { ActivitySession, ServerEvent } from "../shared/protocol";
import { CUANativePiP, resolveCUAFrameHelper, type CUANativePiPEvent } from "./cuaFrameStreams";
import type { WindowRegistry } from "./windowRegistry";

export type ActivityControlAction = "takeover" | "release" | "stop";

const PIP_WIDTH = 380;
const PIP_HEIGHT = 248;
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
type PiPEntry = {
  key: string;
  threadID: string;
  activity: ActivitySession;
  pip: CUANativePiP;
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
  private candidate: PiPEntry | undefined;
  private userBounds: Rectangle | undefined;
  private reconcileTimer: NodeJS.Timeout | undefined;
  private reconcileInFlight = false;
  private activeThreadID: string | undefined;

  constructor(
    private readonly registry: WindowRegistry,
    private readonly snapshot?: ActivitySnapshot,
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
    if (this.candidate?.threadID !== this.activeThreadID) this.stopCandidate();
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
    if (this.candidate?.key === key) {
      this.animateInteractionIfNew(activity, this.candidate.pip);
      return;
    }
    this.startCandidate(activity, key);
  }

  private startCandidate(activity: ActivitySession, key: string): void {
    const helper = resolveCUAFrameHelper();
    const target = activity.target?.trim();
    if (!helper || !target) return;
    this.stopCandidate();
    const pip = new CUANativePiP(
      helper,
      activity.thread_id,
      target,
      activity.process_id,
      activity.window_id,
      this.initialBounds(),
      (event) => this.handleNativePiPEvent(key, event),
      () => this.handleNativePiPFailure(key),
    );
    this.candidate = { key, threadID: activity.thread_id, activity, pip };
    pip.start();
    pip.setVisible(false);
    this.animateInteractionIfNew(activity, pip);
  }

  private handleNativePiPEvent(key: string, event: CUANativePiPEvent): void {
    const entry = this.candidate?.key === key ? this.candidate : this.current?.key === key ? this.current : undefined;
    if (!entry) return;
    switch (event.event) {
      case "ready":
        if (this.candidate?.key !== key || entry.threadID !== this.activeThreadID) return;
        this.current?.pip.stop();
        this.current = entry;
        this.candidate = undefined;
        this.retryAttempts.delete(entry.threadID);
        entry.pip.setVisible(true);
        return;
      case "user_close":
        this.dismissedAt.set(entry.threadID, new Date().toISOString());
        if (this.current?.key === key) this.stopCurrent();
        if (this.candidate?.key === key) this.stopCandidate();
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
    const entry = this.candidate?.key === key ? this.candidate : this.current?.key === key ? this.current : undefined;
    if (!entry) return;
    if (this.candidate?.key === key) this.stopCandidate();
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

  private animateInteractionIfNew(activity: ActivitySession, pip: CUANativePiP): void {
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

  private stopCurrent(): void {
    this.current?.pip.stop();
    this.current = undefined;
  }

  private stopCandidate(): void {
    this.candidate?.pip.stop();
    this.candidate = undefined;
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
  return [activity.thread_id, activity.target?.trim(), activity.process_id ?? 0, activity.window_id ?? 0].join(":");
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
