import { createElement } from "react";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ParticipantSummary } from "../shared/protocol";
import { GroupMembersCapsule } from "./GroupMembersCapsule";

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
  { id: "participant-3", name: "小紫", kind: "named" },
  { id: "participant-4", name: "小白", kind: "named" },
];

describe("GroupMembersCapsule", () => {
  it("renders the first three member avatars and an overflow ellipsis", () => {
    const container = mount(createElement(GroupMembersCapsule, { members }));

    expect(container.querySelectorAll(".group-members-capsule-avatar")).toHaveLength(3);
    expect(container.querySelector(".group-members-capsule-more")?.textContent).toBe("…");
    expect(container.querySelector(".group-members-capsule-label")?.textContent).toBe("成员 4");
    expect(container.querySelector(".group-members-capsule-detail")?.textContent).toContain("小青、阿蓝、小紫、小白");
  });

  it("renders avatar images when summaries carry uploaded avatars", () => {
    const dataUrl = "data:image/png;base64,iVBORw0KGgo=";
    const container = mount(
      createElement(GroupMembersCapsule, {
        members: [
          { id: "participant-5", name: "小橙", kind: "named", avatar_image: dataUrl },
        ],
      }),
    );

    const img = container.querySelector<HTMLImageElement>(
      ".group-members-capsule-avatar img",
    );
    expect(img).not.toBeNull();
    expect(img!.getAttribute("src")).toBe(dataUrl);
  });

  it("marks busy members on avatar status dots", () => {
    const container = mount(
      createElement(GroupMembersCapsule, {
        members,
        busyParticipantIDs: new Set(["participant-2"]),
      }),
    );

    const dots = container.querySelectorAll(".group-members-capsule-status");
    expect(dots).toHaveLength(3);
    expect(dots[0]?.getAttribute("data-status")).toBe("online");
    expect(dots[1]?.getAttribute("data-status")).toBe("busy");
    expect(dots[2]?.getAttribute("data-status")).toBe("online");
    expect(
      container
        .querySelector(".group-members-capsule")
        ?.getAttribute("aria-label"),
    ).toContain("阿蓝（正在响应）");
  });

  it("opens group info when clicked", () => {
    const onOpen = vi.fn();
    const container = mount(
      createElement(GroupMembersCapsule, { members, onOpen }),
    );
    const button = container.querySelector<HTMLButtonElement>(
      ".group-members-capsule",
    );

    act(() => {
      button?.click();
    });

    expect(onOpen).toHaveBeenCalledTimes(1);
  });
});
