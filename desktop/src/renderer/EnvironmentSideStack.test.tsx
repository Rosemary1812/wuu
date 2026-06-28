import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { act, createRef } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import type { DesktopProject, InitializeResult, RuntimeContext } from "../shared/protocol";
import { initialState, type AppState } from "./AppState";
import type { EnvironmentPanelMenu } from "./EnvironmentPanel";
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
  activeMenu?: EnvironmentPanelMenu;
  activeContext?: RuntimeContext;
  activeProject?: DesktopProject;
}): void {
  const state: AppState = {
    ...initialState,
    initialized: initialized(),
    activeContext: options.activeContext,
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
        activeProject={options.activeProject}
        backgroundProcesses={[]}
        stoppingProcessIDs={new Set()}
        activeMenu={options.activeMenu ?? null}
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

  it("labels the context selector as a project control", () => {
    renderStack({
      queryHistoryDocked: false,
      activeMenu: "mode",
      activeContext: { kind: "project", project_id: "project-1", cwd: "/repo/wuu" },
      activeProject: {
        id: "project-1",
        name: "wuu",
        path: "/repo/wuu",
        created_at: "2026-01-01T00:00:00.000Z",
        updated_at: "2026-01-01T00:00:00.000Z",
      },
    });

    expect(container.textContent).toContain("项目");
    expect(container.textContent).toContain("wuu");
    expect(container.textContent).toContain("切换项目");
    expect(container.textContent).toContain("使用现有文件夹");
    expect(container.textContent).toContain("不使用项目");
    expect(container.textContent).not.toContain("打开本地项目");
  });

  it("lets project and branch menus escape the side-stack panel bounds", () => {
    const rule = cssRule(".environment-side-stack > .environment-panel");

    expect(rule).toContain("overflow: visible");
    expect(rule).not.toContain("overflow: hidden");
  });
});
