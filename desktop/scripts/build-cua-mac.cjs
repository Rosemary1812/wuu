const {
  chmodSync,
  copyFileSync,
  existsSync,
  mkdirSync,
  readFileSync,
  writeFileSync,
} = require("node:fs");
const { createHash } = require("node:crypto");
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
const buildInfo = join(outDir, "wuu-cua-mac.build.json");
const signingIdentity = process.env.WUU_CUA_MAC_SIGN_ID || "-";

run("swift", ["build", "-c", "release", "--package-path", packageRoot]);
if (!existsSync(source)) {
  throw new Error(`Swift build did not produce ${source}`);
}
const sourceHash = createHash("sha256").update(readFileSync(source)).digest("hex");
mkdirSync(outDir, { recursive: true });
copyFileSync(source, destination);
chmodSync(destination, 0o755);

// The development launcher supplies WUU_CUA_MAC_SIGN_ID from a stable local
// certificate. Direct and release builds intentionally retain ad-hoc signing.
run("codesign", [
  "--force",
  "--sign",
  signingIdentity,
  "--identifier",
  "com.blueberrycongee.wuu.cua-mac",
  destination,
]);
writeFileSync(buildInfo, `${JSON.stringify({ sourceHash })}\n`);

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
