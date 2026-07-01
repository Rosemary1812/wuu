import { useState, type MouseEvent } from "react";
import type { TurnSource } from "./ToolActivityHelpers";

/**
 * "来源 N" pill rendered at the bottom of an assistant turn. It
 * stacks one favicon per unique host the turn consulted through
 * `web_search` or `web_fetch` and hands the chosen URL to the OS
 * default browser on click. Mirrors the ChatGPT / Claude sources
 * treatment: one slot per host (not per URL), only after at least
 * one web tool resolved, with a first-letter avatar as a fallback
 * for hosts whose favicon can't be fetched.
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

function SourceIcon({
  source,
  onOpen,
}: {
  source: TurnSource;
  onOpen?: (url: string) => void;
}): JSX.Element {
  const [failed, setFailed] = useState(false);
  // Google Favicon Service is the de-facto favicon source for chat-
  // style "sources" rows (ChatGPT, Claude web search). It resolves a
  // 32x32 PNG for any host with no API key. If the host has no
  // favicon, the request 404s and `onError` flips us into the
  // first-letter avatar fallback so the stack still reads as one.
  const faviconURL = `https://www.google.com/s2/favicons?domain=${encodeURIComponent(source.host)}&sz=32`;
  const tooltip = source.title
    ? `${source.title} · ${source.host}`
    : source.host;
  const handleClick = (event: MouseEvent<HTMLButtonElement>): void => {
    event.preventDefault();
    if (onOpen) {
      onOpen(source.url);
      return;
    }
    if (typeof window !== "undefined") {
      void window.wuu?.openExternal?.(source.url);
    }
  };
  return (
    <button
      type="button"
      className="turn-source-icon"
      data-failed={failed || undefined}
      aria-label={`打开 ${tooltip}`}
      title={tooltip}
      onClick={handleClick}
    >
      {failed ? (
        <span className="turn-source-fallback" aria-hidden>
          {source.host[0]?.toUpperCase() ?? "·"}
        </span>
      ) : (
        // `alt=""` because the button itself carries the host label;
        // a redundant alt would force screen readers to read the
        // favicon URL twice.
        <img
          src={faviconURL}
          alt=""
          loading="lazy"
          onError={() => setFailed(true)}
        />
      )}
    </button>
  );
}
