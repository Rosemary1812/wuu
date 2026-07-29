import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AutomationTask, DesktopProject, WuuDesktopApi } from "../shared/protocol";
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

const project: DesktopProject = {
  id: "project-1",
  name: "Example",
  path: "/workspaces/example",
  created_at: "2026-07-27T00:00:00Z",
  updated_at: "2026-07-27T00:00:00Z",
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
  it("creates a paused automation from a workspace-bound draft", async () => {
    const created = {
      ...task,
      id: "automation-new",
      title: "新自动化",
      prompt: "",
      paused: true,
      workspaceId: project.id,
      workspacePath: project.path,
    };
    const createAutomation = vi.fn().mockResolvedValue({ task: created });
    const updateAutomation = vi.fn().mockImplementation(async (params) => ({
      task: {
        ...created,
        workspaceId: params.workspace_id,
        workspacePath: params.workspace_path,
      },
    }));
    installApi([], updateAutomation, createAutomation);
    await act(async () => {
      root = createRoot(container);
      root.render(<AutomationsCatalog projects={[project]} />);
    });

    const createButton = Array.from(container.querySelectorAll<HTMLButtonElement>("button"))
      .find((button) => button.textContent?.includes("新建自动化"));
    expect(createButton?.closest(".automations-heading-row")).toBeTruthy();
    expect(createButton?.closest(".catalog-page-controls")).toBeNull();
    // The workspace picker lives in the new-draft detail, not the page header.
    expect(container.querySelector(".automations-heading-row .settings-select-trigger")).toBeNull();
    await act(async () => {
      createButton?.click();
      await Promise.resolve();
    });

    // Nothing is created until the draft's create action runs.
    expect(createAutomation).not.toHaveBeenCalled();
    const workspaceTrigger = container.querySelector<HTMLButtonElement>('.automation-workspace-field [aria-label="工作区"]');
    expect(workspaceTrigger?.textContent).toContain("Example");

    const submit = container.querySelector<HTMLButtonElement>(".automation-detail-create-bar button");
    expect(submit?.disabled).toBe(false);
    await act(async () => {
      submit?.click();
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(createAutomation).toHaveBeenCalledWith(expect.objectContaining({
      title: "新自动化",
      prompt: "",
      paused: true,
      recurring: true,
    }));
    // After creation the workspace locks to static text.
    expect(container.querySelector(".automation-workspace-field")?.textContent)
      .toContain("/workspaces/example");
    expect(container.querySelector('.automation-workspace-field [aria-label="工作区"]')).toBeNull();
    expect(container.querySelector<HTMLTextAreaElement>("textarea")?.rows).toBe(4);
  });

  it("lets the draft pick a different workspace before creating", async () => {
    const other: DesktopProject = {
      id: "project-2",
      name: "Second",
      path: "/workspaces/second",
      created_at: "2026-07-27T00:00:00Z",
      updated_at: "2026-07-27T00:00:00Z",
    };
    const createAutomation = vi.fn().mockImplementation(async (params) => ({
      task: {
        ...task,
        id: "automation-new",
        workspaceId: params.workspace_id,
        workspacePath: params.workspace_path,
      },
    }));
    installApi([], vi.fn(), createAutomation);
    await act(async () => {
      root = createRoot(container);
      root.render(<AutomationsCatalog projects={[project, other]} activeProjectID={project.id} />);
    });

    const createButton = Array.from(container.querySelectorAll<HTMLButtonElement>("button"))
      .find((button) => button.textContent?.includes("新建自动化"));
    await act(async () => {
      createButton?.click();
      await Promise.resolve();
    });

    const workspaceTrigger = container.querySelector<HTMLButtonElement>('.automation-workspace-field [aria-label="工作区"]');
    expect(workspaceTrigger?.textContent).toContain("Example");
    await chooseSelectMenu(workspaceTrigger!, "project-2");

    const submit = container.querySelector<HTMLButtonElement>(".automation-detail-create-bar button");
    await act(async () => {
      submit?.click();
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(createAutomation).toHaveBeenCalledWith(expect.objectContaining({
      workspace_id: "project-2",
      workspace_path: "/workspaces/second",
    }));
  });

  it("discards an unsaved draft without creating", async () => {
    const createAutomation = vi.fn();
    installApi([], vi.fn(), createAutomation);
    await act(async () => {
      root = createRoot(container);
      root.render(<AutomationsCatalog projects={[project]} activeProjectID={project.id} />);
    });

    const createButton = Array.from(container.querySelectorAll<HTMLButtonElement>("button"))
      .find((button) => button.textContent?.includes("新建自动化"));
    await act(async () => {
      createButton?.click();
      await Promise.resolve();
    });
    expect(container.querySelector(".automations-detail")).toBeTruthy();

    const close = Array.from(container.querySelectorAll<HTMLButtonElement>("button"))
      .find((button) => button.getAttribute("aria-label") === "关闭详情");
    await act(async () => {
      close?.click();
      await Promise.resolve();
    });

    expect(createAutomation).not.toHaveBeenCalled();
    expect(container.querySelector(".automations-detail")).toBeNull();
  });

  it("disables creation when no workspace is available", async () => {
    installApi([], vi.fn(), vi.fn());
    await act(async () => {
      root = createRoot(container);
      root.render(<AutomationsCatalog />);
    });

    const createButton = Array.from(container.querySelectorAll<HTMLButtonElement>("button"))
      .find((button) => button.textContent?.includes("新建自动化"));
    expect(createButton?.disabled).toBe(true);
  });

  it("does not white-screen when listAutomations returns tasks: null", async () => {
    installApi([], vi.fn(), vi.fn(), { tasks: null });
    await act(async () => {
      root = createRoot(container);
      root.render(<AutomationsCatalog projects={[project]} activeProjectID={project.id} />);
    });

    expect(container.querySelector(".catalog-page-header")).toBeTruthy();
    expect(container.textContent).toContain("暂无自动化");
    expect(container.querySelector(".automation-row")).toBeNull();
  });

  it("does not white-screen when listAutomations omits the tasks field", async () => {
    installApi([], vi.fn(), vi.fn(), { tasks: undefined });
    await act(async () => {
      root = createRoot(container);
      root.render(<AutomationsCatalog projects={[project]} activeProjectID={project.id} />);
    });

    expect(container.querySelector(".catalog-page-header")).toBeTruthy();
    expect(container.textContent).toContain("暂无自动化");
    expect(container.querySelector(".automation-row")).toBeNull();
  });

  it("separates an hourly interval from its concrete next run", async () => {
    const updateAutomation = vi.fn().mockImplementation(async (params) => ({
      task: {
        ...task,
        cron: params.schedule ?? "0 * * * *",
        recurring: params.recurring ?? true,
      },
    }));
    installApi([{ ...task, cron: "0 * * * *" }], updateAutomation);
    await act(async () => {
      root = createRoot(container);
      root.render(<AutomationsCatalog />);
    });

    expect(container.textContent).toContain("每小时");
    expect(container.textContent).toContain("下次");
    expect(container.textContent).not.toContain("第 00 分钟");

    await act(async () => container.querySelector<HTMLButtonElement>(".automation-row")?.click());
    expect(container.querySelector('[aria-label="执行方式"]')?.textContent).toContain("重复执行");
    expect(container.querySelector('[aria-label="执行分钟"]')?.textContent).toContain(":00");
    expect(container.querySelector(".automation-schedule-controls select")).toBeNull();

    await act(async () => container.querySelector<HTMLButtonElement>('[aria-label="执行分钟"]')?.click());
    expect(document.body.querySelectorAll(".minute-clock-tick")).toHaveLength(60);
    const minute50 = document.body.querySelector<HTMLButtonElement>('[data-minute="50"]');
    expect(minute50).toBeTruthy();
    await act(async () => {
      minute50?.click();
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(updateAutomation).toHaveBeenCalledWith(expect.objectContaining({ schedule: "50 * * * *" }));

    await act(async () => container.querySelector<HTMLButtonElement>('[aria-label="执行分钟"]')?.click());
    const clockFace = document.body.querySelector<HTMLDivElement>('.minute-clock-face[role="slider"]');
    await act(async () => clockFace?.dispatchEvent(new KeyboardEvent("keydown", {
      key: "ArrowRight",
      bubbles: true,
    })));
    expect(clockFace?.getAttribute("aria-valuenow")).toBe("51");
    await act(async () => {
      clockFace?.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true }));
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(updateAutomation).toHaveBeenCalledWith(expect.objectContaining({ schedule: "51 * * * *" }));

    const executionMode = container.querySelector<HTMLButtonElement>('[aria-label="执行方式"]');
    await chooseSelectMenu(executionMode!, "once");
    expect(updateAutomation).toHaveBeenCalledWith(expect.objectContaining({ recurring: false }));
    expect(container.querySelector('input[type="checkbox"]')).toBeNull();
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
      reservedWidth: 570,
    });
    const separator = container.querySelector<HTMLButtonElement>('[role="separator"]');
    await act(async () => separator?.dispatchEvent(new KeyboardEvent("keydown", {
      key: "ArrowLeft",
      bubbles: true,
    })));
    expect(separator?.getAttribute("aria-valuenow")).toBe("592");
    expect(onDetailPaneLayoutChange).toHaveBeenLastCalledWith({
      open: true,
      reservedWidth: 602,
    });
    await act(async () => separator?.dispatchEvent(new KeyboardEvent("keydown", {
      key: "Home",
      bubbles: true,
    })));
    expect(separator?.getAttribute("aria-valuenow")).toBe("480");
    expect(onDetailPaneLayoutChange).toHaveBeenLastCalledWith({
      open: true,
      reservedWidth: 490,
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

  it("selects a valid timezone from a searchable list and saves it", async () => {
    const updateAutomation = vi.fn().mockResolvedValue({
      task: { ...task, timezone: "UTC" },
    });
    installApi([task], updateAutomation);
    await act(async () => {
      root = createRoot(container);
      root.render(<AutomationsCatalog />);
    });
    await act(async () => container.querySelector<HTMLButtonElement>(".automation-row")?.click());

    const timezone = container.querySelector<HTMLButtonElement>('[aria-label="时区"]');
    expect(timezone?.textContent).toContain("Asia/Shanghai");
    expect(container.querySelector('input[value="Asia/Shanghai"]')).toBeNull();
    await act(async () => timezone?.click());

    const search = document.body.querySelector<HTMLInputElement>('[aria-label="搜索时区"]');
    expect(search).toBeTruthy();
    await act(async () => setInputValue(search!, "日本"));
    const tokyo = document.body.querySelector<HTMLButtonElement>('.select-menu-item[data-value="Asia/Tokyo"]');
    expect(tokyo?.textContent).toContain("日本标准时间");
    await act(async () => {
      tokyo?.click();
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(updateAutomation).toHaveBeenCalledWith(expect.objectContaining({ timezone: "Asia/Tokyo" }));
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

    expect(container.querySelector<HTMLButtonElement>('[aria-label="运行间隔"]')?.textContent)
      .toContain("工作日");
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

    const frequency = container.querySelector<HTMLButtonElement>('[aria-label="运行间隔"]');
    expect(frequency?.textContent).toContain("工作日");
    await chooseSelectMenu(frequency!, "custom");
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

async function chooseSelectMenu(trigger: HTMLButtonElement, value: string): Promise<void> {
  await act(async () => {
    trigger.click();
    await Promise.resolve();
  });
  const option = document.body.querySelector<HTMLButtonElement>(`.select-menu-item[data-value="${value}"]`);
  expect(option).toBeTruthy();
  await act(async () => option?.click());
}

function installApi(
  tasks: AutomationTask[],
  updateAutomation = vi.fn(),
  createAutomation = vi.fn(),
  listResult?: { tasks: AutomationTask[] | null | undefined },
): void {
  const api: Partial<WuuDesktopApi> = {
    listAutomations: vi.fn().mockResolvedValue(listResult ?? { tasks }),
    createAutomation,
    updateAutomation,
    removeAutomation: vi.fn().mockResolvedValue({ ok: true }),
  };
  (window as unknown as { wuu: WuuDesktopApi }).wuu = api as WuuDesktopApi;
}
