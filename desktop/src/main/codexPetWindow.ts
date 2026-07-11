import { BrowserWindow, Menu, screen } from "electron";
import type { CodexPet, CodexPetState, CodexPetStateID, CodexPetsSnapshot } from "../shared/protocol";
import { CODEX_PET_CELL_HEIGHT, CODEX_PET_CELL_WIDTH, CODEX_PET_STATES } from "./codexPets";

export type CodexPetRuntime = { running: boolean; status: string };

export type CodexPetView = {
  spritesheetURL: string;
  y: number;
  endX: number;
  frames: number;
  duration: number;
  label: string;
};

// The pet lives in its own frameless always-on-top window so it survives the
// main window being hidden or minimized. The window is just big enough for
// the half-scale sprite cell plus room for its drop shadow.
const CODEX_PET_WINDOW_WIDTH = 120;
const CODEX_PET_WINDOW_HEIGHT = 128;
const CODEX_PET_SCREEN_INSET = 24;

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

export function codexPetView(pet: CodexPet, state: CodexPetState): CodexPetView {
  return {
    spritesheetURL: pet.spritesheet_url,
    y: state.row * -CODEX_PET_CELL_HEIGHT,
    endX: state.frames * -CODEX_PET_CELL_WIDTH,
    frames: state.frames,
    duration: Math.max(state.frames * 260, 1400),
    label: `${pet.display_name} ${state.id}`,
  };
}

export function codexPetActionFromURL(rawURL: string): "menu" | undefined {
  try {
    const url = new URL(rawURL);
    if (url.protocol !== "wuu-pet:" || url.hostname !== "action") return undefined;
    return url.pathname.replace(/^\/+/, "") === "menu" ? "menu" : undefined;
  } catch {
    return undefined;
  }
}

export function codexPetWindowHTML(view: CodexPetView): string {
  const styleVars = [
    `--pet-sheet:url('${view.spritesheetURL}')`,
    `--pet-y:${view.y}px`,
    `--pet-end-x:${view.endX}px`,
    `--pet-frames:${view.frames}`,
    `--pet-duration:${view.duration}ms`,
  ].join(";");
  return `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8" />
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src wuu-file:; style-src 'unsafe-inline'; script-src 'unsafe-inline'; navigate-to wuu-pet:" />
<meta name="viewport" content="width=device-width,initial-scale=1" />
<style>
*{box-sizing:border-box}html,body{width:100%;height:100%;margin:0;background:transparent;overflow:hidden}
.stage{position:relative;width:100%;height:100%;cursor:grab;-webkit-user-select:none;user-select:none}
.stage.is-dragging{cursor:grabbing}
.sprite{position:absolute;left:calc(50% - ${CODEX_PET_CELL_WIDTH / 2}px);bottom:8px;width:${CODEX_PET_CELL_WIDTH}px;height:${CODEX_PET_CELL_HEIGHT}px;transform:scale(0.5);transform-origin:bottom center;background-image:var(--pet-sheet);background-repeat:no-repeat;background-position:0 var(--pet-y);image-rendering:pixelated;filter:drop-shadow(0 10px 16px rgba(0,0,0,.18));animation:pet-play var(--pet-duration) steps(var(--pet-frames)) infinite}
@keyframes pet-play{from{background-position:0 var(--pet-y)}to{background-position:var(--pet-end-x) var(--pet-y)}}
@media (prefers-reduced-motion:reduce){.sprite{animation:none}}
</style></head><body><div class="stage"><div class="sprite" role="img" aria-label="${escapeHTML(view.label)}" style="${escapeHTML(styleVars)}"></div></div>
<script>
(() => {
  const stage = document.querySelector('.stage');
  const sprite = document.querySelector('.sprite');
  window.wuuPetView = (view) => {
    sprite.style.setProperty('--pet-sheet', 'url("' + view.spritesheetURL + '")');
    sprite.style.setProperty('--pet-y', view.y + 'px');
    sprite.style.setProperty('--pet-end-x', view.endX + 'px');
    sprite.style.setProperty('--pet-frames', String(view.frames));
    sprite.style.setProperty('--pet-duration', view.duration + 'ms');
    sprite.setAttribute('aria-label', view.label);
  };
  window.addEventListener('contextmenu', (event) => {
    event.preventDefault();
    location.href = 'wuu-pet://action/menu';
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

  constructor(private readonly onCloseRequested: () => void) {}

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

  destroy(): void {
    const win = this.win;
    this.win = undefined;
    this.loaded = false;
    this.pendingView = undefined;
    this.lastViewSignature = "";
    if (win && !win.isDestroyed()) win.close();
  }

  private createWindow(pet: CodexPet): void {
    const workArea = screen.getPrimaryDisplay().workArea;
    const view = codexPetView(pet, codexPetStateForRuntime(this.runtime));
    this.lastViewSignature = JSON.stringify(view);
    const win = new BrowserWindow({
      width: CODEX_PET_WINDOW_WIDTH,
      height: CODEX_PET_WINDOW_HEIGHT,
      x: workArea.x + workArea.width - CODEX_PET_WINDOW_WIDTH - CODEX_PET_SCREEN_INSET,
      y: workArea.y + workArea.height - CODEX_PET_WINDOW_HEIGHT - CODEX_PET_SCREEN_INSET,
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
      if (codexPetActionFromURL(rawURL) !== "menu") return;
      navigationEvent.preventDefault();
      this.popupMenu(win);
    });
    win.webContents.on("did-finish-load", () => {
      if (win.isDestroyed() || this.win !== win) return;
      this.loaded = true;
      const pending = this.pendingView;
      this.pendingView = undefined;
      if (pending) this.pushView(win, pending);
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
    });
    void win.loadURL(`data:text/html;charset=utf-8,${encodeURIComponent(codexPetWindowHTML(view))}`);
  }

  private popupMenu(win: BrowserWindow): void {
    const pet = selectedCodexPet(this.snapshot);
    Menu.buildFromTemplate([
      { label: pet ? pet.display_name : "桌宠", enabled: false },
      { type: "separator" },
      { label: "关闭桌宠", click: () => this.onCloseRequested() },
    ]).popup({ window: win });
  }

  private applyView(pet: CodexPet): void {
    const win = this.win;
    if (!win || win.isDestroyed()) return;
    const view = codexPetView(pet, codexPetStateForRuntime(this.runtime));
    const signature = JSON.stringify(view);
    if (signature === this.lastViewSignature && this.loaded) return;
    this.lastViewSignature = signature;
    if (!this.loaded) {
      this.pendingView = view;
      return;
    }
    this.pushView(win, view);
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
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
  })[character] ?? character);
}
