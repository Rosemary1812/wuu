import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { act, createRef, useState } from "react";
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
import { ImagePreviewProvider } from "./ImagePreview";
import type { QueuedComposerMessage } from "./ComposerMessages";
import type {
  DesktopProject,
  InitializeResult,
  PermissionSummary,
  RuntimeContext,
  SkillSummary,
  ToolPolicySummary,
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
  statusLiveProgress?: boolean;
  runtimeControlsDisabled?: boolean;
  readOnly?: boolean;
  onInterrupt?: () => void;
  onSend?: () => void;
  onOpenContextComposition?: () => void;
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
          prompt={props.prompt ?? ""}
          setPrompt={props.setPrompt ?? (() => {})}
          files={[]}
          images={[]}
          queuedMessages={props.queuedMessages ?? []}
          guideMessages={props.guideMessages ?? []}
          running={props.running ?? false}
          runtimeControlsDisabled={props.runtimeControlsDisabled}
          status={props.status ?? "ready"}
          statusLiveProgress={props.statusLiveProgress}
          readOnly={props.readOnly ?? false}
          initialized={initialized(props.toolPolicy, props.permissions)}
          projects={props.projects ?? []}
          activeContext={props.activeContext}
          activeProject={props.activeProject}
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
          onInterrupt={props.onInterrupt ?? (() => {})}
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

function renderStatefulComposer(props: {
  initialPrompt?: string;
  onSend?: (prompt: string) => void;
}): void {
  const codexModels: CodexModelLoadState = {
    loading: false,
    error: "",
    models: [],
  };

  function Harness(): JSX.Element {
    const [prompt, setPrompt] = useState(props.initialPrompt ?? "");
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
          status="ready"
          readOnly={false}
          initialized={initialized()}
          projects={[]}
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
          onPasteAttachmentFiles={() => {}}
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
    expect(leftGroup?.querySelector(".composer-project-control")).not.toBeNull();
    expect(leftGroup?.querySelector(".composer-attachment-button")).not.toBeNull();
    expect(leftGroup?.querySelector(".composer-slash-button")).not.toBeNull();
    expect(leftGroup?.querySelector(".permission-menu-anchor")).not.toBeNull();
    expect(rightGroup?.querySelector(".composer-token-gauge")).not.toBeNull();
    expect(rightGroup?.querySelector(".codex-runtime-anchor")).not.toBeNull();
    expect(rightGroup?.contains(sendButton)).toBe(true);
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
    const slashCollapse = responsiveDesignCSS.indexOf("@container composer-toolbar (max-width: 440px)");
    const projectCollapse = responsiveDesignCSS.indexOf("@container composer-toolbar (max-width: 360px)");

    expect(speedLabelCollapse).toBeGreaterThan(-1);
    expect(permissionLabelCollapse).toBeGreaterThan(speedLabelCollapse);
    expect(gaugeCollapse).toBeGreaterThan(permissionLabelCollapse);
    expect(runtimeCollapse).toBeGreaterThan(gaugeCollapse);
    expect(slashCollapse).toBeGreaterThan(runtimeCollapse);
    expect(projectCollapse).toBeGreaterThan(slashCollapse);
    expect(responsiveDesignCSS).toContain(".composer-token-gauge-label");
    expect(responsiveDesignCSS).toContain(".codex-runtime-anchor");
    expect(responsiveDesignCSS).toContain(".composer-slash-button");
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
    expect(document.body.textContent).not.toContain("profile:");
    expect(document.body.textContent).not.toContain("approval:");
    expect(document.body.textContent).not.toContain("reviewer:");
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
    expect(document.body.textContent).not.toContain("approval:");
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
  it("does not schedule animation frames while idle at zero", () => {
    const requestAnimationFrame = vi.spyOn(window, "requestAnimationFrame");

    try {
      renderComposer({ running: false, tokensPerSecond: 0 });

      expect(requestAnimationFrame).not.toHaveBeenCalled();
    } finally {
      requestAnimationFrame.mockRestore();
    }
  });

  it("keeps the gauge visible with the speed label always shown when idle", () => {
    renderComposer({ running: false, tokensPerSecond: 0 });
    const gauge = container.querySelector(".composer-token-gauge");
    expect(gauge).not.toBeNull();
    expect(gauge?.getAttribute("data-state")).toBe("idle");
    expect(gauge?.getAttribute("aria-label")).toContain("0 token 每秒");

    // The label is now inline next to the dial — no hover portal. It must
    // be in the DOM from the first render so the user always sees the rate.
    const label = container.querySelector(".composer-token-gauge-label");
    expect(label).not.toBeNull();
    expect(label?.textContent).toContain("0 tok/s");
    expect(label?.textContent).toContain("tok/s");
    expect(document.body.querySelector(".composer-token-gauge-tooltip")).toBeNull();
  });

  it("renders a live gauge with the speed label inline, no hover required", () => {
    const requestAnimationFrame = vi
      .spyOn(window, "requestAnimationFrame")
      .mockImplementation(() => 1);
    const cancelAnimationFrame = vi
      .spyOn(window, "cancelAnimationFrame")
      .mockImplementation(() => {});

    try {
      renderComposer({ running: true, tokensPerSecond: 18.4 });

      const gauge = container.querySelector(".composer-token-gauge");
      expect(gauge).not.toBeNull();
      expect(gauge?.getAttribute("data-state")).toBe("running");
      expect(gauge?.getAttribute("title")).toBeNull();
      expect(requestAnimationFrame).toHaveBeenCalled();

      // Label is inline; no portal, no hover gate. Hovering must not
      // resurrect a tooltip either.
      const label = container.querySelector(".composer-token-gauge-label");
      expect(label).not.toBeNull();
      expect(label?.textContent).toContain("18 tok/s");
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
    } finally {
      requestAnimationFrame.mockRestore();
      cancelAnimationFrame.mockRestore();
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
  it("uses anchored flex layouts so the bottom toolbar stays pinned when expanded", () => {
    expect(composerCSS).toContain(".composer-stack.is-expanded");
    expect(composerCSS).toContain("min-height: clamp(180px, 34vh, 320px)");
    expect(composerCSS).toContain("--composer-collapsed-min-height: 128px");
    expect(composerCSS).toContain("--composer-expanded-min-height: clamp(240px, 44vh, 420px)");
    expect(composerCSS).toMatch(
      /\.hero-composer-wrap\s+\.composer-stack\s*\{[^}]*--composer-collapsed-min-height:\s*136px/,
    );
    expect(composerCSS).toMatch(
      /\.dock-composer-wrap\s*\{[^}]*align-self:\s*end/,
    );
    expect(turnsCSS).toMatch(
      /\.empty-home-inner\s*>\s*\.hero-composer-wrap\s*\{[^}]*height:\s*136px[^}]*align-items:\s*flex-end/,
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
      /\.composer-stack\.is-expanded\s+\.composer-frame\s*\{[^}]*margin-bottom:\s*calc\(var\(--composer-expanded-offset,\s*var\(--composer-expanded-delta\)\) \* -1\)[^}]*transform:\s*translateY\(calc\(var\(--composer-expanded-offset,\s*var\(--composer-expanded-delta\)\) \* -1\)\)/,
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

    const queueList = container.querySelector(".composer-queue-list");
    const composer = container.querySelector(".composer");
    const button = container.querySelector<HTMLButtonElement>(".composer-expand-button");

    expect(queueList).not.toBeNull();
    expect(composer).not.toBeNull();
    expect(button).not.toBeNull();
    expect(button?.parentElement).toBe(composer);
    expect(queueList?.contains(button ?? null)).toBe(false);
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
});
