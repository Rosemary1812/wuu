import {
  MDXEditor,
  type MDXEditorMethods,
  codeBlockPlugin,
  headingsPlugin,
  listsPlugin,
  markdownShortcutPlugin,
  quotePlugin,
  tablePlugin,
  thematicBreakPlugin,
} from "@mdxeditor/editor";
import "@mdxeditor/editor/style.css";
import { useEffect, useMemo, useRef, type KeyboardEvent } from "react";

export function WorkspaceMarkdownEditor({
  markdown,
  readOnly = false,
  onChange,
  onSave,
}: {
  path: string;
  markdown: string;
  readOnly?: boolean;
  onChange?: (value: string) => void;
  onSave?: () => void;
}): JSX.Element {
  const editorRef = useRef<MDXEditorMethods>(null);
  const lastMarkdownRef = useRef(markdown);
  const plugins = useMemo(
    () => [
      headingsPlugin(),
      listsPlugin(),
      quotePlugin(),
      thematicBreakPlugin(),
      tablePlugin(),
      codeBlockPlugin(),
      markdownShortcutPlugin(),
    ],
    [],
  );

  useEffect(() => {
    if (lastMarkdownRef.current === markdown) {
      return;
    }
    lastMarkdownRef.current = markdown;
    editorRef.current?.setMarkdown(markdown);
  }, [markdown]);

  const handleChange = (nextMarkdown: string, initialMarkdownNormalize: boolean): void => {
    lastMarkdownRef.current = nextMarkdown;
    if (!initialMarkdownNormalize) {
      onChange?.(nextMarkdown);
    }
  };

  const handleKeyDownCapture = (event: KeyboardEvent<HTMLDivElement>): void => {
    if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "s") {
      event.preventDefault();
      onSave?.();
    }
  };

  return (
    <div className="workspace-markdown-wysiwyg-frame" onKeyDownCapture={handleKeyDownCapture}>
      <MDXEditor
        ref={editorRef}
        markdown={markdown}
        readOnly={readOnly}
        trim={false}
        onChange={handleChange}
        plugins={plugins}
        className="workspace-markdown-wysiwyg"
        contentEditableClassName="workspace-markdown-wysiwyg-content"
        placeholder=""
        spellCheck
      />
    </div>
  );
}
