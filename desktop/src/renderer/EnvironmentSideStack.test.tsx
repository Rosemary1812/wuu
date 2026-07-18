import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { act, createRef } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import type { InitializeResult, Thread } from "../shared/protocol";
import { initialState, type AppState } from "./AppState";
import { environmentPanelScaleForWidth } from "./EnvironmentPanelScale";
import {
  EnvironmentSideStack,
  type SubagentRowSummary,
} from "./EnvironmentSideStack";
import { agentStatusLabel } from "./ThreadAgents";

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

function groupThread(): Thread {
  return {
    id: "thread-group",
    preview: "讨论群聊信息",
    title: "前端小队",
    model_provider: "test",
    model: "test-model",
    cwd: "/repo",
    status: "idle",
    group: true,
    created_at: "2026-07-04T00:00:00Z",
    updated_at: "2026-07-04T00:00:00Z",
    turns: [],
    members: [
      { id: "participant-1", name: "小青", kind: "named", role: "评审" },
    ],
  };
}

function renderStack(
  stateOverrides: Partial<AppState> = {},
  subagentSessions?: SubagentRowSummary[],
): void {
  const state: AppState = {
    ...initialState,
    initialized: initialized(),
    ...stateOverrides,
  };

  act(() => {
    root = createRoot(container);
    root.render(
      <EnvironmentSideStack
        visible={true}
        mounted={true}
        state={state}
        panelRef={createRef<HTMLDivElement>()}
        closing={false}
        motionState="open"
        activeMenu={null}
        running={false}
        pullRequestDisabledReason=""
        onSetActiveMenu={() => {}}
        onClose={() => {}}
        onSelectBranch={() => {}}
        onCreateBranch={() => Promise.resolve()}
        onOpenReview={() => {}}
        onOpenCommit={() => {}}
        onOpenPullRequest={() => {}}
        subagentSessions={subagentSessions}
        participants={[
          { id: "participant-1", name: "小青", kind: "named", role: "评审" },
        ]}
      />,
    );
  });
}

describe("EnvironmentSideStack", () => {
  it("renders the environment panel without a docked query-history card", () => {
    renderStack();

    expect(container.querySelector(".environment-info-side-stack")).not.toBeNull();
    expect(container.querySelector(".environment-side-stack > .environment-panel")).not.toBeNull();
    expect(container.querySelector(".environment-side-stack > .query-history-environment-slot")).toBeNull();
    expect(container.querySelector(".query-history-popover")).toBeNull();
  });

  it("lets branch menus escape the side-stack panel bounds", () => {
    const rule = cssRule(".environment-side-stack > .environment-panel");

    expect(rule).toContain("overflow: visible");
    expect(rule).not.toContain("overflow: hidden");
  });

  it("scales the complete environment panel at narrow widths", () => {
    Object.defineProperty(container, "clientWidth", {
      configurable: true,
      value: 420,
    });
    renderStack();

    expect(environmentPanelScaleForWidth(560)).toBe(0.8);
    expect(environmentPanelScaleForWidth(420)).toBe(0.6);
    expect(environmentPanelScaleForWidth(320)).toBe(0.576);
    expect(environmentPanelScaleForWidth(1_000)).toBe(0.8);
    expect(environmentPanelScaleForWidth(1_200)).toBe(0.9);
    expect(environmentPanelScaleForWidth(1_400)).toBe(1);

    const stack = container.querySelector<HTMLElement>(
      ".environment-info-side-stack",
    );
    expect(stack?.style.getPropertyValue("--environment-panel-scale")).toBe(
      "0.6",
    );

    const rule = cssRule(".environment-side-stack.environment-info-side-stack");
    expect(rule).toContain("transform: scale(var(--environment-panel-scale, 1))");
    expect(rule).toContain("transform-origin: top right");
  });

  it("renders group info instead of session environment rows for group threads", () => {
    renderStack({ thread: groupThread() });

    expect(container.querySelector(".group-info-panel")).not.toBeNull();
    expect(container.textContent).toContain("群聊信息");
    expect(container.textContent).toContain("前端小队");
    expect(container.textContent).not.toContain("群内容");
    expect(container.textContent).not.toContain("创建拉取请求");
  });

  it("renders a subagent's pooled name and source in its tooltip", () => {
    renderStack({}, [
      {
        id: "agent-0",
        status: "running",
        type: "worker",
        task_name: "Check types",
      },
    ]);

    const row = container.querySelector<HTMLButtonElement>(".subagent-row-main");
    expect(row).not.toBeNull();
    expect(container.textContent).toContain("薛定谔");
    expect(row?.title).toContain("薛定谔");
    expect(row?.title).toContain("科学家");
    expect(row?.title).toContain(agentStatusLabel("running"));
    expect(row?.title).toContain("worker");
    expect(row?.title).toContain("Check types");
    expect(row?.title).not.toContain("undefined");
  });
});
