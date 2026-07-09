import { useState, type MouseEvent } from "react";
import type { TurnSource } from "./ToolActivityHelpers";

/**
 * "来源 N" pill rendered beside an assistant turn's process header.
 * It stacks one favicon per unique host the turn consulted through
 * `web_search` or `web_fetch` and hands the chosen URL to the OS default
 * browser on click. One slot per host (not per URL), only after at least
 * one web tool resolved, with a first-letter avatar as a fallback for
 * hosts whose favicon can't be fetched.
 *
 * The accessible name + native tooltip carry the full URL, not just the
 * host. The favicon lookup dedupes on host, but the host alone is
 * ambiguous (docs.anthropic.com vs www.anthropic.com vs status.anthropic.com
 * are all "anthropic.com" to the favicon lookup while each is a different
 * page) — the user wants to see which page on the host was actually
 * consulted before opening it.
 *
 * Single-source shortcut: when the turn only consulted one URL, the
 * whole pill becomes a `<button>` so hovering or clicking the "来源"
 * label anywhere opens the page. The icon stack is still the same
 * visual, just rendered as an inert avatar inside the button instead
 * of a nested click target.
 *
 * The component never decides policy on its own. `sources` is fed by
 * `collectTurnSources` and the open-URL responsibility belongs to the
 * main process via `window.wuu.openExternal`; the `onOpen` prop lets
 * tests inject a spy without touching globals.
 */
export function TurnSourcesRow({
  sources,
  onOpen,
}: {
  sources: TurnSource[];
  onOpen?: (url: string) => void;
}): JSX.Element | null {
  if (sources.length === 0) return null;
  // "来源" alone reads better for a single hit. With more, the count
  // helps users decide whether to expand the pill in the future.
  const label = sources.length === 1 ? "来源" : `来源 ${sources.length}`;

  // Single source → the entire pill is the click target. The label and
  // the icon stack both belong to the same button, so there is no dead
  // area where hovering shows the URL but clicking does nothing.
  if (sources.length === 1) {
    const source = sources[0];
    const tooltip = sourceTooltip(source);
    const handleClick = (event: MouseEvent<HTMLButtonElement>): void => {
      event.preventDefault();
      openSource(source, onOpen);
    };
    return (
      <button
        type="button"
        className="turn-sources-pill turn-sources-pill-single"
        aria-label={`打开 ${tooltip}`}
        title={tooltip}
        onClick={handleClick}
      >
        <SourceAvatar source={source} />
        <span className="turn-sources-label">{label}</span>
      </button>
    );
  }

  // Multi-source → the pill is a passive group; each host keeps its own
  // button so users can pick which URL to open without ambiguity.
  return (
    <div className="turn-sources-pill" role="group" aria-label={label}>
      <div className="turn-sources-icons">
        {sources.map((source) => (
          <SourceIcon key={source.host} source={source} onOpen={onOpen} />
        ))}
      </div>
      <span className="turn-sources-label">{label}</span>
    </div>
  );
}

function sourceTooltip(source: TurnSource): string {
  // Show the full URL so users can verify which page on the host was
  // consulted. Title (when present) reads as the human-readable label
  // and the URL is the unambiguous link target. Host alone is not
  // enough — see the file header.
  return source.title ? `${source.title} — ${source.url}` : source.url;
}

function openSource(
  source: TurnSource,
  onOpen: ((url: string) => void) | undefined,
): void {
  if (onOpen) {
    onOpen(source.url);
    return;
  }
  if (typeof window !== "undefined") {
    void window.wuu?.openExternal?.(source.url);
  }
}

function SourceIcon({
  source,
  onOpen,
}: {
  source: TurnSource;
  onOpen?: (url: string) => void;
}): JSX.Element {
  const tooltip = sourceTooltip(source);
  const handleClick = (event: MouseEvent<HTMLButtonElement>): void => {
    event.preventDefault();
    openSource(source, onOpen);
  };
  return (
    <button
      type="button"
      className="turn-source-icon"
      aria-label={`打开 ${tooltip}`}
      title={tooltip}
      onClick={handleClick}
    >
      <SourceAvatar source={source} />
    </button>
  );
}

function SourceAvatar({ source }: { source: TurnSource }): JSX.Element {
  const [failed, setFailed] = useState(false);
  // Google Favicon Service is the de-facto favicon source for chat-
  // style "sources" rows (ChatGPT, Claude web search). It resolves a
  // 32x32 PNG for any host with no API key. If the host has no
  // favicon, the request 404s and `onError` flips us into the
  // first-letter avatar fallback so the stack still reads as one.
  const faviconURL = `https://www.google.com/s2/favicons?domain=${encodeURIComponent(source.host)}&sz=32`;
  return (
    <span className="turn-source-avatar" data-failed={failed || undefined}>
      {failed ? (
        <span className="turn-source-fallback" aria-hidden>
          {source.host[0]?.toUpperCase() ?? "·"}
        </span>
      ) : (
        // `alt=""` because the surrounding button already carries the
        // accessible label for this source; a redundant alt would force
        // screen readers to read the favicon URL twice.
        <img
          src={faviconURL}
          alt=""
          loading="lazy"
          onError={() => setFailed(true)}
        />
      )}
    </span>
  );
}