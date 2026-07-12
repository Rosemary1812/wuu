const {
  chmodSync,
  copyFileSync,
  existsSync,
  lstatSync,
  mkdirSync,
  readFileSync,
  statSync,
  unlinkSync,
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
const pipDestination = join(outDir, "wuu-cua-mac-pip");
const buildInfo = join(outDir, "wuu-cua-mac.build.json");
const signingIdentity = process.env.WUU_CUA_MAC_SIGN_ID || "-";

run("swift", ["build", "-c", "release", "--package-path", packageRoot]);
if (!existsSync(source)) {
  throw new Error(`Swift build did not produce ${source}`);
}
const sourceHash = createHash("sha256").update(readFileSync(source)).digest("hex");
mkdirSync(outDir, { recursive: true });
removeOutput(destination);
removeOutput(pipDestination);
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
// replayd identifies capture clients by executable path on current macOS
// releases. Keep the MCP server and live PiP on separate physical files so an
// observation cannot invalidate the PiP stream's application connection. Copy
// after signing so both roles use the exact same signed bytes.
copyFileSync(destination, pipDestination);
chmodSync(pipDestination, 0o755);
const mcpStat = statSync(destination);
const pipStat = statSync(pipDestination);
if (lstatSync(pipDestination).isSymbolicLink() || (mcpStat.dev === pipStat.dev && mcpStat.ino === pipStat.ino)) {
  throw new Error("CUA MCP and PiP helpers must be separate physical files");
}
writeFileSync(buildInfo, `${JSON.stringify({ sourceHash })}\n`);

console.log(`built ${destination} and ${pipDestination}`);

function removeOutput(path) {
  try {
    unlinkSync(path);
  } catch (error) {
    if (error.code !== "ENOENT") throw error;
  }
}

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
