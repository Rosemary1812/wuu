/**
 * Tests for `ToolActivityRow`.
 *
 * Contract: an initial live summary renders in full so switching back
 * to a running session does not replay old gray text. Summary text
 * fake-streams when it grows during `streaming`, and snaps to full the
 * moment `streaming` flips false (the catch-up signal AssistantTurnShell
 * raises when an agent_message in the same turn starts streaming).
 */
import { afterEach, beforeAll, describe, expect, it } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { ToolActivityRow } from "./ToolActivity";
import type { ThreadItem } from "../shared/protocol";

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

function fakeReadFileTool(): ThreadItem {
  // Single-segment path so formatPathTarget's basename collapse lands
  // on a deterministic string we can match exactly in assertions.
  return {
    id: "tool-1",
    type: "tool_call",
    status: "completed",
    name: "read_file",
    arguments: JSON.stringify({ path: "foo.ts" }),
  };
}

function fakeInFlightReadFileTool(): ThreadItem {
  return {
    id: "tool-1",
    type: "tool_call",
    status: "in_progress",
    name: "read_file",
  };
}

// ToolActivityRow's summary is `${section.title} ${section.detail}`.
// section.title is the verb group ("查看" for the read group), and
// section.detail comes from compactToolTargets which returns the
// path basename — NOT the full readable title. So a parsed read_file
// surfaces as "查看 foo.ts".
const SUMMARY_TEXT = "查看 foo.ts";

let container: HTMLDivElement | null = null;
let root: Root | null = null;

function mount(props: Parameters<typeof ToolActivityRow>[0]): void {
  if (container) unmount();
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  act(() => {
    root!.render(<ToolActivityRow {...props} />);
  });
}

function rerender(props: Parameters<typeof ToolActivityRow>[0]): void {
  act(() => {
    root!.render(<ToolActivityRow {...props} />);
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

function surfaceText(): string {
  const span = container?.querySelector(".activity-copy span") as
    | HTMLElement
    | null;
  return span?.textContent ?? "";
}

afterEach(() => {
  unmount();
});

describe("ToolActivityRow", () => {
  it("renders the summary text in full immediately when streaming is false", () => {
    mount({ items: [fakeReadFileTool()], streaming: false });
    expect(surfaceText()).toBe(SUMMARY_TEXT);
  });

  it("renders an initial live summary in full", () => {
    mount({ items: [fakeReadFileTool()], streaming: true });
    expect(surfaceText()).toBe(SUMMARY_TEXT);
  });

  it("renders summary text progressively when it grows during streaming", async () => {
    mount({ items: [fakeInFlightReadFileTool()], streaming: true });
    expect(surfaceText()).toBe("查看");

    rerender({ items: [fakeReadFileTool()], streaming: true });

    // Mid-reveal: visible is partial (strictly less than full text).
    // The LightweightStreamingText pace is ~12 cps with a 100 ms base,
    // so 200 ms in we should be partway through.
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 200));
    });
    const mid = surfaceText();
    expect(mid.length).toBeGreaterThan(0);
    expect(mid.length).toBeLessThan(SUMMARY_TEXT.length);
    expect(SUMMARY_TEXT.startsWith(mid)).toBe(true);

    // After enough wall time the reveal settles. The full string is
    // 7 chars, well within the 1800 ms ceiling.
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 2000));
    });
    expect(surfaceText()).toBe(SUMMARY_TEXT);
  });

  it("snaps to full text when streaming flips to false mid-reveal (catch-up)", async () => {
    mount({ items: [fakeInFlightReadFileTool()], streaming: true });
    rerender({ items: [fakeReadFileTool()], streaming: true });

    // Let the reveal advance partway.
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 200));
    });
    const mid = surfaceText();
    expect(mid.length).toBeGreaterThan(0);
    expect(mid.length).toBeLessThan(SUMMARY_TEXT.length);

    // AssistantTurnShell raises the catch-up signal the moment an
    // agent_message in the same turn starts streaming. The row must
    // snap to full immediately so the user's eye follows the body
    // text rather than a still-filling title above it.
    rerender({ items: [fakeReadFileTool()], streaming: false });
    expect(surfaceText()).toBe(SUMMARY_TEXT);
  });

  it("still renders the section title when args haven't arrived yet", () => {
    // compactToolTargets returns [] when args are missing, so
    // section.detail is undefined. The row falls back to the bare
    // section.title ("查看") so the user still sees the section header
    // for the in-flight tool, just without a target. This is the
    // expected unified-timing behaviour — no placeholder like
    // "读取 文件" — the detail only appears once args parse.
    const inFlight: ThreadItem = {
      id: "tool-empty",
      type: "tool_call",
      status: "in_progress",
      name: "read_file",
    };
    mount({ items: [inFlight], streaming: true });
    expect(surfaceText()).toBe("查看");
  });
});
