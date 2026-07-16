import {
  ArrowLeft,
  Archive,
  BarChart3,
  Brain,
  Check,
  Folder,
  KeyRound,
  Plug,
  PlugZap,
  Plus,
  RefreshCw,
  Search,
  Settings,
  SlidersHorizontal,
  Smartphone,
  X
} from "lucide-react";
import type {
  CodexPetSettingsUpdate,
  RuntimeAdvancedSettingsUpdate,
  RuntimeGeneralSettingsUpdate,
  SettingsUsageRange,
} from "../shared/protocol";
import {
  type CSSProperties,
  type FormEvent as ReactFormEvent,
  type KeyboardEvent as ReactKeyboardEvent,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
  type RefObject,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState
} from "react";
import { SidePanelToggleIcon } from "./SidePanelToggleIcon";
import { useSidebarDrawerState } from "./SidebarDrawerState";
import { SelectMenu } from "./SelectMenu";
import type {
  CodexPetsSnapshot,
  DesktopBuildInfo,
  ExtensionInventoryRecord,
  InitializeResult,
  MCPAuthStartResult,
  MCPServerStatus,
  ParticipantProfile,
  ProviderSummary,
  RemoteControlSnapshot,
  RuntimeConnectionUpdate,
  SettingsUsageDay,
  SettingsUsageResponse
} from "../shared/protocol";

// 设置 → 归档页只读侧边栏归档会话的最小字段。
// `state.threads` 是 `Thread[]`（含 `title?: string`），而 ThreadSummary
// 额外要求 `turns` / `turn_count` 等计算字段，渲染层并不关心。
// 用结构性子集既兼容 Thread，也避免把 ThreadSummary 的派生语义
// 漏到 SettingsView。归档页只用于识别和恢复会话，不展示工作目录。
export type ArchivedSessionView = {
  id: string;
  title?: string;
  updated_at: string;
  archive_project_id?: string;
  archive_project_name?: string;
};
import { normalizedVariantForProviderModel, providerModelReasoningMode, providerModelVariantOptions, variantLabel } from "./RuntimeHelpers";
import { ENABLE_REMOTE_CONTROL } from "./FeatureFlags";
import { MemoryPanel } from "./MemoryPanel";
import { MessageFlowFontSizeControl } from "./MessageFlowFontSizeSection";
import { SettingsRemotePage } from "./SettingsRemotePage";
import { ThemePreferenceControl } from "./ThemePreferenceSection";

export type SettingsPage =
  | "providers"
  | "general"
  | "memory"
  | "advanced"
  | "usage"
  | "remote"
  | "archive";

type CopyState = "idle" | "copying" | "copied";

const COPY_RESET_MS = 1500;

function availableSettingsPage(page: SettingsPage | undefined): SettingsPage {
  const next = page ?? "providers";
  return next === "remote" && !ENABLE_REMOTE_CONTROL ? "providers" : next;
}

export function SettingsView({
  initialized,
  initialPage,
  memoryFocusParticipantID,
  running,
  usage,
  runningProviderNames,
  participants,
  showDebugControlsSetting,
  debugControlsEnabled,
  codexPets,
  codexPetsLoading,
  codexPetsError,
  sidebarWidth,
  sidebarMinWidth,
  sidebarMaxWidth,
  resizingSidebar,
  shellRef,
  onBack,
  onSave,
  onRemoveProvider,
  onAdvancedSave,
  onGeneralSave,
  onCodexPetsRefresh,
  onCodexPetsUpdate,
  onDebugControlsChange,
  onSidebarResizeStart,
  onSidebarSeparatorKey,
  usageRange,
  setUsageRange,
  archivedThreads,
  onUnarchiveThread,
  // The settings rail shares the main sidebar's state and handlers wholesale:
  // same persisted width + collapse flag, same drag-to-collapse resize
  // session, same toggle motion. `activeSessionTabID` is forwarded so the
  // drawer hook can reset its hover-open state when the user navigates
  // between settings pages, mirroring how the main view resets on session
  // tab swap.
  sidebarCollapsed,
  sidebarAnimating,
  onToggleSidebar,
  sidebarMotionMs,
  activeSessionTabID = "",
}: {
  initialized?: InitializeResult;
  initialPage?: SettingsPage;
  // 记忆页打开时预选的同事笔记本（档案面板跳转带过来）。
  memoryFocusParticipantID?: string;
  running: boolean;
  usage?: SettingsUsageResponse;
  runningProviderNames?: readonly string[];
  // 记忆页「同事」子 Tab 的数据源：现有 roster 状态（App 的 participants），
  // 面板内部只保留在职 named agent。
  participants?: ParticipantProfile[];
  usageRange: SettingsUsageRange;
  setUsageRange: (range: SettingsUsageRange) => void;
  showDebugControlsSetting: boolean;
  debugControlsEnabled: boolean;
  codexPets?: CodexPetsSnapshot;
  codexPetsLoading: boolean;
  codexPetsError: string;
  sidebarWidth: number;
  sidebarMinWidth: number;
  sidebarMaxWidth: number;
  resizingSidebar: boolean;
  shellRef?: RefObject<HTMLDivElement | null>;
  onBack: () => void;
  onSave: (provider: string, model: string, effort?: string, connection?: RuntimeConnectionUpdate, variant?: string) => Promise<void>;
  onRemoveProvider: (provider: string) => Promise<void>;
  onAdvancedSave: (settings: RuntimeAdvancedSettingsUpdate) => Promise<void>;
  onGeneralSave: (settings: RuntimeGeneralSettingsUpdate) => Promise<void>;
  onCodexPetsRefresh: () => Promise<CodexPetsSnapshot>;
  onCodexPetsUpdate: (settings: CodexPetSettingsUpdate) => Promise<CodexPetsSnapshot>;
  onDebugControlsChange: (enabled: boolean) => void;
  onSidebarResizeStart: (event: ReactPointerEvent<HTMLDivElement>) => void;
  onSidebarSeparatorKey: (event: ReactKeyboardEvent<HTMLDivElement>) => void;
  // 归档页只读侧边栏归档清单 + 恢复回调。列表为空时渲染空态卡片。
  archivedThreads?: readonly ArchivedSessionView[];
  onUnarchiveThread: (thread: ArchivedSessionView) => void;
  sidebarCollapsed: boolean;
  sidebarAnimating: boolean;
  onToggleSidebar: () => void;
  sidebarMotionMs: number;
  activeSessionTabID?: string;
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
  const [activePage, setActivePage] = useState<SettingsPage>(() =>
    availableSettingsPage(initialPage),
  );
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
  const [temperatureDraft, setTemperatureDraft] = useState("");
  const [advancedError, setAdvancedError] = useState("");
  const [advancedSaved, setAdvancedSaved] = useState(false);
  const [copyState, setCopyState] = useState<CopyState>("idle");
  const settingsScrollRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    setActivePage(availableSettingsPage(initialPage));
  }, [initialPage]);

  useLayoutEffect(() => {
    if (settingsScrollRef.current) {
      settingsScrollRef.current.scrollTop = 0;
    }
  }, [activePage]);

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

  async function startMCPAuth(name: string): Promise<MCPAuthStartResult | undefined> {
    setMCPBusyServer(name);
    setMCPError("");
    try {
      const result = await window.wuu.startMCPAuth(name);
      await window.wuu.openExternal(result.authorization_url);
      return result;
    } catch (err) {
      setMCPError(err instanceof Error ? err.message : String(err));
      return undefined;
    } finally {
      setMCPBusyServer("");
    }
  }

  async function finishMCPAuth(name: string, state: string, code: string): Promise<boolean> {
    setMCPBusyServer(name);
    setMCPError("");
    try {
      const result = await window.wuu.finishMCPAuth(name, state, code);
      setMCPServers((servers) => upsertMCPServerStatus(servers, result.server));
      return true;
    } catch (err) {
      setMCPError(err instanceof Error ? err.message : String(err));
      return false;
    } finally {
      setMCPBusyServer("");
    }
  }

  async function removeMCPAuth(name: string): Promise<void> {
    setMCPBusyServer(name);
    setMCPError("");
    try {
      const result = await window.wuu.removeMCPAuth(name);
      setMCPServers((servers) => upsertMCPServerStatus(servers, result.server));
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
    // Same variables as the main app shell (`--sidebar-width` collapses to 0,
    // `--sidebar-open-width` remembers the open width for the hover drawer)
    // so sidebar.css and settings.css read one vocabulary for both shells.
    "--sidebar-width": `${sidebarCollapsed ? 0 : sidebarWidth}px`,
    "--sidebar-open-width": `${sidebarWidth}px`,
    "--sidebar-motion-duration": `${sidebarMotionMs}ms`
  } as CSSProperties;

  // Mirror the main view's sidebar collapse/hover logic so the settings shell
  // behaves the same way: a persistent `sidebarCollapsed` flag hides the rail
  // and only the left-edge hover zone + the toggle button can reopen it as a
  // drawer overlay. `activePage` is used as the sync key so navigating between
  // settings pages resets any in-flight hover timer, just like switching
  // session tabs in the main view.
  const fallbackShellRef = useRef<HTMLDivElement>(null);
  const effectiveShellRef = shellRef ?? fallbackShellRef;
  const {
    sidebarDrawerPhase,
    sidebarHoverZoneRef,
    scheduleSidebarDrawerOpen,
    cancelSidebarDrawerOpen,
    openSidebarDrawer,
    closeSidebarDrawer
  } = useSidebarDrawerState({
    appShellRef: effectiveShellRef,
    sidebarCollapsed,
    resizingSidebar,
    activeSessionTabID: activeSessionTabID || activePage,
    motionMs: sidebarMotionMs,
    closeOnWindowResize: true
  });
  const shellClassName = `settings-shell${resizingSidebar ? " resizing-sidebar" : ""}${
    sidebarCollapsed ? " sidebar-collapsed" : ""
  }${
    sidebarCollapsed && sidebarDrawerPhase === "open" ? " sidebar-drawer-open" : ""
  }${
    sidebarCollapsed && sidebarDrawerPhase === "closing"
      ? " sidebar-drawer-closing"
      : ""
  }${sidebarAnimating ? " sidebar-animating" : ""}`;

  const pageTitle = settingsPageTitle(activePage);

  return (
    <div ref={effectiveShellRef} className={shellClassName} style={shellStyle}>
      <div
        ref={sidebarHoverZoneRef}
        className="sidebar-hover-zone"
        aria-hidden="true"
        onPointerEnter={scheduleSidebarDrawerOpen}
        onPointerLeave={cancelSidebarDrawerOpen}
      />
      <aside
        className="settings-sidebar"
        onPointerEnter={openSidebarDrawer}
        onPointerLeave={closeSidebarDrawer}
      >
        {/*
          * 与主侧栏一致的内层 .sidebar-content：折叠动画期间列宽收窄时，
          * 内容保持 --sidebar-open-width 的固定宽度被裁切（而不是被压扁），
          * 淡出/位移也复用 sidebar.css 里同一组规则。
          */}
        <div className="sidebar-content">
          <div className="traffic-spacer" />
          {/*
            * 同一个品牌占位，复制自主侧栏 AppSidebar 顶部的 .sidebar-brand。
            * 这里放在 settings-sidebar 的 traffic-spacer 与 返回应用 按钮之间，
            * 与主侧栏"traffic-spacer → 品牌 → 主操作"的相对位置一一对应，
            * 这样打开/关闭设置时品牌始终在同一个视觉位置。
            */}
          <div className="sidebar-brand">
            <span className="sidebar-brand-wordmark">wuu</span>
          </div>
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
            <SettingsNavItem icon={<Brain className="icon-lg" />} active={activePage === "memory"} onClick={() => setActivePage("memory")}>
              记忆
            </SettingsNavItem>
            <SettingsNavItem icon={<SlidersHorizontal className="icon-lg" />} active={activePage === "advanced"} onClick={() => setActivePage("advanced")}>
              高级
            </SettingsNavItem>
            <SettingsNavItem icon={<BarChart3 className="icon-lg" />} active={activePage === "usage"} onClick={() => setActivePage("usage")}>
              用量
            </SettingsNavItem>
            {ENABLE_REMOTE_CONTROL ? (
              <SettingsNavItem icon={<Smartphone className="icon-lg" />} active={activePage === "remote"} onClick={() => setActivePage("remote")}>
                远程
              </SettingsNavItem>
            ) : null}
            <SettingsNavItem icon={<Archive className="icon-lg" />} active={activePage === "archive"} onClick={() => setActivePage("archive")}>
              归档
            </SettingsNavItem>
          </nav>
        </div>
      </aside>
      {sidebarCollapsed ? null : (
        <div
          className="sidebar-resizer"
          role="separator"
          aria-label="调整设置侧边栏宽度"
          aria-orientation="vertical"
          aria-valuemin={sidebarMinWidth}
          aria-valuemax={sidebarMaxWidth}
          aria-valuenow={sidebarWidth}
          tabIndex={0}
          onPointerDown={onSidebarResizeStart}
          onDoubleClick={onToggleSidebar}
          onKeyDown={onSidebarSeparatorKey}
        />
      )}
      <main className="settings-main">
        <div className="settings-titlebar">
          <button
            type="button"
            className="icon-button side-panel-toggle-button sidebar-toggle-button settings-sidebar-toggle"
            aria-label={sidebarCollapsed ? "展开左侧栏" : "收起左侧栏"}
            aria-pressed={!sidebarCollapsed}
            onClick={onToggleSidebar}
          >
            <SidePanelToggleIcon side="left" open={!sidebarCollapsed} />
          </button>
        </div>
        <div ref={settingsScrollRef} className="settings-scroll">
          <div
            className={`settings-page${activePage === "archive" ? " settings-page-archive" : ""}`}
            key={activePage}
          >
            {activePage === "memory" ? null : (
              <header className="settings-page-header">
                <h1 className="settings-page-title">{pageTitle}</h1>
              </header>
            )}
  
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
                running={running}
                desktopBuild={desktopBuild}
                showDebugControlsSetting={showDebugControlsSetting}
                debugControlsEnabled={debugControlsEnabled}
                mcpServers={mcpServers}
                mcpLoading={mcpLoading}
                mcpError={mcpError}
                mcpBusyServer={mcpBusyServer}
                codexPets={codexPets}
                codexPetsLoading={codexPetsLoading}
                codexPetsError={codexPetsError}
                onDebugControlsChange={onDebugControlsChange}
                onGeneralSave={onGeneralSave}
                onMCPAction={runMCPAction}
                onMCPAuthStart={startMCPAuth}
                onMCPAuthFinish={finishMCPAuth}
                onMCPAuthRemove={removeMCPAuth}
                onCodexPetsRefresh={onCodexPetsRefresh}
                onCodexPetsUpdate={onCodexPetsUpdate}
                copyState={copyState}
                onCopyVersion={copyVersionInfo}
              />
            ) : activePage === "memory" ? (
              <MemoryPanel
                participants={participants ?? []}
                focusParticipantID={memoryFocusParticipantID}
              />
            ) : activePage === "remote" && ENABLE_REMOTE_CONTROL ? (
              <SettingsRemotePageContainer />
            ) : activePage === "archive" ? (
              <SettingsArchivePage
                archivedThreads={archivedThreads ?? []}
                onUnarchive={onUnarchiveThread}
              />
            ) : (
              <SettingsUsagePage
                usage={usage}
                usageRange={usageRange}
                setUsageRange={setUsageRange}
              />
            )}
          </div>
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

// 小节只有一个安静的小标签；描述性文字一律省略——语义由行本身携带,
// 只在个别行的 description 里保留单位、约束或禁用原因。
function SettingsSection({
  title,
  testID,
  children
}: {
  title?: string;
  testID?: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <section className="settings-section" {...(testID ? { "data-testid": testID } : {})}>
      {title ? (
        <header className="settings-section-header">
          <h2 className="settings-section-title">{title}</h2>
        </header>
      ) : null}
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
    <SettingsSection testID="settings-providers">
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
        <SettingsRow title={addingProvider ? "新增服务" : "当前服务"}>
          <div className="settings-row-control-block">
            {addingProvider ? (
              <span className="settings-inline-flag">新的模型服务</span>
            ) : providers.length > 0 ? (
              <SelectMenu
                triggerClassName="settings-select-trigger"
                ariaLabel="选择当前会话使用的服务"
                value={providerDraft}
                onChange={onProviderChange}
                disabled={running}
                options={providers.map((provider) => ({
                  value: provider.name,
                  label: providerLabels.get(provider.name) ?? provider.name
                }))}
              />
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
          <SettingsRow title="服务类型">
            <SelectMenu
              triggerClassName="settings-select-trigger"
              ariaLabel="服务类型"
              dataTestid="settings-provider-type-select"
              value={providerTypeDraft}
              onChange={onProviderTypeDraftChange}
              disabled={running}
              options={[
                { value: "openai-compatible", label: "OpenAI 兼容" },
                { value: "anthropic", label: "Anthropic 兼容" }
              ]}
            />
          </SettingsRow>
        ) : null}
        {addingProvider ? (
          <SettingsRow
            title="服务标识"
            description={providerNameTaken ? "这个名称已存在" : undefined}
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
        <SettingsRow title="模型名称" block>
          <input
            className="settings-input"
            value={modelDraft}
            onChange={(event) => onModelDraftChange(event.target.value)}
            disabled={running}
          />
        </SettingsRow>
        <SettingsRow
          title="思考强度"
          description={reasoningMode === "off" ? "当前模型不支持思考" : undefined}
          block
        >
          <SelectMenu
            triggerClassName="settings-select-trigger"
            ariaLabel="思考强度"
            value={variantDraft}
            onChange={onVariantDraftChange}
            disabled={running || reasoningMode === "off"}
            options={variantOptions.map((variant) => ({
              value: variant,
              label: variantLabel(variant)
            }))}
          />
        </SettingsRow>
        <SettingsRow
          title="Base URL"
          description={connectionLocked ? "由 OpenAI OAuth 管理" : undefined}
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
        <SettingsRow title={authFieldLabel} block>
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
          {error ? <div className="settings-error">{error}</div> : null}
          {saved && !error ? <div className="settings-saved">已保存</div> : null}
          <button
            className="settings-button settings-button-primary"
            type="submit"
            disabled={disabled}
          >
            {addingProvider ? "添加服务" : "保存配置"}
          </button>
        </SettingsRow>
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
    <SettingsSection testID="settings-advanced">
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
          description="占上下文窗口的百分比"
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
          description="压缩时保留的 token 数"
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
          description={`${providerContextWindowSource}${
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
        <SettingsRow title="Temperature" description="0 到 2" block>
          <input
            className="settings-input"
            value={temperature}
            inputMode="decimal"
            placeholder="Auto"
            onChange={(event) => onTemperatureChange(event.target.value)}
            disabled={running || !initialized}
          />
          {error ? <div className="settings-error">{error}</div> : null}
          {saved && !error ? <div className="settings-saved">已保存</div> : null}
          <button
            className="settings-button settings-button-primary"
            type="submit"
            disabled={running || !initialized}
          >
            保存
          </button>
        </SettingsRow>
      </form>
    </SettingsSection>
  );
}

/* -------------------------------------------------------------------------- */
/*  General page                                                               */
/* -------------------------------------------------------------------------- */

function SettingsGeneralPage({
  initialized,
  running,
  desktopBuild,
  showDebugControlsSetting,
  debugControlsEnabled,
  mcpServers,
  mcpLoading,
  mcpError,
  mcpBusyServer,
  codexPets,
  codexPetsLoading,
  codexPetsError,
  onDebugControlsChange,
  onGeneralSave,
  onMCPAction,
  onMCPAuthStart,
  onMCPAuthFinish,
  onMCPAuthRemove,
  onCodexPetsRefresh,
  onCodexPetsUpdate,
  copyState,
  onCopyVersion
}: {
  initialized: InitializeResult | undefined;
  running: boolean;
  desktopBuild: DesktopBuildInfo | undefined;
  showDebugControlsSetting: boolean;
  debugControlsEnabled: boolean;
  mcpServers: MCPServerStatus[];
  mcpLoading: boolean;
  mcpError: string;
  mcpBusyServer: string;
  codexPets: CodexPetsSnapshot | undefined;
  codexPetsLoading: boolean;
  codexPetsError: string;
  onDebugControlsChange: (enabled: boolean) => void;
  onGeneralSave: (settings: RuntimeGeneralSettingsUpdate) => Promise<void>;
  onMCPAction: (name: string, action: "connect" | "disconnect" | "refresh") => Promise<void>;
  onMCPAuthStart: (name: string) => Promise<MCPAuthStartResult | undefined>;
  onMCPAuthFinish: (name: string, state: string, code: string) => Promise<boolean>;
  onMCPAuthRemove: (name: string) => Promise<void>;
  onCodexPetsRefresh: () => Promise<CodexPetsSnapshot>;
  onCodexPetsUpdate: (settings: CodexPetSettingsUpdate) => Promise<CodexPetsSnapshot>;
  copyState: CopyState;
  onCopyVersion: () => Promise<void>;
}): JSX.Element {
  const generalSettings = initialized?.general_settings;
  const configuredMCPEnabled = generalSettings?.mcp_server_enabled ?? {};
  const configuredMCPKey = stableBoolRecordSignature(configuredMCPEnabled);
  const [appendSystemPromptDraft, setAppendSystemPromptDraft] = useState(generalSettings?.append_system_prompt ?? "");
  const [memoryDisabledDraft, setMemoryDisabledDraft] = useState(generalSettings?.memory_disabled ?? false);
  const [mcpEnabledDraft, setMCPEnabledDraft] = useState<Record<string, boolean>>(() => ({ ...configuredMCPEnabled }));
  const [mcpToggleBusy, setMCPToggleBusy] = useState("");
  const [mcpToggleError, setMCPToggleError] = useState("");
  const [mcpAuthStates, setMCPAuthStates] = useState<Record<string, string>>({});
  const [mcpAuthCodes, setMCPAuthCodes] = useState<Record<string, string>>({});
  const [codexPetBusy, setCodexPetBusy] = useState(false);
  const [codexPetLocalError, setCodexPetLocalError] = useState("");
  const [generalError, setGeneralError] = useState("");
  const [generalSaved, setGeneralSaved] = useState(false);

  useEffect(() => {
    setAppendSystemPromptDraft(generalSettings?.append_system_prompt ?? "");
    setMemoryDisabledDraft(generalSettings?.memory_disabled ?? false);
    setMCPEnabledDraft({ ...configuredMCPEnabled });
    setGeneralError("");
    setGeneralSaved(false);
  }, [generalSettings?.append_system_prompt, generalSettings?.memory_disabled, configuredMCPKey]);

  async function submitGeneral(event: ReactFormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault();
    setGeneralError("");
    setGeneralSaved(false);
    try {
      await onGeneralSave({
        append_system_prompt: appendSystemPromptDraft.trim(),
        memory_disable: memoryDisabledDraft,
      });
      setGeneralSaved(true);
    } catch (saveError) {
      setGeneralError(saveError instanceof Error ? saveError.message : "保存失败");
    }
  }

  async function toggleMCPServer(name: string, enabled: boolean): Promise<void> {
    const previous = mcpEnabledDraft;
    const next = { ...previous, [name]: enabled };
    setMCPEnabledDraft(next);
    setMCPToggleBusy(name);
    setMCPToggleError("");
    try {
      await onGeneralSave({ mcp_enabled_toggles: next });
    } catch (toggleError) {
      setMCPEnabledDraft(previous);
      setMCPToggleError(toggleError instanceof Error ? toggleError.message : "保存失败");
    } finally {
      setMCPToggleBusy("");
    }
  }

  async function beginMCPAuth(name: string): Promise<void> {
    const result = await onMCPAuthStart(name);
    if (!result) {
      return;
    }
    setMCPAuthStates((states) => ({ ...states, [name]: result.state }));
    setMCPAuthCodes((codes) => ({ ...codes, [name]: "" }));
  }

  async function completeMCPAuth(name: string): Promise<void> {
    const state = mcpAuthStates[name]?.trim() ?? "";
    const code = mcpAuthCodes[name]?.trim() ?? "";
    if (!state || !code) {
      return;
    }
    if (await onMCPAuthFinish(name, state, code)) {
      setMCPAuthStates((states) => withoutRecordKey(states, name));
      setMCPAuthCodes((codes) => withoutRecordKey(codes, name));
    }
  }

  const mcpServerByName = new Map(mcpServers.map((server) => [server.name, server]));
  const mcpRowNames = Array.from(
    new Set([...mcpServers.map((server) => server.name), ...Object.keys(mcpEnabledDraft)]),
  ).sort((a, b) => a.localeCompare(b));
  const codexPetOptions = codexPets?.pets ?? [];
  const codexPetSelectedID = codexPets?.selected_id ?? "";
  const codexPetEnabled = Boolean(codexPets?.enabled);
  const codexPetStatus = codexPetLocalError || codexPetsError;
  const extensionInventory = initialized?.extension_inventory ?? [];

  async function refreshCodexPets(): Promise<void> {
    setCodexPetBusy(true);
    setCodexPetLocalError("");
    try {
      await onCodexPetsRefresh();
    } catch (error) {
      setCodexPetLocalError(error instanceof Error ? error.message : "刷新失败");
    } finally {
      setCodexPetBusy(false);
    }
  }

  async function updateCodexPets(settings: CodexPetSettingsUpdate): Promise<void> {
    setCodexPetBusy(true);
    setCodexPetLocalError("");
    try {
      await onCodexPetsUpdate(settings);
    } catch (error) {
      setCodexPetLocalError(error instanceof Error ? error.message : "保存失败");
    } finally {
      setCodexPetBusy(false);
    }
  }

  return (
    <>
      <SettingsSection title="外观" testID="settings-appearance">
        <SettingsCard>
          <SettingsRow title="主题">
            <ThemePreferenceControl />
          </SettingsRow>
          <SettingsRow title="消息流字号" block>
            <MessageFlowFontSizeControl />
          </SettingsRow>
          <SettingsRow
            title="Codex Pet"
            description={codexPets?.home ? `读取 ${codexPets.home}` : "读取 ~/.wuu/pets"}
            block
          >
            <div className="settings-codex-pets-controls">
              <button
                className="settings-switch"
                type="button"
                role="switch"
                aria-checked={codexPetEnabled}
                data-testid="settings-codex-pet-enabled"
                disabled={codexPetsLoading || codexPetBusy || codexPetOptions.length === 0}
                onClick={() => void updateCodexPets({ enabled: !codexPetEnabled })}
              >
                <span className="settings-switch-thumb" aria-hidden="true" />
                <span className="sr-only">{codexPetEnabled ? "关闭 Codex Pet" : "打开 Codex Pet"}</span>
              </button>
              <select
                className="settings-select settings-codex-pet-select"
                aria-label="选择 Codex Pet"
                data-testid="settings-codex-pet-select"
                value={codexPetSelectedID}
                disabled={codexPetsLoading || codexPetBusy || codexPetOptions.length === 0}
                onChange={(event) => void updateCodexPets({ selected_id: event.currentTarget.value })}
              >
                {codexPetOptions.length === 0 ? (
                  <option value="">暂无本地宠物</option>
                ) : (
                  codexPetOptions.map((pet) => (
                    <option key={pet.id} value={pet.id}>
                      {pet.display_name}
                    </option>
                  ))
                )}
              </select>
              <button
                className="settings-button settings-icon-button"
                type="button"
                title="刷新本地 Codex Pets"
                aria-label="刷新本地 Codex Pets"
                disabled={codexPetsLoading || codexPetBusy}
                onClick={() => void refreshCodexPets()}
              >
                <RefreshCw size={15} aria-hidden="true" />
              </button>
            </div>
            {codexPetsLoading ? <small className="settings-muted-line">正在读取本地宠物…</small> : null}
            {!codexPetsLoading && codexPetOptions.length === 0 ? (
              <small className="settings-muted-line">
                把 pet.json 和 spritesheet.webp 放到上面的 Wuu 目录；也兼容 codex-pets.net 安装到 ~/.codex/pets。
              </small>
            ) : null}
            {codexPets?.errors.length ? (
              <small className="settings-muted-line settings-error">
                {codexPets.errors[0]}
              </small>
            ) : null}
            {codexPetStatus ? <small className="settings-muted-line settings-error">{codexPetStatus}</small> : null}
          </SettingsRow>
        </SettingsCard>
      </SettingsSection>

      <SettingsSection title="行为" testID="settings-general">
        <form className="settings-card" onSubmit={submitGeneral}>
          <SettingsRow
            title="附加系统提示"
            description="追加在内置提示之后"
            block
          >
            <textarea
              className="settings-input settings-textarea"
              value={appendSystemPromptDraft}
              placeholder="例如：默认用中文回答。"
              rows={5}
              onChange={(event) => {
                setAppendSystemPromptDraft(event.target.value);
                setGeneralSaved(false);
              }}
              disabled={running || !initialized}
            />
            {generalError ? <div className="settings-error">{generalError}</div> : null}
            {generalSaved && !generalError ? <div className="settings-saved">已保存</div> : null}
            <button
              className="settings-button settings-button-primary"
              type="submit"
              disabled={running || !initialized}
            >
              保存
            </button>
          </SettingsRow>
          <SettingsRow
            title="记忆"
            description="关闭后不读取或写入长期记忆"
          >
            <button
              className="settings-switch"
              type="button"
              role="switch"
              aria-checked={!memoryDisabledDraft}
              disabled={running || !initialized}
              onClick={() => {
                setMemoryDisabledDraft((value) => !value);
                setGeneralSaved(false);
              }}
            >
              <span className="settings-switch-thumb" aria-hidden="true" />
              <span className="sr-only">{memoryDisabledDraft ? "打开记忆" : "关闭记忆"}</span>
            </button>
          </SettingsRow>
        </form>
      </SettingsSection>

      <SettingsSection title="MCP 服务器" testID="settings-mcp">
        <SettingsCard>
          {mcpLoading && mcpRowNames.length === 0 ? (
            <div className="settings-mcp-empty">加载中…</div>
          ) : mcpRowNames.length > 0 ? (
            mcpRowNames.map((name) => {
              const server = mcpServerByName.get(name);
              const busy = mcpBusyServer === name || mcpToggleBusy === name;
              const connected = server ? server.connected || server.state === "connected" || server.state === "ready" : false;
              const disabledByConfig = server?.state === "disabled";
              const enabled = mcpEnabledDraft[name] ?? !disabledByConfig;
              const oauthPending = Boolean(mcpAuthStates[name]);
              const oauthCode = mcpAuthCodes[name] ?? "";
              return (
                <SettingsRow
                  key={name}
                  title={name}
                  description={server ? formatMCPServerMeta(server) : undefined}
                >
                  {server ? (
                    <span className="settings-row-control-value">
                      <span className={`settings-status-pill ${mcpStateTone(server.state)}`}>
                        {mcpStateLabel(server.state)}
                      </span>
                    </span>
                  ) : null}
                  {server ? (
                    <>
                      {oauthPending ? (
                        <>
                          <input
                            className="settings-input settings-mcp-code-input"
                            aria-label={`${name} 授权码`}
                            autoComplete="off"
                            placeholder="授权码"
                            value={oauthCode}
                            disabled={busy}
                            onChange={(event) => {
                              const value = event.currentTarget.value;
                              setMCPAuthCodes((codes) => ({ ...codes, [name]: value }));
                            }}
                          />
                          <button
                            className="settings-button settings-icon-button"
                            type="button"
                            title="完成 OAuth 登录"
                            aria-label={`完成 ${name} OAuth 登录`}
                            disabled={busy || oauthCode.trim() === ""}
                            onClick={() => void completeMCPAuth(name)}
                          >
                            <Check size={15} aria-hidden="true" />
                          </button>
                          <button
                            className="settings-button settings-icon-button"
                            type="button"
                            title="取消登录"
                            aria-label={`取消 ${name} OAuth 登录`}
                            disabled={busy}
                            onClick={() => {
                              setMCPAuthStates((states) => withoutRecordKey(states, name));
                              setMCPAuthCodes((codes) => withoutRecordKey(codes, name));
                            }}
                          >
                            <X size={15} aria-hidden="true" />
                          </button>
                        </>
                      ) : server.auth_status === "not_logged_in" ? (
                        <button
                          className="settings-button settings-icon-button"
                          type="button"
                          title="OAuth 登录"
                          aria-label={`登录 ${name}`}
                          disabled={busy || disabledByConfig}
                          onClick={() => void beginMCPAuth(name)}
                        >
                          <KeyRound size={15} aria-hidden="true" />
                        </button>
                      ) : server.auth_status === "oauth" ? (
                        <button
                          className="settings-button settings-icon-button"
                          type="button"
                          title="移除 OAuth 登录"
                          aria-label={`移除 ${name} OAuth 登录`}
                          disabled={busy}
                          onClick={() => void onMCPAuthRemove(name)}
                        >
                          <X size={15} aria-hidden="true" />
                        </button>
                      ) : null}
                      <button
                        className="settings-button settings-icon-button"
                        type="button"
                        title="刷新"
                        aria-label={`刷新 ${name}`}
                        disabled={busy || disabledByConfig}
                        onClick={() => void onMCPAction(name, "refresh")}
                      >
                        <RefreshCw size={15} aria-hidden="true" />
                      </button>
                      <button
                        className="settings-button settings-icon-button"
                        type="button"
                        title={disabledByConfig ? "已在配置中关闭" : connected ? "断开" : "连接"}
                        aria-label={`${connected ? "断开" : "连接"} ${name}`}
                        disabled={busy || disabledByConfig}
                        onClick={() => void onMCPAction(name, connected ? "disconnect" : "connect")}
                      >
                        {connected ? <PlugZap size={15} aria-hidden="true" /> : <Plug size={15} aria-hidden="true" />}
                      </button>
                    </>
                  ) : null}
                  <button
                    className="settings-switch"
                    type="button"
                    role="switch"
                    aria-checked={enabled}
                    data-testid={`settings-mcp-enabled-${name}`}
                    disabled={running || !initialized || busy}
                    onClick={() => void toggleMCPServer(name, !enabled)}
                  >
                    <span className="settings-switch-thumb" aria-hidden="true" />
                    <span className="sr-only">{enabled ? `关闭 ${name}` : `打开 ${name}`}</span>
                  </button>
                  {server?.error ? (
                    <small className="settings-mcp-error">{server.error}</small>
                  ) : null}
                </SettingsRow>
              );
            })
          ) : (
            <div className="settings-mcp-empty">暂无 MCP 服务器</div>
          )}
          {mcpError ? <div className="settings-mcp-empty settings-mcp-error">{mcpError}</div> : null}
          {mcpToggleError ? <div className="settings-mcp-empty settings-mcp-error">{mcpToggleError}</div> : null}
        </SettingsCard>
      </SettingsSection>

      <SettingsSection title="扩展与权限" testID="settings-extensions">
        <SettingsCard>
          {extensionInventory.length > 0 ? (
            extensionInventory.map((record) => (
              <SettingsRow
                key={record.id}
                title={record.name}
                description={formatExtensionProvenance(record)}
                block
              >
                <div className="settings-extension-detail">
                  <div className="settings-extension-badges">
                    <span className={`settings-status-pill ${extensionStateTone(record.state)}`}>
                      {extensionStateLabel(record.state)}
                    </span>
                    {record.grant_scope ? (
                      <span className="settings-extension-grant">
                        {extensionGrantScopeLabel(record.grant_scope)}
                      </span>
                    ) : null}
                  </div>
                  {record.requested_permissions?.length ? (
                    <small className="settings-extension-meta">
                      权限：{record.requested_permissions.join("、")}
                    </small>
                  ) : null}
                  {record.unsupported_fields?.length ? (
                    <small className="settings-extension-meta settings-extension-warning">
                      不支持字段：{record.unsupported_fields.join("、")}
                    </small>
                  ) : null}
                </div>
              </SettingsRow>
            ))
          ) : (
            <div className="settings-mcp-empty">暂无扩展</div>
          )}
        </SettingsCard>
      </SettingsSection>

      {showDebugControlsSetting ? (
        <SettingsSection title="开发">
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
        <SettingsSection title="工具" testID="settings-tool-surface">
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

      <SettingsSection title="关于" testID="settings-about">
        <SettingsCard>
          <SettingsRow title="版本">
            <span className="settings-row-control-value">
              {desktopBuild ? versionLabel(desktopBuild.version) : "加载中…"}
            </span>
            <button
              className="settings-button"
              type="button"
              aria-label="复制版本信息"
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
/*  Archive page                                                               */
/* -------------------------------------------------------------------------- */

function SettingsArchivePage({
  archivedThreads,
  onUnarchive,
}: {
  archivedThreads: readonly ArchivedSessionView[];
  onUnarchive: (thread: ArchivedSessionView) => void;
}): JSX.Element {
  const [query, setQuery] = useState("");
  const [projectFilter, setProjectFilter] = useState("all");
  const sortedThreads = useMemo(
    () => [...archivedThreads].sort((a, b) => b.updated_at.localeCompare(a.updated_at)),
    [archivedThreads],
  );
  const projectOptions = useMemo(() => {
    const seen = new Set<string>();
    return sortedThreads.flatMap((thread) => {
      const projectID = archiveProjectID(thread);
      if (seen.has(projectID)) {
        return [];
      }
      seen.add(projectID);
      return [{ value: projectID, label: archiveProjectName(thread) }];
    });
  }, [sortedThreads]);
  const groups = useMemo(() => {
    const normalizedQuery = query.trim().toLocaleLowerCase();
    const grouped = new Map<
      string,
      { projectName: string; threads: ArchivedSessionView[] }
    >();
    for (const thread of sortedThreads) {
      const projectID = archiveProjectID(thread);
      const title = archiveThreadTitle(thread);
      if (projectFilter !== "all" && projectID !== projectFilter) {
        continue;
      }
      if (normalizedQuery && !title.toLocaleLowerCase().includes(normalizedQuery)) {
        continue;
      }
      const group = grouped.get(projectID) ?? {
        projectName: archiveProjectName(thread),
        threads: [],
      };
      group.threads.push(thread);
      grouped.set(projectID, group);
    }
    return Array.from(grouped, ([projectID, group]) => ({ projectID, ...group }));
  }, [projectFilter, query, sortedThreads]);
  const noMatches = sortedThreads.length > 0 && groups.length === 0;

  return (
    <div className="settings-archive-page">
      <div className="settings-archive-toolbar" role="search" aria-label="筛选归档会话">
        <label className="settings-archive-search">
          <Search className="icon" aria-hidden="true" />
          <span className="sr-only">搜索归档会话</span>
          <input
            type="search"
            value={query}
            placeholder="搜索归档会话"
            onChange={(event) => setQuery(event.currentTarget.value)}
          />
        </label>
        <SelectMenu
          className="settings-archive-project-filter"
          triggerClassName="settings-archive-filter-trigger"
          value={projectFilter}
          onChange={setProjectFilter}
          ariaLabel="按项目筛选"
          options={[{ value: "all", label: "所有项目" }, ...projectOptions]}
          flip
        />
      </div>
      {sortedThreads.length === 0 || noMatches ? (
        <div className="settings-archive-empty" role="status">
          <Archive className="settings-archive-empty-icon" aria-hidden="true" />
          <p className="settings-archive-empty-title">
            {noMatches ? "没有匹配的归档会话" : "暂无已归档的会话"}
          </p>
          {noMatches ? null : (
            <p className="settings-archive-empty-hint">
              在侧边栏行末点击归档按钮后会出现在这里，随时可以恢复。
            </p>
          )}
        </div>
      ) : (
        <div className="settings-archive-groups" aria-label="已归档会话列表">
          {groups.map((group) => (
            <section className="settings-archive-group" key={group.projectID}>
              <header className="settings-archive-group-header">
                <div className="settings-archive-group-name">
                  <Folder className="icon" aria-hidden="true" />
                  <span>{group.projectName}</span>
                </div>
                <span className="settings-archive-group-count">
                  {group.threads.length} 个会话
                </span>
              </header>
              <div className="settings-archive-list">
                {group.threads.map((thread) => {
                  const title = archiveThreadTitle(thread);
                  return (
                    <div className="settings-archive-row" key={thread.id}>
                      <div className="settings-archive-row-copy">
                        <span className="settings-archive-title" title={title}>{title}</span>
                        <time className="settings-archive-time" dateTime={thread.updated_at}>
                          {formatArchiveTime(thread.updated_at)}
                        </time>
                      </div>
                      <button
                        type="button"
                        className="settings-button settings-archive-restore"
                        aria-label={`恢复 ${title}`}
                        onClick={() => onUnarchive(thread)}
                      >
                        <Archive className="icon-sm" aria-hidden="true" />
                        恢复
                      </button>
                    </div>
                  );
                })}
              </div>
            </section>
          ))}
        </div>
      )}
    </div>
  );
}

function archiveThreadTitle(thread: ArchivedSessionView): string {
  return (thread.title ?? "").trim() || "未命名会话";
}

function archiveProjectID(thread: ArchivedSessionView): string {
  return thread.archive_project_id?.trim() || "no-project";
}

function archiveProjectName(thread: ArchivedSessionView): string {
  return thread.archive_project_name?.trim() || "无项目";
}

function formatArchiveTime(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) {
    return iso;
  }
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "long",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(date);
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

function settingsPageTitle(page: SettingsPage): string {
  switch (page) {
    case "providers":
      return "模型服务";
    case "advanced":
      return "高级";
    case "general":
      return "常规";
    case "memory":
      return "记忆";
    case "usage":
      return "用量";
    case "remote":
      return "远程";
    case "archive":
      return "归档";
  }
}

function stableBoolRecordSignature(record: Record<string, boolean>): string {
  return Object.keys(record)
    .sort((a, b) => a.localeCompare(b))
    .map((key) => `${key}:${record[key] ? "1" : "0"}`)
    .join("|");
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
  const temperature = parseTemperatureDraft(draft.temperature);
  if (temperature.error) {
    return { settings: {}, error: temperature.error };
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

function parseTemperatureDraft(raw: string): { value: number; error?: string } {
  const value = raw.trim();
  if (value === "" || value.toLowerCase() === "auto") {
    return { value: 0 };
  }
  const parsed = Number(value);
  if (!Number.isFinite(parsed) || parsed < 0 || parsed > 2) {
    return { value: 0, error: "Temperature 必须是 Auto 或 0 到 2 之间的数字" };
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
  if (value === undefined || !Number.isFinite(value) || value <= 0) {
    return "";
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
  return `${day.date}\n输入 ${formatTokenCount(day.input_tokens)} · 输出 ${formatTokenCount(day.output_tokens)} · 命中 ${formatPercent(day.cache_hit_rate)}`;
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

function withoutRecordKey(values: Record<string, string>, key: string): Record<string, string> {
  const next = { ...values };
  delete next[key];
  return next;
}

function formatMCPServerMeta(server: MCPServerStatus): string {
  const pieces = [`${server.tool_count ?? 0} 个工具`];
  if (server.auth_status && server.auth_status !== "unsupported") {
    pieces.push(mcpAuthLabel(server.auth_status));
  }
  return pieces.join(" · ");
}

function formatExtensionProvenance(record: ExtensionInventoryRecord): string {
  const source = extensionSourceLabel(record.provenance.source);
  const scope = extensionScopeLabel(record.provenance.scope);
  return `${extensionKindLabel(record.kind)} · ${source} · ${scope}`;
}

function extensionKindLabel(kind: ExtensionInventoryRecord["kind"]): string {
  switch (kind) {
    case "skill":
      return "Skill";
    case "agent_template":
      return "Agent Template";
    case "mcp":
      return "MCP";
    case "hook":
      return "Hook";
    case "plugin":
      return "Plugin";
    case "command":
      return "Command";
  }
}

function extensionSourceLabel(source: string): string {
  if (source === "codex") return "Codex";
  if (source === "claude") return "Claude Code";
  if (source === "wuu" || source === "wuu_config") return "Wuu";
  if (source === "bundled") return "Wuu 内置";
  if (source.startsWith("plugin:")) return source.replace("plugin:", "Plugin · ");
  return source || "未知来源";
}

function extensionScopeLabel(scope: string): string {
  switch (scope) {
    case "project":
      return "项目";
    case "user":
      return "用户";
    case "bundled":
      return "内置";
    default:
      return scope || "未知范围";
  }
}

function extensionStateLabel(state: ExtensionInventoryRecord["state"]): string {
  switch (state) {
    case "active":
      return "已启用";
    case "read_only":
      return "只读";
    case "pending":
      return "待授权";
    case "granted":
      return "已授权";
    case "rejected":
      return "已拒绝";
    case "changed":
      return "配置已变化";
  }
}

function extensionStateTone(state: ExtensionInventoryRecord["state"]): string {
  switch (state) {
    case "active":
    case "granted":
      return "success";
    case "pending":
    case "changed":
      return "warning";
    case "rejected":
      return "danger";
    default:
      return "neutral";
  }
}

function extensionGrantScopeLabel(scope: NonNullable<ExtensionInventoryRecord["grant_scope"]>): string {
  switch (scope) {
    case "action":
      return "单次授权";
    case "session":
      return "会话授权";
    case "project":
      return "项目授权";
    case "user":
      return "用户授权";
  }
}

function mcpStateLabel(state: string): string {
  switch (state) {
    case "ready":
    case "connected":
      return "已连接";
    case "starting":
    case "connecting":
      return "连接中";
    case "error":
    case "failed":
      return "失败";
    case "disabled":
      return "已断开";
    case "auth_required":
    case "needs_auth":
      return "需认证";
    case "needs_client_registration":
      return "需注册";
    case "reconnecting":
      return "重连中";
    case "stopped":
    case "configured":
      return "已配置";
    default:
      return state || "未知";
  }
}

function mcpStateTone(state: string): string {
  switch (state) {
    case "ready":
    case "connected":
      return "success";
    case "error":
    case "failed":
    case "auth_required":
    case "needs_auth":
    case "needs_client_registration":
      return "danger";
    case "starting":
    case "reconnecting":
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

/* -------------------------------------------------------------------------- */
/*  远程控制                                                                    */
/* -------------------------------------------------------------------------- */

/** Data wiring for the remote-control page: pulls the snapshot from the
 *  main-process RemoteHostManager, re-pulls on every remote event (pairing
 *  URI shown, phone paired, host exit), and maps panel actions to IPC. */
function SettingsRemotePageContainer(): JSX.Element {
  const [snapshot, setSnapshot] = useState<RemoteControlSnapshot | null>(null);
  const [actionError, setActionError] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let cancelled = false;
    const refresh = () => {
      window.wuu
        .getRemoteControlSnapshot()
        .then((snap) => {
          if (!cancelled) setSnapshot(snap);
        })
        .catch(() => {});
    };
    refresh();
    const off = window.wuu.onRemoteControlEvent(() => refresh());
    return () => {
      cancelled = true;
      off();
    };
  }, []);

  const run = (action: () => Promise<RemoteControlSnapshot>) => {
    setBusy(true);
    setActionError("");
    action()
      .then(setSnapshot)
      .catch((err: unknown) => {
        setActionError(err instanceof Error ? err.message : String(err));
      })
      .finally(() => setBusy(false));
  };

  return (
    <SettingsRemotePage
      status={snapshot?.status ?? null}
      statusError={actionError || snapshot?.status_error || ""}
      hostRunning={snapshot?.host_running ?? false}
      pairUri={snapshot?.pair_uri ?? null}
      busy={busy}
      onSaveRelay={(relayUrl) => run(() => window.wuu.setRemoteRelay(relayUrl))}
      onToggleHost={(enabled) => run(() => window.wuu.setRemoteHostEnabled(enabled))}
      onOpenPairing={() => run(() => window.wuu.startRemotePairing())}
      onRemoveDevice={(device) => run(() => window.wuu.removeRemoteDevice(device.fingerprint))}
    />
  );
}
