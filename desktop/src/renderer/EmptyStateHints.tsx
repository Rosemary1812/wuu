import type { ParticipantProfile, ProviderSummary } from "../shared/protocol";

/**
 * Onboarding chips rendered under the empty-conversation greeting.
 *
 * The two chips nudge the user toward the two real sources of friction on
 * a brand-new session: (1) talking to the default named agent, and (2)
 * confirming that a model provider is configured. The greeting above is
 * already a meta-layer line, so this strip stays in meta layer too
 * (13px / --ink-soft) and never grows into a hero card.
 *
 * The chips are display-only at the contract level: they report the
 * chip name and the action key to the parent, which owns the
 * composer-insert and settings-open side effects. That keeps the
 * component easy to test and the focus jump in App.tsx.
 */
export type EmptyStateHintAction =
  | { kind: "mentionNamed"; participant: ParticipantProfile }
  | { kind: "openSettings" };

export type EmptyStateHintsProps = {
  // First named participant is what the greeting refers to. When the
  // roster is empty the greeting still renders, but the chip would
  // point at nobody, so the parent omits the hints strip entirely.
  namedParticipant?: ParticipantProfile;
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
  namedParticipant,
  providers,
  onSelect,
}: EmptyStateHintsProps): JSX.Element | null {
  const showMentionChip = Boolean(namedParticipant);
  const showSettingsChip = !hasReadyProvider(providers);
  if (!showMentionChip && !showSettingsChip) {
    return null;
  }
  const mentionLabel = namedParticipant
    ? `@${namedParticipant.name.trim() || "agent"} 打个招呼`
    : "";
  return (
    <div className="empty-home-hints" aria-label="新会话提示">
      {namedParticipant ? (
        <button
          type="button"
          className="participant-chip participant-chip--pill empty-home-hint-chip"
          onClick={() => onSelect({ kind: "mentionNamed", participant: namedParticipant })}
        >
          {mentionLabel}
        </button>
      ) : null}
      {showSettingsChip ? (
        <button
          type="button"
          className="participant-chip participant-chip--pill empty-home-hint-chip"
          onClick={() => onSelect({ kind: "openSettings" })}
        >
          配置模型
        </button>
      ) : null}
    </div>
  );
}
