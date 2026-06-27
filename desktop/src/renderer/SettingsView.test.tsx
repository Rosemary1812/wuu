import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { SettingsView, type SettingsPage } from "./SettingsView";
import type {
  BuildInfoResult,
  InitializeResult,
  RuntimeAdvancedSettingsUpdate,
  RuntimeConnectionUpdate,
  SettingsUsageRange,
  SettingsUsageResponse,
  WuuDesktopApi
} from "../shared/protocol";

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
    listMCPServers: vi.fn().mockResolvedValue({ servers: [] }),
    connectMCPServer: vi.fn(),
    disconnectMCPServer: vi.fn(),
    refreshMCPServer: vi.fn(),
  };
  (globalThis as { wuu?: WuuDesktopApi }).wuu = stub as WuuDesktopApi;
  (window as unknown as GlobalWindow).wuu = stub as WuuDesktopApi;
}

function setInputValue(input: HTMLInputElement, value: string): void {
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
  setter?.call(input, value);
  input.dispatchEvent(new Event("input", { bubbles: true }));
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
  usage?: SettingsUsageResponse;
  usageRange?: SettingsUsageRange;
  initialPage?: SettingsPage;
  onSave?: (provider: string, model: string, effort?: string, connection?: RuntimeConnectionUpdate, variant?: string) => Promise<void>;
  onAdvancedSave?: (settings: RuntimeAdvancedSettingsUpdate) => Promise<void>;
}): { about: Element | null; text: () => string; rootText: () => string } {
  const usageRange: SettingsUsageRange = props.usageRange ?? "all";
  const setUsageRange = vi.fn();
  act(() => {
    root = createRoot(container);
    root!.render(
      <SettingsView
        initialized={props.initialized}
        initialPage={props.initialPage ?? "general"}
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
        onSave={props.onSave ?? (async () => {})}
        onAdvancedSave={props.onAdvancedSave ?? (async () => {})}
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

describe("SettingsView provider configuration", () => {
  it("shows BYOK provider controls as a first-class settings page", async () => {
    installBuildInfoStub({
      core: undefined,
      desktop: { version: "0.0.0-test", date: "1970-01-01T00:00:00Z" },
    });
    const { rootText } = renderSettings({
      initialPage: "providers",
      initialized: baseInitialized({
        provider: "openrouter",
        model: "openai/gpt-5.5",
        providers: [
          {
            name: "openrouter",
            type: "openai-compatible",
            model: "openai/gpt-5.5",
            base_url: "https://openrouter.ai/api/v1",
            api_key_configured: true,
          },
        ],
      }),
    });
    await act(async () => {
      await Promise.resolve();
    });
    expect(container.querySelector("[data-testid=\"settings-providers\"]")).not.toBeNull();
    expect(rootText()).toContain("模型服务");
    expect(rootText()).toContain("openrouter");
    expect(rootText()).toContain("Base URL");
    expect(rootText()).toContain("API key 已配置");
    expect(rootText()).toContain("新增 OpenAI-compatible");
  });

  it("submits a new OpenAI-compatible provider with editable connection fields", async () => {
    installBuildInfoStub({
      core: undefined,
      desktop: { version: "0.0.0-test", date: "1970-01-01T00:00:00Z" },
    });
    const onSave = vi.fn().mockResolvedValue(undefined);
    renderSettings({
      initialPage: "providers",
      initialized: baseInitialized({
        provider: "openai",
        model: "gpt-5.5",
        providers: [
          {
            name: "openai",
            type: "openai",
            model: "gpt-5.5",
            base_url: "https://api.openai.com/v1",
            api_key_configured: true,
          },
        ],
      }),
      onSave,
    });
    const addButton = Array.from(container.querySelectorAll("button")).find((button) =>
      button.textContent?.includes("新增 OpenAI-compatible"),
    );
    expect(addButton).not.toBeUndefined();
    await act(async () => {
      addButton?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });

    const inputs = Array.from(container.querySelectorAll("input"));
    expect(inputs.length).toBeGreaterThanOrEqual(4);
    const [providerInput, modelInput, baseURLInput, apiKeyInput] = inputs;
    await act(async () => {
      setInputValue(providerInput, "openrouter");
      setInputValue(modelInput, "openai/gpt-5.5");
      setInputValue(baseURLInput, "https://openrouter.ai/api/v1");
      setInputValue(apiKeyInput, "sk-test");
    });

    const submitButton = Array.from(container.querySelectorAll("button")).find((button) =>
      button.textContent?.includes("添加服务"),
    ) as HTMLButtonElement | undefined;
    expect(submitButton?.disabled).toBe(false);
    await act(async () => {
      submitButton?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
      await Promise.resolve();
    });

    expect(onSave).toHaveBeenCalledWith(
      "openrouter",
      "openai/gpt-5.5",
      undefined,
      {
        base_url: "https://openrouter.ai/api/v1",
        api_key: "sk-test",
        create_provider: true,
      },
      "",
    );
  });
});

describe("SettingsView advanced settings", () => {
  it("renders and saves BYOK context and compaction controls", async () => {
    installBuildInfoStub({
      core: undefined,
      desktop: { version: "0.0.0-test", date: "1970-01-01T00:00:00Z" },
    });
    const onAdvancedSave = vi.fn().mockResolvedValue(undefined);
    const { rootText } = renderSettings({
      initialPage: "advanced",
      initialized: baseInitialized({
        provider: "openrouter",
        model: "openai/gpt-5.5",
        advanced_settings: {
          max_steps: 0,
          max_context_tokens: 0,
          temperature: 0.2,
          disable_auto_compact: false,
          compact_keep_recent_tokens: 20000,
          context_window_tokens: 400000,
          context_window_source: "built_in_registry",
          output_reserve_tokens: 128000,
          compact_threshold_tokens: 272000,
        },
      }),
      onAdvancedSave,
    });
    await act(async () => {
      await Promise.resolve();
    });
    expect(container.querySelector("[data-testid=\"settings-advanced\"]")).not.toBeNull();
    expect(rootText()).toContain("压缩触发阈值");
    expect(rootText()).toContain("保留最近上下文");
    expect(rootText()).toContain("当前服务上下文窗口");
    expect(rootText()).toContain("400,000");

    const inputs = Array.from(container.querySelectorAll("input"));
    expect(inputs.length).toBeGreaterThanOrEqual(6);
    const [compactThreshold, compactKeepRecent, providerContextWindow, maxContextTokens, maxSteps, temperature] = inputs;
    await act(async () => {
      setInputValue(compactThreshold, "50");
      setInputValue(compactKeepRecent, "20000");
      setInputValue(providerContextWindow, "512000");
      setInputValue(maxContextTokens, "256000");
      setInputValue(maxSteps, "12");
      setInputValue(temperature, "0.4");
    });

    const submitButton = Array.from(container.querySelectorAll("button")).find((button) =>
      button.textContent?.includes("保存高级设置"),
    ) as HTMLButtonElement | undefined;
    expect(submitButton?.disabled).toBe(false);
    await act(async () => {
      submitButton?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
      await Promise.resolve();
    });

    expect(onAdvancedSave).toHaveBeenCalledWith({
      disable_auto_compact: false,
      compact_threshold_pct: 0.5,
      compact_keep_recent_tokens: 20000,
      provider_context_window: 512000,
      max_context_tokens: 256000,
      max_steps: 12,
      temperature: 0.4,
    });
  });
});

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

  it("renders MCP server status", async () => {
    installBuildInfoStub({
      core: undefined,
      desktop: { version: "0.0.0-test", date: "1970-01-01T00:00:00Z" },
    });
    (window as unknown as GlobalWindow).wuu.listMCPServers = vi.fn().mockResolvedValue({
      servers: [
        {
          name: "docs",
          state: "connected",
          auth_status: "bearer_token",
          connected: true,
          tool_count: 3,
        },
      ],
    });
    const { rootText } = renderSettings({ initialized: baseInitialized() });
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(rootText()).toContain("MCP");
    expect(rootText()).toContain("docs");
    expect(rootText()).toContain("已连接");
    expect(rootText()).toContain("3 个工具");
    expect(rootText()).toContain("Header 认证");
  });

  it("switches to the usage page from the settings sidebar", async () => {
    installBuildInfoStub({
      core: undefined,
      desktop: { version: "0.0.0-test", date: "1970-01-01T00:00:00Z" },
    });
    const usage: SettingsUsageResponse = {
      range: "all",
      total_sessions: 1,
      generated_at: "2026-06-18T12:00:00Z",
      metrics: {
        prompt_tokens: 1050,
        context_tokens: 1250,
        input_tokens: 1000,
        output_tokens: 200,
        cache_read_tokens: 50,
        cache_creation_tokens: 20,
        cache_hit_rate: 50 / 1050,
        turns: 1,
        agents: 0,
        date_range: ["2026-06-18", "2026-06-18"],
        active_days: 1,
      },
      model_breakdowns: [
        {
          provider: "OpenAI API",
          model: "fake-model",
          input_tokens: 1000,
          output_tokens: 200,
          cache_creation_tokens: 20,
          cache_read_tokens: 50,
          sessions: 1,
        },
      ],
      days: [
        {
          date: "2026-06-18",
          input_tokens: 1000,
          output_tokens: 200,
          cache_creation_tokens: 20,
          cache_read_tokens: 50,
          cache_hit_rate: 50 / 1050,
          turns: 1,
          agents: 0,
        },
      ],
      entries: [
        {
          id: "turn:turn-1",
          source: "turn",
          title: "测试会话",
          provider: "OpenAI API",
          model: "fake-model",
          at: "2026-06-18T12:00:00Z",
          input_tokens: 1000,
          output_tokens: 200,
          cache_creation_tokens: 20,
          cache_read_tokens: 50,
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
    expect(rootText()).not.toContain("最近记录");
    expect(container.querySelector(".settings-cache-heatmap")).not.toBeNull();
  });
});
