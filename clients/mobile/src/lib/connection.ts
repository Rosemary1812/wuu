// The controller: owns the RemoteClient (relay link), feeds its notification
// stream into the AppStore, and exposes the handful of user actions the
// three screens need. Credential persistence is injected so this module
// stays pure TS (secure-store on device, anything else in tests).

import {
  Credentials,
  RemoteClient,
  pair as corePair,
} from "@wuu/remote-core";
import type {
  MessageMarkWire,
  ParticipantProfile,
  Thread,
  ThreadItem,
  Turn,
} from "@wuu/protocol";

import { AppStore } from "./store";
import { mentionedParticipantIDsFromText } from "./mentions";
import { isThreadRunning } from "./threads";

export interface CredentialStore {
  load(): Promise<Credentials | null>;
  save(creds: Credentials): Promise<void>;
  clear(): Promise<void>;
}

type ThreadListResult = { threads: Thread[] };
type ThreadResult = { thread: Thread };
type ParticipantListResult = { participants: ParticipantProfile[] };
type MarksResult = { marks: MessageMarkWire[] };
type TurnResult = { turn: Turn };
type QueueResult = { queued: { id: string } };

export class WuuMobile {
  readonly store = new AppStore();
  private client: RemoteClient | null = null;
  private refreshTimer: ReturnType<typeof setTimeout> | null = null;
  private nextClientId = 0;

  constructor(private readonly credStore: CredentialStore) {
    this.store.onUnknownThread = () => this.scheduleThreadsRefresh();
  }

  /** True when stored credentials existed and the link is starting. */
  async startFromStoredCredentials(): Promise<boolean> {
    const creds = await this.credStore.load();
    if (!creds) return false;
    this.start(creds);
    return true;
  }

  /** Completes pairing against a scanned/pasted URI, persists credentials,
   *  and brings the link up. */
  async pairWithUri(uri: string, deviceName: string): Promise<Credentials> {
    const creds = await corePair(uri.trim(), deviceName);
    await this.credStore.save(creds);
    this.start(creds);
    return creds;
  }

  async unpair(): Promise<void> {
    await this.credStore.clear();
    await this.client?.stop();
    this.client = null;
    this.store.resetServerState();
    this.store.setPhase("idle");
  }

  private start(creds: Credentials): void {
    this.store.setHostName(creds.host_name ?? "");
    this.store.setPhase("connecting");
    this.client = new RemoteClient(creds, {
      onNotification: (method, params) => this.store.applyNotification(method, params),
      onAttach: (ev) => void this.onAttach(ev.resumed),
      onDetach: () => this.store.setPhase("reconnecting"),
    });
    this.client.start();
  }

  private async onAttach(resumed: boolean): Promise<void> {
    this.store.setPhase("attached");
    try {
      if (!resumed) {
        // Fresh app-server connection: every server-state mirror is stale.
        this.store.resetServerState();
        await this.call("initialize");
      }
      await this.refreshThreads();
      await this.refreshParticipants();
    } catch {
      // The link dropped mid-refresh; the reconnect loop will re-attach and
      // land here again.
    }
  }

  private call<T>(method: string, params?: unknown): Promise<T> {
    const client = this.client;
    if (!client) return Promise.reject(new Error("未连接"));
    return client.call<T>(method, params);
  }

  async refreshThreads(): Promise<void> {
    const result = await this.call<ThreadListResult>("thread/list");
    this.store.setThreads(result.threads ?? []);
  }

  async refreshParticipants(): Promise<void> {
    const result = await this.call<ParticipantListResult>("participant/list");
    this.store.setParticipants(result.participants ?? []);
  }

  private scheduleThreadsRefresh(): void {
    if (this.refreshTimer) return;
    this.refreshTimer = setTimeout(() => {
      this.refreshTimer = null;
      void this.refreshThreads().catch(() => {});
    }, 400);
  }

  /** thread/resume returns the FULL history in its result; marks load
   *  alongside so receipts/reactions render on first paint. */
  async openThread(threadId: string): Promise<void> {
    this.store.setActiveThread(threadId);
    const result = await this.call<ThreadResult>("thread/resume", { session_id: threadId });
    this.store.applyNotification("thread/resumed", result);
    this.store.setActiveThread(threadId); // re-advance unread cursor on fresh turns
    try {
      const marks = await this.call<MarksResult>("thread/marks", { thread_id: threadId });
      this.store.setThreadMarks(threadId, marks.marks ?? []);
    } catch {
      // Older hosts without marks: chat still renders.
    }
  }

  closeThread(): void {
    const active = this.store.getSnapshot().activeThreadId;
    if (active) this.store.markViewed(active);
    this.store.setActiveThread(null);
  }

  /** Send, mirroring the desktop's chat semantics: turn/start when idle,
   *  turn/queue while the thread is mid-run (at-most-once either way). */
  async sendMessage(thread: Thread, text: string): Promise<void> {
    const prompt = text.trim();
    if (prompt === "") return;
    const clientId = `m-${Date.now()}-${++this.nextClientId}`;
    this.store.addPending({
      clientId,
      threadId: thread.id,
      text: prompt,
      atMs: Date.now(),
      queued: false,
    });
    try {
      if (isThreadRunning(thread)) {
        await this.call<QueueResult>("turn/queue", {
          thread_id: thread.id,
          prompt,
          images: [],
          files: [],
          client_id: clientId,
        });
        this.store.markPendingQueued(clientId);
      } else {
        const params: Record<string, unknown> = {
          thread_id: thread.id,
          prompt,
          images: [],
          files: [],
        };
        // Server rejects unknown fields — mentions only travels when non-empty.
        const mentions = mentionedParticipantIDsFromText(
          prompt,
          this.store.getSnapshot().participants,
        );
        if (mentions.length > 0) params.mentions = mentions;
        await this.call<TurnResult>("turn/start", params);
        this.store.removePending(clientId);
        this.scheduleThreadsRefresh();
      }
    } catch (err) {
      this.store.removePending(clientId);
      throw err;
    }
  }

  async interrupt(threadId: string): Promise<void> {
    await this.call("turn/interrupt", { thread_id: threadId });
  }

  async react(threadId: string, seq: number, reaction: string): Promise<void> {
    await this.call("message/react", { thread_id: threadId, seq, reaction });
  }

  async togglePin(thread: Thread): Promise<void> {
    await this.call("thread/pin", { thread_id: thread.id, pinned: !thread.pinned });
    await this.refreshThreads();
  }

  /** Item helper for tests and the read-receipt line. */
  itemBySeq(thread: Thread, seq: number): ThreadItem | undefined {
    for (const turn of thread.turns ?? []) {
      for (const item of turn.items ?? []) {
        if (item.seq === seq) return item;
      }
    }
    return undefined;
  }
}
