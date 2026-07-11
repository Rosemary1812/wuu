const assert = require("node:assert/strict");
const { resolve } = require("node:path");
const {
  launchEnvironment,
  normalizeElectronArguments,
  processIDForLaunchToken,
} = require("./launch-electron-via-open.cjs");
const { helperPathForApp } = require("./prepare-dev-electron-app.cjs");

assert.deepEqual(
  normalizeElectronArguments([".", "--inspect=9229"], "/repo/desktop"),
  [resolve("/repo/desktop"), "--inspect=9229"],
);
assert.deepEqual(
  normalizeElectronArguments(["/repo/desktop", "--no-sandbox"], "/ignored"),
  ["/repo/desktop", "--no-sandbox"],
);

const environment = launchEnvironment(
  { ELECTRON_RENDERER_URL: "http://localhost:5173" },
  "token-1",
  "/repo",
  "/repo/desktop/build/bin/wuu-cua-mac",
);
assert.ok(environment.includes("ELECTRON_RENDERER_URL=http://localhost:5173"));
assert.ok(environment.includes("WUU_DEV_LAUNCH_TOKEN=token-1"));
assert.ok(environment.includes("WUU_SOURCE_ROOT=/repo"));
assert.ok(environment.includes("WUU_CUA_MAC_HELPER=/repo/desktop/build/bin/wuu-cua-mac"));

const processList = [
  "  41 /path/Electron Helper WUU_DEV_LAUNCH_TOKEN=token-1",
  "  42 /repo/desktop/build/dev-host/Wuu Dev.app/Contents/MacOS/Electron /repo/desktop WUU_DEV_LAUNCH_TOKEN=token-1",
].join("\n");
assert.equal(processIDForLaunchToken(processList, "token-1"), 42);
assert.equal(processIDForLaunchToken(processList, "missing"), undefined);
assert.equal(
  helperPathForApp("/repo/desktop/build/dev-host/Wuu Dev.app"),
  "/repo/desktop/build/dev-host/Wuu Dev.app/Contents/Resources/bin/wuu-cua-mac",
);

console.log("dev launcher tests passed");
