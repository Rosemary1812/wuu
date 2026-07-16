import { useEffect, useState } from "react";
import type { RuntimeContext, Thread } from "../shared/protocol";
import { formatCurrentNumber, translateCurrent } from "./i18n";

// Context for the empty new-conversation greeting. We keep it as a
// discriminated union so the helper can't accidentally mix the
// project-name and wuu fallbacks.
export type GreetingContext =
  | { kind: "project"; projectName: string }
  | { kind: "group"; title?: string; memberNames: string[] }
  | { kind: "dm"; agentName: string }
  | { kind: "wuu" };

type GreetingThreadSource = Pick<
  Thread,
  "group" | "title" | "members" | "dm_participant_id"
>;

/**
 * Derives the empty-conversation greeting context from the pieces App.tsx
 * already tracks in render: the active thread (if any), the roster to
 * resolve a DM participant's name against, and the active runtime
 * context's kind + display name.
 *
 * Group and DM threads always win — their framing doesn't depend on which
 * project is open. Otherwise the greeting follows `activeContextKind`
 * directly rather than any separately-cached "last known project" value,
 * so switching between a project and no-project (R1's sidebar "新对话",
 * R2's hero-project-pill / ProjectPickerMenu switches) re-derives the
 * right greeting on every render — there's no stale state to fall behind.
 */
export function resolveGreetingContext(input: {
  activeThread: GreetingThreadSource | undefined;
  participants: readonly { id: string; name: string }[];
  activeContextKind: RuntimeContext["kind"] | undefined;
  activeProjectName: string | undefined;
}): GreetingContext {
  const { activeThread, participants, activeContextKind, activeProjectName } =
    input;
  // Group chats greet with the collaboration framing. The `group` flag is
  // the discriminant; a newly created or explicitly emptied group can still
  // have no members in its current snapshot.
  if (activeThread?.group) {
    return {
      kind: "group",
      title: activeThread.title?.trim() || undefined,
      memberNames: (activeThread.members ?? []).map((member) => member.name),
    };
  }
  // DM threads greet as a one-on-one conversation with the named agent.
  if (activeThread?.dm_participant_id) {
    const agentName =
      participants.find((p) => p.id === activeThread.dm_participant_id)
        ?.name ||
      activeThread.title?.trim() ||
      translateCurrent("greeting.memberFallback");
    return { kind: "dm", agentName };
  }
  if (activeContextKind === "project") {
    return {
      kind: "project",
      projectName: activeProjectName ?? translateCurrent("greeting.projectFallback"),
    };
  }
  return { kind: "wuu" };
}

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
      ? translateCurrent("greeting.project.morning", { project })
      : translateCurrent("greeting.wuu.morning");
  }
  if (hour >= 11 && hour < 14) {
    return project
      ? translateCurrent("greeting.project.noon", { project })
      : translateCurrent("greeting.wuu.noon");
  }
  if (hour >= 14 && hour < 18) {
    return project
      ? translateCurrent("greeting.project.afternoon", { project })
      : translateCurrent("greeting.wuu.afternoon");
  }
  if (hour >= 18 && hour < 22) {
    return project
      ? translateCurrent("greeting.project.evening", { project })
      : translateCurrent("greeting.wuu.evening");
  }
  // 22:00 – 04:59 late night.
  return project
    ? translateCurrent("greeting.project.lateNight", { project })
    : translateCurrent("greeting.wuu.lateNight");
}

// Group threads greet like a room you toss work into — one short line,
// same register as the base greetings above. The tab and header already
// name the group, so the copy deliberately doesn't repeat ctx.title. The
// member snapshot can be empty, so every branch must read well without a
// roster too.
function groupGreeting(
  hour: number,
  ctx: Extract<GreetingContext, { kind: "group" }>,
): string {
  const roster = formatRoster(ctx.memberNames);
  // "Alice 在" for one member, "Alice、Bob 都在" for several.
  const present = roster
    ? ctx.memberNames.length === 1
      ? translateCurrent("greeting.group.onePresent", { roster })
      : translateCurrent("greeting.group.manyPresent", { roster })
    : null;

  if (hour >= 5 && hour < 11) {
    return present
      ? translateCurrent("greeting.group.morningPresent", { present })
      : translateCurrent("greeting.group.morning");
  }
  if (hour >= 11 && hour < 14) {
    return translateCurrent("greeting.group.noon");
  }
  if (hour >= 14 && hour < 18) {
    return translateCurrent("greeting.group.afternoon");
  }
  if (hour >= 18 && hour < 22) {
    return present
      ? translateCurrent("greeting.group.eveningPresent", { present })
      : translateCurrent("greeting.group.evening");
  }
  // 22:00 – 04:59 late night.
  return translateCurrent("greeting.group.lateNight");
}

// DM threads greet as a hand-off to one named agent — name up front,
// one short line, clearly not a group space.
function dmGreeting(
  hour: number,
  ctx: Extract<GreetingContext, { kind: "dm" }>,
): string {
  if (hour >= 5 && hour < 11) {
    return translateCurrent("greeting.dm.morning", { agent: ctx.agentName });
  }
  if (hour >= 11 && hour < 14) {
    return translateCurrent("greeting.dm.noon", { agent: ctx.agentName });
  }
  if (hour >= 14 && hour < 18) {
    return translateCurrent("greeting.dm.afternoon", { agent: ctx.agentName });
  }
  if (hour >= 18 && hour < 22) {
    return translateCurrent("greeting.dm.evening", { agent: ctx.agentName });
  }
  // 22:00 – 04:59 late night.
  return translateCurrent("greeting.dm.lateNight", { agent: ctx.agentName });
}

// Renders the member snapshot as a short roster string, or null when the
// snapshot is empty. Lists at most three names; larger groups list three and
// close with the total headcount ("等 N 位成员" reads as the group total, names
// included).
function formatRoster(memberNames: string[]): string | null {
  if (memberNames.length === 0) {
    return null;
  }
  if (memberNames.length <= 3) {
    return memberNames.join(translateCurrent("greeting.nameSeparator"));
  }
  const listed = memberNames.slice(0, 3).join(translateCurrent("greeting.nameSeparator"));
  return translateCurrent("greeting.rosterOverflow", { listed, count: formatCurrentNumber(memberNames.length) });
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
