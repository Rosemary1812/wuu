import { afterEach, describe, expect, it } from "vitest";
import {
  createAgentTreeDemo,
  createConversationFixture,
} from "./ConversationFixtures";
import { setActiveLocale } from "./i18n";

afterEach(() => setActiveLocale("zh-CN"));

describe("localized conversation fixtures", () => {
  it("builds English conversation and agent-tree samples", () => {
    setActiveLocale("en-US");
    expect(createConversationFixture("long", "/tmp/demo").preview).toBe(
      "Demo: long-form reading width",
    );
    expect(createAgentTreeDemo("/tmp/demo").parent.preview).toBe(
      "Demo: parent agent delegates subtasks",
    );
  });
});
