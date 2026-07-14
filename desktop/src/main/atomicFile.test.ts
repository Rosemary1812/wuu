import fs from "node:fs";
import { mkdtemp, readFile, readdir, rm, stat, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { writeTextFileAtomicSync } from "./atomicFile";

let directory: string;

beforeEach(async () => {
  directory = await mkdtemp(join(tmpdir(), "wuu-atomic-file-"));
});

afterEach(async () => {
  vi.restoreAllMocks();
  await rm(directory, { recursive: true, force: true });
});

describe("writeTextFileAtomicSync", () => {
  it("replaces the file and defaults new files to owner-only permissions", async () => {
    const path = join(directory, "nested", "settings.json");

    writeTextFileAtomicSync(path, "new\n");

    expect(await readFile(path, "utf8")).toBe("new\n");
    if (process.platform !== "win32") {
      expect((await stat(path)).mode & 0o777).toBe(0o600);
    }
  });

  it("preserves the permissions of an existing file", async () => {
    const path = join(directory, "script.sh");
    await writeFile(path, "old\n", { mode: 0o750 });

    writeTextFileAtomicSync(path, "new\n");

    expect(await readFile(path, "utf8")).toBe("new\n");
    if (process.platform !== "win32") {
      expect((await stat(path)).mode & 0o777).toBe(0o750);
    }
  });

  it("leaves the original file intact and removes the temporary file when replacement fails", async () => {
    const path = join(directory, "settings.json");
    await writeFile(path, "old\n");
    vi.spyOn(fs, "renameSync").mockImplementationOnce(() => {
      throw new Error("replacement failed");
    });

    expect(() => writeTextFileAtomicSync(path, "new\n")).toThrow(
      "replacement failed",
    );

    expect(await readFile(path, "utf8")).toBe("old\n");
    expect(await readdir(directory)).toEqual(["settings.json"]);
  });
});
