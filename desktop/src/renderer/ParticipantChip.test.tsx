/**
 * Tests for `ParticipantChip`.
 *
 * Contract: with a `ParticipantSummary` the chip renders avatar, name
 * and role spans (skipping avatar/role when empty). Without one it
 * falls back to legacy `Agent` fields: name comes from fallbackTaskName,
 * then fallbackType, then the literal "agent"; no avatar is rendered and
 * the role is the fallbackType (suppressed when it would duplicate the
 * name).
 */
import { afterEach, describe, expect, it } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { ParticipantChip } from "./ParticipantChip";
import type { ParticipantSummary } from "../shared/protocol";

let container: HTMLDivElement | null = null;
let root: Root | null = null;

function mount(props: Parameters<typeof ParticipantChip>[0]): void {
  if (container) unmount();
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  act(() => {
    root!.render(<ParticipantChip {...props} />);
  });
}

function unmount(): void {
  if (root) {
    act(() => {
      root!.unmount();
    });
    root = null;
  }
  if (container) {
    container.remove();
    container = null;
  }
}

function chip(): HTMLElement | null {
  return container?.querySelector(".participant-chip") ?? null;
}

function spanText(selector: string): string | null {
  const node = container?.querySelector(selector);
  return node ? (node.textContent ?? "") : null;
}

afterEach(() => {
  unmount();
});

const reviewer: ParticipantSummary = {
  id: "agent-1",
  name: "Reviewer·auth",
  kind: "task_worker",
  role: "reviewer",
  avatar: "🧐",
};

describe("ParticipantChip", () => {
  it("renders avatar, name and role from the participant summary", () => {
    mount({ participant: reviewer });
    expect(chip()).not.toBeNull();
    expect(spanText(".participant-chip-avatar")).toBe("🧐");
    expect(spanText(".participant-chip-name")).toBe("Reviewer·auth");
    expect(spanText(".participant-chip-role")).toBe("reviewer");
    expect(chip()!.textContent).toContain("Reviewer·auth");
    expect(chip()!.textContent).toContain("reviewer");
  });

  it("omits the avatar and role spans when the summary lacks them", () => {
    mount({
      participant: { id: "agent-2", name: "Explorer", kind: "task_worker" },
    });
    expect(spanText(".participant-chip-name")).toBe("Explorer");
    expect(chip()!.querySelector(".participant-chip-avatar")).toBeNull();
    expect(chip()!.querySelector(".participant-chip-role")).toBeNull();
  });

  it("applies the sm modifier only when size is sm", () => {
    mount({ participant: reviewer, size: "sm" });
    expect(chip()!.classList.contains("participant-chip--sm")).toBe(true);
    mount({ participant: reviewer });
    expect(chip()!.classList.contains("participant-chip--sm")).toBe(false);
  });

  it("falls back to the task name and type when participant is missing", () => {
    mount({ fallbackType: "explore", fallbackTaskName: "检查左侧树" });
    expect(chip()!.querySelector(".participant-chip-avatar")).toBeNull();
    expect(spanText(".participant-chip-name")).toBe("检查左侧树");
    expect(spanText(".participant-chip-role")).toBe("explore");
  });

  it("uses the type as the name (without a duplicate role) when only the type exists", () => {
    mount({ fallbackType: "explore" });
    expect(spanText(".participant-chip-name")).toBe("explore");
    expect(chip()!.querySelector(".participant-chip-role")).toBeNull();
  });

  it("shows the literal agent when there is no identity at all", () => {
    mount({});
    expect(spanText(".participant-chip-name")).toBe("agent");
    expect(chip()!.querySelector(".participant-chip-avatar")).toBeNull();
    expect(chip()!.querySelector(".participant-chip-role")).toBeNull();
  });
});
