import { act, createElement, createRef, type ReactNode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import type {
  SideThreadMessage,
  SideThreadSummary,
} from "../shared/protocol";
import {
  SideThreadPanel,
  type SideThreadPanelHandle,
} from "./SideThreadPanel";
import type { SideThreadEntryState } from "./SideThreadState";

let mountedRoots: Root[] = [];
let mountedContainers: HTMLElement[] = [];

function mount(element: React.ReactElement): HTMLElement {
  const container = document.createElement("div");
  document.body.appendChild(container);
  const root = createRoot(container);
  act(() => root.render(element));
  mountedRoots.push(root);
  mountedContainers.push(container);
  return container;
}

afterEach(() => {
  for (const root of mountedRoots) {
    act(() => root.unmount());
  }
  for (const container of mountedContainers) {
    container.remove();
  }
  mountedRoots = [];
  mountedContainers = [];
  vi.restoreAllMocks();
});

function makeSummary(
  overrides: Partial<SideThreadSummary> = {},
): SideThreadSummary {
  return {
    side_thread_id: "side-1",
    main_thread_id: "main-1",
    status: "completed",
    revision: 1,
    created_at: "2026-01-01T00:00:00.000Z",
    updated_at: "2026-01-01T00:00:00.000Z",
    ...overrides,
  };
}

function makeEntry(
  overrides: Partial<SideThreadEntryState> = {},
): SideThreadEntryState {
  return {
    open: true,
    summary: null,
    messages: [],
    draft: "",
    streaming: false,
    ...overrides,
  };
}

function makeMessage(
  overrides: Partial<SideThreadMessage> = {},
): SideThreadMessage {
  return {
    id: "m-1",
    side_thread_id: "side-1",
    role: "user",
    text: "现在做到哪了？",
    created_at: "2026-01-01T00:00:00.000Z",
    ...overrides,
  };
}

function renderPanel(
  entry: SideThreadEntryState,
  callbacks: {
    composer?: ReactNode;
    onClose?: () => void;
    onResizeStart?: (event: unknown) => void;
    onChangeDraft?: (draft: string) => void;
  } = {},
): HTMLElement {
  return mount(
    createElement(SideThreadPanel, {
      entry,
      mainThreadId: "main-1",
      width: 400,
      composer:
        callbacks.composer ??
        createElement("textarea", { "aria-label": "side composer" }),
      onClose: callbacks.onClose ?? (() => {}),
      onResizeStart: callbacks.onResizeStart ?? (() => {}),
      onChangeDraft: callbacks.onChangeDraft ?? (() => {}),
    }),
  );
}

describe("SideThreadPanel", () => {
  it("renders side history through the canonical turn and message flow", () => {
    const container = renderPanel(
      makeEntry({
        summary: makeSummary(),
        messages: [
          makeMessage({ id: "u-1", role: "user", text: "**进度？**" }),
          makeMessage({
            id: "a-1",
            role: "assistant",
            text: "正在修复 `test/foo.ts`",
            status: "completed",
          }),
        ],
      }),
    );

    expect(container.querySelectorAll(".turn")).toHaveLength(1);
    expect(container.querySelector(".user-message")).toBeTruthy();
    expect(container.querySelector(".assistant-turn-shell")).toBeTruthy();
    expect(container.textContent).toContain("正在修复");
    expect(container.querySelector(".side-thread-panel__message")).toBeNull();
  });

  it("marks a streaming reply in progress without an extra notice", () => {
    const container = renderPanel(
      makeEntry({
        summary: makeSummary({ status: "running" }),
        streaming: true,
        messages: [
          makeMessage({ id: "u-1", role: "user" }),
          makeMessage({
            id: "a-1",
            role: "assistant",
            text: "正在",
            status: "streaming",
          }),
        ],
      }),
    );

    expect(
      container.querySelector('.turn[data-turn-status="in_progress"]'),
    ).toBeTruthy();
    expect(container.textContent).not.toContain("侧聊回复中");
  });

  it("renders the supplied canonical composer inside the panel", () => {
    const container = renderPanel(makeEntry(), {
      composer: createElement("div", { "data-testid": "shared-composer" }),
    });
    expect(container.querySelector('[data-testid="shared-composer"]')).toBeTruthy();
  });

  it("exposes safe focus control for the embedded composer", () => {
    const ref = createRef<SideThreadPanelHandle>();
    const container = mount(
      createElement(SideThreadPanel, {
        ref,
        entry: makeEntry(),
        mainThreadId: "main-1",
        width: 400,
        composer: createElement("textarea", { "aria-label": "side composer" }),
        onClose: () => {},
        onResizeStart: () => {},
        onChangeDraft: () => {},
      }),
    );
    act(() => ref.current?.focusComposer());
    expect(document.activeElement).toBe(container.querySelector("textarea"));
  });

  it("keeps shell actions, errors, and resize semantics", () => {
    const onClose = vi.fn();
    const container = renderPanel(makeEntry({ lastError: "rate limited" }), {
      onClose,
    });
    act(() => {
      container.querySelector<HTMLButtonElement>(
        ".side-thread-panel__close",
      )?.click();
    });
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(container.querySelector('[role="alert"]')?.textContent).toBe(
      "rate limited",
    );
    expect(
      container
        .querySelector(".side-thread-panel__resizer")
        ?.getAttribute("aria-valuenow"),
    ).toBe("400");
  });
});
