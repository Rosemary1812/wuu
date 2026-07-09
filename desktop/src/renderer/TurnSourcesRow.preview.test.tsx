/**
 * Visual preview of `TurnSourcesRow` — server-side renders the
 * component with realistic `web_search` / `web_fetch` samples and
 * dumps the resulting HTML. Use this to verify what the user will
 * see at the bottom of an assistant turn *without* needing the
 * Electron dev session to be running. The actual styling (icon
 * stack overlap, hover lift, pill chrome) still comes from
 * `styles/turns.css` at runtime; this test only proves the DOM
 * shape and the data wiring.
 *
 * Run from desktop/:
 *   npx vitest run src/renderer/TurnSourcesRow.preview.test.tsx
 */
import { renderToStaticMarkup } from "react-dom/server";
import { createElement } from "react";
import { describe, expect, it } from "vitest";
import type { TurnSource } from "./ToolActivityHelpers";
import { TurnSourcesRow } from "./TurnSourcesRow";

const searchForOpusAndFriends: TurnSource[] = [
  {
    url: "https://www.anthropic.com/news/claude-opus-4-7",
    host: "anthropic.com",
    title: "Claude Opus 4.7 迁移指南",
    origin: "web_search",
  },
  {
    url: "https://docs.anthropic.com/api",
    host: "docs.anthropic.com",
    title: "Anthropic API 文档",
    origin: "web_search",
  },
  {
    url: "https://platform.openai.com/docs/models",
    host: "platform.openai.com",
    title: "OpenAI Models 索引",
    origin: "web_search",
  },
  {
    // web_fetch without a title — collectors fall back to host-only.
    url: "https://huggingface.co/docs/transformers/index",
    host: "huggingface.co",
    origin: "web_fetch",
  },
];

describe("TurnSourcesRow server-render preview", () => {
  it("renders the 4-source shape Anthropic / OpenAI web_search + HF web_fetch", () => {
    const html = renderToStaticMarkup(
      createElement(TurnSourcesRow, {
        sources: searchForOpusAndFriends,
      }),
    );
    // eslint-disable-next-line no-console
    console.log(
      "\n=== TurnSourcesRow HTML preview (server-rendered) ===\n" +
        html +
        "\n=== end ===",
    );
    expect(html).toContain("turn-sources-pill");
    expect(html).toContain("turn-sources-icons");
    expect(html).toContain("来源 4");
    // The accessible name carries the full URL, not just the host.
    // Title (when present) reads as the human-readable label and the
    // URL is the unambiguous link target — the favicon lookup dedupes
    // on host, but `docs.anthropic.com` and `www.anthropic.com` are
    // both "anthropic.com" to the favicon service and only the URL
    // tells them apart.
    expect(html).toContain(
      'aria-label="打开 Claude Opus 4.7 迁移指南 — https://www.anthropic.com/news/claude-opus-4-7"',
    );
    expect(html).toContain('aria-label="打开 https://huggingface.co/docs/transformers/index"');
    // The favicon URL is the only thing the component knows about
    // the host; the actual pixel rendering is the OS default browser
    // (or the fallback first-letter avatar) and lives outside this
    // server render.
    expect(html).toContain("google.com/s2/favicons?domain=anthropic.com");
    expect(html).toContain("google.com/s2/favicons?domain=huggingface.co");
  });
});
