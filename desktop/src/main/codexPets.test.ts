import {
  mkdirSync,
  mkdtempSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import {
  CODEX_PET_CELL_HEIGHT,
  CODEX_PET_CELL_WIDTH,
  CODEX_PET_STATES,
  loadCodexPetsSnapshot,
} from "./codexPets";

const roots: string[] = [];

afterEach(() => {
  for (const root of roots.splice(0)) {
    rmSync(root, { recursive: true, force: true });
  }
});

function createPetsRoot(): string {
  const root = mkdtempSync(join(tmpdir(), "wuu-codex-pets-"));
  roots.push(root);
  return root;
}

function writePet(
  root: string,
  id: string,
  overrides: Partial<{
    id: string;
    displayName: string;
    description: string;
    spritesheetPath: string;
  }> = {},
): void {
  const dir = join(root, id);
  mkdirSync(dir, { recursive: true });
  writeFileSync(
    join(dir, "pet.json"),
    `${JSON.stringify(
      {
        id,
        displayName: id,
        description: "",
        spritesheetPath: "spritesheet.webp",
        ...overrides,
      },
      null,
      2,
    )}\n`,
  );
  writeFileSync(join(dir, "spritesheet.webp"), "not-a-real-image");
}

describe("loadCodexPetsSnapshot", () => {
  it("uses the Codex Pets atlas constants", () => {
    expect(CODEX_PET_CELL_WIDTH).toBe(192);
    expect(CODEX_PET_CELL_HEIGHT).toBe(208);
    expect(CODEX_PET_STATES.map((state) => state.id)).toEqual([
      "idle",
      "running-right",
      "running-left",
      "waving",
      "jumping",
      "failed",
      "waiting",
      "running",
      "review",
    ]);
  });

  it("lists valid local pets and reports broken pet directories", () => {
    const root = createPetsRoot();
    writePet(root, "beta", { displayName: "Beta Pet", description: "second" });
    writePet(root, "alpha", { displayName: "Alpha Pet", description: "first" });
    mkdirSync(join(root, "broken"), { recursive: true });
    writeFileSync(join(root, "broken", "pet.json"), "{bad json");

    const snapshot = loadCodexPetsSnapshot({
      petsDir: root,
      settings: { enabled: true, selected_id: "beta" },
    });

    expect(snapshot.home).toBe(root);
    expect(snapshot.enabled).toBe(true);
    expect(snapshot.selected_id).toBe("beta");
    expect(snapshot.pets.map((pet) => pet.id)).toEqual(["alpha", "beta"]);
    expect(snapshot.pets[0]).toMatchObject({
      id: "alpha",
      display_name: "Alpha Pet",
      description: "first",
      spritesheet_path: join(root, "alpha", "spritesheet.webp"),
    });
    expect(snapshot.pets[0]?.spritesheet_url).toMatch(/^wuu-file:\/\/local\//);
    expect(snapshot.errors).toEqual([
      "broken: pet.json must be valid JSON",
    ]);
  });

  it("falls back to the first available pet when the stored selection is gone", () => {
    const root = createPetsRoot();
    writePet(root, "alpha", { displayName: "Alpha Pet" });
    writePet(root, "beta", { displayName: "Beta Pet" });

    const snapshot = loadCodexPetsSnapshot({
      petsDir: root,
      settings: { enabled: true, selected_id: "missing" },
    });

    expect(snapshot.enabled).toBe(true);
    expect(snapshot.selected_id).toBe("alpha");
  });

  it("disables the layer when no valid pets are installed", () => {
    const root = createPetsRoot();

    const snapshot = loadCodexPetsSnapshot({
      petsDir: root,
      settings: { enabled: true, selected_id: "missing" },
    });

    expect(snapshot.enabled).toBe(false);
    expect(snapshot.selected_id).toBe("");
    expect(snapshot.pets).toEqual([]);
  });
});
