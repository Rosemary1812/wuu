import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type {
  MemoryChatResult,
  MemoryOverviewResult,
  MemoryReadResult,
  ParticipantProfile,
  WuuDesktopApi,
} from "../shared/protocol";
import { MemoryPanel } from "./MemoryPanel";

type GlobalWindow = typeof window & { wuu: WuuDesktopApi };

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
  // Drop the stub so each test installs its own.
  delete (globalThis as { wuu?: WuuDesktopApi }).wuu;
  delete (window as { wuu?: WuuDesktopApi }).wuu;
});

type MemoryStub = {
  getMemoryOverview: ReturnType<typeof vi.fn>;
  sendMemoryChat: ReturnType<typeof vi.fn>;
  readMemoryRaw: ReturnType<typeof vi.fn>;
};

function overviewFixture(
  overrides: Partial<MemoryOverviewResult> = {},
): MemoryOverviewResult {
  return {
    essay_md: "## 协作偏好\n\n喜欢中文简短回复。",
    generated_at: "2026-07-04T10:00:00Z",
    source_mtime: "2026-07-04T09:00:00Z",
    cached: false,
    ...overrides,
  };
}

function chatFixture(): MemoryChatResult {
  return {
    reply_md: "已删除关于旧项目路径的那条记忆。",
    changed_files: [
      { path: "memory/old-project-path.md", action: "deleted" },
      { path: "memory/MEMORY.md", action: "modified" },
    ],
  };
}

function readFixture(): MemoryReadResult {
  return {
    index_md: "- [协作偏好](preferences.md) — 喜欢中文简短回复",
    files: [
      {
        name: "preferences.md",
        description: "用户的协作偏好",
        type: "user",
        mtime: "2026-07-03T08:00:00Z",
      },
    ],
  };
}

function participantsFixture(): ParticipantProfile[] {
  return [
    { id: "p-1", kind: "named", name: "小青", role: "reviewer" },
    { id: "p-2", kind: "named", name: "阿蓝", role: "worker" },
    { id: "h-1", kind: "human", name: "本人" },
  ];
}

function installMemoryStub(overrides: Partial<MemoryStub> = {}): MemoryStub {
  const stub: MemoryStub = {
    getMemoryOverview: vi.fn().mockResolvedValue(overviewFixture()),
    sendMemoryChat: vi.fn().mockResolvedValue(chatFixture()),
    readMemoryRaw: vi.fn().mockResolvedValue(readFixture()),
    ...overrides,
  };
  (globalThis as { wuu?: WuuDesktopApi }).wuu = stub as unknown as WuuDesktopApi;
  (window as unknown as GlobalWindow).wuu = stub as unknown as WuuDesktopApi;
  return stub;
}

function mount(
  participants: ParticipantProfile[] = [],
  focusParticipantID?: string,
): void {
  act(() => {
    root = createRoot(container);
    root!.render(
      <MemoryPanel
        participants={participants}
        focusParticipantID={focusParticipantID}
      />,
    );
  });
}

function setInputValue(input: HTMLInputElement, value: string): void {
  const setter = Object.getOwnPropertyDescriptor(
    HTMLInputElement.prototype,
    "value",
  )?.set;
  setter?.call(input, value);
  input.dispatchEvent(new Event("input", { bubbles: true }));
}

function findButton(text: string): HTMLButtonElement | undefined {
  return Array.from(container.querySelectorAll("button")).find((button) =>
    button.textContent?.includes(text),
  ) as HTMLButtonElement | undefined;
}

function rootText(): string {
  return container.textContent ?? "";
}

// electron 的 invoke 会把 appserver 的错误文案包一层再抛回渲染进程；
// 后端没有 M2 时的真实形态就是这个样子。
function unknownMethodError(method: string): Error {
  return new Error(
    `Error invoking remote method 'wuu:${method}': Error: unknown method "memory/${method}"`,
  );
}

describe("MemoryPanel overview", () => {
  it("shows a skeleton while memory/overview is pending, then renders the essay", async () => {
    let resolveOverview: (value: MemoryOverviewResult) => void = () => {};
    const pending = new Promise<MemoryOverviewResult>((resolve) => {
      resolveOverview = resolve;
    });
    const stub = installMemoryStub({
      getMemoryOverview: vi.fn().mockReturnValue(pending),
    });
    mount();

    expect(
      container.querySelector("[data-testid=\"memory-skeleton\"]"),
    ).not.toBeNull();
    expect(stub.getMemoryOverview).toHaveBeenCalledWith({ scope: "user" });

    await act(async () => {
      resolveOverview(overviewFixture());
      await Promise.resolve();
    });

    expect(
      container.querySelector("[data-testid=\"memory-skeleton\"]"),
    ).toBeNull();
    expect(rootText()).toContain("协作偏好");
    expect(rootText()).toContain("喜欢中文简短回复");
  });

  it("degrades to the backend-not-ready placeholder on unknown method errors", async () => {
    installMemoryStub({
      getMemoryOverview: vi
        .fn()
        .mockRejectedValue(unknownMethodError("memory-overview")),
    });
    mount();
    await act(async () => {
      await Promise.resolve();
    });

    expect(
      container.querySelector("[data-testid=\"memory-backend-missing\"]"),
    ).not.toBeNull();
    expect(rootText()).toContain("记忆面板后端尚未就绪");
    expect(
      container.querySelector("[data-testid=\"memory-skeleton\"]"),
    ).toBeNull();
  });

  it("surfaces other errors with a retry affordance", async () => {
    installMemoryStub({
      getMemoryOverview: vi.fn().mockRejectedValue(new Error("boom")),
    });
    mount();
    await act(async () => {
      await Promise.resolve();
    });

    expect(rootText()).toContain("boom");
    expect(findButton("重试")).not.toBeUndefined();
  });
});

describe("MemoryPanel 同事 tab", () => {
  it("lists only named agents in the picker and requests the participant scope", async () => {
    const stub = installMemoryStub();
    mount(participantsFixture());
    await act(async () => {
      await Promise.resolve();
    });

    const tab = findButton("同事");
    expect(tab).not.toBeUndefined();
    await act(async () => {
      tab?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
      await Promise.resolve();
    });

    const trigger = container.querySelector(
      "[data-testid=\"memory-agent-select\"]",
    ) as HTMLButtonElement | null;
    expect(trigger).not.toBeNull();
    // The SelectMenu renders its options into a floating portal on open.
    act(() => {
      trigger!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    const options = Array.from(
      document.querySelectorAll(".select-menu-panel .select-menu-item"),
    );
    expect(options.map((option) => option.getAttribute("data-value"))).toEqual([
      "p-1",
      "p-2",
    ]);
    // Human members are excluded; the closed trigger shows the active agent.
    expect(rootText()).toContain("小青");
    expect(rootText()).not.toContain("本人");

    expect(stub.getMemoryOverview).toHaveBeenLastCalledWith({
      scope: "participant",
      participant_id: "p-1",
    });
  });

  it("preselects the focused participant notebook when focusParticipantID is set", async () => {
    const stub = installMemoryStub();
    mount(participantsFixture(), "p-2");
    await act(async () => {
      await Promise.resolve();
    });

    expect(stub.getMemoryOverview).toHaveBeenCalledWith({
      scope: "participant",
      participant_id: "p-2",
    });
    const trigger = container.querySelector(
      "[data-testid=\"memory-agent-select\"]",
    ) as HTMLButtonElement | null;
    expect(trigger?.querySelector(".select-menu-value")?.textContent).toBe(
      "阿蓝",
    );
  });

  it("shows an empty state instead of a request when no named agent exists", async () => {
    const stub = installMemoryStub();
    mount([]);
    await act(async () => {
      await Promise.resolve();
    });

    const tab = findButton("同事");
    await act(async () => {
      tab?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
      await Promise.resolve();
    });

    expect(rootText()).toContain("还没有同事");
    // The initial user-scope fetch is the only call — no participant fetch
    // without a participant to target.
    expect(stub.getMemoryOverview).toHaveBeenCalledTimes(1);
  });
});

describe("MemoryPanel chat", () => {
  it("sends memory/chat, shows the pending slot, then reply + changed files, and refreshes the overview", async () => {
    let resolveChat: (value: MemoryChatResult) => void = () => {};
    const pendingChat = new Promise<MemoryChatResult>((resolve) => {
      resolveChat = resolve;
    });
    const stub = installMemoryStub({
      sendMemoryChat: vi.fn().mockReturnValue(pendingChat),
    });
    mount();
    await act(async () => {
      await Promise.resolve();
    });

    const input = container.querySelector(
      "[data-testid=\"memory-chat-input\"]",
    ) as HTMLInputElement | null;
    expect(input).not.toBeNull();
    await act(async () => {
      setInputValue(input!, "删掉关于旧项目路径的记忆");
    });

    const sendButton = findButton("发送");
    expect(sendButton?.disabled).toBe(false);
    await act(async () => {
      sendButton?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
      await Promise.resolve();
    });

    expect(stub.sendMemoryChat).toHaveBeenCalledWith({
      scope: "user",
      message: "删掉关于旧项目路径的记忆",
    });
    expect(rootText()).toContain("整理中");

    await act(async () => {
      resolveChat(chatFixture());
      await Promise.resolve();
    });

    expect(rootText()).not.toContain("整理中");
    expect(rootText()).toContain("已删除关于旧项目路径的那条记忆。");
    expect(rootText()).toContain("变更了 2 个记忆文件");
    expect(rootText()).toContain("memory/old-project-path.md");
    // 契约：chat 返回后自动重新拉取 overview（初始 1 次 + 刷新 1 次）。
    expect(stub.getMemoryOverview).toHaveBeenCalledTimes(2);
  });

  it("shows the backend-not-ready note in the reply slot when chat hits unknown method", async () => {
    const stub = installMemoryStub({
      sendMemoryChat: vi
        .fn()
        .mockRejectedValue(unknownMethodError("memory-chat")),
    });
    mount();
    await act(async () => {
      await Promise.resolve();
    });

    const input = container.querySelector(
      "[data-testid=\"memory-chat-input\"]",
    ) as HTMLInputElement | null;
    await act(async () => {
      setInputValue(input!, "记住我喜欢简短回复");
    });
    const sendButton = findButton("发送");
    await act(async () => {
      sendButton?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
      await Promise.resolve();
    });

    expect(rootText()).toContain("记忆面板后端尚未就绪");
    // 失败的 chat 不触发 overview 刷新。
    expect(stub.getMemoryOverview).toHaveBeenCalledTimes(1);
  });
});

describe("MemoryPanel 查看原文", () => {
  it("toggles the raw view: memory/read index and file inventory", async () => {
    const stub = installMemoryStub();
    mount();
    await act(async () => {
      await Promise.resolve();
    });
    expect(stub.readMemoryRaw).not.toHaveBeenCalled();

    const toggle = container.querySelector(
      "[data-testid=\"memory-raw-toggle\"]",
    ) as HTMLButtonElement | null;
    expect(toggle).not.toBeNull();
    await act(async () => {
      toggle?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
      await Promise.resolve();
    });

    expect(stub.readMemoryRaw).toHaveBeenCalledWith({ scope: "user" });
    expect(
      container.querySelector("[data-testid=\"memory-raw-view\"]"),
    ).not.toBeNull();
    expect(rootText()).toContain("协作偏好");
    expect(rootText()).toContain("preferences.md");
    expect(rootText()).toContain("用户的协作偏好");
    // frontmatter type "user" 显示为中文标签。
    expect(rootText()).toContain("用户");
  });
});
