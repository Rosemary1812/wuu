import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  useViewSwitchState,
  type ViewSwitchStateController,
} from "./ViewSwitchState";

let mountedRoots: Root[] = [];

afterEach(() => {
  act(() => {
    for (const root of mountedRoots) root.unmount();
  });
  mountedRoots = [];
  document.body.innerHTML = "";
  vi.useRealTimers();
  vi.restoreAllMocks();
});

async function renderViewSwitchState(): Promise<{
  get: () => ViewSwitchStateController;
}> {
  let latest: ViewSwitchStateController | undefined;

  function Probe(): null {
    latest = useViewSwitchState({ loadingDelayMs: 50 });
    return null;
  }

  const container = document.createElement("div");
  document.body.appendChild(container);
  const root = createRoot(container);
  mountedRoots.push(root);

  await act(async () => {
    root.render(createElement(Probe));
  });

  return {
    get: () => {
      if (!latest) {
        throw new Error("view switch state was not rendered");
      }
      return latest;
    },
  };
}

describe("useViewSwitchState", () => {
  it("marks thread switches visible immediately", async () => {
    const hook = await renderViewSwitchState();

    act(() => {
      hook.get().beginViewSwitch("thread", "thread-1");
    });

    expect(hook.get().pendingViewSwitch).toEqual({
      kind: "thread",
      targetID: "thread-1",
      visible: true,
    });
    expect(hook.get().visiblePendingThreadID).toBe("thread-1");
    expect(hook.get().viewContextSwitchPending).toBe(false);
  });

  it("marks project switches visible immediately", async () => {
    vi.useFakeTimers();
    const hook = await renderViewSwitchState();

    act(() => {
      hook.get().beginViewSwitch("project", "project-1");
    });
    expect(hook.get().pendingViewSwitch).toEqual({
      kind: "project",
      targetID: "project-1",
      visible: true,
    });
    expect(hook.get().visiblePendingProjectID).toBe("project-1");
    expect(hook.get().viewContextSwitchPending).toBe(true);
  });

  it("rejects stale finish requests and keeps the newest pending switch", async () => {
    const hook = await renderViewSwitchState();

    let staleRequest = 0;
    let currentRequest = 0;
    act(() => {
      staleRequest = hook.get().beginViewSwitch("thread", "thread-old");
      currentRequest = hook.get().beginViewSwitch("thread", "thread-new");
    });

    expect(hook.get().finishViewSwitch(staleRequest)).toBe(false);
    expect(hook.get().pendingViewSwitch?.targetID).toBe("thread-new");
    let finished = false;
    act(() => {
      finished = hook.get().finishViewSwitch(currentRequest);
    });
    expect(finished).toBe(true);
    expect(hook.get().pendingViewSwitch).toBeUndefined();
  });

  it("cancel invalidates in-flight request IDs", async () => {
    const hook = await renderViewSwitchState();

    let requestID = 0;
    act(() => {
      requestID = hook.get().beginInstantThreadSwitch();
      hook.get().cancelViewSwitch();
    });

    expect(hook.get().isCurrentViewSwitchRequest(requestID)).toBe(false);
    expect(hook.get().pendingViewSwitch).toBeUndefined();
  });
});
