import * as monaco from "monaco-editor";
import CssWorker from "monaco-editor/esm/vs/language/css/css.worker?worker";
import EditorWorker from "monaco-editor/esm/vs/editor/editor.worker?worker";
import HtmlWorker from "monaco-editor/esm/vs/language/html/html.worker?worker";
import JsonWorker from "monaco-editor/esm/vs/language/json/json.worker?worker";
import TsWorker from "monaco-editor/esm/vs/language/typescript/ts.worker?worker";
import { useEffect, useMemo, useRef } from "react";

declare global {
  interface Window {
    MonacoEnvironment?: {
      getWorker: (_workerId: string, label: string) => Worker;
    };
  }
}

type MonacoLanguage =
  | "css"
  | "go"
  | "html"
  | "javascript"
  | "json"
  | "markdown"
  | "plaintext"
  | "python"
  | "rust"
  | "shell"
  | "sql"
  | "typescript"
  | "xml"
  | "yaml";

export function WorkspaceMonacoEditor({
  path,
  text,
  readOnly = false,
  onChange,
  onSave,
}: {
  path: string;
  text: string;
  readOnly?: boolean;
  onChange?: (value: string) => void;
  onSave?: () => void;
}): JSX.Element {
  const hostRef = useRef<HTMLDivElement>(null);
  const editorRef = useRef<monaco.editor.IStandaloneCodeEditor | null>(null);
  const modelRef = useRef<monaco.editor.ITextModel | null>(null);
  const onChangeRef = useRef(onChange);
  const onSaveRef = useRef(onSave);
  const language = useMemo(() => monacoLanguageForPath(path), [path]);

  useEffect(() => {
    onChangeRef.current = onChange;
    onSaveRef.current = onSave;
  }, [onChange, onSave]);

  useEffect(() => {
    installMonacoWorkers();
  }, []);

  useEffect(() => {
    const host = hostRef.current;
    if (!host) {
      return undefined;
    }

    const model = monaco.editor.createModel(
      text,
      language,
      monaco.Uri.parse(`wuu-workspace:///${encodeWorkspacePath(path)}`),
    );
    const editor = monaco.editor.create(host, {
      model,
      automaticLayout: true,
      contextmenu: false,
      detectIndentation: true,
      fontFamily: "\"SFMono-Regular\", Consolas, \"Liberation Mono\", monospace",
      fontSize: 12,
      lineHeight: 20,
      minimap: { enabled: false },
      readOnly,
      renderLineHighlight: "line",
      scrollBeyondLastLine: false,
      scrollbar: {
        alwaysConsumeMouseWheel: false,
      },
      tabSize: 2,
      theme: "wuu-workspace",
      wordWrap: "on",
    });

    const changeDisposable = editor.onDidChangeModelContent(() => {
      onChangeRef.current?.(model.getValue());
    });
    editor.addCommand(
      monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS,
      () => onSaveRef.current?.(),
    );

    modelRef.current = model;
    editorRef.current = editor;
    editor.focus();

    return () => {
      changeDisposable.dispose();
      editor.dispose();
      model.dispose();
      if (editorRef.current === editor) {
        editorRef.current = null;
      }
      if (modelRef.current === model) {
        modelRef.current = null;
      }
    };
  }, [language, path]);

  useEffect(() => {
    const model = modelRef.current;
    if (!model || model.getValue() === text) {
      return;
    }
    model.pushEditOperations(
      [],
      [
        {
          range: model.getFullModelRange(),
          text,
        },
      ],
      () => null,
    );
  }, [text]);

  useEffect(() => {
    editorRef.current?.updateOptions({ readOnly });
  }, [readOnly]);

  return (
    <div
      aria-label={`${path} 文件编辑器`}
      className="workspace-monaco-editor"
      data-language={language}
      data-path={path}
      ref={hostRef}
    />
  );
}

function installMonacoWorkers(): void {
  if (typeof window === "undefined" || window.MonacoEnvironment) {
    return;
  }

  window.MonacoEnvironment = {
    getWorker: (_workerId: string, label: string) => {
      if (label === "json") {
        return new JsonWorker();
      }
      if (label === "css" || label === "scss" || label === "less") {
        return new CssWorker();
      }
      if (label === "html" || label === "handlebars" || label === "razor") {
        return new HtmlWorker();
      }
      if (label === "typescript" || label === "javascript") {
        return new TsWorker();
      }
      return new EditorWorker();
    },
  };
}

function encodeWorkspacePath(path: string): string {
  return path
    .split("/")
    .filter(Boolean)
    .map((segment) => encodeURIComponent(segment))
    .join("/");
}

export function monacoLanguageForPath(path: string): MonacoLanguage {
  const basename = path.split(/[\\/]/).filter(Boolean).at(-1) ?? path;
  const extension = basename.includes(".") ? basename.split(".").at(-1)?.toLowerCase() : undefined;

  if (extension === "ts" || extension === "tsx") {
    return "typescript";
  }
  if (extension === "js" || extension === "jsx" || extension === "mjs" || extension === "cjs") {
    return "javascript";
  }
  if (extension === "json" || extension === "jsonc") {
    return "json";
  }
  if (extension === "md" || extension === "mdx") {
    return "markdown";
  }
  if (extension === "yaml" || extension === "yml") {
    return "yaml";
  }
  if (extension === "css" || extension === "scss" || extension === "less") {
    return "css";
  }
  if (extension === "html" || extension === "htm") {
    return "html";
  }
  if (extension === "xml" || extension === "svg") {
    return "xml";
  }
  if (basename === "go.mod" || basename === "go.sum" || extension === "go") {
    return "go";
  }
  if (extension === "py") {
    return "python";
  }
  if (extension === "rs") {
    return "rust";
  }
  if (extension === "sql") {
    return "sql";
  }
  if (extension === "sh" || extension === "bash" || extension === "zsh") {
    return "shell";
  }
  return "plaintext";
}

monaco.editor.defineTheme("wuu-workspace", {
  base: "vs",
  inherit: true,
  rules: [],
  colors: {
    "editor.background": "#ffffff00",
    "editor.foreground": "#24282d",
    "editor.lineHighlightBackground": "#24282d0a",
    "editorLineNumber.foreground": "#8b939b",
    "editorLineNumber.activeForeground": "#24282d",
    "editor.selectionBackground": "#ef5b1838",
    "editorCursor.foreground": "#ef5b18",
  },
});
