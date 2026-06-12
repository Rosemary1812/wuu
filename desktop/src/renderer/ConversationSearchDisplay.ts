export function conversationSearchVisibleSnippet({
  query,
  snippet,
  title,
}: {
  query: string;
  snippet?: string;
  title: string;
}): string {
  const trimmedSnippet = snippet?.trim() ?? "";
  if (!trimmedSnippet || !query.trim()) {
    return "";
  }
  if (normalizeSearchPreview(trimmedSnippet) === normalizeSearchPreview(title)) {
    return "";
  }
  return trimmedSnippet;
}

function normalizeSearchPreview(value: string): string {
  return value.trim().replace(/\s+/g, " ").toLocaleLowerCase();
}
