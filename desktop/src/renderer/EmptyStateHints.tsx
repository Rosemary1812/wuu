import type { ProviderSummary } from "../shared/protocol";

/**
 * Onboarding chips rendered under the empty-conversation greeting.
 *
 * The chip nudges the user toward the real source of friction on a
 * brand-new session: confirming that a model provider is configured.
 * The greeting above is already a meta-layer line, so this strip
 * stays in meta layer too (13px / --ink-soft) and never grows into
 * a hero card.
 *
 * The chips are display-only at the contract level: they report the
 * chip name and the action key to the parent, which owns the
 * settings-open side effect. That keeps the component easy to test
 * and the focus jump in App.tsx.
 */
export type EmptyStateHintAction = { kind: "openSettings" };

export type EmptyStateHintsProps = {
  providers?: ProviderSummary[];
  onSelect: (action: EmptyStateHintAction) => void;
};

function hasReadyProvider(providers: ProviderSummary[] | undefined): boolean {
  if (!providers || providers.length === 0) {
    return false;
  }
  return providers.some(
    (provider) => provider.api_key_configured === true || provider.connection_locked === true,
  );
}

export function EmptyStateHints({
  providers,
  onSelect,
}: EmptyStateHintsProps): JSX.Element | null {
  if (hasReadyProvider(providers)) {
    return null;
  }
  return (
    <div className="empty-home-hints" aria-label="新会话提示">
      <button
        type="button"
        className="participant-chip participant-chip--pill empty-home-hint-chip"
        onClick={() => onSelect({ kind: "openSettings" })}
      >
        配置模型
      </button>
    </div>
  );
}
