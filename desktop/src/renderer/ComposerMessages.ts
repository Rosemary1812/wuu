import type { ClipboardEvent as ReactClipboardEvent } from "react";
import type { InputFile, InputImage, Turn } from "../shared/protocol";

// Renderer-side image compression runs on the Electron canvas before the
// bytes cross the IPC boundary. It is a fast-path optimization, not the
// authoritative copy. The single source of truth lives in the Go core at
// internal/imageproc (imageproc.Encode). Three entry points call imageproc:
// `wuu exec --image` (internal/exec/attachments.go), `wuu app-server`'s
// `turn/start` (internal/appserver/turn_handlers.go), and any future shell
// that goes through the app-server. The renderer pre-compresses here to
// avoid shipping the original file across IPC.
//
// IMPORTANT: when changing the constants below, mirror the change in
// internal/imageproc (and vice versa). The byte-target ladder here is
// intentionally more aggressive than the core's pixel-and-patch budget
// because the renderer can afford the extra encode passes; the core path
// targets a fixed JPEG quality of 85 and a 32×32 patch budget instead.
const IMAGE_MAX_DIMENSION = 2000;
const IMAGE_TARGET_BYTES = (5 * 1024 * 1024 * 3) / 4;

export type ComposerImage = InputImage & {
  id: string;
};

export type ComposerFile = InputFile & {
  id: string;
};

export type QueuedComposerMessage = {
  id: string;
  text: string;
  images: ComposerImage[];
  files: ComposerFile[];
};

export function clipboardAttachmentFiles(event: ReactClipboardEvent<HTMLTextAreaElement>): File[] {
  const items = Array.from(event.clipboardData?.items ?? []);
  const files: File[] = [];
  for (const item of items) {
    if (item.kind !== "file") {
      continue;
    }
    const file = item.getAsFile();
    if (file && isSupportedComposerAttachment(file)) {
      files.push(file);
    }
  }
  return files;
}

export async function composerImageFromFile(file: File): Promise<ComposerImage> {
  const image = await normalizeImageFileForPrompt(file);
  return {
    id: nextComposerAttachmentID(),
    ...image
  };
}

export async function composerFileFromFile(file: File): Promise<ComposerFile> {
  if (!isPDFFile(file)) {
    throw new Error("仅支持 PDF 文件");
  }
  const data = await bufferToBase64(await file.arrayBuffer());
  return {
    id: nextComposerAttachmentID(),
    media_type: "application/pdf",
    data,
    filename: file.name.trim() || "attachment.pdf"
  };
}

export function isSupportedComposerAttachment(file: File): boolean {
  return file.type.toLowerCase().startsWith("image/") || isPDFFile(file);
}

export function isComposerImageFile(file: File): boolean {
  return file.type.toLowerCase().startsWith("image/");
}

export function isPDFFile(file: File): boolean {
  return file.type.toLowerCase() === "application/pdf" || file.name.toLowerCase().endsWith(".pdf");
}

async function normalizeImageFileForPrompt(file: File): Promise<InputImage> {
  const mediaType = normalizeImageMediaType(file.type);
  const original = await file.arrayBuffer();
  const passthrough = async (): Promise<InputImage> => ({
    media_type: mediaType,
    data: await bufferToBase64(original)
  });

  try {
    const bitmap = await createImageBitmap(new Blob([original], { type: mediaType }));
    try {
      if (original.byteLength <= IMAGE_TARGET_BYTES && bitmap.width <= IMAGE_MAX_DIMENSION && bitmap.height <= IMAGE_MAX_DIMENSION) {
        return passthrough();
      }

      const [width, height] = clampImageDimensions(bitmap.width, bitmap.height, IMAGE_MAX_DIMENSION);
      const canvas = document.createElement("canvas");
      canvas.width = width;
      canvas.height = height;
      const context = canvas.getContext("2d");
      if (!context) {
        return passthrough();
      }
      context.drawImage(bitmap, 0, 0, width, height);

      const strategies: Array<{ mediaType: string; quality?: number }> = [
        { mediaType: "image/png" },
        { mediaType: "image/jpeg", quality: 0.82 },
        { mediaType: "image/jpeg", quality: 0.68 },
        { mediaType: "image/jpeg", quality: 0.52 },
        { mediaType: "image/jpeg", quality: 0.38 }
      ];
      let fallback: InputImage | undefined;
      for (const strategy of strategies) {
        const blob = await canvasToBlob(canvas, strategy.mediaType, strategy.quality);
        const encoded = {
          media_type: strategy.mediaType,
          data: await bufferToBase64(await blob.arrayBuffer())
        };
        fallback = encoded;
        if (blob.size <= IMAGE_TARGET_BYTES) {
          return encoded;
        }
      }
      return fallback ?? passthrough();
    } finally {
      bitmap.close();
    }
  } catch {
    return passthrough();
  }
}

function normalizeImageMediaType(value: string): string {
  const mediaType = value.trim().toLowerCase();
  if (mediaType === "image/jpg") {
    return "image/jpeg";
  }
  return mediaType.startsWith("image/") ? mediaType : "image/png";
}

function clampImageDimensions(width: number, height: number, maxDimension: number): [number, number] {
  if (width <= maxDimension && height <= maxDimension) {
    return [width, height];
  }
  if (width >= height) {
    return [maxDimension, Math.max(1, Math.round((height * maxDimension) / width))];
  }
  return [Math.max(1, Math.round((width * maxDimension) / height)), maxDimension];
}

function canvasToBlob(canvas: HTMLCanvasElement, mediaType: string, quality?: number): Promise<Blob> {
  return new Promise((resolve, reject) => {
    canvas.toBlob(
      (blob) => {
        if (!blob) {
          reject(new Error("无法处理图片"));
          return;
        }
        resolve(blob);
      },
      mediaType,
      quality
    );
  });
}

// Encode ArrayBuffer to base64 via FileReader.readAsDataURL. The native
// implementation runs off the JS main thread and is significantly faster
// than a hand-rolled `btoa(String.fromCharCode(...))` loop, especially for
// the multi-MB buffers we expect from clipboard image pastes. Returning a
// Promise keeps the call sites uniform with the rest of the pipeline
// (createImageBitmap, canvas.toBlob, etc.).
function bufferToBase64(buffer: ArrayBuffer): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const result = reader.result;
      if (typeof result !== "string") {
        reject(new Error("无法编码图片"));
        return;
      }
      const commaIndex = result.indexOf(",");
      resolve(commaIndex >= 0 ? result.slice(commaIndex + 1) : result);
    };
    reader.onerror = () => reject(reader.error ?? new Error("无法编码图片"));
    reader.readAsDataURL(new Blob([buffer]));
  });
}

function nextComposerAttachmentID(): string {
  const browserCrypto = globalThis.crypto as Crypto & { randomUUID?: () => string };
  return browserCrypto.randomUUID?.() ?? `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}

function nextComposerMessageID(): string {
  return nextComposerAttachmentID();
}

export function imageSource(image: InputImage): string {
  const mediaType = normalizeImageMediaType(image.media_type);
  return `data:${mediaType};base64,${image.data}`;
}

export function createComposerMessage(
  text: string,
  images: ComposerImage[],
  files: ComposerFile[] = []
): QueuedComposerMessage | undefined {
  const trimmed = text.trim();
  if (!trimmed && images.length === 0 && files.length === 0) {
    return undefined;
  }
  return {
    id: nextComposerMessageID(),
    text,
    images: images.map((image) => ({ ...image })),
    files: files.map((file) => ({ ...file }))
  };
}

export function inputImagesFromComposer(images: ComposerImage[]): InputImage[] {
  return images.map(({ media_type, data }) => ({ media_type, data }));
}

export function inputFilesFromComposer(files: ComposerFile[]): InputFile[] {
  return files.map(({ media_type, data, filename }) => ({ media_type, data, filename }));
}

export function mergeGuideMessages(messages: QueuedComposerMessage[]): QueuedComposerMessage {
  return {
    id: nextComposerMessageID(),
    text: messages
      .map((message) => message.text.trim())
      .filter(Boolean)
      .join("\n"),
    images: messages.flatMap((message) => message.images.map((image) => ({ ...image }))),
    files: messages.flatMap((message) => message.files.map((file) => ({ ...file })))
  };
}

export function queuedMessagePreview(message: QueuedComposerMessage): string {
  const text = message.text.trim().replace(/\s+/g, " ");
  const imageText = message.images.length > 0 ? `${message.images.length} 张图片` : "";
  const fileText = message.files.length > 0 ? `${message.files.length} 个文件` : "";
  const preview = [text, imageText, fileText].filter(Boolean).join(" · ");
  return trimMiddle(preview || "空消息", 48);
}

function trimMiddle(value: string, maxLength: number): string {
  if (value.length <= maxLength) {
    return value;
  }
  const left = Math.ceil((maxLength - 1) / 2);
  const right = Math.floor((maxLength - 1) / 2);
  return `${value.slice(0, left)}…${value.slice(value.length - right)}`;
}

/**
 * Prefix that distinguishes client-side optimistic placeholders from real
 * turn ids minted by the Go core. The renderer can use this to filter,
 * drop, or otherwise special-case them before the real turn arrives.
 */
export const OPTIMISTIC_TURN_ID_PREFIX = "optimistic-turn-";

function nextOptimisticTurnID(): string {
  return `${OPTIMISTIC_TURN_ID_PREFIX}${nextComposerMessageID()}`;
}

/**
 * Build an `in_progress` turn placeholder that mirrors the user's
 * just-sent message. Inserting it into local state before the Go core
 * round-trip completes lets the conversation surface "正在处理/回复"
 * and the live elapsed timer immediately, instead of waiting for the
 * first server-side turn notification.
 *
 * `AssistantTurnDisplay.buildAssistantTurnDisplay` now always returns a
 * display for in_progress turns, so this placeholder does not need to
 * seed an extra agent_message item. The placeholder turn is filtered
 * out by `replaceOptimisticTurn` once the real turn arrives.
 */
export function createOptimisticTurn(
  message: QueuedComposerMessage,
  nowMs: number,
): Turn {
  const id = nextOptimisticTurnID();
  return {
    id,
    status: "in_progress",
    started_at: new Date(nowMs).toISOString(),
    items_view: "full",
    items: [
      {
        id: `${id}-user`,
        type: "user_message",
        status: "completed",
        text: message.text,
        images: inputImagesFromComposer(message.images),
        files: inputFilesFromComposer(message.files),
      },
    ],
  };
}

/**
 * Return the earlier of two ISO timestamps. Used to keep the optimistic
 * started_at (which captures the user's click moment) when the real turn
 * arrives with a slightly later server-side timestamp, so the live timer
 * never appears to jump backwards. Accepts null/undefined to match
 * Turn["started_at"] and returns the same shape so callers can assign the
 * result back without coercion.
 */
export function earlierStartedAt(
  a?: string | null,
  b?: string | null,
): string | null | undefined {
  const aMs = a ? Date.parse(a) : Number.NaN;
  const bMs = b ? Date.parse(b) : Number.NaN;
  if (Number.isFinite(aMs) && Number.isFinite(bMs)) {
    return aMs <= bMs ? a : b;
  }
  if (Number.isFinite(aMs)) {
    return a;
  }
  if (Number.isFinite(bMs)) {
    return b;
  }
  return undefined;
}

/**
 * Drop an optimistic turn placeholder from a thread by id. Used on send
 * failure so the user doesn't see a stuck "正在回复" turn that never
 * receives a real response. A undefined id is a no-op so the helper is
 * safe to call from error paths that may not have inserted a placeholder.
 */
export function dropOptimisticTurn<T extends { turns: Turn[] }>(
  thread: T,
  optimisticTurnID: string | undefined,
): T {
  if (!optimisticTurnID) {
    return thread;
  }
  return {
    ...thread,
    turns: thread.turns.filter((turn) => turn.id !== optimisticTurnID),
  };
}

/**
 * Apply `dropOptimisticTurn` to every thread whose id matches. Useful
 * from renderer-level error paths that hold a `Thread[]` and don't want
 * to round-trip through AppState's `updateThreadByID` (which only accepts
 * the full AppState).
 */
export function dropOptimisticTurnInThreads<T extends { turns: Turn[] }>(
  threads: T[],
  optimisticTurnID: string | undefined,
): T[] {
  if (!optimisticTurnID) {
    return threads;
  }
  return threads.map((thread) => dropOptimisticTurn(thread, optimisticTurnID));
}

/**
 * Replace an optimistic turn placeholder with the real turn returned by
 * the Go core, preserving the earlier started_at so the live timer does
 * not jump. If the optimistic placeholder is no longer in the thread
 * (e.g. it was cleared by a concurrent action), the real turn is still
 * inserted via `upsertTurn`.
 */
export function replaceOptimisticTurn<T extends { turns: Turn[] }>(
  thread: T,
  optimisticTurnID: string,
  realTurn: Turn,
  upsertTurn: (thread: T, turn: Turn) => T,
): T {
  const optimisticStartedAt = thread.turns.find(
    (turn) => turn.id === optimisticTurnID,
  )?.started_at;
  const turnsWithoutOptimistic = thread.turns.filter(
    (turn) => turn.id !== optimisticTurnID,
  );
  const merged: Turn = {
    ...realTurn,
    started_at:
      earlierStartedAt(optimisticStartedAt, realTurn.started_at) ?? null,
  };
  return upsertTurn({ ...thread, turns: turnsWithoutOptimistic }, merged);
}
