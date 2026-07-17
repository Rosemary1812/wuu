import { BrowserWindow, WebContentsView, type Rectangle } from "electron";
import type { ActivitySession } from "../shared/protocol";
import { appShellWebPreferences } from "./appShellGuards";
import type {
  BrowserHostCoordinator,
  BrowserInteractionHint,
  BrowserParentWindowHandle,
  BrowserViewHandle,
} from "./browserHostWindows";
import {
  type ObservationPiPEventSink,
  type ObservationPiPFactory,
  type ObservationPiPHandle,
} from "./cuaActivityWindows";
import { CUANativePiP, resolveCUAFrameHelper } from "./cuaFrameStreams";

// The protocol carries interaction inline on ActivitySession; reuse that shape.
type Interaction = NonNullable<ActivitySession["interaction"]>;

// The host surface the PiP needs from the BrowserHostCoordinator: mount and
// geometry for the real tab view, chrome metadata, and lifecycle listeners.
export type BrowserPiPTabHost = Pick<
  BrowserHostCoordinator,
  | "tabBounds"
  | "tabSurfaceMeta"
  | "mountTabOnWindow"
  | "unmountTabIfOwner"
  | "relayoutMountedTab"
  | "addInteractionListener"
  | "addTabClosedListener"
  | "addNavigateListener"
  | "addTabReparentedListener"
>;

// ---------------------------------------------------------------------------
// Pure helpers (unit-tested).
// ---------------------------------------------------------------------------

export function pipHostname(url: string): string {
  const match = /^[a-zA-Z][a-zA-Z0-9+.-]*:\/\/([^/?#]+)/.exec(url);
  return match?.[1] ?? (url.trim() || "about:blank");
}

// Contain-fit geometry: shrink the tab's layout viewport into the PiP content
// box without changing it. scale doubles as the UI zoom factor — view DIP
// size = viewport × scale, so CSS layout stays at viewport size and every
// CDP coordinate the agent uses keeps its meaning.
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

// A tab whose real bounds were never established (hidden host, zero-size
// view) still gets a deterministic viewport for contain math.
const PIP_FALLBACK_VIEWPORT = { width: 1280, height: 800 };

// ---------------------------------------------------------------------------
// The browser PiP surface: an Electron panel window presenting the REAL tab
// view, reparented off the hidden host and zoom-fitted into the window —
// the same surface the agent acts on, never a re-encoded frame stream. A
// transparent overlay view above it draws the chrome strip, the interaction
// effects, and the frosted placeholder, and swallows pointer input so the
// preview can never steal focus or become an input target.
// ---------------------------------------------------------------------------

export type BrowserPiPWindowHandle = BrowserParentWindowHandle & {
  setAlwaysOnTop(flag: boolean, level?: string): void;
  setVisibleOnAllWorkspaces(visible: boolean, options?: { visibleOnFullScreen?: boolean }): void;
  showInactive(): void;
  hide(): void;
  isVisible(): boolean;
  getBounds(): Rectangle;
  on(
    event: "close" | "closed" | "moved" | "resized" | "ready-to-show",
    listener: (...args: unknown[]) => void,
  ): void;
  destroy(): void;
};

// The overlay is a transparent WebContentsView stacked above the content
// view. It is the only page the PiP loads; updates arrive through the
// window.wuuPip* hooks, user actions leave through wuu-pip:// navigations.
export interface BrowserPiPOverlayHandle {
  readonly webContents: {
    loadURL(url: string): Promise<unknown>;
    executeJavaScript(code: string, userGesture?: boolean): Promise<unknown>;
    setWindowOpenHandler(handler: () => { action: "deny" }): void;
    on(event: "will-navigate", listener: (event: unknown, url: string) => void): void;
    isDestroyed(): boolean;
  };
  setBounds(bounds: Rectangle): void;
}

type BrowserPiPSurfaceDeps = {
  activity: ActivitySession;
  bounds: Rectangle;
  sink: ObservationPiPEventSink;
  host: BrowserPiPTabHost;
  workdir: string;
  tabID: string;
  isPackaged: boolean;
  // Injectable for tests; production uses real Electron views/windows.
  createWindow?: (bounds: Rectangle) => BrowserPiPWindowHandle;
  createOverlay?: () => BrowserPiPOverlayHandle;
};

export class BrowserPiPSurface implements ObservationPiPHandle {
  private win: BrowserPiPWindowHandle | undefined;
  private overlay: BrowserPiPOverlayHandle | undefined;
  private mounted = false;
  private announced = false;
  private restoreBounds: Rectangle | undefined;
  private viewport: { width: number; height: number } = PIP_FALLBACK_VIEWPORT;
  private visible = false;
  private stopped = false;
  private activity: ActivitySession;
  private lastInteractionRevision = 0;
  private readonly unsubs: Array<() => void> = [];

  constructor(private readonly deps: BrowserPiPSurfaceDeps) {
    this.activity = deps.activity;
  }

  start(): void {
    if (this.stopped || this.win) return;
    const win = this.deps.createWindow
      ? this.deps.createWindow(this.deps.bounds)
      : this.createElectronWindow(this.deps.bounds);
    this.win = win;
    const overlay = this.deps.createOverlay ? this.deps.createOverlay() : this.createElectronOverlay();
    this.overlay = overlay;
    const bounds = win.getBounds();
    win.contentView.addChildView(overlay as unknown as BrowserViewHandle);
    overlay.setBounds({ x: 0, y: 0, width: bounds.width, height: bounds.height });
    overlay.webContents.setWindowOpenHandler(() => ({ action: "deny" }));
    overlay.webContents.on("will-navigate", (event: unknown, rawURL: string) => {
      if (typeof rawURL === "string" && rawURL.startsWith("wuu-pip://")) {
        (event as { preventDefault?: () => void }).preventDefault?.();
        if (rawURL === "wuu-pip://close") this.deps.sink.onEvent({ event: "user_close" });
      }
    });
    const initialLabel = pipHostname(
      this.deps.host.tabSurfaceMeta(this.deps.workdir, this.deps.tabID)?.url ?? "",
    );
    void overlay.webContents
      .loadURL(
        `data:text/html;charset=utf-8,${encodeURIComponent(browserPiPOverlayHTML(initialLabel))}`,
      )
      .then(() => {
        if (this.overlay !== overlay || this.win !== win || win.isDestroyed()) return;
        // The hooks only exist after the page loads; replay the state that
        // mount/state pushes before this moment silently dropped.
        this.syncOverlayMode();
        this.pushActivityState();
        this.pushHostLabel();
      })
      .catch(() => undefined);
    const reportGeometry = (): void => {
      if (win.isDestroyed() || this.win !== win) return;
      const b = win.getBounds();
      this.deps.sink.onEvent({ event: "geometry", x: b.x, y: b.y, width: b.width, height: b.height });
    };
    win.on("moved", reportGeometry);
    win.on("resized", () => {
      reportGeometry();
      this.refit();
    });
    win.on("ready-to-show", () => {
      if (win.isDestroyed() || this.win !== win) return;
      if (this.visible) win.showInactive();
    });
    // Reparent the agent tab out before the window dies: a child
    // WebContentsView would be destroyed with the window and kill the tab.
    win.on("close", () => this.unmount());
    win.on("closed", () => {
      if (this.win !== win) return;
      this.win = undefined;
      this.overlay = undefined;
      this.mounted = false;
      this.visible = false;
    });
    this.unsubs.push(
      this.deps.host.addTabClosedListener((workdir, tabID) => {
        if (this.matches(workdir, tabID)) this.deps.sink.onGone();
      }),
      this.deps.host.addInteractionListener((workdir, tabID, hint) => {
        if (this.matches(workdir, tabID)) this.forwardInteraction(hint);
      }),
      this.deps.host.addNavigateListener((workdir, tabID, url) => {
        if (this.matches(workdir, tabID)) this.pushHostLabel(url);
      }),
      this.deps.host.addTabReparentedListener((workdir, tabID, parent) => {
        if (!this.matches(workdir, tabID) || !this.mounted) return;
        if (parent !== this.win?.contentView) {
          // A visibility takeover adopted the tab. Do not fight for it — the
          // placeholder covers the empty PiP until the coordinator hides us.
          this.mounted = false;
          this.restoreBounds = undefined;
          this.syncOverlayMode();
        }
      }),
    );
  }

  setVisible(visible: boolean): void {
    this.visible = visible;
    const win = this.win;
    if (!win || win.isDestroyed()) return;
    if (visible) {
      this.mount();
      win.showInactive();
    } else {
      this.unmount();
      win.hide();
    }
  }

  // A live view has no frame production to freeze: a stopped activity simply
  // leaves its (static) page on screen — the same observation semantics as
  // the CUA PiP's frozen last frame, at zero capture cost.
  setLive(): void {}

  updateActivity(activity: ActivitySession): void {
    this.activity = activity;
    this.pushActivityState();
  }

  animateInteraction(interaction: Interaction): void {
    if (interaction.revision <= this.lastInteractionRevision) return;
    this.lastInteractionRevision = interaction.revision;
    this.forwardInteraction({
      kind: interaction.kind as BrowserInteractionHint["kind"],
      x: interaction.x,
      y: interaction.y,
      direction: interaction.direction,
    });
  }

  stop(onStopped?: () => void): void {
    if (this.stopped) {
      onStopped?.();
      return;
    }
    this.stopped = true;
    for (const unsub of this.unsubs.splice(0)) unsub();
    this.unmount();
    const win = this.win;
    this.win = undefined;
    this.overlay = undefined;
    if (win && !win.isDestroyed()) {
      try {
        win.destroy();
      } catch {
        // Already closing.
      }
    }
    onStopped?.();
  }

  // -------------------------------------------------------------------------
  // Mount lifecycle.
  // -------------------------------------------------------------------------
  private mount(): void {
    const win = this.win;
    if (!win || win.isDestroyed() || this.mounted) return;
    const { workdir, tabID, host } = this.deps;
    const bounds = host.tabBounds(workdir, tabID);
    if (!bounds) {
      this.deps.sink.onGone();
      return;
    }
    this.viewport = {
      width: bounds.width > 0 ? bounds.width : PIP_FALLBACK_VIEWPORT.width,
      height: bounds.height > 0 ? bounds.height : PIP_FALLBACK_VIEWPORT.height,
    };
    const fit = this.containRect();
    const restore = host.mountTabOnWindow(
      workdir,
      tabID,
      win,
      { x: fit.x, y: fit.y, width: fit.width, height: fit.height },
      fit.scale,
    );
    if (!restore) {
      this.deps.sink.onGone();
      return;
    }
    this.restoreBounds = restore;
    this.mounted = true;
    this.syncOverlayMode();
    this.pushHostLabel();
    if (!this.announced) {
      this.announced = true;
      this.deps.sink.onEvent({ event: "ready" });
    }
  }

  private unmount(): void {
    if (!this.mounted) return;
    this.mounted = false;
    const win = this.win;
    const restore = this.restoreBounds;
    this.restoreBounds = undefined;
    if (win && !win.isDestroyed() && restore) {
      this.deps.host.unmountTabIfOwner(this.deps.workdir, this.deps.tabID, win.contentView, restore);
    }
    this.syncOverlayMode();
  }

  private refit(): void {
    const win = this.win;
    if (!win || win.isDestroyed()) return;
    const b = win.getBounds();
    this.overlay?.setBounds({ x: 0, y: 0, width: b.width, height: b.height });
    if (!this.mounted) return;
    const fit = this.containRect();
    this.deps.host.relayoutMountedTab(
      this.deps.workdir,
      this.deps.tabID,
      win.contentView,
      { x: fit.x, y: fit.y, width: fit.width, height: fit.height },
      fit.scale,
    );
  }

  private containRect(): { x: number; y: number; width: number; height: number; scale: number } {
    const b = this.win?.getBounds() ?? this.deps.bounds;
    return pipContainRect(this.viewport.width, this.viewport.height, b.width, b.height);
  }

  private matches(workdir: string, tabID: string): boolean {
    return workdir === this.deps.workdir && tabID === this.deps.tabID;
  }

  // -------------------------------------------------------------------------
  // Overlay pushes.
  // -------------------------------------------------------------------------
  private syncOverlayMode(): void {
    this.execute(`window.wuuPipMount?.(${JSON.stringify({ mounted: this.mounted })})`);
  }

  private pushActivityState(): void {
    this.execute(
      `window.wuuPipState?.(${JSON.stringify({
        state: this.activity.state,
        controller: this.activity.controller,
      })})`,
    );
  }

  private pushHostLabel(url?: string): void {
    const meta = this.deps.host.tabSurfaceMeta(this.deps.workdir, this.deps.tabID);
    const label = pipHostname(url ?? meta?.url ?? "");
    this.execute(`window.wuuPipHost?.(${JSON.stringify({ label })})`);
  }

  private forwardInteraction(hint: BrowserInteractionHint): void {
    if (!this.mounted) return;
    const fit = this.containRect();
    this.execute(
      `window.wuuPipInteract?.(${JSON.stringify({
        kind: hint.kind,
        x: fit.x + hint.x * fit.scale,
        y: fit.y + hint.y * fit.scale,
        direction: hint.direction ?? "",
      })})`,
    );
  }

  private execute(code: string): void {
    const overlay = this.overlay;
    const win = this.win;
    if (!overlay || !win || win.isDestroyed()) return;
    void overlay.webContents.executeJavaScript(code, true).catch(() => undefined);
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

  private createElectronOverlay(): BrowserPiPOverlayHandle {
    const view = new WebContentsView({
      webPreferences: {
        contextIsolation: true,
        nodeIntegration: false,
        sandbox: true,
        ...appShellWebPreferences(this.deps.isPackaged),
      },
    });
    view.setBackgroundColor("#00000000");
    return view as unknown as BrowserPiPOverlayHandle;
  }
}

// Factory covering both observation backends: CUA keeps its native helper,
// browser activities get the Electron surface. One coordinator, one surface,
// two presentation paths.
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
        host: deps.browserHost,
        workdir: activity.workdir,
        tabID,
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
// Overlay page. Sandboxed data: URL like the pet window; all updates arrive
// through window.wuuPip* hooks via executeJavaScript, user actions leave
// through wuu-pip:// navigations. The page draws no content of its own while
// mounted — the pixels below it are the real tab. Without a mounted view it
// shows the frosted placeholder, matching the CUA PiP's "never paint
// failure" rule.
// ---------------------------------------------------------------------------
export function browserPiPOverlayHTML(initialLabel: string): string {
  const label = JSON.stringify(initialLabel);
  return `<!doctype html>
<html><head><meta charset="utf-8" />
<style>
*{box-sizing:border-box;margin:0;padding:0}
html,body{width:100%;height:100%;overflow:hidden;background:transparent;
  font-family:-apple-system,BlinkMacSystemFont,"Helvetica Neue",sans-serif}
#root{position:relative;width:100%;height:100%;-webkit-app-region:drag}
#ph{position:absolute;inset:0;display:flex;align-items:center;justify-content:center;
  background:rgba(28,28,32,.72);backdrop-filter:blur(18px);-webkit-backdrop-filter:blur(18px);
  border-radius:12px;transition:opacity .25s ease;color:rgba(255,255,255,.55)}
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
  var ph=document.getElementById("ph");
  var dot=document.getElementById("dot");
  var ptr=document.getElementById("ptr");
  var ring=document.getElementById("ring");
  var caret=document.getElementById("caret");
  var scrollEl=document.getElementById("scroll");
  var hideTimer=0;
  host.textContent=${label};
  document.getElementById("close").addEventListener("click",function(){
    window.location.href="wuu-pip://close";
  });
  function place(el,x,y){el.style.transform="translate("+x+"px,"+y+"px)";}
  window.wuuPipMount=function(m){
    ph.classList.toggle("gone",!!(m&&m.mounted));
  };
  window.wuuPipHost=function(h){
    host.textContent=(h&&h.label)||"";
  };
  window.wuuPipState=function(s){
    dot.className=s.state==="waiting_confirmation"?"waiting":
      (s.state==="stopped"||s.controller==="none"?"frozen":"");
  };
  window.wuuPipInteract=function(it){
    var x=it.x||0,y=it.y||0;
    ptr.style.transition="transform .14s ease-out,opacity .2s ease";
    place(ptr,x,y);
    ptr.style.opacity="1";
    clearTimeout(hideTimer);
    hideTimer=setTimeout(function(){ptr.style.opacity="0";},1200);
    if(it.kind==="click"){
      place(ring,x,y);
      ring.style.opacity="0";
      ring.animate([{opacity:.9,transform:"translate("+x+"px,"+y+"px) scale(.4)"},
        {opacity:0,transform:"translate("+x+"px,"+y+"px) scale(1.4)"}],
        {duration:380,easing:"ease-out"});
    }else if(it.kind==="type"){
      place(caret,x,y);
      caret.animate([{opacity:1},{opacity:1}],{duration:600});
      caret.animate([{opacity:1},{opacity:0}],{duration:600,delay:600});
    }else if(it.kind==="scroll"){
      var ch={up:"↑",down:"↓",left:"←",right:"→"}[it.direction]||"↕";
      scrollEl.textContent=ch;
      scrollEl.animate([{opacity:.95,transform:"translate(-50%,-50%) translate("+x+"px,"+y+"px)"},
        {opacity:0,transform:"translate(-50%,-50%) translate("+x+"px,"+(y+(it.direction==="up"?10:-10))+"px)"}],
        {duration:520,easing:"ease-out"});
    }
  };
})();
</script>
</body></html>`;
}
