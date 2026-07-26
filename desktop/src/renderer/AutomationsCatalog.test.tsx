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
  it("opens the detail sidebar only after selecting a task", async () => {
    installApi([task]);
    await act(async () => {
      root = createRoot(container);
      root.render(<AutomationsCatalog />);
    });

    expect(container.querySelector(".automations-detail")).toBeNull();
    expect(container.querySelector(".catalog-search input[type=\"search\"]")).toBeTruthy();
    const row = container.querySelector<HTMLButtonElement>(".automation-row");
    await act(async () => row?.click());
    expect(container.querySelector(".automations-detail")).toBeTruthy();
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
});

function installApi(tasks: AutomationTask[], updateAutomation = vi.fn()): void {
  const api: Partial<WuuDesktopApi> = {
    listAutomations: vi.fn().mockResolvedValue({ tasks }),
    updateAutomation,
    removeAutomation: vi.fn().mockResolvedValue({ ok: true }),
  };
  (window as unknown as { wuu: WuuDesktopApi }).wuu = api as WuuDesktopApi;
}
