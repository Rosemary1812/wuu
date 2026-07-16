/**
 * Tests for `ParticipantChip`.
 *
 * Contract: with a `ParticipantSummary` the chip renders name and role
 * spans (skipping the role when empty). Without one it falls back to
 * legacy `Agent` fields: name comes from fallbackTaskName, then
 * fallbackType, then the literal "agent"; the role is the fallbackType
 * (suppressed when it would duplicate the name). The 16px avatar cell
 * is always present: an <img> when the summary carries avatar_image,
 * otherwise a generated geometric default avatar (see DefaultAvatar).
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
};

describe("ParticipantChip", () => {
  it("renders name and role from the participant summary", () => {
    mount({ participant: reviewer });
    expect(chip()).not.toBeNull();
    expect(spanText(".participant-chip-name")).toBe("Reviewer·auth");
    expect(spanText(".participant-chip-role")).toBe("审查");
    expect(chip()!.textContent).toContain("Reviewer·auth");
    expect(chip()!.textContent).toContain("审查");
  });

  it("renders the avatar image when the summary carries avatar_image", () => {
    const dataUrl = "data:image/png;base64,iVBORw0KGgo=";
    mount({ participant: { ...reviewer, avatar_image: dataUrl } });
    const img = chip()!.querySelector<HTMLImageElement>(
      ".participant-chip-avatar img",
    );
    expect(img).not.toBeNull();
    expect(img!.getAttribute("src")).toBe(dataUrl);
    expect(spanText(".participant-chip-avatar")).toBe("");
  });

  it("falls back to a generated default avatar when there is no avatar image", () => {
    mount({ participant: reviewer });
    // No uploaded image (a direct <img> child); the mascot art inside
    // .default-avatar is the generated fallback.
    expect(chip()!.querySelector(".participant-chip-avatar > img")).toBeNull();
    expect(
      chip()!.querySelector(".participant-chip-avatar .default-avatar img"),
    ).not.toBeNull();
  });

  it("omits the role span when the summary lacks it", () => {
    mount({
      participant: { id: "agent-2", name: "Explorer", kind: "task_worker" },
    });
    expect(spanText(".participant-chip-name")).toBe("Explorer");
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
    expect(chip()!.querySelector(".participant-chip-avatar > img")).toBeNull();
    expect(
      chip()!.querySelector(".participant-chip-avatar .default-avatar"),
    ).not.toBeNull();
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
    expect(
      chip()!.querySelector(".participant-chip-avatar .default-avatar"),
    ).not.toBeNull();
    expect(chip()!.querySelector(".participant-chip-role")).toBeNull();
  });

  it("badges a forked agent with a 分身 pill carrying the母体 name tooltip", () => {
    mount({
      participant: { ...reviewer, forked_from_id: "agent-mother" },
      resolveName: (id) => (id === "agent-mother" ? "小青" : id),
    });
    const fork = chip()!.querySelector(".participant-chip-fork");
    expect(fork).not.toBeNull();
    expect(fork!.textContent).toBe("分身");
    expect(fork!.getAttribute("title")).toBe("小青 的分身");
  });

  it("degrades the 分身 tooltip to the id when no resolver is supplied", () => {
    mount({ participant: { ...reviewer, forked_from_id: "agent-mother" } });
    const fork = chip()!.querySelector(".participant-chip-fork");
    expect(fork).not.toBeNull();
    expect(fork!.getAttribute("title")).toBe("agent-mother 的分身");
  });

  it("omits the 分身 pill for an ordinary named agent", () => {
    mount({ participant: reviewer });
    expect(chip()!.querySelector(".participant-chip-fork")).toBeNull();
  });
});
