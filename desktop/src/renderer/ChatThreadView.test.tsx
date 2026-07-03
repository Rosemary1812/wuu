/**
 * Tests for ChatThreadView, the chat-style stream for DM and group
 * threads (docs/plans/2026-07-03-chat-style-threads-design.md §2, §4).
 * The view flattens turns through the whitelist filter in
 * chatMessagesFromTurns — this test asserts on the rendered DOM only,
 * mirroring the render-harness pattern of EnvelopeNotice.test.tsx.
 */
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it } from "vitest";
import { createElement } from "react";
import type { ParticipantSummary, Turn } from "../shared/protocol";
import { ChatThreadView } from "./ChatThreadView";

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

const noel: ParticipantSummary = {
  id: "prt-noel",
  name: "Noel",
  kind: "resident",
  avatar: "🐧",
};

function turns(items: Turn["items"]): ReadonlyArray<Pick<Turn, "id" | "items">> {
  return [{ id: "turn-1", items }];
}

describe("ChatThreadView", () => {
  it("renders a participant row with emoji avatar, name, and bubble text", () => {
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([
          {
            id: "item-1",
            type: "participant_message",
            text: "早上好",
            post_kind: "result",
            participant: noel,
          },
        ]),
        typingParticipants: [],
      }),
    );
    const row = container.querySelector(".chat-row");
    expect(row).not.toBeNull();
    expect(container.querySelector(".chat-avatar")?.textContent).toBe("🐧");
    expect(container.querySelector(".chat-sender-name")?.textContent).toBe("Noel");
    expect(container.querySelector(".chat-bubble")?.textContent).toContain("早上好");
  });

  it("falls back to the first character of the name when there is no emoji avatar", () => {
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([
          {
            id: "item-1",
            type: "participant_message",
            text: "hi",
            post_kind: "result",
            participant: { id: "prt-x", name: "小青", kind: "resident" },
          },
        ]),
        typingParticipants: [],
      }),
    );
    expect(container.querySelector(".chat-avatar")?.textContent).toBe("小");
  });

  it("renders an <img> when avatar_image is a data URL", () => {
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([
          {
            id: "item-1",
            type: "participant_message",
            text: "hi",
            post_kind: "result",
            participant: {
              id: "prt-x",
              name: "小青",
              kind: "resident",
              avatar_image: "data:image/png;base64,AAAA",
            },
          },
        ]),
        typingParticipants: [],
      }),
    );
    const img = container.querySelector(".chat-avatar img");
    expect(img).not.toBeNull();
    expect(img?.getAttribute("src")).toBe("data:image/png;base64,AAAA");
  });

  it("renders user rows right-aligned with no avatar", () => {
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([
          { id: "item-1", type: "user_message", text: "明天上线吗" },
        ]),
        typingParticipants: [],
      }),
    );
    const row = container.querySelector(".chat-row--user");
    expect(row).not.toBeNull();
    expect(row?.querySelector(".chat-avatar")).toBeNull();
    expect(row?.textContent).toContain("明天上线吗");
  });

  it("renders envelope rows via EnvelopeNotice", () => {
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([
          {
            id: "item-1",
            type: "user_message",
            text: "信封正文",
            envelope_meta: [
              { source_thread_id: "thread-x", addressed: true, hop: 1 },
            ],
          },
        ]),
        typingParticipants: [],
      }),
    );
    expect(container.querySelector(".envelope-notice")).not.toBeNull();
  });

  it("renders a decline post_kind as a muted line, not a bubble", () => {
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([
          {
            id: "item-1",
            type: "participant_message",
            text: "无需回应",
            post_kind: "decline",
            participant: noel,
          },
        ]),
        typingParticipants: [],
      }),
    );
    expect(container.querySelector(".chat-decline-line")).not.toBeNull();
    expect(container.querySelector(".chat-bubble")).toBeNull();
  });

  it("renders a typing row per typing participant with an aria-label", () => {
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([]),
        typingParticipants: [noel],
      }),
    );
    const typingRow = container.querySelector(".chat-typing-row");
    expect(typingRow).not.toBeNull();
    expect(typingRow?.getAttribute("aria-label")).toBe("Noel 正在输入");
    expect(container.querySelectorAll(".chat-typing-dot").length).toBe(3);
  });

  it("never renders transcript-only items (agent_message)", () => {
    const container = mount(
      createElement(ChatThreadView, {
        turns: turns([
          { id: "item-1", type: "agent_message", text: "SECRET-TRANSCRIPT-TEXT" },
        ]),
        typingParticipants: [],
      }),
    );
    expect(container.textContent).not.toContain("SECRET-TRANSCRIPT-TEXT");
  });
});
