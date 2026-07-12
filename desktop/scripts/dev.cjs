const { spawn, spawnSync } = require("node:child_process");
const { readFileSync } = require("node:fs");
const { dirname, join, resolve } = require("node:path");
const {
  helperPathForApp,
  prepareDevElectronApp,
} = require("./prepare-dev-electron-app.cjs");
const { ensureDevSigningIdentity } = require("./dev-signing.cjs");

const desktopRoot = resolve(__dirname, "..");
const buildHelper = join(__dirname, "build-cua-mac.cjs");
const electronVitePackage = require.resolve("electron-vite/package.json", { paths: [desktopRoot] });
const electronViteManifest = JSON.parse(readFileSync(electronVitePackage, "utf8"));
const electronVite = join(dirname(electronVitePackage), electronViteManifest.bin["electron-vite"]);

const devSigning = process.platform === "darwin" ? ensureDevSigningIdentity() : undefined;
const build = spawnSync(process.execPath, [buildHelper], {
  cwd: desktopRoot,
  env: {
    ...process.env,
    ...(devSigning ? { WUU_CUA_MAC_SIGN_ID: devSigning.identity } : {}),
  },
  stdio: "inherit",
});
if (build.status !== 0) {
  process.exit(build.status ?? 1);
}

const env = { ...process.env };
if (process.platform === "darwin") {
  // CUA is intentionally available only to internal development builds.
  env.WUU_ENABLE_CUA_MAC = "1";
  env.WUU_DEV_ELECTRON_APP = prepareDevElectronApp(devSigning);
  env.WUU_CUA_MAC_HELPER = helperPathForApp(env.WUU_DEV_ELECTRON_APP);
  env.ELECTRON_EXEC_PATH = join(__dirname, "launch-electron-via-open.cjs");
}

const dev = spawn(process.execPath, [electronVite, desktopRoot], {
  cwd: desktopRoot,
  env,
  stdio: "inherit",
});
dev.on("error", (error) => {
  console.error(`failed to start electron-vite: ${error.message}`);
  process.exit(1);
});
dev.on("exit", (code, signal) => {
  if (signal) {
    process.kill(process.pid, signal);
    return;
  }
  process.exit(code ?? 0);
});
