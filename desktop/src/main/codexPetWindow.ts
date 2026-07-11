import { BrowserWindow, Menu, screen, type Rectangle } from "electron";
import type {
  CodexPet,
  CodexPetHint,
  CodexPetState,
  CodexPetStateID,
  CodexPetsSnapshot,
} from "../shared/protocol";
import { CODEX_PET_CELL_HEIGHT, CODEX_PET_CELL_WIDTH, CODEX_PET_STATES } from "./codexPets";

export type CodexPetRuntime = { running: boolean; status: string };

export type CodexPetView = {
  spritesheetURL: string;
  y: number;
  endX: number;
  frames: number;
  duration: number;
  label: string;
  hint: CodexPetHint | null;
  layout: CodexPetBubbleLayout;
};

export type CodexPetBubbleLayout = "above" | "right" | "below" | "left" | "hidden";

export type CodexPetLayoutDecision = {
  layout: CodexPetBubbleLayout;
  bounds: Rectangle;
};

// The pet lives in its own frameless always-on-top window so it survives the
// main window being hidden or minimized. The base window is sized for the
// half-scale sprite cell plus its drop shadow; when a hint bubble is shown,
// the window grows in the chosen layout direction.
const CODEX_PET_WINDOW_WIDTH = 120;
const CODEX_PET_WINDOW_HEIGHT = 128;
const CODEX_PET_SCREEN_INSET = 24;

// Bubble content is fixed-sized so the renderer can use CSS text-overflow /
// -webkit-line-clamp instead of pre-truncating strings in main. Width is the
// card width; height accommodates a single title line + up to two preview
// lines plus inner padding.
const CODEX_PET_BUBBLE_WIDTH = 280;
const CODEX_PET_BUBBLE_HEIGHT = 80;
const CODEX_PET_BUBBLE_PADDING = 8;
const CODEX_PET_BUBBLE_GAP = 8;

// The sprite is rendered at half scale via CSS transform: scale(0.5) on a
// 192×208 layout box. These are the rendered footprint the layout math
// reasons about (the layout box itself stays 192×208 inside a fixed-size
// flex frame).
const CODEX_PET_SPRITE_RENDER_WIDTH = CODEX_PET_CELL_WIDTH / 2; // 96
const CODEX_PET_SPRITE_RENDER_HEIGHT = CODEX_PET_CELL_HEIGHT / 2; // 104

// The sprite sits 8px above the window bottom in vertical layouts and is
// vertically centered (also producing an 8px bottom inset) in horizontal
// layouts. Treat this as the anchor offset from window bottom for all
// layout directions.
const CODEX_PET_SPRITE_BOTTOM_OFFSET = 8;

const CODEX_PET_BUBBLE_SIZE: { width: number; height: number } = {
  width: CODEX_PET_BUBBLE_WIDTH,
  height: CODEX_PET_BUBBLE_HEIGHT,
};

const STATE_BY_ID = new Map<CodexPetStateID, CodexPetState>(
  CODEX_PET_STATES.map((state) => [state.id, state]),
);

export function codexPetStateForRuntime({
  running,
  status,
}: CodexPetRuntime): CodexPetState {
  const normalizedStatus = status.trim().toLowerCase();
  if (/\b(failed|error)\b|失败|错误/.test(normalizedStatus)) {
    return stateByID("failed");
  }
  if (/权限|批准|确认|permission|approve|review/.test(normalizedStatus)) {
    return stateByID("review");
  }
  if (running) {
    return stateByID("running");
  }
  if (/等待|排队|queued|waiting/.test(normalizedStatus)) {
    return stateByID("waiting");
  }
  return stateByID("idle");
}

export function selectedCodexPet(snapshot: CodexPetsSnapshot | undefined): CodexPet | undefined {
  if (!snapshot?.enabled) {
    return undefined;
  }
  return snapshot.pets.find((pet) => pet.id === snapshot.selected_id) ?? snapshot.pets[0];
}

export function codexPetView(
  pet: CodexPet,
  state: CodexPetState,
  hint: CodexPetHint | null,
  layout: CodexPetBubbleLayout,
): CodexPetView {
  return {
    spritesheetURL: pet.spritesheet_url,
    y: state.row * -CODEX_PET_CELL_HEIGHT,
    endX: state.frames * -CODEX_PET_CELL_WIDTH,
    frames: state.frames,
    duration: Math.max(state.frames * 260, 1400),
    label: `${pet.display_name} ${state.id}`,
    hint,
    layout,
  };
}

export type CodexPetAction =
  | { action: "menu" }
  | { action: "jump"; thread_id: string }
  | undefined;

export function codexPetActionFromURL(rawURL: string): CodexPetAction {
  try {
    const url = new URL(rawURL);
    if (url.protocol !== "wuu-pet:" || url.hostname !== "action") return undefined;
    const actionName = url.pathname.replace(/^\/+/, "");
    if (actionName === "menu") return { action: "menu" };
    if (actionName === "jump") {
      const threadId = url.searchParams.get("thread_id");
      if (!threadId) return undefined;
      return { action: "jump", thread_id: threadId };
    }
    return undefined;
  } catch {
    return undefined;
  }
}

// Compute window bounds for a given bubble layout, keeping the sprite's
// bottom-center (the `anchor`) at the same screen point. The returned
// bounds are not yet clamped to the workArea; the caller decides whether
// the layout fits before applying.
export function codexPetBoundsForLayout({
  layout,
  anchor,
  bubble,
}: {
  layout: Exclude<CodexPetBubbleLayout, "hidden">;
  anchor: { x: number; y: number };
  bubble: { width: number; height: number };
}): Rectangle {
  switch (layout) {
    case "above": {
      const width = Math.max(CODEX_PET_SPRITE_RENDER_WIDTH, bubble.width);
      const height =
        CODEX_PET_BUBBLE_PADDING +
        bubble.height +
        CODEX_PET_BUBBLE_GAP +
        CODEX_PET_SPRITE_RENDER_HEIGHT +
        CODEX_PET_BUBBLE_PADDING;
      return {
        x: Math.round(anchor.x - width / 2),
        y: Math.round(anchor.y - height + CODEX_PET_SPRITE_BOTTOM_OFFSET),
        width,
        height,
      };
    }
    case "below": {
      const width = Math.max(CODEX_PET_SPRITE_RENDER_WIDTH, bubble.width);
      const height =
        CODEX_PET_BUBBLE_PADDING +
        CODEX_PET_SPRITE_RENDER_HEIGHT +
        CODEX_PET_BUBBLE_GAP +
        bubble.height +
        CODEX_PET_BUBBLE_PADDING;
      return {
        x: Math.round(anchor.x - width / 2),
        y: Math.round(anchor.y - CODEX_PET_SPRITE_RENDER_HEIGHT - CODEX_PET_BUBBLE_PADDING),
        width,
        height,
      };
    }
    case "right": {
      const width =
        CODEX_PET_BUBBLE_PADDING +
        CODEX_PET_SPRITE_RENDER_WIDTH +
        CODEX_PET_BUBBLE_GAP +
        bubble.width +
        CODEX_PET_BUBBLE_PADDING;
      const height =
        Math.max(CODEX_PET_SPRITE_RENDER_HEIGHT, bubble.height) + 2 * CODEX_PET_BUBBLE_PADDING;
      return {
        x: Math.round(anchor.x - CODEX_PET_BUBBLE_PADDING - CODEX_PET_SPRITE_RENDER_WIDTH / 2),
        y: Math.round(anchor.y - height + CODEX_PET_SPRITE_BOTTOM_OFFSET),
        width,
        height,
      };
    }
    case "left": {
      const width =
        CODEX_PET_BUBBLE_PADDING +
        bubble.width +
        CODEX_PET_BUBBLE_GAP +
        CODEX_PET_SPRITE_RENDER_WIDTH +
        CODEX_PET_BUBBLE_PADDING;
      const height =
        Math.max(CODEX_PET_SPRITE_RENDER_HEIGHT, bubble.height) + 2 * CODEX_PET_BUBBLE_PADDING;
      return {
        x: Math.round(anchor.x - (width - CODEX_PET_BUBBLE_PADDING - CODEX_PET_SPRITE_RENDER_WIDTH / 2)),
        y: Math.round(anchor.y - height + CODEX_PET_SPRITE_BOTTOM_OFFSET),
        width,
        height,
      };
    }
  }
}

// Choose the first layout direction whose bounds fully fit inside the
// given workArea. If none fit, returns "hidden" with base window bounds
// (no bubble shown). Priority matches the user's brief: above first
// ("pet 头上"), then right, then left/below as last-resort fallbacks.
export function selectCodexPetBubbleLayout({
  workArea,
  anchor,
  bubble,
}: {
  workArea: Rectangle;
  anchor: { x: number; y: number };
  bubble: { width: number; height: number };
}): CodexPetLayoutDecision {
  const order: Array<Exclude<CodexPetBubbleLayout, "hidden">> = [
    "above",
    "right",
    "left",
    "below",
  ];
  for (const layout of order) {
    const bounds = codexPetBoundsForLayout({ layout, anchor, bubble });
    if (
      bounds.x >= workArea.x &&
      bounds.y >= workArea.y &&
      bounds.x + bounds.width <= workArea.x + workArea.width &&
      bounds.y + bounds.height <= workArea.y + workArea.height
    ) {
      return { layout, bounds };
    }
  }
  return {
    layout: "hidden",
    bounds: {
      x: Math.round(anchor.x - CODEX_PET_WINDOW_WIDTH / 2),
      y: Math.round(anchor.y - CODEX_PET_WINDOW_HEIGHT + CODEX_PET_SPRITE_BOTTOM_OFFSET),
      width: CODEX_PET_WINDOW_WIDTH,
      height: CODEX_PET_WINDOW_HEIGHT,
    },
  };
}

export function codexPetWindowHTML(view: CodexPetView): string {
  const styleVars = [
    `--pet-sheet:url('${view.spritesheetURL}')`,
    `--pet-y:${view.y}px`,
    `--pet-end-x:${view.endX}px`,
    `--pet-frames:${view.frames}`,
    `--pet-duration:${view.duration}ms`,
  ].join(";");
  const initialHintJSON = JSON.stringify(view.hint);
  return `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8" />
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src wuu-file:; style-src 'unsafe-inline'; script-src 'unsafe-inline'; navigate-to wuu-pet:" />
<meta name="viewport" content="width=device-width,initial-scale=1" />
<style>
*{box-sizing:border-box}html,body{width:100%;height:100%;margin:0;background:transparent;overflow:hidden}
.stage{position:relative;width:100%;height:100%;display:flex;align-items:center;justify-content:center;flex-direction:column;cursor:grab;-webkit-user-select:none;user-select:none}
.stage.is-dragging{cursor:grabbing}
.stage.layout-above,.stage.layout-hidden{flex-direction:column;justify-content:flex-end}
.stage.layout-below{flex-direction:column;justify-content:flex-start}
.stage.layout-right{flex-direction:row;justify-content:flex-start}
.stage.layout-left{flex-direction:row-reverse;justify-content:flex-start}
.stage.layout-right,.stage.layout-left{align-items:center}
.sprite-frame{width:${CODEX_PET_SPRITE_RENDER_WIDTH}px;height:${CODEX_PET_SPRITE_RENDER_HEIGHT}px;display:flex;align-items:flex-end;justify-content:center;flex-shrink:0;pointer-events:none}
.sprite{width:${CODEX_PET_CELL_WIDTH}px;height:${CODEX_PET_CELL_HEIGHT}px;transform:scale(0.5);transform-origin:bottom center;background-image:var(--pet-sheet);background-repeat:no-repeat;background-position:0 var(--pet-y);image-rendering:pixelated;filter:drop-shadow(0 10px 16px rgba(0,0,0,.18));animation:pet-play var(--pet-duration) steps(var(--pet-frames)) infinite}
@keyframes pet-play{from{background-position:0 var(--pet-y)}to{background-position:var(--pet-end-x) var(--pet-y)}}
@media (prefers-reduced-motion:reduce){.sprite{animation:none}}
.bubble{display:flex;align-items:flex-start;gap:8px;width:${CODEX_PET_BUBBLE_WIDTH}px;min-height:${CODEX_PET_BUBBLE_HEIGHT}px;padding:12px;margin:${CODEX_PET_BUBBLE_PADDING}px;border-radius:12px;background:rgba(255,255,255,.95);box-shadow:0 6px 24px rgba(0,0,0,.18);cursor:pointer;flex-shrink:0;color:#1a1a1a;transition:opacity 200ms ease}
.stage.layout-hidden .bubble{display:none}
.bubble .dot{width:8px;height:8px;border-radius:50%;margin-top:6px;flex-shrink:0;background:#999}
.bubble.attention .dot{box-shadow:0 0 0 0 rgba(255,80,80,.5);animation:pet-pulse 1.6s ease-in-out infinite}
@keyframes pet-pulse{0%{box-shadow:0 0 0 0 rgba(255,80,80,.5)}70%{box-shadow:0 0 0 8px rgba(255,80,80,0)}100%{box-shadow:0 0 0 0 rgba(255,80,80,0)}}
.bubble .content{flex:1;min-width:0;display:flex;flex-direction:column;gap:4px}
.bubble .title{font:600 13px/1.3 -apple-system,BlinkMacSystemFont,system-ui,sans-serif;color:#1a1a1a;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.bubble .preview{font:12px/1.4 -apple-system,BlinkMacSystemFont,system-ui,sans-serif;color:#555;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden;word-break:break-word}
</style></head><body><div class="stage layout-${escapeHTML(view.layout)}" style="${escapeHTML(styleVars)}">
<div class="bubble" data-thread-id="${escapeHTML(view.hint?.thread_id ?? "")}" style="display:${view.hint ? "flex" : "none"}">
<span class="dot"></span>
<div class="content">
<div class="title">${escapeHTML(view.hint?.title ?? "")}</div>
<div class="preview">${escapeHTML(view.hint?.preview ?? "")}</div>
</div>
</div>
<div class="sprite-frame"><div class="sprite" role="img" aria-label="${escapeHTML(view.label)}"></div></div>
</div>
<script>
(() => {
  const stage = document.querySelector('.stage');
  const bubble = document.querySelector('.bubble');
  const sprite = document.querySelector('.sprite');
  const titleEl = bubble.querySelector('.title');
  const previewEl = bubble.querySelector('.preview');
  const dotEl = bubble.querySelector('.dot');
  const STATUS_COLORS = { running: '#4a9eff', done: '#28c840', failed: '#ff3b30', needs_review: '#ff9500', idle: '#999' };
  let currentHint = ${initialHintJSON};
  const applyHint = (hint) => {
    if (!hint) {
      bubble.style.display = 'none';
      bubble.removeAttribute('data-thread-id');
      return;
    }
    bubble.style.display = '';
    bubble.dataset.threadId = hint.thread_id;
    bubble.classList.toggle('attention', !!hint.attention);
    dotEl.style.background = STATUS_COLORS[hint.status] || STATUS_COLORS.idle;
    titleEl.textContent = hint.title || '';
    previewEl.textContent = hint.preview || '';
  };
  applyHint(currentHint);
  window.wuuPetView = (view) => {
    sprite.style.setProperty('--pet-sheet', 'url("' + view.spritesheetURL + '")');
    sprite.style.setProperty('--pet-y', view.y + 'px');
    sprite.style.setProperty('--pet-end-x', view.endX + 'px');
    sprite.style.setProperty('--pet-frames', String(view.frames));
    sprite.style.setProperty('--pet-duration', view.duration + 'ms');
    sprite.setAttribute('aria-label', view.label);
    stage.className = 'stage layout-' + view.layout;
    if (JSON.stringify(view.hint) !== JSON.stringify(currentHint)) {
      currentHint = view.hint;
      applyHint(view.hint);
    }
  };
  window.addEventListener('contextmenu', (event) => {
    event.preventDefault();
    location.href = 'wuu-pet://action/menu';
  });
  bubble.addEventListener('click', (event) => {
    event.stopPropagation();
    const threadId = bubble.dataset.threadId;
    if (threadId) location.href = 'wuu-pet://action/jump?thread_id=' + encodeURIComponent(threadId);
  });
  let pointerID = null;
  let offsetX = 0;
  let offsetY = 0;
  stage.addEventListener('pointerdown', (event) => {
    if (event.button !== 0) return;
    pointerID = event.pointerId;
    offsetX = event.screenX - window.screenX;
    offsetY = event.screenY - window.screenY;
    stage.setPointerCapture(pointerID);
    stage.classList.add('is-dragging');
    event.preventDefault();
  });
  stage.addEventListener('pointermove', (event) => {
    if (event.pointerId !== pointerID) return;
    window.moveTo(Math.round(event.screenX - offsetX), Math.round(event.screenY - offsetY));
  });
  const finish = (event) => {
    if (event.pointerId !== pointerID) return;
    pointerID = null;
    stage.classList.remove('is-dragging');
  };
  stage.addEventListener('pointerup', finish);
  stage.addEventListener('pointercancel', finish);
})();
</script></body></html>`;
}

export class CodexPetWindowManager {
  private win: BrowserWindow | undefined;
  private loaded = false;
  private pendingView: CodexPetView | undefined;
  private lastViewSignature = "";
  private runtime: CodexPetRuntime = { running: false, status: "" };
  private snapshot: CodexPetsSnapshot | undefined;
  private hint: CodexPetHint | null = null;
  private currentLayout: CodexPetBubbleLayout = "hidden";

  constructor(
    private readonly onCloseRequested: () => void,
    private readonly onJumpRequested: (hint: CodexPetHint) => void,
  ) {}

  sync(snapshot: CodexPetsSnapshot | undefined): void {
    this.snapshot = snapshot;
    const pet = selectedCodexPet(snapshot);
    if (!pet) {
      this.destroy();
      return;
    }
    if (!this.win || this.win.isDestroyed()) {
      this.createWindow(pet);
      return;
    }
    this.applyView(pet);
  }

  setRuntime(runtime: CodexPetRuntime): void {
    this.runtime = {
      running: Boolean(runtime?.running),
      status: typeof runtime?.status === "string" ? runtime.status : "",
    };
    const pet = selectedCodexPet(this.snapshot);
    if (pet && this.win && !this.win.isDestroyed()) this.applyView(pet);
  }

  setHint(hint: CodexPetHint | null): void {
    this.hint = hint && typeof hint.thread_id === "string" ? hint : null;
    const pet = selectedCodexPet(this.snapshot);
    if (pet && this.win && !this.win.isDestroyed()) this.applyView(pet);
  }

  destroy(): void {
    const win = this.win;
    this.win = undefined;
    this.loaded = false;
    this.pendingView = undefined;
    this.lastViewSignature = "";
    this.currentLayout = "hidden";
    if (win && !win.isDestroyed()) win.close();
  }

  private createWindow(pet: CodexPet): void {
    const workArea = screen.getPrimaryDisplay().workArea;
    const view = codexPetView(
      pet,
      codexPetStateForRuntime(this.runtime),
      this.hint,
      this.hint ? selectCodexPetBubbleLayout({
        workArea,
        anchor: {
          x: workArea.x + workArea.width - CODEX_PET_WINDOW_WIDTH / 2 - CODEX_PET_SCREEN_INSET,
          y: workArea.y + workArea.height - CODEX_PET_SPRITE_BOTTOM_OFFSET - CODEX_PET_SCREEN_INSET,
        },
        bubble: CODEX_PET_BUBBLE_SIZE,
      }).layout : "hidden",
    );
    const layoutBounds = this.hint
      ? selectCodexPetBubbleLayout({
          workArea,
          anchor: {
            x: workArea.x + workArea.width - CODEX_PET_WINDOW_WIDTH / 2 - CODEX_PET_SCREEN_INSET,
            y: workArea.y + workArea.height - CODEX_PET_SPRITE_BOTTOM_OFFSET - CODEX_PET_SCREEN_INSET,
          },
          bubble: CODEX_PET_BUBBLE_SIZE,
        }).bounds
      : {
          x: workArea.x + workArea.width - CODEX_PET_WINDOW_WIDTH - CODEX_PET_SCREEN_INSET,
          y: workArea.y + workArea.height - CODEX_PET_WINDOW_HEIGHT - CODEX_PET_SCREEN_INSET,
          width: CODEX_PET_WINDOW_WIDTH,
          height: CODEX_PET_WINDOW_HEIGHT,
        };
    this.currentLayout = view.layout;
    this.lastViewSignature = JSON.stringify(view);
    const win = new BrowserWindow({
      width: layoutBounds.width,
      height: layoutBounds.height,
      x: layoutBounds.x,
      y: layoutBounds.y,
      frame: false,
      transparent: true,
      backgroundColor: "#00000000",
      hasShadow: false,
      alwaysOnTop: true,
      skipTaskbar: true,
      resizable: false,
      minimizable: false,
      maximizable: false,
      fullscreenable: false,
      acceptFirstMouse: true,
      show: false,
      type: "panel",
      webPreferences: {
        contextIsolation: true,
        nodeIntegration: false,
        sandbox: true,
      },
    });
    this.win = win;
    this.loaded = false;
    win.setAlwaysOnTop(true, "floating");
    win.setHasShadow(false);
    win.setVisibleOnAllWorkspaces(true, { visibleOnFullScreen: true });
    win.webContents.setWindowOpenHandler(() => ({ action: "deny" }));
    win.webContents.on("will-navigate", (navigationEvent, rawURL) => {
      const parsed = codexPetActionFromURL(rawURL);
      if (!parsed) return;
      navigationEvent.preventDefault();
      if (parsed.action === "menu") {
        this.popupMenu(win);
        return;
      }
      if (parsed.action === "jump" && this.hint && this.hint.thread_id === parsed.thread_id) {
        this.onJumpRequested(this.hint);
      }
    });
    win.webContents.on("did-finish-load", () => {
      if (win.isDestroyed() || this.win !== win) return;
      this.loaded = true;
      const pending = this.pendingView;
      this.pendingView = undefined;
      if (pending) this.pushView(win, pending);
      // Belt-and-suspenders: ready-to-show is unreliable for data: URLs
      // in current Electron, so surface the window from the load event
      // when it is still hidden. showInactive() is a no-op once the
      // window is already up, so calling it after ready-to-show
      // already ran is safe.
      if (!win.isVisible()) {
        win.showInactive();
      }
    });
    win.once("ready-to-show", () => {
      if (win.isDestroyed() || this.win !== win) return;
      win.showInactive();
    });
    win.on("closed", () => {
      if (this.win !== win) return;
      this.win = undefined;
      this.loaded = false;
      this.pendingView = undefined;
      this.lastViewSignature = "";
      this.currentLayout = "hidden";
    });
    void win.loadURL(`data:text/html;charset=utf-8,${encodeURIComponent(codexPetWindowHTML(view))}`);
  }

  private popupMenu(win: BrowserWindow): void {
    const pet = selectedCodexPet(this.snapshot);
    const template: Electron.MenuItemConstructorOptions[] = [
      { label: pet ? pet.display_name : "桌宠", enabled: false },
    ];
    if (this.hint) {
      template.push({
        label: `打开当前会话 · ${this.hint.title}`,
        click: () => this.onJumpRequested(this.hint!),
      });
      template.push({ type: "separator" });
    }
    template.push({ label: "关闭桌宠", click: () => this.onCloseRequested() });
    Menu.buildFromTemplate(template).popup({ window: win });
  }

  private applyView(pet: CodexPet): void {
    const win = this.win;
    if (!win || win.isDestroyed()) return;
    const currentBounds = win.getBounds();
    const anchor = this.anchorFromBounds(currentBounds, this.currentLayout);
    let layout: CodexPetBubbleLayout;
    let targetBounds: Rectangle;
    if (this.hint) {
      const workArea = screen.getPrimaryDisplay().workArea;
      const decision = selectCodexPetBubbleLayout({
        workArea,
        anchor,
        bubble: CODEX_PET_BUBBLE_SIZE,
      });
      layout = decision.layout;
      targetBounds = decision.bounds;
    } else {
      layout = "hidden";
      targetBounds = {
        x: Math.round(anchor.x - CODEX_PET_WINDOW_WIDTH / 2),
        y: Math.round(anchor.y - CODEX_PET_WINDOW_HEIGHT + CODEX_PET_SPRITE_BOTTOM_OFFSET),
        width: CODEX_PET_WINDOW_WIDTH,
        height: CODEX_PET_WINDOW_HEIGHT,
      };
    }
    this.currentLayout = layout;
    if (
      currentBounds.x !== targetBounds.x ||
      currentBounds.y !== targetBounds.y ||
      currentBounds.width !== targetBounds.width ||
      currentBounds.height !== targetBounds.height
    ) {
      win.setBounds(targetBounds);
    }
    const view = codexPetView(pet, codexPetStateForRuntime(this.runtime), this.hint, layout);
    const signature = JSON.stringify(view);
    if (signature === this.lastViewSignature && this.loaded) return;
    this.lastViewSignature = signature;
    if (!this.loaded) {
      this.pendingView = view;
      return;
    }
    this.pushView(win, view);
  }

  private anchorFromBounds(
    bounds: Rectangle,
    layout: CodexPetBubbleLayout,
  ): { x: number; y: number } {
    const y = bounds.y + bounds.height - CODEX_PET_SPRITE_BOTTOM_OFFSET;
    let x: number;
    switch (layout) {
      case "above":
      case "below":
      case "hidden":
        x = bounds.x + bounds.width / 2;
        break;
      case "right":
        x = bounds.x + CODEX_PET_BUBBLE_PADDING + CODEX_PET_SPRITE_RENDER_WIDTH / 2;
        break;
      case "left":
        x = bounds.x + bounds.width - CODEX_PET_BUBBLE_PADDING - CODEX_PET_SPRITE_RENDER_WIDTH / 2;
        break;
    }
    return { x, y };
  }

  private pushView(win: BrowserWindow, view: CodexPetView): void {
    void win.webContents
      .executeJavaScript(`window.wuuPetView?.(${JSON.stringify(view)})`, true)
      .catch(() => undefined);
  }
}

function stateByID(id: CodexPetStateID): CodexPetState {
  return STATE_BY_ID.get(id) ?? CODEX_PET_STATES[0];
}

function escapeHTML(value: string): string {
  return value.replace(/[&<>"']/g, (character) => ({
    "&": "&amp;",
    "<": "&lt;",
    ">": "&gt;",
    '"': "&quot;",
    "'": "&#39;",
  })[character] ?? character);
}