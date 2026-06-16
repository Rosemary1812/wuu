import { act, createRef } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  Composer,
  toolPolicyHasPresetOverrides,
  toolPolicyProfileFromSummary,
  type CodexModelLoadState,
  type ToolPolicyProfile,
} from "./ComposerView";
import type { QueuedComposerMessage } from "./ComposerMessages";
import type { InitializeResult, ToolPolicySummary } from "../shared/protocol";

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
  document.body
    .querySelectorAll("[data-floating-menu-owner=\"composer-access\"]")
    .forEach((element) => element.remove());
});

function initialized(toolPolicy?: ToolPolicySummary): InitializeResult {
  return {
    protocol_version: "wuu-app-server/v0.1",
    provider: "fake",
    model: "fake-model",
    workspace_root: "/tmp/project",
    tool_policy: toolPolicy,
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
  prompt?: string;
  running?: boolean;
  queuedMessages?: QueuedComposerMessage[];
  guideMessages?: QueuedComposerMessage[];
  toolPolicy?: ToolPolicySummary;
  onInterrupt?: () => void;
  onSend?: () => void;
  onGuideQueuedMessage?: (id: string) => void;
  onEditQueuedMessage?: (id: string) => void;
  onEditGuideMessage?: (id: string) => void;
  onSelectToolPolicyProfile?: (profile: ToolPolicyProfile) => void;
}): { onSelectToolPolicyProfile: (profile: ToolPolicyProfile) => void } {
  const codexModels: CodexModelLoadState = {
    loading: false,
    error: "",
    models: [],
  };
  const onSelectToolPolicyProfile = props.onSelectToolPolicyProfile ?? vi.fn();
  act(() => {
    root = createRoot(container);
    root.render(
      <Composer
        prompt={props.prompt ?? ""}
        setPrompt={() => {}}
        files={[]}
        images={[]}
        queuedMessages={props.queuedMessages ?? []}
        guideMessages={props.guideMessages ?? []}
        running={props.running ?? false}
        status="ready"
        readOnly={false}
        initialized={initialized(props.toolPolicy)}
        projects={[]}
        codexModels={codexModels}
        codexRuntimeMenu={null}
        codexRuntimeRef={createRef<HTMLDivElement>()}
        menuOpen={false}
        accessMenuOpen={props.accessMenuOpen ?? false}
        modeMenuOpen={false}
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
        onSelectToolPolicyProfile={onSelectToolPolicyProfile}
        onToggleModeMenu={() => {}}
        onToggleBranchMenu={() => {}}
        onOpenSettings={() => {}}
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
        onGuideQueuedMessage={props.onGuideQueuedMessage ?? (() => {})}
        onEditQueuedMessage={props.onEditQueuedMessage ?? (() => {})}
        onEditGuideMessage={props.onEditGuideMessage ?? (() => {})}
        onSend={props.onSend ?? (() => {})}
        onInterrupt={props.onInterrupt ?? (() => {})}
      />,
    );
  });
  return { onSelectToolPolicyProfile };
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
});

describe("Composer queue strip", () => {
  it("lets a queued message become a guide", () => {
    const onGuideQueuedMessage = vi.fn();
    renderComposer({
      running: true,
      queuedMessages: [
        { id: "queue-1", text: "要求后续变更", images: [], files: [] },
      ],
      onGuideQueuedMessage,
    });

    const guideButton = container.querySelector<HTMLButtonElement>(
      "button[aria-label=\"作为引导发送\"]",
    );
    expect(guideButton).not.toBeNull();

    act(() => {
      guideButton?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });

    expect(onGuideQueuedMessage).toHaveBeenCalledWith("queue-1");
  });

  it("opens the queued message menu and edits the selected item", () => {
    const onEditQueuedMessage = vi.fn();
    renderComposer({
      running: true,
      queuedMessages: [
        { id: "queue-1", text: "要求后续变更", images: [], files: [] },
      ],
      onEditQueuedMessage,
    });

    const menuButton = container.querySelector<HTMLButtonElement>(
      "button[aria-label=\"待发送消息操作\"]",
    );
    expect(menuButton).not.toBeNull();

    act(() => {
      menuButton?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });

    const editItem = Array.from(
      container.querySelectorAll<HTMLButtonElement>("button[role=\"menuitem\"]"),
    ).find((button) => button.textContent?.includes("编辑消息"));
    expect(editItem).not.toBeUndefined();

    act(() => {
      editItem?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });

    expect(onEditQueuedMessage).toHaveBeenCalledWith("queue-1");
  });
});

describe("Composer permission menu", () => {
  it("maps tool policy summaries to preset chip states", () => {
    expect(toolPolicyProfileFromSummary()).toBe("autonomous");
    expect(toolPolicyProfileFromSummary({ profile: "safe" })).toBe("safe");
    expect(toolPolicyProfileFromSummary({ profile: "balanced" })).toBe("auto");
    expect(toolPolicyProfileFromSummary({ profile: "auto" })).toBe("auto");
    expect(toolPolicyProfileFromSummary({ profile: "enterprise_restricted" })).toBe("safe");
    expect(toolPolicyHasPresetOverrides({ profile: "safe" })).toBe(false);
    expect(
      toolPolicyHasPresetOverrides({
        profile: "safe",
        tools: { run_shell: "allow" },
      }),
    ).toBe(true);
  });

  it("shows only the three everyday permission modes in the composer menu", () => {
    const onSelectToolPolicyProfile = vi.fn();
    renderComposer({
      accessMenuOpen: true,
      toolPolicy: { profile: "auto" },
      onSelectToolPolicyProfile,
    });

    const chip = container.querySelector<HTMLButtonElement>(
      "button[aria-label=\"权限模式：自动\"]",
    );
    expect(chip).not.toBeNull();
    expect(chip?.disabled).toBe(false);

    const labels = Array.from(
      document.body.querySelectorAll<HTMLButtonElement>(
        "button[role=\"menuitemradio\"] strong",
      ),
    ).map((label) => label.textContent?.trim());
    expect(labels).toEqual(["手动", "自动", "完全访问"]);
    expect(document.body.textContent).not.toContain("平衡");
    expect(document.body.textContent).not.toContain("严格");

    const checkedLabels = Array.from(
      document.body.querySelectorAll<HTMLButtonElement>(
        "button[role=\"menuitemradio\"][aria-checked=\"true\"] strong",
      ),
    ).map((label) => label.textContent?.trim());
    expect(checkedLabels).toEqual(["自动"]);
  });

  it("lets the user switch between manual, automatic, and full access", () => {
    const onSelectToolPolicyProfile = vi.fn();
    renderComposer({
      accessMenuOpen: true,
      toolPolicy: { profile: "auto" },
      onSelectToolPolicyProfile,
    });

    const manualOption = Array.from(
      document.body.querySelectorAll<HTMLButtonElement>(
        "button[role=\"menuitemradio\"]",
      ),
    ).find((button) => button.textContent?.includes("手动"));
    expect(manualOption).not.toBeUndefined();

    act(() => {
      manualOption?.dispatchEvent(
        new MouseEvent("click", { bubbles: true, cancelable: true }),
      );
    });

    expect(onSelectToolPolicyProfile).toHaveBeenCalledWith("safe");

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

    expect(onSelectToolPolicyProfile).toHaveBeenCalledWith("autonomous");
  });

  it("shows a custom state when advanced overrides are present", () => {
    renderComposer({
      accessMenuOpen: true,
      toolPolicy: {
        profile: "safe",
        tools: { run_shell: "allow" },
      },
    });

    expect(container.textContent).toContain("自定义权限");
    expect(document.body.textContent).toContain("选择任一模式会改为该预设");
  });
});
