import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import type { ThreadItem, Turn } from "../shared/protocol";
import { TurnEditSummaryCard } from "./TurnEditSummaryCard";

beforeAll(() => {
  Element.prototype.getBoundingClientRect = function (): DOMRect {
    return {
      x: 0,
      y: 0,
      top: 0,
      left: 0,
      right: 0,
      bottom: 0,
      width: 0,
      height: 0,
      toJSON() {
        return this;
      },
    } as DOMRect;
  };
});

function buildEditItem(path: string, additions: number, deletions: number): ThreadItem {
  return {
    id: `item-${path}`,
    type: "tool_call",
    name: "edit_file",
    status: "completed",
    result: JSON.stringify({
      path,
      diff: {
        hunks: [
          {
            old_start: 1,
            new_start: 1,
            lines: [
              ...Array.from({ length: additions }, () => ({ op: "insert", content: "new" })),
              ...Array.from({ length: deletions }, () => ({ op: "delete", content: "old" })),
            ],
          },
        ],
      },
    }),
  };
}

function buildApplyPatchItem(): ThreadItem {
  return {
    id: "item-apply-patch",
    type: "tool_call",
    name: "apply_patch",
    status: "completed",
    result: JSON.stringify({
      changed_files: ["src/a.ts", "src/b.ts"],
      risk_summary: {
        file_count: 2,
        hunk_count: 2,
        added_lines: 4,
        deleted_lines: 1,
      },
      files: [
        {
          path: "src/a.ts",
          action: "update",
          diff: {
            hunks: [
              {
                old_start: 2,
                new_start: 2,
                lines: [
                  { op: "equal", content: "const oldName = true;" },
                  { op: "delete", content: "export const value = oldName;" },
                  { op: "insert", content: "export const value = true;" },
                ],
              },
            ],
          },
        },
        {
          path: "src/b.ts",
          action: "add",
          diff: { new_file: true, lines: 3 },
        },
      ],
    }),
  };
}

let container: HTMLDivElement | null = null;
let root: Root | null = null;

function mount(card: JSX.Element): void {
  if (container) unmount();
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  act(() => {
    root!.render(card);
  });
}

function unmount(): void {
  if (root) {
    act(() => {
      root!.unmount();
    });
    root = null;
  }
  if (container) {
    container.remove();
    container = null;
  }
}

afterEach(() => {
  vi.useRealTimers();
  unmount();
});

describe("TurnEditSummaryCard", () => {
  it("returns null when no file edits are present", () => {
    const turn: Turn = {
      id: "turn-1",
      status: "completed",
      items_view: "full",
      items: [],
    };
    mount(<TurnEditSummaryCard turn={turn} />);
    expect(container?.querySelector('.turn-edit-summary-card')).toBeFalsy();
  });

  it("renders a card with file name and diff stats", () => {
    const turn: Turn = {
      id: "turn-1",
      status: "completed",
      items_view: "full",
      items: [buildEditItem("/tmp/.zshrc", 4, 1)],
    };
    mount(<TurnEditSummaryCard turn={turn} />);
    expect(container?.textContent).toContain("已编辑 1 个文件");
    expect(container?.textContent).toContain(".zshrc");
    expect(container?.textContent).toContain("+4");
    expect(container?.textContent).toContain("-1");
  });

  it("waits until the turn reaches a terminal status", () => {
    const turn: Turn = {
      id: "turn-1",
      status: "in_progress",
      items_view: "full",
      items: [buildEditItem("/tmp/.zshrc", 4, 1)],
    };

    mount(<TurnEditSummaryCard turn={turn} />);

    expect(container?.querySelector(".turn-edit-summary-card")).toBeFalsy();
  });

  it("aggregates multiple edits and shows the file count", () => {
    const turn: Turn = {
      id: "turn-1",
      status: "completed",
      items_view: "full",
      items: [
        buildEditItem("/tmp/a.txt", 1, 0),
        buildEditItem("/tmp/b.txt", 0, 2),
      ],
    };
    mount(<TurnEditSummaryCard turn={turn} />);
    expect(container?.textContent).toContain("已编辑 2 个文件");
    expect(container?.textContent).toContain("a.txt");
    expect(container?.textContent).toContain("b.txt");
  });

  it("renders each file from apply_patch with per-file stats", () => {
    const turn: Turn = {
      id: "turn-1",
      status: "completed",
      items_view: "full",
      items: [buildApplyPatchItem()],
    };

    mount(<TurnEditSummaryCard turn={turn} />);

    expect(container?.textContent).toContain("已编辑 2 个文件");
    expect(container?.textContent).toContain("a.ts");
    expect(container?.textContent).toContain("b.ts");
    expect(container?.textContent).toContain("+1");
    expect(container?.textContent).toContain("-1");
    expect(container?.textContent).toContain("+3");
  });

  it("shows the matching apply_patch file diff on hover", () => {
    vi.useFakeTimers();
    const turn: Turn = {
      id: "turn-1",
      status: "completed",
      items_view: "full",
      items: [buildApplyPatchItem()],
    };

    mount(<TurnEditSummaryCard turn={turn} />);

    const row = container?.querySelector<HTMLElement>(".turn-edit-summary-row");
    expect(row).toBeTruthy();
    act(() => {
      row!.dispatchEvent(new MouseEvent("mouseover", { bubbles: true }));
      vi.advanceTimersByTime(300);
    });

    expect(document.body.querySelector(".tool-diff-preview-card")).toBeTruthy();
    expect(document.body.textContent).toContain("src/a.ts");
    expect(document.body.textContent).toContain("export const value = true;");
  });

  it("opens the selected file diff when a file row is clicked", () => {
    const onOpenFileDiff = vi.fn();
    const turn: Turn = {
      id: "turn-1",
      status: "completed",
      items_view: "full",
      items: [buildEditItem("/tmp/a.txt", 2, 1)],
    };

    mount(
      <TurnEditSummaryCard turn={turn} onOpenFileDiff={onOpenFileDiff} />,
    );

    const row = container?.querySelector<HTMLButtonElement>(
      ".turn-edit-summary-row",
    );
    expect(row).toBeTruthy();

    act(() => {
      row?.click();
    });

    expect(onOpenFileDiff).toHaveBeenCalledTimes(1);
    expect(onOpenFileDiff).toHaveBeenCalledWith(
      expect.objectContaining({
        path: "/tmp/a.txt",
        additions: 2,
        deletions: 1,
        diff: expect.objectContaining({
          path: "/tmp/a.txt",
        }),
      }),
    );
  });

  it("renders newly-created files from write_file", () => {
    const writeFileItem: ThreadItem = {
      id: "item-new",
      type: "tool_call",
      name: "write_file",
      status: "completed",
      result: JSON.stringify({
        path: "/tmp/new.txt",
        diff: { new_file: true, lines: 7 },
      }),
    };
    const turn: Turn = {
      id: "turn-1",
      status: "completed",
      items_view: "full",
      items: [writeFileItem],
    };
    mount(<TurnEditSummaryCard turn={turn} />);
    expect(container?.textContent).toContain("已编辑 1 个文件");
    expect(container?.textContent).toContain("new.txt");
    expect(container?.textContent).toContain("+7");
  });

  it("hides files beyond the visible limit", () => {
    const turn: Turn = {
      id: "turn-1",
      status: "completed",
      items_view: "full",
      items: Array.from({ length: 7 }, (_, i) =>
        buildEditItem(`/tmp/file${i}.txt`, 1, 0),
      ),
    };
    mount(<TurnEditSummaryCard turn={turn} />);
    expect(container?.textContent).toContain("已编辑 7 个文件");
    expect(container?.querySelectorAll(".turn-edit-summary-row")).toHaveLength(3);
    expect(container?.textContent).toContain("还有 4 个文件");
    expect(container?.textContent).toContain("再显示 3 个");
  });

  it("reveals additional files three at a time", () => {
    const turn: Turn = {
      id: "turn-1",
      status: "completed",
      items_view: "full",
      items: Array.from({ length: 7 }, (_, i) =>
        buildEditItem(`/tmp/file${i}.txt`, 1, 0),
      ),
    };
    mount(<TurnEditSummaryCard turn={turn} />);

    const showMore = () =>
      container?.querySelector<HTMLButtonElement>(".turn-edit-summary-more-button");

    expect(container?.querySelectorAll(".turn-edit-summary-row")).toHaveLength(3);
    expect(showMore()?.textContent).toContain("再显示 3 个");

    act(() => {
      showMore()?.click();
    });
    expect(container?.querySelectorAll(".turn-edit-summary-row")).toHaveLength(6);
    expect(showMore()?.textContent).toContain("再显示 1 个");
    expect(container?.textContent).toContain("还有 1 个文件");

    act(() => {
      showMore()?.click();
    });
    expect(container?.querySelectorAll(".turn-edit-summary-row")).toHaveLength(7);
    expect(showMore()).toBeFalsy();
  });
});
