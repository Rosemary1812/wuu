import {
  AlertCircle,
  ArrowLeft,
  Brain,
  Bug,
  Check,
  ChevronDown,
  ChevronRight,
  Copy,
  CornerDownRight,
  Clock,
  Globe2,
  FileText,
  Folder,
  FolderX,
  FolderOpen,
  FolderPlus,
  Github,
  GitBranch,
  Info,
  Laptop,
  List as ListIcon,
  MessageSquarePlus,
  MoreHorizontal,
  PanelBottomOpen,
  PanelLeftOpen,
  PanelRightOpen,
  Pencil,
  Plus,
  RefreshCw,
  Search,
  Send,
  Settings,
  ShieldCheck,
  Square,
  Terminal,
  Trash2,
  Wrench,
  X
} from "lucide-react";
import { preparePresortedFileTreeInput } from "@pierre/trees";
import { FileTree, useFileTree, useFileTreeSelection } from "@pierre/trees/react";
import {
  type CSSProperties,
  type ClipboardEvent as ReactClipboardEvent,
  type FormEvent as ReactFormEvent,
  type KeyboardEvent as ReactKeyboardEvent,
  type PointerEvent as ReactPointerEvent,
  type RefObject,
  type ReactNode,
  memo,
  useEffect,
  useMemo,
  useRef,
  useState
} from "react";
import type { PartialOptions } from "overlayscrollbars";
import { OverlayScrollbarsComponent } from "overlayscrollbars-react";
import type {
  AppServerNotification,
  AskUserQuestion,
  AskUserResponse,
  CodexModelSummary,
  DesktopProject,
  FileTreeListResult,
  GitCommitResult,
  GitPullRequestResult,
  GitStatusResult,
  InputImage,
  InitializeResult,
  ProjectListResult,
  RuntimeContext,
  ServerEvent,
  Thread,
  ThreadItem,
  Turn,
  WorkspaceFileReadResult
} from "../shared/protocol";
import { RichContent } from "./RichContent";
import { StreamingMarkdown } from "./StreamingMarkdown";
import { streamTextKey, streamTextStore, type StreamTextField } from "./StreamText";

type AskRequestState = {
  id: string;
  questions: AskUserQuestion[];
};

type CodexModelLoadState = {
  provider?: string;
  loading: boolean;
  error: string;
  models: CodexModelSummary[];
};

type CodexRuntimeMenu = "main" | "model" | null;
type EnvironmentPanelMenu = "mode" | "branch" | "sources" | null;
type EnvironmentDialog = "commit" | "pull-request" | null;
type RunDebugEventSource = "client" | "server";
type RunDebugEventTone = "info" | "running" | "success" | "warning" | "error";
type RunDebugPhaseTone = "idle" | "running" | "success" | "warning" | "error";

type ComposerImage = InputImage & {
  id: string;
};

type QueuedComposerMessage = {
  id: string;
  text: string;
  images: ComposerImage[];
};

type EnvironmentSourceItem = {
  id: string;
  icon: "project" | "temporary" | "file" | "image" | "queue" | "guide";
  title: string;
  detail: string;
};

type RunDebugEvent = {
  id: number;
  at: number;
  source: RunDebugEventSource;
  method: string;
  detail: string;
  tone: RunDebugEventTone;
  threadID?: string;
  turnID?: string;
  itemID?: string;
};

type RunDebugPhase = {
  label: string;
  detail: string;
  tone: RunDebugPhaseTone;
  turn?: Turn;
  activeItem?: ThreadItem;
};

type TurnProgressContent = {
  label: string;
  detail?: string;
};

type TurnProgressScene = "duel" | "chase" | "kick" | "bonk";

const TURN_PROGRESS_SCENES: TurnProgressScene[] = ["duel", "chase", "kick", "bonk"];

type AppState = {
  initialized?: InitializeResult;
  projects: DesktopProject[];
  activeContext?: RuntimeContext;
  activeProjectId?: string;
  gitStatus?: GitStatusResult;
  thread?: Thread;
  threads: Thread[];
  running: boolean;
  status: string;
  askRequest?: AskRequestState;
};

const initialState: AppState = {
  projects: [],
  threads: [],
  running: false,
  status: "connecting"
};

const SIDEBAR_DEFAULT_WIDTH = 326;
const SIDEBAR_MIN_WIDTH = 240;
const SIDEBAR_MAX_WIDTH = 520;
const SIDEBAR_STEP = 24;
const SIDEBAR_WIDTH_KEY = "wuu.desktop.sidebarWidth";
const SIDEBAR_COLLAPSED_KEY = "wuu.desktop.sidebarCollapsed";
const CONVERSATION_AUTO_SCROLL_THRESHOLD_PX = 48;
const IMAGE_MAX_DIMENSION = 2000;
const IMAGE_TARGET_BYTES = (5 * 1024 * 1024 * 3) / 4;
const ENABLE_LAUNCH_PREVIEW = Boolean((import.meta as ImportMeta & { env?: { DEV?: boolean } }).env?.DEV);
const WORKSPACE_FILE_TREE_STYLE: CSSProperties = {
  contain: "strict",
  height: "100%",
  minHeight: 0,
  minWidth: 0,
  width: "100%"
};
const WORKSPACE_FILE_TREE_ITEM_HEIGHT = 28;

installFileTreeResizeObserverGate();

function installFileTreeResizeObserverGate(): void {
  if (typeof window === "undefined" || typeof ResizeObserver === "undefined") {
    return;
  }

  const resizeWindow = window as typeof window & { __wuuFileTreeResizeObserverGate?: boolean };
  if (resizeWindow.__wuuFileTreeResizeObserverGate) {
    return;
  }
  resizeWindow.__wuuFileTreeResizeObserverGate = true;

  const NativeResizeObserver = window.ResizeObserver;
  const pendingObservers = new Set<FileTreeResizeObserverGate>();

  class FileTreeResizeObserverGate implements ResizeObserver {
    private readonly observer: ResizeObserver;
    private readonly callback: ResizeObserverCallback;
    private readonly pendingEntries = new Map<Element, ResizeObserverEntry>();
    private readonly lastResizeBucketByTarget = new WeakMap<Element, string>();

    constructor(callback: ResizeObserverCallback) {
      this.callback = callback;
      this.observer = new NativeResizeObserver((entries, observer) => {
        const deliverNow: ResizeObserverEntry[] = [];
        const resizing = document.documentElement.classList.contains("window-resizing");

        for (const entry of entries) {
          if (!resizing || !isWorkspaceFileTreeResizeTarget(entry.target)) {
            deliverNow.push(entry);
            continue;
          }

          const target = entry.target;
          const nextBucket = fileTreeResizeBucket(entry);
          const previousBucket = this.lastResizeBucketByTarget.get(target);
          this.pendingEntries.set(target, entry);
          pendingObservers.add(this);

          if (previousBucket === nextBucket) {
            continue;
          }

          this.lastResizeBucketByTarget.set(target, nextBucket);
          this.pendingEntries.delete(target);
          deliverNow.push(entry);
        }

        if (deliverNow.length > 0) {
          callback(deliverNow, observer);
        }
      });
    }

    observe(target: Element, options?: ResizeObserverOptions): void {
      this.observer.observe(target, options);
    }

    unobserve(target: Element): void {
      this.pendingEntries.delete(target);
      this.observer.unobserve(target);
    }

    disconnect(): void {
      this.pendingEntries.clear();
      pendingObservers.delete(this);
      this.observer.disconnect();
    }

    flushPending(): void {
      if (this.pendingEntries.size === 0) {
        pendingObservers.delete(this);
        return;
      }

      const entries = [...this.pendingEntries.values()];
      this.pendingEntries.clear();
      pendingObservers.delete(this);
      this.callback(entries, this.observer);
    }
  }

  window.ResizeObserver = FileTreeResizeObserverGate;
  window.addEventListener("wuu-window-resize-end", () => {
    for (const observer of [...pendingObservers]) {
      observer.flushPending();
    }
  });
}

function isWorkspaceFileTreeResizeTarget(target: Element): boolean {
  if (!target.matches('[data-file-tree-virtualized-scroll="true"]')) {
    return false;
  }

  const root = target.getRootNode();
  if (!(root instanceof ShadowRoot)) {
    return false;
  }

  return root.host instanceof Element && Boolean(root.host.closest(".workspace-file-tree-frame"));
}

function fileTreeResizeBucket(entry: ResizeObserverEntry): string {
  const width = Math.round(entry.contentRect.width);
  const rowBucket = Math.floor(entry.contentRect.height / WORKSPACE_FILE_TREE_ITEM_HEIGHT);
  return `${width}:${rowBucket}`;
}

type SidebarResizeSession = {
  startX: number;
  startWidth: number;
};

type ComposerVariant = "dock" | "hero";
type WorkspacePanelView = "files" | "chat" | "browser" | "review" | "terminal";

const WORKSPACE_TOOL_ITEMS: Array<{
  id: WorkspacePanelView;
  title: string;
  subtitle: string;
}> = [
  { id: "files", title: "文件", subtitle: "浏览项目文件" },
  { id: "chat", title: "侧边聊天", subtitle: "发起侧边对话" },
  { id: "browser", title: "浏览器", subtitle: "打开网站" },
  { id: "review", title: "审查", subtitle: "查看代码更改" },
  { id: "terminal", title: "终端", subtitle: "运行项目命令" }
];

const WORKSPACE_TREE_CSS = `
  :host {
    --trees-fg-override: #34393d;
    --trees-muted-fg-override: #7a8085;
    --trees-selected-bg-override: #eeeeeb;
    --trees-hover-bg-override: #f4f4f2;
    --trees-border-color-override: transparent;
    font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    font-size: 13px;
    line-height: 1.35;
  }

  button[data-type="item"] {
    border-radius: 7px;
  }
`;

const OVERLAY_SCROLLBAR_OPTIONS = {
  scrollbars: {
    autoHide: "leave",
    autoHideDelay: 360,
    clickScroll: true,
    theme: "os-theme-wuu"
  }
} satisfies PartialOptions;

function initialSidebarWidth(): number {
  const stored = Number(window.localStorage.getItem(SIDEBAR_WIDTH_KEY));
  if (!Number.isFinite(stored)) {
    return SIDEBAR_DEFAULT_WIDTH;
  }
  return clamp(stored, SIDEBAR_MIN_WIDTH, SIDEBAR_MAX_WIDTH);
}

function initialSidebarCollapsed(): boolean {
  return window.localStorage.getItem(SIDEBAR_COLLAPSED_KEY) === "true";
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}

function isCodexProvider(initialized: InitializeResult): boolean {
  const summary = initialized.providers?.find((provider) => provider.name === initialized.provider);
  const type = (summary?.type ?? initialized.provider).trim().toLowerCase().replaceAll("_", "-");
  return type === "openai-codex" || type === "codex-subscription" || type === "chatgpt-codex";
}

function displayCodexModelName(model?: CodexModelSummary): string {
  return model?.display_name || model?.slug || "GPT";
}

function shortCodexModelLabel(model: string): string {
  return model.replace(/^gpt-/i, "");
}

function codexEffortLabel(effort: string): string {
  switch (effort) {
    case "":
      return "智能";
    case "none":
      return "无";
    case "minimal":
      return "最少";
    case "low":
      return "低";
    case "medium":
      return "中";
    case "high":
      return "高";
    case "xhigh":
    case "max":
      return "超高";
    default:
      return effort;
  }
}

function codexEffortOptions(model: CodexModelSummary | undefined, currentEffort: string): string[] {
  const defaults = ["low", "medium", "high", "xhigh"];
  const supported = (model?.supported_reasoning?.length ? model.supported_reasoning : defaults).filter(Boolean);
  const options = ["", ...supported];
  if (currentEffort && !options.includes(currentEffort)) {
    options.push(currentEffort);
  }
  return options;
}

function normalizedEffortForModel(currentEffort: string, model: CodexModelSummary): string {
  if (currentEffort === "") {
    return "";
  }
  const supported = model.supported_reasoning ?? [];
  if (supported.length === 0 || supported.includes(currentEffort)) {
    return currentEffort;
  }
  if (model.default_reasoning_level && supported.includes(model.default_reasoning_level)) {
    return model.default_reasoning_level;
  }
  return supported[0] ?? "";
}

function clipboardImageFiles(event: ReactClipboardEvent<HTMLTextAreaElement>): File[] {
  const items = Array.from(event.clipboardData?.items ?? []);
  const files: File[] = [];
  for (const item of items) {
    if (item.kind !== "file" || !item.type.toLowerCase().startsWith("image/")) {
      continue;
    }
    const file = item.getAsFile();
    if (file) {
      files.push(file);
    }
  }
  return files;
}

async function composerImageFromFile(file: File): Promise<ComposerImage> {
  const image = await normalizeImageFileForPrompt(file);
  return {
    id: nextComposerImageID(),
    ...image
  };
}

async function normalizeImageFileForPrompt(file: File): Promise<InputImage> {
  const mediaType = normalizeImageMediaType(file.type);
  const original = await file.arrayBuffer();
  const passthrough = async (): Promise<InputImage> => ({
    media_type: mediaType,
    data: arrayBufferToBase64(original)
  });

  try {
    const bitmap = await createImageBitmap(new Blob([original], { type: mediaType }));
    try {
      if (original.byteLength <= IMAGE_TARGET_BYTES && bitmap.width <= IMAGE_MAX_DIMENSION && bitmap.height <= IMAGE_MAX_DIMENSION) {
        return passthrough();
      }

      const [width, height] = clampImageDimensions(bitmap.width, bitmap.height, IMAGE_MAX_DIMENSION);
      const canvas = document.createElement("canvas");
      canvas.width = width;
      canvas.height = height;
      const context = canvas.getContext("2d");
      if (!context) {
        return passthrough();
      }
      context.drawImage(bitmap, 0, 0, width, height);

      const strategies: Array<{ mediaType: string; quality?: number }> = [
        { mediaType: "image/png" },
        { mediaType: "image/jpeg", quality: 0.82 },
        { mediaType: "image/jpeg", quality: 0.68 },
        { mediaType: "image/jpeg", quality: 0.52 },
        { mediaType: "image/jpeg", quality: 0.38 }
      ];
      let fallback: InputImage | undefined;
      for (const strategy of strategies) {
        const blob = await canvasToBlob(canvas, strategy.mediaType, strategy.quality);
        const encoded = {
          media_type: strategy.mediaType,
          data: arrayBufferToBase64(await blob.arrayBuffer())
        };
        fallback = encoded;
        if (blob.size <= IMAGE_TARGET_BYTES) {
          return encoded;
        }
      }
      return fallback ?? passthrough();
    } finally {
      bitmap.close();
    }
  } catch {
    return passthrough();
  }
}

function normalizeImageMediaType(value: string): string {
  const mediaType = value.trim().toLowerCase();
  if (mediaType === "image/jpg") {
    return "image/jpeg";
  }
  return mediaType.startsWith("image/") ? mediaType : "image/png";
}

function clampImageDimensions(width: number, height: number, maxDimension: number): [number, number] {
  if (width <= maxDimension && height <= maxDimension) {
    return [width, height];
  }
  if (width >= height) {
    return [maxDimension, Math.max(1, Math.round((height * maxDimension) / width))];
  }
  return [Math.max(1, Math.round((width * maxDimension) / height)), maxDimension];
}

function canvasToBlob(canvas: HTMLCanvasElement, mediaType: string, quality?: number): Promise<Blob> {
  return new Promise((resolve, reject) => {
    canvas.toBlob(
      (blob) => {
        if (!blob) {
          reject(new Error("无法处理图片"));
          return;
        }
        resolve(blob);
      },
      mediaType,
      quality
    );
  });
}

function arrayBufferToBase64(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer);
  let binary = "";
  const chunkSize = 0x8000;
  for (let offset = 0; offset < bytes.length; offset += chunkSize) {
    binary += String.fromCharCode(...bytes.subarray(offset, offset + chunkSize));
  }
  return btoa(binary);
}

function nextComposerImageID(): string {
  const browserCrypto = globalThis.crypto as Crypto & { randomUUID?: () => string };
  return browserCrypto.randomUUID?.() ?? `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}

function nextComposerMessageID(): string {
  return nextComposerImageID();
}

function imageSource(image: InputImage): string {
  const mediaType = normalizeImageMediaType(image.media_type);
  return `data:${mediaType};base64,${image.data}`;
}

function createComposerMessage(text: string, images: ComposerImage[]): QueuedComposerMessage | undefined {
  const trimmed = text.trim();
  if (!trimmed && images.length === 0) {
    return undefined;
  }
  return {
    id: nextComposerMessageID(),
    text,
    images: images.map((image) => ({ ...image }))
  };
}

function inputImagesFromComposer(images: ComposerImage[]): InputImage[] {
  return images.map(({ media_type, data }) => ({ media_type, data }));
}

function mergeGuideMessages(messages: QueuedComposerMessage[]): QueuedComposerMessage {
  return {
    id: nextComposerMessageID(),
    text: messages
      .map((message) => message.text.trim())
      .filter(Boolean)
      .join("\n"),
    images: messages.flatMap((message) => message.images.map((image) => ({ ...image })))
  };
}

function queuedMessagePreview(message: QueuedComposerMessage): string {
  const text = message.text.trim().replace(/\s+/g, " ");
  const imageText = message.images.length > 0 ? `${message.images.length} 张图片` : "";
  const preview = [text, imageText].filter(Boolean).join(" · ");
  return trimMiddle(preview || "空消息", 48);
}

function trimMiddle(value: string, maxLength: number): string {
  if (value.length <= maxLength) {
    return value;
  }
  const left = Math.ceil((maxLength - 1) / 2);
  const right = Math.floor((maxLength - 1) / 2);
  return `${value.slice(0, left)}…${value.slice(value.length - right)}`;
}

function buildEnvironmentSourceItems({
  activeContext,
  activeProject,
  selectedWorkspaceFile,
  composerImages,
  queuedMessages,
  guideMessages
}: {
  activeContext?: RuntimeContext;
  activeProject?: DesktopProject;
  selectedWorkspaceFile?: string;
  composerImages: ComposerImage[];
  queuedMessages: QueuedComposerMessage[];
  guideMessages: QueuedComposerMessage[];
}): EnvironmentSourceItem[] {
  const items: EnvironmentSourceItem[] = [];
  if (activeContext?.kind === "project") {
    items.push({
      id: "project",
      icon: "project",
      title: activeProject?.name ?? "当前项目",
      detail: activeContext.cwd
    });
  } else if (activeContext?.kind === "no_project") {
    items.push({
      id: "temporary",
      icon: "temporary",
      title: "临时工作区",
      detail: activeContext.cwd
    });
  }
  if (selectedWorkspaceFile) {
    items.push({
      id: "selected-file",
      icon: "file",
      title: "当前文件",
      detail: selectedWorkspaceFile
    });
  }
  if (composerImages.length > 0) {
    items.push({
      id: "composer-images",
      icon: "image",
      title: "输入图片",
      detail: `${composerImages.length} 张`
    });
  }
  if (guideMessages.length > 0) {
    items.push({
      id: "guide-messages",
      icon: "guide",
      title: "下轮引导",
      detail: `${guideMessages.length} 条`
    });
  }
  if (queuedMessages.length > 0) {
    const imageCount = queuedMessages.reduce((count, message) => count + message.images.length, 0);
    items.push({
      id: "queued-messages",
      icon: "queue",
      title: "排队消息",
      detail: imageCount > 0 ? `${queuedMessages.length} 条，${imageCount} 张图片` : `${queuedMessages.length} 条`
    });
  }
  return items;
}

function pullRequestUnavailableReason(gitStatus?: GitStatusResult): string {
  if (!gitStatus?.is_repo) {
    return "不是 Git 仓库";
  }
  if (!gitStatus.gh_available) {
    return "未安装 GitHub CLI";
  }
  if (gitStatus.detached || !gitStatus.branch) {
    return "需要具名分支";
  }
  if (gitStatus.default_branch && gitStatus.branch === gitStatus.default_branch) {
    return "先创建功能分支";
  }
  if (gitStatus.dirty_count > 0) {
    return "先提交本地更改";
  }
  return "";
}

function humanizeBranchTitle(branch: string): string {
  return branch
    .split(/[/-]/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toLocaleUpperCase() + part.slice(1))
    .join(" ");
}

export function App(): JSX.Element {
  const [state, setState] = useState<AppState>(initialState);
  const [prompt, setPrompt] = useState("");
  const [composerImages, setComposerImages] = useState<ComposerImage[]>([]);
  const [queuedMessages, setQueuedMessages] = useState<QueuedComposerMessage[]>([]);
  const [guideMessages, setGuideMessages] = useState<QueuedComposerMessage[]>([]);
  const [sidebarWidth, setSidebarWidth] = useState(initialSidebarWidth);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(initialSidebarCollapsed);
  const [resizingSidebar, setResizingSidebar] = useState(false);
  const [projectMenuOpen, setProjectMenuOpen] = useState(false);
  const [runtimeMenuOpen, setRuntimeMenuOpen] = useState(false);
  const [accessMenuOpen, setAccessMenuOpen] = useState(false);
  const [codexRuntimeMenu, setCodexRuntimeMenu] = useState<CodexRuntimeMenu>(null);
  const [codexModels, setCodexModels] = useState<CodexModelLoadState>({ loading: false, error: "", models: [] });
  const [modeMenuOpen, setModeMenuOpen] = useState(false);
  const [branchMenuOpen, setBranchMenuOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [projectFilter, setProjectFilter] = useState("");
  const [launchPreviewPinned, setLaunchPreviewPinned] = useState(false);
  const [rightPanelOpen, setRightPanelOpen] = useState(false);
  const [bottomPanelOpen, setBottomPanelOpen] = useState(false);
  const [workspacePanelView, setWorkspacePanelView] = useState<WorkspacePanelView>("files");
  const [workspaceMode, setWorkspaceMode] = useState<WorkspacePanelView | undefined>(undefined);
  const [selectedWorkspaceFile, setSelectedWorkspaceFile] = useState<string | undefined>(undefined);
  const [environmentPanelOpen, setEnvironmentPanelOpen] = useState(false);
  const [environmentPanelDismissed, setEnvironmentPanelDismissed] = useState(false);
  const [environmentPanelHasRoom, setEnvironmentPanelHasRoom] = useState(() =>
    typeof window === "undefined" ? false : window.matchMedia("(min-width: 1320px) and (min-height: 680px)").matches
  );
  const [environmentPanelMenu, setEnvironmentPanelMenu] = useState<EnvironmentPanelMenu>(null);
  const [environmentDialog, setEnvironmentDialog] = useState<EnvironmentDialog>(null);
  const [runDebugOpen, setRunDebugOpen] = useState(false);
  const [runDebugEvents, setRunDebugEvents] = useState<RunDebugEvent[]>([]);
  const [runDebugCopied, setRunDebugCopied] = useState(false);
  const conversationScrollRef = useRef<HTMLDivElement | null>(null);
  const conversationAutoFollowRef = useRef(true);
  const streamScrollFrameRef = useRef<number | undefined>(undefined);
  const resizeSessionRef = useRef<SidebarResizeSession | null>(null);
  const projectMenuRef = useRef<HTMLDivElement>(null);
  const runtimeMenuRef = useRef<HTMLDivElement>(null);
  const accessMenuRef = useRef<HTMLDivElement>(null);
  const codexRuntimeRef = useRef<HTMLDivElement>(null);
  const environmentPanelRef = useRef<HTMLDivElement>(null);
  const runDebugRef = useRef<HTMLDivElement>(null);
  const appStateRef = useRef<AppState>(initialState);
  const queuedMessagesRef = useRef<QueuedComposerMessage[]>([]);
  const guideMessagesRef = useRef<QueuedComposerMessage[]>([]);
  const drainingQueueRef = useRef(false);
  const queueDrainPausedRef = useRef(false);
  const runDebugEventIDRef = useRef(0);
  const runDebugDeltaSeenRef = useRef(new Set<string>());

  useEffect(() => {
    const root = document.documentElement;
    let resizeEndTimer: number | undefined;
    let resizing = false;

    function setResizeState(nextResizing: boolean): void {
      if (resizing === nextResizing) {
        return;
      }
      resizing = nextResizing;
      root.classList.toggle("window-resizing", nextResizing);
      if (!nextResizing) {
        window.dispatchEvent(new Event("wuu-window-resize-end"));
      }
    }

    function scheduleResizeEnd(delay = 140): void {
      if (resizeEndTimer !== undefined) {
        window.clearTimeout(resizeEndTimer);
      }
      resizeEndTimer = window.setTimeout(() => {
        resizeEndTimer = undefined;
        setResizeState(false);
      }, delay);
    }

    function handleWindowResize(): void {
      setResizeState(true);
      scheduleResizeEnd();
    }

    const offWindowResizeState = window.wuu.onWindowResizeState(({ resizing: nextResizing }) => {
      if (nextResizing) {
        setResizeState(true);
        scheduleResizeEnd();
        return;
      }
      scheduleResizeEnd(40);
    });

    window.addEventListener("resize", handleWindowResize);
    return () => {
      offWindowResizeState();
      window.removeEventListener("resize", handleWindowResize);
      if (resizeEndTimer !== undefined) {
        window.clearTimeout(resizeEndTimer);
      }
      setResizeState(false);
    };
  }, []);

  useEffect(() => {
    const query = window.matchMedia("(min-width: 1320px) and (min-height: 680px)");
    const update = (): void => setEnvironmentPanelHasRoom(query.matches);
    update();
    query.addEventListener("change", update);
    return () => query.removeEventListener("change", update);
  }, []);

  useEffect(() => {
    appStateRef.current = state;
  }, [state]);

  useEffect(() => {
    if (!state.running) {
      void drainQueuedMessages();
    }
  }, [state.activeContext?.cwd, state.initialized?.model, state.initialized?.provider, state.running, state.thread?.id]);

  useEffect(() => {
    let mounted = true;
    const off = window.wuu.onServerEvent((event) => {
      if (!mounted) {
        return;
      }
      recordRunDebugEvent(event);
      const handling = handleStreamingNotification(event);
      if (handling === "stream") {
        scheduleStreamScroll();
        return;
      }
      if (handling === "skip") {
        return;
      }
      setState((current) => reduceServerEvent(current, event));
    });

    void (async () => {
      try {
        const listedProjects = await window.wuu.listProjects();
        const runtimeState = listedProjects.active_context ? listedProjects : await window.wuu.selectNoProject(false);
        const loadedState = await loadRuntime(runtimeState);
        if (!mounted) {
          return;
        }
        setState((current) => ({
          ...current,
          ...loadedState
        }));
      } catch (error) {
        if (!mounted) {
          return;
        }
        setState((current) => ({
          ...current,
          status: error instanceof Error ? error.message : "failed to start"
        }));
      }
    })();

    return () => {
      mounted = false;
      off();
      if (streamScrollFrameRef.current !== undefined) {
        window.cancelAnimationFrame(streamScrollFrameRef.current);
        streamScrollFrameRef.current = undefined;
      }
    };
  }, []);

  useEffect(() => {
    function handlePointerDown(event: PointerEvent): void {
      const target = event.target;
      if (!(target instanceof Node)) {
        return;
      }
      if (projectMenuOpen && !projectMenuRef.current?.contains(target)) {
        setProjectMenuOpen(false);
      }
      if ((runtimeMenuOpen || modeMenuOpen || branchMenuOpen) && !runtimeMenuRef.current?.contains(target)) {
        setRuntimeMenuOpen(false);
        setModeMenuOpen(false);
        setBranchMenuOpen(false);
      }
      if (accessMenuOpen && !accessMenuRef.current?.contains(target)) {
        setAccessMenuOpen(false);
      }
      if (codexRuntimeMenu && !codexRuntimeRef.current?.contains(target)) {
        setCodexRuntimeMenu(null);
      }
      if (environmentPanelOpen && !environmentPanelRef.current?.contains(target)) {
        setEnvironmentPanelOpen(false);
        setEnvironmentPanelMenu(null);
      }
      if (runDebugOpen && !runDebugRef.current?.contains(target)) {
        setRunDebugOpen(false);
      }
    }

    window.addEventListener("pointerdown", handlePointerDown);
    return () => window.removeEventListener("pointerdown", handlePointerDown);
  }, [
    accessMenuOpen,
    branchMenuOpen,
    codexRuntimeMenu,
    environmentPanelOpen,
    modeMenuOpen,
    projectMenuOpen,
    runDebugOpen,
    runtimeMenuOpen
  ]);

  useEffect(() => {
    conversationAutoFollowRef.current = true;
    scrollConversationToBottom({ force: true });
  }, [state.thread?.id]);

  useEffect(() => {
    scheduleStreamScroll();
  }, [state.thread?.turns]);

  useEffect(() => {
    if (!state.askRequest) {
      return;
    }
    setSettingsOpen(false);
    setWorkspaceMode(undefined);
    conversationAutoFollowRef.current = true;
    window.requestAnimationFrame(() => scrollConversationToBottom({ force: true }));
  }, [state.askRequest?.id]);

  useEffect(() => {
    window.localStorage.setItem(SIDEBAR_WIDTH_KEY, String(sidebarWidth));
    window.localStorage.setItem(SIDEBAR_COLLAPSED_KEY, String(sidebarCollapsed));
  }, [sidebarWidth, sidebarCollapsed]);

  useEffect(() => {
    setSelectedWorkspaceFile(undefined);
  }, [state.activeContext?.cwd]);

  useEffect(() => {
    if (!resizingSidebar) {
      return;
    }

    function handlePointerMove(event: PointerEvent): void {
      const session = resizeSessionRef.current;
      if (!session) {
        return;
      }
      applySidebarWidth(session.startWidth + event.clientX - session.startX);
    }

    function handlePointerUp(): void {
      resizeSessionRef.current = null;
      setResizingSidebar(false);
    }

    window.addEventListener("pointermove", handlePointerMove);
    window.addEventListener("pointerup", handlePointerUp);
    window.addEventListener("pointercancel", handlePointerUp);
    return () => {
      window.removeEventListener("pointermove", handlePointerMove);
      window.removeEventListener("pointerup", handlePointerUp);
      window.removeEventListener("pointercancel", handlePointerUp);
    };
  }, [resizingSidebar]);

  const activeProject = useMemo(
    () => state.projects.find((project) => project.id === state.activeProjectId),
    [state.activeProjectId, state.projects]
  );
  const activeTitle = workspaceMode ? workspaceModeTitle(workspaceMode) : state.thread?.preview || "新对话";
  const emptyThreadTitle =
    state.activeContext?.kind === "project"
      ? `我们应该在 ${activeProject?.name ?? "这个项目"} 中构建什么？`
      : "我们应该在 wuu 中构建什么？";
  const turns = state.thread?.turns ?? [];
  const emptyConversation = turns.length === 0 && !state.askRequest;
  const previewingLaunch = ENABLE_LAUNCH_PREVIEW && launchPreviewPinned;
  const showingWorkspaceMode = state.initialized && !previewingLaunch && workspaceMode !== undefined;
  const shellClassName = `app-shell${sidebarCollapsed ? " sidebar-collapsed" : ""}${
    resizingSidebar ? " resizing-sidebar" : ""
  }${rightPanelOpen ? " right-panel-open" : ""}${bottomPanelOpen ? " bottom-panel-open" : ""}`;
  const shellStyle = {
    "--sidebar-width": `${sidebarCollapsed ? 0 : sidebarWidth}px`,
    "--workspace-right-panel-width": "360px",
    "--workspace-bottom-panel-height": "238px"
  } as CSSProperties;
  const environmentSourceItems = useMemo(
    () =>
      buildEnvironmentSourceItems({
        activeContext: state.activeContext,
        activeProject,
        selectedWorkspaceFile,
        composerImages,
        queuedMessages,
        guideMessages
      }),
    [activeProject, composerImages, guideMessages, queuedMessages, selectedWorkspaceFile, state.activeContext]
  );
  const environmentPanelVisible =
    Boolean(state.initialized && !previewingLaunch && !showingWorkspaceMode && !rightPanelOpen) &&
    (environmentPanelOpen || (environmentPanelHasRoom && !environmentPanelDismissed && !emptyConversation));
  const pullRequestDisabledReason = pullRequestUnavailableReason(state.gitStatus);
  const runDebugPhase = runDebugPhaseForState(state);

  function applySidebarWidth(nextWidth: number): void {
    if (nextWidth <= SIDEBAR_MIN_WIDTH) {
      setSidebarCollapsed(true);
      return;
    }
    setSidebarCollapsed(false);
    setSidebarWidth(clamp(nextWidth, SIDEBAR_MIN_WIDTH, SIDEBAR_MAX_WIDTH));
  }

  function scheduleStreamScroll(): void {
    if (!conversationAutoFollowRef.current) {
      return;
    }
    if (streamScrollFrameRef.current !== undefined) {
      return;
    }
    streamScrollFrameRef.current = window.requestAnimationFrame(() => {
      streamScrollFrameRef.current = undefined;
      scrollConversationToBottom();
    });
  }

  function handleConversationScroll(): void {
    const node = conversationViewport();
    if (!node) {
      return;
    }
    conversationAutoFollowRef.current = isConversationNearBottom(node);
  }

  function scrollConversationToBottom(options: { force?: boolean } = {}): void {
    const node = conversationViewport();
    if (!node || (!options.force && !conversationAutoFollowRef.current)) {
      return;
    }
    node.scrollTop = node.scrollHeight;
    conversationAutoFollowRef.current = true;
  }

  function conversationViewport(): HTMLElement | undefined {
    return conversationScrollRef.current ?? undefined;
  }

  function isConversationNearBottom(node: HTMLElement): boolean {
    const distanceFromBottom = Math.max(0, node.scrollHeight - node.scrollTop - node.clientHeight);
    return distanceFromBottom <= CONVERSATION_AUTO_SCROLL_THRESHOLD_PX;
  }

  function startSidebarResize(event: ReactPointerEvent<HTMLDivElement>): void {
    if (event.button !== 0) {
      return;
    }
    event.preventDefault();
    resizeSessionRef.current = {
      startX: event.clientX,
      startWidth: sidebarCollapsed ? 0 : sidebarWidth
    };
    setResizingSidebar(true);
  }

  function toggleSidebar(): void {
    setSidebarCollapsed((collapsed) => !collapsed);
    setSidebarWidth((width) => (width <= SIDEBAR_MIN_WIDTH ? SIDEBAR_DEFAULT_WIDTH : width));
  }

  function openWorkspaceTool(view: WorkspacePanelView): void {
    setWorkspacePanelView(view);
    setWorkspaceMode(view);
    setRightPanelOpen(true);
  }

  function openWorkspaceFile(path: string): void {
    setWorkspacePanelView("files");
    setWorkspaceMode("files");
    setRightPanelOpen(true);
    setSelectedWorkspaceFile((current) => (current === path ? current : path));
  }

  async function attachComposerImageFiles(files: File[]): Promise<void> {
    if (files.length === 0) {
      return;
    }
    try {
      const images = await Promise.all(files.map((file) => composerImageFromFile(file)));
      setComposerImages((current) => [...current, ...images]);
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "图片粘贴失败"
      }));
    }
  }

  function removeComposerImage(id: string): void {
    setComposerImages((current) => current.filter((image) => image.id !== id));
  }

  function setQueuedMessagesNow(messages: QueuedComposerMessage[]): void {
    queuedMessagesRef.current = messages;
    setQueuedMessages(messages);
  }

  function setGuideMessagesNow(messages: QueuedComposerMessage[]): void {
    guideMessagesRef.current = messages;
    setGuideMessages(messages);
  }

  function clearPendingComposerMessages(): void {
    queueDrainPausedRef.current = false;
    setQueuedMessagesNow([]);
    setGuideMessagesNow([]);
  }

  function enqueueComposerMessage(message: QueuedComposerMessage): void {
    queueDrainPausedRef.current = false;
    const next = [...queuedMessagesRef.current, message];
    setQueuedMessagesNow(next);
    setState((current) => ({
      ...current,
      status: `已排队 ${next.length} 条`
    }));
  }

  function removeQueuedMessage(id: string): void {
    queueDrainPausedRef.current = false;
    setQueuedMessagesNow(queuedMessagesRef.current.filter((message) => message.id !== id));
    void drainQueuedMessages();
  }

  function removeGuideMessage(id: string): void {
    queueDrainPausedRef.current = false;
    setGuideMessagesNow(guideMessagesRef.current.filter((message) => message.id !== id));
    void drainQueuedMessages();
  }

  async function guideQueuedMessage(id: string): Promise<void> {
    const queuedIndex = queuedMessagesRef.current.findIndex((message) => message.id === id);
    if (queuedIndex < 0) {
      return;
    }
    const message = queuedMessagesRef.current[queuedIndex];
    const remainingQueued = [
      ...queuedMessagesRef.current.slice(0, queuedIndex),
      ...queuedMessagesRef.current.slice(queuedIndex + 1)
    ];
    queueDrainPausedRef.current = false;
    setQueuedMessagesNow(remainingQueued);
    setGuideMessagesNow([...guideMessagesRef.current, message]);
    setState((current) => ({
      ...current,
      status: "引导已加入"
    }));

    const currentState = appStateRef.current;
    if (!currentState.running) {
      void drainQueuedMessages();
      return;
    }
    if (!currentState.thread) {
      return;
    }
    try {
      await window.wuu.interruptTurn(currentState.thread.id);
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "interrupt failed"
      }));
    }
  }

  function handleSidebarSeparatorKey(event: ReactKeyboardEvent<HTMLDivElement>): void {
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      toggleSidebar();
      return;
    }
    if (event.key === "ArrowLeft") {
      event.preventDefault();
      if (sidebarCollapsed) {
        return;
      }
      applySidebarWidth(sidebarWidth - SIDEBAR_STEP);
      return;
    }
    if (event.key === "ArrowRight") {
      event.preventDefault();
      if (sidebarCollapsed) {
        setSidebarCollapsed(false);
        setSidebarWidth(SIDEBAR_DEFAULT_WIDTH);
        return;
      }
      applySidebarWidth(sidebarWidth + SIDEBAR_STEP);
    }
  }

  function renderComposer(variant: ComposerVariant): JSX.Element {
    return (
      <Composer
        variant={variant}
        prompt={prompt}
        setPrompt={setPrompt}
        images={composerImages}
        queuedMessages={queuedMessages}
        guideMessages={guideMessages}
        running={state.running}
        status={state.status}
        initialized={state.initialized}
        gitStatus={state.gitStatus}
        projects={state.projects}
        activeContext={state.activeContext}
        activeProject={activeProject}
        codexModels={codexModels}
        codexRuntimeMenu={codexRuntimeMenu}
        codexRuntimeRef={codexRuntimeRef}
        menuOpen={runtimeMenuOpen}
        accessMenuOpen={accessMenuOpen}
        modeMenuOpen={modeMenuOpen}
        branchMenuOpen={branchMenuOpen}
        menuRef={runtimeMenuRef}
        accessMenuRef={accessMenuRef}
        projectFilter={projectFilter}
        setProjectFilter={setProjectFilter}
        onToggleMenu={() => {
          setAccessMenuOpen(false);
          setModeMenuOpen(false);
          setBranchMenuOpen(false);
          setCodexRuntimeMenu(null);
          setRuntimeMenuOpen((open) => !open);
        }}
        onToggleAccessMenu={() => {
          setRuntimeMenuOpen(false);
          setModeMenuOpen(false);
          setBranchMenuOpen(false);
          setCodexRuntimeMenu(null);
          setAccessMenuOpen((open) => !open);
        }}
        onToggleModeMenu={() => {
          setRuntimeMenuOpen(false);
          setAccessMenuOpen(false);
          setBranchMenuOpen(false);
          setCodexRuntimeMenu(null);
          setModeMenuOpen((open) => !open);
        }}
        onToggleBranchMenu={() => {
          setRuntimeMenuOpen(false);
          setAccessMenuOpen(false);
          setModeMenuOpen(false);
          setCodexRuntimeMenu(null);
          setBranchMenuOpen((open) => !open);
        }}
        onToggleCodexRuntimeMenu={toggleCodexRuntimeMenu}
        onSelectCodexModel={(nextModel) => void selectCodexModel(nextModel)}
        onSelectCodexEffort={(nextEffort) => void selectCodexEffort(nextEffort)}
        onOpenSettings={() => {
          closeProjectMenus();
          setSettingsOpen(true);
        }}
        onSelectProject={(id) => void openProject(id)}
        onSelectNoProject={() => void useNoProject(false)}
        onSelectGitBranch={(branch) => void checkoutBranch(branch)}
        onCreateProject={() => void createBlankProject()}
        onOpenProject={() => void chooseProjectFolder()}
        onPasteImageFiles={(files) => void attachComposerImageFiles(files)}
        onRemoveImage={removeComposerImage}
        onRemoveQueuedMessage={removeQueuedMessage}
        onRemoveGuideMessage={removeGuideMessage}
        onGuideQueuedMessage={(id) => void guideQueuedMessage(id)}
        onClearQueuedMessages={clearPendingComposerMessages}
        onSend={() => void sendPrompt()}
        onInterrupt={() => void interrupt()}
      />
    );
  }

  async function loadRuntime(projectState: ProjectListResult): Promise<Partial<AppState>> {
    if (!projectState.active_context) {
      return emptyRuntimeState(projectState);
    }
    const [initialized, gitStatus] = await Promise.all([window.wuu.initialize(), window.wuu.gitStatus()]);
    const listed = await window.wuu.listThreads();
    const thread =
      listed.threads.length > 0
        ? requireThread(await window.wuu.resumeThread(listed.threads[0].id), "resume did not return a thread")
        : undefined;
    return {
      initialized,
      projects: projectState.projects,
      activeContext: projectState.active_context,
      activeProjectId: activeProjectID(projectState.active_context),
      gitStatus,
      thread,
      threads: thread ? upsertThread(listed.threads, thread) : listed.threads.filter(isThread),
      running: false,
      status: "ready"
    };
  }

  function emptyRuntimeState(projectState: ProjectListResult): Partial<AppState> {
    return {
      initialized: undefined,
      projects: projectState.projects,
      activeContext: undefined,
      activeProjectId: undefined,
      gitStatus: undefined,
      thread: undefined,
      threads: [],
      running: false,
      status: "no-runtime"
    };
  }

  function closeProjectMenus(): void {
    setProjectMenuOpen(false);
    setRuntimeMenuOpen(false);
    setAccessMenuOpen(false);
    setCodexRuntimeMenu(null);
    setModeMenuOpen(false);
    setBranchMenuOpen(false);
    setEnvironmentPanelMenu(null);
    setSettingsOpen(false);
    setProjectFilter("");
  }

  async function openProject(projectId: string): Promise<void> {
    if (projectId === state.activeProjectId && state.activeContext?.kind === "project") {
      closeProjectMenus();
      return;
    }
    closeProjectMenus();
    clearPendingComposerMessages();
    setState((current) => ({
      ...current,
      activeContext: undefined,
      activeProjectId: projectId,
      initialized: undefined,
      thread: undefined,
      threads: [],
      running: false,
      status: "opening"
    }));
    try {
      const projectState = await window.wuu.selectProject(projectId);
      const loadedState = await loadRuntime(projectState);
      setState((current) => ({ ...current, ...loadedState }));
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "open project failed"
      }));
    }
  }

  async function createBlankProject(): Promise<void> {
    closeProjectMenus();
    clearPendingComposerMessages();
    try {
      const projectState = await window.wuu.createBlankProject();
      if (sameRuntimeContext(projectState.active_context, state.activeContext)) {
        setState((current) => ({ ...current, projects: projectState.projects }));
        return;
      }
      const loadedState = await loadRuntime(projectState);
      setState((current) => ({ ...current, ...loadedState }));
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "create project failed"
      }));
    }
  }

  async function chooseProjectFolder(): Promise<void> {
    closeProjectMenus();
    clearPendingComposerMessages();
    try {
      const projectState = await window.wuu.chooseProjectFolder();
      if (sameRuntimeContext(projectState.active_context, state.activeContext)) {
        setState((current) => ({ ...current, projects: projectState.projects }));
        return;
      }
      const loadedState = await loadRuntime(projectState);
      setState((current) => ({ ...current, ...loadedState }));
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "open folder failed"
      }));
    }
  }

  async function useNoProject(fresh: boolean): Promise<void> {
    if (!fresh && state.activeContext?.kind === "no_project") {
      closeProjectMenus();
      return;
    }
    closeProjectMenus();
    clearPendingComposerMessages();
    setState((current) => ({
      ...current,
      activeContext: undefined,
      activeProjectId: undefined,
      initialized: undefined,
      thread: undefined,
      threads: [],
      running: false,
      status: "opening"
    }));
    try {
      const projectState = await window.wuu.selectNoProject(fresh);
      const loadedState = await loadRuntime(projectState);
      setState((current) => ({ ...current, ...loadedState }));
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "open no-project failed"
      }));
    }
  }

  async function checkoutBranch(branch: string): Promise<void> {
    if (!branch || state.running) {
      return;
    }
    closeProjectMenus();
    try {
      const gitStatus = await window.wuu.checkoutGitBranch(branch);
      setState((current) => ({
        ...current,
        gitStatus,
        status: current.status === "ready" ? "ready" : current.status
      }));
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "checkout branch failed"
      }));
    }
  }

  async function refreshGitStatus(): Promise<void> {
    if (!state.activeContext) {
      return;
    }
    try {
      const gitStatus = await window.wuu.gitStatus();
      setState((current) => ({
        ...current,
        gitStatus,
        status: current.status === "ready" ? "ready" : current.status
      }));
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "refresh git status failed"
      }));
    }
  }

  async function createAndCheckoutBranch(branch: string): Promise<void> {
    if (!branch || state.running) {
      return;
    }
    try {
      const result = await window.wuu.createCheckoutGitBranch(branch);
      setState((current) => ({
        ...current,
        gitStatus: result.status,
        status: current.status === "ready" ? "ready" : current.status
      }));
      setEnvironmentPanelMenu(null);
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "create branch failed"
      }));
      throw error;
    }
  }

  async function commitEnvironmentChanges(params: { message: string; includeUnstaged: boolean }): Promise<GitCommitResult> {
    const result = await window.wuu.commitGitChanges({
      message: params.message,
      include_unstaged: params.includeUnstaged
    });
    setState((current) => ({
      ...current,
      gitStatus: result.status,
      status: `已提交 ${result.commit}`
    }));
    return result;
  }

  async function createEnvironmentPullRequest(params: {
    title: string;
    body: string;
    draft: boolean;
  }): Promise<GitPullRequestResult> {
    const result = await window.wuu.createPullRequest({
      title: params.title,
      body: params.body,
      draft: params.draft
    });
    setState((current) => ({
      ...current,
      gitStatus: result.status,
      status: result.already_exists ? "已有拉取请求" : "已创建拉取请求"
    }));
    return result;
  }

  function toggleEnvironmentPanel(): void {
    const visible = environmentPanelVisible;
    setEnvironmentPanelOpen(!visible);
    setEnvironmentPanelDismissed(visible);
    if (visible) {
      setEnvironmentPanelMenu(null);
    } else {
      setRuntimeMenuOpen(false);
      setAccessMenuOpen(false);
      setModeMenuOpen(false);
      setBranchMenuOpen(false);
      setCodexRuntimeMenu(null);
    }
  }

  function appendRunDebugEvent(entry: Omit<RunDebugEvent, "id" | "at">): void {
    const next: RunDebugEvent = {
      ...entry,
      id: ++runDebugEventIDRef.current,
      at: Date.now()
    };
    setRunDebugEvents((current) => [...current, next].slice(-80));
  }

  function resetRunDebugEvents(entry: Omit<RunDebugEvent, "id" | "at">): void {
    runDebugDeltaSeenRef.current.clear();
    const next: RunDebugEvent = {
      ...entry,
      id: ++runDebugEventIDRef.current,
      at: Date.now()
    };
    setRunDebugEvents([next]);
  }

  function recordRunDebugEvent(event: ServerEvent): void {
    const entry = runDebugEventFromServerEvent(event, runDebugDeltaSeenRef.current);
    if (entry) {
      appendRunDebugEvent(entry);
    }
  }

  async function copyRunDebugInfo(): Promise<void> {
    const snapshot = buildRunDebugSnapshot({
      state,
      events: runDebugEvents,
      queuedMessages,
      guideMessages,
      composerImages
    });
    try {
      await navigator.clipboard.writeText(snapshot);
      setRunDebugCopied(true);
      window.setTimeout(() => setRunDebugCopied(false), 1200);
    } catch (error) {
      appendRunDebugEvent({
        source: "client",
        method: "debug/copy",
        detail: error instanceof Error ? error.message : "复制失败",
        tone: "error"
      });
    }
  }

  async function startNewThread(): Promise<void> {
    if (!state.activeContext || state.running) {
      return;
    }
    setPrompt("");
    setComposerImages([]);
    clearPendingComposerMessages();
    if (state.activeContext.kind === "no_project" && state.thread) {
      await useNoProject(true);
      return;
    }
    setState((current) => ({
      ...current,
      thread: undefined,
      running: false,
      status: "ready"
    }));
  }

  async function selectThread(threadId: string): Promise<void> {
    if (!state.activeContext || threadId === state.thread?.id || state.running) {
      return;
    }
    clearPendingComposerMessages();
    setState((current) => ({ ...current, status: "loading" }));
    try {
      const thread = requireThread(await window.wuu.resumeThread(threadId), "resume did not return a thread");
      setState((current) => ({
        ...current,
        thread,
        threads: upsertThread(current.threads, thread),
        status: "ready"
      }));
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "load failed"
      }));
    }
  }

  async function sendPrompt(): Promise<void> {
    const message = createComposerMessage(prompt, composerImages);
    const currentState = appStateRef.current;
    if (!message || !currentState.activeContext || !currentState.initialized) {
      return;
    }
    setPrompt("");
    setComposerImages([]);
    if (currentState.running) {
      enqueueComposerMessage(message);
      return;
    }
    await sendComposerMessage(message, true);
  }

  async function sendComposerMessage(message: QueuedComposerMessage, restoreDraftOnError = false): Promise<boolean> {
    const currentState = appStateRef.current;
    const text = message.text.trim();
    const images = inputImagesFromComposer(message.images);
    if ((!text && images.length === 0) || !currentState.activeContext || !currentState.initialized || currentState.running) {
      return false;
    }
    conversationAutoFollowRef.current = true;
    resetRunDebugEvents({
      source: "client",
      method: "client/send",
      detail: images.length > 0 ? `已提交输入，包含 ${images.length} 张图片` : "已提交输入",
      tone: "running",
      threadID: currentState.thread?.id
    });
    appStateRef.current = { ...currentState, running: true, status: "正在发送请求" };
    setState((current) => ({ ...current, running: true, status: "正在发送请求" }));
    try {
      const thread =
        currentState.thread ?? requireThread(await window.wuu.startThread(), "thread/start did not return a thread");
      appStateRef.current = {
        ...appStateRef.current,
        thread,
        threads: upsertThread(appStateRef.current.threads, thread)
      };
      setState((current) => ({
        ...current,
        thread,
        threads: upsertThread(current.threads, thread)
      }));
      const result = await window.wuu.startTurn(thread.id, text, images);
      setState((current) =>
        updateThread({ ...current, thread: current.thread?.id === thread.id ? current.thread : thread }, (currentThread) =>
          upsertTurn(currentThread, result.turn)
        )
      );
      appendRunDebugEvent({
        source: "client",
        method: "turn/start response",
        detail: "服务端已接受本轮请求",
        tone: "running",
        threadID: thread.id,
        turnID: result.turn.id
      });
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : "send failed";
      appStateRef.current = { ...appStateRef.current, running: false, status: errorMessage };
      setState((current) => ({
        ...current,
        running: false,
        status: errorMessage
      }));
      if (restoreDraftOnError) {
        setPrompt(message.text);
        setComposerImages(message.images);
      }
      return false;
    }
    return true;
  }

  async function drainQueuedMessages(): Promise<void> {
    if (drainingQueueRef.current || queueDrainPausedRef.current) {
      return;
    }
    const currentState = appStateRef.current;
    if (currentState.running || !currentState.activeContext || !currentState.initialized) {
      return;
    }

    const guidesToSend = guideMessagesRef.current;
    let message: QueuedComposerMessage | undefined;
    let restoreGuides: QueuedComposerMessage[] = [];
    let restoreQueued: QueuedComposerMessage | undefined;

    if (guidesToSend.length > 0) {
      restoreGuides = guidesToSend;
      message = mergeGuideMessages(guidesToSend);
      setGuideMessagesNow([]);
    } else if (queuedMessagesRef.current.length > 0) {
      const [nextMessage, ...remainingMessages] = queuedMessagesRef.current;
      message = nextMessage;
      restoreQueued = nextMessage;
      setQueuedMessagesNow(remainingMessages);
    }

    if (!message) {
      return;
    }

    drainingQueueRef.current = true;
    const sent = await sendComposerMessage(message);
    drainingQueueRef.current = false;
    if (!sent) {
      queueDrainPausedRef.current = true;
      if (restoreGuides.length > 0) {
        setGuideMessagesNow([...restoreGuides, ...guideMessagesRef.current]);
      } else if (restoreQueued) {
        setQueuedMessagesNow([restoreQueued, ...queuedMessagesRef.current]);
      }
      setState((current) => ({
        ...current,
        status: "队列暂停"
      }));
    }
  }

  async function updateRuntimeSettings(provider: string, model: string, effort?: string): Promise<void> {
    const nextProvider = provider.trim();
    const nextModel = model.trim();
    const nextEffort = effort === undefined ? undefined : effort.trim();
    if (
      !nextProvider ||
      !nextModel ||
      !state.initialized ||
      state.running ||
      (nextProvider === state.initialized.provider &&
        nextModel === state.initialized.model &&
        (nextEffort === undefined || nextEffort === (state.initialized.effort ?? "")))
    ) {
      return;
    }
    try {
      const updated = await window.wuu.updateRuntimeSettings(nextProvider, nextModel, nextEffort);
      setState((current) => {
        const initialized = current.initialized
          ? {
              ...current.initialized,
              provider: updated.provider,
              model: updated.model,
              effort: updated.effort ?? "",
              providers: updated.providers ?? current.initialized.providers
            }
          : current.initialized;
        const updateThreadModel = (thread: Thread): Thread => ({
          ...thread,
          model_provider: updated.provider,
          model: updated.model
        });
        const thread = current.thread ? updateThreadModel(current.thread) : current.thread;
        return {
          ...current,
          initialized,
          thread,
          threads: current.threads.map(updateThreadModel),
          status: current.status === "ready" ? current.status : "ready"
        };
      });
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "update runtime settings failed"
      }));
      throw error;
    }
  }

  function toggleCodexRuntimeMenu(menu: Exclude<CodexRuntimeMenu, null>): void {
    if (!state.initialized || state.running || !isCodexProvider(state.initialized)) {
      return;
    }
    setRuntimeMenuOpen(false);
    setAccessMenuOpen(false);
    setModeMenuOpen(false);
    setBranchMenuOpen(false);
    setCodexRuntimeMenu((current) => (current === menu ? null : menu));
    void loadCodexModelsForProvider(state.initialized.provider);
  }

  async function loadCodexModelsForProvider(provider: string): Promise<void> {
    if (!provider) {
      return;
    }
    if (codexModels.provider === provider && (codexModels.loading || codexModels.models.length > 0)) {
      return;
    }
    setCodexModels({ provider, loading: true, error: "", models: [] });
    try {
      const result = await window.wuu.loadCodexModels(provider);
      setCodexModels({
        provider: result.provider,
        loading: false,
        error: "",
        models: result.models
      });
      setState((current) => {
        if (!current.initialized || current.initialized.provider !== result.provider) {
          return current;
        }
        return {
          ...current,
          initialized: {
            ...current.initialized,
            model: result.model,
            effort: result.effort ?? ""
          }
        };
      });
    } catch (error) {
      setCodexModels({
        provider,
        loading: false,
        error: error instanceof Error ? error.message : "无法加载 Codex 模型",
        models: []
      });
    }
  }

  async function selectCodexModel(nextModel: CodexModelSummary): Promise<void> {
    if (!state.initialized || state.running) {
      return;
    }
    const nextEffort = normalizedEffortForModel(state.initialized.effort ?? "", nextModel);
    await updateRuntimeSettings(state.initialized.provider, nextModel.slug, nextEffort);
    setCodexRuntimeMenu(null);
  }

  async function selectCodexEffort(nextEffort: string): Promise<void> {
    if (!state.initialized || state.running) {
      return;
    }
    await updateRuntimeSettings(state.initialized.provider, state.initialized.model, nextEffort);
    setCodexRuntimeMenu(null);
  }

  async function interrupt(): Promise<void> {
    if (!state.thread) {
      return;
    }
    await window.wuu.interruptTurn(state.thread.id);
  }

  async function respondToAskRequest(request: AskRequestState, response: AskUserResponse): Promise<void> {
    setState((current) => (current.askRequest?.id === request.id ? { ...current, askRequest: undefined } : current));
    try {
      await window.wuu.respondToServerRequest(request.id, response);
    } catch (error) {
      setState((current) => ({
        ...current,
        askRequest: current.askRequest ?? request,
        status: desktopApiErrorMessage(error, "提交选择失败")
      }));
    }
  }

  if (settingsOpen) {
    return (
      <SettingsView
        initialized={state.initialized}
        running={state.running}
        onBack={() => setSettingsOpen(false)}
        onSave={updateRuntimeSettings}
      />
    );
  }

  return (
    <div className={shellClassName} style={shellStyle}>
      <aside className="sidebar">
        <div className="traffic-spacer" />
        <nav className="primary-nav" aria-label="主导航">
          <button className="nav-item" onClick={() => void startNewThread()} disabled={!state.activeContext || state.running}>
            <MessageSquarePlus size={18} />
            <span>新对话</span>
          </button>
        </nav>

        <section className="project-list" aria-label="项目">
          <div className="project-section-header" ref={projectMenuRef}>
            <div className="section-label">项目</div>
            <button
              className="project-add-button"
              aria-label="添加项目"
              aria-haspopup="menu"
              aria-expanded={projectMenuOpen}
              onClick={() => setProjectMenuOpen((open) => !open)}
            >
              <FolderPlus size={20} />
            </button>
            {projectMenuOpen ? (
              <div className="project-add-menu" role="menu">
                <button role="menuitem" onClick={() => void createBlankProject()}>
                  <FolderPlus size={22} />
                  <span>新建空白项目</span>
                </button>
                <button role="menuitem" onClick={() => void chooseProjectFolder()}>
                  <FolderOpen size={22} />
                  <span>使用现有文件夹</span>
                </button>
              </div>
            ) : null}
          </div>
          {state.projects.length === 0 ? <div className="project-empty-note">还没有项目</div> : null}
          <ProjectList
            projects={state.projects}
            activeID={state.activeProjectId}
            threads={state.threads}
            activeThreadID={state.thread?.id}
            onSelectProject={(id) => void openProject(id)}
            onSelectThread={(id) => void selectThread(id)}
          />
        </section>
        <div className="sidebar-settings">
          <button
            className="settings-button"
            type="button"
            disabled={!state.initialized}
            onClick={() => {
              setProjectMenuOpen(false);
              setRuntimeMenuOpen(false);
              setCodexRuntimeMenu(null);
              setSettingsOpen(true);
            }}
          >
            <Settings size={18} />
            <span>设置</span>
          </button>
        </div>
      </aside>

      {sidebarCollapsed ? null : (
        <div
          className="sidebar-resizer"
          role="separator"
          aria-label="调整侧边栏宽度"
          aria-orientation="vertical"
          aria-valuemin={SIDEBAR_MIN_WIDTH}
          aria-valuemax={SIDEBAR_MAX_WIDTH}
          aria-valuenow={sidebarWidth}
          tabIndex={0}
          onPointerDown={startSidebarResize}
          onDoubleClick={toggleSidebar}
          onKeyDown={handleSidebarSeparatorKey}
        />
      )}

      <main className="conversation-pane">
        <header className="titlebar">
          <div className="title-block">
            {sidebarCollapsed ? (
              <button className="icon-button sidebar-toggle-button" aria-label="展开侧边栏" onClick={toggleSidebar}>
                <PanelLeftOpen size={18} />
              </button>
            ) : null}
            {workspaceMode ? (
              <span className="workspace-title-icon" aria-hidden="true">
                <WorkspaceToolIcon view={workspaceMode} size={18} />
              </span>
            ) : null}
            <h1>{activeTitle}</h1>
          </div>
          <div className="title-actions">
            {ENABLE_LAUNCH_PREVIEW ? (
              <button
                className="launch-preview-button"
                type="button"
                disabled={previewingLaunch}
                onClick={() => setLaunchPreviewPinned(true)}
              >
                <Terminal size={15} />
                <span>启动动画</span>
              </button>
            ) : null}
            <div className="run-debug-anchor" ref={runDebugRef}>
              <button
                className={`launch-preview-button run-debug-button${runDebugOpen ? " active" : ""}`}
                type="button"
                aria-label={runDebugOpen ? "隐藏调试信息" : "显示调试信息"}
                aria-expanded={runDebugOpen}
                onClick={() => {
                  setEnvironmentPanelOpen(false);
                  setEnvironmentPanelMenu(null);
                  setRunDebugOpen((open) => !open);
                }}
              >
                <Bug size={15} />
                <span>调试</span>
              </button>
              {runDebugOpen ? (
                <RunDebugPanel
                  state={state}
                  phase={runDebugPhase}
                  events={runDebugEvents}
                  queuedMessages={queuedMessages}
                  guideMessages={guideMessages}
                  composerImages={composerImages}
                  copied={runDebugCopied}
                  onCopy={() => void copyRunDebugInfo()}
                  onClose={() => setRunDebugOpen(false)}
                />
              ) : null}
            </div>
            <button
              className={`icon-button environment-toggle-button${environmentPanelVisible ? " active" : ""}`}
              type="button"
              aria-label={environmentPanelVisible ? "隐藏环境信息" : "显示环境信息"}
              aria-pressed={environmentPanelVisible}
              onClick={toggleEnvironmentPanel}
            >
              <Info size={18} />
            </button>
            <button
              className={`icon-button workspace-toggle-button${bottomPanelOpen ? " active" : ""}`}
              type="button"
              aria-label={bottomPanelOpen ? "关闭底部栏" : "打开底部栏"}
              aria-pressed={bottomPanelOpen}
              onClick={() => setBottomPanelOpen((open) => !open)}
            >
              <PanelBottomOpen size={18} />
            </button>
            <button
              className={`icon-button workspace-toggle-button${rightPanelOpen ? " active" : ""}`}
              type="button"
              aria-label={rightPanelOpen ? "关闭右侧栏" : "打开右侧栏"}
              aria-pressed={rightPanelOpen}
              onClick={() => setRightPanelOpen((open) => !open)}
            >
              <PanelRightOpen size={18} />
            </button>
          </div>
        </header>

        {environmentPanelVisible && state.initialized ? (
          <EnvironmentPanel
            panelRef={environmentPanelRef}
            initialized={state.initialized}
            gitStatus={state.gitStatus}
            activeContext={state.activeContext}
            activeProject={activeProject}
            sourceItems={environmentSourceItems}
            activeMenu={environmentPanelMenu}
            running={state.running}
            pullRequestDisabledReason={pullRequestDisabledReason}
            onSetActiveMenu={setEnvironmentPanelMenu}
            onClose={() => {
              setEnvironmentPanelOpen(false);
              setEnvironmentPanelDismissed(true);
              setEnvironmentPanelMenu(null);
            }}
            onOpenSettings={() => {
              setEnvironmentPanelMenu(null);
              setSettingsOpen(true);
            }}
            onRefreshGit={() => void refreshGitStatus()}
            onOpenProject={() => void chooseProjectFolder()}
            onSelectNoProject={() => void useNoProject(false)}
            onSelectBranch={(branch) => void checkoutBranch(branch)}
            onCreateBranch={(branch) => createAndCheckoutBranch(branch)}
            onOpenCommit={() => setEnvironmentDialog("commit")}
            onOpenPullRequest={() => setEnvironmentDialog("pull-request")}
          />
        ) : null}

        {state.initialized && !previewingLaunch ? (
          <div
            className={`scroll-region${emptyConversation && !showingWorkspaceMode ? " empty-scroll-region" : ""}${
              showingWorkspaceMode ? " workspace-scroll-region" : ""
            }`}
            onScroll={handleConversationScroll}
            ref={conversationScrollRef}
          >
            {workspaceMode ? (
              <WorkspaceMainPanel
                view={workspaceMode}
                activeContext={state.activeContext}
                selectedFilePath={selectedWorkspaceFile}
                onOpenRightPanel={() => {
                  setWorkspacePanelView(workspaceMode);
                  setRightPanelOpen(true);
                }}
              />
            ) : emptyConversation ? (
              <EmptyConversationHome
                title={emptyThreadTitle}
                onOpenProject={() => void chooseProjectFolder()}
                onCreateProject={() => void createBlankProject()}
                onSelectNoProject={() => void useNoProject(true)}
              >
                {renderComposer("hero")}
              </EmptyConversationHome>
            ) : (
              <div className="conversation-width">
                {turns.map((turn) => (
                  <TurnView
                    key={turn.id}
                    turn={turn}
                    cwd={state.thread?.cwd ?? state.activeContext?.cwd}
                    onStreamFrame={scheduleStreamScroll}
                    onInterrupt={() => void interrupt()}
                  />
                ))}
                {state.askRequest ? (
                  <AskUserMessage
                    key={state.askRequest.id}
                    request={state.askRequest}
                    onCancel={(request) => respondToAskRequest(request, { answers: {}, cancelled: true })}
                    onSubmit={(request, answers) => respondToAskRequest(request, { answers })}
                  />
                ) : null}
              </div>
            )}
          </div>
        ) : (
          <RuntimeLoading
            status={state.status}
            pinned={previewingLaunch}
            onExitPreview={() => setLaunchPreviewPinned(false)}
          />
        )}

        {state.initialized && !previewingLaunch && !emptyConversation && !showingWorkspaceMode
          ? renderComposer("dock")
          : null}
      </main>

      <WorkspaceRightPanel
        open={rightPanelOpen}
        view={workspacePanelView}
        activeContext={state.activeContext}
        selectedFilePath={selectedWorkspaceFile}
        onSelectView={openWorkspaceTool}
        onOpenFile={openWorkspaceFile}
        onClose={() => setRightPanelOpen(false)}
      />
      <WorkspaceBottomPanel
        open={bottomPanelOpen}
        selectedView={workspacePanelView}
        onSelectTool={openWorkspaceTool}
        onClose={() => setBottomPanelOpen(false)}
      />

      {environmentDialog === "commit" ? (
        <CommitChangesDialog
          gitStatus={state.gitStatus}
          branch={state.gitStatus?.branch}
          onCancel={() => setEnvironmentDialog(null)}
          onCommit={commitEnvironmentChanges}
        />
      ) : null}
      {environmentDialog === "pull-request" ? (
        <PullRequestDialog
          gitStatus={state.gitStatus}
          disabledReason={pullRequestDisabledReason}
          onCancel={() => setEnvironmentDialog(null)}
          onCreate={createEnvironmentPullRequest}
        />
      ) : null}

    </div>
  );
}

function RunDebugPanel({
  state,
  phase,
  events,
  queuedMessages,
  guideMessages,
  composerImages,
  copied,
  onCopy,
  onClose
}: {
  state: AppState;
  phase: RunDebugPhase;
  events: RunDebugEvent[];
  queuedMessages: QueuedComposerMessage[];
  guideMessages: QueuedComposerMessage[];
  composerImages: ComposerImage[];
  copied: boolean;
  onCopy: () => void;
  onClose: () => void;
}): JSX.Element {
  const turn = phase.turn ?? activeDebugTurn(state.thread);
  const thread = state.thread;
  const lastEvent = events.length > 0 ? events[events.length - 1] : undefined;
  const turnStartedAt = turn ? Date.parse(turn.started_at) : NaN;
  const model = state.initialized
    ? `${state.initialized.provider} / ${state.initialized.model}${state.initialized.effort ? ` / ${state.initialized.effort}` : ""}`
    : "未初始化";
  const queueDetail = [
    queuedMessages.length > 0 ? `排队 ${queuedMessages.length}` : "",
    guideMessages.length > 0 ? `引导 ${guideMessages.length}` : "",
    composerImages.length > 0 ? `图片 ${composerImages.length}` : ""
  ]
    .filter(Boolean)
    .join("，");

  return (
    <aside className="run-debug-panel" aria-label="调试信息">
      <div className="run-debug-header">
        <div>
          <span className={`run-debug-phase ${phase.tone}`}>{phase.label}</span>
          <strong>{phase.detail}</strong>
        </div>
        <div className="run-debug-actions">
          <button className="icon-button" type="button" aria-label="复制调试信息" onClick={onCopy}>
            <Copy size={15} />
          </button>
          <button className="icon-button" type="button" aria-label="关闭调试信息" onClick={onClose}>
            <X size={15} />
          </button>
        </div>
      </div>

      <div className="run-debug-scroll">
        {copied ? <div className="run-debug-copied">已复制诊断信息</div> : null}
        <section className="run-debug-section">
          <h3>当前状态</h3>
          <RunDebugRow label="运行" value={state.running ? "running" : state.status || "ready"} />
          <RunDebugRow label="模型" value={model} />
          <RunDebugRow label="工作区" value={state.activeContext?.cwd ?? thread?.cwd ?? "未连接"} />
          <RunDebugRow label="Thread" value={thread ? shortDebugID(thread.id) : "无"} />
          <RunDebugRow
            label="Turn"
            value={
              turn ? (
                <>
                  {shortDebugID(turn.id)} · {debugTurnStatusLabel(turn.status)} ·{" "}
                  {typeof turn.duration_ms === "number"
                    ? formatDuration(turn.duration_ms)
                    : Number.isFinite(turnStartedAt)
                      ? <LiveDuration startedAtMs={turnStartedAt} />
                      : "未知耗时"}
                </>
              ) : (
                "无"
              )
            }
          />
          <RunDebugRow
            label="最后事件"
            value={
              lastEvent ? (
                <>
                  {lastEvent.method} · <LiveSince atMs={lastEvent.at} />
                </>
              ) : (
                "暂无"
              )
            }
          />
          {queueDetail ? <RunDebugRow label="待发送" value={queueDetail} /> : null}
        </section>

        <section className="run-debug-section">
          <h3>本轮 Item</h3>
          {turn?.items.length ? (
            <div className="run-debug-items">
              {turn.items.map((item) => (
                <RunDebugItem key={item.id} turnID={turn.id} item={item} />
              ))}
            </div>
          ) : (
            <div className="run-debug-empty">还没有收到 turn/item。</div>
          )}
        </section>

        <section className="run-debug-section">
          <h3>事件时间线</h3>
          {events.length > 0 ? (
            <div className="run-debug-events">
              {events
                .slice(-24)
                .reverse()
                .map((event) => (
                  <div className={`run-debug-event ${event.tone}`} key={event.id}>
                    <span>{formatDebugTime(event.at)}</span>
                    <strong>{event.method}</strong>
                    <small>{event.detail}</small>
                  </div>
                ))}
            </div>
          ) : (
            <div className="run-debug-empty">暂无事件。</div>
          )}
        </section>
      </div>
    </aside>
  );
}

function RunDebugRow({ label, value }: { label: string; value: ReactNode }): JSX.Element {
  return (
    <div className="run-debug-row">
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}

function RunDebugItem({ turnID, item }: { turnID: string; item: ThreadItem }): JSX.Element {
  return (
    <div className={`run-debug-item ${item.status ?? "in_progress"}`}>
      <div>
        <strong>{debugItemTitle(item)}</strong>
        <span>
          {shortDebugID(item.id)} · {debugItemStatusLabel(item)}
        </span>
      </div>
      <div className="run-debug-item-meta">
        <DebugFieldLength turnID={turnID} item={item} field="text" label="text" />
        <DebugFieldLength turnID={turnID} item={item} field="arguments" label="args" />
        <DebugFieldLength turnID={turnID} item={item} field="result" label="result" />
        {item.error ? <span className="error">error</span> : null}
      </div>
    </div>
  );
}

function DebugFieldLength({
  turnID,
  item,
  field,
  label
}: {
  turnID: string;
  item: ThreadItem;
  field: StreamTextField;
  label: string;
}): JSX.Element | null {
  const key = streamTextKey(turnID, item.id, field);
  const initialValue = streamTextStore.has(key) ? streamTextStore.get(key) : item[field] ?? "";
  const [length, setLength] = useState(initialValue.length);

  useEffect(() => {
    const currentValue = streamTextStore.has(key) ? streamTextStore.get(key) : item[field] ?? "";
    setLength(currentValue.length);
    return streamTextStore.subscribe(key, (value) => setLength(value.length));
  }, [field, item, key]);

  if (length === 0) {
    return null;
  }
  return (
    <span>
      {label} {length.toLocaleString()}
    </span>
  );
}

function LiveSince({ atMs }: { atMs: number }): JSX.Element {
  const nodeRef = useRef<HTMLSpanElement | null>(null);

  useEffect(() => {
    const update = (): void => {
      if (nodeRef.current) {
        nodeRef.current.textContent = `${formatDuration(Date.now() - atMs)} 前`;
      }
    };
    update();
    const timer = window.setInterval(update, 1000);
    return () => window.clearInterval(timer);
  }, [atMs]);

  return <span ref={nodeRef}>{formatDuration(Date.now() - atMs)} 前</span>;
}

function EnvironmentPanel({
  panelRef,
  initialized,
  gitStatus,
  activeContext,
  activeProject,
  sourceItems,
  activeMenu,
  running,
  pullRequestDisabledReason,
  onSetActiveMenu,
  onClose,
  onOpenSettings,
  onRefreshGit,
  onOpenProject,
  onSelectNoProject,
  onSelectBranch,
  onCreateBranch,
  onOpenCommit,
  onOpenPullRequest
}: {
  panelRef: RefObject<HTMLDivElement>;
  initialized: InitializeResult;
  gitStatus?: GitStatusResult;
  activeContext?: RuntimeContext;
  activeProject?: DesktopProject;
  sourceItems: EnvironmentSourceItem[];
  activeMenu: EnvironmentPanelMenu;
  running: boolean;
  pullRequestDisabledReason: string;
  onSetActiveMenu: (menu: EnvironmentPanelMenu) => void;
  onClose: () => void;
  onOpenSettings: () => void;
  onRefreshGit: () => void;
  onOpenProject: () => void;
  onSelectNoProject: () => void;
  onSelectBranch: (branch: string) => void;
  onCreateBranch: (branch: string) => Promise<void>;
  onOpenCommit: () => void;
  onOpenPullRequest: () => void;
}): JSX.Element {
  const diff = gitStatus?.diff ?? { files: 0, additions: 0, deletions: 0 };
  const hasChanges = Boolean(gitStatus?.is_repo && (gitStatus.dirty_count > 0 || diff.files > 0));
  const branchLabel = gitStatus?.is_repo ? gitStatus.branch ?? "detached" : "非 Git 仓库";
  const contextLabel =
    activeContext?.kind === "project" ? activeProject?.name ?? "当前项目" : activeContext ? "临时对话" : "未连接";
  const prDisabled = Boolean(pullRequestDisabledReason && !gitStatus?.pr_url);

  function toggleMenu(menu: Exclude<EnvironmentPanelMenu, null>): void {
    onSetActiveMenu(activeMenu === menu ? null : menu);
  }

  return (
    <aside className="environment-panel" ref={panelRef} aria-label="环境信息">
      <div className="environment-panel-header">
        <h2>环境信息</h2>
        <div className="environment-panel-actions">
          <button className="icon-button" type="button" aria-label="刷新 Git 状态" onClick={onRefreshGit}>
            <RefreshCw size={16} />
          </button>
          <button className="icon-button" type="button" aria-label="打开设置" onClick={onOpenSettings}>
            <Settings size={16} />
          </button>
          <button className="icon-button" type="button" aria-label="关闭环境信息" onClick={onClose}>
            <X size={16} />
          </button>
        </div>
      </div>

      <div className="environment-panel-body">
        <div className="environment-row static">
          <FolderPlus size={18} />
          <strong>变更</strong>
          <span className="environment-row-meta">
            {gitStatus?.is_repo ? `${diff.files} 个文件` : "非 Git"}
            {gitStatus?.is_repo && diff.files > 0 ? (
              <span className="environment-diff">
                <span className="additions">+{diff.additions.toLocaleString()}</span>
                <span className="deletions">-{diff.deletions.toLocaleString()}</span>
              </span>
            ) : null}
          </span>
        </div>

        <button
          className={`environment-row${activeMenu === "mode" ? " active" : ""}`}
          type="button"
          onClick={() => toggleMenu("mode")}
        >
          <Laptop size={18} />
          <strong>本地</strong>
          <span>{contextLabel}</span>
          <ChevronRight size={17} />
        </button>

        <button
          className={`environment-row${activeMenu === "branch" ? " active" : ""}`}
          type="button"
          disabled={!gitStatus?.is_repo || running}
          onClick={() => toggleMenu("branch")}
        >
          <GitBranch size={18} />
          <strong>{branchLabel}</strong>
          <span>{gitStatus?.dirty_count ? `未提交：${gitStatus.dirty_count} 个文件` : ""}</span>
          {gitStatus?.is_repo ? <ChevronRight size={17} /> : null}
        </button>

        <button
          className="environment-row"
          type="button"
          disabled={!hasChanges || running}
          onClick={onOpenCommit}
        >
          <CornerDownRight size={18} />
          <strong>提交</strong>
          <span>{hasChanges ? "提交当前更改" : "工作区干净"}</span>
        </button>

        <button
          className="environment-row"
          type="button"
          disabled={prDisabled || running}
          title={prDisabled ? pullRequestDisabledReason : undefined}
          onClick={onOpenPullRequest}
        >
          <Github size={18} />
          <strong>{gitStatus?.pr_url ? "查看拉取请求" : "创建拉取请求"}</strong>
          <span>{gitStatus?.pr_url ? "已有 PR" : prDisabled ? pullRequestDisabledReason : "推送并创建 PR"}</span>
        </button>
      </div>

      <button
        className={`environment-footer-row${activeMenu === "sources" ? " active" : ""}`}
        type="button"
        onClick={() => toggleMenu("sources")}
      >
        <span>来源 {sourceItems.length}</span>
        <ChevronRight size={17} />
      </button>

      <div className="environment-runtime-summary">
        <span>{initialized.provider}</span>
        <span>{shortCodexModelLabel(initialized.model)}</span>
      </div>

      {activeMenu === "mode" ? (
        <EnvironmentModeMenu
          activeContext={activeContext}
          activeProject={activeProject}
          onOpenProject={onOpenProject}
          onSelectNoProject={onSelectNoProject}
        />
      ) : null}
      {activeMenu === "branch" && gitStatus?.is_repo ? (
        <EnvironmentBranchMenu
          gitStatus={gitStatus}
          onSelectBranch={onSelectBranch}
          onCreateBranch={onCreateBranch}
        />
      ) : null}
      {activeMenu === "sources" ? <EnvironmentSourcesMenu items={sourceItems} /> : null}
    </aside>
  );
}

function EnvironmentModeMenu({
  activeContext,
  activeProject,
  onOpenProject,
  onSelectNoProject
}: {
  activeContext?: RuntimeContext;
  activeProject?: DesktopProject;
  onOpenProject: () => void;
  onSelectNoProject: () => void;
}): JSX.Element {
  return (
    <div className="environment-side-menu mode" role="menu">
      <div className="environment-side-label">继续使用</div>
      {activeProject ? (
        <button role="menuitem" type="button" disabled>
          <Folder size={17} />
          <span>{activeProject.name}</span>
          <Check size={17} />
        </button>
      ) : null}
      <button role="menuitem" type="button" onClick={onOpenProject}>
        <FolderOpen size={17} />
        <span>打开本地项目</span>
      </button>
      <button role="menuitem" type="button" disabled={activeContext?.kind === "no_project"} onClick={onSelectNoProject}>
        <FolderX size={17} />
        <span>临时对话</span>
        {activeContext?.kind === "no_project" ? <Check size={17} /> : null}
      </button>
    </div>
  );
}

function EnvironmentBranchMenu({
  gitStatus,
  onSelectBranch,
  onCreateBranch
}: {
  gitStatus: GitStatusResult;
  onSelectBranch: (branch: string) => void;
  onCreateBranch: (branch: string) => Promise<void>;
}): JSX.Element {
  const [query, setQuery] = useState("");
  const [newBranch, setNewBranch] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");
  const normalizedQuery = query.trim().toLocaleLowerCase();
  const branches = (gitStatus.branches ?? []).filter((branch) =>
    normalizedQuery ? branch.toLocaleLowerCase().includes(normalizedQuery) : true
  );

  async function submitNewBranch(event: ReactFormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault();
    const branch = newBranch.trim();
    if (!branch || submitting) {
      return;
    }
    setSubmitting(true);
    setError("");
    try {
      await onCreateBranch(branch);
      setNewBranch("");
    } catch (createError) {
      setError(createError instanceof Error ? createError.message : "无法创建分支");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="environment-side-menu branch" role="menu">
      <label className="environment-search">
        <Search size={16} />
        <input value={query} placeholder="搜索分支" onChange={(event) => setQuery(event.target.value)} />
      </label>
      {gitStatus.dirty_count > 0 ? (
        <div className="environment-side-note">未提交更改会跟随分支切换；如果会覆盖本地内容，Git 会拒绝。</div>
      ) : null}
      <div className="environment-branch-list">
        {branches.length === 0 ? <div className="environment-empty">没有匹配分支</div> : null}
        {branches.map((branch) => {
          const selected = branch === gitStatus.branch;
          return (
            <button key={branch} role="menuitem" type="button" disabled={selected} onClick={() => onSelectBranch(branch)}>
              <GitBranch size={17} />
              <span>{branch}</span>
              {selected ? <Check size={17} /> : null}
            </button>
          );
        })}
      </div>
      <form className="environment-create-branch" onSubmit={(event) => void submitNewBranch(event)}>
        <input value={newBranch} placeholder="新分支名称" onChange={(event) => setNewBranch(event.target.value)} />
        <button type="submit" disabled={!newBranch.trim() || submitting}>
          <Plus size={16} />
        </button>
      </form>
      {error ? <div className="environment-side-error">{error}</div> : null}
    </div>
  );
}

function EnvironmentSourcesMenu({ items }: { items: EnvironmentSourceItem[] }): JSX.Element {
  return (
    <div className="environment-side-menu sources" role="menu">
      <div className="environment-side-label">当前上下文</div>
      {items.length === 0 ? <div className="environment-empty">没有额外来源</div> : null}
      {items.map((item) => (
        <div className="environment-source-item" key={item.id}>
          <EnvironmentSourceIcon item={item} />
          <div>
            <strong>{item.title}</strong>
            <span>{item.detail}</span>
          </div>
        </div>
      ))}
    </div>
  );
}

function EnvironmentSourceIcon({ item }: { item: EnvironmentSourceItem }): JSX.Element {
  if (item.icon === "project") {
    return <Folder size={17} />;
  }
  if (item.icon === "temporary") {
    return <FolderX size={17} />;
  }
  if (item.icon === "file") {
    return <FileText size={17} />;
  }
  if (item.icon === "image") {
    return <Globe2 size={17} />;
  }
  if (item.icon === "guide") {
    return <CornerDownRight size={17} />;
  }
  return <MessageSquarePlus size={17} />;
}

function CommitChangesDialog({
  gitStatus,
  branch,
  onCancel,
  onCommit
}: {
  gitStatus?: GitStatusResult;
  branch?: string;
  onCancel: () => void;
  onCommit: (params: { message: string; includeUnstaged: boolean }) => Promise<GitCommitResult>;
}): JSX.Element {
  const [message, setMessage] = useState("");
  const [includeUnstaged, setIncludeUnstaged] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");
  const diff = gitStatus?.diff ?? { files: 0, additions: 0, deletions: 0 };
  const staged = gitStatus?.staged_diff ?? { files: 0, additions: 0, deletions: 0 };
  const hasChanges = Boolean(gitStatus?.is_repo && (gitStatus.dirty_count > 0 || diff.files > 0 || staged.files > 0));

  async function submit(event: ReactFormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault();
    if (!hasChanges || submitting) {
      return;
    }
    setSubmitting(true);
    setError("");
    try {
      await onCommit({ message, includeUnstaged });
      onCancel();
    } catch (commitError) {
      setError(commitError instanceof Error ? commitError.message : "提交失败");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="modal-backdrop environment-modal-backdrop">
      <form className="environment-dialog" onSubmit={(event) => void submit(event)}>
        <div className="environment-dialog-header">
          <span className="environment-dialog-icon">
            <CornerDownRight size={18} />
          </span>
          <button className="icon-button" type="button" aria-label="关闭" onClick={onCancel}>
            <X size={17} />
          </button>
        </div>
        <h2>提交更改</h2>
        <div className="environment-dialog-summary">
          <span>分支</span>
          <strong>{branch ?? "未知"}</strong>
          <span>更改</span>
          <strong>
            {diff.files} 个文件 <span className="additions">+{diff.additions.toLocaleString()}</span>{" "}
            <span className="deletions">-{diff.deletions.toLocaleString()}</span>
          </strong>
        </div>
        <label className="environment-toggle">
          <input
            type="checkbox"
            checked={includeUnstaged}
            onChange={(event) => setIncludeUnstaged(event.currentTarget.checked)}
          />
          <span>包含未暂存的更改</span>
        </label>
        <label className="environment-field">
          <span>提交消息</span>
          <input value={message} placeholder="留空以自动生成提交消息" onChange={(event) => setMessage(event.target.value)} />
        </label>
        {error ? <div className="environment-dialog-error">{error}</div> : null}
        <div className="environment-dialog-footer">
          <button className="secondary-button" type="button" onClick={onCancel}>
            取消
          </button>
          <button className="primary-button" type="submit" disabled={!hasChanges || submitting}>
            继续
          </button>
        </div>
      </form>
    </div>
  );
}

function PullRequestDialog({
  gitStatus,
  disabledReason,
  onCancel,
  onCreate
}: {
  gitStatus?: GitStatusResult;
  disabledReason: string;
  onCancel: () => void;
  onCreate: (params: { title: string; body: string; draft: boolean }) => Promise<GitPullRequestResult>;
}): JSX.Element {
  const [title, setTitle] = useState(() => humanizeBranchTitle(gitStatus?.branch ?? ""));
  const [body, setBody] = useState("");
  const [draft, setDraft] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");
  const [result, setResult] = useState<GitPullRequestResult | undefined>(undefined);
  const existingURL = gitStatus?.pr_url ?? result?.url;
  const blocked = Boolean(disabledReason && !existingURL);

  async function submit(event: ReactFormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault();
    if (blocked || submitting) {
      return;
    }
    if (existingURL) {
      window.open(existingURL, "_blank", "noopener,noreferrer");
      return;
    }
    setSubmitting(true);
    setError("");
    try {
      const created = await onCreate({ title, body, draft });
      setResult(created);
    } catch (createError) {
      setError(createError instanceof Error ? createError.message : "创建拉取请求失败");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="modal-backdrop environment-modal-backdrop">
      <form className="environment-dialog" onSubmit={(event) => void submit(event)}>
        <div className="environment-dialog-header">
          <span className="environment-dialog-icon">
            <Github size={18} />
          </span>
          <button className="icon-button" type="button" aria-label="关闭" onClick={onCancel}>
            <X size={17} />
          </button>
        </div>
        <h2>{existingURL ? "拉取请求" : "创建拉取请求"}</h2>
        {blocked ? <div className="environment-dialog-error">{disabledReason}</div> : null}
        {existingURL ? (
          <div className="environment-pr-result">
            <span>{result?.already_exists ? "已有 PR" : "PR 已准备好"}</span>
            <button className="secondary-button" type="button" onClick={() => window.open(existingURL, "_blank", "noopener,noreferrer")}>
              打开 PR
            </button>
          </div>
        ) : (
          <>
            <label className="environment-field">
              <span>标题</span>
              <input value={title} placeholder="使用分支名作为标题" onChange={(event) => setTitle(event.target.value)} />
            </label>
            <label className="environment-field">
              <span>说明</span>
              <textarea value={body} placeholder="可留空，让 gh 使用提交内容" onChange={(event) => setBody(event.target.value)} />
            </label>
            <label className="environment-toggle">
              <input type="checkbox" checked={draft} onChange={(event) => setDraft(event.currentTarget.checked)} />
              <span>创建为草稿</span>
            </label>
          </>
        )}
        {error ? <div className="environment-dialog-error">{error}</div> : null}
        <div className="environment-dialog-footer">
          <button className="secondary-button" type="button" onClick={onCancel}>
            关闭
          </button>
          <button className="primary-button" type="submit" disabled={blocked || submitting}>
            {existingURL ? "打开" : "继续"}
          </button>
        </div>
      </form>
    </div>
  );
}

function WorkspaceRightPanel({
  open,
  view,
  activeContext,
  selectedFilePath,
  onSelectView,
  onOpenFile,
  onClose
}: {
  open: boolean;
  view: WorkspacePanelView;
  activeContext?: RuntimeContext;
  selectedFilePath?: string;
  onSelectView: (view: WorkspacePanelView) => void;
  onOpenFile: (path: string) => void;
  onClose: () => void;
}): JSX.Element {
  const activeTool = workspaceToolFor(view);

  return (
    <aside className="workspace-right-panel" aria-hidden={!open}>
      <div className="workspace-panel-header">
        <div className="workspace-panel-title">
          <WorkspaceToolIcon view={view} size={18} />
          <span>{activeTool.title}</span>
        </div>
        <button
          className="icon-button workspace-panel-close"
          type="button"
          aria-label="关闭右侧栏"
          disabled={!open}
          onClick={onClose}
        >
          <X size={17} />
        </button>
      </div>
      {open ? (
        <>
          <div className="workspace-panel-tabs" role="tablist" aria-label="右侧栏工具">
            {WORKSPACE_TOOL_ITEMS.map((item) => (
              <button
                key={item.id}
                className={item.id === view ? "active" : ""}
                type="button"
                role="tab"
                aria-selected={item.id === view}
                title={item.title}
                onClick={() => onSelectView(item.id)}
              >
                <WorkspaceToolIcon view={item.id} size={17} />
              </button>
            ))}
          </div>
          <div className="workspace-panel-body">
            {view === "files" ? (
              <WorkspaceFileTree
                activeContext={activeContext}
                open={open}
                selectedFilePath={selectedFilePath}
                onOpenFile={onOpenFile}
              />
            ) : (
              <WorkspacePanelPlaceholder view={view} />
            )}
          </div>
        </>
      ) : null}
    </aside>
  );
}

function WorkspaceBottomPanel({
  open,
  selectedView,
  onSelectTool,
  onClose
}: {
  open: boolean;
  selectedView: WorkspacePanelView;
  onSelectTool: (view: WorkspacePanelView) => void;
  onClose: () => void;
}): JSX.Element {
  return (
    <section className="workspace-bottom-panel" aria-hidden={!open}>
      <div className="workspace-bottom-header">
        <div className="workspace-bottom-title">工具</div>
        <button
          className="icon-button workspace-panel-close"
          type="button"
          aria-label="关闭底部栏"
          disabled={!open}
          onClick={onClose}
        >
          <X size={17} />
        </button>
      </div>
      {open ? (
        <OverlayScrollbarsComponent
          className="workspace-tool-grid"
          aria-label="工作区工具"
          data-overlayscrollbars-initialize
          defer
          options={OVERLAY_SCROLLBAR_OPTIONS}
        >
          {WORKSPACE_TOOL_ITEMS.map((item) => (
            <button
              key={item.id}
              className={`workspace-tool-card${item.id === selectedView ? " active" : ""}`}
              type="button"
              onClick={() => onSelectTool(item.id)}
            >
              <WorkspaceToolIcon view={item.id} size={25} />
              <strong>{item.title}</strong>
              <span>{item.subtitle}</span>
            </button>
          ))}
        </OverlayScrollbarsComponent>
      ) : null}
    </section>
  );
}

function WorkspaceFileTree({
  activeContext,
  open,
  selectedFilePath,
  onOpenFile
}: {
  activeContext?: RuntimeContext;
  open: boolean;
  selectedFilePath?: string;
  onOpenFile: (path: string) => void;
}): JSX.Element {
  const [fileTree, setFileTree] = useState<FileTreeListResult | undefined>(undefined);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);
  const workspaceRoot = activeContext?.cwd;

  useEffect(() => {
    if (!open || !workspaceRoot) {
      return;
    }

    let cancelled = false;
    setFileTree(undefined);
    setLoading(true);
    setError(undefined);
    void window.wuu
      .listWorkspaceFiles()
      .then((result) => {
        if (cancelled) {
          return;
        }
        setFileTree(result);
      })
      .catch((nextError) => {
        if (cancelled) {
          return;
        }
        setError(desktopApiErrorMessage(nextError, "读取文件失败"));
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [open, workspaceRoot]);

  if (!workspaceRoot) {
    return <WorkspacePanelEmpty title="没有项目" description="先选择一个项目。这个面板会显示它的文件。" />;
  }

  if (loading && !fileTree) {
    return <WorkspacePanelEmpty title="正在读取文件" description="文件树马上就绪。" />;
  }

  if (error) {
    return <WorkspacePanelEmpty title="读取失败" description={error} />;
  }

  if (!fileTree || fileTree.paths.length === 0) {
    return <WorkspacePanelEmpty title="没有文件" description={formatWorkspaceRoot(workspaceRoot)} />;
  }

  return (
    <div className="workspace-file-panel">
      <div className="workspace-file-meta">
        <span>{formatWorkspaceRoot(fileTree.root)}</span>
        <small>
          {fileTree.paths.length} 项{fileTree.truncated ? "，已截断" : ""}
        </small>
      </div>
      <WorkspaceFileTreeView
        paths={fileTree.paths}
        selectedFilePath={selectedFilePath}
        onOpenFile={onOpenFile}
      />
    </div>
  );
}

const WorkspaceFileTreeView = memo(function WorkspaceFileTreeView({
  paths,
  selectedFilePath,
  onOpenFile
}: {
  paths: string[];
  selectedFilePath?: string;
  onOpenFile: (path: string) => void;
}): JSX.Element {
  const preparedInput = useMemo(() => preparePresortedFileTreeInput(paths), [paths]);
  const { model } = useFileTree({
    flattenEmptyDirectories: true,
    initialExpansion: 1,
    initialSelectedPaths: selectedFilePath ? [selectedFilePath] : [],
    itemHeight: WORKSPACE_FILE_TREE_ITEM_HEIGHT,
    overscan: 8,
    preparedInput,
    search: true,
    stickyFolders: false,
    unsafeCSS: WORKSPACE_TREE_CSS
  });
  const syncedPathsRef = useRef(paths);
  const selectedPaths = useFileTreeSelection(model);
  const onOpenFileRef = useRef(onOpenFile);

  useEffect(() => {
    onOpenFileRef.current = onOpenFile;
  }, [onOpenFile]);

  useEffect(() => {
    if (paths === syncedPathsRef.current) {
      return;
    }
    model.resetPaths(preparedInput.paths, { preparedInput });
    syncedPathsRef.current = paths;
  }, [model, paths, preparedInput]);

  useEffect(() => {
    const nextPath = selectedPaths[0];
    if (!nextPath || nextPath.endsWith("/")) {
      return;
    }
    onOpenFileRef.current(nextPath);
  }, [selectedPaths]);

  return (
    <div className="workspace-file-tree-frame">
      <FileTree model={model} style={WORKSPACE_FILE_TREE_STYLE} />
    </div>
  );
});

function WorkspacePanelPlaceholder({ view }: { view: WorkspacePanelView }): JSX.Element {
  const tool = workspaceToolFor(view);
  return (
    <WorkspacePanelEmpty
      title={tool.title}
      description={`${tool.subtitle}会在这里打开。`}
      icon={<WorkspaceToolIcon view={view} size={24} />}
    />
  );
}

function WorkspacePanelEmpty({
  title,
  description,
  icon
}: {
  title: string;
  description: string;
  icon?: JSX.Element;
}): JSX.Element {
  return (
    <div className="workspace-panel-empty">
      <div className="workspace-panel-empty-icon">{icon ?? <FolderOpen size={24} />}</div>
      <strong>{title}</strong>
      <span>{description}</span>
    </div>
  );
}

function WorkspaceToolIcon({ view, size }: { view: WorkspacePanelView; size: number }): JSX.Element {
  switch (view) {
    case "files":
      return <FolderOpen size={size} />;
    case "chat":
      return <MessageSquarePlus size={size} />;
    case "browser":
      return <Globe2 size={size} />;
    case "review":
      return <ShieldCheck size={size} />;
    case "terminal":
      return <Terminal size={size} />;
  }
}

function workspaceToolFor(view: WorkspacePanelView): (typeof WORKSPACE_TOOL_ITEMS)[number] {
  return WORKSPACE_TOOL_ITEMS.find((item) => item.id === view) ?? WORKSPACE_TOOL_ITEMS[0];
}

function formatWorkspaceRoot(root: string): string {
  const segments = root.split(/[\\/]/).filter(Boolean);
  return segments.at(-1) ?? root;
}

function workspaceModeTitle(view: WorkspacePanelView): string {
  return view === "files" ? "打开文件" : workspaceToolFor(view).title;
}

function WorkspaceMainPanel({
  view,
  activeContext,
  selectedFilePath,
  onOpenRightPanel
}: {
  view: WorkspacePanelView;
  activeContext?: RuntimeContext;
  selectedFilePath?: string;
  onOpenRightPanel: () => void;
}): JSX.Element {
  if (view === "files") {
    return (
      <WorkspaceFilePreview
        activeContext={activeContext}
        selectedFilePath={selectedFilePath}
        onOpenRightPanel={onOpenRightPanel}
      />
    );
  }

  return (
    <div className="workspace-main-empty">
      <WorkspaceToolIcon view={view} size={34} />
      <strong>{workspaceToolFor(view).title}</strong>
      <span>{workspaceToolFor(view).subtitle}</span>
    </div>
  );
}

function WorkspaceFilePreview({
  activeContext,
  selectedFilePath,
  onOpenRightPanel
}: {
  activeContext?: RuntimeContext;
  selectedFilePath?: string;
  onOpenRightPanel: () => void;
}): JSX.Element {
  const [file, setFile] = useState<WorkspaceFileReadResult | undefined>(undefined);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);

  useEffect(() => {
    if (!selectedFilePath) {
      setFile(undefined);
      setError(undefined);
      setLoading(false);
      return;
    }

    let cancelled = false;
    setFile(undefined);
    setLoading(true);
    setError(undefined);
    void window.wuu
      .readWorkspaceFile(selectedFilePath)
      .then((result) => {
        if (!cancelled) {
          setFile(result);
        }
      })
      .catch((nextError) => {
        if (!cancelled) {
          setError(desktopApiErrorMessage(nextError, "打开文件失败"));
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [selectedFilePath]);

  if (!activeContext) {
    return (
      <div className="workspace-main-empty">
        <FolderX size={36} />
        <strong>没有项目</strong>
        <span>先打开一个项目，再浏览文件。</span>
      </div>
    );
  }

  if (!selectedFilePath) {
    return (
      <div className="workspace-main-empty">
        <FolderOpen size={38} />
        <strong>打开文件</strong>
        <span>从工作区目录树中选择文件</span>
        <button type="button" onClick={onOpenRightPanel}>
          显示目录树
        </button>
      </div>
    );
  }

  if (loading) {
    return (
      <div className="workspace-main-empty">
        <FileText size={36} />
        <strong>正在打开</strong>
        <span>{selectedFilePath}</span>
      </div>
    );
  }

  if (error) {
    return (
      <div className="workspace-main-empty">
        <AlertCircle size={36} />
        <strong>打开失败</strong>
        <span>{error}</span>
      </div>
    );
  }

  if (!file) {
    return (
      <div className="workspace-main-empty">
        <FileText size={36} />
        <strong>没有内容</strong>
        <span>{selectedFilePath}</span>
      </div>
    );
  }

  if (file.binary) {
    return (
      <div className="workspace-main-empty">
        <FileText size={36} />
        <strong>无法预览</strong>
        <span>{file.path} 是二进制文件。</span>
      </div>
    );
  }

  return (
    <article className="workspace-file-preview">
      <header className="workspace-file-preview-header">
        <div>
          <strong>{file.path}</strong>
          <span>
            {formatBytes(file.size_bytes)}
            {file.truncated ? " · 仅显示前 512 KB" : ""}
          </span>
        </div>
      </header>
      <OverlayScrollbarsComponent
        className="workspace-file-code-scroll"
        data-overlayscrollbars-initialize
        defer
        options={OVERLAY_SCROLLBAR_OPTIONS}
      >
        <pre className="workspace-file-code">
          <code>{file.text}</code>
        </pre>
      </OverlayScrollbarsComponent>
    </article>
  );
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) {
    return `${bytes} B`;
  }
  if (bytes < 1024 * 1024) {
    return `${Math.round(bytes / 102.4) / 10} KB`;
  }
  return `${Math.round(bytes / 1024 / 102.4) / 10} MB`;
}

function desktopApiErrorMessage(error: unknown, fallback: string): string {
  const message = error instanceof Error ? error.message : typeof error === "string" ? error : "";
  if (message.includes("No handler registered")) {
    return "文件接口还没被当前窗口加载。请重启桌面端后再试。";
  }
  return message || fallback;
}

function EmptyConversationHome({
  title,
  children,
  onOpenProject,
  onCreateProject,
  onSelectNoProject
}: {
  title: string;
  children: JSX.Element;
  onOpenProject: () => void;
  onCreateProject: () => void;
  onSelectNoProject: () => void;
}): JSX.Element {
  return (
    <section className="empty-home">
      <div className="empty-home-inner">
        <h2>{title}</h2>
        {children}
        <div className="empty-home-actions" aria-label="快速开始">
          <button type="button" onClick={onOpenProject}>
            <FolderOpen size={22} />
            <span>
              <strong>打开项目</strong>
              <small>从本地文件夹开始构建</small>
            </span>
          </button>
          <button type="button" onClick={onCreateProject}>
            <FolderPlus size={22} />
            <span>
              <strong>新建空白项目</strong>
              <small>为新任务准备工作区</small>
            </span>
          </button>
          <button type="button" onClick={onSelectNoProject}>
            <FolderX size={22} />
            <span>
              <strong>临时对话</strong>
              <small>不绑定项目直接开始</small>
            </span>
          </button>
        </div>
      </div>
    </section>
  );
}

function reduceServerEvent(state: AppState, event: ServerEvent): AppState {
  switch (event.kind) {
    case "notification":
      return reduceNotification(state, event.message);
    case "server-request": {
      if (event.message.method !== "item/tool/requestUserInput") {
        void window.wuu.rejectServerRequest(event.message.id, `unsupported server request: ${event.message.method}`);
        return state;
      }
      const params = event.message.params as { questions?: AskUserQuestion[] } | undefined;
      return {
        ...state,
        askRequest: {
          id: event.message.id,
          questions: params?.questions ?? []
        }
      };
    }
    case "server-error":
      return { ...state, status: event.message };
    case "server-exit":
      return { ...state, running: false, status: "app-server exited" };
  }
}

type StreamingNotificationHandling = "state" | "stream" | "skip";

function handleStreamingNotification(event: ServerEvent): StreamingNotificationHandling {
  if (event.kind !== "notification") {
    return "state";
  }
  const notification = event.message;
  const params = notification.params as Record<string, unknown> | undefined;
  switch (notification.method) {
    case "item/agentMessage/delta":
      appendStreamDelta(params, "text");
      return "stream";
    case "item/reasoning/delta":
      appendStreamDelta(params, "text");
      return "stream";
    case "item/toolCall/delta":
      appendStreamDelta(params, "arguments");
      return "stream";
    case "item/toolCall/outputDelta":
      appendStreamDelta(params, "result");
      return "stream";
    case "turn/event":
      return "skip";
    case "item/started":
    case "item/completed":
      syncStreamItem(params);
      return "state";
    default:
      return "state";
  }
}

function appendStreamDelta(params: Record<string, unknown> | undefined, field: StreamTextField): void {
  const turnID = params?.turn_id as string | undefined;
  const itemID = params?.item_id as string | undefined;
  const delta = params?.delta as string | undefined;
  if (!turnID || !itemID || !delta) {
    return;
  }
  streamTextStore.append(streamTextKey(turnID, itemID, field), delta);
}

function syncStreamItem(params: Record<string, unknown> | undefined): void {
  const turnID = params?.turn_id as string | undefined;
  const item = params?.item as ThreadItem | undefined;
  if (!turnID || !item?.id) {
    return;
  }
  const completed = (item.status ?? "in_progress") !== "in_progress";
  const retainTextStream = completed && (item.type === "agent_message" || item.type === "reasoning");
  if (typeof item.text === "string") {
    streamTextStore.set(streamTextKey(turnID, item.id, "text"), item.text);
  }
  if (typeof item.arguments === "string") {
    streamTextStore.set(streamTextKey(turnID, item.id, "arguments"), item.arguments);
  }
  if (typeof item.result === "string") {
    streamTextStore.set(streamTextKey(turnID, item.id, "result"), item.result);
  }
  if (completed && !retainTextStream) {
    window.requestAnimationFrame(() => streamTextStore.clearItem(turnID, item.id));
  }
}

function reduceNotification(state: AppState, notification: AppServerNotification): AppState {
  const params = notification.params as Record<string, unknown> | undefined;
  switch (notification.method) {
    case "thread/started":
    case "thread/resumed": {
      const thread = params?.thread as Thread | undefined;
      if (!thread) {
        return state;
      }
      return { ...state, thread, threads: upsertThread(state.threads, thread), status: "ready" };
    }
    case "turn/started": {
      const turn = params?.turn as Turn | undefined;
      if (!turn) {
        return state;
      }
      return updateThread({ ...state, running: true }, (thread) => upsertTurn(thread, turn));
    }
    case "item/started":
    case "item/completed": {
      const item = params?.item as ThreadItem | undefined;
      const turnID = params?.turn_id as string | undefined;
      if (!item || !turnID) {
        return state;
      }
      return updateThread(state, (thread) => upsertTurnItem(thread, turnID, item));
    }
    case "item/agentMessage/delta":
      return applyDelta(state, params, "text");
    case "item/reasoning/delta":
      return applyDelta(state, params, "text");
    case "item/toolCall/delta":
      return applyDelta(state, params, "arguments");
    case "item/toolCall/outputDelta":
      return applyDelta(state, params, "result");
    case "turn/completed":
    case "turn/error": {
      const turn = params?.turn as Turn | undefined;
      if (!turn) {
        return { ...state, running: false };
      }
      return updateThread({ ...state, running: false, status: "ready" }, (thread) => upsertTurn(thread, turn));
    }
    default:
      return state;
  }
}

function applyDelta(state: AppState, params: Record<string, unknown> | undefined, field: "text" | "arguments" | "result"): AppState {
  const turnID = params?.turn_id as string | undefined;
  const itemID = params?.item_id as string | undefined;
  const delta = params?.delta as string | undefined;
  if (!turnID || !itemID || !delta) {
    return state;
  }
  return updateThread(state, (thread) =>
    updateTurnItem(thread, turnID, itemID, (item) => ({
      ...item,
      [field]: `${item[field] ?? ""}${delta}`
    }))
  );
}

function updateThread(state: AppState, update: (thread: Thread) => Thread): AppState {
  if (!state.thread) {
    return state;
  }
  const thread = update(state.thread);
  return { ...state, thread, threads: upsertThread(state.threads, thread) };
}

function upsertThread(threads: Thread[], thread: Thread | undefined): Thread[] {
  const validThreads = threads.filter(isThread);
  if (!isThread(thread)) {
    return validThreads;
  }
  const index = validThreads.findIndex((item) => item.id === thread.id);
  if (index < 0) {
    return [thread, ...validThreads];
  }
  const next = validThreads.slice();
  next[index] = thread;
  return next;
}

function requireThread(result: { thread?: Thread }, message: string): Thread {
  if (!isThread(result.thread)) {
    throw new Error(message);
  }
  return result.thread;
}

function activeProjectID(context: RuntimeContext | undefined): string | undefined {
  return context?.kind === "project" ? context.project_id : undefined;
}

function sameRuntimeContext(left: RuntimeContext | undefined, right: RuntimeContext | undefined): boolean {
  if (!left || !right || left.kind !== right.kind) {
    return false;
  }
  if (left.kind === "project" && right.kind === "project") {
    return left.project_id === right.project_id;
  }
  return left.cwd === right.cwd;
}

function isThread(value: unknown): value is Thread {
  return Boolean(value && typeof value === "object" && typeof (value as Thread).id === "string");
}

function upsertTurn(thread: Thread, turn: Turn): Thread {
  const index = thread.turns.findIndex((item) => item.id === turn.id);
  if (index < 0) {
    return { ...thread, turns: [...thread.turns, turn], status: turn.status === "in_progress" ? "in_progress" : thread.status };
  }
  const turns = thread.turns.slice();
  turns[index] = turn;
  return { ...thread, turns, status: turn.status === "in_progress" ? "in_progress" : "idle" };
}

function updateTurnItem(thread: Thread, turnID: string, itemID: string, update: (item: ThreadItem) => ThreadItem): Thread {
  const turns = thread.turns.map((turn) => {
    if (turn.id !== turnID) {
      return turn;
    }
    const index = turn.items.findIndex((item) => item.id === itemID);
    if (index < 0) {
      return turn;
    }
    const items = turn.items.slice();
    items[index] = update(items[index]);
    return { ...turn, items };
  });
  return { ...thread, turns };
}

function upsertTurnItem(thread: Thread, turnID: string, item: ThreadItem): Thread {
  const turns = thread.turns.map((turn) => {
    if (turn.id !== turnID) {
      return turn;
    }
    const index = turn.items.findIndex((existing) => existing.id === item.id);
    if (index < 0) {
      return { ...turn, items: [...turn.items, item] };
    }
    const items = turn.items.slice();
    items[index] = item;
    return { ...turn, items };
  });
  return { ...thread, turns };
}

function ProjectList({
  projects,
  activeID,
  threads,
  activeThreadID,
  onSelectProject,
  onSelectThread
}: {
  projects: DesktopProject[];
  activeID?: string;
  threads: Thread[];
  activeThreadID?: string;
  onSelectProject: (id: string) => void;
  onSelectThread: (id: string) => void;
}): JSX.Element {
  return (
    <div className="projects">
      {projects.map((project) => (
        <div key={project.id} className="project-group">
          <button
            className={`project-row ${project.id === activeID ? "active" : ""}`}
            onClick={() => onSelectProject(project.id)}
          >
            <Folder size={18} />
            <span>{project.name}</span>
          </button>
          {project.id === activeID ? (
            <ThreadList threads={threads} activeID={activeThreadID} onSelect={onSelectThread} />
          ) : null}
        </div>
      ))}
    </div>
  );
}

function ThreadList({
  threads,
  activeID,
  onSelect
}: {
  threads: Thread[];
  activeID?: string;
  onSelect: (id: string) => void;
}): JSX.Element {
  const visibleThreads = threads.filter((thread): thread is Thread => Boolean(thread?.id));
  return (
    <div className="thread-list">
      {visibleThreads.map((thread) => (
        <button
          key={thread.id}
          className={`thread-row ${thread.id === activeID ? "active" : ""}`}
          onClick={() => onSelect(thread.id)}
        >
          <span>{thread.preview || "未命名对话"}</span>
        </button>
      ))}
    </div>
  );
}

function TurnView({
  turn,
  cwd,
  onStreamFrame,
  onInterrupt
}: {
  turn: Turn;
  cwd?: string;
  onStreamFrame: () => void;
  onInterrupt: () => void;
}): JSX.Element {
  const renderedItems: JSX.Element[] = [];
  let statusInserted = false;

  for (let index = 0; index < turn.items.length; index++) {
    const item = turn.items[index];
    if (item.type === "user_message") {
      renderedItems.push(
        <ThreadItemView
          key={item.id}
          turnID={turn.id}
          item={item}
          cwd={cwd}
          streaming={false}
          onStreamFrame={onStreamFrame}
        />
      );
      continue;
    }

    if (!statusInserted) {
      renderedItems.push(<TurnStatusLine key={`${turn.id}-status`} turn={turn} onInterrupt={onInterrupt} />);
      statusInserted = true;
    }

    if (item.type === "tool_call" || item.type === "collab_agent_tool_call") {
      const group = [item];
      let nextIndex = index + 1;
      while (
        nextIndex < turn.items.length &&
        (turn.items[nextIndex].type === "tool_call" || turn.items[nextIndex].type === "collab_agent_tool_call")
      ) {
        group.push(turn.items[nextIndex]);
        nextIndex++;
      }
      renderedItems.push(<ToolActivityRow key={`${item.id}-activity`} items={group} />);
      index = nextIndex - 1;
      continue;
    }

    renderedItems.push(
      <ThreadItemView
        key={item.id}
        turnID={turn.id}
        item={item}
        cwd={cwd}
        streaming={turn.status === "in_progress" && item.status === "in_progress"}
        onStreamFrame={onStreamFrame}
      />
    );
  }

  if (!statusInserted && turn.status === "in_progress") {
    renderedItems.push(<TurnStatusLine key={`${turn.id}-status`} turn={turn} onInterrupt={onInterrupt} />);
  }

  return (
    <section className="turn">
      {renderedItems}
      {turn.error ? <div className="turn-error">{turn.error.message}</div> : null}
    </section>
  );
}

function TurnStatusLine({ turn, onInterrupt }: { turn: Turn; onInterrupt: () => void }): JSX.Element {
  const completedDuration = typeof turn.duration_ms === "number" ? turn.duration_ms : undefined;
  const startedAt = Date.parse(turn.started_at);
  const liveDuration = completedDuration === undefined && turn.status === "in_progress" && Number.isFinite(startedAt);
  const liveNow = useLiveNow(liveDuration);
  const elapsedMs =
    completedDuration ?? (Number.isFinite(startedAt) ? Math.max(0, liveNow - startedAt) : 0);
  const content = turnProgressContent(turn, elapsedMs);
  const scene = liveDuration ? turnProgressScene(turn.id) : undefined;

  return (
    <div
      className={`turn-progress ${turn.status}`}
      role={liveDuration ? "status" : undefined}
      aria-live={liveDuration ? "polite" : undefined}
    >
      <div className="turn-progress-header">
        <div className="turn-progress-label">
          <Clock size={17} />
          <span className="turn-progress-copy">
            <span className="turn-progress-title">
              <span>{content.label}</span>
              <span className="turn-progress-duration">{formatDuration(elapsedMs)}</span>
            </span>
            {content.detail ? <span className="turn-progress-detail">{content.detail}</span> : null}
          </span>
        </div>
        {liveDuration ? (
          <button className="turn-progress-stop" type="button" onClick={onInterrupt}>
            <Square size={13} />
            <span>停止</span>
          </button>
        ) : null}
      </div>
      <div className="turn-progress-rule">{scene ? <TurnProgressFightScene scene={scene} /> : null}</div>
    </div>
  );
}

function TurnProgressFightScene({ scene }: { scene: TurnProgressScene }): JSX.Element {
  return (
    <span className={`turn-progress-scene scene-${scene}`} aria-hidden="true">
      <span className="fight-spark spark-one" />
      <span className="fight-spark spark-two" />
      <Stickman className="stickman-a" />
      <Stickman className="stickman-b" />
    </span>
  );
}

function Stickman({ className }: { className: string }): JSX.Element {
  return (
    <span className={`stickman ${className}`}>
      <span className="stickman-figure">
        <span className="stickman-head" />
        <span className="stickman-body" />
        <span className="stickman-arm arm-front" />
        <span className="stickman-arm arm-back" />
        <span className="stickman-leg leg-front" />
        <span className="stickman-leg leg-back" />
        <span className="stickman-weapon" />
      </span>
    </span>
  );
}

function useLiveNow(active: boolean): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!active) {
      return;
    }
    setNow(Date.now());
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [active]);

  return active ? now : Date.now();
}

function turnProgressScene(turnID: string): TurnProgressScene {
  let hash = 0;
  for (let index = 0; index < turnID.length; index++) {
    hash = (hash * 31 + turnID.charCodeAt(index)) >>> 0;
  }
  return TURN_PROGRESS_SCENES[hash % TURN_PROGRESS_SCENES.length];
}

function LiveDuration({ startedAtMs }: { startedAtMs: number }): JSX.Element {
  const nodeRef = useRef<HTMLSpanElement | null>(null);

  useEffect(() => {
    const update = (): void => {
      if (nodeRef.current) {
        nodeRef.current.textContent = formatDuration(Math.max(0, Date.now() - startedAtMs));
      }
    };
    update();
    const timer = window.setInterval(update, 1000);
    return () => window.clearInterval(timer);
  }, [startedAtMs]);

  return <span ref={nodeRef}>{formatDuration(Math.max(0, Date.now() - startedAtMs))}</span>;
}

function turnProgressContent(turn: Turn, elapsedMs: number): TurnProgressContent {
  if (turn.status === "failed") {
    return { label: "处理失败", detail: turn.error?.message ?? "请求没有完成" };
  }
  if (turn.status === "interrupted") {
    return { label: "已停止", detail: "本轮请求已取消" };
  }
  if (turn.status !== "in_progress") {
    return { label: "已处理" };
  }

  const runningTool = turn.items.find(
    (item) =>
      (item.type === "tool_call" || item.type === "collab_agent_tool_call") &&
      (item.status ?? "in_progress") === "in_progress"
  );
  if (runningTool) {
    return { label: "正在处理", detail: `正在调用 ${readableToolName(runningTool.name)}` };
  }

  const latestItem = latestDebugItem(turn);
  if (!latestItem) {
    return {
      label: "正在思考",
      detail: waitingDetail(elapsedMs, "已收到请求，正在等待模型回应")
    };
  }
  if (latestItem.type === "agent_message") {
    const hasText = debugStreamFieldLength(turn.id, latestItem, "text") > 0;
    return {
      label: hasText ? "正在生成回复" : "正在思考",
      detail: hasText ? "正在输出回答" : waitingDetail(elapsedMs, "正在组织回答")
    };
  }
  if (latestItem.type === "reasoning") {
    return {
      label: "正在思考",
      detail: waitingDetail(elapsedMs, "正在组织回答")
    };
  }
  if (latestItem.type === "tool_call" || latestItem.type === "collab_agent_tool_call") {
    return { label: "正在处理", detail: "工具已返回，正在整理结果" };
  }
  if (latestItem.type === "context_compaction") {
    return { label: "正在处理", detail: "正在整理上下文" };
  }
  if (latestItem.type === "error") {
    return { label: "正在处理", detail: "收到错误信息，正在收尾" };
  }

  return { label: "正在处理", detail: waitingDetail(elapsedMs, "请求正在处理中") };
}

function waitingDetail(elapsedMs: number, defaultDetail: string): string {
  if (elapsedMs >= 30_000) {
    return "这个请求比平常更久，仍在等待响应";
  }
  if (elapsedMs >= 8_000) {
    return "请求已开始，正在继续处理";
  }
  return defaultDetail;
}

function ThreadItemView({
  turnID,
  item,
  cwd,
  streaming,
  onStreamFrame
}: {
  turnID: string;
  item: ThreadItem;
  cwd?: string;
  streaming: boolean;
  onStreamFrame: () => void;
}): JSX.Element | null {
  switch (item.type) {
    case "user_message":
      return (
        <div className="message user-message">
          {item.images?.length ? <MessageImageGrid images={item.images} /> : null}
          {item.text ? <RichContent text={item.text} cwd={cwd} /> : null}
        </div>
      );
    case "agent_message":
      return (
        <article className="agent-block">
          <div className="agent-text">
            <AgentMessageContent
              turnID={turnID}
              item={item}
              cwd={cwd}
              streaming={streaming}
              onStreamFrame={onStreamFrame}
            />
          </div>
        </article>
      );
    case "reasoning":
      return (
        <article className="reasoning-block">
          <Brain size={16} />
          <ReasoningContent turnID={turnID} item={item} streaming={streaming} onStreamFrame={onStreamFrame} />
        </article>
      );
    case "tool_call":
    case "collab_agent_tool_call":
      return <ToolActivityRow items={[item]} />;
    case "context_compaction":
      return <div className="system-line">{item.text}</div>;
    case "error":
      return <div className="turn-error">{item.error}</div>;
    default:
      return null;
  }
}

function MessageImageGrid({ images }: { images: InputImage[] }): JSX.Element {
  return (
    <div className="message-images">
      {images.map((image, index) => (
        <img className="message-image" key={`${image.media_type}-${index}`} src={imageSource(image)} alt={`Image ${index + 1}`} />
      ))}
    </div>
  );
}

function AgentMessageContent({
  turnID,
  item,
  cwd,
  streaming,
  onStreamFrame
}: {
  turnID: string;
  item: ThreadItem;
  cwd?: string;
  streaming: boolean;
  onStreamFrame: () => void;
}): JSX.Element {
  const streamKeyValue = streamTextKey(turnID, item.id, "text");
  const hasBufferedStream = streamTextStore.has(streamKeyValue);
  const liveStream = streaming || hasBufferedStream;

  return (
    <StreamingMarkdown
      streamKey={streamKeyValue}
      initialText={hasBufferedStream ? streamTextStore.seedValue(streamKeyValue) : item.text}
      cwd={cwd}
      final={!streaming}
      live={liveStream}
      onFrame={onStreamFrame}
      onSettled={() => {
        streamTextStore.clearItem(turnID, item.id);
        onStreamFrame();
      }}
    />
  );
}

function ReasoningContent({
  turnID,
  item,
  streaming,
  onStreamFrame
}: {
  turnID: string;
  item: ThreadItem;
  streaming: boolean;
  onStreamFrame: () => void;
}): JSX.Element {
  const streamKeyValue = streamTextKey(turnID, item.id, "text");
  const hasBufferedStream = streamTextStore.has(streamKeyValue);
  const liveStream = streaming || hasBufferedStream;

  return (
    <StreamingMarkdown
      streamKey={streamKeyValue}
      initialText={hasBufferedStream ? streamTextStore.seedValue(streamKeyValue) : item.text}
      className="streaming-markdown reasoning-stream"
      final={!streaming}
      live={liveStream}
      onFrame={onStreamFrame}
      onSettled={() => {
        streamTextStore.clearItem(turnID, item.id);
        onStreamFrame();
      }}
    />
  );
}

type ToolActivityKind = "edit" | "create" | "search" | "read" | "list" | "command" | "agent" | "unknown";

type ToolActivitySummary = {
  kind: ToolActivityKind;
  text: string;
  fileName?: string;
  additions: number;
  deletions: number;
  running: boolean;
  failed: boolean;
};

type DiffStats = {
  additions: number;
  deletions: number;
  newFile: boolean;
};

type JsonRecord = Record<string, unknown>;

function runDebugPhaseForState(state: AppState): RunDebugPhase {
  const turn = activeDebugTurn(state.thread);
  if (state.askRequest) {
    return {
      label: "等待用户选择",
      detail: `${state.askRequest.questions.length} 个问题需要响应`,
      tone: "warning",
      turn
    };
  }
  if (!state.initialized) {
    return {
      label: "运行时未就绪",
      detail: state.status || "等待初始化",
      tone: state.status === "connecting" || state.status === "opening" ? "running" : "warning",
      turn
    };
  }
  if (state.running && !turn) {
    return {
      label: "正在发送请求",
      detail: "还没收到 turn/started",
      tone: "running"
    };
  }
  if (turn?.status === "in_progress") {
    const runningTool = turn.items.find(
      (item) =>
        (item.type === "tool_call" || item.type === "collab_agent_tool_call") &&
        (item.status ?? "in_progress") === "in_progress"
    );
    if (runningTool) {
      return {
        label: "正在调用工具",
        detail: readableToolName(runningTool.name),
        tone: "running",
        turn,
        activeItem: runningTool
      };
    }

    const latestItem = latestDebugItem(turn);
    if (!latestItem) {
      return {
        label: "等待模型响应",
        detail: "turn 已开始，尚未收到回复 item",
        tone: "running",
        turn
      };
    }
    if (latestItem.type === "agent_message") {
      const length = debugStreamFieldLength(turn.id, latestItem, "text");
      return {
        label: length > 0 ? "正在生成回复" : "回复已开始",
        detail: length > 0 ? `已收到 ${length.toLocaleString()} 字` : "等待首个回复片段",
        tone: "running",
        turn,
        activeItem: latestItem
      };
    }
    if (latestItem.type === "reasoning") {
      const length = debugStreamFieldLength(turn.id, latestItem, "text");
      return {
        label: "模型正在思考",
        detail: length > 0 ? `已收到 ${length.toLocaleString()} 字思考内容` : "等待推理片段",
        tone: "running",
        turn,
        activeItem: latestItem
      };
    }
    if (latestItem.type === "tool_call" || latestItem.type === "collab_agent_tool_call") {
      return {
        label: "工具已返回",
        detail: "等待模型继续处理工具结果",
        tone: "running",
        turn,
        activeItem: latestItem
      };
    }
    return {
      label: "本轮处理中",
      detail: debugItemTitle(latestItem),
      tone: "running",
      turn,
      activeItem: latestItem
    };
  }
  if (turn?.status === "failed") {
    return {
      label: "处理失败",
      detail: turn.error?.message ?? "本轮返回失败状态",
      tone: "error",
      turn
    };
  }
  if (turn?.status === "interrupted") {
    return {
      label: "已停止",
      detail: "本轮已被中断",
      tone: "warning",
      turn
    };
  }
  if (turn?.status === "completed") {
    return {
      label: "已完成",
      detail: turn.duration_ms === undefined ? "本轮完成" : `耗时 ${formatDuration(turn.duration_ms)}`,
      tone: "success",
      turn
    };
  }
  if (state.running) {
    return {
      label: "运行中",
      detail: state.status || "等待事件",
      tone: "running",
      turn
    };
  }
  return {
    label: state.status === "ready" ? "空闲" : "当前状态",
    detail: state.status === "ready" ? "可以发送新消息" : state.status,
    tone: state.status === "ready" ? "idle" : "warning",
    turn
  };
}

function activeDebugTurn(thread: Thread | undefined): Turn | undefined {
  const turns = thread?.turns ?? [];
  for (let index = turns.length - 1; index >= 0; index--) {
    if (turns[index].status === "in_progress") {
      return turns[index];
    }
  }
  return turns.length > 0 ? turns[turns.length - 1] : undefined;
}

function latestDebugItem(turn: Turn): ThreadItem | undefined {
  for (let index = turn.items.length - 1; index >= 0; index--) {
    const item = turn.items[index];
    if (item.type !== "user_message") {
      return item;
    }
  }
  return undefined;
}

function debugStreamFieldLength(turnID: string, item: ThreadItem, field: StreamTextField): number {
  const key = streamTextKey(turnID, item.id, field);
  const value = streamTextStore.has(key) ? streamTextStore.get(key) : item[field] ?? "";
  return value.length;
}

function runDebugEventFromServerEvent(
  event: ServerEvent,
  deltaSeen: Set<string>
): Omit<RunDebugEvent, "id" | "at"> | undefined {
  switch (event.kind) {
    case "server-request":
      return {
        source: "server",
        method: event.message.method,
        detail: "服务端正在等待客户端响应",
        tone: "warning"
      };
    case "server-error":
      return {
        source: "server",
        method: "server/error",
        detail: event.message,
        tone: "error"
      };
    case "server-exit":
      return {
        source: "server",
        method: "server/exit",
        detail: `app-server 退出：${event.code ?? "unknown"}`,
        tone: "error"
      };
    case "notification":
      return runDebugEventFromNotification(event.message, deltaSeen);
  }
}

function runDebugEventFromNotification(
  notification: AppServerNotification,
  deltaSeen: Set<string>
): Omit<RunDebugEvent, "id" | "at"> | undefined {
  const params = isRecord(notification.params) ? notification.params : undefined;
  const threadID = stringValue(params, "thread_id");
  const turnID = stringValue(params, "turn_id");
  const itemID = stringValue(params, "item_id");

  if (isDeltaNotification(notification.method)) {
    const key = `${notification.method}:${turnID ?? ""}:${itemID ?? ""}`;
    if (deltaSeen.has(key)) {
      return undefined;
    }
    deltaSeen.add(key);
    const delta = stringValue(params, "delta") ?? "";
    return {
      source: "server",
      method: debugNotificationMethodLabel(notification.method),
      detail: `首个片段 ${delta.length.toLocaleString()} 字`,
      tone: "running",
      threadID,
      turnID,
      itemID
    };
  }

  if (notification.method === "turn/event") {
    const payload = recordValue(params, "event");
    const eventType = stringValue(payload, "type") ?? "event";
    if (isHighVolumeStreamEvent(eventType)) {
      return undefined;
    }
    return {
      source: "server",
      method: `event/${eventType}`,
      detail: streamEventDebugDetail(payload),
      tone: streamEventTone(eventType),
      threadID,
      turnID
    };
  }

  if (notification.method === "item/started" || notification.method === "item/completed") {
    const item = threadItemFromRecord(recordValue(params, "item"));
    if (!item) {
      return undefined;
    }
    return {
      source: "server",
      method: notification.method,
      detail: `${debugItemTitle(item)} · ${debugItemStatusLabel(item)}`,
      tone: item.status === "failed" || item.error ? "error" : notification.method === "item/completed" ? "success" : "running",
      threadID,
      turnID,
      itemID: item.id
    };
  }

  if (notification.method === "turn/started") {
    const turn = turnFromRecord(recordValue(params, "turn"));
    return {
      source: "server",
      method: notification.method,
      detail: turn ? `本轮开始：${shortDebugID(turn.id)}` : "本轮开始",
      tone: "running",
      threadID,
      turnID: turn?.id ?? turnID
    };
  }

  if (notification.method === "turn/completed" || notification.method === "turn/error") {
    const turn = turnFromRecord(recordValue(params, "turn"));
    const failed = notification.method === "turn/error" || turn?.status === "failed";
    return {
      source: "server",
      method: notification.method,
      detail: failed ? stringValue(params, "error") ?? "本轮失败" : "本轮完成",
      tone: failed ? "error" : "success",
      threadID,
      turnID: turn?.id ?? turnID
    };
  }

  if (notification.method === "thread/started" || notification.method === "thread/resumed") {
    const thread = threadFromRecord(recordValue(params, "thread"));
    return {
      source: "server",
      method: notification.method,
      detail: thread ? `Thread ${shortDebugID(thread.id)}` : "Thread 已更新",
      tone: "info",
      threadID: thread?.id ?? threadID
    };
  }

  return undefined;
}

function isDeltaNotification(method: string): boolean {
  return (
    method === "item/agentMessage/delta" ||
    method === "item/reasoning/delta" ||
    method === "item/toolCall/delta" ||
    method === "item/toolCall/outputDelta"
  );
}

function isHighVolumeStreamEvent(eventType: string): boolean {
  return eventType === "content_delta" || eventType === "thinking_delta" || eventType === "tool_use_delta";
}

function debugNotificationMethodLabel(method: string): string {
  switch (method) {
    case "item/agentMessage/delta":
      return "reply/first-delta";
    case "item/reasoning/delta":
      return "reasoning/first-delta";
    case "item/toolCall/delta":
      return "tool-args/first-delta";
    case "item/toolCall/outputDelta":
      return "tool-output/first-delta";
    default:
      return method;
  }
}

function streamEventDebugDetail(payload: JsonRecord | undefined): string {
  const eventType = stringValue(payload, "type") ?? "event";
  const toolCall = recordValue(payload, "tool_call");
  const toolName = stringValue(toolCall, "name");
  const stopReason = stringValue(payload, "stop_reason");
  const error = stringValue(payload, "error");
  if (error) {
    return error;
  }
  if (toolName) {
    return readableToolName(toolName);
  }
  if (stopReason) {
    return `stop_reason=${stopReason}`;
  }
  return eventType;
}

function streamEventTone(eventType: string): RunDebugEventTone {
  if (eventType === "error") {
    return "error";
  }
  if (eventType === "done") {
    return "success";
  }
  if (eventType === "reconnect") {
    return "warning";
  }
  if (eventType === "tool_use_start" || eventType === "tool_use_end" || eventType === "lifecycle") {
    return "running";
  }
  return "info";
}

function threadItemFromRecord(record: JsonRecord | undefined): ThreadItem | undefined {
  if (!record || typeof record.id !== "string" || typeof record.type !== "string") {
    return undefined;
  }
  return record as ThreadItem;
}

function turnFromRecord(record: JsonRecord | undefined): Turn | undefined {
  if (!record || typeof record.id !== "string" || !Array.isArray(record.items)) {
    return undefined;
  }
  return record as Turn;
}

function threadFromRecord(record: JsonRecord | undefined): Thread | undefined {
  if (!record || typeof record.id !== "string" || !Array.isArray(record.turns)) {
    return undefined;
  }
  return record as Thread;
}

function debugItemTitle(item: ThreadItem): string {
  switch (item.type) {
    case "user_message":
      return "用户消息";
    case "agent_message":
      return "回复";
    case "reasoning":
      return "思考";
    case "tool_call":
    case "collab_agent_tool_call":
      return `工具：${readableToolName(item.name)}`;
    case "context_compaction":
      return "上下文压缩";
    case "error":
      return "错误";
    default:
      return item.type;
  }
}

function debugItemStatusLabel(item: ThreadItem): string {
  if (item.status === "failed" || item.error) {
    return "失败";
  }
  if ((item.status ?? "in_progress") === "in_progress") {
    return "进行中";
  }
  return "完成";
}

function debugTurnStatusLabel(status: Turn["status"]): string {
  switch (status) {
    case "in_progress":
      return "进行中";
    case "completed":
      return "完成";
    case "failed":
      return "失败";
    case "interrupted":
      return "已停止";
  }
}

function shortDebugID(id: string): string {
  if (id.length <= 12) {
    return id;
  }
  return `${id.slice(0, 6)}…${id.slice(-4)}`;
}

function formatDebugTime(atMs: number): string {
  return new Date(atMs).toLocaleTimeString("zh-CN", {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false
  });
}

function buildRunDebugSnapshot({
  state,
  events,
  queuedMessages,
  guideMessages,
  composerImages
}: {
  state: AppState;
  events: RunDebugEvent[];
  queuedMessages: QueuedComposerMessage[];
  guideMessages: QueuedComposerMessage[];
  composerImages: ComposerImage[];
}): string {
  const phase = runDebugPhaseForState(state);
  const thread = state.thread;
  const turn = phase.turn ?? activeDebugTurn(thread);
  const lines = [
    `phase: ${phase.label} (${phase.detail})`,
    `status: ${state.status}`,
    `running: ${String(state.running)}`,
    `provider: ${state.initialized?.provider ?? "none"}`,
    `model: ${state.initialized?.model ?? "none"}`,
    `effort: ${state.initialized?.effort ?? ""}`,
    `cwd: ${state.activeContext?.cwd ?? thread?.cwd ?? ""}`,
    `thread: ${thread?.id ?? ""}`,
    `turn: ${turn?.id ?? ""}`,
    `turn_status: ${turn?.status ?? ""}`,
    `queued_messages: ${queuedMessages.length}`,
    `guide_messages: ${guideMessages.length}`,
    `composer_images: ${composerImages.length}`
  ];

  lines.push("");
  lines.push("items:");
  if (turn?.items.length) {
    for (const item of turn.items) {
      lines.push(
        `- ${item.id} ${item.type} ${item.status ?? "in_progress"} ${item.name ?? ""} text=${debugStreamFieldLength(
          turn.id,
          item,
          "text"
        )} args=${debugStreamFieldLength(turn.id, item, "arguments")} result=${debugStreamFieldLength(turn.id, item, "result")}`
      );
    }
  } else {
    lines.push("- none");
  }

  lines.push("");
  lines.push("events:");
  for (const event of events.slice(-40)) {
    lines.push(
      `- ${new Date(event.at).toISOString()} ${event.source} ${event.method} ${event.detail} thread=${event.threadID ?? ""} turn=${
        event.turnID ?? ""
      } item=${event.itemID ?? ""}`
    );
  }
  return lines.join("\n");
}

function ToolActivityRow({ items }: { items: ThreadItem[] }): JSX.Element {
  const summary = summarizeToolActivity(items);
  const sections = buildToolActivitySections(items);
  const summaryText = activitySummaryText(sections, summary);
  const [expanded, setExpanded] = useState(summary.running || summary.failed);
  const className = `activity-group${expanded ? " expanded" : ""}${summary.running ? " running" : ""}${
    summary.failed ? " failed" : ""
  }`;

  useEffect(() => {
    if (summary.running || summary.failed) {
      setExpanded(true);
    }
  }, [summary.failed, summary.running]);

  return (
    <article className={className}>
      <button
        className="activity-row activity-toggle"
        type="button"
        aria-expanded={expanded}
        onClick={() => setExpanded((open) => !open)}
      >
        <ActivityIcon kind={summary.kind} failed={summary.failed} />
        <span className="activity-copy">
          <span>{summaryText}</span>
          {summary.fileName ? <span className="activity-file">{summary.fileName}</span> : null}
          {summary.additions > 0 ? <span className="activity-add">+{summary.additions}</span> : null}
          {summary.deletions > 0 ? <span className="activity-delete">-{summary.deletions}</span> : null}
        </span>
        <ChevronDown className="activity-chevron" size={16} />
      </button>
      <div className="activity-details" aria-hidden={!expanded}>
        <div className="activity-details-inner">
          {sections.map((section) => (
            <ToolActivitySectionView key={section.id} section={section} />
          ))}
        </div>
      </div>
    </article>
  );
}

type ToolActivitySectionStatus = "running" | "completed" | "failed";

type ToolActivitySection = {
  id: string;
  kind: ToolActivityKind;
  title: string;
  subtitle?: string;
  detail?: string;
  status: ToolActivitySectionStatus;
  error?: string;
};

function ToolActivitySectionView({ section }: { section: ToolActivitySection }): JSX.Element {
  return (
    <section className="activity-detail">
      <div className="activity-detail-marker">
        <ActivityIcon kind={section.kind} failed={section.status === "failed"} />
      </div>
      <div className="activity-detail-body">
        <div className="activity-detail-title">
          <strong>{section.title}</strong>
          <span>{section.detail ?? section.subtitle}</span>
        </div>
        {section.error ? <div className="activity-detail-error">{section.error}</div> : null}
      </div>
      <span className={`activity-status ${section.status}`}>{toolSectionStatusLabel(section.status)}</span>
    </section>
  );
}

function toolStatusLabel(item: ThreadItem | undefined): string {
  if (!item) {
    return "";
  }
  if (item.status === "failed" || item.error) {
    return "失败";
  }
  if ((item.status ?? "in_progress") === "in_progress") {
    return "运行中";
  }
  return "完成";
}

function toolSectionStatusLabel(status: ToolActivitySectionStatus): string {
  switch (status) {
    case "failed":
      return "未完成";
    case "running":
      return "进行中";
    case "completed":
      return "完成";
  }
}

function buildToolActivitySections(items: ThreadItem[]): ToolActivitySection[] {
  const groups = new Map<string, ThreadItem[]>();
  for (const item of items) {
    const key = toolActivitySectionKey(item);
    groups.set(key, [...(groups.get(key) ?? []), item]);
  }
  return Array.from(groups.entries()).map(([key, grouped]) => toolActivitySectionFromItems(key, grouped));
}

function activitySummaryText(sections: ToolActivitySection[], fallback: ToolActivitySummary): string {
  if (sections.length === 0) {
    return fallback.text;
  }
  const fragments = sections.map((section) => sectionSummaryText(section)).filter(Boolean);
  if (fragments.length === 0) {
    return fallback.text;
  }
  return fallback.failed ? `未完成 · ${fragments.join("，")}` : fragments.join("，");
}

function sectionSummaryText(section: ToolActivitySection): string {
  return section.title;
}

function toolActivitySectionKey(item: ThreadItem): string {
  const name = (item.name ?? "").trim();
  switch (name) {
    case "read_file":
    case "list_files":
      return "read";
    case "grep":
    case "glob":
    case "web_search":
      return "search";
    case "edit_file":
    case "write_file":
      return "change";
    case "run_shell":
    case "git":
    case "start_process":
    case "read_process_output":
    case "stop_process":
      return "command";
    case "spawn_agent":
    case "send_message":
    case "followup_task":
    case "wait_agent":
    case "close_agent":
    case "list_agents":
      return "agent";
    default:
      return "other";
  }
}

function toolActivitySectionFromItems(key: string, items: ThreadItem[]): ToolActivitySection {
  switch (key) {
    case "read":
      return {
        id: key,
        kind: "read",
        title: `查看 ${items.length} 处`,
        detail: compactDetailText(compactToolTargets(items)),
        status: combinedToolStatus(items),
        error: firstToolError(items)
      };
    case "search":
      return {
        id: key,
        kind: "search",
        title: `搜索 ${items.length} 次`,
        detail: compactDetailText(compactSearchTargets(items)),
        status: combinedToolStatus(items),
        error: firstToolError(items)
      };
    case "change":
      return {
        id: key,
        kind: "edit",
        title: `更新 ${items.length} 个文件`,
        detail: compactDetailText(compactToolTargets(items)),
        status: combinedToolStatus(items),
        error: firstToolError(items)
      };
    case "command":
      return {
        id: key,
        kind: "command",
        title: `检查 ${items.length} 项`,
        detail: compactDetailText(compactCommandLabels(items)),
        status: combinedToolStatus(items),
        error: firstToolError(items)
      };
    case "agent":
      return {
        id: key,
        kind: "agent",
        title: `子任务 ${items.length} 项`,
        detail: compactDetailText(compactAgentLabels(items)),
        status: combinedToolStatus(items),
        error: firstToolError(items)
      };
    default:
      return {
        id: key,
        kind: "unknown",
        title: `工具 ${items.length} 项`,
        detail: compactDetailText(uniqueStrings(items.map((item) => readableToolName(item.name)))),
        status: combinedToolStatus(items),
        error: firstToolError(items)
      };
  }
}

function combinedToolStatus(items: ThreadItem[]): ToolActivitySectionStatus {
  if (items.some((item) => item.status === "failed" || item.error)) {
    return "failed";
  }
  if (items.some((item) => (item.status ?? "in_progress") === "in_progress")) {
    return "running";
  }
  return "completed";
}

function firstToolError(items: ThreadItem[]): string | undefined {
  return items.find((item) => item.error)?.error;
}

function compactToolTargets(items: ThreadItem[]): string[] {
  return uniqueStrings(
    items
      .map((item) => {
        const args = parseJSONRecord(item.arguments);
        const result = parseJSONRecord(item.result);
        const path = stringValue(result, "path") ?? stringValue(args, "path") ?? stringValue(args, "file");
        return path ? fileBaseName(path) : undefined;
      })
      .filter((value): value is string => Boolean(value))
  );
}

function compactSearchTargets(items: ThreadItem[]): string[] {
  return uniqueStrings(
    items
      .map((item) => {
        const args = parseJSONRecord(item.arguments);
        return stringValue(args, "pattern") ?? stringValue(args, "query") ?? readableToolName(item.name);
      })
      .filter((value): value is string => Boolean(value))
  );
}

function compactCommandLabels(items: ThreadItem[]): string[] {
  return uniqueStrings(items.map((item) => readableCommandLabel(item)));
}

function compactAgentLabels(items: ThreadItem[]): string[] {
  return uniqueStrings(
    items.map((item) => {
      const args = parseJSONRecord(item.arguments);
      return stringValue(args, "task_name") ?? readableToolName(item.name);
    })
  );
}

function compactDetailText(values: string[]): string | undefined {
  if (values.length === 0) {
    return undefined;
  }
  const shown = values.slice(0, 4).join("、");
  return values.length > 4 ? `${shown} 等 ${values.length} 项` : shown;
}

function readableCommandLabel(item: ThreadItem): string {
  const args = parseJSONRecord(item.arguments);
  const result = parseJSONRecord(item.result);
  const name = (item.name ?? "").trim();
  const command = stringValue(result, "command") ?? stringValue(args, "command") ?? "";
  const subcommand = stringValue(result, "subcommand") ?? stringValue(args, "subcommand") ?? "";
  if (name === "git" || command.startsWith("git ")) {
    if (subcommand === "status" || command.includes("status")) {
      return "检查 Git 状态";
    }
    if (subcommand === "diff" || command.includes("diff")) {
      return "查看代码差异";
    }
    if (subcommand === "log" || command.includes("log")) {
      return "查看提交历史";
    }
    return "执行 Git 操作";
  }
  if (/npm\s+run\s+typecheck|tsc\s+--noEmit/.test(command)) {
    return "检查类型";
  }
  if (/npm\s+run\s+build|vite\s+build|electron-vite\s+build/.test(command)) {
    return "构建应用";
  }
  if (/go\s+test|npm\s+test|pnpm\s+test|yarn\s+test/.test(command)) {
    return "运行测试";
  }
  if (name === "read_process_output") {
    return "读取后台输出";
  }
  if (name === "start_process") {
    return "启动后台任务";
  }
  if (name === "stop_process") {
    return "停止后台任务";
  }
  return "运行命令";
}

function readableToolName(name: string | undefined): string {
  switch ((name ?? "").trim()) {
    case "read_file":
      return "查看文件";
    case "list_files":
      return "查看目录";
    case "grep":
      return "搜索内容";
    case "glob":
      return "匹配文件";
    case "edit_file":
      return "编辑文件";
    case "write_file":
      return "写入文件";
    case "web_search":
      return "搜索网页";
    case "web_fetch":
      return "读取网页";
    case "run_shell":
      return "运行命令";
    case "git":
      return "Git 操作";
    default:
      return name?.trim() || "工具";
  }
}

function uniqueStrings(values: string[]): string[] {
  const out: string[] = [];
  const seen = new Set<string>();
  for (const value of values) {
    const normalized = value.trim();
    if (!normalized || seen.has(normalized)) {
      continue;
    }
    seen.add(normalized);
    out.push(normalized);
  }
  return out;
}

function ActivityIcon({ kind, failed }: { kind: ToolActivityKind; failed: boolean }): JSX.Element {
  if (failed) {
    return <AlertCircle size={18} />;
  }
  switch (kind) {
    case "edit":
    case "create":
      return <Pencil size={18} />;
    case "search":
      return <Search size={18} />;
    case "read":
      return <FileText size={18} />;
    case "list":
      return <ListIcon size={18} />;
    case "command":
      return <Terminal size={18} />;
    case "agent":
      return <MessageSquarePlus size={18} />;
    default:
      return <Wrench size={18} />;
  }
}

function summarizeToolActivity(items: ThreadItem[]): ToolActivitySummary {
  const readFiles = new Set<string>();
  const searchedFiles = new Set<string>();
  const editedFiles = new Set<string>();
  const createdFiles = new Set<string>();
  const unknownTools = new Set<string>();
  let searchCount = 0;
  let listCount = 0;
  let commandCount = 0;
  let agentCount = 0;
  let additions = 0;
  let deletions = 0;
  let running = false;
  let failed = false;
  let primaryKind: ToolActivityKind = "unknown";

  for (const item of items) {
    const name = (item.name ?? "tool").trim() || "tool";
    const args = parseJSONRecord(item.arguments);
    const result = parseJSONRecord(item.result);
    const path = stringValue(result, "path") ?? stringValue(args, "path") ?? stringValue(args, "file");

    running = running || (item.status ?? "in_progress") === "in_progress";
    failed = failed || item.status === "failed" || Boolean(item.error);

    if (name === "read_file") {
      primaryKind = primaryKind === "unknown" ? "read" : primaryKind;
      addPath(readFiles, path);
      continue;
    }
    if (name === "grep" || name === "glob" || name === "web_search") {
      primaryKind = primaryKind === "unknown" || primaryKind === "read" ? "search" : primaryKind;
      searchCount++;
      collectResultFiles(result, searchedFiles);
      continue;
    }
    if (name === "list_files") {
      primaryKind = primaryKind === "unknown" ? "list" : primaryKind;
      listCount++;
      continue;
    }
    if (name === "run_shell" || name === "git") {
      primaryKind = primaryKind === "unknown" ? "command" : primaryKind;
      commandCount++;
      continue;
    }
    if (name === "edit_file" || name === "write_file") {
      const diff = summarizeDiff(result);
      const target = diff.newFile ? createdFiles : editedFiles;
      addPath(target, path);
      additions += diff.additions;
      deletions += diff.deletions;
      primaryKind = diff.newFile ? "create" : "edit";
      continue;
    }
    if (name === "spawn_agent" || name === "fork_agent" || name === "send_message") {
      primaryKind = primaryKind === "unknown" ? "agent" : primaryKind;
      agentCount++;
      continue;
    }
    unknownTools.add(name);
  }

  const singleChangedFile = editedFiles.size + createdFiles.size === 1 && items.length === 1;
  if (singleChangedFile) {
    const created = createdFiles.size === 1;
    const filePath = firstSetValue(created ? createdFiles : editedFiles);
    return {
      kind: created ? "create" : "edit",
      text: failed ? "编辑失败" : created ? (running ? "正在创建" : "已创建") : running ? "正在编辑" : "已编辑",
      fileName: filePath ? fileBaseName(filePath) : undefined,
      additions,
      deletions,
      running,
      failed
    };
  }

  const parts: string[] = [];
  if (createdFiles.size > 0) {
    parts.push(`已创建 ${createdFiles.size} 个文件`);
  }
  if (editedFiles.size > 0) {
    parts.push(`已编辑 ${editedFiles.size} 个文件`);
  }
  if (readFiles.size > 0) {
    parts.push(`已探索 ${readFiles.size} 个文件`);
  }
  if (searchedFiles.size > 0) {
    parts.push(`已搜索 ${searchedFiles.size} 个文件`);
  }
  if (searchCount > 0) {
    parts.push(`${searchCount} 次搜索`);
  }
  if (listCount > 0) {
    parts.push(`${listCount} 次列表`);
  }
  if (commandCount > 0) {
    parts.push(`已运行 ${commandCount} 条命令`);
  }
  if (agentCount > 0) {
    parts.push(`已启动 ${agentCount} 个子任务`);
  }
  if (parts.length === 0 && unknownTools.size > 0) {
    const names = Array.from(unknownTools).slice(0, 2).join("、");
    parts.push(`${running ? "正在调用" : "已调用"} ${names}`);
  }
  if (parts.length === 0) {
    parts.push(running ? "正在使用工具" : "已使用工具");
  }

  return {
    kind: primaryKind,
    text: `${failed ? "工具失败 · " : ""}${parts.join(" · ")}`,
    additions,
    deletions,
    running,
    failed
  };
}

function summarizeDiff(result: JsonRecord | undefined): DiffStats {
  const diff = recordValue(result, "diff");
  if (!diff) {
    return { additions: 0, deletions: 0, newFile: false };
  }
  const newFile = diff.new_file === true;
  if (newFile) {
    return { additions: numberValue(diff, "lines") ?? 0, deletions: 0, newFile };
  }

  let additions = 0;
  let deletions = 0;
  for (const hunk of arrayValue(diff, "hunks")) {
    if (!isRecord(hunk)) {
      continue;
    }
    for (const line of arrayValue(hunk, "lines")) {
      if (!isRecord(line)) {
        continue;
      }
      if (line.op === "insert") {
        additions++;
      } else if (line.op === "delete") {
        deletions++;
      }
    }
  }
  return { additions, deletions, newFile };
}

function collectResultFiles(result: JsonRecord | undefined, output: Set<string>): void {
  for (const file of arrayValue(result, "files")) {
    if (typeof file === "string" && file.trim()) {
      output.add(file.trim());
    }
  }
  for (const match of arrayValue(result, "matches")) {
    if (isRecord(match)) {
      addPath(output, stringValue(match, "file"));
    }
  }
  for (const count of arrayValue(result, "counts")) {
    if (isRecord(count)) {
      addPath(output, stringValue(count, "file"));
    }
  }
}

function parseJSONRecord(value: string | undefined): JsonRecord | undefined {
  if (!value?.trim()) {
    return undefined;
  }
  try {
    const parsed: unknown = JSON.parse(value);
    return isRecord(parsed) ? parsed : undefined;
  } catch {
    return undefined;
  }
}

function isRecord(value: unknown): value is JsonRecord {
  return Boolean(value && typeof value === "object" && !Array.isArray(value));
}

function recordValue(record: JsonRecord | undefined, key: string): JsonRecord | undefined {
  if (!record) {
    return undefined;
  }
  const value = record[key];
  return isRecord(value) ? value : undefined;
}

function arrayValue(record: JsonRecord | undefined, key: string): unknown[] {
  if (!record) {
    return [];
  }
  const value = record[key];
  return Array.isArray(value) ? value : [];
}

function stringValue(record: JsonRecord | undefined, key: string): string | undefined {
  if (!record) {
    return undefined;
  }
  const value = record[key];
  return typeof value === "string" && value.trim() ? value.trim() : undefined;
}

function numberValue(record: JsonRecord | undefined, key: string): number | undefined {
  if (!record) {
    return undefined;
  }
  const value = record[key];
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function addPath(output: Set<string>, path: string | undefined): void {
  if (path?.trim()) {
    output.add(path.trim());
  }
}

function firstSetValue(values: Set<string>): string | undefined {
  for (const value of values) {
    return value;
  }
  return undefined;
}

function fileBaseName(path: string): string {
  const normalized = path.replace(/\\/g, "/");
  const parts = normalized.split("/").filter(Boolean);
  return parts[parts.length - 1] ?? path;
}

function formatDuration(ms: number): string {
  const totalSeconds = Math.max(0, Math.floor(ms / 1000));
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  if (hours > 0) {
    return `${hours}h ${minutes}m ${seconds}s`;
  }
  if (minutes > 0) {
    return `${minutes}m ${seconds}s`;
  }
  return `${seconds}s`;
}

function RuntimeLoading({
  status,
  pinned = false,
  onExitPreview
}: {
  status: string;
  pinned?: boolean;
  onExitPreview?: () => void;
}): JSX.Element {
  const isStarting = pinned || status === "connecting" || status === "opening";
  return (
    <div className="project-empty-pane">
      {isStarting ? (
        <div className="wuu-launch" role="status" aria-label={pinned ? "wuu 启动动画预览" : "wuu 正在启动"}>
          <div className="wuu-launch-mark" aria-hidden="true">
            <span>w</span>
            <span>u</span>
            <span>u</span>
          </div>
          <div className="wuu-launch-rail" aria-hidden="true" />
          {pinned && onExitPreview ? (
            <button className="wuu-launch-exit" type="button" onClick={onExitPreview}>
              退出预览
            </button>
          ) : null}
        </div>
      ) : (
        <div className="project-empty-content">
          <h2>{status}</h2>
        </div>
      )}
    </div>
  );
}

function SettingsView({
  initialized,
  running,
  onBack,
  onSave
}: {
  initialized?: InitializeResult;
  running: boolean;
  onBack: () => void;
  onSave: (provider: string, model: string) => Promise<void>;
}): JSX.Element {
  const providers = initialized?.providers ?? [];
  const [providerDraft, setProviderDraft] = useState(initialized?.provider ?? "");
  const [modelDraft, setModelDraft] = useState(initialized?.model ?? "");
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    setProviderDraft(initialized?.provider ?? "");
    setModelDraft(initialized?.model ?? "");
    setError("");
    setSaved(false);
  }, [initialized?.provider, initialized?.model]);

  function changeProvider(provider: string): void {
    setProviderDraft(provider);
    setSaved(false);
    const summary = providers.find((item) => item.name === provider);
    if (summary) {
      setModelDraft(summary.model);
    }
  }

  async function submit(event: ReactFormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault();
    setError("");
    setSaved(false);
    try {
      await onSave(providerDraft, modelDraft);
      setSaved(true);
    } catch (saveError) {
      setError(saveError instanceof Error ? saveError.message : "保存失败");
    }
  }

  const disabled =
    running ||
    !providerDraft.trim() ||
    !modelDraft.trim() ||
    (providerDraft === initialized?.provider && modelDraft === initialized?.model);

  return (
    <div className="settings-shell">
      <aside className="settings-sidebar">
        <div className="traffic-spacer" />
        <button className="settings-back-button" type="button" onClick={onBack}>
          <ArrowLeft size={17} />
          <span>返回应用</span>
        </button>
        <nav className="settings-nav" aria-label="设置">
          <button className="settings-nav-item active" type="button">
            <Settings size={18} />
            <span>常规</span>
          </button>
        </nav>
      </aside>
      <OverlayScrollbarsComponent
        element="main"
        className="settings-main"
        data-overlayscrollbars-initialize
        defer
        options={OVERLAY_SCROLLBAR_OPTIONS}
      >
        <div className="settings-page">
          <h1>常规</h1>

          <section className="settings-section">
            <div>
              <h2>模型</h2>
              <p>选择 wuu 使用的 Provider 和模型。</p>
            </div>
            <form className="settings-card" onSubmit={submit}>
              <label className="settings-row">
                <span>
                  <strong>Provider</strong>
                  <small>选择当前会话运行时使用的模型服务</small>
                </span>
                {providers.length > 0 ? (
                  <select value={providerDraft} onChange={(event) => changeProvider(event.target.value)} disabled={running}>
                    {providers.map((provider) => (
                      <option key={provider.name} value={provider.name}>
                        {provider.name}
                      </option>
                    ))}
                  </select>
                ) : (
                  <input
                    value={providerDraft}
                    onChange={(event) => {
                      setProviderDraft(event.target.value);
                      setSaved(false);
                    }}
                    disabled={running}
                  />
                )}
              </label>
              <label className="settings-row">
                <span>
                  <strong>模型</strong>
                  <small>Provider 配置里的模型名称</small>
                </span>
                <input
                  value={modelDraft}
                  onChange={(event) => {
                    setModelDraft(event.target.value);
                    setSaved(false);
                  }}
                  disabled={running}
                />
              </label>
              <div className="settings-card-footer">
                {error ? <div className="settings-error">{error}</div> : null}
                {saved ? <div className="settings-saved">已保存</div> : null}
                <button type="submit" disabled={disabled}>
                  保存
                </button>
              </div>
            </form>
          </section>
        </div>
      </OverlayScrollbarsComponent>
    </div>
  );
}

function ComposerImageStrip({
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

function Composer({
  variant = "dock",
  prompt,
  setPrompt,
  images,
  queuedMessages,
  guideMessages,
  running,
  status,
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
  onSelectCodexModel,
  onSelectCodexEffort,
  onToggleModeMenu,
  onToggleBranchMenu,
  onOpenSettings,
  onSelectProject,
  onSelectNoProject,
  onSelectGitBranch,
  onCreateProject,
  onOpenProject,
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
  prompt: string;
  setPrompt: (value: string) => void;
  images: ComposerImage[];
  queuedMessages: QueuedComposerMessage[];
  guideMessages: QueuedComposerMessage[];
  running: boolean;
  status: string;
  initialized?: InitializeResult;
  gitStatus?: GitStatusResult;
  projects: DesktopProject[];
  activeContext?: RuntimeContext;
  activeProject?: DesktopProject;
  codexModels: CodexModelLoadState;
  codexRuntimeMenu: CodexRuntimeMenu;
  codexRuntimeRef: RefObject<HTMLDivElement>;
  menuOpen: boolean;
  accessMenuOpen: boolean;
  modeMenuOpen: boolean;
  branchMenuOpen: boolean;
  menuRef: RefObject<HTMLDivElement>;
  accessMenuRef: RefObject<HTMLDivElement>;
  projectFilter: string;
  setProjectFilter: (value: string) => void;
  onToggleMenu: () => void;
  onToggleAccessMenu: () => void;
  onToggleCodexRuntimeMenu: (menu: Exclude<CodexRuntimeMenu, null>) => void;
  onSelectCodexModel: (model: CodexModelSummary) => void;
  onSelectCodexEffort: (effort: string) => void;
  onToggleModeMenu: () => void;
  onToggleBranchMenu: () => void;
  onOpenSettings: () => void;
  onSelectProject: (id: string) => void;
  onSelectNoProject: () => void;
  onSelectGitBranch: (branch: string) => void;
  onCreateProject: () => void;
  onOpenProject: () => void;
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
  const codexProvider = initialized ? isCodexProvider(initialized) : false;
  const hasDraft = prompt.trim().length > 0 || images.length > 0;
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
        <div className="composer">
          {images.length > 0 ? <ComposerImageStrip images={images} onRemoveImage={onRemoveImage} /> : null}
          <textarea
            value={prompt}
            placeholder={images.length > 0 ? "添加描述" : "尽管问"}
            onChange={(event) => setPrompt(event.target.value)}
            onPaste={(event) => {
              const files = clipboardImageFiles(event);
              if (files.length === 0) {
                return;
              }
              event.preventDefault();
              onPasteImageFiles(files);
            }}
            onKeyDown={(event) => {
              if (event.key === "Enter" && !event.shiftKey) {
                event.preventDefault();
                onSend();
              }
            }}
          />
          <div className="composer-bar">
            <button className="composer-tool-button" type="button" aria-label="打开项目" onClick={onOpenProject}>
              <Plus size={20} />
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
              {accessMenuOpen ? <AccessMenu /> : null}
            </div>
            <div className="composer-spacer" />
            {codexProvider && initialized ? (
              <CodexRuntimePicker
                initialized={initialized}
                state={codexModels}
                openMenu={codexRuntimeMenu}
                anchorRef={codexRuntimeRef}
                running={running}
                onToggleMenu={onToggleCodexRuntimeMenu}
                onSelectModel={onSelectCodexModel}
                onSelectEffort={onSelectCodexEffort}
              />
            ) : (
              <>
                <button className="provider-pill" type="button" onClick={onOpenSettings}>
                  {initialized?.provider ?? "provider"}
                </button>
                <button className="model-label" type="button" onClick={onOpenSettings}>
                  {initialized?.model ?? "model"}
                </button>
              </>
            )}
            {statusText ? <span className="status-label">{statusText}</span> : null}
            {running ? (
              <>
                <button
                  className="send-button"
                  type="button"
                  aria-label="排队发送"
                  title="排队发送"
                  disabled={!hasDraft}
                  onClick={onSend}
                >
                  <Send size={18} />
                </button>
                <button className="stop-button" type="button" onClick={onInterrupt} aria-label="停止" title="停止">
                  <Square size={17} />
                </button>
              </>
            ) : (
              <button className="send-button" type="button" onClick={onSend} aria-label="发送" disabled={!hasDraft}>
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
            <ModeMenu
              activeContext={activeContext}
              onSelectNoProject={onSelectNoProject}
              onOpenProject={onOpenProject}
            />
          ) : null}
          {branchMenuOpen && gitStatus?.is_repo ? (
            <BranchMenu gitStatus={gitStatus} onSelectBranch={onSelectGitBranch} />
          ) : null}
          {menuOpen ? (
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
          ) : null}
        </div>
      </div>
    </div>
  );
  return variant === "hero" ? <div className={className}>{content}</div> : <footer className={className}>{content}</footer>;
}

function CodexRuntimePicker({
  initialized,
  state,
  openMenu,
  anchorRef,
  running,
  onToggleMenu,
  onSelectModel,
  onSelectEffort
}: {
  initialized: InitializeResult;
  state: CodexModelLoadState;
  openMenu: CodexRuntimeMenu;
  anchorRef: RefObject<HTMLDivElement>;
  running: boolean;
  onToggleMenu: (menu: Exclude<CodexRuntimeMenu, null>) => void;
  onSelectModel: (model: CodexModelSummary) => void;
  onSelectEffort: (effort: string) => void;
}): JSX.Element {
  const currentModel = state.models.find((model) => model.slug === initialized.model);
  const effort = initialized.effort ?? "";
  const effortOptions = codexEffortOptions(currentModel, effort);
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
        <span>{shortCodexModelLabel(initialized.model)}</span>
        <span className="codex-runtime-effort">{codexEffortLabel(effort)}</span>
        <ChevronDown size={15} />
      </button>
      {openMenu === "main" ? (
        <CodexMainMenu
          selectedEffort={effort}
          options={effortOptions}
          currentModel={currentModel}
          fallbackModel={initialized.model}
          onSelectEffort={onSelectEffort}
          onOpenModelMenu={() => onToggleMenu("model")}
        />
      ) : null}
      {openMenu === "model" ? (
        <CodexModelMenu
          state={state}
          selectedModel={initialized.model}
          onSelectModel={onSelectModel}
        />
      ) : null}
    </div>
  );
}

function CodexMainMenu({
  selectedEffort,
  options,
  currentModel,
  fallbackModel,
  onSelectEffort,
  onOpenModelMenu
}: {
  selectedEffort: string;
  options: string[];
  currentModel?: CodexModelSummary;
  fallbackModel: string;
  onSelectEffort: (effort: string) => void;
  onOpenModelMenu: () => void;
}): JSX.Element {
  return (
    <div className="codex-runtime-menu codex-main-menu" role="menu">
      {options.map((effort) => {
        const selected = effort === selectedEffort;
        return (
          <button key={effort || "auto"} role="menuitem" type="button" onClick={() => onSelectEffort(effort)}>
            <span>{codexEffortLabel(effort)}</span>
            {selected ? <Check size={18} /> : null}
          </button>
        );
      })}
      <div className="codex-menu-separator" />
      <button role="menuitem" type="button" onClick={onOpenModelMenu}>
        <span>{currentModel ? displayCodexModelName(currentModel) : fallbackModel}</span>
        <ChevronRight className="codex-menu-chevron" size={18} />
      </button>
    </div>
  );
}

function CodexModelMenu({
  state,
  selectedModel,
  onSelectModel
}: {
  state: CodexModelLoadState;
  selectedModel: string;
  onSelectModel: (model: CodexModelSummary) => void;
}): JSX.Element {
  return (
    <div className="codex-runtime-menu codex-model-menu" role="menu">
      <div className="codex-menu-label">模型</div>
      {state.loading ? <div className="composer-menu-empty">正在加载 Codex 模型</div> : null}
      {state.error ? (
        <div className="composer-menu-note warning">
          <strong>无法读取 Codex 登录态</strong>
          <span>{state.error}</span>
        </div>
      ) : null}
      {!state.loading && !state.error && state.models.length === 0 ? (
        <div className="composer-menu-empty">没有可用模型</div>
      ) : null}
      {state.models.map((model) => {
        const selected = model.slug === selectedModel;
        return (
          <button key={model.slug} role="menuitem" type="button" onClick={() => onSelectModel(model)}>
            <span>{displayCodexModelName(model)}</span>
            {selected ? <Check size={18} /> : null}
          </button>
        );
      })}
    </div>
  );
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

function AskUserMessage({
  request,
  onCancel,
  onSubmit
}: {
  request: AskRequestState;
  onCancel: (request: AskRequestState) => Promise<void>;
  onSubmit: (request: AskRequestState, answers: Record<string, string>) => Promise<void>;
}): JSX.Element {
  const [answers, setAnswers] = useState<Record<string, string[]>>(() => {
    const initial: Record<string, string[]> = {};
    for (const question of request.questions) {
      initial[question.question] = question.options[0] ? [question.options[0].label] : [];
    }
    return initial;
  });
  const [submitting, setSubmitting] = useState(false);
  const flatAnswers = useMemo(() => {
    const output: Record<string, string> = {};
    for (const question of request.questions) {
      output[question.question] = (answers[question.question] ?? []).join(", ");
    }
    return output;
  }, [answers, request.questions]);
  const allQuestionsAnswered = request.questions.every((question) => (answers[question.question] ?? []).length > 0);

  function select(question: AskUserQuestion, label: string): void {
    setAnswers((current) => {
      const existing = current[question.question] ?? [];
      if (!question.multi_select) {
        return { ...current, [question.question]: [label] };
      }
      const next = existing.includes(label) ? existing.filter((item) => item !== label) : [...existing, label];
      return { ...current, [question.question]: next };
    });
  }

  async function cancel(): Promise<void> {
    if (submitting) {
      return;
    }
    setSubmitting(true);
    try {
      await onCancel(request);
    } finally {
      setSubmitting(false);
    }
  }

  async function submit(): Promise<void> {
    if (submitting || !allQuestionsAnswered) {
      return;
    }
    setSubmitting(true);
    try {
      await onSubmit(request, flatAnswers);
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <article className="ask-message" aria-live="polite">
      <div className="ask-header">
        <div className="ask-title">
          <MessageSquarePlus size={17} />
          <span>需要你选择</span>
        </div>
        <button
          className="icon-button ask-dismiss"
          type="button"
          aria-label="取消这次提问"
          disabled={submitting}
          onClick={() => void cancel()}
        >
          <X size={17} />
        </button>
      </div>
      <div className="ask-body">
        {request.questions.map((question) => {
          const selectedAnswers = answers[question.question] ?? [];
          return (
            <section key={question.question} className="ask-question">
              <div className="ask-question-meta">
                <div className="ask-chip">{question.header}</div>
                {question.multi_select ? <div className="ask-chip secondary">可多选</div> : null}
              </div>
              <h3>{question.question}</h3>
              <div
                className="ask-options"
                role={question.multi_select ? "group" : "radiogroup"}
                aria-label={question.question}
              >
                {question.options.map((option) => {
                  const selected = selectedAnswers.includes(option.label);
                  return (
                    <button
                      key={option.label}
                      className={`ask-option ${selected ? "selected" : ""}`}
                      type="button"
                      role={question.multi_select ? undefined : "radio"}
                      aria-checked={question.multi_select ? undefined : selected}
                      aria-pressed={question.multi_select ? selected : undefined}
                      disabled={submitting}
                      onClick={() => select(question, option.label)}
                    >
                      <span className="ask-option-check" aria-hidden="true">
                        {selected ? <Check size={15} /> : null}
                      </span>
                      <span className="ask-option-copy">
                        <strong>{option.label}</strong>
                        {option.description ? <span>{option.description}</span> : null}
                      </span>
                    </button>
                  );
                })}
              </div>
            </section>
          );
        })}
      </div>
      <div className="ask-footer">
        <span>{request.questions.length > 1 ? `${request.questions.length} 个问题` : "等待你的选择"}</span>
        <div className="ask-actions">
          <button className="secondary-button" type="button" disabled={submitting} onClick={() => void cancel()}>
            取消
          </button>
          <button className="primary-button" type="button" disabled={submitting || !allQuestionsAnswered} onClick={() => void submit()}>
            提交
          </button>
        </div>
      </div>
    </article>
  );
}
