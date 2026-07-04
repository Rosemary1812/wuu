import {
  AtSign,
  Bug,
  ChevronDown,
  ChevronRight,
  ChevronUp,
  CircleHelp,
  FileText,
  FlaskConical,
  Folder,
  FolderOpen,
  FolderX,
  GitCommitHorizontal,
  GitPullRequest,
  Hammer,
  Laptop,
  MessageSquarePlus,
  Paperclip,
  PieChart,
  Search,
  Send,
  Settings,
  Slash,
  Square,
  Terminal,
  Wrench,
  X,
  Zap
} from "lucide-react";
import {
  type ClipboardEvent as ReactClipboardEvent,
  type KeyboardEvent as ReactKeyboardEvent,
  type Ref,
  type RefObject,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState
} from "react";
import type {
  DesktopProject,
  GitStatusResult,
  InitializeResult,
  ParticipantProfile,
  RuntimeContext,
  SkillSummary
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
import { ComposerGoalStrip } from "./ComposerGoalStrip";
import {
  AccessMenu,
  ChatFocusChip,
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
  PermissionMode
} from "./ComposerTypes";
import { composerStatusIsLiveProgress, composerStatusText } from "./ComposerTypes";
import type { WorkspacePanelView } from "./WorkspacePanels";
import { ComposerTokenGauge } from "./ComposerTokenGauge";
import { ComposerContextMeter } from "./ComposerContextMeter";
import type { TurnContextUsage } from "./AppState";
import type { ComposerGoalSummary } from "../shared/protocol";

type CollapsedComposerPromptBlock = {
  id: string;
  text: string;
};

const COLLAPSIBLE_COMPOSER_PROMPT_LINE_THRESHOLD = 14;
const COLLAPSIBLE_COMPOSER_PROMPT_CHAR_THRESHOLD = 1200;
const COLLAPSIBLE_COMPOSER_PROMPT_SOFT_LINE_CHARS = 84;

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
  runtimeControlsDisabled = running,
  status,
  statusLiveProgress,
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
  onToggleBranchMenu,
  onOpenSettings,
  onOpenSkillsCatalog,
  onSelectProject,
  onSelectNoProject,
  onSelectGitBranch,
  onCreateProject,
  onOpenProject,
  onStartNewThread,
  onOpenWorkspaceTool,
  onOpenContextComposition = () => {},
  onOpenInstructions = () => {},
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
  goalSummary,
  onEditGoal,
  onPauseGoal,
  onResumeGoal,
  onClearGoal,
  tokensPerSecond,
  tokenSpeedSampledAt,
  tokenSpeedSource,
  contextUsage,
  queryHistorySessionID,
  queryHistory = [],
  participants = [],
  chatFocusValue,
  onSelectChatFocus
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
  runtimeControlsDisabled?: boolean;
  status: string;
  statusLiveProgress?: boolean;
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
  onToggleBranchMenu: () => void;
  onOpenSettings: () => void;
  onOpenSkillsCatalog: () => void;
  onSelectProject: (id: string) => void;
  onSelectNoProject: () => void;
  onSelectGitBranch: (branch: string) => void;
  onCreateProject: () => void;
  onOpenProject: () => void;
  onStartNewThread: () => void;
  onOpenWorkspaceTool: (view: WorkspacePanelView) => void;
  onOpenContextComposition?: () => void;
  onOpenInstructions?: () => void;
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
  goalSummary?: ComposerGoalSummary | null;
  onEditGoal?: (nextText: string) => void | Promise<void>;
  onPauseGoal?: () => void | Promise<void>;
  onResumeGoal?: () => void | Promise<void>;
  onClearGoal?: () => void | Promise<void>;
  tokensPerSecond: number;
  tokenSpeedSampledAt?: number;
  tokenSpeedSource?: "real" | "estimated" | "none";
  // contextUsage drives the model-adjacent context meter. When the active
  // model has no catalog window yet the AppState selector returns
  // undefined and the meter hides entirely.
  contextUsage?: TurnContextUsage | null;
  queryHistorySessionID?: string;
  queryHistory?: string[];
  participants?: ParticipantProfile[];
  // Chat-style (DM/group) thread "work focus" chip. `chatFocusValue`
  // doubles as the visibility switch: undefined means the active thread
  // is not chat-style and the chip is not rendered at all; "" renders
  // the de-emphasized default (全部工作区), "~" the personal space, any
  // other string a workspace name. The host owns the per-thread sticky
  // state (App.tsx chatFocusOverrides + Thread.focus_workspace echo).
  chatFocusValue?: string;
  onSelectChatFocus?: (value: string) => void;
}): JSX.Element {
  const statusText = composerStatusText(status);
  const statusIsLiveProgress = composerStatusIsLiveProgress(statusLiveProgress);
  const className = `composer-wrap ${variant === "hero" ? "hero-composer-wrap" : "dock-composer-wrap"}`;
  const hasAttachments = images.length > 0 || files.length > 0;
  const hasDraft = prompt.trim().length > 0 || hasAttachments;
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const composerFrameRef = useRef<HTMLDivElement>(null);
  const collapsedComposerFrameHeightRef = useRef<number | null>(null);
  const attachmentInputRef = useRef<HTMLInputElement>(null);
  const collapsedPromptListRef = useRef<HTMLDivElement>(null);
  const collapsedPromptBlockIDRef = useRef(0);
  const [isComposerExpanded, setIsComposerExpanded] = useState(false);
  const [collapsedPromptBlocks, setCollapsedPromptBlocks] = useState<CollapsedComposerPromptBlock[]>([]);
  const [selectedSlashIndex, setSelectedSlashIndex] = useState(0);
  const [slashDismissedValue, setSlashDismissedValue] = useState("");
  const [selectedMentionIndex, setSelectedMentionIndex] = useState(0);
  const [mentionDismissedValue, setMentionDismissedValue] = useState("");
  const [slashSkills, setSlashSkills] = useState<SkillSummary[]>([]);
  const collapsedPromptPrefix = useMemo(
    () => collapsedPromptBlocks.map((block) => block.text).join(""),
    [collapsedPromptBlocks]
  );
  const hasCollapsedPromptBlocks = collapsedPromptBlocks.length > 0 && prompt.startsWith(collapsedPromptPrefix);
  const activeCollapsedPromptBlocks = hasCollapsedPromptBlocks ? collapsedPromptBlocks : [];
  const visiblePromptValue = hasCollapsedPromptBlocks
    ? prompt.slice(collapsedPromptPrefix.length)
    : prompt;
  const composerPlaceholder = readOnly
    ? "子任务会话只读"
    : hasCollapsedPromptBlocks
      ? "要求后续变更"
      : hasAttachments
        ? "添加描述"
        : "向 wuu 提问，或输入 / 选择命令";
  const slashDraft = parseComposerSlashDraft(prompt);
  const slashQuery = slashDraft?.query ?? "";
  const slashSkillContextKey = activeContext ? composerRuntimeContextKey(activeContext) : "";
  const slashSkillCountKey = initialized?.extension_trust?.main_session?.skills?.count ?? 0;
  const slashRuntimeReady = Boolean(activeContext && initialized);
  const slashCommands = useMemo(
    () => buildComposerSlashCommands({ activeContext, initialized, running, skills: slashSkills }),
    [activeContext, initialized, running, slashSkills]
  );
  const fastModelTarget = useMemo(() => runtimeFastModelTarget(initialized), [initialized]);
  const permissionModeHasOverrides = permissionModeHasAdvancedOverrides(initialized?.tool_policy, initialized?.permissions);
  const permissionMode = permissionModeFromSummary(initialized?.permissions);
  const permissionOption = permissionModeOption(permissionMode);
  const permissionChipLabel = permissionModeHasOverrides ? "自定义权限" : permissionOption.chipLabel;
  const projectPillLabel = heroProjectPillLabel(activeContext, activeProject);
  const projectPillTitle =
    activeContext?.kind === "project" && activeProject?.path
      ? activeProject.path
      : projectPillLabel;
  const ProjectPillIcon =
    activeContext?.kind === "no_project"
      ? FolderX
      : activeContext?.kind === "project"
        ? Folder
        : FolderOpen;
  const visibleSlashCommands = useMemo(
    () => filterComposerSlashCommands(slashCommands, slashQuery),
    [slashCommands, slashQuery]
  );
  const slashMenuOpen = Boolean(!readOnly && slashDraft && slashDismissedValue !== prompt);
  const selectedSlashCommand = slashMenuOpen ? visibleSlashCommands[selectedSlashIndex] : undefined;
  const slashMenuID = `composer-slash-commands-${variant}`;
  const mentionDraft = parseComposerMentionDraft(visiblePromptValue);
  const mentionQuery = mentionDraft?.query ?? "";
  const visibleMentionParticipants = useMemo(
    () => filterMentionParticipants(participants, mentionQuery),
    [mentionQuery, participants]
  );
  const mentionMenuOpen = Boolean(
    !readOnly &&
      !slashMenuOpen &&
      mentionDraft &&
      mentionDismissedValue !== prompt &&
      visibleMentionParticipants.length > 0
  );
  const selectedMentionParticipant = mentionMenuOpen
    ? visibleMentionParticipants[selectedMentionIndex]
    : undefined;
  const mentionMenuID = `composer-mentions-${variant}`;
  const { resetQueryHistoryNavigation, handleQueryHistoryKeyDown } = useComposerQueryHistory({
    disabled: readOnly || hasAttachments || hasCollapsedPromptBlocks,
    prompt,
    queryHistory,
    queryHistorySessionID,
    setPrompt,
    textareaRef
  });

  useEffect(() => {
    setSelectedSlashIndex(firstEnabledSlashCommandIndex(visibleSlashCommands));
  }, [visibleSlashCommands]);

  useEffect(() => {
    setSelectedMentionIndex(0);
  }, [visibleMentionParticipants]);

  useEffect(() => {
    if (!slashRuntimeReady || readOnly) {
      setSlashSkills([]);
      return;
    }
    let cancelled = false;
    void loadSlashSkills();
    return () => {
      cancelled = true;
    };

    async function loadSlashSkills(): Promise<void> {
      try {
        const result = await window.wuu.listSkills();
        if (!cancelled) {
          setSlashSkills(result.skills);
        }
      } catch {
        if (!cancelled) {
          setSlashSkills([]);
        }
      }
    }
  }, [readOnly, slashRuntimeReady, slashSkillContextKey, slashSkillCountKey]);

  useEffect(() => {
    if (readOnly) {
      setIsComposerExpanded(false);
    }
  }, [readOnly]);

  useEffect(() => {
    if (collapsedPromptBlocks.length > 0 && !prompt.startsWith(collapsedPromptPrefix)) {
      setCollapsedPromptBlocks([]);
    }
  }, [collapsedPromptBlocks.length, collapsedPromptPrefix, prompt]);

  useLayoutEffect(() => {
    const frame = composerFrameRef.current;
    if (!frame) {
      return;
    }
    if (!isComposerExpanded) {
      frame.style.removeProperty("--composer-expanded-offset");
      return;
    }
    const collapsedHeight = collapsedComposerFrameHeightRef.current;
    if (!collapsedHeight) {
      frame.style.removeProperty("--composer-expanded-offset");
      return;
    }
    const expandedHeight = Math.ceil(frame.offsetHeight);
    const offset = Math.max(0, expandedHeight - collapsedHeight);
    frame.style.setProperty("--composer-expanded-offset", `${offset}px`);
  }, [
    files.length,
    goalSummary?.id,
    guideMessages.length,
    images.length,
    isComposerExpanded,
    queuedMessages.length,
    activeCollapsedPromptBlocks.length
  ]);

  useLayoutEffect(() => {
    const list = collapsedPromptListRef.current;
    if (!list || activeCollapsedPromptBlocks.length === 0) {
      return;
    }
    list.scrollTop = list.scrollHeight;
  }, [activeCollapsedPromptBlocks.length]);

  function focusComposerSoon(): void {
    window.requestAnimationFrame(() => textareaRef.current?.focus());
  }

  function toggleComposerExpansion(): void {
    if (readOnly) {
      return;
    }
    setIsComposerExpanded((expanded) => {
      if (!expanded) {
        collapsedComposerFrameHeightRef.current = composerFrameRef.current
          ? Math.ceil(composerFrameRef.current.offsetHeight)
          : null;
      }
      return !expanded;
    });
    focusComposerSoon();
  }

  function submitComposer(): void {
    resetQueryHistoryNavigation();
    const actionCommand = slashDraft
      ? exactActionSlashCommand(slashCommands, slashDraft)
      : undefined;
    if (actionCommand) {
      applySlashCommand(actionCommand, slashDraft);
      return;
    }
    onSend();
    focusComposerSoon();
  }

  function updateVisiblePrompt(value: string): void {
    resetQueryHistoryNavigation();
    setSlashDismissedValue("");
    setMentionDismissedValue("");
    setPrompt(hasCollapsedPromptBlocks ? `${collapsedPromptPrefix}${value}` : value);
  }

  function handleComposerPaste(event: ReactClipboardEvent<HTMLTextAreaElement>): void {
    if (readOnly) {
      return;
    }
    const pasted = clipboardAttachmentFiles(event);
    if (pasted.length > 0) {
      event.preventDefault();
      onPasteAttachmentFiles(pasted);
      return;
    }

    const pastedText = event.clipboardData?.getData("text/plain") ?? "";
    if (!isCollapsibleComposerPrompt(pastedText)) {
      return;
    }

    const selectionStart = event.currentTarget.selectionStart ?? 0;
    const selectionEnd = event.currentTarget.selectionEnd ?? 0;
    const visibleValue = event.currentTarget.value;
    const replacingVisiblePrompt = selectionStart === 0 && selectionEnd === visibleValue.length;
    if (visibleValue.length > 0 && !replacingVisiblePrompt) {
      return;
    }

    event.preventDefault();
    resetQueryHistoryNavigation();
    setSlashDismissedValue("");
    const nextBlock = {
      id: `composer-prompt-block-${collapsedPromptBlockIDRef.current++}`,
      text: pastedText
    };
    setCollapsedPromptBlocks(hasCollapsedPromptBlocks ? [...collapsedPromptBlocks, nextBlock] : [nextBlock]);
    setPrompt(`${hasCollapsedPromptBlocks ? collapsedPromptPrefix : ""}${pastedText}`);
    focusComposerSoon();
  }

  function revealCollapsedPromptBlock(index: number): void {
    if (!hasCollapsedPromptBlocks) {
      return;
    }
    const revealedBlock = activeCollapsedPromptBlocks[index];
    if (!revealedBlock) {
      return;
    }
    const nextBlocks = activeCollapsedPromptBlocks.filter((_, blockIndex) => blockIndex !== index);
    const nextPrefix = nextBlocks.map((block) => block.text).join("");
    const nextVisiblePrompt = `${visiblePromptValue}${revealedBlock.text}`;
    setCollapsedPromptBlocks(nextBlocks);
    setPrompt(`${nextPrefix}${nextVisiblePrompt}`);
    focusComposerSoon();
  }

  function removeCollapsedPromptBlock(index: number): void {
    if (!hasCollapsedPromptBlocks) {
      return;
    }
    const nextBlocks = activeCollapsedPromptBlocks.filter((_, blockIndex) => blockIndex !== index);
    const nextPrefix = nextBlocks.map((block) => block.text).join("");
    setCollapsedPromptBlocks(nextBlocks);
    setPrompt(`${nextPrefix}${visiblePromptValue}`);
    focusComposerSoon();
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
    if (command.kind === "prompt" || command.kind === "skill") {
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
      case "open-skills":
        onOpenSkillsCatalog();
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
      case "context":
        onOpenContextComposition();
        break;
      case "instructions":
        onOpenInstructions();
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

  function applyMentionParticipant(participant: ParticipantProfile | undefined): void {
    if (!participant || !mentionDraft) {
      return;
    }
    setMentionDismissedValue("");
    const nextVisiblePrompt = `${visiblePromptValue.slice(0, mentionDraft.start)}@${participant.name} `;
    setPrompt(
      hasCollapsedPromptBlocks
        ? `${collapsedPromptPrefix}${nextVisiblePrompt}`
        : nextVisiblePrompt
    );
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
      if (event.key === "Enter" && !event.shiftKey && slashDraft && exactRunnableSlashCommand(slashCommands, slashDraft)) {
        event.preventDefault();
        submitComposer();
        return;
      }
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
    if (mentionMenuOpen) {
      if (event.key === "ArrowDown" && visibleMentionParticipants.length > 0) {
        event.preventDefault();
        setSelectedMentionIndex((current) =>
          (current + 1) % visibleMentionParticipants.length
        );
        return;
      }
      if (event.key === "ArrowUp" && visibleMentionParticipants.length > 0) {
        event.preventDefault();
        setSelectedMentionIndex((current) =>
          (current - 1 + visibleMentionParticipants.length) %
          visibleMentionParticipants.length
        );
        return;
      }
      if ((event.key === "Enter" || event.key === "Tab") && selectedMentionParticipant) {
        event.preventDefault();
        applyMentionParticipant(selectedMentionParticipant);
        return;
      }
      if (event.key === "Escape") {
        event.preventDefault();
        setMentionDismissedValue(prompt);
        return;
      }
    }
    if (handleQueryHistoryKeyDown(event)) {
      return;
    }
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      submitComposer();
    }
  }

  const content = (
    <div className={`composer-stack${isComposerExpanded ? " is-expanded" : ""}`}>
      <div className="composer-shell">
        {slashMenuOpen ? (
          <div className="slash-command-menu" id={slashMenuID} role="listbox" aria-label="斜杠命令">
            {visibleSlashCommands.length > 0 ? (
              <div className="slash-command-list scrollbar-hidden">
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
        {mentionMenuOpen ? (
          <div className="mention-menu" id={mentionMenuID} role="listbox" aria-label="提及 Agent">
            <div className="mention-list scrollbar-hidden">
              {visibleMentionParticipants.map((participant, index) => {
                const selected = index === selectedMentionIndex;
                const optionID = `${mentionMenuID}-${participant.id}`;
                return (
                  <button
                    className={`mention-item${selected ? " selected" : ""}`}
                    id={optionID}
                    key={participant.id}
                    role="option"
                    type="button"
                    aria-selected={selected}
                    onMouseEnter={() => setSelectedMentionIndex(index)}
                    onMouseDown={(event) => event.preventDefault()}
                    onClick={() => applyMentionParticipant(participant)}
                  >
                    <span className="mention-avatar" aria-hidden="true">
                      <AtSign className="icon" />
                    </span>
                    <span className="mention-copy">
                      <strong>{participant.name}</strong>
                      <small>{participant.tagline || participant.role || "named"}</small>
                    </span>
                    <span className="mention-name">@{participant.name}</span>
                  </button>
                );
              })}
            </div>
          </div>
        ) : null}
        <div className="composer-frame" ref={composerFrameRef}>
          <ComposerGoalStrip
            summary={goalSummary ?? null}
            disabled={readOnly}
            onEdit={(nextText) => {
              if (onEditGoal) {
                return onEditGoal(nextText);
              }
              return undefined;
            }}
            onPause={() => {
              if (onPauseGoal) {
                return onPauseGoal();
              }
              return undefined;
            }}
            onResume={() => {
              if (onResumeGoal) {
                return onResumeGoal();
              }
              return undefined;
            }}
            onClear={() => {
              if (onClearGoal) {
                return onClearGoal();
              }
              return undefined;
            }}
          />
          <ComposerQueueStrip
            guideMessages={guideMessages}
            queuedMessages={queuedMessages}
            onRemoveGuideMessage={onRemoveGuideMessage}
            onRemoveQueuedMessage={onRemoveQueuedMessage}
            onGuideQueuedMessage={onGuideQueuedMessage}
            onEditGuideMessage={onEditGuideMessage}
            onEditQueuedMessage={onEditQueuedMessage}
          />
          <div className={`composer${hasCollapsedPromptBlocks ? " has-collapsed-prompt" : ""}`}>
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
            {hasCollapsedPromptBlocks ? (
              <div className="composer-collapsed-prompt-list" ref={collapsedPromptListRef} aria-label="折叠长文本">
                {activeCollapsedPromptBlocks.map((block, index) => (
                  <CollapsedComposerPromptCard
                    text={block.text}
                    key={block.id}
                    onReveal={() => revealCollapsedPromptBlock(index)}
                    onRemove={() => removeCollapsedPromptBlock(index)}
                  />
                ))}
              </div>
            ) : null}
            <textarea
              ref={textareaRef}
              value={visiblePromptValue}
              placeholder={composerPlaceholder}
              disabled={readOnly}
              aria-readonly={readOnly}
              aria-controls={
                slashMenuOpen ? slashMenuID : mentionMenuOpen ? mentionMenuID : undefined
              }
              aria-activedescendant={
                selectedSlashCommand
                  ? `${slashMenuID}-${selectedSlashCommand.id}`
                  : selectedMentionParticipant
                    ? `${mentionMenuID}-${selectedMentionParticipant.id}`
                    : undefined
              }
              aria-expanded={slashMenuOpen || mentionMenuOpen || undefined}
              onChange={(event) => {
                updateVisiblePrompt(event.target.value);
              }}
              onPaste={handleComposerPaste}
              onBlur={() => {
                if (slashMenuOpen) {
                  setSlashDismissedValue(prompt);
                }
                if (mentionMenuOpen) {
                  setMentionDismissedValue(prompt);
                }
              }}
              onKeyDown={handleComposerKeyDown}
            />
            <button
              className="composer-expand-button"
              type="button"
              aria-label={isComposerExpanded ? "收起输入框" : "展开输入框"}
              aria-pressed={isComposerExpanded}
              title={readOnly ? "只读会话不可展开" : isComposerExpanded ? "收起输入框" : "展开输入框"}
              disabled={readOnly}
              onClick={toggleComposerExpansion}
            >
              {isComposerExpanded ? <ChevronDown aria-hidden="true" /> : <ChevronUp aria-hidden="true" />}
            </button>
            <div className="composer-bar">
              <div className="composer-bar-left">
                {chatFocusValue !== undefined && onSelectChatFocus ? (
                  // Chat-style (DM/group) threads swap the project control
                  // for the work-focus chip in the same leading slot. The
                  // project pill / "打开项目" button switches the whole
                  // app's project context — activating it from inside a DM
                  // yanks the user out of the conversation — so it must
                  // not render here at all, in either variant; the focus
                  // chip is the only scope control a chat thread offers.
                  <ChatFocusChip
                    value={chatFocusValue}
                    projects={projects}
                    disabled={readOnly}
                    onChange={onSelectChatFocus}
                  />
                ) : variant === "hero" ? (
                  // Hero (empty/unsent) project & 对话 conversations: the
                  // project pill both shows and edits the workspace/cwd. Once
                  // the conversation is sent it drops to the dock variant below,
                  // where the cwd is locked (the backend session.CWD is fixed at
                  // creation) so no cwd control renders at all.
                  <div className="hero-project-pill-anchor composer-project-control" ref={menuRef}>
                    <button
                      className="hero-project-pill"
                      type="button"
                      title={projectPillTitle}
                      aria-haspopup="menu"
                      aria-expanded={menuOpen}
                      aria-label={`切换项目：${projectPillLabel}`}
                      onClick={onToggleMenu}
                    >
                      <span className="hero-project-pill-icon" aria-hidden="true">
                        <ProjectPillIcon />
                      </span>
                      <span className="hero-project-pill-text">{projectPillLabel}</span>
                      <ChevronDown className="hero-project-pill-chevron" aria-hidden="true" />
                    </button>
                    {menuOpen ? (
                      <FloatingMenuPortal
                        anchorRef={menuRef}
                        owner="composer-runtime"
                        placement="above"
                        align="left"
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
                ) : null}
                <button
                  className="composer-tool-button composer-attachment-button"
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
                    className={`permission-chip tone-${permissionOption.chipTone}`}
                    type="button"
                    aria-haspopup="menu"
                    aria-expanded={accessMenuOpen}
                    aria-label={`权限模式：${permissionChipLabel}`}
                    disabled={!initialized || readOnly || running}
                    onClick={onToggleAccessMenu}
                  >
                    <permissionOption.icon aria-hidden="true" />
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
              </div>
              <div className="composer-bar-right">
                <ComposerTokenGauge
                  running={running}
                  tokensPerSecond={tokensPerSecond}
                  sampledAt={tokenSpeedSampledAt}
                  source={tokenSpeedSource}
                />
                <ComposerContextMeter usage={contextUsage ?? undefined} />
                {initialized ? (
                  <RuntimePicker
                    variant={variant}
                    initialized={initialized}
                    state={codexModels}
                    openMenu={codexRuntimeMenu}
                    anchorRef={codexRuntimeRef}
                    running={runtimeControlsDisabled}
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
                {statusText ? (
                  <span className="status-label" title={statusText}>
                    <span
                      className={`status-label-text${statusIsLiveProgress ? " live-progress-chip" : ""}`}
                    >
                      {statusText}
                    </span>
                  </span>
                ) : null}
                <button
                  className={`composer-action-button ${running ? "composer-stop-button" : "composer-send-button"}`}
                  type="button"
                  onClick={running ? onInterrupt : submitComposer}
                  aria-label={running ? "停止" : "发送"}
                  title={running ? "停止" : "发送"}
                  disabled={!running && (readOnly || !hasDraft)}
                >
                  {running ? <Square aria-hidden="true" /> : <Send aria-hidden="true" />}
                </button>
              </div>
            </div>
          </div>
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

function CollapsedComposerPromptCard({
  text,
  onReveal,
  onRemove
}: {
  text: string;
  onReveal: () => void;
  onRemove: () => void;
}): JSX.Element {
  const title = collapsedComposerPromptTitle(text);
  return (
    <div className="composer-collapsed-prompt-card">
      <button
        className="composer-collapsed-prompt-main"
        type="button"
        title={title}
        aria-label={`在文本框中显示折叠长文本：${title}`}
        onClick={onReveal}
      >
        <span className="composer-collapsed-prompt-icon" aria-hidden="true">
          <FileText className="icon" />
        </span>
        <span className="composer-collapsed-prompt-copy">
          <strong className="composer-collapsed-prompt-title">{title}</strong>
          <span className="composer-collapsed-prompt-action">
            在文本框中显示
            <ChevronRight aria-hidden="true" />
          </span>
        </span>
      </button>
      <button
        className="composer-collapsed-prompt-remove"
        type="button"
        aria-label="移除折叠长文本"
        onClick={onRemove}
      >
        <X aria-hidden="true" />
      </button>
    </div>
  );
}

function SlashCommandIcon({ command }: { command: ComposerSlashCommand }): JSX.Element {
  switch (command.action ?? command.id) {
    case "open-review":
    case "review":
      return <Search className="icon" />;
    case "debug":
      return <Bug className="icon" />;
    case "fix":
      return <Hammer className="icon" />;
    case "test":
      return <FlaskConical className="icon" />;
    case "explain":
      return <CircleHelp className="icon" />;
    case "commit":
      return <GitCommitHorizontal className="icon" />;
    case "pr":
      return <GitPullRequest className="icon" />;
    case "open-skills":
      return <Wrench className="icon" />;
    case "new-thread":
      return <MessageSquarePlus className="icon" />;
    case "open-terminal":
      return <Terminal className="icon" />;
    case "open-files":
      return <FileText className="icon" />;
    case "open-project":
      return <FolderOpen className="icon" />;
    case "no-project":
      return <FolderX className="icon" />;
    case "context":
      return <PieChart className="icon" />;
    case "fast":
      return <Zap className="icon" />;
    case "model":
    case "effort":
      return <Laptop className="icon" />;
    case "settings":
      return <Settings className="icon" />;
    default:
      return <Wrench className="icon" />;
  }
}

type ComposerMentionDraft = {
  query: string;
  start: number;
};

function parseComposerMentionDraft(value: string): ComposerMentionDraft | undefined {
  const match = /(^|\s)@([^\s@]*)$/.exec(value);
  if (!match) {
    return undefined;
  }
  return {
    query: match[2] ?? "",
    start: match.index + (match[1]?.length ?? 0),
  };
}

function filterMentionParticipants(
  participants: ParticipantProfile[],
  query: string,
): ParticipantProfile[] {
  const normalized = query.trim().toLowerCase();
  const named = participants.filter((participant) => participant.name.trim() !== "");
  if (normalized === "") {
    return named.slice(0, 8);
  }
  return named
    .filter((participant) =>
      [participant.name, participant.role ?? "", participant.tagline ?? ""]
        .join(" ")
        .toLowerCase()
        .includes(normalized),
    )
    .slice(0, 8);
}

function composerRuntimeContextKey(context: RuntimeContext): string {
  return context.kind === "project" ? `project:${context.project_id}` : `no_project:${context.cwd}`;
}

function heroProjectPillLabel(activeContext: RuntimeContext | undefined, activeProject: DesktopProject | undefined): string {
  if (activeContext?.kind === "project") {
    return activeProject?.name ?? "当前项目";
  }
  if (activeContext?.kind === "no_project") {
    // The no-project workspace is surfaced everywhere else — sidebar group,
    // session tab — as "对话", so the draft's cwd control matches that name
    // rather than the older, inconsistent "无项目".
    return "对话";
  }
  return "选择项目";
}

function exactRunnableSlashCommand(commands: ComposerSlashCommand[], draft: ComposerSlashDraft): ComposerSlashCommand | undefined {
  return commands.find(
    (command) =>
      !command.disabledReason &&
      (command.kind === "prompt" || command.kind === "skill") &&
      command.name.toLowerCase() === draft.query
  );
}

function isCollapsibleComposerPrompt(text: string): boolean {
  if (text.trim().length === 0) {
    return false;
  }
  if (text.length > COLLAPSIBLE_COMPOSER_PROMPT_CHAR_THRESHOLD) {
    return true;
  }
  let estimatedLines = 0;
  for (const line of text.split(/\r\n|\r|\n/)) {
    estimatedLines += Math.max(
      1,
      Math.ceil(line.length / COLLAPSIBLE_COMPOSER_PROMPT_SOFT_LINE_CHARS)
    );
    if (estimatedLines > COLLAPSIBLE_COMPOSER_PROMPT_LINE_THRESHOLD) {
      return true;
    }
  }
  return false;
}

function collapsedComposerPromptTitle(text: string): string {
  const firstLine = text
    .split(/\r\n|\r|\n/)
    .map((line) => line.trim())
    .find(Boolean);
  return firstLine || "长文本";
}

function exactActionSlashCommand(commands: ComposerSlashCommand[], draft: ComposerSlashDraft): ComposerSlashCommand | undefined {
  return commands.find(
    (command) =>
      !command.disabledReason &&
      command.kind === "action" &&
      command.name.toLowerCase() === draft.query
  );
}
