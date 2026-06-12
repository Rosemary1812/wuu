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
  return trimmedSnippet;
}

function normalizeSearchPreview(value: string): string {
  return value.trim().replace(/\s+/g, " ").toLocaleLowerCase();
}
