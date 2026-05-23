import { useEffect, useId, useMemo, useState } from "react";

type RichContentProps = {
  text?: string;
  cwd?: string;
};

type RichBlock =
  | { kind: "paragraph"; text: string }
  | { kind: "code"; language: string; code: string }
  | { kind: "mermaid"; code: string };

type MermaidState =
  | { status: "rendering" }
  | { status: "rendered"; svg: string }
  | { status: "error"; message: string };

const IMAGE_MARKDOWN_PATTERN = /!\[([^\]\n]*)\]\(([^)\n]+)\)/g;
const IMAGE_FILE_PATTERN = /\.(apng|avif|gif|jpe?g|png|svg|webp)(?:[?#].*)?$/i;

export function RichContent({ text = "", cwd }: RichContentProps): JSX.Element {
  const blocks = useMemo(() => parseRichBlocks(text), [text]);

  return (
    <div className="rich-content">
      {blocks.map((block, index) => {
        if (block.kind === "mermaid") {
          return <MermaidDiagram key={`${index}-mermaid`} code={block.code} />;
        }
        if (block.kind === "code") {
          return (
            <pre key={`${index}-code`} className="rich-code">
              <code>{block.code}</code>
            </pre>
          );
        }
        return (
          <p key={`${index}-paragraph`} className="rich-paragraph">
            {renderInlineContent(block.text, cwd, index)}
          </p>
        );
      })}
    </div>
  );
}

function parseRichBlocks(text: string): RichBlock[] {
  const blocks: RichBlock[] = [];
  const fencePattern = /```([^\n`]*)\n([\s\S]*?)```/g;
  let cursor = 0;
  let match: RegExpExecArray | null;

  while ((match = fencePattern.exec(text))) {
    pushParagraphBlocks(blocks, text.slice(cursor, match.index));
    const language = match[1].trim().toLowerCase();
    const code = match[2].replace(/\n$/, "");
    blocks.push(language === "mermaid" ? { kind: "mermaid", code } : { kind: "code", language, code });
    cursor = match.index + match[0].length;
  }

  pushParagraphBlocks(blocks, text.slice(cursor));
  return blocks.length > 0 ? blocks : [{ kind: "paragraph", text }];
}

function pushParagraphBlocks(blocks: RichBlock[], text: string): void {
  for (const paragraph of text.split(/\n{2,}/)) {
    const content = paragraph.replace(/^\n+|\n+$/g, "");
    if (content.trim()) {
      blocks.push({ kind: "paragraph", text: content });
    }
  }
}

function renderInlineContent(text: string, cwd: string | undefined, blockIndex: number): Array<JSX.Element | string> {
  const output: Array<JSX.Element | string> = [];
  let cursor = 0;
  let match: RegExpExecArray | null;
  IMAGE_MARKDOWN_PATTERN.lastIndex = 0;

  while ((match = IMAGE_MARKDOWN_PATTERN.exec(text))) {
    if (match.index > cursor) {
      output.push(text.slice(cursor, match.index));
    }
    const alt = match[1].trim();
    const source = resolveImageSource(match[2], cwd);
    output.push(<img key={`${blockIndex}-${match.index}`} className="rich-image" src={source} alt={alt} loading="lazy" />);
    cursor = match.index + match[0].length;
  }

  if (cursor < text.length) {
    output.push(text.slice(cursor));
  }
  return output;
}

function resolveImageSource(rawSource: string, cwd: string | undefined): string {
  const source = imageTarget(rawSource);
  if (isWebImageSource(source)) {
    return source;
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

function imageTarget(rawSource: string): string {
  let source = rawSource.trim();
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
  return `wuu-file://local/${base64URL(filePath)}`;
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
