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
  useRef,
  useState
} from "react";
import type {
  DesktopBuildInfo,
  InitializeResult,
  MCPServerStatus,
  ProviderSummary,
  RuntimeConnectionUpdate,
  SettingsUsageDay,
  SettingsUsageResponse
} from "../shared/protocol";
import { normalizedVariantForProviderModel, providerModelReasoningMode, providerModelVariantOptions, variantLabel } from "./RuntimeHelpers";

export type SettingsPage = "providers" | "advanced" | "general" | "usage";

type CopyState = "idle" | "copying" | "copied";

const COPY_RESET_MS = 1500;

export function SettingsView({
  initialized,
  initialPage,
  running,
  usage,
  runningProviderNames,
  showDebugControlsSetting,
  debugControlsEnabled,
  sidebarWidth,
  sidebarMinWidth,
  sidebarMaxWidth,
  resizingSidebar,
  onBack,
  onSave,
  onRemoveProvider,
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
  runningProviderNames?: readonly string[];
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
  onRemoveProvider: (provider: string) => Promise<void>;
  onAdvancedSave: (settings: RuntimeAdvancedSettingsUpdate) => Promise<void>;
  onDebugControlsChange: (enabled: boolean) => void;
  onSidebarResizeStart: (event: ReactPointerEvent<HTMLDivElement>) => void;
  onSidebarSeparatorKey: (event: ReactKeyboardEvent<HTMLDivElement>) => void;
  onSidebarSeparatorDoubleClick: () => void;
}): JSX.Element {
  const providers = initialized?.providers ?? [];
  const runningProviderNameSet = useMemo(
    () => new Set((runningProviderNames ?? []).map((name) => name.trim()).filter(Boolean)),
    [runningProviderNames],
  );
  const [providerDraft, setProviderDraft] = useState(initialized?.provider ?? "");
  const [modelDraft, setModelDraft] = useState(initialized?.model ?? "");
  const [variantDraft, setVariantDraft] = useState(initialized?.variant ?? initialized?.effort ?? "");
  const [baseURLDraft, setBaseURLDraft] = useState("");
  const [apiKeyDraft, setAPIKeyDraft] = useState("");
  // Draft for the protocol type of a brand-new provider (only used while
  // addingProvider is true). Defaults to "openai-compatible" to preserve
  // the historical behavior; the user can switch to "anthropic" before
  // saving.
  const [providerTypeDraft, setProviderTypeDraft] = useState("openai-compatible");
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
  const [copyState, setCopyState] = useState<CopyState>("idle");

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
    // Reset the type draft: it is only meaningful when creating a provider,
    // and leaving add mode via card click should drop any pending type pick.
    setProviderTypeDraft("openai-compatible");
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
    setProviderTypeDraft("openai-compatible");
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
    setProviderTypeDraft("openai-compatible");
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
      const providerType = addingProvider ? providerTypeDraft : selectedProvider?.type;
      const usesAuthToken = isAnthropicProviderType(providerType);
      if (addingProvider) {
        connection = {
          base_url: baseURLDraft.trim(),
          type: providerTypeDraft,
          create_provider: true
        };
        if (usesAuthToken) {
          connection.auth_token = apiKeyDraft.trim();
        } else {
          connection.api_key = apiKeyDraft.trim();
        }
      } else if (!connectionLocked) {
        connection = {
          base_url: baseURLDraft.trim()
        };
        const apiKey = apiKeyDraft.trim();
        if (apiKey) {
          if (usesAuthToken) {
            connection.auth_token = apiKey;
          } else {
            connection.api_key = apiKey;
          }
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

  async function requestRemoveProvider(name: string): Promise<void> {
    if (running || !name) {
      return;
    }
    setError("");
    setSaved(false);
    try {
      await onRemoveProvider(name);
      // The parent's state update will refresh initialized.providers
      // and active provider/model; sync the local drafts so the form
      // does not show the now-deleted provider as selected.
      setAddingProvider(false);
      setSaved(true);
    } catch (removeError) {
      setError(
        removeError instanceof Error
          ? removeError.message
          : "删除服务失败",
      );
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

  async function copyVersionInfo(): Promise<void> {
    if (!desktopBuild || copyState === "copying") {
      return;
    }
    setCopyState("copying");
    const pieces = [`wuu ${versionLabel(desktopBuild.version)}`];
    if (desktopBuild.date) {
      pieces.push(formatBuildDate(desktopBuild.date));
    }
    if (core?.version) {
      pieces.push(`core ${versionLabel(core.version)}`);
    }
    const text = pieces.join(" · ");
    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(text);
      } else {
        const fallback = document.createElement("textarea");
        fallback.value = text;
        fallback.setAttribute("readonly", "");
        fallback.style.position = "absolute";
        fallback.style.left = "-9999px";
        document.body.appendChild(fallback);
        fallback.select();
        document.execCommand("copy");
        document.body.removeChild(fallback);
      }
      setCopyState("copied");
      window.setTimeout(() => setCopyState("idle"), COPY_RESET_MS);
    } catch {
      setCopyState("idle");
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

  const pageMeta = settingsPageMeta(
    activePage,
    showDebugControlsSetting,
    debugControlsEnabled
  );

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
              providerTypeDraft={providerTypeDraft}
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
              onProviderTypeDraftChange={(value) => {
                setProviderTypeDraft(value);
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
              onRemoveProvider={requestRemoveProvider}
              runningProviderNames={runningProviderNameSet}
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
              providerContextWindowCurrent={formatOptionalTokenCount(
                initialized?.advanced_settings?.context_window_tokens,
              )}
              providerContextWindowSource={advancedContextSourceLabel(
                initialized?.advanced_settings?.context_window_source,
              )}
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
              showDebugControlsSetting={showDebugControlsSetting}
              debugControlsEnabled={debugControlsEnabled}
              mcpServers={mcpServers}
              mcpLoading={mcpLoading}
              mcpError={mcpError}
              mcpBusyServer={mcpBusyServer}
              onDebugControlsChange={onDebugControlsChange}
              onMCPAction={runMCPAction}
              copyState={copyState}
              onCopyVersion={copyVersionInfo}
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
  providerTypeDraft,
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
  onProviderTypeDraftChange,
  onModelDraftChange,
  onVariantDraftChange,
  onBaseURLDraftChange,
  onAPIKeyDraftChange,
  onSubmit,
  onRemoveProvider,
  runningProviderNames,
  disabled
}: {
  providers: ProviderSummary[];
  providerLabels: Map<string, string>;
  running: boolean;
  providerDraft: string;
  providerTypeDraft: string;
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
  onProviderTypeDraftChange: (value: string) => void;
  onModelDraftChange: (value: string) => void;
  onVariantDraftChange: (value: string) => void;
  onBaseURLDraftChange: (value: string) => void;
  onAPIKeyDraftChange: (value: string) => void;
  onSubmit: (event: ReactFormEvent<HTMLFormElement>) => Promise<void>;
  onRemoveProvider?: (provider: string) => Promise<void> | void;
  runningProviderNames: ReadonlySet<string>;
  disabled: boolean;
}): JSX.Element {
  const reasoningMode = providerModelReasoningMode(selectedProvider, modelDraft);
  const authFieldUsesToken = isAnthropicProviderType(addingProvider ? providerTypeDraft : selectedProvider?.type);
  const authFieldLabel = authFieldUsesToken ? "Auth token" : "API key";
  return (
    <SettingsSection
      title="模型服务"
      description=""
      testID="settings-providers"
    >
      {providers.length > 0 ? (
        <div className="settings-provider-overview" data-testid="settings-provider-overview">
          {providers.map((provider) => (
            <div className="settings-provider-card" key={provider.name}>
              <button
                className={`settings-provider-button${!addingProvider && providerDraft === provider.name ? " active" : ""}`}
                type="button"
                disabled={running}
                onClick={() => onProviderChange(provider.name)}
              >
                <strong>{providerServiceLabel(provider)}</strong>
                <small>{provider.name}</small>
                <small>{provider.model || "未选择模型"}</small>
                <small>{providerConnectionStatus(provider)}</small>
              </button>
              {onRemoveProvider && !provider.connection_locked ? (
                <button
                  className="settings-provider-remove"
                  type="button"
                  aria-label={`删除 ${providerServiceLabel(provider)}`}
                  title="删除这个模型服务"
                  disabled={running}
                  onClick={(event) => {
                    event.stopPropagation();
                    if (runningProviderNames.has(provider.name.trim())) {
                      window.alert("这个模型服务正在被运行中的会话使用，等当前回复结束后再删除。");
                      return;
                    }
                    if (
                      typeof window !== "undefined" &&
                      typeof window.confirm === "function" &&
                      !window.confirm(
                        `确定要删除 “${providerServiceLabel(provider)}” 吗?这个操作会从配置中移除该服务。`,
                      )
                    ) {
                      return;
                    }
                    void onRemoveProvider(provider.name);
                  }}
                >
                  <X className="icon" />
                </button>
              ) : null}
            </div>
          ))}
          {onStartAddingProvider ? (
            <button
              className="settings-provider-add-card"
              type="button"
              data-testid="settings-provider-add-card"
              disabled={running || addingProvider}
              onClick={onStartAddingProvider}
            >
              <Plus className="icon-lg" />
              <span>新增服务</span>
            </button>
          ) : null}
        </div>
      ) : onStartAddingProvider ? (
        <button
          className="settings-provider-add-card settings-provider-add-card-empty"
          type="button"
          data-testid="settings-provider-add-card"
          disabled={running || addingProvider}
          onClick={onStartAddingProvider}
        >
          <Plus className="icon-lg" />
          <span>新增服务</span>
        </button>
      ) : null}
      <form className="settings-card" onSubmit={onSubmit}>
        <SettingsRow
          title={addingProvider ? "新增服务" : "选择当前会话使用的服务"}
          description={addingProvider ? "选择协议并填写连接信息;Base URL 和 API key 会保存到本地配置。" : "切换 service 不会丢失 Base URL 和 API key。"}
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
                  <span>新增服务</span>
                </>
              )}
            </button>
          </div>
        </SettingsRow>
        {addingProvider ? (
          <SettingsRow
            title="服务类型"
            description="选择协议,后续的请求会按这个协议走。"
          >
            <select
              className="settings-select"
              value={providerTypeDraft}
              onChange={(event) => onProviderTypeDraftChange(event.target.value)}
              disabled={running}
              data-testid="settings-provider-type-select"
            >
              <option value="openai-compatible">OpenAI 兼容</option>
              <option value="anthropic">Anthropic 兼容</option>
            </select>
          </SettingsRow>
        ) : null}
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
          description={
            reasoningMode === "levels"
              ? "当前模型支持的参数档位"
              : reasoningMode === "toggle"
                ? "可关闭思考"
                : "当前模型不支持思考"
          }
          block
        >
          <select
            className="settings-select"
            value={variantDraft}
            onChange={(event) => onVariantDraftChange(event.target.value)}
            disabled={running || reasoningMode === "off"}
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
          title={authFieldLabel}
          description={
            connectionLocked
              ? "由 OpenAI OAuth 管理"
              : addingProvider
                ? "保存时写入这个服务"
                : selectedProvider?.api_key_configured
                  ? "已配置，留空不修改"
                  : authFieldUsesToken
                    ? "用于 Bearer 鉴权"
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
                ? `不需要 ${authFieldLabel}`
                : addingProvider
                  ? `输入 ${authFieldLabel}`
                  : selectedProvider?.api_key_configured
                    ? "留空保持当前密钥"
                    : `输入 ${authFieldLabel}`
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
  providerContextWindowCurrent,
  providerContextWindowSource,
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
  providerContextWindowCurrent: string;
  providerContextWindowSource: string;
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
      description="调整上下文窗口、压缩策略与单轮运行上限。"
      testID="settings-advanced"
    >
      <form className="settings-card" onSubmit={onSubmit}>
        <SettingsRow
          title="自动压缩"
          description="接近上下文上限时自动整理旧历史"
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
          description="留空使用模型窗口；数值越小越早压缩"
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
          description="留空使用默认 20,000 token"
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
          title="当前服务上下文上限"
          description={`自定义模型或网关别名时使用；${providerContextWindowSource}${
            providerContextWindowCurrent ? `；当前 ${providerContextWindowCurrent} token` : ""
          }`}
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
          title="未知模型上限"
          description="Provider 未覆盖该模型时使用"
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
          title="最大步数"
          description="0 不限"
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
  showDebugControlsSetting,
  debugControlsEnabled,
  mcpServers,
  mcpLoading,
  mcpError,
  mcpBusyServer,
  onDebugControlsChange,
  onMCPAction,
  copyState,
  onCopyVersion
}: {
  initialized: InitializeResult | undefined;
  desktopBuild: DesktopBuildInfo | undefined;
  showDebugControlsSetting: boolean;
  debugControlsEnabled: boolean;
  mcpServers: MCPServerStatus[];
  mcpLoading: boolean;
  mcpError: string;
  mcpBusyServer: string;
  onDebugControlsChange: (enabled: boolean) => void;
  onMCPAction: (name: string, action: "connect" | "disconnect" | "refresh") => Promise<void>;
  copyState: CopyState;
  onCopyVersion: () => Promise<void>;
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

      {showDebugControlsSetting && debugControlsEnabled ? (
        <SettingsSection
          title="工具"
          description="当前模型可调用的能力和文件编辑方式。"
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
              description={initialized?.tool_surface?.bash_first ? "终端命令默认走 bash" : "按模型原生方式编辑"}
            >
              <span className="settings-row-control-value">
                {initialized?.tool_surface?.edit_primitive ?? initialized?.model_profile?.edit_primitive ?? "—"}
              </span>
            </SettingsRow>
            <SettingsRow
              title="可用工具"
              description={formatToolSurfaceCounts(initialized)}
              block
            >
              <span className="settings-row-control-value">
                {formatToolSurfaceCapabilities(initialized)}
              </span>
            </SettingsRow>
          </SettingsCard>
        </SettingsSection>
      ) : null}

      <SettingsSection
        title="MCP"
        description="外部 MCP 服务器的连接状态。"
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
        description="查看当前版本号，或复制版本信息用于反馈问题。"
        testID="settings-about"
      >
        <SettingsCard>
          <SettingsRow
            title="版本"
            description="桌面端当前构建版本"
          >
            <span className="settings-row-control-value">
              {desktopBuild ? versionLabel(desktopBuild.version) : "加载中…"}
            </span>
          </SettingsRow>
          <SettingsRow
            title="复制版本信息"
            description="反馈问题时附上这段文字。"
            block
          >
            <button
              className="settings-button"
              type="button"
              onClick={() => void onCopyVersion()}
              disabled={!desktopBuild || copyState === "copying"}
            >
              {copyState === "copied" ? "已复制" : "复制"}
            </button>
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
  const heatmapCols = heatmap.length > 0 ? Math.ceil(heatmap.length / 7) : 12;

  // Keep grid height = 7 × cell-size so cells stay square as panel resizes
  const heatmapRef = useRef<HTMLDivElement>(null);
  const [heatmapHeight, setHeatmapHeight] = useState<number | undefined>(undefined);
  useEffect(() => {
    const el = heatmapRef.current;
    if (!el) return;
    const GAP = 3;
    const update = () => {
      const cellW = (el.offsetWidth - (heatmapCols - 1) * GAP) / heatmapCols;
      setHeatmapHeight(7 * cellW + 6 * GAP);
    };
    update();
    if (typeof ResizeObserver === "undefined") {
      return;
    }
    const ro = new ResizeObserver(update);
    ro.observe(el);
    return () => ro.disconnect();
  }, [heatmapCols]);

  // Build month label positions: for each column (week), find the first day;
  // when the month changes from the previous column, record (colIndex, monthLabel).
  const monthLabels: { col: number; label: string }[] = [];
  if (heatmap.length > 0) {
    let prevMonth = -1;
    for (let col = 0; col < heatmapCols; col++) {
      const firstDayIdx = col * 7;
      if (firstDayIdx < heatmap.length) {
        const d = new Date(heatmap[firstDayIdx].date);
        const month = d.getMonth();
        if (month !== prevMonth) {
          // Skip the very first column if it's at the left edge — avoid label clipping
          if (col > 0) {
            monthLabels.push({ col, label: `${d.getMonth() + 1}月` });
          }
          prevMonth = month;
        }
      }
    }
  }
  return (
    <div className="settings-usage-page" data-testid="settings-usage">
      <div className="settings-usage-toolbar">
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
        {usage && (
          <div className="settings-usage-stats">
            <UsageStat label="输入" value={formatTokenCount(usage.metrics.input_tokens)} />
            <UsageStat label="上下文" value={formatTokenCount(usage.metrics.context_tokens)} />
            <UsageStat label="输出" value={formatTokenCount(usage.metrics.output_tokens)} />
            <UsageStat label="缓存命中率" value={formatPercent(usage.metrics.cache_hit_rate)} />
            <UsageStat label="活跃" value={`${usage.metrics.active_days} 天`} />
          </div>
        )}
      </div>

      <div className="settings-heatmap-panel">
        {/* Month labels row */}
        <div
          className="settings-heatmap-months"
          aria-hidden="true"
          style={{ "--heatmap-cols": heatmapCols } as CSSProperties}
        >
          {monthLabels.map(({ col, label }) => (
            <span
              key={col}
              className="settings-heatmap-month-label"
              style={{ gridColumn: col + 1 } as CSSProperties}
            >
              {label}
            </span>
          ))}
        </div>
        {/* Grid */}
        <div
          ref={heatmapRef}
          className="settings-cache-heatmap"
          aria-label="缓存命中率热力图"
          role="grid"
          style={{
            "--heatmap-cols": heatmapCols,
            ...(heatmapHeight !== undefined ? { height: `${heatmapHeight}px` } : {})
          } as CSSProperties}
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
        <div className="settings-heatmap-legend" aria-hidden="true">
          <span>少</span>
          {[0, 1, 2, 3, 4].map((level) => (
            <i className="settings-heatmap-legend-cell" data-level={level} key={level} />
          ))}
          <span>多</span>
        </div>
      </div>

      {usage ? (
        usage.model_breakdowns.length > 0 ? (
          <div className="settings-card settings-usage-table-wrap">
            <h2 className="settings-usage-table-title">模型使用</h2>
            <table className="settings-usage-table">
              <thead>
                <tr>
                  <th scope="col">模型</th>
                  <th scope="col" className="settings-usage-num">输入</th>
                  <th scope="col" className="settings-usage-num">输出</th>
                  <th scope="col" className="settings-usage-num">命中率</th>
                </tr>
              </thead>
              <tbody>
                {usage.model_breakdowns.map((b) => {
                  const prompt = b.input_tokens + b.cache_read_tokens;
                  const rate = prompt > 0 ? b.cache_read_tokens / prompt : undefined;
                  return (
                    <tr key={`${b.provider}\n${b.model}`}>
                      <td>
                        <strong>{b.provider || "(未知服务)"}</strong>
                        <small>{b.model || "(未知模型)"}</small>
                      </td>
                      <td className="settings-usage-num">{formatTokenCount(b.input_tokens)}</td>
                      <td className="settings-usage-num">{formatTokenCount(b.output_tokens)}</td>
                      <td className="settings-usage-num">
                        <span className={`settings-usage-rate rate-${hitRateLevel(rate)}`}>
                          {formatPercent(rate)}
                        </span>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        ) : (
          <div className="settings-empty">暂无用量记录</div>
        )
      ) : (
        <div className="settings-empty">加载中…</div>
      )}
    </div>
  );
}

function UsageStat({ label, value }: { label: string; value: string }): JSX.Element {
  return (
    <div className="settings-usage-stat">
      <span className="settings-usage-stat-value">{value}</span>
      <span className="settings-usage-stat-label">{label}</span>
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

function settingsPageMeta(
  page: SettingsPage,
  showDebugControlsSetting: boolean,
  debugControlsEnabled: boolean
): { title: string; description: string } {
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
        description: showDebugControlsSetting && debugControlsEnabled
          ? "查看可用工具、MCP 连接和当前版本。"
          : "查看 MCP 连接和当前版本。"
      };
    case "usage":
      return {
        title: "用量",
        description: ""
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
    case "provider_input_limit":
      return "来自当前通道输入上限";
    case "agent_max_context_tokens":
      return "来自手动上限";
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
  const label = isAnthropicProviderType(provider.type) ? "Auth token" : "API key";
  return provider.api_key_configured ? `${label} 已配置` : `缺少 ${label}`;
}

function providerTypeLabel(provider: ProviderSummary): string {
  const type = provider.type.trim() || "openai-compatible";
  return provider.connection_locked ? "OAuth 管理的服务" : type;
}

function isAnthropicProviderType(type: string | undefined): boolean {
  const normalized = (type ?? "").trim().toLowerCase().replaceAll("_", "-");
  return normalized === "anthropic" || normalized === "claude" || normalized === "anthropic-official";
}

type CacheHeatmapCell = SettingsUsageDay & {
  level: number;
};

function formatTokenCount(value: number): string {
  return Math.max(0, value).toLocaleString();
}

function formatOptionalTokenCount(value: number | undefined): string {
  if (!value || !Number.isFinite(value) || value <= 0) {
    return "";
  }
  return formatTokenCount(value);
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
  const start = startOfWeek(addDays(end, -364));
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

function versionLabel(version: string): string {
  const trimmed = version.trim();
  if (!trimmed) {
    return trimmed;
  }
  return trimmed.toLowerCase().startsWith("v") ? trimmed : `v${trimmed}`;
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
  return `${visible} 个可用，${hidden} 个已隐藏`;
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
  if (isAnthropicProviderType(type)) {
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
