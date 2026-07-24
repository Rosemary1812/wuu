import { Bell, BellOff, Bot, CheckCircle2, ClipboardList, Hash, ImagePlus, Pencil, Plus, Trash2 } from "lucide-react";
import { type KeyboardEvent, type PointerEvent as ReactPointerEvent, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { ChannelMessage, ChannelRoom, InitializeResult, NamedAgent } from "../shared/protocol";
import { AGENT_AVATAR_KEYS, AgentAvatarMark, randomAgentAvatarKey } from "./AgentAvatarMark";
import { AgentRelationshipGraph } from "./AgentRelationshipGraph";
import { useAutoFollowScrollContainer } from "./AutoFollowScroll";
import { ChannelComposer } from "./ChannelComposer";
import { channelSystemNotificationsEnabled, setChannelSystemNotificationsEnabled } from "./ChannelPreferences";
import { buildComposerAttachments } from "./ComposerDraftState";
import { ComposerAttachmentStrip } from "./ComposerInputSections";
import {
  awaitComposerImages,
  inputFilesFromComposer,
  inputImagesFromComposer,
  type ComposerFile,
  type ComposerImage,
} from "./ComposerMessages";
import { useI18n } from "./i18n";
import { SelectMenu, type SelectMenuGroup } from "./SelectMenu";
import { SidebarNameDialog } from "./SidebarNameDialog";
import { RichContent } from "./RichContent";

type SetupPanel = "agent" | "room" | "task" | null;
export type ChannelSection = "rooms" | "agents" | "tasks";
type AgentActivityStatus = "idle" | "thinking" | "sending";

const CHANNEL_SPLIT_WIDTH_KEY = "wuu.channels.splitPaneWidth";
const LEGACY_CHANNEL_LIST_WIDTH_KEY = "wuu.channels.listWidth";
const CHANNEL_SPLIT_MIN_WIDTH = 156;
const CHANNEL_SPLIT_MAX_WIDTH = 360;
const CHANNEL_SPLIT_DEFAULT_WIDTH = 208;
const CHANNEL_SPLIT_WIDTH_STEP = 16;

const AGENT_AVATAR_SOURCE_MAX_BYTES = 10 * 1024 * 1024;
const AGENT_AVATAR_SIZE = 256;
const AGENT_AVATAR_TYPES = new Set(["image/png", "image/jpeg", "image/webp"]);

async function agentAvatarImageFromFile(file: File): Promise<string> {
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
        <strong>{name}</strong>
        <span><i className={`channel-agent-status-swatch ${status}`} />{statusText}</span>
      </span>
    </span>
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

function taskStateKey(state?: string): "channels.taskState.open" | "channels.taskState.doing" | "channels.taskState.done" {
  if (state === "doing") return "channels.taskState.doing";
  if (state === "done") return "channels.taskState.done";
  return "channels.taskState.open";
}

export function ChannelView({ initialized, section = "rooms", onSectionChange }: {
  initialized?: InitializeResult;
  section?: ChannelSection;
  onSectionChange?: (section: ChannelSection) => void;
}): JSX.Element {
  const { t } = useI18n();
  const [agents, setAgents] = useState<NamedAgent[]>([]);
  const [rooms, setRooms] = useState<ChannelRoom[]>([]);
  const [selectedRoomID, setSelectedRoomID] = useState("");
  const [messages, setMessages] = useState<ChannelMessage[]>([]);
  const [trackedTasks, setTrackedTasks] = useState<ChannelMessage[]>([]);
  const [setupPanel, setSetupPanel] = useState<SetupPanel>(null);
  const [splitWidth, setSplitWidth] = useState(initialChannelSplitWidth);
  const [resizingSplit, setResizingSplit] = useState(false);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [sending, setSending] = useState(false);
  const [sendingAgentIDs, setSendingAgentIDs] = useState<Set<string>>(() => new Set());
  const [body, setBody] = useState("");
  const [composerImages, setComposerImages] = useState<ComposerImage[]>([]);
  const [composerFiles, setComposerFiles] = useState<ComposerFile[]>([]);
  const [agentName, setAgentName] = useState("");
  const [agentAvatarKey, setAgentAvatarKey] = useState<string>(() => randomAgentAvatarKey());
  const [agentAvatarImage, setAgentAvatarImage] = useState("");
  const [agentModel, setAgentModel] = useState("");
  const [editingAgentID, setEditingAgentID] = useState("");
  const [roomName, setRoomName] = useState("");
  const [roomAgentIDs, setRoomAgentIDs] = useState<string[]>([]);
  const [systemNotifications, setSystemNotifications] = useState(channelSystemNotificationsEnabled);
  const [taskTitle, setTaskTitle] = useState("");
  const [taskRoomID, setTaskRoomID] = useState("");
  const [taskOwnerID, setTaskOwnerID] = useState("");
  const [updatingTaskID, setUpdatingTaskID] = useState("");
  const conversationRef = useRef<HTMLDivElement | null>(null);
  const composerFooterRef = useRef<HTMLDivElement | null>(null);
  const agentAvatarInputRef = useRef<HTMLInputElement | null>(null);
  const knownMessageIDsRef = useRef<Set<string>>(new Set());
  const sendingTimersRef = useRef<Map<string, number>>(new Map());
  const splitResizeStartRef = useRef({ x: 0, width: CHANNEL_SPLIT_DEFAULT_WIDTH });
  const messageScroll = useAutoFollowScrollContainer({
    open: section === "rooms" && Boolean(selectedRoomID),
    observeKey: selectedRoomID,
  });

  const updateSplitWidth = useCallback((width: number): void => {
    const nextWidth = clampChannelSplitWidth(width);
    setSplitWidth(nextWidth);
    window.localStorage.setItem(CHANNEL_SPLIT_WIDTH_KEY, String(nextWidth));
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

  const selectedRoom = useMemo(
    () => rooms.find((room) => room.id === selectedRoomID),
    [rooms, selectedRoomID],
  );
  const agentNames = useMemo(
    () => new Map(agents.map((agent) => [agent.id, agent.name])),
    [agents],
  );
  const activityFor = useCallback((agent?: NamedAgent): AgentActivityStatus => {
    if (!agent) return "idle";
    if (sendingAgentIDs.has(agent.id)) return "sending";
    return agent.activity_status === "thinking" ? "thinking" : "idle";
  }, [sendingAgentIDs]);
  const activityText = useCallback(
    (status: AgentActivityStatus): string => t(`channels.agentStatus.${status}`),
    [t],
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

  const refreshMessages = useCallback(async (roomID: string): Promise<void> => {
    if (!window.wuu || !roomID) {
      setMessages([]);
      return;
    }
    const result = await window.wuu.listChannelMessages({ room_id: roomID, limit: 500 });
    const nextMessages = result.messages ?? [];
    const known = knownMessageIDsRef.current;
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
    knownMessageIDsRef.current = new Set(nextMessages.map((message) => message.id));
    setMessages(nextMessages);
  }, []);

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
      void window.wuu!.listNamedAgents().then((result) => {
        if (active) setAgents(result.agents ?? []);
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
    if (!selectedRoomID) {
      setMessages([]);
      return;
    }
    knownMessageIDsRef.current = new Set();
    setMessages([]);
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
  }, [refreshMessages, selectedRoomID]);

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
  }, [messageScroll, messages]);

  useLayoutEffect(() => {
    const conversation = conversationRef.current;
    const footer = composerFooterRef.current;
    if (!conversation || !footer) return undefined;

    const updateComposerHeight = (): void => {
      const height = Math.ceil(footer.getBoundingClientRect().height);
      conversation.style.setProperty("--channel-composer-height", `${height}px`);
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
      };
      if (editingAgentID) await window.wuu.updateNamedAgent({ agent_id: editingAgentID, ...params });
      else await window.wuu.createNamedAgent(params);
      setAgentName("");
      setAgentAvatarKey(randomAgentAvatarKey());
      setAgentAvatarImage("");
      setAgentModel("");
      setEditingAgentID("");
      setSetupPanel(null);
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
      const result = await window.wuu.createChannelRoom({
        name,
        agent_ids: roomAgentIDs,
      });
      setRoomName("");
      setRoomAgentIDs([]);
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

  async function updateTask(taskID: string, state: "doing" | "done"): Promise<void> {
    if (!window.wuu || updatingTaskID) return;
    setUpdatingTaskID(taskID);
    setError("");
    try {
      await window.wuu.updateChannelTask({ task_id: taskID, state });
      await refreshMessages(selectedRoomID);
      if (section === "tasks") await refreshTrackedTasks();
    } catch (reason) {
      setError(String(reason));
    } finally {
      setUpdatingTaskID("");
    }
  }

  async function deleteRoom(roomID: string): Promise<void> {
    if (!window.wuu) return;
    await window.wuu.deleteChannelRoom({ room_id: roomID });
    await refreshRoomsAndAgents();
  }

  async function deleteAgent(agentID: string): Promise<void> {
    if (!window.wuu) return;
    await window.wuu.deleteNamedAgent({ agent_id: agentID });
    if (editingAgentID === agentID) {
      setEditingAgentID("");
      setAgentName("");
      setAgentAvatarKey(randomAgentAvatarKey());
      setAgentAvatarImage("");
      setAgentModel("");
    }
    await refreshRoomsAndAgents();
  }

  function editAgent(agent: NamedAgent): void {
    setEditingAgentID(agent.id);
    setAgentName(agent.name);
    setAgentAvatarKey(agent.avatar_key);
    setAgentAvatarImage(agent.avatar_image ?? "");
    setAgentModel(agent.provider_override && agent.model_override ? `${agent.provider_override}\u0000${agent.model_override}` : "");
    setSetupPanel("agent");
  }

  function toggleRoomAgent(agentID: string): void {
    setRoomAgentIDs((current) =>
      current.includes(agentID)
        ? current.filter((candidate) => candidate !== agentID)
        : [...current, agentID],
    );
  }

  return (
    <section
      className={`channel-view channel-mode-${section}${resizingSplit ? " resizing-channel-split" : ""}`}
      aria-label={t("channels.title")}
      style={section === "rooms" ? { gridTemplateColumns: `${splitWidth}px minmax(0, 1fr)` } : undefined}
    >
      {section === "rooms" ? <aside className="channel-list-pane">
        <div className="channel-pane-heading">
          <span>{t("channels.rooms")}</span>
          <div className="channel-heading-actions">
            <button
              className="icon-button"
              type="button"
              aria-pressed={systemNotifications}
              aria-label={t(systemNotifications ? "channels.disableSystemNotifications" : "channels.enableSystemNotifications")}
              onClick={() => {
                const enabled = !systemNotifications;
                setSystemNotifications(enabled);
                setChannelSystemNotificationsEnabled(enabled);
              }}
            >
              {systemNotifications ? <Bell className="icon" /> : <BellOff className="icon" />}
            </button>
            <button className="icon-button" type="button" aria-label={t("channels.newRoom")} onClick={() => setSetupPanel("room")}>
              <Plus className="icon" />
            </button>
          </div>
        </div>
        <div className="channel-room-list">
          {rooms.map((room) => (
            <button
              className={`channel-room-row${room.id === selectedRoomID ? " active" : ""}`}
              type="button"
              key={room.id}
              onClick={() => setSelectedRoomID(room.id)}
            >
              <Hash className="icon" />
              <span>{room.name}</span>
            </button>
          ))}
          {!loading && rooms.length === 0 ? (
            <button className="channel-empty-action" type="button" onClick={() => setSetupPanel("room")}>
              {t("channels.newRoom")}
            </button>
          ) : null}
        </div>
        <button
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
        />
      </aside> : null}

      {section === "rooms" ? <div ref={conversationRef} className="channel-conversation">
        <div className="channel-conversation-heading">
          <div>
            <strong>{selectedRoom ? `# ${selectedRoom.name}` : t("channels.title")}</strong>
            {selectedRoom ? (
              <span>{t("channels.memberCount", { count: selectedRoom.members.length })}</span>
            ) : null}
          </div>
          <div className="channel-heading-actions">
            <button className="icon-button" type="button" disabled={!selectedRoom} aria-label={t("channels.deleteRoom")} onClick={() => selectedRoom && void deleteRoom(selectedRoom.id)}>
              <Trash2 className="icon" />
            </button>
            <button
              className="channel-task-create-button"
              type="button"
              disabled={!selectedRoom || agents.length === 0}
              onClick={() => {
                setTaskRoomID(selectedRoomID);
                setTaskOwnerID(agents[0]?.id ?? "");
                setSetupPanel("task");
              }}
            >
              <ClipboardList className="icon" />
              <span>{t("channels.newTask")}</span>
            </button>
          </div>
        </div>
        {error ? <div className="channel-error" role="alert">{error}</div> : null}
        <div ref={messageScroll.scrollRef} className="channel-message-stream" role="log" aria-live="polite">
          {messages.map((message) => {
            const own = message.author_type === "human";
            const author = own ? t("channels.you") : (agentNames.get(message.author_id) ?? message.author_id);
            if (message.kind === "task") {
              const owner = agentNames.get(message.task_owner ?? "") ?? message.task_owner ?? "";
              const done = message.task_state === "done";
              return (
                <article className={`channel-task-card${done ? " done" : ""}`} key={message.id}>
                  <div className="channel-task-title">
                    <ClipboardList className="icon" />
                    <strong>{message.body}</strong>
                  </div>
                  <div className="channel-task-meta">
                    <span>{t("channels.taskOwner", { owner })}</span>
                    <span>{t(taskStateKey(message.task_state))}</span>
                  </div>
                  {!done ? (
                    <div className="channel-task-actions">
                      {message.task_state === "open" ? (
                        <button type="button" disabled={Boolean(updatingTaskID)} onClick={() => void updateTask(message.id, "doing")}>
                          {t("channels.startTask")}
                        </button>
                      ) : null}
                      <button type="button" disabled={Boolean(updatingTaskID)} onClick={() => void updateTask(message.id, "done")}>
                        <CheckCircle2 className="icon" />
                        {t("channels.completeTask")}
                      </button>
                    </div>
                  ) : null}
                </article>
              );
            }
            return (
              <article className={`channel-message ${own ? "own" : "agent"}`} key={message.id}>
                {!own ? (() => {
                  const agent = agents.find((candidate) => candidate.id === message.author_id);
                  const status = activityFor(agent);
                  return <AgentAvatar name={author} avatarKey={agent?.avatar_key ?? "abstract-1"} avatarImage={agent?.avatar_image} status={status} statusText={activityText(status)} />;
                })() : null}
                <div className="channel-message-content">
                  <div className="channel-message-meta">
                    <strong>{author}</strong>
                    <time dateTime={message.created_at}>
                      {new Date(message.created_at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}
                    </time>
                  </div>
                  {message.body ? (
                    <div className="channel-message-bubble">
                      <RichContent text={message.body} />
                    </div>
                  ) : null}
                  {message.images?.length || message.files?.length ? (
                    <ComposerAttachmentStrip
                      images={(message.images ?? []).map((image, index) => ({ id: `${message.id}-image-${index}`, ...image }))}
                      files={(message.files ?? []).map((file, index) => ({ id: `${message.id}-file-${index}`, ...file }))}
                      removable={false}
                    />
                  ) : null}
                </div>
              </article>
            );
          })}
          {!loading && selectedRoom && messages.length === 0 ? (
            <div className="channel-stream-empty">{t("channels.empty")}</div>
          ) : null}
        </div>
        <div ref={composerFooterRef} className="channel-conversation-footer">
          <ChannelComposer
            draft={body}
            placeholder={selectedRoom ? t("channels.messagePlaceholder") : t("channels.chooseRoom")}
            disabled={!selectedRoom}
            sending={sending}
            files={composerFiles}
            images={composerImages}
            onChangeDraft={setBody}
            onPasteAttachmentFiles={(files) => void attachMessageFiles(files)}
            onRemoveFile={(id) => setComposerFiles((current) => current.filter((file) => file.id !== id))}
            onRemoveImage={(id) => setComposerImages((current) => current.filter((image) => image.id !== id))}
            onSend={() => void sendMessage()}
          />
        </div>
      </div> : section === "agents" ? (
        <div className="channel-agent-workspace" style={{ gridTemplateColumns: `${splitWidth}px minmax(0, 1fr)` }}>
          <aside className="channel-agent-directory">
            <div className="channel-pane-heading">
              <span>{t("channels.agents")}<small className="channel-pane-count">{agents.length}</small></span>
              <div className="channel-heading-actions">
                <button
                  className="icon-button"
                  type="button"
                  aria-label={t("channels.newAgent")}
                  onClick={() => {
                    setEditingAgentID("");
                    setAgentName("");
                    setAgentAvatarKey(randomAgentAvatarKey());
                    setAgentAvatarImage("");
                    setAgentModel("");
                    setSetupPanel("agent");
                  }}
                >
                  <Plus className="icon" />
                </button>
              </div>
            </div>
            {error ? <div className="channel-error" role="alert">{error}</div> : null}
            <div className="channel-agent-directory-list">
            {agents.map((agent) => {
              const status = activityFor(agent);
              const roomCount = rooms.filter((room) => room.members.some((member) => member.member_type === "agent" && member.member_id === agent.id)).length;
              const model = agent.model_override || t("channels.inheritModel");
              return (
                <div className="channel-agent-directory-row" key={agent.id}>
                  <button className="channel-agent-directory-identity" type="button" onClick={() => editAgent(agent)}>
                    <AgentAvatar name={agent.name} avatarKey={agent.avatar_key} avatarImage={agent.avatar_image} status={status} statusText={activityText(status)} />
                    <span><strong>{agent.name}</strong><small>{model} · {t("channels.agentRoomCount", { count: roomCount })}</small></span>
                  </button>
                  <div className="channel-agent-directory-actions">
                    <button className="icon-button" type="button" aria-label={t("channels.editAgent")} onClick={() => editAgent(agent)}><Pencil className="icon" /></button>
                    <button className="icon-button" type="button" aria-label={t("channels.deleteAgent")} onClick={() => void deleteAgent(agent.id)}><Trash2 className="icon" /></button>
                  </div>
                </div>
              );
            })}
            {!loading && agents.length === 0 ? <div className="channel-management-empty">{t("channels.newAgent")}</div> : null}
            </div>
            <button
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
            />
          </aside>
          <div className="channel-agent-graph-pane">
            <AgentRelationshipGraph
              agents={agents}
              rooms={rooms}
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
              disabled={!selectedRoom || agents.length === 0}
              onClick={() => {
                setTaskRoomID(selectedRoomID || rooms[0]?.id || "");
                setTaskOwnerID(agents[0]?.id ?? "");
                setSetupPanel("task");
              }}
            >
              <Plus className="icon" />
              {t("channels.newTask")}
            </button>
          </div>
          {error ? <div className="channel-error" role="alert">{error}</div> : null}
          <div className="channel-management-table channel-task-table">
            <div className="channel-management-table-head" aria-hidden="true">
              <span>{t("channels.taskTitle")}</span>
              <span>{t("channels.rooms")}</span>
              <span>{t("channels.taskOwnerLabel")}</span>
              <span>{t("channels.status")}</span>
            </div>
            {trackedTasks.map((task) => {
              const room = rooms.find((candidate) => candidate.id === task.room_id);
              const owner = agentNames.get(task.task_owner ?? "") ?? task.task_owner ?? "";
              return (
                <button
                  className={`channel-management-row channel-task-management-row${task.task_state === "done" ? " done" : ""}`}
                  type="button"
                  key={task.id}
                  onClick={() => {
                    setSelectedRoomID(task.room_id);
                    onSectionChange?.("rooms");
                  }}
                >
                  <strong>{task.body}</strong>
                  <span className="channel-management-secondary">{room ? `# ${room.name}` : task.room_id}</span>
                  <span className="channel-management-secondary">{owner}</span>
                  <span>{t(taskStateKey(task.task_state))}</span>
                </button>
              );
            })}
            {trackedTasks.length === 0 ? <div className="channel-management-empty">{t("channels.noTasks")}</div> : null}
          </div>
        </div>
      )}

      <SidebarNameDialog
        open={setupPanel === "agent"}
        title={agentName}
        onTitleChange={setAgentName}
        onSubmit={() => void submitAgent()}
        onClose={() => { setSetupPanel(null); setEditingAgentID(""); setAgentName(""); setAgentAvatarKey(randomAgentAvatarKey()); setAgentAvatarImage(""); setAgentModel(""); }}
        dialogTitle={editingAgentID ? t("channels.editAgent") : t("channels.newAgent")}
        dialogTitleId="channel-agent-dialog-title"
        fieldLabel={t("channels.name")}
        fieldAriaLabel={t("channels.name")}
        placeholder="Andy"
        icon={Bot}
        submitLabel={editingAgentID ? t("channels.save") : t("channels.create")}
        cancelLabel={t("channels.cancel")}
        submitDisabled={!agentName.trim()}
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
                  onClick={() => { setAgentAvatarKey(avatarKey); setAgentAvatarImage(""); }}
                >
                  <AgentAvatarMark avatarKey={avatarKey} />
                </button>
              ))}
              <button
                className={`channel-custom-avatar${agentAvatarImage ? " active" : ""}`}
                type="button"
                aria-label={t("channels.customAvatar")}
                aria-pressed={Boolean(agentAvatarImage)}
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
                  setError("");
                  void agentAvatarImageFromFile(file)
                    .then(setAgentAvatarImage)
                    .catch(() => setError(t("channels.invalidAvatarImage")))
                    .finally(() => { input.value = ""; });
                }}
              />
            </div>
          </fieldset>
          <label className="sidebar-name-dialog-field">
            <span className="sidebar-name-dialog-label">{t("channels.model")}</span>
            <SelectMenu value={agentModel} onChange={setAgentModel} groups={modelGroups} ariaLabel={t("channels.model")} flip />
          </label>
        </div>}
      />
      <SidebarNameDialog
        open={setupPanel === "room"}
        title={roomName}
        onTitleChange={setRoomName}
        onSubmit={() => void submitRoom()}
        onClose={() => setSetupPanel(null)}
        dialogTitle={t("channels.newRoom")}
        dialogTitleId="channel-room-dialog-title"
        fieldLabel={t("channels.name")}
        fieldAriaLabel={t("channels.name")}
        placeholder={t("channels.newRoom")}
        icon={Hash}
        submitLabel={t("channels.create")}
        cancelLabel={t("channels.cancel")}
        submitDisabled={!roomName.trim()}
        content={<div className="channel-setup-form">
          <label className="sidebar-name-dialog-field"><span className="sidebar-name-dialog-label">{t("channels.name")}</span><input className="sidebar-name-dialog-input" value={roomName} onChange={(event) => setRoomName(event.currentTarget.value)} autoFocus /></label>
          <fieldset><legend>{t("channels.agents")}</legend>{agents.map((agent) => <label className="channel-checkbox-row" key={agent.id}><input type="checkbox" checked={roomAgentIDs.includes(agent.id)} onChange={() => toggleRoomAgent(agent.id)} /><span>{agent.name}</span></label>)}</fieldset>
        </div>}
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
          <label className="sidebar-name-dialog-field"><span className="sidebar-name-dialog-label">{t("channels.taskOwnerLabel")}</span><SelectMenu value={taskOwnerID} onChange={setTaskOwnerID} options={agents.map((agent) => ({ value: agent.id, label: agent.name }))} ariaLabel={t("channels.taskOwnerLabel")} flip /></label>
        </div>}
      />
    </section>
  );
}
