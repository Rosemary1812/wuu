import { useRef } from "react";
import type { ComposerFile, ComposerImage } from "./ComposerMessages";
import { Composer, type CodexModelLoadState } from "./ComposerView";

const EMPTY_MODEL_STATE: CodexModelLoadState = {
  loading: false,
  error: "",
  models: [],
};

const noop = () => {};

export function ChannelComposer({
  draft,
  placeholder,
  disabled,
  sending,
  files,
  images,
  hideExpandButton = false,
  onChangeDraft,
  onPasteAttachmentFiles,
  onRemoveFile,
  onRemoveImage,
  onSend,
}: {
  draft: string;
  placeholder: string;
  disabled: boolean;
  sending: boolean;
  files: ComposerFile[];
  images: ComposerImage[];
  hideExpandButton?: boolean;
  onChangeDraft: (draft: string) => void;
  onPasteAttachmentFiles: (files: File[]) => void;
  onRemoveFile: (id: string) => void;
  onRemoveImage: (id: string) => void;
  onSend: () => void;
}): JSX.Element {
  const runtimeRef = useRef<HTMLDivElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const accessMenuRef = useRef<HTMLDivElement>(null);

  return (
    <div className="channel-composer">
      <Composer
        variant="dock"
        hideRuntimeControls
        hidePlusButton
        hidePermissionControl
        hideExpandButton={hideExpandButton}
        placeholder={placeholder}
        maxLength={4000}
        prompt={draft}
        setPrompt={onChangeDraft}
        files={files}
        images={images}
        queuedMessages={[]}
        guideMessages={[]}
        running={false}
        sendDisabled={sending}
        runtimeControlsDisabled
        tokensPerSecond={0}
        status=""
        statusLiveProgress={false}
        readOnly={disabled}
        projects={[]}
        codexModels={EMPTY_MODEL_STATE}
        codexRuntimeMenu={null}
        codexRuntimeRef={runtimeRef}
        menuOpen={false}
        accessMenuOpen={false}
        branchMenuOpen={false}
        menuRef={menuRef}
        accessMenuRef={accessMenuRef}
        projectFilter=""
        setProjectFilter={noop}
        onToggleMenu={noop}
        onToggleAccessMenu={noop}
        onToggleBranchMenu={noop}
        onToggleCodexRuntimeMenu={noop}
        onSelectRuntimeModel={noop}
        onSelectRuntimeEffort={noop}
        onSelectPermissionMode={noop}
        onOpenSettings={noop}
        onOpenMemorySettings={noop}
        onOpenSkillsCatalog={noop}
        onSelectProject={noop}
        onSelectNoProject={noop}
        onSelectGitBranch={noop}
        onCreateProject={noop}
        onOpenProject={noop}
        onStartNewThread={noop}
        onOpenWorkspaceTool={noop}
        onOpenInstructions={noop}
        onPasteAttachmentFiles={onPasteAttachmentFiles}
        onRemoveFile={onRemoveFile}
        onRemoveImage={onRemoveImage}
        onRemoveQueuedMessage={noop}
        onRemoveGuideMessage={noop}
        onGuideQueuedMessage={noop}
        onEditQueuedMessage={noop}
        onEditGuideMessage={noop}
        onSend={onSend}
        onInterrupt={noop}
      />
    </div>
  );
}
