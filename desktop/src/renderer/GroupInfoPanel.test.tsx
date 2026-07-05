import { createElement, createRef } from "react";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import type {
  ParticipantProfile,
  ParticipantSummary,
  Thread,
} from "../shared/protocol";
import { GroupInfoPanel } from "./GroupInfoPanel";

let mountedRoots: Root[] = [];
let mountedContainers: HTMLElement[] = [];

function mount(element: React.ReactElement): HTMLElement {
  const container = document.createElement("div");
  document.body.appendChild(container);
  const root = createRoot(container);
  act(() => {
    root.render(element);
  });
  mountedRoots.push(root);
  mountedContainers.push(container);
  return container;
}

afterEach(() => {
  for (const root of mountedRoots) {
    act(() => {
      root.unmount();
    });
  }
  for (const container of mountedContainers) {
    container.remove();
  }
  mountedRoots = [];
  mountedContainers = [];
});

const members: ParticipantSummary[] = [
  { id: "participant-1", name: "小青", kind: "named", role: "评审" },
  { id: "participant-2", name: "阿蓝", kind: "named" },
];

const participants: ParticipantProfile[] = [
  { id: "participant-1", name: "小青", kind: "named", role: "评审" },
  { id: "participant-2", name: "阿蓝", kind: "named" },
  { id: "participant-3", name: "小紫", kind: "named", role: "实现" },
];

function groupThread(overrides: Partial<Thread> = {}): Thread {
  return {
    id: "thread-group",
    preview: "讨论群聊信息面板",
    title: "前端小队",
    model_provider: "test",
    model: "test-model",
    cwd: "/repo",
    status: "idle",
    group: true,
    created_at: "2026-07-04T00:00:00Z",
    updated_at: "2026-07-04T00:00:00Z",
    turns: [],
    members,
    ...overrides,
  };
}

function renderPanel(
  props: Partial<Parameters<typeof GroupInfoPanel>[0]> = {},
): HTMLElement {
  return mount(
    createElement(GroupInfoPanel, {
      panelRef: createRef<HTMLDivElement>(),
      motionState: "open",
      thread: groupThread(),
      members,
      participants,
      onClose: () => {},
      ...props,
    }),
  );
}

describe("GroupInfoPanel", () => {
  it("renders only the group name and full member list", () => {
    const container = renderPanel();

    expect(container.querySelector("h2")?.textContent).toBe("群聊信息");
    expect(container.textContent).toContain("前端小队");
    expect(container.textContent).not.toContain("群内容");
    expect(container.textContent).not.toContain("讨论群聊信息面板");
    expect(container.querySelectorAll(".group-info-member-row")).toHaveLength(2);
    expect(container.textContent).toContain("小青");
    expect(container.textContent).toContain("阿蓝");
  });

  it("keeps member-list avatars free of agent status dots", () => {
    const container = renderPanel();

    const dots = container.querySelectorAll(".group-info-avatar-status");
    expect(dots).toHaveLength(0);
    expect(
      container
        .querySelectorAll(".group-info-avatar")[1]
        ?.getAttribute("aria-label"),
    ).toBeNull();
  });

  it("opens an add-member list and invokes onAddMember for non-members", async () => {
    const onAddMember = vi.fn().mockResolvedValue(undefined);
    const container = renderPanel({ onAddMember });
    const addButton = container.querySelector<HTMLButtonElement>(
      ".group-info-add-button",
    );

    act(() => {
      addButton?.click();
    });

    expect(container.querySelectorAll(".group-info-add-row")).toHaveLength(1);
    const addRow = container.querySelector<HTMLButtonElement>(
      ".group-info-add-row",
    );
    expect(addRow?.textContent).toContain("小紫");

    await act(async () => {
      addRow?.click();
      await Promise.resolve();
    });

    expect(onAddMember).toHaveBeenCalledWith("participant-3");
  });

  it("invokes onRemoveMember from a member row", () => {
    const onRemoveMember = vi.fn();
    const container = renderPanel({ onRemoveMember });
    const removeButton = container.querySelector<HTMLButtonElement>(
      "button[aria-label=\"将 阿蓝 移出群聊\"]",
    );

    act(() => {
      removeButton?.click();
    });

    expect(onRemoveMember).toHaveBeenCalledWith("participant-2");
  });

  it("allows explicit all-channel membership edits", async () => {
    const onAddMember = vi.fn().mockResolvedValue(undefined);
    const onRemoveMember = vi.fn();
    const container = renderPanel({
      thread: groupThread({ title: "all" }),
      onAddMember,
      onRemoveMember,
    });

    expect(container.textContent).toContain("all");
    expect(container.textContent).not.toContain("#all");
    const addButton = container.querySelector<HTMLButtonElement>(
      ".group-info-add-button",
    );
    expect(addButton).not.toBeNull();
    act(() => {
      addButton?.click();
    });
    const addRow = container.querySelector<HTMLButtonElement>(
      ".group-info-add-row",
    );
    await act(async () => {
      addRow?.click();
      await Promise.resolve();
    });
    expect(onAddMember).toHaveBeenCalledWith("participant-3");

    const removeButton = container.querySelector<HTMLButtonElement>(
      "button[aria-label=\"将 阿蓝 移出群聊\"]",
    );
    act(() => {
      removeButton?.click();
    });
    expect(onRemoveMember).toHaveBeenCalledWith("participant-2");
  });
});
