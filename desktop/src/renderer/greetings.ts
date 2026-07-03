import { useEffect, useState } from "react";

// Context for the empty new-conversation greeting. We keep it as a
// discriminated union so the helper can't accidentally mix the
// project-name and wuu fallbacks.
export type GreetingContext =
  | { kind: "project"; projectName: string }
  | { kind: "group"; title?: string; memberNames: string[] }
  | { kind: "wuu" };

// Five time-of-day buckets in the user's local time. Boundaries are
// chosen for a coding tool: we want a clear "morning / noon / afternoon /
// evening / late night" feel without splitting the day into too many
// thin slices that would feel jittery.
export function greetingFor(hour: number, ctx: GreetingContext): string {
  if (ctx.kind === "group") {
    return groupGreeting(hour, ctx);
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

// Group threads greet with a collaboration framing: this is a space to
// hand tasks to a crew — broadcast to everyone, @-mention a member, or
// describe work for anyone to pick up. Member names weave in naturally
// when the snapshot is available; the `all` channel has implicit
// membership (members may be empty), so every branch must read well
// without a roster too.
function groupGreeting(
  hour: number,
  ctx: Extract<GreetingContext, { kind: "group" }>,
): string {
  const roster = formatRoster(ctx.memberNames);
  const title = ctx.title?.trim();
  const space = title ? `群聊「${title}」` : "群聊空间";

  if (hour >= 5 && hour < 11) {
    return roster
      ? `早上好，这里是${space}，${roster} 都在。把任务丢进来，@ 某位成员点名，或直接广播给大家。`
      : `早上好，这里是${space}。把任务丢进来，@ 某位成员点名，或直接广播给大家。`;
  }
  if (hour >= 11 && hour < 14) {
    return roster
      ? `中午好，${roster} 都在这个${space}里。可以广播任务，也可以 @ 指定成员来接。`
      : `中午好，这里是${space}。可以广播任务，也可以 @ 指定成员来接。`;
  }
  if (hour >= 14 && hour < 18) {
    return roster
      ? `下午好，这里是${space}，和 ${roster} 一起协作。描述任务让大家认领，或点名某位成员推进。`
      : `下午好，这里是${space}。描述任务让大家认领，或点名某位成员推进。`;
  }
  if (hour >= 18 && hour < 22) {
    return roster
      ? `晚上好，${roster} 在${space}里待命。广播、点名或直接派活都可以。`
      : `晚上好，${space}的成员在待命。广播、点名或直接派活都可以。`;
  }
  // 22:00 – 04:59 late night.
  return roster
    ? `夜深了，还想让 ${roster} 帮忙推进什么吗？`
    : "夜深了，还想让群里的成员帮忙推进什么吗？";
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
