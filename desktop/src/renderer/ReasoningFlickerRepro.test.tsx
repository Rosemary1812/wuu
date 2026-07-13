/**
 * Reproduction harness for the user-reported "reasoning fold only shows
 * content after expansion, not the full thinking history" bug.
 *
 * Mirrors `CommentaryFlickerRepro.test.tsx` so we can compare the two
 * side by side. The two surfaces share the same `streamTextStore` and
 * the same `syncStreamItem` code path in `AppState`, so if the
 * commentary flicker reproduces for commentary, the reasoning fold
 * should hit the same reset when an `item/started` re-sync arrives
 * with empty `text` after deltas have already accumulated.
 *
 * Method: render the shell with one streaming reasoning item, drive the
 * stream store the way the backend would, and snapshot textContent at
 * each phase. If the visible text collapses to "" mid-stream and then
 * grows back from the post-reset point, that matches the user's
 * report byte-for-byte — the fold body only ever shows content
 * received after the most recent reset.
 *
 * Note: jsdom doesn't fire rAF during a synchronous `act(...)` — we
 * have to await setTimeout(300ms) for the cursor to actually catch
 * up to the target text. Same trick `CommentaryFlickerRepro` uses.
 */
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeAll, describe, expect, it } from "vitest";
import { createElement, type JSX } from "react";
import type { ThreadItem, Turn } from "../shared/protocol";
import { AssistantTurnShell } from "./AssistantTurnShell";
import { buildAssistantTurnDisplay } from "./AssistantTurnDisplay";
import { streamTextKey, streamTextStore } from "./StreamText";

// jsdom doesn't implement layout. Stub getBoundingClientRect so React
// doesn't crash on layout queries. Same as CommentaryFlickerRepro.
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

let idCounter = 0;
const mountedRoots: Root[] = [];

function nextID(prefix: string): string {
  idCounter += 1;
  return `${prefix}-${idCounter}`;
}

function makeTurn(status: Turn["status"], items: ThreadItem[]): Turn {
  return {
    id: "turn-reasoning-repro",
    items,
    items_view: "full",
    status,
  };
}

function makeStreamingReasoning(text: string): ThreadItem {
  return {
    id: nextID("reasoning-live"),
    type: "reasoning",
    status: "in_progress",
    text,
  };
}

function defaultItemRenderer(
  item: ThreadItem,
  _streaming: boolean,
): JSX.Element {
  // Reasoning goes through ReasoningFold; non-reasoning items are not
  // exercised in this reproduction so just emit a placeholder.
  if (item.type === "reasoning") {
    return createElement("div", { "data-reasoning-stub": item.id });
  }
  return createElement("div", null);
}

function renderShell(turn: Turn) {
  const container = document.createElement("div");
  document.body.appendChild(container);
  const root = createRoot(container);
  const display = buildAssistantTurnDisplay(
    turn,
    undefined,
    defaultItemRenderer,
  );
  if (!display) {
    throw new Error("expected a display");
  }
  act(() => {
    root.render(
      createElement(AssistantTurnShell, {
        turn,
        display,
        onStreamFrame: () => {},
      }),
    );
  });
  mountedRoots.push(root);
  return { container, root };
}

/**
 * The reasoning fold body text container. The fold itself is the
 * `<details>` element; the streaming markdown surface lives inside
 * `.turn-reasoning-scroll`. We snapshot its textContent because the
 * user's report is about the visible text inside the fold body.
 */
function findReasoningScroll(container: HTMLElement): HTMLElement | null {
  return container.querySelector(".turn-reasoning-scroll");
}

function textSnapshot(node: HTMLElement): string {
  return (node.textContent ?? "").trim();
}

afterEach(() => {
  while (mountedRoots.length > 0) {
    const root = mountedRoots.pop();
    act(() => {
      root?.unmount();
    });
  }
  for (let i = 1; i <= idCounter; i += 1) {
    streamTextStore.clearItem("turn-reasoning-repro", `reasoning-live-${i}`);
  }
  idCounter = 0;
  document.body.innerHTML = "";
});

/**
 * Drive a state change and wait long enough for the RAF cursor to
 * catch up to the latest target text. Mirrors the helper in
 * `CommentaryFlickerRepro`.
 */
async function step(
  root: Root,
  turn: Turn,
  mutate?: () => void,
): Promise<void> {
  if (mutate) mutate();
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 300));
  });
  const display = buildAssistantTurnDisplay(
    turn,
    undefined,
    defaultItemRenderer,
  );
  if (!display) throw new Error("expected display");
  act(() => {
    root.render(
      createElement(AssistantTurnShell, {
        turn,
        display,
        onStreamFrame: () => {},
      }),
    );
  });
}

describe("reasoning flicker reproduction — end-to-end", () => {
  it("syncStreamItem-style reset wipes accumulated reasoning text", async () => {
    const reasoning = makeStreamingReasoning("");
    const turn = makeTurn("in_progress", [reasoning]);
    const { container, root } = renderShell(turn);

    const reasoningKey = streamTextKey(
      "turn-reasoning-repro",
      reasoning.id,
      "text",
    );

    const log: Array<{ step: string; text: string; storeValue: string }> = [];

    // Phase 1: first delta bumps both stream store and item.text.
    await step(
      root,
      { ...turn, items: [{ ...reasoning, text: "L" }] },
      () => {
        streamTextStore.set(reasoningKey, "L");
      },
    );
    const n1 = findReasoningScroll(container)!;
    log.push({
      step: "1: after first delta 'L'",
      text: textSnapshot(n1),
      storeValue: streamTextStore.get(reasoningKey),
    });

    // Phase 2: more deltas arrive via the "stream" path (item.text
    // stays at the first delta). The store now holds the full text.
    await step(root, turn, () => {
      streamTextStore.append(reasoningKey, "et me think about that.");
    });
    const n2 = findReasoningScroll(container)!;
    log.push({
      step: "2: more deltas, store='Let me think about that.'",
      text: textSnapshot(n2),
      storeValue: streamTextStore.get(reasoningKey),
    });

    // Phase 3: backend re-sends item/started with text=''. This runs
    // syncStreamItem → streamTextStore.set(key, '') which OVERWRITES
    // the stream store's value. Same pattern as the commentary
    // flicker bug.
    await step(
      root,
      { ...turn, items: [{ ...reasoning, text: "" }] },
      () => {
        streamTextStore.set(reasoningKey, "");
      },
    );
    const n3 = findReasoningScroll(container)!;
    log.push({
      step: "3: item/started resync with text=''",
      text: textSnapshot(n3),
      storeValue: streamTextStore.get(reasoningKey),
    });

    // Phase 4: more deltas after the reset — reasoning "reappears"
    // from the post-reset position, exactly matching the user's
    // "only see content after expansion" report.
    await step(root, turn, () => {
      streamTextStore.append(reasoningKey, "L");
      streamTextStore.append(reasoningKey, "et me think about that.");
    });
    const n4 = findReasoningScroll(container)!;
    log.push({
      step: "4: more deltas, reasoning reappears",
      text: textSnapshot(n4),
      storeValue: streamTextStore.get(reasoningKey),
    });

    /* eslint-disable no-console */
    console.log("\n=== reasoning scroll textContent across sync reset ===");
    for (const row of log) {
      console.log(
        `  ${row.step.padEnd(50)} text=${JSON.stringify(row.text).padEnd(28)} store=${JSON.stringify(row.storeValue)}`,
      );
    }
    /* eslint-enable no-console */

    const phase2 = log[1].text;
    const phase3 = log[2].text;
    const phase4 = log[3].text;

    // The fold body must show full text at every phase except the
    // reset window. Phase 3 is the exact moment the user reports
    // "only see content after expansion" — it's logged below as
    // CONFIRMED when the collapse reproduces. We deliberately do NOT
    // assert phase3 here for the same reason CommentaryFlickerRepro
    // doesn't: the test simulates the reset at the store level via
    // `streamTextStore.set(key, "")`, which bypasses the
    // `syncStreamItem` length guard and so keeps reproducing even
    // after the fix lands. The fix is verified by reading the guard
    // in AppState.syncStreamItem and by manual verification, not by
    // this harness. The harness's job is to keep the bug pattern
    // legible across the codebase.
    expect(phase2).toBe("Let me think about that.");
    expect(phase4).toBe("Let me think about that.");

    if (phase2 && !phase3) {
      /* eslint-disable no-console */
      console.log(
        "\n  >>> CONFIRMED: reasoning text collapsed from",
        JSON.stringify(phase2),
        "to empty at phase 3 — same root cause as CommentaryFlicker <<<",
      );
      /* eslint-enable no-console */
    } else if (phase2.length > phase3.length) {
      /* eslint-disable no-console */
      console.log(
        "\n  >>> CONFIRMED: reasoning text shrunk from",
        JSON.stringify(phase2),
        "to",
        JSON.stringify(phase3),
        "at phase 3 — same root cause as CommentaryFlicker <<<",
      );
      /* eslint-enable no-console */
    } else {
      /* eslint-disable no-console */
      console.log(
        "\n  step 2→3: text unchanged at",
        JSON.stringify(phase3),
        "— reasoning is NOT affected by syncStreamItem reset",
      );
      /* eslint-enable no-console */
    }
  });
});
