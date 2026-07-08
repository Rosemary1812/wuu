import { defaultKeymap, history, historyKeymap } from "@codemirror/commands";
import { css } from "@codemirror/lang-css";
import { go } from "@codemirror/lang-go";
import { html } from "@codemirror/lang-html";
import { javascript } from "@codemirror/lang-javascript";
import { json } from "@codemirror/lang-json";
import { markdown } from "@codemirror/lang-markdown";
import { python } from "@codemirror/lang-python";
import { rust } from "@codemirror/lang-rust";
import { sql } from "@codemirror/lang-sql";
import { xml } from "@codemirror/lang-xml";
import { yaml } from "@codemirror/lang-yaml";
import {
  bracketMatching,
  defaultHighlightStyle,
  foldGutter,
  indentOnInput,
  indentUnit,
  syntaxHighlighting,
} from "@codemirror/language";
import { searchKeymap, highlightSelectionMatches } from "@codemirror/search";
import { EditorState, type Extension } from "@codemirror/state";
import {
  drawSelection,
  dropCursor,
  EditorView,
  highlightActiveLine,
  highlightActiveLineGutter,
  highlightSpecialChars,
  keymap,
  lineNumbers,
} from "@codemirror/view";
import { useEffect, useMemo, useRef } from "react";

export function WorkspaceCodeEditor({
  path,
  text,
}: {
  path: string;
  text: string;
}): JSX.Element {
  const hostRef = useRef<HTMLDivElement>(null);
  const extensions = useMemo(() => workspaceEditorExtensions(path), [path]);

  useEffect(() => {
    const host = hostRef.current;
    if (!host) {
      return undefined;
    }

    const view = new EditorView({
      parent: host,
      state: EditorState.create({
        doc: text,
        extensions,
      }),
    });

    return () => {
      view.destroy();
    };
  }, [extensions, text]);

  return (
    <div
      className="workspace-code-editor"
      data-path={path}
      ref={hostRef}
    />
  );
}

function workspaceEditorExtensions(path: string): Extension[] {
  return [
    lineNumbers(),
    foldGutter(),
    highlightActiveLineGutter(),
    highlightSpecialChars(),
    history(),
    drawSelection(),
    dropCursor(),
    EditorState.allowMultipleSelections.of(true),
    indentOnInput(),
    bracketMatching(),
    highlightActiveLine(),
    highlightSelectionMatches(),
    EditorState.readOnly.of(true),
    EditorView.editable.of(false),
    EditorView.lineWrapping,
    EditorState.tabSize.of(2),
    indentUnit.of("  "),
    EditorView.contentAttributes.of({
      "aria-label": `${path} 文件内容`,
      "aria-readonly": "true",
    }),
    keymap.of([...defaultKeymap, ...historyKeymap, ...searchKeymap]),
    workspaceLanguageForPath(path),
    syntaxHighlighting(defaultHighlightStyle, { fallback: true }),
    workspaceEditorTheme,
  ];
}

function workspaceLanguageForPath(path: string): Extension {
  const basename = path.split(/[\\/]/).filter(Boolean).at(-1) ?? path;
  const extension = basename.includes(".") ? basename.split(".").at(-1)?.toLowerCase() : undefined;

  if (basename === "go.mod" || basename === "go.sum" || extension === "go") {
    return go();
  }
  if (extension === "md" || extension === "mdx") {
    return markdown();
  }
  if (extension === "json" || extension === "jsonc") {
    return json();
  }
  if (extension === "yaml" || extension === "yml") {
    return yaml();
  }
  if (extension === "ts" || extension === "tsx") {
    return javascript({ typescript: true, jsx: extension === "tsx" });
  }
  if (extension === "js" || extension === "jsx" || extension === "mjs" || extension === "cjs") {
    return javascript({ jsx: extension === "jsx" });
  }
  if (extension === "css" || extension === "scss" || extension === "less") {
    return css();
  }
  if (extension === "html" || extension === "htm") {
    return html();
  }
  if (extension === "xml" || extension === "svg") {
    return xml();
  }
  if (extension === "py") {
    return python();
  }
  if (extension === "rs") {
    return rust();
  }
  if (extension === "sql") {
    return sql();
  }
  return [];
}

const workspaceEditorTheme = EditorView.theme({
  "&": {
    height: "100%",
    backgroundColor: "var(--paper)",
    color: "var(--ink)",
    fontSize: "12px",
  },
  "&.cm-focused": {
    outline: "none",
  },
  ".cm-scroller": {
    fontFamily: "\"SFMono-Regular\", Consolas, \"Liberation Mono\", monospace",
    lineHeight: "1.6",
  },
  ".cm-content": {
    minHeight: "100%",
    padding: "18px 22px 40px 6px",
  },
  ".cm-line": {
    padding: "0 0 0 4px",
  },
  ".cm-gutters": {
    backgroundColor: "var(--paper)",
    borderRight: "1px solid var(--surface-2)",
    color: "var(--ink-muted)",
    paddingLeft: "8px",
  },
  ".cm-activeLine": {
    backgroundColor: "color-mix(in srgb, var(--ink) 5%, transparent)",
  },
  ".cm-activeLineGutter": {
    backgroundColor: "color-mix(in srgb, var(--ink) 5%, transparent)",
    color: "var(--ink)",
  },
  ".cm-selectionBackground, &.cm-focused .cm-selectionBackground": {
    backgroundColor: "color-mix(in srgb, var(--accent-warm) 22%, transparent)",
  },
  ".cm-cursor": {
    borderLeftColor: "var(--accent-warm)",
  },
  ".cm-searchMatch": {
    backgroundColor: "color-mix(in srgb, var(--warning) 28%, transparent)",
    outline: "1px solid color-mix(in srgb, var(--warning) 34%, transparent)",
  },
  ".cm-searchMatch.cm-searchMatch-selected": {
    backgroundColor: "color-mix(in srgb, var(--accent-warm) 24%, transparent)",
  },
});
