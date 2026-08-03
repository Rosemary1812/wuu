import { Bot, ChevronDown, ChevronUp, ClipboardList, ImagePlus, MessageCircle, PanelLeftClose, PanelLeftOpen, Plus, Reply, Settings2, UserMinus, X } from "lucide-react";
import { type CSSProperties, type KeyboardEvent, type PointerEvent as ReactPointerEvent, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import type { ChannelAgentInsight, ChannelMessage, ChannelRoom, InitializeResult, NamedAgent } from "../shared/protocol";
import { AGENT_AVATAR_KEYS, AgentAvatarMark, randomAgentAvatarKey } from "./AgentAvatarMark";
import { AgentRelationshipGraph } from "./AgentRelationshipGraph";
import { AUTO_FOLLOW_BOTTOM_THRESHOLD_PX, useAutoFollowScrollContainer } from "./AutoFollowScroll";
import { ChannelComposer, type ChannelComposerHandle } from "./ChannelComposer";
import { ChannelGroupAvatar } from "./ChannelGroupAvatar";
import { ChannelMemberPicker } from "./ChannelMemberPicker";
import { buildComposerAttachments } from "./ComposerDraftState";
import { ComposerAttachmentStrip } from "./ComposerInputSections";
import { DefaultAvatarMark } from "./DefaultAvatar";
import { FieldError } from "./FieldError";
import {
  awaitComposerImages,
  inputFilesFromComposer,
  inputImagesFromComposer,
  type ComposerFile,
  type ComposerImage,
} from "./ComposerMessages";
import { useI18n } from "./i18n";
import { JumpToLatestPill } from "./JumpToLatestPill";
import { useLongTextCollapse } from "./LongTextCollapse";
import { SelectMenu, type SelectMenuGroup } from "./SelectMenu";
import { SidebarNameDialog } from "./SidebarNameDialog";
import { RichContent } from "./RichContent";
import { effortLabel, providerModelEffortOptions } from "./RuntimeHelpers";

type SetupPanel = "agent" | "room" | "task" | null;
type RoomMemberMode = "add" | "remove" | null;
export type ChannelSection = "rooms" | "agents" | "tasks";
type AgentActivityStatus = "idle" | "thinking" | "sending";

const CHANNEL_SPLIT_WIDTH_KEY = "wuu.channels.splitPaneWidth";
const LEGACY_CHANNEL_LIST_WIDTH_KEY = "wuu.channels.listWidth";
const CHANNEL_LIST_COLLAPSED_KEY = "wuu.channels.listCollapsed";
const CHANNEL_SPLIT_MIN_WIDTH = 156;
const CHANNEL_SPLIT_MAX_WIDTH = 360;
const CHANNEL_SPLIT_DEFAULT_WIDTH = 208;
const CHANNEL_SPLIT_COLLAPSED_WIDTH = 44;
const CHANNEL_SPLIT_WIDTH_STEP = 16;
const THREAD_PANEL_WIDTH_KEY = "wuu.channels.threadPanelWidth";
const THREAD_PANEL_MIN_WIDTH = 320;
const THREAD_PANEL_MAX_WIDTH = 720;
const THREAD_PANEL_DEFAULT_WIDTH = 420;
const THREAD_PANEL_WIDTH_STEP = 24;
const CHANNEL_MESSAGE_GROUP_WINDOW_MS = 3 * 60 * 1000;

const AGENT_AVATAR_SOURCE_MAX_BYTES = 10 * 1024 * 1024;
const AGENT_AVATAR_SIZE = 256;
const AGENT_AVATAR_TYPES = new Set(["image/png", "image/jpeg", "image/webp"]);

export function formatChannelUnreadCount(count: number): string {
  return count > 99 ? "99+" : String(Math.max(0, count));
}

function canGroupChannelMessages(previous: ChannelMessage | undefined, current: ChannelMessage): boolean {
  if (!previous || previous.author_type !== current.author_type || previous.author_id !== current.author_id) return false;
  const elapsed = Date.parse(current.created_at) - Date.parse(previous.created_at);
  return Number.isFinite(elapsed) && elapsed >= 0 && elapsed <= CHANNEL_MESSAGE_GROUP_WINDOW_MS;
}

async function squareAvatarImageFromFile(file: File): Promise<string> {
  if (!AGENT_AVATAR_TYPES.has(file.type) || file.size === 0 || file.size > AGENT_AVATAR_SOURCE_MAX_BYTES) throw new Error("invalid-avatar-image");
  const bitmap = await createImageBitmap(file);
  try {
    const canvas = document.createElement("canvas");
    canvas.width = AGENT_AVATAR_SIZE;
    canvas.height = AGENT_AVATAR_SIZE;
    const context = canvas.getContext("2d");
    if (!context) throw new Error("invalid-avatar-image");
    const scale = Math.max(AGENT_AVATAR_SIZE / bitmap.width, AGENT_AVATAR_SIZE / bitmap.height);
    const width = bitmap.width * scale;
    const height = bitmap.height * scale;
    context.drawImage(bitmap, (AGENT_AVATAR_SIZE - width) / 2, (AGENT_AVATAR_SIZE - height) / 2, width, height);
    return canvas.toDataURL("image/webp", 0.86);
  } finally {
    bitmap.close();
  }
}

function AgentAvatar({ name, avatarKey, avatarImage, status, statusText, compact = false }: {
  name: string;
  avatarKey: string;
  avatarImage?: string;
  status: AgentActivityStatus;
  statusText: string;
  compact?: boolean;
}): JSX.Element {
  return (
    <span className={`channel-agent-avatar${compact ? " compact" : ""}`} tabIndex={0} aria-label={`${name}: ${statusText}`}>
      <AgentAvatarMark avatarKey={avatarKey} avatarImage={avatarImage} />
      <span className={`channel-agent-status-dot ${status}`} aria-hidden="true" />
      <span className="channel-agent-status-card" role="tooltip">
        <span><i className={`channel-agent-status-swatch ${status}`} />{statusText}</span>
      </span>
    </span>
  );
}

function ChannelAuthorName({ name, mentionLabel, onMention }: {
  name: string;
  mentionLabel?: string;
  onMention?: () => void;
}): JSX.Element {
  if (!onMention) return <strong>{name}</strong>;
  return (
    <button className="channel-author-mention" type="button" aria-label={mentionLabel} onClick={onMention}>
      <span aria-hidden="true">@</span>
      {name}
    </button>
  );
}

const CHANNEL_THREAD_DIGEST_MAX_REPLIES = 3;

const CHANNEL_THREAD_PREVIEW_COMPONENTS = {
  a({ children }) { return <>{children}</>; },
  blockquote({ children }) { return <>{children} </>; },
  br() { return <> </>; },
  code({ children }) { return <>{children}</>; },
  del({ children }) { return <>{children}</>; },
  em({ children }) { return <>{children}</>; },
  h1({ children }) { return <>{children} </>; },
  h2({ children }) { return <>{children} </>; },
  h3({ children }) { return <>{children} </>; },
  h4({ children }) { return <>{children} </>; },
  h5({ children }) { return <>{children} </>; },
  h6({ children }) { return <>{children} </>; },
  hr() { return <> </>; },
  img({ alt }) { return <>{alt ?? ""}</>; },
  input() { return null; },
  li({ children }) { return <>{children} </>; },
  ol({ children }) { return <>{children}</>; },
  p({ children }) { return <>{children} </>; },
  pre({ children }) { return <>{children} </>; },
  strong({ children }) { return <>{children}</>; },
  table({ children }) { return <>{children} </>; },
  tbody({ children }) { return <>{children}</>; },
  td({ children }) { return <>{children} </>; },
  th({ children }) { return <>{children} </>; },
  thead({ children }) { return <>{children}</>; },
  tr({ children }) { return <>{children} </>; },
  ul({ children }) { return <>{children}</>; },
} satisfies Components;

function ChannelThreadMarkdownPreview({ text }: { text: string }): JSX.Element {
  return (
    <ReactMarkdown
      components={CHANNEL_THREAD_PREVIEW_COMPONENTS}
      remarkPlugins={[remarkGfm]}
    >
      {text}
    </ReactMarkdown>
  );
}

function ChannelThreadDigest({
  replies,
  agents,
  onOpen,
  onMention,
}: {
  replies: ChannelMessage[];
  agents: NamedAgent[];
  onOpen: () => void;
  onMention: (name: string) => void;
}): JSX.Element {
  const { formatDate, t } = useI18n();
  const visibleReplies = replies.slice(-CHANNEL_THREAD_DIGEST_MAX_REPLIES);

  return (
    <div className="channel-thread-digest">
      <button
        className="channel-thread-digest-open"
        type="button"
        aria-label={t("channels.replyCount", { count: replies.length })}
        onClick={onOpen}
      />
      {replies.length > CHANNEL_THREAD_DIGEST_MAX_REPLIES ? (
        <span className="channel-thread-digest-heading">
          <strong>{t("channels.replyCount", { count: replies.length })}</strong>
        </span>
      ) : null}
      <span className="channel-thread-digest-rows">
        {visibleReplies.map((reply) => {
          const own = reply.author_type === "human";
          const agent = own
            ? undefined
            : agents.find((candidate) => candidate.id === reply.author_id);
          const author = own ? t("channels.you") : (agent?.name ?? reply.author_id);
          const markdownPreview = reply.body.trim();
          const fallbackPreview = reply.files?.[0]?.filename
            || (reply.images?.length
              ? t("appState.images", { count: reply.images.length })
              : "");
          return (
            <span className="channel-thread-digest-row" key={reply.id}>
              <span className="channel-thread-digest-avatar" aria-hidden="true">
                {own ? (
                  <DefaultAvatarMark seed="local-user" />
                ) : (
                  <AgentAvatarMark
                    avatarKey={agent?.avatar_key ?? "abstract-1"}
                    avatarImage={agent?.avatar_image}
                  />
                )}
              </span>
              <ChannelAuthorName
                name={author}
                mentionLabel={!own ? t("channels.mentionAgent", { name: author }) : undefined}
                onMention={!own ? () => onMention(author) : undefined}
              />
              <span className="channel-thread-digest-preview">
                {markdownPreview ? (
                  <ChannelThreadMarkdownPreview text={markdownPreview} />
                ) : fallbackPreview}
              </span>
              <time dateTime={reply.created_at}>
                {formatDate(reply.created_at, { hour: "2-digit", minute: "2-digit" })}
              </time>
            </span>
          );
        })}
      </span>
    </div>
  );
}

function ChannelMessageBody({
  text,
  allowCollapse,
}: {
  text: string;
  allowCollapse: boolean;
}): JSX.Element {
  const { t } = useI18n();
  const { collapsible, expanded, toggleExpanded } = useLongTextCollapse(text);
  const canCollapse = allowCollapse && collapsible;

  return (
    <div className={`channel-message-bubble${canCollapse ? ` long-card ${expanded ? "expanded" : "collapsed"}` : ""}`}>
      <RichContent text={text} />
      {canCollapse ? (
        <button
          className="channel-message-expand-toggle"
          type="button"
          aria-expanded={expanded}
          onClick={toggleExpanded}
        >
          <span>{expanded ? t("common.collapse") : t("common.showMore")}</span>
          {expanded ? <ChevronUp aria-hidden="true" /> : <ChevronDown aria-hidden="true" />}
        </button>
      ) : null}
    </div>
  );
}

function clampChannelSplitWidth(width: number): number {
  return Math.min(CHANNEL_SPLIT_MAX_WIDTH, Math.max(CHANNEL_SPLIT_MIN_WIDTH, Math.round(width)));
}

function initialChannelSplitWidth(): number {
  const storedValue = window.localStorage.getItem(CHANNEL_SPLIT_WIDTH_KEY) ?? window.localStorage.getItem(LEGACY_CHANNEL_LIST_WIDTH_KEY);
  const stored = Number(storedValue);
  return storedValue !== null && Number.isFinite(stored) && stored >= CHANNEL_SPLIT_MIN_WIDTH
    ? clampChannelSplitWidth(stored)
    : CHANNEL_SPLIT_DEFAULT_WIDTH;
}

function initialChannelListCollapsed(): boolean {
  return window.localStorage.getItem(CHANNEL_LIST_COLLAPSED_KEY) === "true";
}

function clampThreadPanelWidth(width: number): number {
  return Math.min(THREAD_PANEL_MAX_WIDTH, Math.max(THREAD_PANEL_MIN_WIDTH, Math.round(width)));
}

function initialThreadPanelWidth(): number {
  const storedValue = window.localStorage.getItem(THREAD_PANEL_WIDTH_KEY);
  const stored = Number(storedValue);
  return storedValue !== null && Number.isFinite(stored) && stored >= THREAD_PANEL_MIN_WIDTH
    ? clampThreadPanelWidth(stored)
    : THREAD_PANEL_DEFAULT_WIDTH;
}

function taskStateKey(state?: string): "channels.taskState.open" | "channels.taskState.doing" | "channels.taskState.done" {
  if (state === "doing") return "channels.taskState.doing";
  if (state === "done") return "channels.taskState.done";
  return "channels.taskState.open";
}

export function ChannelView({ initialized, section = "rooms", onSectionChange, selectedRoomID: controlledRoomID, onSelectRoom, onRoomRead, composerDraft, onComposerDraftChange, newRoomRequest }: {
  initialized?: InitializeResult;
  section?: ChannelSection;
  onSectionChange?: (section: ChannelSection) => void;
  // Optional controlled room selection. App.tsx drives this so the unified
  // sidebar can select rooms; when absent the view manages selection
  // internally (tests and standalone usage).
  selectedRoomID?: string;
  onSelectRoom?: (roomID: string) => void;
  onRoomRead?: (roomID: string) => void;
  composerDraft?: {
    prompt: string;
    images: ComposerImage[];
    files: ComposerFile[];
  };
  onComposerDraftChange?: (draft: {
    prompt: string;
    images: ComposerImage[];
    files: ComposerFile[];
  }) => void;
  // Incremented by the parent (sidebar ＋ button) to request the new-room
  // dialog; the dialog itself stays inside this view.
  newRoomRequest?: number;
}): JSX.Element {
  const { formatDate, locale, t } = useI18n();
  const [agents, setAgents] = useState<NamedAgent[]>([]);
  const [agentInsights, setAgentInsights] = useState<Record<string, ChannelAgentInsight>>({});
  const [rooms, setRooms] = useState<ChannelRoom[]>([]);
  const [internalSelectedRoomID, setInternalSelectedRoomID] = useState("");
  const selectedRoomID = controlledRoomID ?? internalSelectedRoomID;
  const setSelectedRoomID = useCallback((value: string | ((current: string) => string)): void => {
    const base = controlledRoomID ?? internalSelectedRoomID;
    const next = typeof value === "function" ? value(base) : value;
    setInternalSelectedRoomID(next);
    if (next !== base) onSelectRoom?.(next);
  }, [controlledRoomID, internalSelectedRoomID, onSelectRoom]);
  const [messagesByRoomID, setMessagesByRoomID] = useState<Record<string, ChannelMessage[]>>({});
  const [loadedRoomIDs, setLoadedRoomIDs] = useState<Set<string>>(() => new Set());
  const messages = messagesByRoomID[selectedRoomID] ?? [];
  const [trackedTasks, setTrackedTasks] = useState<ChannelMessage[]>([]);
  const [setupPanel, setSetupPanel] = useState<SetupPanel>(null);
  const [splitWidth, setSplitWidth] = useState(initialChannelSplitWidth);
  const [listCollapsed, setListCollapsed] = useState(initialChannelListCollapsed);
  const [resizingSplit, setResizingSplit] = useState(false);
  const [threadWidth, setThreadWidth] = useState(initialThreadPanelWidth);
  const [resizingThread, setResizingThread] = useState(false);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [sending, setSending] = useState(false);
  const [sendingAgentIDs, setSendingAgentIDs] = useState<Set<string>>(() => new Set());
  const [body, setBody] = useState(composerDraft?.prompt ?? "");
  const [composerImages, setComposerImages] = useState<ComposerImage[]>(
    composerDraft?.images ?? [],
  );
  const [composerFiles, setComposerFiles] = useState<ComposerFile[]>(
    composerDraft?.files ?? [],
  );
  const [activeThreadRootID, setActiveThreadRootID] = useState("");
  const [threadReplyTargetID, setThreadReplyTargetID] = useState("");
  const [threadBody, setThreadBody] = useState("");
  const [threadSending, setThreadSending] = useState(false);
  const [threadComposerImages, setThreadComposerImages] = useState<ComposerImage[]>([]);
  const [threadComposerFiles, setThreadComposerFiles] = useState<ComposerFile[]>([]);

  const composerRoomIDRef = useRef(selectedRoomID);
  const skipComposerPublishRef = useRef(false);
  useLayoutEffect(() => {
    if (composerRoomIDRef.current === selectedRoomID) return;
    composerRoomIDRef.current = selectedRoomID;
    skipComposerPublishRef.current = true;
    setBody(composerDraft?.prompt ?? "");
    setComposerImages(composerDraft?.images ?? []);
    setComposerFiles(composerDraft?.files ?? []);
    setActiveThreadRootID("");
  }, [composerDraft, selectedRoomID]);

  useEffect(() => {
    if (!selectedRoomID) return;
    if (skipComposerPublishRef.current) {
      skipComposerPublishRef.current = false;
      return;
    }
    onComposerDraftChange?.({
      prompt: body,
      images: composerImages,
      files: composerFiles,
    });
  }, [body, composerFiles, composerImages, onComposerDraftChange, selectedRoomID]);
  const [agentName, setAgentName] = useState("");
  const [agentAvatarKey, setAgentAvatarKey] = useState<string>(() => randomAgentAvatarKey());
  const [agentAvatarImage, setAgentAvatarImage] = useState("");
  const [agentAvatarError, setAgentAvatarError] = useState("");
  const [agentModel, setAgentModel] = useState("");
  const [agentEffort, setAgentEffort] = useState("");
  const [editingAgentID, setEditingAgentID] = useState("");
  const [roomName, setRoomName] = useState("");
  const [roomAgentIDs, setRoomAgentIDs] = useState<string[]>([]);
  const [roomAvatarImage, setRoomAvatarImage] = useState("");
  const [editingRoomID, setEditingRoomID] = useState("");
  const [roomMemberMode, setRoomMemberMode] = useState<RoomMemberMode>(null);
  const [roomMemberSelectionIDs, setRoomMemberSelectionIDs] = useState<string[]>([]);
  const [updatingRoomMembers, setUpdatingRoomMembers] = useState(false);
  const [taskTitle, setTaskTitle] = useState("");
  const [taskRoomID, setTaskRoomID] = useState("");
  const [taskOwnerID, setTaskOwnerID] = useState("");
  const [composerFooterNode, setComposerFooterNode] = useState<HTMLDivElement | null>(null);
  const conversationRef = useRef<HTMLDivElement | null>(null);
  const composerFooterRef = useRef<HTMLDivElement | null>(null);
  const roomComposerRef = useRef<ChannelComposerHandle | null>(null);
  const threadComposerRef = useRef<ChannelComposerHandle | null>(null);
  const agentAvatarInputRef = useRef<HTMLInputElement | null>(null);
  const roomAvatarInputRef = useRef<HTMLInputElement | null>(null);
  const roomAvatarTargetRef = useRef<string>("");
  const savedRoomNameRef = useRef("");
  const messagesByRoomIDRef = useRef<Map<string, ChannelMessage[]>>(new Map());
  const markedMessageSeqByRoomRef = useRef<Map<string, number>>(new Map());
  const visibleRoomIDRef = useRef("");
  visibleRoomIDRef.current = section === "rooms" ? selectedRoomID : "";
  const sendingTimersRef = useRef<Map<string, number>>(new Map());
  const splitResizeStartRef = useRef({ x: 0, width: CHANNEL_SPLIT_DEFAULT_WIDTH });
  const threadResizeStartRef = useRef({ x: 0, width: THREAD_PANEL_DEFAULT_WIDTH });
  const messageScroll = useAutoFollowScrollContainer({
    open: section === "rooms" && Boolean(selectedRoomID),
    observeKey: selectedRoomID,
  });
  const setComposerFooter = useCallback((node: HTMLDivElement | null): void => {
    composerFooterRef.current = node;
    setComposerFooterNode(node);
  }, []);

  const updateSplitWidth = useCallback((width: number): void => {
    const nextWidth = clampChannelSplitWidth(width);
    setSplitWidth(nextWidth);
    window.localStorage.setItem(CHANNEL_SPLIT_WIDTH_KEY, String(nextWidth));
  }, []);

  const toggleListCollapsed = useCallback((): void => {
    setListCollapsed((collapsed) => {
      const nextCollapsed = !collapsed;
      window.localStorage.setItem(CHANNEL_LIST_COLLAPSED_KEY, String(nextCollapsed));
      return nextCollapsed;
    });
  }, []);

  const updateThreadWidth = useCallback((width: number): void => {
    const nextWidth = clampThreadPanelWidth(width);
    setThreadWidth(nextWidth);
    window.localStorage.setItem(THREAD_PANEL_WIDTH_KEY, String(nextWidth));
  }, []);

  const closeThread = useCallback((): void => {
    setActiveThreadRootID("");
    setThreadReplyTargetID("");
    setThreadBody("");
    setThreadComposerImages([]);
    setThreadComposerFiles([]);
    setResizingThread(false);
  }, []);

  useEffect(() => {
    if (!resizingSplit) return;
    const handlePointerMove = (event: PointerEvent): void => {
      updateSplitWidth(splitResizeStartRef.current.width + event.clientX - splitResizeStartRef.current.x);
    };
    const handlePointerUp = (): void => setResizingSplit(false);
    window.addEventListener("pointermove", handlePointerMove);
    window.addEventListener("pointerup", handlePointerUp, { once: true });
    return () => {
      window.removeEventListener("pointermove", handlePointerMove);
      window.removeEventListener("pointerup", handlePointerUp);
    };
  }, [resizingSplit, updateSplitWidth]);

  useEffect(() => {
    if (!resizingThread) return;
    const handlePointerMove = (event: PointerEvent): void => {
      const nextWidth = threadResizeStartRef.current.width - (event.clientX - threadResizeStartRef.current.x);
      if (nextWidth <= THREAD_PANEL_MIN_WIDTH) {
        closeThread();
        return;
      }
      updateThreadWidth(nextWidth);
    };
    const handlePointerUp = (): void => setResizingThread(false);
    window.addEventListener("pointermove", handlePointerMove);
    window.addEventListener("pointerup", handlePointerUp, { once: true });
    return () => {
      window.removeEventListener("pointermove", handlePointerMove);
      window.removeEventListener("pointerup", handlePointerUp);
    };
  }, [closeThread, resizingThread, updateThreadWidth]);

  useEffect(() => {
    if (newRoomRequest) openNewRoom();
  }, [newRoomRequest]);

  useEffect(() => {
    setActiveThreadRootID("");
    setThreadReplyTargetID("");
    setThreadBody("");
    setThreadComposerImages([]);
    setThreadComposerFiles([]);
  }, [selectedRoomID]);

  function startSplitResize(event: ReactPointerEvent<HTMLButtonElement>): void {
    event.preventDefault();
    splitResizeStartRef.current = { x: event.clientX, width: splitWidth };
    setResizingSplit(true);
  }

  function handleSplitResizeKeyDown(event: KeyboardEvent<HTMLButtonElement>): void {
    if (event.key === "ArrowLeft") {
      event.preventDefault();
      updateSplitWidth(splitWidth - CHANNEL_SPLIT_WIDTH_STEP);
    } else if (event.key === "ArrowRight") {
      event.preventDefault();
      updateSplitWidth(splitWidth + CHANNEL_SPLIT_WIDTH_STEP);
    } else if (event.key === "Home") {
      event.preventDefault();
      updateSplitWidth(CHANNEL_SPLIT_MIN_WIDTH);
    } else if (event.key === "End") {
      event.preventDefault();
      updateSplitWidth(CHANNEL_SPLIT_MAX_WIDTH);
    }
  }

  function startThreadResize(event: ReactPointerEvent<HTMLButtonElement>): void {
    event.preventDefault();
    threadResizeStartRef.current = { x: event.clientX, width: threadWidth };
    setResizingThread(true);
  }

  function handleThreadResizeKeyDown(event: KeyboardEvent<HTMLButtonElement>): void {
    if (event.key === "ArrowLeft") {
      event.preventDefault();
      updateThreadWidth(threadWidth + THREAD_PANEL_WIDTH_STEP);
    } else if (event.key === "ArrowRight") {
      event.preventDefault();
      if (threadWidth - THREAD_PANEL_WIDTH_STEP <= THREAD_PANEL_MIN_WIDTH) closeThread();
      else updateThreadWidth(threadWidth - THREAD_PANEL_WIDTH_STEP);
    } else if (event.key === "Home") {
      event.preventDefault();
      closeThread();
    } else if (event.key === "End") {
      event.preventDefault();
      updateThreadWidth(THREAD_PANEL_MAX_WIDTH);
    }
  }

  const selectedRoom = useMemo(
    () => rooms.find((room) => room.id === selectedRoomID),
    [rooms, selectedRoomID],
  );
  const roomIncludesCurrentUser = selectedRoom?.members.some(
    (member) => member.member_type === "human" && member.member_id === "local-user",
  ) ?? false;
  const agentNames = useMemo(
    () => new Map(agents.map((agent) => [agent.id, agent.name])),
    [agents],
  );
  const selectedRoomAgents = useMemo(() => {
    const memberIDs = new Set(
      selectedRoom?.members
        .filter((member) => member.member_type === "agent")
        .map((member) => member.member_id) ?? [],
    );
    return agents.filter((agent) => memberIDs.has(agent.id));
  }, [agents, selectedRoom]);
  const taskRoom = useMemo(
    () => rooms.find((room) => room.id === (taskRoomID || selectedRoomID)),
    [rooms, selectedRoomID, taskRoomID],
  );
  const taskOwnerAgents = useMemo(() => {
    const memberIDs = new Set(
      taskRoom?.members
        .filter((member) => member.member_type === "agent")
        .map((member) => member.member_id) ?? [],
    );
    return agents.filter((agent) => memberIDs.has(agent.id));
  }, [agents, taskRoom]);
  useEffect(() => {
    if (setupPanel !== "task") return;
    setTaskOwnerID((current) => (
      taskOwnerAgents.some((agent) => agent.id === current)
        ? current
        : (taskOwnerAgents[0]?.id ?? "")
    ));
  }, [setupPanel, taskOwnerAgents]);
  const messageByID = useMemo(
    () => new Map(messages.map((message) => [message.id, message])),
    [messages],
  );
  const rootMessages = useMemo(
    () => messages.filter((message) => message.kind !== "task" && !message.thread_id),
    [messages],
  );
  const repliesByThread = useMemo(() => {
    const replies = new Map<string, ChannelMessage[]>();
    for (const message of messages) {
      if (!message.thread_id || message.kind === "task") continue;
      const current = replies.get(message.thread_id) ?? [];
      current.push(message);
      replies.set(message.thread_id, current);
    }
    return replies;
  }, [messages]);
  const activeThreadRoot = activeThreadRootID ? messageByID.get(activeThreadRootID) : undefined;
  const activeThreadMessages = useMemo(
    () => activeThreadRootID ? (repliesByThread.get(activeThreadRootID) ?? []) : [],
    [activeThreadRootID, repliesByThread],
  );
  const threadConversationMessages = useMemo(
    () => activeThreadRoot ? [activeThreadRoot, ...activeThreadMessages] : [],
    [activeThreadMessages, activeThreadRoot],
  );
  const threadScroll = useAutoFollowScrollContainer({
    open: section === "rooms" && Boolean(activeThreadRootID),
    observeKey: `${activeThreadRootID}:${activeThreadMessages.length}`,
  });
  const activityFor = useCallback((agent?: NamedAgent): AgentActivityStatus => {
    if (!agent) return "idle";
    if (sendingAgentIDs.has(agent.id)) return "sending";
    return agent.activity_status === "thinking" ? "thinking" : "idle";
  }, [sendingAgentIDs]);
  const activityText = useCallback(
    (status: AgentActivityStatus): string => t(`channels.agentStatus.${status}`),
    [t],
  );
  const respondingAgents = useMemo(() => {
    const roomAgentIDs = new Set(
      selectedRoom?.members
        .filter((member) => member.member_type === "agent")
        .map((member) => member.member_id) ?? [],
    );
    return agents
      .filter((agent) => roomAgentIDs.has(agent.id))
      .map((agent) => ({ agent, status: activityFor(agent) }))
      .filter(({ agent, status }) => status === "sending"
        || (status === "thinking" && agent.activity_room_ids?.includes(selectedRoomID)));
  }, [activityFor, agents, selectedRoom, selectedRoomID]);
  const respondingAgentNames = useMemo(
    () => new Intl.ListFormat(locale, { style: "short", type: "conjunction" })
      .format(respondingAgents.map(({ agent }) => agent.name)),
    [locale, respondingAgents],
  );
  const modelGroups = useMemo<SelectMenuGroup[]>(() => {
    const inherited = initialized ? `${initialized.provider} · ${initialized.model}` : undefined;
    const groups: SelectMenuGroup[] = [{ options: [{ value: "", label: t("channels.inheritModel"), hint: inherited }] }];
    for (const provider of initialized?.providers ?? []) {
      const models = provider.models?.length ? provider.models : [{ id: provider.model, display_name: provider.model }];
      groups.push({
        label: provider.name,
        options: models.filter((model) => model.id).map((model) => ({
          value: `${provider.name}\u0000${model.id}`,
          label: model.display_name || model.id,
          hint: model.id,
        })),
      });
    }
    return groups;
  }, [initialized, t]);
  const [agentProviderName, agentModelID] = agentModel.split("\u0000");
  const agentProvider = initialized?.providers?.find((provider) => provider.name === agentProviderName);
  const agentEffortOptions = providerModelEffortOptions(agentProvider, agentModelID ?? "", agentEffort);

  function selectAgentModel(value: string): void {
    setAgentModel(value);
    setAgentEffort("");
  }

  const refreshRoomsAndAgents = useCallback(async (): Promise<void> => {
    if (!window.wuu) return;
    const result = await window.wuu.bootstrapChannels();
    setAgents(result.agents ?? []);
    setRooms(result.rooms ?? []);
    setSelectedRoomID((current) =>
      current && result.rooms.some((room) => room.id === current)
        ? current
        : (result.rooms[0]?.id ?? ""),
    );
  }, []);

  const markRoomRead = useCallback((roomID: string, latestMessageSeq: number): void => {
    if (!roomID || !window.wuu) return;
    const markedSeq = markedMessageSeqByRoomRef.current.get(roomID) ?? 0;
    if (latestMessageSeq <= markedSeq) return;
    markedMessageSeqByRoomRef.current.set(roomID, latestMessageSeq);
    setRooms((current) => current.map((room) => room.id === roomID ? { ...room, unread_count: 0 } : room));
    onRoomRead?.(roomID);
    void window.wuu.markChannelRoomRead({ room_id: roomID }).catch((reason: unknown) => {
      if (markedMessageSeqByRoomRef.current.get(roomID) === latestMessageSeq) {
        markedMessageSeqByRoomRef.current.delete(roomID);
      }
      setError(String(reason));
    });
  }, [onRoomRead]);

  const refreshMessages = useCallback(async (roomID: string): Promise<void> => {
    if (!window.wuu || !roomID) return;
    const result = await window.wuu.listChannelMessages({ room_id: roomID, limit: 500 });
    const nextMessages = result.messages ?? [];
    const previousMessages = messagesByRoomIDRef.current.get(roomID) ?? [];
    messagesByRoomIDRef.current.set(roomID, nextMessages);
    setMessagesByRoomID((current) => ({ ...current, [roomID]: nextMessages }));
    setLoadedRoomIDs((current) => {
      if (current.has(roomID)) return current;
      return new Set(current).add(roomID);
    });
    if (visibleRoomIDRef.current !== roomID) return;
    const known = new Set(previousMessages.map((message) => message.id));
    if (known.size > 0) {
      for (const message of nextMessages) {
        if (message.author_type !== "agent" || known.has(message.id)) continue;
        const agentID = message.author_id;
        const previousTimer = sendingTimersRef.current.get(agentID);
        if (previousTimer) window.clearTimeout(previousTimer);
        setSendingAgentIDs((current) => new Set(current).add(agentID));
        const timer = window.setTimeout(() => {
          setSendingAgentIDs((current) => {
            const next = new Set(current);
            next.delete(agentID);
            return next;
          });
          sendingTimersRef.current.delete(agentID);
        }, 1_800);
        sendingTimersRef.current.set(agentID, timer);
      }
    }
    const latestMessageSeq = nextMessages.reduce(
      (latest, message) => Math.max(latest, message.seq),
      0,
    );
    if (latestMessageSeq > 0) markRoomRead(roomID, latestMessageSeq);
  }, [markRoomRead]);

  const refreshTrackedTasks = useCallback(async (): Promise<void> => {
    if (!window.wuu || rooms.length === 0) {
      setTrackedTasks([]);
      return;
    }
    const results = await Promise.all(
      rooms.map((room) => window.wuu!.listChannelMessages({ room_id: room.id, limit: 500 })),
    );
    setTrackedTasks(
      results
        .flatMap((result) => result.messages ?? [])
        .filter((message) => message.kind === "task")
        .sort((left, right) => right.created_at.localeCompare(left.created_at)),
    );
  }, [rooms]);

  useEffect(() => {
    let active = true;
    void refreshRoomsAndAgents()
      .catch((reason: unknown) => {
        if (active) setError(String(reason));
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [refreshRoomsAndAgents]);

  useEffect(() => {
    if (!window.wuu) return;
    let active = true;
    const refresh = (): void => {
      void Promise.all([
        window.wuu!.listNamedAgents(),
        window.wuu!.listChannelRooms(),
      ]).then(([agentResult, roomResult]) => {
        if (!active) return;
        setAgents(agentResult.agents ?? []);
        setRooms(roomResult.rooms ?? []);
      }).catch((reason: unknown) => {
        if (active) setError(String(reason));
      });
    };
    refresh();
    const timer = window.setInterval(refresh, 1_000);
    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, []);

  useEffect(() => {
    if (!window.wuu || typeof window.wuu.getNamedAgentInsights !== "function" || section !== "agents") return;
    let active = true;
    const refresh = (): void => {
      void window.wuu!.getNamedAgentInsights().then((result) => {
        if (!active) return;
        setAgentInsights(Object.fromEntries((result.insights ?? []).map((insight) => [insight.agent_id, insight])));
      }).catch(() => {
        // Activity statistics are an enhancement; the relationship graph and
        // agent management remain usable if historical aggregation fails.
      });
    };
    refresh();
    const timer = window.setInterval(refresh, 60_000);
    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, [section]);

  useEffect(() => {
    if (section !== "rooms" || !selectedRoomID) return;
    let active = true;
    const refresh = (): void => {
      void refreshMessages(selectedRoomID).catch((reason: unknown) => {
        if (active) setError(String(reason));
      });
    };
    refresh();
    const timer = window.setInterval(refresh, 2_000);
    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, [refreshMessages, section, selectedRoomID]);

  useEffect(() => () => {
    for (const timer of sendingTimersRef.current.values()) window.clearTimeout(timer);
  }, []);

  useEffect(() => {
    if (section !== "tasks") return;
    let active = true;
    const refresh = (): void => {
      void refreshTrackedTasks().catch((reason: unknown) => {
        if (active) setError(String(reason));
      });
    };
    refresh();
    const timer = window.setInterval(refresh, 2_000);
    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, [refreshTrackedTasks, section]);

  useEffect(() => {
    messageScroll.scrollToBottom();
  }, [messageScroll, messages.length, messages.at(-1)?.id]);

  useLayoutEffect(() => {
    const conversation = conversationRef.current;
    const footer = composerFooterRef.current;
    if (!conversation || !footer) return undefined;

    const updateComposerHeight = (): void => {
      const height = Math.ceil(footer.getBoundingClientRect().height);
      if (height > 0) {
        conversation.style.setProperty("--channel-composer-height", `${height}px`);
      } else {
        conversation.style.removeProperty("--channel-composer-height");
      }
      messageScroll.scheduleScrollToBottom();
    };

    updateComposerHeight();
    if (typeof ResizeObserver === "undefined") return undefined;
    const observer = new ResizeObserver(updateComposerHeight);
    observer.observe(footer);
    return () => observer.disconnect();
  }, [messageScroll]);

  async function submitAgent(): Promise<void> {
    if (!window.wuu || !agentName.trim()) return;
    setError("");
    try {
      const [providerOverride, modelOverride] = agentModel.split("\u0000");
      const params = {
        name: agentName.trim(),
        avatar_key: agentAvatarKey,
        avatar_image: agentAvatarImage,
        provider_override: providerOverride || undefined,
        model_override: modelOverride || undefined,
        effort_override: modelOverride && agentEffort ? agentEffort : undefined,
      };
      if (editingAgentID) await window.wuu.updateNamedAgent({ agent_id: editingAgentID, ...params });
      else await window.wuu.createNamedAgent(params);
      closeAgentPanel();
      await refreshRoomsAndAgents();
    } catch (reason) {
      setError(String(reason));
    }
  }

  async function submitRoom(): Promise<void> {
    const name = roomName.trim();
    if (!window.wuu || !name) return;
    setError("");
    try {
      if (editingRoomID) {
        const roomID = editingRoomID;
        const result = await window.wuu.updateChannelRoom({
          room_id: roomID,
          name,
          agent_ids: roomAgentIDs,
        });
        if (result.room.id === roomID) {
          setRooms((current) => current.map((room) => room.id === result.room.id ? result.room : room));
        }
        await refreshRoomsAndAgents();
        setSelectedRoomID(roomID);
        closeRoomPanel();
        return;
      }
      const result = await window.wuu.createChannelRoom({
        name,
        avatar_image: roomAvatarImage || undefined,
        agent_ids: roomAgentIDs,
      });
      setRoomName("");
      setRoomAgentIDs([]);
      setRoomAvatarImage("");
      setSetupPanel(null);
      await refreshRoomsAndAgents();
      setSelectedRoomID(result.room.id);
    } catch (reason) {
      setError(String(reason));
    }
  }

  async function sendMessage(): Promise<void> {
    const messageBody = body.trim();
    if (!window.wuu || !selectedRoomID || (!messageBody && composerImages.length === 0 && composerFiles.length === 0) || sending) return;
    setSending(true);
    setError("");
    try {
      const resolvedImages = await awaitComposerImages(composerImages);
      await window.wuu.sendChannelMessage({
        room_id: selectedRoomID,
        body: messageBody,
        images: inputImagesFromComposer(resolvedImages),
        files: inputFilesFromComposer(composerFiles),
      });
      setBody("");
      setComposerImages([]);
      setComposerFiles([]);
      await refreshMessages(selectedRoomID);
    } catch (reason) {
      setError(String(reason));
    } finally {
      setSending(false);
    }
  }

  function openThread(message: ChannelMessage, replyTargetID = message.id): void {
    setActiveThreadRootID(message.thread_id ?? message.id);
    setThreadReplyTargetID(replyTargetID);
    setThreadBody("");
    setThreadComposerImages([]);
    setThreadComposerFiles([]);
  }

  async function sendThreadReply(): Promise<void> {
    const messageBody = threadBody.trim();
    if (!window.wuu || !selectedRoomID || !activeThreadRootID || (!messageBody && threadComposerImages.length === 0 && threadComposerFiles.length === 0) || threadSending) return;
    setThreadSending(true);
    setError("");
    try {
      const resolvedImages = await awaitComposerImages(threadComposerImages);
      await window.wuu.sendChannelMessage({
        room_id: selectedRoomID,
        thread_id: activeThreadRootID,
        reply_to: threadReplyTargetID || activeThreadRootID,
        body: messageBody,
        images: inputImagesFromComposer(resolvedImages),
        files: inputFilesFromComposer(threadComposerFiles),
      });
      setThreadBody("");
      setThreadComposerImages([]);
      setThreadComposerFiles([]);
      setThreadReplyTargetID(activeThreadRootID);
      await refreshMessages(selectedRoomID);
    } catch (reason) {
      setError(String(reason));
    } finally {
      setThreadSending(false);
    }
  }

  async function attachMessageFiles(files: File[]): Promise<void> {
    try {
      await buildComposerAttachments(
        files,
        (placeholder) => setComposerImages((current) => [...current, placeholder]),
        (encoded) => setComposerImages((current) => current.map((image) => image.id === encoded.id ? encoded : image)),
        (file) => setComposerFiles((current) => [...current, file]),
      );
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason));
    }
  }

  async function attachThreadMessageFiles(files: File[]): Promise<void> {
    try {
      await buildComposerAttachments(
        files,
        (placeholder) => setThreadComposerImages((current) => [...current, placeholder]),
        (encoded) => setThreadComposerImages((current) => current.map((image) => image.id === encoded.id ? encoded : image)),
        (file) => setThreadComposerFiles((current) => [...current, file]),
      );
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason));
    }
  }

  async function submitTask(): Promise<void> {
    const title = taskTitle.trim();
    const roomID = taskRoomID || selectedRoomID;
    if (!window.wuu || !roomID || !title || !taskOwnerID) return;
    setError("");
    try {
      await window.wuu.createChannelTask({
        room_id: roomID,
        title,
        owner_id: taskOwnerID,
      });
      setTaskTitle("");
      setTaskRoomID("");
      setTaskOwnerID("");
      setSetupPanel(null);
      await refreshMessages(selectedRoomID);
      if (section === "tasks") await refreshTrackedTasks();
    } catch (reason) {
      setError(String(reason));
    }
  }

  async function deleteAgent(agentID: string): Promise<void> {
    if (!window.wuu) return;
    if (!window.confirm(t("channels.deleteAgentConfirm", { name: agentName.trim() }))) return;
    setError("");
    try {
      await window.wuu.deleteNamedAgent({ agent_id: agentID });
      closeAgentPanel();
      await refreshRoomsAndAgents();
    } catch (reason) {
      setError(String(reason));
    }
  }

  function editAgent(agent: NamedAgent): void {
    setEditingAgentID(agent.id);
    setAgentName(agent.name);
    setAgentAvatarKey(agent.avatar_key);
    setAgentAvatarImage(agent.avatar_image ?? "");
    setAgentAvatarError("");
    setAgentModel(agent.provider_override && agent.model_override ? `${agent.provider_override}\u0000${agent.model_override}` : "");
    setAgentEffort(agent.effort_override ?? "");
    setSetupPanel("agent");
  }

  function closeAgentPanel(): void {
    setSetupPanel(null);
    setEditingAgentID("");
    setAgentName("");
    setAgentAvatarKey(randomAgentAvatarKey());
    setAgentAvatarImage("");
    setAgentAvatarError("");
    setAgentModel("");
    setAgentEffort("");
  }

  function toggleRoomAgent(agentID: string): void {
    setRoomAgentIDs((current) =>
      current.includes(agentID)
        ? current.filter((candidate) => candidate !== agentID)
        : [...current, agentID],
    );
  }

  function toggleRoomMemberSelection(agentID: string): void {
    setRoomMemberSelectionIDs((current) =>
      current.includes(agentID)
        ? current.filter((candidate) => candidate !== agentID)
        : [...current, agentID],
    );
  }

  function openRoomMemberMode(mode: Exclude<RoomMemberMode, null>): void {
    setRoomMemberSelectionIDs([]);
    setRoomMemberMode(mode);
  }

  function closeRoomMemberMode(): void {
    setRoomMemberMode(null);
    setRoomMemberSelectionIDs([]);
  }

  function openNewRoom(): void {
    setEditingRoomID("");
    setRoomName("");
    setRoomAgentIDs([]);
    setRoomAvatarImage("");
    closeRoomMemberMode();
    setSetupPanel("room");
  }

  function editRoom(room: ChannelRoom): void {
    setEditingRoomID(room.id);
    setRoomName(room.name);
    setRoomAgentIDs(room.members
      .filter((member) => member.member_type === "agent")
      .map((member) => member.member_id));
    setRoomAvatarImage(room.avatar_image ?? "");
    savedRoomNameRef.current = room.name;
    closeRoomMemberMode();
    setSetupPanel("room");
  }

  function closeRoomPanel(): void {
    if (editingRoomID) void persistRoomName();
    setSetupPanel(null);
    setEditingRoomID("");
    setRoomName("");
    setRoomAgentIDs([]);
    setRoomAvatarImage("");
    closeRoomMemberMode();
  }

  async function persistRoomName(): Promise<void> {
    const name = roomName.trim();
    if (!window.wuu || !editingRoomID || !name || name === savedRoomNameRef.current) return;
    const previousName = savedRoomNameRef.current;
    savedRoomNameRef.current = name;
    setError("");
    try {
      const result = await window.wuu.updateChannelRoom({
        room_id: editingRoomID,
        name,
        agent_ids: roomAgentIDs,
      });
      setRooms((current) => current.map((candidate) => candidate.id === editingRoomID ? result.room : candidate));
      setRoomName(result.room.name);
      savedRoomNameRef.current = result.room.name;
    } catch (reason) {
      savedRoomNameRef.current = previousName;
      setError(String(reason));
    }
  }

  async function submitRoomMemberChange(): Promise<void> {
    if (!window.wuu || !editingRoomID || !roomMemberMode || roomMemberSelectionIDs.length === 0 || updatingRoomMembers) return;
    const room = rooms.find((candidate) => candidate.id === editingRoomID);
    if (!room) return;
    if (roomMemberMode === "remove" && !window.confirm(t("channels.removeMembersConfirm", { count: roomMemberSelectionIDs.length }))) return;
    const selected = new Set(roomMemberSelectionIDs);
    const nextAgentIDs = roomMemberMode === "add"
      ? [...roomAgentIDs, ...roomMemberSelectionIDs.filter((agentID) => !roomAgentIDs.includes(agentID))]
      : roomAgentIDs.filter((agentID) => !selected.has(agentID));
    setUpdatingRoomMembers(true);
    setError("");
    try {
      const result = await window.wuu.updateChannelRoom({
        room_id: editingRoomID,
        name: roomName.trim() || room.name,
        agent_ids: nextAgentIDs,
      });
      setRooms((current) => current.map((candidate) => candidate.id === editingRoomID ? result.room : candidate));
      setRoomAgentIDs(nextAgentIDs);
      closeRoomMemberMode();
      await refreshRoomsAndAgents();
    } catch (reason) {
      setError(String(reason));
    } finally {
      setUpdatingRoomMembers(false);
    }
  }

  async function deleteRoom(): Promise<void> {
    if (!window.wuu || !editingRoomID) return;
    if (!window.confirm(t("channels.deleteRoomConfirm", { name: roomName.trim() }))) return;
    setError("");
    try {
      const roomID = editingRoomID;
      await window.wuu.deleteChannelRoom({ room_id: roomID });
      closeRoomPanel();
      messagesByRoomIDRef.current.delete(roomID);
      setMessagesByRoomID((current) => {
        const next = { ...current };
        delete next[roomID];
        return next;
      });
      setLoadedRoomIDs((current) => {
        const next = new Set(current);
        next.delete(roomID);
        return next;
      });
      if (selectedRoomID === roomID) {
        setActiveThreadRootID("");
      }
      await refreshRoomsAndAgents();
    } catch (reason) {
      setError(String(reason));
    }
  }

  function chooseRoomAvatar(roomID: string): void {
    roomAvatarTargetRef.current = roomID;
    roomAvatarInputRef.current?.click();
  }

  async function updateRoomAvatarFromFile(file: File): Promise<void> {
    if (!window.wuu) return;
    setError("");
    try {
      const avatarImage = await squareAvatarImageFromFile(file);
      const roomID = roomAvatarTargetRef.current;
      if (!roomID) {
        setRoomAvatarImage(avatarImage);
        return;
      }
      const result = await window.wuu.updateChannelRoom({ room_id: roomID, avatar_image: avatarImage });
      setRooms((current) => current.map((room) => room.id === result.room.id ? result.room : room));
      if (editingRoomID === roomID) setRoomAvatarImage(result.room.avatar_image ?? "");
    } catch {
      setError(t("channels.invalidAvatarImage"));
    }
  }

  return (
    <section
      className={`channel-view channel-mode-${section}${listCollapsed && section === "agents" ? " channel-list-collapsed" : ""}${resizingSplit ? " resizing-channel-split" : ""}`}
      aria-label={t("channels.title")}
      style={section === "agents" ? { gridTemplateColumns: `${listCollapsed ? CHANNEL_SPLIT_COLLAPSED_WIDTH : splitWidth}px minmax(0, 1fr)` } : undefined}
    >
      {section === "rooms" ? <div
        ref={conversationRef}
        className={`channel-conversation${activeThreadRoot ? " thread-open" : ""}${resizingThread ? " resizing-thread-split" : ""}`}
        style={{ "--channel-thread-width": `${threadWidth}px` } as CSSProperties}
      >
        <div className="channel-room-main">
          {selectedRoom ? (
            <header className="channel-room-header">
              <div className="channel-room-header-title">
                <h2>{selectedRoom.name}</h2>
              </div>
              {respondingAgents.length > 0 ? (
                <div
                  className="channel-response-status"
                  role="status"
                  aria-live="polite"
                  aria-label={respondingAgents.map(({ agent, status }) => `${agent.name}: ${activityText(status)}`).join(", ")}
                >
                  <span className="channel-response-status-avatars" aria-hidden="true">
                    {respondingAgents.slice(0, 3).map(({ agent, status }) => (
                      <span className="channel-response-status-avatar" key={agent.id}>
                        <AgentAvatarMark avatarKey={agent.avatar_key} avatarImage={agent.avatar_image} />
                        <i className={`channel-response-status-dot ${status}`} />
                      </span>
                    ))}
                  </span>
                  <span className="channel-response-status-copy">
                    <strong>{respondingAgentNames}</strong>
                    <span>
                      {respondingAgents.length === 1
                        ? activityText(respondingAgents[0].status)
                        : t("channels.agentsResponding", { count: respondingAgents.length })}
                    </span>
                  </span>
                </div>
              ) : null}
              <div className="channel-room-header-actions">
                <button
                  className="icon-button"
                  type="button"
                  aria-label={t("channels.manageRoom", { name: selectedRoom.name })}
                  aria-haspopup="dialog"
                  onClick={() => editRoom(selectedRoom)}
                >
                  <Settings2 className="icon" />
                </button>
              </div>
            </header>
          ) : null}
          {!loading && rooms.length === 0 ? (
            <div className="channel-room-main-empty">
              <button className="channel-empty-action" type="button" onClick={openNewRoom}>
                {t("channels.newRoom")}
              </button>
            </div>
          ) : null}
          <input
            ref={roomAvatarInputRef}
            className="channel-avatar-file-input"
            type="file"
            accept="image/png,image/jpeg,image/webp"
            onChange={(event) => {
              const input = event.currentTarget;
              const file = input.files?.[0];
              if (!file) return;
              void updateRoomAvatarFromFile(file).finally(() => { input.value = ""; });
            }}
          />
          {error ? <div className="channel-error" role="alert">{error}</div> : null}
        <div ref={messageScroll.scrollRef} className="channel-message-stream" role="log" aria-live="polite">
          {rootMessages.map((message, index) => {
            const own = message.author_type === "human";
            const author = own ? t("channels.you") : (agentNames.get(message.author_id) ?? message.author_id);
            const threadReplies = repliesByThread.get(message.id) ?? [];
            const previousMessage = rootMessages[index - 1];
            const previousThreadReplies = previousMessage ? (repliesByThread.get(previousMessage.id) ?? []) : [];
            const grouped = Boolean(
              canGroupChannelMessages(previousMessage, message)
              && previousThreadReplies.length === 0,
            );
            return (
              <article className={`channel-message ${own ? "own" : "agent"}${grouped ? " grouped" : ""}`} key={message.id}>
                {!grouped && own ? (
                  <span className="channel-human-avatar" aria-hidden="true">
                    <DefaultAvatarMark seed="local-user" />
                  </span>
                ) : !grouped ? (() => {
                  const agent = agents.find((candidate) => candidate.id === message.author_id);
                  const status = activityFor(agent);
                  return <AgentAvatar name={author} avatarKey={agent?.avatar_key ?? "abstract-1"} avatarImage={agent?.avatar_image} status={status} statusText={activityText(status)} />;
                })() : null}
                <div className={`channel-message-content${threadReplies.length ? " has-thread-digest" : ""}`}>
                  <div className="channel-message-meta">
                    {!grouped ? (
                      <ChannelAuthorName
                        name={author}
                        mentionLabel={!own ? t("channels.mentionAgent", { name: author }) : undefined}
                        onMention={!own ? () => roomComposerRef.current?.insertMention(author) : undefined}
                      />
                    ) : null}
                    {!grouped ? <time dateTime={message.created_at}>
                      {formatDate(message.created_at, { hour: "2-digit", minute: "2-digit" })}
                    </time> : null}
                    <div className="channel-message-actions">
                      <button type="button" onClick={() => openThread(message)}>
                        <Reply aria-hidden="true" />
                        {t("channels.reply")}
                      </button>
                    </div>
                  </div>
                  {message.body ? (
                    <ChannelMessageBody text={message.body} allowCollapse={!own} />
                  ) : null}
                  {message.images?.length || message.files?.length ? (
                    <ComposerAttachmentStrip
                      images={(message.images ?? []).map((image, index) => ({ id: `${message.id}-image-${index}`, ...image }))}
                      files={(message.files ?? []).map((file, index) => ({ id: `${message.id}-file-${index}`, ...file }))}
                      removable={false}
                    />
                  ) : null}
                  {threadReplies.length ? (
                    <ChannelThreadDigest
                      replies={threadReplies}
                      agents={agents}
                      onOpen={() => openThread(message)}
                      onMention={(name) => roomComposerRef.current?.insertMention(name)}
                    />
                  ) : null}
                </div>
              </article>
            );
          })}
          {!loading && loadedRoomIDs.has(selectedRoomID) && selectedRoom && rootMessages.length === 0 ? (
            <div className="channel-stream-empty">{t("channels.empty")}</div>
          ) : null}
        </div>
        <JumpToLatestPill
          containerRef={messageScroll.scrollRef}
          bottomAnchor={composerFooterNode}
          threshold={AUTO_FOLLOW_BOTTOM_THRESHOLD_PX}
        />
        {selectedRoom ? (
          <div ref={setComposerFooter} className="channel-conversation-footer">
            <ChannelComposer
              ref={roomComposerRef}
              draft={body}
              placeholder={t("channels.messagePlaceholder")}
              disabled={false}
              sending={sending}
              files={composerFiles}
              images={composerImages}
              mentionAgents={selectedRoomAgents}
              onChangeDraft={setBody}
              onPasteAttachmentFiles={(files) => void attachMessageFiles(files)}
              onRemoveFile={(id) => setComposerFiles((current) => current.filter((file) => file.id !== id))}
              onRemoveImage={(id) => setComposerImages((current) => current.filter((image) => image.id !== id))}
              onSend={() => void sendMessage()}
            />
          </div>
        ) : null}
        </div>
        {activeThreadRoot ? (
          <aside className="channel-thread-panel" aria-label={t("channels.thread")}>
            <button
              className="channel-thread-resizer"
              type="button"
              role="separator"
              aria-label={t("channels.resizeThread")}
              aria-orientation="vertical"
              aria-valuemin={0}
              aria-valuemax={THREAD_PANEL_MAX_WIDTH}
              aria-valuenow={threadWidth}
              onPointerDown={startThreadResize}
              onKeyDown={handleThreadResizeKeyDown}
            />
            <div ref={threadScroll.scrollRef} className="channel-thread-messages">
              {threadConversationMessages.map((message, index) => {
                const own = message.author_type === "human";
                const author = own ? t("channels.you") : (agentNames.get(message.author_id) ?? message.author_id);
                const repliedMessage = message.reply_to ? messageByID.get(message.reply_to) : undefined;
                const previousMessage = threadConversationMessages[index - 1];
                const grouped = Boolean(
                  canGroupChannelMessages(previousMessage, message)
                  && previousMessage.reply_to === message.reply_to,
                );
                return (
                  <article className={`channel-message channel-thread-message ${own ? "own" : "agent"}${grouped ? " grouped" : ""}`} key={message.id}>
                    {!grouped && own ? (
                      <span className="channel-human-avatar" aria-hidden="true">
                        <DefaultAvatarMark seed="local-user" />
                      </span>
                    ) : !grouped ? (() => {
                      const agent = agents.find((candidate) => candidate.id === message.author_id);
                      const status = activityFor(agent);
                      return <AgentAvatar name={author} avatarKey={agent?.avatar_key ?? "abstract-1"} avatarImage={agent?.avatar_image} status={status} statusText={activityText(status)} />;
                    })() : null}
                    <div className="channel-message-content">
                      <div className="channel-message-meta">
                        {!grouped ? (
                          <ChannelAuthorName
                            name={author}
                            mentionLabel={!own ? t("channels.mentionAgent", { name: author }) : undefined}
                            onMention={!own ? () => threadComposerRef.current?.insertMention(author) : undefined}
                          />
                        ) : null}
                        {!grouped ? <time dateTime={message.created_at}>
                          {formatDate(message.created_at, { hour: "2-digit", minute: "2-digit" })}
                        </time> : null}
                        <div className="channel-message-actions">
                          <button type="button" onClick={() => setThreadReplyTargetID(message.id)}>
                            <Reply aria-hidden="true" />
                            {t("channels.reply")}
                          </button>
                        </div>
                      </div>
                      {repliedMessage && repliedMessage.id !== activeThreadRoot.id ? (
                        <button className="channel-thread-reference" type="button" onClick={() => setThreadReplyTargetID(repliedMessage.id)}>
                          {t("channels.replyingTo", {
                            name: repliedMessage.author_type === "human"
                              ? t("channels.you")
                              : (agentNames.get(repliedMessage.author_id) ?? repliedMessage.author_id),
                          })}
                          <span>{repliedMessage.body}</span>
                        </button>
                      ) : null}
                      {message.body ? <ChannelMessageBody text={message.body} allowCollapse={!own} /> : null}
                      {message.images?.length || message.files?.length ? (
                        <ComposerAttachmentStrip
                          images={(message.images ?? []).map((image, index) => ({ id: `${message.id}-thread-image-${index}`, ...image }))}
                          files={(message.files ?? []).map((file, index) => ({ id: `${message.id}-thread-file-${index}`, ...file }))}
                          removable={false}
                        />
                      ) : null}
                    </div>
                  </article>
                );
              })}
            </div>
            <div className="channel-thread-footer">
              {threadReplyTargetID && threadReplyTargetID !== activeThreadRoot.id ? (
                <div className="channel-thread-replying">
                  <span>{t("channels.replyingTo", {
                    name: messageByID.get(threadReplyTargetID)?.author_type === "human"
                      ? t("channels.you")
                      : (agentNames.get(messageByID.get(threadReplyTargetID)?.author_id ?? "") ?? messageByID.get(threadReplyTargetID)?.author_id ?? ""),
                  })}</span>
                  <button type="button" onClick={() => setThreadReplyTargetID(activeThreadRoot.id)} aria-label={t("channels.cancelReply")}><X aria-hidden="true" /></button>
                </div>
              ) : null}
              <ChannelComposer
                ref={threadComposerRef}
                draft={threadBody}
                placeholder={t("channels.threadPlaceholder")}
                hideExpandButton
                disabled={false}
                sending={threadSending}
                files={threadComposerFiles}
                images={threadComposerImages}
                mentionAgents={selectedRoomAgents}
                onChangeDraft={setThreadBody}
                onPasteAttachmentFiles={(files) => void attachThreadMessageFiles(files)}
                onRemoveFile={(id) => setThreadComposerFiles((current) => current.filter((file) => file.id !== id))}
                onRemoveImage={(id) => setThreadComposerImages((current) => current.filter((image) => image.id !== id))}
                onSend={() => void sendThreadReply()}
              />
            </div>
          </aside>
        ) : null}
      </div> : section === "agents" ? (
        <div className="channel-agent-workspace">
          <aside className={`channel-list-pane channel-agent-directory${listCollapsed ? " collapsed" : ""}`}>
            <div className="channel-pane-heading">
              {!listCollapsed ? <span>{t("channels.agents")}<small className="channel-pane-count">{agents.length}</small></span> : null}
              <div className="channel-heading-actions">
                <button className="icon-button channel-list-collapse-toggle" type="button" aria-label={t(listCollapsed ? "channels.expandList" : "channels.collapseList")} aria-expanded={!listCollapsed} onClick={toggleListCollapsed}>
                  {listCollapsed ? <PanelLeftOpen className="icon" /> : <PanelLeftClose className="icon" />}
                </button>
                {!listCollapsed ? <button
                  className="icon-button"
                  type="button"
                  aria-label={t("channels.newAgent")}
                  onClick={() => {
                    setEditingAgentID("");
                    setAgentName("");
                    setAgentAvatarKey(randomAgentAvatarKey());
                    setAgentAvatarImage("");
                    setAgentAvatarError("");
                    setAgentModel("");
                    setAgentEffort("");
                    setSetupPanel("agent");
                  }}
                >
                  <Plus className="icon" />
                </button> : null}
              </div>
            </div>
            {!listCollapsed && error ? <div className="channel-error" role="alert">{error}</div> : null}
            {!listCollapsed ? <div className="channel-agent-directory-list channel-directory-list">
            {agents.map((agent) => {
              const status = activityFor(agent);
              const roomCount = rooms.filter((room) => room.members.some((member) => member.member_type === "agent" && member.member_id === agent.id)).length;
              const model = agent.model_override || t("channels.inheritModel");
              return (
                <div className="channel-directory-row channel-agent-directory-row" key={agent.id}>
                  <button className="channel-directory-avatar" type="button" aria-label={t("channels.editAgent")} onClick={() => editAgent(agent)}>
                    <AgentAvatar name={agent.name} avatarKey={agent.avatar_key} avatarImage={agent.avatar_image} status={status} statusText={activityText(status)} />
                  </button>
                  <button className="channel-directory-identity channel-agent-directory-identity" type="button" onClick={() => editAgent(agent)}>
                    <span><strong>{agent.name}</strong><small>{model} · {t("channels.agentRoomCount", { count: roomCount })}</small></span>
                  </button>
                  <button className="icon-button channel-directory-settings" type="button" aria-label={t("channels.editAgent")} onClick={() => editAgent(agent)}><Settings2 className="icon" /></button>
                </div>
              );
            })}
            {!loading && agents.length === 0 ? <div className="channel-management-empty">{t("channels.newAgent")}</div> : null}
            </div> : null}
            {!listCollapsed ? <button
              className="channel-split-resizer"
              type="button"
              role="separator"
              aria-label={t("channels.resizeList")}
              aria-orientation="vertical"
              aria-valuemin={CHANNEL_SPLIT_MIN_WIDTH}
              aria-valuemax={CHANNEL_SPLIT_MAX_WIDTH}
              aria-valuenow={splitWidth}
              onPointerDown={startSplitResize}
              onKeyDown={handleSplitResizeKeyDown}
            /> : null}
          </aside>
          <div className="channel-agent-graph-pane">
            <AgentRelationshipGraph
              agents={agents}
              rooms={rooms}
              insights={agentInsights}
              inheritedProvider={initialized?.provider}
              inheritedModel={initialized?.model}
              onSelectAgent={editAgent}
              ariaLabel={t("channels.relationshipGraph")}
              zoomInLabel={t("channels.zoomIn")}
              zoomOutLabel={t("channels.zoomOut")}
              resetViewLabel={t("channels.resetGraphView")}
            />
          </div>
        </div>
      ) : (
        <div className="channel-management-view channel-tasks-view">
          <div className="channel-management-heading">
            <div>
              <strong>{t("channels.tasks")}</strong>
              <span>{trackedTasks.length}</span>
            </div>
            <button
              className="channel-management-primary"
              type="button"
              disabled={!selectedRoom || selectedRoomAgents.length === 0}
              onClick={() => {
                setTaskRoomID(selectedRoomID || rooms[0]?.id || "");
                setTaskOwnerID(selectedRoomAgents[0]?.id ?? "");
                setSetupPanel("task");
              }}
            >
              <Plus className="icon" />
              {t("channels.newTask")}
            </button>
          </div>
          {error ? <div className="channel-error" role="alert">{error}</div> : null}
          <div className="channel-task-board channel-task-table">
            {(["open", "doing", "done"] as const).map((state) => {
              const tasks = trackedTasks.filter((task) => (task.task_state ?? "open") === state);
              return (
                <section className={`channel-task-column channel-task-column-${state}`} key={state}>
                  <header className="channel-task-column-heading">
                    <strong>{t(taskStateKey(state))}</strong>
                    <span>{tasks.length}</span>
                  </header>
                  <div className="channel-task-column-items">
                    {tasks.map((task) => {
                      const room = rooms.find((candidate) => candidate.id === task.room_id);
                      const owner = agentNames.get(task.task_owner ?? "") ?? task.task_owner ?? "";
                      return (
                        <button
                          className={`channel-task-card${state === "done" ? " done" : ""}`}
                          type="button"
                          key={task.id}
                          data-tooltip={`${room ? `# ${room.name}` : task.room_id} · ${owner}`}
                          onClick={() => {
                            setSelectedRoomID(task.room_id);
                            onSectionChange?.("rooms");
                          }}
                        >
                          <strong>{task.body}</strong>
                          <span className="channel-task-card-meta">{room ? `# ${room.name}` : task.room_id} · {owner}</span>
                        </button>
                      );
                    })}
                    {tasks.length === 0 ? <span className="channel-task-column-empty">—</span> : null}
                  </div>
                </section>
              );
            })}
          </div>
        </div>
      )}

      <SidebarNameDialog
        open={setupPanel === "agent"}
        title={agentName}
        onTitleChange={setAgentName}
        onSubmit={() => void submitAgent()}
        onClose={closeAgentPanel}
        dialogTitle={editingAgentID ? t("channels.editAgent") : t("channels.newAgent")}
        dialogTitleId="channel-agent-dialog-title"
        fieldLabel={t("channels.name")}
        fieldAriaLabel={t("channels.name")}
        placeholder="Andy"
        icon={Bot}
        submitLabel={editingAgentID ? t("channels.save") : t("channels.create")}
        cancelLabel={t("channels.cancel")}
        submitDisabled={!agentName.trim()}
        destructiveAction={editingAgentID ? { label: t("channels.deleteAgent"), onClick: () => void deleteAgent(editingAgentID) } : undefined}
        content={<div className="channel-setup-form">
          {error ? <div className="channel-error" role="alert">{error}</div> : null}
          <label className="sidebar-name-dialog-field">
            <span className="sidebar-name-dialog-label">{t("channels.name")}</span>
            <input className="sidebar-name-dialog-input" value={agentName} onChange={(event) => setAgentName(event.currentTarget.value)} autoFocus />
          </label>
          <fieldset className="channel-avatar-picker">
            <legend>{t("channels.avatar")}</legend>
            <div>
              {AGENT_AVATAR_KEYS.map((avatarKey, index) => (
                <button
                  className={agentAvatarKey === avatarKey ? "active" : ""}
                  type="button"
                  key={avatarKey}
                  aria-label={t("channels.chooseAvatar", { index: index + 1 })}
                  aria-pressed={agentAvatarKey === avatarKey}
                  onClick={() => { setAgentAvatarKey(avatarKey); setAgentAvatarImage(""); setAgentAvatarError(""); }}
                >
                  <AgentAvatarMark avatarKey={avatarKey} />
                </button>
              ))}
              <button
                className={`channel-custom-avatar${agentAvatarImage ? " active" : ""}`}
                type="button"
                aria-label={t("channels.customAvatar")}
                aria-pressed={Boolean(agentAvatarImage)}
                aria-invalid={Boolean(agentAvatarError)}
                aria-describedby={agentAvatarError ? "channel-agent-avatar-error" : undefined}
                onClick={() => agentAvatarInputRef.current?.click()}
              >
                {agentAvatarImage
                  ? <AgentAvatarMark avatarKey={agentAvatarKey} avatarImage={agentAvatarImage} />
                  : <ImagePlus className="icon" />}
              </button>
              <input
                ref={agentAvatarInputRef}
                className="channel-avatar-file-input"
                type="file"
                accept="image/png,image/jpeg,image/webp"
                onChange={(event) => {
                  const input = event.currentTarget;
                  const file = input.files?.[0];
                  if (!file) return;
                  setAgentAvatarError("");
                  void squareAvatarImageFromFile(file)
                    .then(setAgentAvatarImage)
                    .catch(() => setAgentAvatarError(t("channels.invalidAvatarImage")))
                    .finally(() => { input.value = ""; });
                }}
              />
            </div>
            <FieldError id="channel-agent-avatar-error">{agentAvatarError}</FieldError>
          </fieldset>
          <label className="sidebar-name-dialog-field">
            <span className="sidebar-name-dialog-label">{t("channels.model")}</span>
            <SelectMenu value={agentModel} onChange={selectAgentModel} groups={modelGroups} ariaLabel={t("channels.model")} flip />
          </label>
          {agentModel && agentEffortOptions.length > 1 ? (
            <label className="sidebar-name-dialog-field">
              <span className="sidebar-name-dialog-label">{t("channels.effort")}</span>
              <SelectMenu
                value={agentEffort}
                onChange={setAgentEffort}
                options={agentEffortOptions.map((effort) => ({ value: effort, label: effortLabel(effort) }))}
                ariaLabel={t("channels.effort")}
                flip
              />
            </label>
          ) : null}
        </div>}
      />
      <SidebarNameDialog
        open={setupPanel === "room"}
        title={roomName}
        onTitleChange={setRoomName}
        onSubmit={() => void submitRoom()}
        onClose={closeRoomPanel}
        dialogTitle={t(editingRoomID ? "channels.roomDetails" : "channels.newRoom")}
        dialogTitleId="channel-room-dialog-title"
        fieldLabel={t("channels.name")}
        fieldAriaLabel={t("channels.name")}
        placeholder={t("channels.newRoom")}
        icon={MessageCircle}
        submitLabel={t(editingRoomID ? "channels.save" : "channels.create")}
        cancelLabel={t("channels.cancel")}
        submitDisabled={!roomName.trim()}
        variant={editingRoomID ? "drawer" : "default"}
        hideActions={Boolean(editingRoomID)}
        closeOnEscape={!roomMemberMode}
        backgrounded={Boolean(roomMemberMode)}
        content={editingRoomID ? (
          <div className="channel-room-details-form">
            {selectedRoom ? (
              <section className="channel-room-identity" aria-label={t("channels.groupOverview")}>
                <span className="channel-room-identity-avatar">
                  <ChannelGroupAvatar room={selectedRoom} agents={agents} />
                </span>
                <span className="channel-room-identity-copy">
                  <strong>{roomName}</strong>
                  <span>{t("channels.agentCount", { count: roomAgentIDs.length })}</span>
                </span>
              </section>
            ) : null}
            <section className="channel-room-members-section" aria-labelledby="channel-room-members-title">
              <header className="channel-room-members-header">
                <div>
                  <h3 id="channel-room-members-title">{t("channels.groupMembers")}</h3>
                  <span>{t("channels.memberCount", { count: roomAgentIDs.length + (roomIncludesCurrentUser ? 1 : 0) })}</span>
                </div>
              </header>
              <div className="channel-room-member-grid">
                {roomIncludesCurrentUser ? (
                  <div className="channel-room-member-card current" aria-label={t("channels.you")}>
                    <span className="channel-room-member-avatar">
                      <DefaultAvatarMark seed="local-user" />
                    </span>
                    <span className="channel-room-member-name">{t("channels.you")}</span>
                  </div>
                ) : null}
                {agents.filter((agent) => roomAgentIDs.includes(agent.id)).map((agent) => (
                  <div
                    className="channel-room-member-card"
                    key={agent.id}
                    aria-label={agent.name}
                  >
                    <span className="channel-room-member-avatar">
                      <AgentAvatarMark avatarKey={agent.avatar_key} avatarImage={agent.avatar_image} />
                    </span>
                    <span className="channel-room-member-name">{agent.name}</span>
                  </div>
                ))}
                <button
                  className="channel-room-member-action"
                  type="button"
                  onClick={() => openRoomMemberMode("add")}
                  disabled={agents.every((agent) => roomAgentIDs.includes(agent.id))}
                >
                  <span className="channel-room-member-action-icon" aria-hidden="true"><Plus className="icon" /></span>
                  <span>{t("channels.addMember")}</span>
                </button>
                <button
                  className="channel-room-member-action"
                  type="button"
                  onClick={() => openRoomMemberMode("remove")}
                  disabled={roomAgentIDs.length === 0}
                >
                  <span className="channel-room-member-action-icon" aria-hidden="true"><UserMinus className="icon" /></span>
                  <span>{t("channels.manageMembers")}</span>
                </button>
              </div>
            </section>
            <section className="channel-room-settings-section" aria-labelledby="channel-room-settings-title">
              <h3 id="channel-room-settings-title">{t("channels.groupSettings")}</h3>
              <label className="sidebar-name-dialog-field">
                <span className="sidebar-name-dialog-label">{t("channels.name")}</span>
                <input
                  className="sidebar-name-dialog-input"
                  value={roomName}
                  onChange={(event) => setRoomName(event.currentTarget.value)}
                  onBlur={() => {
                    if (!roomName.trim()) {
                      setRoomName(savedRoomNameRef.current);
                      return;
                    }
                    void persistRoomName();
                  }}
                />
              </label>
            </section>
            <section className="channel-room-danger-zone" aria-labelledby="channel-room-danger-title">
              <div>
                <h3 id="channel-room-danger-title">{t("channels.dangerZone")}</h3>
                <p>{t("channels.deleteRoomHint")}</p>
              </div>
              <button type="button" onClick={() => void deleteRoom()}>{t("channels.deleteRoom")}</button>
            </section>
          </div>
        ) : (
          <div className="channel-setup-form">
            <label className="sidebar-name-dialog-field"><span className="sidebar-name-dialog-label">{t("channels.name")}</span><input className="sidebar-name-dialog-input" value={roomName} onChange={(event) => setRoomName(event.currentTarget.value)} autoFocus /></label>
            <fieldset className="channel-room-avatar-picker">
              <legend>{t("channels.groupAvatar")}</legend>
              <button
                className="channel-room-avatar-preview"
                type="button"
                aria-label={t("channels.customGroupAvatar")}
                onClick={() => chooseRoomAvatar("")}
              >
                <span className="channel-room-avatar-media">
                  <ChannelGroupAvatar
                    room={{
                      id: editingRoomID || "new-room",
                      kind: "channel",
                      name: roomName,
                      avatar_image: roomAvatarImage || undefined,
                      created_by: "local-user",
                      created_at: "",
                      members: [
                        { room_id: editingRoomID || "new-room", member_type: "human", member_id: "local-user", joined_at: "" },
                        ...roomAgentIDs.map((agentID) => ({ room_id: editingRoomID || "new-room", member_type: "agent" as const, member_id: agentID, joined_at: "" })),
                      ],
                    }}
                    agents={agents}
                  />
                  <span className="channel-room-avatar-badge" aria-hidden="true"><ImagePlus className="icon" /></span>
                </span>
                <span className="channel-room-avatar-label">{t("channels.customGroupAvatar")}</span>
              </button>
            </fieldset>
            <ChannelMemberPicker agents={agents} selectedAgentIDs={roomAgentIDs} onToggle={toggleRoomAgent} />
          </div>
        )}
      />
      <SidebarNameDialog
        open={Boolean(editingRoomID && roomMemberMode)}
        title=""
        onTitleChange={() => undefined}
        onSubmit={() => void submitRoomMemberChange()}
        onClose={closeRoomMemberMode}
        dialogTitle={t(roomMemberMode === "remove" ? "channels.removeAgents" : "channels.addAgents")}
        dialogTitleId="channel-room-member-dialog-title"
        fieldLabel={t("channels.groupMembers")}
        fieldAriaLabel={t("channels.groupMembers")}
        placeholder=""
        icon={roomMemberMode === "remove" ? UserMinus : Plus}
        submitLabel={t(roomMemberMode === "remove" ? "channels.removeSelectedMembers" : "channels.addSelectedMembers", { count: roomMemberSelectionIDs.length })}
        cancelLabel={t("channels.cancel")}
        submitDisabled={roomMemberSelectionIDs.length === 0 || updatingRoomMembers}
        dialogClassName="channel-room-member-dialog"
        content={roomMemberMode ? (
          <div className="channel-room-member-flow">
            <p>{t(roomMemberMode === "add" ? "channels.addAgentsHint" : "channels.removeAgentsHint")}</p>
            <ChannelMemberPicker
              agents={roomMemberMode === "add"
                ? agents.filter((agent) => !roomAgentIDs.includes(agent.id))
                : agents.filter((agent) => roomAgentIDs.includes(agent.id))}
              selectedAgentIDs={roomMemberSelectionIDs}
              onToggle={toggleRoomMemberSelection}
              label={t(roomMemberMode === "add" ? "channels.availableAgents" : "channels.currentAgents")}
            />
          </div>
        ) : null}
      />
      <SidebarNameDialog
        open={setupPanel === "task"}
        title={taskTitle}
        onTitleChange={setTaskTitle}
        onSubmit={() => void submitTask()}
        onClose={() => { setSetupPanel(null); setTaskRoomID(""); }}
        dialogTitle={t("channels.newTask")}
        dialogTitleId="channel-task-dialog-title"
        fieldLabel={t("channels.taskTitle")}
        fieldAriaLabel={t("channels.taskTitle")}
        placeholder={t("channels.taskTitle")}
        icon={ClipboardList}
        submitLabel={t("channels.create")}
        cancelLabel={t("channels.cancel")}
        submitDisabled={!taskTitle.trim() || !(taskRoomID || selectedRoomID) || !taskOwnerID}
        content={<div className="channel-setup-form">
          <label className="sidebar-name-dialog-field"><span className="sidebar-name-dialog-label">{t("channels.taskTitle")}</span><input className="sidebar-name-dialog-input" value={taskTitle} onChange={(event) => setTaskTitle(event.currentTarget.value)} autoFocus /></label>
          <label className="sidebar-name-dialog-field"><span className="sidebar-name-dialog-label">{t("channels.taskRoomLabel")}</span><SelectMenu value={taskRoomID || selectedRoomID} onChange={setTaskRoomID} options={rooms.map((room) => ({ value: room.id, label: `# ${room.name}` }))} ariaLabel={t("channels.taskRoomLabel")} flip /></label>
          <label className="sidebar-name-dialog-field"><span className="sidebar-name-dialog-label">{t("channels.taskOwnerLabel")}</span><SelectMenu value={taskOwnerID} onChange={setTaskOwnerID} options={taskOwnerAgents.map((agent) => ({ value: agent.id, label: agent.name }))} ariaLabel={t("channels.taskOwnerLabel")} flip /></label>
        </div>}
      />
    </section>
  );
}
