import {
  ChevronDown,
  FileText,
  Folder,
  FolderOpen,
  FolderX,
  GitBranch,
  Laptop,
  MessageSquarePlus,
  Paperclip,
  Plus,
  Search,
  Send,
  Settings,
  ShieldCheck,
  Slash,
  Square,
  Terminal,
  Wrench,
  Zap
} from "lucide-react";
import {
  type KeyboardEvent as ReactKeyboardEvent,
  type Ref,
  type RefObject,
  useEffect,
  useMemo,
  useRef,
  useState
} from "react";
import type {
  DesktopProject,
  GitStatusResult,
  InitializeResult,
  RuntimeContext
} from "../shared/protocol";
import {
  buildComposerSlashCommands,
  composerSlashPrompt,
  filterComposerSlashCommands,
  firstEnabledSlashCommandIndex,
  isComposerTextComposing,
  nextEnabledSlashCommandIndex,
  parseComposerSlashDraft,
  runtimeFastModelTarget,
  type ComposerSlashCommand,
  type ComposerSlashDraft
} from "./ComposerSlashCommands";
import {
  clipboardAttachmentFiles,
  type ComposerFile,
  type ComposerImage,
  type QueuedComposerMessage
} from "./ComposerMessages";
import { FloatingMenuPortal } from "./ComposerFloatingMenu";
import { ComposerAttachmentStrip, ComposerQueueStrip } from "./ComposerInputSections";
import {
  AccessMenu,
  BranchMenu,
  ModeMenu,
  ProjectPickerMenu,
  RuntimePicker,
  permissionModeFromSummary,
  permissionModeHasAdvancedOverrides,
  permissionModeOption
} from "./ComposerRuntimeMenus";
import { useComposerQueryHistory } from "./ComposerQueryHistory";
import type {
  CodexModelLoadState,
  CodexRuntimeMenu,
  ComposerVariant,
  FloatingMenuPlacement,
  PermissionMode
} from "./ComposerTypes";
import type { WorkspacePanelView } from "./WorkspacePanels";

export type {
  CodexModelLoadState,
  CodexRuntimeMenu,
  ComposerVariant,
  FloatingMenuOwner,
  FloatingMenuPlacement,
  FloatingMenuAlign,
  PermissionMode
} from "./ComposerTypes";
export { FloatingMenuPortal, isInsideFloatingMenu } from "./ComposerFloatingMenu";
export { ComposerAttachmentStrip, SplitPaneComposer } from "./ComposerInputSections";
export { permissionModeFromSummary, permissionModeHasAdvancedOverrides } from "./ComposerRuntimeMenus";

export function Composer({
  variant = "dock",
  containerRef,
  prompt,
  setPrompt,
  files,
  images,
  queuedMessages,
  guideMessages,
  running,
  status,
  readOnly,
  initialized,
  gitStatus,
  projects,
  activeContext,
  activeProject,
  codexModels,
  codexRuntimeMenu,
  codexRuntimeRef,
  menuOpen,
  accessMenuOpen,
  modeMenuOpen,
  branchMenuOpen,
  menuRef,
  accessMenuRef,
  projectFilter,
  setProjectFilter,
  onToggleMenu,
  onToggleAccessMenu,
  onToggleCodexRuntimeMenu,
  onSelectRuntimeModel,
  onSelectRuntimeEffort,
  onSelectPermissionMode,
  onToggleModeMenu,
  onToggleBranchMenu,
  onOpenSettings,
  onSelectProject,
  onSelectNoProject,
  onSelectGitBranch,
  onCreateProject,
  onOpenProject,
  onStartNewThread,
  onOpenWorkspaceTool,
  onPasteAttachmentFiles,
  onRemoveFile,
  onRemoveImage,
  onRemoveQueuedMessage,
  onRemoveGuideMessage,
  onGuideQueuedMessage,
  onEditQueuedMessage,
  onEditGuideMessage,
  onSend,
  onInterrupt,
  queryHistorySessionID,
  queryHistory = []
}: {
  variant?: ComposerVariant;
  containerRef?: Ref<HTMLElement>;
  prompt: string;
  setPrompt: (value: string) => void;
  files: ComposerFile[];
  images: ComposerImage[];
  queuedMessages: QueuedComposerMessage[];
  guideMessages: QueuedComposerMessage[];
  running: boolean;
  status: string;
  readOnly: boolean;
  initialized?: InitializeResult;
  gitStatus?: GitStatusResult;
  projects: DesktopProject[];
  activeContext?: RuntimeContext;
  activeProject?: DesktopProject;
  codexModels: CodexModelLoadState;
  codexRuntimeMenu: CodexRuntimeMenu;
  codexRuntimeRef: RefObject<HTMLDivElement | null>;
  menuOpen: boolean;
  accessMenuOpen: boolean;
  modeMenuOpen: boolean;
  branchMenuOpen: boolean;
  menuRef: RefObject<HTMLDivElement | null>;
  accessMenuRef: RefObject<HTMLDivElement | null>;
  projectFilter: string;
  setProjectFilter: (value: string) => void;
  onToggleMenu: () => void;
  onToggleAccessMenu: () => void;
  onToggleCodexRuntimeMenu: (menu: Exclude<CodexRuntimeMenu, null>) => void;
  onSelectRuntimeModel: (provider: string, model: string, variant?: string) => void;
  onSelectRuntimeEffort: (variant: string) => void;
  onSelectPermissionMode: (mode: PermissionMode) => void;
  onToggleModeMenu: () => void;
  onToggleBranchMenu: () => void;
  onOpenSettings: () => void;
  onSelectProject: (id: string) => void;
  onSelectNoProject: () => void;
  onSelectGitBranch: (branch: string) => void;
  onCreateProject: () => void;
  onOpenProject: () => void;
  onStartNewThread: () => void;
  onOpenWorkspaceTool: (view: WorkspacePanelView) => void;
  onPasteAttachmentFiles: (files: File[]) => void;
  onRemoveFile: (id: string) => void;
  onRemoveImage: (id: string) => void;
  onRemoveQueuedMessage: (id: string) => void;
  onRemoveGuideMessage: (id: string) => void;
  onGuideQueuedMessage: (id: string) => void;
  onEditQueuedMessage: (id: string) => void;
  onEditGuideMessage: (id: string) => void;
  onSend: () => void;
  onInterrupt: () => void;
  queryHistorySessionID?: string;
  queryHistory?: string[];
}): JSX.Element {
  const contextLabel = activeContext?.kind === "project" ? activeProject?.name ?? "项目" : "不使用项目";
  const statusText = status === "ready" ? "" : status;
  const className = `composer-wrap ${variant === "hero" ? "hero-composer-wrap" : "dock-composer-wrap"}`;
  const hasAttachments = images.length > 0 || files.length > 0;
  const hasDraft = prompt.trim().length > 0 || hasAttachments;
  const menuPlacement: FloatingMenuPlacement = variant === "hero" ? "below" : "above";
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const attachmentInputRef = useRef<HTMLInputElement>(null);
  const [selectedSlashIndex, setSelectedSlashIndex] = useState(0);
  const [slashDismissedValue, setSlashDismissedValue] = useState("");
  const slashDraft = parseComposerSlashDraft(prompt);
  const slashQuery = slashDraft?.query ?? "";
  const slashCommands = useMemo(
    () => buildComposerSlashCommands({ activeContext, initialized, running }),
    [activeContext, initialized, running]
  );
  const fastModelTarget = useMemo(() => runtimeFastModelTarget(initialized), [initialized]);
  const permissionModeHasOverrides = permissionModeHasAdvancedOverrides(initialized?.tool_policy);
  const permissionMode = permissionModeFromSummary(initialized?.permissions, initialized?.tool_policy);
  const permissionOption = permissionModeOption(permissionMode);
  const permissionChipLabel = permissionModeHasOverrides ? "自定义权限" : permissionOption.chipLabel;
  const visibleSlashCommands = useMemo(
    () => filterComposerSlashCommands(slashCommands, slashQuery),
    [slashCommands, slashQuery]
  );
  const slashMenuOpen = Boolean(!readOnly && slashDraft && slashDismissedValue !== prompt);
  const selectedSlashCommand = slashMenuOpen ? visibleSlashCommands[selectedSlashIndex] : undefined;
  const slashMenuID = `composer-slash-commands-${variant}`;
  const { resetQueryHistoryNavigation, handleQueryHistoryKeyDown } = useComposerQueryHistory({
    disabled: readOnly || hasAttachments,
    prompt,
    queryHistory,
    queryHistorySessionID,
    setPrompt,
    textareaRef
  });

  useEffect(() => {
    setSelectedSlashIndex(firstEnabledSlashCommandIndex(visibleSlashCommands));
  }, [visibleSlashCommands]);

  function focusComposerSoon(): void {
    window.requestAnimationFrame(() => textareaRef.current?.focus());
  }

  function revealSlashCommands(): void {
    if (readOnly) {
      return;
    }
    setSlashDismissedValue("");
    if (prompt.trim().length === 0) {
      setPrompt("/");
    }
    focusComposerSoon();
  }

  function applySlashCommand(command: ComposerSlashCommand | undefined, draft: ComposerSlashDraft | undefined): void {
    if (!command || command.disabledReason) {
      return;
    }
    setSlashDismissedValue("");
    if (command.kind === "prompt") {
      setPrompt(composerSlashPrompt(command, draft?.args ?? ""));
      focusComposerSoon();
      return;
    }
    setPrompt("");
    switch (command.action) {
      case "new-thread":
        onStartNewThread();
        break;
      case "open-review":
        onOpenWorkspaceTool("review");
        break;
      case "open-files":
        onOpenWorkspaceTool("files");
        break;
      case "open-terminal":
        onOpenWorkspaceTool("terminal");
        break;
      case "open-project":
        onOpenProject();
        break;
      case "no-project":
        onSelectNoProject();
        break;
      case "model":
        onToggleCodexRuntimeMenu("model");
        break;
      case "fast":
        if (fastModelTarget && !fastModelTarget.current) {
          onSelectRuntimeModel(fastModelTarget.provider, fastModelTarget.model);
        }
        break;
      case "effort":
        onToggleCodexRuntimeMenu("main");
        break;
      case "settings":
        onOpenSettings();
        break;
    }
    focusComposerSoon();
  }

  function handleComposerKeyDown(event: ReactKeyboardEvent<HTMLTextAreaElement>): void {
    if (readOnly) {
      return;
    }
    if (isComposerTextComposing(event)) {
      return;
    }
    if (slashMenuOpen) {
      if (event.key === "ArrowDown" && visibleSlashCommands.length > 0) {
        event.preventDefault();
        setSelectedSlashIndex((current) => nextEnabledSlashCommandIndex(visibleSlashCommands, current, 1));
        return;
      }
      if (event.key === "ArrowUp" && visibleSlashCommands.length > 0) {
        event.preventDefault();
        setSelectedSlashIndex((current) => nextEnabledSlashCommandIndex(visibleSlashCommands, current, -1));
        return;
      }
      if ((event.key === "Enter" || event.key === "Tab") && visibleSlashCommands.length > 0) {
        event.preventDefault();
        const fallbackCommand = visibleSlashCommands[firstEnabledSlashCommandIndex(visibleSlashCommands)];
        applySlashCommand(selectedSlashCommand?.disabledReason ? fallbackCommand : (selectedSlashCommand ?? fallbackCommand), slashDraft);
        return;
      }
      if (event.key === "Escape") {
        event.preventDefault();
        setSlashDismissedValue(prompt);
        return;
      }
    }
    if (handleQueryHistoryKeyDown(event)) {
      return;
    }
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      resetQueryHistoryNavigation();
      onSend();
    }
  }

  const content = (
    <div className="composer-stack">
      <div className="composer-shell">
        <ComposerQueueStrip
          guideMessages={guideMessages}
          queuedMessages={queuedMessages}
          onRemoveGuideMessage={onRemoveGuideMessage}
          onRemoveQueuedMessage={onRemoveQueuedMessage}
          onGuideQueuedMessage={onGuideQueuedMessage}
          onEditGuideMessage={onEditGuideMessage}
          onEditQueuedMessage={onEditQueuedMessage}
        />
        {slashMenuOpen ? (
          <div className="slash-command-menu" id={slashMenuID} role="listbox" aria-label="斜杠命令">
            {visibleSlashCommands.length > 0 ? (
              <div className="slash-command-list">
                {visibleSlashCommands.map((command, index) => {
                  const selected = index === selectedSlashIndex;
                  const optionID = `${slashMenuID}-${command.id}`;
                  return (
                    <button
                      className={`slash-command-item${selected ? " selected" : ""}`}
                      id={optionID}
                      key={command.id}
                      role="option"
                      type="button"
                      aria-selected={selected}
                      disabled={Boolean(command.disabledReason)}
                      onMouseEnter={() => {
                        if (!command.disabledReason) {
                          setSelectedSlashIndex(index);
                        }
                      }}
                      onMouseDown={(event) => event.preventDefault()}
                      onClick={() => applySlashCommand(command, slashDraft)}
                    >
                      <span className="slash-command-icon" aria-hidden="true">
                        <SlashCommandIcon command={command} />
                      </span>
                      <span className="slash-command-copy">
                        <strong>{command.title}</strong>
                        <small>{command.disabledReason ?? command.description}</small>
                      </span>
                      <span className="slash-command-name">/{command.name}</span>
                    </button>
                  );
                })}
              </div>
            ) : (
              <div className="slash-command-empty">没有匹配 “/{slashQuery}” 的命令</div>
            )}
          </div>
        ) : null}
        <div className="composer">
          <ComposerAttachmentStrip files={files} images={images} onRemoveFile={onRemoveFile} onRemoveImage={onRemoveImage} />
          <input
            ref={attachmentInputRef}
            className="composer-file-input"
            type="file"
            accept="image/*,application/pdf"
            multiple
            tabIndex={-1}
            onChange={(event) => {
              const selected = Array.from(event.currentTarget.files ?? []);
              event.currentTarget.value = "";
              if (selected.length > 0) {
                onPasteAttachmentFiles(selected);
              }
            }}
          />
          <textarea
            ref={textareaRef}
            value={prompt}
            placeholder={readOnly ? "子任务会话只读" : hasAttachments ? "添加描述" : "尽管问，或输入 / 选择命令"}
            disabled={readOnly}
            aria-readonly={readOnly}
            aria-controls={slashMenuOpen ? slashMenuID : undefined}
            aria-activedescendant={selectedSlashCommand ? `${slashMenuID}-${selectedSlashCommand.id}` : undefined}
            aria-expanded={slashMenuOpen || undefined}
            onChange={(event) => {
              resetQueryHistoryNavigation();
              setSlashDismissedValue("");
              setPrompt(event.target.value);
            }}
            onPaste={(event) => {
              if (readOnly) {
                return;
              }
              const pasted = clipboardAttachmentFiles(event);
              if (pasted.length === 0) {
                return;
              }
              event.preventDefault();
              onPasteAttachmentFiles(pasted);
            }}
            onBlur={() => {
              if (slashMenuOpen) {
                setSlashDismissedValue(prompt);
              }
            }}
            onKeyDown={handleComposerKeyDown}
          />
          <div className="composer-bar">
            <button className="composer-tool-button" type="button" aria-label="打开项目" onClick={onOpenProject}>
              <Plus size={20} />
            </button>
            <button
              className="composer-tool-button"
              type="button"
              aria-label="添加附件"
              title="添加附件"
              disabled={readOnly}
              onClick={() => attachmentInputRef.current?.click()}
            >
              <Paperclip aria-hidden="true" />
            </button>
            <button
              className="composer-tool-button composer-slash-button"
              type="button"
              aria-label="打开斜杠命令"
              title="输入 / 打开命令"
              disabled={readOnly}
              onClick={revealSlashCommands}
            >
              <Slash aria-hidden="true" />
            </button>
            <div className="permission-menu-anchor" ref={accessMenuRef}>
              <button
                className="permission-chip"
                type="button"
                aria-haspopup="menu"
                aria-expanded={accessMenuOpen}
                aria-label={`权限模式：${permissionChipLabel}`}
                disabled={!initialized || readOnly || running}
                onClick={onToggleAccessMenu}
              >
                <ShieldCheck aria-hidden="true" />
                <span>{permissionChipLabel}</span>
                <ChevronDown aria-hidden="true" />
              </button>
              {accessMenuOpen ? (
                <FloatingMenuPortal
                  anchorRef={accessMenuRef}
                  owner="composer-access"
                  placement="above"
                  align="left"
                  offset={6}
                  width={176}
                >
                  <AccessMenu
                    permissions={initialized?.permissions}
                    policy={initialized?.tool_policy}
                    disabled={!initialized || readOnly || running}
                    onSelect={onSelectPermissionMode}
                  />
                </FloatingMenuPortal>
              ) : null}
            </div>
            <div className="composer-spacer" />
            {initialized ? (
              <RuntimePicker
                variant={variant}
                initialized={initialized}
                state={codexModels}
                openMenu={codexRuntimeMenu}
                anchorRef={codexRuntimeRef}
                running={running}
                onToggleMenu={onToggleCodexRuntimeMenu}
                onSelectModel={onSelectRuntimeModel}
                onSelectEffort={onSelectRuntimeEffort}
              />
            ) : (
              <>
                <button className="provider-pill" type="button" onClick={onOpenSettings}>
                  provider
                </button>
                <button className="model-label" type="button" onClick={onOpenSettings}>
                  model
                </button>
              </>
            )}
            {statusText ? <span className="status-label">{statusText}</span> : null}
            <button
              className={`composer-action-button ${running ? "composer-stop-button" : "composer-send-button"}`}
              type="button"
              onClick={running ? onInterrupt : onSend}
              aria-label={running ? "停止" : "发送"}
              title={running ? "停止" : "发送"}
              disabled={!running && (readOnly || !hasDraft)}
            >
              {running ? <Square aria-hidden="true" /> : <Send aria-hidden="true" />}
            </button>
          </div>
        </div>
        <div className="composer-context-bar" ref={menuRef}>
          <button className="context-project-button" onClick={onToggleMenu} aria-haspopup="menu" aria-expanded={menuOpen}>
            {activeContext?.kind === "project" ? <Folder aria-hidden="true" /> : <FolderX aria-hidden="true" />}
            <span>{contextLabel}</span>
            <ChevronDown aria-hidden="true" />
          </button>
          <button
            className="context-mode-chip"
            type="button"
            aria-haspopup="menu"
            aria-expanded={modeMenuOpen}
            onClick={onToggleModeMenu}
          >
            <Laptop aria-hidden="true" />
            <span>本地模式</span>
            <ChevronDown aria-hidden="true" />
          </button>
          {gitStatus?.is_repo && gitStatus.branch ? (
            <button
              className="context-branch-chip"
              type="button"
              aria-haspopup="menu"
              aria-expanded={branchMenuOpen}
              onClick={onToggleBranchMenu}
            >
              <GitBranch aria-hidden="true" />
              <span>{gitStatus.branch}</span>
              {gitStatus.dirty_count > 0 ? <small>未提交：{gitStatus.dirty_count} 个文件</small> : null}
              <ChevronDown aria-hidden="true" />
            </button>
          ) : null}
          {modeMenuOpen ? (
            <FloatingMenuPortal
              anchorRef={menuRef}
              owner="composer-runtime"
              placement={menuPlacement}
              align="left"
              crossAxisOffset={170}
              width={224}
            >
              <ModeMenu
                activeContext={activeContext}
                onSelectNoProject={onSelectNoProject}
                onOpenProject={onOpenProject}
              />
            </FloatingMenuPortal>
          ) : null}
          {branchMenuOpen && gitStatus?.is_repo ? (
            <FloatingMenuPortal
              anchorRef={menuRef}
              owner="composer-runtime"
              placement={menuPlacement}
              align="left"
              crossAxisOffset={304}
              width={300}
            >
              <BranchMenu gitStatus={gitStatus} onSelectBranch={onSelectGitBranch} />
            </FloatingMenuPortal>
          ) : null}
          {menuOpen ? (
            <FloatingMenuPortal
              anchorRef={menuRef}
              owner="composer-runtime"
              placement={menuPlacement}
              align="left"
              crossAxisOffset={12}
              width={300}
            >
              <ProjectPickerMenu
                projects={projects}
                activeContext={activeContext}
                query={projectFilter}
                setQuery={setProjectFilter}
                onSelectProject={onSelectProject}
                onSelectNoProject={onSelectNoProject}
                onCreateProject={onCreateProject}
                onOpenProject={onOpenProject}
              />
            </FloatingMenuPortal>
          ) : null}
        </div>
      </div>
    </div>
  );
  return variant === "hero" ? (
    <div className={className}>{content}</div>
  ) : (
    <footer className={className} ref={containerRef}>
      {content}
    </footer>
  );
}

function SlashCommandIcon({ command }: { command: ComposerSlashCommand }): JSX.Element {
  switch (command.action ?? command.id) {
    case "open-review":
    case "review":
      return <Search size={16} />;
    case "new-thread":
      return <MessageSquarePlus size={16} />;
    case "open-terminal":
      return <Terminal size={16} />;
    case "open-files":
      return <FileText size={16} />;
    case "open-project":
      return <FolderOpen size={16} />;
    case "no-project":
      return <FolderX size={16} />;
    case "fast":
      return <Zap size={16} />;
    case "model":
    case "effort":
      return <Laptop size={16} />;
    case "settings":
      return <Settings size={16} />;
    default:
      return <Wrench size={16} />;
  }
}
