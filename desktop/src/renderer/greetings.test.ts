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
      expect(greeting).toBe("早上好，Alice、Bob 都在，有事直接喊。");
    });

    it("uses 在 instead of 都在 for a single member", () => {
      const ctx: GreetingContext = { kind: "group", memberNames: ["Alice"] };
      expect(greetingFor(8, ctx)).toBe("早上好，Alice 在，有事直接喊。");
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
      expect(greeting).toBe("早上好，有事直接在群里喊。");
    });

    it("covers every bucket when members are empty", () => {
      const ctx: GreetingContext = { kind: "group", memberNames: [] };
      expect(greetingFor(12, ctx)).toBe("中午好，想让谁接手，@ 一下就行。");
      expect(greetingFor(15, ctx)).toBe("下午好，有任务就丢进群里。");
      expect(greetingFor(20, ctx)).toBe("晚上好，直接在群里派活吧。");
      expect(greetingFor(23, ctx)).toBe("夜深了，还要拉大家推进什么吗？");
    });

    it("does not repeat the group title (the tab and header already show it)", () => {
      const ctx: GreetingContext = {
        kind: "group",
        title: "前端小队",
        memberNames: ["Alice"],
      };
      expect(greetingFor(12, ctx)).not.toContain("前端小队");
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

    it("covers evening bucket with members", () => {
      const ctx: GreetingContext = {
        kind: "group",
        memberNames: ["Alice", "Bob"],
      };
      expect(greetingFor(20, ctx)).toBe("晚上好，Alice、Bob 都在，直接派活吧。");
    });
  });

  describe("dm context", () => {
    it("returns a hand-off greeting for morning", () => {
      const ctx: GreetingContext = { kind: "dm", agentName: "Andy" };
      expect(greetingFor(8, ctx)).toBe("早上好，有什么要交给 Andy 的？");
    });

    it("returns a hand-off greeting for afternoon", () => {
      const ctx: GreetingContext = { kind: "dm", agentName: "Andy" };
      expect(greetingFor(15, ctx)).toBe("下午好，想让 Andy 帮你做点什么？");
    });

    it("covers noon, evening and late-night buckets", () => {
      const ctx: GreetingContext = { kind: "dm", agentName: "Andy" };
      expect(greetingFor(12, ctx)).toBe("中午好，有事直接跟 Andy 说。");
      expect(greetingFor(20, ctx)).toBe("晚上好，任务交给 Andy 就行。");
      expect(greetingFor(23, ctx)).toBe("夜深了，还有要交给 Andy 的吗？");
    });

    it("is clearly distinct from the group greeting", () => {
      const dmCtx: GreetingContext = { kind: "dm", agentName: "Andy" };
      const groupCtx: GreetingContext = {
        kind: "group",
        memberNames: ["Andy"],
      };
      for (const hour of [8, 12, 15, 20, 23]) {
        expect(greetingFor(hour, dmCtx)).toContain("Andy");
        expect(greetingFor(hour, dmCtx)).not.toBe(greetingFor(hour, groupCtx));
      }
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
