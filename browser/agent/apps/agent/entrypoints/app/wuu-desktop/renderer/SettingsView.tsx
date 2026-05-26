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
import type { InitializeResult } from "../shared/protocol";
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
  onSave: (provider: string, model: string) => Promise<void>;
  onDebugControlsChange: (enabled: boolean) => void;
  onSidebarResizeStart: (event: ReactPointerEvent<HTMLDivElement>) => void;
  onSidebarSeparatorKey: (event: ReactKeyboardEvent<HTMLDivElement>) => void;
  onSidebarSeparatorDoubleClick: () => void;
}): JSX.Element {
  const providers = initialized?.providers ?? [];
  const [providerDraft, setProviderDraft] = useState(initialized?.provider ?? "");
  const [modelDraft, setModelDraft] = useState(initialized?.model ?? "");
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    setProviderDraft(initialized?.provider ?? "");
    setModelDraft(initialized?.model ?? "");
    setError("");
    setSaved(false);
  }, [initialized?.provider, initialized?.model]);

  function changeProvider(provider: string): void {
    setProviderDraft(provider);
    setSaved(false);
    const summary = providers.find((item) => item.name === provider);
    if (summary) {
      setModelDraft(summary.model);
    }
  }

  async function submit(event: ReactFormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault();
    setError("");
    setSaved(false);
    try {
      await onSave(providerDraft, modelDraft);
      setSaved(true);
    } catch (saveError) {
      setError(saveError instanceof Error ? saveError.message : "保存失败");
    }
  }

  const disabled =
    running ||
    !providerDraft.trim() ||
    !modelDraft.trim() ||
    (providerDraft === initialized?.provider && modelDraft === initialized?.model);
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
