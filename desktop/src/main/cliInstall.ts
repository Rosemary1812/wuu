import { existsSync } from "node:fs";
import { lstat, mkdir, readlink, realpath, symlink, unlink } from "node:fs/promises";
import { homedir } from "node:os";
import { delimiter, join, resolve } from "node:path";
import type {
  CliAutoInstallResult,
  CliInstallFsStatus,
  CliInstallResult,
} from "../shared/protocol";

// Feature: "install 'wuu' command" — symlink the bundled/available wuu CLI
// into ~/.local/bin/wuu, mirroring VS Code's "install 'code' command". The
// logic lives here (not in index.ts) so it can be unit tested with an
// injected environment instead of the real process/electron globals.
//
// The wire shapes (CliInstallFsStatus / CliInstallResult /
// CliAutoInstallResult) live in shared protocol.ts so the renderer and main
// process agree on them.
//
// Install also runs automatically at app startup (autoInstallCli): the CLI is
// how agents invoke wuu, so it must not depend on the user finding a button.
// The auto pass is idempotent and self-repairing (missing or dangling links
// are rebuilt after an app move/update) but never replaces a foreign
// ~/.local/bin/wuu — a real binary from go install / install.sh or a symlink
// owned by something else. Those stay untouched and are only surfaced as
// status in settings; replacing them requires the manual overwrite flow.

// Injectable environment so the filesystem behavior is testable without
// touching the real process globals.
export type CliInstallEnv = {
  homeDir?: string;
  execPath?: string;
  resourcesPath?: string;
  wuuBin?: string;
  pathEnv?: string;
  platform?: NodeJS.Platform;
};

function resolveEnv(env: CliInstallEnv = {}): Required<CliInstallEnv> {
  return {
    homeDir: env.homeDir ?? homedir(),
    execPath: env.execPath ?? process.execPath,
    // process.resourcesPath only exists in a packaged Electron main process.
    resourcesPath: env.resourcesPath ?? (process as { resourcesPath?: string }).resourcesPath ?? "",
    wuuBin: env.wuuBin ?? process.env.WUU_BIN ?? "",
    pathEnv: env.pathEnv ?? process.env.PATH ?? "",
    platform: env.platform ?? process.platform,
  };
}

export function installDir(env?: CliInstallEnv): string {
  return join(resolveEnv(env).homeDir, ".local", "bin");
}

export function installPath(env?: CliInstallEnv): string {
  return join(installDir(env), "wuu");
}

// isDirOnPath reports whether dir appears as an entry in a PATH-style string.
export function isDirOnPath(dir: string, pathEnv: string): boolean {
  const target = normalizeDir(dir);
  return pathEnv
    .split(delimiter)
    .map((entry) => normalizeDir(entry))
    .some((entry) => entry === target);
}

function normalizeDir(dir: string): string {
  if (!dir) {
    return "";
  }
  const cleaned = resolve(dir);
  // Treat /a/b and /a/b/ as the same directory.
  return cleaned.endsWith("/") && cleaned.length > 1 ? cleaned.slice(0, -1) : cleaned;
}

// wuuBinaryCandidates lists, in priority order, where the desktop app might
// find a real wuu executable to link from. The install target itself is never
// a candidate, so we can't accidentally self-link.
export function wuuBinaryCandidates(env?: CliInstallEnv): string[] {
  const resolved = resolveEnv(env);
  const target = installPath(env);
  const candidates: string[] = [];
  const push = (candidate: string): void => {
    if (candidate && resolve(candidate) !== target && !candidates.includes(candidate)) {
      candidates.push(candidate);
    }
  };

  if (resolved.wuuBin) {
    push(resolved.wuuBin);
  }
  if (resolved.resourcesPath) {
    push(join(resolved.resourcesPath, "bin", "wuu"));
    push(join(resolved.resourcesPath, "wuu"));
  }
  if (resolved.execPath) {
    const execDir = resolve(resolved.execPath, "..");
    push(join(execDir, "wuu"));
    push(join(execDir, "bin", "wuu"));
  }
  // Anything named `wuu` already resolvable on PATH (skips the install target
  // via the `push` guard above).
  for (const dir of resolved.pathEnv.split(delimiter)) {
    if (dir) {
      push(join(dir, "wuu"));
    }
  }
  return candidates;
}

export function resolveWuuBinary(env?: CliInstallEnv): string | null {
  for (const candidate of wuuBinaryCandidates(env)) {
    if (existsSync(candidate)) {
      return candidate;
    }
  }
  return null;
}

async function pathExists(target: string): Promise<boolean> {
  try {
    await lstat(target);
    return true;
  } catch {
    return false;
  }
}

async function linkResolvesTo(link: string, source: string): Promise<boolean> {
  try {
    const [linkReal, sourceReal] = await Promise.all([realpath(link), realpath(source)]);
    return linkReal === sourceReal;
  } catch {
    // Fall back to a raw readlink comparison for broken/relative links.
    try {
      const raw = await readlink(link);
      return resolve(raw) === resolve(source);
    } catch {
      return false;
    }
  }
}

type ExistingEntry = {
  exists: boolean;
  // Entry is a symlink whose target no longer exists (broken link).
  dangling: boolean;
  // Entry resolves to the current wuu source binary.
  linkedToSource: boolean;
};

// classifyExisting inspects whatever currently sits at the install path so
// both the status view and the auto-install pass agree on what it is.
async function classifyExisting(target: string, source: string | null): Promise<ExistingEntry> {
  let isLink = false;
  try {
    const stats = await lstat(target);
    isLink = stats.isSymbolicLink();
  } catch {
    return { exists: false, dangling: false, linkedToSource: false };
  }
  let dangling = false;
  if (isLink) {
    try {
      await realpath(target);
    } catch {
      dangling = true;
    }
  }
  const linkedToSource = !dangling && source ? await linkResolvesTo(target, source) : false;
  return { exists: true, dangling, linkedToSource };
}

export async function getCliInstallStatus(env?: CliInstallEnv): Promise<CliInstallFsStatus> {
  const resolved = resolveEnv(env);
  const dir = installDir(env);
  const target = installPath(env);
  const source = resolveWuuBinary(env);
  const supported = resolved.platform === "darwin" || resolved.platform === "linux";

  const existing = await classifyExisting(target, source);

  return {
    platform_supported: supported,
    platform: resolved.platform,
    source_path: source,
    install_dir: dir,
    install_path: target,
    installed: existing.exists,
    linked_to_source: existing.linkedToSource,
    link_dangling: existing.dangling,
    foreign_install: existing.exists && !existing.linkedToSource && !existing.dangling,
    on_path: isDirOnPath(dir, resolved.pathEnv),
  };
}

// autoInstallCli is the startup pass. It never throws and never prompts:
// unsupported platforms and missing binaries are reported as outcomes, a
// foreign install is left untouched ("skipped-existing"), and filesystem
// errors come back as "failed" so app startup is never blocked.
export async function autoInstallCli(env?: CliInstallEnv): Promise<CliAutoInstallResult> {
  const resolved = resolveEnv(env);
  const dir = installDir(env);
  const target = installPath(env);
  const onPath = isDirOnPath(dir, resolved.pathEnv);

  if (resolved.platform !== "darwin" && resolved.platform !== "linux") {
    return { outcome: "unsupported", install_path: target, source_path: null, on_path: onPath };
  }

  const source = resolveWuuBinary(env);
  if (!source) {
    return {
      outcome: "no-source",
      install_path: target,
      source_path: null,
      on_path: onPath,
      message: "找不到 wuu 可执行文件，跳过自动安装。",
    };
  }

  try {
    const existing = await classifyExisting(target, source);
    if (existing.linkedToSource) {
      return { outcome: "already-linked", install_path: target, source_path: source, on_path: onPath };
    }
    if (existing.exists && !existing.dangling) {
      // Safety boundary: a real binary (go install / install.sh) or a
      // symlink owned by something else. Never replaced automatically.
      return {
        outcome: "skipped-existing",
        install_path: target,
        source_path: source,
        on_path: onPath,
        message: "检测到已有 wuu 命令，桌面版未接管。",
      };
    }

    await mkdir(dir, { recursive: true });
    if (existing.exists) {
      // Dangling symlink from a previous install — the app was moved or
      // updated. Rebuild it.
      await unlink(target);
      await symlink(source, target);
      return { outcome: "repaired", install_path: target, source_path: source, on_path: onPath };
    }
    await symlink(source, target);
    return { outcome: "installed", install_path: target, source_path: source, on_path: onPath };
  } catch (error) {
    return {
      outcome: "failed",
      install_path: target,
      source_path: source,
      on_path: onPath,
      message: `自动安装失败：${errorText(error)}`,
    };
  }
}

export async function installCli(
  options: { overwrite?: boolean } = {},
  env?: CliInstallEnv,
): Promise<CliInstallResult> {
  const resolved = resolveEnv(env);
  const dir = installDir(env);
  const target = installPath(env);
  const onPath = isDirOnPath(dir, resolved.pathEnv);

  if (resolved.platform !== "darwin" && resolved.platform !== "linux") {
    return {
      ok: false,
      install_path: target,
      on_path: onPath,
      message: "暂不支持在当前系统安装命令行工具（仅支持 macOS / Linux）。",
    };
  }

  const source = resolveWuuBinary(env);
  if (!source) {
    throw new Error("找不到 wuu 可执行文件，无法创建命令行链接。");
  }

  try {
    await mkdir(dir, { recursive: true });
  } catch (error) {
    throw new Error(`无法创建目录 ${dir}：${errorText(error)}`);
  }

  const exists = await pathExists(target);
  if (exists) {
    if (await linkResolvesTo(target, source)) {
      return { ok: true, install_path: target, source_path: source, on_path: onPath };
    }
    if (!options.overwrite) {
      return {
        ok: false,
        needs_overwrite: true,
        install_path: target,
        source_path: source,
        on_path: onPath,
      };
    }
    try {
      await unlink(target);
    } catch (error) {
      throw new Error(`无法移除已存在的 ${target}：${errorText(error)}`);
    }
  }

  try {
    await symlink(source, target);
  } catch (error) {
    throw new Error(`创建链接失败：${errorText(error)}`);
  }

  return { ok: true, install_path: target, source_path: source, on_path: onPath };
}

function errorText(error: unknown): string {
  if (error instanceof Error) {
    const code = (error as NodeJS.ErrnoException).code;
    if (code === "EACCES" || code === "EPERM") {
      return "权限不足，请检查目标目录是否可写。";
    }
    if (code === "EROFS") {
      return "目标位于只读文件系统，无法写入。";
    }
    return error.message;
  }
  return String(error);
}
