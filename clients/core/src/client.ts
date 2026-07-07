// The controller-side client of wuu remote control, mirrored from the Go
// phone package (the reference implementation): pair with a host, keep an
// end-to-end encrypted session to it through the relay, and drive the host's
// app-server with the exact same line-JSON protocol the desktop uses.
//
// The client owns reconnection: on every transport drop it re-handshakes and
// attaches with the last applied host seq, so the host either replays the
// missed lines (resumed) or hands out a fresh app-server connection the
// caller re-initializes against (not resumed; watch onAttach).

import { b64decode, b64encode } from "./b64.js";
import { utf8Decode, utf8Encode } from "./bytes.js";
import {
  Channel,
  Handshake,
  Identity,
  decodeKey,
  encodeKey,
  newHandshake,
  parsePairURI,
  sealPairOffer,
} from "./secure.js";
import {
  E2E_ACK,
  E2E_ATTACH,
  E2E_ATTACHED,
  E2E_BYE,
  E2E_HS1,
  E2E_HS2,
  E2E_PING,
  E2E_PONG,
  E2E_RPC,
  E2E_STATE,
  E2EMsg,
  HostInfo,
  KIND_HANDSHAKE,
  KIND_SEALED,
  PROTO_VERSION,
  ROLE_PHONE,
  RelayMsg,
  RunningTurn,
  TYPE_AUTH,
  TYPE_AUTH_OK,
  TYPE_CHALLENGE,
  TYPE_DELIVER_ERR,
  TYPE_FRAME,
  TYPE_HELLO,
  TYPE_PAIR_ANSWER,
  TYPE_PAIR_ERR,
  TYPE_PAIR_OFFER,
  TYPE_PRESENCE,
  splitPayload,
  wrapPayload,
} from "./wire.js";
import { ProtocolClient, ProtocolEnvelope, ServerRequestHandler } from "./rpc.js";

// --- Transport injection -------------------------------------------------------

/** The subset of the standard WebSocket surface the client needs. Satisfied
 *  by browsers, React Native, and Node 22+. */
export interface WebSocketLike {
  send(data: string): void;
  close(): void;
  addEventListener(type: "open" | "message" | "close" | "error", listener: (ev: never) => void): void;
}

export type WebSocketFactory = (url: string) => WebSocketLike;

function defaultWsFactory(url: string): WebSocketLike {
  const WS = (globalThis as { WebSocket?: new (url: string) => WebSocketLike }).WebSocket;
  if (!WS) throw new Error("no WebSocket implementation available; pass wsFactory");
  return new WS(url);
}

// --- Credentials ---------------------------------------------------------------

/** Everything a phone needs to reach its paired host. Field names match the
 *  Go credentials file so stores are interchangeable. Persist via secure
 *  storage (Keychain/Keystore) on mobile. */
export interface Credentials {
  v: number;
  device_seed: string; // base64url Ed25519 seed
  device_name?: string;
  host_pub: string; // base64url, pinned at pairing
  host_name?: string;
  relay_url: string;
}

// --- Socket helpers ------------------------------------------------------------

class SocketClosed extends Error {
  constructor(msg = "relay socket closed") {
    super(msg);
  }
}

/** Wraps a WebSocketLike into awaitable open/read, mirroring the reference
 *  client's sequential readMsg loop. */
class RelaySocket {
  private queue: RelayMsg[] = [];
  private waiter: { resolve: (m: RelayMsg) => void; reject: (e: Error) => void } | null = null;
  private openPromise: Promise<void>;
  private closedErr: Error | null = null;

  constructor(readonly ws: WebSocketLike) {
    let openResolve!: () => void;
    let openReject!: (e: Error) => void;
    this.openPromise = new Promise<void>((resolve, reject) => {
      openResolve = resolve;
      openReject = reject;
    });
    // Some factories (tests, pre-opened sockets) deliver open synchronously
    // before listeners attach; a readyState check papers over none of our
    // targets, so the factory contract is: events fire asynchronously.
    ws.addEventListener("open", () => openResolve());
    ws.addEventListener("message", (ev: unknown) => {
      const data = (ev as { data?: unknown }).data;
      if (typeof data !== "string") return;
      let msg: RelayMsg;
      try {
        msg = JSON.parse(data) as RelayMsg;
      } catch {
        return;
      }
      if (!msg || typeof msg.type !== "string") return;
      if (this.waiter) {
        const w = this.waiter;
        this.waiter = null;
        w.resolve(msg);
      } else {
        this.queue.push(msg);
      }
    });
    const fail = (reason: string) => {
      const err = new SocketClosed(reason);
      this.closedErr = err;
      openReject(err);
      if (this.waiter) {
        const w = this.waiter;
        this.waiter = null;
        w.reject(err);
      }
    };
    ws.addEventListener("close", () => fail("relay socket closed"));
    ws.addEventListener("error", () => fail("relay socket error"));
    // Swallow the unhandled-rejection that occurs when open succeeds first.
    this.openPromise.catch(() => {});
  }

  async waitOpen(timeoutMs: number): Promise<void> {
    let timer: ReturnType<typeof setTimeout> | undefined;
    const timeout = new Promise<never>((_, reject) => {
      timer = setTimeout(() => reject(new Error("relay dial timeout")), timeoutMs);
    });
    try {
      await Promise.race([this.openPromise, timeout]);
    } finally {
      clearTimeout(timer);
    }
  }

  async read(): Promise<RelayMsg> {
    if (this.queue.length > 0) return this.queue.shift()!;
    if (this.closedErr) throw this.closedErr;
    if (this.waiter) throw new Error("concurrent relay reads are not supported");
    return new Promise<RelayMsg>((resolve, reject) => {
      this.waiter = { resolve, reject };
    });
  }

  write(msg: RelayMsg): void {
    if (this.closedErr) throw this.closedErr;
    this.ws.send(JSON.stringify(msg));
  }

  close(): void {
    try {
      this.ws.close();
    } catch {
      // Already closed.
    }
  }
}

// --- Pairing -------------------------------------------------------------------

export interface PairOptions {
  wsFactory?: WebSocketFactory;
  timeoutMs?: number;
  platform?: string;
}

/** Completes the pairing exchange for a scanned pairing URI and returns
 *  ready-to-store credentials. The relay never sees anything it could use. */
export async function pair(uri: string, deviceName: string, opts: PairOptions = {}): Promise<Credentials> {
  const link = parsePairURI(uri);
  const id = Identity.generate();
  const { payload, pairing } = sealPairOffer(link, {
    devicePub: id.public_(),
    name: deviceName,
    platform: opts.platform,
  });

  const sock = new RelaySocket((opts.wsFactory ?? defaultWsFactory)(link.relayUrl));
  const timeoutMs = opts.timeoutMs ?? 30_000;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const deadline = new Promise<never>((_, reject) => {
    timer = setTimeout(() => reject(new Error("pairing timeout")), timeoutMs);
  });
  deadline.catch(() => {}); // No unhandled rejection when run() wins the race.
  try {
    return await Promise.race([run(), deadline]);
  } finally {
    clearTimeout(timer);
    sock.close();
  }

  async function run(): Promise<Credentials> {
    await sock.waitOpen(timeoutMs);
    sock.write({ type: TYPE_PAIR_OFFER, pairing_id: link.pairingId, payload: b64encode(payload) });
    for (;;) {
      const msg = await sock.read();
      switch (msg.type) {
        case TYPE_PAIR_ANSWER: {
          const answer = pairing.openPairAnswer(b64decode(msg.payload ?? ""), id.public_());
          return {
            v: 1,
            device_seed: b64encode(id.seed()),
            device_name: deviceName,
            host_pub: encodeKey(answer.hostPub),
            host_name: answer.hostName,
            relay_url: link.relayUrl,
          };
        }
        case TYPE_PAIR_ERR:
          throw new Error(`pairing rejected: ${msg.code ?? "unknown"}`);
        default:
          break; // Ignore anything else.
      }
    }
  }
}

// --- Client --------------------------------------------------------------------

export interface HostState {
  ver: number;
  host: HostInfo;
  running: RunningTurn[];
}

/** Reports a completed attach. When resumed is false the phone got a fresh
 *  app-server connection: rpc() returns a new client and any previously
 *  fetched state must be refetched. */
export interface AttachEvent {
  session: string;
  resumed: boolean;
}

export interface RemoteClientOptions {
  wsFactory?: WebSocketFactory;
  logf?: (msg: string) => void;
  reconnectMinMs?: number;
  reconnectMaxMs?: number;
  dialTimeoutMs?: number;
  ackIntervalMs?: number;
  pingIntervalMs?: number;
  onNotification?: (method: string, params: unknown) => void;
  onServerRequest?: ServerRequestHandler;
  onState?: (state: HostState) => void;
  onAttach?: (ev: AttachEvent) => void;
}

/** Keeps one phone connected to its paired host. */
export class RemoteClient {
  private readonly id: Identity;
  private readonly hostPub: Uint8Array;
  private readonly hostPubEncoded: string;
  private readonly relayUrl: string;
  private readonly wsFactory: WebSocketFactory;
  private readonly logf: (msg: string) => void;
  private readonly reconnectMinMs: number;
  private readonly reconnectMaxMs: number;
  private readonly dialTimeoutMs: number;
  private readonly ackIntervalMs: number;
  private readonly pingIntervalMs: number;

  private sock: RelaySocket | null = null;
  private channel: Channel | null = null;
  private pendingHS: Handshake | null = null;
  private attached = false;
  private contId = "";
  private lastRecv = 0;
  private ackDirty = false;
  private proto: ProtocolClient | null = null;
  private state: HostState | null = null;
  private waiters: Array<() => void> = [];
  private stopped = true;
  private runPromise: Promise<void> | null = null;
  private wake: (() => void) | null = null;
  private ackTimer: ReturnType<typeof setInterval> | undefined;
  private pingTimer: ReturnType<typeof setInterval> | undefined;

  constructor(
    creds: Credentials,
    private readonly opts: RemoteClientOptions = {},
  ) {
    this.id = Identity.fromSeed(b64decode(creds.device_seed));
    this.hostPub = decodeKey(creds.host_pub);
    this.hostPubEncoded = encodeKey(this.hostPub);
    this.relayUrl = creds.relay_url;
    this.wsFactory = opts.wsFactory ?? defaultWsFactory;
    this.logf = opts.logf ?? (() => {});
    this.reconnectMinMs = opts.reconnectMinMs ?? 1000;
    this.reconnectMaxMs = opts.reconnectMaxMs ?? 30_000;
    this.dialTimeoutMs = opts.dialTimeoutMs ?? 30_000;
    this.ackIntervalMs = opts.ackIntervalMs ?? 500;
    this.pingIntervalMs = opts.pingIntervalMs ?? 20_000;
  }

  /** Starts the connect-and-reconnect loop. Idempotent. */
  start(): void {
    if (!this.stopped) return;
    this.stopped = false;
    this.runPromise = this.run();
  }

  /** Stops the loop and tears down the connection. */
  async stop(): Promise<void> {
    if (this.stopped) return;
    this.stopped = true;
    this.wake?.();
    this.sock?.close();
    await this.runPromise;
    this.proto?.close("client stopped");
  }

  private async run(): Promise<void> {
    let attempt = 0;
    while (!this.stopped) {
      let authed = false;
      try {
        authed = await this.runOnce();
      } catch (err) {
        // Factory/setup failures land here; read-loop drops are handled (and
        // logged) inside runOnce.
        if (!this.stopped) {
          this.logf(`remote client: connect failed: ${err instanceof Error ? err.message : String(err)}`);
        }
      }
      if (this.stopped) return;
      if (authed) attempt = 0;
      let delay = this.reconnectMinMs * 2 ** attempt;
      if (delay > this.reconnectMaxMs || delay <= 0) {
        delay = this.reconnectMaxMs;
      } else if (attempt < 30) {
        attempt++;
      }
      await this.sleep(delay);
    }
  }

  private sleep(ms: number): Promise<void> {
    return new Promise((resolve) => {
      const timer = setTimeout(() => {
        this.wake = null;
        resolve();
      }, ms);
      this.wake = () => {
        clearTimeout(timer);
        this.wake = null;
        resolve();
      };
    });
  }

  /** One connection lifetime. Returns whether relay auth succeeded, so the
   *  reconnect loop resets its backoff (mirrors the reference client). */
  private async runOnce(): Promise<boolean> {
    const sock = new RelaySocket(this.wsFactory(this.relayUrl));
    this.sock = sock;
    let authed = false;
    try {
      await sock.waitOpen(this.dialTimeoutMs);
      const hostOnline = await this.authenticate(sock);
      authed = true;
      if (hostOnline) this.startHandshake();
      this.startLoops();
      for (;;) {
        this.dispatch(await sock.read());
      }
    } catch (err) {
      if (!this.stopped) {
        this.logf(`remote client: connection lost: ${err instanceof Error ? err.message : String(err)}`);
      }
      return authed;
    } finally {
      this.stopLoops();
      if (this.sock === sock) this.sock = null;
      this.channel = null;
      this.pendingHS = null;
      this.attached = false;
      sock.close();
    }
  }

  private async authenticate(sock: RelaySocket): Promise<boolean> {
    sock.write({ type: TYPE_HELLO, proto: PROTO_VERSION, role: ROLE_PHONE, pub: encodeKey(this.id.public_()) });
    const challenge = await sock.read();
    if (challenge.type !== TYPE_CHALLENGE) {
      throw new Error(`relay: expected challenge, got ${challenge.type} (${challenge.msg ?? ""})`);
    }
    const nonce = b64decode(challenge.nonce ?? "");
    sock.write({ type: TYPE_AUTH, sig: b64encode(this.id.signRelayAuth(nonce, ROLE_PHONE)) });
    const ok = await sock.read();
    if (ok.type !== TYPE_AUTH_OK) {
      throw new Error(`relay auth failed: ${ok.code ?? ""} (${ok.msg ?? ""})`);
    }
    return ok.online === true;
  }

  private dispatch(msg: RelayMsg): void {
    switch (msg.type) {
      case TYPE_FRAME: {
        let payload: Uint8Array;
        try {
          payload = b64decode(msg.payload ?? "");
        } catch {
          return;
        }
        if (payload.length < 1) return;
        const { kind, body } = splitPayload(payload);
        if (kind === KIND_HANDSHAKE) this.handleHS2(body);
        else if (kind === KIND_SEALED) this.handleSealed(body);
        return;
      }
      case TYPE_PRESENCE:
        if (msg.pub === this.hostPubEncoded && msg.online !== undefined) {
          if (msg.online) this.startHandshake();
          else this.markDetached();
        }
        return;
      case TYPE_DELIVER_ERR:
        this.markDetached();
        return;
      default:
        return;
    }
  }

  /** Sends a fresh HS1 toward the host. */
  private startHandshake(): void {
    let hs;
    try {
      hs = newHandshake(this.id, this.hostPub);
    } catch {
      return;
    }
    this.pendingHS = hs.handshake;
    this.channel = null;
    this.attached = false;
    const body = utf8Encode(
      JSON.stringify({
        t: E2E_HS1,
        device_pub: hs.hs1.device_pub,
        eph: hs.hs1.eph,
        nonce: hs.hs1.nonce,
        sig: hs.hs1.sig,
      } satisfies E2EMsg),
    );
    try {
      this.sendFrame(wrapPayload(KIND_HANDSHAKE, body));
    } catch {
      // Transport gone; the read loop will notice and reconnect.
    }
  }

  private handleHS2(body: Uint8Array): void {
    let msg: E2EMsg;
    try {
      msg = JSON.parse(utf8Decode(body)) as E2EMsg;
    } catch {
      return;
    }
    if (msg.t !== E2E_HS2 || !this.pendingHS) return;
    let channel: Channel;
    try {
      channel = this.pendingHS.complete({ eph: msg.eph ?? "", nonce: msg.nonce ?? "", sig: msg.sig ?? "" });
    } catch (err) {
      this.logf(`remote client: handshake failed: ${err instanceof Error ? err.message : String(err)}`);
      this.pendingHS = null;
      return;
    }
    this.pendingHS = null;
    this.channel = channel;
    // Attach immediately: quote the previous continuity id and the highest
    // applied seq so the host can replay exactly what we missed.
    this.sendSealed({ t: E2E_ATTACH, prev: this.contId, recv: this.lastRecv });
  }

  private handleSealed(body: Uint8Array): void {
    const channel = this.channel;
    if (!channel) return;
    let plain: Uint8Array;
    try {
      plain = channel.open(body);
    } catch {
      return;
    }
    let msg: E2EMsg;
    try {
      msg = JSON.parse(utf8Decode(plain)) as E2EMsg;
    } catch {
      return;
    }
    switch (msg.t) {
      case E2E_ATTACHED:
        this.handleAttached(msg);
        return;
      case E2E_RPC: {
        const seq = msg.seq ?? 0;
        if (!this.attached || seq <= this.lastRecv) return;
        this.lastRecv = seq;
        this.ackDirty = true;
        if (this.proto && msg.line !== undefined && msg.line !== null) {
          this.proto.feed(msg.line as ProtocolEnvelope);
        }
        return;
      }
      case E2E_STATE: {
        const state: HostState = { ver: msg.ver ?? 0, host: msg.host ?? {}, running: msg.running ?? [] };
        this.state = state;
        this.opts.onState?.(state);
        return;
      }
      case E2E_PONG:
        return;
      case E2E_BYE:
        this.channel = null;
        this.attached = false;
        return;
      default:
        return;
    }
  }

  private handleAttached(msg: E2EMsg): void {
    const session = msg.session ?? "";
    const resumed = msg.resumed === true;
    const fresh = !resumed || this.proto === null || session !== this.contId;
    this.contId = session;
    this.attached = true;
    this.ackDirty = false;
    if (fresh) {
      this.proto?.close("superseded by fresh app-server connection");
      this.lastRecv = 0;
      this.proto = new ProtocolClient((env) => this.writeRPC(env), {
        onNotification: this.opts.onNotification,
        onServerRequest: this.opts.onServerRequest,
        idPrefix: "rc",
      });
    }
    for (const w of this.waiters) w();
    this.waiters = [];
    this.opts.onAttach?.({ session, resumed });
    this.logf(`remote client: attached (session=${session} resumed=${resumed})`);
  }

  private markDetached(): void {
    this.channel = null;
    this.attached = false;
  }

  private startLoops(): void {
    // Ack loop: flush cumulative acks so the host can trim its spool.
    this.ackTimer = setInterval(() => {
      if (this.attached && this.ackDirty && this.channel) {
        this.ackDirty = false;
        this.sendSealed({ t: E2E_ACK, recv: this.lastRecv });
      }
    }, this.ackIntervalMs);
    // Ping loop: sealed e2e pings (browser sockets cannot send transport
    // pings); the host answers pong, keeping intermediaries and its own
    // liveness view warm.
    this.pingTimer = setInterval(() => {
      if (this.channel) this.sendSealed({ t: E2E_PING });
    }, this.pingIntervalMs);
  }

  private stopLoops(): void {
    clearInterval(this.ackTimer);
    clearInterval(this.pingTimer);
  }

  /** Seals and sends one e2e message; drops the channel on send failure. */
  private sendSealed(msg: E2EMsg): void {
    const channel = this.channel;
    if (!channel) return;
    const sealed = channel.seal(utf8Encode(JSON.stringify(msg)));
    try {
      this.sendFrame(wrapPayload(KIND_SEALED, sealed));
    } catch {
      this.channel = null;
      this.attached = false;
    }
  }

  private sendFrame(payload: Uint8Array): void {
    const sock = this.sock;
    if (!sock) throw new Error("relay not connected");
    sock.write({ type: TYPE_FRAME, to: this.hostPubEncoded, payload: b64encode(payload) });
  }

  /** Uplink rpc lines are deliberately unnumbered: at-most-once, callers see
   *  missing responses fail and retry at their own layer. */
  private writeRPC(env: ProtocolEnvelope): void {
    if (!this.attached || !this.channel) throw new Error("not attached to host");
    this.sendSealed({ t: E2E_RPC, line: env });
  }

  /** Resolves once the client is attached to the host. */
  waitAttached(timeoutMs?: number): Promise<void> {
    if (this.attached) return Promise.resolve();
    return new Promise((resolve, reject) => {
      let timer: ReturnType<typeof setTimeout> | undefined;
      const waiter = () => {
        clearTimeout(timer);
        resolve();
      };
      this.waiters.push(waiter);
      if (timeoutMs !== undefined) {
        timer = setTimeout(() => {
          this.waiters = this.waiters.filter((w) => w !== waiter);
          reject(new Error("attach timeout"));
        }, timeoutMs);
      }
    });
  }

  /** The app-server protocol client for the current connection, or null
   *  before the first attach. After an AttachEvent with resumed=false, call
   *  this again: the previous client is dead. */
  rpc(): ProtocolClient | null {
    return this.proto;
  }

  /** Convenience: waits for attach and issues one call. */
  async call<T = unknown>(method: string, params?: unknown): Promise<T> {
    await this.waitAttached();
    const proto = this.proto;
    if (!proto) throw new Error("not attached to host");
    return proto.call<T>(method, params);
  }

  isAttached(): boolean {
    return this.attached;
  }

  latestState(): HostState | null {
    return this.state;
  }
}
