import type { Thread } from "../shared/protocol";

const DEFAULT_THREAD_TITLE = "未命名对话";
const FORK_TITLE_SUFFIX = " · 分叉";

export function threadDisplayTitle(
  thread: Thread | undefined,
  threads: Thread[] = [],
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

function baseThreadTitle(thread: Thread, threads: Thread[], fallback: string): string {
  if (thread.forked_from_id) {
    const source = threads.find((candidate) => candidate.id === thread.forked_from_id);
    const sourceTitle = source?.preview?.trim();
    if (sourceTitle) {
      return sourceTitle;
    }
  }
  const ownTitle = thread.preview?.trim();
  return ownTitle || fallback;
}
