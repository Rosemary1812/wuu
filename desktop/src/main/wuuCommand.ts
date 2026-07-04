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
 *  3. `wuu` on PATH — the installed CLI (`~/.local/bin/wuu -> ~/go/bin/wuu` in
 *     local dev, the packaged CLI otherwise).
 */
export function resolveWuuCommand(
  env: NodeJS.ProcessEnv,
  workdir: string,
  sourceRoot: string | undefined,
): WuuCommand {
  if (env.WUU_BIN) {
    return { command: env.WUU_BIN, args: [], cwd: workdir };
  }
  if (sourceRoot && env.WUU_DESKTOP_USE_GO_RUN === "1") {
    return { command: "go", args: ["run", "./cmd/wuu"], cwd: sourceRoot };
  }
  return { command: "wuu", args: [], cwd: workdir };
}
