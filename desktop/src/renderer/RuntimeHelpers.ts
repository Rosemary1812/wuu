import type { CodexModelSummary, GitStatusResult, InitializeResult, ProviderModelSummary, ProviderSummary } from "../shared/protocol";

export function isCodexProvider(initialized: InitializeResult): boolean {
  const summary = initialized.providers?.find((provider) => provider.name === initialized.provider);
  const type = (summary?.type ?? initialized.provider).trim().toLowerCase().replaceAll("_", "-");
  return type === "openai-codex" || type === "codex-subscription" || type === "chatgpt-codex";
}

export function displayCodexModelName(model?: CodexModelSummary): string {
  return model?.display_name || model?.slug || "GPT";
}

export function shortCodexModelLabel(model: string): string {
  return model.replace(/^gpt-/i, "");
}

export function codexEffortLabel(effort: string): string {
  switch (effort) {
    case "":
      return "默认";
    case "none":
      return "无";
    case "minimal":
      return "最少";
    case "low":
      return "低";
    case "medium":
      return "中";
    case "high":
      return "高";
    case "xhigh":
    case "max":
      return "超高";
    default:
      return effort;
  }
}

export function effortLabel(effort: string): string {
  return codexEffortLabel(effort);
}

export function variantLabel(variant: string): string {
  return codexEffortLabel(variant);
}

export function providerModelDisplayName(model?: ProviderModelSummary): string {
  return model?.display_name || model?.id || "model";
}

export function providerModelVariantOptions(
  provider: ProviderSummary | undefined,
  modelID: string,
  currentVariant: string
): string[] {
  const model = provider?.models?.find((item) => item.id === modelID);
  const variants = (model?.variants ?? []).map((item) => item.id).filter(Boolean);
  const supported = variants.length > 0 ? variants : (model?.supported_efforts ?? []).filter(Boolean);
  const options = ["", ...supported];
  // When the model supports thinking but exposes no adjustable levels,
  // still let the user explicitly turn thinking off via "none" instead
  // of silently locking them to the model's default behavior.
  if (supported.length === 0 && model?.capabilities?.reasoning === true && !options.includes("none")) {
    options.push("none");
  }
  if (currentVariant && !options.includes(currentVariant)) {
    options.push(currentVariant);
  }
  return options;
}

export function providerModelReasoningMode(
  provider: ProviderSummary | undefined,
  modelID: string
): "off" | "toggle" | "levels" {
  const model = provider?.models?.find((item) => item.id === modelID);
  if (!model) {
    return "off";
  }
  const variants = (model.variants ?? []).map((item) => item.id).filter(Boolean);
  const supported = variants.length > 0 ? variants : (model.supported_efforts ?? []).filter(Boolean);
  if (supported.length > 0) {
    return "levels";
  }
  return model.capabilities?.reasoning === true ? "toggle" : "off";
}

export function providerModelEffortOptions(
  provider: ProviderSummary | undefined,
  modelID: string,
  currentEffort: string
): string[] {
  return providerModelVariantOptions(provider, modelID, currentEffort);
}

export function normalizedEffortForProviderModel(
  currentEffort: string,
  provider: ProviderSummary | undefined,
  modelID: string
): string {
  if (!currentEffort) {
    return "";
  }
  const model = provider?.models?.find((item) => item.id === modelID);
  const supported = model?.supported_efforts ?? [];
  if (supported.length === 0 || supported.includes(currentEffort)) {
    return currentEffort;
  }
  if (model?.default_effort && supported.includes(model.default_effort)) {
    return model.default_effort;
  }
  return "";
}

export function normalizedVariantForProviderModel(
  currentVariant: string,
  provider: ProviderSummary | undefined,
  modelID: string
): string {
  if (!currentVariant) {
    return "";
  }
  const model = provider?.models?.find((item) => item.id === modelID);
  const variants = (model?.variants ?? []).map((item) => item.id).filter(Boolean);
  const supported = variants.length > 0 ? variants : model?.supported_efforts ?? [];
  if (supported.length === 0 || supported.includes(currentVariant)) {
    return currentVariant;
  }
  if (model?.default_variant && supported.includes(model.default_variant)) {
    return model.default_variant;
  }
  if (model?.default_effort && supported.includes(model.default_effort)) {
    return model.default_effort;
  }
  return "";
}

export function codexEffortOptions(model: CodexModelSummary | undefined, currentEffort: string): string[] {
  const defaults = ["low", "medium", "high", "xhigh"];
  const supported = (model?.supported_reasoning?.length ? model.supported_reasoning : defaults).filter(Boolean);
  const options = ["", ...supported];
  if (currentEffort && !options.includes(currentEffort)) {
    options.push(currentEffort);
  }
  return options;
}

export function normalizedEffortForModel(currentEffort: string, model: CodexModelSummary): string {
  if (currentEffort === "") {
    return "";
  }
  const supported = model.supported_reasoning ?? [];
  if (supported.length === 0 || supported.includes(currentEffort)) {
    return currentEffort;
  }
  if (model.default_reasoning_level && supported.includes(model.default_reasoning_level)) {
    return model.default_reasoning_level;
  }
  return "";
}

export function pullRequestUnavailableReason(gitStatus?: GitStatusResult): string {
  if (!gitStatus?.is_repo) {
    return "不是 Git 仓库";
  }
  if (!gitStatus.gh_available) {
    return "未安装 GitHub CLI";
  }
  if (gitStatus.detached || !gitStatus.branch) {
    return "需要具名分支";
  }
  if (gitStatus.default_branch && gitStatus.branch === gitStatus.default_branch) {
    return "先创建功能分支";
  }
  if (gitStatus.dirty_count > 0) {
    return "先提交本地更改";
  }
  return "";
}

export function humanizeBranchTitle(branch: string): string {
  return branch
    .split(/[/-]/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toLocaleUpperCase() + part.slice(1))
    .join(" ");
}
