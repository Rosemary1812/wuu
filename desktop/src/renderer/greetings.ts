import { useEffect, useState } from "react";

// Context for the empty new-conversation greeting. We keep it as a
// discriminated union so the helper can't accidentally mix the
// project-name and wuu fallbacks.
export type GreetingContext =
  | { kind: "project"; projectName: string }
  | { kind: "group"; title?: string; memberNames: string[] }
  | { kind: "dm"; agentName: string }
  | { kind: "wuu" };

// Five time-of-day buckets in the user's local time. Boundaries are
// chosen for a coding tool: we want a clear "morning / noon / afternoon /
// evening / late night" feel without splitting the day into too many
// thin slices that would feel jittery.
export function greetingFor(hour: number, ctx: GreetingContext): string {
  if (ctx.kind === "group") {
    return groupGreeting(hour, ctx);
  }
  if (ctx.kind === "dm") {
    return dmGreeting(hour, ctx);
  }

  const project = ctx.kind === "project" ? ctx.projectName : null;

  if (hour >= 5 && hour < 11) {
    return project
      ? `早上好，今天想在 ${project} 里先搞定什么？`
      : "早上好，今天想先搞定什么？";
  }
  if (hour >= 11 && hour < 14) {
    return project
      ? `中午好，接下来想在 ${project} 里做什么？`
      : "中午好，休息一下，还是接着做点什么？";
  }
  if (hour >= 14 && hour < 18) {
    return project
      ? `下午好，今天想在 ${project} 里推进什么？`
      : "下午好，有什么我能帮忙推进的？";
  }
  if (hour >= 18 && hour < 22) {
    return project
      ? `晚上好，今天还想在 ${project} 里处理什么？`
      : "晚上好，今天还想处理什么？";
  }
  // 22:00 – 04:59 late night.
  return project
    ? `夜深了，还想在 ${project} 里做点什么？`
    : "夜深了，还要继续吗？";
}

// Group threads greet like a room you toss work into — one short line,
// same register as the base greetings above. The tab and header already
// name the group, so the copy deliberately doesn't repeat ctx.title. The
// `all` channel has implicit membership (members may be empty), so every
// branch must read well without a roster too.
function groupGreeting(
  hour: number,
  ctx: Extract<GreetingContext, { kind: "group" }>,
): string {
  const roster = formatRoster(ctx.memberNames);
  // "Alice 在" for one member, "Alice、Bob 都在" for several.
  const present = roster
    ? ctx.memberNames.length === 1
      ? `${roster} 在`
      : `${roster} 都在`
    : null;

  if (hour >= 5 && hour < 11) {
    return present
      ? `早上好，${present}，有事直接喊。`
      : "早上好，有事直接在群里喊。";
  }
  if (hour >= 11 && hour < 14) {
    return "中午好，想让谁接手，@ 一下就行。";
  }
  if (hour >= 14 && hour < 18) {
    return "下午好，有任务就丢进群里。";
  }
  if (hour >= 18 && hour < 22) {
    return present
      ? `晚上好，${present}，直接派活吧。`
      : "晚上好，直接在群里派活吧。";
  }
  // 22:00 – 04:59 late night.
  return "夜深了，还要拉大家推进什么吗？";
}

// DM threads greet as a hand-off to one named agent — name up front,
// one short line, clearly not a group space.
function dmGreeting(
  hour: number,
  ctx: Extract<GreetingContext, { kind: "dm" }>,
): string {
  if (hour >= 5 && hour < 11) {
    return `早上好，有什么要交给 ${ctx.agentName} 的？`;
  }
  if (hour >= 11 && hour < 14) {
    return `中午好，有事直接跟 ${ctx.agentName} 说。`;
  }
  if (hour >= 14 && hour < 18) {
    return `下午好，想让 ${ctx.agentName} 帮你做点什么？`;
  }
  if (hour >= 18 && hour < 22) {
    return `晚上好，任务交给 ${ctx.agentName} 就行。`;
  }
  // 22:00 – 04:59 late night.
  return `夜深了，还有要交给 ${ctx.agentName} 的吗？`;
}

// Renders the member snapshot as a short roster string, or null when the
// snapshot is empty (e.g. the implicit-membership `all` channel). Lists
// at most three names; larger groups list three and close with the total
// headcount ("等 N 位成员" reads as the group total, names included).
function formatRoster(memberNames: string[]): string | null {
  if (memberNames.length === 0) {
    return null;
  }
  if (memberNames.length <= 3) {
    return memberNames.join("、");
  }
  const listed = memberNames.slice(0, 3).join("、");
  return `${listed} 等 ${memberNames.length} 位成员`;
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
