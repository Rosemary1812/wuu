import { act, createRef } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import type { InitializeResult } from "../shared/protocol";
import { initialState, type AppState } from "./AppState";
import { EnvironmentSideStack } from "./EnvironmentSideStack";

let container: HTMLDivElement;
let root: Root | null = null;

beforeEach(() => {
  container = document.createElement("div");
  document.body.appendChild(container);
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  root = null;
  container.remove();
});

function initialized(): InitializeResult {
  return {
    protocol_version: "wuu-app-server/v0.1",
    provider: "test",
    model: "test-model",
    workspace_root: "/repo",
  };
}

function renderStack(options: {
  queryHistoryDocked: boolean;
}): void {
  const state: AppState = {
    ...initialState,
    initialized: initialized(),
  };

  act(() => {
    root = createRoot(container);
    root.render(
      <EnvironmentSideStack
        visible
        mounted
        state={state}
        panelRef={createRef<HTMLDivElement>()}
        closing={false}
        motionState="open"
        backgroundProcesses={[]}
        stoppingProcessIDs={new Set()}
        activeMenu={null}
        running={false}
        pullRequestDisabledReason=""
        queryHistoryDocked={options.queryHistoryDocked}
        queryHistory={[
          { turnID: "turn-1", itemID: "item-1", text: "first query" },
          { turnID: "turn-2", itemID: "item-2", text: "second query" },
        ]}
        onSetActiveMenu={() => {}}
        onClose={() => {}}
        onOpenProject={() => {}}
        onSelectNoProject={() => {}}
        onSelectBranch={() => {}}
        onCreateBranch={() => Promise.resolve()}
        onOpenReview={() => {}}
        onOpenCommit={() => {}}
        onOpenPullRequest={() => {}}
        onStopBackgroundProcess={() => {}}
        onOpenBackgroundPreview={() => {}}
        onSelectQueryHistory={() => {}}
      />,
    );
  });
}

describe("EnvironmentSideStack", () => {
  it("docks query history below the environment panel when requested", () => {
    renderStack({ queryHistoryDocked: true });

    expect(container.querySelector(".environment-side-stack > .environment-panel")).not.toBeNull();
    expect(container.querySelector(".environment-side-stack > .query-history-environment-slot")).not.toBeNull();
    expect(container.querySelectorAll(".query-history-item")).toHaveLength(2);
  });

  it("does not render the docked query history slot when docking is off", () => {
    renderStack({ queryHistoryDocked: false });

    expect(container.querySelector(".environment-side-stack > .query-history-environment-slot")).toBeNull();
  });
});
