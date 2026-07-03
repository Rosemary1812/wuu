import { describe, expect, it } from "vitest";
import { greetingFor, type GreetingContext } from "./greetings";

describe("greetingFor", () => {
  describe("project context", () => {
    it("returns project-specific greeting for morning", () => {
      const ctx: GreetingContext = {
        kind: "project",
        projectName: "MyApp",
      };
      const greeting = greetingFor(8, ctx);
      expect(greeting).toContain("早上好");
      expect(greeting).toContain("MyApp");
    });

    it("returns project-specific greeting for afternoon", () => {
      const ctx: GreetingContext = {
        kind: "project",
        projectName: "MyApp",
      };
      const greeting = greetingFor(15, ctx);
      expect(greeting).toContain("下午好");
      expect(greeting).toContain("MyApp");
    });

    it("returns default greeting when project name is not provided", () => {
      const ctx: GreetingContext = { kind: "wuu" };
      const greeting = greetingFor(8, ctx);
      expect(greeting).toContain("早上好");
      expect(greeting).not.toContain("MyApp");
    });
  });

  describe("group context", () => {
    it("returns group-specific greeting for morning with members", () => {
      const ctx: GreetingContext = {
        kind: "group",
        memberNames: ["Alice", "Bob"],
      };
      const greeting = greetingFor(8, ctx);
      expect(greeting).toBe(
        "早上好，这里是群聊空间，Alice、Bob 都在。把任务丢进来，@ 某位成员点名，或直接广播给大家。",
      );
    });

    it("returns group-specific greeting for afternoon with members", () => {
      const ctx: GreetingContext = {
        kind: "group",
        memberNames: ["Alice", "Bob"],
      };
      const greeting = greetingFor(15, ctx);
      expect(greeting).toBe(
        "下午好，这里是群聊空间，和 Alice、Bob 一起协作。描述任务让大家认领，或点名某位成员推进。",
      );
    });

    it("returns group greeting even when members are empty (implicit all channel)", () => {
      // The `all` channel is a group thread with implicit membership —
      // the backend never writes thread_members for it, so the roster is
      // empty. The greeting must still read as a group space.
      const ctx: GreetingContext = {
        kind: "group",
        title: "all",
        memberNames: [],
      };
      const greeting = greetingFor(8, ctx);
      expect(greeting).toBe(
        "早上好，这里是群聊「all」。把任务丢进来，@ 某位成员点名，或直接广播给大家。",
      );
    });

    it("returns the roster-free variant for every bucket when members are empty", () => {
      const ctx: GreetingContext = { kind: "group", memberNames: [] };
      expect(greetingFor(12, ctx)).toBe(
        "中午好，这里是群聊空间。可以广播任务，也可以 @ 指定成员来接。",
      );
      expect(greetingFor(15, ctx)).toBe(
        "下午好，这里是群聊空间。描述任务让大家认领，或点名某位成员推进。",
      );
      expect(greetingFor(20, ctx)).toBe(
        "晚上好，群聊空间的成员在待命。广播、点名或直接派活都可以。",
      );
      expect(greetingFor(23, ctx)).toBe(
        "夜深了，还想让群里的成员帮忙推进什么吗？",
      );
    });

    it("uses the thread title as the space name when present", () => {
      const ctx: GreetingContext = {
        kind: "group",
        title: "前端小队",
        memberNames: ["Alice"],
      };
      const greeting = greetingFor(12, ctx);
      expect(greeting).toBe(
        "中午好，Alice 都在这个群聊「前端小队」里。可以广播任务，也可以 @ 指定成员来接。",
      );
    });

    it("lists at most 3 member names and closes with the total headcount", () => {
      const ctx: GreetingContext = {
        kind: "group",
        memberNames: ["Alice", "Bob", "Charlie", "Diana", "Eve"],
      };
      const greeting = greetingFor(8, ctx);
      expect(greeting).toContain("Alice、Bob、Charlie 等 5 位成员");
      expect(greeting).not.toContain("Diana");
      expect(greeting).not.toContain("Eve");
    });

    it("covers evening and late-night buckets with members", () => {
      const ctx: GreetingContext = {
        kind: "group",
        memberNames: ["Alice", "Bob"],
      };
      expect(greetingFor(20, ctx)).toBe(
        "晚上好，Alice、Bob 在群聊空间里待命。广播、点名或直接派活都可以。",
      );
      expect(greetingFor(23, ctx)).toBe(
        "夜深了，还想让 Alice、Bob 帮忙推进什么吗？",
      );
    });
  });

  describe("time-of-day buckets", () => {
    it("uses different greetings for different hours", () => {
      const ctx: GreetingContext = { kind: "wuu" };
      const morning = greetingFor(8, ctx);
      const noon = greetingFor(12, ctx);
      const afternoon = greetingFor(15, ctx);
      const evening = greetingFor(20, ctx);
      const lateNight = greetingFor(23, ctx);

      expect(morning).toContain("早上好");
      expect(noon).toContain("中午好");
      expect(afternoon).toContain("下午好");
      expect(evening).toContain("晚上好");
      expect(lateNight).toContain("夜深了");
    });

    it("treats 5:00 as morning boundary", () => {
      const ctx: GreetingContext = { kind: "wuu" };
      const greeting = greetingFor(5, ctx);
      expect(greeting).toContain("早上好");
    });

    it("treats 22:00 as late night boundary", () => {
      const ctx: GreetingContext = { kind: "wuu" };
      const greeting = greetingFor(22, ctx);
      expect(greeting).toContain("夜深了");
    });
  });
});
