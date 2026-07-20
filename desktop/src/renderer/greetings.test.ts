import { describe, expect, it } from "vitest";
import {
  greetingFor,
  type GreetingContext,
} from "./greetings";

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
