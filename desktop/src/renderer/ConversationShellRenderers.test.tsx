import { act, createRef } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Thread } from "../shared/protocol";
import {
  emptyComposerDraft,
  initialState,
} from "./AppState";

vi.mock("./ConversationSplitPane", () => ({
  ConversationSplitPane: ({
    onOpenFile,
    thread,
  }: {
    onOpenFile?: (path: string) => void;
    thread: Thread;
  }) => (
    <button
      type="button"
      data-testid={`open-file-${thread.id}`}
      onClick={() => onOpenFile?.("src/App.tsx")}
    >
      open file
    </button>
  ),
}));

import {
  ConversationSplitPaneRenderer,
  ConversationTitleActions,
} from "./ConversationShellRenderers";

let container: HTMLDivElement;
let root: Root | null = null;

afterEach(() => {
  act(() => root?.unmount());
  root = null;
  container?.remove();
});

function thread(id: string, cwd: string): Thread {
  return {
    id,
    preview: id,
    model_provider: "test",
    model: "test",
    cwd,
    status: "idle",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    turns: [],
  };
}

describe("ConversationSplitPaneRenderer file routing", () => {
  it("reports the pane thread together with the opened file path", () => {
    const secondaryThread = thread("secondary", "/repo/worktrees/secondary");
    const onOpenFile = vi.fn();
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);

    act(() => {
      root?.render(
        <ConversationSplitPaneRenderer
          state={{ ...initialState, threads: [secondaryThread] }}
          thread={secondaryThread}
          pane="secondary"
          splitComposerDrafts={{
            primary: emptyComposerDraft(),
            secondary: emptyComposerDraft(),
          }}
          splitPaneRefs={{ current: { primary: null, secondary: null } }}
          viewSwitchPending={false}
          onActivatePane={() => {}}
          onClosePane={() => {}}
          onConversationScroll={() => {}}
          onSetPrompt={() => {}}
          onPasteAttachmentFiles={() => {}}
          onRemoveFile={() => {}}
          onRemoveImage={() => {}}
          onSend={() => {}}
          onInterrupt={() => {}}
          onForkMessage={() => {}}
          onOpenFile={onOpenFile}
          onOpenAgent={() => {}}
          canEditThreadMessage={() => false}
          onEditMessage={() => {}}
          onCancelEditMessage={() => {}}
          onSubmitEditMessage={() => {}}
          onStreamFrame={() => {}}
          onOpenFileDiff={() => {}}
        />,
      );
    });

    act(() => {
      container.querySelector<HTMLButtonElement>('[data-testid="open-file-secondary"]')?.click();
    });

    expect(onOpenFile).toHaveBeenCalledWith(secondaryThread, "src/App.tsx");
  });
});

describe("ConversationTitleActions icon sizing", () => {
  it("uses the info icon as the 18px optical-size baseline", () => {
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);

    act(() => {
      root?.render(
        <ConversationTitleActions
          state={initialState}
          debugControlsVisible={false}
          enableLaunchPreview={false}
          previewingLaunch={false}
          onPinLaunchPreview={() => {}}
          enablePlanPanelDebug={false}
          onSeedPlanPanelDebug={() => {}}
          conversationGridVisible={false}
          onToggleConversationGrid={() => {}}
          enableRunDebugPanel={false}
          runDebugRef={createRef<HTMLDivElement>()}
          runDebugOpen={false}
          onToggleRunDebug={() => {}}
          runDebugPhase={{ label: "", detail: "", tone: "idle" }}
          runDebugEvents={[]}
          queuedMessages={[]}
          guideMessages={[]}
          composerImages={[]}
          composerFiles={[]}
          runDebugCopied={false}
          onCopyRunDebug={() => {}}
          onCloseRunDebug={() => {}}
          chipGalleryOpen={false}
          onCloseChipGallery={() => {}}
          environmentToggleRef={createRef<HTMLButtonElement>()}
          environmentPanelVisible={false}
          onToggleEnvironmentPanel={() => {}}
          rightPanelOpen={false}
          onToggleRightPanel={() => {}}
        />,
      );
    });

    const infoIcon = container.querySelector(".environment-toggle-button svg");

    expect(infoIcon?.getAttribute("width")).toBe("18");
    expect(infoIcon?.getAttribute("height")).toBe("18");
    expect(infoIcon?.getAttribute("viewBox")).toBe("0 0 24 24");
  });

});
