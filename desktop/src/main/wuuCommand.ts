import { existsSync } from "node:fs";
import { join } from "node:path";

export type WuuCommand = {
  command: string;
  args: string[];
  cwd: string;
};

/**
 * Decide how the desktop launches the Wuu core (`wuu app-server`).
 *
 * This deliberately NEVER inspects the active workspace for a `bin/wuu` or
 * `wuu` file. A project's local build artifact — which may be stale, built for
 * another OS, or simply not executable — must not be run as the desktop's core
 * (see issue #8: selecting the wuu repo, whose `make build` writes an ignored
 * `bin/wuu`, failed with `spawn ENOEXEC` on macOS because the artifact was a
 * Linux ELF). The active workspace is for user operations; the core binary is
 * desktop-owned or explicitly configured.
 *
 * Priority:
 *  1. `WUU_BIN` — an explicit override.
 *  2. `go run ./cmd/wuu` — only when `WUU_DESKTOP_USE_GO_RUN=1` and a source
 *     root was found (local development against the repo).
 *  3. packaged `Resources/bin/wuu` — the app-owned core for release builds.
 *  4. `wuu` on PATH — the installed CLI (`~/.local/bin/wuu -> ~/go/bin/wuu` in
 *     local dev, the packaged CLI otherwise).
 */
export function resolveWuuCommand(
  env: NodeJS.ProcessEnv,
  workdir: string,
  sourceRoot: string | undefined,
  resourcesPath?: string,
): WuuCommand {
  if (env.WUU_BIN) {
    return { command: env.WUU_BIN, args: [], cwd: workdir };
  }
  if (sourceRoot && env.WUU_DESKTOP_USE_GO_RUN === "1") {
    return { command: "go", args: ["run", "./cmd/wuu"], cwd: sourceRoot };
  }
  const packaged = resolvePackagedWuu(resourcesPath);
  if (packaged) {
    return { command: packaged, args: [], cwd: workdir };
  }
  return { command: "wuu", args: [], cwd: workdir };
}

function resolvePackagedWuu(resourcesPath: string | undefined): string | undefined {
  if (!resourcesPath) {
    return undefined;
  }
  for (const candidate of [
    join(resourcesPath, "bin", "wuu"),
    join(resourcesPath, "bin", "wuu.exe"),
    join(resourcesPath, "wuu"),
    join(resourcesPath, "wuu.exe"),
  ]) {
    if (existsSync(candidate)) {
      return candidate;
    }
  }
  return undefined;
}
