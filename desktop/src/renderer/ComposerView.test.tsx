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
  toolPolicy?: ToolPolicySummary;
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
        prompt=""
        setPrompt={() => {}}
        files={[]}
        images={[]}
        queuedMessages={[]}
        guideMessages={[]}
        running={false}
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
        onGuideQueuedMessage={() => {}}
        onClearQueuedMessages={() => {}}
        onSend={() => {}}
        onInterrupt={() => {}}
      />,
    );
  });
  return { onSelectToolPolicyProfile };
}

describe("Composer permission menu", () => {
  it("maps tool policy summaries to preset chip states", () => {
    expect(toolPolicyProfileFromSummary()).toBe("autonomous");
    expect(toolPolicyProfileFromSummary({ profile: "safe" })).toBe("safe");
    expect(toolPolicyProfileFromSummary({ profile: "balanced" })).toBe("balanced");
    expect(toolPolicyProfileFromSummary({ profile: "auto" })).toBe("auto");
    expect(toolPolicyHasPresetOverrides({ profile: "safe" })).toBe(false);
    expect(
      toolPolicyHasPresetOverrides({
        profile: "safe",
        tools: { run_shell: "allow" },
      }),
    ).toBe(true);
  });

  it("lets the user choose the automatic permission mode from the composer menu", () => {
    const onSelectToolPolicyProfile = vi.fn();
    renderComposer({
      accessMenuOpen: true,
      toolPolicy: { profile: "safe" },
      onSelectToolPolicyProfile,
    });

    const chip = container.querySelector<HTMLButtonElement>(
      "button[aria-label=\"权限模式：默认权限\"]",
    );
    expect(chip).not.toBeNull();
    expect(chip?.disabled).toBe(false);

    const automaticOption = Array.from(
      document.body.querySelectorAll<HTMLButtonElement>(
        "button[role=\"menuitemradio\"]",
      ),
    ).find((button) => button.textContent?.includes("自动"));
    expect(automaticOption).not.toBeUndefined();

    act(() => {
      automaticOption?.dispatchEvent(
        new MouseEvent("click", { bubbles: true, cancelable: true }),
      );
    });

    expect(onSelectToolPolicyProfile).toHaveBeenCalledWith("auto");
  });

  it("keeps the balanced permission mode separate from automatic mode", () => {
    const onSelectToolPolicyProfile = vi.fn();
    renderComposer({
      accessMenuOpen: true,
      toolPolicy: { profile: "safe" },
      onSelectToolPolicyProfile,
    });

    const labels = Array.from(
      document.body.querySelectorAll<HTMLButtonElement>(
        "button[role=\"menuitemradio\"] strong",
      ),
    ).map((label) => label.textContent?.trim());
    expect(labels).toEqual(["默认", "平衡", "自动", "危险", "严格"]);

    const balancedOption = Array.from(
      document.body.querySelectorAll<HTMLButtonElement>(
        "button[role=\"menuitemradio\"]",
      ),
    ).find((button) => button.textContent?.includes("平衡"));
    expect(balancedOption).not.toBeUndefined();

    act(() => {
      balancedOption?.dispatchEvent(
        new MouseEvent("click", { bubbles: true, cancelable: true }),
      );
    });

    expect(onSelectToolPolicyProfile).toHaveBeenCalledWith("balanced");
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
