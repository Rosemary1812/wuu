import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import { TurnFileDiffPanel } from "./TurnFileDiffPanel";

let container: HTMLDivElement | null = null;
let root: Root | null = null;

function mount(element: JSX.Element): void {
  if (container) unmount();
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  act(() => {
    root!.render(element);
  });
}

function unmount(): void {
  if (root) {
    act(() => {
      root!.unmount();
    });
    root = null;
  }
  container?.remove();
  container = null;
}

afterEach(() => {
  unmount();
});

describe("TurnFileDiffPanel", () => {
  it("renders the selected turn file diff in a right-side panel", () => {
    const onClose = vi.fn();

    mount(
      <TurnFileDiffPanel
        selection={{
          path: "/tmp/a.txt",
          additions: 1,
          deletions: 1,
          newFile: false,
          diff: {
            path: "/tmp/a.txt",
            hunks: [
              {
                oldStart: 4,
                newStart: 4,
                lines: [
                  { op: "delete", content: "old value" },
                  { op: "insert", content: "new value" },
                ],
              },
            ],
          },
        }}
        onClose={onClose}
      />,
    );

    const panel = container?.querySelector<HTMLElement>(".turn-file-diff-panel");
    expect(panel).toBeTruthy();
    expect(panel?.textContent).toContain("/tmp/a.txt");
    expect(panel?.textContent).toContain("old value");
    expect(panel?.textContent).toContain("new value");
    expect(panel?.textContent).toContain("+1");
    expect(panel?.textContent).toContain("-1");

    act(() => {
      panel
        ?.querySelector<HTMLButtonElement>(".turn-file-diff-close")
        ?.click();
    });

    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("does not render without a selected diff", () => {
    mount(<TurnFileDiffPanel selection={undefined} onClose={() => {}} />);

    expect(container?.querySelector(".turn-file-diff-panel")).toBeFalsy();
  });
});
