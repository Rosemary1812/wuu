import { screen, type Rectangle } from "electron";
import type { ActivitySession, ServerEvent } from "../shared/protocol";
import { CUANativePiP, resolveCUAFrameHelper, type CUANativePiPEvent } from "./cuaFrameStreams";
import type { WindowRegistry } from "./windowRegistry";

export type ActivityControlAction = "takeover" | "release" | "stop";

const MAX_LIVE_CUA_STREAMS = 3;
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

export function nativePiPInitialBounds(
  mainBounds: Rectangle | undefined,
  workArea: Rectangle,
): Rectangle {
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

type ActivityControl = (
  activity: ActivitySession,
  action: ActivityControlAction,
) => Promise<ActivitySession>;

export class CUAActivityWindowManager {
  private readonly activities = new Map<string, ActivitySession>();
  private readonly dismissedActivityIDs = new Set<string>();
  private readonly nativePiPs = new Map<string, { target: string; pip: CUANativePiP; startedAt: number }>();
  private readonly retries = new Map<string, { attempts: number; timer?: NodeJS.Timeout }>();
  private readonly lastInteractionRevisions = new Map<string, number>();
  private activeThreadID: string | undefined;

  constructor(
    private readonly registry: WindowRegistry,
    private readonly control: ActivityControl,
  ) {}

  handleServerEvent(event: ServerEvent): void {
    const activity = cuaActivityFromServerEvent(event);
    if (activity) this.update(activity);
  }

  setActiveThread(threadID?: string): void {
    this.activeThreadID = threadID?.trim() || undefined;
    for (const [activityID, activity] of this.activities) {
      if (activityVisibleForThread(activity.thread_id, this.activeThreadID) && !this.dismissedActivityIDs.has(activityID)) {
        this.syncNativePiP(activity);
      } else {
        this.stopNativePiP(activityID);
      }
    }
  }

  update(activity: ActivitySession): void {
    if (activity.state === "stopped") {
      this.stopNativePiP(activity.id);
      this.activities.delete(activity.id);
      this.dismissedActivityIDs.delete(activity.id);
      this.lastInteractionRevisions.delete(activity.id);
      return;
    }
    this.activities.set(activity.id, activity);
    if (this.dismissedActivityIDs.has(activity.id)) return;
    if (activityVisibleForThread(activity.thread_id, this.activeThreadID)) {
      this.syncNativePiP(activity);
    } else {
      this.stopNativePiP(activity.id);
    }
  }

  private syncNativePiP(activity: ActivitySession): void {
    if (activity.plugin_id !== "cua-mac") return;
    const target = activity.target?.trim();
    const helper = resolveCUAFrameHelper();
    if (!target || !helper) return;

    const current = this.nativePiPs.get(activity.id);
    if (current?.target === target) {
      current.pip.setVisible(true);
      this.animateInteractionIfNew(activity, current.pip);
      const retry = this.retries.get(activity.id);
      if (current.pip.isLive() || retry?.timer) return;
      this.retries.delete(activity.id);
      current.pip.start();
      return;
    }

    this.stopNativePiP(activity.id);
    if (this.nativePiPs.size >= MAX_LIVE_CUA_STREAMS) {
      const oldest = [...this.nativePiPs.entries()]
        .sort((left, right) => left[1].startedAt - right[1].startedAt)[0];
      if (oldest) this.stopNativePiP(oldest[0]);
    }

    const pip = new CUANativePiP(
      helper,
      activity.id,
      target,
      this.initialBounds(),
      (event) => this.handleNativePiPEvent(activity.id, event),
      () => this.scheduleRetry(activity.id),
    );
    this.nativePiPs.set(activity.id, { target, pip, startedAt: Date.now() });
    pip.start();
    this.animateInteractionIfNew(activity, pip);
  }

  private animateInteractionIfNew(activity: ActivitySession, pip: CUANativePiP): void {
    const interaction = activity.interaction;
    if (!interaction) return;
    const previous = this.lastInteractionRevisions.get(activity.id);
    if (previous !== undefined && interaction.revision <= previous) return;
    this.lastInteractionRevisions.set(activity.id, interaction.revision);
    pip.animateInteraction(interaction);
  }

  private initialBounds(): Rectangle {
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

  private handleNativePiPEvent(activityID: string, event: CUANativePiPEvent): void {
    switch (event.event) {
      case "ready": {
        const retry = this.retries.get(activityID);
        if (retry && !retry.timer) this.retries.delete(activityID);
        this.nativePiPs.get(activityID)?.pip.setVisible(true);
        return;
      }
      case "user_close":
        this.dismissedActivityIDs.add(activityID);
        this.stopNativePiP(activityID);
        return;
      case "user_input":
        this.handleDetectedUserInput(activityID);
        return;
      case "capture_status":
        return;
    }
  }

  private handleDetectedUserInput(activityID: string): void {
    const activity = this.activities.get(activityID);
    if (!activity || activity.controller !== "agent") return;
    void this.control(activity, "takeover")
      .then((updated) => this.update(updated))
      .catch(() => undefined);
  }

  private stopNativePiP(activityID: string): void {
    this.nativePiPs.get(activityID)?.pip.stop();
    this.nativePiPs.delete(activityID);
    const retry = this.retries.get(activityID);
    if (retry?.timer) clearTimeout(retry.timer);
    this.retries.delete(activityID);
  }

  private scheduleRetry(activityID: string): void {
    const current = this.nativePiPs.get(activityID);
    if (!current || current.pip.isLive() || !this.activities.has(activityID)) return;
    const retry = this.retries.get(activityID) ?? { attempts: 0 };
    if (retry.timer) return;
    retry.attempts += 1;
    retry.timer = setTimeout(() => {
      retry.timer = undefined;
      const activity = this.activities.get(activityID);
      const entry = this.nativePiPs.get(activityID);
      if (!activity || !entry || entry.pip.isLive() || activity.target?.trim() !== entry.target) return;
      entry.pip.start();
    }, frameStreamRetryDelay(retry.attempts));
    retry.timer.unref?.();
    this.retries.set(activityID, retry);
  }
}

export function cuaActivityFromServerEvent(event: ServerEvent): ActivitySession | undefined {
  if (event.kind !== "notification") return undefined;
  const method = event.message.method;
  if (method !== "activity/started" && method !== "activity/updated" && method !== "activity/control_changed" && method !== "activity/stopped") {
    return undefined;
  }
  const value = event.message.params;
  if (!value || typeof value !== "object" || Array.isArray(value)) return undefined;
  const activity = value as Partial<ActivitySession>;
  if (activity.kind !== "cua" || typeof activity.id !== "string" || typeof activity.thread_id !== "string") return undefined;
  return value as ActivitySession;
}
