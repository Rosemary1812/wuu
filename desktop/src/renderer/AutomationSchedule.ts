export type AutomationScheduleKind =
  | "minutes"
  | "hourly"
  | "daily"
  | "weekdays"
  | "weekly"
  | "custom";

export type AutomationScheduleValue = {
  kind: AutomationScheduleKind;
  cron: string;
  interval: number;
  minute: number;
  time: string;
  weekday: number;
};

export type AutomationNextExecution = {
  dayOffset: number;
  weekday: number;
  time: string;
};

const DEFAULT_TIME = "09:00";

export function parseAutomationSchedule(cron: string): AutomationScheduleValue {
  const normalized = cron.trim().replace(/\s+/g, " ");
  const fields = normalized.split(" ");
  const fallback: AutomationScheduleValue = {
    kind: "custom",
    cron: normalized,
    interval: 15,
    minute: 0,
    time: DEFAULT_TIME,
    weekday: 1,
  };
  if (fields.length !== 5) return fallback;

  const [minuteField, hourField, dayOfMonth, month, dayOfWeek] = fields;
  if (dayOfMonth !== "*" || month !== "*") return fallback;

  const intervalMatch = minuteField.match(/^\*\/(5|10|15|30)$/);
  if (intervalMatch && hourField === "*" && dayOfWeek === "*") {
    return { ...fallback, kind: "minutes", interval: Number(intervalMatch[1]) };
  }

  const minute = parseBoundedInteger(minuteField, 0, 59);
  if (minute === null) return fallback;
  if (hourField === "*" && dayOfWeek === "*") {
    return { ...fallback, kind: "hourly", minute };
  }

  const hour = parseBoundedInteger(hourField, 0, 23);
  if (hour === null) return fallback;
  const time = `${padTime(hour)}:${padTime(minute)}`;
  if (dayOfWeek === "*") return { ...fallback, kind: "daily", minute, time };
  if (dayOfWeek === "1-5") return { ...fallback, kind: "weekdays", minute, time };

  const weekday = parseBoundedInteger(dayOfWeek, 0, 7);
  if (weekday !== null) {
    return { ...fallback, kind: "weekly", minute, time, weekday: weekday === 7 ? 0 : weekday };
  }
  return fallback;
}

export function cronForAutomationSchedule(value: AutomationScheduleValue): string {
  switch (value.kind) {
    case "minutes":
      return `*/${value.interval} * * * *`;
    case "hourly":
      return `${value.minute} * * * *`;
    case "daily":
      return `${timeParts(value.time).minute} ${timeParts(value.time).hour} * * *`;
    case "weekdays":
      return `${timeParts(value.time).minute} ${timeParts(value.time).hour} * * 1-5`;
    case "weekly":
      return `${timeParts(value.time).minute} ${timeParts(value.time).hour} * * ${value.weekday}`;
    case "custom":
      return value.cron;
  }
}

export function defaultCronForScheduleKind(kind: AutomationScheduleKind, currentCron: string): string {
  switch (kind) {
    case "minutes":
      return "*/15 * * * *";
    case "hourly":
      return "0 * * * *";
    case "daily":
      return "0 9 * * *";
    case "weekdays":
      return "0 9 * * 1-5";
    case "weekly":
      return "0 9 * * 1";
    case "custom":
      return currentCron;
  }
}

export function nextAutomationExecution(
  cron: string,
  timezone: string | undefined,
  after = new Date(),
): AutomationNextExecution | null {
  const schedule = parseAutomationSchedule(cron);
  if (schedule.kind === "custom") return null;

  const localNow = localDateParts(after, timezone);
  if (!localNow) return null;
  const startOfToday = Date.UTC(localNow.year, localNow.month - 1, localNow.day);
  const firstCandidate = Date.UTC(
    localNow.year,
    localNow.month - 1,
    localNow.day,
    localNow.hour,
    localNow.minute + 1,
  );
  const limit = firstCandidate + 8 * 24 * 60 * 60 * 1000;

  for (let cursor = firstCandidate; cursor < limit; cursor += 60_000) {
    const candidate = new Date(cursor);
    const minute = candidate.getUTCMinutes();
    const hour = candidate.getUTCHours();
    const weekday = candidate.getUTCDay();
    const matches = schedule.kind === "minutes"
      ? minute % schedule.interval === 0
      : schedule.kind === "hourly"
        ? minute === schedule.minute
        : schedule.kind === "daily"
          ? timeMatches(schedule.time, hour, minute)
          : schedule.kind === "weekdays"
            ? weekday >= 1 && weekday <= 5 && timeMatches(schedule.time, hour, minute)
            : weekday === schedule.weekday && timeMatches(schedule.time, hour, minute);
    if (!matches) continue;

    const candidateDay = Date.UTC(
      candidate.getUTCFullYear(),
      candidate.getUTCMonth(),
      candidate.getUTCDate(),
    );
    return {
      dayOffset: Math.round((candidateDay - startOfToday) / (24 * 60 * 60 * 1000)),
      weekday,
      time: `${padTime(hour)}:${padTime(minute)}`,
    };
  }
  return null;
}

function parseBoundedInteger(value: string, min: number, max: number): number | null {
  if (!/^\d+$/.test(value)) return null;
  const parsed = Number(value);
  return parsed >= min && parsed <= max ? parsed : null;
}

function padTime(value: number): string {
  return String(value).padStart(2, "0");
}

function timeParts(value: string): { hour: number; minute: number } {
  const match = value.match(/^(\d{2}):(\d{2})$/);
  if (!match) return { hour: 9, minute: 0 };
  return { hour: Number(match[1]), minute: Number(match[2]) };
}

function timeMatches(value: string, hour: number, minute: number): boolean {
  const expected = timeParts(value);
  return expected.hour === hour && expected.minute === minute;
}

function localDateParts(date: Date, timezone: string | undefined): {
  year: number;
  month: number;
  day: number;
  hour: number;
  minute: number;
} | null {
  try {
    const formatter = new Intl.DateTimeFormat("en-US-u-ca-gregory", {
      timeZone: timezone || Intl.DateTimeFormat().resolvedOptions().timeZone,
      year: "numeric",
      month: "numeric",
      day: "numeric",
      hour: "numeric",
      minute: "numeric",
      hourCycle: "h23",
    });
    const parts = Object.fromEntries(
      formatter.formatToParts(date)
        .filter((part) => part.type !== "literal")
        .map((part) => [part.type, Number(part.value)]),
    );
    return {
      year: parts.year,
      month: parts.month,
      day: parts.day,
      hour: parts.hour,
      minute: parts.minute,
    };
  } catch {
    return null;
  }
}
