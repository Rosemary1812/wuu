/**
 * Tests for `ToolApprovalCard`. Verifies:
 *   - default vs danger visual class
 *   - capability-specific rendering (command prompt, network method badge)
 *   - the three decisions route to the right callback
 *   - keyboard: Enter on body fires approve; Esc fires deny
 *   - long arguments are collapsed with an expand toggle
 */
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { ToolApprovalCard } from "./ToolApprovalCard";
import type { PendingToolApproval } from "../shared/protocol";

// jsdom doesn't implement layout. Stub getBoundingClientRect so React
// doesn't crash on layout queries.
beforeAll(() => {
  Element.prototype.getBoundingClientRect = function (): DOMRect {
    return {
      x: 0,
      y: 0,
      top: 0,
      left: 0,
      right: 0,
      bottom: 0,
      width: 0,
      height: 0,
      toJSON() {
        return this;
      },
    } as DOMRect;
  };
});

function baseApproval(
  overrides: Partial<PendingToolApproval> = {},
): PendingToolApproval {
  return {
    server_request_id: "srv-1",
    id: "approval-1",
    tool_name: "run_shell",
    call_id: "call-1",
    capability: "command.bash",
    capability_action: "execute",
    capability_object: "npm test",
    arguments_preview: '{"command":"npm test"}',
    policy_reason: "需要审批",
    destructive: false,
    ...overrides,
  };
}

let container: HTMLDivElement | null = null;
let root: Root | null = null;

function mount(props: Parameters<typeof ToolApprovalCard>[0]): void {
  if (container) unmount();
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  act(() => {
    root!.render(<ToolApprovalCard {...props} />);
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

afterEach(() => {
  unmount();
});

function buttonByText(container: HTMLElement, text: string): HTMLElement {
  const buttons = Array.from(container.querySelectorAll("button"));
  const match = buttons.find((button) => button.textContent?.trim() === text);
  if (!match) {
    throw new Error(
      `button with text "${text}" not found; saw: ${buttons
        .map((b) => b.textContent?.trim())
        .join(", ")}`,
    );
  }
  return match;
}

function click(element: HTMLElement): void {
  act(() => {
    element.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  });
}

describe("ToolApprovalCard", () => {
  it("renders the compact approval summary and reason", () => {
    mount({
      approval: baseApproval(),
      onApprove: () => undefined,
      onApproveForSession: () => undefined,
      onDeny: () => undefined,
    });

    const card = container!.querySelector(".tool-approval-card");
    expect(card).not.toBeNull();
    expect(card?.textContent).toContain("运行命令");
    expect(card?.textContent).toContain("npm test");
    expect(card?.textContent).toContain("需要审批");
  });

  it("uses the danger class for destructive approvals", () => {
    mount({
      approval: baseApproval({ destructive: true }),
      onApprove: () => undefined,
      onApproveForSession: () => undefined,
      onDeny: () => undefined,
    });

    const card = container!.querySelector(".tool-approval-card");
    expect(card?.classList.contains("danger")).toBe(true);
  });

  it("uses the danger class for non-idempotent network calls", () => {
    mount({
      approval: baseApproval({
        capability: "network.fetch",
        capability_action: "POST",
        capability_object: "https://api.example.com/items",
        arguments_preview: '{"method":"POST","url":"https://api.example.com/items"}',
        destructive: false,
      }),
      onApprove: () => undefined,
      onApproveForSession: () => undefined,
      onDeny: () => undefined,
    });

    const card = container!.querySelector(".tool-approval-card");
    expect(card?.classList.contains("danger")).toBe(true);
    // The method badge should show POST.
    expect(card?.textContent).toContain("POST");
  });

  it("stays neutral for GET network calls even though capability is network.*", () => {
    mount({
      approval: baseApproval({
        capability: "network.fetch",
        capability_action: "GET",
        capability_object: "https://api.example.com/items",
        arguments_preview: '{"method":"GET","url":"https://api.example.com/items"}',
        destructive: false,
      }),
      onApprove: () => undefined,
      onApproveForSession: () => undefined,
      onDeny: () => undefined,
    });

    const card = container!.querySelector(".tool-approval-card");
    expect(card?.classList.contains("danger")).toBe(false);
  });

  it("shows the command prompt for command.* capabilities", () => {
    mount({
      approval: baseApproval({ capability: "command.bash" }),
      onApprove: () => undefined,
      onApproveForSession: () => undefined,
      onDeny: () => undefined,
    });

    expect(
      container!.querySelector(".tool-approval-card-preview-prompt"),
    ).not.toBeNull();
  });

  it("routes the three button clicks to the matching callback", () => {
    const onApprove = vi.fn();
    const onApproveForSession = vi.fn();
    const onDeny = vi.fn();

    mount({
      approval: baseApproval(),
      onApprove,
      onApproveForSession,
      onDeny,
    });

    click(buttonByText(container!, "批准一次"));
    click(buttonByText(container!, "本会话批准"));
    click(buttonByText(container!, "拒绝"));

    expect(onApprove).toHaveBeenCalledTimes(1);
    expect(onApproveForSession).toHaveBeenCalledTimes(1);
    expect(onDeny).toHaveBeenCalledTimes(1);
  });

  it("Esc anywhere on the card fires deny", () => {
    const onDeny = vi.fn();

    mount({
      approval: baseApproval(),
      onApprove: () => undefined,
      onApproveForSession: () => undefined,
      onDeny,
    });

    act(() => {
      container!.querySelector(".tool-approval-card")!.dispatchEvent(
        new KeyboardEvent("keydown", {
          key: "Escape",
          bubbles: true,
          cancelable: true,
        }),
      );
    });

    expect(onDeny).toHaveBeenCalledTimes(1);
  });

  it("Enter on a non-button target fires approve", () => {
    const onApprove = vi.fn();

    mount({
      approval: baseApproval(),
      onApprove,
      onApproveForSession: () => undefined,
      onDeny: () => undefined,
    });

    // The card itself becomes the event target when focus is on its body.
    act(() => {
      container!.querySelector(".tool-approval-card")!.dispatchEvent(
        new KeyboardEvent("keydown", {
          key: "Enter",
          bubbles: true,
          cancelable: true,
        }),
      );
    });

    expect(onApprove).toHaveBeenCalledTimes(1);
  });

  it("collapses long arguments and reveals the full preview on expand", () => {
    const longPreview = Array.from({ length: 12 }, (_, i) => `line ${i + 1}`).join(
      "\n",
    );

    mount({
      approval: baseApproval({
        arguments_preview: longPreview,
      }),
      onApprove: () => undefined,
      onApproveForSession: () => undefined,
      onDeny: () => undefined,
    });

    // Only the first 6 lines should be visible before expanding.
    const previewBefore = container!.querySelector(
      ".tool-approval-card-preview-text",
    );
    expect(previewBefore?.textContent?.trim().split("\n")).toHaveLength(6);

    const expand = buttonByText(container!, "展开完整参数 (12 行)");
    click(expand);

    const previewAfter = container!.querySelector(
      ".tool-approval-card-preview-text",
    );
    expect(previewAfter?.textContent?.trim().split("\n")).toHaveLength(12);
    expect(container!.textContent).toContain("收起");
  });
});