import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { ChannelRoom, NamedAgent, WuuDesktopApi } from "../shared/protocol";
import { graphDensityScale } from "./AgentRelationshipGraph";
import { groupAvatarRowSizes } from "./ChannelGroupAvatar";
import { ChannelView } from "./ChannelView";

let container: HTMLDivElement;
let root: Root | null = null;

const agents: NamedAgent[] = [
  {
    id: "agent-1",
    name: "Alpha",
    memory_dir: "/agents/agent-1/memory",
    avatar_key: "abstract-3",
    avatar_image: "data:image/png;base64,iVBORw0KGgo=",
    model_override: "",
    autostart: true,
    created_at: "2026-07-23T00:00:00Z",
    activity_status: "thinking",
    activity_room_ids: ["room-1"],
  },
  {
    id: "agent-2",
    name: "Beta",
    memory_dir: "/agents/agent-2/memory",
    avatar_key: "abstract-6",
    model_override: "",
    autostart: true,
    created_at: "2026-07-23T00:00:00Z",
    activity_status: "idle",
  },
];

const rooms: ChannelRoom[] = [
  {
    id: "room-1",
    name: "general",
    kind: "channel",
    created_by: "human",
    members: [
      { room_id: "room-1", member_type: "agent", member_id: "agent-1", joined_at: "2026-07-23T00:00:00Z" },
      { room_id: "room-1", member_type: "agent", member_id: "agent-2", joined_at: "2026-07-23T00:00:00Z" },
    ],
    created_at: "2026-07-23T00:00:00Z",
  },
  {
    id: "room-2",
    name: "research",
    kind: "channel",
    created_by: "human",
    members: [],
    created_at: "2026-07-23T00:00:00Z",
  },
];

function createApi(): Partial<WuuDesktopApi> {
  return {
    bootstrapChannels: vi.fn(async () => ({ agents, rooms })),
    listNamedAgents: vi.fn(async () => ({ agents })),
    createNamedAgent: vi.fn(async (params) => ({ agent: { ...agents[0], name: params.name } })),
    updateNamedAgent: vi.fn(async (params) => ({ agent: { ...agents[0], name: params.name } })),
    deleteNamedAgent: vi.fn(async () => ({ deleted: true })),
    startNamedAgent: vi.fn(async () => ({ agent: agents[0] })),
    listChannelRooms: vi.fn(async () => ({ rooms })),
    createChannelRoom: vi.fn(async (params) => ({ room: { ...rooms[1], name: params.name } })),
    updateChannelRoom: vi.fn(async (params) => ({ room: { ...rooms[0], avatar_image: params.avatar_image } })),
    deleteChannelRoom: vi.fn(async () => ({ deleted: true })),
    listChannelMessages: vi.fn(async ({ room_id }) => ({
      messages: room_id === "room-1"
        ? [{
            id: "message-1",
            room_id,
            seq: 1,
            author_type: "agent" as const,
            author_id: "agent-1",
            kind: "text" as const,
            body: "Hello from **Alpha** with `markdown`\n\n<img src=x onerror=alert(1)>",
            created_at: "2026-07-23T00:00:00Z",
          }, {
            id: "message-2",
            room_id,
            seq: 2,
            author_type: "human" as const,
            author_id: "human",
            kind: "text" as const,
            body: "Human direction",
            images: [{ media_type: "image/png", data: "aW1hZ2U=" }],
            files: [{ media_type: "application/pdf", data: "cGRm", filename: "brief.pdf" }],
            created_at: "2026-07-23T00:00:30Z",
          }, {
            id: "task-1",
            room_id,
            seq: 3,
            author_type: "human" as const,
            author_id: "human",
            kind: "task" as const,
            body: "Investigate flaky build",
            task_state: "doing",
            task_owner: "agent-1",
            created_at: "2026-07-23T00:01:00Z",
          }, {
            id: "message-3",
            room_id,
            seq: 4,
            thread_id: "message-1",
            reply_to: "message-1",
            author_type: "agent" as const,
            author_id: "agent-2",
            kind: "text" as const,
            body: "A threaded answer",
            created_at: "2026-07-23T00:02:00Z",
          }]
        : [],
    })),
    sendChannelMessage: vi.fn(async (params) => ({
      message: {
        id: "message-2",
        room_id: params.room_id,
        seq: 2,
        author_type: "human" as const,
        author_id: "human",
        kind: "text" as const,
        body: params.body,
        created_at: "2026-07-23T00:01:00Z",
      },
    })),
    createChannelTask: vi.fn(async (params) => ({
      task: {
        id: "task-1",
        room_id: params.room_id,
        seq: 3,
        author_type: "human" as const,
        author_id: "human",
        kind: "task" as const,
        body: params.title,
        task_state: "open",
        task_owner: params.owner_id,
        created_at: "2026-07-23T00:02:00Z",
      },
    })),
    updateChannelTask: vi.fn(async (params) => ({
      task: {
        id: params.task_id,
        room_id: "room-1",
        seq: 3,
        author_type: "human" as const,
        author_id: "human",
        kind: "task" as const,
        body: "Investigate",
        task_state: params.state ?? "open",
        task_owner: "agent-1",
        created_at: "2026-07-23T00:02:00Z",
      },
    })),
  };
}

async function settle(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

function setInputValue(input: HTMLInputElement | HTMLTextAreaElement, value: string): void {
  const prototype = input instanceof HTMLTextAreaElement
    ? HTMLTextAreaElement.prototype
    : HTMLInputElement.prototype;
  const setter = Object.getOwnPropertyDescriptor(prototype, "value")?.set;
  setter?.call(input, value);
  input.dispatchEvent(new Event("input", { bubbles: true }));
}

function setSelectValue(select: HTMLSelectElement, value: string): void {
  const setter = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, "value")?.set;
  setter?.call(select, value);
  select.dispatchEvent(new Event("change", { bubbles: true }));
}

beforeEach(() => {
  window.localStorage.clear();
  container = document.createElement("div");
  document.body.appendChild(container);
  Object.defineProperty(Element.prototype, "scrollIntoView", {
    configurable: true,
    value: vi.fn(),
  });
});

afterEach(() => {
  act(() => root?.unmount());
  root = null;
  container.remove();
  vi.restoreAllMocks();
});

describe("ChannelView", () => {
  it("uses WeChat-style centered rows for one through nine members", () => {
    expect(Array.from({ length: 10 }, (_, index) => groupAvatarRowSizes(index))).toEqual([
      [1], [1], [2], [1, 2], [2, 2], [2, 3], [3, 3], [1, 3, 3], [2, 3, 3], [3, 3, 3],
    ]);
  });

  it("scales graph nodes down within a bounded range as the graph grows", () => {
    expect(graphDensityScale(2)).toBe(1.35);
    expect(graphDensityScale(12)).toBeLessThan(graphDensityScale(4));
    expect(graphDensityScale(10_000)).toBe(0.68);
  });

  it("collapses the room and agent lists with one persistent control", async () => {
    const api = createApi();
    Object.defineProperty(window, "wuu", { configurable: true, value: api });
    root = createRoot(container);
    act(() => root?.render(<ChannelView />));
    await settle();

    const collapse = container.querySelector<HTMLButtonElement>('[aria-label="收起列表"]');
    expect(collapse?.getAttribute("aria-expanded")).toBe("true");
    act(() => collapse?.click());

    expect(container.querySelector(".channel-view")?.classList.contains("channel-list-collapsed")).toBe(true);
    expect(container.querySelector(".channel-room-list")).toBeNull();
    expect(container.querySelector('[aria-label="展开列表"]')).not.toBeNull();
    expect(window.localStorage.getItem("wuu.channels.listCollapsed")).toBe("true");

    act(() => root?.render(<ChannelView section="agents" />));
    expect(container.querySelector(".channel-agent-directory-list")).toBeNull();
    expect(container.querySelector(".channel-agent-graph-pane")).not.toBeNull();

    act(() => container.querySelector<HTMLButtonElement>('[aria-label="展开列表"]')?.click());
    expect(container.querySelector(".channel-agent-directory-list")).not.toBeNull();
    expect(window.localStorage.getItem("wuu.channels.listCollapsed")).toBe("false");
  });

  it("does not show a conversation composer without a selected room", async () => {
    const api = createApi();
    api.bootstrapChannels = vi.fn(async () => ({ agents, rooms: [] }));
    Object.defineProperty(window, "wuu", { configurable: true, value: api });
    root = createRoot(container);
    act(() => root?.render(<ChannelView />));
    await settle();

    expect(container.querySelector(".channel-empty-action")?.textContent).toBe("新建频道");
    expect(container.querySelector(".channel-conversation-footer")).toBeNull();
    expect(container.querySelector(".channel-composer")).toBeNull();
    expect(container.querySelector(".channel-response-status")?.textContent).toBe("暂无 Agent 响应");
  });

  it("shows active agents from the selected room in the response status bar", async () => {
    const api = createApi();
    Object.defineProperty(window, "wuu", { configurable: true, value: api });
    root = createRoot(container);
    act(() => root?.render(<ChannelView />));
    await settle();

    const status = container.querySelector<HTMLElement>(".channel-response-status");
    expect(status?.textContent).toContain("Alpha");
    expect(status?.textContent).toContain("处理中");
    expect(status?.getAttribute("aria-label")).toBe("Alpha: 处理中");
    expect(status?.querySelectorAll(".channel-response-status-avatar")).toHaveLength(1);
    expect(status?.textContent).not.toContain("Beta");
  });

  it("does not show an agent responding in another room", async () => {
    const crossRoomAgents = agents.map((agent) => agent.id === "agent-1"
      ? { ...agent, activity_room_ids: ["room-2"] }
      : agent);
    const api = createApi();
    api.bootstrapChannels = vi.fn(async () => ({ agents: crossRoomAgents, rooms }));
    api.listNamedAgents = vi.fn(async () => ({ agents: crossRoomAgents }));
    Object.defineProperty(window, "wuu", { configurable: true, value: api });
    root = createRoot(container);
    act(() => root?.render(<ChannelView />));
    await settle();

    expect(container.querySelector(".channel-response-status")?.textContent).toBe("暂无 Agent 响应");
  });

  it("inserts and focuses a mention when an agent author name is clicked", async () => {
    const api = createApi();
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      callback(0);
      return 1;
    });
    Object.defineProperty(window, "wuu", { configurable: true, value: api });
    root = createRoot(container);
    act(() => root?.render(<ChannelView />));
    await settle();

    const author = container.querySelector<HTMLButtonElement>('[aria-label="提及 Alpha"]');
    const textarea = container.querySelector<HTMLTextAreaElement>(".channel-conversation-footer textarea");
    expect(author?.querySelector("span")?.textContent).toBe("@");
    act(() => author?.click());

    expect(textarea?.value).toBe("@Alpha ");
    expect(document.activeElement).toBe(textarea);
  });

  it("opens and filters the member picker when typing @", async () => {
    const api = createApi();
    const mentionAgents = [agents[0], { ...agents[1], model_override: "gpt-5.3-codex" }];
    api.bootstrapChannels = vi.fn(async () => ({
      agents: mentionAgents,
      rooms,
    }));
    api.listNamedAgents = vi.fn(async () => ({ agents: mentionAgents }));
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      callback(0);
      return 1;
    });
    Object.defineProperty(window, "wuu", { configurable: true, value: api });
    root = createRoot(container);
    act(() => root?.render(<ChannelView />));
    await settle();

    const textarea = container.querySelector<HTMLTextAreaElement>(".channel-conversation-footer textarea");
    act(() => setInputValue(textarea!, "@"));
    expect(Array.from(container.querySelectorAll(".channel-mention-name")).map((name) => name.textContent)).toEqual(["Alpha", "Beta"]);
    expect(container.querySelector(".channel-mention-model")?.textContent).toBe("gpt-5.3-codex");
    expect(container.querySelector(".channel-mention-menu button.selected .channel-mention-key")?.textContent).toBe("↵");
    act(() => {
      textarea?.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowDown", bubbles: true }));
      textarea?.dispatchEvent(new KeyboardEvent("keyup", { key: "ArrowDown", bubbles: true }));
    });
    expect(container.querySelector(".channel-mention-menu button.selected .channel-mention-name")?.textContent).toBe("Beta");
    act(() => {
      textarea?.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowUp", bubbles: true }));
      textarea?.dispatchEvent(new KeyboardEvent("keyup", { key: "ArrowUp", bubbles: true }));
    });
    expect(container.querySelector(".channel-mention-menu button.selected .channel-mention-name")?.textContent).toBe("Alpha");

    act(() => setInputValue(textarea!, "@Be"));
    expect(Array.from(container.querySelectorAll(".channel-mention-name")).map((name) => name.textContent)).toEqual(["Beta"]);
    act(() => textarea?.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true })));
    expect(textarea?.value).toBe("@Beta ");
    expect(container.querySelector(".channel-mention-menu")).toBeNull();
  });

  it("loads rooms, selects a room, and sends a human message", async () => {
    const api = createApi();
    Object.defineProperty(window, "wuu", { configurable: true, value: api });

    root = createRoot(container);
    act(() => root?.render(<ChannelView />));
    await settle();

    expect(container.querySelector(".channel-conversation-heading")).toBeNull();
    expect(document.querySelector(".sidebar-name-dialog")).toBeNull();
    const agentBubble = container.querySelector(".channel-message.agent .channel-message-bubble");
    expect(agentBubble?.textContent).toBe("Hello from Alpha with markdown\n<img src=x onerror=alert(1)>");
    expect(
      container
        .querySelector<HTMLElement>(".channel-conversation")
        ?.style.getPropertyValue("--channel-composer-height"),
    ).toBe("");
    expect(agentBubble?.querySelector("strong")?.textContent).toBe("Alpha");
    expect(agentBubble?.querySelector("code")?.textContent).toBe("markdown");
    expect(agentBubble?.querySelector("img")).toBeNull();
    expect(agentBubble?.textContent).not.toContain("**");
    expect(container.querySelector(".channel-message.own .channel-message-bubble")?.textContent).toBe("Human direction");
    expect(container.querySelector<HTMLImageElement>(".channel-message.own .composer-image-attachment img")?.src).toContain("data:image/png;base64,aW1hZ2U=");
    expect(container.querySelector(".channel-message.own .composer-file-attachment")?.textContent).toContain("brief.pdf");
    expect(container.querySelector(".channel-message.own .composer-attachments button")).toBeNull();
    expect(container.querySelector(".channel-message.own .channel-human-avatar .default-avatar")).not.toBeNull();
    expect(container.querySelector(".channel-message.own .channel-message-meta strong")?.textContent).toBe("你");
    expect(container.querySelector(".channel-task-card")).toBeNull();
    expect(container.querySelector(".channel-message-stream")?.textContent).not.toContain("Investigate flaky build");
    expect(container.querySelector('[aria-label="Alpha: 处理中"]')).not.toBeNull();
    expect(container.querySelector(".channel-agent-status-dot.thinking")).not.toBeNull();
    expect(container.querySelector(".channel-agent-status-card")?.textContent).toBe("处理中");
    expect(container.querySelector(".channel-agent-status-card strong")).toBeNull();
    const firstRoomRow = container.querySelector(".channel-room-row");
    expect(firstRoomRow?.textContent).toContain("2 位成员");
    expect(firstRoomRow?.querySelectorAll(".channel-group-avatar-cell")).toHaveLength(2);
    expect(firstRoomRow?.querySelector(".channel-directory-settings")).not.toBeNull();
    const detailsToggle = container.querySelector<HTMLButtonElement>(".channel-room-details-toggle");
    expect(detailsToggle).not.toBeNull();
    act(() => detailsToggle?.click());
    const detailsDialog = document.querySelector(".sidebar-name-dialog");
    expect(detailsDialog?.textContent).toContain("群聊详情");
    expect(detailsDialog?.textContent).toContain("群成员");
    expect(container.querySelector(".channel-conversation")?.classList.contains("details-open")).toBe(false);
    expect(container.querySelector(".channel-room-main")).not.toBeNull();
    expect(container.querySelector(".channel-conversation-heading")).toBeNull();
    const cancelDetails = Array.from(detailsDialog?.querySelectorAll<HTMLButtonElement>("button") ?? [])
      .find((button) => button.textContent === "取消");
    act(() => cancelDetails?.click());
    expect(document.querySelector(".sidebar-name-dialog")).toBeNull();
    const research = Array.from(container.querySelectorAll<HTMLButtonElement>(".channel-room-select"))
      .find((button) => button.textContent?.includes("research"));
    act(() => research?.click());
    await settle();
    expect(api.listChannelMessages).toHaveBeenCalledWith({ room_id: "room-2", limit: 500 });

    const textarea = container.querySelector<HTMLTextAreaElement>(".channel-composer textarea");
    expect(textarea).not.toBeNull();
    expect(container.querySelector(".channel-composer .composer-plus-button")).toBeNull();
    expect(container.querySelector(".channel-composer .permission-chip")).toBeNull();
    act(() => setInputValue(textarea!, "Ask Alpha"));
    const send = container.querySelector<HTMLButtonElement>(".channel-composer .composer-send-button");
    await act(async () => send?.click());

    expect(api.sendChannelMessage).toHaveBeenCalledWith({ room_id: "room-2", body: "Ask Alpha", images: [], files: [] });
  });

  it("groups adjacent messages from the same author without repeating identity", async () => {
    const api = createApi();
    api.listChannelMessages = vi.fn(async ({ room_id }) => ({
      messages: [{
        id: "message-1",
        room_id,
        seq: 1,
        author_type: "agent" as const,
        author_id: "agent-1",
        kind: "text" as const,
        body: "First update",
        created_at: "2026-07-23T00:00:00Z",
      }, {
        id: "message-2",
        room_id,
        seq: 2,
        author_type: "agent" as const,
        author_id: "agent-1",
        kind: "text" as const,
        body: "Follow-up update",
        created_at: "2026-07-23T00:00:30Z",
      }, {
        id: "message-3",
        room_id,
        seq: 3,
        author_type: "human" as const,
        author_id: "human",
        kind: "text" as const,
        body: "New speaker",
        created_at: "2026-07-23T00:01:00Z",
      }],
    }));
    Object.defineProperty(window, "wuu", { configurable: true, value: api });
    root = createRoot(container);
    act(() => root?.render(<ChannelView />));
    await settle();

    const renderedMessages = container.querySelectorAll<HTMLElement>(".channel-message-stream > .channel-message");
    expect(renderedMessages).toHaveLength(3);
    expect(renderedMessages[0].classList.contains("grouped")).toBe(false);
    expect(renderedMessages[0].querySelector(".channel-agent-avatar")).not.toBeNull();
    expect(renderedMessages[0].querySelector(".channel-author-mention")?.textContent).toBe("@Alpha");
    expect(renderedMessages[1].classList.contains("grouped")).toBe(true);
    expect(renderedMessages[1].querySelector(".channel-agent-avatar")).toBeNull();
    expect(renderedMessages[1].querySelector(".channel-author-mention")).toBeNull();
    expect(renderedMessages[2].classList.contains("grouped")).toBe(false);
    expect(renderedMessages[2].querySelector(".channel-human-avatar")).not.toBeNull();
  });

  it("groups a thread root with its preceding author but starts a new group after the digest", async () => {
    const api = createApi();
    api.listChannelMessages = vi.fn(async ({ room_id }) => ({
      messages: [{
        id: "message-1",
        room_id,
        seq: 1,
        author_type: "agent" as const,
        author_id: "agent-1",
        kind: "text" as const,
        body: "Before the thread",
        created_at: "2026-07-23T00:00:00Z",
      }, {
        id: "message-2",
        room_id,
        seq: 2,
        author_type: "agent" as const,
        author_id: "agent-1",
        kind: "text" as const,
        body: "Thread root",
        created_at: "2026-07-23T00:00:30Z",
      }, {
        id: "reply-1",
        room_id,
        seq: 3,
        thread_id: "message-2",
        reply_to: "message-2",
        author_type: "human" as const,
        author_id: "human",
        kind: "text" as const,
        body: "Thread reply 1",
        created_at: "2026-07-23T00:00:45Z",
      }, {
        id: "reply-2",
        room_id,
        seq: 4,
        thread_id: "message-2",
        reply_to: "message-2",
        author_type: "human" as const,
        author_id: "human",
        kind: "text" as const,
        body: "Thread reply 2",
        created_at: "2026-07-23T00:00:46Z",
      }, {
        id: "reply-3",
        room_id,
        seq: 5,
        thread_id: "message-2",
        reply_to: "message-2",
        author_type: "human" as const,
        author_id: "human",
        kind: "text" as const,
        body: "Thread reply 3",
        created_at: "2026-07-23T00:00:47Z",
      }, {
        id: "reply-4",
        room_id,
        seq: 6,
        thread_id: "message-2",
        reply_to: "message-2",
        author_type: "human" as const,
        author_id: "human",
        kind: "text" as const,
        body: "Thread reply 4",
        created_at: "2026-07-23T00:00:48Z",
      }, {
        id: "message-3",
        room_id,
        seq: 7,
        author_type: "agent" as const,
        author_id: "agent-1",
        kind: "text" as const,
        body: "After the thread",
        created_at: "2026-07-23T00:01:00Z",
      }],
    }));
    Object.defineProperty(window, "wuu", { configurable: true, value: api });
    root = createRoot(container);
    act(() => root?.render(<ChannelView />));
    await settle();

    const renderedMessages = container.querySelectorAll<HTMLElement>(".channel-message-stream > .channel-message");
    expect(renderedMessages).toHaveLength(3);
    expect(renderedMessages[0].classList.contains("grouped")).toBe(false);
    expect(renderedMessages[1].classList.contains("grouped")).toBe(true);
    const digest = renderedMessages[1].querySelector(".channel-thread-digest");
    expect(digest?.textContent).toContain("4 条回复");
    expect(digest?.querySelectorAll(".channel-thread-digest-row")).toHaveLength(3);
    expect(digest?.textContent).not.toContain("Thread reply 1");
    expect(digest?.textContent).toContain("Thread reply 2");
    expect(renderedMessages[2].classList.contains("grouped")).toBe(false);
  });

  it("opens a thread, keeps replies out of the room stream, and sends a direct reply", async () => {
    const api = createApi();
    Object.defineProperty(window, "wuu", { configurable: true, value: api });
    root = createRoot(container);
    act(() => root?.render(<ChannelView />));
    await settle();

    const digest = container.querySelector<HTMLButtonElement>(".channel-thread-digest");
    expect(digest?.textContent).not.toContain("1 条回复");
    expect(digest?.querySelector(".channel-thread-digest-heading")).toBeNull();
    expect(digest?.textContent).toContain("Beta");
    expect(digest?.textContent).toContain("A threaded answer");
    expect(container.querySelector(".channel-message-stream > .channel-message")?.textContent).toContain("A threaded answer");
    act(() => digest?.click());

    const panel = container.querySelector<HTMLElement>(".channel-thread-panel");
    expect(panel?.textContent).toContain("A threaded answer");
    expect(panel?.querySelector(".channel-thread-header")).toBeNull();
    expect(panel?.querySelector(".channel-thread-close")).toBeNull();
    const threadSeparator = panel?.querySelector<HTMLButtonElement>(".channel-thread-resizer");
    expect(threadSeparator?.getAttribute("aria-valuenow")).toBe("420");
    act(() => threadSeparator?.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowLeft", bubbles: true })));
    expect(threadSeparator?.getAttribute("aria-valuenow")).toBe("444");
    expect(window.localStorage.getItem("wuu.channels.threadPanelWidth")).toBe("444");
    expect(panel?.querySelector(".composer-expand-button")).toBeNull();
    const textarea = panel?.querySelector<HTMLTextAreaElement>("textarea");
    act(() => panel?.querySelector<HTMLButtonElement>('[aria-label="提及 Beta"]')?.click());
    expect(textarea?.value).toBe("@Beta ");
    const replyActions = panel?.querySelectorAll<HTMLButtonElement>(".channel-message-actions button");
    act(() => replyActions?.item(replyActions.length - 1).click());
    expect(panel?.querySelector(".channel-thread-replying")?.textContent).toContain("Beta");

    act(() => setInputValue(textarea!, "Follow-up question"));
    await act(async () => panel?.querySelector<HTMLButtonElement>(".composer-send-button")?.click());

    expect(api.sendChannelMessage).toHaveBeenCalledWith({
      room_id: "room-1",
      thread_id: "message-1",
      reply_to: "message-3",
      body: "Follow-up question",
      images: [],
      files: [],
    });

    act(() => threadSeparator?.dispatchEvent(new MouseEvent("pointerdown", { clientX: 100, bubbles: true })));
    act(() => window.dispatchEvent(new MouseEvent("pointermove", { clientX: 240 })));
    expect(container.querySelector(".channel-thread-panel")).toBeNull();
  });

  it("collapses long agent messages and restores rich content on demand", async () => {
    const api = createApi();
    const longBody = `${Array.from({ length: 16 }, (_, index) => `Line ${index + 1}`).join("\n")}\n**Final detail**`;
    api.listChannelMessages = vi.fn(async () => ({
      messages: [{
        id: "long-message",
        room_id: "room-1",
        seq: 1,
        author_type: "agent" as const,
        author_id: "agent-1",
        kind: "text" as const,
        body: longBody,
        created_at: "2026-07-23T00:00:00Z",
      }],
    }));
    Object.defineProperty(window, "wuu", { configurable: true, value: api });
    root = createRoot(container);
    act(() => root?.render(<ChannelView />));
    await settle();

    const bubble = container.querySelector(".channel-message-bubble.long-card");
    const toggle = bubble?.querySelector<HTMLButtonElement>(".channel-message-expand-toggle");
    expect(bubble?.classList.contains("collapsed")).toBe(true);
    expect(bubble?.textContent).not.toContain("Final detail");
    expect(toggle?.textContent).toContain("显示更多");
    expect(toggle?.getAttribute("aria-expanded")).toBe("false");

    act(() => toggle?.click());
    expect(bubble?.classList.contains("expanded")).toBe(true);
    expect(bubble?.querySelector("strong")?.textContent).toBe("Final detail");
    expect(toggle?.textContent).toContain("收起");
    expect(toggle?.getAttribute("aria-expanded")).toBe("true");
  });

  it("uses the same long-message collapse inside the thread panel", async () => {
    const api = createApi();
    const longReply = Array.from({ length: 16 }, (_, index) => `Thread line ${index + 1}`).join("\n");
    api.listChannelMessages = vi.fn(async () => ({
      messages: [{
        id: "thread-root",
        room_id: "room-1",
        seq: 1,
        author_type: "human" as const,
        author_id: "human",
        kind: "text" as const,
        body: "Please investigate",
        created_at: "2026-07-23T00:00:00Z",
      }, {
        id: "thread-long-reply",
        room_id: "room-1",
        seq: 2,
        thread_id: "thread-root",
        reply_to: "thread-root",
        author_type: "agent" as const,
        author_id: "agent-2",
        kind: "text" as const,
        body: longReply,
        created_at: "2026-07-23T00:01:00Z",
      }],
    }));
    Object.defineProperty(window, "wuu", { configurable: true, value: api });
    root = createRoot(container);
    act(() => root?.render(<ChannelView />));
    await settle();

    act(() => container.querySelector<HTMLButtonElement>(".channel-thread-digest")?.click());
    const panel = container.querySelector(".channel-thread-panel");
    const replyBubble = panel?.querySelector(".channel-thread-message.agent .channel-message-bubble.long-card");
    expect(replyBubble?.classList.contains("collapsed")).toBe(true);
    expect(replyBubble?.textContent).not.toContain("Thread line 16");
    expect(replyBubble?.querySelector(".channel-message-expand-toggle")?.textContent).toContain("显示更多");
  });

  it("does not issue another bottom scroll when polling returns the same messages", async () => {
    vi.useFakeTimers();
    try {
      const api = createApi();
      Object.defineProperty(window, "wuu", { configurable: true, value: api });
      root = createRoot(container);
      act(() => root?.render(<ChannelView />));
      await settle();

      const stream = container.querySelector<HTMLDivElement>(".channel-message-stream");
      expect(stream).not.toBeNull();
      let scrollTop = 600;
      let scrollWrites = 0;
      Object.defineProperties(stream!, {
        scrollHeight: { configurable: true, get: () => 1000 },
        clientHeight: { configurable: true, get: () => 400 },
        scrollTop: {
          configurable: true,
          get: () => scrollTop,
          set: (value: number) => {
            scrollTop = value;
            scrollWrites += 1;
          },
        },
      });

      await act(async () => {
        await vi.advanceTimersByTimeAsync(20);
      });
      scrollTop = 600;
      scrollWrites = 0;

      await act(async () => {
        await vi.advanceTimersByTimeAsync(2_000);
      });

      expect(api.listChannelMessages).toHaveBeenCalledTimes(2);
      expect(scrollWrites).toBe(0);
      expect(scrollTop).toBe(600);
    } finally {
      vi.useRealTimers();
    }
  });

  it("offers the shared jump-to-latest control after the user leaves the bottom", async () => {
    vi.useFakeTimers();
    try {
      Object.defineProperty(window, "wuu", { configurable: true, value: createApi() });
      root = createRoot(container);
      act(() => root?.render(<ChannelView />));
      await settle();

      const stream = container.querySelector<HTMLDivElement>(".channel-message-stream");
      expect(stream).not.toBeNull();
      let scrollTop = 600;
      const scrollTo = vi.fn();
      Object.defineProperties(stream!, {
        scrollHeight: { configurable: true, get: () => 1000 },
        clientHeight: { configurable: true, get: () => 400 },
        scrollTop: {
          configurable: true,
          get: () => scrollTop,
          set: (value: number) => { scrollTop = value; },
        },
        scrollTo: { configurable: true, value: scrollTo },
      });

      await act(async () => {
        await vi.advanceTimersByTimeAsync(20);
      });
      act(() => {
        scrollTop = 300;
        stream?.dispatchEvent(new WheelEvent("wheel", { deltaY: -20 }));
        stream?.dispatchEvent(new Event("scroll"));
      });
      await act(async () => {
        await vi.advanceTimersByTimeAsync(20);
      });

      const jump = document.body.querySelector<HTMLButtonElement>(".jump-to-latest-pill");
      expect(jump).not.toBeNull();
      act(() => jump?.click());
      expect(scrollTo).toHaveBeenCalledWith({ top: 1000, behavior: "smooth" });
    } finally {
      vi.useRealTimers();
    }
  });

  it("shares a nonzero resizable sidebar width between rooms and agents", async () => {
    Object.defineProperty(window, "wuu", { configurable: true, value: createApi() });
    root = createRoot(container);
    act(() => root?.render(<ChannelView />));
    await settle();

    const separator = container.querySelector<HTMLButtonElement>(".channel-split-resizer");
    expect(separator?.getAttribute("aria-valuenow")).toBe("208");
    act(() => separator?.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowRight", bubbles: true })));

    expect(separator?.getAttribute("aria-valuenow")).toBe("224");
    expect(window.localStorage.getItem("wuu.channels.splitPaneWidth")).toBe("224");
    expect(container.querySelector<HTMLElement>(".channel-view")?.style.gridTemplateColumns).toBe("224px minmax(0, 1fr)");

    act(() => root?.render(<ChannelView section="agents" />));
    await settle();
    const agentSeparator = container.querySelector<HTMLButtonElement>(".channel-split-resizer");
    expect(agentSeparator?.getAttribute("aria-valuenow")).toBe("224");
    expect(container.querySelector<HTMLElement>(".channel-view")?.style.gridTemplateColumns).toBe("224px minmax(0, 1fr)");
    expect(container.querySelector<HTMLElement>(".channel-agent-workspace")?.style.gridTemplateColumns).toBe("");
    const agentRow = container.querySelector(".channel-agent-directory-row");
    expect(agentRow?.classList.contains("channel-directory-row")).toBe(true);
    expect(agentRow?.children).toHaveLength(3);
    const agentAvatar = agentRow?.querySelector<HTMLButtonElement>("button.channel-directory-avatar");
    expect(agentAvatar).not.toBeNull();
    expect(agentRow?.querySelector(".channel-directory-identity")?.textContent).toContain("Alpha");
    expect(agentRow?.querySelectorAll(".channel-directory-settings")).toHaveLength(1);
    expect(agentRow?.querySelector(".channel-agent-directory-actions")).toBeNull();
    act(() => agentAvatar?.click());
    expect(document.querySelector(".sidebar-name-dialog-title")?.textContent).toBe("编辑 Agent");

    act(() => agentSeparator?.dispatchEvent(new KeyboardEvent("keydown", { key: "Home", bubbles: true })));
    expect(agentSeparator?.getAttribute("aria-valuenow")).toBe("156");
  });

  it("tracks tasks across channels from the task section", async () => {
    const api = createApi();
    Object.defineProperty(window, "wuu", { configurable: true, value: api });
    root = createRoot(container);
    act(() => root?.render(<ChannelView section="tasks" />));
    await settle();

    expect(container.querySelector(".channel-task-table")?.textContent).toContain("Investigate flaky build");
    expect(container.querySelector(".channel-task-table")?.textContent).toContain("# general");
    expect(container.querySelector(".channel-conversation")).toBeNull();
    expect(container.querySelector(".channel-list-pane")).toBeNull();
    expect(api.listChannelMessages).toHaveBeenCalledWith({ room_id: "room-2", limit: 500 });
  });

  it("creates a named agent from the setup panel", async () => {
    const api = createApi();
    Object.defineProperty(window, "wuu", { configurable: true, value: api });

    root = createRoot(container);
    act(() => root?.render(<ChannelView section="agents" />));
    await settle();

    expect(container.querySelector(".channel-conversation")).toBeNull();
    const agentDirectory = container.querySelector(".channel-agent-directory");
    expect(agentDirectory?.classList.contains("channel-list-pane")).toBe(true);
    expect(agentDirectory?.textContent).toContain("Alpha");
    expect(container.querySelector(".channel-agent-directory .agent-avatar-image")).not.toBeNull();
    expect(container.querySelector('svg[aria-label="关系图谱"]')).not.toBeNull();
    expect(container.querySelectorAll(".channel-agent-graph-links line.relationship")).toHaveLength(1);
    expect(container.querySelectorAll(".channel-agent-graph-links line.membership")).toHaveLength(2);
    expect(container.querySelectorAll(".channel-agent-graph-node.agent")).toHaveLength(2);
    expect(container.querySelectorAll(".channel-agent-graph-node.room")).toHaveLength(1);
    expect(container.querySelector('button[aria-label="放大图谱"]')).not.toBeNull();
    const graphSettingsButton = container.querySelector<HTMLButtonElement>('button[aria-label="图谱设置"]');
    act(() => graphSettingsButton?.click());
    expect(container.querySelector(".channel-agent-graph-settings")?.textContent).toContain("节点斥力");
    const newAgentButton = container.querySelector<HTMLButtonElement>('button[aria-label="新建 Agent"]');
    act(() => newAgentButton?.click());
    const nameInput = document.querySelector<HTMLInputElement>(".channel-setup-form input:not([type])");
    expect(nameInput).not.toBeNull();
    act(() => setInputValue(nameInput!, "Beta"));
    const avatar = document.querySelector<HTMLButtonElement>('button[aria-label="选择头像 5"]');
    expect(avatar).not.toBeNull();
    expect(document.querySelector('button[aria-label="选择自定义头像图片"]')).not.toBeNull();
    expect(document.querySelector<HTMLInputElement>('.channel-avatar-file-input')?.accept).toBe("image/png,image/jpeg,image/webp");
    act(() => avatar?.click());
    const form = document.querySelector<HTMLFormElement>(".sidebar-name-dialog");
    await act(async () => form?.requestSubmit());

    expect(api.createNamedAgent).toHaveBeenCalledWith({
      name: "Beta",
      avatar_key: "abstract-5",
      avatar_image: "",
      provider_override: undefined,
      model_override: undefined,
    });
  });

  it("keeps agent deletion inside the shared settings dialog", async () => {
    const api = createApi();
    Object.defineProperty(window, "wuu", { configurable: true, value: api });
    root = createRoot(container);
    act(() => root?.render(<ChannelView section="agents" />));
    await settle();

    const settingsButton = container.querySelector<HTMLButtonElement>('.channel-agent-directory-row button[aria-label="编辑 Agent"]');
    act(() => settingsButton?.click());
    expect(document.querySelector(".sidebar-name-dialog-title")?.textContent).toBe("编辑 Agent");
    expect(container.querySelector('button[aria-label="删除 Agent"]')).toBeNull();

    const confirmDelete = vi.spyOn(window, "confirm").mockReturnValue(true);
    const deleteButton = document.querySelector<HTMLButtonElement>(".sidebar-name-dialog-destructive");
    await act(async () => deleteButton?.click());
    expect(confirmDelete).toHaveBeenCalledWith("删除“Alpha”？该 Agent 将从所有频道移除，其保存的状态也会被删除。");
    expect(api.deleteNamedAgent).toHaveBeenCalledWith({ agent_id: "agent-1" });
  });

  it("keeps invalid avatar feedback beside the avatar control", async () => {
    Object.defineProperty(window, "wuu", { configurable: true, value: createApi() });
    root = createRoot(container);
    act(() => root?.render(<ChannelView section="agents" />));
    await settle();

    act(() => container.querySelector<HTMLButtonElement>('button[aria-label="新建 Agent"]')?.click());
    const input = document.querySelector<HTMLInputElement>(".channel-avatar-file-input");
    expect(input).not.toBeNull();
    const oversizedImage = new File(["image"], "avatar.png", { type: "image/png" });
    Object.defineProperty(oversizedImage, "size", { configurable: true, value: 10 * 1024 * 1024 + 1 });
    Object.defineProperty(input!, "files", { configurable: true, value: [oversizedImage] });

    await act(async () => input?.dispatchEvent(new Event("change", { bubbles: true })));

    const dialog = document.querySelector(".sidebar-name-dialog");
    const alert = dialog?.querySelector<HTMLElement>("#channel-agent-avatar-error");
    const avatarButton = dialog?.querySelector<HTMLButtonElement>('button[aria-label="选择自定义头像图片"]');
    expect(alert?.textContent).toBe("请选择不超过 10 MB 的 PNG、JPEG 或 WebP 图片。");
    expect(avatarButton?.getAttribute("aria-invalid")).toBe("true");
    expect(avatarButton?.getAttribute("aria-describedby")).toBe(alert?.id);
    expect(container.querySelector(".channel-error")).toBeNull();
  });

  it("creates only a channel with selected agents", async () => {
    const api = createApi();
    Object.defineProperty(window, "wuu", { configurable: true, value: api });

    root = createRoot(container);
    act(() => root?.render(<ChannelView />));
    await settle();

    const newRoomButton = container.querySelector<HTMLButtonElement>('button[aria-label="新建频道"]');
    act(() => newRoomButton?.click());
    expect(document.querySelector(".channel-setup-form select")).toBeNull();
    const avatarPreview = document.querySelector<HTMLButtonElement>(".channel-room-avatar-preview");
    const avatarMedia = avatarPreview?.querySelector(".channel-room-avatar-media");
    expect(avatarMedia?.querySelector(".channel-group-avatar-grid, .channel-group-avatar-image")).not.toBeNull();
    expect(avatarMedia?.querySelector(".channel-room-avatar-badge")?.getAttribute("aria-hidden")).toBe("true");
    expect(avatarPreview?.querySelector(".channel-room-avatar-label")?.textContent).toBe("自定义群头像");
    const name = document.querySelector<HTMLInputElement>('.channel-setup-form input:not([type])');
    act(() => setInputValue(name!, "review"));
    expect(document.querySelector('.channel-setup-form input[type="checkbox"]')).toBeNull();
    const agent = document.querySelector<HTMLButtonElement>('.channel-member-picker-option[role="option"]');
    act(() => agent?.click());
    expect(agent?.getAttribute("aria-selected")).toBe("true");
    expect(api.createChannelRoom).not.toHaveBeenCalled();
    const form = document.querySelector<HTMLFormElement>(".sidebar-name-dialog");
    await act(async () => form?.requestSubmit());

    expect(api.createChannelRoom).toHaveBeenCalledWith({
      name: "review",
      agent_ids: ["agent-1"],
    });
  });

  it("manages channel members and deletes a channel from channel settings", async () => {
    const api = createApi();
    Object.defineProperty(window, "wuu", { configurable: true, value: api });
    root = createRoot(container);
    act(() => root?.render(<ChannelView />));
    await settle();

    const research = Array.from(container.querySelectorAll<HTMLButtonElement>(".channel-room-select"))
      .find((button) => button.textContent?.includes("research"));
    act(() => research?.click());
    await settle();
    const manageResearch = container.querySelector<HTMLButtonElement>('button[aria-label="管理 research"]');
    expect(manageResearch).not.toBeNull();
    act(() => manageResearch?.click());
    const detailsDialog = document.querySelector(".sidebar-name-dialog");
    expect(detailsDialog?.textContent).toContain("群聊详情");
    expect(document.querySelector(".sidebar-name-dialog-overlay-drawer")).not.toBeNull();
    expect(detailsDialog?.classList.contains("sidebar-name-dialog-drawer")).toBe(true);
    expect(detailsDialog?.textContent).toContain("群成员");
    expect(detailsDialog?.textContent).not.toContain("群公告");
    expect(detailsDialog?.querySelectorAll(".channel-room-member-card")).toHaveLength(0);
    const addMemberTrigger = detailsDialog?.querySelector<HTMLButtonElement>(".channel-room-member-add-trigger");
    expect(addMemberTrigger).not.toBeNull();
    act(() => addMemberTrigger?.click());
    const addAgent = document.querySelector<HTMLButtonElement>('.select-menu-item[data-value="agent-1"]');
    expect(addAgent).not.toBeNull();
    act(() => addAgent?.click());
    const saveButton = detailsDialog?.querySelector<HTMLButtonElement>('button[type="submit"]');
    await act(async () => saveButton?.click());

    expect(api.updateChannelRoom).toHaveBeenCalledWith({
      room_id: "room-2",
      name: "research",
      agent_ids: ["agent-1"],
    });

    expect(document.querySelector(".sidebar-name-dialog")).toBeNull();
    act(() => manageResearch?.click());
    const confirmDelete = vi.spyOn(window, "confirm").mockReturnValue(true);
    const deleteButton = document.querySelector<HTMLButtonElement>(".sidebar-name-dialog-destructive");
    expect(deleteButton?.textContent).toBe("删除频道");
    await act(async () => deleteButton?.click());
    expect(confirmDelete).toHaveBeenCalledWith("删除“research”？频道及其中的消息将被永久删除。");
    expect(api.deleteChannelRoom).toHaveBeenCalledWith({ room_id: "room-2" });
  });

  it("shows current members as avatar cards and supports removing one", async () => {
    const api = createApi();
    Object.defineProperty(window, "wuu", { configurable: true, value: api });
    root = createRoot(container);
    act(() => root?.render(<ChannelView />));
    await settle();

    const general = Array.from(container.querySelectorAll<HTMLButtonElement>(".channel-room-select"))
      .find((button) => button.textContent?.includes("general"));
    act(() => general?.click());
    await settle();
    act(() => container.querySelector<HTMLButtonElement>('button[aria-label="管理 general"]')?.click());

    const detailsDialog = document.querySelector(".sidebar-name-dialog");
    expect(detailsDialog?.querySelectorAll(".channel-room-member-card")).toHaveLength(2);
    const removeAlpha = detailsDialog?.querySelector<HTMLButtonElement>('[aria-label="移除 Alpha"]');
    expect(removeAlpha).not.toBeNull();
    act(() => removeAlpha?.click());
    await act(async () => document.querySelector<HTMLFormElement>(".sidebar-name-dialog")?.requestSubmit());

    expect(api.updateChannelRoom).toHaveBeenCalledWith({
      room_id: "room-1",
      name: "general",
      agent_ids: ["agent-2"],
    });
  });

  it("creates a task for a named agent", async () => {
    const api = createApi();
    Object.defineProperty(window, "wuu", { configurable: true, value: api });

    root = createRoot(container);
    act(() => root?.render(<ChannelView section="tasks" />));
    await settle();

    act(() => container.querySelector<HTMLButtonElement>(".channel-management-primary")?.click());
    const title = document.querySelector<HTMLInputElement>(".channel-setup-form input");
    expect(title).not.toBeNull();
    act(() => setInputValue(title!, "Investigate flaky build"));
    const form = document.querySelector<HTMLFormElement>(".sidebar-name-dialog");
    await act(async () => form?.requestSubmit());

    expect(api.createChannelTask).toHaveBeenCalledWith({
      room_id: "room-1",
      title: "Investigate flaky build",
      owner_id: "agent-1",
    });
  });
});
