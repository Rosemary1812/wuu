import { useEffect, useState } from "react";

// Context for the empty new-conversation greeting. We keep it as a
// discriminated union so the helper can't accidentally mix the
// project-name and wuu fallbacks.
export type GreetingContext =
  | { kind: "project"; projectName: string }
  | { kind: "wuu" };

// Five time-of-day buckets in the user's local time. Boundaries are
// chosen for a coding tool: we want a clear "morning / noon / afternoon /
// evening / late night" feel without splitting the day into too many
// thin slices that would feel jittery.
export function greetingFor(hour: number, ctx: GreetingContext): string {
  const project = ctx.kind === "project" ? ctx.projectName : null;

  if (hour >= 5 && hour < 11) {
    return project
      ? `早上好,今天想在 ${project} 里搞定什么？`
      : "早上好,今天想搞定什么？";
  }
  if (hour >= 11 && hour < 14) {
    return project
      ? `中午好,${project} 接下来想做什么？`
      : "中午好,休息一下还是接着干？";
  }
  if (hour >= 14 && hour < 18) {
    return project
      ? `下午好,${project} 有什么想推进的？`
      : "下午好,有什么我能帮上的？";
  }
  if (hour >= 18 && hour < 22) {
    return project
      ? `晚上好,${project} 今天还想做什么？`
      : "晚上好,今天还想推进什么？";
  }
  // 22:00 – 04:59 late night.
  return project
    ? `夜深了,${project} 还能帮你做点什么？`
    : "夜深了,继续还是先歇会儿？";
}

// Re-render once a minute so the greeting updates when the user crosses
// an hour boundary while the app sits idle on the empty screen. React
// bails out of the setState call when the hour is unchanged, so a
// no-op tick is free.
export function useCurrentHour(intervalMs: number = 60_000): number {
  const [hour, setHour] = useState<number>(() => new Date().getHours());
  useEffect(() => {
    const id = setInterval(() => {
      setHour(new Date().getHours());
    }, intervalMs);
    return () => clearInterval(id);
  }, [intervalMs]);
  return hour;
}
