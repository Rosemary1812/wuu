const assert = require("node:assert/strict");
const { resolve } = require("node:path");
const packageJSON = require("../package.json");
const {
  launchEnvironment,
  normalizeElectronArguments,
  processIDForLaunchToken,
} = require("./launch-electron-via-open.cjs");
const {
  helperPathForApp,
  sourceHashFromBuildInfo,
} = require("./prepare-dev-electron-app.cjs");
const {
  DEFAULT_DEV_SIGNING_ID,
  matchingIdentity,
  parseCodeSigningIdentities,
} = require("./dev-signing.cjs");

assert.deepEqual(
  normalizeElectronArguments([".", "--inspect=9229"], "/repo/desktop"),
  [resolve("/repo/desktop"), "--inspect=9229"],
);
assert.deepEqual(
  normalizeElectronArguments(["/repo/desktop", "--no-sandbox"], "/ignored"),
  ["/repo/desktop", "--no-sandbox"],
);

const environment = launchEnvironment(
  { ELECTRON_RENDERER_URL: "http://localhost:5173", WUU_ENABLE_CUA_MAC: "1" },
  "token-1",
  "/repo",
  "/repo/desktop/build/bin/wuu-cua-mac",
);
assert.ok(environment.includes("ELECTRON_RENDERER_URL=http://localhost:5173"));
assert.ok(environment.includes("WUU_DEV_LAUNCH_TOKEN=token-1"));
assert.ok(environment.includes("WUU_SOURCE_ROOT=/repo"));
assert.ok(environment.includes("WUU_ENABLE_CUA_MAC=1"));
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
assert.equal(
  sourceHashFromBuildInfo({ sourceHash: "a".repeat(64) }, () => "fallback"),
  "a".repeat(64),
);
assert.equal(sourceHashFromBuildInfo({}, () => "fallback"), "fallback");
assert.match(packageJSON.scripts["pack:mac"], /CSC_IDENTITY_AUTO_DISCOVERY=false/);
assert.match(packageJSON.scripts["dist:mac"], /CSC_IDENTITY_AUTO_DISCOVERY=false/);
assert.match(packageJSON.scripts["dev:direct"], /WUU_ENABLE_CUA_MAC=1/);
assert.equal(packageJSON.scripts["build:core"], "node scripts/build-core.cjs");
assert.doesNotMatch(packageJSON.scripts["pack:mac"], /cua-mac/);
assert.doesNotMatch(packageJSON.scripts["dist:mac"], /cua-mac/);
assert.ok(packageJSON.build.extraResources[0].filter.includes("!wuu-cua-mac*"));
assert.equal(packageJSON.build.mac.extendInfo, undefined);

const identities = parseCodeSigningIdentities([
  '  1) 0123456789ABCDEF0123456789ABCDEF01234567 "Wuu Dev Signing"',
  "     1 valid identities found",
].join("\n"));
assert.deepEqual(identities, [{
  sha1: "0123456789ABCDEF0123456789ABCDEF01234567",
  name: DEFAULT_DEV_SIGNING_ID,
}]);
assert.equal(
  matchingIdentity(identities, DEFAULT_DEV_SIGNING_ID)?.sha1,
  "0123456789ABCDEF0123456789ABCDEF01234567",
);
assert.equal(
  matchingIdentity(identities, "0123456789abcdef0123456789abcdef01234567")?.name,
  DEFAULT_DEV_SIGNING_ID,
);

console.log("dev launcher tests passed");
