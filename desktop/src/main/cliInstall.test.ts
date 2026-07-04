import { mkdir, mkdtemp, readFile, readlink, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  autoInstallCli,
  getCliInstallStatus,
  installCli,
  isDirOnPath,
  resolveWuuBinary,
  type CliInstallEnv,
} from "./cliInstall";

let home: string;
let sourceBin: string;

beforeEach(async () => {
  home = await mkdtemp(join(tmpdir(), "wuu-cli-home-"));
  const sourceDir = await mkdtemp(join(tmpdir(), "wuu-cli-src-"));
  sourceBin = join(sourceDir, "wuu");
  await writeFile(sourceBin, "#!/bin/sh\n", { mode: 0o755 });
});

afterEach(async () => {
  await rm(home, { recursive: true, force: true });
});

function env(overrides: Partial<CliInstallEnv> = {}): CliInstallEnv {
  return {
    homeDir: home,
    platform: "linux",
    wuuBin: sourceBin,
    resourcesPath: "",
    execPath: "",
    pathEnv: "/usr/bin:/bin",
    ...overrides,
  };
}

describe("isDirOnPath", () => {
  it("matches a directory present in a PATH string", () => {
    expect(isDirOnPath("/home/u/.local/bin", "/usr/bin:/home/u/.local/bin")).toBe(true);
    expect(isDirOnPath("/home/u/.local/bin/", "/home/u/.local/bin")).toBe(true);
    expect(isDirOnPath("/home/u/.local/bin", "/usr/bin:/bin")).toBe(false);
  });
});

describe("resolveWuuBinary", () => {
  it("prefers the WUU_BIN override when it exists", () => {
    expect(resolveWuuBinary(env())).toBe(sourceBin);
  });

  it("returns null when no candidate exists", () => {
    expect(resolveWuuBinary(env({ wuuBin: "/does/not/exist", pathEnv: "/nope" }))).toBeNull();
  });

  it("never returns the install target itself", () => {
    const target = join(home, ".local", "bin");
    // Put ~/.local/bin on PATH; there is no wuu there, so it must not be picked
    // as a self-link candidate even if it existed.
    const resolved = resolveWuuBinary(env({ pathEnv: `${target}:/usr/bin` }));
    expect(resolved).toBe(sourceBin);
  });
});

describe("getCliInstallStatus", () => {
  it("reports not-installed and off-PATH before install", async () => {
    const status = await getCliInstallStatus(env());
    expect(status.platform_supported).toBe(true);
    expect(status.installed).toBe(false);
    expect(status.linked_to_source).toBe(false);
    expect(status.on_path).toBe(false);
    expect(status.source_path).toBe(sourceBin);
    expect(status.install_path).toBe(join(home, ".local", "bin", "wuu"));
  });

  it("reports on_path when ~/.local/bin is on PATH", async () => {
    const status = await getCliInstallStatus(
      env({ pathEnv: `${join(home, ".local", "bin")}:/usr/bin` }),
    );
    expect(status.on_path).toBe(true);
  });

  it("flags an unsupported platform", async () => {
    const status = await getCliInstallStatus(env({ platform: "win32" }));
    expect(status.platform_supported).toBe(false);
  });

  it("classifies a dangling symlink", async () => {
    const dir = join(home, ".local", "bin");
    await mkdir(dir, { recursive: true });
    await symlink(join(home, "gone", "wuu"), join(dir, "wuu"));
    const status = await getCliInstallStatus(env());
    expect(status.installed).toBe(true);
    expect(status.link_dangling).toBe(true);
    expect(status.foreign_install).toBe(false);
    expect(status.linked_to_source).toBe(false);
  });

  it("classifies a foreign real binary", async () => {
    const dir = join(home, ".local", "bin");
    await mkdir(dir, { recursive: true });
    await writeFile(join(dir, "wuu"), "user installed binary");
    const status = await getCliInstallStatus(env());
    expect(status.installed).toBe(true);
    expect(status.link_dangling).toBe(false);
    expect(status.foreign_install).toBe(true);
    expect(status.linked_to_source).toBe(false);
  });

  it("classifies our own link as linked, not foreign", async () => {
    await installCli({}, env());
    const status = await getCliInstallStatus(env());
    expect(status.linked_to_source).toBe(true);
    expect(status.foreign_install).toBe(false);
    expect(status.link_dangling).toBe(false);
  });
});

describe("installCli", () => {
  it("creates the symlink and reports success", async () => {
    const result = await installCli({}, env());
    expect(result.ok).toBe(true);
    const target = join(home, ".local", "bin", "wuu");
    expect(result.install_path).toBe(target);
    expect(await readlink(target)).toBe(sourceBin);
  });

  it("is idempotent when already linked to the same source", async () => {
    await installCli({}, env());
    const result = await installCli({}, env());
    expect(result.ok).toBe(true);
    expect(result.needs_overwrite).toBeUndefined();
  });

  it("asks to overwrite when a different file is present", async () => {
    const dir = join(home, ".local", "bin");
    await mkdir(dir, { recursive: true });
    await writeFile(join(dir, "wuu"), "not a link");

    const first = await installCli({}, env());
    expect(first.ok).toBe(false);
    expect(first.needs_overwrite).toBe(true);

    const second = await installCli({ overwrite: true }, env());
    expect(second.ok).toBe(true);
    expect(await readlink(join(dir, "wuu"))).toBe(sourceBin);
  });

  it("replaces a stale symlink pointing elsewhere", async () => {
    const dir = join(home, ".local", "bin");
    await mkdir(dir, { recursive: true });
    await symlink("/somewhere/else/wuu", join(dir, "wuu"));

    const first = await installCli({}, env());
    expect(first.needs_overwrite).toBe(true);

    const second = await installCli({ overwrite: true }, env());
    expect(second.ok).toBe(true);
    expect(await readlink(join(dir, "wuu"))).toBe(sourceBin);
  });

  it("returns an unsupported message on Windows without throwing", async () => {
    const result = await installCli({}, env({ platform: "win32" }));
    expect(result.ok).toBe(false);
    expect(result.message).toContain("暂不支持");
  });

  it("throws a readable error when no source binary is found", async () => {
    await expect(installCli({}, env({ wuuBin: "/nope", pathEnv: "/nope" }))).rejects.toThrow(
      /找不到 wuu/,
    );
  });
});

describe("autoInstallCli", () => {
  const target = () => join(home, ".local", "bin", "wuu");

  it("installs fresh when nothing exists", async () => {
    const result = await autoInstallCli(env());
    expect(result.outcome).toBe("installed");
    expect(await readlink(target())).toBe(sourceBin);
  });

  it("reports already-linked on subsequent startups", async () => {
    await autoInstallCli(env());
    const result = await autoInstallCli(env());
    expect(result.outcome).toBe("already-linked");
  });

  it("repairs a dangling symlink after the app moved", async () => {
    const dir = join(home, ".local", "bin");
    await mkdir(dir, { recursive: true });
    await symlink(join(home, "old-app-location", "wuu"), join(dir, "wuu"));

    const result = await autoInstallCli(env());
    expect(result.outcome).toBe("repaired");
    expect(await readlink(target())).toBe(sourceBin);
  });

  it("never touches a real user-installed binary", async () => {
    const dir = join(home, ".local", "bin");
    await mkdir(dir, { recursive: true });
    await writeFile(join(dir, "wuu"), "user installed binary");

    const result = await autoInstallCli(env());
    expect(result.outcome).toBe("skipped-existing");
    expect(result.message).toContain("桌面版未接管");
    expect(await readFile(join(dir, "wuu"), "utf8")).toBe("user installed binary");
  });

  it("never touches a valid symlink owned by something else", async () => {
    const otherDir = await mkdtemp(join(tmpdir(), "wuu-cli-other-"));
    const otherBin = join(otherDir, "wuu");
    await writeFile(otherBin, "#!/bin/sh\n");
    const dir = join(home, ".local", "bin");
    await mkdir(dir, { recursive: true });
    await symlink(otherBin, join(dir, "wuu"));

    const result = await autoInstallCli(env());
    expect(result.outcome).toBe("skipped-existing");
    expect(await readlink(target())).toBe(otherBin);
  });

  it("returns unsupported on Windows without filesystem changes", async () => {
    const result = await autoInstallCli(env({ platform: "win32" }));
    expect(result.outcome).toBe("unsupported");
    const status = await getCliInstallStatus(env());
    expect(status.installed).toBe(false);
  });

  it("reports no-source instead of throwing", async () => {
    const result = await autoInstallCli(env({ wuuBin: "/nope", pathEnv: "/nope" }));
    expect(result.outcome).toBe("no-source");
  });

  it("reports failed instead of throwing on filesystem errors", async () => {
    // Make ~/.local/bin an ordinary file so mkdir/symlink inside it fails.
    await mkdir(join(home, ".local"), { recursive: true });
    await writeFile(join(home, ".local", "bin"), "not a directory");

    const result = await autoInstallCli(env());
    expect(result.outcome).toBe("failed");
    expect(result.message).toContain("自动安装失败");
  });
});
