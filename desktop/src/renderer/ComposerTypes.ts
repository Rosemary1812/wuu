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
  | "codex-runtime"
  | "composer-query-history";
export type FloatingMenuPlacement = "above" | "below" | "middle";
export type FloatingMenuAlign = "left" | "right";
export type ToolPolicyProfile =
  | "safe"
  | "balanced"
  | "auto"
  | "autonomous"
  | "enterprise_restricted";
