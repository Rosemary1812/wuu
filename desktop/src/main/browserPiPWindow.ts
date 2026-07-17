import { BrowserWindow, type Rectangle } from "electron";
import type { ActivitySession } from "../shared/protocol";
import { appShellWebPreferences } from "./appShellGuards";
import type {
  BrowserHostCoordinator,
  BrowserInteractionHint,
  BrowserTabFrame,
  BrowserTabSurfaceMeta,
} from "./browserHostWindows";
import {
  frameStreamRetryDelay,
  type ObservationPiPEventSink,
  type ObservationPiPFactory,
  type ObservationPiPHandle,
} from "./cuaActivityWindows";
import { CUANativePiP, resolveCUAFrameHelper } from "./cuaFrameStreams";

// The protocol carries interaction inline on ActivitySession; reuse that shape.
type Interaction = NonNullable<ActivitySession["interaction"]>;

// ---------------------------------------------------------------------------
// Frame source. The surface reads frames + page identity through this narrow
// interface; the production adapter binds a (workdir, tabID) pair of the
// BrowserHostCoordinator, tests bind fakes.
// ---------------------------------------------------------------------------
export interface BrowserFrameSource {
  // One fresh frame; undefined means the tab/view is gone for good.
  capture(): Promise<BrowserTabFrame | undefined>;
  meta(): BrowserTabSurfaceMeta | undefined;
  onClosed(listener: () => void): () => void;
  onInteraction(listener: (hint: BrowserInteractionHint) => void): () => void;
}

export function browserFrameSourceFor(
  host: BrowserHostCoordinator,
  workdir: string,
  tabID: string,
): BrowserFrameSource {
  return {
    capture: () => host.captureTabFrame(workdir, tabID),
    meta: () => host.tabSurfaceMeta(workdir, tabID),
    onClosed: (listener) =>
      host.addTabClosedListener((w, t) => {
        if (w === workdir && t === tabID) listener();
      }),
    onInteraction: (listener) =>
      host.addInteractionListener((w, t, hint) => {
        if (w === workdir && t === tabID) listener(hint);
      }),
  };
}

// ---------------------------------------------------------------------------
// Pure helpers (unit-tested).
// ---------------------------------------------------------------------------

// FNV-1a 32-bit. Frame dedupe only needs change detection, not crypto.
export function pipFrameHash(buffer: Buffer): string {
  let hash = 0x811c9dc5;
  for (let i = 0; i < buffer.length; i++) {
    hash ^= buffer[i];
    hash = Math.imul(hash, 0x01000193) >>> 0;
  }
  return hash.toString(16);
}

export function pipHostname(url: string): string {
  const match = /^[a-zA-Z][a-zA-Z0-9+.-]*:\/\/([^/?#]+)/.exec(url);
  return match?.[1] ?? (url.trim() || "about:blank");
}

// object-fit: contain geometry for mapping page CSS coordinates into the
// surface's content box.
export function pipContainRect(
  contentW: number,
  contentH: number,
  boxW: number,
  boxH: number,
): { x: number; y: number; width: number; height: number; scale: number } {
  if (contentW <= 0 || contentH <= 0 || boxW <= 0 || boxH <= 0) {
    return { x: 0, y: 0, width: 0, height: 0, scale: 0 };
  }
  const scale = Math.min(boxW / contentW, boxH / contentH);
  const width = contentW * scale;
  const height = contentH * scale;
  return { x: (boxW - width) / 2, y: (boxH - height) / 2, width, height, scale };
}

export function pipMapPoint(
  pointX: number,
  pointY: number,
  contentW: number,
  contentH: number,
  boxW: number,
  boxH: number,
): { x: number; y: number } {
  const rect = pipContainRect(contentW, contentH, boxW, boxH);
  return { x: rect.x + pointX * rect.scale, y: rect.y + pointY * rect.scale };
}

// ---------------------------------------------------------------------------
// Frame pump. Owns the capture cadence for one tab: ~3fps with a single
// in-flight capture (that is the backpressure — a slow capture simply delays
// the next tick), PNG-hash dedupe so a static page stops producing sends, and
// exponential backoff on transient capture errors. A missing tab is fatal and
// reported exactly once; the pump never touches the browser control path.
// ---------------------------------------------------------------------------
export const BROWSER_PIP_FRAME_INTERVAL_MS = 333;

export type BrowserPipFrame = BrowserTabFrame & BrowserTabSurfaceMeta;

export class BrowserFramePump {
  private timer: NodeJS.Timeout | undefined;
  private inFlight = false;
  private running = false;
  private failures = 0;
  private lastHash: string | undefined;
  private lastMetaKey: string | undefined;
  private firstFrameSent = false;

  constructor(
    private readonly source: Pick<BrowserFrameSource, "capture" | "meta">,
    private readonly callbacks: {
      onFrame(frame: BrowserPipFrame): void;
      onGone(): void;
      onFirstFrame?(): void;
    },
    private readonly intervalMs: number = BROWSER_PIP_FRAME_INTERVAL_MS,
  ) {}

  start(): void {
    if (this.running) return;
    this.running = true;
    this.schedule(0);
  }

  pause(): void {
    this.running = false;
    this.clearTimer();
  }

  stop(): void {
    this.pause();
  }

  isRunning(): boolean {
    return this.running;
  }

  private schedule(delay: number): void {
    this.clearTimer();
    if (!this.running) return;
    this.timer = setTimeout(() => {
      void this.tick();
    }, delay);
    this.timer.unref?.();
  }

  private clearTimer(): void {
    if (this.timer) {
      clearTimeout(this.timer);
      this.timer = undefined;
    }
  }

  private async tick(): Promise<void> {
    if (!this.running) return;
    if (this.inFlight) {
      // Previous capture still running: skip this beat, keep the cadence.
      this.schedule(this.intervalMs);
      return;
    }
    this.inFlight = true;
    try {
      const frame = await this.source.capture();
      if (frame === undefined) {
        this.running = false;
        this.callbacks.onGone();
        return;
      }
      this.failures = 0;
      const meta = this.source.meta() ?? { url: "", title: "" };
      const hash = pipFrameHash(frame.png);
      const metaKey = `${meta.url}\n${meta.title}`;
      if (hash !== this.lastHash || metaKey !== this.lastMetaKey) {
        this.lastHash = hash;
        this.lastMetaKey = metaKey;
        this.callbacks.onFrame({ ...frame, ...meta });
        if (!this.firstFrameSent) {
          this.firstFrameSent = true;
          this.callbacks.onFirstFrame?.();
        }
      }
      this.schedule(this.intervalMs);
    } catch {
      this.failures += 1;
      this.schedule(frameStreamRetryDelay(this.failures));
    } finally {
      this.inFlight = false;
    }
  }
}

// ---------------------------------------------------------------------------
// The browser PiP surface: an Electron panel window presenting pumped frames
// with the CUA PiP's visual language — same default size and corner parking,
// frosted placeholder until the first frame, synthetic pointer overlay, hover
// close. It shows pixels only; the page itself never leaves the hidden host,
// so the preview can never steal focus or become an input target.
// ---------------------------------------------------------------------------

type BrowserPiPSurfaceDeps = {
  activity: ActivitySession;
  bounds: Rectangle;
  sink: ObservationPiPEventSink;
  source: BrowserFrameSource;
  isPackaged: boolean;
  // Injectable for tests; production uses the real BrowserWindow.
  createWindow?: (bounds: Rectangle) => BrowserPiPWindowHandle;
};

export type BrowserPiPWindowHandle = {
  webContents: {
    executeJavaScript(code: string, userGesture?: boolean): Promise<unknown>;
    setWindowOpenHandler(handler: () => { action: "deny" }): void;
    on(event: "will-navigate" | "did-finish-load", listener: (...args: unknown[]) => void): void;
  };
  setAlwaysOnTop(flag: boolean, level?: string): void;
  setVisibleOnAllWorkspaces(visible: boolean, options?: { visibleOnFullScreen?: boolean }): void;
  showInactive(): void;
  hide(): void;
  isDestroyed(): boolean;
  isVisible(): boolean;
  getBounds(): Rectangle;
  on(event: "moved" | "resized" | "closed" | "ready-to-show", listener: (...args: unknown[]) => void): void;
  loadURL(url: string): Promise<unknown>;
  destroy(): void;
};

export class BrowserPiPSurface implements ObservationPiPHandle {
  private win: BrowserPiPWindowHandle | undefined;
  private loaded = false;
  private readonly pump: BrowserFramePump;
  private readonly unsubs: Array<() => void> = [];
  private visible = false;
  private live = true;
  private stopped = false;
  private activity: ActivitySession;
  private lastInteractionRevision = 0;
  private frameSeq = 0;
  private pendingFrame: (BrowserPipFrame & { dataUrl: string }) | undefined;

  constructor(private readonly deps: BrowserPiPSurfaceDeps) {
    this.activity = deps.activity;
    this.pump = new BrowserFramePump(deps.source, {
      onFrame: (frame) => this.presentFrame(frame),
      onGone: () => deps.sink.onGone(),
      onFirstFrame: () => deps.sink.onEvent({ event: "ready" }),
    });
  }

  start(): void {
    if (this.stopped || this.win) return;
    const win = this.deps.createWindow
      ? this.deps.createWindow(this.deps.bounds)
      : this.createElectronWindow(this.deps.bounds);
    this.win = win;
    win.webContents.setWindowOpenHandler(() => ({ action: "deny" }));
    win.webContents.on("will-navigate", (event: unknown, rawURL: unknown) => {
      if (typeof rawURL === "string" && rawURL.startsWith("wuu-pip://")) {
        (event as { preventDefault?: () => void }).preventDefault?.();
        if (rawURL === "wuu-pip://close") this.deps.sink.onEvent({ event: "user_close" });
      }
    });
    win.webContents.on("did-finish-load", () => {
      if (win.isDestroyed() || this.win !== win) return;
      this.loaded = true;
      const pending = this.pendingFrame;
      this.pendingFrame = undefined;
      if (pending) this.pushFrame(pending);
      this.pushActivityState();
      if (!win.isVisible() && this.visible) win.showInactive();
    });
    win.on("ready-to-show", () => {
      if (win.isDestroyed() || this.win !== win) return;
      if (this.visible) win.showInactive();
    });
    const reportGeometry = (): void => {
      if (win.isDestroyed() || this.win !== win) return;
      const b = win.getBounds();
      this.deps.sink.onEvent({ event: "geometry", x: b.x, y: b.y, width: b.width, height: b.height });
    };
    win.on("moved", reportGeometry);
    win.on("resized", reportGeometry);
    win.on("closed", () => {
      if (this.win !== win) return;
      this.win = undefined;
      this.loaded = false;
      // The window is gone (app quit, or an external close): stop producing
      // frames even if the coordinator never called stop().
      this.visible = false;
      this.syncPump();
    });
    void win.loadURL(
      `data:text/html;charset=utf-8,${encodeURIComponent(
        browserPiPWindowHTML(pipHostname(this.deps.source.meta()?.url ?? "")),
      )}`,
    );
    this.unsubs.push(
      this.deps.source.onClosed(() => this.deps.sink.onGone()),
      this.deps.source.onInteraction((hint) =>
        this.forwardInteraction({
          kind: hint.kind,
          x: hint.x,
          y: hint.y,
          direction: hint.direction,
          revision: ++this.lastInteractionRevision,
        }),
      ),
    );
    this.syncPump();
  }

  setVisible(visible: boolean): void {
    this.visible = visible;
    const win = this.win;
    if (!win || win.isDestroyed()) return;
    if (visible) {
      win.showInactive();
    } else {
      win.hide();
    }
    this.syncPump();
  }

  // Freeze/resume frame production. A stopped activity keeps its last frame
  // on screen (CUA observation semantics) without burning captures.
  setLive(live: boolean): void {
    this.live = live;
    this.syncPump();
  }

  updateActivity(activity: ActivitySession): void {
    this.activity = activity;
    this.pushActivityState();
  }

  animateInteraction(interaction: Interaction): void {
    if (interaction.revision <= this.lastInteractionRevision) return;
    this.lastInteractionRevision = interaction.revision;
    this.forwardInteraction(interaction);
  }

  stop(onStopped?: () => void): void {
    if (this.stopped) {
      onStopped?.();
      return;
    }
    this.stopped = true;
    this.pump.stop();
    for (const unsub of this.unsubs.splice(0)) unsub();
    const win = this.win;
    this.win = undefined;
    if (win && !win.isDestroyed()) {
      try {
        win.destroy();
      } catch {
        // Already closing.
      }
    }
    onStopped?.();
  }

  private syncPump(): void {
    if (this.stopped) return;
    if (this.visible && this.live) {
      this.pump.start();
    } else {
      this.pump.pause();
    }
  }

  private presentFrame(frame: BrowserPipFrame): void {
    const payload = {
      ...frame,
      dataUrl: `data:image/png;base64,${frame.png.toString("base64")}`,
      seq: ++this.frameSeq,
    };
    if (!this.loaded) {
      this.pendingFrame = payload;
      return;
    }
    this.pushFrame(payload);
  }

  private pushFrame(payload: BrowserPipFrame & { dataUrl: string; seq?: number }): void {
    this.execute(
      `window.wuuPipFrame?.(${JSON.stringify({
        dataUrl: payload.dataUrl,
        url: payload.url,
        title: payload.title,
        cssW: payload.width,
        cssH: payload.height,
        seq: payload.seq ?? 0,
      })})`,
    );
  }

  private pushActivityState(): void {
    if (!this.loaded) return;
    this.execute(
      `window.wuuPipState?.(${JSON.stringify({
        state: this.activity.state,
        controller: this.activity.controller,
      })})`,
    );
  }

  private forwardInteraction(interaction: Interaction): void {
    if (!this.loaded) return;
    this.execute(
      `window.wuuPipInteract?.(${JSON.stringify({
        kind: interaction.kind,
        x: interaction.x,
        y: interaction.y,
        direction: interaction.direction ?? "",
        revision: interaction.revision,
      })})`,
    );
  }

  private execute(code: string): void {
    const win = this.win;
    if (!win || win.isDestroyed()) return;
    void win.webContents.executeJavaScript(code, true).catch(() => undefined);
  }

  private createElectronWindow(bounds: Rectangle): BrowserPiPWindowHandle {
    const win = new BrowserWindow({
      width: bounds.width,
      height: bounds.height,
      x: bounds.x,
      y: bounds.y,
      frame: false,
      transparent: true,
      backgroundColor: "#00000000",
      hasShadow: false,
      alwaysOnTop: true,
      skipTaskbar: true,
      resizable: true,
      minimizable: false,
      maximizable: false,
      fullscreenable: false,
      acceptFirstMouse: true,
      show: false,
      type: "panel",
      minWidth: 220,
      minHeight: 140,
      webPreferences: {
        contextIsolation: true,
        nodeIntegration: false,
        sandbox: true,
        ...appShellWebPreferences(this.deps.isPackaged),
      },
    }) as unknown as BrowserPiPWindowHandle;
    win.setAlwaysOnTop(true, "floating");
    win.setVisibleOnAllWorkspaces(true, { visibleOnFullScreen: true });
    return win;
  }
}

// Factory covering both observation backends: CUA keeps its native helper,
// browser activities get the Electron surface. One coordinator, one surface,
// two frame sources.
export function createObservationPiPFactory(deps: {
  browserHost: BrowserHostCoordinator;
  isPackaged: boolean;
}): ObservationPiPFactory {
  return (activity, _key, sink, bounds) => {
    if (activity.kind === "browser") {
      const tabID = activity.target?.trim();
      if (!tabID) return undefined;
      return new BrowserPiPSurface({
        activity,
        bounds: bounds(),
        sink,
        source: browserFrameSourceFor(deps.browserHost, activity.workdir, tabID),
        isPackaged: deps.isPackaged,
      });
    }
    const helper = resolveCUAFrameHelper();
    const target = activity.target?.trim();
    if (!helper || !target) return undefined;
    return new CUANativePiP(
      helper,
      activity.thread_id,
      target,
      activity.process_id,
      activity.window_id,
      bounds(),
      sink.onEvent,
      sink.onFailure,
    );
  };
}

// ---------------------------------------------------------------------------
// Surface page. Sandboxed data: URL like the pet window; all updates arrive
// through window.wuuPip* hooks via executeJavaScript, user actions leave
// through wuu-pip:// navigations. The page never draws error text into the
// frame area — without fresh frames it simply keeps the last one (or the
// placeholder), matching the CUA PiP's "never paint failure" rule.
// ---------------------------------------------------------------------------
export function browserPiPWindowHTML(initialLabel: string): string {
  const label = JSON.stringify(initialLabel);
  return `<!doctype html>
<html><head><meta charset="utf-8" />
<style>
*{box-sizing:border-box;margin:0;padding:0}
html,body{width:100%;height:100%;overflow:hidden;background:transparent;
  font-family:-apple-system,BlinkMacSystemFont,"Helvetica Neue",sans-serif}
#root{position:relative;width:100%;height:100%;border-radius:12px;overflow:hidden;
  background:rgba(24,24,27,.92);box-shadow:0 8px 28px rgba(0,0,0,.35);
  -webkit-app-region:drag}
#view{position:absolute;inset:0;width:100%;height:100%;object-fit:contain;
  opacity:0;transition:opacity .25s ease}
#view.live{opacity:1}
#ph{position:absolute;inset:0;display:flex;align-items:center;justify-content:center;
  background:rgba(28,28,32,.72);backdrop-filter:blur(18px);-webkit-backdrop-filter:blur(18px);
  transition:opacity .25s ease;color:rgba(255,255,255,.55)}
#ph.gone{opacity:0;pointer-events:none}
#strip{position:absolute;top:0;left:0;right:0;height:26px;display:flex;align-items:center;
  gap:6px;padding:0 8px;background:linear-gradient(rgba(0,0,0,.55),rgba(0,0,0,0));
  opacity:0;transition:opacity .15s ease}
#root:hover #strip{opacity:1}
#dot{width:7px;height:7px;border-radius:50%;background:#34c759;flex:none}
#dot.waiting{background:#ff9f0a}
#dot.frozen{background:#8e8e93}
#host{flex:1;min-width:0;font-size:11px;line-height:1;color:rgba(255,255,255,.85);
  white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
#close{flex:none;width:16px;height:16px;border:none;border-radius:50%;
  background:rgba(255,255,255,.22);color:#fff;font-size:11px;line-height:16px;
  text-align:center;cursor:pointer;-webkit-app-region:no-drag;padding:0}
#close:hover{background:rgba(255,90,80,.9)}
#ptr{position:absolute;width:14px;height:14px;margin:-7px 0 0 -7px;border-radius:50%;
  background:rgba(255,255,255,.95);box-shadow:0 0 0 2px rgba(0,0,0,.45);
  opacity:0;pointer-events:none;transition:transform .14s ease-out,opacity .2s ease}
#ptr.on{opacity:1}
#ring{position:absolute;width:26px;height:26px;margin:-13px 0 0 -13px;border-radius:50%;
  border:2px solid rgba(255,255,255,.9);opacity:0;pointer-events:none}
#caret{position:absolute;width:2px;height:14px;margin:-7px 0 0 -1px;background:#fff;
  box-shadow:0 0 0 1px rgba(0,0,0,.4);opacity:0;pointer-events:none}
#scroll{position:absolute;font-size:14px;color:#fff;text-shadow:0 1px 2px rgba(0,0,0,.6);
  opacity:0;pointer-events:none;transform:translate(-50%,-50%)}
</style></head>
<body>
<div id="root">
  <img id="view" alt="" draggable="false" />
  <div id="ph"><svg width="30" height="30" viewBox="0 0 24 24" fill="none"
    stroke="currentColor" stroke-width="1.6" stroke-linecap="round">
    <circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c2.6 2.6 3.9 5.7 3.9 9s-1.3 6.4-3.9 9c-2.6-2.6-3.9-5.7-3.9-9S9.4 5.6 12 3z"/>
  </svg></div>
  <div id="strip"><span id="dot"></span><span id="host"></span>
    <button id="close" title="Close" aria-label="Close">✕</button></div>
  <div id="ring"></div><div id="ptr"></div><div id="caret"></div><div id="scroll"></div>
</div>
<script>
(function(){
  var host=document.getElementById("host");
  var view=document.getElementById("view");
  var ph=document.getElementById("ph");
  var dot=document.getElementById("dot");
  var ptr=document.getElementById("ptr");
  var ring=document.getElementById("ring");
  var caret=document.getElementById("caret");
  var scrollEl=document.getElementById("scroll");
  var page={w:0,h:0};
  var lastSeq=0;
  var hideTimer=0;
  host.textContent=${label};
  document.getElementById("close").addEventListener("click",function(){
    window.location.href="wuu-pip://close";
  });
  function fit(px,py){
    if(!page.w||!page.h)return{x:0,y:0};
    var w=window.innerWidth,h=window.innerHeight;
    var s=Math.min(w/page.w,h/page.h);
    return{x:(w-page.w*s)/2+px*s,y:(h-page.h*s)/2+py*s};
  }
  function place(el,p){el.style.transform="translate("+p.x+"px,"+p.y+"px)";}
  function ping(el){el.style.opacity="1";}
  window.wuuPipFrame=function(f){
    if(f.seq&&f.seq<=lastSeq)return;
    lastSeq=f.seq||lastSeq;
    page.w=f.cssW;page.h=f.cssH;
    view.src=f.dataUrl;
    view.classList.add("live");
    ph.classList.add("gone");
    if(f.url){try{host.textContent=new URL(f.url).host||f.url;}catch(e){host.textContent=f.url;}}
  };
  window.wuuPipState=function(s){
    dot.className=s.state==="waiting_confirmation"?"waiting":
      (s.state==="stopped"||s.controller==="none"?"frozen":"");
  };
  window.wuuPipInteract=function(it){
    var p=fit(it.x,it.y);
    ptr.style.transition="transform .14s ease-out,opacity .2s ease";
    place(ptr,p);ping(ptr);
    clearTimeout(hideTimer);
    hideTimer=setTimeout(function(){ptr.style.opacity="0";},1200);
    if(it.kind==="click"){
      place(ring,p);
      ring.style.opacity="0";
      ring.animate([{opacity:.9,transform:"translate("+p.x+"px,"+p.y+"px) scale(.4)"},
        {opacity:0,transform:"translate("+p.x+"px,"+p.y+"px) scale(1.4)"}],
        {duration:380,easing:"ease-out"});
    }else if(it.kind==="type"){
      place(caret,p);
      caret.animate([{opacity:1},{opacity:1}],{duration:600});
      caret.animate([{opacity:1},{opacity:0}],{duration:600,delay:600});
    }else if(it.kind==="scroll"){
      var ch={up:"↑",down:"↓",left:"←",right:"→"}[it.direction]||"↕";
      scrollEl.textContent=ch;
      scrollEl.animate([{opacity:.95,transform:"translate("+p.x+"px,"+p.y+"px)"},
        {opacity:0,transform:"translate("+p.x+"px,"+(p.y+(it.direction==="up"?10:-10))+"px)"}],
        {duration:520,easing:"ease-out"});
    }
  };
})();
</script>
</body></html>`;
}
