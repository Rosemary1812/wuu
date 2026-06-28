import type { CodexModelSummary } from "../shared/protocol";

export type CodexModelLoadState = {
  provider?: string;
  loading: boolean;
  error: string;
  models: CodexModelSummary[];
};

export type CodexRuntimeMenu = "main" | "model" | null;
export type ComposerVariant = "dock" | "hero";
export type FloatingMenuOwner =
  | "composer-runtime"
  | "composer-access"
  | "composer-context-meter"
  | "codex-runtime"
  | "composer-query-history";
export type FloatingMenuPlacement = "above" | "below" | "middle";
export type FloatingMenuAlign = "left" | "right";
export type PermissionMode =
  | "read_only"
  | "agent"
  | "auto_review"
  | "full_access";

const HIDDEN_COMPOSER_STATUSES = new Set(["ready", "正在发送请求"]);

export function composerStatusText(status: string): string {
  return HIDDEN_COMPOSER_STATUSES.has(status) ? "" : status;
}
