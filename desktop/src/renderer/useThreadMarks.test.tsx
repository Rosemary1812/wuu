import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { MessageMarkWire, ServerEvent } from "../shared/protocol";
import type { MessageMarksView } from "./MessageMarks";
import { upsertMark, useThreadMarks } from "./useThreadMarks";

describe("upsertMark", () => {
  it("replaces the same (seq, participant, kind) in place, keeps other kinds", () => {
    let list: MessageMarkWire[] = [
      { seq: 1, participant_id: "a", kind: "seen", status: "in_progress" },
    ];
    list = upsertMark(list, {
      seq: 1,
      participant_id: "a",
      kind: "seen",
      status: "completed",
    });
    expect(list).toHaveLength(1);
    expect(list[0].status).toBe("completed");
    list = upsertMark(list, {
      seq: 1,
      participant_id: "a",
      kind: "reaction",
      reaction: "smug",
    });
    expect(list).toHaveLength(2);
  });
});

let mountedRoots: Root[] = [];
let eventHandler: ((event: ServerEvent) => void) | undefined;

afterEach(() => {
  act(() => {
    for (const root of mountedRoots) root.unmount();
  });
  mountedRoots = [];
  eventHandler = undefined;
  document.body.innerHTML = "";
  delete (window as unknown as { wuu?: unknown }).wuu;
});

function stubWuu(initial: MessageMarkWire[]) {
  (window as unknown as { wuu: unknown }).wuu = {
    getThreadMarks: vi.fn(async () => ({ marks: initial })),
    onServerEvent: (handler: (event: ServerEvent) => void) => {
      eventHandler = handler;
      return () => {
        eventHandler = undefined;
      };
    },
  };
}

async function renderHook(): Promise<{ marks: () => ReadonlyMap<number, MessageMarksView> }> {
  let latest: ReadonlyMap<number, MessageMarksView> = new Map();
  function Probe() {
    latest = useThreadMarks("thread-1", true);
    return null;
  }
  const container = document.createElement("div");
  document.body.appendChild(container);
  const root = createRoot(container);
  mountedRoots.push(root);
  await act(async () => {
    root.render(createElement(Probe));
  });
  return { marks: () => latest };
}

describe("useThreadMarks", () => {
  it("loads marks from thread/marks and applies live message/mark events", async () => {
    stubWuu([{ seq: 4, participant_id: "a", kind: "seen", status: "completed" }]);
    const hook = await renderHook();
    // initial fetch resolved into an aggregated view
    expect(hook.marks().get(4)?.seen.completed).toEqual(["a"]);

    // a live reaction notification for this thread patches the map
    await act(async () => {
      eventHandler?.({
        kind: "notification",
        message: {
          method: "message/mark",
          params: {
            thread_id: "thread-1",
            seq: 4,
            participant_id: "b",
            kind: "reaction",
            reaction: "shrug",
          },
        },
      } as ServerEvent);
    });
    expect(hook.marks().get(4)?.reactions[0]?.key).toBe("shrug");

    // a notification for a different thread is ignored
    await act(async () => {
      eventHandler?.({
        kind: "notification",
        message: {
          method: "message/mark",
          params: {
            thread_id: "other",
            seq: 9,
            participant_id: "z",
            kind: "seen",
            status: "completed",
          },
        },
      } as ServerEvent);
    });
    expect(hook.marks().has(9)).toBe(false);
  });
});
