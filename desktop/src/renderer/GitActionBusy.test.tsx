import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ServerEvent } from "../shared/protocol";
import { useGitActionBusy } from "./GitActionBusy";

const originalWuu = (window as unknown as { wuu?: unknown }).wuu;
let root: Root | null = null;
let container: HTMLElement | null = null;

afterEach(() => {
  act(() => root?.unmount());
  root = null;
  container?.remove();
  container = null;
  if (originalWuu === undefined) {
    delete (window as unknown as { wuu?: unknown }).wuu;
  } else {
    Object.defineProperty(window, "wuu", {
      configurable: true,
      value: originalWuu,
    });
  }
});

describe("useGitActionBusy", () => {
  it("refreshes on server exit and ignores the superseded response", async () => {
    const resolvers: Array<(busy: boolean) => void> = [];
    const gitActionBusy = vi.fn(
      () => new Promise<boolean>((resolve) => resolvers.push(resolve)),
    );
    let emitServerEvent: ((event: ServerEvent) => void) | undefined;
    const off = vi.fn();
    Object.defineProperty(window, "wuu", {
      configurable: true,
      value: {
        gitActionBusy,
        onServerEvent: (listener: (event: ServerEvent) => void) => {
          emitServerEvent = listener;
          return off;
        },
      },
    });

    let busy = false;
    function Harness(): null {
      busy = useGitActionBusy(
        { kind: "project", project_id: "project-1", cwd: "/repo" },
        "thread-1",
      );
      return null;
    }
    container = document.createElement("div");
    document.body.appendChild(container);
    act(() => {
      root = createRoot(container!);
      root.render(<Harness />);
    });

    expect(busy).toBe(true);
    expect(gitActionBusy).toHaveBeenCalledTimes(1);

    act(() => {
      emitServerEvent?.({
        kind: "server-exit",
        workdir: "/repo",
        code: 1,
        message: "crashed",
      });
    });
    expect(gitActionBusy).toHaveBeenCalledTimes(2);
    expect(busy).toBe(true);

    await act(async () => {
      resolvers[1]?.(false);
      await Promise.resolve();
    });
    expect(busy).toBe(false);

    await act(async () => {
      resolvers[0]?.(true);
      await Promise.resolve();
    });
    expect(busy).toBe(false);
  });
});
