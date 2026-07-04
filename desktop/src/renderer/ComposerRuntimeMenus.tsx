import {
  Check,
  ChevronDown,
  ChevronRight,
  Eye,
  Focus,
  Folder,
  FolderOpen,
  FolderPlus,
  FolderX,
  GitBranch,
  Home,
  LayoutGrid,
  Search,
  Shield,
  TriangleAlert,
  type LucideIcon
} from "lucide-react";
import { type RefObject, useEffect, useRef, useState } from "react";
import type {
  CodexModelSummary,
  DesktopProject,
  GitStatusResult,
  InitializeResult,
  PermissionSummary,
  ProviderModelSummary,
  ProviderSummary,
  RuntimeContext
} from "../shared/protocol";
import { FloatingMenuPortal, isInsideFloatingMenu } from "./ComposerFloatingMenu";
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

type PermissionModeState = PermissionMode;

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
    mode: "standard",
    label: "工作区内完全信任",
    chipLabel: "标准",
    icon: Shield,
    chipTone: "neutral"
  },
  {
    mode: "read_only",
    label: "只读",
    chipLabel: "只读",
    icon: Eye,
    chipTone: "neutral"
  },
  {
    mode: "unconfined",
    label: "无边界",
    chipLabel: "无边界",
    icon: TriangleAlert,
    chipTone: "danger",
    tone: "danger"
  }
];

export function permissionModeFromSummary(permissions?: PermissionSummary): PermissionModeState {
  const mode = permissions?.mode?.trim();
  switch (mode) {
    case "read_only":
      return "read_only";
    case "unconfined":
      return "unconfined";
    case "standard":
      return "standard";
    default:
      return "standard";
  }
}

export function permissionModeHasAdvancedOverrides(_permissions?: PermissionSummary): boolean {
  return false;
}

export function permissionModeOption(mode: PermissionModeState): Omit<PermissionModeOption, "mode"> & { mode: PermissionModeState } {
  return PERMISSION_MODE_OPTIONS.find((option) => option.mode === mode) ?? PERMISSION_MODE_OPTIONS[0];
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
  disabled,
  onSelect
}: {
  permissions?: PermissionSummary;
  disabled: boolean;
  onSelect: (mode: PermissionMode) => void;
}): JSX.Element {
  const mode = permissionModeFromSummary(permissions);
  return (
    <div className="composer-context-menu access-menu" role="menu">
      {PERMISSION_MODE_OPTIONS.map((option) => (
        <button
          key={option.mode}
          className={`permission-mode-option${option.tone === "danger" ? " danger" : ""}`}
          role="menuitemradio"
          aria-checked={mode === option.mode}
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

// --- Chat-style composer "work focus" chip ----------------------------
//
// DM/group threads (chat-style-threads-design.md) get their own focus
// control, distinct from ProjectPickerMenu above: that one binds a
// *normal* session to a project directory; this one is a per-thread,
// sticky selection of how much of the workspace a resident agent's
// chat-style thread can touch this turn (all workspaces / one named
// project / the agent's own personal space). Same visual language
// (composer-project-menu, project-search, project-picker-list) but its
// own semantics, so it stays a separate component rather than a variant
// of ProjectPickerMenu.

// Wire value for the "全部工作区" (all workspaces) selection — mirrors
// an absent/blank Thread.focus_workspace.
const CHAT_FOCUS_ALL = "";
// Wire value for the "仅个人空间" (personal space only) selection.
const CHAT_FOCUS_HOME = "~";

function chatFocusChipDisplay(value: string): {
  label: string;
  isDefault: boolean;
  title: string;
  ariaLabel: string;
} {
  const trimmed = value.trim();
  if (trimmed === CHAT_FOCUS_HOME) {
    return {
      label: "⌂ 个人",
      isDefault: false,
      title: "仅个人空间",
      ariaLabel: "工作焦点：仅个人空间"
    };
  }
  if (trimmed === CHAT_FOCUS_ALL) {
    // Default state renders as a bare, de-emphasized icon with no
    // visible label — the chip stays invisible-ish until the user
    // narrows the focus.
    return { label: "", isDefault: true, title: "全部工作区", ariaLabel: "工作焦点：全部工作区" };
  }
  return { label: trimmed, isDefault: false, title: trimmed, ariaLabel: `工作焦点：${trimmed}` };
}

export function ChatFocusChip({
  value,
  projects,
  disabled,
  onChange
}: {
  value: string;
  projects: DesktopProject[];
  disabled?: boolean;
  onChange: (value: string) => void;
}): JSX.Element {
  const anchorRef = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");

  // Self-contained open/close state — deliberately not lifted into the
  // host app's shared floating-menu registry (unlike the runtime/access/
  // branch menus above) so this control has no coupling to that
  // registry's state or its outside-click effect. isInsideFloatingMenu
  // is the same helper the host uses for its own menus; borrowing it
  // here keeps "click inside the portal doesn't close it" behavior
  // identical without requiring host wiring.
  useEffect(() => {
    if (!open) {
      return undefined;
    }
    function handlePointerDown(event: PointerEvent): void {
      const target = event.target;
      if (!(target instanceof Node)) {
        return;
      }
      if (anchorRef.current?.contains(target)) {
        return;
      }
      if (isInsideFloatingMenu(target, "composer-focus")) {
        return;
      }
      setOpen(false);
    }
    function handleKeyDown(event: KeyboardEvent): void {
      if (event.key === "Escape") {
        setOpen(false);
      }
    }
    document.addEventListener("pointerdown", handlePointerDown);
    document.addEventListener("keydown", handleKeyDown);
    return () => {
      document.removeEventListener("pointerdown", handlePointerDown);
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, [open]);

  const display = chatFocusChipDisplay(value);

  return (
    <div className="chat-focus-menu-anchor" ref={anchorRef}>
      <button
        type="button"
        className={
          display.isDefault
            ? "composer-tool-button chat-focus-chip is-default"
            : "permission-chip tone-neutral chat-focus-chip"
        }
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={display.ariaLabel}
        title={display.title}
        disabled={disabled}
        onClick={() => setOpen((current) => !current)}
      >
        <Focus aria-hidden="true" />
        {display.isDefault ? null : <span>{display.label}</span>}
      </button>
      {open ? (
        <FloatingMenuPortal
          anchorRef={anchorRef}
          owner="composer-focus"
          placement="above"
          align="left"
          width={260}
        >
          <ChatFocusMenu
            value={value}
            projects={projects}
            query={query}
            setQuery={setQuery}
            onSelect={(next) => {
              onChange(next);
              setOpen(false);
            }}
          />
        </FloatingMenuPortal>
      ) : null}
    </div>
  );
}

function ChatFocusMenu({
  value,
  projects,
  query,
  setQuery,
  onSelect
}: {
  value: string;
  projects: DesktopProject[];
  query: string;
  setQuery: (value: string) => void;
  onSelect: (value: string) => void;
}): JSX.Element {
  const normalizedQuery = query.trim().toLocaleLowerCase();
  const filteredProjects = normalizedQuery
    ? projects.filter((project) => project.name.toLocaleLowerCase().includes(normalizedQuery))
    : projects;
  const trimmedValue = value.trim();
  return (
    <div className="composer-project-menu chat-focus-menu" role="menu">
      <button
        role="menuitemradio"
        aria-checked={trimmedValue === CHAT_FOCUS_ALL}
        onClick={() => onSelect(CHAT_FOCUS_ALL)}
      >
        <LayoutGrid className="icon-lg" />
        <span>全部工作区</span>
        {trimmedValue === CHAT_FOCUS_ALL ? <Check className="icon-lg" /> : null}
      </button>
      <div className="project-picker-divider" />
      <label className="project-search">
        <Search className="icon-lg" />
        <input value={query} placeholder="搜索项目" onChange={(event) => setQuery(event.target.value)} />
      </label>
      <div className="project-picker-list">
        {filteredProjects.length === 0 ? <div className="project-picker-empty">没有匹配项目</div> : null}
        {filteredProjects.map((project) => {
          const selected = trimmedValue === project.name;
          return (
            <button
              key={project.id}
              role="menuitemradio"
              aria-checked={selected}
              onClick={() => onSelect(project.name)}
            >
              <Folder className="icon-lg" />
              <span>{project.name}</span>
              {selected ? <Check className="icon-lg" /> : null}
            </button>
          );
        })}
      </div>
      <div className="project-picker-divider" />
      <button
        role="menuitemradio"
        aria-checked={trimmedValue === CHAT_FOCUS_HOME}
        onClick={() => onSelect(CHAT_FOCUS_HOME)}
      >
        <Home className="icon-lg" />
        <span>仅个人空间</span>
        {trimmedValue === CHAT_FOCUS_HOME ? <Check className="icon-lg" /> : null}
      </button>
    </div>
  );
}
