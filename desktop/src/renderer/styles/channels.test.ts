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

  it("aligns the empty room action with the pane heading", () => {
    const roomList = ruleFor(".channel-room-list");

    expect(roomList).toMatch(/padding:\s*var\(--channel-directory-list-padding\)/);
    expect(channelsCss).toContain("--channel-directory-list-padding: 0 8px");
    expect(channelsCss).toMatch(/\.channel-empty-action\s*\{\s*padding:\s*10px 8px/);
  });
});

describe("channel member picker", () => {
  it("keeps member search and selection in one flat scrollable surface", () => {
    const search = ruleFor(".channel-member-picker-search");
    const searchIcon = ruleFor(".channel-member-picker-search > svg");
    const options = ruleFor(".channel-member-picker-options");
    const option = ruleFor(".channel-member-picker-option");

    expect(search).toMatch(/border-bottom:\s*1px solid var\(--border-subtle\)/);
    expect(search).not.toMatch(/background:/);
    expect(search).toMatch(/grid-template-columns:\s*28px minmax\(0, 1fr\) 16px/);
    expect(search).toMatch(/gap:\s*9px/);
    expect(search).toMatch(/padding:\s*0 8px/);
    expect(searchIcon).toMatch(/justify-self:\s*start/);
    expect(options).toMatch(/max-height:\s*132px/);
    expect(options).toMatch(/overflow-y:\s*auto/);
    expect(option).toMatch(/height:\s*43px/);
    expect(option).toMatch(/grid-template-columns:\s*28px minmax\(0, 1fr\) 16px/);
    expect(option).toMatch(/gap:\s*9px/);
    expect(option).toMatch(/padding:\s*0 8px/);
    expect(option).toMatch(/background:\s*transparent/);
    expect(channelsCss).not.toContain(".channel-checkbox-row");
  });
});

describe("channel message resizing", () => {
  it("keeps bubble width and horizontal gutters continuous across window sizes", () => {
    const view = ruleFor(".channel-view");
    const stream = ruleFor(".channel-message-stream");
    const composer = ruleFor(".channel-composer");
    const composerWrap = ruleFor(".channel-composer .dock-composer-wrap");
    const message = ruleFor(".channel-message");
    const ownMessage = ruleFor(".channel-message.own");
    const messageContent = ruleFor(".channel-message-content");
    const ownMessageContent = ruleFor(".channel-message.own .channel-message-content");
    const messageBubble = ruleFor(".channel-message-bubble");

    expect(view).toMatch(/--channel-content-max-width:\s*1040px/);
    expect(view).toMatch(/--channel-horizontal-gutter:\s*clamp\(16px, 3vw, 40px\)/);
    expect(view).toMatch(/--channel-avatar-size:\s*30px/);
    expect(view).toMatch(/--channel-message-column-gap:\s*10px/);
    expect(stream).toMatch(/padding:\s*12px var\(--channel-horizontal-gutter\)/);
    expect(stream).toMatch(/--channel-composer-height,[\s\S]*?--conversation-composer-min-height, 100px[\s\S]*?\+ 30px[\s\S]*?\+ 8px/);
    expect(composer).toMatch(/padding:\s*10px var\(--channel-horizontal-gutter\) 12px/);
    expect(composerWrap).toMatch(/width:\s*min\(100%, var\(--channel-content-max-width\)\)/);
    expect(message).toMatch(/width:\s*min\(100%, var\(--channel-content-max-width\)\)/);
    expect(message).toMatch(/grid-template-columns:\s*var\(--channel-avatar-size\) minmax\(0, 1fr\)/);
    expect(message).toMatch(/gap:\s*var\(--channel-message-column-gap\)/);
    expect(ownMessage).toMatch(/grid-template-columns:\s*var\(--channel-avatar-size\) minmax\(0, 1fr\)/);
    expect(messageContent).toMatch(/max-width:\s*100%/);
    expect(messageContent).toMatch(/width:\s*100%/);
    expect(ownMessageContent).toMatch(/max-width:\s*100%/);
    expect(messageBubble).toMatch(/max-width:\s*100%/);
    expect(messageBubble).toMatch(/background:\s*transparent/);
    expect(messageBubble).toMatch(/padding:\s*0/);
    expect(channelsCss).not.toMatch(/@media\s*\(max-width:\s*720px\)[\s\S]*\.channel-message-content/);
  });

  it("keeps thread summaries on the full message axis with clipped reply previews", () => {
    const content = ruleFor(".channel-message-content.has-thread-digest,\n.channel-message.own .channel-message-content.has-thread-digest");
    const digest = ruleFor(".channel-thread-digest");
    const preview = ruleFor(".channel-thread-digest-preview");

    expect(content).toMatch(/width:\s*100%/);
    expect(content).toMatch(/max-width:\s*100%/);
    expect(digest).toMatch(/display:\s*grid/);
    expect(digest).toMatch(/width:\s*100%/);
    expect(channelsCss).not.toContain(".channel-thread-digest-heading svg");
    expect(digest).toMatch(/padding:\s*5px 0 0/);
    expect(digest).toMatch(/border-top:\s*1px solid var\(--border-subtle\)/);
    expect(digest).toMatch(/background:\s*transparent/);
    expect(preview).toMatch(/text-overflow:\s*ellipsis/);
    expect(preview).toMatch(/white-space:\s*nowrap/);
  });

  it("shares one outer axis between thread messages and their composer", () => {
    const messages = ruleFor(".channel-thread-messages");
    const composer = ruleFor(".channel-thread-footer .channel-composer");

    expect(messages).toMatch(/padding:\s*14px 12px 12px/);
    expect(composer).toMatch(/padding:\s*10px 12px 12px/);
  });

  it("keeps hover actions out of the vertical reading rhythm", () => {
    const meta = ruleFor(".channel-message-meta");
    const actions = ruleFor(".channel-message-actions");

    expect(meta).toMatch(/display:\s*flex/);
    expect(actions).toMatch(/margin:\s*0 0 0 auto/);
    expect(actions).not.toMatch(/position:/);
  });

  it("runs the room scroll surface to the bottom behind a floating composer", () => {
    const roomMain = ruleFor(".channel-room-main");
    const stream = ruleFor(".channel-message-stream");
    const footer = ruleFor(".channel-conversation-footer");

    expect(roomMain).toMatch(/display:\s*grid/);
    expect(roomMain).toMatch(/grid-template-rows:\s*auto auto minmax\(0, 1fr\)/);
    expect(stream).toMatch(/grid-row:\s*3/);
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

describe("channel long agent messages", () => {
  it("stacks a bounded preview and an explicit expand control", () => {
    const card = ruleFor(".channel-message-bubble.long-card");
    const preview = ruleFor(".channel-message-raw-query");
    const toggle = ruleFor(".channel-message-expand-toggle");

    expect(card).toMatch(/display:\s*flex/);
    expect(card).toMatch(/flex-direction:\s*column/);
    expect(preview).toMatch(/white-space:\s*pre-wrap/);
    expect(preview).toMatch(/overflow-wrap:\s*anywhere/);
    expect(toggle).toMatch(/align-self:\s*flex-start/);
    expect(toggle).toMatch(/background:\s*transparent/);
  });
});

describe("channel task board spacing", () => {
  it("uses a compact two-line card rhythm without hidden metadata", () => {
    const board = ruleFor(".channel-task-board");
    const heading = ruleFor(".channel-task-column-heading");
    const items = ruleFor(".channel-task-column-items");
    const card = ruleFor(".channel-task-card");
    const meta = ruleFor(".channel-task-card-meta");

    expect(board).toMatch(/gap:\s*16px/);
    expect(board).toMatch(/padding-top:\s*20px/);
    expect(heading).toMatch(/min-height:\s*36px/);
    expect(items).toMatch(/gap:\s*4px/);
    expect(card).toMatch(/gap:\s*4px/);
    expect(card).toMatch(/min-height:\s*0/);
    expect(card).toMatch(/padding:\s*10px 12px 11px/);
    expect(meta).not.toMatch(/position:\s*absolute/);
    expect(meta).not.toMatch(/clip:/);
    expect(channelsCss).not.toContain(".channel-task-card:hover::after");
  });
});
