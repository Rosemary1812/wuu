const { chmodSync, copyFileSync, existsSync, mkdirSync } = require("node:fs");
const { join, resolve } = require("node:path");
const { spawnSync } = require("node:child_process");

if (process.platform !== "darwin") {
  console.log("skipping cua-mac helper build outside macOS");
  process.exit(0);
}

const desktopRoot = resolve(__dirname, "..");
const packageRoot = join(desktopRoot, "native", "cua-mac");
const outDir = join(desktopRoot, "build", "bin");
const source = join(packageRoot, ".build", "release", "wuu-cua-mac");
const destination = join(outDir, "wuu-cua-mac");

run("swift", ["build", "-c", "release", "--package-path", packageRoot]);
if (!existsSync(source)) {
  throw new Error(`Swift build did not produce ${source}`);
}
mkdirSync(outDir, { recursive: true });
copyFileSync(source, destination);
chmodSync(destination, 0o755);

// Ad-hoc signing gives the helper a stable local code identity for macOS TCC
// during development. The release pipeline can replace this with Developer ID
// signing when Wuu gains Apple credentials.
run("codesign", [
  "--force",
  "--sign",
  "-",
  "--identifier",
  "com.blueberrycongee.wuu.cua-mac",
  destination,
]);

console.log(`built ${destination}`);

function run(command, args) {
  const result = spawnSync(command, args, {
    cwd: desktopRoot,
    env: process.env,
    encoding: "utf8",
    stdio: "inherit",
  });
  if (result.status !== 0) {
    process.exit(result.status ?? 1);
  }
}
