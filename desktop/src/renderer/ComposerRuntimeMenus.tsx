import {
  Brain,
  Bug,
  Check,
  ChevronDown,
  CircleHelp,
  Cpu,
  Eye,
  FileText,
  FlaskConical,
  FoldVertical,
  Folder,
  FolderOpen,
  FolderPlus,
  FolderX,
  Gauge,
  GitBranch,
  GitCommitHorizontal,
  GitCompare,
  GitPullRequest,
  Hammer,
  LifeBuoy,
  MessageSquarePlus,
  Paperclip,
  PieChart,
  Plus,
  Puzzle,
  RotateCcw,
  ScrollText,
  Search,
  Settings,
  Shield,
  Terminal,
  TriangleAlert,
  Zap,
  type LucideIcon
} from "lucide-react";
import { type CSSProperties, type RefObject, useEffect, useRef, useState } from "react";
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
import type { ComposerSlashCommand } from "./ComposerSlashCommands";
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
  providerIsCodex,
  providerModelDisplayName,
  providerModelVariantOptions,
  shortCodexModelLabel,
  variantLabel
} from "./RuntimeHelpers";
import { translateCurrent as translate, useI18n } from "./i18n";
import { Tooltip } from "./Tooltip";

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

function permissionModeOptions(): PermissionModeOption[] {
  return [
    {
      mode: "standard",
      label: translate("runtime.permission.standardLabel"),
      chipLabel: translate("runtime.permission.standard"),
      icon: Shield,
      chipTone: "neutral"
    },
    {
      mode: "read_only",
      label: translate("runtime.permission.readOnly"),
      chipLabel: translate("runtime.permission.readOnly"),
      icon: Eye,
      chipTone: "neutral"
    },
    {
      mode: "unconfined",
      label: translate("runtime.permission.unconfined"),
      chipLabel: translate("runtime.permission.unconfined"),
      icon: TriangleAlert,
      chipTone: "danger",
      tone: "danger"
    }
  ];
}

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
  const options = permissionModeOptions();
  return options.find((option) => option.mode === mode) ?? options[0];
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
  const placement: FloatingMenuPlacement = variant === "hero" ? "below" : "above";
  return (
    <div className="codex-runtime-anchor" ref={anchorRef}>
      <button
        className="codex-runtime-trigger"
        type="button"
        disabled={running}
        aria-haspopup="menu"
        aria-expanded={openMenu === "model"}
        onClick={() => onToggleMenu("model")}
      >
        <span>{runtimeTriggerLabel(initialized, currentProviderModel, currentCodexModel)}</span>
        <span className="codex-runtime-effort">{variantLabel(currentVariant)}</span>
        <ChevronDown className="icon" />
      </button>
      {openMenu === "model" ? (
        <FloatingMenuPortal
          anchorRef={anchorRef}
          owner="codex-runtime"
          placement={placement}
          align="right"
          width={320}
          flip
        >
          <RuntimeModelMenu
            initialized={initialized}
            state={state}
            selectedProvider={initialized.provider}
            selectedModel={initialized.model}
            selectedVariant={currentVariant}
            onSelectModel={onSelectModel}
            onSelectEffort={onSelectEffort}
          />
        </FloatingMenuPortal>
      ) : null}
    </div>
  );
}

function RuntimeModelMenu({
  initialized,
  state,
  selectedProvider,
  selectedModel,
  selectedVariant,
  onSelectModel,
  onSelectEffort
}: {
  initialized: InitializeResult;
  state: CodexModelLoadState;
  selectedProvider: string;
  selectedModel: string;
  selectedVariant: string;
  onSelectModel: (provider: string, model: string, variant?: string) => void;
  onSelectEffort: (variant: string) => void;
}): JSX.Element {
  const { t } = useI18n();
  const [query, setQuery] = useState("");
  // The panel stays open while a selection commits through the app-server
  // stream, so the highlighted row and the effort pills follow the click
  // immediately instead of waiting for the round-trip. Once the stream
  // confirms, the external props become the source of truth again and the
  // optimistic state is dropped.
  const [optimistic, setOptimistic] = useState<{ provider: string; model: string; variant: string } | null>(null);
  // The effort heading follows the slider thumb while dragging, so the level
  // being aimed at is readable before the commit lands on release.
  const [previewVariant, setPreviewVariant] = useState<string | null>(null);
  useEffect(() => {
    setOptimistic(null);
    setPreviewVariant(null);
  }, [selectedProvider, selectedModel, selectedVariant]);

  const providers = initialized.providers ?? [];
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

  // Group by provider with the configured model first, preserving provider
  // order. The group header is the provider name — the classification users
  // actually think in ("Claude is Anthropic's"), unlike a flat configured/
  // additional split.
  const groups: RuntimeModelGroup[] = [];
  const groupByProvider = new Map<string, RuntimeModelGroup>();
  for (const item of configuredModels) {
    const group = { provider: item.provider, models: [item.model] };
    groups.push(group);
    groupByProvider.set(item.provider.name, group);
  }
  for (const item of additionalModels) {
    const group = groupByProvider.get(item.provider.name);
    if (group) {
      group.models.push(item.model);
    } else {
      const created = { provider: item.provider, models: [item.model] };
      groups.push(created);
      groupByProvider.set(item.provider.name, created);
    }
  }

  const normalizedQuery = query.trim().toLocaleLowerCase();
  const filteredGroups = normalizedQuery
    ? groups
        .map((group) => ({
          ...group,
          models: group.models.filter(
            (model) =>
              providerModelDisplayName(model).toLocaleLowerCase().includes(normalizedQuery) ||
              model.id.toLocaleLowerCase().includes(normalizedQuery) ||
              group.provider.name.toLocaleLowerCase().includes(normalizedQuery)
          )
        }))
        .filter((group) => group.models.length > 0)
    : groups;

  // The effort pills act on the model the picker is about to run — the
  // optimistic target when one is pending, otherwise the committed selection.
  const effectiveProviderName = optimistic?.provider ?? selectedProvider;
  const effectiveModelID = optimistic?.model ?? selectedModel;
  const effectiveVariant = optimistic?.variant ?? selectedVariant;
  const effectiveProvider = providers.find((provider) => provider.name === effectiveProviderName);
  const effectiveCodex = providerIsCodex(initialized, effectiveProviderName);
  const effectiveCodexModel = effectiveCodex
    ? state.models.find((model) => model.slug === effectiveModelID)
    : undefined;
  const effortOptions = effectiveCodex
    ? codexEffortOptions(effectiveCodexModel, effectiveVariant)
    : providerModelVariantOptions(effectiveProvider, effectiveModelID, effectiveVariant);
  const showEffort = effortOptions.length > 1;

  return (
    <div className="codex-runtime-menu codex-model-menu" role="menu">
      <label className="select-menu-search">
        <Search className="select-menu-search-icon icon-lg" />
        <input
          type="search"
          value={query}
          placeholder={t("runtime.searchModels")}
          aria-label={t("runtime.searchModels")}
          onChange={(event) => setQuery(event.currentTarget.value)}
        />
      </label>
      <div className="codex-model-groups">
        {effectiveCodex && state.loading ? <div className="composer-menu-empty">{t("runtime.loadingCodexModels")}</div> : null}
        {effectiveCodex && state.error ? (
          <div className="composer-menu-note warning">
            <strong>{t("runtime.codexLoginUnavailable")}</strong>
            <span>{state.error}</span>
          </div>
        ) : null}
        {!state.loading && providers.length === 0 ? (
          <div className="composer-menu-empty">{t("runtime.noModels")}</div>
        ) : null}
        {filteredGroups.length === 0 ? (
          <div className="composer-menu-empty">{t("runtime.noMatchingModels")}</div>
        ) : null}
        {filteredGroups.map((group) => (
          <div className="codex-model-group" key={group.provider.name}>
            <div className="codex-menu-label codex-model-group-label">{group.provider.name}</div>
            {group.models.map((model) => (
              <RuntimeModelMenuItem
                key={`${group.provider.name}/${model.id}`}
                provider={group.provider}
                model={model}
                selected={group.provider.name === effectiveProviderName && model.id === effectiveModelID}
                selectedVariant={effectiveVariant}
                onSelectModel={(provider, model, variant) => {
                  setOptimistic({ provider, model, variant: variant ?? "" });
                  onSelectModel(provider, model, variant);
                }}
              />
            ))}
          </div>
        ))}
      </div>
      {showEffort ? (
        <div className="codex-effort-section">
          <div className="codex-menu-label codex-effort-heading">
            <span>{t("runtime.reasoningEffort")}</span>
            <span className="codex-effort-current">{variantLabel(previewVariant ?? effectiveVariant)}</span>
          </div>
          <EffortSlider
            options={effortOptions}
            selectedVariant={effectiveVariant}
            onPreviewEffort={setPreviewVariant}
            onSelectEffort={(variant) => {
              setPreviewVariant(null);
              setOptimistic((current) =>
                current
                  ? { ...current, variant }
                  : { provider: selectedProvider, model: selectedModel, variant }
              );
              onSelectEffort(variant);
            }}
          />
        </div>
      ) : null}
    </div>
  );
}

// Draggable reasoning-effort slider rendered as a bare capsule: the inked
// fill ends at the current stop, small dots mark the discrete levels, and the
// pill carries no level text — the heading above live-shows the current
// level while dragging. The thumb snaps to the discrete levels the model
// supports, and the commit fires once on release (or on a keyboard step)
// instead of once per intermediate position, so tuning never spams the
// runtime stream.
function EffortSlider({
  options,
  selectedVariant,
  onPreviewEffort,
  onSelectEffort
}: {
  options: string[];
  selectedVariant: string;
  onPreviewEffort: (variant: string | null) => void;
  onSelectEffort: (variant: string) => void;
}): JSX.Element {
  const selectedIndex = Math.max(0, options.indexOf(selectedVariant));
  const [previewIndex, setPreviewIndex] = useState<number | null>(null);
  const pendingIndex = useRef(selectedIndex);
  const pointerX = useRef<number | null>(null);
  const capsuleRef = useRef<HTMLDivElement>(null);
  const displayIndex = previewIndex ?? selectedIndex;

  useEffect(() => {
    setPreviewIndex(null);
  }, [selectedVariant]);

  const previewTo = (index: number | null): void => {
    setPreviewIndex(index);
    onPreviewEffort(index === null ? null : options[index]);
  };

  const commit = (): void => {
    pointerX.current = null;
    onPreviewEffort(null);
    onSelectEffort(options[pendingIndex.current]);
  };

  // Each option owns one equal span; the knob and marker sit on that span's
  // right edge rather than in its center.
  const stopFromPointer = (): number | null => {
    const capsule = capsuleRef.current;
    if (pointerX.current === null || !capsule) {
      return null;
    }
    const rect = capsule.getBoundingClientRect();
    if (rect.width <= 0) {
      return null;
    }
    const ratio = (pointerX.current - rect.left) / rect.width;
    if (!Number.isFinite(ratio)) {
      return null;
    }
    return Math.min(options.length - 1, Math.max(0, Math.floor(ratio * options.length)));
  };

  const stopPosition = ((displayIndex + 1) / options.length) * 100;
  const capsuleStyle = {
    "--effort-slider-fill": `${stopPosition}%`,
    "--effort-slider-pos": `${stopPosition}%`
  } as CSSProperties;
  // The top level reads as a charged state: a slow sheen sweeps the fill as
  // long as the thumb (or the committed value) sits on it.
  const maxed = displayIndex === options.length - 1;

  return (
    <div className="codex-effort-slider-wrap" style={capsuleStyle}>
      <div ref={capsuleRef} className={`codex-effort-capsule${maxed ? " maxed" : ""}`}>
        <span className="codex-effort-capsule-fill" aria-hidden="true" />
        <div className="codex-effort-stops" aria-hidden="true">
          {options.slice(0, -1).map((variant, index) => (
            <span
              key={variant || `default-${index}`}
              className="codex-effort-stop"
              style={{ left: `${((index + 1) / options.length) * 100}%` }}
            />
          ))}
        </div>
        {maxed ? <span className="codex-effort-capsule-sheen" aria-hidden="true" /> : null}
        <span className="codex-effort-knob" aria-hidden="true" />
        <input
          className="codex-effort-slider"
          type="range"
          min={0}
          max={options.length - 1}
          step={1}
          value={displayIndex}
          aria-label={translate("runtime.reasoningEffort")}
          aria-valuetext={variantLabel(options[displayIndex])}
          onPointerDown={(event) => {
            pointerX.current = event.clientX;
          }}
          onPointerMove={(event) => {
            pointerX.current = event.clientX;
          }}
          onChange={(event) => {
            const next = stopFromPointer() ?? Number(event.currentTarget.value);
            pendingIndex.current = next;
            previewTo(next);
          }}
          onPointerUp={commit}
          onPointerCancel={commit}
          onKeyDown={() => {
            // Keyboard takes over from the pointer: drop any hovered/dragged
            // position so arrows step from the committed value instead.
            pointerX.current = null;
          }}
          onKeyUp={commit}
          onBlur={() => {
            pointerX.current = null;
            previewTo(null);
          }}
        />
      </div>
    </div>
  );
}

type RuntimeModelOption = {
  provider: ProviderSummary;
  model: ProviderModelSummary;
};

type RuntimeModelGroup = {
  provider: ProviderSummary;
  models: ProviderModelSummary[];
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
  const nextVariant = selected
    ? selectedVariant
    : defaultVariantForRuntimeModel(provider, model);
  return (
    <button
      className="codex-model-item"
      role="menuitemradio"
      type="button"
      aria-checked={selected}
      onClick={() => onSelectModel(provider.name, model.id, nextVariant)}
    >
      <span className="codex-model-item-name">{providerModelDisplayName(model)}</span>
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

function defaultVariantForRuntimeModel(
  provider: ProviderSummary,
  model: ProviderModelSummary
): string {
  const modelVariants = (model.variants ?? []).map((item) => item.id).filter(Boolean);
  const supported = modelVariants.length > 0 ? modelVariants : model.supported_efforts ?? [];
  if (supported.length === 0) {
    return "";
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

// Composer-bar "+" menu: the single entry point for everything the composer
// can attach or invoke — attachments plus the full slash-command list
// (built-in actions, prompts, and skills; future plugin actions join here).
// Clicking a command behaves exactly like picking it in the "/" panel.
// Open state is local, so the host's floating-menu registry needs no wiring.
export function ComposerPlusButton({
  variant,
  disabled,
  commands,
  menuAnchorRef,
  onAddAttachment,
  onSelectCommand
}: {
  variant: ComposerVariant;
  disabled: boolean;
  commands: ComposerSlashCommand[];
  menuAnchorRef: RefObject<HTMLElement | null>;
  onAddAttachment: () => void;
  onSelectCommand: (command: ComposerSlashCommand) => void;
}): JSX.Element {
  const { t } = useI18n();
  const triggerRef = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);

  useEffect(() => {
    if (!open) {
      return undefined;
    }
    function handlePointerDown(event: PointerEvent): void {
      const target = event.target;
      if (!(target instanceof Node)) {
        return;
      }
      if (triggerRef.current?.contains(target)) {
        return;
      }
      if (isInsideFloatingMenu(target, "composer-plus")) {
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

  return (
    <div className="composer-plus-menu-anchor" ref={triggerRef}>
      <button
        className="composer-tool-button composer-plus-button"
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={t("composer.plusMenu")}
        title={t("composer.plusMenu")}
        disabled={disabled}
        onClick={() => setOpen((current) => !current)}
      >
        <Plus aria-hidden="true" />
      </button>
      {open ? (
        <FloatingMenuPortal
          anchorRef={menuAnchorRef}
          owner="composer-plus"
          placement="above"
          align="left"
          offset={variant === "hero" ? 10 : 8}
          width={320}
          matchAnchorWidth
        >
          <div className="composer-context-menu composer-plus-menu" role="menu" aria-label={t("composer.plusMenu")}>
            <div className="composer-plus-menu-section" role="presentation">{t("composer.plusSectionAdd")}</div>
            <button
              role="menuitem"
              type="button"
              onClick={() => {
                setOpen(false);
                onAddAttachment();
              }}
            >
              <Paperclip className="icon-lg" />
              <span className="composer-plus-menu-item-title">{t("composer.addAttachment")}</span>
              <span className="composer-plus-menu-item-desc">{t("composer.addAttachmentHint")}</span>
            </button>
            <div className="composer-plus-menu-section" role="presentation">{t("composer.plusSectionCommands")}</div>
            {commands.map((command) => (
              <Tooltip content={command.disabledReason} key={command.id}>
                <button
                  role="menuitem"
                  type="button"
                  disabled={Boolean(command.disabledReason)}
                  onClick={() => {
                    setOpen(false);
                    onSelectCommand(command);
                  }}
                >
                  <SlashCommandIcon command={command} />
                  <span className="composer-plus-menu-item-title">
                    {command.kind === "skill" ? command.description : command.title}
                  </span>
                  <span className="composer-plus-menu-item-desc">
                    {command.kind === "skill" ? command.title : command.description}
                  </span>
                </button>
              </Tooltip>
            ))}
          </div>
        </FloatingMenuPortal>
      ) : null}
    </div>
  );
}

// Icon resolver shared by the "/" command panel and the "+" menu so both
// surfaces show the same glyph for the same command.
export function SlashCommandIcon({ command }: { command: ComposerSlashCommand }): JSX.Element {
  switch (command.action ?? command.id) {
    case "review":
      return <Search className="icon" />;
    case "open-review":
      return <GitCompare className="icon" />;
    case "debug":
      return <Bug className="icon" />;
    case "fix":
      return <Hammer className="icon" />;
    case "helpme":
      return <LifeBuoy className="icon" />;
    case "test":
      return <FlaskConical className="icon" />;
    case "explain":
      return <CircleHelp className="icon" />;
    case "commit":
      return <GitCommitHorizontal className="icon" />;
    case "pr":
      return <GitPullRequest className="icon" />;
    case "open-skills":
      return <Puzzle className="icon" />;
    case "new-thread":
      return <MessageSquarePlus className="icon" />;
    case "open-terminal":
      return <Terminal className="icon" />;
    case "open-files":
      return <FileText className="icon" />;
    case "open-project":
      return <FolderOpen className="icon" />;
    case "no-project":
      return <FolderX className="icon" />;
    case "reset-side-thread":
      return <RotateCcw className="icon" />;
    case "context":
      return <PieChart className="icon" />;
    case "compact":
      return <FoldVertical className="icon" />;
    case "instructions":
      return <ScrollText className="icon" />;
    case "open-memory":
      return <Brain className="icon" />;
    case "fast":
      return <Zap className="icon" />;
    case "model":
      return <Cpu className="icon" />;
    case "effort":
      return <Gauge className="icon" />;
    case "settings":
      return <Settings className="icon" />;
    default:
      return <Puzzle className="icon" />;
  }
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
  useI18n();
  const mode = permissionModeFromSummary(permissions);
  const options = permissionModeOptions();
  return (
    <div className="composer-context-menu access-menu" role="menu">
      {options.map((option) => (
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
  const { t } = useI18n();
  const branches = gitStatus.branches ?? [];
  return (
    <div className="composer-context-menu branch-menu" role="menu">
      {gitStatus.dirty_count > 0 ? (
        <div className="composer-menu-note warning">
          <strong>{t("runtime.uncommittedChanges")}</strong>
          <span>{t(gitStatus.dirty_count === 1 ? "runtime.dirtyFileWarningOne" : "runtime.dirtyFileWarning", { count: gitStatus.dirty_count })}</span>
        </div>
      ) : null}
      {branches.length === 0 ? <div className="composer-menu-empty">{t("runtime.noLocalBranches")}</div> : null}
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
  const { t } = useI18n();
  const normalizedQuery = query.trim().toLocaleLowerCase();
  const filteredProjects = normalizedQuery
    ? projects.filter((project) => project.name.toLocaleLowerCase().includes(normalizedQuery) || project.path.toLocaleLowerCase().includes(normalizedQuery))
    : projects;

  return (
    <div className="composer-project-menu" role="menu">
      <label className="project-search">
        <Search className="icon-lg" />
        <input value={query} placeholder={t("runtime.searchProjects")} onChange={(event) => setQuery(event.target.value)} />
      </label>
      <div className="project-picker-list">
        {filteredProjects.length === 0 ? <div className="project-picker-empty">{t("runtime.noMatchingProjects")}</div> : null}
        {filteredProjects.map((project) => {
          const selected = activeContext?.kind === "project" && activeContext.project_id === project.id;
          return (
            <button key={project.id} type="button" role="menuitem" onClick={() => onSelectProject(project.id)}>
              <Folder className="icon-lg" />
              <span>{project.name}</span>
              {selected ? <Check className="icon-lg" /> : null}
            </button>
          );
        })}
      </div>
      <div className="project-picker-divider" />
      <button type="button" role="menuitem" onClick={onOpenProject}>
        <FolderOpen className="icon-lg" />
        <span>{t("runtime.useExistingFolder")}</span>
      </button>
      <button type="button" role="menuitem" onClick={onCreateProject}>
        <FolderPlus className="icon-lg" />
        <span>{t("runtime.createBlankProject")}</span>
      </button>
      <button type="button" role="menuitem" onClick={onSelectNoProject}>
        <FolderX className="icon-lg" />
        <span>{t("runtime.noProject")}</span>
        {activeContext?.kind === "no_project" ? <Check className="icon-lg" /> : null}
      </button>
    </div>
  );
}
