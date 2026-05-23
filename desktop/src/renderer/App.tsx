import {
  AlertCircle,
  Archive,
  ArrowLeft,
  Brain,
  Bug,
  Check,
  ChevronDown,
  ChevronRight,
  Copy,
  CornerDownRight,
  Clock,
  FileText,
  Film,
  Folder,
  FolderX,
  FolderOpen,
  FolderPlus,
  Github,
  GitBranch,
  Image as ImageIcon,
  Info,
  Laptop,
  List as ListIcon,
  MessageSquarePlus,
  MoreHorizontal,
  PanelBottomOpen,
  PanelLeftOpen,
  PanelRightOpen,
  Pencil,
  Pin,
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
  Fragment,
  memo,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState
} from "react";
import { createPortal } from "react-dom";
import type { PartialOptions } from "overlayscrollbars";
import { OverlayScrollbarsComponent } from "overlayscrollbars-react";
import type {
  Agent,
  AppServerNotification,
  AskUserQuestion,
  AskUserResponse,
  CodexModelSummary,
  DesktopProject,
  FileTreeListResult,
  GitChangeFile,
  GitChangesResult,
  GitCommitResult,
  GitFileDiffResult,
  GitPullRequestResult,
  GitStatusResult,
  InputImage,
  InitializeResult,
  ProjectListResult,
  RuntimeContext,
  ServerEvent,
  TerminalCommandEvent,
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
  threadID?: string;
  questions: AskUserQuestion[];
};

type AnsweredAskRequestState = AskRequestState & {
  answers: Record<string, string>;
  cancelled: boolean;
  turnID?: string;
};

const ASK_USER_OTHER_VALUE = "__wuu_other__";

type CodexModelLoadState = {
  provider?: string;
  loading: boolean;
  error: string;
  models: CodexModelSummary[];
};

type CodexRuntimeMenu = "main" | "model" | null;
type EnvironmentPanelMenu = "mode" | "branch" | "sources" | null;
type EnvironmentDialog = "commit" | "pull-request" | null;
type FloatingMenuOwner = "composer-runtime" | "composer-access" | "codex-runtime";
type FloatingMenuPlacement = "above" | "below";
type FloatingMenuAlign = "left" | "right";
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

type GitChangeTreeNode = {
  kind: "directory" | "file";
  id: string;
  name: string;
  path: string;
  children: GitChangeTreeNode[];
  file?: GitChangeFile;
  additions: number;
  deletions: number;
  fileCount: number;
  binary: boolean;
};

type GitDiffDisplayLine = {
  content: string;
  kind: string;
  oldLine?: number;
  newLine?: number;
};

type TurnProgressContent = {
  label: string;
  detail?: string;
};

type TurnProgressEra =
  | "sticks"
  | "swords"
  | "fortress"
  | "cannon"
  | "factory"
  | "armor"
  | "rockets"
  | "orbit"
  | "galaxy";

type TurnProgressCampaign = {
  currentEra: TurnProgressEra;
  nextEra: TurnProgressEra;
  currentLayer: "a" | "b";
  transitionProgress: number;
  variant: number;
};

const TURN_PROGRESS_ERA_MS = 4 * 60 * 1000;
const TURN_PROGRESS_TRANSITION_MS = 30 * 1000;
const TURN_PROGRESS_PREVIEW_MS = 72 * 1000;
const TURN_PROGRESS_ERAS: TurnProgressEra[] = [
  "sticks",
  "swords",
  "fortress",
  "cannon",
  "factory",
  "armor",
  "rockets",
  "orbit",
  "galaxy"
];
const TURN_PROGRESS_PREVIEW_SPEED = (TURN_PROGRESS_ERA_MS * TURN_PROGRESS_ERAS.length) / TURN_PROGRESS_PREVIEW_MS;
const TURN_PROGRESS_CAMPAIGN_MS = TURN_PROGRESS_ERA_MS * TURN_PROGRESS_ERAS.length;
const TURN_PROGRESS_ERA_LABELS: Record<TurnProgressEra, string> = {
  sticks: "木棍",
  swords: "刀剑",
  fortress: "城堡",
  cannon: "火炮",
  factory: "工厂",
  armor: "装甲",
  rockets: "火箭",
  orbit: "轨道",
  galaxy: "星际"
};

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
  askRequests: AskRequestState[];
  answeredAskRequests: AnsweredAskRequestState[];
};

const initialState: AppState = {
  projects: [],
  threads: [],
  running: false,
  status: "connecting",
  askRequests: [],
  answeredAskRequests: []
};

const SIDEBAR_DEFAULT_WIDTH = 326;
const SIDEBAR_MIN_WIDTH = 240;
const SIDEBAR_MAX_WIDTH = 520;
const SIDEBAR_STEP = 24;
const SIDEBAR_WIDTH_KEY = "wuu.desktop.sidebarWidth";
const SIDEBAR_COLLAPSED_KEY = "wuu.desktop.sidebarCollapsed";
const WORKSPACE_REVIEW_TREE_DEFAULT_WIDTH = 280;
const WORKSPACE_REVIEW_TREE_MIN_WIDTH = 240;
const WORKSPACE_REVIEW_TREE_MAX_WIDTH = 520;
const WORKSPACE_REVIEW_DIFF_MIN_WIDTH = 140;
const WORKSPACE_REVIEW_TREE_STEP = 24;
const WORKSPACE_REVIEW_TREE_WIDTH_KEY = "wuu.desktop.reviewTreeWidth";
const CONVERSATION_AUTO_SCROLL_THRESHOLD_PX = 48;
const CONVERSATION_SCROLLBAR_HIDE_DELAY_MS = 700;
const IMAGE_MAX_DIMENSION = 2000;
const IMAGE_TARGET_BYTES = (5 * 1024 * 1024 * 3) / 4;
const RENDERER_ENV = (
  import.meta as ImportMeta & { env?: { DEV?: boolean; VITE_ENABLE_RUN_DEBUG_PANEL?: string } }
).env;
const ENABLE_LAUNCH_PREVIEW = Boolean(RENDERER_ENV?.DEV);
const ENABLE_RUN_DEBUG_PANEL = Boolean(RENDERER_ENV?.DEV || RENDERER_ENV?.VITE_ENABLE_RUN_DEBUG_PANEL === "true");
const ENABLE_AGENT_TREE_DEMO = Boolean(RENDERER_ENV?.DEV);
const ENABLE_TURN_PROGRESS_EXPERIMENT = false;
const WORKSPACE_FILE_TREE_STYLE: CSSProperties = {
  contain: "strict",
  height: "100%",
  minHeight: 0,
  minWidth: 0,
  width: "100%"
};
const WORKSPACE_FILE_TREE_ITEM_HEIGHT = 28;

type SidebarResizeSession = {
  startX: number;
  startWidth: number;
};

type ComposerVariant = "dock" | "hero";
type WorkspacePanelView = "files" | "review" | "terminal";
type WorkspaceRightPanelView = "tools" | WorkspacePanelView;

const WORKSPACE_TOOL_ITEMS: Array<{
  id: WorkspacePanelView;
  title: string;
  subtitle: string;
}> = [
  { id: "files", title: "文件", subtitle: "浏览项目文件" },
  { id: "review", title: "审查", subtitle: "查看代码更改" },
  { id: "terminal", title: "终端", subtitle: "运行 shell 命令" }
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

function initialWorkspaceReviewTreeWidth(): number {
  const stored = Number(window.localStorage.getItem(WORKSPACE_REVIEW_TREE_WIDTH_KEY));
  if (!Number.isFinite(stored)) {
    return WORKSPACE_REVIEW_TREE_DEFAULT_WIDTH;
  }
  return clamp(stored, WORKSPACE_REVIEW_TREE_MIN_WIDTH, WORKSPACE_REVIEW_TREE_MAX_WIDTH);
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}

function clampWorkspaceReviewTreeWidth(width: number, panelWidth = Number.POSITIVE_INFINITY): number {
  if (!Number.isFinite(panelWidth)) {
    return clamp(width, WORKSPACE_REVIEW_TREE_MIN_WIDTH, WORKSPACE_REVIEW_TREE_MAX_WIDTH);
  }
  const maxForPanel = Math.max(
    WORKSPACE_REVIEW_TREE_MIN_WIDTH,
    Math.min(WORKSPACE_REVIEW_TREE_MAX_WIDTH, panelWidth - WORKSPACE_REVIEW_DIFF_MIN_WIDTH)
  );
  return clamp(width, WORKSPACE_REVIEW_TREE_MIN_WIDTH, maxForPanel);
}

function isInsideFloatingMenu(target: Node, owner: FloatingMenuOwner): boolean {
  const element = target instanceof Element ? target : target.parentElement;
  return Boolean(element?.closest(`[data-floating-menu-owner="${owner}"]`));
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

function createAgentTreeDemo(cwd: string, initialized?: InitializeResult): { parent: Thread; children: Thread[] } {
  const base = Date.now();
  const at = (offsetMs: number): string => new Date(base + offsetMs).toISOString();
  const parentID = `demo-agent-tree-${base}`;
  const inspectID = `${parentID}-inspect-sidebar`;
  const resumeID = `${parentID}-resume-child`;
  const provider = initialized?.provider ?? "demo-provider";
  const model = initialized?.model ?? "demo-model";
  const parentTurnID = `${parentID}-turn-0001`;
  const inspectTurnID = `${inspectID}-turn-0001`;
  const resumeTurnID = `${resumeID}-turn-0001`;

  const parent: Thread = {
    id: parentID,
    preview: "模拟：父 agent 分发子任务",
    model_provider: provider,
    model,
    cwd,
    status: "idle",
    created_at: at(0),
    updated_at: at(4000),
    turns: [
      {
        id: parentTurnID,
        items_view: "full",
        status: "completed",
        items: [
          {
            id: `${parentTurnID}-item-1`,
            type: "user_message",
            status: "completed",
            role: "user",
            text: "把左侧 session 树改成可以展示父 agent 和子 agent 的层级。"
          },
          {
            id: `${parentTurnID}-item-2`,
            type: "collab_agent_tool_call",
            status: "completed",
            name: "spawn_agent",
            arguments: `{"task_name":"检查左侧树","message":"验证子 agent 行的视觉和状态"}`,
            result: `{"agent_id":"${inspectID}","agent_path":"/root/inspect-sidebar","status":"completed"}`
          },
          {
            id: `${parentTurnID}-item-3`,
            type: "collab_agent_tool_call",
            status: "completed",
            name: "spawn_agent",
            arguments: `{"task_name":"子会话恢复","message":"确认点击子 agent 后能正常查看会话"}`,
            result: `{"agent_id":"${resumeID}","agent_path":"/root/resume-child","status":"running"}`
          },
          {
            id: `${parentTurnID}-item-4`,
            type: "agent_message",
            status: "completed",
            role: "assistant",
            text: "已分发两个子 agent。左侧只展示直属子任务；更深层级会折叠成数量提示。"
          }
        ]
      }
    ],
    child_agents: [
      {
        id: inspectID,
        type: "worker",
        task_name: "检查左侧树",
        agent_path: "/root/inspect-sidebar",
        parent_id: parentID,
        description: "验证子 agent 行的视觉和状态",
        status: "completed",
        nested_count: 1,
        nested_running_count: 0,
        started_at: at(1000),
        completed_at: at(2500)
      },
      {
        id: resumeID,
        type: "worker",
        task_name: "子会话恢复",
        agent_path: "/root/resume-child",
        parent_id: parentID,
        description: "确认点击子 agent 后能正常查看会话",
        status: "running",
        nested_count: 1,
        nested_running_count: 1,
        started_at: at(1800)
      }
    ]
  };

  const children: Thread[] = [
    {
      id: inspectID,
      parent_id: parentID,
      agent_path: "/root/inspect-sidebar",
      preview: "检查左侧树",
      model_provider: provider,
      model,
      cwd,
      status: "idle",
      read_only: true,
      created_at: at(1000),
      updated_at: at(2500),
      turns: [
        {
          id: inspectTurnID,
          items_view: "full",
          status: "completed",
          items: [
            {
              id: `${inspectTurnID}-item-1`,
              type: "user_message",
              status: "completed",
              role: "user",
              text: "检查左侧 session 树中子 agent 的缩进、状态和折叠数量。"
            },
            {
              id: `${inspectTurnID}-item-2`,
              type: "agent_message",
              status: "completed",
              role: "assistant",
              text: "子 agent 显示在父会话下方，保留一层缩进；更深层级只显示数量，不再展开。"
            }
          ]
        }
      ]
    },
    {
      id: resumeID,
      parent_id: parentID,
      agent_path: "/root/resume-child",
      preview: "子会话恢复",
      model_provider: provider,
      model,
      cwd,
      status: "idle",
      read_only: true,
      created_at: at(1800),
      updated_at: at(4200),
      turns: [
        {
          id: resumeTurnID,
          items_view: "full",
          status: "completed",
          items: [
            {
              id: `${resumeTurnID}-item-1`,
              type: "user_message",
              status: "completed",
              role: "user",
              text: "验证点击左侧子 agent 后，主区域可以正常展示这个子会话。"
            },
            {
              id: `${resumeTurnID}-item-2`,
              type: "agent_message",
              status: "completed",
              role: "assistant",
              text: "这个视图是只读的子 agent session。左侧仍保留父子关系，主区域展示子 agent 的对话内容。"
            }
          ]
        }
      ]
    }
  ];

  return { parent, children };
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
  const [turnProgressPreviewOpen, setTurnProgressPreviewOpen] = useState(false);
  const [rightPanelOpen, setRightPanelOpen] = useState(false);
  const [bottomPanelOpen, setBottomPanelOpen] = useState(false);
  const [workspacePanelView, setWorkspacePanelView] = useState<WorkspacePanelView>("files");
  const [workspaceRightPanelView, setWorkspaceRightPanelView] = useState<WorkspaceRightPanelView>("tools");
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
  const [archiveConfirmThreadID, setArchiveConfirmThreadID] = useState<string | undefined>(undefined);
  const conversationScrollRef = useRef<HTMLDivElement | null>(null);
  const conversationPaneRef = useRef<HTMLElement | null>(null);
  const dockComposerRef = useRef<HTMLElement>(null);
  const dockComposerHeightRef = useRef(0);
  const conversationAutoFollowRef = useRef(true);
  const streamScrollFrameRef = useRef<number | undefined>(undefined);
  const conversationScrollbarHideTimerRef = useRef<number | undefined>(undefined);
  const resizeSessionRef = useRef<SidebarResizeSession | null>(null);
  const projectMenuRef = useRef<HTMLDivElement>(null);
  const runtimeMenuRef = useRef<HTMLDivElement>(null);
  const accessMenuRef = useRef<HTMLDivElement>(null);
  const codexRuntimeRef = useRef<HTMLDivElement>(null);
  const environmentToggleRef = useRef<HTMLButtonElement>(null);
  const environmentPanelRef = useRef<HTMLDivElement>(null);
  const runDebugRef = useRef<HTMLDivElement>(null);
  const appStateRef = useRef<AppState>(initialState);
  const queuedMessagesRef = useRef<QueuedComposerMessage[]>([]);
  const guideMessagesRef = useRef<QueuedComposerMessage[]>([]);
  const demoAgentThreadsRef = useRef(new Map<string, Thread>());
  const drainingQueueRef = useRef(false);
  const queueDrainPausedRef = useRef(false);
  const runDebugEventIDRef = useRef(0);
  const runDebugDeltaSeenRef = useRef(new Set<string>());

  useEffect(() => {
    return () => {
      if (conversationScrollbarHideTimerRef.current !== undefined) {
        window.clearTimeout(conversationScrollbarHideTimerRef.current);
      }
    };
  }, []);

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
    if (!isStateActiveThreadRunning(state)) {
      void drainQueuedMessages();
    }
  }, [
    state.activeContext?.cwd,
    state.initialized?.model,
    state.initialized?.provider,
    state.running,
    state.thread?.id,
    state.thread?.status,
    state.thread?.turns
  ]);

  useEffect(() => {
    let mounted = true;
    const off = window.wuu.onServerEvent((event) => {
      if (!mounted) {
        return;
      }
      recordRunDebugEvent(event);
      const handling = handleStreamingNotification(event, appStateRef.current);
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
      if (
        (runtimeMenuOpen || modeMenuOpen || branchMenuOpen) &&
        !runtimeMenuRef.current?.contains(target) &&
        !isInsideFloatingMenu(target, "composer-runtime")
      ) {
        setRuntimeMenuOpen(false);
        setModeMenuOpen(false);
        setBranchMenuOpen(false);
      }
      if (
        accessMenuOpen &&
        !accessMenuRef.current?.contains(target) &&
        !isInsideFloatingMenu(target, "composer-access")
      ) {
        setAccessMenuOpen(false);
      }
      if (
        codexRuntimeMenu &&
        !codexRuntimeRef.current?.contains(target) &&
        !isInsideFloatingMenu(target, "codex-runtime")
      ) {
        setCodexRuntimeMenu(null);
      }
      const environmentPanelClickOutside =
        !environmentToggleRef.current?.contains(target) && !environmentPanelRef.current?.contains(target);
      if (environmentPanelClickOutside) {
        if (environmentPanelMenu) {
          setEnvironmentPanelMenu(null);
        }
        if (environmentPanelOpen && !environmentPanelHasRoom) {
          setEnvironmentPanelOpen(false);
        }
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
    environmentPanelHasRoom,
    environmentPanelMenu,
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
    if (!visibleAskRequestForThread(state.askRequests, state.thread?.id)) {
      return;
    }
    setSettingsOpen(false);
    setWorkspaceMode(undefined);
    conversationAutoFollowRef.current = true;
    window.requestAnimationFrame(() => scrollConversationToBottom({ force: true }));
  }, [state.askRequests, state.thread?.id]);

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
  const visibleAskRequest = visibleAskRequestForThread(state.askRequests, state.thread?.id);
  const visibleAnsweredAskRequests = visibleAnsweredAskRequestsForThread(state.answeredAskRequests, state.thread?.id);
  const answeredAskRequestsWithoutVisibleTurn = visibleAnsweredAskRequests.filter(
    (request) => !request.turnID || !turns.some((turn) => turn.id === request.turnID)
  );
  const emptyConversation = turns.length === 0 && !visibleAskRequest && visibleAnsweredAskRequests.length === 0;
  const previewingLaunch = ENABLE_LAUNCH_PREVIEW && launchPreviewPinned;
  const showingWorkspaceMode = state.initialized && !previewingLaunch && workspaceMode !== undefined;
  const sidebarPinnedThreads = pinnedThreads(state.threads);
  const activeThreadReadOnly = Boolean(state.thread?.read_only);
  const activeThreadIsRunning = !activeThreadReadOnly && isStateActiveThreadRunning(state);
  const pendingAskThreadIDs = pendingAskThreadIDsForRequests(state.askRequests);
  const anyThreadIsRunning = isAnyThreadRunning(state);
  const environmentPanelCanShow = Boolean(state.initialized && !previewingLaunch && !showingWorkspaceMode && !rightPanelOpen);
  const environmentPanelVisible =
    environmentPanelCanShow &&
    (environmentPanelOpen || (environmentPanelHasRoom && !environmentPanelDismissed && !emptyConversation));
  const shellClassName = `app-shell${sidebarCollapsed ? " sidebar-collapsed" : ""}${
    resizingSidebar ? " resizing-sidebar" : ""
  }${rightPanelOpen ? " right-panel-open" : ""}${bottomPanelOpen ? " bottom-panel-open" : ""}`;
  const shellStyle = {
    "--sidebar-width": `${sidebarCollapsed ? 0 : sidebarWidth}px`,
    "--workspace-right-panel-width":
      rightPanelOpen && workspaceRightPanelView === "review"
        ? "min(clamp(560px, 40vw, 860px), max(420px, calc(100vw - var(--sidebar-width, 326px) - 360px)))"
        : "360px",
    "--environment-panel-width": "360px",
    "--environment-panel-reserved-width": "432px",
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
  const pullRequestDisabledReason = pullRequestUnavailableReason(state.gitStatus);
  const runDebugPhase = runDebugPhaseForState(state);

  useEffect(() => {
    if (visibleAnsweredAskRequests.length === 0) {
      return;
    }
    conversationAutoFollowRef.current = true;
    window.requestAnimationFrame(() => scrollConversationToBottom({ force: true }));
  }, [visibleAnsweredAskRequests.length]);

  useLayoutEffect(() => {
    const node = dockComposerRef.current;
    const pane = conversationPaneRef.current;
    const applyHeight = (nextHeight: number): void => {
      if (dockComposerHeightRef.current === nextHeight) {
        return;
      }
      dockComposerHeightRef.current = nextHeight;
      pane?.style.setProperty("--dock-composer-height", `${nextHeight}px`);
      if (nextHeight > 0 && conversationAutoFollowRef.current) {
        scrollConversationToBottom();
      }
    };

    if (!node) {
      applyHeight(0);
      return;
    }

    const updateHeight = (): void => {
      const nextHeight = Math.ceil(node.getBoundingClientRect().height);
      applyHeight(nextHeight);
    };

    updateHeight();
    const resizeObserver = new ResizeObserver(updateHeight);
    resizeObserver.observe(node);
    return () => resizeObserver.disconnect();
  }, [emptyConversation, previewingLaunch, showingWorkspaceMode, state.initialized]);

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
    showConversationScrollbar(node);
    conversationAutoFollowRef.current = isConversationNearBottom(node);
  }

  function scrollConversationToBottom(options: { force?: boolean } = {}): void {
    const node = conversationViewport();
    if (!node || (!options.force && !conversationAutoFollowRef.current)) {
      return;
    }
    node.scrollTop = node.scrollHeight;
    showConversationScrollbar(node);
    conversationAutoFollowRef.current = true;
  }

  function showConversationScrollbar(node: HTMLElement): void {
    if (
      node.classList.contains("empty-scroll-region") ||
      node.classList.contains("workspace-scroll-region") ||
      node.scrollHeight <= node.clientHeight
    ) {
      return;
    }
    node.classList.add("scrollbar-visible");
    if (conversationScrollbarHideTimerRef.current !== undefined) {
      window.clearTimeout(conversationScrollbarHideTimerRef.current);
    }
    conversationScrollbarHideTimerRef.current = window.setTimeout(() => {
      conversationScrollbarHideTimerRef.current = undefined;
      node.classList.remove("scrollbar-visible");
    }, CONVERSATION_SCROLLBAR_HIDE_DELAY_MS);
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
    setWorkspaceRightPanelView(view);
    if (view === "review" || view === "terminal") {
      setWorkspaceMode(undefined);
      setRightPanelOpen(true);
      return;
    }
    setWorkspaceMode(view);
    setRightPanelOpen(true);
  }

  function openWorkspaceFile(path: string): void {
    setWorkspacePanelView("files");
    setWorkspaceRightPanelView("files");
    setWorkspaceMode("files");
    setRightPanelOpen(true);
    setSelectedWorkspaceFile((current) => (current === path ? current : path));
  }

  function toggleRightPanel(): void {
    if (rightPanelOpen) {
      setRightPanelOpen(false);
      return;
    }
    setWorkspaceRightPanelView("tools");
    setRightPanelOpen(true);
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
    if (!isStateActiveThreadRunning(currentState)) {
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
        containerRef={variant === "dock" ? dockComposerRef : undefined}
        prompt={prompt}
        setPrompt={setPrompt}
        images={composerImages}
        queuedMessages={queuedMessages}
        guideMessages={guideMessages}
        running={activeThreadIsRunning}
        status={activeThreadReadOnly ? "子任务会话只读" : state.status}
        readOnly={activeThreadReadOnly}
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
    const listedThreads = sortThreads(listed.threads);
    const defaultThread = listedThreads.find((candidate) => !candidate.pinned) ?? listedThreads[0];
    const thread = defaultThread
      ? requireThread(await window.wuu.resumeThread(defaultThread.id), "resume did not return a thread")
      : undefined;
    return {
      initialized,
      projects: projectState.projects,
      activeContext: projectState.active_context,
      activeProjectId: activeProjectID(projectState.active_context),
      gitStatus,
      thread,
      threads: thread ? upsertThread(listedThreads, thread) : listedThreads,
      running: isThreadRunning(thread),
      status: "ready",
      askRequests: [],
      answeredAskRequests: []
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
      status: "no-runtime",
      askRequests: [],
      answeredAskRequests: []
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
      status: "opening",
      askRequests: [],
      answeredAskRequests: []
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
      status: "opening",
      askRequests: [],
      answeredAskRequests: []
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
    if (!branch || anyThreadIsRunning) {
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
    if (!branch || anyThreadIsRunning) {
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
    if (!state.activeContext) {
      return;
    }
    setArchiveConfirmThreadID(undefined);
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

  function seedAgentTreeDemo(): void {
    if (!state.activeContext || !state.initialized) {
      return;
    }
    setArchiveConfirmThreadID(undefined);
    setPrompt("");
    setComposerImages([]);
    clearPendingComposerMessages();
    const demo = createAgentTreeDemo(state.activeContext.cwd, state.initialized);
    demoAgentThreadsRef.current = new Map(demo.children.map((thread) => [thread.id, thread]));
    setState((current) => ({
      ...current,
      thread: demo.parent,
      threads: upsertThread(current.threads, demo.parent),
      running: false,
      status: "ready",
      askRequests: [],
      answeredAskRequests: []
    }));
  }

  async function selectThread(threadId: string): Promise<void> {
    if (!state.activeContext || threadId === state.thread?.id) {
      return;
    }
    setArchiveConfirmThreadID(undefined);
    clearPendingComposerMessages();
    setState((current) => ({ ...current, status: "loading" }));
    try {
      const thread = requireThread(await window.wuu.resumeThread(threadId), "resume did not return a thread");
      setState((current) => ({
        ...current,
        thread,
        threads: upsertThread(current.threads, thread),
        running: isThreadRunning(thread),
        status: "ready"
      }));
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "load failed"
      }));
    }
  }

  async function selectChildAgent(agent: Agent): Promise<void> {
    if (!state.activeContext || agent.id === state.thread?.id) {
      return;
    }
    setArchiveConfirmThreadID(undefined);
    setPrompt("");
    setComposerImages([]);
    clearPendingComposerMessages();
    setState((current) => ({ ...current, status: "loading", running: false, askRequests: [], answeredAskRequests: [] }));
    try {
      const thread =
        demoAgentThreadsRef.current.get(agent.id) ??
        requireThread(await window.wuu.resumeThread(agent.id), "resume did not return a child agent thread");
      setState((current) => ({
        ...current,
        thread,
        threads: upsertThread(current.threads, thread),
        running: false,
        status: "ready"
      }));
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "load child agent failed"
      }));
    }
  }

  async function toggleThreadPinned(thread: Thread): Promise<void> {
    if (!state.activeContext) {
      return;
    }
    setArchiveConfirmThreadID(undefined);
    try {
      const result = await window.wuu.pinThread(thread.id, !thread.pinned);
      setState((current) => ({
        ...current,
        thread: current.thread?.id === thread.id ? result.thread : current.thread,
        threads: upsertThread(current.threads, result.thread),
        status: current.status === "ready" ? "ready" : current.status
      }));
    } catch (error) {
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "pin thread failed"
      }));
    }
  }

  async function archiveThread(thread: Thread): Promise<void> {
    if (!state.activeContext || isThreadRunning(thread)) {
      return;
    }
    if (archiveConfirmThreadID !== thread.id) {
      setArchiveConfirmThreadID(thread.id);
      return;
    }
    clearPendingComposerMessages();
    try {
      const result = await window.wuu.archiveThread(thread.id, true);
      setArchiveConfirmThreadID(undefined);
      setState((current) => ({
        ...current,
        thread: current.thread?.id === thread.id ? undefined : current.thread,
        threads: current.threads.filter((candidate) => candidate.id !== result.thread.id),
        running: false,
        status: "ready"
      }));
    } catch (error) {
      setArchiveConfirmThreadID(undefined);
      setState((current) => ({
        ...current,
        status: error instanceof Error ? error.message : "archive thread failed"
      }));
    }
  }

  async function sendPrompt(): Promise<void> {
    const message = createComposerMessage(prompt, composerImages);
    const currentState = appStateRef.current;
    if (currentState.thread?.read_only) {
      setState((current) => ({ ...current, status: "子任务会话只读" }));
      return;
    }
    if (!message || !currentState.activeContext || !currentState.initialized) {
      return;
    }
    setPrompt("");
    setComposerImages([]);
    if (isStateActiveThreadRunning(currentState)) {
      enqueueComposerMessage(message);
      return;
    }
    await sendComposerMessage(message, true);
  }

  async function sendComposerMessage(message: QueuedComposerMessage, restoreDraftOnError = false): Promise<boolean> {
    const currentState = appStateRef.current;
    const text = message.text.trim();
    const images = inputImagesFromComposer(message.images);
    if (
      (!text && images.length === 0) ||
      !currentState.activeContext ||
      !currentState.initialized ||
      currentState.thread?.read_only ||
      isStateActiveThreadRunning(currentState)
    ) {
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
    if (isStateActiveThreadRunning(currentState) || !currentState.activeContext || !currentState.initialized) {
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
      anyThreadIsRunning ||
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
    if (!state.initialized || anyThreadIsRunning || !isCodexProvider(state.initialized)) {
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
    if (!state.initialized || anyThreadIsRunning) {
      return;
    }
    const nextEffort = normalizedEffortForModel(state.initialized.effort ?? "", nextModel);
    await updateRuntimeSettings(state.initialized.provider, nextModel.slug, nextEffort);
    setCodexRuntimeMenu(null);
  }

  async function selectCodexEffort(nextEffort: string): Promise<void> {
    if (!state.initialized || anyThreadIsRunning) {
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
    try {
      await window.wuu.respondToServerRequest(request.id, response);
      const currentThread = appStateRef.current.thread;
      const answeredRequest: AnsweredAskRequestState = {
        ...request,
        threadID: request.threadID ?? currentThread?.id,
        turnID: activeDebugTurn(currentThread)?.id,
        answers: response.answers ?? {},
        cancelled: response.cancelled === true
      };
      setState((current) => ({
        ...current,
        askRequests: removeAskRequest(current.askRequests, request.id),
        answeredAskRequests: upsertAnsweredAskRequest(current.answeredAskRequests, answeredRequest)
      }));
    } catch (error) {
      setState((current) => ({
        ...current,
        askRequests: upsertAskRequest(current.askRequests, request),
        status: desktopApiErrorMessage(error, "提交选择失败")
      }));
    }
  }

  if (settingsOpen) {
    return (
      <SettingsView
        initialized={state.initialized}
        running={anyThreadIsRunning}
        onBack={() => setSettingsOpen(false)}
        onSave={updateRuntimeSettings}
      />
    );
  }

  const environmentPanelNode =
    environmentPanelVisible && state.initialized ? (
      <EnvironmentPanel
        panelRef={environmentPanelRef}
        initialized={state.initialized}
        gitStatus={state.gitStatus}
        activeContext={state.activeContext}
        activeProject={activeProject}
        sourceItems={environmentSourceItems}
        activeMenu={environmentPanelMenu}
        running={anyThreadIsRunning}
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
        onOpenReview={() => {
          setWorkspacePanelView("review");
          setWorkspaceRightPanelView("review");
          setWorkspaceMode(undefined);
          setRightPanelOpen(true);
          setEnvironmentPanelOpen(false);
          setEnvironmentPanelDismissed(true);
          setEnvironmentPanelMenu(null);
        }}
        onOpenCommit={() => setEnvironmentDialog("commit")}
        onOpenPullRequest={() => setEnvironmentDialog("pull-request")}
      />
    ) : null;

  return (
    <div className={shellClassName} style={shellStyle}>
      <aside className="sidebar">
        <div className="traffic-spacer" />
        <nav className="primary-nav" aria-label="主导航">
          <button className="nav-item" onClick={() => void startNewThread()} disabled={!state.activeContext}>
            <MessageSquarePlus size={18} />
            <span>新对话</span>
          </button>
          {ENABLE_AGENT_TREE_DEMO ? (
            <button className="nav-item" onClick={seedAgentTreeDemo} disabled={!state.activeContext || !state.initialized}>
              <CornerDownRight size={18} />
              <span>模拟子任务</span>
            </button>
          ) : null}
        </nav>

        {sidebarPinnedThreads.length > 0 ? (
          <section className="pinned-thread-section" aria-label="置顶">
            <div className="section-label pinned-thread-label">置顶</div>
            <PinnedThreadList
              threads={sidebarPinnedThreads}
              activeID={state.thread?.id}
              pendingAskThreadIDs={pendingAskThreadIDs}
              archiveConfirmThreadID={archiveConfirmThreadID}
              onSelect={(id) => void selectThread(id)}
              onSelectChildAgent={(agent) => void selectChildAgent(agent)}
              onTogglePinned={(thread) => void toggleThreadPinned(thread)}
              onArchive={(thread) => void archiveThread(thread)}
              onClearArchiveConfirm={(id) =>
                setArchiveConfirmThreadID((current) => (current === id ? undefined : current))
              }
            />
          </section>
        ) : null}

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
            pendingAskThreadIDs={pendingAskThreadIDs}
            archiveConfirmThreadID={archiveConfirmThreadID}
            onSelectProject={(id) => void openProject(id)}
            onSelectThread={(id) => void selectThread(id)}
            onSelectChildAgent={(agent) => void selectChildAgent(agent)}
            onToggleThreadPinned={(thread) => void toggleThreadPinned(thread)}
            onArchiveThread={(thread) => void archiveThread(thread)}
            onClearArchiveConfirm={(id) =>
              setArchiveConfirmThreadID((current) => (current === id ? undefined : current))
            }
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

      <main className={`conversation-pane${environmentPanelVisible ? " environment-panel-visible" : ""}`} ref={conversationPaneRef}>
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
            {ENABLE_TURN_PROGRESS_EXPERIMENT ? (
              <button
                className={`launch-preview-button turn-progress-preview-button${turnProgressPreviewOpen ? " active" : ""}`}
                type="button"
                aria-pressed={turnProgressPreviewOpen}
                onClick={() => setTurnProgressPreviewOpen(true)}
              >
                <Film size={15} />
                <span>完整预览</span>
              </button>
            ) : null}
            {ENABLE_RUN_DEBUG_PANEL ? (
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
            ) : null}
            <button
              ref={environmentToggleRef}
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
              onClick={toggleRightPanel}
            >
              <PanelRightOpen size={18} />
            </button>
          </div>
        </header>

        {ENABLE_TURN_PROGRESS_EXPERIMENT && turnProgressPreviewOpen ? (
          <TurnProgressPreviewOverlay onClose={() => setTurnProgressPreviewOpen(false)} />
        ) : null}

        {environmentPanelNode}

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
                gitStatus={state.gitStatus}
                selectedFilePath={selectedWorkspaceFile}
                onOpenRightPanel={() => {
                  setWorkspacePanelView(workspaceMode);
                  setWorkspaceRightPanelView(workspaceMode);
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
                  <Fragment key={turn.id}>
                    <TurnView
                      turn={turn}
                      cwd={state.thread?.cwd ?? state.activeContext?.cwd}
                      onStreamFrame={scheduleStreamScroll}
                    />
                    {visibleAnsweredAskRequests
                      .filter((request) => request.turnID === turn.id)
                      .map((request) => (
                        <AnsweredAskUserMessage key={`answered-${request.id}`} request={request} />
                      ))}
                  </Fragment>
                ))}
                {answeredAskRequestsWithoutVisibleTurn.map((request) => (
                  <AnsweredAskUserMessage key={`answered-${request.id}`} request={request} />
                ))}
                {visibleAskRequest ? (
                  <AskUserMessage
                    key={visibleAskRequest.id}
                    request={visibleAskRequest}
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
        view={workspaceRightPanelView}
        selectedView={workspacePanelView}
        activeContext={state.activeContext}
        gitStatus={state.gitStatus}
        selectedFilePath={selectedWorkspaceFile}
        onSelectView={openWorkspaceTool}
        onShowTools={() => setWorkspaceRightPanelView("tools")}
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
  const turnStartedAt = turn ? parseTurnTimestampMs(turn.started_at) : NaN;
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
                    : turn.status === "in_progress" && Number.isFinite(turnStartedAt)
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
  onOpenReview,
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
  onOpenReview: () => void;
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
        <button
          className="environment-row environment-change-row"
          type="button"
          disabled={!gitStatus?.is_repo}
          onClick={onOpenReview}
        >
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
          {gitStatus?.is_repo ? <ChevronRight size={17} /> : null}
        </button>

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

function WorkspaceReviewPanel({ gitStatus }: { gitStatus?: GitStatusResult }): JSX.Element {
  const panelRef = useRef<HTMLDivElement>(null);
  const splitResizeRef = useRef<{ startX: number; startTreeWidth: number } | null>(null);
  const [changes, setChanges] = useState<GitChangesResult | undefined>(undefined);
  const [selectedPath, setSelectedPath] = useState<string | undefined>(undefined);
  const [fileDiff, setFileDiff] = useState<GitFileDiffResult | undefined>(undefined);
  const [loadingChanges, setLoadingChanges] = useState(false);
  const [loadingDiff, setLoadingDiff] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);
  const [treeQuery, setTreeQuery] = useState("");
  const [expandedPaths, setExpandedPaths] = useState<Set<string>>(() => new Set());
  const [treePaneWidth, setTreePaneWidth] = useState(initialWorkspaceReviewTreeWidth);
  const [resizingSplit, setResizingSplit] = useState(false);
  const files = changes?.files ?? [];
  const filteredFiles = useMemo(() => filterGitChangeFiles(files, treeQuery), [files, treeQuery]);
  const treeNodes = useMemo(() => buildGitChangeTree(filteredFiles), [filteredFiles]);
  const selectedFile = files.find((file) => file.path === selectedPath);
  const panelStyle = {
    "--workspace-review-tree-width": `${treePaneWidth}px`
  } as CSSProperties;

  useEffect(() => {
    let cancelled = false;
    setChanges(undefined);
    setSelectedPath(undefined);
    setFileDiff(undefined);
    if (!desktopApiSupportsGitReview()) {
      setError("审查接口还没被当前窗口加载。请重启桌面端后再试。");
      setLoadingChanges(false);
      return;
    }
    setLoadingChanges(true);
    setError(undefined);
    void window.wuu
      .listGitChanges()
      .then((result) => {
        if (cancelled) {
          return;
        }
        setChanges(result);
        setExpandedPaths(new Set(collectGitChangeTreeDirectoryPaths(buildGitChangeTree(result.files))));
        setSelectedPath((current) => {
          if (current && result.files.some((file) => file.path === current)) {
            return current;
          }
          return result.files[0]?.path;
        });
      })
      .catch((nextError) => {
        if (!cancelled) {
          setError(desktopApiErrorMessage(nextError, "读取变更失败"));
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoadingChanges(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    if (!selectedPath) {
      setFileDiff(undefined);
      setLoadingDiff(false);
      return;
    }
    setExpandedPaths((current) => {
      const next = new Set(current);
      for (const ancestor of gitPathAncestors(selectedPath)) {
        next.add(ancestor);
      }
      return next;
    });
  }, [selectedPath]);

  useEffect(() => {
    if (!selectedPath) {
      return;
    }
    let cancelled = false;
    setFileDiff(undefined);
    if (!desktopApiSupportsGitReview()) {
      setError("审查接口还没被当前窗口加载。请重启桌面端后再试。");
      setLoadingDiff(false);
      return;
    }
    setLoadingDiff(true);
    setError(undefined);
    void window.wuu
      .readGitFileDiff(selectedPath)
      .then((result) => {
        if (!cancelled) {
          setFileDiff(result);
        }
      })
      .catch((nextError) => {
        if (!cancelled) {
          setError(desktopApiErrorMessage(nextError, "读取 diff 失败"));
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoadingDiff(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [selectedPath]);

  useEffect(() => {
    window.localStorage.setItem(WORKSPACE_REVIEW_TREE_WIDTH_KEY, String(treePaneWidth));
  }, [treePaneWidth]);

  useEffect(() => {
    const root = document.documentElement;
    root.classList.toggle("resizing-review-split", resizingSplit);
    if (!resizingSplit) {
      return () => root.classList.remove("resizing-review-split");
    }

    function handlePointerMove(event: PointerEvent): void {
      const session = splitResizeRef.current;
      if (!session) {
        return;
      }
      const panelWidth = panelRef.current?.getBoundingClientRect().width;
      setTreePaneWidth(
        clampWorkspaceReviewTreeWidth(session.startTreeWidth - (event.clientX - session.startX), panelWidth)
      );
    }

    function handlePointerUp(): void {
      splitResizeRef.current = null;
      setResizingSplit(false);
    }

    window.addEventListener("pointermove", handlePointerMove);
    window.addEventListener("pointerup", handlePointerUp);
    window.addEventListener("pointercancel", handlePointerUp);
    return () => {
      root.classList.remove("resizing-review-split");
      window.removeEventListener("pointermove", handlePointerMove);
      window.removeEventListener("pointerup", handlePointerUp);
      window.removeEventListener("pointercancel", handlePointerUp);
    };
  }, [resizingSplit]);

  function toggleTreePath(path: string): void {
    setExpandedPaths((current) => {
      const next = new Set(current);
      if (next.has(path)) {
        next.delete(path);
      } else {
        next.add(path);
      }
      return next;
    });
  }

  function resizeTreePaneBy(delta: number): void {
    const panelWidth = panelRef.current?.getBoundingClientRect().width;
    setTreePaneWidth((current) => clampWorkspaceReviewTreeWidth(current + delta, panelWidth));
  }

  function startReviewSplitResize(event: ReactPointerEvent<HTMLDivElement>): void {
    if (event.button !== 0) {
      return;
    }
    event.preventDefault();
    splitResizeRef.current = {
      startX: event.clientX,
      startTreeWidth: treePaneWidth
    };
    setResizingSplit(true);
  }

  function handleReviewSplitKeyDown(event: ReactKeyboardEvent<HTMLDivElement>): void {
    if (event.key === "ArrowLeft") {
      event.preventDefault();
      resizeTreePaneBy(WORKSPACE_REVIEW_TREE_STEP);
    } else if (event.key === "ArrowRight") {
      event.preventDefault();
      resizeTreePaneBy(-WORKSPACE_REVIEW_TREE_STEP);
    } else if (event.key === "Home") {
      event.preventDefault();
      resizeTreePaneBy(WORKSPACE_REVIEW_TREE_MAX_WIDTH);
    } else if (event.key === "End") {
      event.preventDefault();
      resizeTreePaneBy(-WORKSPACE_REVIEW_TREE_MAX_WIDTH);
    }
  }

  return (
    <div
      className={`workspace-review-panel${selectedFile ? " has-diff" : ""}${
        resizingSplit ? " resizing-split" : ""
      }`}
      aria-label="审查变更"
      ref={panelRef}
      style={panelStyle}
    >
      {selectedFile ? (
        <WorkspaceReviewDiffPeekPanel
          file={selectedFile}
          fileDiff={fileDiff}
          loading={loadingDiff}
          error={error}
          branch={gitStatus?.branch}
        />
      ) : null}
      {selectedFile ? (
        <div
          className="workspace-review-resizer"
          role="separator"
          aria-label="调整 diff 和文件树宽度"
          aria-orientation="vertical"
          aria-valuemin={WORKSPACE_REVIEW_TREE_MIN_WIDTH}
          aria-valuemax={WORKSPACE_REVIEW_TREE_MAX_WIDTH}
          aria-valuenow={Math.round(treePaneWidth)}
          tabIndex={0}
          onPointerDown={startReviewSplitResize}
          onKeyDown={handleReviewSplitKeyDown}
        />
      ) : null}
      <div className="workspace-review-tree-pane">
        <GitChangeTreePanel
          files={filteredFiles}
          nodes={treeNodes}
          selectedPath={selectedPath}
          expandedPaths={expandedPaths}
          query={treeQuery}
          onQueryChange={setTreeQuery}
          onSelectFile={setSelectedPath}
          onTogglePath={toggleTreePath}
        />
        {loadingChanges ? <div className="workspace-review-overlay">正在读取变更...</div> : null}
        {error && !selectedFile ? <div className="workspace-review-overlay error">{error}</div> : null}
        {!loadingChanges && changes?.is_repo && files.length === 0 ? (
          <div className="workspace-review-overlay">工作区干净</div>
        ) : null}
      </div>
    </div>
  );
}

function WorkspaceReviewDiffPeekPanel({
  file,
  fileDiff,
  loading,
  error,
  branch
}: {
  file: GitChangeFile;
  fileDiff?: GitFileDiffResult;
  loading: boolean;
  error?: string;
  branch?: string;
}): JSX.Element {
  const diffLines = useMemo(() => (fileDiff?.patch ? gitDiffDisplayLines(fileDiff.patch) : []), [fileDiff?.patch]);
  return (
    <section className="workspace-review-diff-panel workspace-diff-detail" aria-label={`${file.path} 的代码差异`}>
      <div className="workspace-diff-detail-header">
        <div>
          <strong>{gitChangeFilePathLabel(file)}</strong>
          <span>
            {branch ?? "当前分支"} · {gitChangeStatusDescription(file)}
          </span>
        </div>
      </div>
      {error ? <div className="workspace-diff-error">{error}</div> : null}
      {loading ? (
        <div className="workspace-diff-empty">正在读取 diff...</div>
      ) : fileDiff?.binary ? (
        <div className="workspace-diff-empty">这是二进制文件，无法显示文本 diff。</div>
      ) : fileDiff?.patch ? (
        <OverlayScrollbarsComponent
          className="workspace-diff-code-scroll"
          data-overlayscrollbars-initialize
          defer
          options={OVERLAY_SCROLLBAR_OPTIONS}
        >
          <pre className="workspace-diff-code" aria-label={`${fileDiff.path} 的代码差异`}>
            {diffLines.map((line, index) => (
              <span className={`workspace-diff-line ${line.kind}`} key={`${index}:${line.content.slice(0, 24)}`}>
                <span className="workspace-diff-line-number">{line.oldLine ?? ""}</span>
                <span className="workspace-diff-line-number">{line.newLine ?? ""}</span>
                <span className="workspace-diff-line-code">{line.content || " "}</span>
              </span>
            ))}
          </pre>
        </OverlayScrollbarsComponent>
      ) : (
        <div className="workspace-diff-empty">没有可显示的文本 diff。</div>
      )}
      {fileDiff?.truncated ? <div className="workspace-diff-truncated">diff 太大，已截断预览。</div> : null}
    </section>
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
    return <ImageIcon size={17} />;
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
  selectedView,
  activeContext,
  gitStatus,
  selectedFilePath,
  onSelectView,
  onShowTools,
  onOpenFile,
  onClose
}: {
  open: boolean;
  view: WorkspaceRightPanelView;
  selectedView: WorkspacePanelView;
  activeContext?: RuntimeContext;
  gitStatus?: GitStatusResult;
  selectedFilePath?: string;
  onSelectView: (view: WorkspacePanelView) => void;
  onShowTools: () => void;
  onOpenFile: (path: string) => void;
  onClose: () => void;
}): JSX.Element {
  const detailView = view === "tools" ? undefined : view;
  const activeTool = detailView ? workspaceToolFor(detailView) : undefined;

  return (
    <aside
      className={`workspace-right-panel${detailView ? " detail" : " tools"}${detailView === "review" ? " review" : ""}`}
      aria-hidden={!open}
    >
      <div className="workspace-panel-header">
        <div className="workspace-panel-title">
          {detailView ? (
            <>
              <button
                className="icon-button workspace-panel-back"
                type="button"
                aria-label="返回工具"
                disabled={!open}
                onClick={onShowTools}
              >
                <ArrowLeft size={16} />
              </button>
              <WorkspaceToolIcon view={detailView} size={18} />
              <span>{activeTool?.title}</span>
            </>
          ) : (
            <>
              <Wrench size={18} />
              <span>工具</span>
            </>
          )}
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
          {detailView ? (
            <div className="workspace-panel-tabs" role="tablist" aria-label="右侧栏工具">
              {WORKSPACE_TOOL_ITEMS.map((item) => (
                <button
                  key={item.id}
                  className={item.id === detailView ? "active" : ""}
                  type="button"
                  role="tab"
                  aria-selected={item.id === detailView}
                  title={item.title}
                  onClick={() => onSelectView(item.id)}
                >
                  <WorkspaceToolIcon view={item.id} size={17} />
                </button>
              ))}
            </div>
          ) : null}
          <div className="workspace-panel-body">
            {view === "tools" ? (
              <WorkspaceToolMenu selectedView={selectedView} onSelectTool={onSelectView} />
            ) : view === "files" ? (
              <WorkspaceFileTree
                activeContext={activeContext}
                open={open}
                selectedFilePath={selectedFilePath}
                onOpenFile={onOpenFile}
              />
            ) : view === "review" ? (
              <WorkspaceReviewPanel gitStatus={gitStatus} />
            ) : view === "terminal" ? (
              <WorkspaceTerminalPanel activeContext={activeContext} />
            ) : null}
          </div>
        </>
      ) : null}
    </aside>
  );
}

function WorkspaceToolMenu({
  selectedView,
  onSelectTool
}: {
  selectedView: WorkspacePanelView;
  onSelectTool: (view: WorkspacePanelView) => void;
}): JSX.Element {
  return (
    <div className="workspace-tool-menu" aria-label="工作区工具">
      {WORKSPACE_TOOL_ITEMS.map((item) => (
        <button
          key={item.id}
          className={`workspace-tool-menu-item${item.id === selectedView ? " active" : ""}`}
          type="button"
          onClick={() => onSelectTool(item.id)}
        >
          <span className="workspace-tool-menu-icon" aria-hidden="true">
            <WorkspaceToolIcon view={item.id} size={20} />
          </span>
          <span className="workspace-tool-menu-copy">
            <strong>{item.title}</strong>
            <span>{item.subtitle}</span>
          </span>
          <ChevronRight size={17} aria-hidden="true" />
        </button>
      ))}
    </div>
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

const WORKSPACE_TERMINAL_MAX_LINES = 800;
const WORKSPACE_TERMINAL_PENDING_EVENT_IDS = 12;

type WorkspaceTerminalLine = {
  id: string;
  kind: "command" | "stdout" | "stderr" | "status" | "error";
  text: string;
};

function terminalExitText(event: Extract<TerminalCommandEvent, { type: "exit" }>): string {
  const duration = formatDuration(event.duration_ms);
  if (event.signal) {
    return `stopped by ${event.signal} after ${duration}`;
  }
  return `exit ${event.exit_code ?? "unknown"} after ${duration}`;
}

function WorkspaceTerminalPanel({ activeContext }: { activeContext?: RuntimeContext }): JSX.Element {
  const [lines, setLines] = useState<WorkspaceTerminalLine[]>([]);
  const [draft, setDraft] = useState("");
  const [runningCommandID, setRunningCommandID] = useState<string | undefined>(undefined);
  const [history, setHistory] = useState<string[]>([]);
  const [historyIndex, setHistoryIndex] = useState<number | undefined>(undefined);
  const knownCommandIDsRef = useRef(new Set<string>());
  const pendingTerminalEventsRef = useRef(new Map<string, TerminalCommandEvent[]>());
  const runningCommandIDRef = useRef<string | undefined>(undefined);
  const lineCounterRef = useRef(1);
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const workspaceRoot = activeContext?.cwd;

  useEffect(() => {
    runningCommandIDRef.current = runningCommandID;
  }, [runningCommandID]);

  useEffect(() => {
    return window.wuu.onTerminalEvent((event) => {
      if (!knownCommandIDsRef.current.has(event.id)) {
        bufferTerminalEvent(event);
        return;
      }
      handleTerminalEvent(event);
    });
  }, []);

  useEffect(() => {
    const node = scrollRef.current;
    if (!node) {
      return;
    }
    node.scrollTop = node.scrollHeight;
  }, [lines]);

  useEffect(() => {
    const commandID = runningCommandIDRef.current;
    if (commandID) {
      void window.wuu.stopTerminalCommand(commandID);
      runningCommandIDRef.current = undefined;
    }
    setLines([]);
    setDraft("");
    setRunningCommandID(undefined);
    setHistoryIndex(undefined);
    knownCommandIDsRef.current.clear();
    pendingTerminalEventsRef.current.clear();
  }, [workspaceRoot]);

  useEffect(() => {
    return () => {
      const commandID = runningCommandIDRef.current;
      if (commandID) {
        void window.wuu.stopTerminalCommand(commandID);
      }
    };
  }, []);

  function nextLineID(): string {
    const next = lineCounterRef.current++;
    return `terminal-line-${next}`;
  }

  function appendLines(nextLines: WorkspaceTerminalLine[]): void {
    setLines((current) => [...current, ...nextLines].slice(-WORKSPACE_TERMINAL_MAX_LINES));
  }

  function bufferTerminalEvent(event: TerminalCommandEvent): void {
    const events = pendingTerminalEventsRef.current.get(event.id) ?? [];
    pendingTerminalEventsRef.current.set(event.id, [...events, event]);
    while (pendingTerminalEventsRef.current.size > WORKSPACE_TERMINAL_PENDING_EVENT_IDS) {
      const firstID = pendingTerminalEventsRef.current.keys().next().value;
      if (!firstID) {
        break;
      }
      pendingTerminalEventsRef.current.delete(firstID);
    }
  }

  function flushPendingTerminalEvents(id: string): void {
    const events = pendingTerminalEventsRef.current.get(id);
    if (!events) {
      return;
    }
    pendingTerminalEventsRef.current.delete(id);
    for (const event of events) {
      handleTerminalEvent(event);
    }
  }

  function handleTerminalEvent(event: TerminalCommandEvent): void {
    if (event.type === "output") {
      appendLines([{ id: nextLineID(), kind: event.stream, text: event.text }]);
      return;
    }
    if (event.type === "exit") {
      appendLines([{ id: nextLineID(), kind: event.exit_code === 0 ? "status" : "error", text: terminalExitText(event) }]);
      if (runningCommandIDRef.current === event.id) {
        runningCommandIDRef.current = undefined;
        setRunningCommandID(undefined);
      }
      knownCommandIDsRef.current.delete(event.id);
      return;
    }
    appendLines([{ id: nextLineID(), kind: "error", text: event.message }]);
    if (runningCommandIDRef.current === event.id) {
      runningCommandIDRef.current = undefined;
      setRunningCommandID(undefined);
    }
    knownCommandIDsRef.current.delete(event.id);
  }

  async function submitCommand(event: ReactFormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault();
    const command = draft.trim();
    if (!command || runningCommandID || !workspaceRoot) {
      return;
    }
    appendLines([{ id: nextLineID(), kind: "command", text: command }]);
    setDraft("");
    setHistory((current) => [...current.filter((item) => item !== command), command].slice(-80));
    setHistoryIndex(undefined);
    try {
      const started = await window.wuu.startTerminalCommand(command);
      knownCommandIDsRef.current.add(started.id);
      runningCommandIDRef.current = started.id;
      setRunningCommandID(started.id);
      flushPendingTerminalEvents(started.id);
    } catch (error) {
      appendLines([{ id: nextLineID(), kind: "error", text: desktopApiErrorMessage(error, "命令启动失败") }]);
    }
  }

  async function stopCommand(): Promise<void> {
    const commandID = runningCommandIDRef.current;
    if (!commandID) {
      return;
    }
    try {
      const stopped = await window.wuu.stopTerminalCommand(commandID);
      if (!stopped.ok) {
        setRunningCommandID(undefined);
      }
    } catch (error) {
      appendLines([{ id: nextLineID(), kind: "error", text: desktopApiErrorMessage(error, "停止失败") }]);
    }
  }

  function handleInputKeyDown(event: ReactKeyboardEvent<HTMLInputElement>): void {
    if (event.key === "ArrowUp") {
      event.preventDefault();
      if (history.length === 0) {
        return;
      }
      const nextIndex = historyIndex === undefined ? history.length - 1 : Math.max(0, historyIndex - 1);
      setHistoryIndex(nextIndex);
      setDraft(history[nextIndex] ?? "");
      return;
    }
    if (event.key === "ArrowDown") {
      event.preventDefault();
      if (historyIndex === undefined) {
        return;
      }
      const nextIndex = historyIndex + 1;
      if (nextIndex >= history.length) {
        setHistoryIndex(undefined);
        setDraft("");
        return;
      }
      setHistoryIndex(nextIndex);
      setDraft(history[nextIndex] ?? "");
    }
  }

  if (!workspaceRoot) {
    return <WorkspacePanelEmpty title="没有项目" description="先选择一个项目。" icon={<Terminal size={24} />} />;
  }

  return (
    <div className="workspace-terminal-panel">
      <div className="workspace-terminal-meta">
        <span>{formatWorkspaceRoot(workspaceRoot)}</span>
        <small>{runningCommandID ? "运行中" : "就绪"}</small>
      </div>
      <div className="workspace-terminal-screen" ref={scrollRef}>
        {lines.length === 0 ? <div className="workspace-terminal-cursor">$</div> : null}
        {lines.map((line) => (
          <pre className={`workspace-terminal-line ${line.kind}`} key={line.id}>
            {line.kind === "command" ? `$ ${line.text}` : line.text}
          </pre>
        ))}
      </div>
      <form className="workspace-terminal-input" onSubmit={(event) => void submitCommand(event)}>
        <span aria-hidden="true">$</span>
        <input
          value={draft}
          placeholder="输入 shell 命令"
          disabled={Boolean(runningCommandID)}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={handleInputKeyDown}
        />
        {runningCommandID ? (
          <button type="button" aria-label="停止命令" onClick={() => void stopCommand()}>
            <Square size={16} />
          </button>
        ) : (
          <button type="submit" aria-label="运行命令" disabled={!draft.trim()}>
            <Send size={16} />
          </button>
        )}
      </form>
    </div>
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
  gitStatus,
  selectedFilePath,
  onOpenRightPanel
}: {
  view: WorkspacePanelView;
  activeContext?: RuntimeContext;
  gitStatus?: GitStatusResult;
  selectedFilePath?: string;
  onOpenRightPanel: () => void;
}): JSX.Element | null {
  if (view === "files") {
    return (
      <WorkspaceFilePreview
        activeContext={activeContext}
        selectedFilePath={selectedFilePath}
        onOpenRightPanel={onOpenRightPanel}
      />
    );
  }

  if (view === "review") {
    return <WorkspaceDiffReview activeContext={activeContext} gitStatus={gitStatus} />;
  }

  return null;
}

function WorkspaceDiffReview({
  activeContext,
  gitStatus
}: {
  activeContext?: RuntimeContext;
  gitStatus?: GitStatusResult;
}): JSX.Element {
  const [changes, setChanges] = useState<GitChangesResult | undefined>(undefined);
  const [selectedPath, setSelectedPath] = useState<string | undefined>(undefined);
  const [fileDiff, setFileDiff] = useState<GitFileDiffResult | undefined>(undefined);
  const [loadingChanges, setLoadingChanges] = useState(false);
  const [loadingDiff, setLoadingDiff] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);
  const [refreshVersion, setRefreshVersion] = useState(0);
  const [treeQuery, setTreeQuery] = useState("");
  const [expandedPaths, setExpandedPaths] = useState<Set<string>>(() => new Set());
  const workspaceRoot = activeContext?.cwd;
  const files = changes?.files ?? [];
  const totals = useMemo(() => summarizeGitChangeFiles(files), [files]);
  const filteredFiles = useMemo(() => filterGitChangeFiles(files, treeQuery), [files, treeQuery]);
  const treeNodes = useMemo(() => buildGitChangeTree(filteredFiles), [filteredFiles]);
  const diffLines = useMemo(() => (fileDiff?.patch ? gitDiffDisplayLines(fileDiff.patch) : []), [fileDiff?.patch]);
  const selectedFile = files.find((file) => file.path === selectedPath);
  const branchLabel = gitStatus?.is_repo ? gitStatus.branch ?? "detached" : "非 Git 仓库";
  const upstreamLabel = gitStatus?.upstream;

  useEffect(() => {
    if (!workspaceRoot) {
      setChanges(undefined);
      setSelectedPath(undefined);
      setFileDiff(undefined);
      setError(undefined);
      setLoadingChanges(false);
      return;
    }

    let cancelled = false;
    setChanges(undefined);
    setSelectedPath(undefined);
    setFileDiff(undefined);
    if (!desktopApiSupportsGitReview()) {
      setError("审查接口还没被当前窗口加载。请重启桌面端后再试。");
      setLoadingChanges(false);
      return;
    }
    setLoadingChanges(true);
    setError(undefined);
    void window.wuu
      .listGitChanges()
      .then((result) => {
        if (cancelled) {
          return;
        }
        setChanges(result);
        setExpandedPaths(new Set(collectGitChangeTreeDirectoryPaths(buildGitChangeTree(result.files))));
        setSelectedPath((current) => {
          if (current && result.files.some((file) => file.path === current)) {
            return current;
          }
          return result.files[0]?.path;
        });
      })
      .catch((nextError) => {
        if (!cancelled) {
          setError(desktopApiErrorMessage(nextError, "读取变更失败"));
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoadingChanges(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [workspaceRoot, refreshVersion]);

  useEffect(() => {
    if (!selectedPath) {
      return;
    }
    setExpandedPaths((current) => {
      const next = new Set(current);
      for (const ancestor of gitPathAncestors(selectedPath)) {
        next.add(ancestor);
      }
      return next;
    });
  }, [selectedPath]);

  useEffect(() => {
    if (!workspaceRoot || !selectedPath) {
      setFileDiff(undefined);
      setLoadingDiff(false);
      return;
    }

    let cancelled = false;
    setFileDiff(undefined);
    if (!desktopApiSupportsGitReview()) {
      setError("审查接口还没被当前窗口加载。请重启桌面端后再试。");
      setLoadingDiff(false);
      return;
    }
    setLoadingDiff(true);
    setError(undefined);
    void window.wuu
      .readGitFileDiff(selectedPath)
      .then((result) => {
        if (!cancelled) {
          setFileDiff(result);
        }
      })
      .catch((nextError) => {
        if (!cancelled) {
          setError(desktopApiErrorMessage(nextError, "读取 diff 失败"));
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoadingDiff(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [workspaceRoot, selectedPath, refreshVersion]);

  function toggleTreePath(path: string): void {
    setExpandedPaths((current) => {
      const next = new Set(current);
      if (next.has(path)) {
        next.delete(path);
      } else {
        next.add(path);
      }
      return next;
    });
  }

  if (!activeContext) {
    return (
      <div className="workspace-main-empty">
        <FolderX size={36} />
        <strong>没有项目</strong>
        <span>先打开一个项目，再查看本地变更。</span>
      </div>
    );
  }

  if (loadingChanges && !changes) {
    return (
      <div className="workspace-main-empty">
        <GitBranch size={36} />
        <strong>正在读取变更</strong>
        <span>{formatWorkspaceRoot(workspaceRoot ?? "")}</span>
      </div>
    );
  }

  if (error && !changes) {
    return (
      <div className="workspace-main-empty">
        <AlertCircle size={36} />
        <strong>读取失败</strong>
        <span>{error}</span>
      </div>
    );
  }

  if (changes && !changes.is_repo) {
    return (
      <div className="workspace-main-empty">
        <FolderX size={36} />
        <strong>不是 Git 仓库</strong>
        <span>当前项目没有可查看的 Git 变更。</span>
      </div>
    );
  }

  if (changes && files.length === 0) {
    return (
      <div className="workspace-main-empty">
        <Check size={36} />
        <strong>工作区干净</strong>
        <span>当前没有未提交的代码差异。</span>
        <button type="button" onClick={() => setRefreshVersion((version) => version + 1)}>
          刷新
        </button>
      </div>
    );
  }

  return (
    <article className="workspace-diff-review">
      <header className="workspace-diff-header">
        <div className="workspace-diff-title">
          <strong>审查</strong>
          <span>
            {branchLabel}
            {upstreamLabel ? (
              <>
                <span className="workspace-diff-branch-arrow">-&gt;</span>
                {upstreamLabel}
              </>
            ) : null}
          </span>
        </div>
        <div className="workspace-diff-summary">
          <span>{files.length} 个文件</span>
          <span className="additions">+{totals.additions.toLocaleString()}</span>
          <span className="deletions">-{totals.deletions.toLocaleString()}</span>
          <button
            className="icon-button"
            type="button"
            aria-label="刷新变更"
            title="刷新变更"
            disabled={loadingChanges || loadingDiff}
            onClick={() => setRefreshVersion((version) => version + 1)}
          >
            <RefreshCw size={16} />
          </button>
        </div>
      </header>
      <div className="workspace-diff-content">
        <section className="workspace-diff-detail">
          <div className="workspace-diff-detail-header">
            <div>
              <strong>{selectedFile ? gitChangeFilePathLabel(selectedFile) : "选择文件"}</strong>
              <span>{selectedFile ? gitChangeStatusDescription(selectedFile) : "从左侧选择一个变更文件"}</span>
            </div>
          </div>
          {error ? <div className="workspace-diff-error">{error}</div> : null}
          {loadingDiff ? (
            <div className="workspace-diff-empty">正在读取 diff...</div>
          ) : fileDiff?.binary ? (
            <div className="workspace-diff-empty">这是二进制文件，无法显示文本 diff。</div>
          ) : fileDiff?.patch ? (
            <OverlayScrollbarsComponent
              className="workspace-diff-code-scroll"
              data-overlayscrollbars-initialize
              defer
              options={OVERLAY_SCROLLBAR_OPTIONS}
            >
              <pre className="workspace-diff-code" aria-label={`${fileDiff.path} 的代码差异`}>
                {diffLines.map((line, index) => (
                  <span
                    className={`workspace-diff-line ${line.kind}`}
                    key={`${index}:${line.content.slice(0, 24)}`}
                  >
                    <span className="workspace-diff-line-number">{line.oldLine ?? ""}</span>
                    <span className="workspace-diff-line-number">{line.newLine ?? ""}</span>
                    <span className="workspace-diff-line-code">{line.content || " "}</span>
                  </span>
                ))}
              </pre>
            </OverlayScrollbarsComponent>
          ) : (
            <div className="workspace-diff-empty">没有可显示的文本 diff。</div>
          )}
          {fileDiff?.truncated ? <div className="workspace-diff-truncated">diff 太大，已截断预览。</div> : null}
        </section>
        <GitChangeTreePanel
          files={filteredFiles}
          nodes={treeNodes}
          selectedPath={selectedPath}
          expandedPaths={expandedPaths}
          query={treeQuery}
          onQueryChange={setTreeQuery}
          onSelectFile={setSelectedPath}
          onTogglePath={toggleTreePath}
        />
      </div>
    </article>
  );
}

function GitChangeTreePanel({
  files,
  nodes,
  selectedPath,
  expandedPaths,
  query,
  onQueryChange,
  onSelectFile,
  onTogglePath
}: {
  files: GitChangeFile[];
  nodes: GitChangeTreeNode[];
  selectedPath?: string;
  expandedPaths: Set<string>;
  query: string;
  onQueryChange: (value: string) => void;
  onSelectFile: (path: string) => void;
  onTogglePath: (path: string) => void;
}): JSX.Element {
  const forceExpanded = query.trim().length > 0;
  const totals = summarizeGitChangeFiles(files);
  return (
    <aside className="workspace-diff-tree" aria-label="变更文件树">
      <div className="workspace-diff-tree-header">
        <div>
          <strong>文件</strong>
          <span>
            {forceExpanded ? `${files.length} 个匹配` : `${files.length} 个文件`}
            {files.length > 0 ? (
              <>
                {" "}
                <span className="additions">+{totals.additions.toLocaleString()}</span>{" "}
                <span className="deletions">-{totals.deletions.toLocaleString()}</span>
              </>
            ) : null}
          </span>
        </div>
      </div>
      <label className="workspace-diff-search">
        <Search size={16} />
        <input
          value={query}
          placeholder="筛选文件..."
          onChange={(event) => onQueryChange(event.currentTarget.value)}
        />
      </label>
      <OverlayScrollbarsComponent
        className="workspace-diff-tree-scroll"
        data-overlayscrollbars-initialize
        defer
        options={OVERLAY_SCROLLBAR_OPTIONS}
      >
        {nodes.length === 0 ? (
          <div className="workspace-diff-tree-empty">没有匹配文件</div>
        ) : (
          <div className="workspace-diff-tree-list">
            {nodes.map((node) => (
              <GitChangeTreeNodeView
                key={node.id}
                node={node}
                depth={0}
                forceExpanded={forceExpanded}
                selectedPath={selectedPath}
                expandedPaths={expandedPaths}
                onSelectFile={onSelectFile}
                onTogglePath={onTogglePath}
              />
            ))}
          </div>
        )}
      </OverlayScrollbarsComponent>
    </aside>
  );
}

function GitChangeTreeNodeView({
  node,
  depth,
  forceExpanded,
  selectedPath,
  expandedPaths,
  onSelectFile,
  onTogglePath
}: {
  node: GitChangeTreeNode;
  depth: number;
  forceExpanded: boolean;
  selectedPath?: string;
  expandedPaths: Set<string>;
  onSelectFile: (path: string) => void;
  onTogglePath: (path: string) => void;
}): JSX.Element {
  const indentation = { paddingLeft: `${10 + depth * 18}px` } as CSSProperties;
  if (node.kind === "directory") {
    const expanded = forceExpanded || expandedPaths.has(node.path);
    return (
      <div className="workspace-diff-tree-node">
        <button
          className="workspace-diff-tree-row directory"
          type="button"
          style={indentation}
          aria-expanded={expanded}
          onClick={() => onTogglePath(node.path)}
        >
          <ChevronRight className="workspace-diff-tree-chevron" size={15} />
          {expanded ? <FolderOpen size={16} /> : <Folder size={16} />}
          <span className="workspace-diff-tree-name">{node.name}</span>
          <span className="workspace-diff-tree-count">{node.fileCount}</span>
        </button>
        {expanded ? (
          <div className="workspace-diff-tree-children">
            {node.children.map((child) => (
              <GitChangeTreeNodeView
                key={child.id}
                node={child}
                depth={depth + 1}
                forceExpanded={forceExpanded}
                selectedPath={selectedPath}
                expandedPaths={expandedPaths}
                onSelectFile={onSelectFile}
                onTogglePath={onTogglePath}
              />
            ))}
          </div>
        ) : null}
      </div>
    );
  }

  const file = node.file;
  const selected = file?.path === selectedPath;
  return (
    <button
      className={`workspace-diff-tree-row file${selected ? " active" : ""}`}
      type="button"
      style={indentation}
      aria-pressed={selected}
      onClick={() => {
        if (file) {
          onSelectFile(file.path);
        }
      }}
    >
      <span className="workspace-diff-tree-spacer" />
      <FileText size={16} />
      <span className="workspace-diff-tree-name">{node.name}</span>
      {file ? <GitChangeFileStats file={file} /> : null}
    </button>
  );
}

function GitChangeFileStats({ file }: { file: GitChangeFile }): JSX.Element {
  return (
    <span className="workspace-diff-tree-stats">
      <span className={`workspace-diff-file-status ${file.status}`}>{gitChangeStatusLabel(file.status)}</span>
      {file.binary ? (
        <span className="workspace-diff-tree-binary">binary</span>
      ) : (
        <>
          <span className="additions">+{file.additions.toLocaleString()}</span>
          <span className="deletions">-{file.deletions.toLocaleString()}</span>
        </>
      )}
    </span>
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

function summarizeGitChangeFiles(files: GitChangeFile[]): { additions: number; deletions: number } {
  return files.reduce(
    (summary, file) => ({
      additions: summary.additions + file.additions,
      deletions: summary.deletions + file.deletions
    }),
    { additions: 0, deletions: 0 }
  );
}

function filterGitChangeFiles(files: GitChangeFile[], query: string): GitChangeFile[] {
  const normalized = query.trim().toLocaleLowerCase();
  if (!normalized) {
    return files;
  }
  return files.filter((file) => {
    const current = file.path.toLocaleLowerCase();
    const previous = file.old_path?.toLocaleLowerCase() ?? "";
    return current.includes(normalized) || previous.includes(normalized);
  });
}

function buildGitChangeTree(files: GitChangeFile[]): GitChangeTreeNode[] {
  const root = createGitChangeDirectoryNode("", "");
  for (const file of files) {
    insertGitChangeTreeFile(root, file);
  }
  summarizeGitChangeTreeNode(root);
  sortGitChangeTreeNodes(root.children);
  return root.children;
}

function createGitChangeDirectoryNode(name: string, path: string): GitChangeTreeNode {
  return {
    kind: "directory",
    id: `dir:${path || "root"}`,
    name,
    path,
    children: [],
    additions: 0,
    deletions: 0,
    fileCount: 0,
    binary: false
  };
}

function insertGitChangeTreeFile(root: GitChangeTreeNode, file: GitChangeFile): void {
  const parts = file.path.split("/").filter(Boolean);
  if (parts.length === 0) {
    return;
  }
  let parent = root;
  for (let index = 0; index < parts.length - 1; index++) {
    const path = parts.slice(0, index + 1).join("/");
    let child = parent.children.find((node) => node.kind === "directory" && node.path === path);
    if (!child) {
      child = createGitChangeDirectoryNode(parts[index], path);
      parent.children.push(child);
    }
    parent = child;
  }
  parent.children.push({
    kind: "file",
    id: `file:${file.path}`,
    name: parts[parts.length - 1],
    path: file.path,
    children: [],
    file,
    additions: file.additions,
    deletions: file.deletions,
    fileCount: 1,
    binary: file.binary === true
  });
}

function summarizeGitChangeTreeNode(node: GitChangeTreeNode): void {
  if (node.kind === "file") {
    return;
  }
  let additions = 0;
  let deletions = 0;
  let fileCount = 0;
  let binary = false;
  for (const child of node.children) {
    summarizeGitChangeTreeNode(child);
    additions += child.additions;
    deletions += child.deletions;
    fileCount += child.fileCount;
    binary = binary || child.binary;
  }
  node.additions = additions;
  node.deletions = deletions;
  node.fileCount = fileCount;
  node.binary = binary;
}

function sortGitChangeTreeNodes(nodes: GitChangeTreeNode[]): void {
  nodes.sort((left, right) => {
    if (left.kind !== right.kind) {
      return left.kind === "directory" ? -1 : 1;
    }
    return left.name.localeCompare(right.name, undefined, { sensitivity: "base" });
  });
  for (const node of nodes) {
    sortGitChangeTreeNodes(node.children);
  }
}

function collectGitChangeTreeDirectoryPaths(nodes: GitChangeTreeNode[]): string[] {
  const paths: string[] = [];
  for (const node of nodes) {
    if (node.kind !== "directory") {
      continue;
    }
    paths.push(node.path);
    paths.push(...collectGitChangeTreeDirectoryPaths(node.children));
  }
  return paths;
}

function gitPathAncestors(path: string): string[] {
  const parts = path.split("/").filter(Boolean);
  const ancestors: string[] = [];
  for (let index = 0; index < parts.length - 1; index++) {
    ancestors.push(parts.slice(0, index + 1).join("/"));
  }
  return ancestors;
}

function gitChangeStatusLabel(status: GitChangeFile["status"]): string {
  switch (status) {
    case "modified":
      return "M";
    case "added":
      return "A";
    case "deleted":
      return "D";
    case "renamed":
      return "R";
    case "copied":
      return "C";
    case "untracked":
      return "U";
    default:
      return "?";
  }
}

function gitChangeStatusText(status: GitChangeFile["status"]): string {
  switch (status) {
    case "modified":
      return "已修改";
    case "added":
      return "已新增";
    case "deleted":
      return "已删除";
    case "renamed":
      return "已重命名";
    case "copied":
      return "已复制";
    case "untracked":
      return "未跟踪";
    default:
      return "已变更";
  }
}

function gitChangeFilePathLabel(file: GitChangeFile): string {
  return file.old_path && file.old_path !== file.path ? `${file.old_path} -> ${file.path}` : file.path;
}

function gitChangeStatusDescription(file: GitChangeFile): string {
  if (file.binary) {
    return `${gitChangeStatusText(file.status)} · 二进制文件`;
  }
  return `${gitChangeStatusText(file.status)} · +${file.additions.toLocaleString()} -${file.deletions.toLocaleString()}`;
}

function gitDiffDisplayLines(patch: string): GitDiffDisplayLine[] {
  const lines: GitDiffDisplayLine[] = [];
  let oldLine: number | undefined;
  let newLine: number | undefined;
  for (const content of patch.split("\n")) {
    if (content.startsWith("@@")) {
      const match = /@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/.exec(content);
      oldLine = match ? Number(match[1]) : undefined;
      newLine = match ? Number(match[2]) : undefined;
      lines.push({ content, kind: "hunk" });
      continue;
    }
    if (content.startsWith("diff --git") || content.startsWith("index ") || content.startsWith("--- ") || content.startsWith("+++ ")) {
      lines.push({ content, kind: "meta" });
      continue;
    }
    if (content.startsWith("\\ No newline")) {
      lines.push({ content, kind: "meta" });
      continue;
    }
    if (content.startsWith("+")) {
      lines.push({ content, kind: "add", newLine });
      if (newLine !== undefined) {
        newLine++;
      }
      continue;
    }
    if (content.startsWith("-")) {
      lines.push({ content, kind: "delete", oldLine });
      if (oldLine !== undefined) {
        oldLine++;
      }
      continue;
    }
    lines.push({ content, kind: "context", oldLine, newLine });
    if (oldLine !== undefined) {
      oldLine++;
    }
    if (newLine !== undefined) {
      newLine++;
    }
  }
  return lines;
}

function desktopApiSupportsGitReview(): boolean {
  const maybeApi = window.wuu as Partial<typeof window.wuu>;
  return typeof maybeApi.listGitChanges === "function" && typeof maybeApi.readGitFileDiff === "function";
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
      const params = event.message.params as { thread_id?: string; questions?: AskUserQuestion[] } | undefined;
      const request: AskRequestState = {
        id: event.message.id,
        threadID: typeof params?.thread_id === "string" && params.thread_id ? params.thread_id : undefined,
        questions: params?.questions ?? []
      };
      return {
        ...state,
        answeredAskRequests: state.answeredAskRequests.filter((request) => request.id !== event.message.id),
        askRequests: upsertAskRequest(state.askRequests, request)
      };
    }
    case "server-error":
      return { ...state, status: event.message };
    case "server-exit":
      return { ...state, running: false, status: "app-server exited" };
  }
}

type StreamingNotificationHandling = "state" | "stream" | "skip";

function handleStreamingNotification(event: ServerEvent, state: AppState): StreamingNotificationHandling {
  if (event.kind !== "notification") {
    return "state";
  }
  const notification = event.message;
  const params = notification.params as Record<string, unknown> | undefined;
  switch (notification.method) {
    case "item/agentMessage/delta":
      if (!notificationTargetsActiveThread(params, state)) {
        return "skip";
      }
      appendStreamDelta(params, "text");
      return "stream";
    case "item/reasoning/delta":
      if (!notificationTargetsActiveThread(params, state)) {
        return "skip";
      }
      appendStreamDelta(params, "text");
      return "stream";
    case "item/toolCall/delta":
      if (!notificationTargetsActiveThread(params, state)) {
        return "skip";
      }
      appendStreamDelta(params, "arguments");
      return "stream";
    case "item/toolCall/outputDelta":
      if (!notificationTargetsActiveThread(params, state)) {
        return "skip";
      }
      appendStreamDelta(params, "result");
      return "stream";
    case "turn/event":
      return "skip";
    case "item/started":
    case "item/completed":
      if (notificationTargetsActiveThread(params, state)) {
        syncStreamItem(params);
      }
      return "state";
    default:
      return "state";
  }
}

function notificationTargetsActiveThread(params: Record<string, unknown> | undefined, state: AppState): boolean {
  const threadID = threadIDFromParams(params);
  return !threadID || threadID === state.thread?.id;
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
      const knownThread = state.threads.some((item) => item.id === thread.id);
      const activateThread = state.thread?.id === thread.id || (!state.thread && !knownThread);
      return {
        ...state,
        thread: activateThread ? thread : state.thread,
        threads: upsertThread(state.threads, thread),
        status: activateThread ? "ready" : state.status
      };
    }
    case "agent/updated": {
      const threadID = threadIDFromParams(params);
      const agent = agentFromRecord(recordValue(params, "agent"));
      if (!threadID || !agent || !isDirectChildAgent(threadID, agent)) {
        return state;
      }
      return updateThreadByID(state, threadID, (thread) => upsertThreadChildAgent(thread, agent));
    }
    case "turn/started": {
      const turn = params?.turn as Turn | undefined;
      if (!turn) {
        return state;
      }
      return updateThreadByID(state, threadIDFromParams(params), (thread) => upsertTurn(thread, turn), {
        running: true
      });
    }
    case "item/started":
    case "item/completed": {
      const item = params?.item as ThreadItem | undefined;
      const turnID = params?.turn_id as string | undefined;
      if (!item || !turnID) {
        return state;
      }
      return updateThreadByID(state, threadIDFromParams(params), (thread) => upsertTurnItem(thread, turnID, item));
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
      const threadID = threadIDFromParams(params);
      if (!turn) {
        return threadID === state.thread?.id ? { ...state, running: false } : state;
      }
      return updateThreadByID(state, threadID, (thread) => upsertTurn(thread, turn), {
        running: false,
        status: "ready"
      });
    }
    default:
      return state;
  }
}

function applyDelta(state: AppState, params: Record<string, unknown> | undefined, field: "text" | "arguments" | "result"): AppState {
  const threadID = threadIDFromParams(params);
  const turnID = params?.turn_id as string | undefined;
  const itemID = params?.item_id as string | undefined;
  const delta = params?.delta as string | undefined;
  if (!turnID || !itemID || !delta) {
    return state;
  }
  return updateThreadByID(state, threadID, (thread) =>
    updateTurnItem(thread, turnID, itemID, (item) => ({
      ...item,
      [field]: `${item[field] ?? ""}${delta}`
    }))
  );
}

function threadIDFromParams(params: Record<string, unknown> | undefined): string | undefined {
  const threadID = params?.thread_id;
  return typeof threadID === "string" && threadID ? threadID : undefined;
}

function updateThreadByID(
  state: AppState,
  threadID: string | undefined,
  update: (thread: Thread) => Thread,
  activePatch: Partial<Pick<AppState, "running" | "status">> = {}
): AppState {
  if (!threadID) {
    return state;
  }
  const active = state.thread?.id === threadID;
  if (active && state.thread) {
    const thread = update(state.thread);
    return { ...state, ...activePatch, thread, threads: upsertThread(state.threads, thread) };
  }
  let updated = false;
  const threads = state.threads.map((thread) => {
    if (thread.id !== threadID) {
      return thread;
    }
    updated = true;
    return update(thread);
  });
  if (!updated) {
    return state;
  }
  return { ...state, threads: sortThreads(threads) };
}

function updateThread(state: AppState, update: (thread: Thread) => Thread): AppState {
  if (!state.thread) {
    return state;
  }
  const thread = update(state.thread);
  return { ...state, thread, threads: upsertThread(state.threads, thread) };
}

function upsertThread(threads: Thread[], thread: Thread | undefined): Thread[] {
  const validThreads = sortThreads(threads);
  if (!isThread(thread)) {
    return validThreads;
  }
  if (thread.archived || thread.read_only) {
    return validThreads.filter((item) => item.id !== thread.id);
  }
  const index = validThreads.findIndex((item) => item.id === thread.id);
  if (index < 0) {
    return sortThreads([thread, ...validThreads]);
  }
  const next = validThreads.slice();
  next[index] = thread;
  return sortThreads(next);
}

function sortThreads(threads: Thread[]): Thread[] {
  return threads
    .filter((thread): thread is Thread => isThread(thread) && !thread.archived && !thread.read_only)
    .sort((left, right) => threadTime(right) - threadTime(left));
}

function pinnedThreads(threads: Thread[]): Thread[] {
  return sortThreads(threads).filter((thread) => thread.pinned);
}

function projectThreads(threads: Thread[]): Thread[] {
  return sortThreads(threads).filter((thread) => !thread.pinned);
}

function threadTime(thread: Thread): number {
  const updatedAt = Date.parse(thread.updated_at);
  if (Number.isFinite(updatedAt)) {
    return updatedAt;
  }
  const createdAt = Date.parse(thread.created_at);
  return Number.isFinite(createdAt) ? createdAt : 0;
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

function isThreadRunning(thread: Thread | undefined): boolean {
  if (thread?.read_only) {
    return false;
  }
  return Boolean(thread?.status === "in_progress" || thread?.turns.some((turn) => turn.status === "in_progress"));
}

function isStateActiveThreadRunning(state: AppState): boolean {
  return Boolean(state.running || isThreadRunning(state.thread));
}

function isAnyThreadRunning(state: AppState): boolean {
  return Boolean(state.running || isThreadRunning(state.thread) || state.threads.some(isThreadRunning));
}

function visibleAskRequestForThread(requests: AskRequestState[], threadID: string | undefined): AskRequestState | undefined {
  for (let index = requests.length - 1; index >= 0; index--) {
    const request = requests[index];
    if (!request.threadID || request.threadID === threadID) {
      return request;
    }
  }
  return undefined;
}

function pendingAskThreadIDsForRequests(requests: AskRequestState[]): Set<string> {
  const ids = new Set<string>();
  for (const request of requests) {
    if (request.threadID) {
      ids.add(request.threadID);
    }
  }
  return ids;
}

function visibleAnsweredAskRequestsForThread(
  requests: AnsweredAskRequestState[],
  threadID: string | undefined
): AnsweredAskRequestState[] {
  return requests.filter((request) => !request.threadID || request.threadID === threadID);
}

function upsertAskRequest(requests: AskRequestState[], request: AskRequestState): AskRequestState[] {
  const index = requests.findIndex((item) => item.id === request.id);
  if (index < 0) {
    return [...requests, request];
  }
  const next = requests.slice();
  next[index] = request;
  return next;
}

function removeAskRequest(requests: AskRequestState[], id: string): AskRequestState[] {
  return requests.filter((request) => request.id !== id);
}

function upsertAnsweredAskRequest(
  requests: AnsweredAskRequestState[],
  request: AnsweredAskRequestState
): AnsweredAskRequestState[] {
  const index = requests.findIndex((item) => item.id === request.id);
  if (index < 0) {
    return [...requests, request];
  }
  const next = requests.slice();
  next[index] = request;
  return next;
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

function upsertThreadChildAgent(thread: Thread, agent: Agent): Thread {
  const current = thread.child_agents ?? [];
  const index = current.findIndex((item) => item.id === agent.id);
  const nextAgent = mergeAgentSummary(index >= 0 ? current[index] : undefined, agent);
  const next = current.slice();
  if (index < 0) {
    next.push(nextAgent);
  } else {
    next[index] = nextAgent;
  }
  return { ...thread, child_agents: sortChildAgents(next) };
}

function mergeAgentSummary(current: Agent | undefined, incoming: Agent): Agent {
  if (!current) {
    return incoming;
  }
  return {
    ...current,
    ...incoming,
    nested_count: incoming.nested_count ?? current.nested_count,
    nested_running_count: incoming.nested_running_count ?? current.nested_running_count,
    started_at: incoming.started_at ?? current.started_at,
    completed_at: incoming.completed_at ?? current.completed_at
  };
}

function sortChildAgents(agents: Agent[]): Agent[] {
  return agents.slice().sort((left, right) => {
    const leftTime = Date.parse(left.started_at ?? "");
    const rightTime = Date.parse(right.started_at ?? "");
    if (Number.isFinite(leftTime) && Number.isFinite(rightTime) && leftTime !== rightTime) {
      return leftTime - rightTime;
    }
    return agentLabel(left).localeCompare(agentLabel(right), "zh-CN");
  });
}

function ProjectList({
  projects,
  activeID,
  threads,
  activeThreadID,
  pendingAskThreadIDs,
  archiveConfirmThreadID,
  onSelectProject,
  onSelectThread,
  onSelectChildAgent,
  onToggleThreadPinned,
  onArchiveThread,
  onClearArchiveConfirm
}: {
  projects: DesktopProject[];
  activeID?: string;
  threads: Thread[];
  activeThreadID?: string;
  pendingAskThreadIDs: Set<string>;
  archiveConfirmThreadID?: string;
  onSelectProject: (id: string) => void;
  onSelectThread: (id: string) => void;
  onSelectChildAgent: (agent: Agent) => void;
  onToggleThreadPinned: (thread: Thread) => void;
  onArchiveThread: (thread: Thread) => void;
  onClearArchiveConfirm: (threadID: string) => void;
}): JSX.Element {
  return (
    <div className="projects">
      {projects.map((project) => (
        <div key={project.id} className="project-group">
          <button
            className={`project-row ${project.id === activeID ? "active" : ""}`}
            aria-current={project.id === activeID ? "page" : undefined}
            onClick={() => onSelectProject(project.id)}
          >
            <ChevronRight className="project-row-chevron" size={15} aria-hidden="true" />
            <Folder size={18} />
            <span>{project.name}</span>
          </button>
          {project.id === activeID ? (
            <ThreadList
              threads={threads}
              activeID={activeThreadID}
              pendingAskThreadIDs={pendingAskThreadIDs}
              archiveConfirmThreadID={archiveConfirmThreadID}
              onSelect={onSelectThread}
              onSelectChildAgent={onSelectChildAgent}
              onTogglePinned={onToggleThreadPinned}
              onArchive={onArchiveThread}
              onClearArchiveConfirm={onClearArchiveConfirm}
            />
          ) : null}
        </div>
      ))}
    </div>
  );
}

function ThreadList({
  threads,
  activeID,
  pendingAskThreadIDs,
  archiveConfirmThreadID,
  onSelect,
  onSelectChildAgent,
  onTogglePinned,
  onArchive,
  onClearArchiveConfirm
}: {
  threads: Thread[];
  activeID?: string;
  pendingAskThreadIDs: Set<string>;
  archiveConfirmThreadID?: string;
  onSelect: (id: string) => void;
  onSelectChildAgent: (agent: Agent) => void;
  onTogglePinned: (thread: Thread) => void;
  onArchive: (thread: Thread) => void;
  onClearArchiveConfirm: (threadID: string) => void;
}): JSX.Element {
  const visibleThreads = projectThreads(threads);
  return (
    <div className="thread-list">
      <ThreadRows
        threads={visibleThreads}
        activeID={activeID}
        pendingAskThreadIDs={pendingAskThreadIDs}
        archiveConfirmThreadID={archiveConfirmThreadID}
        onSelect={onSelect}
        onSelectChildAgent={onSelectChildAgent}
        onTogglePinned={onTogglePinned}
        onArchive={onArchive}
        onClearArchiveConfirm={onClearArchiveConfirm}
      />
    </div>
  );
}

function PinnedThreadList({
  threads,
  activeID,
  pendingAskThreadIDs,
  archiveConfirmThreadID,
  onSelect,
  onSelectChildAgent,
  onTogglePinned,
  onArchive,
  onClearArchiveConfirm
}: {
  threads: Thread[];
  activeID?: string;
  pendingAskThreadIDs: Set<string>;
  archiveConfirmThreadID?: string;
  onSelect: (id: string) => void;
  onSelectChildAgent: (agent: Agent) => void;
  onTogglePinned: (thread: Thread) => void;
  onArchive: (thread: Thread) => void;
  onClearArchiveConfirm: (threadID: string) => void;
}): JSX.Element {
  return (
    <div className="pinned-thread-list">
      <ThreadRows
        threads={threads}
        activeID={activeID}
        pendingAskThreadIDs={pendingAskThreadIDs}
        archiveConfirmThreadID={archiveConfirmThreadID}
        onSelect={onSelect}
        onSelectChildAgent={onSelectChildAgent}
        onTogglePinned={onTogglePinned}
        onArchive={onArchive}
        onClearArchiveConfirm={onClearArchiveConfirm}
      />
    </div>
  );
}

function ThreadRows({
  threads,
  activeID,
  pendingAskThreadIDs,
  archiveConfirmThreadID,
  onSelect,
  onSelectChildAgent,
  onTogglePinned,
  onArchive,
  onClearArchiveConfirm
}: {
  threads: Thread[];
  activeID?: string;
  pendingAskThreadIDs: Set<string>;
  archiveConfirmThreadID?: string;
  onSelect: (id: string) => void;
  onSelectChildAgent: (agent: Agent) => void;
  onTogglePinned: (thread: Thread) => void;
  onArchive: (thread: Thread) => void;
  onClearArchiveConfirm: (threadID: string) => void;
}): JSX.Element {
  return (
    <>
      {threads.map((thread, index) => {
        const archiveConfirming = archiveConfirmThreadID === thread.id;
        const pendingAsk = pendingAskThreadIDs.has(thread.id);
        return (
          <Fragment key={thread.id}>
            <div
              className={`thread-row ${thread.id === activeID ? "active" : ""}${pendingAsk ? " pending-ask" : ""}`}
              aria-current={thread.id === activeID ? "page" : undefined}
              style={{ animationDelay: `${index * 18}ms` } as CSSProperties}
              onMouseLeave={() => onClearArchiveConfirm(thread.id)}
            >
              <button className="thread-row-main" type="button" onClick={() => onSelect(thread.id)}>
                <span className="thread-row-title">{thread.preview || "未命名对话"}</span>
                {pendingAsk ? (
                  <span className="thread-row-ask-badge" title="需要你选择">
                    <MessageSquarePlus size={12} />
                    <span>需选择</span>
                  </span>
                ) : null}
              </button>
              <div className="thread-row-actions" aria-label="对话操作">
                <button
                  className={`thread-row-action ${thread.pinned ? "active" : ""}`}
                  type="button"
                  aria-label={thread.pinned ? "取消置顶" : "置顶"}
                  title={thread.pinned ? "取消置顶" : "置顶"}
                  onClick={() => onTogglePinned(thread)}
                >
                  <Pin size={14} />
                </button>
                <button
                  className={`thread-row-action archive ${archiveConfirming ? "confirm" : ""}`}
                  type="button"
                  aria-label={archiveConfirming ? "确认归档" : "归档"}
                  title={archiveConfirming ? "再次点击归档" : "归档"}
                  onClick={() => onArchive(thread)}
                >
                  <Archive size={14} />
                </button>
              </div>
            </div>
            {thread.child_agents?.length ? (
              <ThreadChildAgentRows agents={thread.child_agents} activeID={activeID} onSelect={onSelectChildAgent} />
            ) : null}
          </Fragment>
        );
      })}
    </>
  );
}

function ThreadChildAgentRows({
  agents,
  activeID,
  onSelect
}: {
  agents: Agent[];
  activeID?: string;
  onSelect: (agent: Agent) => void;
}): JSX.Element {
  return (
    <div className="thread-child-agent-list" aria-label="子任务">
      {sortChildAgents(agents).map((agent) => {
        const status = agentStatusTone(agent.status);
        const label = agentLabel(agent);
        const nestedLabel = agentNestedLabel(agent);
        const active = activeID === agent.id;
        return (
          <button
            key={agent.id}
            className={`thread-child-agent-row ${status}${active ? " active" : ""}`}
            type="button"
            aria-current={active ? "page" : undefined}
            aria-label={`${label}，${agentStatusLabel(agent.status)}`}
            title={agentTooltip(agent)}
            onClick={() => onSelect(agent)}
          >
            <CornerDownRight className="thread-child-agent-branch" size={13} />
            <span className="thread-child-agent-name">{label}</span>
            {nestedLabel ? <span className="thread-child-agent-nested">{nestedLabel}</span> : null}
            <span className="thread-child-agent-status">{agentStatusLabel(agent.status)}</span>
          </button>
        );
      })}
    </div>
  );
}

function agentLabel(agent: Agent): string {
  const pathParts = agent.agent_path?.split("/").filter(Boolean) ?? [];
  return agent.task_name || agent.description || pathParts[pathParts.length - 1] || agent.id;
}

function agentTooltip(agent: Agent): string {
  const path = agent.agent_path ? ` · ${agent.agent_path}` : "";
  return `${agentLabel(agent)} · ${agentStatusLabel(agent.status)}${path}`;
}

function agentNestedLabel(agent: Agent): string | undefined {
  const total = agent.nested_count ?? 0;
  if (total <= 0) {
    return undefined;
  }
  const running = agent.nested_running_count ?? 0;
  return running > 0 ? `${running}/${total}` : `+${total}`;
}

function agentStatusLabel(status: string | undefined): string {
  switch (status) {
    case "pending":
      return "等待";
    case "running":
      return "运行中";
    case "completed":
      return "完成";
    case "failed":
      return "失败";
    case "cancelled":
      return "已停止";
    default:
      return status?.trim() || "未知";
  }
}

function agentStatusTone(status: string | undefined): "running" | "completed" | "failed" | "cancelled" | "pending" {
  switch (status) {
    case "running":
      return "running";
    case "completed":
      return "completed";
    case "failed":
      return "failed";
    case "cancelled":
      return "cancelled";
    default:
      return "pending";
  }
}

function TurnView({
  turn,
  cwd,
  onStreamFrame
}: {
  turn: Turn;
  cwd?: string;
  onStreamFrame: () => void;
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
      renderedItems.push(<TurnStatusLine key={`${turn.id}-status`} turn={turn} />);
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
    renderedItems.push(<TurnStatusLine key={`${turn.id}-status`} turn={turn} />);
  }

  return (
    <section className="turn">
      {renderedItems}
      {turn.error ? <div className="turn-error">{turn.error.message}</div> : null}
    </section>
  );
}

function TurnStatusLine({ turn }: { turn: Turn }): JSX.Element {
  const completedDuration = typeof turn.duration_ms === "number" ? turn.duration_ms : undefined;
  const startedAt = parseTurnTimestampMs(turn.started_at);
  const liveDuration = completedDuration === undefined && turn.status === "in_progress" && Number.isFinite(startedAt);
  const liveNow = useLiveNow(liveDuration);
  const elapsedMs = completedDuration ?? (liveDuration ? Math.max(0, liveNow - startedAt) : 0);
  const showDuration = completedDuration !== undefined || liveDuration;
  const content = turnProgressContent(turn, elapsedMs);
  const campaign =
    ENABLE_TURN_PROGRESS_EXPERIMENT && liveDuration ? turnProgressCampaign(turn.id, elapsedMs) : undefined;

  return (
    <div
      className={`turn-progress ${turn.status}${campaign ? " has-campaign" : ""}`}
      role={liveDuration ? "status" : undefined}
      aria-live={liveDuration ? "polite" : undefined}
    >
      <div className="turn-progress-header">
        <div className="turn-progress-label">
          <Clock size={17} />
          <span className="turn-progress-copy">
            <span className="turn-progress-title">
              <span>{content.label}</span>
              {showDuration ? <span className="turn-progress-duration">{formatDuration(elapsedMs)}</span> : null}
            </span>
            {content.detail ? <span className="turn-progress-detail">{content.detail}</span> : null}
          </span>
        </div>
      </div>
      <div className="turn-progress-rule">{campaign ? <TurnProgressCampaignScene campaign={campaign} /> : null}</div>
    </div>
  );
}

function TurnProgressPreviewOverlay({ onClose }: { onClose: () => void }): JSX.Element {
  const [startedAt, setStartedAt] = useState(() => Date.now());
  const [complete, setComplete] = useState(false);
  const now = usePreviewNow(!complete);
  const previewElapsedMs = Math.min(TURN_PROGRESS_PREVIEW_MS, Math.max(0, now - startedAt));
  const previewRatio = previewElapsedMs / TURN_PROGRESS_PREVIEW_MS;
  const previewComplete = previewElapsedMs >= TURN_PROGRESS_PREVIEW_MS;
  const campaignElapsedMs = previewComplete
    ? TURN_PROGRESS_CAMPAIGN_MS
    : Math.min(TURN_PROGRESS_CAMPAIGN_MS - 1, previewRatio * TURN_PROGRESS_CAMPAIGN_MS);
  const campaign = turnProgressCampaign("turn-progress-preview", Math.min(TURN_PROGRESS_CAMPAIGN_MS - 1, campaignElapsedMs));

  useEffect(() => {
    if (!complete && previewComplete) {
      setComplete(true);
    }
  }, [complete, previewComplete]);

  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent): void {
      if (event.key === "Escape") {
        onClose();
      }
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [onClose]);

  function restart(): void {
    setStartedAt(Date.now());
    setComplete(false);
  }

  const progressStyle = { width: `${previewRatio * 100}%` } as CSSProperties;

  return (
    <div className="turn-progress-preview-backdrop" role="dialog" aria-modal="true" aria-label="完整预览等待动画">
      <div className="turn-progress-preview-panel">
        <div className="turn-progress-preview-header">
          <div>
            <h2>完整预览</h2>
            <p>
              {formatDuration(campaignElapsedMs)} / {formatDuration(TURN_PROGRESS_CAMPAIGN_MS)}
              <span>{TURN_PROGRESS_ERA_LABELS[campaign.currentEra]}</span>
              <span>{formatDuration(TURN_PROGRESS_PREVIEW_MS)} 预览</span>
              <span>{Math.round(TURN_PROGRESS_PREVIEW_SPEED)}x</span>
            </p>
          </div>
          <div className="turn-progress-preview-actions">
            <button className="icon-button" type="button" aria-label="重播" title="重播" onClick={restart}>
              <RefreshCw size={16} />
            </button>
            <button className="icon-button" type="button" aria-label="关闭" title="关闭" onClick={onClose}>
              <X size={17} />
            </button>
          </div>
        </div>
        <div className="turn-progress-preview-stage">
          <div className="turn-progress-preview-rule">
            <TurnProgressCampaignScene campaign={campaign} />
          </div>
          <div className="turn-progress-preview-track" aria-hidden="true">
            <span style={progressStyle} />
          </div>
        </div>
      </div>
    </div>
  );
}

function TurnProgressCampaignScene({ campaign }: { campaign: TurnProgressCampaign }): JSX.Element {
  const currentLayerActive = campaign.currentLayer === "a";
  return (
    <span className="turn-progress-campaign" aria-hidden="true">
      <TurnProgressSceneLayer
        era={currentLayerActive ? campaign.currentEra : campaign.nextEra}
        variant={campaign.variant}
        state={currentLayerActive ? "current" : "next"}
        transitionProgress={campaign.transitionProgress}
      />
      <TurnProgressSceneLayer
        era={currentLayerActive ? campaign.nextEra : campaign.currentEra}
        variant={campaign.variant}
        state={currentLayerActive ? "next" : "current"}
        transitionProgress={campaign.transitionProgress}
      />
    </span>
  );
}

function TurnProgressSceneLayer({
  era,
  variant,
  state,
  transitionProgress
}: {
  era: TurnProgressEra;
  variant: number;
  state: "current" | "next";
  transitionProgress: number;
}): JSX.Element {
  const entering = state === "next";
  const progress = entering ? transitionProgress : 1 - transitionProgress;
  const opacity = entering ? transitionProgress : 1 - transitionProgress;
  const yOffset = entering ? (1 - progress) * 6 : -transitionProgress * 5;
  const scale = entering ? 0.982 + progress * 0.018 : 1 + transitionProgress * 0.012;
  const blur = entering ? (1 - progress) * 1.1 : transitionProgress * 1.1;
  const style = {
    "--scene-opacity": opacity,
    "--scene-y": `${yOffset}px`,
    "--scene-scale": scale,
    "--scene-blur": `${blur}px`
  } as CSSProperties;

  return (
    <span className={`turn-progress-scene era-${era} variant-${variant} scene-${state}`} style={style}>
      <span className="civilization-prop prop-left" />
      <span className="civilization-prop prop-mid" />
      <span className="civilization-prop prop-right" />
      <span className="civilization-fire" />
      <span className="civilization-banner banner-left" />
      <span className="civilization-banner banner-right" />
      <span className="battle-ground" />
      <span className="battle-dust dust-left" />
      <span className="battle-dust dust-right" />
      <span className="battle-front front-left" />
      <span className="battle-front front-right" />
      <span className="battle-impact impact-one" />
      <span className="battle-impact impact-two" />
      <span className="battle-tracer tracer-one" />
      <span className="battle-tracer tracer-two" />
      <span className="battle-squad squad-left" />
      <span className="battle-squad squad-left-rear" />
      <span className="battle-squad squad-right" />
      <span className="battle-squad squad-right-rear" />
      <span className="battle-smoke smoke-left" />
      <span className="battle-smoke smoke-right" />
      <span className="battle-barrage barrage-one" />
      <span className="battle-barrage barrage-two" />
      <span className="era-projectile projectile-one" />
      <span className="era-projectile projectile-two" />
      <span className="era-vehicle" />
      <span className="era-rocket" />
      <span className="era-planet" />
      <span className="era-ship ship-one" />
      <span className="era-ship ship-two" />
      <span className="fight-spark spark-one" />
      <span className="fight-spark spark-two" />
      <Stickman className="stickman-a" />
      <Stickman className="stickman-b" />
      <Stickman className="stickman-crowd crowd-one" />
      <Stickman className="stickman-crowd crowd-two" />
      <Stickman className="stickman-crowd crowd-three" />
      <Stickman className="stickman-crowd crowd-four" />
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

function usePreviewNow(active: boolean): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!active) {
      return;
    }
    setNow(Date.now());
    const timer = window.setInterval(() => setNow(Date.now()), 120);
    return () => window.clearInterval(timer);
  }, [active]);

  return now;
}

function turnProgressCampaign(turnID: string, elapsedMs: number): TurnProgressCampaign {
  let hash = 0;
  for (let index = 0; index < turnID.length; index++) {
    hash = (hash * 31 + turnID.charCodeAt(index)) >>> 0;
  }
  const campaignMs = elapsedMs % TURN_PROGRESS_CAMPAIGN_MS;
  const eraIndex = Math.floor(campaignMs / TURN_PROGRESS_ERA_MS);
  const eraElapsedMs = campaignMs % TURN_PROGRESS_ERA_MS;
  const nextEraIndex = (eraIndex + 1) % TURN_PROGRESS_ERAS.length;
  const rawTransitionProgress =
    eraElapsedMs < TURN_PROGRESS_ERA_MS - TURN_PROGRESS_TRANSITION_MS
      ? 0
      : Math.min(1, (eraElapsedMs - (TURN_PROGRESS_ERA_MS - TURN_PROGRESS_TRANSITION_MS)) / TURN_PROGRESS_TRANSITION_MS);
  const transitionProgress = rawTransitionProgress * rawTransitionProgress * (3 - 2 * rawTransitionProgress);
  return {
    currentEra: TURN_PROGRESS_ERAS[eraIndex] ?? TURN_PROGRESS_ERAS[0],
    nextEra: TURN_PROGRESS_ERAS[nextEraIndex] ?? TURN_PROGRESS_ERAS[0],
    currentLayer: eraIndex % 2 === 0 ? "a" : "b",
    transitionProgress,
    variant: hash % 3
  };
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
  const askRequest = visibleAskRequestForThread(state.askRequests, state.thread?.id);
  if (askRequest) {
    return {
      label: "等待用户选择",
      detail: `${askRequest.questions.length} 个问题需要响应`,
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

function agentFromRecord(record: JsonRecord | undefined): Agent | undefined {
  const id = stringValue(record, "id");
  const status = stringValue(record, "status");
  if (!id || !status) {
    return undefined;
  }
  return {
    id,
    type: stringValue(record, "type"),
    task_name: stringValue(record, "task_name"),
    agent_path: stringValue(record, "agent_path"),
    parent_id: stringValue(record, "parent_id"),
    description: stringValue(record, "description"),
    status,
    result: stringValue(record, "result"),
    error: stringValue(record, "error"),
    input_tokens: numberValue(record, "input_tokens"),
    output_tokens: numberValue(record, "output_tokens"),
    nested_count: numberValue(record, "nested_count"),
    nested_running_count: numberValue(record, "nested_running_count"),
    started_at: stringValue(record, "started_at"),
    completed_at: stringValue(record, "completed_at")
  };
}

function isDirectChildAgent(threadID: string, agent: Agent): boolean {
  if (agent.parent_id === threadID) {
    return true;
  }
  return agentPathDepth(agent.agent_path) === 2;
}

function agentPathDepth(path: string | undefined): number {
  const trimmed = path?.trim().replace(/^\/+|\/+$/g, "") ?? "";
  return trimmed ? trimmed.split("/").length : 0;
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

function parseTurnTimestampMs(value: string | null | undefined): number {
  if (!value) {
    return NaN;
  }
  return Date.parse(value);
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
  anchorRef: RefObject<HTMLElement>;
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

function Composer({
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
  containerRef?: RefObject<HTMLElement>;
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
  const menuPlacement: FloatingMenuPlacement = variant === "hero" ? "below" : "above";
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
            placeholder={readOnly ? "子任务会话只读" : images.length > 0 ? "添加描述" : "尽管问"}
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
            onKeyDown={(event) => {
              if (readOnly) {
                return;
              }
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
            {codexProvider && initialized ? (
              <CodexRuntimePicker
                variant={variant}
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

function CodexRuntimePicker({
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
  anchorRef: RefObject<HTMLDivElement>;
  running: boolean;
  onToggleMenu: (menu: Exclude<CodexRuntimeMenu, null>) => void;
  onSelectModel: (model: CodexModelSummary) => void;
  onSelectEffort: (effort: string) => void;
}): JSX.Element {
  const currentModel = state.models.find((model) => model.slug === initialized.model);
  const effort = initialized.effort ?? "";
  const effortOptions = codexEffortOptions(currentModel, effort);
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
        <span>{shortCodexModelLabel(initialized.model)}</span>
        <span className="codex-runtime-effort">{codexEffortLabel(effort)}</span>
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
          <CodexMainMenu
            selectedEffort={effort}
            options={effortOptions}
            currentModel={currentModel}
            fallbackModel={initialized.model}
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
          <CodexModelMenu
            state={state}
            selectedModel={initialized.model}
            onSelectModel={onSelectModel}
          />
        </FloatingMenuPortal>
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

function AnsweredAskUserMessage({ request }: { request: AnsweredAskRequestState }): JSX.Element {
  return (
    <article className={`ask-message ask-message-answered${request.cancelled ? " cancelled" : ""}`} aria-live="polite">
      <div className="ask-header">
        <div className="ask-title">
          <Check size={17} />
          <span>{request.cancelled ? "已取消回答" : "你已回答"}</span>
        </div>
      </div>
      <div className="ask-body ask-answer-body">
        {request.cancelled ? (
          <div className="ask-answer-empty">这次提问没有提交答案。</div>
        ) : (
          request.questions.map((question) => {
            const answer = request.answers[question.question]?.trim();
            if (!answer) {
              return null;
            }
            return (
              <section key={question.question} className="ask-question ask-answer-question">
                <div className="ask-question-meta">
                  <div className="ask-chip">{question.header}</div>
                </div>
                <h3>{question.question}</h3>
                <div className="ask-answer-text">{answer}</div>
              </section>
            );
          })
        )}
      </div>
    </article>
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
      initial[question.question] = [];
    }
    return initial;
  });
  const [otherAnswers, setOtherAnswers] = useState<Record<string, string>>({});
  const [submitting, setSubmitting] = useState(false);
  const flatAnswers = useMemo(() => {
    const output: Record<string, string> = {};
    for (const question of request.questions) {
      const selected = answers[question.question] ?? [];
      const other = otherAnswers[question.question]?.trim() ?? "";
      const values = selected.filter((label) => label !== ASK_USER_OTHER_VALUE);
      if (selected.includes(ASK_USER_OTHER_VALUE) && other) {
        values.push(other);
      }
      output[question.question] = values.join(", ");
    }
    return output;
  }, [answers, otherAnswers, request.questions]);
  const answeredCount = request.questions.filter((question) => flatAnswers[question.question]?.trim()).length;
  const allQuestionsAnswered = answeredCount === request.questions.length && request.questions.length > 0;

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

  function updateOtherAnswer(question: AskUserQuestion, value: string): void {
    setOtherAnswers((current) => ({ ...current, [question.question]: value }));
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
                      role={question.multi_select ? "checkbox" : "radio"}
                      aria-checked={selected}
                      disabled={submitting}
                      onClick={() => select(question, option.label)}
                    >
                      <span className="ask-option-check" aria-hidden="true">
                        {selected ? <Check size={15} /> : null}
                      </span>
                      <span className="ask-option-copy">
                        <strong>{option.label}</strong>
                        {option.description ? <span>{option.description}</span> : null}
                        {option.preview ? <span className="ask-option-preview">{option.preview}</span> : null}
                      </span>
                    </button>
                  );
                })}
                <div className={`ask-other ${selectedAnswers.includes(ASK_USER_OTHER_VALUE) ? "selected" : ""}`}>
                  <button
                    className="ask-other-toggle"
                    type="button"
                    role={question.multi_select ? "checkbox" : "radio"}
                    aria-checked={selectedAnswers.includes(ASK_USER_OTHER_VALUE)}
                    disabled={submitting}
                    onClick={() => select(question, ASK_USER_OTHER_VALUE)}
                  >
                    <span className="ask-option-check" aria-hidden="true">
                      {selectedAnswers.includes(ASK_USER_OTHER_VALUE) ? <Check size={15} /> : null}
                    </span>
                    <span className="ask-option-copy">
                      <strong>其他</strong>
                    </span>
                  </button>
                  {selectedAnswers.includes(ASK_USER_OTHER_VALUE) ? (
                    <textarea
                      className="ask-other-input"
                      value={otherAnswers[question.question] ?? ""}
                      placeholder="输入答案"
                      rows={3}
                      disabled={submitting}
                      onChange={(event) => updateOtherAnswer(question, event.currentTarget.value)}
                    />
                  ) : null}
                </div>
              </div>
            </section>
          );
        })}
      </div>
      <div className="ask-footer">
        <span>{request.questions.length > 1 ? `${answeredCount}/${request.questions.length} 个已回答` : allQuestionsAnswered ? "已选择" : "等待你的选择"}</span>
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
