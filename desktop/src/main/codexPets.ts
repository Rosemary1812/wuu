import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { homedir } from "node:os";
import { isAbsolute, join, normalize, relative } from "node:path";
import type {
  CodexPet,
  CodexPetSettings,
  CodexPetsSnapshot,
  CodexPetState,
} from "../shared/protocol";
import { renderableFileURL } from "./renderableFileURLs";

export const CODEX_PET_CELL_WIDTH = 192;
export const CODEX_PET_CELL_HEIGHT = 208;

export const CODEX_PET_STATES: CodexPetState[] = [
  { id: "idle", label: "Idle", row: 0, frames: 6 },
  { id: "running-right", label: "Run right", row: 1, frames: 8 },
  { id: "running-left", label: "Run left", row: 2, frames: 8 },
  { id: "waving", label: "Waving", row: 3, frames: 4 },
  { id: "jumping", label: "Jumping", row: 4, frames: 5 },
  { id: "failed", label: "Failed", row: 5, frames: 8 },
  { id: "waiting", label: "Waiting", row: 6, frames: 6 },
  { id: "running", label: "Running", row: 7, frames: 6 },
  { id: "review", label: "Review", row: 8, frames: 6 },
];

type CodexPetManifest = {
  id: string;
  displayName: string;
  description: string;
  spritesheetPath: string;
};

type LoadCodexPetsSnapshotOptions = {
  petsDir?: string;
  settings?: CodexPetSettings;
};

export function defaultCodexPetsDir(homeDir: string = homedir()): string {
  return join(homeDir, ".codex", "pets");
}

export function loadCodexPetsSnapshot({
  petsDir = defaultCodexPetsDir(),
  settings = { enabled: false, selected_id: "" },
}: LoadCodexPetsSnapshotOptions = {}): CodexPetsSnapshot {
  const pets: CodexPet[] = [];
  const errors: string[] = [];

  if (existsSync(petsDir)) {
    for (const entry of readdirSync(petsDir, { withFileTypes: true })) {
      if (!entry.isDirectory()) {
        continue;
      }
      const pet = readCodexPet(join(petsDir, entry.name), entry.name, errors);
      if (pet) {
        pets.push(pet);
      }
    }
  }

  pets.sort((left, right) =>
    left.display_name.localeCompare(right.display_name) || left.id.localeCompare(right.id),
  );

  const storedSelectedID = settings.selected_id.trim();
  const selectedPet = pets.find((pet) => pet.id === storedSelectedID) ?? pets[0];
  const selectedID = selectedPet?.id ?? "";

  return {
    home: petsDir,
    pets,
    errors,
    enabled: settings.enabled && selectedID !== "",
    selected_id: selectedID,
  };
}

function readCodexPet(petDir: string, dirName: string, errors: string[]): CodexPet | undefined {
  const manifestPath = join(petDir, "pet.json");
  if (!existsSync(manifestPath)) {
    errors.push(`${dirName}: missing pet.json`);
    return undefined;
  }

  let manifest: CodexPetManifest;
  try {
    manifest = parseCodexPetManifest(readFileSync(manifestPath, "utf8"));
  } catch (error) {
    errors.push(`${dirName}: ${error instanceof Error ? error.message : "invalid pet.json"}`);
    return undefined;
  }

  const spritesheetPath = resolvePetSpritesheetPath(petDir, manifest.spritesheetPath);
  if (!spritesheetPath || !fileExists(spritesheetPath)) {
    errors.push(`${dirName}: missing ${manifest.spritesheetPath}`);
    return undefined;
  }

  return {
    id: manifest.id,
    display_name: manifest.displayName,
    description: manifest.description,
    manifest_path: manifestPath,
    spritesheet_path: spritesheetPath,
    spritesheet_url: renderableFileURL(spritesheetPath),
  };
}

function parseCodexPetManifest(raw: string): CodexPetManifest {
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    throw new Error("pet.json must be valid JSON");
  }
  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
    throw new Error("pet.json must be an object");
  }
  const record = parsed as Record<string, unknown>;
  const id = stringField(record, "id");
  const displayName = stringField(record, "displayName");
  const description = typeof record.description === "string" ? record.description : "";
  const spritesheetPath = stringField(record, "spritesheetPath");
  return { id, displayName, description, spritesheetPath };
}

function stringField(record: Record<string, unknown>, key: string): string {
  const value = record[key];
  if (typeof value !== "string" || value.trim() === "") {
    throw new Error(`pet.json ${key} must be a non-empty string`);
  }
  return value.trim();
}

function resolvePetSpritesheetPath(petDir: string, spritesheetPath: string): string | undefined {
  if (isAbsolute(spritesheetPath)) {
    return undefined;
  }
  const resolved = normalize(join(petDir, spritesheetPath));
  const rel = relative(petDir, resolved);
  if (rel === "" || rel.startsWith("..") || isAbsolute(rel)) {
    return undefined;
  }
  return resolved;
}

function fileExists(path: string): boolean {
  try {
    return statSync(path).isFile();
  } catch {
    return false;
  }
}
