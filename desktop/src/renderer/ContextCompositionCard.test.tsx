import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { ContextCompositionCard, type ContextCompositionEntry } from "./ContextCompositionCard";

let container: HTMLDivElement;
let root: Root | null = null;

beforeEach(() => {
  container = document.createElement("div");
  document.body.appendChild(container);
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  root = null;
  container.remove();
});

function renderCard(entry: ContextCompositionEntry): void {
  act(() => {
    root = createRoot(container);
    root.render(<ContextCompositionCard entry={entry} />);
  });
}

describe("ContextCompositionCard", () => {
  it("uses retained history as the primary context occupancy", () => {
    renderCard({
      id: "entry-1",
      threadID: "thread-1",
      loading: false,
      result: {
        thread_id: "thread-1",
        available: true,
        provider: "custom-provider",
        model: "bring-your-own-model",
        prompt_tokens: 508_000,
        context_window_tokens: 512_000,
        input_limit_tokens: 512_000,
        compact_threshold_tokens: 384_000,
        retained_tokens: 103_000,
        categories: [
          {
            id: "turn_prefix",
            label: "本轮前缀",
            contributes: true,
            tokens: 491_000,
            tone: "turn",
          },
        ],
      },
    });

    const text = container.textContent ?? "";
    expect(text).toContain("103k / 512k");
    expect(text).toContain("历史占用 / 上下文上限");
    expect(text).toContain("历史余量 409k");
    expect(text).toContain("最近请求 508k");
    expect(text).toContain("最近请求组成");
    expect(text).not.toContain("压缩线");
  });

  it("scales the composition bar to retained context occupancy", () => {
    renderCard({
      id: "entry-2",
      threadID: "thread-1",
      loading: false,
      result: {
        thread_id: "thread-1",
        available: true,
        prompt_tokens: 400_000,
        context_window_tokens: 500_000,
        retained_tokens: 100_000,
        categories: [
          {
            id: "system",
            label: "系统提示",
            contributes: true,
            tokens: 100_000,
            tone: "system",
          },
          {
            id: "stable_history",
            label: "稳定历史",
            contributes: true,
            tokens: 300_000,
            tone: "stable",
          },
        ],
      },
    });

    const segments = Array.from(container.querySelectorAll<HTMLElement>(".context-composition-segment"));

    expect(segments).toHaveLength(2);
    expect(Number.parseFloat(segments[0]?.style.width ?? "")).toBeCloseTo(5);
    expect(Number.parseFloat(segments[1]?.style.width ?? "")).toBeCloseTo(15);
  });
});
