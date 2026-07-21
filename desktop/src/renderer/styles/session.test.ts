// Contract test for the session.css abstraction.
//
// This file is the SINGLE place to change session-level design tokens
// (outer width, horizontal padding, composer dialog width/radius).
// These tests pin the contract: if session.css is deleted, the tokens
// stop being defined, the .session-flow class disappears, and the
// "change one file" promise silently breaks.

import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, it, expect } from "vitest";

const sessionCss = readFileSync(
  resolve(__dirname, "session.css"),
  "utf-8",
);
const composerCss = readFileSync(
  resolve(__dirname, "composer.css"),
  "utf-8",
);
const conversationShellCss = readFileSync(
  resolve(__dirname, "conversation-shell.css"),
  "utf-8",
);
const workspaceCss = readFileSync(
  resolve(__dirname, "workspace.css"),
  "utf-8",
);

describe("session.css design tokens (single source of truth)", () => {
  it("defines --session-outer-width at :root", () => {
    expect(sessionCss).toMatch(/--session-outer-width:\s*776px/);
  });

  it("defines --session-outer-padding-inline at :root", () => {
    expect(sessionCss).toMatch(/--session-outer-padding-inline:\s*48px/);
  });

  it("defines --session-composer-width at :root", () => {
    expect(sessionCss).toMatch(
      /--session-composer-width:\s*calc\(\s*var\(--session-outer-width\)\s*-\s*var\(--session-outer-padding-inline\)\s*-\s*var\(--session-outer-padding-inline\)\s*\)/,
    );
  });

  it("defines --session-composer-radius at :root", () => {
    expect(sessionCss).toMatch(/--session-composer-radius:\s*18px/);
  });

  it("does not depend on .conversation-pane scope (tokens are at :root)", () => {
    // The whole point of moving tokens out of .conversation-pane was to
    // stop the cascade dependency on being inside the conversation pane.
    // The :root block must appear before the .session-flow selector.
    // Match the selector with its opening brace so the doc comment,
    // which mentions ".session-flow" by name, does not trip the check.
    const rootIndex = sessionCss.indexOf(":root {");
    const classIndex = sessionCss.indexOf(".session-flow {");
    expect(rootIndex).toBeGreaterThanOrEqual(0);
    expect(classIndex).toBeGreaterThan(rootIndex);
  });
});

describe("session.css .session-flow shared layout class", () => {
  it("caps max-width at --session-outer-width", () => {
    expect(sessionCss).toMatch(
      /\.session-flow[\s\S]*?max-width:\s*var\(--session-outer-width\)/,
    );
  });

  it("uses --session-outer-padding-inline for padding-inline", () => {
    expect(sessionCss).toMatch(
      /\.session-flow[\s\S]*?padding-inline:\s*var\(--session-outer-padding-inline\)/,
    );
  });

  it("uses --wuu-grid-cols for grid-template-columns", () => {
    expect(sessionCss).toMatch(
      /\.session-flow[\s\S]*?grid-template-columns:\s*repeat\(var\(--wuu-grid-cols\)/,
    );
  });
});

describe("conversation flow and dock composer alignment", () => {
  it("uses the composer width as the message-flow width", () => {
    expect(conversationShellCss).toMatch(
      /--conversation-message-max-width:\s*var\(--session-composer-width\);/,
    );
  });

  it("does not offset or narrow either side with manual pixel corrections", () => {
    expect(composerCss).not.toMatch(
      /\.dock-composer-wrap \.composer-stack\s*\{[^}]*transform:/,
    );
    expect(workspaceCss).not.toMatch(
      /\.conversation-width:has\(\.turn\)\s*\{/,
    );
  });
});

describe("session.css no legacy conversation-* token names leak through", () => {
  // After the migration, these old names should not reappear in
  // session.css. If they do, the single-source-of-truth story breaks
  // because the same value is defined in two places.
  it("does not redefine --conversation-flow-width", () => {
    expect(sessionCss).not.toMatch(/--conversation-flow-width/);
  });

  it("does not redefine --conversation-dialog-width", () => {
    expect(sessionCss).not.toMatch(/--conversation-dialog-width/);
  });
});
