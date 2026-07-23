import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { ChannelRoom, NamedAgent, WuuDesktopApi } from "../shared/protocol";
import { ChannelView } from "./ChannelView";

let container: HTMLDivElement;
let root: Root | null = null;

const agents: NamedAgent[] = [
  {
    id: "agent-1",
    name: "Alpha",
    memory_dir: "/agents/agent-1/memory",
    model_override: "",
    autostart: true,
    created_at: "2026-07-23T00:00:00Z",
  },
];

const rooms: ChannelRoom[] = [
  {
    id: "room-1",
    name: "general",
    kind: "channel",
    created_by: "human",
    members: [{ room_id: "room-1", member_type: "agent", member_id: "agent-1", joined_at: "2026-07-23T00:00:00Z" }],
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
    listNamedAgents: vi.fn(async () => ({ agents })),
    createNamedAgent: vi.fn(async (params) => ({ agent: { ...agents[0], name: params.name } })),
    startNamedAgent: vi.fn(async () => ({ agent: agents[0] })),
    listChannelRooms: vi.fn(async () => ({ rooms })),
    createChannelRoom: vi.fn(async (params) => ({ room: { ...rooms[1], name: params.name } })),
    listChannelMessages: vi.fn(async ({ room_id }) => ({
      messages: room_id === "room-1"
        ? [{
            id: "message-1",
            room_id,
            seq: 1,
            author_type: "agent" as const,
            author_id: "agent-1",
            kind: "text" as const,
            body: "Hello from Alpha",
            created_at: "2026-07-23T00:00:00Z",
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
  it("loads rooms, selects a room, and sends a human message", async () => {
    const api = createApi();
    Object.defineProperty(window, "wuu", { configurable: true, value: api });

    root = createRoot(container);
    act(() => root?.render(<ChannelView />));
    await settle();

    expect(container.textContent).toContain("Hello from Alpha");
    const research = Array.from(container.querySelectorAll<HTMLButtonElement>(".channel-room-row"))
      .find((button) => button.textContent?.includes("research"));
    act(() => research?.click());
    await settle();
    expect(api.listChannelMessages).toHaveBeenCalledWith({ room_id: "room-2", limit: 500 });

    const textarea = container.querySelector<HTMLTextAreaElement>(".channel-composer textarea");
    expect(textarea).not.toBeNull();
    act(() => setInputValue(textarea!, "Ask Alpha"));
    const form = container.querySelector<HTMLFormElement>(".channel-composer");
    await act(async () => form?.requestSubmit());

    expect(api.sendChannelMessage).toHaveBeenCalledWith({ room_id: "room-2", body: "Ask Alpha" });
  });

  it("creates a named agent from the setup panel", async () => {
    const api = createApi();
    Object.defineProperty(window, "wuu", { configurable: true, value: api });

    root = createRoot(container);
    act(() => root?.render(<ChannelView />));
    await settle();

    const agentButton = container.querySelector<HTMLButtonElement>(".channel-agent-button");
    act(() => agentButton?.click());
    const nameInput = container.querySelector<HTMLInputElement>(".channel-setup-form input:not([type])");
    expect(nameInput).not.toBeNull();
    act(() => setInputValue(nameInput!, "Beta"));
    const form = container.querySelector<HTMLFormElement>(".channel-setup-form");
    await act(async () => form?.requestSubmit());

    expect(api.createNamedAgent).toHaveBeenCalledWith({
      name: "Beta",
      model_override: undefined,
      autostart: true,
    });

    act(() => container.querySelector<HTMLButtonElement>(".channel-agent-button")?.click());
    const startButton = container.querySelector<HTMLButtonElement>(".channel-agent-row button");
    await act(async () => startButton?.click());
    expect(api.startNamedAgent).toHaveBeenCalledWith({ agent_id: "agent-1" });
  });

  it("creates a direct message with exactly one agent and toggles system notifications", async () => {
    const api = createApi();
    Object.defineProperty(window, "wuu", { configurable: true, value: api });

    root = createRoot(container);
    act(() => root?.render(<ChannelView />));
    await settle();

    const notificationButton = container.querySelector<HTMLButtonElement>('button[aria-pressed]');
    expect(notificationButton?.getAttribute("aria-pressed")).toBe("false");
    act(() => notificationButton?.click());
    expect(window.localStorage.getItem("wuu.channels.systemNotifications")).toBe("true");

    const headingButtons = container.querySelectorAll<HTMLButtonElement>(".channel-heading-actions button");
    act(() => headingButtons.item(1).click());
    const kind = container.querySelector<HTMLSelectElement>(".channel-setup-form select");
    expect(kind).not.toBeNull();
    act(() => setSelectValue(kind!, "dm"));
    const agent = container.querySelector<HTMLInputElement>('.channel-setup-form input[type="radio"]');
    act(() => agent?.click());
    const form = container.querySelector<HTMLFormElement>(".channel-setup-form");
    await act(async () => form?.requestSubmit());

    expect(api.createChannelRoom).toHaveBeenCalledWith({
      name: "Alpha",
      kind: "dm",
      agent_ids: ["agent-1"],
    });
  });

  it("creates a task for a named agent", async () => {
    const api = createApi();
    Object.defineProperty(window, "wuu", { configurable: true, value: api });

    root = createRoot(container);
    act(() => root?.render(<ChannelView />));
    await settle();

    act(() => container.querySelector<HTMLButtonElement>(".channel-task-create-button")?.click());
    const title = container.querySelector<HTMLInputElement>(".channel-setup-form input");
    expect(title).not.toBeNull();
    act(() => setInputValue(title!, "Investigate flaky build"));
    const form = container.querySelector<HTMLFormElement>(".channel-setup-form");
    await act(async () => form?.requestSubmit());

    expect(api.createChannelTask).toHaveBeenCalledWith({
      room_id: "room-1",
      title: "Investigate flaky build",
      owner_id: "agent-1",
    });
  });
});
