/**
 * Tests for `ProcessSurface`.
 *
 * The headline assertion of this file is "same DOM node from first tool
 * call to final settle". The old architecture unmounted
 * <ToolActivityTimeline> and remounted a brand-new <ProcessClusterRow>
 * the moment the cluster condition became true, which read as a single-
 * frame visual jump (the "process cluster flicker"). ProcessSurface
 * owns the entire 1 → N → settled lifecycle in a single component, so
 * the assertion below is the regression guard for that fix.
 */
import { act, type ReactElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeAll, describe, expect, it } from "vitest";
import { ProcessSurface } from "./ProcessSurface";
import type { ThreadItem } from "../shared/protocol";

beforeAll(() => {
  // jsdom does not lay out real heights. Stub getBoundingClientRect so
  // React's effects do not crash on layout queries.
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

function makeReadFile(
  id: string,
  path: string,
  status: ThreadItem["status"] = "completed",
): ThreadItem {
  return {
    id,
    type: "tool_call",
    status,
    name: "read_file",
    arguments: JSON.stringify({ path }),
  };
}

function makeReasoning(
  id: string,
  text: string,
  status: ThreadItem["status"] = "completed",
): ThreadItem {
  return {
    id,
    type: "reasoning",
    status,
    text,
  };
}

let container: HTMLDivElement | null = null;
let root: Root | null = null;

type SurfaceProps = Parameters<typeof ProcessSurface>[0];

function render(
  props: SurfaceProps,
): { container: HTMLDivElement } {
  if (container) unmount();
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  act(() => {
    root!.render(<ProcessSurface {...props} /> as ReactElement);
  });
  return { container };
}

function rerender(props: SurfaceProps): void {
  act(() => {
    root!.render(<ProcessSurface {...props} /> as ReactElement);
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

describe("ProcessSurface", () => {
  it("keeps the same surface node when items grow from one to many (no unmount)", () => {
    const { container: initialContainer } = render({
      processItems: [makeReadFile("tool-1", "a.ts")],
      streaming: true,
    });
    const surfaceBefore = initialContainer.querySelector(".process-surface");
    expect(surfaceBefore).toBeTruthy();

    rerender({
      processItems: [
        makeReadFile("tool-1", "a.ts"),
        makeReadFile("tool-2", "b.ts"),
      ],
      streaming: true,
    });
    const surfaceAfter = initialContainer.querySelector(".process-surface");

    // Same DOM node, not just same selector — proves React reused the
    // component instance instead of unmounting and remounting. This is
    // the regression guard for the cluster flicker.
    expect(surfaceAfter).toBe(surfaceBefore);
  });

  it("renders a flat article for a single in-progress tool with no fold", () => {
    const { container } = render({
      processItems: [makeReadFile("tool-1", "a.ts", "in_progress")],
      streaming: true,
    });
    expect(container.querySelector("details.process-surface-fold")).toBeNull();
    const surface = container.querySelector("div.process-surface");
    expect(surface).toBeTruthy();
    expect(surface?.classList.contains("is-streaming")).toBe(true);
    const article = container.querySelector("article.process-surface-inline");
    expect(article).toBeTruthy();
  });

  it("opens a fold when there are multiple tools", () => {
    const { container } = render({
      processItems: [
        makeReadFile("tool-1", "a.ts", "completed"),
        makeReadFile("tool-2", "b.ts", "in_progress"),
      ],
      streaming: true,
    });
    const details = container.querySelector("details.process-surface-fold");
    expect(details).toBeTruthy();
    expect(details?.hasAttribute("open")).toBe(true);
  });

  it("applies is-streaming class on the row while the surface is streaming", () => {
    const { container } = render({
      processItems: [
        makeReadFile("tool-1", "a.ts", "in_progress"),
        makeReadFile("tool-2", "b.ts", "in_progress"),
      ],
      streaming: true,
    });
    const row = container.querySelector(".process-surface-row");
    expect(row?.classList.contains("is-streaming")).toBe(true);
  });

  it("removes is-streaming class once the surface settles", () => {
    const { container } = render({
      processItems: [
        makeReadFile("tool-1", "a.ts", "completed"),
        makeReadFile("tool-2", "b.ts", "completed"),
      ],
      streaming: false,
    });
    const row = container.querySelector(".process-surface-row");
    expect(row?.classList.contains("is-streaming")).toBe(false);
  });

  it("auto-expands the fold when streaming starts and the user has not interacted", () => {
    const { container } = render({
      processItems: [
        makeReadFile("tool-1", "a.ts", "completed"),
        makeReadFile("tool-2", "b.ts", "in_progress"),
      ],
      streaming: false,
    });
    const detailsBefore = container.querySelector(
      "details.process-surface-fold",
    );
    expect(detailsBefore?.hasAttribute("open")).toBe(false);

    rerender({
      processItems: [
        makeReadFile("tool-1", "a.ts", "completed"),
        makeReadFile("tool-2", "b.ts", "in_progress"),
      ],
      streaming: true,
    });
    const detailsAfter = container.querySelector(
      "details.process-surface-fold",
    );
    expect(detailsAfter?.hasAttribute("open")).toBe(true);
  });

  it("renders the reasoning summary segment with '思考过程' when settled", () => {
    const { container } = render({
      processItems: [
        makeReadFile("tool-1", "a.ts", "completed"),
        makeReasoning("reason-1", "thinking aloud", "completed"),
      ],
      streaming: false,
    });
    const label = container.querySelector(
      ".process-surface-reasoning-label",
    );
    expect(label?.textContent).toBe("思考过程");
  });

  it("renders '正在思考' while reasoning is still in progress", () => {
    const { container } = render({
      processItems: [
        makeReadFile("tool-1", "a.ts", "completed"),
        makeReasoning("reason-1", "thinking aloud", "in_progress"),
      ],
      streaming: true,
    });
    const label = container.querySelector(
      ".process-surface-reasoning-label",
    );
    expect(label?.textContent).toBe("正在思考");
  });

  it("preserves the user expand choice after they toggle", () => {
    const { container } = render({
      processItems: [
        makeReadFile("tool-1", "a.ts", "in_progress"),
        makeReadFile("tool-2", "b.ts", "in_progress"),
      ],
      streaming: true,
    });
    const details = container.querySelector(
      "details.process-surface-fold",
    ) as HTMLDetailsElement | null;
    expect(details?.open).toBe(true);

    // User clicks the summary to collapse.
    act(() => {
      if (!details) return;
      details.open = false;
      details.dispatchEvent(new Event("toggle"));
    });
    expect(details?.open).toBe(false);

    // Streaming flips off; the fold should NOT auto-reopen or
    // auto-collapse, because the user has interacted.
    rerender({
      processItems: [
        makeReadFile("tool-1", "a.ts", "completed"),
        makeReadFile("tool-2", "b.ts", "completed"),
      ],
      streaming: false,
    });
    const detailsAfter = container.querySelector(
      "details.process-surface-fold",
    ) as HTMLDetailsElement | null;
    expect(detailsAfter?.open).toBe(false);
  });

  it("renders a count segment for multiple files of the same kind", () => {
    const { container } = render({
      processItems: [
        makeReadFile("tool-1", "a.ts", "completed"),
        makeReadFile("tool-2", "b.ts", "completed"),
        makeReadFile("tool-3", "c.ts", "completed"),
      ],
      streaming: false,
    });
    const count = container.querySelector(".process-surface-count");
    expect(count?.textContent).toBe("3");
  });

  it("marks the count is-changing for ~180ms when the value changes", async () => {
    const { container } = render({
      processItems: [
        makeReadFile("tool-1", "a.ts", "completed"),
        makeReadFile("tool-2", "b.ts", "completed"),
      ],
      streaming: false,
    });
    const countBefore = container.querySelector(".process-surface-count");
    expect(countBefore?.textContent).toBe("2");
    expect(countBefore?.classList.contains("is-changing")).toBe(false);

    rerender({
      processItems: [
        makeReadFile("tool-1", "a.ts", "completed"),
        makeReadFile("tool-2", "b.ts", "completed"),
        makeReadFile("tool-3", "c.ts", "completed"),
      ],
      streaming: false,
    });
    const countDuring = container.querySelector(".process-surface-count");
    expect(countDuring?.textContent).toBe("3");
    expect(countDuring?.classList.contains("is-changing")).toBe(true);

    // Past the 180ms window, the class is cleared.
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 220));
    });
    const countAfter = container.querySelector(".process-surface-count");
    expect(countAfter?.classList.contains("is-changing")).toBe(false);
  });

  it("uses the renderReasoningItem callback for reasoning items in the body", () => {
    const { container } = render({
      processItems: [
        makeReadFile("tool-1", "a.ts", "completed"),
        makeReadFile("tool-2", "b.ts", "completed"),
        makeReasoning("reason-1", "thinking aloud", "in_progress"),
      ],
      streaming: true,
      renderReasoningItem: (item, isStreaming) => (
        <span
          data-testid="reasoning-mock"
          data-streaming={String(isStreaming)}
        >
          {item.id}
        </span>
      ),
    });
    const mocked = container.querySelectorAll(
      '[data-testid="reasoning-mock"]',
    );
    expect(mocked.length).toBe(1);
    expect(mocked[0].getAttribute("data-streaming")).toBe("true");
  });

  it("omits reasoning body items when renderReasoningItem is not provided", () => {
    const { container } = render({
      processItems: [
        makeReadFile("tool-1", "a.ts", "completed"),
        makeReadFile("tool-2", "b.ts", "completed"),
        makeReasoning("reason-1", "thinking aloud", "completed"),
      ],
      streaming: false,
    });
    const list = container.querySelector(
      ".process-surface-reasoning-list",
    );
    expect(list).toBeNull();
    // Summary still mentions 思考过程.
    const label = container.querySelector(
      ".process-surface-reasoning-label",
    );
    expect(label?.textContent).toBe("思考过程");
  });

  describe("reasoning scroll container", () => {
    type ScrollLayout = {
      scrollHeight: number;
      clientHeight: number;
      scrollTop: number;
    };

    function stubScrollLayout(
      node: HTMLElement,
      opts: Partial<ScrollLayout>,
    ): ScrollLayout {
      const layout: ScrollLayout = {
        scrollHeight: opts.scrollHeight ?? 1000,
        clientHeight: opts.clientHeight ?? 200,
        scrollTop: opts.scrollTop ?? 0,
      };
      Object.defineProperty(node, "scrollHeight", {
        configurable: true,
        get: () => layout.scrollHeight,
      });
      Object.defineProperty(node, "clientHeight", {
        configurable: true,
        get: () => layout.clientHeight,
      });
      Object.defineProperty(node, "scrollTop", {
        configurable: true,
        get: () => layout.scrollTop,
        set: (value) => {
          layout.scrollTop = value;
        },
      });
      return layout;
    }

    it("wraps reasoning items in a scroll container for bounded growth", () => {
      const { container } = render({
        processItems: [makeReasoning("reason-1", "thinking aloud", "in_progress")],
        streaming: true,
        renderReasoningItem: (item) => (
          <span data-testid="reasoning-mock">{item.id}</span>
        ),
      });
      const scroll = container.querySelector(
        ".process-surface-reasoning-scroll",
      );
      expect(scroll).toBeTruthy();
      const items = scroll?.querySelectorAll(
        ".process-surface-reasoning-item",
      );
      expect(items?.length).toBe(1);
      // The mock item is wrapped inside the reasoning item slot.
      const mock = scroll?.querySelector('[data-testid="reasoning-mock"]');
      expect(mock?.textContent).toBe("reason-1");
    });

    it("snaps the reasoning scroll container to the bottom when the fold opens", async () => {
      const { container } = render({
        processItems: [makeReasoning("reason-1", "thinking aloud", "completed")],
        // streaming=true auto-opens the fold, which is the production
        // entry path: the user sees the reasoning scroll snap to its
        // tail the moment the surface mounts in its expanded state.
        streaming: true,
        renderReasoningItem: (item) => <span>{item.id}</span>,
      });
      const details = container.querySelector(
        "details.process-surface-fold",
      ) as HTMLElement | null;
      expect(details).toBeTruthy();
      const scroll = details?.querySelector(
        ".process-surface-reasoning-scroll",
      ) as HTMLElement | null;
      expect(scroll).toBeTruthy();

      // jsdom does not lay out real heights — mock the scroll geometry
      // so snap-to-bottom has measurable values to read.
      const layout = stubScrollLayout(scroll!, {
        scrollHeight: 1000,
        clientHeight: 200,
      });

      // The fold-open snap fires ~280ms after mount. Wait it out.
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 320));
      });

      expect(layout.scrollTop).toBe(1000);
    });

    it("snaps again on every fold re-open so the user always lands on the latest reasoning", async () => {
      const { container } = render({
        processItems: [makeReasoning("reason-1", "thinking aloud", "completed")],
        streaming: true,
        renderReasoningItem: (item) => <span>{item.id}</span>,
      });
      const details = container.querySelector(
        "details.process-surface-fold",
      ) as HTMLDetailsElement | null;
      const scroll = details?.querySelector(
        ".process-surface-reasoning-scroll",
      ) as HTMLElement | null;
      const layout = stubScrollLayout(scroll!, {
        scrollHeight: 1000,
        clientHeight: 200,
      });

      // First snap (initial open).
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 320));
      });
      expect(layout.scrollTop).toBe(1000);

      // User scrolls up to read earlier reasoning.
      act(() => {
        scroll!.dispatchEvent(new UIEvent("scroll", { bubbles: true }));
      });
      layout.scrollTop = 240;
      act(() => {
        scroll!.dispatchEvent(new UIEvent("scroll", { bubbles: true }));
      });

      // User collapses then re-expands the fold; the scroll should
      // snap back to the latest reasoning so they land on it instead
      // of where they were when they collapsed.
      details!.open = false;
      act(() => {
        details!.dispatchEvent(new Event("toggle", { bubbles: true }));
      });
      details!.open = true;
      act(() => {
        details!.dispatchEvent(new Event("toggle", { bubbles: true }));
      });
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 320));
      });

      expect(layout.scrollTop).toBe(1000);
    });
  });
});
