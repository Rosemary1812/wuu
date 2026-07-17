import { useRef, useState, type MutableRefObject } from "react";
import type { ServerEvent, Thread } from "../shared/protocol";
import {
  activeThreadIDForState,
  activeTurnIDForThread,
  composerDraftHasContent,
  isThreadRunning,
  threadForTab,
  threadItemFromRecord,
  type AppState,
  type ComposerDraftState,
} from "./AppState";
import {
  inputFilesFromComposer,
  inputImagesFromComposer,
  type QueuedComposerMessage,
} from "./ComposerMessages";
import {
  appendPendingComposerMessage,
  applyHeldComposerSnapshot,
  emptyThreadPendingComposerMessages,
  findPendingComposerMessage,
  pendingComposerMessagesForThread,
  reconcilePendingComposerMessagesForThread,
  removePendingComposerMessagesByID,
  threadPendingComposerMessagesIsEmpty,
  type PendingComposerMessageRemovalScope,
  type PendingComposerMessagesByThread,
  type ThreadPendingComposerMessages,
} from "./ComposerPendingMessages";
import { isRecord, recordValue, stringValue } from "./ToolActivity";
import { localizedText, translateCurrent } from "./i18n";

function heldComposerMessage(
  value: unknown,
  position: number,
): QueuedComposerMessage | undefined {
  if (!isRecord(value)) {
    return undefined;
  }
  const id = stringValue(value, "id");
  const origin = stringValue(value, "origin");
  if (!id || (origin !== "queue" && origin !== "steer")) {
    return undefined;
  }
  const images = (Array.isArray(value.images) ? value.images : []).flatMap(
    (candidate, index) => {
      if (!isRecord(candidate)) {
        return [];
      }
      const mediaType = stringValue(candidate, "media_type");
      const data = stringValue(candidate, "data");
      return mediaType && data
        ? [{ id: `${id}-held-image-${index}`, media_type: mediaType, data }]
        : [];
    },
  );
  const files = (Array.isArray(value.files) ? value.files : []).flatMap(
    (candidate, index) => {
      if (!isRecord(candidate)) {
        return [];
      }
      const mediaType = stringValue(candidate, "media_type");
      const data = stringValue(candidate, "data");
      if (!mediaType || !data) {
        return [];
      }
      const filename = stringValue(candidate, "filename");
      return [
        {
          id: `${id}-held-file-${index}`,
          media_type: mediaType,
          data,
          ...(filename ? { filename } : {}),
        },
      ];
    },
  );
  return {
    id,
    text: stringValue(value, "prompt") ?? "",
    images,
    files,
    held: true,
    heldPosition: position,
    origin,
  };
}

function heldComposerMessagesFromParams(
  params: Record<string, unknown>,
  method: string,
): { threadID: string; messages: QueuedComposerMessage[] } | undefined {
  const thread = recordValue(params, "thread");
  const threadID =
    stringValue(params, "thread_id") ||
    (isRecord(thread) ? stringValue(thread, "id") : "");
  if (!threadID) {
    return undefined;
  }
  const key = method === "thread/resumed" ? "held_user_messages" : "messages";
  const raw = params[key];
  if (method !== "thread/resumed" && !Array.isArray(raw)) {
    return undefined;
  }
  const messages = (Array.isArray(raw) ? raw : []).flatMap((value, position) => {
    const message = heldComposerMessage(value, position);
    return message ? [message] : [];
  });
  return { threadID, messages };
}

export type ComposerPendingStateController = {
  pendingComposerMessagesByThread: PendingComposerMessagesByThread;
  pendingComposerMessagesByThreadRef: MutableRefObject<PendingComposerMessagesByThread>;
  setPendingComposerMessagesByThreadNow: (
    messagesByThread: PendingComposerMessagesByThread,
  ) => void;
  pendingComposerMessagesForThread: (
    threadID: string | undefined,
  ) => ThreadPendingComposerMessages;
  updateThreadPendingComposerMessages: (
    threadID: string,
    update: (
      previous: ThreadPendingComposerMessages,
    ) => ThreadPendingComposerMessages,
  ) => void;
  clearThreadPendingComposerMessages: (threadID: string) => void;
  removePendingComposerMessageByID: (
    threadID: string | undefined,
    id: string,
    scope?: PendingComposerMessageRemovalScope,
  ) => void;
  syncPendingComposerMessagesFromServerEvent: (event: ServerEvent) => void;
  reconcilePendingComposerMessagesForState: (state: AppState) => void;
  enqueueComposerMessage: (
    threadID: string,
    message: QueuedComposerMessage,
  ) => void;
  removeQueuedMessage: (id: string) => Promise<boolean>;
  removeGuideMessage: (id: string) => Promise<boolean>;
  editQueuedMessage: (id: string) => Promise<void>;
  editGuideMessage: (id: string) => Promise<void>;
  guideQueuedMessage: (id: string) => Promise<void>;
  threadHasPendingComposerMessages: (threadID: string) => boolean;
};

type ComposerPendingStateOptions = {
  getAppState: () => AppState;
  getPrimaryComposerDraft: () => ComposerDraftState;
  restoreComposerDraftForThread: (
    threadID: string,
    draft: ComposerDraftState,
  ) => void;
  setStatus: (status: string) => void;
  sendComposerMessageToThread: (
    message: QueuedComposerMessage,
    targetThread: Thread,
  ) => Promise<boolean>;
};

export function useComposerPendingState({
  getAppState,
  getPrimaryComposerDraft,
  restoreComposerDraftForThread,
  setStatus,
  sendComposerMessageToThread,
}: ComposerPendingStateOptions): ComposerPendingStateController {
  const [pendingComposerMessagesByThread, setPendingComposerMessagesByThread] =
    useState<PendingComposerMessagesByThread>({});
  const pendingComposerMessagesByThreadRef =
    useRef<PendingComposerMessagesByThread>({});

  function setPendingComposerMessagesByThreadNow(
    messagesByThread: PendingComposerMessagesByThread,
  ): void {
    pendingComposerMessagesByThreadRef.current = messagesByThread;
    setPendingComposerMessagesByThread(messagesByThread);
  }

  function updateThreadPendingComposerMessages(
    threadID: string,
    update: (
      previous: ThreadPendingComposerMessages,
    ) => ThreadPendingComposerMessages,
  ): void {
    const previousByThread = pendingComposerMessagesByThreadRef.current;
    const previous =
      previousByThread[threadID] ?? emptyThreadPendingComposerMessages();
    const nextForThread = update(previous);
    const nextByThread = { ...previousByThread };
    if (threadPendingComposerMessagesIsEmpty(nextForThread)) {
      delete nextByThread[threadID];
    } else {
      nextByThread[threadID] = nextForThread;
    }
    setPendingComposerMessagesByThreadNow(nextByThread);
  }

  function clearThreadPendingComposerMessages(threadID: string): void {
    const previousByThread = pendingComposerMessagesByThreadRef.current;
    if (!previousByThread[threadID]) {
      return;
    }
    const nextByThread = { ...previousByThread };
    delete nextByThread[threadID];
    setPendingComposerMessagesByThreadNow(nextByThread);
  }

  function removePendingComposerMessageByID(
    threadID: string | undefined,
    id: string,
    scope: PendingComposerMessageRemovalScope = "all",
  ): void {
    setPendingComposerMessagesByThreadNow(
      removePendingComposerMessagesByID(
        pendingComposerMessagesByThreadRef.current,
        threadID,
        id,
        scope,
      ),
    );
  }

  function syncPendingComposerMessagesFromServerEvent(
    event: ServerEvent,
  ): void {
    if (event.kind !== "notification") {
      return;
    }
    const params = isRecord(event.message.params)
      ? event.message.params
      : undefined;
    if (!params) {
      return;
    }
    const threadID = stringValue(params, "thread_id");
    if (
      event.message.method === "turn/held" ||
      event.message.method === "thread/resumed"
    ) {
      const snapshot = heldComposerMessagesFromParams(
        params,
        event.message.method,
      );
      if (!snapshot) {
        return;
      }
      updateThreadPendingComposerMessages(snapshot.threadID, (previous) =>
        applyHeldComposerSnapshot(previous, snapshot.messages),
      );
      return;
    }
    if (event.message.method === "turn/started") {
      const queueID = stringValue(params, "queue_id");
      if (queueID) {
        removePendingComposerMessageByID(threadID, queueID);
      }
      return;
    }
    if (event.message.method === "turn/dequeued") {
      const queueID = stringValue(params, "queue_id");
      if (queueID) {
        removePendingComposerMessageByID(threadID, queueID, "queue");
      }
      return;
    }
    if (event.message.method === "item/completed") {
      const item = threadItemFromRecord(recordValue(params, "item"));
      if (item?.type === "user_message" && item.source_id) {
        removePendingComposerMessageByID(threadID, item.source_id);
      }
    }
  }

  function reconcilePendingComposerMessagesForState(state: AppState): void {
    const byThread = pendingComposerMessagesByThreadRef.current;
    let next = byThread;
    for (const threadID of Object.keys(byThread)) {
      const thread = threadForTab(state, threadID);
      if (thread) {
        next = reconcilePendingComposerMessagesForThread(next, thread);
      }
    }
    if (next !== byThread) {
      setPendingComposerMessagesByThreadNow(next);
    }
  }

  function enqueueComposerMessage(
    threadID: string,
    message: QueuedComposerMessage,
  ): void {
    updateThreadPendingComposerMessages(threadID, (previous) =>
      appendPendingComposerMessage(previous, "queue", message),
    );
  }

  async function removeQueuedMessage(id: string): Promise<boolean> {
    const target = findPendingComposerMessage(
      pendingComposerMessagesByThreadRef.current,
      id,
      "queue",
      activeThreadIDForState(getAppState()),
    );
    if (!target) {
      return false;
    }
    updateThreadPendingComposerMessages(target.threadID, (previous) => ({
      ...previous,
      queued: previous.queued.filter((message) => message.id !== id),
    }));
    try {
      const result = await window.wuu.dequeueTurn(target.threadID, id);
      if (!result.ok) {
        setStatus(localizedText("composer.queueAlreadyHandled"));
        return false;
      }
      return true;
    } catch (error) {
      updateThreadPendingComposerMessages(target.threadID, (previous) => {
        if (previous.queued.some((message) => message.id === id)) {
          return previous;
        }
        const insertAt = Math.min(target.index, previous.queued.length);
        return {
          ...previous,
          queued: [
            ...previous.queued.slice(0, insertAt),
            target.message,
            ...previous.queued.slice(insertAt),
          ],
        };
      });
      setStatus(
        error instanceof Error
          ? error.message
          : translateCurrent("composer.cancelQueueFailed"),
      );
      return false;
    }
  }

  async function removeGuideMessage(id: string): Promise<boolean> {
    const target = findPendingComposerMessage(
      pendingComposerMessagesByThreadRef.current,
      id,
      "guide",
      activeThreadIDForState(getAppState()),
    );
    if (!target) {
      return false;
    }
    updateThreadPendingComposerMessages(target.threadID, (previous) => ({
      ...previous,
      guides: previous.guides.filter((message) => message.id !== id),
    }));
    try {
      const result = await window.wuu.unsteerTurn(target.threadID, id);
      if (!result.ok) {
        setStatus(localizedText("composer.guideAlreadyHandled"));
        return false;
      }
      return true;
    } catch (error) {
      updateThreadPendingComposerMessages(target.threadID, (previous) => {
        if (previous.guides.some((message) => message.id === id)) {
          return previous;
        }
        const insertAt = Math.min(target.index, previous.guides.length);
        return {
          ...previous,
          guides: [
            ...previous.guides.slice(0, insertAt),
            target.message,
            ...previous.guides.slice(insertAt),
          ],
        };
      });
      setStatus(
        error instanceof Error
          ? error.message
          : translateCurrent("composer.cancelGuideFailed"),
      );
      return false;
    }
  }

  function restorePendingComposerMessage(
    threadID: string,
    message: QueuedComposerMessage,
  ): void {
    restoreComposerDraftForThread(threadID, {
      prompt: message.text,
      images: message.images.map((image) => ({ ...image })),
      files: message.files.map((file) => ({ ...file })),
    });
  }

  function canRestorePendingComposerMessage(): boolean {
    if (!composerDraftHasContent(getPrimaryComposerDraft())) {
      return true;
    }
    setStatus(localizedText("composer.clearBeforeEditingQueue"));
    return false;
  }

  async function editQueuedMessage(id: string): Promise<void> {
    const target = findPendingComposerMessage(
      pendingComposerMessagesByThreadRef.current,
      id,
      "queue",
      activeThreadIDForState(getAppState()),
    );
    if (!target || !canRestorePendingComposerMessage()) {
      return;
    }
    if (!(await removeQueuedMessage(id))) {
      return;
    }
    restorePendingComposerMessage(target.threadID, target.message);
    setStatus(localizedText("composer.queueRestoredForEditing"));
  }

  async function editGuideMessage(id: string): Promise<void> {
    const target = findPendingComposerMessage(
      pendingComposerMessagesByThreadRef.current,
      id,
      "guide",
      activeThreadIDForState(getAppState()),
    );
    if (!target || !canRestorePendingComposerMessage()) {
      return;
    }
    if (await removeGuideMessage(id)) {
      restorePendingComposerMessage(target.threadID, target.message);
    }
  }

  async function guideQueuedMessage(id: string): Promise<void> {
    const target = findPendingComposerMessage(
      pendingComposerMessagesByThreadRef.current,
      id,
      "queue",
      activeThreadIDForState(getAppState()),
    ) ??
      findPendingComposerMessage(
        pendingComposerMessagesByThreadRef.current,
        id,
        "guide",
        activeThreadIDForState(getAppState()),
      );
    if (!target) {
      return;
    }
    const currentState = getAppState();
    const targetThread = threadForTab(currentState, target.threadID);
    if (!targetThread) {
      return;
    }
    if (!isThreadRunning(targetThread) && !target.message.held) {
      if (!(await removeQueuedMessage(id))) {
        return;
      }
      if (!(await sendComposerMessageToThread(target.message, targetThread))) {
        restorePendingComposerMessage(target.threadID, target.message);
      }
      return;
    }
    const turnID = activeTurnIDForThread(targetThread) ?? "";
    if (!turnID && !target.message.held) {
      setStatus(localizedText("composer.noActiveTurnToGuide"));
      return;
    }
    try {
      const files = inputFilesFromComposer(target.message.files);
      await window.wuu.steerTurn(
        targetThread.id,
        turnID,
        target.message.text.trim(),
        inputImagesFromComposer(target.message.images),
        target.message.id,
        files,
      );
      updateThreadPendingComposerMessages(target.threadID, (previous) => ({
        queued: previous.queued.filter((message) => message.id !== id),
        guides: target.message.held
          ? previous.guides.filter((message) => message.id !== id)
          : [...previous.guides, target.message],
      }));
    } catch (error) {
      setStatus(
        error instanceof Error
          ? error.message
          : translateCurrent("composer.guideFailed"),
      );
    }
  }

  function threadHasPendingComposerMessages(threadID: string): boolean {
    return !threadPendingComposerMessagesIsEmpty(
      pendingComposerMessagesForThread(
        pendingComposerMessagesByThreadRef.current,
        threadID,
      ),
    );
  }

  return {
    pendingComposerMessagesByThread,
    pendingComposerMessagesByThreadRef,
    setPendingComposerMessagesByThreadNow,
    pendingComposerMessagesForThread: (threadID) =>
      pendingComposerMessagesForThread(
        pendingComposerMessagesByThread,
        threadID,
      ),
    updateThreadPendingComposerMessages,
    clearThreadPendingComposerMessages,
    removePendingComposerMessageByID,
    syncPendingComposerMessagesFromServerEvent,
    reconcilePendingComposerMessagesForState,
    enqueueComposerMessage,
    removeQueuedMessage,
    removeGuideMessage,
    editQueuedMessage,
    editGuideMessage,
    guideQueuedMessage,
    threadHasPendingComposerMessages,
  };
}
