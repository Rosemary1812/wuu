import { BrowserWindow, Menu, screen, type Rectangle } from "electron";
import {
  CODEX_PET_SIZE_DEFAULT,
  CODEX_PET_SIZE_OPTIONS,
  type CodexPet,
  type CodexPetHint,
  type CodexPetSize,
  type CodexPetState,
  type CodexPetStateID,
  type CodexPetsSnapshot,
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
  // Render-time sprite geometry — pre-computed at view-build time so the
  // HTML template and the JS bridge don't have to know about CodexPetSize.
  size: CodexPetSize;
  spriteWidth: number;
  spriteHeight: number;
  scale: number;
};

export type CodexPetBubbleLayout = "above" | "right" | "below" | "left" | "hidden";

export type CodexPetLayoutDecision = {
  layout: CodexPetBubbleLayout;
  bounds: Rectangle;
};

// The pet lives in its own frameless always-on-top window so it survives the
// main window being hidden or minimized. The base window is sized for the
// sprite cell plus its drop shadow; when a hint bubble is shown, the window
// grows in the chosen layout direction. Per-size math is computed by
// `codexPetRenderedSpriteForSize(this.size)`.
const CODEX_PET_SCREEN_INSET = 24;

// Bubble is a single-line clamp. The pet's job is to surface the latest
// stable commentary as a glance, not a paragraph; -webkit-line-clamp:1
// plus a BUBBLE_HEIGHT tuned for one line of 13/1.45 text keeps the card
// compact. When the renderer pushes a hint whose preview is empty (no
// stable agent_message anywhere on the top-ranked thread), the inline
// applyHint hides the bubble entirely — the sprite keeps its running/
// idle/failed state without a stale text fallback. The Thread.preview
// denormalized field is no longer surfaced by deriveActiveSessionHint, so
// this hide path matters for both `null hint` and `hint with empty preview`.
const CODEX_PET_BUBBLE_WIDTH = 280;
const CODEX_PET_BUBBLE_HEIGHT = 44;
const CODEX_PET_BUBBLE_PADDING = 8;
const CODEX_PET_BUBBLE_GAP = 8;
// Bubble preview swap animation: when `hint.preview` changes (e.g. first
// commentary finishes and a second one lands, or the bubble disappears), the
// .preview element fades out, swaps text, fades back in. Asymmetric timings so
// the fade-out feels decisive and the fade-in is gentle — total cycle ~320ms.
const CODEX_PET_BUBBLE_SWAP_OUT_MS = 140;
const CODEX_PET_BUBBLE_SWAP_IN_MS = 180;

// The sprite is rendered from the 192×208 layout box via CSS transform:
// scale(<size>). At the default (100%) size the transform is 0.5, so the
// base rendered footprint is 96×104; the per-size helper below recomputes
// the footprint against the user's chosen multiplier.
const CODEX_PET_SPRITE_RENDER_WIDTH_BASE = CODEX_PET_CELL_WIDTH / 2; // 96
const CODEX_PET_SPRITE_RENDER_HEIGHT_BASE = CODEX_PET_CELL_HEIGHT / 2; // 104
const CODEX_PET_SPRITE_BASE_SCALE = 0.5;

// Window chrome padding around the sprite is held constant across sizes so
// the chrome (drop shadow margin, hit-test room, breathing space) doesn't
// grow with the pet itself.
const CODEX_PET_WINDOW_HORIZONTAL_PADDING = 24; // 12px each side
const CODEX_PET_WINDOW_TOP_PADDING = 16;

// The sprite sits 8px above the window bottom in vertical layouts and is
// vertically centered (also producing an 8px bottom inset) in horizontal
// layouts. Treat this as the anchor offset from window bottom for all
// layout directions.
const CODEX_PET_SPRITE_BOTTOM_OFFSET = 8;

const CODEX_PET_BUBBLE_SIZE: { width: number; height: number } = {
  width: CODEX_PET_BUBBLE_WIDTH,
  height: CODEX_PET_BUBBLE_HEIGHT,
};

export type CodexPetRenderedSprite = {
  width: number;
  height: number;
  scale: number;
  windowWidth: number;
  windowHeight: number;
};

// Maps a size level to its rendered footprint + the CSS transform that
// produces it. Driven by CODEX_PET_SIZE_OPTIONS so the menu, the
// persisted settings, and the geometry stay in sync.
export function codexPetRenderedSpriteForSize(
  size: CodexPetSize,
): CodexPetRenderedSprite {
  const option =
    CODEX_PET_SIZE_OPTIONS.find((entry) => entry.id === size) ??
    CODEX_PET_SIZE_OPTIONS.find((entry) => entry.id === CODEX_PET_SIZE_DEFAULT)!;
  const multiplier = option.multiplier;
  const width = Math.round(CODEX_PET_SPRITE_RENDER_WIDTH_BASE * multiplier);
  const height = Math.round(CODEX_PET_SPRITE_RENDER_HEIGHT_BASE * multiplier);
  return {
    width,
    height,
    scale: CODEX_PET_SPRITE_BASE_SCALE * multiplier,
    windowWidth: width + CODEX_PET_WINDOW_HORIZONTAL_PADDING,
    windowHeight:
      height + CODEX_PET_WINDOW_TOP_PADDING + CODEX_PET_SPRITE_BOTTOM_OFFSET,
  };
}

// Build rendered sprite geometry from a raw scale value. Used by the
// continuous resize path: when the user drags the strip the scale moves
// smoothly between the preset extremes and we need geometry for any
// value, not just the four preset ids. Same math as
// codexPetRenderedSpriteForSize but skips the preset lookup so it can
// interpolate freely without going through CODEX_PET_SIZE_OPTIONS.
export function codexPetRenderedSpriteForScale(
  scale: number,
): CodexPetRenderedSprite {
  const multiplier = scale / CODEX_PET_SPRITE_BASE_SCALE;
  const width = Math.round(CODEX_PET_SPRITE_RENDER_WIDTH_BASE * multiplier);
  const height = Math.round(CODEX_PET_SPRITE_RENDER_HEIGHT_BASE * multiplier);
  return {
    width,
    height,
    scale,
    windowWidth: width + CODEX_PET_WINDOW_HORIZONTAL_PADDING,
    windowHeight:
      height + CODEX_PET_WINDOW_TOP_PADDING + CODEX_PET_SPRITE_BOTTOM_OFFSET,
  };
}

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
  size: CodexPetSize = CODEX_PET_SIZE_DEFAULT,
  customScale?: number,
): CodexPetView {
  // Continuous drag passes a raw `customScale` and we derive geometry
  // from it directly. Without it, fall back to the preset-derived
  // geometry so callers that don't know about continuous resize see
  // exactly the original behavior.
  const useCustomScale =
    typeof customScale === "number" &&
    Number.isFinite(customScale) &&
    customScale > 0;
  const rendered = useCustomScale
    ? codexPetRenderedSpriteForScale(customScale)
    : codexPetRenderedSpriteForSize(size);
  return {
    spritesheetURL: pet.spritesheet_url,
    y: state.row * -CODEX_PET_CELL_HEIGHT,
    endX: state.frames * -CODEX_PET_CELL_WIDTH,
    frames: state.frames,
    duration: Math.max(state.frames * 260, 1400),
    label: `${pet.display_name} ${state.id}`,
    hint,
    layout,
    size,
    spriteWidth: rendered.width,
    spriteHeight: rendered.height,
    scale: rendered.scale,
  };
}

export type CodexPetAction =
  | { action: "menu" }
  | { action: "jump"; thread_id: string }
  | { action: "resize"; id: CodexPetSize }
  | { action: "scale"; value: number; commit: boolean }
  | undefined;

// Continuous resize pushes the live scale as a number. Clamp the parser
// range so a malformed URL can't drag the pet off-screen (or under
// 1px) even if the inline script's client-side clamp ever drifts.
export const CODEX_PET_SCALE_MIN = 0.25;
export const CODEX_PET_SCALE_MAX = 1.5;

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
    if (actionName === "resize") {
      // Only forward ids the preset list knows about. A stale page that names
      // an id the type system doesn't model falls through to undefined so the
      // navigate handler treats it as a no-op instead of crashing setSize.
      const id = url.searchParams.get("id");
      if (!id) return undefined;
      const known = CODEX_PET_SIZE_OPTIONS.some((option) => option.id === id);
      if (!known) return undefined;
      return { action: "resize", id: id as CodexPetSize };
    }
    if (actionName === "scale") {
      // Live drag emits a float; drop anything non-finite and clamp the
      // accepted range. Without the clamp a hostile or buggy page could
      // push values like 1e9 or 0 and resize the pet into uselessness.
      // `commit=1` marks the pointerup emission: same geometry update,
      // but the manager also notifies the host so the value persists.
      const valueStr = url.searchParams.get("value");
      if (!valueStr) return undefined;
      const value = parseFloat(valueStr);
      if (!Number.isFinite(value)) return undefined;
      const clamped = Math.max(
        CODEX_PET_SCALE_MIN,
        Math.min(CODEX_PET_SCALE_MAX, value),
      );
      return {
        action: "scale",
        value: clamped,
        commit: url.searchParams.get("commit") === "1",
      };
    }
    return undefined;
  } catch {
    return undefined;
  }
}

// Compute window bounds for a given bubble layout, keeping the sprite's
// bottom-center (the `anchor`) at the same screen point. The returned
// bounds are not yet clamped to the workArea; the caller decides whether
// the layout fits before applying. `size` defaults to the 100% preset so
// callers that don't pass one see the pre-size-feature geometry.
export function codexPetBoundsForLayout({
  layout,
  anchor,
  bubble,
  size = CODEX_PET_SIZE_DEFAULT,
  customScale,
}: {
  layout: Exclude<CodexPetBubbleLayout, "hidden">;
  anchor: { x: number; y: number };
  bubble: { width: number; height: number };
  size?: CodexPetSize;
  customScale?: number;
}): Rectangle {
  // During continuous resize we already know the live scale; use it
  // directly so the window bounds shrink/grow with the sprite. Without
  // it, fall back to the preset-derived geometry so callers that don't
  // know about continuous resize see the original behavior.
  const useCustomScale =
    typeof customScale === "number" &&
    Number.isFinite(customScale) &&
    customScale > 0;
  const rendered = useCustomScale
    ? codexPetRenderedSpriteForScale(customScale)
    : codexPetRenderedSpriteForSize(size);
  const spriteW = rendered.width;
  const spriteH = rendered.height;
  switch (layout) {
    case "above": {
      const width = Math.max(spriteW, bubble.width);
      const height =
        CODEX_PET_BUBBLE_PADDING +
        bubble.height +
        CODEX_PET_BUBBLE_GAP +
        spriteH +
        CODEX_PET_BUBBLE_PADDING;
      return {
        x: Math.round(anchor.x - width / 2),
        y: Math.round(anchor.y - height + CODEX_PET_SPRITE_BOTTOM_OFFSET),
        width,
        height,
      };
    }
    case "below": {
      const width = Math.max(spriteW, bubble.width);
      const height =
        CODEX_PET_BUBBLE_PADDING +
        spriteH +
        CODEX_PET_BUBBLE_GAP +
        bubble.height +
        CODEX_PET_BUBBLE_PADDING;
      return {
        x: Math.round(anchor.x - width / 2),
        y: Math.round(anchor.y - spriteH - CODEX_PET_BUBBLE_PADDING),
        width,
        height,
      };
    }
    case "right": {
      const width =
        CODEX_PET_BUBBLE_PADDING +
        spriteW +
        CODEX_PET_BUBBLE_GAP +
        bubble.width +
        CODEX_PET_BUBBLE_PADDING;
      const height =
        Math.max(spriteH, bubble.height) + 2 * CODEX_PET_BUBBLE_PADDING;
      return {
        x: Math.round(anchor.x - CODEX_PET_BUBBLE_PADDING - spriteW / 2),
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
        spriteW +
        CODEX_PET_BUBBLE_PADDING;
      const height =
        Math.max(spriteH, bubble.height) + 2 * CODEX_PET_BUBBLE_PADDING;
      return {
        x: Math.round(
          anchor.x - (width - CODEX_PET_BUBBLE_PADDING - spriteW / 2),
        ),
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
// `size` defaults to the 100% preset so callers that don't pass one see
// the pre-size-feature geometry.
export function selectCodexPetBubbleLayout({
  workArea,
  anchor,
  bubble,
  size = CODEX_PET_SIZE_DEFAULT,
  customScale,
}: {
  workArea: Rectangle;
  anchor: { x: number; y: number };
  bubble: { width: number; height: number };
  size?: CodexPetSize;
  customScale?: number;
}): CodexPetLayoutDecision {
  const order: Array<Exclude<CodexPetBubbleLayout, "hidden">> = [
    "above",
    "right",
    "left",
    "below",
  ];
  for (const layout of order) {
    const bounds = codexPetBoundsForLayout({
      layout,
      anchor,
      bubble,
      size,
      customScale,
    });
    if (
      bounds.x >= workArea.x &&
      bounds.y >= workArea.y &&
      bounds.x + bounds.width <= workArea.x + workArea.width &&
      bounds.y + bounds.height <= workArea.y + workArea.height
    ) {
      return { layout, bounds };
    }
  }
  const fallback =
    typeof customScale === "number" &&
    Number.isFinite(customScale) &&
    customScale > 0
      ? codexPetRenderedSpriteForScale(customScale)
      : codexPetRenderedSpriteForSize(size);
  return {
    layout: "hidden",
    bounds: {
      x: Math.round(anchor.x - fallback.windowWidth / 2),
      y: Math.round(
        anchor.y - fallback.windowHeight + CODEX_PET_SPRITE_BOTTOM_OFFSET,
      ),
      width: fallback.windowWidth,
      height: fallback.windowHeight,
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
    `--pet-frame-width:${view.spriteWidth}px`,
    `--pet-frame-height:${view.spriteHeight}px`,
    `--pet-scale:${view.scale}`,
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
.sprite-frame{width:var(--pet-frame-width);height:var(--pet-frame-height);display:flex;align-items:flex-end;justify-content:center;flex-shrink:0;pointer-events:none}
.sprite{width:${CODEX_PET_CELL_WIDTH}px;height:${CODEX_PET_CELL_HEIGHT}px;flex-shrink:0;transform:scale(var(--pet-scale));transform-origin:bottom center;background-image:var(--pet-sheet);background-repeat:no-repeat;background-position:0 var(--pet-y);image-rendering:pixelated;filter:drop-shadow(0 10px 16px rgba(0,0,0,.18));animation:pet-play var(--pet-duration) steps(var(--pet-frames)) infinite}
@keyframes pet-play{from{background-position:0 var(--pet-y)}to{background-position:var(--pet-end-x) var(--pet-y)}}
@media (prefers-reduced-motion:reduce){.sprite{animation:none}}
.bubble{display:block;width:${CODEX_PET_BUBBLE_WIDTH}px;min-height:${CODEX_PET_BUBBLE_HEIGHT}px;padding:12px;margin:${CODEX_PET_BUBBLE_PADDING}px;border-radius:12px;background:rgba(255,255,255,.95);box-shadow:0 6px 24px rgba(0,0,0,.18);cursor:pointer;flex-shrink:0;text-align:left;transition:opacity 200ms ease}
.stage.layout-hidden .bubble{display:none}
.bubble .preview{font:13px/1.45 -apple-system,BlinkMacSystemFont,system-ui,sans-serif;color:#1a1a1a;display:-webkit-box;-webkit-line-clamp:1;-webkit-box-orient:vertical;overflow:hidden;word-break:break-word;white-space:pre-wrap;opacity:1;transition:opacity ${CODEX_PET_BUBBLE_SWAP_IN_MS}ms ease}
.bubble.is-swapping .preview{opacity:0;transition:opacity ${CODEX_PET_BUBBLE_SWAP_OUT_MS}ms ease}
.resize-handle{position:absolute;z-index:2}
.resize-handle.edge-top{top:0;left:16px;right:16px;height:16px;cursor:ns-resize}
.resize-handle.edge-bottom{bottom:0;left:16px;right:16px;height:16px;cursor:ns-resize}
.resize-handle.edge-left{left:0;top:16px;bottom:16px;width:16px;cursor:ew-resize}
.resize-handle.edge-right{right:0;top:16px;bottom:16px;width:16px;cursor:ew-resize}
.resize-handle.corner-nw{top:0;left:0;width:16px;height:16px;cursor:nwse-resize}
.resize-handle.corner-ne{top:0;right:0;width:16px;height:16px;cursor:nesw-resize}
.resize-handle.corner-sw{bottom:0;left:0;width:16px;height:16px;cursor:nesw-resize}
.resize-handle.corner-se{bottom:0;right:0;width:16px;height:16px;cursor:nwse-resize}
</style></head><body><div class="stage layout-${escapeHTML(view.layout)}" style="${escapeHTML(styleVars)}">
<div class="bubble" data-thread-id="${escapeHTML(view.hint?.thread_id ?? "")}" style="display:${view.hint ? "block" : "none"}">
<div class="preview">${escapeHTML(view.hint?.preview ?? "")}</div>
</div>
<div class="sprite-frame"><div class="sprite" role="img" aria-label="${escapeHTML(view.label)}"></div></div>
<div class="resize-handle corner-nw" data-dir-x="-1" data-dir-y="-1" aria-label="调整大小"></div>
<div class="resize-handle corner-ne" data-dir-x="1" data-dir-y="-1" aria-label="调整大小"></div>
<div class="resize-handle corner-sw" data-dir-x="-1" data-dir-y="1" aria-label="调整大小"></div>
<div class="resize-handle corner-se" data-dir-x="1" data-dir-y="1" aria-label="调整大小"></div>
<div class="resize-handle edge-top" data-dir-x="0" data-dir-y="-1" aria-label="调整大小"></div>
<div class="resize-handle edge-bottom" data-dir-x="0" data-dir-y="1" aria-label="调整大小"></div>
<div class="resize-handle edge-left" data-dir-x="-1" data-dir-y="0" aria-label="调整大小"></div>
<div class="resize-handle edge-right" data-dir-x="1" data-dir-y="0" aria-label="调整大小"></div>
</div>
<script>
(() => {
  const stage = document.querySelector('.stage');
  const bubble = document.querySelector('.bubble');
  const sprite = document.querySelector('.sprite');
  // The bubble is text-only now (see codexPetWindowHTML above), so the live
  // updater only touches .preview. Earlier revisions still queried for a
  // .title / .dot node that the markup no longer renders, which raised a
  // TypeError on every wuuPetView refresh and silently broke hint updates.
  const previewEl = bubble.querySelector('.preview');
  let currentHint = ${initialHintJSON};
  // First apply is just the inline-rendered initial state, so no fade.
  // Subsequent applies fade out -> swap text -> fade in only when the
  // preview string actually changes (same title but new commentary, or
  // the bubble disappearing entirely). cancelSwap clears any pending
  // mid-swap timeout so a rapid back-to-back push doesn't strand the
  // .is-swapping class with mismatched text under it.
  let isFirstApply = true;
  let pendingSwap = null;
  const SWAP_OUT_MS = ${CODEX_PET_BUBBLE_SWAP_OUT_MS};
  const cancelSwap = () => {
    if (pendingSwap !== null) {
      clearTimeout(pendingSwap);
      pendingSwap = null;
      bubble.classList.remove('is-swapping');
    }
  };
  const applyHint = (hint) => {
    cancelSwap();
    // Hide the bubble whenever there's no stable commentary to surface.
    // The renderer's deriveActiveSessionHint now refuses to fall back to
    // Thread.preview, so a top-ranked thread with no agent_message yet
    // (running but hasn't started speaking, or idle with no commentary)
    // pushes a non-null hint with empty preview here. Treat both
    // 'null hint' and 'hint with empty/blank preview' as hide; the
    // sprite keeps its running/idle/failed state and no stale
    // fallback text is ever shown.
    const trimmedPreview = (hint?.preview ?? "").trim();
    if (!hint || trimmedPreview.length === 0) {
      if (isFirstApply) {
        currentHint = null;
        isFirstApply = false;
        return;
      }
      bubble.classList.add('is-swapping');
      pendingSwap = setTimeout(() => {
        pendingSwap = null;
        bubble.style.display = 'none';
        bubble.removeAttribute('data-thread-id');
        previewEl.textContent = '';
        bubble.classList.remove('is-swapping');
        currentHint = null;
      }, SWAP_OUT_MS);
      return;
    }
    bubble.style.display = '';
    const newPreview = trimmedPreview;
    const oldPreview = previewEl.textContent || '';
    if (isFirstApply) {
      bubble.dataset.threadId = hint.thread_id;
      previewEl.textContent = newPreview;
      currentHint = hint;
      isFirstApply = false;
      return;
    }
    if (oldPreview === newPreview) {
      bubble.dataset.threadId = hint.thread_id;
      currentHint = hint;
      return;
    }
    bubble.classList.add('is-swapping');
    pendingSwap = setTimeout(() => {
      pendingSwap = null;
      bubble.dataset.threadId = hint.thread_id;
      previewEl.textContent = newPreview;
      bubble.classList.remove('is-swapping');
      currentHint = hint;
    }, SWAP_OUT_MS);
  };
  applyHint(currentHint);
  window.wuuPetView = (view) => {
    sprite.style.setProperty('--pet-sheet', 'url("' + view.spritesheetURL + '")');
    sprite.style.setProperty('--pet-y', view.y + 'px');
    sprite.style.setProperty('--pet-end-x', view.endX + 'px');
    sprite.style.setProperty('--pet-frames', String(view.frames));
    sprite.style.setProperty('--pet-duration', view.duration + 'ms');
    sprite.style.setProperty('--pet-scale', String(view.scale));
    liveScale = view.scale;
    stage.style.setProperty('--pet-frame-width', view.spriteWidth + 'px');
    stage.style.setProperty('--pet-frame-height', view.spriteHeight + 'px');
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
  // Standard window-style continuous resize: four 16px corner squares
  // and four edge strips between them (all invisible — the directional
  // resize cursors are the affordance; earlier revisions painted a pair
  // of dark grip bars that read as a stray black line over the pet).
  // Everything still maps to the single uniform sprite scale: each
  // pointermove converts the drag delta along the handle's outward
  // direction (data-dir-x / data-dir-y) into a scale delta. The sprite
  // is a ${CODEX_PET_CELL_WIDTH}×${CODEX_PET_CELL_HEIGHT} cell under transform: scale, so 1px of drag =
  // 1px of rendered sprite width/height on that axis; corners average
  // both axes so a 45° drag tracks ~1:1 too. Dragging away from the
  // sprite grows (window-edge semantics), toward it shrinks. The main
  // process live-updates window bounds and CSS scale around the
  // sprite's bottom-center anchor; emission is rAF-throttled so a fast
  // drag doesn't queue more navigations than frames, and pointerup
  // re-sends the final value with commit=1 so the host persists it.
  // stopPropagation on pointerdown keeps the stage's move-the-window
  // drag from engaging, and a 2px dead-zone keeps a plain click from
  // emitting anything.
  const SCALE_MIN = ${CODEX_PET_SCALE_MIN};
  const SCALE_MAX = ${CODEX_PET_SCALE_MAX};
  const CELL_WIDTH = ${CODEX_PET_CELL_WIDTH};
  const CELL_HEIGHT = ${CODEX_PET_CELL_HEIGHT};
  let liveScale = ${JSON.stringify(view.scale)};
  const emitScale = (value, commit) => {
    location.href = 'wuu-pet://action/scale?value=' + encodeURIComponent(value)
      + (commit ? '&commit=1' : '');
  };
  let resizePointerID = null;
  let resizeStartX = 0;
  let resizeStartY = 0;
  let resizeStartScale = 0;
  let resizeMoved = false;
  let pendingScale = null;
  let resizeRAF = 0;
  const RESIZE_DEAD_ZONE = 2;
  const finishResize = (event) => {
    if (event.pointerId !== resizePointerID) return;
    resizePointerID = null;
    if (resizeRAF) {
      cancelAnimationFrame(resizeRAF);
      resizeRAF = 0;
    }
    if (resizeMoved && pendingScale !== null) {
      liveScale = pendingScale;
      emitScale(pendingScale, true);
    }
    pendingScale = null;
    resizeMoved = false;
  };
  for (const handle of stage.querySelectorAll('.resize-handle')) {
    const dirX = Number(handle.dataset.dirX) || 0;
    const dirY = Number(handle.dataset.dirY) || 0;
    const axes = (dirX !== 0 ? 1 : 0) + (dirY !== 0 ? 1 : 0);
    handle.addEventListener('pointerdown', (event) => {
      if (event.button !== 0) return;
      event.stopPropagation();
      resizePointerID = event.pointerId;
      resizeStartX = event.screenX;
      resizeStartY = event.screenY;
      resizeStartScale = liveScale;
      resizeMoved = false;
      pendingScale = null;
      handle.setPointerCapture(event.pointerId);
      event.preventDefault();
    });
    handle.addEventListener('pointermove', (event) => {
      if (event.pointerId !== resizePointerID) return;
      const deltaX = event.screenX - resizeStartX;
      const deltaY = event.screenY - resizeStartY;
      if (
        !resizeMoved &&
        Math.abs(deltaX) < RESIZE_DEAD_ZONE &&
        Math.abs(deltaY) < RESIZE_DEAD_ZONE
      ) {
        return;
      }
      resizeMoved = true;
      const deltaScale =
        ((dirX * deltaX) / CELL_WIDTH + (dirY * deltaY) / CELL_HEIGHT) / axes;
      pendingScale = Math.min(
        SCALE_MAX,
        Math.max(SCALE_MIN, resizeStartScale + deltaScale),
      );
      if (!resizeRAF) {
        resizeRAF = requestAnimationFrame(() => {
          resizeRAF = 0;
          if (pendingScale !== null) emitScale(pendingScale, false);
        });
      }
    });
    handle.addEventListener('pointerup', finishResize);
    handle.addEventListener('pointercancel', finishResize);
  }
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
  // User-facing sprite size; defaults to the 100% preset so older callers
  // and tests that don't set it see exactly the pre-size-feature window
  // geometry. Mutated only through setSize(), which re-applies the view
  // (so the window resizes around the sprite's existing anchor) and
  // notifies onSizeChange so the host shell can persist the choice.
  private size: CodexPetSize = CODEX_PET_SIZE_DEFAULT;
  // Live sprite scale, set when the user drags the resize strip with
  // the new continuous behavior. Overrides the preset-derived scale
  // while defined so the pet can be sized to any value between the
  // smallest and largest presets, not just the four preset ids. Cleared
  // the next time setSize() picks a preset (a future menu shortcut,
  // settings rehydration, or the inline script snapping to the nearest
  // preset on drag end). applyView() picks this up so the window
  // bounds and the CSS-driven sprite scale stay in sync without going
  // through the preset list.
  private currentScale: number | undefined;
  // The scale (preset-derived or continuous) that produced the window's
  // current bounds. anchorFromBounds() must reverse-engineer the sprite
  // anchor from the bounds as they are on screen, so it needs the scale
  // that was in effect when those bounds were applied — not this.size /
  // this.currentScale, which setSize()/setScale() mutate *before*
  // applyView() runs. Without this, every left/right-bubble resize frame
  // extracts a slightly wrong anchor and the pet drifts sideways over a
  // continuous drag.
  private appliedScale: number | undefined;

  constructor(
    private readonly onCloseRequested: () => void,
    private readonly onJumpRequested: (hint: CodexPetHint) => void,
    private readonly onSizeChange?: (size: CodexPetSize) => void,
    private readonly onScaleChange?: (scale: number) => void,
  ) {}

  setSize(size: CodexPetSize): void {
    if (size === this.size) return;
    this.size = size;
    // Switching to a preset clears any in-flight custom scale. The
    // previous drag's customScale no longer represents the user's
    // intent — they explicitly picked a discrete preset. (Edge-drag
    // resize itself never lands here anymore; it stays continuous and
    // commits through setScale. This path serves preset callers such
    // as settings rehydration or a future size menu.)
    this.currentScale = undefined;
    const pet = selectedCodexPet(this.snapshot);
    if (pet && this.win && !this.win.isDestroyed()) {
      this.applyView(pet);
    }
    if (this.onSizeChange) {
      this.onSizeChange(size);
    }
  }

  setScale(value: number, commit = false): void {
    // Live-update the sprite scale without going through the preset
    // list. The window resizes around the current anchor and the new
    // scale is pushed back to the renderer via applyView(). During the
    // drag (commit=false) the host shell is intentionally NOT notified —
    // continuous drag would otherwise spam persistence on every
    // pointermove frame. The pointerup emission arrives with commit=true
    // and pushes the final value to the host for persistence. Clamp here
    // too: the startup rehydration path calls setScale directly with
    // whatever desktop-settings.json holds, bypassing the URL parser's
    // clamp.
    const clamped = Math.max(
      CODEX_PET_SCALE_MIN,
      Math.min(CODEX_PET_SCALE_MAX, value),
    );
    this.currentScale = clamped;
    const pet = selectedCodexPet(this.snapshot);
    if (pet && this.win && !this.win.isDestroyed()) {
      this.applyView(pet);
    }
    if (commit && this.onScaleChange) {
      this.onScaleChange(clamped);
    }
  }

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
    this.appliedScale = undefined;
    if (win && !win.isDestroyed()) win.close();
  }

  private createWindow(pet: CodexPet): void {
    const workArea = screen.getPrimaryDisplay().workArea;
    // Startup rehydration may have pushed a persisted continuous scale
    // via setScale() before the window exists; honor it here the same
    // way applyView() does, so the pet doesn't flash at the preset size
    // and then jump on the first view push.
    const rendered =
      typeof this.currentScale === "number"
        ? codexPetRenderedSpriteForScale(this.currentScale)
        : codexPetRenderedSpriteForSize(this.size);
    const initialAnchor = {
      x:
        workArea.x +
        workArea.width -
        rendered.windowWidth / 2 -
        CODEX_PET_SCREEN_INSET,
      y:
        workArea.y +
        workArea.height -
        CODEX_PET_SPRITE_BOTTOM_OFFSET -
        CODEX_PET_SCREEN_INSET,
    };
    const layoutDecision = this.hint
      ? selectCodexPetBubbleLayout({
          workArea,
          anchor: initialAnchor,
          bubble: CODEX_PET_BUBBLE_SIZE,
          size: this.size,
          customScale: this.currentScale,
        })
      : undefined;
    const view = codexPetView(
      pet,
      codexPetStateForRuntime(this.runtime),
      this.hint,
      layoutDecision?.layout ?? "hidden",
      this.size,
      this.currentScale,
    );
    const layoutBounds =
      layoutDecision?.bounds ?? {
        x:
          workArea.x +
          workArea.width -
          rendered.windowWidth -
          CODEX_PET_SCREEN_INSET,
        y:
          workArea.y +
          workArea.height -
          rendered.windowHeight -
          CODEX_PET_SCREEN_INSET,
        width: rendered.windowWidth,
        height: rendered.windowHeight,
      };
    this.currentLayout = view.layout;
    this.appliedScale = rendered.scale;
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
      if (parsed.action === "resize") {
        this.setSize(parsed.id);
      }
      if (parsed.action === "scale") {
        this.setScale(parsed.value, parsed.commit);
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
      this.appliedScale = undefined;
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
        size: this.size,
        customScale: this.currentScale,
      });
      layout = decision.layout;
      targetBounds = decision.bounds;
    } else {
      layout = "hidden";
      const rendered =
        typeof this.currentScale === "number"
          ? codexPetRenderedSpriteForScale(this.currentScale)
          : codexPetRenderedSpriteForSize(this.size);
      targetBounds = {
        x: Math.round(anchor.x - rendered.windowWidth / 2),
        y: Math.round(
          anchor.y - rendered.windowHeight + CODEX_PET_SPRITE_BOTTOM_OFFSET,
        ),
        width: rendered.windowWidth,
        height: rendered.windowHeight,
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
    const view = codexPetView(
      pet,
      codexPetStateForRuntime(this.runtime),
      this.hint,
      layout,
      this.size,
      this.currentScale,
    );
    this.appliedScale = view.scale;
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
    // anchor = sprite's bottom-center on screen. Reads sprite footprint from
    // the scale that produced these bounds (see appliedScale) so a resize
    // that happens between snapshots still extracts the right anchor.
    const rendered =
      typeof this.appliedScale === "number"
        ? codexPetRenderedSpriteForScale(this.appliedScale)
        : codexPetRenderedSpriteForSize(this.size);
    const spriteW = rendered.width;
    const y = bounds.y + bounds.height - CODEX_PET_SPRITE_BOTTOM_OFFSET;
    let x: number;
    switch (layout) {
      case "above":
      case "below":
      case "hidden":
        x = bounds.x + bounds.width / 2;
        break;
      case "right":
        x = bounds.x + CODEX_PET_BUBBLE_PADDING + spriteW / 2;
        break;
      case "left":
        x = bounds.x + bounds.width - CODEX_PET_BUBBLE_PADDING - spriteW / 2;
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
