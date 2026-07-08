import type { ThreadSearchResultItem } from "../shared/protocol";

export type ConversationSearchResultSection = {
  title: string;
  results: ThreadSearchResultItem[];
  startIndex: number;
};

export function conversationSearchResultSections(
  results: ThreadSearchResultItem[],
  query: string,
): ConversationSearchResultSection[] {
  if (query.trim()) {
    return results.length > 0
      ? [{ title: "搜索结果", results, startIndex: 0 }]
      : [];
  }
  return results.length > 0
    ? [{ title: "最近会话", results, startIndex: 0 }]
    : [];
}

export function conversationSearchStatusText({
  loading,
  query,
  resultCount,
}: {
  loading: boolean;
  query: string;
  resultCount: number;
}): string {
  if (loading) {
    return "正在搜索";
  }
  if (query.trim()) {
    return `${resultCount} 个结果`;
  }
  return `${resultCount} 个会话`;
}

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
  // Cap the snippet at the source so a leading window from the search
  // backend (the kind that easily runs 1k+ chars once an agent's
  // markdown reply starts dumping code blocks, headers and bullet
  // lists) cannot throw a multi-paragraph block into the right pane.
  // The CSS line-clamp in `.conversation-search-preview-snippet` is
  // the visual counterpart; this bounds the actual string so the
  // pane stays compact even when CSS does not load.
  return capSnippetText(trimmedSnippet);
}

// Cut a snippet at the last whitespace inside the cap so a long
// snippet never splits a word or markdown token mid-stream. Falls back
// to a hard slice when the cap lands inside a single long token (e.g.
// a base64 blob) where there is no whitespace to break on.
function capSnippetText(s: string, maxChars = 280): string {
  if (s.length <= maxChars) return s;
  const slice = s.slice(0, maxChars);
  const lastSpace = slice.lastIndexOf(" ");
  const trimmed =
    lastSpace > maxChars * 0.6 ? slice.slice(0, lastSpace) : slice;
  return trimmed.trimEnd() + "…";
}

function normalizeSearchPreview(value: string): string {
  return value.trim().replace(/\s+/g, " ").toLocaleLowerCase();
}
