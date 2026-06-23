import { act, createRef } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  Composer,
  SplitPaneComposer,
  permissionModeFromSummary,
  permissionModeHasAdvancedOverrides,
  type CodexModelLoadState,
  type ComposerVariant,
  type PermissionMode,
} from "./ComposerView";
import type { QueuedComposerMessage } from "./ComposerMessages";
import type {
  InitializeResult,
  PermissionSummary,
  RuntimeContext,
  SkillSummary,
  ToolPolicySummary,
  WuuDesktopApi,
} from "../shared/protocol";

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
  delete (globalThis as { wuu?: WuuDesktopApi }).wuu;
  container.remove();
  document.body
    .querySelectorAll("[data-floating-menu-owner=\"composer-access\"]")
    .forEach((element) => element.remove());
});

function initialized(toolPolicy?: ToolPolicySummary, permissions?: PermissionSummary): InitializeResult {
  return {
    protocol_version: "wuu-app-server/v0.1",
    provider: "fake",
    model: "fake-model",
    workspace_root: "/tmp/project",
    tool_policy: toolPolicy,
    permissions,
    providers: [
      {
        name: "fake",
        type: "openai-compatible",
        model: "fake-model",
      },
    ],
  };
}

function renderComposer(props: {
  accessMenuOpen?: boolean;
  variant?: ComposerVariant;
  prompt?: string;
  running?: boolean;
  queuedMessages?: QueuedComposerMessage[];
  guideMessages?: QueuedComposerMessage[];
  toolPolicy?: ToolPolicySummary;
  status?: string;
  onInterrupt?: () => void;
  onSend?: () => void;
  onRemoveQueuedMessage?: (id: string) => void;
  onRemoveGuideMessage?: (id: string) => void;
  onGuideQueuedMessage?: (id: string) => void;
  onEditQueuedMessage?: (id: string) => void;
  onEditGuideMessage?: (id: string) => void;
  permissions?: PermissionSummary;
  activeContext?: RuntimeContext;
  setPrompt?: (value: string) => void;
  onSelectPermissionMode?: (mode: PermissionMode) => void;
  tokensPerSecond?: number;
  tokenSpeedSampledAt?: number;
  tokenSpeedSource?: "real" | "estimated" | "none";
}): { onSelectPermissionMode: (mode: PermissionMode) => void } {
  const codexModels: CodexModelLoadState = {
    loading: false,
    error: "",
    models: [],
  };
  const onSelectPermissionMode = props.onSelectPermissionMode ?? vi.fn();
  act(() => {
    root = createRoot(container);
    root.render(
      <Composer
        variant={props.variant}
        prompt={props.prompt ?? ""}
        setPrompt={props.setPrompt ?? (() => {})}
        files={[]}
        images={[]}
        queuedMessages={props.queuedMessages ?? []}
        guideMessages={props.guideMessages ?? []}
        running={props.running ?? false}
        status={props.status ?? "ready"}
        readOnly={false}
        initialized={initialized(props.toolPolicy, props.permissions)}
        projects={[]}
        activeContext={props.activeContext}
        codexModels={codexModels}
        codexRuntimeMenu={null}
        codexRuntimeRef={createRef<HTMLDivElement>()}
        menuOpen={false}
        accessMenuOpen={props.accessMenuOpen ?? false}
        branchMenuOpen={false}
        menuRef={createRef<HTMLDivElement>()}
        accessMenuRef={createRef<HTMLDivElement>()}
        projectFilter=""
        setProjectFilter={() => {}}
        onToggleMenu={() => {}}
        onToggleAccessMenu={() => {}}
        onToggleCodexRuntimeMenu={() => {}}
        onSelectRuntimeModel={() => {}}
        onSelectRuntimeEffort={() => {}}
        onSelectPermissionMode={onSelectPermissionMode}
        onToggleBranchMenu={() => {}}
        onOpenSettings={() => {}}
        onOpenSkillsCatalog={() => {}}
        onSelectProject={() => {}}
        onSelectNoProject={() => {}}
        onSelectGitBranch={() => {}}
        onCreateProject={() => {}}
        onOpenProject={() => {}}
        onStartNewThread={() => {}}
        onOpenWorkspaceTool={() => {}}
        onPasteAttachmentFiles={() => {}}
        onRemoveFile={() => {}}
        onRemoveImage={() => {}}
        onRemoveQueuedMessage={props.onRemoveQueuedMessage ?? (() => {})}
        onRemoveGuideMessage={props.onRemoveGuideMessage ?? (() => {})}
        onGuideQueuedMessage={props.onGuideQueuedMessage ?? (() => {})}
        onEditQueuedMessage={props.onEditQueuedMessage ?? (() => {})}
        onEditGuideMessage={props.onEditGuideMessage ?? (() => {})}
        onSend={props.onSend ?? (() => {})}
        onInterrupt={props.onInterrupt ?? (() => {})}
        tokensPerSecond={props.tokensPerSecond ?? 0}
        tokenSpeedSampledAt={props.tokenSpeedSampledAt}
        tokenSpeedSource={props.tokenSpeedSource}
      />,
    );
  });
  return { onSelectPermissionMode };
}

function installSkillList(skills: SkillSummary[]): void {
  (globalThis as { wuu?: Partial<WuuDesktopApi> }).wuu = {
    listSkills: vi.fn().mockResolvedValue({ skills }),
  };
}

function renderSplitPaneComposer(props: {
  prompt?: string;
  running?: boolean;
  status?: string;
  onSend?: () => void;
}): void {
  act(() => {
    root = createRoot(container);
    root.render(
      <SplitPaneComposer
        prompt={props.prompt ?? ""}
        setPrompt={() => {}}
        files={[]}
        images={[]}
        running={props.running ?? false}
        readOnly={false}
        status={props.status ?? "ready"}
        onPasteAttachmentFiles={() => {}}
        onRemoveFile={() => {}}
        onRemoveImage={() => {}}
        onSend={props.onSend ?? (() => {})}
        onInterrupt={() => {}}
      />,
    );
  });
}

async function nextAnimationFrame(): Promise<void> {
  await new Promise<void>((resolve) => window.requestAnimationFrame(() => resolve()));
}

describe("Composer send control", () => {
  it("shows one stateful action button while a request is running", () => {
    const onInterrupt = vi.fn();
    const onSend = vi.fn();
    renderComposer({
      prompt: "queued follow-up",
      running: true,
      onInterrupt,
      onSend,
    });

    expect(
      container.querySelectorAll(".composer-action-button.composer-stop-button"),
    ).toHaveLength(1);
    expect(container.querySelector("button[aria-label=\"发送\"]")).toBeNull();
    expect(container.querySelector("button[aria-label=\"排队发送\"]")).toBeNull();

    const stopButton = container.querySelector<HTMLButtonElement>("button[aria-label=\"停止\"]");
    expect(stopButton).not.toBeNull();

    act(() => {
      stopButton?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });

    expect(onInterrupt).toHaveBeenCalledTimes(1);
    expect(onSend).not.toHaveBeenCalled();
  });

  it("returns focus to the textarea after clicking send", async () => {
    const onSend = vi.fn();
    renderComposer({
      prompt: "send this",
      onSend,
    });

    const textarea = container.querySelector<HTMLTextAreaElement>("textarea");
    const sendButton = container.querySelector<HTMLButtonElement>("button[aria-label=\"发送\"]");
    expect(textarea).not.toBeNull();
    expect(sendButton).not.toBeNull();

    act(() => {
      sendButton?.focus();
      sendButton?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });

    await act(async () => {
      await nextAnimationFrame();
    });

    expect(onSend).toHaveBeenCalledTimes(1);
    expect(document.activeElement).toBe(textarea);
  });

  it("returns focus to the split-pane textarea after clicking send", async () => {
    const onSend = vi.fn();
    renderSplitPaneComposer({
      prompt: "continue this branch",
      onSend,
    });

    const textarea = container.querySelector<HTMLTextAreaElement>("textarea");
    const sendButton = container.querySelector<HTMLButtonElement>("button[aria-label=\"发送\"]");
    expect(textarea).not.toBeNull();
    expect(sendButton).not.toBeNull();

    act(() => {
      sendButton?.focus();
      sendButton?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });

    await act(async () => {
      await nextAnimationFrame();
    });

    expect(onSend).toHaveBeenCalledTimes(1);
    expect(document.activeElement).toBe(textarea);
  });

  it("hides the transient sending status from the composer bar", () => {
    renderComposer({
      prompt: "queued follow-up",
      running: true,
      status: "正在发送请求",
    });

    expect(container.querySelector(".status-label")).toBeNull();
    expect(container.textContent).not.toContain("正在发送请求");
  });

  it("keeps non-transient composer status visible", () => {
    renderComposer({
      prompt: "retry later",
      status: "发送失败",
    });

    expect(container.querySelector(".status-label")?.textContent).toBe("发送失败");
  });

  it("hides the transient sending status from the split-pane composer bar", () => {
    renderSplitPaneComposer({
      prompt: "continue this branch",
      running: true,
      status: "正在发送请求",
    });

    expect(container.querySelector(".split-composer-status")).toBeNull();
    expect(container.textContent).not.toContain("正在发送请求");
  });

  it("hides session context chips in the dock composer", () => {
    renderComposer({
      variant: "dock",
      prompt: "follow up",
    });

    expect(container.querySelector(".composer-context-bar")).toBeNull();
    expect(container.querySelector(".context-project-button")).toBeNull();
  });

  it("keeps context chips in the hero composer before a session starts", () => {
    renderComposer({
      variant: "hero",
    });

    expect(container.querySelector(".composer-context-bar")).not.toBeNull();
    expect(container.querySelector(".context-project-button")).not.toBeNull();
  });

  it("inserts a selected skill slash command into the composer", async () => {
    const setPrompt = vi.fn();
    installSkillList([
      {
        name: "slides",
        description: "Create slide decks",
        source: "bundled",
        user_invocable: true,
        disable_model_invoke: false,
      },
    ]);
    renderComposer({
      prompt: "/sli",
      setPrompt,
      activeContext: { kind: "project", project_id: "repo", cwd: "/repo" },
    });

    await act(async () => {
      await Promise.resolve();
    });

    const skillButton = Array.from(
      container.querySelectorAll<HTMLButtonElement>(".slash-command-item"),
    ).find((button) => button.textContent?.includes("/slides"));
    expect(skillButton).not.toBeUndefined();

    act(() => {
      skillButton?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });

    expect(setPrompt).toHaveBeenCalledWith("/slides ");
  });

  it("sends an exact slash command with arguments on Enter", () => {
    const onSend = vi.fn();
    renderComposer({
      prompt: "/debug 登录失败",
      onSend,
      activeContext: { kind: "project", project_id: "repo", cwd: "/repo" },
    });

    const textarea = container.querySelector<HTMLTextAreaElement>("textarea");
    expect(textarea).not.toBeNull();

    act(() => {
      textarea?.dispatchEvent(
        new KeyboardEvent("keydown", {
          key: "Enter",
          bubbles: true,
          cancelable: true,
        }),
      );
    });

    expect(onSend).toHaveBeenCalledTimes(1);
  });
});

describe("Composer queue strip", () => {
  it("renders queued and guide messages in combined sequential order", () => {
    renderComposer({
      running: true,
      queuedMessages: [
        { id: "queue-1", text: "第一个排队消息", images: [], files: [] },
        { id: "queue-2", text: "第二个排队消息", images: [], files: [] }
      ],
      guideMessages: [
        { id: "guide-1", text: "唯一一条引导消息", images: [], files: [] }
      ]
    });

    const rows = Array.from(
      container.querySelectorAll<HTMLLIElement>(".composer-queue-row")
    );
    expect(rows).toHaveLength(3);
    // guide (oldest, first) → queue items follow in queue order
    expect(rows[0]?.dataset.position).toBe("1");
    expect(rows[0]?.classList.contains("guide")).toBe(true);
    expect(rows[0]?.querySelector(".composer-queue-index")?.textContent).toBe("1");
    expect(rows[1]?.dataset.position).toBe("2");
    expect(rows[1]?.classList.contains("queue")).toBe(true);
    expect(rows[1]?.querySelector(".composer-queue-index")?.textContent).toBe("2");
    expect(rows[2]?.dataset.position).toBe("3");
    expect(rows[2]?.classList.contains("queue")).toBe(true);
    expect(rows[2]?.querySelector(".composer-queue-index")?.textContent).toBe("3");
  });

  it("lives inside the composer shell so the queue spans the input width", () => {
    renderComposer({
      running: true,
      queuedMessages: [
        { id: "queue-1", text: "排队宽度测试", images: [], files: [] }
      ]
    });

    const list = container.querySelector(".composer-queue-list");
    const shell = container.querySelector(".composer-shell");
    expect(list).not.toBeNull();
    expect(shell).not.toBeNull();
    expect(shell?.contains(list ?? null)).toBe(true);
  });

  it("lets a queued message become a guide from a single inline button click", () => {
    const onGuideQueuedMessage = vi.fn();
    renderComposer({
      running: true,
      queuedMessages: [
        { id: "queue-1", text: "要求后续变更", images: [], files: [] }
      ],
      onGuideQueuedMessage
    });

    const guideButton = container.querySelector<HTMLButtonElement>(
      "button[aria-label=\"转为引导 1\"]"
    );
    expect(guideButton).not.toBeNull();

    act(() => {
      guideButton?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });

    expect(onGuideQueuedMessage).toHaveBeenCalledWith("queue-1");
  });

  it("removes a queued message from the inline close button", () => {
    const onRemoveQueuedMessage = vi.fn();
    renderComposer({
      running: true,
      queuedMessages: [
        { id: "queue-1", text: "准备删除", images: [], files: [] }
      ],
      onRemoveQueuedMessage
    });

    const removeButton = container.querySelector<HTMLButtonElement>(
      "button[aria-label=\"移除排队消息 1\"]"
    );
    expect(removeButton).not.toBeNull();

    act(() => {
      removeButton?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });

    expect(onRemoveQueuedMessage).toHaveBeenCalledWith("queue-1");
  });

  it("edits a queued message by clicking the preview text", () => {
    const onEditQueuedMessage = vi.fn();
    renderComposer({
      running: true,
      queuedMessages: [
        { id: "queue-1", text: "要求后续变更", images: [], files: [] }
      ],
      onEditQueuedMessage
    });

    const previewButton = container.querySelector<HTMLButtonElement>(
      "button[aria-label=\"编辑排队消息 1\"]"
    );
    expect(previewButton).not.toBeNull();

    act(() => {
      previewButton?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });

    expect(onEditQueuedMessage).toHaveBeenCalledWith("queue-1");
  });

  it("cancels a guide from a single inline button click", () => {
    const onRemoveGuideMessage = vi.fn();
    renderComposer({
      running: true,
      guideMessages: [
        { id: "guide-1", text: "已引导消息", images: [], files: [] }
      ],
      onRemoveGuideMessage
    });

    const cancelButton = container.querySelector<HTMLButtonElement>(
      "button[aria-label=\"取消引导 1\"]"
    );
    expect(cancelButton).not.toBeNull();

    act(() => {
      cancelButton?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });

    expect(onRemoveGuideMessage).toHaveBeenCalledWith("guide-1");
  });

  it("does not render a per-row overflow menu (actions are inline)", () => {
    renderComposer({
      running: true,
      queuedMessages: [
        { id: "queue-1", text: "no menu", images: [], files: [] }
      ],
      guideMessages: [
        { id: "guide-1", text: "no menu either", images: [], files: [] }
      ]
    });

    expect(container.querySelector('[role="menu"]')).toBeNull();
    expect(
      container.querySelector('button[aria-label="待发送消息操作"]')
    ).toBeNull();
  });
});

describe("Composer permission menu", () => {
  it("maps permission summaries to mode chip states", () => {
    expect(permissionModeFromSummary()).toBe("agent");
    expect(permissionModeFromSummary({ mode: "read_only" })).toBe("read_only");
    expect(permissionModeFromSummary({ mode: "agent" })).toBe("agent");
    expect(permissionModeFromSummary({ mode: "auto_review" })).toBe("auto_review");
    expect(permissionModeFromSummary({ mode: "full_access" })).toBe("full_access");
    expect(
      permissionModeFromSummary({
        mode: "full_access",
        permission_profile: "danger_full_access",
        approval_policy: "on_request",
        approvals_reviewer: "user",
      }),
    ).toBe("custom");
    expect(permissionModeHasAdvancedOverrides({ profile: "agent" })).toBe(false);
    expect(
      permissionModeHasAdvancedOverrides(undefined, {
        mode: "agent",
        permission_profile: "workspace_write",
        approval_policy: "never",
        approvals_reviewer: "user",
      }),
    ).toBe(true);
    expect(
      permissionModeHasAdvancedOverrides({
        profile: "agent",
        tools: { run_shell: "allow" },
      }),
    ).toBe(true);
  });

  it("shows the everyday permission modes in the composer menu", () => {
    const onSelectPermissionMode = vi.fn();
    renderComposer({
      accessMenuOpen: true,
      toolPolicy: { profile: "agent" },
      permissions: { mode: "agent" },
      onSelectPermissionMode,
    });

    const chip = container.querySelector<HTMLButtonElement>(
      "button[aria-label=\"权限模式：默认\"]",
    );
    expect(chip).not.toBeNull();
    expect(chip?.disabled).toBe(false);

    const labels = Array.from(
      document.body.querySelectorAll<HTMLButtonElement>(
        "button[role=\"menuitemradio\"] strong",
      ),
    ).map((label) => label.textContent?.trim());
    expect(labels).toEqual(["只读", "默认", "替我审批", "完全访问"]);
    expect(document.body.textContent).not.toContain("平衡");
    expect(document.body.textContent).not.toContain("严格");

    const checkedLabels = Array.from(
      document.body.querySelectorAll<HTMLButtonElement>(
        "button[role=\"menuitemradio\"][aria-checked=\"true\"] strong",
      ),
    ).map((label) => label.textContent?.trim());
    expect(checkedLabels).toEqual(["默认"]);
    expect(document.body.textContent).toContain("profile: workspace_write");
    expect(document.body.textContent).toContain("approval: on_request");
    expect(document.body.textContent).toContain("reviewer: user");
  });

  it("shows a custom state when explicit permission axes do not match the mode preset", () => {
    renderComposer({
      accessMenuOpen: true,
      toolPolicy: { profile: "full_access" },
      permissions: {
        mode: "full_access",
        permission_profile: "danger_full_access",
        approval_policy: "on_request",
        approvals_reviewer: "user",
      },
    });

    const chip = container.querySelector<HTMLButtonElement>(
      "button[aria-label=\"权限模式：自定义权限\"]",
    );
    expect(chip).not.toBeNull();

    const checkedLabels = Array.from(
      document.body.querySelectorAll<HTMLButtonElement>(
        "button[role=\"menuitemradio\"][aria-checked=\"true\"] strong",
      ),
    ).map((label) => label.textContent?.trim());
    expect(checkedLabels).toEqual([]);
    expect(document.body.textContent).toContain("自定义权限");
    expect(document.body.textContent).toContain("approval: on_request");
  });

  it("lets the user switch between read only, approve for me, and full access", () => {
    const onSelectPermissionMode = vi.fn();
    renderComposer({
      accessMenuOpen: true,
      toolPolicy: { profile: "agent" },
      permissions: { mode: "agent" },
      onSelectPermissionMode,
    });

    const readOnlyOption = Array.from(
      document.body.querySelectorAll<HTMLButtonElement>(
        "button[role=\"menuitemradio\"]",
      ),
    ).find((button) => button.textContent?.includes("只读"));
    expect(readOnlyOption).not.toBeUndefined();

    act(() => {
      readOnlyOption?.dispatchEvent(
        new MouseEvent("click", { bubbles: true, cancelable: true }),
      );
    });

    expect(onSelectPermissionMode).toHaveBeenCalledWith("read_only");

    const approveForMeOption = Array.from(
      document.body.querySelectorAll<HTMLButtonElement>(
        "button[role=\"menuitemradio\"]",
      ),
    ).find((button) => button.textContent?.includes("替我审批"));
    expect(approveForMeOption).not.toBeUndefined();

    act(() => {
      approveForMeOption?.dispatchEvent(
        new MouseEvent("click", { bubbles: true, cancelable: true }),
      );
    });

    expect(onSelectPermissionMode).toHaveBeenCalledWith("auto_review");

    const fullAccessOption = Array.from(
      document.body.querySelectorAll<HTMLButtonElement>(
        "button[role=\"menuitemradio\"]",
      ),
    ).find((button) => button.textContent?.includes("完全访问"));
    expect(fullAccessOption).not.toBeUndefined();

    act(() => {
      fullAccessOption?.dispatchEvent(
        new MouseEvent("click", { bubbles: true, cancelable: true }),
      );
    });

    expect(onSelectPermissionMode).toHaveBeenCalledWith("full_access");
  });

  it("shows a custom state when advanced overrides are present", () => {
    renderComposer({
      accessMenuOpen: true,
      toolPolicy: {
        profile: "agent",
        tools: { run_shell: "allow" },
      },
    });

    expect(container.textContent).toContain("自定义权限");
    expect(document.body.textContent).toContain("选择任一模式会改为该预设");
  });
});

describe("ComposerTokenGauge", () => {
  it("keeps the gauge visible with the speed label always shown when idle", () => {
    renderComposer({ running: false, tokensPerSecond: 0 });
    const gauge = container.querySelector(".composer-token-gauge");
    expect(gauge).not.toBeNull();
    expect(gauge?.getAttribute("data-state")).toBe("idle");
    expect(gauge?.getAttribute("aria-label")).toContain("0.0");

    // The label is now inline next to the dial — no hover portal. It must
    // be in the DOM from the first render so the user always sees the rate.
    const label = container.querySelector(".composer-token-gauge-label");
    expect(label).not.toBeNull();
    expect(label?.textContent).toContain("0.0");
    expect(label?.textContent).toContain("tok/s");
    expect(document.body.querySelector(".composer-token-gauge-tooltip")).toBeNull();
  });

  it("renders a live gauge with the speed label inline, no hover required", () => {
    renderComposer({ running: true, tokensPerSecond: 18.4 });

    const gauge = container.querySelector(".composer-token-gauge");
    expect(gauge).not.toBeNull();
    expect(gauge?.getAttribute("data-state")).toBe("running");
    expect(gauge?.getAttribute("title")).toBeNull();

    // Label is inline; no portal, no hover gate. Hovering must not
    // resurrect a tooltip either.
    const label = container.querySelector(".composer-token-gauge-label");
    expect(label).not.toBeNull();
    expect(label?.textContent).toContain("18.4");
    expect(label?.textContent).toContain("tok/s");

    act(() => {
      gauge?.dispatchEvent(new MouseEvent("mouseover", { bubbles: true }));
    });
    expect(document.body.querySelector(".composer-token-gauge-tooltip")).toBeNull();

    // Dial components are still rendered.
    const svg = container.querySelector(".composer-token-gauge-svg");
    expect(svg?.getAttribute("width")).toBe("20");
    expect(svg?.getAttribute("height")).toBe("20");
    expect(container.querySelector(".composer-token-gauge-progress")).not.toBeNull();
    expect(container.querySelector(".composer-token-gauge-needle")).not.toBeNull();
    expect(container.querySelector(".composer-token-gauge-inner-arc")).toBeNull();
    expect(container.querySelectorAll(".composer-token-gauge-speed-dot")).toHaveLength(0);

  });

  it("marks fallback token speed as approximate in the inline label", () => {
    renderComposer({
      running: true,
      tokensPerSecond: 18.4,
      tokenSpeedSource: "estimated",
    });

    const label = container.querySelector(".composer-token-gauge-label");
    expect(label?.textContent).toContain("约 18.4 tok/s");
  });
});
