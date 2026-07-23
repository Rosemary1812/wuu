import { Bell, BellOff, Bot, CheckCircle2, ClipboardList, Hash, MessageCircle, Plus, Send, Users, X } from "lucide-react";
import { FormEvent, useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { ChannelMessage, ChannelRoom, NamedAgent } from "../shared/protocol";
import { channelSystemNotificationsEnabled, setChannelSystemNotificationsEnabled } from "./ChannelPreferences";
import { useI18n } from "./i18n";

type SetupPanel = "agent" | "room" | "task" | null;

function taskStateKey(state?: string): "channels.taskState.open" | "channels.taskState.doing" | "channels.taskState.done" {
  if (state === "doing") return "channels.taskState.doing";
  if (state === "done") return "channels.taskState.done";
  return "channels.taskState.open";
}

export function ChannelView(): JSX.Element {
  const { t } = useI18n();
  const [agents, setAgents] = useState<NamedAgent[]>([]);
  const [rooms, setRooms] = useState<ChannelRoom[]>([]);
  const [selectedRoomID, setSelectedRoomID] = useState("");
  const [messages, setMessages] = useState<ChannelMessage[]>([]);
  const [setupPanel, setSetupPanel] = useState<SetupPanel>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [sending, setSending] = useState(false);
  const [body, setBody] = useState("");
  const [agentName, setAgentName] = useState("");
  const [agentModel, setAgentModel] = useState("");
  const [agentAutostart, setAgentAutostart] = useState(true);
  const [startingAgentID, setStartingAgentID] = useState("");
  const [roomName, setRoomName] = useState("");
  const [roomKind, setRoomKind] = useState<"channel" | "dm">("channel");
  const [roomAgentIDs, setRoomAgentIDs] = useState<string[]>([]);
  const [systemNotifications, setSystemNotifications] = useState(channelSystemNotificationsEnabled);
  const [taskTitle, setTaskTitle] = useState("");
  const [taskOwnerID, setTaskOwnerID] = useState("");
  const [updatingTaskID, setUpdatingTaskID] = useState("");
  const streamEndRef = useRef<HTMLDivElement | null>(null);

  const selectedRoom = useMemo(
    () => rooms.find((room) => room.id === selectedRoomID),
    [rooms, selectedRoomID],
  );
  const agentNames = useMemo(
    () => new Map(agents.map((agent) => [agent.id, agent.name])),
    [agents],
  );

  const refreshRoomsAndAgents = useCallback(async (): Promise<void> => {
    if (!window.wuu) return;
    const [agentResult, roomResult] = await Promise.all([
      window.wuu.listNamedAgents(),
      window.wuu.listChannelRooms(),
    ]);
    setAgents(agentResult.agents ?? []);
    setRooms(roomResult.rooms ?? []);
    setSelectedRoomID((current) =>
      current && roomResult.rooms.some((room) => room.id === current)
        ? current
        : (roomResult.rooms[0]?.id ?? ""),
    );
  }, []);

  const refreshMessages = useCallback(async (roomID: string): Promise<void> => {
    if (!window.wuu || !roomID) {
      setMessages([]);
      return;
    }
    const result = await window.wuu.listChannelMessages({ room_id: roomID, limit: 500 });
    setMessages(result.messages ?? []);
  }, []);

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
    if (!selectedRoomID) {
      setMessages([]);
      return;
    }
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

  useEffect(() => {
    streamEndRef.current?.scrollIntoView({ block: "end" });
  }, [messages.length]);

  async function submitAgent(event: FormEvent): Promise<void> {
    event.preventDefault();
    if (!window.wuu || !agentName.trim()) return;
    setError("");
    try {
      await window.wuu.createNamedAgent({
        name: agentName.trim(),
        model_override: agentModel.trim() || undefined,
        autostart: agentAutostart,
      });
      setAgentName("");
      setAgentModel("");
      setAgentAutostart(true);
      setSetupPanel(null);
      await refreshRoomsAndAgents();
    } catch (reason) {
      setError(String(reason));
    }
  }

  async function submitRoom(event: FormEvent): Promise<void> {
    event.preventDefault();
    const selectedAgent = roomKind === "dm" ? agents.find((agent) => agent.id === roomAgentIDs[0]) : undefined;
    const name = roomName.trim() || selectedAgent?.name || "";
    if (!window.wuu || !name || (roomKind === "dm" && roomAgentIDs.length !== 1)) return;
    setError("");
    try {
      const result = await window.wuu.createChannelRoom({
        name,
        kind: roomKind,
        agent_ids: roomAgentIDs,
      });
      setRoomName("");
      setRoomKind("channel");
      setRoomAgentIDs([]);
      setSetupPanel(null);
      await refreshRoomsAndAgents();
      setSelectedRoomID(result.room.id);
    } catch (reason) {
      setError(String(reason));
    }
  }

  async function sendMessage(event: FormEvent): Promise<void> {
    event.preventDefault();
    const messageBody = body.trim();
    if (!window.wuu || !selectedRoomID || !messageBody || sending) return;
    setSending(true);
    setError("");
    try {
      await window.wuu.sendChannelMessage({ room_id: selectedRoomID, body: messageBody });
      setBody("");
      await refreshMessages(selectedRoomID);
    } catch (reason) {
      setError(String(reason));
    } finally {
      setSending(false);
    }
  }

  async function submitTask(event: FormEvent): Promise<void> {
    event.preventDefault();
    const title = taskTitle.trim();
    if (!window.wuu || !selectedRoomID || !title || !taskOwnerID) return;
    setError("");
    try {
      await window.wuu.createChannelTask({
        room_id: selectedRoomID,
        title,
        owner_id: taskOwnerID,
      });
      setTaskTitle("");
      setTaskOwnerID("");
      setSetupPanel(null);
      await refreshMessages(selectedRoomID);
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
    } catch (reason) {
      setError(String(reason));
    } finally {
      setUpdatingTaskID("");
    }
  }

  async function startAgent(agentID: string): Promise<void> {
    if (!window.wuu || startingAgentID) return;
    setStartingAgentID(agentID);
    setError("");
    try {
      await window.wuu.startNamedAgent({ agent_id: agentID });
    } catch (reason) {
      setError(String(reason));
    } finally {
      setStartingAgentID("");
    }
  }

  function toggleRoomAgent(agentID: string): void {
    if (roomKind === "dm") {
      setRoomAgentIDs([agentID]);
      return;
    }
    setRoomAgentIDs((current) =>
      current.includes(agentID)
        ? current.filter((candidate) => candidate !== agentID)
        : [...current, agentID],
    );
  }

  return (
    <section className="channel-view" aria-label={t("channels.title")}>
      <aside className="channel-list-pane">
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
              {room.kind === "dm" ? <MessageCircle className="icon" /> : <Hash className="icon" />}
              <span>{room.name}</span>
            </button>
          ))}
          {!loading && rooms.length === 0 ? (
            <button className="channel-empty-action" type="button" onClick={() => setSetupPanel("room")}>
              {t("channels.newRoom")}
            </button>
          ) : null}
        </div>
        <div className="channel-agent-footer">
          <button className="channel-agent-button" type="button" onClick={() => setSetupPanel("agent")}>
            <Users className="icon" />
            <span>{t("channels.agents")}</span>
            <span className="channel-count">{agents.length}</span>
          </button>
        </div>
      </aside>

      <div className="channel-conversation">
        <div className="channel-conversation-heading">
          <div>
            <strong>{selectedRoom ? `# ${selectedRoom.name}` : t("channels.title")}</strong>
            {selectedRoom ? (
              <span>{t("channels.memberCount", { count: selectedRoom.members.length })}</span>
            ) : null}
          </div>
          <button
            className="channel-task-create-button"
            type="button"
            disabled={!selectedRoom || agents.length === 0}
            onClick={() => {
              setTaskOwnerID(agents[0]?.id ?? "");
              setSetupPanel("task");
            }}
          >
            <ClipboardList className="icon" />
            <span>{t("channels.newTask")}</span>
          </button>
        </div>
        {error ? <div className="channel-error" role="alert">{error}</div> : null}
        <div className="channel-message-stream" aria-live="polite">
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
              <article className={`channel-message${own ? " own" : ""}`} key={message.id}>
                <div className="channel-message-avatar" aria-hidden="true">
                  {own ? author.slice(0, 1) : <Bot className="icon" />}
                </div>
                <div className="channel-message-content">
                  <div className="channel-message-meta">
                    <strong>{author}</strong>
                    <time dateTime={message.created_at}>
                      {new Date(message.created_at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}
                    </time>
                  </div>
                  <p>{message.body}</p>
                </div>
              </article>
            );
          })}
          {!loading && selectedRoom && messages.length === 0 ? (
            <div className="channel-stream-empty">{t("channels.empty")}</div>
          ) : null}
          <div ref={streamEndRef} />
        </div>
        <form className="channel-composer" onSubmit={(event) => void sendMessage(event)}>
          <textarea
            value={body}
            onChange={(event) => setBody(event.currentTarget.value)}
            placeholder={selectedRoom ? t("channels.messagePlaceholder") : t("channels.chooseRoom")}
            disabled={!selectedRoom || sending}
            maxLength={4000}
            rows={2}
            onKeyDown={(event) => {
              if (event.key === "Enter" && !event.shiftKey) {
                event.preventDefault();
                event.currentTarget.form?.requestSubmit();
              }
            }}
          />
          <button className="channel-send-button" type="submit" disabled={!body.trim() || !selectedRoom || sending}>
            <Send className="icon" />
            <span>{t("channels.send")}</span>
          </button>
        </form>
      </div>

      {setupPanel ? (
        <div className="channel-setup-backdrop" role="presentation">
          <section className="channel-setup-panel" role="dialog" aria-modal="true">
            <div className="channel-setup-heading">
              <strong>{t(setupPanel === "agent" ? "channels.newAgent" : setupPanel === "room" ? "channels.newRoom" : "channels.newTask")}</strong>
              <button className="icon-button" type="button" onClick={() => setSetupPanel(null)} aria-label={t("common.close")}>
                <X className="icon" />
              </button>
            </div>
            {setupPanel === "agent" ? (
              <>
                <form className="channel-setup-form" onSubmit={(event) => void submitAgent(event)}>
                  <label>
                    <span>{t("channels.name")}</span>
                    <input value={agentName} onChange={(event) => setAgentName(event.currentTarget.value)} autoFocus />
                  </label>
                  <label>
                    <span>{t("channels.model")}</span>
                    <input value={agentModel} onChange={(event) => setAgentModel(event.currentTarget.value)} placeholder={t("channels.inheritModel")} />
                  </label>
                  <label className="channel-checkbox-row">
                    <input type="checkbox" checked={agentAutostart} onChange={(event) => setAgentAutostart(event.currentTarget.checked)} />
                    <span>{t("channels.autostart")}</span>
                  </label>
                  <button className="primary-button" type="submit" disabled={!agentName.trim()}>{t("channels.create")}</button>
                </form>
                {agents.length > 0 ? (
                  <div className="channel-agent-list">
                    {agents.map((agent) => (
                      <div className="channel-agent-row" key={agent.id}>
                        <Bot className="icon" />
                        <span>{agent.name}</span>
                        <button type="button" disabled={Boolean(startingAgentID)} onClick={() => void startAgent(agent.id)}>
                          {startingAgentID === agent.id ? t("channels.starting") : t("channels.start")}
                        </button>
                      </div>
                    ))}
                  </div>
                ) : null}
              </>
            ) : setupPanel === "room" ? (
              <form className="channel-setup-form" onSubmit={(event) => void submitRoom(event)}>
                <label>
                  <span>{t("channels.roomKind")}</span>
                  <select value={roomKind} onChange={(event) => {
                    const kind = event.currentTarget.value as "channel" | "dm";
                    setRoomKind(kind);
                    if (kind === "dm") setRoomAgentIDs((current) => current.slice(0, 1));
                  }}>
                    <option value="channel">{t("channels.channel")}</option>
                    <option value="dm">{t("channels.dm")}</option>
                  </select>
                </label>
                <label>
                  <span>{t("channels.name")}</span>
                  <input value={roomName} onChange={(event) => setRoomName(event.currentTarget.value)} autoFocus placeholder={roomKind === "dm" ? t("channels.dmNameOptional") : ""} />
                </label>
                <fieldset>
                  <legend>{t("channels.agents")}</legend>
                  {agents.map((agent) => (
                    <label className="channel-checkbox-row" key={agent.id}>
                      <input type={roomKind === "dm" ? "radio" : "checkbox"} name={roomKind === "dm" ? "dm-agent" : undefined} checked={roomAgentIDs.includes(agent.id)} onChange={() => toggleRoomAgent(agent.id)} />
                      <span>{agent.name}</span>
                    </label>
                  ))}
                </fieldset>
                <button className="primary-button" type="submit" disabled={roomKind === "dm" ? roomAgentIDs.length !== 1 : !roomName.trim()}>{t("channels.create")}</button>
              </form>
            ) : (
              <form className="channel-setup-form" onSubmit={(event) => void submitTask(event)}>
                <label>
                  <span>{t("channels.taskTitle")}</span>
                  <input value={taskTitle} onChange={(event) => setTaskTitle(event.currentTarget.value)} autoFocus maxLength={4000} />
                </label>
                <label>
                  <span>{t("channels.taskOwnerLabel")}</span>
                  <select value={taskOwnerID} onChange={(event) => setTaskOwnerID(event.currentTarget.value)}>
                    {agents.map((agent) => <option key={agent.id} value={agent.id}>{agent.name}</option>)}
                  </select>
                </label>
                <button className="primary-button" type="submit" disabled={!taskTitle.trim() || !taskOwnerID}>{t("channels.create")}</button>
              </form>
            )}
          </section>
        </div>
      ) : null}
    </section>
  );
}
