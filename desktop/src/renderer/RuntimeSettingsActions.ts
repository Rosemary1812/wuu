import type { SetStateAction } from "react";
import type {
  RuntimeAdvancedSettingsUpdate,
  RuntimeConnectionUpdate,
  RuntimeGeneralSettingsUpdate,
  Thread,
} from "../shared/protocol";
import {
  activeThreadForState,
  threadForPane,
  updateThreadByID,
  type AppState,
  type ConversationPaneID,
} from "./AppState";
import type {
  CodexModelLoadState,
  CodexRuntimeMenu,
  PermissionMode,
} from "./ComposerTypes";
import { isCodexProvider } from "./RuntimeHelpers";
import { runtimeViewForSession } from "./SessionRuntimeState";
import { translateCurrent } from "./i18n";

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
  clearThreadPendingComposerMessages: (threadID: string) => void;
  variantByModel: Map<string, string>;
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
  updateUltraMode: (enabled: boolean) => Promise<void>;
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

type RuntimeSelectionUpdate = {
  provider?: string;
  model?: string;
  effort?: string;
  connection?: RuntimeConnectionUpdate;
  variant?: string;
  permissionMode?: string;
};

export function createRuntimeSettingsActions(
  deps: RuntimeSettingsActionsDeps,
): RuntimeSettingsActions {
  function modelSelectionKey(
    scope: string,
    provider: string,
    model: string,
  ): string {
    return JSON.stringify([scope, provider.trim(), model.trim()]);
  }

  // Sends only the fields the caller explicitly changed. The server inherits
  // omitted provider/model/variant/effort/permission from the target thread
  // and leaves the workspace defaults for them untouched, so forwarding
  // unchanged thread values here would rewrite the workspace defaults.
  async function sendRuntimeSelection(
    update: RuntimeSelectionUpdate,
  ): Promise<void> {
    const state = deps.getAppState();
    if (!state.initialized) {
      return;
    }
    const targetThread = activeThreadForState(state);
    let nextProvider = update.provider?.trim() || undefined;
    let nextModel = update.model?.trim() || undefined;
    const nextEffort =
      update.effort === undefined ? undefined : update.effort.trim();
    // An explicit empty variant resets the stored reasoning effort to the
    // model default (the server clears the selection), so '' must survive;
    // only undefined means "not part of this update".
    const nextVariant =
      update.variant === undefined ? undefined : update.variant.trim();
    const nextPermissionMode =
      update.permissionMode === undefined
        ? undefined
        : update.permissionMode.trim();
    if (!targetThread) {
      // Workspace-scoped update: the server requires an explicit model when
      // no thread is targeted.
      nextProvider = nextProvider ?? state.initialized.provider;
      nextModel = nextModel ?? state.initialized.model;
    }
    const connection = update.connection;
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
    const currentProvider = state.initialized.providers?.find(
      (item) => item.name === nextProvider,
    );
    const connectionChanged =
      Boolean(nextConnection?.create_provider) ||
      Boolean(nextConnection?.api_key) ||
      Boolean(nextConnection?.auth_token) ||
      (nextConnection?.base_url !== undefined &&
        nextConnection.base_url !== (currentProvider?.base_url ?? ""));
    const providerChanged =
      nextProvider !== undefined &&
      nextProvider !==
        (targetThread?.model_provider ?? state.initialized.provider);
    const modelChanged =
      nextModel !== undefined &&
      nextModel !== (targetThread?.model ?? state.initialized.model);
    const effortChanged =
      nextEffort !== undefined &&
      nextEffort !==
        (targetThread?.model_effort ?? state.initialized.effort ?? "");
    const variantChanged =
      nextVariant !== undefined &&
      nextVariant !==
        (targetThread?.model_variant ?? state.initialized.variant ?? "");
    const permissionModeChanged =
      nextPermissionMode !== undefined &&
      nextPermissionMode !==
        (targetThread?.permission_mode ||
          state.initialized.permissions?.mode ||
          "");
    if (
      !providerChanged &&
      !modelChanged &&
      !effortChanged &&
      !variantChanged &&
      !connectionChanged &&
      !permissionModeChanged
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
        targetThread?.id,
      );
      deps.setAppState((current) => {
        // The result is workspace-effective, so initialized takes it
        // wholesale.
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
        // Optimistic thread patch limited to the fields this call explicitly
        // changed; the server's thread/updated snapshot stays the authority
        // for resolved values.
        const threadPatch: Partial<Thread> = {
          ...(nextProvider === undefined
            ? {}
            : { model_provider: updated.provider }),
          ...(nextModel === undefined ? {} : { model: updated.model }),
          ...(nextVariant === undefined && nextEffort === undefined
            ? {}
            : {
                model_variant: nextVariant ?? nextEffort ?? "",
                model_effort: nextEffort ?? "",
              }),
          ...(nextPermissionMode === undefined
            ? {}
            : { permission_mode: nextPermissionMode }),
        };
        const next = updateThreadByID(
          { ...current, initialized },
          targetThread?.id,
          (thread) => ({ ...thread, ...threadPatch }),
        );
        return {
          ...next,
          status: next.status === "ready" ? next.status : "ready",
        };
      });
    } catch (error) {
      deps.setAppState((current) => ({
        ...current,
        status:
          error instanceof Error
            ? error.message
            : translateCurrent("runtime.settingsUpdateFailed"),
      }));
      throw error;
    }
  }

  async function updateRuntimeSettings(
    provider: string,
    model: string,
    effort?: string,
    connection?: RuntimeConnectionUpdate,
    variant?: string,
    permissionMode?: string,
  ): Promise<void> {
    const nextProvider = provider.trim();
    const nextModel = model.trim();
    if (!nextProvider || !nextModel) {
      return;
    }
    await sendRuntimeSelection({
      provider: nextProvider,
      model: nextModel,
      effort,
      connection,
      variant,
      permissionMode,
    });
  }

  async function updateUltraMode(enabled: boolean): Promise<void> {
    const state = deps.getAppState();
    if (!state.initialized || deps.getViewContextSwitchPending()) {
      return;
    }
    if (Boolean(state.initialized.ultra) === enabled) {
      return;
    }
    try {
      const updated = await window.wuu.updateUltraMode(enabled);
      deps.setAppState((current) => {
        if (!current.initialized) {
          return current;
        }
        return {
          ...current,
          initialized: {
            ...current.initialized,
            ultra: updated.ultra,
          },
          status: current.status === "ready" ? current.status : "ready",
        };
      });
    } catch (error) {
      deps.setAppState((current) => ({
        ...current,
        status:
          error instanceof Error
            ? error.message
            : translateCurrent("runtime.ultraUpdateFailed"),
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
              model_aliases:
                updated.model_aliases ??
                settings.model_aliases ??
                current.initialized.model_aliases,
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
            : translateCurrent("runtime.advancedUpdateFailed"),
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
            : translateCurrent("runtime.generalUpdateFailed"),
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
          error instanceof Error ? error.message : translateCurrent("runtime.providerRemoveFailed"),
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
    const runtime = runtimeViewForSession(
      state.initialized,
      activeThreadForState(state),
    );
    if (runtime && isCodexProvider(runtime)) {
      void loadCodexModelsForProvider(runtime.provider);
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
    } catch (error) {
      deps.setCodexModels({
        provider,
        loading: false,
        error: error instanceof Error ? error.message : translateCurrent("runtime.modelsLoadFailed"),
        models: [],
      });
    }
  }

  async function selectRuntimeModel(
    provider: string,
    model: string,
    variant?: string,
  ): Promise<void> {
    const state = deps.getAppState();
    if (!state.initialized || deps.getViewContextSwitchPending()) {
      return;
    }
    const targetThread = activeThreadForState(state);
    const currentProvider =
      targetThread?.model_provider ?? state.initialized.provider;
    const currentModel = targetThread?.model ?? state.initialized.model;
    const currentVariant =
      targetThread?.model_variant ??
      targetThread?.model_effort ??
      state.initialized.variant ??
      state.initialized.effort ??
      "";
    const selectionScope = targetThread?.id ?? "workspace";
    deps.variantByModel.set(
      modelSelectionKey(selectionScope, currentProvider, currentModel),
      currentVariant,
    );
    const targetKey = modelSelectionKey(selectionScope, provider, model);
    const nextVariant = deps.variantByModel.has(targetKey)
      ? deps.variantByModel.get(targetKey)
      : variant;
    // The model panel stays open after a selection: the row highlight and the
    // effort pills update optimistically inside the panel, so the user can
    // chain "switch model → tune effort" in one visit instead of reopening the
    // picker for every change.
    try {
      await sendRuntimeSelection({ provider, model, variant: nextVariant });
    } catch {
      // Failure already surfaced through the status line.
    }
  }

  async function selectRuntimeEffort(nextVariant: string): Promise<void> {
    if (!deps.getAppState().initialized || deps.getViewContextSwitchPending()) {
      return;
    }
    try {
      await sendRuntimeSelection({ variant: nextVariant });
    } catch {
      // Failure already surfaced through the status line.
    }
    // Keep the panel open — see selectRuntimeModel.
  }

  async function selectPermissionMode(mode: PermissionMode): Promise<void> {
    if (!deps.getAppState().initialized || deps.getViewContextSwitchPending()) {
      return;
    }
    try {
      await sendRuntimeSelection({ permissionMode: mode });
    } catch {
      // Failure already surfaced through the status line.
    }
    deps.setAccessMenuOpen(false);
  }

  async function interrupt(): Promise<void> {
    const thread = activeThreadForState(deps.getAppState());
    if (!thread) {
      return;
    }
    await window.wuu.interruptTurn(thread.id);
  }

  async function interruptPane(pane: ConversationPaneID): Promise<void> {
    const thread = threadForPane(deps.getAppState(), pane);
    if (!thread) {
      return;
    }
    await window.wuu.interruptTurn(thread.id);
  }

  return {
    updateRuntimeSettings,
    updateUltraMode,
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
