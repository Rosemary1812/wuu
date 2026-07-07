/**
 * Tests for JumpToLatestPill — the self-contained "跳到最新" pill (issue #5).
 * The pill owns its scroll listener against the containerRef it is given,
 * shows only when the container is scrolled away from the bottom, and
 * smooth-scrolls that same container (and only it) on click. jsdom does no
 * layout, so scroll geometry is stubbed directly on the container node.
 */
import { act } from "react";
import { createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import { JumpToLatestPill } from "./JumpToLatestPill";

let mountedRoots: Root[] = [];
let mountedContainers: HTMLElement[] = [];

afterEach(() => {
  for (const root of mountedRoots) {
    act(() => {
      root.unmount();
    });
  }
  for (const container of mountedContainers) {
    container.remove();
  }
  mountedRoots = [];
  mountedContainers = [];
});

type ScrollGeometry = {
  scrollHeight: number;
  clientHeight: number;
  scrollTop: number;
};

function scrollContainer(geometry: ScrollGeometry): {
  node: HTMLDivElement;
  scrollTo: ReturnType<typeof vi.fn>;
  setScrollTop: (top: number) => void;
} {
  const node = document.createElement("div");
  document.body.appendChild(node);
  mountedContainers.push(node);
  let scrollTop = geometry.scrollTop;
  Object.defineProperty(node, "scrollHeight", {
    configurable: true,
    get: () => geometry.scrollHeight,
  });
  Object.defineProperty(node, "clientHeight", {
    configurable: true,
    get: () => geometry.clientHeight,
  });
  Object.defineProperty(node, "scrollTop", {
    configurable: true,
    get: () => scrollTop,
    set: (value: number) => {
      scrollTop = value;
    },
  });
  const scrollTo = vi.fn();
  Object.defineProperty(node, "scrollTo", {
    configurable: true,
    value: scrollTo,
  });
  return {
    node,
    scrollTo,
    setScrollTop: (top: number) => {
      scrollTop = top;
    },
  };
}

function mountPill(node: HTMLElement): HTMLElement {
  const host = document.createElement("div");
  node.appendChild(host);
  const root = createRoot(host);
  act(() => {
    root.render(
      createElement(JumpToLatestPill, { containerRef: { current: node } }),
    );
  });
  mountedRoots.push(root);
  return host;
}

describe("JumpToLatestPill", () => {
  it("stays hidden at the bottom and appears when scrolled away", () => {
    const { node, setScrollTop } = scrollContainer({
      scrollHeight: 1000,
      clientHeight: 400,
      scrollTop: 600, // exactly at the bottom
    });
    const host = mountPill(node);
    expect(host.querySelector(".jump-to-latest-pill")).toBeNull();

    act(() => {
      setScrollTop(0); // 600px from the bottom > 80px threshold
      node.dispatchEvent(new Event("scroll"));
    });
    const pill = host.querySelector<HTMLButtonElement>(".jump-to-latest-pill");
    expect(pill).not.toBeNull();
    expect(pill?.className).toContain("jump-to-latest-pill-sticky-centered");
  });

  it("shows immediately when mounted into an already scrolled-away container", () => {
    const { node } = scrollContainer({
      scrollHeight: 1000,
      clientHeight: 400,
      scrollTop: 0,
    });
    const host = mountPill(node);
    expect(host.querySelector(".jump-to-latest-pill")).not.toBeNull();
  });

  it("smooth-scrolls its own container to the bottom on click", () => {
    const { node, scrollTo } = scrollContainer({
      scrollHeight: 1000,
      clientHeight: 400,
      scrollTop: 0,
    });
    const host = mountPill(node);
    act(() => {
      host
        .querySelector<HTMLButtonElement>(".jump-to-latest-pill")
        ?.click();
    });
    expect(scrollTo).toHaveBeenCalledWith({ top: 1000, behavior: "smooth" });
  });

  it("hides again once the container scrolls back within the threshold", () => {
    const { node, setScrollTop } = scrollContainer({
      scrollHeight: 1000,
      clientHeight: 400,
      scrollTop: 0,
    });
    const host = mountPill(node);
    expect(host.querySelector(".jump-to-latest-pill")).not.toBeNull();
    act(() => {
      setScrollTop(560); // 40px from the bottom, inside the 80px threshold
      node.dispatchEvent(new Event("scroll"));
    });
    expect(host.querySelector(".jump-to-latest-pill")).toBeNull();
  });
});
