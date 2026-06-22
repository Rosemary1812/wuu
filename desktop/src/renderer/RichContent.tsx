import { memo, useMemo } from "react";
import { Streamdown, type Components } from "streamdown";
import { createCodePlugin } from "@streamdown/code";
import { createMermaidPlugin } from "@streamdown/mermaid";

type RichContentProps = {
  text?: string;
  cwd?: string;
};

type MarkdownContentProps = {
  text?: string;
  cwd?: string;
  /**
   * Whether the source item is still receiving deltas. Streamdown uses
   * this to show its caret at the end of the parsed text and to keep
   * Mermaid and Shiki quiet until the underlying block is complete.
   */
  isLive?: boolean;
};

export const RichContent = memo(function RichContent({
  text = "",
  cwd
}: RichContentProps): JSX.Element {
  return (
    <div className="rich-content">
      <MarkdownContent text={text} cwd={cwd} />
    </div>
  );
});

/**
 * Plugin instances are created once and reused across renders. Both
 * plugins hold their own caches (Shiki language worker, Mermaid instance),
 * so reconstructing them per render would defeat the cache and make every
 * streamed delta a full re-init.
 */
const codePlugin = createCodePlugin({ themes: ["github-light", "github-dark"] });
const mermaidPlugin = createMermaidPlugin();

function MarkdownContentView({
  text = "",
  cwd,
  isLive = false
}: MarkdownContentProps): JSX.Element {
  const components = useMemo(() => markdownComponents(cwd), [cwd]);
  return (
    <Streamdown
      components={components}
      plugins={{ code: codePlugin, mermaid: mermaidPlugin }}
      isAnimating={isLive}
      parseIncompleteMarkdown
      lineNumbers={false}
      mode="streaming"
    >
      {text}
    </Streamdown>
  );
}

export const MarkdownContent = memo(MarkdownContentView);

/**
 * Map the standard HTML elements Streamdown emits onto our existing
 * `.rich-*` class names so the desktop's design tokens keep applying
 * without per-block style duplication. The default Streamdown
 * implementations also bake in Tailwind/shadcn utilities, which we
 * explicitly do not want.
 */
function markdownComponents(cwd: string | undefined): Components {
  return {
    p({ children }) {
      return <p className="rich-paragraph">{children}</p>;
    },
    h1({ children }) {
      return <h1 className="rich-heading rich-heading-1">{children}</h1>;
    },
    h2({ children }) {
      return <h2 className="rich-heading rich-heading-2">{children}</h2>;
    },
    h3({ children }) {
      return <h3 className="rich-heading rich-heading-3">{children}</h3>;
    },
    h4({ children }) {
      return <h4 className="rich-heading rich-heading-4">{children}</h4>;
    },
    h5({ children }) {
      return <h5 className="rich-heading rich-heading-5">{children}</h5>;
    },
    h6({ children }) {
      return <h6 className="rich-heading rich-heading-6">{children}</h6>;
    },
    a({ href, children, ...rest }) {
      const safeHref = safeMarkdownHref(href);
      if (!safeHref) {
        return <span>{children}</span>;
      }
      return (
        <a
          className="rich-link"
          href={safeHref}
          target="_blank"
          rel="noreferrer"
          {...rest}
        >
          {children}
        </a>
      );
    },
    img({ src, alt }) {
      if (!src) {
        return null;
      }
      return <RichImage source={src} alt={alt ?? ""} cwd={cwd} inline />;
    },
    blockquote({ children }) {
      return <blockquote className="rich-blockquote">{children}</blockquote>;
    },
    hr() {
      return <hr className="rich-rule" />;
    }
  };
}

/**
 * Only http(s), mailto, wuu-file://, and in-page anchors get through.
 * The renderer must never honor javascript:, data:, or other exotic
 * schemes that an upstream provider could put inside a markdown link.
 */
function safeMarkdownHref(href: string | undefined): string | undefined {
  const value = href?.trim();
  if (!value) {
    return undefined;
  }
  if (value.startsWith("#")) {
    return value;
  }
  return /^(https?:|mailto:|wuu-file:)/i.test(value) ? value : undefined;
}

const IMAGE_MARKDOWN_PATTERN = /!\[([^\]\n]*)\]\(([^)\n]+)\)/g;
const IMAGE_FILE_PATTERN = /\.(apng|avif|gif|jpe?g|png|svg|webp)(?:[?#].*)?$/i;

function RichImage({
  source,
  alt,
  cwd,
  inline = false
}: {
  source: string;
  alt: string;
  cwd: string | undefined;
  inline?: boolean;
}): JSX.Element {
  const resolvedSource = resolveImageSource(source, cwd);
  const image = (
    <img
      className="rich-image"
      src={resolvedSource}
      alt={alt}
      title={imageTarget(source)}
      loading="lazy"
    />
  );
  return inline ? (
    <span className="rich-image-block inline">{image}</span>
  ) : (
    <figure className="rich-image-block">{image}</figure>
  );
}

function bareImageSource(line: string): string | undefined {
  const source = imageTarget(stripListMarker(line));
  if (!source || !IMAGE_FILE_PATTERN.test(source)) {
    return undefined;
  }
  if (isWebImageSource(source) || source.startsWith("file://")) {
    return source;
  }
  if (
    source.startsWith("/") ||
    source.startsWith("~/") ||
    source.startsWith("./") ||
    source.startsWith("../")
  ) {
    return source;
  }
  return source.includes("/") ? source : undefined;
}

function stripListMarker(line: string): string {
  return stripWrappers(line.trim().replace(/^[-*]\s+/, ""));
}

function stripWrappers(value: string): string {
  const pairs: Array<[string, string]> = [
    ["`", "`"],
    ['"', '"'],
    ["'", "'"],
    ["<", ">"]
  ];
  for (const [open, close] of pairs) {
    if (value.startsWith(open) && value.endsWith(close)) {
      return value.slice(open.length, -close.length).trim();
    }
  }
  return value;
}

export function resolveImageSource(
  rawSource: string,
  cwd: string | undefined
): string {
  const source = imageTarget(rawSource);
  const renderableWuuFileURL = renderableBrowserFileURLFromWuuFile(source);
  if (renderableWuuFileURL) {
    return renderableWuuFileURL;
  }
  if (isWebImageSource(source)) {
    return source;
  }
  if (source.startsWith("file://")) {
    return renderableFileURL(fileURLPath(source));
  }
  if (source.startsWith("~/")) {
    return renderableFileURL(resolveHomePath(cwd, source));
  }
  if (source.startsWith("/") || source.startsWith("./") || source.startsWith("../")) {
    return renderableFileURL(
      source.startsWith("/") ? source : resolveRelativePath(cwd, source)
    );
  }
  if (cwd && IMAGE_FILE_PATTERN.test(source)) {
    return renderableFileURL(resolveRelativePath(cwd, source));
  }
  return source;
}

export function imageTarget(rawSource: string): string {
  let source = stripWrappers(rawSource.trim());
  if (source.startsWith("<") && source.endsWith(">")) {
    return source.slice(1, -1).trim();
  }
  const titleMatch = source.match(/^(.*?)(?:\s+["'][^"']*["'])$/);
  if (titleMatch) {
    source = titleMatch[1].trim();
  }
  return source;
}

function isWebImageSource(source: string): boolean {
  return /^(https?:|data:image\/|blob:|wuu-file:)/i.test(source);
}

function fileURLPath(source: string): string {
  try {
    return decodeURIComponent(new URL(source).pathname);
  } catch {
    return source.replace(/^file:\/\//, "");
  }
}

function resolveRelativePath(
  cwd: string | undefined,
  relativePath: string
): string {
  const base = cwd ?? "/";
  const parts = `${base}/${relativePath}`.split("/");
  const stack: string[] = [];
  for (const part of parts) {
    if (!part || part === ".") {
      continue;
    }
    if (part === "..") {
      stack.pop();
      continue;
    }
    stack.push(part);
  }
  return `/${stack.join("/")}`;
}

function resolveHomePath(cwd: string | undefined, path: string): string {
  const homePath = cwd?.match(/^\/Users\/[^/]+/)?.[0] ?? "";
  return `${homePath}/${path.slice(2)}`;
}

function renderableFileURL(filePath: string): string {
  const encodedPath = base64URL(filePath);
  return (
    window.wuuRenderableFileURL?.(encodedPath) ??
    `wuu-file://local/${encodedPath}`
  );
}

function renderableBrowserFileURLFromWuuFile(
  source: string
): string | undefined {
  if (!/^wuu-file:/i.test(source)) {
    return undefined;
  }
  try {
    const url = new URL(source);
    if (url.hostname !== "local") {
      return undefined;
    }
    const encodedPath = url.pathname.replace(/^\/+/, "");
    return encodedPath
      ? window.wuuRenderableFileURL?.(encodedPath)
      : undefined;
  } catch {
    return undefined;
  }
}

function base64URL(value: string): string {
  const bytes = new TextEncoder().encode(value);
  let binary = "";
  for (const byte of bytes) {
    binary += String.fromCharCode(byte);
  }
  return btoa(binary)
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/g, "");
}

// Re-export the IMAGE_MARKDOWN_PATTERN for any consumer that needs the
// same inline-image detection as the markdown pipe. Currently unused
// outside this file, but the symbol may be useful to future shells.
export { IMAGE_MARKDOWN_PATTERN, bareImageSource };
