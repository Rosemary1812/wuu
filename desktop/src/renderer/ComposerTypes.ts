import type { CodexModelSummary } from "../shared/protocol";
import { resolveLocalizedText } from "./i18n";

export type CodexModelLoadState = {
  provider?: string;
  loading: boolean;
  error: string;
  models: CodexModelSummary[];
};

export type CodexRuntimeMenu = "main" | "model" | "effort" | null;
export type ComposerVariant = "dock" | "document" | "hero";
export type FloatingMenuOwner =
  | "composer-runtime"
  | "composer-access"
  | "composer-context-meter"
  | "composer-token-gauge"
  | "composer-focus"
  | "composer-goal"
  | "composer-plus"
  | "composer-slash"
  | "codex-runtime"
  | "composer-query-history"
  | "minute-clock"
  | "select-menu";
export type FloatingMenuPlacement = "above" | "below" | "middle";
export type FloatingMenuAlign = "left" | "right";
export type PermissionMode =
  | "standard"
  | "read_only"
  | "unconfined";

const HIDDEN_COMPOSER_STATUSES = new Set([
  "ready",
  "正在发送请求",
  "Sending request",
]);

export function composerStatusText(status: string): string {
  const resolved = resolveLocalizedText(status);
  return HIDDEN_COMPOSER_STATUSES.has(resolved) ? "" : resolved;
}

export function composerStatusIsLiveProgress(liveProgress?: boolean): boolean {
  return liveProgress === true;
}
