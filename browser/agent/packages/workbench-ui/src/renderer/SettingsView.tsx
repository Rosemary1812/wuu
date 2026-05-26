import { ArrowLeft, Plus, Settings, X } from "lucide-react";
import {
  type CSSProperties,
  type FormEvent as ReactFormEvent,
  type KeyboardEvent as ReactKeyboardEvent,
  type PointerEvent as ReactPointerEvent,
  useEffect,
  useMemo,
  useState
} from "react";
import { OverlayScrollbarsComponent } from "overlayscrollbars-react";
import type { InitializeResult, ProviderSummary, RuntimeConnectionUpdate } from "../shared/protocol";
import { OVERLAY_SCROLLBAR_OPTIONS } from "./ScrollbarOptions";

export function SettingsView({
  initialized,
  running,
  showDebugControlsSetting,
  debugControlsEnabled,
  sidebarWidth,
  sidebarMinWidth,
  sidebarMaxWidth,
  resizingSidebar,
  onBack,
  onSave,
  onDebugControlsChange,
  onSidebarResizeStart,
  onSidebarSeparatorKey,
  onSidebarSeparatorDoubleClick
}: {
  initialized?: InitializeResult;
  running: boolean;
  showDebugControlsSetting: boolean;
  debugControlsEnabled: boolean;
  sidebarWidth: number;
  sidebarMinWidth: number;
  sidebarMaxWidth: number;
  resizingSidebar: boolean;
  onBack: () => void;
  onSave: (provider: string, model: string, effort?: string, connection?: RuntimeConnectionUpdate) => Promise<void>;
  onDebugControlsChange: (enabled: boolean) => void;
  onSidebarResizeStart: (event: ReactPointerEvent<HTMLDivElement>) => void;
  onSidebarSeparatorKey: (event: ReactKeyboardEvent<HTMLDivElement>) => void;
  onSidebarSeparatorDoubleClick: () => void;
}): JSX.Element {
  const providers = initialized?.providers ?? [];
  const [providerDraft, setProviderDraft] = useState(initialized?.provider ?? "");
  const [modelDraft, setModelDraft] = useState(initialized?.model ?? "");
  const [baseURLDraft, setBaseURLDraft] = useState("");
  const [apiKeyDraft, setAPIKeyDraft] = useState("");
  const [addingProvider, setAddingProvider] = useState(false);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);
  const selectedProvider = addingProvider ? undefined : providers.find((item) => item.name === providerDraft);
  const providerLabels = useMemo(() => providerDisplayLabels(providers), [providers]);
  const selectedBaseURL = selectedProvider?.base_url ?? "";
  const connectionLocked = !addingProvider && (selectedProvider?.connection_locked ?? false);

  useEffect(() => {
    setProviderDraft(initialized?.provider ?? "");
    setModelDraft(initialized?.model ?? "");
    const summary = initialized?.providers?.find((item) => item.name === initialized.provider);
    setBaseURLDraft(summary?.base_url ?? "");
    setAPIKeyDraft("");
    setAddingProvider(false);
    setError("");
    setSaved(false);
  }, [initialized?.provider, initialized?.model, initialized?.providers]);

  function changeProvider(provider: string): void {
    setAddingProvider(false);
    setProviderDraft(provider);
    setSaved(false);
    const summary = providers.find((item) => item.name === provider);
    if (summary) {
      setModelDraft(summary.model);
      setBaseURLDraft(summary.base_url ?? "");
      setAPIKeyDraft("");
    }
  }

  function startAddingProvider(): void {
    setAddingProvider(true);
    setProviderDraft(nextCustomProviderName(providers));
    setModelDraft("");
    setBaseURLDraft("");
    setAPIKeyDraft("");
    setError("");
    setSaved(false);
  }

  function cancelAddingProvider(): void {
    setAddingProvider(false);
    setProviderDraft(initialized?.provider ?? "");
    setModelDraft(initialized?.model ?? "");
    const summary = initialized?.providers?.find((item) => item.name === initialized.provider);
    setBaseURLDraft(summary?.base_url ?? "");
    setAPIKeyDraft("");
    setError("");
    setSaved(false);
  }

  async function submit(event: ReactFormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault();
    setError("");
    setSaved(false);
    try {
      let connection: RuntimeConnectionUpdate | undefined;
      if (addingProvider) {
        connection = {
          base_url: baseURLDraft.trim(),
          api_key: apiKeyDraft.trim(),
          create_provider: true
        };
      } else if (!connectionLocked) {
        connection = {
          base_url: baseURLDraft.trim()
        };
        const apiKey = apiKeyDraft.trim();
        if (apiKey) {
          connection.api_key = apiKey;
        }
      }
      await onSave(providerDraft, modelDraft, undefined, connection);
      setAddingProvider(false);
      setAPIKeyDraft("");
      setSaved(true);
    } catch (saveError) {
      setError(saveError instanceof Error ? saveError.message : "保存失败");
    }
  }

  const disabled =
    running ||
    !providerDraft.trim() ||
    !modelDraft.trim() ||
    (!connectionLocked && !baseURLDraft.trim()) ||
    (addingProvider && !apiKeyDraft.trim()) ||
    (!addingProvider &&
      providerDraft === initialized?.provider &&
      modelDraft === initialized?.model &&
      (connectionLocked || baseURLDraft.trim() === selectedBaseURL) &&
      (connectionLocked || !apiKeyDraft.trim()));
  const shellStyle = {
    "--sidebar-width": `${sidebarWidth}px`
  } as CSSProperties;

  return (
    <div className={`settings-shell${resizingSidebar ? " resizing-sidebar" : ""}`} style={shellStyle}>
      <aside className="settings-sidebar">
        <div className="traffic-spacer" />
        <button className="settings-back-button" type="button" onClick={onBack}>
          <ArrowLeft size={17} />
          <span>返回应用</span>
        </button>
        <nav className="settings-nav" aria-label="设置">
          <button className="settings-nav-item active" type="button">
            <Settings size={18} />
            <span>常规</span>
          </button>
        </nav>
      </aside>
      <div
        className="sidebar-resizer settings-sidebar-resizer"
        role="separator"
        aria-label="调整设置侧边栏宽度"
        aria-orientation="vertical"
        aria-valuemin={sidebarMinWidth}
        aria-valuemax={sidebarMaxWidth}
        aria-valuenow={sidebarWidth}
        tabIndex={0}
        onPointerDown={onSidebarResizeStart}
        onDoubleClick={onSidebarSeparatorDoubleClick}
        onKeyDown={onSidebarSeparatorKey}
      />
      <OverlayScrollbarsComponent
        element="main"
        className="settings-main"
        data-overlayscrollbars-initialize
        defer
        options={OVERLAY_SCROLLBAR_OPTIONS}
      >
        <div className="settings-page">
          <h1>常规</h1>

          <section className="settings-section">
            <div>
              <h2>模型</h2>
              <p>选择请求要发送到哪个模型服务，以及传给服务的模型名称。</p>
            </div>
            <form className="settings-card" onSubmit={submit}>
              <div className="settings-row">
                <span>
                  <strong>模型服务</strong>
                  <small>{addingProvider ? "添加一个新的 OpenAI-compatible 服务" : "选择当前会话使用的连接方式"}</small>
                </span>
                {addingProvider ? (
                  <div className="settings-provider-control">
                    <div className="settings-new-provider-label">新的模型服务</div>
                    <button className="settings-inline-button" type="button" onClick={cancelAddingProvider} disabled={running}>
                      <X size={15} />
                      <span>取消</span>
                    </button>
                  </div>
                ) : (
                  <div className="settings-provider-control">
                    {providers.length > 0 ? (
                      <select value={providerDraft} onChange={(event) => changeProvider(event.target.value)} disabled={running}>
                        {providers.map((provider) => (
                          <option key={provider.name} value={provider.name}>
                            {providerLabels.get(provider.name) ?? provider.name}
                          </option>
                        ))}
                      </select>
                    ) : (
                      <div className="settings-new-provider-label">暂无模型服务</div>
                    )}
                    <button className="settings-inline-button" type="button" onClick={startAddingProvider} disabled={running}>
                      <Plus size={15} />
                      <span>添加</span>
                    </button>
                  </div>
                )}
              </div>
              <label className="settings-row">
                <span>
                  <strong>模型名称</strong>
                  <small>发送给模型服务的 model 名称</small>
                </span>
                <input
                  value={modelDraft}
                  onChange={(event) => {
                    setModelDraft(event.target.value);
                    setSaved(false);
                  }}
                  disabled={running}
                />
              </label>
              <label className="settings-row">
                <span>
                  <strong>Base URL</strong>
                  <small>{connectionLocked ? "由 OpenAI OAuth 管理" : "模型服务的 API 地址"}</small>
                </span>
                <input
                  value={baseURLDraft}
                  placeholder={connectionLocked ? "由 OpenAI OAuth 管理" : "https://api.openai.com/v1"}
                  onChange={(event) => {
                    setBaseURLDraft(event.target.value);
                    setSaved(false);
                  }}
                  disabled={running || connectionLocked}
                />
              </label>
              <label className="settings-row">
                <span>
                  <strong>API key</strong>
                  <small>
                    {connectionLocked
                      ? "由 OpenAI OAuth 管理"
                      : addingProvider
                        ? "保存时写入这个服务"
                        : selectedProvider?.api_key_configured
                        ? "已配置，留空不修改"
                        : "用于访问这个 Provider"}
                  </small>
                </span>
                <input
                  value={apiKeyDraft}
                  type="password"
                  autoComplete="new-password"
                  placeholder={
                    connectionLocked
                      ? "不需要 API key"
                      : addingProvider
                        ? "输入 API key"
                        : selectedProvider?.api_key_configured
                        ? "留空保持当前密钥"
                        : "输入 API key"
                  }
                  onChange={(event) => {
                    setAPIKeyDraft(event.target.value);
                    setSaved(false);
                  }}
                  disabled={running || connectionLocked}
                />
              </label>
              <div className="settings-card-footer">
                {error ? <div className="settings-error">{error}</div> : null}
                {saved ? <div className="settings-saved">已保存</div> : null}
                <button type="submit" disabled={disabled}>
                  {addingProvider ? "添加" : "保存"}
                </button>
              </div>
            </form>
          </section>

          {showDebugControlsSetting ? (
            <section className="settings-section">
              <div>
                <h2>开发</h2>
                <p>控制开发时才需要的界面入口。</p>
              </div>
              <div className="settings-card">
                <div className="settings-row">
                  <span>
                    <strong>调试入口</strong>
                    <small>显示启动动画、调试面板和开发样例入口</small>
                  </span>
                  <button
                    className="settings-switch"
                    type="button"
                    role="switch"
                    aria-checked={debugControlsEnabled}
                    onClick={() => onDebugControlsChange(!debugControlsEnabled)}
                  >
                    <span className="settings-switch-thumb" aria-hidden="true" />
                    <span className="sr-only">{debugControlsEnabled ? "关闭调试入口" : "打开调试入口"}</span>
                  </button>
                </div>
              </div>
            </section>
          ) : null}
        </div>
      </OverlayScrollbarsComponent>
    </div>
  );
}

function providerDisplayLabels(providers: ProviderSummary[]): Map<string, string> {
  const baseLabels = new Map<string, string>();
  const counts = new Map<string, number>();
  providers.forEach((provider) => {
    const label = providerBaseLabel(provider);
    baseLabels.set(provider.name, label);
    counts.set(label, (counts.get(label) ?? 0) + 1);
  });
  return new Map(
    providers.map((provider) => {
      const label = baseLabels.get(provider.name) ?? provider.name;
      if ((counts.get(label) ?? 0) > 1) {
        return [provider.name, `${label} · ${provider.name}`];
      }
      return [provider.name, label];
    })
  );
}

function nextCustomProviderName(providers: ProviderSummary[]): string {
  const existing = new Set(providers.map((provider) => provider.name));
  let index = 1;
  while (existing.has(`custom-${index}`)) {
    index += 1;
  }
  return `custom-${index}`;
}

function providerBaseLabel(provider: ProviderSummary): string {
  const service = providerServiceLabel(provider);
  const model = provider.model.trim();
  return model ? `${service} · ${model}` : service;
}

function providerServiceLabel(provider: ProviderSummary): string {
  const type = provider.type.trim().toLowerCase().replaceAll("_", "-");
  if (provider.connection_locked || type === "openai-codex" || type === "codex-subscription" || type === "chatgpt-codex") {
    return "OpenAI OAuth";
  }
  const baseURLLabel = serviceLabelFromBaseURL(provider.base_url);
  if (baseURLLabel) {
    return baseURLLabel;
  }
  if (type === "anthropic" || type === "claude" || type === "anthropic-official") {
    return "Anthropic";
  }
  if (type === "openai" || type === "codex") {
    return "OpenAI API";
  }
  if (type === "openai-compatible") {
    return serviceLabelFromBaseURL(provider.base_url) || "OpenAI-compatible";
  }
  return type || "模型服务";
}

function serviceLabelFromBaseURL(baseURL?: string): string {
  const host = hostFromBaseURL(baseURL);
  if (!host) return "";
  if (host.includes("api.openai.com")) return "OpenAI API";
  if (host.includes("api.anthropic.com")) return "Anthropic";
  if (host.includes("openrouter.ai")) return "OpenRouter";
  if (host.includes("moonshot") || host.includes("kimi")) return "Kimi";
  if (host.includes("bigmodel") || host.includes("zhipu")) return "智谱";
  if (host.includes("deepseek")) return "DeepSeek";
  if (host.includes("generativelanguage.googleapis.com") || host.includes("googleapis.com")) return "Google Gemini";
  if (host.includes("dashscope") || host.includes("aliyuncs.com")) return "阿里云百炼";
  if (host.includes("volces") || host.includes("ark.cn-beijing.volces.com")) return "火山方舟";
  if (host.includes("siliconflow")) return "硅基流动";
  if (host === "localhost" || host === "127.0.0.1" || host === "::1") return "本地模型服务";
  return host;
}

function hostFromBaseURL(baseURL?: string): string {
  if (!baseURL) return "";
  try {
    return new URL(baseURL).hostname.replace(/^www\./, "");
  } catch {
    return "";
  }
}
