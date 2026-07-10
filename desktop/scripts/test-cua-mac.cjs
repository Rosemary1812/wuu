const { resolve } = require("node:path");
const { spawnSync } = require("node:child_process");

if (process.platform !== "darwin") {
  console.log("skipping cua-mac tests outside macOS");
  process.exit(0);
}

const packageRoot = resolve(__dirname, "..", "native", "cua-mac");
run("swift", ["run", "cua-mac-protocol-tests"]);
run("node", ["ProtocolTests/stdio-smoke.mjs"]);

function run(command, args) {
  const result = spawnSync(command, args, {
    cwd: packageRoot,
    env: process.env,
    encoding: "utf8",
    stdio: "inherit",
  });
  if (result.status !== 0) {
    process.exit(result.status ?? 1);
  }
}
