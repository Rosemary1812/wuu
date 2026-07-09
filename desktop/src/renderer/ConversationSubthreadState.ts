import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type Dispatch,
  type SetStateAction,
} from "react";
import type {
  ParticipantSummary,
  SubthreadUpdatedNotification,
  Thread,
  ThreadItem,
} from "../shared/protocol";
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
  threads: ReadonlyArray<Thread | undefined>;
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
  subthreadLeadCandidates: ParticipantSummary[];
  handleSubthreadUpdatedNotification: (
    note: SubthreadUpdatedNotification,
    activeThreadID?: string,
  ) => void;
  openConversationSubthreadByID: (
    threadID: string,
    subthreadID: string,
  ) => void;
  openConversationSubthread: (thread: Thread, item: ThreadItem) => void;
  resolveOpenConversationSubthread: (resolved: boolean) => void;
  sendOpenConversationSubthreadMessage: () => void;
  escalateOpenConversationSubthread: (leadParticipantId: string) => void;
  bubbleOpenConversationSubthread: (summary: string) => void;
  reactToOpenConversationSubthreadMessage: (
    item: ThreadItem,
    reaction: string,
  ) => void;
};

export function useConversationSubthreadState({
  activeThreadID,
  threads,
  subthreadComposerDraft,
  setSubthreadComposerDraft,
  onOpenSubthreadPanel,
}: ConversationSubthreadStateOptions): ConversationSubthreadStateController {
  const [openSubthreadPanel, setOpenSubthreadPanel] = useState<
    OpenSubthreadPanel | undefined
  >();
  const [chatSubthreadsNonce, setChatSubthreadsNonce] = useState(0);

  const subthreadLeadCandidates = useMemo<ParticipantSummary[]>(() => {
    if (!openSubthreadPanel) {
      return [];
    }
    const parentID = openSubthreadPanel.threadID;
    const parent = threads.find((thread) => thread?.id === parentID);
    const named = (parent?.members ?? []).filter(
      (member): member is ParticipantSummary => member.kind === "named",
    );
    const subset = openSubthreadPanel.subthread?.participants;
    if (subset && subset.length > 0) {
      const inSubset = new Set(subset);
      const scoped = named.filter((member) => inSubset.has(member.id));
      if (scoped.length > 0) {
        return scoped;
      }
    }
    return named;
  }, [openSubthreadPanel, threads]);

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
      setOpenSubthreadPanel(undefined);
    }
  }, [activeThreadID, openSubthreadPanel]);

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
    setOpenSubthreadPanel({ threadID, subthread: undefined, loading: true });
    void (async () => {
      try {
        const result = await window.wuu.openConversationSubthread(threadID, {
          subthreadId: subthreadID,
        });
        setOpenSubthreadPanel({
          threadID,
          subthread: result.subthread,
          loading: false,
        });
      } catch (error) {
        setOpenSubthreadPanel({
          threadID,
          loading: false,
          error: desktopApiErrorMessage(error, "无法打开 thread"),
        });
      }
    })();
  }

  function openConversationSubthread(thread: Thread, item: ThreadItem): void {
    const subthreadID = item.task?.subthread_id;
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
          createdBy: item.participant?.id,
        });
        setOpenSubthreadPanel({
          threadID: thread.id,
          subthread: result.subthread,
          loading: false,
        });
        setChatSubthreadsNonce((nonce) => nonce + 1);
      } catch (error) {
        setOpenSubthreadPanel({
          threadID: thread.id,
          loading: false,
          error: desktopApiErrorMessage(error, "无法打开 thread"),
        });
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
    setOpenSubthreadPanel({ ...current, loading: true });
    void (async () => {
      try {
        const result = await window.wuu.resolveConversationSubthread(
          threadID,
          subthreadID,
          resolved,
        );
        setOpenSubthreadPanel({
          threadID,
          subthread: result.subthread,
          loading: false,
        });
        setChatSubthreadsNonce((nonce) => nonce + 1);
      } catch (error) {
        setOpenSubthreadPanel({
          ...current,
          loading: false,
          error: desktopApiErrorMessage(error, "无法更新 thread"),
        });
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
        setSubthreadComposerDraft((existing) =>
          existing.prompt.trim() === "" &&
          existing.images.length === 0 &&
          existing.files.length === 0
            ? draft
            : existing,
        );
        setOpenSubthreadPanel((prev) =>
          prev && prev.threadID === threadID
            ? { ...prev, error: desktopApiErrorMessage(error, "无法发送回复") }
            : prev,
        );
      }
    })();
  }

  function escalateOpenConversationSubthread(leadParticipantId: string): void {
    const current = openSubthreadPanel;
    if (!current?.subthread) {
      return;
    }
    const threadID = current.threadID;
    const subthreadID = current.subthread.id;
    const title = current.subthread.title;
    setOpenSubthreadPanel({ ...current, loading: true });
    void (async () => {
      try {
        const result = await window.wuu.escalateConversationSubthread(
          threadID,
          subthreadID,
          { title, leadParticipantId: leadParticipantId || undefined },
        );
        setOpenSubthreadPanel({
          threadID,
          subthread: result.subthread,
          loading: false,
        });
        setChatSubthreadsNonce((nonce) => nonce + 1);
      } catch (error) {
        setOpenSubthreadPanel({
          ...current,
          loading: false,
          error: desktopApiErrorMessage(error, "无法升级为 Task"),
        });
      }
    })();
  }

  function bubbleOpenConversationSubthread(summary: string): void {
    const current = openSubthreadPanel;
    if (!current?.subthread) {
      return;
    }
    const trimmed = summary.trim();
    if (!trimmed) {
      return;
    }
    const threadID = current.threadID;
    const subthreadID = current.subthread.id;
    setOpenSubthreadPanel({ ...current, loading: true });
    void (async () => {
      try {
        const result = await window.wuu.bubbleConversationSubthread(
          threadID,
          subthreadID,
          trimmed,
        );
        setOpenSubthreadPanel({
          threadID,
          subthread: result.subthread,
          loading: false,
        });
        setChatSubthreadsNonce((nonce) => nonce + 1);
      } catch (error) {
        setOpenSubthreadPanel({
          ...current,
          loading: false,
          error: desktopApiErrorMessage(error, "无法完成 Task"),
        });
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
    setOpenSubthreadPanel,
    chatSubthreadsNonce,
    subthreadLeadCandidates,
    handleSubthreadUpdatedNotification,
    openConversationSubthreadByID,
    openConversationSubthread,
    resolveOpenConversationSubthread,
    sendOpenConversationSubthreadMessage,
    escalateOpenConversationSubthread,
    bubbleOpenConversationSubthread,
    reactToOpenConversationSubthreadMessage,
  };
}
