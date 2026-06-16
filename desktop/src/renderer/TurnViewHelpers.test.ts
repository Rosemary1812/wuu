import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  scrollToUserMessage,
  turnAnchorID,
  userMessageAnchorID,
} from "./TurnViewHelpers";

/**
 * Build a tiny DOM tree that mimics the conversation scroll container
 * (`.scroll-region` or `.conversation-split-body`) wrapping a
 * `user-message-block` with the anchor ID. Returns the scroll container
 * so each test can assert the scrollTop delta.
 */
function mountAnchor({
  variant,
  containerHeight = 800,
  containerScrollHeight = 1600,
  nodeOffsetTop = 1200,
}: {
  variant: "scroll-region" | "conversation-split-body";
  containerHeight?: number;
  containerScrollHeight?: number;
  nodeOffsetTop?: number;
}): {
  container: HTMLElement;
  node: HTMLElement;
} {
  const container = document.createElement("div");
  container.className = variant;
  Object.defineProperty(container, "clientHeight", {
    configurable: true,
    value: containerHeight,
  });
  Object.defineProperty(container, "scrollHeight", {
    configurable: true,
    value: containerScrollHeight,
  });
  container.scrollTop = 0;

  const spacer = document.createElement("div");
  spacer.style.height = `${nodeOffsetTop}px`;
  container.appendChild(spacer);

  const node = document.createElement("div");
  node.className = "user-message-block";
  node.id = userMessageAnchorID("turn-1", "item-1");
  container.appendChild(node);

  // Fill the remainder of the scroll surface so it actually overflows.
  const tail = document.createElement("div");
  tail.style.height = `${Math.max(0, containerScrollHeight - nodeOffsetTop - 80)}px`;
  container.appendChild(tail);

  document.body.appendChild(container);

  // jsdom returns {0,0,0,0} for every getBoundingClientRect. Patch the
  // container and node so the helper's offset math resolves to the
  // values we set up above — otherwise `nodeRect.top - containerRect.top`
  // is always 0 and the helper bails out of its scroll call.
  container.getBoundingClientRect = () => ({
    top: 0,
    left: 0,
    right: container.clientWidth,
    bottom: container.clientHeight,
    width: container.clientWidth,
    height: container.clientHeight,
    x: 0,
    y: 0,
    toJSON: () => ({}),
  });
  node.getBoundingClientRect = () => ({
    top: nodeOffsetTop,
    left: 0,
    right: 0,
    bottom: nodeOffsetTop,
    width: 0,
    height: 0,
    x: 0,
    y: nodeOffsetTop,
    toJSON: () => ({}),
  });

  return { container, node };
}

function flushTimers(): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, 250);
  });
}

beforeEach(() => {
  // jsdom does not implement scrollTo — stub it so the tests can
  // observe what scroll target the helper picks.
  Element.prototype.scrollTo = function scrollTo(
    this: HTMLElement,
    options: ScrollToOptions | number,
    _y?: number,
  ) {
    if (typeof options === "number") {
      this.scrollTop = options;
      return;
    }
    if (options?.top !== undefined) {
      this.scrollTop = options.top;
    }
  } as typeof Element.prototype.scrollTo;
});

afterEach(() => {
  document.body.innerHTML = "";
  vi.useRealTimers();
});

describe("scrollToUserMessage", () => {
  it("scrolls the .scroll-region container so the anchor lands below the top padding", async () => {
    const { container, node } = mountAnchor({
      variant: "scroll-region",
      containerHeight: 800,
      // scrollHeight (1800) - clientHeight (800) = 1000, so an offsetTop
      // of 600 leaves plenty of room without triggering the clamp.
      containerScrollHeight: 1800,
      nodeOffsetTop: 600,
    });

    scrollToUserMessage("turn-1", "item-1");
    await flushTimers();

    // The helper subtracts JUMP_TOP_OFFSET_PX (64) from the node's
    // offsetTop so the message sits 64px below the visible top — this
    // is what gives the jump enough headroom to keep the previous turn
    // header in view.
    expect(container.scrollTop).toBe(600 - 64);
    expect(node.classList.contains("user-message-jump-flash")).toBe(true);
  });

  it("also scrolls the split-pane container when split mode is active", async () => {
    const { container, node } = mountAnchor({
      variant: "conversation-split-body",
      containerHeight: 600,
      // scrollHeight (1600) - clientHeight (600) = 1000, so offsetTop
      // 700 does not hit the clamp.
      containerScrollHeight: 1600,
      nodeOffsetTop: 700,
    });

    scrollToUserMessage("turn-1", "item-1");
    await flushTimers();

    expect(container.scrollTop).toBe(700 - 64);
    expect(node.classList.contains("user-message-jump-flash")).toBe(true);
  });

  it("clamps the target scrollTop so the scroll surface does not overshoot", async () => {
    const { container } = mountAnchor({
      variant: "scroll-region",
      containerHeight: 800,
      // Bottom is at 800 (1600 - 800). Pulling the message up by 64
      // would request 1600-64, but the helper should clamp to 800.
      containerScrollHeight: 1600,
      nodeOffsetTop: 1600,
    });

    scrollToUserMessage("turn-1", "item-1");
    await flushTimers();

    expect(container.scrollTop).toBe(800);
  });

  it("retries the anchor lookup until the DOM catches up", async () => {
    const { container } = mountAnchor({
      variant: "scroll-region",
      containerHeight: 800,
      containerScrollHeight: 1800,
      nodeOffsetTop: 600,
    });

    // Hide the anchor first so the helper has to retry.
    const existing = container.querySelector<HTMLElement>(
      `#${userMessageAnchorID("turn-1", "item-1")}`,
    );
    existing?.remove();

    scrollToUserMessage("turn-1", "item-1");

    // Re-mount the anchor before the longest retry delay (200ms) fires.
    const replacement = document.createElement("div");
    replacement.className = "user-message-block";
    replacement.id = userMessageAnchorID("turn-1", "item-1");
    Object.defineProperty(replacement, "getBoundingClientRect", {
      configurable: true,
      value: () => ({
        top: 600,
        left: 0,
        right: 0,
        bottom: 600,
        width: 0,
        height: 0,
        x: 0,
        y: 600,
        toJSON: () => ({}),
      }),
    });
    container.appendChild(replacement);

    // Wait for the longest retry delay (200ms) plus a safety margin.
    await new Promise((resolve) => setTimeout(resolve, 260));

    expect(replacement.classList.contains("user-message-jump-flash")).toBe(true);
  });

  it("does nothing when no anchor exists and retries are exhausted", async () => {
    // No DOM at all — the helper should give up silently.
    scrollToUserMessage("missing", "missing");
    await new Promise((resolve) => setTimeout(resolve, 260));
    expect(document.body.innerHTML).toBe("");
  });
});

describe("anchor ID helpers", () => {
  it("turnAnchorID is stable across reloads", () => {
    expect(turnAnchorID("abc")).toBe("turn-abc");
  });

  it("userMessageAnchorID combines turn and item IDs uniquely", () => {
    expect(userMessageAnchorID("abc", "xyz")).toBe("user-msg-abc-xyz");
  });
});