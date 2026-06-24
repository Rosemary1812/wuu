import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it } from "vitest";
import type { Turn } from "../shared/protocol";
import { TurnView } from "./TurnView";

let root: Root | undefined;
let container: HTMLDivElement | undefined;

function makeTurn(status: Turn["status"]): Turn {
  return {
    id: "turn-1",
    items: [],
    items_view: "full",
    status,
  };
}

function render(turn: Turn): HTMLDivElement {
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  act(() => {
    root!.render(
      <TurnView
        turn={turn}
        onStreamFrame={() => {}}
        onNoticeAction={() => {}}
      />,
    );
  });
  return container;
}

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
  root = undefined;
  container = undefined;
});

describe("TurnView", () => {
  it("marks the turn root with the live status used by scroll-stable CSS", () => {
    const view = render(makeTurn("in_progress"));

    const turn = view.querySelector<HTMLElement>(".turn");
    expect(turn?.dataset.turnStatus).toBe("in_progress");
  });
});
