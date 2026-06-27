import {
  ArrowLeft,
  BarChart3,
  KeyRound,
  Plug,
  PlugZap,
  Plus,
  RefreshCw,
  Settings,
  SlidersHorizontal,
  X
} from "lucide-react";
import type { RuntimeAdvancedSettingsUpdate, SettingsUsageRange } from "../shared/protocol";
import {
  type CSSProperties,
  type FormEvent as ReactFormEvent,
  type KeyboardEvent as ReactKeyboardEvent,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
  useEffect,
  useMemo,
  useState
} from "react";
import type {
  DesktopBuildInfo,
  ExtensionSessionTrustSummary,
  ExtensionSurfaceTrustSummary,
  ExtensionTrustSummary,
  InitializeResult,
  MCPServerStatus,
  ProviderSummary,
  RuntimeConnectionUpdate,
  SettingsUsageDay,
  SettingsUsageResponse
} from "../shared/protocol";
import { normalizedVariantForProviderModel, providerModelVariantOptions, variantLabel } from "./RuntimeHelpers";

export type SettingsPage = "providers" | "advanced" | "general" | "usage";

export function SettingsView({
  initialized,
  initialPage,
  running,
  usage,
  showDebugControlsSetting,
  debugControlsEnabled,
  sidebarWidth,
  sidebarMinWidth,
  sidebarMaxWidth,
  resizingSidebar,
  onBack,
  onSave,
  onAdvancedSave,
  onDebugControlsChange,
  onSidebarResizeStart,
  onSidebarSeparatorKey,
  onSidebarSeparatorDoubleClick,
  usageRange,
  setUsageRange,
}: {
  initialized?: InitializeResult;
  initialPage?: SettingsPage;
  running: boolean;
  usage?: SettingsUsageResponse;
  usageRange: SettingsUsageRange;
  setUsageRange: (range: SettingsUsageRange) => void;
  showDebugControlsSetting: boolean;
  debugControlsEnabled: boolean;
  sidebarWidth: number;
  sidebarMinWidth: number;
  sidebarMaxWidth: number;
  resizingSidebar: boolean;
  onBack: () => void;
  onSave: (provider: string, model: string, effort?: string, connection?: RuntimeConnectionUpdate, variant?: string) => Promise<void>;
  onAdvancedSave: (settings: RuntimeAdvancedSettingsUpdate) => Promise<void>;
  onDebugControlsChange: (enabled: boolean) => void;
  onSidebarResizeStart: (event: ReactPointerEvent<HTMLDivElement>) => void;
  onSidebarSeparatorKey: (event: ReactKeyboardEvent<HTMLDivElement>) => void;
  onSidebarSeparatorDoubleClick: () => void;
}): JSX.Element {
  const providers = initialized?.providers ?? [];
  const [providerDraft, setProviderDraft] = useState(initialized?.provider ?? "");
  const [modelDraft, setModelDraft] = useState(initialized?.model ?? "");
  const [variantDraft, setVariantDraft] = useState(initialized?.variant ?? initialized?.effort ?? "");
  const [baseURLDraft, setBaseURLDraft] = useState("");
  const [apiKeyDraft, setAPIKeyDraft] = useState("");
  const [addingProvider, setAddingProvider] = useState(false);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);
  const [desktopBuild, setDesktopBuild] = useState<DesktopBuildInfo | undefined>();
  const [activePage, setActivePage] = useState<SettingsPage>(initialPage ?? "providers");
  const [mcpServers, setMCPServers] = useState<MCPServerStatus[]>([]);
  const [mcpLoading, setMCPLoading] = useState(false);
  const [mcpError, setMCPError] = useState("");
  const [mcpBusyServer, setMCPBusyServer] = useState("");
  const [autoCompactDraft, setAutoCompactDraft] = useState(true);
  const [compactThresholdDraft, setCompactThresholdDraft] = useState("");
  const [compactKeepRecentDraft, setCompactKeepRecentDraft] = useState("");
  const [providerContextWindowDraft, setProviderContextWindowDraft] = useState("");
  const [maxContextTokensDraft, setMaxContextTokensDraft] = useState("");
  const [maxStepsDraft, setMaxStepsDraft] = useState("0");
  const [temperatureDraft, setTemperatureDraft] = useState("0.2");
  const [advancedError, setAdvancedError] = useState("");
  const [advancedSaved, setAdvancedSaved] = useState(false);

  useEffect(() => {
    setActivePage(initialPage ?? "providers");
  }, [initialPage]);

  useEffect(() => {
    let cancelled = false;
    void window.wuu.getBuildInfo().then((info) => {
      if (!cancelled) {
        setDesktopBuild(info.desktop);
      }
    });
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    let cancelled = false;
    setMCPLoading(true);
    setMCPError("");
    void window.wuu
      .listMCPServers()
      .then((result) => {
        if (!cancelled) {
          setMCPServers(result.servers ?? []);
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setMCPError(err instanceof Error ? err.message : String(err));
        }
      })
      .finally(() => {
        if (!cancelled) {
          setMCPLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const core = initialized?.core;
  const selectedProvider = addingProvider ? undefined : providers.find((item) => item.name === providerDraft);
  const providerLabels = useMemo(() => providerDisplayLabels(providers), [providers]);
  const selectedBaseURL = selectedProvider?.base_url ?? "";
  const connectionLocked = !addingProvider && (selectedProvider?.connection_locked ?? false);
  const variantOptions = providerModelVariantOptions(selectedProvider, modelDraft, variantDraft);
  const providerNameTaken = addingProvider && providers.some((item) => item.name === providerDraft.trim());

  useEffect(() => {
    setProviderDraft(initialized?.provider ?? "");
    setModelDraft(initialized?.model ?? "");
    setVariantDraft(initialized?.variant ?? initialized?.effort ?? "");
    const summary = initialized?.providers?.find((item) => item.name === initialized.provider);
    setBaseURLDraft(summary?.base_url ?? "");
    setAPIKeyDraft("");
    setAddingProvider(false);
    setError("");
    setSaved(false);
  }, [initialized?.provider, initialized?.model, initialized?.variant, initialized?.effort, initialized?.providers]);

  useEffect(() => {
    const advanced = initialized?.advanced_settings;
    setAutoCompactDraft(!(advanced?.disable_auto_compact ?? false));
    setCompactThresholdDraft(formatPercentDraft(advanced?.compact_threshold_pct));
    setCompactKeepRecentDraft(formatOptionalNumberDraft(advanced?.compact_keep_recent_tokens));
    setProviderContextWindowDraft(formatOptionalNumberDraft(advanced?.provider_context_window));
    setMaxContextTokensDraft(formatOptionalNumberDraft(advanced?.max_context_tokens));
    setMaxStepsDraft(String(advanced?.max_steps ?? 0));
    setTemperatureDraft(formatTemperatureDraft(advanced?.temperature));
    setAdvancedError("");
    setAdvancedSaved(false);
  }, [initialized?.advanced_settings, initialized?.provider, initialized?.model]);

  function changeProvider(provider: string): void {
    setAddingProvider(false);
    setProviderDraft(provider);
    setSaved(false);
    const summary = providers.find((item) => item.name === provider);
    if (summary) {
      setModelDraft(summary.model);
      setVariantDraft(normalizedVariantForProviderModel(initialized?.variant ?? initialized?.effort ?? "", summary, summary.model));
      setBaseURLDraft(summary.base_url ?? "");
      setAPIKeyDraft("");
    }
  }

  function startAddingProvider(): void {
    setAddingProvider(true);
    setProviderDraft(nextCustomProviderName(providers));
    setModelDraft("");
    setVariantDraft("");
    setBaseURLDraft("");
    setAPIKeyDraft("");
    setError("");
    setSaved(false);
  }

  function cancelAddingProvider(): void {
    setAddingProvider(false);
    setProviderDraft(initialized?.provider ?? "");
    setModelDraft(initialized?.model ?? "");
    setVariantDraft(initialized?.variant ?? initialized?.effort ?? "");
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
      await onSave(providerDraft, modelDraft, undefined, connection, variantDraft);
      setAddingProvider(false);
      setAPIKeyDraft("");
      setSaved(true);
    } catch (saveError) {
      setError(saveError instanceof Error ? saveError.message : "保存失败");
    }
  }

  async function runMCPAction(name: string, action: "connect" | "disconnect" | "refresh"): Promise<void> {
    setMCPBusyServer(name);
    setMCPError("");
    try {
      const result =
        action === "connect"
          ? await window.wuu.connectMCPServer(name)
          : action === "disconnect"
            ? await window.wuu.disconnectMCPServer(name)
            : await window.wuu.refreshMCPServer(name);
      setMCPServers((servers) => upsertMCPServerStatus(servers, result.status));
    } catch (err) {
      setMCPError(err instanceof Error ? err.message : String(err));
    } finally {
      setMCPBusyServer("");
    }
  }

  async function submitAdvanced(event: ReactFormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault();
    setAdvancedError("");
    setAdvancedSaved(false);
    const update = parseAdvancedSettingsDraft({
      autoCompact: autoCompactDraft,
      compactThreshold: compactThresholdDraft,
      compactKeepRecent: compactKeepRecentDraft,
      providerContextWindow: providerContextWindowDraft,
      maxContextTokens: maxContextTokensDraft,
      maxSteps: maxStepsDraft,
      temperature: temperatureDraft,
    });
    if (update.error) {
      setAdvancedError(update.error);
      return;
    }
    try {
      await onAdvancedSave(update.settings);
      setAdvancedSaved(true);
    } catch (saveError) {
      setAdvancedError(saveError instanceof Error ? saveError.message : "保存失败");
    }
  }

  const disabled =
    running ||
    !providerDraft.trim() ||
    providerNameTaken ||
    !modelDraft.trim() ||
    (!connectionLocked && !baseURLDraft.trim()) ||
    (addingProvider && !apiKeyDraft.trim()) ||
    (!addingProvider &&
      providerDraft === initialized?.provider &&
      modelDraft === initialized?.model &&
      variantDraft === (initialized?.variant ?? initialized?.effort ?? "") &&
      (connectionLocked || baseURLDraft.trim() === selectedBaseURL) &&
      (connectionLocked || !apiKeyDraft.trim()));
  const shellStyle = {
    "--sidebar-width": `${sidebarWidth}px`
  } as CSSProperties;

  const pageMeta = settingsPageMeta(activePage);

  return (
    <div className={`settings-shell${resizingSidebar ? " resizing-sidebar" : ""}`} style={shellStyle}>
      <aside className="settings-sidebar">
        <div className="traffic-spacer" />
        <button className="settings-back-button" type="button" onClick={onBack}>
          <ArrowLeft className="icon" />
          <span>返回应用</span>
        </button>
        <nav className="settings-nav" aria-label="设置">
          <SettingsNavItem icon={<KeyRound className="icon-lg" />} active={activePage === "providers"} onClick={() => setActivePage("providers")}>
            模型服务
          </SettingsNavItem>
          <SettingsNavItem icon={<Settings className="icon-lg" />} active={activePage === "general"} onClick={() => setActivePage("general")}>
            常规
          </SettingsNavItem>
          <SettingsNavItem icon={<SlidersHorizontal className="icon-lg" />} active={activePage === "advanced"} onClick={() => setActivePage("advanced")}>
            高级
          </SettingsNavItem>
          <SettingsNavItem icon={<BarChart3 className="icon-lg" />} active={activePage === "usage"} onClick={() => setActivePage("usage")}>
            用量
          </SettingsNavItem>
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
      <main className="settings-main">
        <div className="settings-page">
          <header className="settings-page-header">
            <div>
              <h1 className="settings-page-title">{pageMeta.title}</h1>
              <p className="settings-page-description">{pageMeta.description}</p>
            </div>
          </header>

          {activePage === "providers" ? (
            <SettingsProvidersPage
              providers={providers}
              providerLabels={providerLabels}
              running={running}
              providerDraft={providerDraft}
              modelDraft={modelDraft}
              variantDraft={variantDraft}
              baseURLDraft={baseURLDraft}
              apiKeyDraft={apiKeyDraft}
              addingProvider={addingProvider}
              error={error}
              saved={saved}
              selectedProvider={selectedProvider}
              connectionLocked={connectionLocked}
              variantOptions={variantOptions}
              providerNameTaken={Boolean(providerNameTaken)}
              onProviderChange={changeProvider}
              onStartAddingProvider={startAddingProvider}
              onCancelAddingProvider={cancelAddingProvider}
              onProviderDraftChange={(value) => {
                setProviderDraft(value);
                setSaved(false);
              }}
              onModelDraftChange={(value) => {
                setModelDraft(value);
                setVariantDraft("");
                setSaved(false);
              }}
              onVariantDraftChange={(value) => {
                setVariantDraft(value);
                setSaved(false);
              }}
              onBaseURLDraftChange={(value) => {
                setBaseURLDraft(value);
                setSaved(false);
              }}
              onAPIKeyDraftChange={(value) => {
                setAPIKeyDraft(value);
                setSaved(false);
              }}
              onSubmit={submit}
              disabled={disabled}
            />
          ) : activePage === "advanced" ? (
            <SettingsAdvancedPage
              initialized={initialized}
              running={running}
              autoCompact={autoCompactDraft}
              compactThreshold={compactThresholdDraft}
              compactKeepRecent={compactKeepRecentDraft}
              providerContextWindow={providerContextWindowDraft}
              maxContextTokens={maxContextTokensDraft}
              maxSteps={maxStepsDraft}
              temperature={temperatureDraft}
              error={advancedError}
              saved={advancedSaved}
              onAutoCompactToggle={() => {
                setAutoCompactDraft((value) => !value);
                setAdvancedSaved(false);
              }}
              onCompactThresholdChange={(value) => {
                setCompactThresholdDraft(value);
                setAdvancedSaved(false);
              }}
              onCompactKeepRecentChange={(value) => {
                setCompactKeepRecentDraft(value);
                setAdvancedSaved(false);
              }}
              onProviderContextWindowChange={(value) => {
                setProviderContextWindowDraft(value);
                setAdvancedSaved(false);
              }}
              onMaxContextTokensChange={(value) => {
                setMaxContextTokensDraft(value);
                setAdvancedSaved(false);
              }}
              onMaxStepsChange={(value) => {
                setMaxStepsDraft(value);
                setAdvancedSaved(false);
              }}
              onTemperatureChange={(value) => {
                setTemperatureDraft(value);
                setAdvancedSaved(false);
              }}
              onSubmit={submitAdvanced}
            />
          ) : activePage === "general" ? (
            <SettingsGeneralPage
              initialized={initialized}
              desktopBuild={desktopBuild}
              core={core}
              showDebugControlsSetting={showDebugControlsSetting}
              debugControlsEnabled={debugControlsEnabled}
              mcpServers={mcpServers}
              mcpLoading={mcpLoading}
              mcpError={mcpError}
              mcpBusyServer={mcpBusyServer}
              onDebugControlsChange={onDebugControlsChange}
              onMCPAction={runMCPAction}
            />
          ) : (
            <SettingsUsagePage
              usage={usage}
              usageRange={usageRange}
              setUsageRange={setUsageRange}
            />
          )}
        </div>
      </main>
    </div>
  );
}

/* -------------------------------------------------------------------------- */
/*  Shared primitives                                                          */
/* -------------------------------------------------------------------------- */

function SettingsNavItem({
  icon,
  active,
  onClick,
  children
}: {
  icon: ReactNode;
  active: boolean;
  onClick: () => void;
  children: ReactNode;
}): JSX.Element {
  return (
    <button
      className={`settings-nav-item${active ? " active" : ""}`}
      type="button"
      aria-current={active ? "page" : undefined}
      onClick={onClick}
    >
      {icon}
      <span>{children}</span>
    </button>
  );
}

function SettingsSection({
  title,
  description,
  testID,
  children
}: {
  title: string;
  description: string;
  testID?: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <section className="settings-section" {...(testID ? { "data-testid": testID } : {})}>
      <header className="settings-section-header">
        <h2 className="settings-section-title">{title}</h2>
        <p className="settings-section-description">{description}</p>
      </header>
      {children}
    </section>
  );
}

function SettingsCard({ children }: { children: ReactNode }): JSX.Element {
  return <div className="settings-card">{children}</div>;
}

function SettingsRow({
  title,
  description,
  children,
  block = false
}: {
  title: string;
  description?: string;
  children: ReactNode;
  block?: boolean;
}): JSX.Element {
  return (
    <div className="settings-row">
      <div className="settings-row-label">
        <span className="settings-row-label-title">{title}</span>
        {description ? <span className="settings-row-label-description">{description}</span> : null}
      </div>
      <div className={block ? "settings-row-control-block" : "settings-row-control"}>{children}</div>
    </div>
  );
}

function SettingsCardFooter({
  error,
  saved,
  submitLabel,
  disabled,
  children
}: {
  error: string;
  saved: boolean;
  submitLabel: string;
  disabled: boolean;
  children?: ReactNode;
}): JSX.Element {
  return (
    <div className="settings-card-footer">
      {error ? <div className="settings-error">{error}</div> : null}
      {saved && !error ? <div className="settings-saved">已保存</div> : null}
      {children}
      <button className="settings-button settings-button-primary" type="submit" disabled={disabled}>
        {submitLabel}
      </button>
    </div>
  );
}

/* -------------------------------------------------------------------------- */
/*  Providers page                                                             */
/* -------------------------------------------------------------------------- */

function SettingsProvidersPage({
  providers,
  providerLabels,
  running,
  providerDraft,
  modelDraft,
  variantDraft,
  baseURLDraft,
  apiKeyDraft,
  addingProvider,
  error,
  saved,
  selectedProvider,
  connectionLocked,
  variantOptions,
  providerNameTaken,
  onProviderChange,
  onStartAddingProvider,
  onCancelAddingProvider,
  onProviderDraftChange,
  onModelDraftChange,
  onVariantDraftChange,
  onBaseURLDraftChange,
  onAPIKeyDraftChange,
  onSubmit,
  disabled
}: {
  providers: ProviderSummary[];
  providerLabels: Map<string, string>;
  running: boolean;
  providerDraft: string;
  modelDraft: string;
  variantDraft: string;
  baseURLDraft: string;
  apiKeyDraft: string;
  addingProvider: boolean;
  error: string;
  saved: boolean;
  selectedProvider: ProviderSummary | undefined;
  connectionLocked: boolean;
  variantOptions: string[];
  providerNameTaken: boolean;
  onProviderChange: (provider: string) => void;
  onStartAddingProvider: () => void;
  onCancelAddingProvider: () => void;
  onProviderDraftChange: (value: string) => void;
  onModelDraftChange: (value: string) => void;
  onVariantDraftChange: (value: string) => void;
  onBaseURLDraftChange: (value: string) => void;
  onAPIKeyDraftChange: (value: string) => void;
  onSubmit: (event: ReactFormEvent<HTMLFormElement>) => Promise<void>;
  disabled: boolean;
}): JSX.Element {
  return (
    <SettingsSection
      title="模型服务"
      description="管理 BYOK provider、模型名称、Base URL 和 API key。"
      testID="settings-providers"
    >
      {providers.length > 0 ? (
        <div className="settings-provider-overview" data-testid="settings-provider-overview">
          {providers.map((provider) => (
            <button
              className={`settings-provider-button${!addingProvider && providerDraft === provider.name ? " active" : ""}`}
              type="button"
              key={provider.name}
              disabled={running}
              onClick={() => onProviderChange(provider.name)}
            >
              <strong>{providerServiceLabel(provider)}</strong>
              <small>{provider.name}</small>
              <small>{provider.model || "未选择模型"}</small>
              <small>{providerConnectionStatus(provider)}</small>
            </button>
          ))}
        </div>
      ) : null}
      <form className="settings-card" onSubmit={onSubmit}>
        <SettingsRow
          title={addingProvider ? "新增 OpenAI-compatible 服务" : "选择当前会话使用的服务"}
          description={addingProvider ? "添加一个新的 OpenAI-compatible 服务" : "切换 service 不会丢失 Base URL 和 API key。"}
        >
          <div className="settings-row-control-block">
            {addingProvider ? (
              <span className="settings-inline-flag">新的模型服务</span>
            ) : providers.length > 0 ? (
              <select
                className="settings-select"
                value={providerDraft}
                onChange={(event) => onProviderChange(event.target.value)}
                disabled={running}
              >
                {providers.map((provider) => (
                  <option key={provider.name} value={provider.name}>
                    {providerLabels.get(provider.name) ?? provider.name}
                  </option>
                ))}
              </select>
            ) : (
              <span className="settings-inline-flag">暂无模型服务</span>
            )}
            <button
              className="settings-button"
              type="button"
              onClick={addingProvider ? onCancelAddingProvider : onStartAddingProvider}
              disabled={running}
            >
              {addingProvider ? (
                <>
                  <X className="icon" />
                  <span>取消</span>
                </>
              ) : (
                <>
                  <Plus className="icon" />
                  <span>新增 OpenAI-compatible</span>
                </>
              )}
            </button>
          </div>
        </SettingsRow>
        {addingProvider ? (
          <SettingsRow
            title="服务标识"
            description={providerNameTaken ? "这个名称已存在" : "写入配置的 provider 名称"}
          >
            <input
              className="settings-input"
              value={providerDraft}
              onChange={(event) => onProviderDraftChange(event.target.value)}
              disabled={running}
            />
          </SettingsRow>
        ) : selectedProvider ? (
          <SettingsRow title="服务标识" description={providerTypeLabel(selectedProvider)}>
            <span className="settings-row-control-value">{selectedProvider.name}</span>
          </SettingsRow>
        ) : null}
        <SettingsRow
          title="模型名称"
          description="发送给模型服务的 model 名称"
          block
        >
          <input
            className="settings-input"
            value={modelDraft}
            onChange={(event) => onModelDraftChange(event.target.value)}
            disabled={running}
          />
        </SettingsRow>
        <SettingsRow
          title="思考强度"
          description={variantOptions.length > 1 ? "当前模型支持的参数档位" : "当前模型没有可调参数档位"}
          block
        >
          <select
            className="settings-select"
            value={variantDraft}
            onChange={(event) => onVariantDraftChange(event.target.value)}
            disabled={running || variantOptions.length <= 1}
          >
            {variantOptions.map((variant) => (
              <option key={variant || "auto"} value={variant}>
                {variantLabel(variant)}
              </option>
            ))}
          </select>
        </SettingsRow>
        <SettingsRow
          title="Base URL"
          description={connectionLocked ? "由 OpenAI OAuth 管理" : "模型服务的 API 地址"}
          block
        >
          <input
            className="settings-input"
            value={baseURLDraft}
            placeholder={connectionLocked ? "由 OpenAI OAuth 管理" : "https://api.openai.com/v1"}
            onChange={(event) => onBaseURLDraftChange(event.target.value)}
            disabled={running || connectionLocked}
          />
        </SettingsRow>
        <SettingsRow
          title="API key"
          description={
            connectionLocked
              ? "由 OpenAI OAuth 管理"
              : addingProvider
                ? "保存时写入这个服务"
                : selectedProvider?.api_key_configured
                  ? "已配置，留空不修改"
                  : "用于访问这个 Provider"
          }
          block
        >
          <input
            className="settings-input"
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
            onChange={(event) => onAPIKeyDraftChange(event.target.value)}
            disabled={running || connectionLocked}
          />
        </SettingsRow>
        <SettingsCardFooter
          error={error}
          saved={saved}
          submitLabel={addingProvider ? "添加服务" : "保存配置"}
          disabled={disabled}
        />
      </form>
    </SettingsSection>
  );
}

/* -------------------------------------------------------------------------- */
/*  Advanced page                                                              */
/* -------------------------------------------------------------------------- */

function SettingsAdvancedPage({
  initialized,
  running,
  autoCompact,
  compactThreshold,
  compactKeepRecent,
  providerContextWindow,
  maxContextTokens,
  maxSteps,
  temperature,
  error,
  saved,
  onAutoCompactToggle,
  onCompactThresholdChange,
  onCompactKeepRecentChange,
  onProviderContextWindowChange,
  onMaxContextTokensChange,
  onMaxStepsChange,
  onTemperatureChange,
  onSubmit
}: {
  initialized: InitializeResult | undefined;
  running: boolean;
  autoCompact: boolean;
  compactThreshold: string;
  compactKeepRecent: string;
  providerContextWindow: string;
  maxContextTokens: string;
  maxSteps: string;
  temperature: string;
  error: string;
  saved: boolean;
  onAutoCompactToggle: () => void;
  onCompactThresholdChange: (value: string) => void;
  onCompactKeepRecentChange: (value: string) => void;
  onProviderContextWindowChange: (value: string) => void;
  onMaxContextTokensChange: (value: string) => void;
  onMaxStepsChange: (value: string) => void;
  onTemperatureChange: (value: string) => void;
  onSubmit: (event: ReactFormEvent<HTMLFormElement>) => Promise<void>;
}): JSX.Element {
  return (
    <SettingsSection
      title="高级"
      description="调整上下文窗口、自动压缩触发点和单轮运行预算。"
      testID="settings-advanced"
    >
      <form className="settings-card" onSubmit={onSubmit}>
        <SettingsRow
          title="上下文与压缩"
          description="用于 BYOK 网关、模型别名和长会话预算控制"
          block
        >
          <span />
        </SettingsRow>
        <SettingsRow
          title="自动压缩"
          description="接近可用上下文时自动整理旧历史；溢出恢复仍会保留"
        >
          <button
            className="settings-switch"
            type="button"
            role="switch"
            aria-checked={autoCompact}
            disabled={running || !initialized}
            onClick={onAutoCompactToggle}
          >
            <span className="settings-switch-thumb" aria-hidden="true" />
            <span className="sr-only">{autoCompact ? "关闭自动压缩" : "打开自动压缩"}</span>
          </button>
        </SettingsRow>
        <SettingsRow
          title="压缩触发阈值"
          description="百分比；留空使用模型可用窗口，50 表示更早压缩"
          block
        >
          <input
            className="settings-input"
            value={compactThreshold}
            inputMode="numeric"
            placeholder="自动"
            onChange={(event) => onCompactThresholdChange(event.target.value)}
            disabled={running || !initialized}
          />
        </SettingsRow>
        <SettingsRow
          title="保留最近上下文"
          description="token；留空使用默认 20,000，控制压缩后保留的原文历史"
          block
        >
          <input
            className="settings-input"
            value={compactKeepRecent}
            inputMode="numeric"
            placeholder="20,000"
            onChange={(event) => onCompactKeepRecentChange(event.target.value)}
            disabled={running || !initialized}
          />
        </SettingsRow>
        <SettingsRow
          title="当前服务上下文窗口"
          description="token；用于自定义模型、网关别名或未收录新模型"
          block
        >
          <input
            className="settings-input"
            value={providerContextWindow}
            inputMode="numeric"
            placeholder="自动识别"
            onChange={(event) => onProviderContextWindowChange(event.target.value)}
            disabled={running || !initialized}
          />
        </SettingsRow>
        <SettingsRow
          title="未知模型窗口"
          description="token；当前 Provider 未覆盖且模型库未知时使用"
          block
        >
          <input
            className="settings-input"
            value={maxContextTokens}
            inputMode="numeric"
            placeholder="自动"
            onChange={(event) => onMaxContextTokensChange(event.target.value)}
            disabled={running || !initialized}
          />
        </SettingsRow>
        <SettingsRow
          title="上下文窗口"
          description={advancedContextSourceLabel(initialized?.advanced_settings?.context_window_source)}
        >
          <span className="settings-row-control-value">
            {formatTokenCount(initialized?.advanced_settings?.context_window_tokens ?? 0)}
          </span>
        </SettingsRow>
        <SettingsRow
          title="压缩触发"
          description="扣除输出预留后，主动整理旧历史的 token 点"
        >
          <span className="settings-row-control-value">
            {formatTokenCount(initialized?.advanced_settings?.compact_threshold_tokens ?? 0)}
          </span>
        </SettingsRow>
        <SettingsRow
          title="输出预留"
          description="为模型回答保留的 token 预算"
        >
          <span className="settings-row-control-value">
            {formatTokenCount(initialized?.advanced_settings?.output_reserve_tokens ?? 0)}
          </span>
        </SettingsRow>
        <SettingsRow
          title="Agent 预算"
          description="控制单轮自动执行的边界"
          block
        >
          <span />
        </SettingsRow>
        <SettingsRow
          title="最大步数"
          description="0 表示不设硬上限"
          block
        >
          <input
            className="settings-input"
            value={maxSteps}
            inputMode="numeric"
            onChange={(event) => onMaxStepsChange(event.target.value)}
            disabled={running || !initialized}
          />
        </SettingsRow>
        <SettingsRow
          title="Temperature"
          description="0 到 2；默认 0.2"
          block
        >
          <input
            className="settings-input"
            value={temperature}
            inputMode="decimal"
            onChange={(event) => onTemperatureChange(event.target.value)}
            disabled={running || !initialized}
          />
        </SettingsRow>
        <SettingsCardFooter
          error={error}
          saved={saved}
          submitLabel="保存高级设置"
          disabled={running || !initialized}
        />
      </form>
    </SettingsSection>
  );
}

/* -------------------------------------------------------------------------- */
/*  General page                                                               */
/* -------------------------------------------------------------------------- */

function SettingsGeneralPage({
  initialized,
  desktopBuild,
  core,
  showDebugControlsSetting,
  debugControlsEnabled,
  mcpServers,
  mcpLoading,
  mcpError,
  mcpBusyServer,
  onDebugControlsChange,
  onMCPAction
}: {
  initialized: InitializeResult | undefined;
  desktopBuild: DesktopBuildInfo | undefined;
  core: InitializeResult["core"] | undefined;
  showDebugControlsSetting: boolean;
  debugControlsEnabled: boolean;
  mcpServers: MCPServerStatus[];
  mcpLoading: boolean;
  mcpError: string;
  mcpBusyServer: string;
  onDebugControlsChange: (enabled: boolean) => void;
  onMCPAction: (name: string, action: "connect" | "disconnect" | "refresh") => Promise<void>;
}): JSX.Element {
  return (
    <>
      {showDebugControlsSetting ? (
        <SettingsSection
          title="开发"
          description="控制开发时才需要的界面入口。"
        >
          <SettingsCard>
            <SettingsRow
              title="调试入口"
              description="显示启动动画、调试面板和开发样例入口"
            >
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
            </SettingsRow>
          </SettingsCard>
        </SettingsSection>
      ) : null}

      <SettingsSection
        title="模型工具面"
        description="当前模型实际能看到的能力和文件编辑方式。"
        testID="settings-tool-surface"
      >
        <SettingsCard>
          <SettingsRow
            title="Profile"
            description={formatSurfaceRuntime(initialized)}
          >
            <span className="settings-row-control-value">
              {initialized?.tool_surface?.profile_name ?? initialized?.model_profile?.profile_name ?? "—"}
            </span>
          </SettingsRow>
          <SettingsRow
            title="编辑方式"
            description={initialized?.tool_surface?.bash_first ? "终端操作默认走 bash" : "按模型工具面执行"}
          >
            <span className="settings-row-control-value">
              {initialized?.tool_surface?.edit_primitive ?? initialized?.model_profile?.edit_primitive ?? "—"}
            </span>
          </SettingsRow>
          <SettingsRow
            title="可见能力"
            description={formatToolSurfaceCounts(initialized)}
            block
          >
            <span className="settings-row-control-value">
              {formatToolSurfaceCapabilities(initialized)}
            </span>
          </SettingsRow>
        </SettingsCard>
      </SettingsSection>

      <SettingsSection
        title="MCP"
        description="外部 MCP 服务器连接状态。"
        testID="settings-mcp"
      >
        <SettingsCard>
          {mcpLoading ? (
            <div className="settings-mcp-empty">加载中…</div>
          ) : mcpServers.length > 0 ? (
            mcpServers.map((server) => {
              const busy = mcpBusyServer === server.name;
              const connected = server.connected || server.state === "connected";
              return (
                <SettingsRow
                  key={server.name}
                  title={server.name}
                  description={formatMCPServerMeta(server)}
                >
                  <span className="settings-row-control-value">
                    <span className={`settings-status-pill ${mcpStateTone(server.state)}`}>
                      {mcpStateLabel(server.state)}
                    </span>
                  </span>
                  <button
                    className="settings-button settings-icon-button"
                    type="button"
                    title="刷新"
                    aria-label={`刷新 ${server.name}`}
                    disabled={busy}
                    onClick={() => void onMCPAction(server.name, "refresh")}
                  >
                    <RefreshCw size={15} aria-hidden="true" />
                  </button>
                  <button
                    className="settings-button settings-icon-button"
                    type="button"
                    title={connected ? "断开" : "连接"}
                    aria-label={`${connected ? "断开" : "连接"} ${server.name}`}
                    disabled={busy}
                    onClick={() => void onMCPAction(server.name, connected ? "disconnect" : "connect")}
                  >
                    {connected ? <PlugZap size={15} aria-hidden="true" /> : <Plug size={15} aria-hidden="true" />}
                  </button>
                  {server.error ? (
                    <small className="settings-mcp-error">{server.error}</small>
                  ) : null}
                </SettingsRow>
              );
            })
          ) : (
            <div className="settings-mcp-empty">暂无 MCP 服务器</div>
          )}
          {mcpError ? <div className="settings-mcp-empty settings-mcp-error">{mcpError}</div> : null}
        </SettingsCard>
      </SettingsSection>

      <SettingsSection
        title="关于"
        description="wuu 桌面与核心的构建信息。报问题时带上这一段。"
        testID="settings-about"
      >
        <SettingsCard>
          <SettingsRow
            title="桌面端"
            description="Electron 客户端构建"
          >
            <span className="settings-row-control-value">
              {desktopBuild ? `v${desktopBuild.version} · ${formatBuildDate(desktopBuild.date)}` : "加载中…"}
            </span>
          </SettingsRow>
          <SettingsRow
            title="核心"
            description="wuu app-server 二进制构建"
          >
            <span className="settings-row-control-value">{formatCoreBuild(core)}</span>
          </SettingsRow>
          <SettingsRow
            title="App-server 协议"
            description="桌面与核心的 IPC 协议版本"
          >
            <span className="settings-row-control-value">{initialized?.protocol_version ?? "—"}</span>
          </SettingsRow>
          <SettingsRow
            title="扩展边界"
            description="MCP、Hooks、Plugins、Skills 和 Workflows 的运行时状态"
            block
          >
            <span className="settings-row-control-value">{formatExtensionTrust(initialized?.extension_trust)}</span>
          </SettingsRow>
        </SettingsCard>
      </SettingsSection>
    </>
  );
}

/* -------------------------------------------------------------------------- */
/*  Usage page                                                                 */
/* -------------------------------------------------------------------------- */

function SettingsUsagePage({
  usage,
  usageRange,
  setUsageRange
}: {
  usage: SettingsUsageResponse | undefined;
  usageRange: SettingsUsageRange;
  setUsageRange: (range: SettingsUsageRange) => void;
}): JSX.Element {
  const ranges: SettingsUsageRange[] = ["all", "7d", "30d", "90d"];
  const heatmap = usage ? buildCacheHeatmap(usage.days) : [];
  const heatmapWeeks = heatmap.length > 0 ? Math.ceil(heatmap.length / 7) : 12;
  return (
    <>
      <SettingsSection
        title="本地用量"
        description="显示当前桌面已加载会话的 token 统计。"
        testID="settings-usage"
      >
        <div
          className="settings-usage-range"
          role="tablist"
          aria-label="时间范围"
        >
          {ranges.map((range) => (
            <button
              key={range}
              type="button"
              role="tab"
              aria-selected={usageRange === range}
              data-range={range}
              className={`settings-usage-range-button${usageRange === range ? " active" : ""}`}
              onClick={() => setUsageRange(range)}
            >
              {formatUsageRange(range)}
            </button>
          ))}
        </div>
        {usage ? (
          <div className="settings-usage-metrics">
            <UsageMetric
              label="上下文 token"
              value={usage.metrics.context_tokens}
              detail={`输入 ${formatTokenCount(usage.metrics.input_tokens)} · 缓存读取 ${formatTokenCount(usage.metrics.cache_read_tokens)}`}
            />
            <UsageMetric
              label="输入 token"
              value={usage.metrics.input_tokens}
              detail={`${usage.metrics.turns} 轮主会话`}
            />
            <UsageMetric
              label="输出 token"
              value={usage.metrics.output_tokens}
              detail={`${usage.metrics.agents} 个子任务`}
            />
            <UsageMetric
              label="缓存命中率"
              value={formatPercent(usage.metrics.cache_hit_rate)}
              detail={`读取 ${formatTokenCount(usage.metrics.cache_read_tokens)} / 提示 ${formatTokenCount(usage.metrics.prompt_tokens)}`}
            />
            <UsageMetric
              label="缓存写入"
              value={usage.metrics.cache_creation_tokens}
              detail="供后续请求复用"
            />
          </div>
        ) : (
          <div className="settings-empty">加载中…</div>
        )}
      </SettingsSection>

      {usage ? (
        <>
          <SettingsSection
            title="模型使用"
            description="用过的模型服务、模型名称和缓存命中情况。"
          >
            <div className="settings-card">
              {usage.model_breakdowns.length > 0 ? (
                <div className="settings-usage-table-wrap">
                  <table className="settings-usage-table">
                    <thead>
                      <tr>
                        <th scope="col">模型</th>
                        <th scope="col" className="settings-usage-num">上下文</th>
                        <th scope="col" className="settings-usage-num">缓存命中</th>
                        <th scope="col" className="settings-usage-num">输入</th>
                        <th scope="col" className="settings-usage-num">输出</th>
                      </tr>
                    </thead>
                    <tbody>
                      {usage.model_breakdowns.map((b) => {
                        const ctx = b.input_tokens + b.cache_read_tokens + b.output_tokens;
                        const prompt = b.input_tokens + b.cache_read_tokens;
                        const rate = prompt > 0 ? b.cache_read_tokens / prompt : undefined;
                        return (
                          <tr key={`${b.provider}\n${b.model}`}>
                            <td>
                              <strong>{b.provider || "(未知服务)"}</strong>
                              <small>{b.model || "(未知模型)"}</small>
                            </td>
                            <td className="settings-usage-num">{formatTokenCount(ctx)}</td>
                            <td className="settings-usage-num">
                              <span className={`settings-usage-rate rate-${hitRateLevel(rate)}`}>
                                {formatPercent(rate)}
                              </span>
                            </td>
                            <td className="settings-usage-num">{formatTokenCount(b.input_tokens)}</td>
                            <td className="settings-usage-num">{formatTokenCount(b.output_tokens)}</td>
                          </tr>
                        );
                      })}
                    </tbody>
                  </table>
                </div>
              ) : (
                <div className="settings-empty">暂无用量记录</div>
              )}
            </div>
          </SettingsSection>

          <SettingsSection
            title="缓存命中热力图"
            description="最近 12 周每天的缓存命中率。"
          >
            <div className="settings-card settings-cache-heatmap-card">
              <div className="settings-cache-heatmap-summary">
                <span className="settings-cache-heatmap-summary-value">
                  {formatPercent(usage.metrics.cache_hit_rate)}
                </span>
                <span className="settings-cache-heatmap-summary-label">
                  整体缓存命中率
                </span>
                <span className="settings-cache-heatmap-summary-detail">
                  活跃 {usage.metrics.active_days} 天 · 读取 {formatTokenCount(usage.metrics.cache_read_tokens)} · 写入 {formatTokenCount(usage.metrics.cache_creation_tokens)}
                </span>
              </div>
              <div
                className="settings-cache-heatmap"
                aria-label="缓存命中率热力图"
                role="grid"
                style={{ "--heatmap-cols": heatmapWeeks } as CSSProperties}
              >
                {heatmap.map((day) => (
                  <span
                    className="settings-cache-heatmap-cell"
                    data-level={day.level}
                    key={day.date}
                    role="gridcell"
                    title={formatHeatmapTitle(day)}
                    aria-label={formatHeatmapTitle(day)}
                  />
                ))}
              </div>
              <div className="settings-cache-heatmap-legend" aria-hidden="true">
                <span>低</span>
                {[0, 1, 2, 3, 4].map((level) => (
                  <i data-level={level} key={level} />
                ))}
                <span>高</span>
              </div>
            </div>
          </SettingsSection>
        </>
      ) : null}
    </>
  );
}

function UsageMetric({ label, value, detail }: { label: string; value: number | string; detail: string }): JSX.Element {
  return (
    <div className="settings-usage-metric">
      <span className="settings-usage-metric-label">{label}</span>
      <strong className="settings-usage-metric-value">
        {typeof value === "number" ? formatTokenCount(value) : value}
      </strong>
      <small className="settings-usage-metric-detail">{detail}</small>
    </div>
  );
}

/* -------------------------------------------------------------------------- */
/*  Helpers (kept at module scope, no behavior change)                         */
/* -------------------------------------------------------------------------- */

type AdvancedDraft = {
  autoCompact: boolean;
  compactThreshold: string;
  compactKeepRecent: string;
  providerContextWindow: string;
  maxContextTokens: string;
  maxSteps: string;
  temperature: string;
};

function settingsPageMeta(page: SettingsPage): { title: string; description: string } {
  switch (page) {
    case "providers":
      return {
        title: "模型服务",
        description: "配置 BYOK provider、模型和连接信息；切换不会丢失现有设置。"
      };
    case "advanced":
      return {
        title: "高级",
        description: "调整上下文窗口、压缩触发点和 Agent 单轮运行预算。"
      };
    case "general":
      return {
        title: "常规",
        description: "查看模型工具面、MCP 服务器状态和构建信息。"
      };
    case "usage":
      return {
        title: "用量",
        description: "本地会话的 token 消耗、缓存命中和模型使用情况。"
      };
  }
}

function parseAdvancedSettingsDraft(draft: AdvancedDraft): { settings: RuntimeAdvancedSettingsUpdate; error?: string } {
  const compactPercent = parseOptionalNumber(draft.compactThreshold, "压缩触发阈值");
  if (compactPercent.error) {
    return { settings: {}, error: compactPercent.error };
  }
  if (compactPercent.value < 0 || compactPercent.value >= 100) {
    return { settings: {}, error: "压缩触发阈值必须小于 100" };
  }
  const compactKeepRecent = parseOptionalInteger(draft.compactKeepRecent, "保留最近上下文");
  if (compactKeepRecent.error) {
    return { settings: {}, error: compactKeepRecent.error };
  }
  const providerContextWindow = parseOptionalInteger(draft.providerContextWindow, "当前服务上下文窗口");
  if (providerContextWindow.error) {
    return { settings: {}, error: providerContextWindow.error };
  }
  const maxContextTokens = parseOptionalInteger(draft.maxContextTokens, "未知模型窗口");
  if (maxContextTokens.error) {
    return { settings: {}, error: maxContextTokens.error };
  }
  const maxSteps = parseOptionalInteger(draft.maxSteps, "最大步数");
  if (maxSteps.error) {
    return { settings: {}, error: maxSteps.error };
  }
  const temperature = parseRequiredNumber(draft.temperature, "Temperature");
  if (temperature.error) {
    return { settings: {}, error: temperature.error };
  }
  if (temperature.value < 0 || temperature.value > 2) {
    return { settings: {}, error: "Temperature 必须在 0 到 2 之间" };
  }
  return {
    settings: {
      disable_auto_compact: !draft.autoCompact,
      compact_threshold_pct: compactPercent.value > 0 ? compactPercent.value / 100 : 0,
      compact_keep_recent_tokens: compactKeepRecent.value,
      provider_context_window: providerContextWindow.value,
      max_context_tokens: maxContextTokens.value,
      max_steps: maxSteps.value,
      temperature: temperature.value,
    },
  };
}

function parseOptionalInteger(raw: string, label: string): { value: number; error?: string } {
  const parsed = parseOptionalNumber(raw, label);
  if (parsed.error) {
    return parsed;
  }
  if (!Number.isInteger(parsed.value)) {
    return { value: 0, error: `${label} 必须是整数` };
  }
  return parsed;
}

function parseOptionalNumber(raw: string, label: string): { value: number; error?: string } {
  const value = raw.trim();
  if (value === "") {
    return { value: 0 };
  }
  const parsed = Number(value);
  if (!Number.isFinite(parsed) || parsed < 0) {
    return { value: 0, error: `${label} 必须是非负数字` };
  }
  return { value: parsed };
}

function parseRequiredNumber(raw: string, label: string): { value: number; error?: string } {
  const value = raw.trim();
  if (value === "") {
    return { value: 0, error: `${label} 不能为空` };
  }
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) {
    return { value: 0, error: `${label} 必须是数字` };
  }
  return { value: parsed };
}

function formatPercentDraft(value: number | undefined): string {
  if (!value || !Number.isFinite(value) || value <= 0) {
    return "";
  }
  return String(Math.round(value * 100));
}

function formatOptionalNumberDraft(value: number | undefined): string {
  if (!value || !Number.isFinite(value) || value <= 0) {
    return "";
  }
  return String(value);
}

function formatTemperatureDraft(value: number | undefined): string {
  if (value === undefined || !Number.isFinite(value)) {
    return "0.2";
  }
  return String(value);
}

function advancedContextSourceLabel(source: string | undefined): string {
  switch (source) {
    case "provider_context_window":
      return "来自当前服务覆盖";
    case "provider_model_limit":
      return "来自模型配置";
    case "agent_max_context_tokens":
      return "来自未知模型窗口";
    case "built_in_registry":
      return "来自内置模型库";
    case "unknown":
    case "":
    case undefined:
      return "未识别，主动压缩只依赖服务错误恢复";
    default:
      return source;
  }
}

function providerConnectionStatus(provider: ProviderSummary): string {
  if (provider.connection_locked) {
    return "OAuth";
  }
  return provider.api_key_configured ? "API key 已配置" : "缺少 API key";
}

function providerTypeLabel(provider: ProviderSummary): string {
  const type = provider.type.trim() || "openai-compatible";
  return provider.connection_locked ? "OAuth 管理的服务" : type;
}

type CacheHeatmapCell = SettingsUsageDay & {
  level: number;
};

function formatTokenCount(value: number): string {
  return Math.max(0, value).toLocaleString();
}

function formatUsageRange(range: SettingsUsageRange): string {
  switch (range) {
    case "all":
      return "全部";
    case "7d":
      return "7 天";
    case "30d":
      return "30 天";
    case "90d":
      return "90 天";
  }
}

function formatPercent(value: number | undefined): string {
  if (value === undefined || !Number.isFinite(value)) {
    return "—";
  }
  return `${Math.round(Math.max(0, Math.min(1, value)) * 100)}%`;
}

function hitRateLevel(rate: number | undefined): number {
  if (rate === undefined || rate <= 0) return 0;
  if (rate < 0.25) return 1;
  if (rate < 0.5) return 2;
  if (rate < 0.75) return 3;
  return 4;
}

function buildCacheHeatmap(days: SettingsUsageDay[]): CacheHeatmapCell[] {
  const byDate = new Map(days.map((day) => [day.date, day]));
  const end = startOfLocalDay(new Date());
  const start = startOfWeek(addDays(end, -77));
  const cells: CacheHeatmapCell[] = [];
  for (let cursor = start; cursor.getTime() <= end.getTime(); cursor = addDays(cursor, 1)) {
    const date = localDateKey(cursor);
    const day = byDate.get(date) ?? {
      date,
      input_tokens: 0,
      output_tokens: 0,
      cache_creation_tokens: 0,
      cache_read_tokens: 0,
      cache_hit_rate: 0,
      turns: 0,
      agents: 0,
    };
    cells.push({
      ...day,
      level: heatmapLevel(day),
    });
  }
  return cells;
}

function heatmapLevel(day: SettingsUsageDay): number {
  if (!hasUsageDayData(day)) {
    return 0;
  }
  return hitRateLevel(day.cache_hit_rate);
}

function hasUsageDayData(day: SettingsUsageDay): boolean {
  return (
    day.input_tokens > 0 ||
    day.output_tokens > 0 ||
    day.cache_creation_tokens > 0 ||
    day.cache_read_tokens > 0 ||
    day.turns > 0 ||
    day.agents > 0
  );
}

function formatHeatmapTitle(day: CacheHeatmapCell): string {
  if (!hasUsageDayData(day)) {
    return `${day.date}：暂无用量`;
  }
  return `${day.date}：缓存命中 ${formatPercent(day.cache_hit_rate)}，读取 ${formatTokenCount(day.cache_read_tokens)}，写入 ${formatTokenCount(day.cache_creation_tokens)}`;
}



function startOfLocalDay(date: Date): Date {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate());
}

function startOfWeek(date: Date): Date {
  const day = date.getDay();
  return addDays(startOfLocalDay(date), -day);
}

function addDays(date: Date, days: number): Date {
  const out = new Date(date);
  out.setDate(out.getDate() + days);
  return out;
}

function localDateKey(date: Date): string {
  const year = date.getFullYear();
  const month = `${date.getMonth() + 1}`.padStart(2, "0");
  const day = `${date.getDate()}`.padStart(2, "0");
  return `${year}-${month}-${day}`;
}

function formatBuildDate(iso: string): string {
  // The build date is a UTC ISO timestamp; render in a compact local form
  // so the user can correlate it with their clock without doing TZ math.
  const parsed = new Date(iso);
  if (Number.isNaN(parsed.getTime())) {
    return iso;
  }
  return parsed.toISOString().replace("T", " ").replace(/\.\d{3}Z$/, "Z");
}

function formatCoreBuild(core: InitializeResult["core"]): string {
  if (!core || !core.version) {
    return "未连接";
  }
  const pieces: string[] = [`v${core.version}`];
  if (core.commit) {
    pieces.push(core.dirty ? `${core.commit}-dirty` : core.commit);
  }
  if (core.date) {
    pieces.push(formatBuildDate(core.date));
  }
  return pieces.join(" · ");
}

function formatSurfaceRuntime(initialized: InitializeResult | undefined): string {
  if (!initialized) {
    return "未连接";
  }
  const provider = initialized.tool_surface?.provider || initialized.model_profile?.provider || initialized.provider;
  const model = initialized.tool_surface?.model || initialized.model_profile?.model || initialized.model;
  return `${provider} · ${model}`;
}

function formatToolSurfaceCounts(initialized: InitializeResult | undefined): string {
  const surface = initialized?.tool_surface;
  if (!surface) {
    return "未连接";
  }
  const visible = surface.tool_names.length;
  const hidden = surface.hidden_tool_names.length;
  return `${visible} 个工具可见，${hidden} 个已隐藏`;
}

function formatToolSurfaceCapabilities(initialized: InitializeResult | undefined): string {
  const capabilities = initialized?.tool_surface?.capabilities ?? [];
  if (capabilities.length === 0) {
    return "—";
  }
  const shown = capabilities.slice(0, 4).join("、");
  return capabilities.length > 4 ? `${shown} 等 ${capabilities.length} 项` : shown;
}

function upsertMCPServerStatus(servers: MCPServerStatus[], status: MCPServerStatus): MCPServerStatus[] {
  const next = [...servers];
  const index = next.findIndex((item) => item.name === status.name);
  if (index >= 0) {
    next[index] = status;
  } else {
    next.push(status);
  }
  next.sort((a, b) => a.name.localeCompare(b.name));
  return next;
}

function formatMCPServerMeta(server: MCPServerStatus): string {
  const pieces = [`${server.tool_count ?? 0} 个工具`];
  if (server.auth_status && server.auth_status !== "unsupported") {
    pieces.push(mcpAuthLabel(server.auth_status));
  }
  return pieces.join(" · ");
}

function mcpStateLabel(state: string): string {
  switch (state) {
    case "connected":
      return "已连接";
    case "connecting":
      return "连接中";
    case "failed":
      return "失败";
    case "disabled":
      return "已断开";
    case "needs_auth":
      return "需认证";
    case "needs_client_registration":
      return "需注册";
    case "configured":
      return "已配置";
    default:
      return state || "未知";
  }
}

function mcpStateTone(state: string): string {
  switch (state) {
    case "connected":
      return "success";
    case "failed":
    case "needs_auth":
    case "needs_client_registration":
      return "danger";
    case "connecting":
      return "warning";
    default:
      return "neutral";
  }
}

function mcpAuthLabel(status: string): string {
  switch (status) {
    case "bearer_token":
      return "Header 认证";
    case "not_logged_in":
      return "未登录";
    case "oauth":
      return "OAuth";
    default:
      return status;
  }
}

function formatExtensionTrust(trust?: ExtensionTrustSummary): string {
  if (!trust) {
    return "未连接";
  }
  const active: string[] = [];
  appendExtensionSurface(active, "MCP", trust.main_session?.mcp);
  appendExtensionSurface(active, "Hooks", trust.main_session?.hooks);
  appendExtensionSurface(active, "Plugins", trust.main_session?.plugins);
  appendExtensionSurface(active, "Skills", trust.main_session?.skills);
  appendExtensionSurface(active, "Workflows", trust.main_session?.workflows);
  const mainSummary = active.length > 0 ? active.join("，") : "无活跃扩展";
  const reviewerSummary = extensionSessionAllowsAny(trust.reviewer_session) ? "Reviewer：允许部分扩展" : "Reviewer：关闭扩展";
  return `主会话：${mainSummary} · ${reviewerSummary}`;
}

function appendExtensionSurface(parts: string[], label: string, surface?: ExtensionSurfaceTrustSummary): void {
  if (!surface?.active) {
    return;
  }
  const count = surface.count ?? surface.known_tools ?? surface.visible_tools ?? 0;
  const disabled = surface.allowed ? "" : "（已禁用）";
  parts.push(count > 0 ? `${label} ${count}${disabled}` : `${label}${disabled}`);
}

function extensionSessionAllowsAny(session?: ExtensionSessionTrustSummary): boolean {
  return Boolean(
    session?.mcp?.allowed ||
      session?.hooks?.allowed ||
      session?.plugins?.allowed ||
      session?.skills?.allowed ||
      session?.workflows?.allowed ||
      session?.external_tools?.allowed
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
