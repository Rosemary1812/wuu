import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AutomationTask, WuuDesktopApi } from "../shared/protocol";
import { AutomationsCatalog } from "./AutomationsCatalog";

let container: HTMLDivElement;
let root: Root | null = null;

const task: AutomationTask = {
  id: "daily-1",
  title: "每日简报",
  prompt: "总结今天的工作",
  cron: "0 8 * * 1-5",
  timezone: "Asia/Shanghai",
  mode: "new_thread",
  createdAt: Date.now(),
  recurring: true,
};

beforeEach(() => {
  window.localStorage.removeItem("wuu.desktop.automationDetailPaneWidth");
  container = document.createElement("div");
  document.body.appendChild(container);
});

afterEach(() => {
  act(() => root?.unmount());
  root = null;
  container.remove();
  delete (window as { wuu?: WuuDesktopApi }).wuu;
});

describe("AutomationsCatalog", () => {
  it("separates an hourly interval from its concrete next run", async () => {
    installApi([{ ...task, cron: "0 * * * *" }]);
    await act(async () => {
      root = createRoot(container);
      root.render(<AutomationsCatalog />);
    });

    expect(container.textContent).toContain("每小时");
    expect(container.textContent).toContain("下次");
    expect(container.textContent).not.toContain("第 00 分钟");
  });

  it("opens the detail sidebar only after selecting a task", async () => {
    const onDetailPaneLayoutChange = vi.fn();
    installApi([task]);
    await act(async () => {
      root = createRoot(container);
      root.render(<AutomationsCatalog onDetailPaneLayoutChange={onDetailPaneLayoutChange} />);
    });

    expect(container.querySelector(".automations-detail")).toBeNull();
    expect(container.querySelector(".catalog-page-header")).toBeTruthy();
    expect(container.textContent).toContain("让 Wuu 按计划自动执行任务");
    expect(container.querySelector(".catalog-search input[type=\"search\"]")).toBeTruthy();
    const row = container.querySelector<HTMLButtonElement>(".automation-row");
    await act(async () => row?.click());
    expect(container.querySelector(".automations-detail")).toBeTruthy();
    expect(container.querySelector('[role="separator"]')).toBeTruthy();
    expect(container.querySelector(".automation-row-chevron")).toBeTruthy();
    expect(container.querySelector(".automation-state")?.getAttribute("aria-label")).toBe("已开启");
    expect(container.querySelector(".automation-detail-heading")).toBeNull();
    expect(container.querySelectorAll(".automation-detail-section")).toHaveLength(2);
    expect(container.querySelectorAll(".automation-detail-form .settings-input").length).toBeGreaterThan(1);
    expect(onDetailPaneLayoutChange).toHaveBeenLastCalledWith({
      open: true,
      reservedWidth: 530,
    });
    const separator = container.querySelector<HTMLButtonElement>('[role="separator"]');
    await act(async () => separator?.dispatchEvent(new KeyboardEvent("keydown", {
      key: "ArrowLeft",
      bubbles: true,
    })));
    expect(separator?.getAttribute("aria-valuenow")).toBe("552");
    expect(onDetailPaneLayoutChange).toHaveBeenLastCalledWith({
      open: true,
      reservedWidth: 562,
    });
    expect(container.querySelector<HTMLInputElement>('input[value="每日简报"]')).toBeTruthy();

    const close = Array.from(container.querySelectorAll<HTMLButtonElement>("button"))
      .find((button) => button.getAttribute("aria-label") === "关闭详情");
    await act(async () => close?.click());
    expect(container.querySelector(".automations-detail")).toBeNull();
  });

  it("pauses the selected task through the update API", async () => {
    const updateAutomation = vi.fn().mockResolvedValue({ task: { ...task, paused: true } });
    installApi([task], updateAutomation);
    await act(async () => {
      root = createRoot(container);
      root.render(<AutomationsCatalog />);
    });
    await act(async () => container.querySelector<HTMLButtonElement>(".automation-row")?.click());
    const pause = Array.from(container.querySelectorAll<HTMLButtonElement>("button"))
      .find((button) => button.getAttribute("aria-label") === "暂停");
    await act(async () => pause?.click());
    expect(updateAutomation).toHaveBeenCalledWith({ id: task.id, paused: true });
  });

  it("auto-saves pending edits before closing the detail pane", async () => {
    const updateAutomation = vi.fn().mockImplementation(async (params) => ({
      task: { ...task, title: params.title ?? task.title },
    }));
    installApi([task], updateAutomation);
    await act(async () => {
      root = createRoot(container);
      root.render(<AutomationsCatalog />);
    });
    await act(async () => container.querySelector<HTMLButtonElement>(".automation-row")?.click());

    const name = container.querySelector<HTMLInputElement>('input[value="每日简报"]');
    expect(name).toBeTruthy();
    await act(async () => {
      setInputValue(name!, "每周简报");
    });
    expect(container.textContent).not.toContain("保存修改");

    const close = Array.from(container.querySelectorAll<HTMLButtonElement>("button"))
      .find((button) => button.getAttribute("aria-label") === "关闭详情");
    await act(async () => {
      close?.click();
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(updateAutomation).toHaveBeenCalledWith(expect.objectContaining({
      id: task.id,
      title: "每周简报",
    }));
    expect(container.querySelector(".automations-detail")).toBeNull();
  });

  it("edits a weekday schedule without exposing Cron", async () => {
    const updateAutomation = vi.fn().mockImplementation(async (params) => ({
      task: { ...task, cron: params.schedule ?? task.cron },
    }));
    installApi([task], updateAutomation);
    await act(async () => {
      root = createRoot(container);
      root.render(<AutomationsCatalog />);
    });
    await act(async () => container.querySelector<HTMLButtonElement>(".automation-row")?.click());

    expect(container.querySelector<HTMLSelectElement>(".automation-schedule-controls select")?.value)
      .toBe("weekdays");
    expect(container.textContent).toContain("工作日");
    expect(container.textContent).toContain("下次执行");
    expect(container.textContent).not.toContain("Cron 表达式");

    const time = container.querySelector<HTMLInputElement>('input[type="time"]');
    expect(time?.value).toBe("08:00");
    await act(async () => {
      setInputValue(time!, "18:30");
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(updateAutomation).toHaveBeenCalledWith(expect.objectContaining({
      schedule: "30 18 * * 1-5",
    }));
    expect(container.textContent).toContain("18:30");
  });

  it("restores the last valid draft and uses the shared toast when auto-save fails", async () => {
    const updateAutomation = vi.fn().mockRejectedValue(new Error("invalid cron"));
    installApi([task], updateAutomation);
    await act(async () => {
      root = createRoot(container);
      root.render(<AutomationsCatalog />);
    });
    await act(async () => container.querySelector<HTMLButtonElement>(".automation-row")?.click());

    const frequency = container.querySelector<HTMLSelectElement>(".automation-schedule-controls select");
    expect(frequency?.value).toBe("weekdays");
    await act(async () => {
      setSelectValue(frequency!, "custom");
    });
    const schedule = container.querySelector<HTMLInputElement>('input[value="0 8 * * 1-5"]');
    expect(schedule).toBeTruthy();
    await act(async () => {
      setInputValue(schedule!, "invalid");
      schedule?.focus();
      schedule?.blur();
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(schedule?.value).toBe("0 8 * * 1-5");
    const notice = document.body.querySelector('[role="alert"]');
    expect(notice?.classList.contains("archive-tip")).toBe(true);
    expect(notice?.textContent).toContain("已恢复上一次有效设置");
  });
});

function setInputValue(input: HTMLInputElement, value: string): void {
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
  setter?.call(input, value);
  input.dispatchEvent(new Event("input", { bubbles: true }));
}

function setSelectValue(select: HTMLSelectElement, value: string): void {
  const setter = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, "value")?.set;
  setter?.call(select, value);
  select.dispatchEvent(new Event("change", { bubbles: true }));
}

function installApi(tasks: AutomationTask[], updateAutomation = vi.fn()): void {
  const api: Partial<WuuDesktopApi> = {
    listAutomations: vi.fn().mockResolvedValue({ tasks }),
    updateAutomation,
    removeAutomation: vi.fn().mockResolvedValue({ ok: true }),
  };
  (window as unknown as { wuu: WuuDesktopApi }).wuu = api as WuuDesktopApi;
}
