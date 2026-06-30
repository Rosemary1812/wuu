import {
  Check,
  ChevronDown,
  ChevronRight,
  ClipboardCheck,
  Eye,
  Folder,
  FolderOpen,
  FolderPlus,
  FolderX,
  GitBranch,
  Search,
  Shield,
  TriangleAlert,
  type LucideIcon
} from "lucide-react";
import type { RefObject } from "react";
import type {
  CodexModelSummary,
  DesktopProject,
  GitStatusResult,
  InitializeResult,
  PermissionSummary,
  ProviderModelSummary,
  ProviderSummary,
  RuntimeContext,
  ToolPolicySummary
} from "../shared/protocol";
import { FloatingMenuPortal } from "./ComposerFloatingMenu";
import type {
  CodexModelLoadState,
  CodexRuntimeMenu,
  ComposerVariant,
  FloatingMenuPlacement,
  PermissionMode
} from "./ComposerTypes";
import {
  codexEffortOptions,
  displayCodexModelName,
  isCodexProvider,
  providerModelDisplayName,
  providerModelReasoningMode,
  providerModelVariantOptions,
  shortCodexModelLabel,
  variantLabel
} from "./RuntimeHelpers";

type ChipTone = "neutral" | "danger";

type PermissionModeState = PermissionMode | "custom";

type PermissionModeOption = {
  mode: PermissionMode;
  label: string;
  chipLabel: string;
  icon: LucideIcon;
  chipTone: ChipTone;
  tone?: "danger";
};

const PERMISSION_MODE_OPTIONS: PermissionModeOption[] = [
  {
    mode: "read_only",
    label: "只读",
    chipLabel: "只读",
    icon: Eye,
    chipTone: "neutral"
  },
  {
    mode: "agent",
    label: "默认",
    chipLabel: "默认",
    icon: Shield,
    chipTone: "neutral"
  },
  {
    mode: "auto_review",
    label: "替我审批",
    chipLabel: "替我审批",
    icon: ClipboardCheck,
    chipTone: "neutral"
  },
  {
    mode: "full_access",
    label: "完全访问",
    chipLabel: "完全访问",
    icon: TriangleAlert,
    chipTone: "danger",
    tone: "danger"
  }
];

const CUSTOM_PERMISSION_MODE_OPTION: Omit<PermissionModeOption, "mode"> & { mode: PermissionModeState } = {
  mode: "custom",
  label: "自定义权限",
  chipLabel: "自定义权限",
  icon: Shield,
  chipTone: "neutral"
};

function permissionPresetAxes(mode: PermissionMode): { profile: string; approval: string; reviewer: string } {
  switch (mode) {
    case "read_only":
      return { profile: "read_only", approval: "on_request", reviewer: "user" };
    case "auto_review":
      return { profile: "workspace_write", approval: "on_request", reviewer: "auto_review" };
    case "full_access":
      return { profile: "danger_full_access", approval: "never", reviewer: "user" };
    case "agent":
    default:
      return { profile: "workspace_write", approval: "on_request", reviewer: "user" };
  }
}

function permissionSummaryMatchesPreset(permissions: PermissionSummary | undefined, mode: PermissionMode): boolean {
  const preset = permissionPresetAxes(mode);
  const profile = permissions?.permission_profile?.trim();
  const approval = permissions?.approval_policy?.trim();
  const reviewer = permissions?.approvals_reviewer?.trim();
  return (
    (!profile || profile === preset.profile) &&
    (!approval || approval === preset.approval) &&
    (!reviewer || reviewer === preset.reviewer)
  );
}

export function permissionModeFromSummary(permissions?: PermissionSummary): PermissionModeState {
  const mode = permissions?.mode?.trim();
  let preset: PermissionMode;
  switch (mode) {
    case "read_only":
      preset = "read_only";
      break;
    case "agent":
      preset = "agent";
      break;
    case "auto_review":
      preset = "auto_review";
      break;
    case "full_access":
      preset = "full_access";
      break;
    default:
      preset = "agent";
  }
  return permissionSummaryMatchesPreset(permissions, preset) ? preset : "custom";
}

export function permissionModeHasAdvancedOverrides(policy?: ToolPolicySummary, permissions?: PermissionSummary): boolean {
  return Boolean(
    permissionModeFromSummary(permissions) === "custom" ||
      policy?.default_action ||
      Object.keys(policy?.tools ?? {}).length > 0 ||
      Object.keys(policy?.kinds ?? {}).length > 0 ||
      Object.keys(policy?.risks ?? {}).length > 0
  );
}

export function permissionModeOption(mode: PermissionModeState): Omit<PermissionModeOption, "mode"> & { mode: PermissionModeState } {
  if (mode === "custom") {
    return CUSTOM_PERMISSION_MODE_OPTION;
  }
  return PERMISSION_MODE_OPTIONS.find((option) => option.mode === mode) ?? PERMISSION_MODE_OPTIONS[1];
}

export function RuntimePicker({
  variant,
  initialized,
  state,
  openMenu,
  anchorRef,
  running,
  onToggleMenu,
  onSelectModel,
  onSelectEffort
}: {
  variant: ComposerVariant;
  initialized: InitializeResult;
  state: CodexModelLoadState;
  openMenu: CodexRuntimeMenu;
  anchorRef: RefObject<HTMLDivElement | null>;
  running: boolean;
  onToggleMenu: (menu: Exclude<CodexRuntimeMenu, null>) => void;
  onSelectModel: (provider: string, model: string, variant?: string) => void;
  onSelectEffort: (variant: string) => void;
}): JSX.Element {
  const currentProvider = initialized.providers?.find((provider) => provider.name === initialized.provider);
  const codexProvider = isCodexProvider(initialized);
  const currentCodexModel = codexProvider ? state.models.find((model) => model.slug === initialized.model) : undefined;
  const currentProviderModel = currentProvider?.models?.find((model) => model.id === initialized.model);
  const currentVariant = initialized.variant ?? initialized.effort ?? "";
  const variantOptions = codexProvider
    ? codexEffortOptions(currentCodexModel, currentVariant)
    : providerModelVariantOptions(currentProvider, initialized.model, currentVariant);
  const reasoningMode = codexProvider
    ? "levels"
    : providerModelReasoningMode(currentProvider, initialized.model);
  const placement: FloatingMenuPlacement = variant === "hero" ? "below" : "above";
  return (
    <div className="codex-runtime-anchor" ref={anchorRef}>
      <button
        className="codex-runtime-trigger"
        type="button"
        disabled={running}
        aria-haspopup="menu"
        aria-expanded={openMenu !== null}
        onClick={() => onToggleMenu("main")}
      >
        <span>{runtimeTriggerLabel(initialized, currentProviderModel, currentCodexModel)}</span>
        <span className="codex-runtime-effort">{variantLabel(currentVariant)}</span>
        <ChevronDown className="icon" />
      </button>
      {openMenu === "main" ? (
        <FloatingMenuPortal
          anchorRef={anchorRef}
          owner="codex-runtime"
          placement={placement}
          align="right"
          width={236}
        >
          <RuntimeMainMenu
            selectedVariant={currentVariant}
            options={variantOptions}
            reasoningMode={reasoningMode}
            currentLabel={runtimeModelLabel(initialized, currentProviderModel, currentCodexModel)}
            onSelectEffort={onSelectEffort}
            onOpenModelMenu={() => onToggleMenu("model")}
          />
        </FloatingMenuPortal>
      ) : null}
      {openMenu === "model" ? (
        <FloatingMenuPortal
          anchorRef={anchorRef}
          owner="codex-runtime"
          placement={placement}
          align="right"
          width={286}
        >
          <RuntimeModelMenu
            initialized={initialized}
            state={state}
            selectedProvider={initialized.provider}
            selectedModel={initialized.model}
            selectedVariant={currentVariant}
            onSelectModel={onSelectModel}
          />
        </FloatingMenuPortal>
      ) : null}
    </div>
  );
}

function RuntimeMainMenu({
  selectedVariant,
  options,
  reasoningMode,
  currentLabel,
  onSelectEffort,
  onOpenModelMenu
}: {
  selectedVariant: string;
  options: string[];
  reasoningMode: "off" | "toggle" | "levels";
  currentLabel: string;
  onSelectEffort: (variant: string) => void;
  onOpenModelMenu: () => void;
}): JSX.Element {
  return (
    <div className="codex-runtime-menu codex-main-menu" role="menu">
      <div className="codex-menu-label">思考强度</div>
      {options.length > 1 ? (
        options.map((variant) => {
          const selected = variant === selectedVariant;
          return (
            <button key={variant || "auto"} role="menuitem" type="button" onClick={() => onSelectEffort(variant)}>
              <span>{variantLabel(variant)}</span>
              {selected ? <Check className="icon-lg" /> : null}
            </button>
          );
        })
      ) : (
        <div className="composer-menu-empty">
          {reasoningMode === "off" ? "当前模型不支持思考" : "当前模型没有可调思考强度"}
        </div>
      )}
      <div className="codex-menu-separator" />
      <button role="menuitem" type="button" onClick={onOpenModelMenu}>
        <span>{currentLabel}</span>
        <ChevronRight className="codex-menu-chevron icon-lg" />
      </button>
    </div>
  );
}

function RuntimeModelMenu({
  initialized,
  state,
  selectedProvider,
  selectedModel,
  selectedVariant,
  onSelectModel
}: {
  initialized: InitializeResult;
  state: CodexModelLoadState;
  selectedProvider: string;
  selectedModel: string;
  selectedVariant: string;
  onSelectModel: (provider: string, model: string, variant?: string) => void;
}): JSX.Element {
  const providers = initialized.providers ?? [];
  const codexProviderSelected = isCodexProvider(initialized);
  const configuredModels = providers
    .map((provider) => {
      const model = configuredRuntimeModelForProvider(provider, state);
      return model ? { provider, model } : undefined;
    })
    .filter((item): item is RuntimeModelOption => Boolean(item));
  const additionalModels = providers.flatMap((provider) =>
    runtimeModelsForProvider(provider, state)
      .filter((model) => model.id !== provider.model)
      .map((model) => ({ provider, model }))
  );
  return (
    <div className="codex-runtime-menu codex-model-menu" role="menu">
      <div className="codex-menu-label">已配置</div>
      {codexProviderSelected && state.loading ? <div className="composer-menu-empty">正在加载 Codex 模型</div> : null}
      {codexProviderSelected && state.error ? (
        <div className="composer-menu-note warning">
          <strong>无法读取 Codex 登录态</strong>
          <span>{state.error}</span>
        </div>
      ) : null}
      {!state.loading && providers.length === 0 ? (
        <div className="composer-menu-empty">没有可用模型</div>
      ) : null}
      {configuredModels.map(({ provider, model }) => (
        <RuntimeModelMenuItem
          key={`configured/${provider.name}/${model.id}`}
          provider={provider}
          model={model}
          selected={provider.name === selectedProvider && model.id === selectedModel}
          selectedVariant={selectedVariant}
          onSelectModel={onSelectModel}
        />
      ))}
      {additionalModels.length > 0 ? (
        <>
          <div className="codex-menu-separator" />
          <div className="codex-menu-label">更多模型</div>
          {additionalModels.map(({ provider, model }) => (
            <RuntimeModelMenuItem
              key={`additional/${provider.name}/${model.id}`}
              provider={provider}
              model={model}
              selected={provider.name === selectedProvider && model.id === selectedModel}
              selectedVariant={selectedVariant}
              onSelectModel={onSelectModel}
            />
          ))}
        </>
      ) : null}
    </div>
  );
}

type RuntimeModelOption = {
  provider: ProviderSummary;
  model: ProviderModelSummary;
};

function RuntimeModelMenuItem({
  provider,
  model,
  selected,
  selectedVariant,
  onSelectModel
}: {
  provider: ProviderSummary;
  model: ProviderModelSummary;
  selected: boolean;
  selectedVariant: string;
  onSelectModel: (provider: string, model: string, variant?: string) => void;
}): JSX.Element {
  const nextVariant = normalizedVariantForRuntimeModel(selectedVariant, provider, model);
  return (
    <button role="menuitem" type="button" onClick={() => onSelectModel(provider.name, model.id, nextVariant)}>
      <span>
        <strong>{providerModelDisplayName(model)}</strong>
        <small>{provider.name}</small>
      </span>
      {selected ? <Check className="icon-lg" /> : null}
    </button>
  );
}

function runtimeTriggerLabel(
  initialized: InitializeResult,
  providerModel?: ProviderModelSummary,
  codexModel?: CodexModelSummary
): string {
  if (codexModel) {
    return shortCodexModelLabel(codexModel.slug);
  }
  return shortCodexModelLabel(providerModel?.display_name || initialized.model);
}

function runtimeModelLabel(
  initialized: InitializeResult,
  providerModel?: ProviderModelSummary,
  codexModel?: CodexModelSummary
): string {
  if (codexModel) {
    return displayCodexModelName(codexModel);
  }
  return providerModel?.display_name || initialized.model;
}

function configuredRuntimeModelForProvider(
  provider: ProviderSummary,
  state: CodexModelLoadState
): ProviderModelSummary | undefined {
  if (!provider.model) {
    return undefined;
  }
  return (
    runtimeModelsForProvider(provider, state).find((model) => model.id === provider.model) ??
    provider.models?.find((model) => model.id === provider.model) ?? { id: provider.model, source: "selected" }
  );
}

function runtimeModelsForProvider(provider: ProviderSummary, state: CodexModelLoadState): ProviderModelSummary[] {
  const type = provider.type.trim().toLowerCase().replaceAll("_", "-");
  const isCodex = type === "openai-codex" || type === "codex-subscription" || type === "chatgpt-codex";
  if (isCodex && state.provider === provider.name && state.models.length > 0) {
    return state.models.map((model) => ({
      id: model.slug,
      display_name: displayCodexModelName(model),
      default_effort: model.default_reasoning_level,
      supported_efforts: model.supported_reasoning,
      source: "live"
    }));
  }
  if (provider.models?.length) {
    return provider.models;
  }
  return [{ id: provider.model, source: "selected" }];
}

function normalizedVariantForRuntimeModel(
  currentVariant: string,
  provider: ProviderSummary,
  model: ProviderModelSummary
): string {
  if (!currentVariant) {
    return "";
  }
  const modelVariants = (model.variants ?? []).map((item) => item.id).filter(Boolean);
  const supported = modelVariants.length > 0 ? modelVariants : model.supported_efforts ?? [];
  if (supported.length === 0) {
    return "";
  }
  if (supported.includes(currentVariant)) {
    return currentVariant;
  }
  if (model.default_variant && supported.includes(model.default_variant)) {
    return model.default_variant;
  }
  if (model.default_effort && supported.includes(model.default_effort)) {
    return model.default_effort;
  }
  const providerModel = provider.models?.find((item) => item.id === model.id);
  if (providerModel?.default_variant && supported.includes(providerModel.default_variant)) {
    return providerModel.default_variant;
  }
  if (providerModel?.default_effort && supported.includes(providerModel.default_effort)) {
    return providerModel.default_effort;
  }
  return "";
}

export function AccessMenu({
  permissions,
  policy,
  disabled,
  onSelect
}: {
  permissions?: PermissionSummary;
  policy?: ToolPolicySummary;
  disabled: boolean;
  onSelect: (mode: PermissionMode) => void;
}): JSX.Element {
  const hasOverrides = permissionModeHasAdvancedOverrides(policy, permissions);
  const mode = permissionModeFromSummary(permissions);
  const activeMode = hasOverrides ? undefined : mode;
  return (
    <div className="composer-context-menu access-menu" role="menu">
      {hasOverrides ? (
        <div className="composer-menu-note">
          <strong>自定义权限</strong>
          <span>当前设置不匹配任何预设；选择任一模式会改为该预设</span>
        </div>
      ) : null}
      {PERMISSION_MODE_OPTIONS.map((option) => (
        <button
          key={option.mode}
          className={`permission-mode-option${option.tone === "danger" ? " danger" : ""}`}
          role="menuitemradio"
          aria-checked={activeMode === option.mode}
          aria-label={option.label}
          type="button"
          disabled={disabled}
          onClick={() => onSelect(option.mode)}
        >
          <strong>{option.label}</strong>
        </button>
      ))}
    </div>
  );
}



export function BranchMenu({
  gitStatus,
  onSelectBranch
}: {
  gitStatus: GitStatusResult;
  onSelectBranch: (branch: string) => void;
}): JSX.Element {
  const branches = gitStatus.branches ?? [];
  return (
    <div className="composer-context-menu branch-menu" role="menu">
      {gitStatus.dirty_count > 0 ? (
        <div className="composer-menu-note warning">
          <strong>有未提交更改</strong>
          <span>{gitStatus.dirty_count} 个文件会随分支切换保留；如果会覆盖本地改动，Git 会拒绝切换。</span>
        </div>
      ) : null}
      {branches.length === 0 ? <div className="composer-menu-empty">没有本地分支</div> : null}
      {branches.map((branch) => {
        const selected = branch === gitStatus.branch;
        return (
          <button
            key={branch}
            role="menuitem"
            type="button"
            disabled={selected}
            onClick={() => onSelectBranch(branch)}
          >
            <GitBranch className="icon-lg" />
            <span>{branch}</span>
            {selected ? <Check className="icon" /> : null}
          </button>
        );
      })}
    </div>
  );
}

export function ProjectPickerMenu({
  projects,
  activeContext,
  query,
  setQuery,
  onSelectProject,
  onSelectNoProject,
  onCreateProject,
  onOpenProject
}: {
  projects: DesktopProject[];
  activeContext?: RuntimeContext;
  query: string;
  setQuery: (value: string) => void;
  onSelectProject: (id: string) => void;
  onSelectNoProject: () => void;
  onCreateProject: () => void;
  onOpenProject: () => void;
}): JSX.Element {
  const normalizedQuery = query.trim().toLocaleLowerCase();
  const filteredProjects = normalizedQuery
    ? projects.filter((project) => project.name.toLocaleLowerCase().includes(normalizedQuery) || project.path.toLocaleLowerCase().includes(normalizedQuery))
    : projects;

  return (
    <div className="composer-project-menu" role="menu">
      <label className="project-search">
        <Search className="icon-lg" />
        <input value={query} placeholder="搜索项目" onChange={(event) => setQuery(event.target.value)} />
      </label>
      <div className="project-picker-list">
        {filteredProjects.length === 0 ? <div className="project-picker-empty">没有匹配项目</div> : null}
        {filteredProjects.map((project) => {
          const selected = activeContext?.kind === "project" && activeContext.project_id === project.id;
          return (
            <button key={project.id} role="menuitem" onClick={() => onSelectProject(project.id)}>
              <Folder className="icon-lg" />
              <span>{project.name}</span>
              {selected ? <Check className="icon-lg" /> : null}
            </button>
          );
        })}
      </div>
      <div className="project-picker-divider" />
      <button role="menuitem" onClick={onOpenProject}>
        <FolderOpen className="icon-lg" />
        <span>使用现有文件夹</span>
      </button>
      <button role="menuitem" onClick={onCreateProject}>
        <FolderPlus className="icon-lg" />
        <span>新建空白项目</span>
      </button>
      <button role="menuitem" onClick={onSelectNoProject}>
        <FolderX className="icon-lg" />
        <span>不使用项目</span>
        {activeContext?.kind === "no_project" ? <Check className="icon-lg" /> : null}
      </button>
    </div>
  );
}
