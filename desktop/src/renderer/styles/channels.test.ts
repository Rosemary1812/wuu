import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const channelsCss = readFileSync(resolve(__dirname, "channels.css"), "utf-8");

function ruleFor(selector: string): string {
  return channelsCss.match(new RegExp(`${selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}\\s*\\{([^}]*)\\}`))?.[1] ?? "";
}

describe("channel directory alignment", () => {
  it("uses the shared directory metrics for rooms and agents", () => {
    const roomList = ruleFor(".channel-room-list");
    const roomRows = ruleFor(".channel-room-row,\n.channel-agent-button");
    const agentList = ruleFor(".channel-agent-directory-list");
    const agentRow = ruleFor(".channel-agent-directory-row");
    const agentIdentity = ruleFor(".channel-agent-directory-identity");

    expect(roomList).toMatch(/padding:\s*var\(--channel-directory-list-padding\)/);
    expect(agentList).toMatch(/padding:\s*var\(--channel-directory-list-padding\)/);
    expect(roomRows).toMatch(/height:\s*var\(--channel-directory-row-height\)/);
    expect(roomRows).toMatch(/gap:\s*var\(--channel-directory-row-gap\)/);
    expect(roomRows).toMatch(/padding:\s*var\(--channel-directory-row-padding\)/);
    expect(agentRow).toMatch(/height:\s*var\(--channel-directory-row-height\)/);
    expect(agentRow).toMatch(/border-radius:\s*var\(--channel-directory-row-radius\)/);
    expect(agentIdentity).toMatch(/gap:\s*var\(--channel-directory-row-gap\)/);
    expect(agentIdentity).toMatch(/padding:\s*var\(--channel-directory-row-padding\)/);
  });
});

describe("channel message resizing", () => {
  it("keeps bubble width and horizontal gutters continuous across window sizes", () => {
    const stream = ruleFor(".channel-message-stream");
    const composer = ruleFor(".channel-composer");
    const messageContent = ruleFor(".channel-message-content");

    expect(stream).toMatch(/padding:\s*18px clamp\(20px, 5vw, 72px\)/);
    expect(composer).toMatch(/padding:\s*12px clamp\(20px, 5vw, 72px\) 18px/);
    expect(messageContent).toMatch(/max-width:\s*72%/);
    expect(channelsCss).not.toMatch(/@media\s*\(max-width:\s*720px\)[\s\S]*\.channel-message-content/);
  });
});
