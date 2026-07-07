import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it } from "vitest";
import { ReadReceiptRing } from "./ReadReceiptRing";
import { MessageReactions } from "./MessageReactions";
import { aggregateMarksBySeq, ringModel, type MessageMark } from "./MessageMarks";

let mountedRoots: Root[] = [];

function render(el: React.ReactNode): HTMLElement {
  const container = document.createElement("div");
  document.body.appendChild(container);
  const root = createRoot(container);
  mountedRoots.push(root);
  act(() => {
    root.render(el);
  });
  return container;
}

afterEach(() => {
  act(() => {
    for (const root of mountedRoots) root.unmount();
  });
  mountedRoots = [];
  document.body.innerHTML = "";
});

const resolve = (id: string) => ({ a: "Andy", b: "le", c: "Cara" })[id] ?? id;

describe("ReadReceiptRing", () => {
  it("marks all-seen and labels the members when everyone finished", () => {
    const seen = { completed: ["a", "b"], inProgress: [], failed: [] };
    const el = render(
      createElement(ReadReceiptRing, { ring: ringModel(seen, 2), seen, resolveName: resolve }),
    );
    const ring = el.querySelector(".chat-receipt-ring")!;
    expect(ring.getAttribute("data-all-seen")).toBe("true");
    expect(ring.getAttribute("aria-label")).toContain("已读 2/2");
    expect(el.querySelector(".chat-receipt-ring-done")).not.toBeNull();
    // The custom hover tooltip carries the breakdown + member names.
    const tooltip = ring.querySelector(".chat-receipt-tooltip")!;
    expect(tooltip).not.toBeNull();
    expect(tooltip.textContent).toContain("已读 2/2");
    expect(tooltip.textContent).toContain("Andy");
    expect(tooltip.textContent).toContain("le");
  });

  it("renders a failed segment and never claims all-seen when one crashed", () => {
    const seen = { completed: ["a"], inProgress: [], failed: ["b"] };
    const el = render(
      createElement(ReadReceiptRing, { ring: ringModel(seen, 2), seen, resolveName: resolve }),
    );
    const ring = el.querySelector(".chat-receipt-ring")!;
    expect(ring.getAttribute("data-failed")).toBe("true");
    expect(ring.getAttribute("data-all-seen")).toBeNull();
    expect(el.querySelector(".chat-receipt-ring-failed")).not.toBeNull();
    // Failed members get their own tooltip row.
    const failRow = ring.querySelector('.chat-receipt-tooltip-row[data-kind="fail"]')!;
    expect(failRow).not.toBeNull();
    expect(failRow.textContent).toContain("未完成");
    expect(failRow.textContent).toContain("le");
  });

  it("renders nothing when there is no readership", () => {
    const seen = { completed: [], inProgress: [], failed: [] };
    const el = render(createElement(ReadReceiptRing, { ring: ringModel(seen, 0), seen }));
    expect(el.querySelector(".chat-receipt-ring")).toBeNull();
  });
});

describe("MessageReactions", () => {
  it("renders one chip per reaction with a count and hover names", () => {
    const marks: MessageMark[] = [
      { seq: 1, participant_id: "a", kind: "reaction", reaction: "shrug" },
      { seq: 1, participant_id: "b", kind: "reaction", reaction: "shrug" },
      { seq: 1, participant_id: "c", kind: "reaction", reaction: "smug" },
    ];
    const view = aggregateMarksBySeq(marks).get(1)!;
    const el = render(
      createElement(MessageReactions, { reactions: view.reactions, resolveName: resolve }),
    );
    const chips = el.querySelectorAll(".chat-reaction-chip");
    expect(chips).toHaveLength(2);
    const shrug = el.querySelector('[data-reaction="shrug"]')!;
    const shrugGlyph = shrug.querySelector("img.chat-reaction-glyph")!;
    expect(shrugGlyph.getAttribute("src")).toBeTruthy();
    expect(shrugGlyph.getAttribute("alt")).toBe("无所谓");
    expect(shrug.querySelector(".chat-reaction-count")!.textContent).toBe("2");
    expect(shrug.getAttribute("title")).toBe("无所谓 · Andy、le");
    // A single reactor shows no count.
    const smug = el.querySelector('[data-reaction="smug"]')!;
    expect(smug.querySelector(".chat-reaction-count")).toBeNull();
  });

  it("renders nothing with no reactions", () => {
    const el = render(createElement(MessageReactions, { reactions: [] }));
    expect(el.querySelector(".chat-reactions")).toBeNull();
  });
});
