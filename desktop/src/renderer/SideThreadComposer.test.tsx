import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SideThreadComposer } from "./SideThreadComposer";

let container: HTMLDivElement;
let root: Root | null = null;

beforeEach(() => {
  container = document.createElement("div");
  document.body.appendChild(container);
});

afterEach(() => {
  act(() => root?.unmount());
  root = null;
  container.remove();
});

function renderComposer({
  draft = "",
  running = false,
  disabledReason,
  onChangeDraft = () => {},
  onSend = () => {},
  onInterrupt = () => {},
  onReset = () => {},
}: {
  draft?: string;
  running?: boolean;
  disabledReason?: string;
  onChangeDraft?: (draft: string) => void;
  onSend?: (prompt: string) => void;
  onInterrupt?: () => void;
  onReset?: () => void;
} = {}): void {
  act(() => {
    root = createRoot(container);
    root.render(
      <SideThreadComposer
        draft={draft}
        running={running}
        disabledReason={disabledReason}
        queryHistorySessionID="side-1"
        queryHistory={[]}
        onChangeDraft={onChangeDraft}
        onSend={onSend}
        onInterrupt={onInterrupt}
        onReset={onReset}
      />,
    );
  });
}

describe("SideThreadComposer", () => {
  it("uses the canonical composer without unsupported text-only controls", () => {
    renderComposer({ draft: " status? " });

    expect(container.querySelector(".composer-wrap")).toBeTruthy();
    expect(container.querySelector(".composer-attachment-button")).toBeNull();
    expect(container.querySelector(".composer-slash-button")).toBeNull();
    expect(container.querySelector(".permission-chip")).toBeNull();
    expect(container.querySelector("textarea")?.getAttribute("placeholder")).toBe(
      "询问当前任务，不会加入主对话",
    );
  });

  it("sends the trimmed draft through the side transport", () => {
    const onSend = vi.fn();
    renderComposer({ draft: " status? ", onSend });

    act(() => {
      (container.querySelector('.composer-action-button[aria-label="发送"]') as HTMLButtonElement).click();
    });

    expect(onSend).toHaveBeenCalledWith("status?");
  });

  it("offers only the side /reset slash command and dispatches it", () => {
    const onReset = vi.fn();
    const onSend = vi.fn();
    renderComposer({ draft: "/reset", onReset, onSend });

    const items = Array.from(
      container.querySelectorAll<HTMLButtonElement>(".slash-command-item"),
    );
    expect(items.map((item) => item.textContent)).toHaveLength(1);
    expect(items[0]?.textContent).toContain("重置侧聊");

    act(() => items[0]?.click());
    expect(onReset).toHaveBeenCalledTimes(1);
    expect(onSend).not.toHaveBeenCalled();
  });

  it("switches the shared composer to stop-only mode while running", () => {
    const onInterrupt = vi.fn();
    renderComposer({ draft: "keep this", running: true, onInterrupt });

    const textarea = container.querySelector("textarea") as HTMLTextAreaElement;
    expect(textarea.disabled).toBe(true);
    expect(textarea.value).toBe("");
    const stop = container.querySelector(
      '.composer-action-button[aria-label="停止"]',
    ) as HTMLButtonElement;
    expect(stop).toBeTruthy();
    act(() => stop.click());
    expect(onInterrupt).toHaveBeenCalledTimes(1);
  });
});
