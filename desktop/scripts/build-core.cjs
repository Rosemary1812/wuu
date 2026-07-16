const { chmodSync, mkdirSync, readFileSync } = require("node:fs");
const { join, resolve } = require("node:path");
const { spawnSync } = require("node:child_process");

const desktopRoot = resolve(__dirname, "..");
const repoRoot = resolve(desktopRoot, "..");
const version = readFileSync(join(repoRoot, "VERSION"), "utf8").trim() || "0.1.0";
const commit =
  run("git", ["rev-parse", "--short", "HEAD"], { cwd: repoRoot, optional: true }) || "none";
const date = new Date().toISOString().replace(/\.\d{3}Z$/, "Z");
const binaryName = process.platform === "win32" ? "wuu-core.exe" : "wuu-core";
const outDir = join(desktopRoot, "build", "bin");
const outPath = join(outDir, binaryName);

mkdirSync(outDir, { recursive: true });

const ldflags = [
  "-s",
  "-w",
  `-X github.com/blueberrycongee/wuu/internal/version.Version=v${version}`,
  `-X github.com/blueberrycongee/wuu/internal/version.Commit=${commit}`,
  `-X github.com/blueberrycongee/wuu/internal/version.Date=${date}`,
].join(" ");

run("go", ["build", "-ldflags", ldflags, "-o", outPath, "./cmd/wuu"], {
  cwd: repoRoot,
  env: { ...process.env, CGO_ENABLED: "0" },
});

if (process.platform !== "win32") {
  chmodSync(outPath, 0o755);
}

console.log(`built ${outPath}`);

function run(command, args, options = {}) {
  const result = spawnSync(command, args, {
    cwd: options.cwd,
    env: options.env ?? process.env,
    encoding: "utf8",
    stdio: options.optional ? ["ignore", "pipe", "ignore"] : "inherit",
  });
  if (result.status !== 0) {
    if (options.optional) {
      return "";
    }
    process.exit(result.status ?? 1);
  }
  return options.optional ? result.stdout.trim() : "";
}
