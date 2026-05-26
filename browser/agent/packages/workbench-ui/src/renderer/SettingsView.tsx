import { ArrowLeft, Settings } from "lucide-react";
import {
  type CSSProperties,
  type FormEvent as ReactFormEvent,
  type KeyboardEvent as ReactKeyboardEvent,
  type PointerEvent as ReactPointerEvent,
  useEffect,
  useState
} from "react";
import { OverlayScrollbarsComponent } from "overlayscrollbars-react";
import type { InitializeResult, RuntimeConnectionUpdate } from "../shared/protocol";
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
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);
  const selectedProvider = providers.find((item) => item.name === providerDraft);
  const selectedBaseURL = selectedProvider?.base_url ?? "";

  useEffect(() => {
    setProviderDraft(initialized?.provider ?? "");
    setModelDraft(initialized?.model ?? "");
    const summary = initialized?.providers?.find((item) => item.name === initialized.provider);
    setBaseURLDraft(summary?.base_url ?? "");
    setAPIKeyDraft("");
    setError("");
    setSaved(false);
  }, [initialized?.provider, initialized?.model, initialized?.providers]);

  function changeProvider(provider: string): void {
    setProviderDraft(provider);
    setSaved(false);
    const summary = providers.find((item) => item.name === provider);
    if (summary) {
      setModelDraft(summary.model);
      setBaseURLDraft(summary.base_url ?? "");
      setAPIKeyDraft("");
    }
  }

  async function submit(event: ReactFormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault();
    setError("");
    setSaved(false);
    try {
      const connection: RuntimeConnectionUpdate = {
        base_url: baseURLDraft.trim()
      };
      const apiKey = apiKeyDraft.trim();
      if (apiKey) {
        connection.api_key = apiKey;
      }
      await onSave(providerDraft, modelDraft, undefined, connection);
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
    !baseURLDraft.trim() ||
    (providerDraft === initialized?.provider &&
      modelDraft === initialized?.model &&
      baseURLDraft.trim() === selectedBaseURL &&
      !apiKeyDraft.trim());
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
              <p>选择 wuu 使用的 Provider 和模型。</p>
            </div>
            <form className="settings-card" onSubmit={submit}>
              <label className="settings-row">
                <span>
                  <strong>Provider</strong>
                  <small>选择当前会话运行时使用的模型服务</small>
                </span>
                {providers.length > 0 ? (
                  <select value={providerDraft} onChange={(event) => changeProvider(event.target.value)} disabled={running}>
                    {providers.map((provider) => (
                      <option key={provider.name} value={provider.name}>
                        {provider.name}
                      </option>
                    ))}
                  </select>
                ) : (
                  <input
                    value={providerDraft}
                    onChange={(event) => {
                      setProviderDraft(event.target.value);
                      setSaved(false);
                    }}
                    disabled={running}
                  />
                )}
              </label>
              <label className="settings-row">
                <span>
                  <strong>模型</strong>
                  <small>Provider 配置里的模型名称</small>
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
                  <small>模型服务的 API 地址</small>
                </span>
                <input
                  value={baseURLDraft}
                  placeholder="https://api.openai.com/v1"
                  onChange={(event) => {
                    setBaseURLDraft(event.target.value);
                    setSaved(false);
                  }}
                  disabled={running}
                />
              </label>
              <label className="settings-row">
                <span>
                  <strong>API key</strong>
                  <small>{selectedProvider?.api_key_configured ? "已配置，留空不修改" : "用于访问这个 Provider"}</small>
                </span>
                <input
                  value={apiKeyDraft}
                  type="password"
                  autoComplete="new-password"
                  placeholder={selectedProvider?.api_key_configured ? "留空保持当前密钥" : "输入 API key"}
                  onChange={(event) => {
                    setAPIKeyDraft(event.target.value);
                    setSaved(false);
                  }}
                  disabled={running}
                />
              </label>
              <div className="settings-card-footer">
                {error ? <div className="settings-error">{error}</div> : null}
                {saved ? <div className="settings-saved">已保存</div> : null}
                <button type="submit" disabled={disabled}>
                  保存
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
