import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { SettingsView, type ArchivedSessionView, type SettingsPage } from "./SettingsView";
import type {
  BuildInfoResult,
  CodexPetsSnapshot,
  InitializeResult,
  RuntimeAdvancedSettingsUpdate,
  CodexPetSettingsUpdate,
  RuntimeConnectionUpdate,
  RuntimeGeneralSettingsUpdate,
  SettingsUsageResponse,
  WuuDesktopApi
} from "../shared/protocol";
import { I18nProvider } from "./i18n";

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
  Reflect.deleteProperty(document, "elementFromPoint");
  vi.restoreAllMocks();
  vi.useRealTimers();
});

function installBuildInfoStub(info: BuildInfoResult): void {
  const stub: Partial<WuuDesktopApi> = {
    getBuildInfo: vi.fn().mockResolvedValue(info),
    listMCPServers: vi.fn().mockResolvedValue({ servers: [] }),
    connectMCPServer: vi.fn(),
    disconnectMCPServer: vi.fn(),
    refreshMCPServer: vi.fn(),
    startMCPAuth: vi.fn(),
    getMCPAuthStatus: vi.fn(),
    finishMCPAuth: vi.fn(),
    removeMCPAuth: vi.fn(),
    openExternal: vi.fn(),
    listCodexPets: vi.fn().mockResolvedValue(emptyCodexPetsSnapshot()),
    updateCodexPetSettings: vi.fn().mockResolvedValue(emptyCodexPetsSnapshot()),
  };
  (globalThis as { wuu?: WuuDesktopApi }).wuu = stub as WuuDesktopApi;
  (window as unknown as GlobalWindow).wuu = stub as WuuDesktopApi;
}

function setInputValue(input: HTMLInputElement | HTMLTextAreaElement, value: string): void {
  const prototype = input instanceof HTMLTextAreaElement
    ? HTMLTextAreaElement.prototype
    : HTMLInputElement.prototype;
  const setter = Object.getOwnPropertyDescriptor(prototype, "value")?.set;
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

function emptyCodexPetsSnapshot(overrides: Partial<CodexPetsSnapshot> = {}): CodexPetsSnapshot {
  return {
    home: "/Users/test/.wuu/pets",
    enabled: false,
    selected_id: "",
    pets: [],
    errors: [],
    ...overrides,
  };
}

function renderSettings(props: {
  initialized: InitializeResult | undefined;
  usage?: SettingsUsageResponse;
  initialPage?: SettingsPage;
  runningProviderNames?: string[];
  codexPets?: CodexPetsSnapshot;
  codexPetsLoading?: boolean;
  codexPetsError?: string;
  onCodexPetsUpdate?: (settings: CodexPetSettingsUpdate) => Promise<CodexPetsSnapshot>;
  onSave?: (provider: string, model: string, effort?: string, connection?: RuntimeConnectionUpdate, variant?: string) => Promise<void>;
  onRemoveProvider?: (provider: string) => Promise<void>;
  onAdvancedSave?: (settings: RuntimeAdvancedSettingsUpdate) => Promise<void>;
  onGeneralSave?: (settings: RuntimeGeneralSettingsUpdate) => Promise<void>;
  onToggleSidebar?: () => void;
  sidebarCollapsed?: boolean;
  // ArchivedSessionView 是结构子集（id/title?/updated_at），这里
  // 直接传对象字面量，避免引入 ThreadSummary（它要求 turns/turn_count
  // 等计算字段，测试场景下冗余）。
  archivedThreads?: readonly ArchivedSessionView[];
  onUnarchiveThread?: (thread: ArchivedSessionView) => void;
  locale?: "zh-CN" | "en-US";
}): { about: Element | null; text: () => string; rootText: () => string } {
  if (props.locale) {
    window.wuu.initialLanguagePreference = props.locale;
    window.wuu.initialSystemLocale = props.locale;
  }
  const view = (
    <SettingsView
        initialized={props.initialized}
        initialPage={props.initialPage ?? "general"}
        running={false}
        usage={props.usage}
        runningProviderNames={props.runningProviderNames}
        codexPets={props.codexPets ?? emptyCodexPetsSnapshot()}
        codexPetsLoading={props.codexPetsLoading ?? false}
        codexPetsError={props.codexPetsError ?? ""}
        onCodexPetsRefresh={async () => props.codexPets ?? emptyCodexPetsSnapshot()}
        onCodexPetsUpdate={props.onCodexPetsUpdate ?? (async () => props.codexPets ?? emptyCodexPetsSnapshot())}
        showDebugControlsSetting={false}
        debugControlsEnabled={false}
        sidebarWidth={320}
        sidebarMinWidth={240}
        sidebarMaxWidth={480}
        resizingSidebar={false}
        // Mirror the App-level collapse/hover wiring so existing render
        // assertions still produce a sensible non-collapsed shell by default.
        sidebarCollapsed={props.sidebarCollapsed ?? false}
        sidebarAnimating={false}
        onToggleSidebar={props.onToggleSidebar ?? (() => {})}
        sidebarMotionMs={240}
        onBack={() => {}}
        onSave={props.onSave ?? (async () => {})}
        onRemoveProvider={props.onRemoveProvider ?? (async () => {})}
        onAdvancedSave={props.onAdvancedSave ?? (async () => {})}
        onGeneralSave={props.onGeneralSave ?? (async () => {})}
        onDebugControlsChange={() => {}}
        onSidebarResizeStart={noopResizeStart}
        onSidebarSeparatorKey={noopResizeKey}
        archivedThreads={props.archivedThreads ?? []}
        onUnarchiveThread={props.onUnarchiveThread ?? (() => {})}
    />
  );
  act(() => {
    root = createRoot(container);
    root!.render(props.locale ? <I18nProvider>{view}</I18nProvider> : view);
  });
  const about = container.querySelector("[data-testid=\"settings-about\"]");
  return {
    about,
    text: () => about?.textContent ?? "",
    rootText: () => container.textContent ?? "",
  };
}

describe("SettingsView shell", () => {
  it("hides remote control by default and redirects a remote initial page", () => {
    installBuildInfoStub({
      core: undefined,
      desktop: { version: "0.0.0-test", date: "1970-01-01T00:00:00Z" },
    });
    renderSettings({ initialized: baseInitialized(), initialPage: "remote" });

    const remoteButton = Array.from(
      container.querySelectorAll<HTMLButtonElement>(".settings-nav-item"),
    ).find((button) => button.textContent?.trim() === "远程");

    expect(remoteButton).toBeUndefined();
    expect(container.querySelector('[data-testid="settings-remote-page"]')).toBeNull();
    expect(container.querySelector(".settings-page-title")?.textContent).toBe("模型服务");
  });

  it("renders the brand placeholder at the top of the settings sidebar", () => {
    installBuildInfoStub({
      core: undefined,
      desktop: { version: "0.0.0-test", date: "1970-01-01T00:00:00Z" },
    });
    renderSettings({ initialized: baseInitialized() });

    const sidebar = container.querySelector(".settings-sidebar");
    const trafficSpacer = sidebar?.querySelector(".traffic-spacer");
    const brand = sidebar?.querySelector(".sidebar-brand");
    const backButton = sidebar?.querySelector(".settings-back-button");

    // 品牌占位必须排在 traffic-spacer 之后、返回应用 按钮之前，
    // 跟主侧栏的相对位置一致；等真正的 logo / lockup 落地后两个测试一起替换。
    expect(brand).not.toBeNull();
    expect(brand?.querySelector(".sidebar-brand-wordmark")?.textContent).toBe("wuu");
    expect(brand?.previousElementSibling).toBe(trafficSpacer);
    expect(brand?.nextElementSibling).toBe(backButton);
    expect(brand?.textContent?.trim()).toBe("wuu");
  });

  it("uses the same transparent-until-hover sidebar toggle as the conversation titlebar", () => {
    installBuildInfoStub({
      core: undefined,
      desktop: { version: "0.0.0-test", date: "1970-01-01T00:00:00Z" },
    });
    const onToggleSidebar = vi.fn();
    renderSettings({ initialized: baseInitialized(), onToggleSidebar });

    const toggle = container.querySelector<HTMLButtonElement>(
      ".settings-titlebar .sidebar-toggle-button",
    );
    expect(toggle).not.toBeNull();
    expect(toggle?.classList.contains("icon-button")).toBe(true);
    expect(toggle?.classList.contains("side-panel-toggle-button")).toBe(true);
    expect(toggle?.getAttribute("aria-pressed")).toBe("true");

    act(() => {
      toggle?.click();
    });
    expect(onToggleSidebar).toHaveBeenCalledTimes(1);
  });

  it("keeps the hover drawer open when the pointer moves from it to the settings toggle", () => {
    vi.useFakeTimers();
    installBuildInfoStub({
      core: undefined,
      desktop: { version: "0.0.0-test", date: "1970-01-01T00:00:00Z" },
    });
    renderSettings({ initialized: baseInitialized(), sidebarCollapsed: true });

    const shell = container.querySelector<HTMLElement>(".settings-shell");
    const sidebar = container.querySelector<HTMLElement>(".settings-sidebar");
    const toggle = container.querySelector<HTMLElement>(
      ".settings-titlebar .sidebar-toggle-button",
    );
    expect(sidebar).not.toBeNull();
    expect(toggle).not.toBeNull();

    Object.defineProperty(document, "elementFromPoint", {
      configurable: true,
      value: vi.fn(() => toggle),
    });
    act(() => {
      toggle?.dispatchEvent(
        new MouseEvent("pointerover", {
          bubbles: true,
          clientX: 80,
          clientY: 24,
        }),
      );
      vi.advanceTimersByTime(240);
    });
    expect(shell?.classList.contains("sidebar-drawer-open")).toBe(true);

    act(() => {
      sidebar?.dispatchEvent(
        new MouseEvent("pointerout", {
          bubbles: true,
          clientX: 80,
          clientY: 24,
          relatedTarget: toggle,
        }),
      );
      vi.advanceTimersByTime(1);
    });

    expect(shell?.classList.contains("sidebar-drawer-open")).toBe(true);
    expect(shell?.classList.contains("sidebar-drawer-closing")).toBe(false);
  });

  it("starts each settings page at the top and replaces the content surface", () => {
    installBuildInfoStub({
      core: undefined,
      desktop: { version: "0.0.0-test", date: "1970-01-01T00:00:00Z" },
    });
    renderSettings({ initialized: baseInitialized(), initialPage: "providers" });

    const scroll = container.querySelector<HTMLElement>(".settings-scroll")!;
    const providersPage = container.querySelector<HTMLElement>(".settings-page")!;
    scroll.scrollTop = 420;
    const advancedButton = Array.from(
      container.querySelectorAll<HTMLButtonElement>(".settings-nav-item"),
    ).find((button) => button.textContent?.includes("高级"));

    act(() => {
      advancedButton?.click();
    });

    expect(scroll.scrollTop).toBe(0);
    expect(container.querySelector(".settings-page")).not.toBe(providersPage);
    expect(container.querySelector(".settings-page-title")?.textContent).toBe("高级");
  });
});

describe("SettingsView provider configuration", () => {
  it("renders provider configuration in English", () => {
    installBuildInfoStub({
      core: undefined,
      desktop: { version: "0.0.0-test", date: "1970-01-01T00:00:00Z" },
    });
    const { rootText } = renderSettings({
      locale: "en-US",
      initialPage: "providers",
      initialized: baseInitialized({
        provider: "local",
        model: "",
        providers: [{
          name: "local",
          type: "openai-compatible",
          model: "",
          base_url: "http://127.0.0.1:11434/v1",
          api_key_configured: false,
        }],
      }),
    });

    expect(rootText()).toContain("Model providers");
    expect(rootText()).toContain("Local model provider");
    expect(rootText()).toContain("No model selected");
    expect(rootText()).toContain("Missing API key");
    expect(rootText()).toContain("Reasoning effort");
    expect(rootText()).not.toContain("模型服务");
  });

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
    expect(rootText()).toContain("新增服务");
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
      button.textContent?.includes("新增服务"),
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
        type: "openai-compatible",
        create_provider: true,
      },
      "",
    );
  });

  it("shows an alert instead of removing a provider used by a running turn", async () => {
    installBuildInfoStub({
      core: undefined,
      desktop: { version: "0.0.0-test", date: "1970-01-01T00:00:00Z" },
    });
    const alert = vi.spyOn(window, "alert").mockImplementation(() => {});
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(true);
    const onRemoveProvider = vi.fn().mockResolvedValue(undefined);
    renderSettings({
      initialPage: "providers",
      runningProviderNames: ["drop"],
      initialized: baseInitialized({
        provider: "keep",
        model: "keep-model",
        providers: [
          {
            name: "keep",
            type: "openai-compatible",
            model: "keep-model",
            base_url: "https://keep.example.test/v1",
            api_key_configured: true,
          },
          {
            name: "drop",
            type: "openai-compatible",
            model: "drop-model",
            base_url: "https://drop.example.test/v1",
            api_key_configured: true,
          },
        ],
      }),
      onRemoveProvider,
    });

    const removeButtons = Array.from(container.querySelectorAll<HTMLButtonElement>(".settings-provider-remove"));
    expect(removeButtons).toHaveLength(2);
    await act(async () => {
      removeButtons[1].dispatchEvent(new MouseEvent("click", { bubbles: true }));
      await Promise.resolve();
    });

    expect(alert).toHaveBeenCalledWith("这个模型服务正在被运行中的会话使用，等当前回复结束后再删除。");
    expect(confirm).not.toHaveBeenCalled();
    expect(onRemoveProvider).not.toHaveBeenCalled();
  });

  it("submits a new Anthropic-compatible provider with bearer token auth", async () => {
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
      button.textContent?.includes("新增服务"),
    );
    await act(async () => {
      addButton?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });

    const providerTypeTrigger = container.querySelector("[data-testid=\"settings-provider-type-select\"]") as HTMLButtonElement;
    await act(async () => {
      providerTypeTrigger.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    const anthropicOption = Array.from(
      document.querySelectorAll<HTMLButtonElement>(".select-menu-panel .select-menu-item"),
    ).find((item) => item.getAttribute("data-value") === "anthropic");
    await act(async () => {
      anthropicOption?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });

    const inputs = Array.from(container.querySelectorAll("input"));
    const [providerInput, modelInput, baseURLInput, authTokenInput] = inputs;
    await act(async () => {
      setInputValue(providerInput, "anthropic-gateway");
      setInputValue(modelInput, "claude-sonnet-4-6[1M]");
      setInputValue(baseURLInput, "https://tokenhub.zhuanspirit.com/anthropic/");
      setInputValue(authTokenInput, "sk-token");
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
      "anthropic-gateway",
      "claude-sonnet-4-6[1M]",
      undefined,
      {
        base_url: "https://tokenhub.zhuanspirit.com/anthropic/",
        auth_token: "sk-token",
        type: "anthropic",
        create_provider: true,
      },
      "",
    );
  });
});

describe("SettingsView advanced settings", () => {
  it("renders compaction controls and saves each field on commit", async () => {
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
          temperature: 0,
          disable_auto_compact: false,
          compact_keep_recent_tokens: 20000,
          context_window_tokens: 400000,
          context_window_source: "provider_input_limit",
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
    expect(rootText()).toContain("当前服务上下文上限");
    expect(rootText()).toContain("来自当前通道输入上限");
    expect(rootText()).toContain("400,000");
    // Instant-apply: no draft form, no Save button.
    expect(
      Array.from(container.querySelectorAll("button")).some((button) =>
        button.textContent?.includes("保存"),
      ),
    ).toBe(false);

    const inputs = Array.from(container.querySelectorAll("input"));
    expect(inputs.length).toBeGreaterThanOrEqual(6);
    const [compactThreshold, compactKeepRecent, providerContextWindow, maxContextTokens, maxSteps, temperature] = inputs;
    expect((temperature as HTMLInputElement).value).toBe("");
    // The placeholder carries the automatic-value meaning instead of the row description.
    expect((temperature as HTMLInputElement).placeholder).toBe("自动");

    const commit = async (input: Element) => {
      await act(async () => {
        input.dispatchEvent(new FocusEvent("focusout", { bubbles: true }));
        await Promise.resolve();
      });
    };

    await act(async () => {
      setInputValue(compactThreshold, "50");
      compactThreshold.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true }));
      await Promise.resolve();
    });
    expect(onAdvancedSave).toHaveBeenLastCalledWith({ compact_threshold_pct: 0.5 });

    await act(async () => {
      setInputValue(compactKeepRecent, "30000");
    });
    await commit(compactKeepRecent);
    expect(onAdvancedSave).toHaveBeenLastCalledWith({ compact_keep_recent_tokens: 30000 });

    await act(async () => {
      setInputValue(providerContextWindow, "512000");
    });
    await commit(providerContextWindow);
    expect(onAdvancedSave).toHaveBeenLastCalledWith({ provider_context_window: 512000 });

    await act(async () => {
      setInputValue(maxContextTokens, "256000");
    });
    await commit(maxContextTokens);
    expect(onAdvancedSave).toHaveBeenLastCalledWith({ max_context_tokens: 256000 });

    await act(async () => {
      setInputValue(maxSteps, "12");
    });
    await commit(maxSteps);
    expect(onAdvancedSave).toHaveBeenLastCalledWith({ max_steps: 12 });

    await act(async () => {
      setInputValue(temperature, "0.4");
    });
    await commit(temperature);
    expect(onAdvancedSave).toHaveBeenLastCalledWith({ temperature: 0.4 });

    // Blurring an untouched field does not round-trip the same value.
    const callsBefore = onAdvancedSave.mock.calls.length;
    await commit(temperature);
    expect(onAdvancedSave.mock.calls.length).toBe(callsBefore);

    const switchButton = container.querySelector(".settings-switch") as HTMLButtonElement;
    await act(async () => {
      switchButton.dispatchEvent(new MouseEvent("click", { bubbles: true }));
      await Promise.resolve();
    });
    expect(onAdvancedSave).toHaveBeenLastCalledWith({ disable_auto_compact: true });
  });

  it("saves cleared temperature as Auto on commit", async () => {
    installBuildInfoStub({
      core: undefined,
      desktop: { version: "0.0.0-test", date: "1970-01-01T00:00:00Z" },
    });
    const onAdvancedSave = vi.fn().mockResolvedValue(undefined);
    renderSettings({
      initialPage: "advanced",
      initialized: baseInitialized({
        provider: "openrouter",
        model: "openai/gpt-5.5",
        advanced_settings: {
          max_steps: 0,
          max_context_tokens: 0,
          temperature: 0,
          disable_auto_compact: false,
        },
      }),
      onAdvancedSave,
    });
    await act(async () => {
      await Promise.resolve();
    });

    const temperature = Array.from(container.querySelectorAll("input")).at(-1) as HTMLInputElement;
    await act(async () => {
      setInputValue(temperature, "0.4");
      temperature.dispatchEvent(new FocusEvent("focusout", { bubbles: true }));
      await Promise.resolve();
    });
    expect(onAdvancedSave).toHaveBeenLastCalledWith({ temperature: 0.4 });

    await act(async () => {
      setInputValue(temperature, "");
      temperature.dispatchEvent(new FocusEvent("focusout", { bubbles: true }));
      await Promise.resolve();
    });
    expect(onAdvancedSave).toHaveBeenLastCalledWith({ temperature: 0 });
  });
});

describe("SettingsView general settings", () => {
  it("loads and toggles WUU Agent commit attribution", async () => {
    installBuildInfoStub({
      core: undefined,
      desktop: { version: "0.0.0-test", date: "1970-01-01T00:00:00Z" },
    });
    const onGeneralSave = vi.fn().mockResolvedValue(undefined);
    renderSettings({
      locale: "en-US",
      initialized: baseInitialized({
        general_settings: {
          append_system_prompt: "",
          git_attribution_enabled: true,
          memory_disabled: false,
          mcp_server_enabled: {},
        },
      }),
      initialPage: "general",
      onGeneralSave,
    });
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });

    const attributionSwitch = container.querySelector<HTMLButtonElement>(
      '[data-testid="settings-git-attribution"]',
    );
    expect(attributionSwitch?.getAttribute("aria-checked")).toBe("true");
    expect(container.textContent).toContain("Agent commit attribution");
    expect(container.textContent).toContain("wuu-agent[bot]");

    await act(async () => {
      attributionSwitch?.click();
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(onGeneralSave).toHaveBeenCalledWith({
      git_attribution_enabled: false,
    });
  });

  it("renders and saves prompt, memory, and MCP toggles", async () => {
    installBuildInfoStub({
      core: undefined,
      desktop: { version: "0.0.0-test", date: "1970-01-01T00:00:00Z" },
    });
    const onGeneralSave = vi.fn().mockResolvedValue(undefined);
    const { rootText } = renderSettings({
      initialPage: "general",
      initialized: baseInitialized({
        general_settings: {
          append_system_prompt: "Keep answers compact.",
          memory_disabled: false,
          mcp_server_enabled: {
            docs: true,
            search: false,
          },
        },
      }),
      onGeneralSave,
    });
    await act(async () => {
      await Promise.resolve();
    });
    expect(container.querySelector("[data-testid=\"settings-general\"]")).not.toBeNull();
    expect(rootText()).toContain("附加系统提示");
    expect(rootText()).toContain("记忆");
    expect(rootText()).toContain("docs");
    expect(rootText()).toContain("search");

    const textarea = container.querySelector("textarea") as HTMLTextAreaElement | null;
    expect(textarea).not.toBeNull();
    await act(async () => {
      setInputValue(textarea!, "默认用中文回答。");
    });
    // The prompt commits on blur, sending only its own field.
    await act(async () => {
      textarea!.dispatchEvent(new FocusEvent("focusout", { bubbles: true }));
      await Promise.resolve();
    });
    expect(onGeneralSave).toHaveBeenCalledWith({
      append_system_prompt: "默认用中文回答。",
    });

    // The memory switch saves immediately, optimistic with rollback on failure.
    const memorySwitch = Array.from(container.querySelectorAll("button[role=\"switch\"]")).find((button) =>
      button.textContent?.includes("关闭记忆"),
    ) as HTMLButtonElement | undefined;
    expect(memorySwitch).not.toBeUndefined();
    await act(async () => {
      memorySwitch?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
      await Promise.resolve();
    });
    expect(onGeneralSave).toHaveBeenCalledWith({ memory_disable: true });

    // MCP toggles now save immediately on switch, sending only the toggle map.
    const docsSwitch = container.querySelector("[data-testid=\"settings-mcp-enabled-docs\"]") as HTMLButtonElement | null;
    expect(docsSwitch).not.toBeNull();
    await act(async () => {
      docsSwitch?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
      await Promise.resolve();
    });
    expect(onGeneralSave).toHaveBeenCalledWith({
      mcp_enabled_toggles: {
        docs: false,
        search: false,
      },
    });

    // Instant-apply: no draft form, no Save button on the page.
    expect(
      Array.from(container.querySelectorAll("button")).some((button) =>
        button.textContent?.includes("保存"),
      ),
    ).toBe(false);
  });

  it("renders Codex Pets controls and saves pet selection", async () => {
    installBuildInfoStub({
      core: undefined,
      desktop: { version: "0.0.0-test", date: "1970-01-01T00:00:00Z" },
    });
    const onCodexPetsUpdate = vi.fn().mockImplementation(async (settings: CodexPetSettingsUpdate) =>
      emptyCodexPetsSnapshot({
        enabled: settings.enabled ?? true,
        selected_id: settings.selected_id ?? "alpha",
        pets: [
          {
            id: "alpha",
            display_name: "Alpha Pet",
            description: "",
            manifest_path: "/Users/test/.wuu/pets/alpha/pet.json",
            spritesheet_path: "/Users/test/.wuu/pets/alpha/spritesheet.webp",
            spritesheet_url: "wuu-file://local/alpha",
          },
          {
            id: "beta",
            display_name: "Beta Pet",
            description: "",
            manifest_path: "/Users/test/.wuu/pets/beta/pet.json",
            spritesheet_path: "/Users/test/.wuu/pets/beta/spritesheet.webp",
            spritesheet_url: "wuu-file://local/beta",
          },
        ],
      }),
    );
    const { rootText } = renderSettings({
      initialPage: "general",
      initialized: baseInitialized(),
      codexPets: emptyCodexPetsSnapshot({
        enabled: true,
        selected_id: "alpha",
        pets: [
          {
            id: "alpha",
            display_name: "Alpha Pet",
            description: "",
            manifest_path: "/Users/test/.wuu/pets/alpha/pet.json",
            spritesheet_path: "/Users/test/.wuu/pets/alpha/spritesheet.webp",
            spritesheet_url: "wuu-file://local/alpha",
          },
          {
            id: "beta",
            display_name: "Beta Pet",
            description: "",
            manifest_path: "/Users/test/.wuu/pets/beta/pet.json",
            spritesheet_path: "/Users/test/.wuu/pets/beta/spritesheet.webp",
            spritesheet_url: "wuu-file://local/beta",
          },
        ],
      }),
      onCodexPetsUpdate,
    });

    expect(rootText()).toContain("Codex Pet");
    expect(rootText()).toContain("Alpha Pet");

    const petSwitch = container.querySelector("[data-testid=\"settings-codex-pet-enabled\"]") as HTMLButtonElement | null;
    expect(petSwitch).not.toBeNull();
    await act(async () => {
      petSwitch?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
      await Promise.resolve();
    });
    expect(onCodexPetsUpdate).toHaveBeenCalledWith({ enabled: false });

    // The pet picker is the shared SelectMenu, not a native select.
    const petSelect = container.querySelector("[data-testid=\"settings-codex-pet-select\"]") as HTMLButtonElement | null;
    expect(petSelect).not.toBeNull();
    expect(petSelect?.tagName).toBe("BUTTON");
    await act(async () => {
      petSelect?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    const betaOption = Array.from(
      document.querySelectorAll<HTMLButtonElement>(".select-menu-panel .select-menu-item"),
    ).find((item) => item.getAttribute("data-value") === "beta");
    expect(betaOption?.textContent).toContain("Beta Pet");
    await act(async () => {
      betaOption?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
      await Promise.resolve();
    });
    expect(onCodexPetsUpdate).toHaveBeenCalledWith({ selected_id: "beta" });
  });
});

describe("SettingsView About section", () => {
  it("includes core version in the copied version info when initialized", async () => {
    installBuildInfoStub({
      core: {
        version: "v0.2.3",
        commit: "abc1234",
        date: "2026-06-04T07:00:00Z",
        dirty: false,
      },
      desktop: { version: "0.0.0-test", date: "2026-06-28T18:44:52Z" },
    });
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText }
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
    await act(async () => {
      await Promise.resolve();
    });
    // The visible About row only shows the desktop version; the core version
    // lives in the clipboard payload instead.
    expect(text()).toContain("v0.0.0-test");
    expect(text()).not.toContain("v0.2.3");
    const button = about?.querySelector("button.settings-button");
    await act(async () => {
      button?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
      await Promise.resolve();
    });
    expect(writeText).toHaveBeenCalledWith(
      "wuu v0.0.0-test · 2026-06-28 18:44:52Z · core v0.2.3"
    );
  });

  it("omits core version from the copy when the app-server has not reported core info", async () => {
    installBuildInfoStub({
      core: undefined,
      desktop: { version: "0.0.0-test", date: "2026-06-28T18:44:52Z" },
    });
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText }
    });
    const { about, text } = renderSettings({ initialized: baseInitialized() });
    await act(async () => {
      await Promise.resolve();
    });
    expect(text()).toContain("关于");
    expect(text()).not.toContain("未连接");
    const button = about?.querySelector("button.settings-button");
    await act(async () => {
      button?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
      await Promise.resolve();
    });
    expect(writeText).toHaveBeenCalledWith("wuu v0.0.0-test · 2026-06-28 18:44:52Z");
  });

  it("renders About section with desktop version and copy action", async () => {
    installBuildInfoStub({
      core: undefined,
      desktop: { version: "0.0.0-test", date: "2026-06-28T18:44:52Z" },
    });
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText }
    });
    const { about, text } = renderSettings({ initialized: baseInitialized() });
    await act(async () => {
      await Promise.resolve();
    });
    expect(text()).toContain("关于");
    expect(text()).toContain("v0.0.0-test");
    expect(text()).not.toContain("更新于");
    // 版本与复制合并为一行；按钮语义在 aria-label 上，行内只显示「复制」。
    expect(about?.querySelector('button[aria-label="复制版本信息"]')).not.toBeNull();
    const button = about?.querySelector("button.settings-button");
    expect(button?.textContent).toBe("复制");
    await act(async () => {
      button?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
      await Promise.resolve();
    });
    expect(writeText).toHaveBeenCalledWith("wuu v0.0.0-test · 2026-06-28 18:44:52Z");
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

  it("opens MCP OAuth and completes the authorization code flow inline", async () => {
    installBuildInfoStub({
      core: undefined,
      desktop: { version: "0.0.0-test", date: "1970-01-01T00:00:00Z" },
    });
    const api = (window as unknown as GlobalWindow).wuu;
    api.listMCPServers = vi.fn().mockResolvedValue({
      servers: [
        {
          name: "docs",
          state: "auth_required",
          auth_status: "not_logged_in",
          connected: false,
          tool_count: 0,
        },
      ],
    });
    api.startMCPAuth = vi.fn().mockResolvedValue({
      authorization_url: "https://auth.example.test/authorize",
      state: "oauth-state",
      scopes: ["tools"],
    });
    api.finishMCPAuth = vi.fn().mockResolvedValue({
      auth: { name: "docs", authenticated: true, scopes: ["tools"] },
      server: {
        name: "docs",
        state: "stopped",
        auth_status: "oauth",
        connected: false,
        tool_count: 0,
      },
    });
    api.removeMCPAuth = vi.fn().mockResolvedValue({
      auth: { name: "docs", authenticated: false },
      server: {
        name: "docs",
        state: "auth_required",
        auth_status: "not_logged_in",
        connected: false,
        tool_count: 0,
      },
    });
    api.openExternal = vi.fn().mockResolvedValue(undefined);

    renderSettings({ initialized: baseInitialized() });
    const rendered = container;
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });

    const login = rendered.querySelector('button[aria-label="登录 docs"]') as HTMLButtonElement | null;
    expect(login).not.toBeNull();
    await act(async () => {
      login?.click();
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(api.startMCPAuth).toHaveBeenCalledWith("docs");
    expect(api.openExternal).toHaveBeenCalledWith("https://auth.example.test/authorize");

    const code = rendered.querySelector('input[aria-label="docs 授权码"]') as HTMLInputElement | null;
    expect(code).not.toBeNull();
    act(() => {
      setInputValue(code!, "authorization-code");
    });
    const finish = rendered.querySelector('button[aria-label="完成 docs OAuth 登录"]') as HTMLButtonElement | null;
    await act(async () => {
      finish?.click();
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(api.finishMCPAuth).toHaveBeenCalledWith("docs", "oauth-state", "authorization-code");

    const remove = rendered.querySelector('button[aria-label="移除 docs OAuth 登录"]') as HTMLButtonElement | null;
    expect(remove).not.toBeNull();
    await act(async () => {
      remove?.click();
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(api.removeMCPAuth).toHaveBeenCalledWith("docs");
  });

  it("switches to the usage page from the settings sidebar", async () => {
    installBuildInfoStub({
      core: undefined,
      desktop: { version: "0.0.0-test", date: "1970-01-01T00:00:00Z" },
    });
    const heatmapDates = Array.from({ length: 4 }, (_, index) => {
      const date = new Date();
      date.setHours(12, 0, 0, 0);
      date.setDate(date.getDate() - (3 - index));
      return [
        date.getFullYear(),
        String(date.getMonth() + 1).padStart(2, "0"),
        String(date.getDate()).padStart(2, "0"),
      ].join("-");
    });
    const usage: SettingsUsageResponse = {
      total_sessions: 1,
      generated_at: "2026-06-18T12:00:00Z",
      metrics: {
        prompt_tokens: 12_345_700,
        context_tokens: 999_999,
        input_tokens: 12_345_678,
        output_tokens: 1_234,
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
      days: heatmapDates.map((date, index) => ({
        date,
        input_tokens: (index + 1) * 100,
        output_tokens: 0,
        cache_creation_tokens: 20,
        cache_read_tokens: 50,
        cache_hit_rate: 0.5,
        turns: 1,
        agents: 0,
      })),
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
    expect(rootText()).toContain("12.3M");
    expect(rootText()).toContain("1M");
    expect(rootText()).toContain("1.2k");
    expect(container.querySelector('[title="12,345,678"]')).not.toBeNull();
    const modelInput = container.querySelector(".settings-usage-number");
    expect(modelInput?.textContent).toBe("1k");
    expect(modelInput?.getAttribute("title")).toBe("1,000");
    expect(rootText()).toContain("模型使用");
    expect(rootText()).toContain("缓存命中率");
    expect(rootText()).toContain("5%");
    expect(rootText()).toContain("OpenAI API");
    expect(rootText()).not.toContain("最近记录");
    const heatmap = container.querySelector(".settings-usage-heatmap");
    expect(heatmap).not.toBeNull();
    expect(heatmap?.getAttribute("aria-label")).toBe("每日用量热力图");
    expect(
      heatmapDates.map((date) =>
        heatmap
          ?.querySelector<HTMLElement>(`[title^="${date}"]`)
          ?.getAttribute("data-level"),
      ),
    ).toEqual(["1", "2", "3", "4"]);
  });
});

describe("SettingsView archive page", () => {
  // SettingsView 在挂载时会同步触发 `window.wuu.getBuildInfo()` / `listMCPServers()`
  // 这两个 useEffect，archive 页的测试也得装上 stub，不然 effect 一进来就抛。
  beforeEach(() => {
    installBuildInfoStub({
      core: undefined,
      desktop: { version: "0.0.0-test", date: "1970-01-01T00:00:00Z" },
    });
  });

  // ArchivedSessionView 的结构子集：渲染层读取标题、时间和归档时所属项目。
  // 这里直接返回对象字面量，结构上兼容 SettingsView 里的 ArchivedSessionView，
  // 避免引入 ThreadSummary（它要求 turns/turn_count 等计算字段，测试场景下冗余）。
  function archivedThread(
    id: string,
    overrides: Partial<ArchivedSessionView> = {},
  ): ArchivedSessionView {
    return {
      id,
      title: `已归档 ${id}`,
      updated_at: "2026-06-18T12:00:00Z",
      ...overrides,
    };
  }

  it("renders the empty-state card when no threads are archived", () => {
    renderSettings({
      initialized: baseInitialized(),
      initialPage: "archive",
      archivedThreads: [],
    });

    expect(container.textContent).toContain("暂无已归档的会话");
    expect(container.querySelector(".settings-archive-empty")).not.toBeNull();
    expect(container.querySelector(".settings-archive-list")).toBeNull();
  });

  it("lists archived threads and orders them by updated_at descending", () => {
    renderSettings({
      initialized: baseInitialized(),
      initialPage: "archive",
      archivedThreads: [
        archivedThread("older", {
          title: "旧会话",
          updated_at: "2026-06-10T00:00:00Z",
        }),
        archivedThread("newer", {
          title: "新会话",
          updated_at: "2026-06-20T00:00:00Z",
        }),
      ],
    });

    const titles = Array.from(
      container.querySelectorAll<HTMLElement>(".settings-archive-title"),
    ).map((node) => node.textContent);
    // 较新的会话排在前面，跟侧边栏"最新活动优先"的心智一致
    expect(titles).toEqual(["新会话", "旧会话"]);
  });

  it("groups archived threads by project and shows per-project counts", () => {
    renderSettings({
      initialized: baseInitialized(),
      initialPage: "archive",
      archivedThreads: [
        archivedThread("wuu-1", {
          archive_project_id: "wuu",
          archive_project_name: "wuu",
        }),
        archivedThread("wuu-2", {
          archive_project_id: "wuu",
          archive_project_name: "wuu",
        }),
        archivedThread("site-1", {
          archive_project_id: "site",
          archive_project_name: "网站",
        }),
      ],
    });

    const groups = container.querySelectorAll(".settings-archive-group");
    expect(groups).toHaveLength(2);
    expect(groups[0]?.textContent).toContain("wuu");
    expect(groups[0]?.textContent).toContain("2 个会话");
    expect(groups[1]?.textContent).toContain("网站");
    expect(groups[1]?.textContent).toContain("1 个会话");
  });

  it("filters archived threads by project", () => {
    renderSettings({
      initialized: baseInitialized(),
      initialPage: "archive",
      archivedThreads: [
        archivedThread("wuu-session", {
          title: "wuu 会话",
          archive_project_id: "wuu",
          archive_project_name: "wuu",
        }),
        archivedThread("site-session", {
          title: "网站会话",
          archive_project_id: "site",
          archive_project_name: "网站",
        }),
      ],
    });

    const trigger = container.querySelector<HTMLButtonElement>(
      '[aria-label="按项目筛选"]',
    );
    act(() => {
      trigger?.click();
    });
    const siteOption = Array.from(
      document.querySelectorAll<HTMLButtonElement>(".select-menu-item"),
    ).find((option) => option.textContent?.includes("网站"));
    act(() => {
      siteOption?.click();
    });

    expect(container.textContent).toContain("网站会话");
    expect(container.textContent).not.toContain("wuu 会话");
  });

  it("filters archived threads by title", () => {
    renderSettings({
      initialized: baseInitialized(),
      initialPage: "archive",
      archivedThreads: [
        archivedThread("docker", { title: "运行 Docker bench" }),
        archivedThread("electron", { title: "排查 Electron 启动失败" }),
      ],
    });

    const search = container.querySelector<HTMLInputElement>(
      '.settings-archive-search input[type="search"]',
    );
    expect(search).not.toBeNull();
    act(() => {
      if (search) {
        setInputValue(search, "Electron");
      }
    });

    expect(container.textContent).toContain("排查 Electron 启动失败");
    expect(container.textContent).not.toContain("运行 Docker bench");
  });

  it("keeps the full long title in a single ellipsized title element", () => {
    const longTitle = "这是一个非常长的归档会话标题，需要在恢复按钮前截断并显示省略号";
    renderSettings({
      initialized: baseInitialized(),
      initialPage: "archive",
      archivedThreads: [archivedThread("long", { title: longTitle })],
    });

    const title = container.querySelector<HTMLElement>(".settings-archive-title");
    expect(title?.textContent).toBe(longTitle);
    expect(title?.getAttribute("title")).toBe(longTitle);
  });

  it("keeps archived rows compact and does not render their local paths", () => {
    const threadWithPath = {
      ...archivedThread("hidden-path"),
      cwd: "/private/workspace",
    };
    renderSettings({
      initialized: baseInitialized(),
      initialPage: "archive",
      archivedThreads: [threadWithPath],
    });

    expect(container.querySelectorAll(".settings-archive-row")).toHaveLength(1);
    expect(container.querySelectorAll(".settings-row")).toHaveLength(0);
    expect(container.textContent).not.toContain("/private/workspace");
  });

  it("invokes onUnarchiveThread with the clicked thread", () => {
    const onUnarchiveThread = vi.fn();
    const target = archivedThread("dm-1", { title: "DM 旧会话" });
    renderSettings({
      initialized: baseInitialized(),
      initialPage: "archive",
      archivedThreads: [target],
      onUnarchiveThread,
    });

    const restoreButton = container.querySelector<HTMLButtonElement>(
      ".settings-archive-restore",
    );
    expect(restoreButton).not.toBeNull();
    act(() => {
      restoreButton?.click();
    });

    expect(onUnarchiveThread).toHaveBeenCalledTimes(1);
    expect(onUnarchiveThread).toHaveBeenCalledWith(target);
  });
});
