import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { NamedAgent } from "../shared/protocol";
import { ChannelMemberPicker } from "./ChannelMemberPicker";

const agents: NamedAgent[] = [
  { id: "agent-1", name: "Alpha", memory_dir: "/alpha", avatar_key: "abstract-1", model_override: "gpt-5", autostart: true, created_at: "2026-08-01T00:00:00Z" },
  { id: "agent-2", name: "Beta", memory_dir: "/beta", avatar_key: "abstract-2", model_override: "", autostart: true, created_at: "2026-08-01T00:00:00Z" },
  { id: "agent-3", name: "Gamma", memory_dir: "/gamma", avatar_key: "abstract-3", model_override: "gpt-5", autostart: true, created_at: "2026-08-01T00:00:00Z" },
  { id: "agent-4", name: "Delta", memory_dir: "/delta", avatar_key: "abstract-4", model_override: "gpt-5", autostart: true, created_at: "2026-08-01T00:00:00Z" },
];

let container: HTMLDivElement;
let root: Root | null = null;

function setInputValue(input: HTMLInputElement, value: string): void {
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
  setter?.call(input, value);
  input.dispatchEvent(new Event("input", { bubbles: true }));
}

beforeEach(() => {
  container = document.createElement("div");
  document.body.appendChild(container);
});

afterEach(() => {
  act(() => root?.unmount());
  root = null;
  container.remove();
});

describe("ChannelMemberPicker", () => {
  it("renders a flat multi-select list without native checkboxes", () => {
    const onToggle = vi.fn();
    root = createRoot(container);
    act(() => root?.render(<ChannelMemberPicker agents={agents} selectedAgentIDs={["agent-2"]} onToggle={onToggle} />));

    const listbox = container.querySelector('[role="listbox"]');
    const options = container.querySelectorAll<HTMLButtonElement>('[role="option"]');
    expect(listbox?.getAttribute("aria-multiselectable")).toBe("true");
    expect(options).toHaveLength(4);
    expect(container.querySelector('input[type="checkbox"]')).toBeNull();
    expect(options[1]?.getAttribute("aria-selected")).toBe("true");
    expect(options[1]?.querySelector(".channel-member-picker-check")).not.toBeNull();

    act(() => options[0]?.click());
    expect(onToggle).toHaveBeenCalledWith("agent-1");
  });

  it("filters agents and moves focus from search into the results", () => {
    root = createRoot(container);
    act(() => root?.render(<ChannelMemberPicker agents={agents} selectedAgentIDs={[]} onToggle={() => {}} />));

    const search = container.querySelector<HTMLInputElement>('input[type="search"]')!;
    act(() => setInputValue(search, "gam"));
    const options = container.querySelectorAll<HTMLButtonElement>('[role="option"]');
    expect(options).toHaveLength(1);
    expect(options[0]?.textContent).toContain("Gamma");

    act(() => search.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowDown", bubbles: true })));
    expect(document.activeElement).toBe(options[0]);
  });
});
