import { useEffect, useRef, useState } from "react";

export function useLiveNow(active: boolean): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!active) {
      return;
    }
    setNow(Date.now());
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [active]);

  return active ? now : Date.now();
}

export function LiveDuration({ startedAtMs }: { startedAtMs: number }): JSX.Element {
  const nodeRef = useRef<HTMLSpanElement | null>(null);

  useEffect(() => {
    const update = (): void => {
      if (nodeRef.current) {
        nodeRef.current.textContent = formatDuration(Math.max(0, Date.now() - startedAtMs));
      }
    };
    update();
    const timer = window.setInterval(update, 1000);
    return () => window.clearInterval(timer);
  }, [startedAtMs]);

  return <span ref={nodeRef}>{formatDuration(Math.max(0, Date.now() - startedAtMs))}</span>;
}

export function formatDuration(ms: number): string {
  const totalSeconds = Math.max(0, Math.floor(ms / 1000));
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  if (hours > 0) {
    return `${hours}h ${minutes}m ${seconds}s`;
  }
  if (minutes > 0) {
    return `${minutes}m ${seconds}s`;
  }
  return `${seconds}s`;
}

/**
 * Format a millisecond duration as a Chinese-language phrase for product
 * copy. Picks the top two non-zero units and drops trailing zeros. The
 * helper does NOT include "用时" — callers wrap with the surrounding
 * sentence so the same duration portion can be reused in different
 * framings (e.g. "用时 3 秒", "耗时 1 分 30 秒").
 *
 *   3_000       → "3 秒"
 *   90_000      → "1 分 30 秒"
 *   3_900_000   → "1 小时 5 分"
 *   90_000_000  → "1 天 1 小时"
 *
 * Sub-second values round down to "0 秒"; callers handling edge cases
 * explicitly (e.g. "不到 1 秒") should branch on `ms < 1000` themselves.
 */
export function formatChineseDuration(ms: number): string {
  const totalSeconds = Math.max(0, Math.floor(ms / 1000));
  const days = Math.floor(totalSeconds / 86400);
  const hours = Math.floor((totalSeconds % 86400) / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  const parts: string[] = [];
  if (days > 0) {
    parts.push(`${days} 天`);
    if (hours > 0) parts.push(`${hours} 小时`);
  } else if (hours > 0) {
    parts.push(`${hours} 小时`);
    if (minutes > 0) parts.push(`${minutes} 分`);
  } else if (minutes > 0) {
    parts.push(`${minutes} 分`);
    if (seconds > 0) parts.push(`${seconds} 秒`);
  } else {
    parts.push(`${seconds} 秒`);
  }
  return parts.join(" ");
}
