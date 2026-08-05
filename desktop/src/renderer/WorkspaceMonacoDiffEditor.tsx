import * as monaco from "monaco-editor";
import { useEffect, useMemo, useRef } from "react";
import { currentAppliedTheme, observeAppliedTheme } from "./Theme";
import {
  installMonacoWorkers,
  monacoLanguageForPath,
  workspaceMonacoModelURI,
  workspaceMonacoTheme,
  workspaceScrollbarSize,
} from "./WorkspaceMonacoEditor";
import { useI18n } from "./i18n";

export function WorkspaceMonacoDiffEditor({
  path,
  originalText,
  modifiedText,
}: {
  path: string;
  originalText: string;
  modifiedText: string;
}): JSX.Element {
  const { t } = useI18n();
  const hostRef = useRef<HTMLDivElement>(null);
  const language = useMemo(() => monacoLanguageForPath(path), [path]);

  useEffect(() => {
    installMonacoWorkers();
  }, []);

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return undefined;

    const scrollbarSize = workspaceScrollbarSize(host);
    const resourceID = encodeURIComponent(path);
    const originalModel = monaco.editor.createModel(
      originalText,
      language,
      workspaceMonacoModelURI(`diff/original/${resourceID}`),
    );
    const modifiedModel = monaco.editor.createModel(
      modifiedText,
      language,
      workspaceMonacoModelURI(`diff/modified/${resourceID}`),
    );
    const editor = monaco.editor.createDiffEditor(host, {
      automaticLayout: true,
      contextmenu: false,
      diffCodeLens: false,
      diffWordWrap: "on",
      enableSplitViewResizing: true,
      fontFamily: "\"SFMono-Regular\", Consolas, \"Liberation Mono\", monospace",
      fontSize: 12,
      glyphMargin: false,
      hideUnchangedRegions: {
        enabled: true,
        contextLineCount: 3,
        minimumLineCount: 8,
        revealLineCount: 12,
      },
      lineHeight: 20,
      lineNumbersMinChars: 3,
      minimap: { enabled: false },
      originalEditable: false,
      readOnly: true,
      renderIndicators: true,
      renderMarginRevertIcon: false,
      renderOverviewRuler: false,
      renderSideBySide: true,
      scrollBeyondLastLine: false,
      scrollbar: {
        alwaysConsumeMouseWheel: false,
        horizontalScrollbarSize: scrollbarSize,
        horizontalSliderSize: Math.max(4, scrollbarSize - 2),
        useShadows: false,
        verticalScrollbarSize: scrollbarSize,
        verticalSliderSize: Math.max(4, scrollbarSize - 2),
      },
      theme: workspaceMonacoTheme(currentAppliedTheme()),
      useInlineViewWhenSpaceIsLimited: true,
    });
    editor.setModel({ original: originalModel, modified: modifiedModel });
    const stopObservingTheme = observeAppliedTheme((theme) => {
      monaco.editor.setTheme(workspaceMonacoTheme(theme));
    });

    return () => {
      stopObservingTheme();
      editor.dispose();
      originalModel.dispose();
      modifiedModel.dispose();
    };
  }, [language, modifiedText, originalText, path]);

  return (
    <div
      aria-label={t("workspaceReview.codeDiffFor", { path })}
      className="workspace-monaco-diff-editor"
      data-language={language}
      data-path={path}
      ref={hostRef}
    />
  );
}
