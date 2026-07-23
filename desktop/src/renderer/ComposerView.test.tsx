import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { act, createRef, useState } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  Composer,
  SplitPaneComposer,
  permissionModeFromSummary,
  type CodexModelLoadState,
  type ComposerVariant,
  type PermissionMode,
} from "./ComposerView";
import { ImagePreviewProvider } from "./ImagePreview";
import { WORKSPACE_FILE_DRAG_MIME, type QueuedComposerMessage } from "./ComposerMessages";
import type {
  DesktopProject,
  ComposerGoalSummary,
  InitializeResult,
  PermissionSummary,
  RuntimeContext,
  SkillSummary,
  WuuDesktopApi,
} from "../shared/protocol";

let container: HTMLDivElement;
let root: Root | null = null;
const composerCSS = readFileSync(
  resolve(process.cwd(), "src/renderer/styles/composer.css"),
  "utf8",
);
const turnsCSS = readFileSync(
  resolve(process.cwd(), "src/renderer/styles/turns.css"),
  "utf8",
);
const workspaceCSS = readFileSync(
  resolve(process.cwd(), "src/renderer/styles/workspace.css"),
  "utf8",
);
const responsiveDesignCSS = readFileSync(
  resolve(process.cwd(), "src/renderer/styles/responsive-design.css"),
  "utf8",
);

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
    .querySelectorAll(
      "[data-floating-menu-owner=\"composer-access\"], [data-floating-menu-owner=\"composer-focus\"], [data-floating-menu-owner=\"composer-plus\"], [data-floating-menu-owner=\"composer-token-gauge\"]",
    )
    .forEach((element) => element.remove());
});

function initialized(permissions?: PermissionSummary): InitializeResult {
  return {
    protocol_version: "wuu-app-server/v0.1",
    provider: "fake",
    model: "fake-model",
    workspace_root: "/tmp/project",
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
  mainConversation?: boolean;
  prompt?: string;
  running?: boolean;
  ultraEnabled?: boolean;
  onToggleUltra?: (enabled: boolean) => void;
  queuedMessages?: QueuedComposerMessage[];
  guideMessages?: QueuedComposerMessage[];
  status?: string;
  statusLiveProgress?: boolean;
  runtimeControlsDisabled?: boolean;
  initialized?: InitializeResult;
  readOnly?: boolean;
  onInterrupt?: () => void;
  onSend?: () => void;
  onSteer?: () => void;
  onQueue?: () => void;
  onStartNewThread?: () => void;
  onOpenContextComposition?: () => void;
  onOpenSideThread?: () => void;
  sideThreadDisabledReason?: string;
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
  goalSummary?: ComposerGoalSummary | null;
  onEditGoal?: (text: string) => void | Promise<void>;
  onPauseGoal?: () => void | Promise<void>;
  onResumeGoal?: () => void | Promise<void>;
  onClearGoal?: () => void | Promise<void>;
  activeProject?: DesktopProject;
  projects?: DesktopProject[];
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
      <ImagePreviewProvider>
        <Composer
          variant={props.variant}
          mainConversation={props.mainConversation}
          prompt={props.prompt ?? ""}
          setPrompt={props.setPrompt ?? (() => {})}
          files={[]}
          images={[]}
          queuedMessages={props.queuedMessages ?? []}
          guideMessages={props.guideMessages ?? []}
          running={props.running ?? false}
          ultraEnabled={props.ultraEnabled}
          onToggleUltra={props.onToggleUltra}
          runtimeControlsDisabled={props.runtimeControlsDisabled}
          status={props.status ?? "ready"}
          statusLiveProgress={props.statusLiveProgress}
          readOnly={props.readOnly ?? false}
          initialized={props.initialized ?? initialized(props.permissions)}
          projects={props.projects ?? []}
          activeContext={props.activeContext}
          activeProject={props.activeProject}
          sideThreadDisabledReason={props.sideThreadDisabledReason}
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
          onStartNewThread={props.onStartNewThread ?? (() => {})}
          onOpenSideThread={props.onOpenSideThread}
          onOpenWorkspaceTool={() => {}}
          onOpenContextComposition={props.onOpenContextComposition ?? (() => {})}
          onPasteAttachmentFiles={() => {}}
          onRemoveFile={() => {}}
          onRemoveImage={() => {}}
          onRemoveQueuedMessage={props.onRemoveQueuedMessage ?? (() => {})}
          onRemoveGuideMessage={props.onRemoveGuideMessage ?? (() => {})}
          onGuideQueuedMessage={props.onGuideQueuedMessage ?? (() => {})}
          onEditQueuedMessage={props.onEditQueuedMessage ?? (() => {})}
          onEditGuideMessage={props.onEditGuideMessage ?? (() => {})}
          onSend={props.onSend ?? (() => {})}
          onSteer={props.onSteer}
          onQueue={props.onQueue}
          onInterrupt={props.onInterrupt ?? (() => {})}
          goalSummary={props.goalSummary}
          onEditGoal={props.onEditGoal}
          onPauseGoal={props.onPauseGoal}
          onResumeGoal={props.onResumeGoal}
          onClearGoal={props.onClearGoal}
          tokensPerSecond={props.tokensPerSecond ?? 0}
          tokenSpeedSampledAt={props.tokenSpeedSampledAt}
          tokenSpeedSource={props.tokenSpeedSource}
        />
      </ImagePreviewProvider>,
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
  statusLiveProgress?: boolean;
  onSend?: () => void;
}): void {
  act(() => {
    root = createRoot(container);
    root.render(
      <ImagePreviewProvider>
        <SplitPaneComposer
          prompt={props.prompt ?? ""}
          setPrompt={() => {}}
          files={[]}
          images={[]}
          running={props.running ?? false}
          readOnly={false}
          status={props.status ?? "ready"}
          statusLiveProgress={props.statusLiveProgress}
          onPasteAttachmentFiles={() => {}}
          onRemoveFile={() => {}}
          onRemoveImage={() => {}}
          onSend={props.onSend ?? (() => {})}
          onInterrupt={() => {}}
        />
      </ImagePreviewProvider>,
    );
  });
}

function renderStatefulSplitPaneComposer(props: {
  initialPrompt?: string;
  readOnly?: boolean;
  onPasteAttachmentFiles?: (files: File[]) => void;
}): void {
  function Harness(): JSX.Element {
    const [prompt, setPrompt] = useState(props.initialPrompt ?? "");
    return (
      <ImagePreviewProvider>
        <SplitPaneComposer
          prompt={prompt}
          setPrompt={setPrompt}
          files={[]}
          images={[]}
          running={false}
          readOnly={props.readOnly ?? false}
          status="ready"
          onPasteAttachmentFiles={props.onPasteAttachmentFiles ?? (() => {})}
          onRemoveFile={() => {}}
          onRemoveImage={() => {}}
          onSend={() => {}}
          onInterrupt={() => {}}
        />
      </ImagePreviewProvider>
    );
  }

  act(() => {
    root = createRoot(container);
    root.render(<Harness />);
  });
}

function mockDataTransfer(init: {
  types: string[];
  files?: File[];
  pathData?: string;
}): DataTransfer {
  const data = new Map<string, string>();
  if (init.pathData !== undefined) {
    data.set(WORKSPACE_FILE_DRAG_MIME, init.pathData);
  }
  return {
    types: init.types,
    files: init.files ?? [],
    getData: (type: string) => data.get(type) ?? "",
    setData: (type: string, value: string) => {
      data.set(type, value);
    },
    dropEffect: "none",
    effectAllowed: "all",
  } as unknown as DataTransfer;
}

function dispatchDrag(
  target: Element,
  type: "dragover" | "dragleave" | "drop",
  dataTransfer: DataTransfer,
  relatedTarget?: EventTarget | null,
): void {
  const event = new Event(type, { bubbles: true, cancelable: true });
  Object.defineProperty(event, "dataTransfer", { value: dataTransfer });
  if (relatedTarget !== undefined) {
    Object.defineProperty(event, "relatedTarget", { value: relatedTarget });
  }
  target.dispatchEvent(event);
}

function renderStatefulComposer(props: {
  initialPrompt?: string;
  onSend?: (prompt: string) => void;
  showUltraToggle?: boolean;
  activeContext?: RuntimeContext;
  readOnly?: boolean;
  textOnly?: boolean;
  onPasteAttachmentFiles?: (files: File[]) => void;
}): void {
  const codexModels: CodexModelLoadState = {
    loading: false,
    error: "",
    models: [],
  };

  function Harness(): JSX.Element {
    const [prompt, setPrompt] = useState(props.initialPrompt ?? "");
    const [ultraEnabled, setUltraEnabled] = useState(false);
    return (
      <ImagePreviewProvider>
        <Composer
          prompt={prompt}
          setPrompt={setPrompt}
          files={[]}
          images={[]}
          queuedMessages={[]}
          guideMessages={[]}
          running={false}
          ultraEnabled={ultraEnabled}
          onToggleUltra={props.showUltraToggle ? setUltraEnabled : undefined}
          status="ready"
          readOnly={props.readOnly ?? false}
          textOnly={props.textOnly}
          initialized={initialized()}
          projects={[]}
          activeContext={props.activeContext}
          codexModels={codexModels}
          codexRuntimeMenu={null}
          codexRuntimeRef={createRef<HTMLDivElement>()}
          menuOpen={false}
          accessMenuOpen={false}
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
          onSelectPermissionMode={() => {}}
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
          onPasteAttachmentFiles={props.onPasteAttachmentFiles ?? (() => {})}
          onRemoveFile={() => {}}
          onRemoveImage={() => {}}
          onRemoveQueuedMessage={() => {}}
          onRemoveGuideMessage={() => {}}
          onGuideQueuedMessage={() => {}}
          onEditQueuedMessage={() => {}}
          onEditGuideMessage={() => {}}
          onSend={() => props.onSend?.(prompt)}
          onInterrupt={() => {}}
          tokensPerSecond={0}
        />
      </ImagePreviewProvider>
    );
  }

  act(() => {
    root = createRoot(container);
    root.render(<Harness />);
  });
}

async function nextAnimationFrame(): Promise<void> {
  await new Promise<void>((resolve) => window.requestAnimationFrame(() => resolve()));
}

function longPastedPrompt(title = "# 交接提示词(直接粘贴)", label = "交接"): string {
  return [
    title,
    "",
    `这是第一段${label}内容。`,
    `这是第二段${label}内容。`,
    `这是第三段${label}内容。`,
    `这是第四段${label}内容。`,
    `这是第五段${label}内容。`,
    `这是第六段${label}内容。`,
    `这是第七段${label}内容。`,
    `这是第八段${label}内容。`,
    `这是第九段${label}内容。`,
    `这是第十段${label}内容。`,
    `这是第十一段${label}内容。`,
    `这是第十二段${label}内容。`,
    `这是第十三段${label}内容。`,
    `这是第十四段${label}内容。`,
    `这是第十五段${label}内容。`,
  ].join("\n");
}

function activeGoalSummary(text = "Ship the active goal"): ComposerGoalSummary {
  return {
    id: "goal-1",
    text,
    status: "active",
    can_pause: true,
    can_clear: true,
  };
}

function pastePlainText(textarea: HTMLTextAreaElement, text: string): void {
  const event = new Event("paste", { bubbles: true, cancelable: true });
  Object.defineProperty(event, "clipboardData", {
    value: {
      items: [],
      getData: (type: string) => (type === "text/plain" ? text : ""),
    },
  });
  textarea.dispatchEvent(event);
}

function setTextareaValue(textarea: HTMLTextAreaElement, value: string): void {
  const setter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")?.set;
  setter?.call(textarea, value);
  textarea.dispatchEvent(new Event("input", { bubbles: true }));
}

describe("Composer send control", () => {
  it("sends on Enter when Chromium leaves a stale IME keyCode", () => {
    const onSend = vi.fn();
    renderComposer({ prompt: "发送这条消息", onSend });
    const textarea = container.querySelector<HTMLTextAreaElement>("textarea");
    const event = new KeyboardEvent("keydown", {
      key: "Enter",
      keyCode: 229,
      bubbles: true,
      cancelable: true,
    });

    act(() => {
      textarea?.dispatchEvent(event);
    });

    expect(event.defaultPrevented).toBe(true);
    expect(onSend).toHaveBeenCalledTimes(1);
  });

  it("does not send when Enter confirms an active IME composition", async () => {
    const onSend = vi.fn();
    renderStatefulComposer({ initialPrompt: "正在", onSend });
    const textarea = container.querySelector<HTMLTextAreaElement>("textarea");
    const event = new KeyboardEvent("keydown", {
      key: "Enter",
      isComposing: true,
      bubbles: true,
      cancelable: true,
    });

    act(() => {
      textarea?.dispatchEvent(event);
    });

    expect(event.defaultPrevented).toBe(false);
    expect(onSend).not.toHaveBeenCalled();

    await act(async () => {
      setTextareaValue(textarea as HTMLTextAreaElement, "正在输入");
      textarea?.dispatchEvent(new CompositionEvent("compositionend", { bubbles: true }));
    });

    expect(onSend).not.toHaveBeenCalled();
    expect(textarea?.value).toBe("正在输入");

    act(() => {
      textarea?.dispatchEvent(new KeyboardEvent("keydown", {
        key: "Enter",
        bubbles: true,
        cancelable: true,
      }));
    });

    expect(onSend).toHaveBeenCalledOnce();
    expect(onSend).toHaveBeenCalledWith("正在输入");
  });

  it("does not send from the split composer when Enter confirms IME composition", async () => {
    const onSend = vi.fn();
    renderSplitPaneComposer({ prompt: "继续修改", onSend });
    const textarea = container.querySelector<HTMLTextAreaElement>("textarea");

    act(() => {
      textarea?.dispatchEvent(new KeyboardEvent("keydown", {
        key: "Enter",
        isComposing: true,
        bubbles: true,
        cancelable: true,
      }));
    });
    expect(onSend).not.toHaveBeenCalled();

    await act(async () => {
      textarea?.dispatchEvent(new CompositionEvent("compositionend", { bubbles: true }));
    });

    expect(onSend).not.toHaveBeenCalled();

    act(() => {
      textarea?.dispatchEvent(new KeyboardEvent("keydown", {
        key: "Enter",
        bubbles: true,
        cancelable: true,
      }));
    });

    expect(onSend).toHaveBeenCalledOnce();
  });

  it("steers with Enter and queues with Tab while a turn is running", () => {
    const onSend = vi.fn();
    const onSteer = vi.fn();
    const onQueue = vi.fn();
    renderComposer({
      prompt: "change direction",
      running: true,
      onSend,
      onSteer,
      onQueue,
    });
    const textarea = container.querySelector<HTMLTextAreaElement>("textarea");

    act(() => {
      textarea?.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true }));
    });
    expect(onSteer).toHaveBeenCalledTimes(1);
    expect(onQueue).not.toHaveBeenCalled();
    expect(onSend).not.toHaveBeenCalled();

    act(() => {
      textarea?.dispatchEvent(new KeyboardEvent("keydown", { key: "Tab", bubbles: true }));
    });
    expect(onQueue).toHaveBeenCalledTimes(1);
    expect(onSend).not.toHaveBeenCalled();
  });

  it("keeps Tab available for focus navigation when there is no running draft", () => {
    const onQueue = vi.fn();
    renderComposer({ prompt: "", running: true, onQueue });
    const textarea = container.querySelector<HTMLTextAreaElement>("textarea");
    const event = new KeyboardEvent("keydown", {
      key: "Tab",
      bubbles: true,
      cancelable: true,
    });

    act(() => {
      textarea?.dispatchEvent(event);
    });

    expect(event.defaultPrevented).toBe(false);
    expect(onQueue).not.toHaveBeenCalled();
  });

  it("keeps Tab and Shift+Tab available outside a running draft", () => {
    const onQueue = vi.fn();
    renderComposer({ prompt: "not running", running: false, onQueue });
    const textarea = container.querySelector<HTMLTextAreaElement>("textarea");
    const tabEvent = new KeyboardEvent("keydown", {
      key: "Tab",
      bubbles: true,
      cancelable: true,
    });
    const reverseTabEvent = new KeyboardEvent("keydown", {
      key: "Tab",
      shiftKey: true,
      bubbles: true,
      cancelable: true,
    });

    act(() => {
      textarea?.dispatchEvent(tabEvent);
      textarea?.dispatchEvent(reverseTabEvent);
    });

    expect(tabEvent.defaultPrevented).toBe(false);
    expect(reverseTabEvent.defaultPrevented).toBe(false);
    expect(onQueue).not.toHaveBeenCalled();
  });

  it("keeps a send (queue) button while running when the input has a draft", () => {
    const onInterrupt = vi.fn();
    const onSend = vi.fn();
    renderComposer({
      prompt: "queued follow-up",
      running: true,
      onInterrupt,
      onSend,
    });

    // A draft typed mid-turn must stay sendable (queued), not be forced into a
    // stop control — Enter already queues it, so the button must agree.
    expect(
      container.querySelectorAll(".composer-action-button.composer-stop-button"),
    ).toHaveLength(0);
    expect(container.querySelector("button[aria-label=\"停止\"]")).toBeNull();

    const sendButton = container.querySelector<HTMLButtonElement>(
      "button[aria-label=\"排队发送\"]",
    );
    expect(sendButton).not.toBeNull();
    expect(sendButton?.disabled).toBe(false);

    act(() => {
      sendButton?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });

    expect(onSend).toHaveBeenCalledTimes(1);
    expect(onInterrupt).not.toHaveBeenCalled();
  });

  it("shows a stop button while running only when the input is empty", () => {
    const onInterrupt = vi.fn();
    const onSend = vi.fn();
    renderComposer({
      prompt: "",
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

  it("keeps active goal controls enabled while a request is running", async () => {
    const onPauseGoal = vi.fn().mockResolvedValue(undefined);
    const onClearGoal = vi.fn().mockResolvedValue(undefined);
    renderComposer({
      running: true,
      goalSummary: activeGoalSummary(),
      onPauseGoal,
      onClearGoal,
    });

    const actionButton = container.querySelector<HTMLButtonElement>(
      "button[aria-label=\"目标操作\"]",
    );
    const editButton = container.querySelector<HTMLButtonElement>(
      "button[aria-label=\"编辑目标\"]",
    );
    expect(actionButton?.disabled).toBe(false);
    expect(editButton?.disabled).toBe(false);

    const openGoalMenu = (): void => {
      act(() => {
        container
          .querySelector<HTMLButtonElement>("button[aria-label=\"目标操作\"]")
          ?.dispatchEvent(
            new MouseEvent("click", { bubbles: true, cancelable: true }),
          );
      });
    };
    const goalMenuItem = (label: string): HTMLButtonElement | undefined =>
      Array.from(
        document.querySelectorAll<HTMLButtonElement>("button[role=\"menuitem\"]"),
      ).find((button) => button.textContent === label);

    openGoalMenu();
    expect(goalMenuItem("暂停目标")?.disabled).toBe(false);
    expect(goalMenuItem("清除目标")?.disabled).toBe(false);

    act(() => {
      editButton?.dispatchEvent(
        new MouseEvent("click", { bubbles: true, cancelable: true }),
      );
    });
    expect(document.querySelector(".composer-goal-edit-dialog")).not.toBeNull();

    act(() => {
      document
        .querySelector<HTMLButtonElement>(".composer-goal-edit-dialog .secondary-button")
        ?.dispatchEvent(
          new MouseEvent("click", { bubbles: true, cancelable: true }),
        );
    });

    openGoalMenu();
    const resumedPauseButton = goalMenuItem("暂停目标");
    await act(async () => {
      resumedPauseButton?.dispatchEvent(
        new MouseEvent("click", { bubbles: true, cancelable: true }),
      );
      await Promise.resolve();
    });
    expect(onPauseGoal).toHaveBeenCalledTimes(1);

    openGoalMenu();
    const resumedClearButton = goalMenuItem("清除目标");
    act(() => {
      resumedClearButton?.dispatchEvent(
        new MouseEvent("click", { bubbles: true, cancelable: true }),
      );
    });
    await act(async () => {
      goalMenuItem("再次点击确认清除")?.dispatchEvent(
          new MouseEvent("click", { bubbles: true, cancelable: true }),
        );
      await Promise.resolve();
    });
    expect(onClearGoal).toHaveBeenCalledTimes(1);
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

  it("renders reconnect status with the shared live progress chip", () => {
    renderComposer({
      prompt: "retry later",
      running: true,
      status: "消息流重连中 1/3",
      statusLiveProgress: true,
    });

    expect(container.querySelector(".status-label")?.textContent).toBe("消息流重连中 1/3");
    expect(container.querySelector(".status-label-text")?.classList.contains("live-progress-chip")).toBe(true);
  });

  it("renders static fallback status without the live progress chip", () => {
    renderComposer({
      prompt: "retry later",
      running: true,
      status: "WebSocket 不可用，已切到 HTTP",
      statusLiveProgress: false,
    });

    expect(container.querySelector(".status-label")?.textContent).toBe("WebSocket 不可用，已切到 HTTP");
    expect(container.querySelector(".status-label-text")?.classList.contains("live-progress-chip")).toBe(false);
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

  it("renders split-pane reconnect status with the shared live progress chip", () => {
    renderSplitPaneComposer({
      prompt: "continue this branch",
      running: true,
      status: "HTTP 消息流重连中 2/3",
      statusLiveProgress: true,
    });

    expect(container.querySelector(".split-composer-status")?.textContent).toBe("HTTP 消息流重连中 2/3");
    expect(container.querySelector(".split-composer-status-text")?.classList.contains("live-progress-chip")).toBe(true);
  });

  it("hides session context chips in the dock composer", () => {
    renderComposer({
      variant: "dock",
      prompt: "follow up",
    });

    expect(container.querySelector(".composer-context-bar")).toBeNull();
    expect(container.querySelector(".context-project-button")).toBeNull();
  });

  it("keeps dock composer content inside the visual frame", () => {
    renderComposer({
      variant: "dock",
      prompt: "follow up",
    });

    const shell = container.querySelector(".composer-shell");
    const frame = container.querySelector(".composer-frame");
    const composer = container.querySelector(".composer");

    expect(shell).not.toBeNull();
    expect(frame).not.toBeNull();
    expect(composer).not.toBeNull();
    expect(shell?.contains(frame)).toBe(true);
    expect(frame?.contains(composer)).toBe(true);
    expect(frame?.querySelector(".composer-context-bar")).toBeNull();
  });

  it("renders controlled Ultra state and preserves the existing treatment", () => {
    renderStatefulComposer({
      initialPrompt: "keep this draft",
      showUltraToggle: true,
    });

    const button = container.querySelector<HTMLButtonElement>(".composer-ultra-button");
    const frame = container.querySelector(".composer-frame");

    expect(button?.querySelector(".composer-ultra-notch")).not.toBeNull();
    expect(button?.getAttribute("aria-label")).toBe("开启 Ultra 多 agent 模式");
    expect(button?.getAttribute("aria-pressed")).toBe("false");
    expect(frame?.classList.contains("is-ultra")).toBe(false);

    act(() => button?.click());

    expect(button?.getAttribute("aria-pressed")).toBe("true");
    expect(button?.getAttribute("aria-label")).toBe("关闭 Ultra 多 agent 模式");
    expect(frame?.classList.contains("is-ultra")).toBe(true);
    expect(container.querySelector(".composer-ultra-energy.turning-on")).not.toBeNull();
    expect(
      container.querySelector<HTMLTextAreaElement>("textarea")?.value,
    ).toBe("keep this draft");

    act(() => button?.click());

    expect(button?.getAttribute("aria-pressed")).toBe("false");
    expect(frame?.classList.contains("is-ultra")).toBe(false);
    expect(container.querySelector(".composer-ultra-energy.turning-off")).not.toBeNull();
  });

  it("hides the global Ultra control when the host does not provide it", () => {
    renderComposer({ variant: "dock" });

    expect(container.querySelector(".composer-ultra-button")).toBeNull();
  });

  it("keeps the Ultra control available while a turn is running", () => {
    const onToggleUltra = vi.fn();
    renderComposer({
      variant: "dock",
      running: true,
      ultraEnabled: false,
      onToggleUltra,
    });

    const button = container.querySelector<HTMLButtonElement>(
      ".composer-ultra-button",
    );
    expect(button?.disabled).toBe(false);

    act(() => button?.click());
    expect(onToggleUltra).toHaveBeenCalledWith(true);
  });

  it("keeps the slash command menu outside the clipped visual frame", () => {
    renderComposer({
      variant: "dock",
      prompt: "/",
    });

    const shell = container.querySelector(".composer-shell");
    const frame = container.querySelector(".composer-frame");
    const slashMenu = container.querySelector(".slash-command-menu");

    expect(shell).not.toBeNull();
    expect(frame).not.toBeNull();
    expect(slashMenu).not.toBeNull();
    expect(shell?.contains(slashMenu)).toBe(true);
    expect(frame?.contains(slashMenu)).toBe(false);
  });

  it("places the caret after a command inserted from the plus menu", async () => {
    renderStatefulComposer({ activeContext: { kind: "no_project", cwd: "/tmp" } });

    const plusButton = container.querySelector<HTMLButtonElement>(".composer-plus-button");
    const textarea = container.querySelector<HTMLTextAreaElement>("textarea");

    act(() => plusButton?.click());
    const reviewItem = Array.from(
      document.body.querySelectorAll<HTMLButtonElement>(
        '[data-floating-menu-owner="composer-plus"] button',
      ),
    ).find((button) => button.textContent?.includes("审查当前更改"));
    act(() => reviewItem?.click());
    await act(async () => nextAnimationFrame());

    expect(textarea?.value).toBe("/review ");
    expect(textarea?.selectionStart).toBe(8);
    expect(textarea?.selectionEnd).toBe(8);
  });

  it("resizes the slash command menu with its composer and available viewport height", () => {
    let shellTop = 320;
    const titlebar = document.createElement("header");
    titlebar.className = "titlebar";
    document.body.appendChild(titlebar);
    const rectSpy = vi
      .spyOn(HTMLElement.prototype, "getBoundingClientRect")
      .mockImplementation(function (this: HTMLElement) {
        const isShell = this.classList.contains("composer-shell");
        const top = isShell ? shellTop : 0;
        const bottom = this.classList.contains("titlebar") ? 64 : top;
        return {
          bottom,
          height: 0,
          left: 0,
          right: 0,
          top,
          width: 0,
          x: 0,
          y: top,
          toJSON: () => ({}),
        };
      });

    try {
      renderComposer({
        variant: "dock",
        prompt: "/",
      });

      const shell = container.querySelector<HTMLElement>(".composer-shell");
      expect(shell?.style.getPropertyValue("--slash-command-available-height")).toBe("240px");

      shellTop = 220;
      act(() => window.dispatchEvent(new Event("resize")));
      expect(shell?.style.getPropertyValue("--slash-command-available-height")).toBe("140px");

      shellTop = 70;
      act(() => window.dispatchEvent(new Event("resize")));
      expect(shell?.style.getPropertyValue("--slash-command-available-height")).toBe("0px");
      expect(composerCSS).toMatch(/\.slash-command-menu\s*{[^}]*width:\s*100%;/s);
      expect(composerCSS).toContain("var(--slash-command-available-height");
    } finally {
      rectSpy.mockRestore();
      titlebar.remove();
    }
  });

  it("keeps the slash command title visible at every composer width", () => {
    expect(composerCSS).toMatch(
      /\.composer-shell\s*{[^}]*container:\s*composer-shell\s*\/\s*inline-size;/s,
    );

    const iconCollapse = composerCSS.indexOf(
      "@container composer-shell (max-width: 460px)",
    );

    expect(iconCollapse).toBeGreaterThan(-1);
    expect(composerCSS).toMatch(
      /@container composer-shell \(max-width: 460px\)[\s\S]*?\.slash-command-icon\s*{[^}]*display:\s*none;/,
    );
    // No 360px breakpoint should hide the title, since the title is the only
    // text in each row and hiding it would leave an empty menu.
    expect(composerCSS).not.toMatch(
      /@container composer-shell \(max-width: 360px\)/,
    );
  });

  it("shows the hero project selector inside the composer toolbar", () => {
    renderComposer({
      variant: "hero",
    });

    expect(container.querySelector(".composer-context-bar")).toBeNull();
    expect(container.querySelector(".context-project-button")).toBeNull();
    expect(container.querySelector(".composer-bar-left > .hero-project-pill-anchor")).not.toBeNull();
    expect(container.querySelector(".hero-project-pill")).not.toBeNull();
    expect(container.querySelector(".hero-project-pill")?.textContent).toContain("选择项目");
    expect(container.querySelector<HTMLButtonElement>("button[aria-label=\"打开项目\"]")).toBeNull();
  });

  it("hides the cwd control once a project conversation is sent (dock variant)", () => {
    renderComposer({
      variant: "dock",
      prompt: "follow up",
      activeContext: { kind: "project", project_id: "project-1", cwd: "/repo/wuu" },
    });

    // A sent conversation locks its cwd (backend session.CWD is immutable),
    // so neither the hero pill nor the old dock "+" project control renders.
    expect(container.querySelector(".composer-project-control")).toBeNull();
    expect(
      container.querySelector<HTMLButtonElement>("button[aria-label=\"打开项目\"]"),
    ).toBeNull();
    // The composer itself still renders — only the workspace/cwd control is gone.
    expect(container.querySelector(".composer-plus-button")).not.toBeNull();
  });

  it("uses the active project name in the hero project selector", () => {
    renderComposer({
      variant: "hero",
      activeContext: { kind: "project", project_id: "project-1", cwd: "/repo/wuu" },
      activeProject: {
        id: "project-1",
        name: "wuu",
        path: "/repo/wuu",
        created_at: "2026-06-26T00:00:00.000Z",
        updated_at: "2026-06-26T00:00:00.000Z",
      },
    });

    const selector = container.querySelector<HTMLButtonElement>(".hero-project-pill");
    expect(selector).not.toBeNull();
    expect(selector?.textContent).toContain("wuu");
    expect(selector?.getAttribute("title")).toBe("/repo/wuu");
  });

  it("labels the hero pill 对话 for the no-project workspace", () => {
    renderComposer({
      variant: "hero",
      activeContext: { kind: "no_project", cwd: "/scratch/default" },
    });

    const selector = container.querySelector<HTMLButtonElement>(".hero-project-pill");
    expect(selector).not.toBeNull();
    // Matches the sidebar group and the session tab, which both read "对话".
    expect(selector?.textContent).toContain("对话");
  });

  it("does not apply dock Plus icon sizing to the hero project selector", () => {
    expect(composerCSS).toContain(".composer-bar button.composer-project-control > svg");
    expect(composerCSS).not.toContain(".composer-bar .composer-project-control svg");
  });

  it("separates auxiliary controls from the send action for responsive collapse", () => {
    renderComposer({
      variant: "dock",
      prompt: "follow up",
    });

    const leftGroup = container.querySelector(".composer-bar-left");
    const rightGroup = container.querySelector(".composer-bar-right");
    const sendButton = container.querySelector("button[aria-label=\"发送\"]");

    expect(leftGroup).not.toBeNull();
    expect(rightGroup).not.toBeNull();
    // A sent project/对话 conversation (dock variant) locks its cwd, so no
    // project/cwd control renders in the leading slot anymore.
    expect(leftGroup?.querySelector(".composer-project-control")).toBeNull();
    expect(leftGroup?.querySelector(".composer-plus-button")).not.toBeNull();
    expect(leftGroup?.querySelector(".permission-menu-anchor")).not.toBeNull();
    expect(rightGroup?.querySelector(".composer-token-gauge")).not.toBeNull();
    expect(rightGroup?.querySelector(".codex-runtime-anchor")).not.toBeNull();
    expect(rightGroup?.contains(sendButton)).toBe(true);
  });

  it("folds attachment and slash commands into the plus menu", () => {
    renderComposer({
      variant: "dock",
      prompt: "follow up",
      activeContext: { kind: "no_project", cwd: "/tmp" },
    });

    const plusButton = container.querySelector<HTMLButtonElement>(".composer-plus-button");
    expect(plusButton).not.toBeNull();
    expect(container.querySelector(".composer-attachment-button")).toBeNull();
    expect(container.querySelector(".composer-slash-button")).toBeNull();

    const shell = container.querySelector<HTMLElement>(".composer-shell");
    vi.spyOn(shell as HTMLElement, "getBoundingClientRect").mockReturnValue({
      bottom: 580,
      height: 100,
      left: 80,
      right: 720,
      top: 480,
      width: 640,
      x: 80,
      y: 480,
      toJSON: () => ({}),
    });

    const fileInput = container.querySelector<HTMLInputElement>(".composer-file-input");
    expect(fileInput).not.toBeNull();
    const inputClickSpy = vi.spyOn(fileInput as HTMLInputElement, "click").mockImplementation(() => {});

    act(() => {
      plusButton?.click();
    });
    expect(plusButton?.getAttribute("aria-expanded")).toBe("true");

    const menu = document.body.querySelector<HTMLElement>('[data-floating-menu-owner="composer-plus"]');
    expect(menu?.style.width).toBe("640px");
    expect(menu?.style.left).toBe("80px");
    expect(menu?.style.bottom).toBe(`${window.innerHeight - 480 + 8}px`);
    expect(composerCSS).toMatch(/\.composer-plus-menu\s*{[^}]*width:\s*100%;/s);
    expect(menu?.querySelectorAll(".composer-plus-menu-section")).toHaveLength(2);
    expect(menu?.textContent).toContain("添加");
    expect(menu?.textContent).toContain("添加附件");
    expect(menu?.textContent).toContain("图片或 PDF");
    expect(menu?.textContent).toContain("命令");
    expect(menu?.textContent).toContain("审查当前更改");
    expect(menu?.textContent).not.toContain("打开斜杠命令");

    const attachmentItem = Array.from(menu?.querySelectorAll("button") ?? []).find(
      (button) => button.textContent?.includes("添加附件"),
    );
    act(() => {
      attachmentItem?.click();
    });
    expect(inputClickSpy).toHaveBeenCalledTimes(1);
    expect(document.body.querySelector('[data-floating-menu-owner="composer-plus"]')).toBeNull();
  });

  it("runs prompt commands directly from the plus menu", () => {
    const setPrompt = vi.fn();
    renderComposer({
      variant: "dock",
      prompt: "",
      setPrompt,
      activeContext: { kind: "no_project", cwd: "/tmp" },
    });

    act(() => {
      container.querySelector<HTMLButtonElement>(".composer-plus-button")?.click();
    });
    const reviewItem = Array.from(
      document.body.querySelectorAll('[data-floating-menu-owner="composer-plus"] button'),
    ).find((button) => button.textContent?.includes("审查当前更改"));
    act(() => {
      (reviewItem as HTMLButtonElement | undefined)?.click();
    });

    expect(setPrompt).toHaveBeenCalledWith("/review ");
    expect(document.body.querySelector('[data-floating-menu-owner="composer-plus"]')).toBeNull();
  });

  it("runs action commands directly from the plus menu", () => {
    const onStartNewThread = vi.fn();
    renderComposer({
      variant: "dock",
      prompt: "",
      onStartNewThread,
      activeContext: { kind: "no_project", cwd: "/tmp" },
    });

    act(() => {
      container.querySelector<HTMLButtonElement>(".composer-plus-button")?.click();
    });
    const newThreadItem = Array.from(
      document.body.querySelectorAll('[data-floating-menu-owner="composer-plus"] button'),
    ).find((button) => button.textContent?.includes("新建对话"));
    act(() => {
      (newThreadItem as HTMLButtonElement | undefined)?.click();
    });

    expect(onStartNewThread).toHaveBeenCalledTimes(1);
    expect(document.body.querySelector('[data-floating-menu-owner="composer-plus"]')).toBeNull();
  });

  it("keeps runtime controls separate from composer send state", () => {
    renderComposer({
      variant: "dock",
      prompt: "follow up",
      running: false,
      runtimeControlsDisabled: true,
    });

    const runtimeButton = container.querySelector<HTMLButtonElement>(".codex-runtime-trigger");
    const sendButton = container.querySelector<HTMLButtonElement>("button[aria-label=\"发送\"]");

    expect(runtimeButton?.disabled).toBe(true);
    expect(sendButton?.disabled).toBe(false);
  });

  it("renders the conversation runtime selected by the app", () => {
    const runtime = initialized();
    runtime.provider = "provider-a";
    runtime.model = "model-a";
    runtime.providers = [
      { name: "provider-a", type: "openai-compatible", model: "model-a" },
      { name: "provider-b", type: "openai-compatible", model: "model-b" }
    ];

    renderComposer({
      variant: "dock",
      initialized: runtime,
      runtimeControlsDisabled: true
    });

    const runtimeButton = container.querySelector<HTMLButtonElement>(".codex-runtime-trigger");
    expect(runtimeButton?.textContent).toContain("model-a");
    expect(runtimeButton?.disabled).toBe(true);
  });

  it("renders the next-turn session runtime when controls unlock", () => {
    const runtime = initialized();
    runtime.provider = "provider-b";
    runtime.model = "model-b";
    runtime.providers = [
      { name: "provider-a", type: "openai-compatible", model: "model-a" },
      { name: "provider-b", type: "openai-compatible", model: "model-b" }
    ];

    renderComposer({
      variant: "dock",
      initialized: runtime,
      runtimeControlsDisabled: false
    });

    const runtimeButton = container.querySelector<HTMLButtonElement>(".codex-runtime-trigger");
    expect(runtimeButton?.textContent).toContain("model-b");
    expect(runtimeButton?.disabled).toBe(false);
  });

  it("uses the disabled cursor for locked runtime model controls", () => {
    expect(workspaceCSS).toMatch(
      /\.codex-runtime-trigger:disabled\s*{[^}]*cursor:\s*not-allowed;/,
    );
  });

  it("declares composer-width collapse rules for the least important controls first", () => {
    expect(composerCSS).toContain("container: composer-toolbar / inline-size");

    const speedLabelCollapse = responsiveDesignCSS.indexOf("@container composer-toolbar (max-width: 680px)");
    const permissionLabelCollapse = responsiveDesignCSS.indexOf("@container composer-toolbar (max-width: 620px)");
    const gaugeCollapse = responsiveDesignCSS.indexOf("@container composer-toolbar (max-width: 560px)");
    const runtimeCollapse = responsiveDesignCSS.indexOf("@container composer-toolbar (max-width: 500px)");
    const projectCollapse = responsiveDesignCSS.indexOf("@container composer-toolbar (max-width: 360px)");

    expect(speedLabelCollapse).toBeGreaterThan(-1);
    expect(permissionLabelCollapse).toBeGreaterThan(speedLabelCollapse);
    expect(gaugeCollapse).toBeGreaterThan(permissionLabelCollapse);
    expect(runtimeCollapse).toBeGreaterThan(gaugeCollapse);
    expect(projectCollapse).toBeGreaterThan(runtimeCollapse);
    expect(responsiveDesignCSS).toContain(".composer-token-gauge-label");
    expect(responsiveDesignCSS).toContain(".codex-runtime-anchor");
    expect(responsiveDesignCSS).toContain(".composer-project-control");
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

    const skillButton = container.querySelector<HTMLButtonElement>(
      '.slash-command-item[data-command-name="slides"]',
    );
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

  it("runs /context as a local action when the send button is clicked", () => {
    const onSend = vi.fn();
    const onOpenContextComposition = vi.fn();
    const setPrompt = vi.fn();
    renderComposer({
      prompt: "/context",
      setPrompt,
      onSend,
      onOpenContextComposition,
      activeContext: { kind: "project", project_id: "repo", cwd: "/repo" },
    });

    const sendButton = container.querySelector<HTMLButtonElement>(
      'button[aria-label="发送"]',
    );
    expect(sendButton).not.toBeNull();

    act(() => {
      sendButton?.dispatchEvent(
        new MouseEvent("click", { bubbles: true, cancelable: true }),
      );
    });

    expect(onOpenContextComposition).toHaveBeenCalledTimes(1);
    expect(onSend).not.toHaveBeenCalled();
    expect(setPrompt).toHaveBeenCalledWith("");
  });

  it("runs /new as a new-thread action", () => {
    const setPrompt = vi.fn();
    const onStartNewThread = vi.fn();
    renderComposer({
      prompt: "/new",
      setPrompt,
      onStartNewThread,
      activeContext: { kind: "project", project_id: "repo", cwd: "/repo" },
    });

    const sendButton = container.querySelector<HTMLButtonElement>(
      'button[aria-label="发送"]',
    );
    act(() => sendButton?.click());

    expect(onStartNewThread).toHaveBeenCalledTimes(1);
    expect(setPrompt).toHaveBeenCalledWith("");
  });

  it("marks only an explicitly identified main-conversation composer", () => {
    renderComposer({ variant: "hero", mainConversation: true });

    expect(
      container.querySelector("[data-main-conversation-composer=\"hero\"]"),
    ).not.toBeNull();
  });

  it("gives the document composer its own compact styling hook", () => {
    renderComposer({ variant: "document", mainConversation: true });

    const composer = container.querySelector(
      '[data-main-conversation-composer="document"]',
    );
    expect(composer?.classList.contains("dock-composer-wrap")).toBe(true);
    expect(composer?.classList.contains("document-composer-wrap")).toBe(true);
  });

  it("keeps /side disabled on a draft without a persisted thread", () => {
    const setPrompt = vi.fn();
    const onOpenSideThread = vi.fn();
    renderComposer({
      prompt: "/side",
      setPrompt,
      onOpenSideThread,
      sideThreadDisabledReason: "先发送一条消息",
      activeContext: { kind: "project", project_id: "repo", cwd: "/repo" },
    });

    const side = container.querySelector<HTMLButtonElement>(
      '.slash-command-item[data-command-name="side"]',
    );
    expect(side?.disabled).toBe(true);
    expect(side?.textContent).toContain("先发送一条消息");

    act(() => side?.click());
    expect(onOpenSideThread).not.toHaveBeenCalled();
    expect(setPrompt).not.toHaveBeenCalled();
  });

  it("runs /side as an open action for a persisted thread", () => {
    const setPrompt = vi.fn();
    const onOpenSideThread = vi.fn();
    renderComposer({
      prompt: "/side",
      setPrompt,
      onOpenSideThread,
      activeContext: { kind: "project", project_id: "repo", cwd: "/repo" },
    });

    const sendButton = container.querySelector<HTMLButtonElement>(
      'button[aria-label="发送"]',
    );
    act(() => sendButton?.click());

    expect(onOpenSideThread).toHaveBeenCalledTimes(1);
    expect(setPrompt).toHaveBeenCalledWith("");
  });
});

describe("Composer long text folding", () => {
  it("bounds folded paste rows inside a scrollable list", () => {
    expect(composerCSS).toContain(".composer-collapsed-prompt-list");
    expect(composerCSS).toContain("display: grid");
    expect(composerCSS).toContain("width: auto");
    expect(composerCSS).toContain("grid-template-columns: repeat(auto-fit, minmax(min(260px, 100%), 1fr))");
    expect(composerCSS).toContain("max-height: min(168px, 26vh)");
    expect(composerCSS).toContain("overflow-y: auto");
    expect(composerCSS).toContain("overscroll-behavior: contain");
  });

  it("folds a long paste while sending the original text plus follow-up", () => {
    const longText = longPastedPrompt();
    const onSend = vi.fn();
    renderStatefulComposer({ onSend });

    const textarea = container.querySelector<HTMLTextAreaElement>("textarea");
    expect(textarea).not.toBeNull();

    act(() => {
      pastePlainText(textarea as HTMLTextAreaElement, longText);
    });

    expect(container.querySelector(".composer-collapsed-prompt-card")).not.toBeNull();
    expect(container.querySelector(".composer-collapsed-prompt-title")?.textContent).toBe("# 交接提示词(直接粘贴)");
    expect((textarea as HTMLTextAreaElement).value).toBe("");
    expect((textarea as HTMLTextAreaElement).placeholder).toBe("要求后续变更");

    act(() => {
      setTextareaValue(textarea as HTMLTextAreaElement, "\n要求后续变更");
    });

    const sendButton = container.querySelector<HTMLButtonElement>("button[aria-label=\"发送\"]");
    act(() => {
      sendButton?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });

    expect(onSend).toHaveBeenCalledWith(`${longText}\n要求后续变更`);
  });

  it("reveals a folded long paste back into the textarea", () => {
    const longText = longPastedPrompt();
    renderStatefulComposer({});

    const textarea = container.querySelector<HTMLTextAreaElement>("textarea");
    expect(textarea).not.toBeNull();

    act(() => {
      pastePlainText(textarea as HTMLTextAreaElement, longText);
    });

    const revealButton = container.querySelector<HTMLButtonElement>(".composer-collapsed-prompt-main");
    expect(revealButton).not.toBeNull();

    act(() => {
      revealButton?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });

    expect(container.querySelector(".composer-collapsed-prompt-card")).toBeNull();
    expect(container.querySelector<HTMLTextAreaElement>("textarea")?.value).toBe(longText);
  });

  it("reveals folded rows into the textarea in click order", () => {
    const firstLongText = longPastedPrompt("# A 交接提示词", "A");
    const secondLongText = longPastedPrompt("# B 交接提示词", "B");
    const thirdLongText = longPastedPrompt("# C 交接提示词", "C");
    const onSend = vi.fn();
    renderStatefulComposer({ onSend });

    const textarea = container.querySelector<HTMLTextAreaElement>("textarea");
    expect(textarea).not.toBeNull();

    act(() => {
      pastePlainText(textarea as HTMLTextAreaElement, firstLongText);
    });
    act(() => {
      pastePlainText(textarea as HTMLTextAreaElement, secondLongText);
    });
    act(() => {
      pastePlainText(textarea as HTMLTextAreaElement, thirdLongText);
    });

    expect(container.querySelectorAll(".composer-collapsed-prompt-card")).toHaveLength(3);

    act(() => {
      foldedPromptButton("# B 交接提示词")?.click();
    });

    expect(container.querySelectorAll(".composer-collapsed-prompt-card")).toHaveLength(2);
    expect(container.querySelector<HTMLTextAreaElement>("textarea")?.value).toBe(secondLongText);

    act(() => {
      foldedPromptButton("# A 交接提示词")?.click();
    });

    expect(container.querySelectorAll(".composer-collapsed-prompt-card")).toHaveLength(1);
    expect(container.querySelector<HTMLTextAreaElement>("textarea")?.value).toBe(`${secondLongText}${firstLongText}`);

    act(() => {
      foldedPromptButton("# C 交接提示词")?.click();
    });

    expect(container.querySelector(".composer-collapsed-prompt-card")).toBeNull();
    expect(container.querySelector<HTMLTextAreaElement>("textarea")?.value).toBe(
      `${secondLongText}${firstLongText}${thirdLongText}`,
    );

    const sendButton = container.querySelector<HTMLButtonElement>("button[aria-label=\"发送\"]");
    act(() => {
      sendButton?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });

    expect(onSend).toHaveBeenCalledWith(`${secondLongText}${firstLongText}${thirdLongText}`);
  });

  it("folds repeated long pastes into sequential rows", () => {
    const firstLongText = longPastedPrompt("# A 交接提示词", "A");
    const secondLongText = longPastedPrompt("# B 交接提示词", "B");
    const onSend = vi.fn();
    renderStatefulComposer({ onSend });

    const textarea = container.querySelector<HTMLTextAreaElement>("textarea");
    expect(textarea).not.toBeNull();

    act(() => {
      pastePlainText(textarea as HTMLTextAreaElement, firstLongText);
    });
    act(() => {
      pastePlainText(textarea as HTMLTextAreaElement, secondLongText);
    });

    const cards = Array.from(container.querySelectorAll(".composer-collapsed-prompt-card"));
    expect(cards).toHaveLength(2);
    expect(cards[0]?.textContent).toContain("# A 交接提示词");
    expect(cards[1]?.textContent).toContain("# B 交接提示词");
    expect((textarea as HTMLTextAreaElement).value).toBe("");

    act(() => {
      setTextareaValue(textarea as HTMLTextAreaElement, "\n要求后续变更");
    });

    const sendButton = container.querySelector<HTMLButtonElement>("button[aria-label=\"发送\"]");
    act(() => {
      sendButton?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });

    expect(onSend).toHaveBeenCalledWith(`${firstLongText}${secondLongText}\n要求后续变更`);
  });

  it("removes only the folded prefix and keeps the follow-up draft", () => {
    const longText = longPastedPrompt();
    const onSend = vi.fn();
    renderStatefulComposer({ onSend });

    const textarea = container.querySelector<HTMLTextAreaElement>("textarea");
    expect(textarea).not.toBeNull();

    act(() => {
      pastePlainText(textarea as HTMLTextAreaElement, longText);
    });
    act(() => {
      setTextareaValue(textarea as HTMLTextAreaElement, "要求后续变更");
    });

    const removeButton = container.querySelector<HTMLButtonElement>(".composer-collapsed-prompt-remove");
    expect(removeButton).not.toBeNull();

    act(() => {
      removeButton?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });

    expect(container.querySelector(".composer-collapsed-prompt-card")).toBeNull();
    expect(container.querySelector<HTMLTextAreaElement>("textarea")?.value).toBe("要求后续变更");

    const sendButton = container.querySelector<HTMLButtonElement>("button[aria-label=\"发送\"]");
    act(() => {
      sendButton?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });

    expect(onSend).toHaveBeenCalledWith("要求后续变更");
  });
});

function foldedPromptButton(title: string): HTMLButtonElement | undefined {
  return Array.from(container.querySelectorAll<HTMLButtonElement>(".composer-collapsed-prompt-main")).find((button) =>
    button.textContent?.includes(title),
  );
}

function expandPendingMessages(): void {
  const button = container.querySelector<HTMLButtonElement>(
    'button[aria-label="展开待处理消息"]',
  );
  expect(button).not.toBeNull();
  act(() => {
    button?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
  });
}

describe("Composer queue strip", () => {
  it("stacks the goal and pending drawers outside the composer frame", () => {
    renderComposer({
      running: true,
      goalSummary: activeGoalSummary(),
      queuedMessages: [
        { id: "queue-1", text: "排队消息", images: [], files: [] },
      ],
    });

    const goal = container.querySelector(".composer-goal-strip");
    const pending = container.querySelector(".composer-pending-drawer");
    const shell = container.querySelector(".composer-shell");
    const frameShell = container.querySelector(".composer-frame-shell");
    const frame = container.querySelector(".composer-frame");
    const actions = Array.from(
      container.querySelectorAll(".composer-goal-strip-action, .composer-pending-toggle"),
    );

    expect(container.querySelector(".composer-input-header")).toBeNull();
    expect(goal?.parentElement).toBe(shell);
    expect(pending?.parentElement).toBe(shell);
    expect(frameShell?.parentElement).toBe(shell);
    expect(frame?.parentElement).toBe(frameShell);
    expect(goal?.nextElementSibling).toBe(pending);
    expect(pending?.nextElementSibling).toBe(frameShell);
    expect(pending?.querySelector(".composer-pending-title")?.textContent).toBe("待处理 1");
    expect(pending?.querySelector(".composer-pending-preview")?.textContent).toBe("排队消息");
    expect(pending?.querySelector(".composer-queue-list")).toBeNull();
    expect(actions.length).toBeGreaterThan(0);
    expect(
      actions.every((action) => action.classList.contains("composer-input-header-action")),
    ).toBe(true);
    expect(pending?.querySelector(".composer-pending-summary-select")).not.toBeNull();
    expect(goal?.querySelector(".composer-goal-strip-summary-select")).not.toBeNull();
  });

  it("allows only one goal or pending drawer to be expanded", () => {
    renderComposer({
      running: true,
      goalSummary: activeGoalSummary(),
      queuedMessages: [
        { id: "queue-1", text: "待处理消息", images: [], files: [] },
      ],
    });

    const goalToggle = container.querySelector<HTMLButtonElement>(
      'button[aria-label="展开目标"]',
    );
    const pendingToggle = container.querySelector<HTMLButtonElement>(
      'button[aria-label="展开待处理消息"]',
    );
    expect(goalToggle?.getAttribute("aria-expanded")).toBe("false");
    expect(pendingToggle?.getAttribute("aria-expanded")).toBe("false");

    act(() => {
      container
        .querySelector<HTMLButtonElement>(".composer-pending-summary-select")
        ?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    expect(container.querySelector(".composer-pending-drawer")?.classList.contains("expanded")).toBe(true);
    expect(container.querySelector(".composer-goal-strip")?.classList.contains("expanded")).toBe(false);
    expect(container.querySelector(".composer-queue-list")).not.toBeNull();

    act(() => {
      goalToggle?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    expect(container.querySelector(".composer-goal-strip")?.classList.contains("expanded")).toBe(true);
    expect(container.querySelector(".composer-pending-drawer")?.classList.contains("expanded")).toBe(false);
    expect(container.querySelector(".composer-queue-list")).toBeNull();
  });

  it("does not present ordinary pending messages as held when only the goal is paused", () => {
    renderComposer({
      running: true,
      goalSummary: {
        ...activeGoalSummary(),
        status: "paused",
        can_pause: false,
        can_resume: true,
      },
      queuedMessages: [
        { id: "queue-1", text: "仍是普通 Queue", images: [], files: [] },
      ],
    });

    expect(container.querySelector(".composer-goal-strip-state")?.textContent).toBe("已暂停");
    expect(container.querySelector(".composer-pending-title")?.textContent).toBe("待处理 1");
    expect(container.querySelector(".composer-pending-drawer")?.classList.contains("is-held")).toBe(false);
    expect(container.querySelector(".composer-held-notice")).toBeNull();
  });

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
    expandPendingMessages();

    const rows = Array.from(
      container.querySelectorAll<HTMLLIElement>(".composer-queue-row")
    );
    expect(rows).toHaveLength(3);
    // guide (oldest, first) → queue items follow in queue order
    expect(rows[0]?.dataset.position).toBe("1");
    expect(rows[0]?.classList.contains("guide")).toBe(true);
    expect(rows[0]?.querySelector(".composer-queue-index")?.textContent).toBe("1");
    expect(rows[0]?.querySelector(".composer-queue-kind")?.textContent).toBe("Steer");
    expect(rows[1]?.dataset.position).toBe("2");
    expect(rows[1]?.classList.contains("queue")).toBe(true);
    expect(rows[1]?.querySelector(".composer-queue-index")?.textContent).toBe("2");
    expect(rows[1]?.querySelector(".composer-queue-kind")?.textContent).toBe("Queue");
    expect(rows[2]?.dataset.position).toBe("3");
    expect(rows[2]?.classList.contains("queue")).toBe(true);
    expect(rows[2]?.querySelector(".composer-queue-index")?.textContent).toBe("3");
  });

  it("shows interrupted held work in server order with per-item continue controls", () => {
    const onGuideQueuedMessage = vi.fn();
    renderComposer({
      running: false,
      goalSummary: activeGoalSummary(),
      queuedMessages: [
        {
          id: "queue-1",
          text: "第一个",
          images: [],
          files: [],
          held: true,
          heldPosition: 1,
          origin: "queue",
        },
        {
          id: "queue-2",
          text: "第二个",
          images: [],
          files: [],
          held: true,
          heldPosition: 2,
          origin: "queue",
        },
      ],
      guideMessages: [
        {
          id: "guide-1",
          text: "先前引导",
          images: [],
          files: [],
          held: true,
          heldPosition: 0,
          origin: "steer",
        },
      ],
      onGuideQueuedMessage,
    });

    expect(container.querySelector(".composer-held-notice")?.textContent).toBe(
      "当前回复已中断；这些 Steer 和 Queue 不会自动执行。",
    );
    expect(container.querySelector(".composer-pending-title")?.textContent).toBe("已暂存 3");
    expect(container.querySelector(".composer-pending-drawer")?.classList.contains("expanded")).toBe(true);
    expect(container.querySelector(".composer-goal-strip")?.classList.contains("expanded")).toBe(false);
    const rows = Array.from(
      container.querySelectorAll<HTMLLIElement>(".composer-queue-row"),
    );
    expect(rows.map((row) => row.querySelector(".composer-queue-preview")?.textContent)).toEqual([
      "先前引导",
      "第一个",
      "第二个",
    ]);
    const continueGuide = container.querySelector<HTMLButtonElement>(
      'button[aria-label="继续暂存任务 1"]',
    );
    act(() => {
      continueGuide?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    expect(onGuideQueuedMessage).toHaveBeenCalledWith("guide-1");
  });

  it("lives inside the composer shell so the drawer spans the input width", () => {
    renderComposer({
      running: true,
      queuedMessages: [
        { id: "queue-1", text: "排队宽度测试", images: [], files: [] }
      ]
    });

    const pending = container.querySelector(".composer-pending-drawer");
    const shell = container.querySelector(".composer-shell");
    expect(pending).not.toBeNull();
    expect(shell).not.toBeNull();
    expect(shell?.contains(pending ?? null)).toBe(true);
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
    expandPendingMessages();

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
    expandPendingMessages();

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
    expandPendingMessages();

    const previewButton = container.querySelector<HTMLButtonElement>(
      "button[aria-label=\"编辑排队消息内容 1\"]"
    );
    expect(previewButton).not.toBeNull();

    act(() => {
      previewButton?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });

    expect(onEditQueuedMessage).toHaveBeenCalledWith("queue-1");
  });

  it("edits a queued message from the explicit inline edit button", () => {
    const onEditQueuedMessage = vi.fn();
    renderComposer({
      running: true,
      queuedMessages: [
        { id: "queue-1", text: "要求后续变更", images: [], files: [] }
      ],
      onEditQueuedMessage
    });
    expandPendingMessages();

    const editButton = container.querySelector<HTMLButtonElement>(
      "button[aria-label=\"编辑排队消息 1\"]"
    );
    expect(editButton).not.toBeNull();

    act(() => {
      editButton?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
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
    expandPendingMessages();

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
    expect(permissionModeFromSummary()).toBe("standard");
    expect(permissionModeFromSummary({ mode: "standard" })).toBe("standard");
    expect(permissionModeFromSummary({ mode: "read_only" })).toBe("read_only");
    expect(permissionModeFromSummary({ mode: "unconfined" })).toBe("unconfined");
  });

  it("shows the everyday permission modes in the composer menu", () => {
    const onSelectPermissionMode = vi.fn();
    renderComposer({
      accessMenuOpen: true,
      permissions: { mode: "standard" },
      onSelectPermissionMode,
    });

    const chip = container.querySelector<HTMLButtonElement>(
      "button[aria-label=\"权限模式：标准\"]",
    );
    expect(chip).not.toBeNull();
    expect(chip?.disabled).toBe(false);

    const labels = Array.from(
      document.body.querySelectorAll<HTMLButtonElement>(
        "button[role=\"menuitemradio\"] strong",
      ),
    ).map((label) => label.textContent?.trim());
    expect(labels).toEqual(["工作区内完全信任", "只读", "无边界"]);
    expect(document.body.textContent).not.toContain("平衡");
    expect(document.body.textContent).not.toContain("严格");
    expect(document.body.textContent).not.toContain("替我审批");

    const checkedLabels = Array.from(
      document.body.querySelectorAll<HTMLButtonElement>(
        "button[role=\"menuitemradio\"][aria-checked=\"true\"] strong",
      ),
    ).map((label) => label.textContent?.trim());
    expect(checkedLabels).toEqual(["工作区内完全信任"]);
    expect(document.body.textContent).not.toContain("profile:");
    expect(document.body.textContent).not.toContain("reviewer:");
  });

  it("lets the user switch between the three permission modes", () => {
    const onSelectPermissionMode = vi.fn();
    renderComposer({
      accessMenuOpen: true,
      permissions: { mode: "standard" },
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

    const unconfinedOption = Array.from(
      document.body.querySelectorAll<HTMLButtonElement>(
        "button[role=\"menuitemradio\"]",
      ),
    ).find((button) => button.textContent?.includes("无边界"));
    expect(unconfinedOption).not.toBeUndefined();

    act(() => {
      unconfinedOption?.dispatchEvent(
        new MouseEvent("click", { bubbles: true, cancelable: true }),
      );
    });

    expect(onSelectPermissionMode).toHaveBeenCalledWith("unconfined");
  });
});

describe("ComposerTokenGauge", () => {
  it("does not schedule animation frames while idle at zero", () => {
    const requestAnimationFrame = vi.spyOn(window, "requestAnimationFrame");

    try {
      renderComposer({ running: false, tokensPerSecond: 0 });

      expect(requestAnimationFrame).not.toHaveBeenCalled();
    } finally {
      requestAnimationFrame.mockRestore();
    }
  });

  it("keeps the gauge visible with the speed label inline and a hidden hover tooltip", () => {
    renderComposer({ running: false, tokensPerSecond: 0 });
    const gauge = container.querySelector(".composer-token-gauge");
    expect(gauge).not.toBeNull();
    expect(gauge?.getAttribute("data-state")).toBe("idle");
    expect(gauge?.getAttribute("aria-label")).toContain("0 token 每秒");

    // The label is still inline next to the dial when the toolbar is wide
    // enough, but the same value is also available through a hover tooltip
    // for narrow widths where the inline label is hidden by the container query.
    const label = container.querySelector(".composer-token-gauge-label");
    expect(label).not.toBeNull();
    expect(label?.textContent).toContain("0 tok/s");
    expect(label?.textContent).toContain("tok/s");
    expect(document.body.querySelector(".composer-token-gauge-tooltip")).toBeNull();
  });

  it("renders a live gauge without scheduling animation frames and shows the speed on hover", () => {
    const requestAnimationFrame = vi
      .spyOn(window, "requestAnimationFrame")
      .mockImplementation(() => 1);

    try {
      renderComposer({ running: true, tokensPerSecond: 18.4 });

      const gauge = container.querySelector(".composer-token-gauge");
      expect(gauge).not.toBeNull();
      expect(gauge?.getAttribute("data-state")).toBe("running");
      expect(gauge?.getAttribute("title")).toBeNull();
      expect(requestAnimationFrame).not.toHaveBeenCalled();

      // Label is inline by default; hovering opens the tooltip portal.
      const label = container.querySelector(".composer-token-gauge-label");
      expect(label).not.toBeNull();
      expect(label?.textContent).toContain("18 tok/s");
      expect(label?.textContent).toContain("tok/s");
      expect(document.body.querySelector(".composer-token-gauge-tooltip")).toBeNull();

      act(() => {
        gauge?.dispatchEvent(new MouseEvent("mouseover", { bubbles: true }));
      });
      const tooltip = document.body.querySelector(".composer-token-gauge-tooltip");
      expect(tooltip).not.toBeNull();
      expect(tooltip?.textContent).toContain("18");
      expect(tooltip?.textContent).toContain("token 每秒");

      // Dial components are still rendered.
      const svg = container.querySelector(".composer-token-gauge-svg");
      expect(svg?.getAttribute("width")).toBe("20");
      expect(svg?.getAttribute("height")).toBe("20");
      expect(container.querySelector(".composer-token-gauge-progress")).not.toBeNull();
      expect(container.querySelector(".composer-token-gauge-needle")).not.toBeNull();
      expect(container.querySelector(".composer-token-gauge-inner-arc")).toBeNull();
      expect(container.querySelectorAll(".composer-token-gauge-speed-dot")).toHaveLength(0);
    } finally {
      requestAnimationFrame.mockRestore();
    }
  });

  it("decays a stale token speed without animation frames", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-01-01T00:00:00Z"));
    const requestAnimationFrame = vi.spyOn(window, "requestAnimationFrame");

    try {
      renderComposer({
        running: true,
        tokensPerSecond: 20,
        tokenSpeedSampledAt: Date.now(),
      });

      expect(container.querySelector(".composer-token-gauge-label")?.textContent).toContain(
        "20 tok/s",
      );

      act(() => {
        vi.advanceTimersByTime(3_800);
      });
      expect(container.querySelector(".composer-token-gauge-label")?.textContent).toContain(
        "10 tok/s",
      );

      act(() => {
        vi.advanceTimersByTime(3_000);
      });
      expect(container.querySelector(".composer-token-gauge-label")?.textContent).toContain(
        "0 tok/s",
      );
      expect(requestAnimationFrame).not.toHaveBeenCalled();
    } finally {
      requestAnimationFrame.mockRestore();
      vi.useRealTimers();
    }
  });

  it("marks fallback token speed as approximate in the inline label", () => {
    renderComposer({
      running: true,
      tokensPerSecond: 18.4,
      tokenSpeedSource: "estimated",
    });

    const label = container.querySelector(".composer-token-gauge-label");
    expect(label?.textContent).toContain("约 18 tok/s");
  });
});

describe("Composer expand button", () => {
  it("keeps the document composer shorter without changing expanded input", () => {
    expect(composerCSS).toMatch(
      /\.document-composer-wrap\s+\.composer-stack\s*\{[^}]*--composer-collapsed-min-height:\s*80px/,
    );
    expect(composerCSS).toMatch(
      /\.document-composer-wrap\s+\.composer-stack:not\(\.is-expanded\)\s+\.composer\s+textarea\s*\{[^}]*height:\s*44px[^}]*min-height:\s*44px/,
    );
    expect(composerCSS).toMatch(
      /\.document-composer-wrap\s+\.composer-bar\s*\{[^}]*height:\s*36px[^}]*padding-bottom:\s*2px/,
    );
    expect(workspaceCSS).toMatch(
      /\.workspace-document-turn-drawer\s*\{[^}]*width:\s*calc\(100% - 48px\)/,
    );
    expect(composerCSS).toMatch(
      /\.document-composer-wrap\s+\.composer-goal-strip:has\(\+ \.composer-pending-drawer\)\s*\{[^}]*width:\s*calc\(100% - 48px\)/,
    );
    expect(workspaceCSS).toMatch(
      /\.workspace-document-turn-dock:has\([^}]*\.composer-goal-strip \+ \.composer-pending-drawer[^}]*\)[^{]*\.workspace-document-turn-drawer\s*\{[^}]*width:\s*calc\(100% - 72px\)/,
    );
    expect(composerCSS).toMatch(
      /\.document-composer-wrap\s+\.composer-accessory-drawer:hover,[^{]*\.document-composer-wrap\s+\.composer-accessory-drawer:focus-within\s*\{[^}]*translate:\s*0 -2px[^}]*border-color:[^}]*box-shadow:/,
    );
    expect(composerCSS).toMatch(
      /\.document-composer-wrap\s+\.composer-accessory-drawer\.expanded\s*\{[^}]*z-index:\s*4[^}]*translate:\s*0 -6px/,
    );
    expect(composerCSS).toMatch(
      /\.composer-accessory-drawer\s*\{[^}]*border-radius:\s*12px 12px 10px 10px/,
    );
    expect(workspaceCSS).toMatch(
      /\.workspace-document-turn-drawer\.expanded\s*\{[^}]*translate:\s*0 -6px[^}]*border-radius:\s*16px/,
    );
    expect(workspaceCSS).not.toMatch(
      /\.workspace-document-turn-drawer\.expanded\s*\{[^}]*width:/,
    );
    expect(workspaceCSS).toMatch(
      /\.workspace-document-turn-summary\s*\{[^}]*width:\s*100%[^}]*justify-items:\s*end[^}]*padding:\s*0 12px 0 0/,
    );
  });

  it("uses anchored flex layouts so the bottom toolbar stays pinned when expanded", () => {
    expect(composerCSS).toContain(".composer-stack.is-expanded");
    expect(composerCSS).toContain("min-height: clamp(180px, 34vh, 320px)");
    expect(composerCSS).toContain("--composer-collapsed-min-height: 100px");
    expect(composerCSS).toMatch(
      /\.composer\s+textarea\s*\{[^}]*display:\s*block[^}]*height:\s*60px[^}]*min-height:\s*60px[^}]*padding:\s*10px\s+44px\s+8px\s+var\(--composer-text-start\)/,
    );
    expect(composerCSS).toMatch(
      /\.hero-composer-wrap\s+\.composer\s+textarea\s*\{[^}]*height:\s*66px[^}]*min-height:\s*66px[^}]*padding:\s*16px\s+52px\s+8px\s+var\(--composer-text-start\)/,
    );
    expect(composerCSS).toMatch(
      /\.composer-bar\s*\{[^}]*height:\s*40px[^}]*padding:\s*0\s+8px\s+4px\s+calc\(var\(--composer-text-start\)\s*-\s*var\(--composer-control-icon-inset\)\)/,
    );
    expect(composerCSS).toMatch(
      /\.composer-send-button,\s*\n\.composer-stop-button\s*\{[^}]*width:\s*28px[^}]*height:\s*28px/,
    );
    expect(composerCSS).toMatch(
      /\.composer-send-button\s+svg\s*\{[^}]*width:\s*14px[^}]*height:\s*14px/,
    );
    expect(composerCSS).toContain("--composer-expanded-min-height: clamp(240px, 44vh, 420px)");
    expect(composerCSS).toMatch(
      /\.hero-composer-wrap\s+\.composer-stack\s*\{[^}]*--composer-collapsed-min-height:\s*106px/,
    );
    expect(composerCSS).toMatch(
      /\.dock-composer-wrap\s*\{[^}]*align-self:\s*end/,
    );
    expect(turnsCSS).toMatch(
      /\.empty-home-inner\s*>\s*\.hero-composer-wrap\s*\{[^}]*height:\s*106px[^}]*align-items:\s*flex-end/,
    );
    expect(composerCSS).toMatch(
      /\.composer-frame\s*\{[^}]*contain:\s*layout paint/,
    );
    expect(composerCSS).toMatch(
      /\.composer\s*\{[^}]*position:\s*relative/,
    );
    // Expanded composer is a flex column; the textarea absorbs the extra
    // height so .composer-bar stays at the original bottom edge instead
    // of floating above block-flow whitespace.
    expect(composerCSS).toMatch(
      /\.composer-stack\.is-expanded\s+\.composer\s*\{[^}]*display:\s*flex[^}]*flex-direction:\s*column/,
    );
    expect(composerCSS).toMatch(
      /\.composer-stack\.is-expanded\s+\.composer-frame-shell\s*\{[^}]*margin-bottom:\s*calc\(var\(--composer-expanded-offset,\s*var\(--composer-expanded-delta\)\) \* -1\)[^}]*transform:\s*translateY\(calc\(var\(--composer-expanded-offset,\s*var\(--composer-expanded-delta\)\) \* -1\)\)/,
    );
    expect(composerCSS).toMatch(
      /\.composer-stack\.is-expanded\s+\.composer-goal-strip,\s*\.composer-stack\.is-expanded\s+\.composer-pending-drawer\s*\{[^}]*transform:\s*translateY\(calc\(var\(--composer-expanded-offset,\s*var\(--composer-expanded-delta\)\) \* -1\)\)/,
    );
    expect(composerCSS).toMatch(
      /\.composer-stack\.is-expanded\s+\.composer\s*\{[^}]*min-height:\s*var\(--composer-expanded-min-height\)/,
    );
    expect(composerCSS).toMatch(
      /\.composer-stack\.is-expanded\s+\.composer\s+textarea\s*\{[^}]*flex:\s*1\s+1\s+0[^}]*height:\s*auto/,
    );
    expect(composerCSS).not.toContain("grid-template-rows: auto minmax(0, 1fr) auto");
    expect(composerCSS).not.toContain("transition: min-height");
    expect(composerCSS).not.toContain("transition: width");
    // Width stays pinned to the session composer width in both dock and hero
    // variants — the expand button only grows the composer vertically.
    expect(composerCSS).not.toContain("width: min(1040px");
  });

  it("anchors the expanded frame to the original bottom edge in the hero composer", () => {
    renderComposer({ variant: "hero" });
    const stack = container.querySelector(".composer-stack");
    const frame = container.querySelector<HTMLDivElement>(".composer-frame");
    const button = container.querySelector<HTMLButtonElement>(".composer-expand-button");
    expect(stack).not.toBeNull();
    expect(frame).not.toBeNull();
    expect(button).not.toBeNull();

    Object.defineProperty(frame!, "offsetHeight", {
      configurable: true,
      get: () => (stack?.classList.contains("is-expanded") ? 420 : 136),
    });

    act(() => {
      button?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });

    expect(stack?.classList.contains("is-expanded")).toBe(true);
    expect(frame?.style.getPropertyValue("--composer-expanded-offset")).toBe("284px");

    act(() => {
      button?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });

    expect(stack?.classList.contains("is-expanded")).toBe(false);
    expect(frame?.style.getPropertyValue("--composer-expanded-offset")).toBe("");
  });

  it("renders the expand button inside the composer input area", () => {
    renderComposer({});

    const frame = container.querySelector(".composer-frame");
    const composer = container.querySelector(".composer");
    const button = container.querySelector<HTMLButtonElement>(".composer-expand-button");

    expect(button).not.toBeNull();
    expect(button?.getAttribute("aria-label")).toBe("展开输入框");
    expect(button?.getAttribute("aria-pressed")).toBe("false");
    expect(button?.getAttribute("title")).toBe("展开输入框");
    expect(button?.parentElement).toBe(composer);
    expect(frame?.lastElementChild).toBe(composer);
    expect(button?.querySelector("svg")).not.toBeNull();
  });

  it("keeps the expand button anchored to the input area when messages are queued", () => {
    renderComposer({
      running: true,
      queuedMessages: [
        { id: "queue-1", text: "排队时按钮应该跟输入区对齐", images: [], files: [] },
      ],
    });

    const pending = container.querySelector(".composer-pending-drawer");
    const composer = container.querySelector(".composer");
    const button = container.querySelector<HTMLButtonElement>(".composer-expand-button");

    expect(pending).not.toBeNull();
    expect(composer).not.toBeNull();
    expect(button).not.toBeNull();
    expect(button?.parentElement).toBe(composer);
    expect(pending?.contains(button ?? null)).toBe(false);
  });

  it("toggles the expanded composer state from one click", async () => {
    renderComposer({});
    const stack = container.querySelector(".composer-stack");
    const textarea = container.querySelector<HTMLTextAreaElement>("textarea");
    const button = container.querySelector<HTMLButtonElement>(".composer-expand-button");
    expect(stack).not.toBeNull();
    expect(textarea).not.toBeNull();
    expect(button).not.toBeNull();

    act(() => {
      button?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });

    await act(async () => {
      await nextAnimationFrame();
    });

    expect(stack?.classList.contains("is-expanded")).toBe(true);
    expect(button?.getAttribute("aria-label")).toBe("收起输入框");
    expect(button?.getAttribute("aria-pressed")).toBe("true");
    expect(button?.getAttribute("title")).toBe("收起输入框");
    expect(document.activeElement).toBe(textarea);

    act(() => {
      button?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });

    expect(stack?.classList.contains("is-expanded")).toBe(false);
    expect(button?.getAttribute("aria-label")).toBe("展开输入框");
    expect(button?.getAttribute("aria-pressed")).toBe("false");
  });

  it("disables expansion in read-only mode", () => {
    renderComposer({ readOnly: true });
    const stack = container.querySelector(".composer-stack");
    const button = container.querySelector<HTMLButtonElement>(".composer-expand-button");
    expect(button).not.toBeNull();
    expect(button?.disabled).toBe(true);
    expect(button?.getAttribute("title")).toBe("只读会话不可展开");
    expect(stack?.classList.contains("is-expanded")).toBe(false);
  });

  it("places the placeholder line on the same axis as the expand button", () => {
    // Top padding 10px + line-height 24px / 2 = 22px visual center; the
    // expand button is at top:8 height:28 → center 22px. They line up so
    // the placeholder glyph sits next to the chevron, not high above it.
    expect(composerCSS).toMatch(
      /\.composer\s+textarea\s*\{[^}]*padding:\s*10px\s+44px\s+8px\s+var\(--composer-text-start\)/,
    );
    expect(composerCSS).toMatch(
      /\.hero-composer-wrap\s+\.composer\s+textarea\s*\{[^}]*padding:\s*16px\s+52px\s+8px\s+var\(--composer-text-start\)/,
    );
  });

  it("centers the expand button on the same vertical axis as the send button", () => {
    // The send button is the rightmost 28x28 element in the bar; the expand
    // button is positioned absolutely at right:8 width:28, so its right edge
    // sits 8px from the frame's right edge — same gutter as the bar.
    expect(composerCSS).toMatch(
      /\.composer-expand-button\s*\{[^}]*right:\s*8px[^}]*width:\s*28px[^}]*height:\s*28px/,
    );
  });

  it("moves the goal / steer / queue header actions in sync with the expand button", () => {
    // The expand button intentionally mirrors the input-header row actions;
    // when it shifts right by 6px the header row inline padding must follow
    // so the right-edge control column stays aligned across rows.
    expect(composerCSS).toMatch(
      /--composer-input-header-inline-padding:\s*8px;/,
    );
    expect(composerCSS).not.toMatch(
      /--composer-input-header-inline-padding:\s*14px;/,
    );
  });
});

describe("composer drag and drop", () => {
  function composerFrame(): HTMLElement {
    const frame = container.querySelector<HTMLElement>(".composer-frame");
    if (!frame) throw new Error("missing composer frame");
    return frame;
  }

  it("appends a dragged workspace path to the prompt as plain text", () => {
    renderStatefulComposer({ initialPrompt: "看看这个文件" });

    act(() => {
      dispatchDrag(composerFrame(), "drop", mockDataTransfer({
        types: [WORKSPACE_FILE_DRAG_MIME, "text/plain"],
        pathData: "src/index.ts",
      }));
    });

    expect(container.querySelector<HTMLTextAreaElement>("textarea")?.value).toBe(
      "看看这个文件 src/index.ts ",
    );
  });

  it("inserts a path into an empty prompt without a leading space", () => {
    renderStatefulComposer({});

    act(() => {
      dispatchDrag(composerFrame(), "drop", mockDataTransfer({
        types: [WORKSPACE_FILE_DRAG_MIME],
        pathData: "src/components/",
      }));
    });

    expect(container.querySelector<HTMLTextAreaElement>("textarea")?.value).toBe(
      "src/components/ ",
    );
  });

  it("forwards dropped external files to the attachment pipeline", () => {
    const onPasteAttachmentFiles = vi.fn();
    renderStatefulComposer({ onPasteAttachmentFiles });
    const image = new File(["png"], "shot.png", { type: "image/png" });
    const pdf = new File(["%PDF"], "brief.pdf", { type: "application/pdf" });

    act(() => {
      dispatchDrag(composerFrame(), "drop", mockDataTransfer({
        types: ["Files"],
        files: [image, pdf],
      }));
    });

    expect(onPasteAttachmentFiles).toHaveBeenCalledTimes(1);
    expect(onPasteAttachmentFiles.mock.calls[0][0]).toEqual([image, pdf]);
  });

  it("ignores drops while read-only", () => {
    const onPasteAttachmentFiles = vi.fn();
    renderStatefulComposer({ initialPrompt: "draft", readOnly: true, onPasteAttachmentFiles });

    act(() => {
      dispatchDrag(composerFrame(), "drop", mockDataTransfer({
        types: [WORKSPACE_FILE_DRAG_MIME],
        pathData: "src/index.ts",
      }));
      dispatchDrag(composerFrame(), "drop", mockDataTransfer({
        types: ["Files"],
        files: [new File(["png"], "shot.png", { type: "image/png" })],
      }));
    });

    expect(container.querySelector<HTMLTextAreaElement>("textarea")?.value).toBe("draft");
    expect(onPasteAttachmentFiles).not.toHaveBeenCalled();
  });

  it("accepts path drops on a text-only composer but ignores external files", () => {
    const onPasteAttachmentFiles = vi.fn();
    renderStatefulComposer({ textOnly: true, onPasteAttachmentFiles });

    act(() => {
      dispatchDrag(composerFrame(), "drop", mockDataTransfer({
        types: [WORKSPACE_FILE_DRAG_MIME],
        pathData: "src/index.ts",
      }));
    });

    expect(container.querySelector<HTMLTextAreaElement>("textarea")?.value).toBe("src/index.ts ");

    act(() => {
      dispatchDrag(composerFrame(), "drop", mockDataTransfer({
        types: ["Files"],
        files: [new File(["png"], "shot.png", { type: "image/png" })],
      }));
    });

    expect(onPasteAttachmentFiles).not.toHaveBeenCalled();
  });

  it("highlights the frame during an accepted dragover and clears on dragleave and drop", () => {
    renderStatefulComposer({});
    const frame = composerFrame();

    act(() => {
      dispatchDrag(frame, "dragover", mockDataTransfer({ types: ["Files"] }));
    });
    expect(container.querySelector(".composer-frame-drop-active")).not.toBeNull();

    // Moving between children keeps the highlight: the related target is
    // still inside the frame.
    const child = frame.querySelector("textarea");
    act(() => {
      dispatchDrag(frame, "dragleave", mockDataTransfer({ types: ["Files"] }), child);
    });
    expect(container.querySelector(".composer-frame-drop-active")).not.toBeNull();

    act(() => {
      dispatchDrag(frame, "dragleave", mockDataTransfer({ types: ["Files"] }), document.body);
    });
    expect(container.querySelector(".composer-frame-drop-active")).toBeNull();

    act(() => {
      dispatchDrag(frame, "dragover", mockDataTransfer({ types: ["Files"] }));
    });
    expect(container.querySelector(".composer-frame-drop-active")).not.toBeNull();
    act(() => {
      dispatchDrag(frame, "drop", mockDataTransfer({ types: [WORKSPACE_FILE_DRAG_MIME], pathData: "a.ts" }));
    });
    expect(container.querySelector(".composer-frame-drop-active")).toBeNull();
  });

  it("does not highlight for payloads the composer does not accept", () => {
    renderStatefulComposer({});
    const frame = composerFrame();

    act(() => {
      dispatchDrag(frame, "dragover", mockDataTransfer({ types: ["text/plain"] }));
    });

    expect(container.querySelector(".composer-frame-drop-active")).toBeNull();
  });

  it("keeps the frame border highlight style in the stylesheet", () => {
    expect(composerCSS).toContain(".composer-frame.composer-frame-drop-active");
    expect(workspaceCSS).toContain(".split-composer-shell.split-composer-shell-drop-active");
  });

  it("appends a dragged workspace path in the split pane composer", () => {
    renderStatefulSplitPaneComposer({ initialPrompt: "review" });
    const shell = container.querySelector<HTMLElement>(".split-composer-shell");
    if (!shell) throw new Error("missing split composer shell");

    act(() => {
      dispatchDrag(shell, "drop", mockDataTransfer({
        types: [WORKSPACE_FILE_DRAG_MIME],
        pathData: "src/Button.tsx",
      }));
    });

    expect(container.querySelector<HTMLTextAreaElement>("textarea")?.value).toBe(
      "review src/Button.tsx ",
    );
  });

  it("forwards dropped files in the split pane composer and highlights its shell", () => {
    const onPasteAttachmentFiles = vi.fn();
    renderStatefulSplitPaneComposer({ onPasteAttachmentFiles });
    const shell = container.querySelector<HTMLElement>(".split-composer-shell");
    if (!shell) throw new Error("missing split composer shell");

    act(() => {
      dispatchDrag(shell, "dragover", mockDataTransfer({ types: ["Files"] }));
    });
    expect(container.querySelector(".split-composer-shell-drop-active")).not.toBeNull();

    const image = new File(["png"], "shot.png", { type: "image/png" });
    act(() => {
      dispatchDrag(shell, "drop", mockDataTransfer({ types: ["Files"], files: [image] }));
    });

    expect(onPasteAttachmentFiles).toHaveBeenCalledTimes(1);
    expect(onPasteAttachmentFiles.mock.calls[0][0]).toEqual([image]);
    expect(container.querySelector(".split-composer-shell-drop-active")).toBeNull();
  });
});
