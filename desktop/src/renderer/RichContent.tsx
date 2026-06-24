import { Children, cloneElement, isValidElement, memo, useEffect, useId, useMemo, useState, type KeyboardEvent as ReactKeyboardEvent, type ReactNode } from "react";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { useImagePreview } from "./ImagePreview";
import { MessageCopyButton } from "./MessageActions";

type RichContentProps = {
  text?: string;
  cwd?: string;
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

type MermaidState =
  | { status: "rendering" }
  | { status: "rendered"; svg: string }
  | { status: "error"; message: string };

const IMAGE_MARKDOWN_PATTERN = /!\[([^\]\n]*)\]\(([^)\n]+)\)/g;
const IMAGE_FILE_PATTERN = /\.(apng|avif|gif|jpe?g|png|svg|webp)(?:[?#].*)?$/i;

export const RichContent = memo(function RichContent({ text = "", cwd }: RichContentProps): JSX.Element {
  return (
    <div className="rich-content">
      <MarkdownContent text={text} cwd={cwd} />
    </div>
  );
});

function MarkdownContentView({
  text,
  cwd,
  renderText,
  renderMermaid = true
}: {
  text: string;
  cwd?: string;
  renderText?: RichTextRenderer;
  renderMermaid?: boolean;
}): JSX.Element {
  const components = useMemo(() => markdownComponents(cwd, renderText, renderMermaid), [cwd, renderMermaid, renderText]);
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
  renderText
}: {
  block: RichBlock;
  blockKey: string;
  cwd?: string;
  renderText?: RichTextRenderer;
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
      {renderInlineContent(block.text, cwd, blockKey, renderText)}
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
  renderText?: RichTextRenderer
): Array<JSX.Element | string> {
  const output: Array<JSX.Element | string> = [];
  const pushText = (value: string, key: string): void => {
    if (!value) {
      return;
    }
    output.push(...(renderText ? renderText(value, key) : [value]));
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
  renderMermaid: boolean
): Components {
  return {
    p({ children }) {
      return <p className="rich-paragraph">{renderMarkdownText(children, renderText, "p")}</p>;
    },
    h1({ children }) {
      return <p className="rich-heading">{renderMarkdownText(children, renderText, "h1")}</p>;
    },
    h2({ children }) {
      return <p className="rich-heading">{renderMarkdownText(children, renderText, "h2")}</p>;
    },
    h3({ children }) {
      return <p className="rich-heading">{renderMarkdownText(children, renderText, "h3")}</p>;
    },
    h4({ children }) {
      return <p className="rich-heading">{renderMarkdownText(children, renderText, "h4")}</p>;
    },
    h5({ children }) {
      return <p className="rich-heading">{renderMarkdownText(children, renderText, "h5")}</p>;
    },
    h6({ children }) {
      return <p className="rich-heading">{renderMarkdownText(children, renderText, "h6")}</p>;
    },
    a({ href, title, children }) {
      const safeHref = safeMarkdownHref(href);
      if (!safeHref) {
        return <span>{renderMarkdownText(children, renderText, "a-disabled")}</span>;
      }
      return (
        <a className="rich-link" href={safeHref} title={title} target="_blank" rel="noreferrer">
          {renderMarkdownText(children, renderText, "a")}
        </a>
      );
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
      return <code className={className}>{renderMarkdownText(children, renderText, "code")}</code>;
    },
    li({ children }) {
      return <li>{renderMarkdownText(children, renderText, "li")}</li>;
    },
    table({ children }) {
      return (
        <div className="rich-table-wrap">
          <table>{children}</table>
        </div>
      );
    },
    th({ children }) {
      return <th>{renderMarkdownText(children, renderText, "th")}</th>;
    },
    td({ children }) {
      return <td>{renderMarkdownText(children, renderText, "td")}</td>;
    },
    blockquote({ children }) {
      return <blockquote className="rich-blockquote">{renderMarkdownText(children, renderText, "blockquote")}</blockquote>;
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
  return (
    <div className="rich-code-block">
      <pre className="rich-code">
        {children}
      </pre>
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
  );
}

function renderMarkdownText(
  children: ReactNode,
  renderText: RichTextRenderer | undefined,
  keyPrefix: string
): ReactNode {
  if (!renderText) {
    return children;
  }
  return Children.toArray(children).flatMap((child, index): ReactNode[] => {
    const childKey = `${keyPrefix}-${index}`;
    if (typeof child === "string" || typeof child === "number") {
      return renderText(String(child), childKey);
    }
    if (!isValidElement<{ children?: ReactNode }>(child) || child.props.children === undefined) {
      return [child];
    }
    return [
      cloneElement(child, {
        children: renderMarkdownText(child.props.children, renderText, childKey)
      })
    ];
  });
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
