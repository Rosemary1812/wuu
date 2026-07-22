import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import type { Turn } from "../shared/protocol";
import { I18nProvider, setActiveLocale } from "./i18n";
import { WorkspaceDocumentTurnDock } from "./WorkspaceDocumentTurnDock";

function turn(
  id: string,
  status: Turn["status"] = "in_progress",
  userText = "Rewrite the weak section.",
): Turn {
  return {
    id,
    items_view: "full",
    status,
    items: [
      { id: `${id}-user`, type: "user_message", text: userText },
      {
        id: `${id}-agent`,
        type: "agent_message",
        phase: "final_answer",
        status: "completed",
        text: "I am **tightening** that section now.",
      },
    ],
  };
}

describe("WorkspaceDocumentTurnDock", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    setActiveLocale("en-US");
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
  });

  function render(turns: Turn[], key = "thread-a", statusText?: string): void {
    act(() => {
      root.render(
        <I18nProvider>
          <WorkspaceDocumentTurnDock key={key} turns={turns} statusText={statusText}>
            <div data-testid="composer">Composer</div>
          </WorkspaceDocumentTurnDock>
        </I18nProvider>,
      );
    });
  }

  it("shows a compact turn peek and expands only the rich final answer", () => {
    render([turn("turn-1")], "thread-a", "Editing README.md");

    const toggle = container.querySelector<HTMLButtonElement>(
      ".workspace-document-turn-summary",
    );
    expect(toggle?.getAttribute("aria-expanded")).toBe("false");
    expect(toggle?.textContent).toContain("Editing README.md");
    expect(toggle?.textContent).toContain("Rewrite the weak section.");
    expect(container.querySelector(".workspace-document-turn-details")).toBeNull();

    act(() => toggle?.click());

    expect(toggle?.getAttribute("aria-expanded")).toBe("true");
    const details = container.querySelector(".workspace-document-turn-details");
    expect(details?.textContent).not.toContain("Rewrite the weak section.");
    expect(details?.textContent).toContain("I am tightening that section now.");
    expect(details?.querySelector("strong")?.textContent).toBe("tightening");
    expect(details?.querySelector(".workspace-document-turn-message")).toBeNull();
  });

  it("ignores commentary and shows a simple status before final text arrives", () => {
    const runningTurn = turn("turn-running");
    runningTurn.items = [
      runningTurn.items[0],
      {
        id: "turn-running-commentary",
        type: "agent_message",
        phase: "commentary",
        status: "in_progress",
        text: "Internal progress that does not belong in the result.",
      },
    ];
    render([runningTurn], "thread-a", "Editing README.md");

    act(() => {
      container.querySelector<HTMLButtonElement>(".workspace-document-turn-summary")?.click();
    });

    const details = container.querySelector(".workspace-document-turn-details");
    expect(details?.textContent).toBe("Editing README.md");
    expect(details?.textContent).not.toContain("Internal progress");
    expect(details?.querySelector(".rich-content")).toBeNull();
  });

  it("resets the drawer when the active session changes", () => {
    render([turn("turn-shared")]);
    act(() => {
      container.querySelector<HTMLButtonElement>(".workspace-document-turn-summary")?.click();
    });
    expect(
      container
        .querySelector(".workspace-document-turn-summary")
        ?.getAttribute("aria-expanded"),
    ).toBe("true");

    render([turn("turn-shared")], "thread-b");

    expect(
      container
        .querySelector(".workspace-document-turn-summary")
        ?.getAttribute("aria-expanded"),
    ).toBe("false");
  });

  it("keeps the Composer clean when the thread has no user turn", () => {
    render([
      {
        id: "internal-turn",
        items_view: "full",
        status: "completed",
        items: [{ id: "internal-agent", type: "agent_message", text: "Internal" }],
      },
    ]);

    expect(container.querySelector('[data-testid="composer"]')).not.toBeNull();
    expect(container.querySelector('[data-testid="workspace-document-turn-drawer"]')).toBeNull();
  });
});
