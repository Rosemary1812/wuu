import {
  Check,
  ChevronDown,
  ChevronRight,
  CornerDownRight,
  FileText,
  Folder,
  FolderOpen,
  FolderPlus,
  FolderX,
  GitBranch,
  Laptop,
  MessageSquarePlus,
  MoreHorizontal,
  Plus,
  Search,
  Send,
  Settings,
  ShieldCheck,
  Square,
  Terminal,
  Trash2,
  Wrench,
  X,
  Zap
} from "lucide-react";
import {
  type CSSProperties,
  type KeyboardEvent as ReactKeyboardEvent,
  type ReactNode,
  type Ref,
  type RefObject,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState
} from "react";
import { createPortal } from "react-dom";
import { OverlayScrollbarsComponent } from "overlayscrollbars-react";
import type {
  CodexModelSummary,
  DesktopProject,
  GitStatusResult,
  InitializeResult,
  ProviderModelSummary,
  ProviderSummary,
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
  clipboardImageFiles,
  imageSource,
  queuedMessagePreview,
  type ComposerImage,
  type QueuedComposerMessage
} from "./ComposerMessages";
import {
  codexEffortOptions,
  displayCodexModelName,
  isCodexProvider,
  providerModelDisplayName,
  providerModelVariantOptions,
  shortCodexModelLabel,
  variantLabel
} from "./RuntimeHelpers";
import { OVERLAY_SCROLLBAR_OPTIONS } from "./ScrollbarOptions";
import type { WorkspacePanelView } from "./WorkspacePanels";

export type CodexModelLoadState = {
  provider?: string;
  loading: boolean;
  error: string;
  models: CodexModelSummary[];
};

export type CodexRuntimeMenu = "main" | "model" | null;
export type ComposerVariant = "dock" | "hero";
export type FloatingMenuOwner = "composer-runtime" | "composer-access" | "codex-runtime";
type FloatingMenuPlacement = "above" | "below";
type FloatingMenuAlign = "left" | "right";

export function isInsideFloatingMenu(target: Node, owner: FloatingMenuOwner): boolean {
  const element = target instanceof Element ? target : target.parentElement;
  return Boolean(element?.closest('[data-floating-menu-owner="' + owner + '"]'));
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}

export function ComposerImageStrip({
  images,
  onRemoveImage
}: {
  images: ComposerImage[];
  onRemoveImage: (id: string) => void;
}): JSX.Element {
  return (
    <div className="composer-attachments">
      {images.map((image, index) => (
        <div className="composer-image-attachment" key={image.id}>
          <img src={imageSource(image)} alt={`Image ${index + 1}`} />
          <button type="button" aria-label={`移除图片 ${index + 1}`} onClick={() => onRemoveImage(image.id)}>
            <X size={13} />
          </button>
        </div>
      ))}
    </div>
  );
}

export function SplitPaneComposer({
  prompt,
  setPrompt,
  images,
  running,
  readOnly,
  status,
  onPasteImageFiles,
  onRemoveImage,
  onSend,
  onInterrupt
}: {
  prompt: string;
  setPrompt: (value: string) => void;
  images: ComposerImage[];
  running: boolean;
  readOnly: boolean;
  status: string;
  onPasteImageFiles: (files: File[]) => void;
  onRemoveImage: (id: string) => void;
  onSend: () => void;
  onInterrupt: () => void;
}): JSX.Element {
  const hasDraft = prompt.trim().length > 0 || images.length > 0;
  const statusText = status === "ready" ? "" : status;

  function handleKeyDown(event: ReactKeyboardEvent<HTMLTextAreaElement>): void {
    if (readOnly || isComposerTextComposing(event)) {
      return;
    }
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      onSend();
    }
  }

  return (
    <footer className="split-composer">
      <div className="split-composer-shell">
        {images.length > 0 ? <ComposerImageStrip images={images} onRemoveImage={onRemoveImage} /> : null}
        <textarea
          value={prompt}
          placeholder={readOnly ? "子任务会话只读" : images.length > 0 ? "添加描述" : "继续这个分支"}
          disabled={readOnly}
          aria-readonly={readOnly}
          onChange={(event) => setPrompt(event.target.value)}
          onPaste={(event) => {
            if (readOnly) {
              return;
            }
            const files = clipboardImageFiles(event);
            if (files.length === 0) {
              return;
            }
            event.preventDefault();
            onPasteImageFiles(files);
          }}
          onKeyDown={handleKeyDown}
        />
        <div className="split-composer-bar">
          {statusText ? <span className="split-composer-status">{statusText}</span> : <span />}
          {running ? (
            <button
              className="composer-action-button composer-stop-button"
              type="button"
              onClick={onInterrupt}
              aria-label="停止"
              title="停止"
            >
              <Square size={16} />
            </button>
          ) : (
            <button
              className="composer-action-button composer-send-button"
              type="button"
              onClick={onSend}
              aria-label="发送"
              disabled={readOnly || !hasDraft}
            >
              <Send size={17} />
            </button>
          )}
        </div>
      </div>
    </footer>
  );
}

function ComposerQueueStrip({
  guideMessages,
  queuedMessages,
  onRemoveGuideMessage,
  onRemoveQueuedMessage,
  onGuideQueuedMessage,
  onClearQueuedMessages
}: {
  guideMessages: QueuedComposerMessage[];
  queuedMessages: QueuedComposerMessage[];
  onRemoveGuideMessage: (id: string) => void;
  onRemoveQueuedMessage: (id: string) => void;
  onGuideQueuedMessage: (id: string) => void;
  onClearQueuedMessages: () => void;
}): JSX.Element | null {
  const total = guideMessages.length + queuedMessages.length;
  if (total === 0) {
    return null;
  }

  return (
    <div className="composer-queue-strip" aria-label="待发送消息">
      <div className="composer-queue-items">
        {guideMessages.map((message) => (
          <ComposerQueueItem
            key={message.id}
            message={message}
            kind="guide"
            onClearAll={onClearQueuedMessages}
            onRemove={() => onRemoveGuideMessage(message.id)}
          />
        ))}
        {queuedMessages.map((message) => (
          <ComposerQueueItem
            key={message.id}
            message={message}
            kind="queue"
            onGuide={() => onGuideQueuedMessage(message.id)}
            onClearAll={onClearQueuedMessages}
            onRemove={() => onRemoveQueuedMessage(message.id)}
          />
        ))}
      </div>
    </div>
  );
}

function ComposerQueueItem({
  message,
  kind,
  onGuide,
  onClearAll,
  onRemove
}: {
  message: QueuedComposerMessage;
  kind: "guide" | "queue";
  onGuide?: () => void;
  onClearAll: () => void;
  onRemove: () => void;
}): JSX.Element {
  return (
    <div className={`composer-queue-item ${kind}`}>
      <CornerDownRight className="composer-queue-corner" size={18} aria-hidden="true" />
      <strong>{queuedMessagePreview(message)}</strong>
      {kind === "guide" ? (
        <span className="composer-queue-guide active">
          <CornerDownRight size={16} aria-hidden="true" />
          引导
        </span>
      ) : (
        <button className="composer-queue-guide" type="button" aria-label="作为引导发送" onClick={onGuide}>
          <CornerDownRight size={16} aria-hidden="true" />
          <span>引导</span>
        </button>
      )}
      <button className="composer-queue-icon" type="button" aria-label="移除待发送消息" onClick={onRemove}>
        <Trash2 size={16} />
      </button>
      <button className="composer-queue-icon" type="button" aria-label="清空全部待发送消息" onClick={onClearAll}>
        <MoreHorizontal size={18} />
      </button>
    </div>
  );
}

function FloatingMenuPortal({
  anchorRef,
  owner,
  placement,
  align,
  offset = 8,
  crossAxisOffset = 0,
  width,
  children
}: {
  anchorRef: RefObject<HTMLElement | null>;
  owner: FloatingMenuOwner;
  placement: FloatingMenuPlacement;
  align: FloatingMenuAlign;
  offset?: number;
  crossAxisOffset?: number;
  width: number;
  children: ReactNode;
}): JSX.Element | null {
  const [style, setStyle] = useState<CSSProperties>({
    position: "fixed",
    visibility: "hidden"
  });

  useLayoutEffect(() => {
    function updatePosition(): void {
      const anchor = anchorRef.current;
      if (!anchor) {
        return;
      }
      const viewportMargin = 8;
      const rect = anchor.getBoundingClientRect();
      const baseLeft = align === "right" ? rect.right - width : rect.left;
      const maxLeft = Math.max(viewportMargin, window.innerWidth - width - viewportMargin);
      const left = clamp(baseLeft + crossAxisOffset, viewportMargin, maxLeft);
      const nextStyle: CSSProperties = {
        left,
        position: "fixed",
        visibility: "visible",
        zIndex: 80
      };
      if (placement === "above") {
        nextStyle.bottom = Math.max(viewportMargin, window.innerHeight - rect.top + offset);
      } else {
        nextStyle.top = Math.max(viewportMargin, rect.bottom + offset);
      }
      setStyle(nextStyle);
    }

    updatePosition();
    window.addEventListener("resize", updatePosition);
    window.addEventListener("scroll", updatePosition, true);
    return () => {
      window.removeEventListener("resize", updatePosition);
      window.removeEventListener("scroll", updatePosition, true);
    };
  }, [align, anchorRef, crossAxisOffset, offset, placement, width]);

  return createPortal(
    <div className={`floating-menu-layer floating-menu-${placement}`} data-floating-menu-owner={owner} style={style}>
      {children}
    </div>,
    document.body
  );
}

export function Composer({
  variant = "dock",
  containerRef,
  prompt,
  setPrompt,
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
  onPasteImageFiles,
  onRemoveImage,
  onRemoveQueuedMessage,
  onRemoveGuideMessage,
  onGuideQueuedMessage,
  onClearQueuedMessages,
  onSend,
  onInterrupt
}: {
  variant?: ComposerVariant;
  containerRef?: Ref<HTMLElement>;
  prompt: string;
  setPrompt: (value: string) => void;
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
  onPasteImageFiles: (files: File[]) => void;
  onRemoveImage: (id: string) => void;
  onRemoveQueuedMessage: (id: string) => void;
  onRemoveGuideMessage: (id: string) => void;
  onGuideQueuedMessage: (id: string) => void;
  onClearQueuedMessages: () => void;
  onSend: () => void;
  onInterrupt: () => void;
}): JSX.Element {
  const contextLabel = activeContext?.kind === "project" ? activeProject?.name ?? "项目" : "不使用项目";
  const statusText = status === "ready" ? "" : status;
  const className = `composer-wrap ${variant === "hero" ? "hero-composer-wrap" : "dock-composer-wrap"}`;
  const hasDraft = prompt.trim().length > 0 || images.length > 0;
  const menuPlacement: FloatingMenuPlacement = variant === "hero" ? "below" : "above";
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const [selectedSlashIndex, setSelectedSlashIndex] = useState(0);
  const [slashDismissedValue, setSlashDismissedValue] = useState("");
  const slashDraft = parseComposerSlashDraft(prompt);
  const slashQuery = slashDraft?.query ?? "";
  const slashCommands = useMemo(
    () => buildComposerSlashCommands({ activeContext, initialized, running }),
    [activeContext, initialized, running]
  );
  const fastModelTarget = useMemo(() => runtimeFastModelTarget(initialized), [initialized]);
  const visibleSlashCommands = useMemo(
    () => filterComposerSlashCommands(slashCommands, slashQuery),
    [slashCommands, slashQuery]
  );
  const slashMenuOpen = Boolean(!readOnly && slashDraft && slashDismissedValue !== prompt);
  const selectedSlashCommand = slashMenuOpen ? visibleSlashCommands[selectedSlashIndex] : undefined;
  const slashMenuID = `composer-slash-commands-${variant}`;

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
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      onSend();
    }
  }

  const content = (
    <div className="composer-stack">
      <ComposerQueueStrip
        guideMessages={guideMessages}
        queuedMessages={queuedMessages}
        onRemoveGuideMessage={onRemoveGuideMessage}
        onRemoveQueuedMessage={onRemoveQueuedMessage}
        onGuideQueuedMessage={onGuideQueuedMessage}
        onClearQueuedMessages={onClearQueuedMessages}
      />
      <div className="composer-shell">
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
          {images.length > 0 ? <ComposerImageStrip images={images} onRemoveImage={onRemoveImage} /> : null}
          <textarea
            ref={textareaRef}
            value={prompt}
            placeholder={readOnly ? "子任务会话只读" : images.length > 0 ? "添加描述" : "尽管问，或输入 / 选择命令"}
            disabled={readOnly}
            aria-readonly={readOnly}
            aria-controls={slashMenuOpen ? slashMenuID : undefined}
            aria-activedescendant={selectedSlashCommand ? `${slashMenuID}-${selectedSlashCommand.id}` : undefined}
            aria-expanded={slashMenuOpen || undefined}
            onChange={(event) => {
              setSlashDismissedValue("");
              setPrompt(event.target.value);
            }}
            onPaste={(event) => {
              if (readOnly) {
                return;
              }
              const files = clipboardImageFiles(event);
              if (files.length === 0) {
                return;
              }
              event.preventDefault();
              onPasteImageFiles(files);
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
              className="composer-tool-button composer-slash-button"
              type="button"
              aria-label="打开斜杠命令"
              title="输入 / 打开命令"
              disabled={readOnly}
              onClick={revealSlashCommands}
            >
              <span>/</span>
            </button>
            <div className="permission-menu-anchor" ref={accessMenuRef}>
              <button
                className="permission-chip"
                type="button"
                aria-haspopup="menu"
                aria-expanded={accessMenuOpen}
                onClick={onToggleAccessMenu}
              >
                <ShieldCheck size={16} />
                <span>完全访问权限</span>
                <ChevronDown size={15} />
              </button>
              {accessMenuOpen ? (
                <FloatingMenuPortal
                  anchorRef={accessMenuRef}
                  owner="composer-access"
                  placement="above"
                  align="left"
                  offset={6}
                  width={260}
                >
                  <AccessMenu />
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
            {running ? (
              <button
                className="composer-action-button composer-stop-button"
                type="button"
                onClick={onInterrupt}
                aria-label="停止"
                title="停止"
              >
                <Square size={17} />
              </button>
            ) : (
              <button
                className="composer-action-button composer-send-button"
                type="button"
                onClick={onSend}
                aria-label="发送"
                disabled={readOnly || !hasDraft}
              >
                <Send size={18} />
              </button>
            )}
          </div>
        </div>
        <div className="composer-context-bar" ref={menuRef}>
          <button className="context-project-button" onClick={onToggleMenu} aria-haspopup="menu" aria-expanded={menuOpen}>
            {activeContext?.kind === "project" ? <Folder size={18} /> : <FolderX size={18} />}
            <span>{contextLabel}</span>
            <ChevronDown size={16} />
          </button>
          <button
            className="context-mode-chip"
            type="button"
            aria-haspopup="menu"
            aria-expanded={modeMenuOpen}
            onClick={onToggleModeMenu}
          >
            <Laptop size={17} />
            <span>本地模式</span>
            <ChevronDown size={15} />
          </button>
          {gitStatus?.is_repo && gitStatus.branch ? (
            <button
              className="context-branch-chip"
              type="button"
              aria-haspopup="menu"
              aria-expanded={branchMenuOpen}
              onClick={onToggleBranchMenu}
            >
              <GitBranch size={17} />
              <span>{gitStatus.branch}</span>
              {gitStatus.dirty_count > 0 ? <small>未提交：{gitStatus.dirty_count} 个文件</small> : null}
              <ChevronDown size={15} />
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

function RuntimePicker({
  variant,
  initialized,
  state,
  openMenu,
  anchorRef,
  running,
  onToggleMenu,
  onSelectModel,
  onSelectEffort
}: {
  variant: ComposerVariant;
  initialized: InitializeResult;
  state: CodexModelLoadState;
  openMenu: CodexRuntimeMenu;
  anchorRef: RefObject<HTMLDivElement | null>;
  running: boolean;
  onToggleMenu: (menu: Exclude<CodexRuntimeMenu, null>) => void;
  onSelectModel: (provider: string, model: string, variant?: string) => void;
  onSelectEffort: (variant: string) => void;
}): JSX.Element {
  const currentProvider = initialized.providers?.find((provider) => provider.name === initialized.provider);
  const codexProvider = isCodexProvider(initialized);
  const currentCodexModel = codexProvider ? state.models.find((model) => model.slug === initialized.model) : undefined;
  const currentProviderModel = currentProvider?.models?.find((model) => model.id === initialized.model);
  const currentVariant = initialized.variant ?? initialized.effort ?? "";
  const variantOptions = codexProvider
    ? codexEffortOptions(currentCodexModel, currentVariant)
    : providerModelVariantOptions(currentProvider, initialized.model, currentVariant);
  const placement: FloatingMenuPlacement = variant === "hero" ? "below" : "above";
  return (
    <div className="codex-runtime-anchor" ref={anchorRef}>
      <button
        className="codex-runtime-trigger"
        type="button"
        disabled={running}
        aria-haspopup="menu"
        aria-expanded={openMenu !== null}
        onClick={() => onToggleMenu("main")}
      >
        <span>{runtimeTriggerLabel(initialized, currentProviderModel, currentCodexModel)}</span>
        <span className="codex-runtime-effort">{variantLabel(currentVariant)}</span>
        <ChevronDown size={15} />
      </button>
      {openMenu === "main" ? (
        <FloatingMenuPortal
          anchorRef={anchorRef}
          owner="codex-runtime"
          placement={placement}
          align="right"
          width={236}
        >
          <RuntimeMainMenu
            selectedVariant={currentVariant}
            options={variantOptions}
            currentLabel={runtimeModelLabel(initialized, currentProviderModel, currentCodexModel)}
            onSelectEffort={onSelectEffort}
            onOpenModelMenu={() => onToggleMenu("model")}
          />
        </FloatingMenuPortal>
      ) : null}
      {openMenu === "model" ? (
        <FloatingMenuPortal
          anchorRef={anchorRef}
          owner="codex-runtime"
          placement={placement}
          align="right"
          width={286}
        >
          <RuntimeModelMenu
            initialized={initialized}
            state={state}
            selectedProvider={initialized.provider}
            selectedModel={initialized.model}
            selectedVariant={currentVariant}
            onSelectModel={onSelectModel}
          />
        </FloatingMenuPortal>
      ) : null}
    </div>
  );
}

function RuntimeMainMenu({
  selectedVariant,
  options,
  currentLabel,
  onSelectEffort,
  onOpenModelMenu
}: {
  selectedVariant: string;
  options: string[];
  currentLabel: string;
  onSelectEffort: (variant: string) => void;
  onOpenModelMenu: () => void;
}): JSX.Element {
  return (
    <div className="codex-runtime-menu codex-main-menu" role="menu">
      <div className="codex-menu-label">思考强度</div>
      {options.length > 1 ? (
        options.map((variant) => {
          const selected = variant === selectedVariant;
          return (
            <button key={variant || "auto"} role="menuitem" type="button" onClick={() => onSelectEffort(variant)}>
              <span>{variantLabel(variant)}</span>
              {selected ? <Check size={18} /> : null}
            </button>
          );
        })
      ) : (
        <div className="composer-menu-empty">当前模型没有可调思考强度</div>
      )}
      <div className="codex-menu-separator" />
      <button role="menuitem" type="button" onClick={onOpenModelMenu}>
        <span>{currentLabel}</span>
        <ChevronRight className="codex-menu-chevron" size={18} />
      </button>
    </div>
  );
}

function RuntimeModelMenu({
  initialized,
  state,
  selectedProvider,
  selectedModel,
  selectedVariant,
  onSelectModel
}: {
  initialized: InitializeResult;
  state: CodexModelLoadState;
  selectedProvider: string;
  selectedModel: string;
  selectedVariant: string;
  onSelectModel: (provider: string, model: string, variant?: string) => void;
}): JSX.Element {
  const providers = initialized.providers ?? [];
  const codexProviderSelected = isCodexProvider(initialized);
  const configuredModels = providers
    .map((provider) => {
      const model = configuredRuntimeModelForProvider(provider, state);
      return model ? { provider, model } : undefined;
    })
    .filter((item): item is RuntimeModelOption => Boolean(item));
  const additionalModels = providers.flatMap((provider) =>
    runtimeModelsForProvider(provider, state)
      .filter((model) => model.id !== provider.model)
      .map((model) => ({ provider, model }))
  );
  return (
    <div className="codex-runtime-menu codex-model-menu" role="menu">
      <div className="codex-menu-label">已配置</div>
      {codexProviderSelected && state.loading ? <div className="composer-menu-empty">正在加载 Codex 模型</div> : null}
      {codexProviderSelected && state.error ? (
        <div className="composer-menu-note warning">
          <strong>无法读取 Codex 登录态</strong>
          <span>{state.error}</span>
        </div>
      ) : null}
      {!state.loading && providers.length === 0 ? (
        <div className="composer-menu-empty">没有可用模型</div>
      ) : null}
      {configuredModels.map(({ provider, model }) => (
        <RuntimeModelMenuItem
          key={`configured/${provider.name}/${model.id}`}
          provider={provider}
          model={model}
          selected={provider.name === selectedProvider && model.id === selectedModel}
          selectedVariant={selectedVariant}
          onSelectModel={onSelectModel}
        />
      ))}
      {additionalModels.length > 0 ? (
        <>
          <div className="codex-menu-separator" />
          <div className="codex-menu-label">更多模型</div>
          {additionalModels.map(({ provider, model }) => (
            <RuntimeModelMenuItem
              key={`additional/${provider.name}/${model.id}`}
              provider={provider}
              model={model}
              selected={provider.name === selectedProvider && model.id === selectedModel}
              selectedVariant={selectedVariant}
              onSelectModel={onSelectModel}
            />
          ))}
        </>
      ) : null}
    </div>
  );
}

type RuntimeModelOption = {
  provider: ProviderSummary;
  model: ProviderModelSummary;
};

function RuntimeModelMenuItem({
  provider,
  model,
  selected,
  selectedVariant,
  onSelectModel
}: {
  provider: ProviderSummary;
  model: ProviderModelSummary;
  selected: boolean;
  selectedVariant: string;
  onSelectModel: (provider: string, model: string, variant?: string) => void;
}): JSX.Element {
  const nextVariant = normalizedVariantForRuntimeModel(selectedVariant, provider, model);
  return (
    <button role="menuitem" type="button" onClick={() => onSelectModel(provider.name, model.id, nextVariant)}>
      <span>
        <strong>{providerModelDisplayName(model)}</strong>
        <small>{provider.name}</small>
      </span>
      {selected ? <Check size={18} /> : null}
    </button>
  );
}

function runtimeTriggerLabel(
  initialized: InitializeResult,
  providerModel?: ProviderModelSummary,
  codexModel?: CodexModelSummary
): string {
  if (codexModel) {
    return shortCodexModelLabel(codexModel.slug);
  }
  return shortCodexModelLabel(providerModel?.display_name || initialized.model);
}

function runtimeModelLabel(
  initialized: InitializeResult,
  providerModel?: ProviderModelSummary,
  codexModel?: CodexModelSummary
): string {
  if (codexModel) {
    return displayCodexModelName(codexModel);
  }
  return providerModel?.display_name || initialized.model;
}

function configuredRuntimeModelForProvider(
  provider: ProviderSummary,
  state: CodexModelLoadState
): ProviderModelSummary | undefined {
  if (!provider.model) {
    return undefined;
  }
  return (
    runtimeModelsForProvider(provider, state).find((model) => model.id === provider.model) ??
    provider.models?.find((model) => model.id === provider.model) ?? { id: provider.model, source: "selected" }
  );
}

function runtimeModelsForProvider(provider: ProviderSummary, state: CodexModelLoadState): ProviderModelSummary[] {
  const type = provider.type.trim().toLowerCase().replaceAll("_", "-");
  const isCodex = type === "openai-codex" || type === "codex-subscription" || type === "chatgpt-codex";
  if (isCodex && state.provider === provider.name && state.models.length > 0) {
    return state.models.map((model) => ({
      id: model.slug,
      display_name: displayCodexModelName(model),
      default_effort: model.default_reasoning_level,
      supported_efforts: model.supported_reasoning,
      source: "live"
    }));
  }
  if (provider.models?.length) {
    return provider.models;
  }
  return [{ id: provider.model, source: "selected" }];
}

function normalizedVariantForRuntimeModel(
  currentVariant: string,
  provider: ProviderSummary,
  model: ProviderModelSummary
): string {
  if (!currentVariant) {
    return "";
  }
  const modelVariants = (model.variants ?? []).map((item) => item.id).filter(Boolean);
  const supported = modelVariants.length > 0 ? modelVariants : model.supported_efforts ?? [];
  if (supported.length === 0) {
    return "";
  }
  if (supported.includes(currentVariant)) {
    return currentVariant;
  }
  if (model.default_variant && supported.includes(model.default_variant)) {
    return model.default_variant;
  }
  if (model.default_effort && supported.includes(model.default_effort)) {
    return model.default_effort;
  }
  const providerModel = provider.models?.find((item) => item.id === model.id);
  if (providerModel?.default_variant && supported.includes(providerModel.default_variant)) {
    return providerModel.default_variant;
  }
  if (providerModel?.default_effort && supported.includes(providerModel.default_effort)) {
    return providerModel.default_effort;
  }
  return "";
}

function AccessMenu(): JSX.Element {
  return (
    <div className="composer-context-menu access-menu" role="menu">
      <div className="composer-menu-note">
        <strong>完全访问权限</strong>
        <span>wuu 可以读取并修改当前工作区文件，适合直接做开发任务。</span>
      </div>
    </div>
  );
}

function ModeMenu({
  activeContext,
  onSelectNoProject,
  onOpenProject
}: {
  activeContext?: RuntimeContext;
  onSelectNoProject: () => void;
  onOpenProject: () => void;
}): JSX.Element {
  return (
    <div className="composer-context-menu mode-menu" role="menu">
      <button role="menuitem" type="button" onClick={onOpenProject}>
        <FolderOpen size={18} />
        <span>打开本地项目</span>
        {activeContext?.kind === "project" ? <Check size={17} /> : null}
      </button>
      <button role="menuitem" type="button" onClick={onSelectNoProject}>
        <FolderX size={18} />
        <span>不使用项目</span>
        {activeContext?.kind === "no_project" ? <Check size={17} /> : null}
      </button>
    </div>
  );
}

function BranchMenu({
  gitStatus,
  onSelectBranch
}: {
  gitStatus: GitStatusResult;
  onSelectBranch: (branch: string) => void;
}): JSX.Element {
  const branches = gitStatus.branches ?? [];
  return (
    <div className="composer-context-menu branch-menu" role="menu">
      {gitStatus.dirty_count > 0 ? (
        <div className="composer-menu-note warning">
          <strong>有未提交更改</strong>
          <span>{gitStatus.dirty_count} 个文件会随分支切换保留；如果会覆盖本地改动，Git 会拒绝切换。</span>
        </div>
      ) : null}
      {branches.length === 0 ? <div className="composer-menu-empty">没有本地分支</div> : null}
      {branches.map((branch) => {
        const selected = branch === gitStatus.branch;
        return (
          <button
            key={branch}
            role="menuitem"
            type="button"
            disabled={selected}
            onClick={() => onSelectBranch(branch)}
          >
            <GitBranch size={18} />
            <span>{branch}</span>
            {selected ? <Check size={17} /> : null}
          </button>
        );
      })}
    </div>
  );
}

function ProjectPickerMenu({
  projects,
  activeContext,
  query,
  setQuery,
  onSelectProject,
  onSelectNoProject,
  onCreateProject,
  onOpenProject
}: {
  projects: DesktopProject[];
  activeContext?: RuntimeContext;
  query: string;
  setQuery: (value: string) => void;
  onSelectProject: (id: string) => void;
  onSelectNoProject: () => void;
  onCreateProject: () => void;
  onOpenProject: () => void;
}): JSX.Element {
  const normalizedQuery = query.trim().toLocaleLowerCase();
  const filteredProjects = normalizedQuery
    ? projects.filter((project) => project.name.toLocaleLowerCase().includes(normalizedQuery) || project.path.toLocaleLowerCase().includes(normalizedQuery))
    : projects;

  return (
    <div className="composer-project-menu" role="menu">
      <label className="project-search">
        <Search size={18} />
        <input value={query} placeholder="搜索项目" onChange={(event) => setQuery(event.target.value)} />
      </label>
      <OverlayScrollbarsComponent
        className="project-picker-list"
        data-overlayscrollbars-initialize
        defer
        options={OVERLAY_SCROLLBAR_OPTIONS}
      >
        {filteredProjects.length === 0 ? <div className="project-picker-empty">没有匹配项目</div> : null}
        {filteredProjects.map((project) => {
          const selected = activeContext?.kind === "project" && activeContext.project_id === project.id;
          return (
            <button key={project.id} role="menuitem" onClick={() => onSelectProject(project.id)}>
              <Folder size={19} />
              <span>{project.name}</span>
              {selected ? <Check size={18} /> : null}
            </button>
          );
        })}
      </OverlayScrollbarsComponent>
      <div className="project-picker-divider" />
      <button role="menuitem" onClick={onOpenProject}>
        <FolderOpen size={19} />
        <span>使用现有文件夹</span>
      </button>
      <button role="menuitem" onClick={onCreateProject}>
        <FolderPlus size={19} />
        <span>新建空白项目</span>
      </button>
      <button role="menuitem" onClick={onSelectNoProject}>
        <FolderX size={19} />
        <span>不使用项目</span>
        {activeContext?.kind === "no_project" ? <Check size={18} /> : null}
      </button>
    </div>
  );
}
