import { ArrowLeft, BarChart3, Plus, Settings, X } from "lucide-react";
import type { SettingsUsageRange } from "../shared/protocol";
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
import type {
  DesktopBuildInfo,
  ExtensionSessionTrustSummary,
  ExtensionSurfaceTrustSummary,
  ExtensionTrustSummary,
  InitializeResult,
  ProviderSummary,
  RuntimeConnectionUpdate
} from "../shared/protocol";
import { providerModelVariantOptions, variantLabel } from "./RuntimeHelpers";
import { OVERLAY_SCROLLBAR_OPTIONS } from "./ScrollbarOptions";

export type SettingsUsageBucket = {
  id: string;
  provider: string;
  model: string;
  inputTokens: number;
  outputTokens: number;
  cacheCreationTokens: number;
  cacheReadTokens: number;
  turns: number;
  agents: number;
};

export type SettingsUsageDay = {
  date: string;
  inputTokens: number;
  outputTokens: number;
  cacheCreationTokens: number;
  cacheReadTokens: number;
  turns: number;
  agents: number;
};

export type SettingsUsageEntry = {
  id: string;
  kind: "turn" | "agent";
  title: string;
  provider: string;
  model: string;
  date?: string;
  status?: string;
  inputTokens: number;
  outputTokens: number;
  cacheCreationTokens: number;
  cacheReadTokens: number;
};

export type SettingsUsageData = {
  inputTokens: number;
  outputTokens: number;
  cacheCreationTokens: number;
  cacheReadTokens: number;
  turns: number;
  agents: number;
  buckets: SettingsUsageBucket[];
  days: SettingsUsageDay[];
  entries: SettingsUsageEntry[];
};

type SettingsPage = "general" | "usage";

const EMPTY_USAGE: SettingsUsageData = {
  inputTokens: 0,
  outputTokens: 0,
  cacheCreationTokens: 0,
  cacheReadTokens: 0,
  turns: 0,
  agents: 0,
  buckets: [],
  days: [],
  entries: [],
};

export function SettingsView({
  initialized,
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
  onDebugControlsChange,
  onSidebarResizeStart,
  onSidebarSeparatorKey,
  onSidebarSeparatorDoubleClick,
  usageRange,
  setUsageRange,
}: {
  initialized?: InitializeResult;
  running: boolean;
  usage?: SettingsUsageData;
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
  const [activePage, setActivePage] = useState<SettingsPage>("general");

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

  const core = initialized?.core;
  const selectedProvider = addingProvider ? undefined : providers.find((item) => item.name === providerDraft);
  const providerLabels = useMemo(() => providerDisplayLabels(providers), [providers]);
  const selectedBaseURL = selectedProvider?.base_url ?? "";
  const connectionLocked = !addingProvider && (selectedProvider?.connection_locked ?? false);
  const variantOptions = providerModelVariantOptions(selectedProvider, modelDraft, variantDraft);

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

  function changeProvider(provider: string): void {
    setAddingProvider(false);
    setProviderDraft(provider);
    setSaved(false);
    const summary = providers.find((item) => item.name === provider);
    if (summary) {
      setModelDraft(summary.model);
      setVariantDraft(initialized?.variant ?? initialized?.effort ?? "");
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

  const disabled =
    running ||
    !providerDraft.trim() ||
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

  return (
    <div className={`settings-shell${resizingSidebar ? " resizing-sidebar" : ""}`} style={shellStyle}>
      <aside className="settings-sidebar">
        <div className="traffic-spacer" />
        <button className="settings-back-button" type="button" onClick={onBack}>
          <ArrowLeft className="icon" />
          <span>返回应用</span>
        </button>
        <nav className="settings-nav" aria-label="设置">
          <button
            className={`settings-nav-item${activePage === "general" ? " active" : ""}`}
            type="button"
            aria-current={activePage === "general" ? "page" : undefined}
            onClick={() => setActivePage("general")}
          >
            <Settings className="icon-lg" />
            <span>常规</span>
          </button>
          <button
            className={`settings-nav-item${activePage === "usage" ? " active" : ""}`}
            type="button"
            aria-current={activePage === "usage" ? "page" : undefined}
            onClick={() => setActivePage("usage")}
          >
            <BarChart3 className="icon-lg" />
            <span>用量</span>
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
          <h1>{activePage === "general" ? "常规" : "用量"}</h1>

          {activePage === "general" ? (
            <>
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
                          <X className="icon" />
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
                          <Plus className="icon" />
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
                        setVariantDraft("");
                        setSaved(false);
                      }}
                      disabled={running}
                    />
                  </label>
                  <label className="settings-row">
                    <span>
                      <strong>思考强度</strong>
                      <small>{variantOptions.length > 1 ? "当前模型支持的参数档位" : "当前模型没有可调参数档位"}</small>
                    </span>
                    <select
                      value={variantDraft}
                      onChange={(event) => {
                        setVariantDraft(event.target.value);
                        setSaved(false);
                      }}
                      disabled={running || variantOptions.length <= 1}
                    >
                      {variantOptions.map((variant) => (
                        <option key={variant || "auto"} value={variant}>
                          {variantLabel(variant)}
                        </option>
                      ))}
                    </select>
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

              <section className="settings-section" data-testid="settings-about">
                <div>
                  <h2>关于</h2>
                  <p>wuu 桌面与核心的构建信息。报问题时带上这一段。</p>
                </div>
                <div className="settings-card">
                  <dl className="settings-about-list">
                    <div className="settings-row">
                      <span>
                        <strong>桌面端</strong>
                        <small>Electron 客户端构建</small>
                      </span>
                      <span className="settings-about-value">
                        {desktopBuild ? `v${desktopBuild.version} · ${formatBuildDate(desktopBuild.date)}` : "加载中…"}
                      </span>
                    </div>
                    <div className="settings-row">
                      <span>
                        <strong>核心</strong>
                        <small>wuu app-server 二进制构建</small>
                      </span>
                      <span className="settings-about-value">{formatCoreBuild(core)}</span>
                    </div>
                    <div className="settings-row">
                      <span>
                        <strong>App-server 协议</strong>
                        <small>桌面与核心的 IPC 协议版本</small>
                      </span>
                      <span className="settings-about-value">{initialized?.protocol_version ?? "—"}</span>
                    </div>
                    <div className="settings-row">
                      <span>
                        <strong>扩展边界</strong>
                        <small>MCP、Hooks、Plugins、Skills 和 Workflows 的运行时状态</small>
                      </span>
                      <span className="settings-about-value">{formatExtensionTrust(initialized?.extension_trust)}</span>
                    </div>
                  </dl>
                </div>
              </section>
            </>
          ) : (
            <SettingsUsagePage
              usage={usage ?? EMPTY_USAGE}
              usageRange={usageRange}
              setUsageRange={setUsageRange}
            />
          )}
        </div>
      </OverlayScrollbarsComponent>
    </div>
  );
}

function SettingsUsagePage({
  usage,
  usageRange,
  setUsageRange,
}: {
  usage: SettingsUsageData;
  usageRange: SettingsUsageRange;
  setUsageRange: (range: SettingsUsageRange) => void;
}): JSX.Element {
  const contextTokens = usageContextTokens(usage);
  const hitRate = cacheHitRate(usage);
  const heatmap = buildCacheHeatmap(usage.days);
  const hasUsage = contextTokens > 0 || usage.cacheCreationTokens > 0 || usage.entries.length > 0;
  const ranges: SettingsUsageRange[] = ["all", "7d", "30d", "90d"];
  return (
    <>
      <section className="settings-section" data-testid="settings-usage">
        <div>
          <h2>本地用量</h2>
          <p>显示当前桌面已加载会话和子任务的 token 统计。</p>
        </div>
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
        <div className="settings-usage-metrics">
          <UsageMetric label="上下文 token" value={contextTokens} detail="输入 + 缓存读取 + 输出" />
          <UsageMetric label="输入 token" value={usage.inputTokens} detail={`${usage.turns} 轮主会话`} />
          <UsageMetric label="输出 token" value={usage.outputTokens} detail={`${usage.agents} 个子任务`} />
          <UsageMetric label="缓存命中率" value={formatPercent(hitRate)} detail={`缓存读取 ${formatTokenCount(usage.cacheReadTokens)}`} />
          <UsageMetric label="缓存写入" value={usage.cacheCreationTokens} detail="供后续请求复用" />
        </div>
      </section>

      <section className="settings-section">
        <div>
          <h2>模型使用</h2>
          <p>用过的模型服务、模型名称和缓存命中情况。</p>
        </div>
        <div className="settings-card settings-usage-table-card">
          {usage.buckets.length > 0 ? (
            <table className="settings-usage-table">
              <thead>
                <tr>
                  <th scope="col">模型</th>
                  <th scope="col">上下文</th>
                  <th scope="col">缓存命中</th>
                  <th scope="col">输入</th>
                  <th scope="col">输出</th>
                  <th scope="col">记录</th>
                </tr>
              </thead>
              <tbody>
                {usage.buckets.map((bucket) => (
                  <tr key={bucket.id}>
                    <td>
                      <strong>{bucket.provider}</strong>
                      <small>{bucket.model}</small>
                    </td>
                    <td>{formatTokenCount(usageContextTokens(bucket))}</td>
                    <td>{formatPercent(cacheHitRate(bucket))}</td>
                    <td>{formatTokenCount(bucket.inputTokens)}</td>
                    <td>{formatTokenCount(bucket.outputTokens)}</td>
                    <td>{formatUsageRecordCount(bucket)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : (
            <div className="settings-usage-empty">暂无用量记录</div>
          )}
        </div>
      </section>

      <section className="settings-section">
        <div>
          <h2>缓存命中热力图</h2>
          <p>最近 12 周每天的缓存命中率。</p>
        </div>
        <div className="settings-card settings-cache-heatmap-card">
          {hasUsage ? (
            <>
              <div className="settings-cache-heatmap-summary">
                <strong>{formatPercent(hitRate)}</strong>
                <span>整体缓存命中率</span>
                <small>
                  读取 {formatTokenCount(usage.cacheReadTokens)} · 写入 {formatTokenCount(usage.cacheCreationTokens)}
                </small>
              </div>
              <div className="settings-cache-heatmap" aria-label="缓存命中率热力图" role="grid">
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
            </>
          ) : (
            <div className="settings-usage-empty">暂无缓存命中数据</div>
          )}
        </div>
      </section>

      <section className="settings-section">
        <div>
          <h2>最近记录</h2>
          <p>主会话轮次和子任务的最近用量。</p>
        </div>
        <div className="settings-card">
          {hasUsage ? (
            <div className="settings-usage-entries">
              {usage.entries.slice(0, 8).map((entry) => (
                <div className="settings-usage-entry" key={entry.id}>
                  <span>
                    <strong>{entry.title}</strong>
                    <small>{formatUsageEntryMeta(entry)}</small>
                  </span>
                  <span className="settings-usage-entry-value">{formatTokenCount(usageContextTokens(entry))}</span>
                </div>
              ))}
            </div>
          ) : (
            <div className="settings-usage-empty">暂无用量记录</div>
          )}
        </div>
      </section>
    </>
  );
}

type CacheHeatmapCell = SettingsUsageDay & {
  level: number;
  hitRate?: number;
};

function UsageMetric({ label, value, detail }: { label: string; value: number | string; detail: string }): JSX.Element {
  return (
    <div className="settings-usage-metric">
      <span>{label}</span>
      <strong>{typeof value === "number" ? formatTokenCount(value) : value}</strong>
      <small>{detail}</small>
    </div>
  );
}

function usageContextTokens(usage: {
  inputTokens: number;
  outputTokens: number;
  cacheReadTokens: number;
}): number {
  return usage.inputTokens + usage.cacheReadTokens + usage.outputTokens;
}

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

function cacheHitRate(usage: {
  inputTokens: number;
  cacheReadTokens: number;
}): number | undefined {
  const promptTokens = usage.inputTokens + usage.cacheReadTokens;
  if (promptTokens <= 0) {
    return undefined;
  }
  return usage.cacheReadTokens / promptTokens;
}

function formatPercent(value: number | undefined): string {
  if (value === undefined || !Number.isFinite(value)) {
    return "—";
  }
  return `${Math.round(Math.max(0, Math.min(1, value)) * 100)}%`;
}

function buildCacheHeatmap(days: SettingsUsageDay[]): CacheHeatmapCell[] {
  const byDate = new Map(days.map((day) => [day.date, day]));
  const end = startOfLocalDay(new Date());
  const start = startOfWeek(addDays(end, -77));
  const cells: CacheHeatmapCell[] = [];
  for (let cursor = start; cursor.getTime() <= end.getTime(); cursor = addDays(cursor, 1)) {
    const date = localDateKey(cursor);
    const usage = byDate.get(date) ?? {
      date,
      inputTokens: 0,
      outputTokens: 0,
      cacheCreationTokens: 0,
      cacheReadTokens: 0,
      turns: 0,
      agents: 0,
    };
    const hitRate = cacheHitRate(usage);
    cells.push({
      ...usage,
      hitRate,
      level: heatmapLevel(usage, hitRate),
    });
  }
  return cells;
}

function heatmapLevel(day: SettingsUsageDay, hitRate: number | undefined): number {
  if (!hasUsageDayData(day)) {
    return 0;
  }
  if (hitRate === undefined || hitRate <= 0) {
    return 1;
  }
  if (hitRate < 0.25) {
    return 1;
  }
  if (hitRate < 0.5) {
    return 2;
  }
  if (hitRate < 0.75) {
    return 3;
  }
  return 4;
}

function hasUsageDayData(day: SettingsUsageDay): boolean {
  return (
    day.inputTokens > 0 ||
    day.outputTokens > 0 ||
    day.cacheCreationTokens > 0 ||
    day.cacheReadTokens > 0 ||
    day.turns > 0 ||
    day.agents > 0
  );
}

function formatHeatmapTitle(day: CacheHeatmapCell): string {
  if (!hasUsageDayData(day)) {
    return `${day.date}：暂无用量`;
  }
  return `${day.date}：缓存命中 ${formatPercent(day.hitRate)}，读取 ${formatTokenCount(day.cacheReadTokens)}，写入 ${formatTokenCount(day.cacheCreationTokens)}`;
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

function formatUsageRecordCount(bucket: SettingsUsageBucket): string {
  const pieces: string[] = [];
  if (bucket.turns > 0) {
    pieces.push(`${bucket.turns} 轮`);
  }
  if (bucket.agents > 0) {
    pieces.push(`${bucket.agents} 子任务`);
  }
  return pieces.length > 0 ? pieces.join("，") : "—";
}

function formatUsageEntryMeta(entry: SettingsUsageEntry): string {
  const kind = entry.kind === "agent" ? "子任务" : "主会话";
  const status = entry.status ? ` · ${entry.status}` : "";
  const date = entry.date ? ` · ${entry.date}` : "";
  return `${entry.provider} · ${entry.model} · ${kind}${status}${date}`;
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
