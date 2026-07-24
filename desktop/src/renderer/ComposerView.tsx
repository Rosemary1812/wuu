import {
  ChevronDown,
  ChevronRight,
  ChevronUp,
  FileText,
  Folder,
  FolderOpen,
  FolderX,
  Send,
  Square,
  X
} from "lucide-react";
import {
  type ClipboardEvent as ReactClipboardEvent,
  type DragEvent as ReactDragEvent,
  type KeyboardEvent as ReactKeyboardEvent,
  type MouseEvent as ReactMouseEvent,
  type Ref,
  type RefObject,
  type ReactNode,
  useContext,
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
import { translateCurrent as translate, useI18n } from "./i18n";
import {
  WORKSPACE_FILE_DRAG_MIME,
  appendWorkspacePathToPrompt,
  clipboardAttachmentFiles,
  type ComposerFile,
  type ComposerImage,
  type QueuedComposerMessage
} from "./ComposerMessages";
import { FloatingMenuPortal } from "./ComposerFloatingMenu";
import { ComposerContextMenu } from "./ComposerContextMenu";
import { ComposerAttachmentStrip, ComposerQueueStrip } from "./ComposerInputSections";
import { ComposerGoalStrip } from "./ComposerGoalStrip";
import { WorkspaceDocumentDrawerContext } from "./WorkspaceDocumentTurnDock";
import {
  AccessMenu,
  ComposerPlusButton,
  ProjectPickerMenu,
  RuntimePicker,
  SlashCommandIcon,
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

type CollapsedComposerPromptBlock = {
  id: string;
  text: string;
};

type ExpandedComposerDrawer = "goal" | "pending" | null;

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
  mainConversation = false,
  topAccessory,
  containerRef,
  prompt,
  setPrompt,
  files,
  images,
  queuedMessages,
  guideMessages,
  running,
  sendDisabled = false,
  forceStopWhileRunning = false,
  ultraEnabled = false,
  runtimeControlsDisabled = running,
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
  onSteer,
  onQueue,
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
  hideRuntimeControls = false,
  hidePlusButton = false,
  hidePermissionControl = false,
  hideExpandButton = false,
  placeholder,
  maxLength,
  textOnly = false,
  slashCommandsOverride,
  onResetSideThread,
}: {
  variant?: ComposerVariant;
  mainConversation?: boolean;
  topAccessory?: ReactNode;
  containerRef?: Ref<HTMLElement>;
  prompt: string;
  setPrompt: (value: string) => void;
  files: ComposerFile[];
  images: ComposerImage[];
  queuedMessages: QueuedComposerMessage[];
  guideMessages: QueuedComposerMessage[];
  running: boolean;
  sendDisabled?: boolean;
  forceStopWhileRunning?: boolean;
  ultraEnabled?: boolean;
  runtimeControlsDisabled?: boolean;
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
  onSteer?: () => void;
  onQueue?: () => void;
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
  // Suppress the model/context/token runtime chrome on the bar's right edge.
  // Side-thread composers reuse this input without a separate runtime picker.
  hideRuntimeControls?: boolean;
  hidePlusButton?: boolean;
  hidePermissionControl?: boolean;
  hideExpandButton?: boolean;
  // A shared composer can be embedded in a conversation surface whose
  // transport accepts text only. The editor, keyboard handling, expansion,
  // context menu, and send/stop controls remain the canonical Composer; only
  // unsupported attachment, slash-command, and permission affordances
  // are removed.
  textOnly?: boolean;
  placeholder?: string;
  maxLength?: number;
  // Replaces the built-in main-conversation command list with a
  // surface-specific one (e.g. the side chat's /reset). The menu, keyboard
  // handling, and action dispatch stay the canonical Composer machinery, so
  // providing an override re-enables slash input even in textOnly mode.
  slashCommandsOverride?: ComposerSlashCommand[];
  // Reset the side thread this composer is embedded in.
  onResetSideThread?: () => void;
}): JSX.Element {
  const { locale, t } = useI18n();
  const statusText = composerStatusText(status);
  const statusIsLiveProgress = composerStatusIsLiveProgress(statusLiveProgress);
  const className = `composer-wrap ${
    variant === "hero"
      ? "hero-composer-wrap"
      : variant === "document"
        ? "dock-composer-wrap document-composer-wrap"
        : "dock-composer-wrap"
  }`;
  const hasAttachments = images.length > 0 || files.length > 0;
  const hasDraft = prompt.trim().length > 0 || hasAttachments;
  // The action button is a stop control ONLY while a turn runs AND the input
  // is empty. The moment there is something to send, it flips back to a send
  // button — because Enter already queues a draft mid-turn, so the button must
  // match that (queuing "排队发送" rather than interrupting). This keeps the
  // stop affordance for the common "watching a turn, empty input" case while
  // never blocking a queued follow-up the user has clearly typed.
  const showComposerStop = running && (forceStopWhileRunning || !hasDraft);
  const composerSendLabel = running ? t("composer.queueSend") : t("composer.send");
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const composerShellRef = useRef<HTMLDivElement>(null);
  const composerFrameRef = useRef<HTMLDivElement>(null);
  const collapsedComposerFrameHeightRef = useRef<number | null>(null);
  const attachmentInputRef = useRef<HTMLInputElement>(null);
  const collapsedPromptListRef = useRef<HTMLDivElement>(null);
  const collapsedPromptBlockIDRef = useRef(0);
  const submitAfterCompositionRef = useRef(false);
  const documentDrawer = useContext(WorkspaceDocumentDrawerContext);
  const [isComposerExpanded, setIsComposerExpanded] = useState(false);
  const [expandedDrawer, setExpandedDrawer] = useState<ExpandedComposerDrawer>(null);
  const [dropActive, setDropActive] = useState(false);
  const [ultraAnimationCycle, setUltraAnimationCycle] = useState(0);
  const previousUltraEnabledRef = useRef(ultraEnabled);
  const [collapsedPromptBlocks, setCollapsedPromptBlocks] = useState<CollapsedComposerPromptBlock[]>([]);
  const [selectedSlashIndex, setSelectedSlashIndex] = useState(0);
  const [slashDismissedValue, setSlashDismissedValue] = useState("");
  const [compositionSubmitRequest, setCompositionSubmitRequest] = useState(0);
  const hasPendingMessages = guideMessages.length > 0 || queuedMessages.length > 0;
  const hasHeldMessages = [...guideMessages, ...queuedMessages].some((message) => message.held);
  const previousHeldMessagesRef = useRef(false);
  const previousDrawerSessionRef = useRef(queryHistorySessionID);

  useEffect(() => {
    if (previousDrawerSessionRef.current === queryHistorySessionID) {
      return;
    }
    previousDrawerSessionRef.current = queryHistorySessionID;
    previousHeldMessagesRef.current = false;
    setExpandedDrawer(null);
  }, [queryHistorySessionID]);

  useEffect(() => {
    if (documentDrawer?.documentResultExpanded) {
      setExpandedDrawer(null);
    }
  }, [documentDrawer?.documentResultExpanded]);

  useEffect(() => {
    if (hasHeldMessages && !previousHeldMessagesRef.current) {
      setComposerDrawer("pending");
    }
    previousHeldMessagesRef.current = hasHeldMessages;
  }, [hasHeldMessages]);

  useEffect(() => {
    setExpandedDrawer((current) => {
      if (current === "goal" && !goalSummary) return null;
      if (current === "pending" && !hasPendingMessages) return null;
      return current;
    });
  }, [goalSummary, hasPendingMessages]);

  function setComposerDrawer(next: ExpandedComposerDrawer): void {
    setExpandedDrawer(next);
    if (next) {
      documentDrawer?.collapseDocumentResult();
    }
  }

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
    ? t("composer.readOnly")
    : hasCollapsedPromptBlocks
      ? t("composer.followupChanges")
      : hasAttachments
        ? t("composer.addDescription")
        : t("composer.placeholder"));
  const slashDraft =
    textOnly && !slashCommandsOverride ? undefined : parseComposerSlashDraft(prompt);
  const slashQuery = slashDraft?.query ?? "";
  const slashSkillContextKey = activeContext ? composerRuntimeContextKey(activeContext) : "";
  const slashSkillCountKey = initialized?.extension_trust?.main_session?.skills?.count ?? 0;
  const slashRuntimeReady = Boolean(activeContext && initialized);
  const builtinSlashCommands = useMemo(
    () => buildComposerSlashCommands({ activeContext, initialized, running, compactDisabledReason, sideThreadDisabledReason, skills: slashSkills }),
    [activeContext, compactDisabledReason, initialized, locale, running, sideThreadDisabledReason, slashSkills]
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

  function submitComposerWith(onSubmit: () => void): void {
    resetQueryHistoryNavigation();
    const actionCommand = slashDraft
      ? exactActionSlashCommand(slashCommands, slashDraft)
      : undefined;
    if (actionCommand) {
      applySlashCommand(actionCommand, slashDraft);
      return;
    }
    onSubmit();
    focusComposerSoon();
  }

  function submitComposer(): void {
    submitComposerWith(onSend);
  }

  function updateVisiblePrompt(value: string): void {
    resetQueryHistoryNavigation();
    setSlashDismissedValue("");
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

  // A drop carries either a workspace path reference (file tree drag) or
  // external files (Finder/Desktop). Path drops insert plain text and stay
  // available on text-only composers; file drops reuse the paste pipeline.
  // During dragover only dataTransfer.types is readable, so acceptance is
  // decided by MIME alone — anything else keeps its native behavior (e.g.
  // dragging selected text into the textarea).
  function composerDropAcceptsPayload(dataTransfer: DataTransfer): boolean {
    const types = Array.from(dataTransfer.types ?? []);
    if (types.includes(WORKSPACE_FILE_DRAG_MIME)) {
      return true;
    }
    return !textOnly && types.includes("Files");
  }

  function handleComposerDragOver(event: ReactDragEvent<HTMLDivElement>): void {
    if (readOnly || !composerDropAcceptsPayload(event.dataTransfer)) {
      return;
    }
    event.preventDefault();
    event.dataTransfer.dropEffect = "copy";
    setDropActive(true);
  }

  function handleComposerDragLeave(event: ReactDragEvent<HTMLDivElement>): void {
    if (event.currentTarget.contains(event.relatedTarget as Node | null)) {
      return;
    }
    setDropActive(false);
  }

  function handleComposerDrop(event: ReactDragEvent<HTMLDivElement>): void {
    setDropActive(false);
    if (readOnly) {
      return;
    }
    const workspacePath = event.dataTransfer.getData(WORKSPACE_FILE_DRAG_MIME);
    if (workspacePath) {
      event.preventDefault();
      resetQueryHistoryNavigation();
      setSlashDismissedValue("");
      setPrompt(appendWorkspacePathToPrompt(prompt, workspacePath));
      focusComposerAtEndSoon();
      return;
    }
    if (textOnly) {
      return;
    }
    const dropped = Array.from(event.dataTransfer.files ?? []);
    if (dropped.length === 0) {
      return;
    }
    event.preventDefault();
    onPasteAttachmentFiles(dropped);
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
    if (handleQueryHistoryKeyDown(event)) {
      return;
    }
    if (
      event.key === "Tab" &&
      !event.shiftKey &&
      !event.metaKey &&
      !event.ctrlKey &&
      !event.altKey &&
      running &&
      hasDraft &&
      onQueue
    ) {
      event.preventDefault();
      submitComposerWith(onQueue);
      return;
    }
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      submitComposerWith(running && hasDraft && onSteer ? onSteer : onSend);
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
      {topAccessory ? <div className="composer-top-accessory">{topAccessory}</div> : null}
      <div className="composer-shell" ref={composerShellRef}>
        {slashMenuOpen ? (
          <div className="slash-command-menu" id={slashMenuID} role="listbox" aria-label={t("composer.slashCommands")}>
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
              <div className="slash-command-empty">{t("composer.noMatchingCommand", { query: slashQuery })}</div>
            )}
          </div>
        ) : null}
        <ComposerGoalStrip
          summary={goalSummary ?? null}
          disabled={readOnly}
          expanded={expandedDrawer === "goal"}
          onExpandedChange={(expanded) => setComposerDrawer(expanded ? "goal" : null)}
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
          expanded={expandedDrawer === "pending"}
          onExpandedChange={(expanded) => setComposerDrawer(expanded ? "pending" : null)}
          onRemoveGuideMessage={onRemoveGuideMessage}
          onRemoveQueuedMessage={onRemoveQueuedMessage}
          onGuideQueuedMessage={onGuideQueuedMessage}
          onEditGuideMessage={onEditGuideMessage}
          onEditQueuedMessage={onEditQueuedMessage}
        />
        <div className="composer-frame-shell">
          {onToggleUltra ? (
            <>
              <button
                className={`composer-ultra-button${ultraEnabled ? " is-active" : ""}`}
                type="button"
                aria-label={ultraEnabled ? t("composer.disableUltraMode") : t("composer.enableUltraMode")}
                aria-pressed={ultraEnabled}
                title={ultraEnabled ? t("composer.disableUltra") : t("composer.enableUltra")}
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
            className={`composer-frame${ultraEnabled ? " is-ultra" : ""}${dropActive ? " composer-frame-drop-active" : ""}`}
            ref={composerFrameRef}
            onDragOver={handleComposerDragOver}
            onDragLeave={handleComposerDragLeave}
            onDrop={handleComposerDrop}
          >
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
              <div className="composer-collapsed-prompt-list" ref={collapsedPromptListRef} aria-label={t("composer.collapsedLongText")}>
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
              maxLength={maxLength}
              disabled={readOnly}
              aria-readonly={readOnly}
              aria-controls={slashMenuOpen ? slashMenuID : undefined}
              aria-activedescendant={
                selectedSlashCommand
                  ? `${slashMenuID}-${selectedSlashCommand.id}`
                  : undefined
              }
              aria-expanded={slashMenuOpen || undefined}
              onChange={(event) => {
                updateVisiblePrompt(event.target.value);
              }}
              onPaste={handleComposerPaste}
              onBlur={() => {
                if (slashMenuOpen) {
                  setSlashDismissedValue(prompt);
                }
              }}
              onKeyDown={handleComposerKeyDown}
              onContextMenu={handleComposerContextMenu}
            />
            {!hideExpandButton ? (
              <button
                className="composer-expand-button"
                type="button"
                aria-label={isComposerExpanded ? t("composer.collapseInput") : t("composer.expandInput")}
                aria-pressed={isComposerExpanded}
                title={readOnly ? t("composer.readOnlyCannotExpand") : isComposerExpanded ? t("composer.collapseInput") : t("composer.expandInput")}
                disabled={readOnly}
                onClick={toggleComposerExpansion}
              >
                {isComposerExpanded ? <ChevronDown aria-hidden="true" /> : <ChevronUp aria-hidden="true" />}
              </button>
            ) : null}
            <div className="composer-bar">
              <div className="composer-bar-left">
                {variant === "hero" ? (
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
                      aria-label={t("composer.switchProject", { project: projectPillLabel })}
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
                {!textOnly && !hidePlusButton ? (
                  <ComposerPlusButton
                    variant={variant}
                    disabled={readOnly}
                    commands={slashCommands}
                    menuAnchorRef={composerShellRef}
                    onAddAttachment={() => attachmentInputRef.current?.click()}
                    onSelectCommand={(command) => applySlashCommand(command, undefined)}
                  />
                ) : null}
                {!textOnly && !hidePermissionControl ? (
                  <div className="permission-menu-anchor" ref={accessMenuRef}>
                    <button
                      className={`permission-chip tone-${permissionOption.chipTone}`}
                      type="button"
                      aria-haspopup="menu"
                      aria-expanded={accessMenuOpen}
                      aria-label={t("composer.permissionMode", { mode: permissionChipLabel })}
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
                ) : null}
              </div>
              <div className="composer-bar-right">
                {hideRuntimeControls ? null : (
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
                  aria-label={showComposerStop ? t("composer.stop") : composerSendLabel}
                  title={showComposerStop ? t("composer.stop") : composerSendLabel}
                  disabled={!showComposerStop && (sendDisabled || readOnly || !hasDraft)}
                >
                  {showComposerStop ? <Square aria-hidden="true" /> : <Send aria-hidden="true" />}
                </button>
              </div>
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
    <div
      className={className}
      data-main-conversation-composer={mainConversation ? variant : undefined}
    >
      {content}
    </div>
  ) : (
    <footer
      className={className}
      data-main-conversation-composer={mainConversation ? variant : undefined}
      ref={containerRef}
    >
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
        aria-label={translate("composer.showCollapsedTextNamed", { title })}
        onClick={onReveal}
      >
        <span className="composer-collapsed-prompt-icon" aria-hidden="true">
          <FileText className="icon" />
        </span>
        <span className="composer-collapsed-prompt-copy">
          <strong className="composer-collapsed-prompt-title">{title}</strong>
          <span className="composer-collapsed-prompt-action">
            {translate("composer.showInTextBox")}
            <ChevronRight aria-hidden="true" />
          </span>
        </span>
      </button>
      <button
        className="composer-collapsed-prompt-remove"
        type="button"
        aria-label={translate("composer.removeCollapsedText")}
        onClick={onRemove}
      >
        <X aria-hidden="true" />
      </button>
    </div>
  );
}

function composerRuntimeContextKey(context: RuntimeContext): string {
  return context.kind === "project" ? `project:${context.project_id}` : `no_project:${context.cwd}`;
}

function heroProjectPillLabel(activeContext: RuntimeContext | undefined, activeProject: DesktopProject | undefined): string {
  if (activeContext?.kind === "project") {
    return activeProject?.name ?? translate("composer.currentProject");
  }
  if (activeContext?.kind === "no_project") {
    // The no-project workspace is surfaced everywhere else — sidebar group,
    // session tab — as "对话", so the draft's cwd control matches that name
    // rather than the older, inconsistent "无项目".
    return translate("composer.conversation");
  }
  return translate("composer.selectProject");
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
  return firstLine || translate("composer.longText");
}

function exactActionSlashCommand(commands: ComposerSlashCommand[], draft: ComposerSlashDraft): ComposerSlashCommand | undefined {
  return commands.find(
    (command) =>
      !command.disabledReason &&
      command.kind === "action" &&
      command.name.toLowerCase() === draft.query
  );
}
