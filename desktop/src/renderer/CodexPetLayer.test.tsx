import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import type { CodexPetsSnapshot } from "../shared/protocol";
import { CodexPetLayer, codexPetStateForRuntime } from "./CodexPetLayer";

let container: HTMLDivElement;
let root: Root | null = null;

beforeEach(() => {
  container = document.createElement("div");
  document.body.appendChild(container);
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  root = null;
  container.remove();
});

function renderLayer(snapshot: CodexPetsSnapshot | undefined, running: boolean, status = "ready"): void {
  act(() => {
    root = createRoot(container);
    root!.render(
      <CodexPetLayer
        snapshot={snapshot}
        running={running}
        status={status}
      />,
    );
  });
}

function snapshot(enabled: boolean): CodexPetsSnapshot {
  return {
    home: "/tmp/pets",
    enabled,
    selected_id: "alpha",
    errors: [],
    pets: [
      {
        id: "alpha",
        display_name: "Alpha Pet",
        description: "",
        manifest_path: "/tmp/pets/alpha/pet.json",
        spritesheet_path: "/tmp/pets/alpha/spritesheet.webp",
        spritesheet_url: "wuu-file://local/alpha",
      },
    ],
  };
}

describe("codexPetStateForRuntime", () => {
  it("maps app runtime state onto Codex Pets atlas states", () => {
    expect(codexPetStateForRuntime({ running: false, status: "ready" }).id).toBe("idle");
    expect(codexPetStateForRuntime({ running: true, status: "正在发送请求" }).id).toBe("running");
    expect(codexPetStateForRuntime({ running: false, status: "等待权限确认" }).id).toBe("review");
    expect(codexPetStateForRuntime({ running: false, status: "send failed" }).id).toBe("failed");
  });
});

describe("CodexPetLayer", () => {
  it("does not render when no pet is enabled", () => {
    renderLayer(snapshot(false), false);
    expect(container.querySelector(".codex-pet-layer")).toBeNull();
  });

  it("renders the selected pet with spritesheet animation variables", () => {
    renderLayer(snapshot(true), true);

    const sprite = container.querySelector<HTMLElement>(".codex-pet-sprite");
    expect(sprite).not.toBeNull();
    expect(sprite?.getAttribute("aria-label")).toBe("Alpha Pet running");
    expect(sprite?.style.backgroundImage).toContain("wuu-file://local/alpha");
    expect(sprite?.style.getPropertyValue("--codex-pet-y")).toBe("-1456px");
    expect(sprite?.style.getPropertyValue("--codex-pet-frames")).toBe("6");
  });
});
