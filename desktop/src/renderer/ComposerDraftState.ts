import {
  useState,
  type Dispatch,
  type SetStateAction,
} from "react";
import {
  cloneComposerDraft,
  emptyComposerDraft,
  initialSplitComposerDrafts,
  type ComposerDraftState,
  type ConversationPaneID,
} from "./AppState";
import {
  composerFileFromFile,
  composerImagePlaceholder,
  isComposerImageFile,
  isPDFFile,
  revokeComposerImagePreview,
  type ComposerFile,
  type ComposerImage,
} from "./ComposerMessages";
import { localizedText, translateCurrent } from "./i18n";

type ComposerDraftStatusSetter = (status: string) => void;

export type ComposerDraftStateController = {
  prompt: string;
  setPrompt: Dispatch<SetStateAction<string>>;
  composerImages: ComposerImage[];
  setComposerImages: Dispatch<SetStateAction<ComposerImage[]>>;
  composerFiles: ComposerFile[];
  setComposerFiles: Dispatch<SetStateAction<ComposerFile[]>>;
  splitComposerDrafts: Record<ConversationPaneID, ComposerDraftState>;
  setSplitComposerDrafts: Dispatch<
    SetStateAction<Record<ConversationPaneID, ComposerDraftState>>
  >;
  subthreadComposerDraft: ComposerDraftState;
  setSubthreadComposerDraft: Dispatch<SetStateAction<ComposerDraftState>>;
  attachComposerAttachmentFiles: (files: File[]) => Promise<void>;
  removeComposerImage: (id: string) => void;
  removeComposerFile: (id: string) => void;
  attachSubthreadComposerAttachmentFiles: (files: File[]) => Promise<void>;
  removeSubthreadComposerImage: (id: string) => void;
  removeSubthreadComposerFile: (id: string) => void;
  setSplitComposerPrompt: (pane: ConversationPaneID, value: string) => void;
  attachSplitComposerAttachmentFiles: (
    pane: ConversationPaneID,
    files: File[],
  ) => Promise<void>;
  removeSplitComposerImage: (pane: ConversationPaneID, id: string) => void;
  removeSplitComposerFile: (pane: ConversationPaneID, id: string) => void;
  moveSplitDraftToGlobalComposer: (pane: ConversationPaneID) => void;
  currentPrimaryComposerDraft: () => ComposerDraftState;
  restorePrimaryComposerDraft: (draft: ComposerDraftState) => void;
};

type ComposerAttachmentTargets = {
  onImagePlaceholder: (placeholder: ComposerImage) => void;
  onImageEncoded: (encoded: ComposerImage) => void;
  onFile: (file: ComposerFile) => void;
};

export async function buildComposerAttachments(
  files: File[],
  onImagePlaceholder: (placeholder: ComposerImage) => void,
  onImageEncoded: (encoded: ComposerImage) => void,
  onFile: (file: ComposerFile) => void,
): Promise<void> {
  const imageFiles = files.filter(isComposerImageFile);
  const pdfFiles = files.filter(isPDFFile);
  await Promise.all([
    ...imageFiles.map(async (file) => {
      const placeholder = composerImagePlaceholder(file);
      onImagePlaceholder(placeholder);
      const encoded = await placeholder.encodePromise;
      if (encoded) {
        onImageEncoded(encoded);
      }
    }),
    ...pdfFiles.map(async (file) => {
      const pdf = await composerFileFromFile(file);
      onFile(pdf);
    }),
  ]);
}

async function attachComposerAttachmentFilesToDraft(
  files: File[],
  setStatus: ComposerDraftStatusSetter,
  targets: ComposerAttachmentTargets,
): Promise<void> {
  if (files.length === 0) {
    return;
  }
  const imageFiles = files.filter(isComposerImageFile);
  const pdfFiles = files.filter(isPDFFile);
  if (imageFiles.length === 0 && pdfFiles.length === 0) {
    setStatus(localizedText("composer.attachment.imagesAndPdfOnly"));
    return;
  }
  try {
    await buildComposerAttachments(
      files,
      targets.onImagePlaceholder,
      targets.onImageEncoded,
      targets.onFile,
    );
  } catch (error) {
    setStatus(error instanceof Error ? error.message : translateCurrent("composer.attachment.addFailed"));
  }
}

export function useComposerDraftState({
  setStatus,
}: {
  setStatus: ComposerDraftStatusSetter;
}): ComposerDraftStateController {
  const [prompt, setPrompt] = useState("");
  const [composerImages, setComposerImages] = useState<ComposerImage[]>([]);
  const [composerFiles, setComposerFiles] = useState<ComposerFile[]>([]);
  const [splitComposerDrafts, setSplitComposerDrafts] = useState<
    Record<ConversationPaneID, ComposerDraftState>
  >(initialSplitComposerDrafts);
  const [subthreadComposerDraft, setSubthreadComposerDraft] =
    useState<ComposerDraftState>(emptyComposerDraft);

  async function attachComposerAttachmentFiles(files: File[]): Promise<void> {
    await attachComposerAttachmentFilesToDraft(files, setStatus, {
      onImagePlaceholder: (placeholder) =>
        setComposerImages((current) => [...current, placeholder]),
      onImageEncoded: (encoded) =>
        setComposerImages((current) =>
          current.map((existing) =>
            existing.id === encoded.id ? encoded : existing,
          ),
        ),
      onFile: (file) => setComposerFiles((current) => [...current, file]),
    });
  }

  function removeComposerImage(id: string): void {
    setComposerImages((current) => {
      const removed = current.find((image) => image.id === id);
      revokeComposerImagePreview(removed);
      return current.filter((image) => image.id !== id);
    });
  }

  function removeComposerFile(id: string): void {
    setComposerFiles((current) => current.filter((file) => file.id !== id));
  }

  async function attachSubthreadComposerAttachmentFiles(
    files: File[],
  ): Promise<void> {
    await attachComposerAttachmentFilesToDraft(files, setStatus, {
      onImagePlaceholder: (placeholder) =>
        setSubthreadComposerDraft((draft) => ({
          ...draft,
          images: [...draft.images, placeholder],
        })),
      onImageEncoded: (encoded) =>
        setSubthreadComposerDraft((draft) => ({
          ...draft,
          images: draft.images.map((existing) =>
            existing.id === encoded.id ? encoded : existing,
          ),
        })),
      onFile: (file) =>
        setSubthreadComposerDraft((draft) => ({
          ...draft,
          files: [...draft.files, file],
        })),
    });
  }

  function removeSubthreadComposerImage(id: string): void {
    setSubthreadComposerDraft((draft) => {
      const removed = draft.images.find((image) => image.id === id);
      revokeComposerImagePreview(removed);
      return { ...draft, images: draft.images.filter((image) => image.id !== id) };
    });
  }

  function removeSubthreadComposerFile(id: string): void {
    setSubthreadComposerDraft((draft) => ({
      ...draft,
      files: draft.files.filter((file) => file.id !== id),
    }));
  }

  function updateSplitComposerDraft(
    pane: ConversationPaneID,
    update: (draft: ComposerDraftState) => ComposerDraftState,
  ): void {
    setSplitComposerDrafts((current) => {
      const draft = current[pane] ?? emptyComposerDraft();
      return {
        ...current,
        [pane]: update(draft),
      };
    });
  }

  function setSplitComposerPrompt(
    pane: ConversationPaneID,
    value: string,
  ): void {
    updateSplitComposerDraft(pane, (draft) => ({ ...draft, prompt: value }));
  }

  async function attachSplitComposerAttachmentFiles(
    pane: ConversationPaneID,
    files: File[],
  ): Promise<void> {
    await attachComposerAttachmentFilesToDraft(files, setStatus, {
      onImagePlaceholder: (placeholder) =>
        updateSplitComposerDraft(pane, (draft) => ({
          ...draft,
          images: [...draft.images, placeholder],
        })),
      onImageEncoded: (encoded) =>
        updateSplitComposerDraft(pane, (draft) => ({
          ...draft,
          images: draft.images.map((existing) =>
            existing.id === encoded.id ? encoded : existing,
          ),
        })),
      onFile: (file) =>
        updateSplitComposerDraft(pane, (draft) => ({
          ...draft,
          files: [...draft.files, file],
        })),
    });
  }

  function removeSplitComposerImage(
    pane: ConversationPaneID,
    id: string,
  ): void {
    updateSplitComposerDraft(pane, (draft) => {
      const removed = draft.images.find((image) => image.id === id);
      revokeComposerImagePreview(removed);
      return {
        ...draft,
        images: draft.images.filter((image) => image.id !== id),
      };
    });
  }

  function removeSplitComposerFile(pane: ConversationPaneID, id: string): void {
    updateSplitComposerDraft(pane, (draft) => ({
      ...draft,
      files: draft.files.filter((file) => file.id !== id),
    }));
  }

  function moveSplitDraftToGlobalComposer(pane: ConversationPaneID): void {
    const draft = splitComposerDrafts[pane] ?? emptyComposerDraft();
    restorePrimaryComposerDraft(draft);
    setSplitComposerDrafts(initialSplitComposerDrafts());
  }

  function currentPrimaryComposerDraft(): ComposerDraftState {
    return cloneComposerDraft({
      prompt,
      images: composerImages,
      files: composerFiles,
    });
  }

  function restorePrimaryComposerDraft(draft: ComposerDraftState): void {
    const nextDraft = cloneComposerDraft(draft);
    setPrompt(nextDraft.prompt);
    setComposerImages(nextDraft.images);
    setComposerFiles(nextDraft.files);
  }

  return {
    prompt,
    setPrompt,
    composerImages,
    setComposerImages,
    composerFiles,
    setComposerFiles,
    splitComposerDrafts,
    setSplitComposerDrafts,
    subthreadComposerDraft,
    setSubthreadComposerDraft,
    attachComposerAttachmentFiles,
    removeComposerImage,
    removeComposerFile,
    attachSubthreadComposerAttachmentFiles,
    removeSubthreadComposerImage,
    removeSubthreadComposerFile,
    setSplitComposerPrompt,
    attachSplitComposerAttachmentFiles,
    removeSplitComposerImage,
    removeSplitComposerFile,
    moveSplitDraftToGlobalComposer,
    currentPrimaryComposerDraft,
    restorePrimaryComposerDraft,
  };
}
