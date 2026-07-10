import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type Dispatch,
  type SetStateAction,
} from "react";
import type { SubthreadUpdatedNotification, Thread, ThreadItem } from "../shared/protocol";
import {
  awaitComposerImages,
  inputFilesFromComposer,
  inputImagesFromComposer,
} from "./ComposerMessages";
import {
  applySubthreadUpdatedNotification,
  emptyComposerDraft,
  type ComposerDraftState,
  type OpenSubthreadPanel,
} from "./AppState";
import { desktopApiErrorMessage } from "./WorkspaceReviewHelpers";

export type ConversationSubthreadStateOptions = {
  activeThreadID?: string;
  subthreadComposerDraft: ComposerDraftState;
  setSubthreadComposerDraft: Dispatch<SetStateAction<ComposerDraftState>>;
  onOpenSubthreadPanel?: () => void;
};

export type ConversationSubthreadStateController = {
  openSubthreadPanel: OpenSubthreadPanel | undefined;
  setOpenSubthreadPanel: Dispatch<
    SetStateAction<OpenSubthreadPanel | undefined>
  >;
  chatSubthreadsNonce: number;
  handleSubthreadUpdatedNotification: (
    note: SubthreadUpdatedNotification,
    activeThreadID?: string,
  ) => void;
  openConversationSubthreadByID: (
    threadID: string,
    subthreadID: string,
  ) => void;
  openConversationSubthread: (
    thread: Thread,
    item: ThreadItem,
    threadOwnerParticipantID?: string,
    existingSubthreadID?: string,
  ) => void;
  resolveOpenConversationSubthread: (resolved: boolean) => void;
  sendOpenConversationSubthreadMessage: () => void;
  escalateOpenConversationSubthread: () => void;
  reactToOpenConversationSubthreadMessage: (
    item: ThreadItem,
    reaction: string,
  ) => void;
};

export function useConversationSubthreadState({
  activeThreadID,
  subthreadComposerDraft,
  setSubthreadComposerDraft,
  onOpenSubthreadPanel,
}: ConversationSubthreadStateOptions): ConversationSubthreadStateController {
  const [openSubthreadPanel, setOpenSubthreadPanel] = useState<
    OpenSubthreadPanel | undefined
  >();
  const [chatSubthreadsNonce, setChatSubthreadsNonce] = useState(0);
  const openRequestVersionRef = useRef(0);
  const openPanelIdentityRef = useRef<
    { threadID: string; subthreadID?: string } | undefined
  >(undefined);

  const invalidateOpenPanel = useCallback(
    (identity?: { threadID: string; subthreadID?: string }): number => {
      openPanelIdentityRef.current = identity;
      openRequestVersionRef.current += 1;
      return openRequestVersionRef.current;
    },
    [],
  );

  const setOpenSubthreadPanelExternal = useCallback<
    Dispatch<SetStateAction<OpenSubthreadPanel | undefined>>
  >((update) => {
    if (typeof update !== "function") {
      invalidateOpenPanel(
        update
          ? {
              threadID: update.threadID,
              subthreadID: update.subthread?.id,
            }
          : undefined,
      );
      setOpenSubthreadPanel(update);
      return;
    }
    invalidateOpenPanel();
    setOpenSubthreadPanel((current) => {
      const next = update(current);
      openPanelIdentityRef.current = next
        ? { threadID: next.threadID, subthreadID: next.subthread?.id }
        : undefined;
      return next;
    });
  }, [invalidateOpenPanel]);

  const openSubthreadID = openSubthreadPanel?.subthread?.id;
  useEffect(() => {
    setSubthreadComposerDraft(emptyComposerDraft());
  }, [openSubthreadID, setSubthreadComposerDraft]);

  useEffect(() => {
    if (
      openSubthreadPanel &&
      activeThreadID &&
      openSubthreadPanel.threadID !== activeThreadID
    ) {
      invalidateOpenPanel();
      setOpenSubthreadPanel(undefined);
    }
  }, [activeThreadID, invalidateOpenPanel, openSubthreadPanel]);

  const handleSubthreadUpdatedNotification = useCallback(
    (note: SubthreadUpdatedNotification, targetActiveThreadID?: string): void => {
      setOpenSubthreadPanel((prev) =>
        applySubthreadUpdatedNotification(prev, note),
      );
      if (note?.thread_id && targetActiveThreadID === note.thread_id) {
        setChatSubthreadsNonce((nonce) => nonce + 1);
      }
    },
    [],
  );

  function openConversationSubthreadByID(
    threadID: string,
    subthreadID: string,
  ): void {
    const requestVersion = invalidateOpenPanel({ threadID, subthreadID });
    setOpenSubthreadPanel({ threadID, subthread: undefined, loading: true });
    void (async () => {
      try {
        const result = await window.wuu.openConversationSubthread(threadID, {
          subthreadId: subthreadID,
        });
        if (openRequestVersionRef.current === requestVersion) {
          openPanelIdentityRef.current = {
            threadID,
            subthreadID: result.subthread.id,
          };
        }
        setOpenSubthreadPanel((current) =>
          openRequestVersionRef.current === requestVersion &&
          current?.threadID === threadID
            ? {
                threadID,
                subthread: result.subthread,
                loading: false,
              }
            : current,
        );
      } catch (error) {
        setOpenSubthreadPanel((current) =>
          openRequestVersionRef.current === requestVersion &&
          current?.threadID === threadID
            ? {
                threadID,
                loading: false,
                error: desktopApiErrorMessage(error, "无法打开 thread"),
              }
            : current,
        );
      }
    })();
  }

  function openConversationSubthread(
    thread: Thread,
    item: ThreadItem,
    threadOwnerParticipantID?: string,
    existingSubthreadID?: string,
  ): void {
    if (!thread.group) {
      return;
    }
    const subthreadID = existingSubthreadID?.trim() || item.task?.subthread_id;
    const ownerID =
      threadOwnerParticipantID?.trim() ||
      (item.participant?.kind === "named" ? item.participant.id : "");
    if (!subthreadID && !ownerID) {
      return;
    }
    const requestVersion = invalidateOpenPanel({
      threadID: thread.id,
      subthreadID,
    });
    onOpenSubthreadPanel?.();
    setOpenSubthreadPanel({
      threadID: thread.id,
      subthread: undefined,
      loading: true,
    });
    void (async () => {
      try {
        const result = await window.wuu.openConversationSubthread(thread.id, {
          subthreadId: subthreadID,
          anchorItemId: subthreadID ? undefined : item.id,
          title: item.task?.name,
          threadOwnerParticipantId: ownerID || undefined,
        });
        if (openRequestVersionRef.current === requestVersion) {
          openPanelIdentityRef.current = {
            threadID: thread.id,
            subthreadID: result.subthread.id,
          };
          setOpenSubthreadPanel((current) =>
            current?.threadID === thread.id
              ? {
                  threadID: thread.id,
                  subthread: result.subthread,
                  loading: false,
                }
              : current,
          );
          setChatSubthreadsNonce((nonce) => nonce + 1);
        }
      } catch (error) {
        setOpenSubthreadPanel((current) =>
          openRequestVersionRef.current === requestVersion &&
          current?.threadID === thread.id
            ? {
                threadID: thread.id,
                loading: false,
                error: desktopApiErrorMessage(error, "无法打开 thread"),
              }
            : current,
        );
      }
    })();
  }

  function resolveOpenConversationSubthread(resolved: boolean): void {
    const current = openSubthreadPanel;
    if (!current?.subthread) {
      return;
    }
    const threadID = current.threadID;
    const subthreadID = current.subthread.id;
    const requestVersion = invalidateOpenPanel({ threadID, subthreadID });
    setOpenSubthreadPanel((panel) =>
      panel?.threadID === threadID && panel.subthread?.id === subthreadID
        ? { ...panel, loading: true, error: undefined }
        : panel,
    );
    void (async () => {
      try {
        const result = await window.wuu.resolveConversationSubthread(
          threadID,
          subthreadID,
          resolved,
        );
        if (openRequestVersionRef.current !== requestVersion) {
          return;
        }
        setOpenSubthreadPanel((panel) =>
          panel?.threadID === threadID && panel.subthread?.id === subthreadID
            ? {
                threadID,
                subthread: result.subthread,
                loading: false,
              }
            : panel,
        );
        setChatSubthreadsNonce((nonce) => nonce + 1);
      } catch (error) {
        setOpenSubthreadPanel((panel) =>
          openRequestVersionRef.current === requestVersion &&
          panel?.threadID === threadID &&
          panel.subthread?.id === subthreadID
            ? {
                ...panel,
                loading: false,
                error: desktopApiErrorMessage(error, "无法更新 thread"),
              }
            : panel,
        );
      }
    })();
  }

  function sendOpenConversationSubthreadMessage(): void {
    const current = openSubthreadPanel;
    if (!current?.subthread) {
      return;
    }
    const draft = subthreadComposerDraft;
    const trimmed = draft.prompt.trim();
    const files = inputFilesFromComposer(draft.files);
    if (!trimmed && draft.images.length === 0 && files.length === 0) {
      return;
    }
    const threadID = current.threadID;
    const subthreadID = current.subthread.id;
    const requestVersion = openRequestVersionRef.current;
    setSubthreadComposerDraft(emptyComposerDraft());
    void (async () => {
      try {
        const encodedImages = await awaitComposerImages(draft.images);
        const images = inputImagesFromComposer(encodedImages);
        const result = await window.wuu.postSubthreadMessage(
          threadID,
          subthreadID,
          trimmed,
          images,
          files,
        );
        setOpenSubthreadPanel((prev) =>
          prev &&
          prev.threadID === threadID &&
          prev.subthread?.id === subthreadID
            ? { ...prev, subthread: result.subthread, error: undefined }
            : prev,
        );
        setChatSubthreadsNonce((nonce) => nonce + 1);
      } catch (error) {
        const identity = openPanelIdentityRef.current;
        const stillCurrent =
          openRequestVersionRef.current === requestVersion &&
          identity?.threadID === threadID &&
          identity.subthreadID === subthreadID;
        if (stillCurrent) {
          setSubthreadComposerDraft((existing) =>
            existing.prompt.trim() === "" &&
            existing.images.length === 0 &&
            existing.files.length === 0
              ? draft
              : existing,
          );
        }
        setOpenSubthreadPanel((prev) =>
          stillCurrent &&
          prev?.threadID === threadID &&
          prev.subthread?.id === subthreadID
            ? { ...prev, error: desktopApiErrorMessage(error, "无法发送回复") }
            : prev,
        );
      }
    })();
  }

  function escalateOpenConversationSubthread(): void {
    const current = openSubthreadPanel;
    if (!current?.subthread) {
      return;
    }
    const threadID = current.threadID;
    const subthreadID = current.subthread.id;
    const title = current.subthread.title;
    const requestVersion = invalidateOpenPanel({ threadID, subthreadID });
    setOpenSubthreadPanel((panel) =>
      panel?.threadID === threadID && panel.subthread?.id === subthreadID
        ? { ...panel, loading: true, error: undefined }
        : panel,
    );
    void (async () => {
      try {
        const result = await window.wuu.escalateConversationSubthread(
          threadID,
          subthreadID,
          { title },
        );
        if (openRequestVersionRef.current !== requestVersion) {
          return;
        }
        setOpenSubthreadPanel((panel) =>
          panel?.threadID === threadID && panel.subthread?.id === subthreadID
            ? {
                threadID,
                subthread: result.subthread,
                loading: false,
              }
            : panel,
        );
        setChatSubthreadsNonce((nonce) => nonce + 1);
      } catch (error) {
        setOpenSubthreadPanel((panel) =>
          openRequestVersionRef.current === requestVersion &&
          panel?.threadID === threadID &&
          panel.subthread?.id === subthreadID
            ? {
                ...panel,
                loading: false,
                error: desktopApiErrorMessage(error, "无法升级为 Task"),
              }
            : panel,
        );
      }
    })();
  }

  function reactToOpenConversationSubthreadMessage(
    item: ThreadItem,
    reaction: string,
  ): void {
    const current = openSubthreadPanel;
    if (!current) {
      return;
    }
    const seq = item.seq;
    if (typeof seq !== "number" || seq < 0) {
      return;
    }
    void window.wuu
      .reactToMessage(current.threadID, seq, reaction)
      .catch((error) => {
        console.error("react to subthread message failed", error);
      });
  }

  return {
    openSubthreadPanel,
    setOpenSubthreadPanel: setOpenSubthreadPanelExternal,
    chatSubthreadsNonce,
    handleSubthreadUpdatedNotification,
    openConversationSubthreadByID,
    openConversationSubthread,
    resolveOpenConversationSubthread,
    sendOpenConversationSubthreadMessage,
    escalateOpenConversationSubthread,
    reactToOpenConversationSubthreadMessage,
  };
}
