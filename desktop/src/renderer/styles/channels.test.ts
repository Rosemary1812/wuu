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
    const ownMessageContent = ruleFor(".channel-message.own .channel-message-content");
    const messageBubble = ruleFor(".channel-message-bubble");

    expect(stream).toMatch(/--channel-composer-height,[\s\S]*?--conversation-composer-min-height, 100px[\s\S]*?\+ 30px[\s\S]*?\+ 12px/);
    expect(composer).toMatch(/padding:\s*12px clamp\(20px, 5vw, 72px\) 18px/);
    expect(messageContent).toMatch(/max-width:\s*100%/);
    expect(ownMessageContent).toMatch(/max-width:\s*calc\(100% - 40px\)/);
    expect(messageBubble).toMatch(/max-width:\s*100%/);
    expect(channelsCss).not.toMatch(/@media\s*\(max-width:\s*720px\)[\s\S]*\.channel-message-content/);
  });

  it("runs the room scroll surface to the bottom behind a floating composer", () => {
    const conversation = ruleFor(".channel-conversation");
    const stream = ruleFor(".channel-message-stream");
    const footer = ruleFor(".channel-conversation-footer");

    expect(conversation).toMatch(/display:\s*grid/);
    expect(conversation).toMatch(/grid-template-rows:\s*auto minmax\(0, 1fr\)/);
    expect(stream).toMatch(/grid-row:\s*2/);
    expect(stream).toMatch(/overflow-y:\s*auto/);
    expect(stream).toMatch(/scrollbar-gutter:\s*stable/);
    expect(footer).toMatch(/position:\s*absolute/);
    expect(footer).toMatch(/bottom:\s*0/);
    expect(footer).toMatch(/pointer-events:\s*none/);
    expect(channelsCss).not.toMatch(/\.channel-composer \.dock-composer-wrap::before\s*\{[^}]*display:\s*none/);
  });
});

describe("channel agent status", () => {
  it("keeps thinking indicators static", () => {
    expect(channelsCss).not.toContain("channel-agent-status-pulse");
  });
});
