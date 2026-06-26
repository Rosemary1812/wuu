import { FileCode } from "lucide-react";
import { type ReactElement } from "react";

/**
 * Recognized source-file extensions. The detector only matches a relative
 * path when it ends with one of these, which keeps the match conservative
 * enough that common phrases like "foo.com" or "x/y" stay as plain text.
 *
 * Keep the list scoped to files a coding agent would actually cite in a
 * reply. If a caller needs more, they can add the extension here.
 */
const FILE_EXTENSIONS: ReadonlySet<string> = new Set([
  // Code
  "ts", "tsx", "mts", "cts",
  "js", "jsx", "mjs", "cjs",
  "go", "py", "rs", "swift", "kt", "kts",
  "java", "c", "h", "cpp", "cc", "cxx", "hpp", "hxx",
  "cs", "rb", "php",
  "sh", "bash", "zsh", "fish", "ps1",
  // Config / data
  "json", "jsonc", "json5",
  "yml", "yaml", "toml", "ini", "env", "conf", "cfg",
  // Docs / markup
  "md", "mdx", "markdown",
  "html", "htm", "xml", "svg",
  "css", "scss", "sass", "less",
  // Misc source-ish
  "sql", "graphql", "gql", "proto",
  "lua", "vim", "vue", "svelte",
  "tcl", "zig", "nim", "cr",
  "txt", "log", "csv", "tsv",
  "dockerfile", "makefile",
  "gitignore", "gitattributes", "editorconfig", "envrc",
  "mod", "sum", "lock"
]);

const FILE_EXTENSION_PATTERN = /\.[A-Za-z][A-Za-z0-9]{0,9}$/;

export type FilePathMatch = {
  /** Inclusive start offset in the input string. */
  start: number;
  /** Exclusive end offset in the input string. */
  end: number;
  /** Human-readable path text for the chip. */
  display: string;
  /** Absolute path the file viewer should read. */
  absolutePath: string;
};

export type FilePathDetectorOptions = {
  /** Working directory used to resolve relative paths and bound absolute paths. */
  cwd?: string;
};

/**
 * Scan `text` for file paths under the current working directory.
 *
 * Recognizes three shapes:
 *   1. Absolute paths starting with `cwd + '/'`.
 *   2. Home-relative paths starting with `~/`.
 *   3. Relative paths (with optional `./` or `../` prefix, or single
 *      dotfiles like `.eslintrc.json`) ending in a recognized extension.
 *
 * URLs (`http://`, `https://`, `file://`, `wuu-file://`) are skipped so the
 * detector does not eat paths inside URLs. The caller is expected to skip
 * detection inside `<a>` and `<code>` elements where another visual
 * treatment already applies.
 */
export function detectFilePaths(
  text: string,
  options: FilePathDetectorOptions = {}
): FilePathMatch[] {
  const cwd = normalizeCwd(options.cwd);
  if (!text || !cwd) {
    return [];
  }

  const matches: FilePathMatch[] = [];
  let cursor = 0;

  while (cursor < text.length) {
    if (isUrlStart(text, cursor)) {
      cursor = scanUrlEnd(text, cursor);
      continue;
    }

    const abs = matchAbsoluteUnderCwd(text, cursor, cwd);
    if (abs) {
      matches.push(abs);
      cursor = abs.end;
      continue;
    }

    const home = matchHomeRelative(text, cursor);
    if (home) {
      matches.push(home);
      cursor = home.end;
      continue;
    }

    const rel = matchRelativePath(text, cursor, cwd);
    if (rel) {
      matches.push(rel);
      cursor = rel.end;
      continue;
    }

    cursor += 1;
  }

  return matches;
}

function matchAbsoluteUnderCwd(
  text: string,
  cursor: number,
  cwd: string
): FilePathMatch | undefined {
  const prefix = `${cwd}/`;
  if (!text.startsWith(prefix, cursor)) {
    return undefined;
  }
  const end = scanPathEnd(text, cursor + prefix.length);
  if (end <= cursor + prefix.length) {
    return undefined;
  }
  const absolutePath = text.slice(cursor, end);
  return {
    start: cursor,
    end,
    display: toRelativeDisplay(absolutePath, cwd),
    absolutePath
  };
}

function matchHomeRelative(
  text: string,
  cursor: number
): FilePathMatch | undefined {
  if (text.charCodeAt(cursor) !== 126 /* ~ */) {
    return undefined;
  }
  if (text.charCodeAt(cursor + 1) !== 47 /* / */) {
    return undefined;
  }
  const end = scanPathEnd(text, cursor + 2);
  if (end <= cursor + 2) {
    return undefined;
  }
  const matchedText = text.slice(cursor, end);
  return {
    start: cursor,
    end,
    display: matchedText,
    absolutePath: matchedText
  };
}

function matchRelativePath(
  text: string,
  cursor: number,
  cwd: string
): FilePathMatch | undefined {
  // Must start at a word boundary so we don't claim a path out of the middle
  // of a longer token.
  if (cursor > 0) {
    const prev = text.charCodeAt(cursor - 1);
    if (isPathCharCode(prev)) {
      return undefined;
    }
    if (prev === 47 /* / */) {
      return undefined;
    }
  }

  let scanStart = cursor;

  // Optional ./ or ../ prefix
  if (
    text.charCodeAt(cursor) === 46 /* . */ &&
    text.charCodeAt(cursor + 1) === 47 /* / */
  ) {
    scanStart = cursor + 2;
  } else if (
    text.charCodeAt(cursor) === 46 &&
    text.charCodeAt(cursor + 1) === 46 &&
    text.charCodeAt(cursor + 2) === 47
  ) {
    scanStart = cursor + 3;
  } else if (!isPathCharCode(text.charCodeAt(cursor))) {
    return undefined;
  }

  const pathEnd = scanRelativePathEnd(text, scanStart);
  if (pathEnd <= scanStart) {
    return undefined;
  }

  const matchedText = text.slice(cursor, pathEnd);
  if (!looksLikeRelativeFilePath(matchedText)) {
    return undefined;
  }

  return {
    start: cursor,
    end: pathEnd,
    display: matchedText,
    absolutePath: resolveRelativePath(cwd, matchedText)
  };
}

function scanRelativePathEnd(text: string, from: number): number {
  let cursor = from;
  while (cursor < text.length) {
    const ch = text.charCodeAt(cursor);
    if (ch === 47 /* / */ || isPathCharCode(ch)) {
      cursor += 1;
      continue;
    }
    break;
  }
  // Trim trailing slashes so "/foo/bar/" does not include the trailing "/".
  let end = cursor;
  while (end > from && text.charCodeAt(end - 1) === 47 /* / */) {
    end -= 1;
  }
  return end;
}

function scanPathEnd(text: string, from: number): number {
  let end = from;
  while (end < text.length) {
    const ch = text.charCodeAt(end);
    if (ch === 47 /* / */ || isPathCharCode(ch)) {
      end += 1;
      continue;
    }
    break;
  }
  while (end > from && text.charCodeAt(end - 1) === 47 /* / */) {
    end -= 1;
  }
  return end;
}

function isPathCharCode(ch: number): boolean {
  if (ch === 95 /* _ */ || ch === 45 /* - */ || ch === 46 /* . */) {
    return true;
  }
  if (ch >= 48 && ch <= 57 /* 0-9 */) return true;
  if (ch >= 65 && ch <= 90 /* A-Z */) return true;
  if (ch >= 97 && ch <= 122 /* a-z */) return true;
  return false;
}

function isUrlStart(text: string, cursor: number): boolean {
  // Anchor at the start of a token so we don't mistake "abc:def" mid-word
  // for a URL.
  if (cursor > 0) {
    const prev = text.charCodeAt(cursor - 1);
    if (isPathCharCode(prev) || prev === 47 /* / */) {
      return false;
    }
  }
  let schemeEnd = cursor;
  while (schemeEnd < text.length) {
    const ch = text.charCodeAt(schemeEnd);
    if (schemeEnd === cursor) {
      if (!((ch >= 65 && ch <= 90) || (ch >= 97 && ch <= 122))) {
        return false;
      }
    } else if (
      (ch >= 65 && ch <= 90) ||
      (ch >= 97 && ch <= 122) ||
      (ch >= 48 && ch <= 57) ||
      ch === 43 /* + */ ||
      ch === 45 /* - */ ||
      ch === 46 /* . */
    ) {
      // valid scheme char
    } else if (ch === 58 /* : */) {
      return (
        text.charCodeAt(schemeEnd + 1) === 47 &&
        text.charCodeAt(schemeEnd + 2) === 47
      );
    } else {
      return false;
    }
    schemeEnd += 1;
  }
  return false;
}

function scanUrlEnd(text: string, from: number): number {
  let end = from;
  while (end < text.length) {
    const ch = text.charCodeAt(end);
    if (
      ch === 32 /* space */ ||
      ch === 9 /* tab */ ||
      ch === 10 /* \n */ ||
      ch === 13 /* \r */ ||
      ch === 41 /* ) */ ||
      ch === 93 /* ] */ ||
      ch === 125 /* } */ ||
      ch === 39 /* ' */ ||
      ch === 34 /* " */ ||
      ch === 96 /* ` */ ||
      ch === 60 /* < */
    ) {
      break;
    }
    end += 1;
  }
  return end;
}

function looksLikeRelativeFilePath(text: string): boolean {
  const match = FILE_EXTENSION_PATTERN.exec(text);
  if (!match) {
    return false;
  }
  const ext = match[0].slice(1).toLowerCase();
  return FILE_EXTENSIONS.has(ext);
}

function normalizeCwd(cwd: string | undefined): string | undefined {
  if (!cwd) return undefined;
  return cwd.replace(/\/+$/, "");
}

function toRelativeDisplay(absolutePath: string, cwd: string): string {
  const prefix = `${cwd}/`;
  if (absolutePath.startsWith(prefix)) {
    return absolutePath.slice(prefix.length);
  }
  return absolutePath;
}

function resolveRelativePath(cwd: string, relativePath: string): string {
  const parts = `${cwd}/${relativePath}`.split("/");
  const stack: string[] = [];
  for (const part of parts) {
    if (!part || part === ".") continue;
    if (part === "..") {
      stack.pop();
      continue;
    }
    stack.push(part);
  }
  return `/${stack.join("/")}`;
}

export function RichFileChip({
  absolutePath,
  display,
  onActivate
}: {
  absolutePath: string;
  display: string;
  onActivate: (path: string) => void;
}): ReactElement {
  return (
    <button
      type="button"
      className="rich-file-chip"
      data-file-path={absolutePath}
      title={absolutePath}
      onClick={() => onActivate(absolutePath)}
    >
      <FileCode aria-hidden="true" className="rich-file-chip-icon" />
      <span className="rich-file-chip-text">{display}</span>
    </button>
  );
}

/**
 * Replace detected file-path substrings in `text` with `RichFileChip`
 * elements. Returns the original text unchanged when `cwd` is undefined or
 * no paths are detected. Callers are responsible for skipping this inside
 * `<code>` and `<a>` subtrees where another treatment already applies.
 */
export function splitTextByFilePaths(
  text: string,
  cwd: string | undefined,
  onOpenFile: (path: string) => void,
  keyPrefix: string
): Array<string | ReactElement> {
  const matches = detectFilePaths(text, { cwd });
  if (matches.length === 0) {
    return [text];
  }
  const output: Array<string | ReactElement> = [];
  let cursor = 0;
  for (const match of matches) {
    if (match.start > cursor) {
      output.push(text.slice(cursor, match.start));
    }
    output.push(
      <RichFileChip
        key={`${keyPrefix}-file-${match.start}`}
        absolutePath={match.absolutePath}
        display={match.display}
        onActivate={onOpenFile}
      />
    );
    cursor = match.end;
  }
  if (cursor < text.length) {
    output.push(text.slice(cursor));
  }
  return output;
}
