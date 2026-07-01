import type { Thread } from "../shared/protocol";

const DEFAULT_THREAD_TITLE = "未命名对话";
const FORK_TITLE_SUFFIX = " · 分叉";

export type ThreadTitleSource = Pick<
  Thread,
  "id" | "preview" | "title" | "forked_from_id"
>;

/**
 * Base title for a thread (no fork marker). The sidebar uses this and pairs
 * the result with a separate `GitFork` icon to indicate forks, instead of
 * relying on a text suffix that gets truncated on long titles.
 */
export function baseThreadTitle(
  thread: ThreadTitleSource,
  threads: ThreadTitleSource[] = [],
  fallback = DEFAULT_THREAD_TITLE
): string {
  if (thread.forked_from_id) {
    const source = threads.find((candidate) => candidate.id === thread.forked_from_id);
    // Prefer source.title (set by the right-click Rename menu) over
    // source.preview (auto-generated) so a renamed source shows the
    // user's title in the fork header.
    const sourceTitle = source ? source.title?.trim() || source.preview?.trim() : undefined;
    if (sourceTitle) {
      return sourceTitle;
    }
  }
  // Prefer thread.title (set by Rename) over thread.preview
  // (auto-generated) so a renamed thread shows the user's title.
  const ownTitle = thread.title?.trim() || thread.preview?.trim();
  return ownTitle || fallback;
}

export function threadDisplayTitle(
  thread: ThreadTitleSource | undefined,
  threads: ThreadTitleSource[] = [],
  fallback = DEFAULT_THREAD_TITLE
): string {
  if (!thread) {
    return fallback;
  }
  const baseTitle = baseThreadTitle(thread, threads, fallback);
  if (!thread.forked_from_id) {
    return baseTitle;
  }
  return `${baseTitle}${FORK_TITLE_SUFFIX}`;
}
