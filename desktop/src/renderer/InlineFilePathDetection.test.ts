import { describe, expect, it, vi } from "vitest";
import {
  detectFilePaths,
  RichFileChip,
  splitTextByFilePaths
} from "./InlineFilePathDetection";

describe("detectFilePaths", () => {
  it("returns no matches when cwd is undefined", () => {
    expect(detectFilePaths("see src/foo.ts and package.json here")).toEqual([]);
  });

  it("returns no matches for an empty input", () => {
    expect(detectFilePaths("", { cwd: "/repo" })).toEqual([]);
  });

  it("matches an absolute path under cwd", () => {
    const matches = detectFilePaths("See /repo/src/foo.ts for details", { cwd: "/repo" });
    expect(matches).toHaveLength(1);
    expect(matches[0]).toMatchObject({
      display: "src/foo.ts",
      absolutePath: "/repo/src/foo.ts"
    });
    // "See " is 4 chars; "/repo/src/foo.ts" is 16 chars. End is exclusive.
    expect(matches[0]?.start).toBe(4);
    expect(matches[0]?.end).toBe(20);
  });

  it("does not match an absolute path outside cwd", () => {
    expect(detectFilePaths("see /etc/passwd", { cwd: "/repo" })).toEqual([]);
  });

  it("matches a home-relative path", () => {
    const matches = detectFilePaths("edit ~/Desktop/notes.md", { cwd: "/Users/me/repo" });
    expect(matches).toHaveLength(1);
    expect(matches[0]).toMatchObject({
      display: "~/Desktop/notes.md",
      absolutePath: "~/Desktop/notes.md"
    });
  });

  it("matches a relative path containing a slash", () => {
    const matches = detectFilePaths("edit src/foo.ts now", { cwd: "/repo" });
    expect(matches).toHaveLength(1);
    expect(matches[0]).toMatchObject({
      display: "src/foo.ts",
      absolutePath: "/repo/src/foo.ts"
    });
  });

  it("matches a single-component file with a recognized extension", () => {
    const matches = detectFilePaths("edit package.json here", { cwd: "/repo" });
    expect(matches).toHaveLength(1);
    expect(matches[0]).toMatchObject({
      display: "package.json",
      absolutePath: "/repo/package.json"
    });
  });

  it("does not match a single-component word with an unrecognized extension", () => {
    expect(detectFilePaths("see example.com here", { cwd: "/repo" })).toEqual([]);
  });

  it("does not match a URL even if its path ends in a recognized extension", () => {
    expect(detectFilePaths("see https://example.com/foo.ts", { cwd: "/repo" })).toEqual([]);
  });

  it("matches paths that surround a URL", () => {
    const text = "check src/foo.ts at https://example.com for context";
    const matches = detectFilePaths(text, { cwd: "/repo" });
    expect(matches).toHaveLength(1);
    expect(matches[0]?.display).toBe("src/foo.ts");
    // The match must end before the URL starts.
    expect(matches[0]?.end).toBeLessThan(text.indexOf("https://"));
  });

  it("does not match a path terminated by sentence punctuation", () => {
    // The trailing period breaks the extension match, so this stays as plain text.
    expect(detectFilePaths("I edited foo.js.", { cwd: "/repo" })).toEqual([]);
  });

  it("matches a dotfile like .eslintrc.json", () => {
    const matches = detectFilePaths("see .eslintrc.json", { cwd: "/repo" });
    expect(matches).toHaveLength(1);
    expect(matches[0]).toMatchObject({
      display: ".eslintrc.json",
      absolutePath: "/repo/.eslintrc.json"
    });
  });

  it("matches multiple paths in a single text", () => {
    const matches = detectFilePaths("see src/foo.ts and bar/baz.go for context", { cwd: "/repo" });
    expect(matches).toHaveLength(2);
    expect(matches.map((m) => m.display)).toEqual(["src/foo.ts", "bar/baz.go"]);
  });

  it("matches ./ prefixed relative paths", () => {
    const matches = detectFilePaths("see ./foo.ts here", { cwd: "/repo" });
    expect(matches).toHaveLength(1);
    expect(matches[0]).toMatchObject({
      display: "./foo.ts",
      absolutePath: "/repo/foo.ts"
    });
  });

  it("matches ../ prefixed relative paths and resolves them above cwd", () => {
    const matches = detectFilePaths("see ../shared/util.ts here", { cwd: "/repo/app" });
    expect(matches).toHaveLength(1);
    expect(matches[0]).toMatchObject({
      display: "../shared/util.ts",
      absolutePath: "/repo/shared/util.ts"
    });
  });

  it("strips a trailing slash from cwd when matching", () => {
    const matches = detectFilePaths("see /repo/src/foo.ts", { cwd: "/repo/" });
    expect(matches).toHaveLength(1);
    expect(matches[0]?.display).toBe("src/foo.ts");
  });

  it("matches a syntactically valid path even when the directory name could be a typo", () => {
    // The detector is purely syntactic — "theasrc" is a valid directory
    // component, so we surface it as a chip rather than silently swallowing
    // the reference. The user sees the chip in the rendered message and can
    // correct obvious typos (e.g. "src/foo.ts") before sending the next reply.
    const matches = detectFilePaths("theasrc/foo.ts identifier", { cwd: "/repo" });
    expect(matches).toHaveLength(1);
    expect(matches[0]).toMatchObject({
      display: "theasrc/foo.ts",
      absolutePath: "/repo/theasrc/foo.ts"
    });
  });
});

describe("splitTextByFilePaths", () => {
  it("returns the original text unchanged when no paths match", () => {
    const onOpenFile = vi.fn();
    expect(splitTextByFilePaths("hello world", "/repo", onOpenFile, "k")).toEqual(["hello world"]);
  });

  it("interleaves text segments and a chip element when a path matches", () => {
    const onOpenFile = vi.fn();
    const segments = splitTextByFilePaths("see src/foo.ts here", "/repo", onOpenFile, "k");
    expect(segments).toHaveLength(3);
    expect(segments[0]).toBe("see ");
    expect(segments[2]).toBe(" here");
    // segments[1] is the RichFileChip React element — confirm by inspecting
    // the props passed to it (absolutePath/display/onActivate), not the
    // props of the inner <button> element.
    const chip = segments[1] as unknown as {
      props: { absolutePath: string; display: string; onActivate: (path: string) => void };
    };
    expect(chip.props.absolutePath).toBe("/repo/src/foo.ts");
    expect(chip.props.display).toBe("src/foo.ts");
    expect(chip.props.onActivate).toBe(onOpenFile);
  });

  it("returns a single string segment for a path with no recognized extension", () => {
    const onOpenFile = vi.fn();
    // "src/" has no recognized extension and no slash after "src", so no chip.
    expect(splitTextByFilePaths("open src/", "/repo", onOpenFile, "k")).toEqual(["open src/"]);
  });

  it("propagates the click through onActivate to the caller-supplied callback", () => {
    const onOpenFile = vi.fn();
    const segments = splitTextByFilePaths("see src/foo.ts", "/repo", onOpenFile, "k");
    const chip = segments[1] as unknown as {
      props: { absolutePath: string; onActivate: (path: string) => void };
    };
    chip.props.onActivate(chip.props.absolutePath);
    expect(onOpenFile).toHaveBeenCalledWith("/repo/src/foo.ts");
  });
});

describe("RichFileChip", () => {
  it("renders a <button> bound to the absolute path", () => {
    const onActivate = vi.fn();
    // Invoking the component as a function returns the inner <button> JSX
    // element directly, so `element.props` is the button's own props.
    const element = RichFileChip({
      absolutePath: "/repo/src/foo.ts",
      display: "src/foo.ts",
      onActivate
    });
    const props = element.props as unknown as Record<string, unknown>;
    expect(props["data-file-path"]).toBe("/repo/src/foo.ts");
    expect(props.title).toBe("/repo/src/foo.ts");
    expect(typeof props.onClick).toBe("function");
  });

  it("invokes onActivate with the absolute path when the button is clicked", () => {
    const onActivate = vi.fn();
    const element = RichFileChip({
      absolutePath: "/repo/src/foo.ts",
      display: "src/foo.ts",
      onActivate
    });
    const onClick = (element.props as unknown as { onClick: () => void }).onClick;
    onClick();
    expect(onActivate).toHaveBeenCalledWith("/repo/src/foo.ts");
  });
});
