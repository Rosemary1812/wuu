import { Settings2 } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type PointerEvent as ReactPointerEvent, type WheelEvent as ReactWheelEvent } from "react";
import type { ChannelRoom, NamedAgent } from "../shared/protocol";
import { AgentAvatarMark } from "./AgentAvatarMark";
import { useI18n } from "./i18n";

const GRAPH_WIDTH = 960;
const GRAPH_HEIGHT = 560;

export function graphDensityScale(nodeCount: number): number {
  return Math.max(0.68, Math.min(1.35, Math.sqrt(6 / Math.max(nodeCount, 1))));
}

type GraphNode = {
  id: string;
  kind: "agent" | "room";
  label: string;
  avatarKey?: string;
  avatarImage?: string;
  status?: "idle" | "thinking";
  x: number;
  y: number;
  vx: number;
  vy: number;
  radius: number;
  pinned: boolean;
};

type GraphLink = {
  id: string;
  kind: "relationship" | "membership";
  source: GraphNode;
  target: GraphNode;
  weight: number;
};

function buildGraph(agents: NamedAgent[], rooms: ChannelRoom[]): { nodes: GraphNode[]; links: GraphLink[] } {
  const nodes: GraphNode[] = [];
  const byID = new Map<string, GraphNode>();
  agents.forEach((agent, index) => {
    const angle = (index / Math.max(agents.length, 1)) * Math.PI * 2;
    const node: GraphNode = {
      id: `agent:${agent.id}`,
      kind: "agent",
      label: agent.name,
      avatarKey: agent.avatar_key,
      avatarImage: agent.avatar_image,
      status: agent.activity_status === "thinking" ? "thinking" : "idle",
      x: GRAPH_WIDTH / 2 + Math.cos(angle) * 130,
      y: GRAPH_HEIGHT / 2 + Math.sin(angle) * 100,
      vx: 0,
      vy: 0,
      radius: 24,
      pinned: false,
    };
    nodes.push(node);
    byID.set(node.id, node);
  });
  const linkedRooms = rooms.filter((room) => room.members.filter((member) => member.member_type === "agent" && byID.has(`agent:${member.member_id}`)).length >= 2);
  linkedRooms.forEach((room, index) => {
    const angle = ((index + 0.5) / Math.max(linkedRooms.length, 1)) * Math.PI * 2;
    const node: GraphNode = {
      id: `room:${room.id}`,
      kind: "room",
      label: `# ${room.name}`,
      x: GRAPH_WIDTH / 2 + Math.cos(angle) * 230,
      y: GRAPH_HEIGHT / 2 + Math.sin(angle) * 140,
      vx: 0,
      vy: 0,
      radius: 8,
      pinned: false,
    };
    nodes.push(node);
    byID.set(node.id, node);
  });
  const links: GraphLink[] = [];
  const relationships = new Map<string, GraphLink>();
  for (const room of linkedRooms) {
    const target = byID.get(`room:${room.id}`);
    if (!target) continue;
    const members = room.members
      .filter((member) => member.member_type === "agent")
      .map((member) => byID.get(`agent:${member.member_id}`))
      .filter((node): node is GraphNode => Boolean(node));
    for (const source of members) {
      links.push({ id: `membership:${source.id}:${target.id}`, kind: "membership", source, target, weight: 1 });
    }
    for (let leftIndex = 0; leftIndex < members.length; leftIndex++) {
      for (let rightIndex = leftIndex + 1; rightIndex < members.length; rightIndex++) {
        const pair = [members[leftIndex], members[rightIndex]].sort((left, right) => left.id.localeCompare(right.id));
        const id = `relationship:${pair[0].id}:${pair[1].id}`;
        const existing = relationships.get(id);
        if (existing) existing.weight += 1;
        else relationships.set(id, { id, kind: "relationship", source: pair[0], target: pair[1], weight: 1 });
      }
    }
  }
  return { nodes, links: [...relationships.values(), ...links] };
}

export function AgentRelationshipGraph({ agents, rooms, onSelectAgent, ariaLabel, zoomInLabel, zoomOutLabel, resetViewLabel }: {
  agents: NamedAgent[];
  rooms: ChannelRoom[];
  onSelectAgent: (agent: NamedAgent) => void;
  ariaLabel: string;
  zoomInLabel: string;
  zoomOutLabel: string;
  resetViewLabel: string;
}): JSX.Element {
  const { t } = useI18n();
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [settings, setSettings] = useState({ repulsion: 2800, linkLength: 136, centerForce: 0.015, nodeScale: 1, showLabels: true });
  const settingsRef = useRef(settings);
  const agentSignature = agents.map((agent) => `${agent.id}:${agent.name}:${agent.avatar_key}:${agent.activity_status}`).join("|");
  const roomSignature = rooms.map((room) => `${room.id}:${room.name}:${room.members.map((member) => `${member.member_type}:${member.member_id}`).join(",")}`).join("|");
  const graph = useMemo(() => buildGraph(agents, rooms), [agentSignature, roomSignature]);
  const densityScale = graphDensityScale(graph.nodes.length);
  const nodeRefs = useRef(new Map<string, SVGGElement>());
  const linkRefs = useRef(new Map<string, SVGLineElement>());
  const frameRef = useRef<number | null>(null);
  const viewportElementRef = useRef<SVGGElement | null>(null);
  const viewportRef = useRef({ x: 0, y: 0, scale: 1 });
  const restartRef = useRef<() => void>(() => undefined);
  const paintRef = useRef<() => void>(() => undefined);
  const dragRef = useRef<{ node: GraphNode; pointerID: number; moved: boolean } | null>(null);
  const panRef = useRef<{ pointerID: number; x: number; y: number; originX: number; originY: number } | null>(null);

  useEffect(() => {
    let alpha = 1;
    let stopped = false;
    const paint = (): void => {
      for (const link of graph.links) {
        const element = linkRefs.current.get(link.id);
        if (!element) continue;
        element.setAttribute("x1", String(link.source.x));
        element.setAttribute("y1", String(link.source.y));
        element.setAttribute("x2", String(link.target.x));
        element.setAttribute("y2", String(link.target.y));
      }
      for (const node of graph.nodes) {
        nodeRefs.current.get(node.id)?.setAttribute("transform", `translate(${node.x} ${node.y})`);
      }
    };
    paintRef.current = paint;
    const tick = (): void => {
      frameRef.current = null;
      if (stopped) return;
      for (let leftIndex = 0; leftIndex < graph.nodes.length; leftIndex++) {
        const left = graph.nodes[leftIndex];
        for (let rightIndex = leftIndex + 1; rightIndex < graph.nodes.length; rightIndex++) {
          const right = graph.nodes[rightIndex];
          let dx = right.x - left.x;
          let dy = right.y - left.y;
          const distanceSquared = Math.max(dx * dx + dy * dy, 16);
          const distance = Math.sqrt(distanceSquared);
          dx /= distance;
          dy /= distance;
          const repulsionStrength = (left.kind === "agent" && right.kind === "agent" ? settingsRef.current.repulsion : settingsRef.current.repulsion * 0.32) * densityScale;
          const repulsion = (repulsionStrength * alpha) / distanceSquared;
          left.vx -= dx * repulsion;
          left.vy -= dy * repulsion;
          right.vx += dx * repulsion;
          right.vy += dy * repulsion;
          const overlap = (left.radius + right.radius) * settingsRef.current.nodeScale * densityScale + 16 - distance;
          if (overlap > 0) {
            left.vx -= dx * overlap * 0.025;
            left.vy -= dy * overlap * 0.025;
            right.vx += dx * overlap * 0.025;
            right.vy += dy * overlap * 0.025;
          }
        }
      }
      for (const link of graph.links) {
        const dx = link.target.x - link.source.x;
        const dy = link.target.y - link.source.y;
        const distance = Math.max(Math.hypot(dx, dy), 1);
        const densityLengthScale = Math.max(0.76, Math.min(1.15, densityScale));
        const desiredLength = link.kind === "relationship"
          ? Math.max(64, (settingsRef.current.linkLength - link.weight * 12) * densityLengthScale)
          : settingsRef.current.linkLength * 0.78 * densityLengthScale;
        const strength = link.kind === "relationship" ? 0.0038 : 0.0012;
        const pull = (distance - desiredLength) * strength * alpha;
        link.source.vx += (dx / distance) * pull;
        link.source.vy += (dy / distance) * pull;
        link.target.vx -= (dx / distance) * pull;
        link.target.vy -= (dy / distance) * pull;
      }
      for (const node of graph.nodes) {
        if (dragRef.current?.node === node || node.pinned) {
          node.vx = 0;
          node.vy = 0;
          continue;
        }
        node.vx += (GRAPH_WIDTH / 2 - node.x) * settingsRef.current.centerForce * 0.047 * alpha;
        node.vy += (GRAPH_HEIGHT / 2 - node.y) * settingsRef.current.centerForce * 0.06 * alpha;
        node.vx *= 0.88;
        node.vy *= 0.88;
        node.x = Math.max(56, Math.min(GRAPH_WIDTH - 56, node.x + node.vx));
        node.y = Math.max(44, Math.min(GRAPH_HEIGHT - 44, node.y + node.vy));
      }
      paint();
      alpha *= 0.975;
      if (alpha > 0.018 || dragRef.current) frameRef.current = window.requestAnimationFrame(tick);
    };
    restartRef.current = () => {
      alpha = Math.max(alpha, 0.42);
      if (frameRef.current === null) frameRef.current = window.requestAnimationFrame(tick);
    };
    paint();
    restartRef.current();
    return () => {
      stopped = true;
      if (frameRef.current !== null) window.cancelAnimationFrame(frameRef.current);
      frameRef.current = null;
    };
  }, [graph]);

  function updateSettings(next: typeof settings): void {
    settingsRef.current = next;
    setSettings(next);
    restartRef.current();
  }

  function pointFromEvent(event: ReactPointerEvent<SVGGElement>): { x: number; y: number } {
    const bounds = event.currentTarget.ownerSVGElement?.getBoundingClientRect();
    if (!bounds || bounds.width === 0 || bounds.height === 0) return { x: GRAPH_WIDTH / 2, y: GRAPH_HEIGHT / 2 };
    const rawX = ((event.clientX - bounds.left) / bounds.width) * GRAPH_WIDTH;
    const rawY = ((event.clientY - bounds.top) / bounds.height) * GRAPH_HEIGHT;
    const viewport = viewportRef.current;
    return { x: (rawX - viewport.x) / viewport.scale, y: (rawY - viewport.y) / viewport.scale };
  }

  function applyViewport(): void {
    const { x, y, scale } = viewportRef.current;
    viewportElementRef.current?.setAttribute("transform", `translate(${x} ${y}) scale(${scale})`);
  }

  function zoomAt(nextScale: number, centerX: number, centerY: number): void {
    const viewport = viewportRef.current;
    const scale = Math.max(0.55, Math.min(2.5, nextScale));
    const worldX = (centerX - viewport.x) / viewport.scale;
    const worldY = (centerY - viewport.y) / viewport.scale;
    viewportRef.current = { x: centerX - worldX * scale, y: centerY - worldY * scale, scale };
    applyViewport();
  }

  function handleWheel(event: ReactWheelEvent<SVGSVGElement>): void {
    event.preventDefault();
    const bounds = event.currentTarget.getBoundingClientRect();
    if (bounds.width === 0 || bounds.height === 0) return;
    const x = ((event.clientX - bounds.left) / bounds.width) * GRAPH_WIDTH;
    const y = ((event.clientY - bounds.top) / bounds.height) * GRAPH_HEIGHT;
    zoomAt(viewportRef.current.scale * Math.exp(-event.deltaY * 0.0015), x, y);
  }

  function handleCanvasPointerDown(event: ReactPointerEvent<SVGSVGElement>): void {
    if (event.target !== event.currentTarget) return;
    event.currentTarget.setPointerCapture(event.pointerId);
    panRef.current = {
      pointerID: event.pointerId,
      x: event.clientX,
      y: event.clientY,
      originX: viewportRef.current.x,
      originY: viewportRef.current.y,
    };
  }

  function handleCanvasPointerMove(event: ReactPointerEvent<SVGSVGElement>): void {
    const pan = panRef.current;
    if (!pan || pan.pointerID !== event.pointerId) return;
    const bounds = event.currentTarget.getBoundingClientRect();
    viewportRef.current.x = pan.originX + ((event.clientX - pan.x) / Math.max(bounds.width, 1)) * GRAPH_WIDTH;
    viewportRef.current.y = pan.originY + ((event.clientY - pan.y) / Math.max(bounds.height, 1)) * GRAPH_HEIGHT;
    applyViewport();
  }

  function handleCanvasPointerUp(event: ReactPointerEvent<SVGSVGElement>): void {
    if (panRef.current?.pointerID !== event.pointerId) return;
    event.currentTarget.releasePointerCapture(event.pointerId);
    panRef.current = null;
  }

  function handlePointerDown(event: ReactPointerEvent<SVGGElement>, node: GraphNode): void {
    event.currentTarget.setPointerCapture(event.pointerId);
    dragRef.current = { node, pointerID: event.pointerId, moved: false };
  }

  function handlePointerMove(event: ReactPointerEvent<SVGGElement>): void {
    const drag = dragRef.current;
    if (!drag || drag.pointerID !== event.pointerId) return;
    if (!drag.moved) {
      drag.node.pinned = true;
      drag.node.vx = 0;
      drag.node.vy = 0;
      if (frameRef.current !== null) {
        window.cancelAnimationFrame(frameRef.current);
        frameRef.current = null;
      }
    }
    const point = pointFromEvent(event);
    drag.node.x = point.x;
    drag.node.y = point.y;
    drag.moved = true;
    paintRef.current();
  }

  function handlePointerUp(event: ReactPointerEvent<SVGGElement>): void {
    const drag = dragRef.current;
    if (!drag || drag.pointerID !== event.pointerId) return;
    event.currentTarget.releasePointerCapture(event.pointerId);
    dragRef.current = null;
    if (!drag.moved && drag.node.kind === "agent") {
      const agent = agents.find((candidate) => `agent:${candidate.id}` === drag.node.id);
      if (agent) onSelectAgent(agent);
    }
  }

  function resetGraph(): void {
    viewportRef.current = { x: 0, y: 0, scale: 1 };
    applyViewport();
    for (const node of graph.nodes) {
      node.pinned = false;
      node.vx = 0;
      node.vy = 0;
    }
    restartRef.current();
  }

  return (
    <div className="channel-agent-graph-shell">
      <div className="channel-agent-graph-surface">
      <button className="icon-button channel-agent-graph-settings-toggle" type="button" aria-label={t("channels.graphSettings")} aria-expanded={settingsOpen} onClick={() => setSettingsOpen((open) => !open)}><Settings2 className="icon" /></button>
      <svg
        className="channel-agent-graph-canvas"
        viewBox={`0 0 ${GRAPH_WIDTH} ${GRAPH_HEIGHT}`}
        role="img"
        aria-label={ariaLabel}
        onWheel={handleWheel}
        onPointerDown={handleCanvasPointerDown}
        onPointerMove={handleCanvasPointerMove}
        onPointerUp={handleCanvasPointerUp}
        onPointerCancel={handleCanvasPointerUp}
        onDoubleClick={(event) => { if (event.target === event.currentTarget) resetGraph(); }}
      >
        <g ref={viewportElementRef}>
          <g className="channel-agent-graph-links">
            {graph.links.map((link) => <line className={link.kind} key={link.id} ref={(element) => { if (element) linkRefs.current.set(link.id, element); }} />)}
          </g>
          <g className="channel-agent-graph-nodes">
        {graph.nodes.map((node) => (
          <g
            className={`channel-agent-graph-node ${node.kind}`}
            key={node.id}
            ref={(element) => { if (element) nodeRefs.current.set(node.id, element); }}
            role={node.kind === "agent" ? "button" : undefined}
            tabIndex={node.kind === "agent" ? 0 : undefined}
            aria-label={node.kind === "agent" ? node.label : undefined}
            onPointerDown={(event) => handlePointerDown(event, node)}
            onPointerMove={handlePointerMove}
            onPointerUp={handlePointerUp}
            onPointerCancel={handlePointerUp}
            onDragStart={(event) => event.preventDefault()}
            onKeyDown={(event) => {
              if (node.kind !== "agent" || (event.key !== "Enter" && event.key !== " ")) return;
              event.preventDefault();
              const agent = agents.find((candidate) => `agent:${candidate.id}` === node.id);
              if (agent) onSelectAgent(agent);
            }}
          >
            <g transform={`scale(${settings.nodeScale * densityScale})`}>
              {node.kind === "agent" ? (
              <>
                <circle className="channel-agent-graph-hit-target" r="26" />
                <foreignObject x="-18" y="-18" width="36" height="36" pointerEvents="none">
                  <div className="channel-agent-graph-avatar"><AgentAvatarMark avatarKey={node.avatarKey ?? "abstract-1"} avatarImage={node.avatarImage} /></div>
                </foreignObject>
                <circle className={`channel-agent-graph-status ${node.status}`} cx="15" cy="-15" r="3.5" />
              </>
              ) : <circle r="6" />}
              {settings.showLabels ? <text y={node.kind === "agent" ? 34 : 22} textAnchor="middle">{node.label}</text> : null}
            </g>
          </g>
        ))}
          </g>
        </g>
      </svg>
      {settingsOpen ? (
        <aside className="channel-agent-graph-settings">
          <strong>{t("channels.graphSettings")}</strong>
          <label><span>{t("channels.nodeRepulsion")} <output>{settings.repulsion}</output></span><input type="range" min="800" max="6000" step="100" value={settings.repulsion} onChange={(event) => updateSettings({ ...settings, repulsion: Number(event.target.value) })} /></label>
          <label><span>{t("channels.linkLength")} <output>{settings.linkLength}</output></span><input type="range" min="70" max="240" step="5" value={settings.linkLength} onChange={(event) => updateSettings({ ...settings, linkLength: Number(event.target.value) })} /></label>
          <label><span>{t("channels.centerForce")} <output>{settings.centerForce.toFixed(3)}</output></span><input type="range" min="0" max="0.03" step="0.001" value={settings.centerForce} onChange={(event) => updateSettings({ ...settings, centerForce: Number(event.target.value) })} /></label>
          <label><span>{t("channels.nodeSize")} <output>{settings.nodeScale.toFixed(1)}x</output></span><input type="range" min="0.7" max="1.5" step="0.1" value={settings.nodeScale} onChange={(event) => updateSettings({ ...settings, nodeScale: Number(event.target.value) })} /></label>
          <label className="channel-agent-graph-check"><input type="checkbox" checked={settings.showLabels} onChange={(event) => updateSettings({ ...settings, showLabels: event.target.checked })} /><span>{t("channels.showLabels")}</span></label>
        </aside>
      ) : null}
      <div className="channel-agent-graph-controls">
        <button type="button" aria-label={zoomOutLabel} onClick={() => zoomAt(viewportRef.current.scale / 1.25, GRAPH_WIDTH / 2, GRAPH_HEIGHT / 2)}>−</button>
        <button type="button" aria-label={resetViewLabel} onClick={resetGraph}>1:1</button>
        <button type="button" aria-label={zoomInLabel} onClick={() => zoomAt(viewportRef.current.scale * 1.25, GRAPH_WIDTH / 2, GRAPH_HEIGHT / 2)}>+</button>
      </div>
      </div>
    </div>
  );
}
