import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { SettingsView, type SettingsUsageData } from "./SettingsView";
import type { BuildInfoResult, InitializeResult, WuuDesktopApi } from "../shared/protocol";

type GlobalWindow = typeof window & { wuu: WuuDesktopApi };

let container: HTMLDivElement;
let root: Root | null = null;

function noopResizeStart(): void {}
function noopResizeKey(): void {}

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

function installBuildInfoStub(info: BuildInfoResult): void {
  const stub: Partial<WuuDesktopApi> = {
    getBuildInfo: vi.fn().mockResolvedValue(info),
  };
  (globalThis as { wuu?: WuuDesktopApi }).wuu = stub as WuuDesktopApi;
  (window as unknown as GlobalWindow).wuu = stub as WuuDesktopApi;
}

function baseInitialized(overrides: Partial<InitializeResult> = {}): InitializeResult {
  return {
    protocol_version: "wuu-app-server/v0.1",
    provider: "fake",
    model: "fake-model",
    workspace_root: "/tmp/project",
    ...overrides,
  };
}

function renderSettings(props: {
  initialized: InitializeResult | undefined;
  usage?: SettingsUsageData;
  usageRange?: SettingsUsageRange;
}): { about: Element | null; text: () => string; rootText: () => string } {
  const usageRange: SettingsUsageRange = props.usageRange ?? "all";
  const setUsageRange = vi.fn();
  act(() => {
    root = createRoot(container);
    root!.render(
      <SettingsView
        initialized={props.initialized}
        running={false}
        usage={props.usage}
        usageRange={usageRange}
        setUsageRange={setUsageRange}
        showDebugControlsSetting={false}
        debugControlsEnabled={false}
        sidebarWidth={320}
        sidebarMinWidth={240}
        sidebarMaxWidth={480}
        resizingSidebar={false}
        onBack={() => {}}
        onSave={async () => {}}
        onDebugControlsChange={() => {}}
        onSidebarResizeStart={noopResizeStart}
        onSidebarSeparatorKey={noopResizeKey}
        onSidebarSeparatorDoubleClick={() => {}}
      />,
    );
  });
  const about = container.querySelector("[data-testid=\"settings-about\"]");
  return {
    about,
    text: () => about?.textContent ?? "",
    rootText: () => container.textContent ?? "",
  };
}

describe("SettingsView About section", () => {
  it("renders core and protocol version once initialized", async () => {
    installBuildInfoStub({
      core: {
        version: "v0.2.3",
        commit: "abc1234",
        date: "2026-06-04T07:00:00Z",
        dirty: false,
      },
      desktop: { version: "0.0.0-test", date: "1970-01-01T00:00:00Z" },
    });
    const { about, text } = renderSettings({
      initialized: baseInitialized({
        core: {
          version: "v0.2.3",
          commit: "abc1234",
          date: "2026-06-04T07:00:00Z",
          dirty: false,
        },
      }),
    });
    expect(about).not.toBeNull();
    // Wait for the desktop build info effect to resolve.
    await act(async () => {
      await Promise.resolve();
    });
    expect(text()).toContain("v0.2.3");
    expect(text()).toContain("abc1234");
    expect(text()).toContain("wuu-app-server/v0.1");
  });

  it("marks dirty core builds so the user can tell work-in-progress", async () => {
    installBuildInfoStub({
      core: undefined,
      desktop: { version: "0.0.0-test", date: "1970-01-01T00:00:00Z" },
    });
    const { text } = renderSettings({
      initialized: baseInitialized({
        core: {
          version: "v0.1.0-dev",
          commit: "fb3e89e",
          date: "2026-06-04T07:00:00Z",
          dirty: true,
        },
      }),
    });
    await act(async () => {
      await Promise.resolve();
    });
    expect(text()).toContain("v0.1.0-dev");
    expect(text()).toContain("fb3e89e-dirty");
  });

  it("falls back to a placeholder when the app-server has not reported core info", async () => {
    installBuildInfoStub({
      core: undefined,
      desktop: { version: "0.0.0-test", date: "1970-01-01T00:00:00Z" },
    });
    const { text } = renderSettings({ initialized: baseInitialized() });
    await act(async () => {
      await Promise.resolve();
    });
    expect(text()).toContain("未连接");
  });

  it("renders extension trust summary", async () => {
    installBuildInfoStub({
      core: undefined,
      desktop: { version: "0.0.0-test", date: "1970-01-01T00:00:00Z" },
    });
    const { text } = renderSettings({
      initialized: baseInitialized({
        extension_trust: {
          main_session: {
            mcp: { allowed: true, active: false },
            hooks: { allowed: true, active: true },
            plugins: { allowed: true, active: true, count: 1 },
            skills: { allowed: true, active: true, count: 2, known_tools: 1, visible_tools: 1 },
            workflows: { allowed: true, active: false },
            external_tools: { allowed: true, active: false },
          },
          reviewer_session: {
            mcp: { allowed: false, active: false },
            hooks: { allowed: false, active: false },
            plugins: { allowed: false, active: false },
            skills: { allowed: false, active: false },
            workflows: { allowed: false, active: false },
            external_tools: { allowed: false, active: false },
          },
        },
      }),
    });
    await act(async () => {
      await Promise.resolve();
    });
    expect(text()).toContain("扩展边界");
    expect(text()).toContain("Plugins 1");
    expect(text()).toContain("Skills 2");
    expect(text()).toContain("Reviewer：关闭扩展");
  });

  it("switches to the usage page from the settings sidebar", async () => {
    installBuildInfoStub({
      core: undefined,
      desktop: { version: "0.0.0-test", date: "1970-01-01T00:00:00Z" },
    });
    const usage: SettingsUsageData = {
      inputTokens: 1000,
      outputTokens: 200,
      cacheCreationTokens: 20,
      cacheReadTokens: 50,
      turns: 1,
      agents: 0,
      buckets: [
        {
          id: "openai\nfake-model",
          provider: "OpenAI API",
          model: "fake-model",
          inputTokens: 1000,
          outputTokens: 200,
          cacheCreationTokens: 20,
          cacheReadTokens: 50,
          turns: 1,
          agents: 0,
        },
      ],
      days: [
        {
          date: "2026-06-18",
          inputTokens: 1000,
          outputTokens: 200,
          cacheCreationTokens: 20,
          cacheReadTokens: 50,
          turns: 1,
          agents: 0,
        },
      ],
      entries: [
        {
          id: "turn:turn-1",
          kind: "turn",
          title: "测试会话",
          provider: "OpenAI API",
          model: "fake-model",
          date: "2026-06-18",
          inputTokens: 1000,
          outputTokens: 200,
          cacheCreationTokens: 20,
          cacheReadTokens: 50,
        },
      ],
    };
    const { rootText } = renderSettings({
      initialized: baseInitialized(),
      usage,
    });
    const usageButton = Array.from(container.querySelectorAll("button")).find((button) =>
      button.textContent?.includes("用量"),
    );
    expect(usageButton).not.toBeUndefined();
    await act(async () => {
      usageButton?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    expect(container.querySelector("[data-testid=\"settings-usage\"]")).not.toBeNull();
    expect(rootText()).toContain("1,250");
    expect(rootText()).toContain("模型使用");
    expect(rootText()).toContain("缓存命中率");
    expect(rootText()).toContain("5%");
    expect(rootText()).toContain("OpenAI API");
    expect(rootText()).toContain("测试会话");
    expect(container.querySelector(".settings-cache-heatmap")).not.toBeNull();
  });
});
