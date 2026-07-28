import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const channelsCss = readFileSync(resolve(__dirname, "channels.css"), "utf-8");

function ruleFor(selector: string): string {
  return channelsCss.match(new RegExp(`${selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}\\s*\\{([^}]*)\\}`))?.[1] ?? "";
}

describe("channel directory alignment", () => {
  it("uses one directory structure for rooms and agents", () => {
    const directoryLists = ruleFor(".channel-room-list,\n.channel-agent-directory-list");
    const directoryRow = ruleFor(".channel-directory-row");
    const directoryIdentity = ruleFor(".channel-directory-identity");
    const directorySettings = ruleFor(".channel-directory-settings");
    const agentWorkspace = ruleFor(".channel-agent-workspace");
    const agentIdentity = ruleFor(".channel-agent-directory-identity");

    expect(directoryLists).toMatch(/padding:\s*var\(--channel-directory-list-padding\)/);
    expect(directoryLists).toMatch(/gap:\s*3px/);
    expect(directoryRow).toMatch(/height:\s*var\(--channel-directory-row-height\)/);
    expect(directoryRow).toMatch(/grid-template-columns:\s*34px minmax\(0, 1fr\) 28px/);
    expect(directoryRow).toMatch(/gap:\s*var\(--channel-directory-row-gap\)/);
    expect(directoryRow).toMatch(/padding:\s*var\(--channel-directory-row-padding\)/);
    expect(directoryRow).toMatch(/border-radius:\s*var\(--channel-directory-row-radius\)/);
    expect(directoryIdentity).toMatch(/min-width:\s*0/);
    expect(directorySettings).toMatch(/width:\s*28px/);
    expect(agentWorkspace).toMatch(/display:\s*contents/);
    expect(agentIdentity).not.toMatch(/grid-template-columns/);
    expect(channelsCss).not.toContain("channel-agent-directory-actions");
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
    const roomMain = ruleFor(".channel-room-main");
    const stream = ruleFor(".channel-message-stream");
    const footer = ruleFor(".channel-conversation-footer");

    expect(roomMain).toMatch(/display:\s*grid/);
    expect(roomMain).toMatch(/grid-template-rows:\s*auto minmax\(0, 1fr\)/);
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
