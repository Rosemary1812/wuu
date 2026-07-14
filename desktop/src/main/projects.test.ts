import {
  chmod,
  mkdir,
  mkdtemp,
  readdir,
  readFile,
  rm,
  stat,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { DesktopProject } from "../shared/protocol";

const electronMock = vi.hoisted(() => ({
  userDataPath: "",
}));

vi.mock("electron", () => ({
  app: {
    getPath: vi.fn(() => electronMock.userDataPath),
  },
}));

import { ProjectManager } from "./projects";

let home: string;
let legacyUserData: string;
let originalWuuHome: string | undefined;

beforeEach(async () => {
  home = await mkdtemp(join(tmpdir(), "wuu-projects-home-"));
  legacyUserData = await mkdtemp(join(tmpdir(), "wuu-projects-legacy-"));
  electronMock.userDataPath = legacyUserData;
  originalWuuHome = process.env.WUU_HOME;
  process.env.WUU_HOME = home;
});

afterEach(async () => {
  if (originalWuuHome === undefined) {
    delete process.env.WUU_HOME;
  } else {
    process.env.WUU_HOME = originalWuuHome;
  }
  await rm(home, { recursive: true, force: true });
  await rm(legacyUserData, { recursive: true, force: true });
});

function project(id: string, name: string, path: string): DesktopProject {
  return {
    id,
    name,
    path,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

async function writeStore(path: string, projects: DesktopProject[]): Promise<void> {
  await mkdir(dirname(path), { recursive: true });
  await writeFile(
    path,
    `${JSON.stringify({ projects }, null, 2)}\n`,
    "utf8",
  );
}

async function createProjectDir(name: string): Promise<string> {
  const path = join(home, "workspaces", name);
  await mkdir(path, { recursive: true });
  return path;
}

function canonicalStorePath(): string {
  return join(home, "projects.json");
}

function legacyStorePath(): string {
  return join(legacyUserData, "projects.json");
}

async function pathExists(path: string): Promise<boolean> {
  try {
    await stat(path);
    return true;
  } catch {
    return false;
  }
}

async function legacyArchiveNames(): Promise<string[]> {
  return (await readdir(legacyUserData)).filter((name) =>
    name.startsWith("projects.json.migrated"),
  );
}

async function expectRejectedCanonicalStore(
  contents: string,
  expectedError: string,
): Promise<void> {
  const legacyPath = await createProjectDir("legacy");
  await writeFile(canonicalStorePath(), contents, "utf8");
  await writeStore(legacyStorePath(), [
    project("legacy", "legacy", legacyPath),
  ]);
  const legacyContents = await readFile(legacyStorePath(), "utf8");

  let loadError: unknown;
  try {
    new ProjectManager().load();
  } catch (error) {
    loadError = error;
  }

  expect(loadError).toBeInstanceOf(Error);
  expect((loadError as Error).message).toContain(expectedError);
  expect((loadError as Error).message).toContain(canonicalStorePath());
  expect(await readFile(canonicalStorePath(), "utf8")).toBe(contents);
  expect(await readFile(legacyStorePath(), "utf8")).toBe(legacyContents);
  expect(await legacyArchiveNames()).toEqual([]);
}

describe("ProjectManager project store migration", () => {
  it("imports and archives the legacy project store when the canonical store is missing", async () => {
    const legacyPath = await createProjectDir("legacy");
    const legacyProject = project("legacy", "legacy", legacyPath);
    await writeStore(legacyStorePath(), [legacyProject]);

    const manager = new ProjectManager();
    manager.load();

    expect(manager.list().projects.map((item) => item.id)).toEqual(["legacy"]);
    expect(
      JSON.parse(await readFile(canonicalStorePath(), "utf8")).projects,
    ).toHaveLength(1);
    expect(await pathExists(legacyStorePath())).toBe(false);
    expect(await legacyArchiveNames()).toHaveLength(1);
  });

  it("does not resurrect legacy projects after the canonical store is reset", async () => {
    const legacyPath = await createProjectDir("legacy");
    const legacyProject = project("legacy", "legacy", legacyPath);
    await writeStore(legacyStorePath(), [legacyProject]);

    const manager = new ProjectManager();
    manager.load();
    expect(manager.list().projects.map((item) => item.id)).toEqual(["legacy"]);

    await rm(canonicalStorePath(), { force: true });
    const resetManager = new ProjectManager();
    resetManager.load();

    expect(resetManager.list().projects.map((item) => item.id)).toEqual([]);
    expect(await legacyArchiveNames()).toHaveLength(1);
  });

  it("does not re-merge legacy projects after the canonical store exists", async () => {
    const keepPath = await createProjectDir("keep");
    const removedPath = await createProjectDir("removed");
    const legacyOnlyPath = await createProjectDir("legacy-only");
    await writeStore(canonicalStorePath(), [
      project("keep", "keep", keepPath),
      project("removed", "removed", removedPath),
    ]);
    await writeStore(legacyStorePath(), [
      project("removed", "removed", removedPath),
      project("legacy-only", "legacy-only", legacyOnlyPath),
    ]);

    const manager = new ProjectManager();
    const result = manager.remove("removed");

    expect(result.projects.map((item) => item.id)).toEqual(["keep"]);
    const saved = JSON.parse(await readFile(canonicalStorePath(), "utf8")) as {
      projects: DesktopProject[];
    };
    expect(saved.projects.map((item) => item.id)).toEqual(["keep"]);
  });

  it("preserves a corrupted canonical store instead of replacing it from legacy", async () => {
    await expectRejectedCanonicalStore(
      "{not json",
      "failed to parse project store",
    );
  });

  it.each([
    ["a non-object root", "[]"],
    ["a missing projects array", "{}"],
    [
      "an invalid project entry",
      JSON.stringify({ projects: [{ id: "incomplete" }] }),
    ],
    [
      "an invalid active context",
      JSON.stringify({ projects: [], active_context: { kind: "no_project" } }),
    ],
  ])(
    "rejects %s without replacing the canonical store",
    async (_name, contents) => {
      await expectRejectedCanonicalStore(contents, "invalid project store");
    },
  );

  it.skipIf(
    process.platform === "win32" ||
      typeof process.getuid !== "function" ||
      process.getuid() === 0,
  )("surfaces canonical store permission errors without migrating legacy data", async () => {
    const contents = `${JSON.stringify({ projects: [] }, null, 2)}\n`;
    const legacyPath = await createProjectDir("legacy");
    await writeFile(canonicalStorePath(), contents, "utf8");
    await writeStore(legacyStorePath(), [
      project("legacy", "legacy", legacyPath),
    ]);
    const legacyContents = await readFile(legacyStorePath(), "utf8");
    await chmod(canonicalStorePath(), 0o000);

    let loadError: unknown;
    try {
      new ProjectManager().load();
    } catch (error) {
      loadError = error;
    } finally {
      await chmod(canonicalStorePath(), 0o600);
    }

    expect(loadError).toBeInstanceOf(Error);
    expect((loadError as Error).message).toContain(
      "failed to read project store",
    );
    expect((loadError as Error).message).toContain(canonicalStorePath());
    expect(await readFile(canonicalStorePath(), "utf8")).toBe(contents);
    expect(await readFile(legacyStorePath(), "utf8")).toBe(legacyContents);
    expect(await legacyArchiveNames()).toEqual([]);
  });
});
