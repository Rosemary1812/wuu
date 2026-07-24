import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ChannelRoom, NamedAgent } from "../shared/protocol";
import { AgentRelationshipGraph } from "./AgentRelationshipGraph";

function pointerEvent(type: string, x: number, y: number): Event {
  const event = new MouseEvent(type, { bubbles: true, clientX: x, clientY: y });
  Object.defineProperty(event, "pointerId", { value: 1 });
  return event;
}

describe("AgentRelationshipGraph", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
    vi.spyOn(window, "requestAnimationFrame").mockImplementation(() => 1);
    vi.spyOn(window, "cancelAnimationFrame").mockImplementation(() => undefined);
    Object.defineProperty(SVGElement.prototype, "setPointerCapture", { configurable: true, value: vi.fn() });
    Object.defineProperty(SVGElement.prototype, "releasePointerCapture", { configurable: true, value: vi.fn() });
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    vi.restoreAllMocks();
  });

  it("uses a full-height canvas and keeps a dragged node where it is placed", () => {
    const onSelectAgent = vi.fn();
    const agents = [
      { id: "agent-1", name: "Andy", avatar_key: "abstract-1" },
      { id: "agent-2", name: "Le", avatar_key: "abstract-2" },
    ] as NamedAgent[];
    const rooms = [{
      id: "room-1",
      name: "general",
      members: agents.map((agent) => ({ member_type: "agent" as const, member_id: agent.id })),
    }] as ChannelRoom[];

    act(() => root.render(
      <AgentRelationshipGraph
        agents={agents}
        rooms={rooms}
        onSelectAgent={onSelectAgent}
        ariaLabel="Relationship graph"
        zoomInLabel="Zoom in"
        zoomOutLabel="Zoom out"
        resetViewLabel="Reset"
      />,
    ));

    expect(container.querySelector(".channel-agent-graph-toolbar")).toBeNull();
    expect(container.querySelector(".channel-agent-graph-settings-toggle")).not.toBeNull();

    const svg = container.querySelector<SVGSVGElement>(".channel-agent-graph-canvas")!;
    vi.spyOn(svg, "getBoundingClientRect").mockReturnValue({
      left: 0, top: 0, width: 960, height: 560, right: 960, bottom: 560, x: 0, y: 0, toJSON: () => ({}),
    });
    const node = container.querySelector<SVGGElement>('[aria-label="Andy"]')!;
    const clickNode = container.querySelector<SVGGElement>('[aria-label="Le"]')!;
    act(() => {
      clickNode.dispatchEvent(pointerEvent("pointerdown", 350, 280));
      clickNode.dispatchEvent(pointerEvent("pointerup", 350, 280));
    });
    expect(onSelectAgent).toHaveBeenCalledWith(agents[1]);
    expect(window.cancelAnimationFrame).not.toHaveBeenCalled();

    act(() => {
      node.dispatchEvent(pointerEvent("pointerdown", 610, 280));
      node.dispatchEvent(pointerEvent("pointermove", 200, 150));
      node.dispatchEvent(pointerEvent("pointerup", 200, 150));
    });

    expect(node.getAttribute("transform")).toBe("translate(200 150)");
    expect(Array.from(container.querySelectorAll("line")).some((line) =>
      line.getAttribute("x1") === "200" || line.getAttribute("x2") === "200"
    )).toBe(true);
  });
});
