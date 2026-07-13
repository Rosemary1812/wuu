import type { SetStateAction } from "react";
import type {
  RuntimeAdvancedSettingsUpdate,
  RuntimeConnectionUpdate,
  RuntimeGeneralSettingsUpdate,
} from "../shared/protocol";
import {
  activeThreadForState,
  threadForPane,
  type AppState,
  type ConversationPaneID,
} from "./AppState";
import type {
  CodexModelLoadState,
  CodexRuntimeMenu,
  PermissionMode,
} from "./ComposerTypes";
import { isCodexProvider } from "./RuntimeHelpers";

type SetAppState = (update: SetStateAction<AppState>) => void;

export type RuntimeSettingsActionsDeps = {
  getAppState: () => AppState;
  setAppState: SetAppState;
  getViewContextSwitchPending: () => boolean;
  getCodexModels: () => CodexModelLoadState;
  setCodexModels: (update: SetStateAction<CodexModelLoadState>) => void;
  setRuntimeMenuOpen: (open: boolean) => void;
  setAccessMenuOpen: (open: boolean) => void;
  setBranchMenuOpen: (open: boolean) => void;
  setCodexRuntimeMenu: (update: SetStateAction<CodexRuntimeMenu>) => void;
  setSelectedPermissionMode: (mode: PermissionMode) => void;
  clearThreadPendingComposerMessages: (threadID: string) => void;
};

export type RuntimeSettingsActions = {
  updateRuntimeSettings: (
    provider: string,
    model: string,
    effort?: string,
    connection?: RuntimeConnectionUpdate,
    variant?: string,
    permissionMode?: string,
  ) => Promise<void>;
  updateAdvancedSettings: (
    settings: RuntimeAdvancedSettingsUpdate,
  ) => Promise<void>;
  updateGeneralSettings: (
    settings: RuntimeGeneralSettingsUpdate,
  ) => Promise<void>;
  removeProvider: (
    provider: string,
    options?: { fallbackProvider?: string; fallbackModel?: string },
  ) => Promise<void>;
  toggleCodexRuntimeMenu: (menu: Exclude<CodexRuntimeMenu, null>) => void;
  loadCodexModelsForProvider: (provider: string) => Promise<void>;
  selectRuntimeModel: (
    provider: string,
    model: string,
    variant?: string,
  ) => Promise<void>;
  selectRuntimeEffort: (nextVariant: string) => Promise<void>;
  selectPermissionMode: (mode: PermissionMode) => Promise<void>;
  interrupt: () => Promise<void>;
  interruptPane: (pane: ConversationPaneID) => Promise<void>;
};

export function createRuntimeSettingsActions(
  deps: RuntimeSettingsActionsDeps,
): RuntimeSettingsActions {
  async function updateRuntimeSettings(
    provider: string,
    model: string,
    effort?: string,
    connection?: RuntimeConnectionUpdate,
    variant?: string,
    permissionMode?: string,
  ): Promise<void> {
    const state = deps.getAppState();
    const nextProvider = provider.trim();
    const nextModel = model.trim();
    const nextEffort = effort === undefined ? undefined : effort.trim();
    const nextVariant = variant === undefined ? undefined : variant.trim();
    const nextPermissionMode =
      permissionMode === undefined ? undefined : permissionMode.trim();
    const nextConnection =
      connection === undefined
        ? undefined
        : {
            ...(connection.base_url === undefined
              ? {}
              : { base_url: connection.base_url.trim() }),
            ...(connection.api_key === undefined
              ? {}
              : { api_key: connection.api_key.trim() }),
            ...(connection.auth_token === undefined
              ? {}
              : { auth_token: connection.auth_token.trim() }),
            ...(connection.type !== undefined && connection.type !== ""
              ? { type: connection.type }
              : {}),
            ...(connection.create_provider ? { create_provider: true } : {}),
          };
    const currentProvider = state.initialized?.providers?.find(
      (item) => item.name === nextProvider,
    );
    const connectionChanged =
      Boolean(nextConnection?.create_provider) ||
      Boolean(nextConnection?.api_key) ||
      Boolean(nextConnection?.auth_token) ||
      (nextConnection?.base_url !== undefined &&
        nextConnection.base_url !== (currentProvider?.base_url ?? ""));
    const currentPermissionMode = state.initialized?.permissions?.mode ?? "";
    const permissionModeChanged =
      nextPermissionMode !== undefined &&
      nextPermissionMode !== currentPermissionMode;
    if (
      !nextProvider ||
      !nextModel ||
      !state.initialized ||
      (nextProvider === state.initialized.provider &&
        nextModel === state.initialized.model &&
        (nextEffort === undefined ||
          nextEffort === (state.initialized.effort ?? "")) &&
        (nextVariant === undefined ||
          nextVariant === (state.initialized.variant ?? "")) &&
        !connectionChanged &&
        !permissionModeChanged)
    ) {
      return;
    }
    try {
      const updated = await window.wuu.updateRuntimeSettings(
        nextProvider,
        nextModel,
        nextEffort,
        nextConnection,
        nextVariant,
        nextPermissionMode,
      );
      deps.setAppState((current) => {
        const initialized = current.initialized
          ? {
              ...current.initialized,
              provider: updated.provider,
              model: updated.model,
              effort: updated.effort ?? "",
              variant: updated.variant ?? "",
              permissions:
                updated.permissions ?? current.initialized.permissions,
              extension_trust:
                updated.extension_trust ?? current.initialized.extension_trust,
              providers: updated.providers ?? current.initialized.providers,
              advanced_settings:
                updated.advanced_settings ??
                current.initialized.advanced_settings,
            }
          : current.initialized;
        return {
          ...current,
          initialized,
          status: current.status === "ready" ? current.status : "ready",
        };
      });
    } catch (error) {
      deps.setAppState((current) => ({
        ...current,
        status:
          error instanceof Error
            ? error.message
            : "update runtime settings failed",
      }));
      throw error;
    }
  }

  async function updateAdvancedSettings(
    settings: RuntimeAdvancedSettingsUpdate,
  ): Promise<void> {
    if (!deps.getAppState().initialized || deps.getViewContextSwitchPending()) {
      return;
    }
    try {
      const updated = await window.wuu.updateAdvancedSettings(settings);
      deps.setAppState((current) => {
        const initialized = current.initialized
          ? {
              ...current.initialized,
              advanced_settings: updated.advanced_settings,
              providers: updated.providers ?? current.initialized.providers,
            }
          : current.initialized;
        return {
          ...current,
          initialized,
          status: current.status === "ready" ? current.status : "ready",
        };
      });
    } catch (error) {
      deps.setAppState((current) => ({
        ...current,
        status:
          error instanceof Error
            ? error.message
            : "update advanced settings failed",
      }));
      throw error;
    }
  }

  async function updateGeneralSettings(
    settings: RuntimeGeneralSettingsUpdate,
  ): Promise<void> {
    if (!deps.getAppState().initialized || deps.getViewContextSwitchPending()) {
      return;
    }
    try {
      const updated = await window.wuu.updateGeneralSettings(settings);
      deps.setAppState((current) => {
        const initialized = current.initialized
          ? {
              ...current.initialized,
              general_settings: updated.general_settings,
            }
          : current.initialized;
        return {
          ...current,
          initialized,
          status: current.status === "ready" ? current.status : "ready",
        };
      });
    } catch (error) {
      deps.setAppState((current) => ({
        ...current,
        status:
          error instanceof Error
            ? error.message
            : "update general settings failed",
      }));
      throw error;
    }
  }

  async function removeProvider(
    provider: string,
    options?: { fallbackProvider?: string; fallbackModel?: string },
  ): Promise<void> {
    const state = deps.getAppState();
    if (!state.initialized || deps.getViewContextSwitchPending()) {
      return;
    }
    const target = provider.trim();
    if (!target) {
      return;
    }
    try {
      const updated = await window.wuu.removeProvider(target, options);
      deps.setAppState((current) => {
        const initialized = current.initialized
          ? {
              ...current.initialized,
              provider: updated.provider ?? current.initialized.provider,
              model: updated.model ?? current.initialized.model,
              effort: updated.effort ?? current.initialized.effort,
              variant: updated.variant ?? current.initialized.variant,
              permissions:
                updated.permissions ?? current.initialized.permissions,
              extension_trust:
                updated.extension_trust ?? current.initialized.extension_trust,
              providers: updated.providers ?? current.initialized.providers,
              advanced_settings:
                updated.advanced_settings ??
                current.initialized.advanced_settings,
            }
          : current.initialized;
        return {
          ...current,
          initialized,
          status: current.status === "ready" ? current.status : "ready",
        };
      });
      if (state.initialized) {
        void loadCodexModelsForProvider(updated.provider);
      }
    } catch (error) {
      deps.setAppState((current) => ({
        ...current,
        status:
          error instanceof Error ? error.message : "remove provider failed",
      }));
      throw error;
    }
  }

  function toggleCodexRuntimeMenu(
    menu: Exclude<CodexRuntimeMenu, null>,
  ): void {
    const state = deps.getAppState();
    if (!state.initialized || deps.getViewContextSwitchPending()) {
      return;
    }
    deps.setRuntimeMenuOpen(false);
    deps.setAccessMenuOpen(false);
    deps.setBranchMenuOpen(false);
    deps.setCodexRuntimeMenu((current) => (current === menu ? null : menu));
    if (isCodexProvider(state.initialized)) {
      void loadCodexModelsForProvider(state.initialized.provider);
    }
  }

  async function loadCodexModelsForProvider(provider: string): Promise<void> {
    if (!provider) {
      return;
    }
    const codexModels = deps.getCodexModels();
    if (
      codexModels.provider === provider &&
      (codexModels.loading || codexModels.models.length > 0)
    ) {
      return;
    }
    deps.setCodexModels({ provider, loading: true, error: "", models: [] });
    try {
      const result = await window.wuu.loadCodexModels(provider);
      deps.setCodexModels({
        provider: result.provider,
        loading: false,
        error: "",
        models: result.models,
      });
      deps.setAppState((current) => {
        if (
          !current.initialized ||
          current.initialized.provider !== result.provider
        ) {
          return current;
        }
        return {
          ...current,
          initialized: {
            ...current.initialized,
            model: result.model,
            effort: result.effort ?? "",
            variant: result.variant ?? "",
          },
        };
      });
    } catch (error) {
      deps.setCodexModels({
        provider,
        loading: false,
        error: error instanceof Error ? error.message : "无法加载 Codex 模型",
        models: [],
      });
    }
  }

  async function selectRuntimeModel(
    provider: string,
    model: string,
    variant?: string,
  ): Promise<void> {
    if (!deps.getAppState().initialized || deps.getViewContextSwitchPending()) {
      return;
    }
    await updateRuntimeSettings(provider, model, undefined, undefined, variant);
    deps.setCodexRuntimeMenu(null);
  }

  async function selectRuntimeEffort(nextVariant: string): Promise<void> {
    const state = deps.getAppState();
    if (!state.initialized || deps.getViewContextSwitchPending()) {
      return;
    }
    await updateRuntimeSettings(
      state.initialized.provider,
      state.initialized.model,
      undefined,
      undefined,
      nextVariant,
    );
    deps.setCodexRuntimeMenu(null);
  }

  async function selectPermissionMode(mode: PermissionMode): Promise<void> {
    if (!deps.getAppState().initialized || deps.getViewContextSwitchPending()) {
      return;
    }
    deps.setSelectedPermissionMode(mode);
    deps.setAccessMenuOpen(false);
  }

  async function interrupt(): Promise<void> {
    const thread = activeThreadForState(deps.getAppState());
    if (!thread) {
      return;
    }
    await window.wuu.interruptTurn(thread.id);
    deps.clearThreadPendingComposerMessages(thread.id);
  }

  async function interruptPane(pane: ConversationPaneID): Promise<void> {
    const thread = threadForPane(deps.getAppState(), pane);
    if (!thread) {
      return;
    }
    await window.wuu.interruptTurn(thread.id);
    deps.clearThreadPendingComposerMessages(thread.id);
  }

  return {
    updateRuntimeSettings,
    updateAdvancedSettings,
    updateGeneralSettings,
    removeProvider,
    toggleCodexRuntimeMenu,
    loadCodexModelsForProvider,
    selectRuntimeModel,
    selectRuntimeEffort,
    selectPermissionMode,
    interrupt,
    interruptPane,
  };
}
