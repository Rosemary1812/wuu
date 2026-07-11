import { describe, expect, it } from "vitest";
import type { CodexPetsSnapshot } from "../shared/protocol";
import {
  codexPetActionFromURL,
  codexPetStateForRuntime,
  codexPetView,
  codexPetWindowHTML,
  selectedCodexPet,
} from "./codexPetWindow";

function snapshot(enabled: boolean, selectedID = "alpha"): CodexPetsSnapshot {
  return {
    home: "/tmp/pets",
    enabled,
    selected_id: selectedID,
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
    expect(codexPetStateForRuntime({ running: false, status: "queued" }).id).toBe("waiting");
  });
});

describe("selectedCodexPet", () => {
  it("returns nothing while pets are disabled", () => {
    expect(selectedCodexPet(snapshot(false))).toBeUndefined();
    expect(selectedCodexPet(undefined)).toBeUndefined();
  });

  it("falls back to the first pet when the selection is stale", () => {
    expect(selectedCodexPet(snapshot(true, "missing"))?.id).toBe("alpha");
  });
});

describe("codexPetView", () => {
  it("derives spritesheet animation variables from the atlas state", () => {
    const view = codexPetView(
      snapshot(true).pets[0],
      codexPetStateForRuntime({ running: true, status: "" }),
    );
    expect(view.spritesheetURL).toBe("wuu-file://local/alpha");
    expect(view.y).toBe(-1456);
    expect(view.endX).toBe(-1152);
    expect(view.frames).toBe(6);
    expect(view.duration).toBe(1560);
    expect(view.label).toBe("Alpha Pet running");
  });
});

describe("codexPetWindowHTML", () => {
  const html = codexPetWindowHTML(
    codexPetView(
      snapshot(true).pets[0],
      codexPetStateForRuntime({ running: false, status: "" }),
    ),
  );

  it("embeds the spritesheet and animation variables inline", () => {
    expect(html).toContain("wuu-file://local/alpha");
    expect(html).toContain("--pet-frames:6");
    expect(html).toContain("steps(var(--pet-frames))");
  });

  it("keeps the window draggable and offers the context menu action", () => {
    expect(html).toContain("pointerdown");
    expect(html).toContain("window.moveTo");
    expect(html).toContain("wuu-pet://action/menu");
  });

  it("locks the page down to spritesheet images and inline assets", () => {
    expect(html).toContain("default-src 'none'");
    expect(html).toContain("img-src wuu-file:");
  });
});

describe("codexPetActionFromURL", () => {
  it("accepts only the pet menu action", () => {
    expect(codexPetActionFromURL("wuu-pet://action/menu")).toBe("menu");
    expect(codexPetActionFromURL("wuu-pet://action/close")).toBeUndefined();
    expect(codexPetActionFromURL("wuu-cua://action/menu")).toBeUndefined();
    expect(codexPetActionFromURL("not a url")).toBeUndefined();
  });
});
