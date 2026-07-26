import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { act, createRef } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import type { InitializeResult } from "../shared/protocol";
import { initialState, type AppState } from "./AppState";
import { environmentPanelScaleForWidth } from "./EnvironmentPanelScale";
import {
  EnvironmentSideStack,
  type SubagentRowSummary,
} from "./EnvironmentSideStack";
import { agentStatusLabel } from "./ThreadAgents";
import { translateCurrent } from "./i18n";
import { hoverTooltipText, unhoverTooltip } from "./tooltipTestUtils";

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
  unhoverTooltip();
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

  it("renders a subagent's pooled name and source in its tooltip", async () => {
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
    expect(row?.title).toBe("");
    const tooltip = await hoverTooltipText(row);
    expect(tooltip).toContain("薛定谔");
    expect(tooltip).toContain("科学家");
    expect(tooltip).toContain(agentStatusLabel("running"));
    expect(tooltip).toContain("worker");
    expect(tooltip).toContain("Check types");
    expect(tooltip).not.toContain("undefined");
  });

  it("shows only the five most recent subagents and reports older hidden ones", async () => {
    renderStack(
      {},
      Array.from({ length: 7 }, (_, index) => ({
        id: `agent-${index}`,
        status: "completed",
        task_name: `Task ${index}`,
        started_at: `2026-01-0${index + 1}T00:00:00Z`,
      })),
    );

    const rows = Array.from(
      container.querySelectorAll<HTMLButtonElement>(".subagent-row-main"),
    );
    expect(rows).toHaveLength(5);
    // Rows share pooled display names; the per-agent task name lives in the
    // hover tooltip, so hover each row to see which agents made the cut.
    const tooltips: string[] = [];
    for (const row of rows) {
      tooltips.push((await hoverTooltipText(row)) ?? "");
    }
    expect(tooltips).toEqual(
      expect.arrayContaining([
        expect.stringContaining("Task 2"),
        expect.stringContaining("Task 6"),
      ]),
    );
    expect(tooltips.some((text) => text.includes("Task 0"))).toBe(false);
    expect(tooltips.some((text) => text.includes("Task 1"))).toBe(false);
    expect(container.querySelector(".environment-subagent-overflow-note")?.textContent).toBe(
      translateCurrent("environment.earlierSubtasksHidden", { count: 2 }),
    );
  });
});
