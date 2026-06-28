import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { act, createRef } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import type { InitializeResult } from "../shared/protocol";
import { initialState, type AppState } from "./AppState";
import { EnvironmentSideStack } from "./EnvironmentSideStack";

let container: HTMLDivElement;
let root: Root | null = null;

const environmentCSS = readFileSync(
  resolve(process.cwd(), "src/renderer/styles/environment.css"),
  "utf8",
);

function cssRule(selector: string): string {
  const escapedSelector = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const matches = Array.from(
    environmentCSS.matchAll(
      new RegExp(`^${escapedSelector}\\s*\\{([\\s\\S]*?)\\n\\}`, "gm"),
    ),
  );
  expect(matches).not.toHaveLength(0);
  return matches.at(-1)?.[1] ?? "";
}

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

  it("lets branch menus escape the side-stack panel bounds", () => {
    const rule = cssRule(".environment-side-stack > .environment-panel");

    expect(rule).toContain("overflow: visible");
    expect(rule).not.toContain("overflow: hidden");
  });
});
