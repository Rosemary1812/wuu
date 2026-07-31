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

function translateOf(element: Element): { x: number; y: number } {
  const match = /translate\((-?[\d.e]+) (-?[\d.e]+)\)/.exec(element.getAttribute("transform") ?? "");
  if (!match) throw new Error(`no translate in ${element.getAttribute("transform")}`);
  return { x: Number(match[1]), y: Number(match[2]) };
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

  it("keeps the grabbed point under the cursor while dragging", () => {
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

    // First open fits the camera to the content: the 2 agents + 1 room
    // cluster spans 360x140 world units, so fit clamps to its 1.6 cap and
    // the reset button shows the live zoom percentage.
    const viewport = container.querySelector(".channel-agent-graph-viewport")!;
    expect(viewport.getAttribute("transform")).toContain("scale(1.6)");
    const controls = container.querySelectorAll<HTMLButtonElement>(".channel-agent-graph-controls button");
    expect(controls[1]?.textContent).toBe("160%");

    const node = container.querySelector<SVGGElement>('[aria-label="Andy"]')!;
    const clickNode = container.querySelector<SVGGElement>('[aria-label="Le"]')!;
    expect(node.querySelector(".channel-agent-graph-hit-target")).not.toBeNull();
    expect(node.querySelector("foreignObject")?.getAttribute("pointer-events")).toBe("none");
    act(() => {
      clickNode.dispatchEvent(pointerEvent("pointerdown", 350, 280));
      clickNode.dispatchEvent(pointerEvent("pointerup", 350, 280));
    });
    expect(onSelectAgent).toHaveBeenCalledWith(agents[1]);
    expect(window.cancelAnimationFrame).not.toHaveBeenCalled();

    // Andy (world 610,280) sits at viewBox ~(768,280) under the fitted
    // camera (scale 1.6, translate ~(-208,-168)). Grabbing its centre moves
    // it with the cursor: viewBox (500,400) -> world (442.5,355).
    act(() => {
      node.dispatchEvent(pointerEvent("pointerdown", 768, 280));
      node.dispatchEvent(pointerEvent("pointermove", 500, 400));
      node.dispatchEvent(pointerEvent("pointerup", 500, 400));
    });

    const first = translateOf(node);
    expect(first.x).toBeCloseTo(442.5, 4);
    expect(first.y).toBeCloseTo(355, 4);
    expect(Array.from(container.querySelectorAll("line")).some((line) =>
      Math.abs(Number(line.getAttribute("x1")) - 442.5) < 0.001 || Math.abs(Number(line.getAttribute("x2")) - 442.5) < 0.001
    )).toBe(true);

    // Grabbing off-centre must not snap the node centre to the cursor: the
    // grab offset (world -7.5,-5) is preserved through the drag.
    act(() => {
      node.dispatchEvent(pointerEvent("pointerdown", 512, 408));
      node.dispatchEvent(pointerEvent("pointermove", 600, 300));
      node.dispatchEvent(pointerEvent("pointerup", 600, 300));
    });

    const second = translateOf(node);
    expect(second.x).toBeCloseTo(497.5, 4);
    expect(second.y).toBeCloseTo(287.5, 4);
    expect(window.cancelAnimationFrame).not.toHaveBeenCalled();

    // Zoom buttons move the percentage.
    act(() => controls[2]?.click());
    expect(controls[1]?.textContent).toBe("200%");
    act(() => controls[0]?.click());
    expect(controls[1]?.textContent).toBe("160%");
  });
});
