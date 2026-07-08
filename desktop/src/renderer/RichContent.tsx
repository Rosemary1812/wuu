import { Children, cloneElement, isValidElement, memo, useEffect, useId, useMemo, useState, type KeyboardEvent as ReactKeyboardEvent, type ReactElement, type ReactNode } from "react";
import { FileText, Github, Globe2, Mail } from "lucide-react";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import type { WorkspaceFileReferenceResolveResult } from "../shared/protocol";
import { useImagePreview } from "./ImagePreview";
import { MessageCopyButton } from "./MessageActions";

type RichContentProps = {
  text?: string;
  cwd?: string;
  onOpenFile?: (path: string) => void;
};

export type RichBlock =
  | { kind: "paragraph"; text: string }
  | { kind: "image"; source: string; alt?: string }
  | { kind: "code"; language: string; code: string }
  | { kind: "mermaid"; code: string };

export type RichBlockWithOffset = RichBlock & {
  startOffset: number;
};

export type RichTextRenderer = (text: string, keyPrefix: string) => Array<JSX.Element | string>;

type RichTextRenderOptions = {
  cwd?: string;
  onOpenFile?: (path: string) => void;
  renderText?: RichTextRenderer;
  autoLinkFiles?: boolean;
};

type MermaidState =
  | { status: "rendering" }
  | { status: "rendered"; svg: string }
  | { status: "error"; message: string };

const IMAGE_MARKDOWN_PATTERN = /!\[([^\]\n]*)\]\(([^)\n]+)\)/g;
const IMAGE_FILE_PATTERN = /\.(apng|avif|gif|jpe?g|png|svg|webp)(?:[?#].*)?$/i;
const AUTO_FILE_REFERENCE_PREFIX_SOURCE = String.raw`(^|[\s([{"'])`;
const AUTO_FILE_PATH_SOURCE = String.raw`(?:(?:~|\.{1,2})\/|\/)?(?:[A-Za-z0-9_@+.-]+\/)*[A-Za-z0-9_@+.-]+\.[A-Za-z0-9][A-Za-z0-9_-]{0,15}`;
const AUTO_FILE_LINE_RANGE_SEPARATOR_SOURCE = String.raw`[-:\u2013\u2014]`;
const AUTO_FILE_LINE_SUFFIX_SOURCE = String.raw`(?::\d+(?:${AUTO_FILE_LINE_RANGE_SEPARATOR_SOURCE}\d+)?|\s+\((?:line|lines)\s+\d+(?:${AUTO_FILE_LINE_RANGE_SEPARATOR_SOURCE}\d+)?\))`;
const AUTO_FILE_REFERENCE_PATTERN = new RegExp(
  `${AUTO_FILE_REFERENCE_PREFIX_SOURCE}${AUTO_FILE_PATH_SOURCE}${AUTO_FILE_LINE_SUFFIX_SOURCE}?`,
  "gi",
);
const AUTO_FILE_LINE_SUFFIX_PATTERN = new RegExp(`^(.*?)${AUTO_FILE_LINE_SUFFIX_SOURCE}$`, "i");
const AUTO_LINK_FILE_EXTENSIONS = new Set([
  ".avif",
  ".bash",
  ".c",
  ".cc",
  ".cjs",
  ".cpp",
  ".cs",
  ".css",
  ".csv",
  ".dart",
  ".dockerignore",
  ".env",
  ".fish",
  ".gif",
  ".gitignore",
  ".go",
  ".h",
  ".hpp",
  ".htm",
  ".html",
  ".java",
  ".jpeg",
  ".jpg",
  ".js",
  ".json",
  ".jsonl",
  ".jsx",
  ".kt",
  ".kts",
  ".less",
  ".lock",
  ".lua",
  ".md",
  ".mdx",
  ".mjs",
  ".mod",
  ".pdf",
  ".php",
  ".png",
  ".py",
  ".rb",
  ".rs",
  ".sass",
  ".scss",
  ".sh",
  ".sql",
  ".sum",
  ".svg",
  ".svelte",
  ".swift",
  ".toml",
  ".ts",
  ".tsv",
  ".tsx",
  ".txt",
  ".vue",
  ".webp",
  ".xml",
  ".yaml",
  ".yml",
  ".zsh"
]);

export const RichContent = memo(function RichContent({ text = "", cwd, onOpenFile }: RichContentProps): JSX.Element {
  return (
    <div className="rich-content">
      <MarkdownContent text={text} cwd={cwd} onOpenFile={onOpenFile} />
    </div>
  );
});

function MarkdownContentView({
  text,
  cwd,
  renderText,
  onOpenFile,
  renderMermaid = true
}: {
  text: string;
  cwd?: string;
  renderText?: RichTextRenderer;
  onOpenFile?: (path: string) => void;
  renderMermaid?: boolean;
}): JSX.Element {
  const components = useMemo(
    () => markdownComponents(cwd, renderText, renderMermaid, onOpenFile),
    [cwd, renderText, renderMermaid, onOpenFile]
  );
  return (
    <ReactMarkdown components={components} remarkPlugins={[remarkGfm]}>
      {text}
    </ReactMarkdown>
  );
}

export const MarkdownContent = memo(MarkdownContentView);

export function RichContentBlock({
  block,
  blockKey,
  cwd,
  renderText,
  onOpenFile
}: {
  block: RichBlock;
  blockKey: string;
  cwd?: string;
  renderText?: RichTextRenderer;
  onOpenFile?: (path: string) => void;
}): JSX.Element {
  if (block.kind === "image") {
    return <RichImage source={block.source} alt={block.alt ?? ""} cwd={cwd} />;
  }
  if (block.kind === "mermaid") {
    return <MermaidDiagram code={block.code} />;
  }
  if (block.kind === "code") {
    return (
      <pre className="rich-code">
        <code>{block.code}</code>
      </pre>
    );
  }
  return (
    <p className="rich-paragraph">
      {renderInlineContent(block.text, cwd, blockKey, renderText, onOpenFile)}
    </p>
  );
}

export function parseRichBlocks(text: string): RichBlock[] {
  return parseRichBlocksWithOffsets(text).map(({ startOffset: _startOffset, ...block }) => block);
}

export function parseRichBlocksWithOffsets(
  text: string,
  { allowOpenFence = false }: { allowOpenFence?: boolean } = {}
): RichBlockWithOffset[] {
  const blocks: RichBlockWithOffset[] = [];
  const fencePattern = allowOpenFence ? /```([^\n`]*)\n([\s\S]*?)(```|$)/g : /```([^\n`]*)\n([\s\S]*?)```/g;
  let cursor = 0;
  let match: RegExpExecArray | null;

  while ((match = fencePattern.exec(text))) {
    pushParagraphBlocks(blocks, text.slice(cursor, match.index), cursor);
    const language = match[1].trim().toLowerCase();
    const code = match[2].replace(/\n$/, "");
    blocks.push(
      language === "mermaid"
        ? { kind: "mermaid", code, startOffset: match.index }
        : { kind: "code", language, code, startOffset: match.index }
    );
    cursor = match.index + match[0].length;
  }

  pushParagraphBlocks(blocks, text.slice(cursor), cursor);
  return blocks.length > 0 ? blocks : [{ kind: "paragraph", text, startOffset: 0 }];
}

function pushParagraphBlocks(blocks: RichBlockWithOffset[], text: string, baseOffset: number): void {
  const separatorPattern = /\n{2,}/g;
  let cursor = 0;
  let match: RegExpExecArray | null;

  while ((match = separatorPattern.exec(text))) {
    pushParagraphSegment(blocks, text.slice(cursor, match.index), baseOffset + cursor);
    cursor = match.index + match[0].length;
  }
  pushParagraphSegment(blocks, text.slice(cursor), baseOffset + cursor);
}

function pushParagraphSegment(blocks: RichBlockWithOffset[], paragraph: string, baseOffset: number): void {
  const leadingTrim = paragraph.match(/^\n+/)?.[0].length ?? 0;
  const trailingTrim = paragraph.match(/\n+$/)?.[0].length ?? 0;
  const content = paragraph.slice(leadingTrim, paragraph.length - trailingTrim);
  if (content.trim()) {
    pushParagraphOrImageBlocks(blocks, content, baseOffset + leadingTrim);
  }
}

function pushParagraphOrImageBlocks(blocks: RichBlockWithOffset[], content: string, baseOffset: number): void {
  const textLines: string[] = [];
  let textStartOffset = baseOffset;
  let lineOffset = 0;
  for (const line of content.split("\n")) {
    const imageSource = bareImageSource(line);
    if (!imageSource) {
      if (textLines.length === 0) {
        textStartOffset = baseOffset + lineOffset;
      }
      textLines.push(line);
      lineOffset += line.length + 1;
      continue;
    }
    pushTextLines(blocks, textLines, textStartOffset);
    blocks.push({ kind: "image", source: imageSource, startOffset: baseOffset + lineOffset });
    lineOffset += line.length + 1;
  }
  pushTextLines(blocks, textLines, textStartOffset);
}

function pushTextLines(blocks: RichBlockWithOffset[], lines: string[], baseOffset: number): void {
  const rawText = lines.join("\n");
  lines.length = 0;
  const leadingTrim = rawText.length - rawText.trimStart().length;
  const text = rawText.trim();
  if (text) {
    blocks.push({ kind: "paragraph", text, startOffset: baseOffset + leadingTrim });
  }
}

function renderInlineContent(
  text: string,
  cwd: string | undefined,
  keyPrefix: string,
  renderText: RichTextRenderer | undefined,
  onOpenFile: ((path: string) => void) | undefined
): Array<JSX.Element | string> {
  const output: Array<JSX.Element | string> = [];
  const pushText = (value: string, key: string): void => {
    if (!value) {
      return;
    }
    output.push(
      ...renderRichText(value, key, {
        cwd,
        onOpenFile,
        renderText,
        autoLinkFiles: true
      })
    );
  };
  let cursor = 0;
  let match: RegExpExecArray | null;
  IMAGE_MARKDOWN_PATTERN.lastIndex = 0;

  while ((match = IMAGE_MARKDOWN_PATTERN.exec(text))) {
    if (match.index > cursor) {
      pushText(text.slice(cursor, match.index), `${keyPrefix}-text-${cursor}`);
    }
    const alt = match[1].trim();
    output.push(<RichImage key={`${keyPrefix}-image-${match.index}`} source={match[2]} alt={alt} cwd={cwd} inline />);
    cursor = match.index + match[0].length;
  }

  if (cursor < text.length) {
    pushText(text.slice(cursor), `${keyPrefix}-text-${cursor}`);
  }
  return output;
}

type CodeElementProps = {
  className?: string;
  children?: ReactNode;
};

function markdownComponents(
  cwd: string | undefined,
  renderText: RichTextRenderer | undefined,
  renderMermaid: boolean,
  onOpenFile: ((path: string) => void) | undefined
): Components {
  const richTextOptions: RichTextRenderOptions = {
    cwd,
    onOpenFile,
    renderText,
    autoLinkFiles: true
  };
  const plainTextOptions: RichTextRenderOptions = {
    renderText,
    autoLinkFiles: false
  };
  return {
    p({ children }) {
      return <p className="rich-paragraph">{renderMarkdownText(children, richTextOptions, "p")}</p>;
    },
    // Every heading level renders as the same whisper-level anchor: a
    // same-size semibold paragraph with extra space above (.rich-heading).
    // One tier only — the stream needs scannable section marks, not a
    // document outline. Flattening headings to plain paragraphs (the
    // previous iteration) left long answers with no anchors at all.
    h1({ children }) {
      return <p className="rich-heading">{renderMarkdownText(children, richTextOptions, "h1")}</p>;
    },
    h2({ children }) {
      return <p className="rich-heading">{renderMarkdownText(children, richTextOptions, "h2")}</p>;
    },
    h3({ children }) {
      return <p className="rich-heading">{renderMarkdownText(children, richTextOptions, "h3")}</p>;
    },
    h4({ children }) {
      return <p className="rich-heading">{renderMarkdownText(children, richTextOptions, "h4")}</p>;
    },
    h5({ children }) {
      return <p className="rich-heading">{renderMarkdownText(children, richTextOptions, "h5")}</p>;
    },
    h6({ children }) {
      return <p className="rich-heading">{renderMarkdownText(children, richTextOptions, "h6")}</p>;
    },
    a({ href, title, children }) {
      const inner = renderMarkdownText(children, plainTextOptions, "a");
      const safeHref = safeMarkdownHref(href);
      if (!safeHref) {
        return <span>{inner}</span>;
      }
      return <RichWebLink href={safeHref} title={title}>{inner}</RichWebLink>;
    },
    img({ src, alt }) {
      if (!src) {
        return null;
      }
      return <RichImage source={src} alt={alt ?? ""} cwd={cwd} inline />;
    },
    pre({ children }) {
      const child = Children.toArray(children)[0];
      if (isValidElement<CodeElementProps>(child)) {
        const language = languageFromClassName(child.props.className);
        if (language === "mermaid" && renderMermaid) {
          return <MermaidDiagram code={reactNodeText(child.props.children).replace(/\n$/, "")} />;
        }
        const code = reactNodeText(child.props.children).replace(/\n$/, "");
        return (
          <RichCodeBlock code={code} language={language}>
            {children}
          </RichCodeBlock>
        );
      }
      return <pre className="rich-code">{children}</pre>;
    },
    code({ className, children }) {
      return <code className={className}>{renderMarkdownText(children, plainTextOptions, "code")}</code>;
    },
    li({ children }) {
      return <li>{renderMarkdownText(children, richTextOptions, "li")}</li>;
    },
    table({ children }) {
      return (
        <div className="rich-table-wrap">
          <table>{children}</table>
        </div>
      );
    },
    th({ children }) {
      return <th>{renderMarkdownText(children, richTextOptions, "th")}</th>;
    },
    td({ children }) {
      return <td>{renderMarkdownText(children, richTextOptions, "td")}</td>;
    },
    blockquote({ children }) {
      return <blockquote className="rich-blockquote">{renderMarkdownText(children, richTextOptions, "blockquote")}</blockquote>;
    },
    hr() {
      return <hr className="rich-rule" />;
    }
  };
}

function RichCodeBlock({
  code,
  language,
  children
}: {
  code: string;
  language: string;
  children: ReactNode;
}): JSX.Element {
  // The header (language label + copy button) lives in its own row so the
  // scrolling code area is not visually coupled to a floating overlay. With
  // the previous single-container layout the absolute-positioned header sat
  // on top of a pre that used padding-top to leave room, which meant long
  // code blocks had the header painted into the same overflow:hidden box as
  // the scrollable content.
  return (
    <div className="rich-code-block">
      <div className="rich-code-header">
        {language ? <span className="rich-code-language">{language}</span> : null}
        <MessageCopyButton
          getText={() => code}
          className="rich-code-copy"
          iconSize={13}
          idleLabel="复制代码"
          copiedLabel="已复制代码"
          failedLabel="复制失败"
        />
      </div>
      <pre className="rich-code">
        {children}
      </pre>
    </div>
  );
}

function renderMarkdownText(
  children: ReactNode,
  options: RichTextRenderOptions,
  keyPrefix: string
): ReactNode {
  if (!options.renderText && (!options.autoLinkFiles || !options.onOpenFile)) {
    return children;
  }
  return Children.toArray(children).flatMap((child, index): ReactNode[] => {
    const childKey = `${keyPrefix}-${index}`;
    if (typeof child === "string" || typeof child === "number") {
      const text = String(child);
      return renderRichText(text, childKey, options);
    }
    if (!isValidElement<{ children?: ReactNode }>(child) || child.props.children === undefined) {
      return [child];
    }
    return [
      cloneElement(child, {
        children: renderMarkdownText(child.props.children, options, childKey)
      })
    ];
  });
}

function renderRichText(
  text: string,
  keyPrefix: string,
  options: RichTextRenderOptions
): Array<JSX.Element | string> {
  if (!options.autoLinkFiles || !options.onOpenFile) {
    return options.renderText ? options.renderText(text, keyPrefix) : [text];
  }

  const output: Array<JSX.Element | string> = [];
  const pushPlain = (value: string, key: string): void => {
    if (!value) {
      return;
    }
    output.push(...(options.renderText ? options.renderText(value, key) : [value]));
  };

  let cursor = 0;
  let match: RegExpExecArray | null;
  AUTO_FILE_REFERENCE_PATTERN.lastIndex = 0;
  while ((match = AUTO_FILE_REFERENCE_PATTERN.exec(text))) {
    const prefixLength = match[1].length;
    const referenceStart = match.index + prefixLength;
    const reference = match[0].slice(prefixLength);
    const candidatePath = filePathFromReference(reference);
    if (!isFileReferenceCandidate(candidatePath)) {
      continue;
    }
    pushPlain(text.slice(cursor, referenceStart), `${keyPrefix}-text-${cursor}`);
    output.push(
      <RichResolvedFileReference
        key={`${keyPrefix}-file-${referenceStart}`}
        display={reference}
        cwd={options.cwd}
        onOpenFile={options.onOpenFile}
      />
    );
    cursor = referenceStart + reference.length;
  }

  pushPlain(text.slice(cursor), `${keyPrefix}-text-${cursor}`);
  return output;
}

// Resolved file references are cached at the module level so that switching
// sessions or tabs does not force every previously-rendered file reference to
// re-issue an IPC roundtrip and visually flip from plain text to the red link.
// The cache is keyed by (display, cwd) so different worktrees stay separate.
type ResolvedFileReference =
  | { status: "resolved"; path: string }
  | { status: "unresolved" };

const fileReferenceResolutionCache = new Map<string, ResolvedFileReference>();
const fileReferenceResolutionInflight = new Map<string, Promise<ResolvedFileReference>>();

function fileReferenceCacheKey(display: string, cwd: string | undefined): string {
  return `${cwd ?? ""}::${display}`;
}

function lookupCachedFileReference(
  display: string,
  cwd: string | undefined
): ResolvedFileReference | undefined {
  return fileReferenceResolutionCache.get(fileReferenceCacheKey(display, cwd));
}

function subscribeToFileReferenceResolution(
  display: string,
  cwd: string | undefined,
  onResolved: (result: ResolvedFileReference) => void
): void {
  const key = fileReferenceCacheKey(display, cwd);
  const cached = fileReferenceResolutionCache.get(key);
  if (cached) {
    onResolved(cached);
    return;
  }

  const resolver = window.wuu?.resolveWorkspaceFileReference;
  if (!resolver) {
    const result: ResolvedFileReference = { status: "unresolved" };
    fileReferenceResolutionCache.set(key, result);
    onResolved(result);
    return;
  }

  let inflight = fileReferenceResolutionInflight.get(key);
  if (!inflight) {
    inflight = resolver(display)
      .then(
        (result): ResolvedFileReference =>
          result.status === "resolved" && result.path
            ? { status: "resolved", path: result.path }
            : { status: "unresolved" }
      )
      .catch((): ResolvedFileReference => ({ status: "unresolved" }));
    fileReferenceResolutionInflight.set(key, inflight);
    void inflight.then((result) => {
      fileReferenceResolutionCache.set(key, result);
      fileReferenceResolutionInflight.delete(key);
    });
  }
  void inflight.then((result) => {
    onResolved(result);
  });
}

function RichResolvedFileReference({
  display,
  cwd,
  onOpenFile
}: {
  display: string;
  cwd: string | undefined;
  onOpenFile: (path: string) => void;
}): JSX.Element {
  const [resolved, setResolved] = useState<ResolvedFileReference | undefined>(
    () => lookupCachedFileReference(display, cwd)
  );

  useEffect(() => {
    let cancelled = false;
    setResolved(lookupCachedFileReference(display, cwd));
    subscribeToFileReferenceResolution(display, cwd, (result) => {
      if (cancelled) {
        return;
      }
      setResolved(result);
    });
    return () => {
      cancelled = true;
    };
  }, [cwd, display]);

  if (resolved === undefined) {
    // First render for a reference we have not resolved yet. Render a
    // link-shaped placeholder so the visual layout is stable; the real
    // link replaces it once the IPC roundtrip completes.
    return (
      <span
        className="rich-link rich-file-link rich-file-link--pending"
        aria-busy="true"
        data-pending-file-reference={display}
      >
        <span className="rich-link-content">
          <span className="rich-link-icon" aria-hidden="true">
            <FileText className="icon-xs" />
          </span>
          <span className="rich-link-label">{display}</span>
        </span>
      </span>
    );
  }

  // Any reference that survived the candidate check (extension whitelist,
  // not a URL) is rendered as a red link for visual consistency, even if
  // the workspace does not actually contain the file. Otherwise a list
  // like "段二改了 → a.ts, b.ts, c.ts" would mix highlighted and plain
  // entries whenever one of the files has been deleted or is ambiguous,
  // which is the exact UX regression the user reported. When the IPC
  // reports missing/ambiguous/invalid, the link still points at the
  // display string and the file viewer surfaces its own "not found"
  // feedback on click.
  const linkPath = resolved.status === "resolved" ? resolved.path : display;
  return (
    <RichFileLink
      display={display}
      path={linkPath}
      onOpenFile={onOpenFile}
    />
  );
}

function resolveWorkspaceFileReference(
  reference: string,
  cwd: string | undefined,
): Promise<WorkspaceFileReferenceResolveResult> {
  const resolver = window.wuu?.resolveWorkspaceFileReference;
  if (!resolver) {
    return Promise.resolve({
      root: cwd ?? "",
      reference,
      status: "missing",
    });
  }

  return resolver(reference).catch(() => ({
    root: cwd ?? "",
    reference,
    status: "invalid" as const,
  }));
}

// Exposed for tests so the module-level cache does not leak between cases.
export function __resetRichFileReferenceResolutionCacheForTests(): void {
  fileReferenceResolutionCache.clear();
  fileReferenceResolutionInflight.clear();
}

function RichFileLink({
  display,
  path,
  onOpenFile
}: {
  display: string;
  path: string;
  onOpenFile: (path: string) => void;
}): JSX.Element {
  return (
    <button
      type="button"
      className="rich-link rich-file-link"
      title={`打开文件：${path}`}
      onClick={() => onOpenFile(path)}
    >
      <span className="rich-link-content">
        <span className="rich-link-icon" aria-hidden="true">
          <FileText className="icon-xs" />
        </span>
        <span className="rich-link-label">{display}</span>
      </span>
    </button>
  );
}

function RichWebLink({
  href,
  title,
  children
}: {
  href: string;
  title?: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <a className="rich-link rich-web-link" href={href} title={title} target="_blank" rel="noreferrer">
      <span className="rich-link-content">
        <RichWebLinkIcon href={href} />
        <span className="rich-link-label">{children}</span>
      </span>
    </a>
  );
}

function RichWebLinkIcon({ href }: { href: string }): JSX.Element {
  if (/^mailto:/i.test(href)) {
    return (
      <span className="rich-link-icon" aria-hidden="true">
        <Mail className="icon-xs" />
      </span>
    );
  }

  const host = linkHost(href);
  if (host && /(^|\.)github\.com$/i.test(host)) {
    return (
      <span className="rich-link-icon" aria-hidden="true">
        <Github className="icon-xs" />
      </span>
    );
  }

  const favicon = faviconSource(href);
  return (
    <span className="rich-link-icon rich-link-favicon-frame" aria-hidden="true">
      <Globe2 className="icon-xs rich-link-fallback-icon" />
      {favicon ? (
        <img
          className="rich-link-favicon"
          src={favicon}
          alt=""
          loading="lazy"
          onError={(event) => {
            event.currentTarget.style.display = "none";
          }}
        />
      ) : null}
    </span>
  );
}

function filePathFromReference(reference: string): string | undefined {
  const match = reference.match(AUTO_FILE_LINE_SUFFIX_PATTERN);
  const path = (match?.[1] ?? reference).trim();
  return path ? path : undefined;
}

function isFileReferenceCandidate(path: string | undefined): boolean {
  const normalizedPath = path?.trim() ?? "";
  if (!normalizedPath || /^https?:\/\//i.test(normalizedPath)) {
    return false;
  }
  return AUTO_LINK_FILE_EXTENSIONS.has(fileExtension(normalizedPath));
}

function fileExtension(path: string): string {
  const fileName = path.split("/").pop() ?? "";
  const dotIndex = fileName.lastIndexOf(".");
  if (dotIndex < 0) {
    return "";
  }
  return fileName.slice(dotIndex).toLowerCase();
}

function faviconSource(href: string): string | undefined {
  try {
    const url = new URL(href);
    if (url.protocol !== "http:" && url.protocol !== "https:") {
      return undefined;
    }
    return `${url.origin}/favicon.ico`;
  } catch {
    return undefined;
  }
}

function linkHost(href: string): string | undefined {
  try {
    return new URL(href).hostname;
  } catch {
    return undefined;
  }
}

function languageFromClassName(className: string | undefined): string {
  const match = className?.match(/(?:^|\s)language-([^\s]+)/);
  return match?.[1]?.toLowerCase() ?? "";
}

function reactNodeText(node: ReactNode): string {
  if (typeof node === "string" || typeof node === "number") {
    return String(node);
  }
  if (Array.isArray(node)) {
    return node.map(reactNodeText).join("");
  }
  return "";
}

function safeMarkdownHref(href: string | undefined): string | undefined {
  const value = href?.trim();
  if (!value) {
    return undefined;
  }
  if (value.startsWith("#")) {
    return value;
  }
  return /^(https?:|mailto:)/i.test(value) ? value : undefined;
}

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
  const { openPreview } = useImagePreview();
  const titleText = imageTarget(source);
  const handleActivate = (): void => {
    openPreview({ src: resolvedSource, alt, title: titleText });
  };
  const handleKeyDown = (event: ReactKeyboardEvent<HTMLImageElement>): void => {
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      handleActivate();
    }
  };
  const image = (
    <img
      className="rich-image"
      src={resolvedSource}
      alt={alt}
      title={titleText}
      loading="lazy"
      role="button"
      tabIndex={0}
      aria-label={alt ? `放大查看：${alt}` : "放大查看图片"}
      onClick={handleActivate}
      onKeyDown={handleKeyDown}
    />
  );
  return inline ? <span className="rich-image-block inline">{image}</span> : <figure className="rich-image-block">{image}</figure>;
}

function bareImageSource(line: string): string | undefined {
  const source = imageTarget(stripListMarker(line));
  if (!source || !IMAGE_FILE_PATTERN.test(source)) {
    return undefined;
  }
  if (isWebImageSource(source) || source.startsWith("file://")) {
    return source;
  }
  if (source.startsWith("/") || source.startsWith("~/") || source.startsWith("./") || source.startsWith("../")) {
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

export function resolveImageSource(rawSource: string, cwd: string | undefined): string {
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
    return renderableFileURL(source.startsWith("/") ? source : resolveRelativePath(cwd, source));
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

function resolveRelativePath(cwd: string | undefined, relativePath: string): string {
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
  return window.wuuRenderableFileURL?.(encodedPath) ?? `wuu-file://local/${encodedPath}`;
}

function renderableBrowserFileURLFromWuuFile(source: string): string | undefined {
  if (!/^wuu-file:/i.test(source)) {
    return undefined;
  }
  try {
    const url = new URL(source);
    if (url.hostname !== "local") {
      return undefined;
    }
    const encodedPath = url.pathname.replace(/^\/+/, "");
    return encodedPath ? window.wuuRenderableFileURL?.(encodedPath) : undefined;
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
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/g, "");
}

function MermaidDiagram({ code }: { code: string }): JSX.Element {
  const reactID = useId();
  const diagramID = useMemo(() => `wuu-mermaid-${reactID.replace(/[^a-zA-Z0-9_-]/g, "")}-${hashString(code)}`, [code, reactID]);
  const [state, setState] = useState<MermaidState>({ status: "rendering" });

  useEffect(() => {
    let cancelled = false;
    setState({ status: "rendering" });

    void (async () => {
      try {
        const mermaid = (await import("mermaid")).default;
        mermaid.initialize({
          startOnLoad: false,
          securityLevel: "strict",
          theme: "base",
          themeVariables: {
            background: "#ffffff",
            primaryColor: "#eef2f0",
            primaryTextColor: "#202427",
            primaryBorderColor: "#ccd6d0",
            lineColor: "#6f7478",
            fontFamily: "Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, Segoe UI, sans-serif"
          }
        });
        const result = await mermaid.render(diagramID, code);
        if (!cancelled) {
          setState({ status: "rendered", svg: result.svg });
        }
      } catch (error) {
        if (!cancelled) {
          setState({ status: "error", message: error instanceof Error ? error.message : "无法渲染 Mermaid 图" });
        }
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [code, diagramID]);

  if (state.status === "rendered") {
    return <div className="rich-mermaid" dangerouslySetInnerHTML={{ __html: state.svg }} />;
  }
  if (state.status === "error") {
    return (
      <div className="rich-mermaid rich-mermaid-error">
        <span>{state.message}</span>
        <pre>
          <code>{code}</code>
        </pre>
      </div>
    );
  }
  return <div className="rich-mermaid rich-mermaid-loading">正在渲染图表</div>;
}

function hashString(value: string): string {
  let hash = 0;
  for (let index = 0; index < value.length; index += 1) {
    hash = (hash * 31 + value.charCodeAt(index)) >>> 0;
  }
  return hash.toString(36);
}
