import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { ChannelRoom, NamedAgent, WuuDesktopApi } from "../shared/protocol";
import { graphDensityScale } from "./AgentRelationshipGraph";
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
});

describe("ChannelView", () => {
  it("scales graph nodes down within a bounded range as the graph grows", () => {
    expect(graphDensityScale(2)).toBe(1.35);
    expect(graphDensityScale(12)).toBeLessThan(graphDensityScale(4));
    expect(graphDensityScale(10_000)).toBe(0.68);
  });

  it("loads rooms, selects a room, and sends a human message", async () => {
    const api = createApi();
    Object.defineProperty(window, "wuu", { configurable: true, value: api });

    root = createRoot(container);
    act(() => root?.render(<ChannelView />));
    await settle();

    expect(container.querySelector(".channel-conversation-heading")).toBeNull();
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
    expect(container.querySelector(".channel-message.own .channel-agent-avatar")).toBeNull();
    expect(container.querySelector(".channel-task-card")).toBeNull();
    expect(container.querySelector(".channel-message-stream")?.textContent).not.toContain("Investigate flaky build");
    expect(container.querySelector('[aria-label="Alpha: 处理中"]')).not.toBeNull();
    expect(container.querySelector(".channel-agent-status-dot.thinking")).not.toBeNull();
    const research = Array.from(container.querySelectorAll<HTMLButtonElement>(".channel-room-row"))
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

    act(() => root?.render(<ChannelView section="agents" />));
    await settle();
    const agentSeparator = container.querySelector<HTMLButtonElement>(".channel-split-resizer");
    expect(agentSeparator?.getAttribute("aria-valuenow")).toBe("224");
    expect(container.querySelector<HTMLElement>(".channel-agent-workspace")?.style.gridTemplateColumns).toContain("224px");

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
    expect(container.querySelector(".channel-list-pane")).toBeNull();
    expect(container.querySelector(".channel-agent-directory")?.textContent).toContain("Alpha");
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

  it("creates only a channel with selected agents", async () => {
    const api = createApi();
    Object.defineProperty(window, "wuu", { configurable: true, value: api });

    root = createRoot(container);
    act(() => root?.render(<ChannelView />));
    await settle();

    const newRoomButton = container.querySelector<HTMLButtonElement>('button[aria-label="新建频道"]');
    act(() => newRoomButton?.click());
    expect(document.querySelector(".channel-setup-form select")).toBeNull();
    const name = document.querySelector<HTMLInputElement>('.channel-setup-form input:not([type])');
    act(() => setInputValue(name!, "review"));
    const agent = document.querySelector<HTMLInputElement>('.channel-setup-form input[type="checkbox"]');
    act(() => agent?.click());
    const form = document.querySelector<HTMLFormElement>(".sidebar-name-dialog");
    await act(async () => form?.requestSubmit());

    expect(api.createChannelRoom).toHaveBeenCalledWith({
      name: "review",
      agent_ids: ["agent-1"],
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
