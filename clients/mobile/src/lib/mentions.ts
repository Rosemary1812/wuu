// @mention resolution, mirrored from desktop AppState.mentionedParticipantIDsFromText:
// whole-word matching with longest-name-first ordering so "@Noel" never
// resolves to a roster entry named "Noe". The @Name text stays in the prompt
// verbatim; only the resolved ids travel in turn/start `mentions` (and only
// when non-empty — the server rejects unknown fields).

export function mentionedParticipantIDsFromText(
  text: string,
  participants: ReadonlyArray<{ id: string; name: string }>,
): string[] {
  const source = text.trim();
  if (source === "") {
    return [];
  }
  const ids = new Set<string>();
  const candidates = [...participants]
    .filter((participant) => participant.name.trim() !== "")
    .sort((a, b) => b.name.length - a.name.length);
  for (const participant of candidates) {
    const escaped = escapeRegExp(participant.name.trim());
    const pattern = new RegExp(`(^|\\s)@${escaped}(?=$|\\s|[,.!?，。；：、])`);
    if (pattern.test(source)) {
      ids.add(participant.id);
    }
  }
  return [...ids];
}

/** The names still matching as the user types "@..." at the caret. */
export function mentionCandidates(
  draft: string,
  participants: ReadonlyArray<{ id: string; name: string }>,
): Array<{ id: string; name: string }> {
  const match = /(^|\s)@([^\s@]*)$/.exec(draft);
  if (!match) return [];
  const query = match[2].toLowerCase();
  return participants.filter((p) => {
    const name = p.name.trim();
    if (name === "") return false;
    return query === "" || name.toLowerCase().startsWith(query);
  });
}

/** Replaces the trailing "@partial" with "@Name " once picked. */
export function applyMention(draft: string, name: string): string {
  return draft.replace(/(^|\s)@([^\s@]*)$/, `$1@${name} `);
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}
