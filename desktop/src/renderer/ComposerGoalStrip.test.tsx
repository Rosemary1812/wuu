import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ComposerGoalSummary } from "../shared/protocol";
import { ComposerGoalStrip } from "./ComposerGoalStrip";

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

function renderStrip(props: {
  summary: ComposerGoalSummary | null;
  onEdit?: (text: string) => void | Promise<void>;
  onPause?: () => void | Promise<void>;
  onResume?: () => void | Promise<void>;
  onClear?: () => void | Promise<void>;
  disabled?: boolean;
}): void {
  act(() => {
    root = createRoot(container);
    root.render(
      <ComposerGoalStrip
        summary={props.summary}
        disabled={props.disabled}
        onEdit={props.onEdit ?? (() => {})}
        onPause={props.onPause ?? (() => {})}
        onResume={props.onResume ?? (() => {})}
        onClear={props.onClear ?? (() => {})}
      />,
    );
  });
}

function goalSummary(text = "Ship the composer goal strip"): ComposerGoalSummary {
  return {
    id: "goal-1",
    text,
    status: "running",
    can_pause: true,
    can_clear: true,
  };
}

function changeInput(input: HTMLInputElement, value: string): void {
  const setter = Object.getOwnPropertyDescriptor(
    HTMLInputElement.prototype,
    "value",
  )?.set;
  setter?.call(input, value);
  input.dispatchEvent(new Event("input", { bubbles: true }));
}

describe("ComposerGoalStrip", () => {
  it("does not occupy composer space when there is no active goal", () => {
    renderStrip({ summary: null });

    expect(container.querySelector(".composer-goal-strip")).toBeNull();
    expect(container.textContent).toBe("");
  });

  it("renders a compact Goal label, one-line text, and icon actions", () => {
    renderStrip({ summary: goalSummary("first line\nsecond line") });

    const strip = container.querySelector(".composer-goal-strip");
    expect(strip).not.toBeNull();
    expect(container.querySelector(".composer-goal-strip-label")?.textContent).toBe("Goal");
    expect(container.querySelector(".composer-goal-strip-text")?.textContent).toBe("first line");
    expect(container.querySelectorAll(".composer-goal-strip-action")).toHaveLength(3);
    expect(container.querySelector("button[aria-label=\"暂停目标\"]")).not.toBeNull();
    expect(container.querySelector("button[aria-label=\"编辑目标\"]")).not.toBeNull();
    expect(container.querySelector("button[aria-label=\"清除目标\"]")).not.toBeNull();
    expect(
      Array.from(container.querySelectorAll(".composer-goal-strip-action")).map(
        (button) => button.textContent,
      ),
    ).toEqual(["", "", ""]);
  });

  it("renders runtime detail text from status, progress, and usage", () => {
    renderStrip({
      summary: {
        ...goalSummary("ship runtime loop"),
        status: "blocked",
        stop_reason: "blocked",
        blocker: "等待用户选择策略",
        blocker_consecutive_turns: 3,
        tokens_used: 1250,
        goal_turns: 2,
        time_used_seconds: 75,
      },
    });

    expect(container.querySelector(".composer-goal-strip-detail")?.textContent).toBe(
      "已阻塞 | 等待用户选择策略 (3 次) | 2 轮 / 1,250 tokens / 1 分 15 秒",
    );
  });

  it("pauses and resumes through runtime controls", async () => {
    const onPause = vi.fn().mockResolvedValue(undefined);
    const onResume = vi.fn().mockResolvedValue(undefined);
    renderStrip({ summary: goalSummary(), onPause, onResume });

    await act(async () => {
      container
        .querySelector<HTMLButtonElement>("button[aria-label=\"暂停目标\"]")
        ?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
      await Promise.resolve();
    });
    expect(onPause).toHaveBeenCalledTimes(1);

    act(() => {
      root?.render(
        <ComposerGoalStrip
          summary={{
            ...goalSummary(),
            status: "paused",
            stop_reason: "paused",
            can_pause: false,
            can_resume: true,
          }}
          onEdit={() => {}}
          onPause={onPause}
          onResume={onResume}
          onClear={() => {}}
        />,
      );
    });

    await act(async () => {
      container
        .querySelector<HTMLButtonElement>("button[aria-label=\"继续目标\"]")
        ?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
      await Promise.resolve();
    });
    expect(onResume).toHaveBeenCalledTimes(1);
  });

  it("edits the goal inline and waits for save before leaving edit mode", async () => {
    const onEdit = vi.fn().mockResolvedValue(undefined);
    renderStrip({ summary: goalSummary("old goal"), onEdit });

    act(() => {
      container
        .querySelector<HTMLButtonElement>("button[aria-label=\"编辑目标\"]")
        ?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });

    const input = container.querySelector<HTMLInputElement>(".composer-goal-strip-input");
    expect(input).not.toBeNull();
    act(() => {
      if (input) {
        changeInput(input, "new goal");
      }
    });

    await act(async () => {
      container
        .querySelector<HTMLButtonElement>("button[aria-label=\"保存目标\"]")
        ?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
      await Promise.resolve();
    });

    expect(onEdit).toHaveBeenCalledWith("new goal");
    expect(container.querySelector(".composer-goal-strip-input")).toBeNull();
  });

  it("requires a second click before clearing the active goal", async () => {
    const onClear = vi.fn().mockResolvedValue(undefined);
    renderStrip({ summary: goalSummary(), onClear });

    const clearButton = container.querySelector<HTMLButtonElement>(
      "button[aria-label=\"清除目标\"]",
    );
    expect(clearButton).not.toBeNull();

    act(() => {
      clearButton?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });
    expect(onClear).not.toHaveBeenCalled();
    expect(
      container.querySelector<HTMLButtonElement>(
        "button[aria-label=\"再次点击确认清除目标\"]",
      ),
    ).not.toBeNull();

    await act(async () => {
      container
        .querySelector<HTMLButtonElement>(
          "button[aria-label=\"再次点击确认清除目标\"]",
        )
        ?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
      await Promise.resolve();
    });

    expect(onClear).toHaveBeenCalledTimes(1);
  });

  describe("with elapsed timer", () => {
    beforeEach(() => {
      vi.useFakeTimers();
      vi.setSystemTime(new Date("2026-06-24T12:00:00Z"));
    });

    afterEach(() => {
      vi.useRealTimers();
    });

    function goalSummaryWithStartedAt(
      text: string,
      startedAt: string,
    ): ComposerGoalSummary {
      return {
        id: "goal-1",
        text,
        status: "running",
        started_at: startedAt,
        can_pause: true,
        can_clear: true,
      };
    }

    it("renders mm:ss elapsed when started_at is within an hour", () => {
      const startedAt = new Date("2026-06-24T11:58:55Z").toISOString();
      renderStrip({
        summary: goalSummaryWithStartedAt(
          "Ship the composer goal strip",
          startedAt,
        ),
      });

      expect(
        container.querySelector(".composer-goal-strip-elapsed")?.textContent,
      ).toBe("01:05");
    });

    it("renders h:mm:ss elapsed when started_at is over an hour ago", () => {
      const startedAt = new Date("2026-06-24T09:54:30Z").toISOString();
      renderStrip({
        summary: goalSummaryWithStartedAt(
          "Ship the composer goal strip",
          startedAt,
        ),
      });

      expect(
        container.querySelector(".composer-goal-strip-elapsed")?.textContent,
      ).toBe("2:05:30");
    });

    it("does not render the elapsed chip when started_at is missing", () => {
      renderStrip({ summary: goalSummary() });

      expect(
        container.querySelector(".composer-goal-strip-elapsed"),
      ).toBeNull();
    });

    it("advances the displayed elapsed as time passes", () => {
      const startedAt = new Date("2026-06-24T11:59:30Z").toISOString();
      renderStrip({
        summary: goalSummaryWithStartedAt("Ship", startedAt),
      });

      expect(
        container.querySelector(".composer-goal-strip-elapsed")?.textContent,
      ).toBe("00:30");

      act(() => {
        vi.advanceTimersByTime(1000);
      });

      expect(
        container.querySelector(".composer-goal-strip-elapsed")?.textContent,
      ).toBe("00:31");
    });

    it("clamps negative drift (clock skew) to 00:00", () => {
      // started_at is in the future relative to the fake system clock;
      // formatElapsed should clamp at zero instead of going negative.
      const startedAt = new Date("2026-06-24T12:00:30Z").toISOString();
      renderStrip({
        summary: goalSummaryWithStartedAt("Ship", startedAt),
      });

      expect(
        container.querySelector(".composer-goal-strip-elapsed")?.textContent,
      ).toBe("00:00");
    });
  });
});
