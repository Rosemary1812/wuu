import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ComposerGoalSummary } from "../shared/protocol";
import { ComposerGoalStrip } from "./ComposerGoalStrip";
import { setActiveLocale } from "./i18n";

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
  setActiveLocale("zh-CN");
  container.remove();
});

function renderStrip(props: {
  summary: ComposerGoalSummary | null;
  onEdit?: (text: string) => void | Promise<void>;
  onPause?: () => void | Promise<void>;
  onResume?: () => void | Promise<void>;
  onClear?: () => void | Promise<void>;
  disabled?: boolean;
  expanded?: boolean;
}): void {
  act(() => {
    root = createRoot(container);
    root.render(
      <ComposerGoalStrip
        summary={props.summary}
        disabled={props.disabled}
        expanded={props.expanded}
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

function changeInput(input: HTMLInputElement | HTMLTextAreaElement, value: string): void {
  const setter = Object.getOwnPropertyDescriptor(
    input instanceof HTMLTextAreaElement
      ? HTMLTextAreaElement.prototype
      : HTMLInputElement.prototype,
    "value",
  )?.set;
  setter?.call(input, value);
  input.dispatchEvent(new Event("input", { bubbles: true }));
}

describe("ComposerGoalStrip", () => {
  it("formats generated goal status in the active language", () => {
    setActiveLocale("en-US");
    renderStrip({ summary: { ...goalSummary(), status: "paused" } });

    expect(container.querySelector(".composer-goal-strip-state")?.textContent).toBe("Paused");
  });
  it("labels an active goal without a running turn as ready to continue", () => {
    renderStrip({ summary: { ...goalSummary("Ship"), status: "active" }, expanded: true });

    expect(container.querySelector(".composer-goal-strip-details")?.textContent).toContain(
      "状态待继续",
    );
  });

  it("labels an active goal with running_since as running", () => {
    renderStrip({
      summary: {
        ...goalSummary("Ship"),
        status: "active",
        running_since: "2026-07-15T02:00:03Z",
      },
      expanded: true,
    });

    expect(container.querySelector(".composer-goal-strip-details")?.textContent).toContain(
      "状态运行中",
    );
  });

  it("does not occupy composer space when there is no active goal", () => {
    renderStrip({ summary: null });

    expect(container.querySelector(".composer-goal-strip")).toBeNull();
    expect(container.textContent).toBe("");
  });

  it("keeps the collapsed drawer to the goal text and compact controls", () => {
    renderStrip({ summary: goalSummary("first line\nsecond line") });

    const strip = container.querySelector(".composer-goal-strip");
    expect(strip).not.toBeNull();
    expect(strip?.classList.contains("expanded")).toBe(false);
    expect(container.querySelector(".composer-goal-strip-label")).toBeNull();
    expect(container.querySelector(".composer-goal-strip-text")?.textContent).toBe("first line");
    expect(container.querySelectorAll(".composer-goal-strip-action")).toHaveLength(3);
    expect(container.querySelector("button[aria-label=\"编辑目标\"]")).not.toBeNull();
    expect(container.querySelector("button[aria-label=\"展开目标\"]")).not.toBeNull();
    expect(container.querySelector("button[aria-label=\"目标操作\"]")).not.toBeNull();
    expect(container.querySelector(".composer-goal-strip-details")).toBeNull();
    expect(document.querySelector("button[role=\"menuitem\"]")).toBeNull();

    act(() => {
      container
        .querySelector<HTMLButtonElement>("button[aria-label=\"目标操作\"]")
        ?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });

    expect(
      Array.from(document.querySelectorAll("button[role=\"menuitem\"]")).map(
        (button) => button.textContent,
      ),
    ).toEqual(["暂停目标", "清除目标"]);
  });

  it("expands full goal text, usage, and blocker detail inside the drawer", () => {
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

    expect(container.querySelector(".composer-goal-strip-state")?.textContent).toBe(
      "已阻塞",
    );
    expect(container.querySelector(".composer-goal-strip-details")).toBeNull();

    act(() => {
      container
        .querySelector<HTMLButtonElement>("button[aria-label=\"展开目标\"]")
        ?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });

    expect(container.querySelector(".composer-goal-strip")?.classList.contains("expanded")).toBe(true);
    expect(container.querySelector(".composer-goal-strip-full-text")?.textContent).toBe(
      "ship runtime loop",
    );
    const rows = Array.from(
      container.querySelectorAll(".composer-goal-strip-info-row"),
    ).map((row) => row.textContent);
    expect(rows).toEqual([
      "状态已阻塞",
      "运行1 分 15 秒",
      "回合2 轮",
      "Tokens1,250",
      "阻塞原因等待用户选择策略",
    ]);
    expect(container.querySelector("button[aria-label=\"收起目标\"]")).not.toBeNull();
  });

  it("collapses goal details from the drawer chevron", () => {
    renderStrip({ summary: goalSummary() });

    act(() => {
      container
        .querySelector<HTMLButtonElement>("button[aria-label=\"展开目标\"]")
        ?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });
    expect(container.querySelector(".composer-goal-strip-details")).not.toBeNull();

    act(() => {
      container
        .querySelector<HTMLButtonElement>("button[aria-label=\"收起目标\"]")
        ?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });
    expect(container.querySelector(".composer-goal-strip-details")).toBeNull();
  });

  it("does not present task-only needs_human as a Goal state", () => {
    renderStrip({
      summary: {
        ...goalSummary(),
        status: "needs_human",
      },
    });

    expect(container.querySelector(".composer-goal-strip-state")).toBeNull();
    expect(container.textContent).not.toContain("需要你");
  });

  it("pauses and resumes through runtime controls", async () => {
    const onPause = vi.fn().mockResolvedValue(undefined);
    const onResume = vi.fn().mockResolvedValue(undefined);
    renderStrip({ summary: goalSummary(), onPause, onResume });

    act(() => {
      container
        .querySelector<HTMLButtonElement>("button[aria-label=\"目标操作\"]")
        ?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });
    await act(async () => {
      document
        .querySelector<HTMLButtonElement>("button[role=\"menuitem\"]")
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

    act(() => {
      container
        .querySelector<HTMLButtonElement>("button[aria-label=\"目标操作\"]")
        ?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });
    await act(async () => {
      document
        .querySelector<HTMLButtonElement>("button[role=\"menuitem\"]")
        ?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
      await Promise.resolve();
    });
    expect(onResume).toHaveBeenCalledTimes(1);
  });

  it("edits a multi-line goal in a dialog and waits for save before closing", async () => {
    const onEdit = vi.fn().mockResolvedValue(undefined);
    renderStrip({ summary: goalSummary("old goal"), onEdit });

    act(() => {
      container
        .querySelector<HTMLButtonElement>("button[aria-label=\"编辑目标\"]")
        ?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });

    const dialog = document.querySelector<HTMLElement>(".composer-goal-edit-dialog");
    expect(dialog?.getAttribute("role")).toBe("dialog");
    const input = dialog?.querySelector<HTMLTextAreaElement>(".composer-goal-edit-textarea");
    expect(input?.value).toBe("old goal");
    act(() => {
      if (input) {
        changeInput(input, "new goal\nwith details");
      }
    });

    await act(async () => {
      dialog
        ?.querySelector<HTMLButtonElement>("button[type=\"submit\"]")
        ?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
      await Promise.resolve();
    });

    expect(onEdit).toHaveBeenCalledWith("new goal\nwith details");
    expect(document.querySelector(".composer-goal-edit-dialog")).toBeNull();
  });

  it("requires a second click before clearing the active goal", async () => {
    const onClear = vi.fn().mockResolvedValue(undefined);
    renderStrip({ summary: goalSummary(), onClear });

    act(() => {
      container
        .querySelector<HTMLButtonElement>("button[aria-label=\"目标操作\"]")
        ?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });
    const clearButton = Array.from(
      document.querySelectorAll<HTMLButtonElement>("button[role=\"menuitem\"]"),
    ).find((button) => button.textContent === "清除目标");
    expect(clearButton).not.toBeNull();

    act(() => {
      clearButton?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });
    expect(onClear).not.toHaveBeenCalled();
    const confirmButton = Array.from(
      document.querySelectorAll<HTMLButtonElement>("button[role=\"menuitem\"]"),
    ).find((button) => button.textContent === "再次点击确认清除");
    expect(confirmButton).not.toBeNull();

    await act(async () => {
      confirmButton?.dispatchEvent(
        new MouseEvent("click", { bubbles: true, cancelable: true }),
      );
      await Promise.resolve();
    });

    expect(onClear).toHaveBeenCalledTimes(1);
  });

  it("does not expose wall-clock elapsed time from started_at", () => {
    renderStrip({
      summary: {
        ...goalSummary("Ship"),
        status: "blocked",
        started_at: "2020-01-01T00:00:00Z",
        time_used_seconds: 42,
        can_pause: false,
        can_resume: true,
      },
    });

    expect(container.querySelector(".composer-goal-strip-elapsed")).toBeNull();
    expect(container.textContent).not.toContain("2020");

    act(() => {
      container
        .querySelector<HTMLButtonElement>("button[aria-label=\"展开目标\"]")
        ?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });

    expect(container.querySelector(".composer-goal-strip-details")?.textContent).toContain(
      "运行42 秒",
    );
  });

  it("advances the current turn runtime from running_since", () => {
    const now = Date.parse("2026-07-15T02:00:10Z");
    const nowSpy = vi.spyOn(Date, "now").mockReturnValue(now);
    renderStrip({
      summary: {
        ...goalSummary("Ship"),
        time_used_seconds: 4,
        running_since: "2026-07-15T02:00:03Z",
      },
    });

    act(() => {
      container
        .querySelector<HTMLButtonElement>("button[aria-label=\"展开目标\"]")
        ?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });

    expect(container.querySelector(".composer-goal-strip-details")?.textContent).toContain(
      "运行11 秒",
    );
    nowSpy.mockRestore();
  });
});
