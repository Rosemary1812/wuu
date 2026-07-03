/**
 * Tests for the group-thread member chips bar (docs/plans/
 * 2026-07-03-resident-named-agents.md §7.4). A thread with named
 * members shows them as inline ParticipantChips under the titlebar;
 * each chip carries a hover-revealed × that removes the member.
 */
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createElement } from "react";
import type { ParticipantSummary } from "../shared/protocol";
import { ThreadMembersBar } from "./ThreadMembersBar";

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

describe("ThreadMembersBar", () => {
  it("renders a chip per member", () => {
    const container = mount(
      createElement(ThreadMembersBar, { members, onRemove: () => {} }),
    );
    const chips = container.querySelectorAll(".participant-chip");
    expect(chips).toHaveLength(2);
    expect(container.textContent).toContain("小青");
    expect(container.textContent).toContain("阿蓝");
  });

  it("renders nothing when there are no members", () => {
    const container = mount(
      createElement(ThreadMembersBar, { members: [], onRemove: () => {} }),
    );
    expect(container.querySelector(".thread-members-bar")).toBeNull();
  });

  it("invokes onRemove with the participant id when × is clicked", () => {
    const onRemove = vi.fn();
    const container = mount(
      createElement(ThreadMembersBar, { members, onRemove }),
    );
    const removeButton = container.querySelector<HTMLButtonElement>(
      "button[aria-label=\"将 阿蓝 移出会话\"]",
    );
    expect(removeButton).not.toBeNull();
    act(() => {
      removeButton?.click();
    });
    expect(onRemove).toHaveBeenCalledWith("participant-2");
  });

  it("omits remove buttons when onRemove is not provided", () => {
    const container = mount(createElement(ThreadMembersBar, { members }));
    expect(container.querySelectorAll(".thread-members-remove")).toHaveLength(0);
  });
});
