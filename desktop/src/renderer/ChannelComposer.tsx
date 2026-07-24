import { useRef } from "react";
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
  onChangeDraft,
  onSend,
}: {
  draft: string;
  placeholder: string;
  disabled: boolean;
  sending: boolean;
  onChangeDraft: (draft: string) => void;
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
        textOnly
        placeholder={placeholder}
        maxLength={4000}
        prompt={draft}
        setPrompt={onChangeDraft}
        files={[]}
        images={[]}
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
        onPasteAttachmentFiles={noop}
        onRemoveFile={noop}
        onRemoveImage={noop}
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
