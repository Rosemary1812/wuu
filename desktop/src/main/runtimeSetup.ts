import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import type { RuntimeSetupSaveParams, RuntimeSetupState } from "../shared/protocol";

type JsonObject = Record<string, unknown>;

type ProviderDraft = {
  provider: string;
  provider_type: string;
  base_url: string;
  model: string;
  api_key_env: string;
};

type AuthStore = {
  keys?: Record<string, string>;
  codex_oauth?: unknown;
};

const GLOBAL_CONFIG_RELATIVE = ".config/wuu/config.json";
const AUTH_RELATIVE = ".config/wuu/auth.json";
const PREFERENCES_RELATIVE = ".config/wuu/preferences.json";

const DEFAULT_PROVIDER: ProviderDraft = {
  provider: "openai",
  provider_type: "openai-compatible",
  base_url: "https://api.openai.com/v1",
  model: "gpt-4.1",
  api_key_env: "OPENAI_API_KEY"
};

export function readRuntimeSetup(workdir: string, home: string | undefined): RuntimeSetupState {
  const resolvedHome = requireHome(home);
  const existingConfigPath = findConfigPath(workdir, resolvedHome);
  const targetConfigPath = existingConfigPath ?? join(resolvedHome, GLOBAL_CONFIG_RELATIVE);

  if (!existingConfigPath) {
    return {
      required: true,
      reason: "missing_config",
      message: "未找到 wuu 配置",
      target_config_path: targetConfigPath,
      has_api_key: hasProviderCredential(resolvedHome, DEFAULT_PROVIDER),
      ...DEFAULT_PROVIDER
    };
  }

  const raw = readJsonObject(existingConfigPath);
  const provider = providerDraftFromConfig(raw);
  const hasApiKey = hasProviderCredential(resolvedHome, provider, raw);

  return {
    required: !hasApiKey,
    reason: hasApiKey ? "runtime_error" : "missing_api_key",
    message: hasApiKey ? "运行时启动失败" : "缺少模型访问密钥",
    config_path: existingConfigPath,
    target_config_path: targetConfigPath,
    has_api_key: hasApiKey,
    ...provider
  };
}

export function saveRuntimeSetup(
  workdir: string,
  home: string | undefined,
  params: RuntimeSetupSaveParams
): RuntimeSetupState {
  const resolvedHome = requireHome(home);
  const provider = normalizeSetupParams(params);
  const existingConfigPath = findConfigPath(workdir, resolvedHome);
  const targetConfigPath = existingConfigPath ?? join(resolvedHome, GLOBAL_CONFIG_RELATIVE);
  const raw = existingConfigPath ? readJsonObject(existingConfigPath) : defaultConfigJson();

  raw.default_provider = provider.provider;
  const providers = objectValue(raw.providers);
  raw.providers = providers;
  providers[provider.provider] = providerJson(provider, Boolean(params.api_key?.trim()), objectValue(providers[provider.provider]));
  if (!isJsonObject(raw.agent)) {
    raw.agent = {
      max_steps: 0,
      temperature: 0.2
    };
  }

  writeJson(targetConfigPath, raw, 0o644);
  if (params.api_key?.trim()) {
    saveAuthKey(resolvedHome, provider.provider, params.api_key.trim());
  }
  markOnboardingComplete(resolvedHome);

  return readRuntimeSetup(workdir, resolvedHome);
}

function findConfigPath(workdir: string, home: string): string | undefined {
  const candidates = [join(workdir, ".wuu.json"), join(workdir, "wuu.json"), join(home, GLOBAL_CONFIG_RELATIVE)];
  return candidates.find((candidate) => existsSync(candidate));
}

function providerDraftFromConfig(raw: JsonObject): ProviderDraft {
  const providers = objectValue(raw.providers);
  const configuredDefault = typeof raw.default_provider === "string" ? raw.default_provider.trim() : "";
  const providerName = configuredDefault || Object.keys(providers)[0] || DEFAULT_PROVIDER.provider;
  const provider = objectValue(providers[providerName]);
  const providerType = stringValue(provider.type) || DEFAULT_PROVIDER.provider_type;
  return {
    provider: providerName,
    provider_type: providerType,
    base_url: stringValue(provider.base_url) || defaultBaseURL(providerName, providerType),
    model: stringValue(provider.model) || defaultModel(providerName, providerType),
    api_key_env: stringValue(provider.api_key_env) || defaultAPIKeyEnv(providerName, providerType)
  };
}

function normalizeSetupParams(params: RuntimeSetupSaveParams): ProviderDraft {
  const provider = params.provider.trim();
  const providerType = params.provider_type.trim();
  const model = params.model.trim();
  const baseURL = params.base_url.trim();
  const apiKeyEnv = params.api_key_env.trim();
  if (!provider) {
    throw new Error("provider is required");
  }
  if (!providerType) {
    throw new Error("provider type is required");
  }
  if (!model) {
    throw new Error("model is required");
  }
  if (!baseURL && !isCodexSubscription(providerType)) {
    throw new Error("base URL is required");
  }
  return {
    provider,
    provider_type: providerType,
    base_url: baseURL,
    model,
    api_key_env: apiKeyEnv || defaultAPIKeyEnv(provider, providerType)
  };
}

function providerJson(provider: ProviderDraft, keySavedToAuthStore: boolean, existing: JsonObject): JsonObject {
  const out: JsonObject = {
    ...existing,
    type: provider.provider_type,
    base_url: provider.base_url,
    model: provider.model
  };
  if (provider.api_key_env) {
    out.api_key_env = provider.api_key_env;
  }
  if (isCodexSubscription(provider.provider_type)) {
    out.wire_api = "responses";
    delete out.api_key_env;
  }
  if (keySavedToAuthStore) {
    delete out.api_key;
    delete out.auth_token;
  }
  return out;
}

function defaultConfigJson(): JsonObject {
  return {
    default_provider: DEFAULT_PROVIDER.provider,
    providers: {},
    agent: {
      max_steps: 0,
      temperature: 0.2
    }
  };
}

function hasProviderCredential(home: string, provider: ProviderDraft, rawConfig?: JsonObject): boolean {
  if (isCodexSubscription(provider.provider_type)) {
    return hasCodexOAuth(home);
  }
  const configuredProvider = rawConfig
    ? objectValue(objectValue(rawConfig.providers)[provider.provider])
    : undefined;
  if (stringValue(configuredProvider?.api_key)) {
    return true;
  }
  if (stringValue(configuredProvider?.auth_token)) {
    return true;
  }
  const authTokenEnv = stringValue(configuredProvider?.auth_token_env);
  if (authTokenEnv && process.env[authTokenEnv]?.trim()) {
    return true;
  }
  const envKey = provider.api_key_env || defaultAPIKeyEnv(provider.provider, provider.provider_type);
  if (envKey && process.env[envKey]?.trim()) {
    return true;
  }
  return Boolean(readAuthStore(home).keys?.[provider.provider]?.trim());
}

function hasCodexOAuth(home: string): boolean {
  const store = readAuthStore(home);
  const oauth = objectValue(store.codex_oauth);
  const tokens = objectValue(oauth.tokens);
  return Boolean(stringValue(tokens.access_token));
}

function saveAuthKey(home: string, provider: string, apiKey: string): void {
  const store = readAuthStore(home);
  store.keys = store.keys ?? {};
  store.keys[provider] = apiKey;
  writeJson(join(home, AUTH_RELATIVE), store as JsonObject, 0o600);
}

function markOnboardingComplete(home: string): void {
  const path = join(home, PREFERENCES_RELATIVE);
  const preferences = existsSync(path) ? readJsonObject(path) : {};
  preferences.has_completed_onboarding = true;
  writeJson(path, preferences, 0o644);
}

function readAuthStore(home: string): AuthStore {
  const path = join(home, AUTH_RELATIVE);
  if (!existsSync(path)) {
    return { keys: {} };
  }
  try {
    const raw = readJsonObject(path);
    return {
      keys: recordOfStrings(raw.keys),
      codex_oauth: raw.codex_oauth
    };
  } catch {
    return { keys: {} };
  }
}

function readJsonObject(path: string): JsonObject {
  const value = JSON.parse(readFileSync(path, "utf8")) as unknown;
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error(`${path} must contain a JSON object`);
  }
  return value as JsonObject;
}

function writeJson(path: string, value: JsonObject, mode: number): void {
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, `${JSON.stringify(value, null, 2)}\n`, { mode });
}

function requireHome(home: string | undefined): string {
  const value = home?.trim();
  if (!value) {
    throw new Error("home directory is required");
  }
  return value;
}

function objectValue(value: unknown): JsonObject {
  if (!isJsonObject(value)) {
    return {};
  }
  return value;
}

function isJsonObject(value: unknown): value is JsonObject {
  return Boolean(value && typeof value === "object" && !Array.isArray(value));
}

function recordOfStrings(value: unknown): Record<string, string> {
  const raw = objectValue(value);
  const out: Record<string, string> = {};
  for (const [key, entry] of Object.entries(raw)) {
    if (typeof entry === "string") {
      out[key] = entry;
    }
  }
  return out;
}

function stringValue(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

function defaultBaseURL(provider: string, providerType: string): string {
  if (isAnthropic(providerType)) {
    return "https://api.anthropic.com";
  }
  if (provider === "openrouter") {
    return "https://openrouter.ai/api/v1";
  }
  if (isCodexSubscription(providerType)) {
    return "https://chatgpt.com/backend-api/codex";
  }
  return DEFAULT_PROVIDER.base_url;
}

function defaultModel(provider: string, providerType: string): string {
  if (isAnthropic(providerType)) {
    return "claude-3-5-sonnet-latest";
  }
  if (provider === "openrouter") {
    return "openai/gpt-4.1-mini";
  }
  if (isCodexSubscription(providerType)) {
    return "gpt-5.5";
  }
  return DEFAULT_PROVIDER.model;
}

function defaultAPIKeyEnv(provider: string, providerType: string): string {
  if (isAnthropic(providerType)) {
    return "ANTHROPIC_API_KEY";
  }
  if (provider === "openrouter") {
    return "OPENROUTER_API_KEY";
  }
  if (isCodexSubscription(providerType)) {
    return "";
  }
  return "OPENAI_API_KEY";
}

function isAnthropic(providerType: string): boolean {
  const normalized = providerType.toLowerCase().trim().replaceAll("_", "-");
  return normalized === "anthropic" || normalized === "claude" || normalized === "anthropic-official";
}

function isCodexSubscription(providerType: string): boolean {
  const normalized = providerType.toLowerCase().trim().replaceAll("_", "-");
  return normalized === "openai-codex" || normalized === "codex-subscription" || normalized === "chatgpt-codex";
}
