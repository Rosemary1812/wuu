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
  onCancel?: () => void | Promise<void>;
  disabled?: boolean;
}): void {
  act(() => {
    root = createRoot(container);
    root.render(
      <ComposerGoalStrip
        summary={props.summary}
        disabled={props.disabled}
        onEdit={props.onEdit ?? (() => {})}
        onCancel={props.onCancel ?? (() => {})}
      />,
    );
  });
}

function goalSummary(text = "Ship the composer goal strip"): ComposerGoalSummary {
  return {
    id: "goal-1",
    text,
    status: "running",
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
    expect(container.querySelectorAll(".composer-goal-strip-action")).toHaveLength(2);
    expect(
      Array.from(container.querySelectorAll(".composer-goal-strip-action")).map(
        (button) => button.textContent,
      ),
    ).toEqual(["", ""]);
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

  it("requires a second click before cancelling the active goal", async () => {
    const onCancel = vi.fn().mockResolvedValue(undefined);
    renderStrip({ summary: goalSummary(), onCancel });

    const cancelButton = container.querySelector<HTMLButtonElement>(
      "button[aria-label=\"取消目标\"]",
    );
    expect(cancelButton).not.toBeNull();

    act(() => {
      cancelButton?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });
    expect(onCancel).not.toHaveBeenCalled();
    expect(
      container.querySelector<HTMLButtonElement>(
        "button[aria-label=\"再次点击确认取消目标\"]",
      ),
    ).not.toBeNull();

    await act(async () => {
      container
        .querySelector<HTMLButtonElement>(
          "button[aria-label=\"再次点击确认取消目标\"]",
        )
        ?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
      await Promise.resolve();
    });

    expect(onCancel).toHaveBeenCalledTimes(1);
  });
});
