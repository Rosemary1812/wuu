const { spawn, spawnSync } = require("node:child_process");
const { join, resolve } = require("node:path");
const {
  helperPathForApp,
  prepareDevElectronApp,
} = require("./prepare-dev-electron-app.cjs");

const desktopRoot = resolve(__dirname, "..");
const buildHelper = join(__dirname, "build-cua-mac.cjs");
const electronVite = join(desktopRoot, "node_modules", "electron-vite", "dist", "cli.mjs");

const build = spawnSync(process.execPath, [buildHelper], {
  cwd: desktopRoot,
  env: process.env,
  stdio: "inherit",
});
if (build.status !== 0) {
  process.exit(build.status ?? 1);
}

const env = { ...process.env };
if (process.platform === "darwin") {
  env.WUU_DEV_ELECTRON_APP = prepareDevElectronApp();
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
