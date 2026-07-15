import {
  AtSign,
  Brain,
  Bug,
  ChevronDown,
  ChevronRight,
  ChevronUp,
  CircleHelp,
  Cpu,
  FileText,
  FlaskConical,
  FoldVertical,
  Folder,
  FolderOpen,
  FolderX,
  Gauge,
  GitCommitHorizontal,
  GitCompare,
  GitPullRequest,
  Hammer,
  LifeBuoy,
  MessageSquarePlus,
  Paperclip,
  PieChart,
  Puzzle,
  RotateCcw,
  ScrollText,
  Search,
  Send,
  Settings,
  Slash,
  Square,
  Terminal,
  X,
  Zap
} from "lucide-react";
import {
  type ClipboardEvent as ReactClipboardEvent,
  type KeyboardEvent as ReactKeyboardEvent,
  type MouseEvent as ReactMouseEvent,
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
  ParticipantSummary,
  RuntimeContext,
  SkillSummary,
  Turn
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
import { ComposerContextMenu } from "./ComposerContextMenu";
import { ComposerAttachmentStrip, ComposerQueueStrip } from "./ComposerInputSections";
import { ComposerGoalStrip } from "./ComposerGoalStrip";
import {
  AccessMenu,
  ChatFocusChip,
  ProjectPickerMenu,
  RuntimePicker,
  permissionModeFromSummary,
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
import { GroupMembersCapsule } from "./GroupMembersCapsule";

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
  ultraEnabled = false,
  runtimeControlsDisabled = running,
  activeTurn,
  status,
  statusLiveProgress,
  readOnly,
  initialized,
  projects,
  activeContext,
  activeProject,
  compactDisabledReason,
  sideThreadDisabledReason,
  codexModels,
  codexRuntimeMenu,
  codexRuntimeRef,
  menuOpen,
  accessMenuOpen,
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
  onOpenSettings,
  onOpenSkillsCatalog,
  onSelectProject,
  onSelectNoProject,
  onCreateProject,
  onOpenProject,
  onStartNewThread,
  onOpenSideThread,
  onOpenWorkspaceTool,
  onOpenContextComposition = () => {},
  onCompactContext = () => {},
  onOpenInstructions = () => {},
  onOpenMemorySettings = () => {},
  onPasteAttachmentFiles,
  onRemoveFile,
  onRemoveImage,
  onRemoveQueuedMessage,
  onRemoveGuideMessage,
  onGuideQueuedMessage,
  onEditQueuedMessage,
  onToggleUltra,
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
  onSelectChatFocus,
  groupMembers,
  onOpenGroupInfo,
  hideRuntimeControls = false,
  placeholder,
  textOnly = false,
  slashCommandsOverride,
  onResetSideThread
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
  ultraEnabled?: boolean;
  runtimeControlsDisabled?: boolean;
  activeTurn?: Pick<Turn, "model_provider" | "model">;
  status: string;
  statusLiveProgress?: boolean;
  readOnly: boolean;
  initialized?: InitializeResult;
  gitStatus?: GitStatusResult;
  projects: DesktopProject[];
  activeContext?: RuntimeContext;
  activeProject?: DesktopProject;
  compactDisabledReason?: string;
  sideThreadDisabledReason?: string;
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
  // Open or focus the side thread attached to the active main conversation.
  onOpenSideThread?: () => void;
  onOpenWorkspaceTool: (view: WorkspacePanelView) => void;
  onOpenContextComposition?: () => void;
  onCompactContext?: () => void;
  onOpenInstructions?: () => void;
  // 打开 设置 → 记忆（/memory 指令）。
  onOpenMemorySettings?: () => void;
  onPasteAttachmentFiles: (files: File[]) => void;
  onRemoveFile: (id: string) => void;
  onRemoveImage: (id: string) => void;
  onRemoveQueuedMessage: (id: string) => void;
  onRemoveGuideMessage: (id: string) => void;
  onGuideQueuedMessage: (id: string) => void;
  onEditQueuedMessage: (id: string) => void;
  onToggleUltra?: (enabled: boolean) => void;
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
  groupMembers?: ParticipantSummary[];
  onOpenGroupInfo?: () => void;
  // Suppress the model/context/token runtime chrome on the bar's right edge.
  // The split reply panel reuses this same composer for a cth, where the
  // token gauge / context meter / model picker are meaningless — only the
  // 附件/命令/盾牌 controls and the send button carry over.
  hideRuntimeControls?: boolean;
  // A shared composer can be embedded in a conversation surface whose
  // transport accepts text only. The editor, keyboard handling, expansion,
  // context menu, and send/stop controls remain the canonical Composer; only
  // unsupported attachment, slash-command, mention, and permission affordances
  // are removed.
  textOnly?: boolean;
  placeholder?: string;
  // Replaces the built-in main-conversation command list with a
  // surface-specific one (e.g. the side chat's /reset). The menu, keyboard
  // handling, and action dispatch stay the canonical Composer machinery, so
  // providing an override re-enables slash input even in textOnly mode.
  slashCommandsOverride?: ComposerSlashCommand[];
  // Reset the side thread this composer is embedded in.
  onResetSideThread?: () => void;
}): JSX.Element {
  const statusText = composerStatusText(status);
  const statusIsLiveProgress = composerStatusIsLiveProgress(statusLiveProgress);
  const className = `composer-wrap ${variant === "hero" ? "hero-composer-wrap" : "dock-composer-wrap"}`;
  const hasAttachments = images.length > 0 || files.length > 0;
  const hasDraft = prompt.trim().length > 0 || hasAttachments;
  // The action button is a stop control ONLY while a turn runs AND the input
  // is empty. The moment there is something to send, it flips back to a send
  // button — because Enter already queues a draft mid-turn, so the button must
  // match that (queuing "排队发送" rather than interrupting). This keeps the
  // stop affordance for the common "watching a turn, empty input" case while
  // never blocking a queued follow-up the user has clearly typed.
  const showComposerStop = running && !hasDraft;
  const composerSendLabel = running ? "排队发送" : "发送";
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const composerShellRef = useRef<HTMLDivElement>(null);
  const composerFrameRef = useRef<HTMLDivElement>(null);
  const collapsedComposerFrameHeightRef = useRef<number | null>(null);
  const attachmentInputRef = useRef<HTMLInputElement>(null);
  const collapsedPromptListRef = useRef<HTMLDivElement>(null);
  const collapsedPromptBlockIDRef = useRef(0);
  const [isComposerExpanded, setIsComposerExpanded] = useState(false);
  const [ultraAnimationCycle, setUltraAnimationCycle] = useState(0);
  const previousUltraEnabledRef = useRef(ultraEnabled);
  const [collapsedPromptBlocks, setCollapsedPromptBlocks] = useState<CollapsedComposerPromptBlock[]>([]);
  const [selectedSlashIndex, setSelectedSlashIndex] = useState(0);
  const [slashDismissedValue, setSlashDismissedValue] = useState("");
  const [selectedMentionIndex, setSelectedMentionIndex] = useState(0);
  const [mentionDismissedValue, setMentionDismissedValue] = useState("");
  useEffect(() => {
    if (previousUltraEnabledRef.current === ultraEnabled) {
      return;
    }
    previousUltraEnabledRef.current = ultraEnabled;
    setUltraAnimationCycle((cycle) => cycle + 1);
  }, [ultraEnabled]);
  const [slashSkills, setSlashSkills] = useState<SkillSummary[]>([]);
  const [composerContextMenu, setComposerContextMenu] = useState<{
    x: number;
    y: number;
    hasSelection: boolean;
  } | null>(null);
  const collapsedPromptPrefix = useMemo(
    () => collapsedPromptBlocks.map((block) => block.text).join(""),
    [collapsedPromptBlocks]
  );
  const hasCollapsedPromptBlocks = collapsedPromptBlocks.length > 0 && prompt.startsWith(collapsedPromptPrefix);
  const activeCollapsedPromptBlocks = hasCollapsedPromptBlocks ? collapsedPromptBlocks : [];
  const visiblePromptValue = hasCollapsedPromptBlocks
    ? prompt.slice(collapsedPromptPrefix.length)
    : prompt;
  const composerPlaceholder = placeholder ?? (readOnly
    ? "子任务会话只读"
    : hasCollapsedPromptBlocks
      ? "要求后续变更"
      : hasAttachments
        ? "添加描述"
        : "向 wuu 提问，或输入 / 选择命令");
  const slashDraft =
    textOnly && !slashCommandsOverride ? undefined : parseComposerSlashDraft(prompt);
  const slashQuery = slashDraft?.query ?? "";
  const slashSkillContextKey = activeContext ? composerRuntimeContextKey(activeContext) : "";
  const slashSkillCountKey = initialized?.extension_trust?.main_session?.skills?.count ?? 0;
  const slashRuntimeReady = Boolean(activeContext && initialized);
  const builtinSlashCommands = useMemo(
    () => buildComposerSlashCommands({ activeContext, initialized, running, compactDisabledReason, sideThreadDisabledReason, skills: slashSkills }),
    [activeContext, compactDisabledReason, initialized, running, sideThreadDisabledReason, slashSkills]
  );
  const slashCommands = slashCommandsOverride ?? builtinSlashCommands;
  const fastModelTarget = useMemo(() => runtimeFastModelTarget(initialized), [initialized]);
  const permissionMode = permissionModeFromSummary(initialized?.permissions);
  const permissionOption = permissionModeOption(permissionMode);
  const permissionChipLabel = permissionOption.chipLabel;
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
  const mentionDraft = textOnly ? undefined : parseComposerMentionDraft(visiblePromptValue);
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
    if (!slashRuntimeReady || readOnly || textOnly) {
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
  }, [readOnly, slashRuntimeReady, slashSkillContextKey, slashSkillCountKey, textOnly]);

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

  useLayoutEffect(() => {
    const shell = composerShellRef.current;
    if (!shell || !slashMenuOpen) {
      shell?.style.removeProperty("--slash-command-available-height");
      return;
    }

    const updateAvailableHeight = (): void => {
      const menuGap = variant === "hero" ? 10 : 8;
      const titlebar =
        shell.closest(".conversation-pane")?.querySelector<HTMLElement>(":scope > .titlebar") ??
        document.querySelector<HTMLElement>(".titlebar");
      const menuTopGutter = 8;
      const safeTop = (titlebar?.getBoundingClientRect().bottom ?? 0) + menuTopGutter;
      const availableHeight = Math.max(
        0,
        Math.floor(shell.getBoundingClientRect().top - menuGap - safeTop)
      );
      shell.style.setProperty("--slash-command-available-height", `${availableHeight}px`);
    };

    updateAvailableHeight();
    window.addEventListener("resize", updateAvailableHeight);
    const resizeObserver = typeof ResizeObserver === "undefined"
      ? null
      : new ResizeObserver(updateAvailableHeight);
    resizeObserver?.observe(shell);
    const titlebar =
      shell.closest(".conversation-pane")?.querySelector<HTMLElement>(":scope > .titlebar") ??
      document.querySelector<HTMLElement>(".titlebar");
    if (titlebar) {
      resizeObserver?.observe(titlebar);
    }

    return () => {
      window.removeEventListener("resize", updateAvailableHeight);
      resizeObserver?.disconnect();
      shell.style.removeProperty("--slash-command-available-height");
    };
  }, [slashMenuOpen, variant]);

  function focusComposerSoon(): void {
    window.requestAnimationFrame(() => textareaRef.current?.focus());
  }

  function focusComposerAtEndSoon(): void {
    window.requestAnimationFrame(() => {
      const textarea = textareaRef.current;
      if (!textarea) {
        return;
      }
      textarea.focus();
      const end = textarea.value.length;
      textarea.setSelectionRange(end, end);
    });
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
    if (!textOnly) {
      const pasted = clipboardAttachmentFiles(event);
      if (pasted.length > 0) {
        event.preventDefault();
        onPasteAttachmentFiles(pasted);
        return;
      }
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
    focusComposerAtEndSoon();
  }

  function applySlashCommand(command: ComposerSlashCommand | undefined, draft: ComposerSlashDraft | undefined): void {
    if (!command || command.disabledReason) {
      return;
    }
    setSlashDismissedValue("");
    if (command.kind === "prompt" || command.kind === "skill") {
      setPrompt(composerSlashPrompt(command, draft?.args ?? ""));
      focusComposerAtEndSoon();
      return;
    }
    setPrompt("");
    switch (command.action) {
      case "new-thread":
        onStartNewThread();
        break;
      case "open-side-thread":
        onOpenSideThread?.();
        break;
      case "reset-side-thread":
        onResetSideThread?.();
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
      case "compact":
        onCompactContext();
        break;
      case "instructions":
        onOpenInstructions();
        break;
      case "open-memory":
        onOpenMemorySettings();
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

  // Suppress the browser's native context menu on the textarea and open
  // our composer context menu anchored at the cursor instead. We still
  // allow the gesture in a read-only composer so the user can copy
  // existing prompt text out — copy/select-all remain enabled; only
  // mutating actions (cut/paste/delete) are blocked.
  function handleComposerContextMenu(event: ReactMouseEvent<HTMLTextAreaElement>): void {
    event.preventDefault();
    const textarea = textareaRef.current;
    const hasSelection = textarea
      ? textarea.selectionStart !== textarea.selectionEnd
      : false;
    setComposerContextMenu({
      x: event.clientX,
      y: event.clientY,
      hasSelection
    });
  }

  const content = (
    <div className={`composer-stack${isComposerExpanded ? " is-expanded" : ""}`}>
      <div className="composer-shell" ref={composerShellRef}>
        {slashMenuOpen ? (
          <div className="slash-command-menu" id={slashMenuID} role="listbox" aria-label="斜杠命令">
            {visibleSlashCommands.length > 0 ? (
              <>
                <div className="slash-command-list scrollbar-hidden">
                  {visibleSlashCommands.map((command, index) => {
                    const selected = index === selectedSlashIndex;
                    const optionID = `${slashMenuID}-${command.id}`;
                    return (
                      <button
                        className={`slash-command-item${selected ? " selected" : ""}`}
                        data-command-name={command.name}
                        id={optionID}
                        key={command.id}
                        role="option"
                        type="button"
                        aria-selected={selected}
                        disabled={Boolean(command.disabledReason)}
                        title={command.disabledReason ?? command.description}
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
                        <span className="slash-command-label">
                          <span className="slash-command-title">
                            {command.kind === "skill" ? command.description : command.title}
                          </span>
                        </span>
                        {command.disabledReason ? (
                          <span className="slash-command-meta">{command.disabledReason}</span>
                        ) : null}
                      </button>
                    );
                  })}
                </div>
              </>
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
        {onToggleUltra ? (
          <>
            <button
              className={`composer-ultra-button${ultraEnabled ? " is-active" : ""}`}
              type="button"
              aria-label={ultraEnabled ? "关闭 Ultra 多 agent 模式" : "开启 Ultra 多 agent 模式"}
              aria-pressed={ultraEnabled}
              title={ultraEnabled ? "关闭 Ultra" : "开启 Ultra"}
              onClick={() => onToggleUltra(!ultraEnabled)}
            >
              <span className="composer-ultra-notch" aria-hidden="true" />
              <span className="composer-ultra-impact" aria-hidden="true" />
            </button>
            {ultraAnimationCycle > 0 ? (
              <span
                className={`composer-ultra-energy${ultraEnabled ? " turning-on" : " turning-off"}`}
                key={ultraAnimationCycle}
                aria-hidden="true"
              />
            ) : null}
          </>
        ) : null}
        <div
          className={`composer-frame${ultraEnabled ? " is-ultra" : ""}`}
          ref={composerFrameRef}
        >
          {goalSummary || guideMessages.length > 0 || queuedMessages.length > 0 ? (
            <div className="composer-input-header">
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
            </div>
          ) : null}
          <div className={`composer${hasCollapsedPromptBlocks ? " has-collapsed-prompt" : ""}`}>
            {textOnly ? null : (
              <>
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
              </>
            )}
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
              onContextMenu={handleComposerContextMenu}
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
                {textOnly ? null : (
                  <>
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
                            disabled={!initialized || readOnly || running}
                            onSelect={onSelectPermissionMode}
                          />
                        </FloatingMenuPortal>
                      ) : null}
                    </div>
                  </>
                )}
              </div>
              <div className="composer-bar-right">
                {hideRuntimeControls ? null : groupMembers ? (
                  // Group threads have no primary agent turn, so the token
                  // gauge / context meter / model picker are meaningless
                  // noise there — the members capsule takes their place on
                  // the right edge of the bar instead.
                  <GroupMembersCapsule
                    members={groupMembers}
                    onOpen={onOpenGroupInfo}
                  />
                ) : (
                  <>
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
                        activeTurn={activeTurn}
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
                  className={`composer-action-button ${showComposerStop ? "composer-stop-button" : "composer-send-button"}`}
                  type="button"
                  onClick={showComposerStop ? onInterrupt : submitComposer}
                  aria-label={showComposerStop ? "停止" : composerSendLabel}
                  title={showComposerStop ? "停止" : composerSendLabel}
                  disabled={!showComposerStop && (readOnly || !hasDraft)}
                >
                  {showComposerStop ? <Square aria-hidden="true" /> : <Send aria-hidden="true" />}
                </button>
              </div>
            </div>
          </div>
        </div>
      </div>
      {composerContextMenu ? (
        <ComposerContextMenu
          textareaRef={textareaRef}
          x={composerContextMenu.x}
          y={composerContextMenu.y}
          hasSelection={composerContextMenu.hasSelection}
          disabled={readOnly}
          onClose={() => setComposerContextMenu(null)}
          onValueChange={updateVisiblePrompt}
        />
      ) : null}
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
    case "review":
      return <Search className="icon" />;
    case "open-review":
      return <GitCompare className="icon" />;
    case "debug":
      return <Bug className="icon" />;
    case "fix":
      return <Hammer className="icon" />;
    case "helpme":
      return <LifeBuoy className="icon" />;
    case "test":
      return <FlaskConical className="icon" />;
    case "explain":
      return <CircleHelp className="icon" />;
    case "commit":
      return <GitCommitHorizontal className="icon" />;
    case "pr":
      return <GitPullRequest className="icon" />;
    case "open-skills":
      return <Puzzle className="icon" />;
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
    case "reset-side-thread":
      return <RotateCcw className="icon" />;
    case "context":
      return <PieChart className="icon" />;
    case "compact":
      return <FoldVertical className="icon" />;
    case "instructions":
      return <ScrollText className="icon" />;
    case "open-memory":
      return <Brain className="icon" />;
    case "fast":
      return <Zap className="icon" />;
    case "model":
      return <Cpu className="icon" />;
    case "effort":
      return <Gauge className="icon" />;
    case "settings":
      return <Settings className="icon" />;
    default:
      return <Puzzle className="icon" />;
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
