const {
  copyFileSync,
  existsSync,
  mkdirSync,
  readFileSync,
  rmSync,
  writeFileSync,
} = require("node:fs");
const { createHash } = require("node:crypto");
const { join, resolve } = require("node:path");
const { spawnSync } = require("node:child_process");

const desktopRoot = resolve(__dirname, "..");
const sourceApp = join(desktopRoot, "node_modules", "electron", "dist", "Electron.app");
const hostRoot = join(desktopRoot, "build", "dev-host");
const devApp = join(hostRoot, "Wuu Dev.app");
const markerPath = join(hostRoot, "identity.json");
const electronPackagePath = join(desktopRoot, "node_modules", "electron", "package.json");
const builtHelper = join(desktopRoot, "build", "bin", "wuu-cua-mac");
const builtPiPHelper = join(desktopRoot, "build", "bin", "wuu-cua-mac-pip");
const builtSpeechHelper = join(desktopRoot, "build", "bin", "wuu-speech-mac");
const helperBuildInfo = join(desktopRoot, "build", "bin", "wuu-cua-mac.build.json");
const identityVersion = 8;

function prepareDevElectronApp(signing = { identity: "-", fingerprint: "adhoc", name: "ad-hoc" }) {
  const electronVersion = JSON.parse(readFileSync(electronPackagePath, "utf8")).version;
  const helperHash = helperSourceHash();
  const builtHelperHash = hashFile(builtHelper);
  const builtPiPHelperHash = hashFile(builtPiPHelper);
  const builtSpeechHelperHash = hashFile(builtSpeechHelper);
  if (devHostIsCurrent(
    electronVersion,
    helperHash,
    builtHelperHash,
    builtPiPHelperHash,
    builtSpeechHelperHash,
    signing.fingerprint,
  )) return devApp;

  console.log(`preparing stable Wuu Dev host for Electron ${electronVersion}...`);
  mkdirSync(hostRoot, { recursive: true });
  rmSync(devApp, { recursive: true, force: true });

  const cloned = spawnSync("cp", ["-cR", sourceApp, devApp], { stdio: "inherit" });
  if (cloned.status !== 0) {
    rmSync(devApp, { recursive: true, force: true });
    run("ditto", [sourceApp, devApp]);
  }

  const info = join(devApp, "Contents", "Info.plist");
  run("/usr/libexec/PlistBuddy", ["-c", "Set :CFBundleIdentifier com.blueberrycongee.wuu.dev", info]);
  run("/usr/libexec/PlistBuddy", ["-c", "Set :CFBundleName Wuu Dev", info]);
  run("/usr/libexec/PlistBuddy", ["-c", "Set :CFBundleDisplayName Wuu Dev", info]);
  setPlistString(
    info,
    "NSScreenCaptureUsageDescription",
    "Wuu uses screen capture to show a live preview while an agent operates a Mac app.",
  );
  setPlistString(
    info,
    "NSMicrophoneUsageDescription",
    "Wuu uses the microphone only while you are dictating text.",
  );
  setPlistString(
    info,
    "NSSpeechRecognitionUsageDescription",
    "Wuu uses macOS Speech Recognition to turn your dictation into text.",
  );
  const packagedHelper = helperPathForApp(devApp);
  const packagedPiPHelper = pipHelperPathForApp(devApp);
  mkdirSync(join(devApp, "Contents", "Resources", "bin"), { recursive: true });
  copyFileSync(builtHelper, packagedHelper);
  copyFileSync(builtPiPHelper, packagedPiPHelper);
  const packagedSpeechHelper = speechHelperPathForApp(devApp);
  copyFileSync(builtSpeechHelper, packagedSpeechHelper);
  run("codesign", [
    "--force",
    "--deep",
    "--sign",
    signing.identity,
    "--identifier",
    "com.blueberrycongee.wuu.dev",
    devApp,
  ]);
  run("codesign", ["--verify", "--deep", "--strict", devApp]);
  const embeddedHelperHash = hashFile(packagedHelper);
  const embeddedPiPHelperHash = hashFile(packagedPiPHelper);
  writeFileSync(markerPath, `${JSON.stringify({
    electronVersion,
    helperHash,
    builtHelperHash,
    builtPiPHelperHash,
    builtSpeechHelperHash,
    embeddedHelperHash,
    embeddedPiPHelperHash,
    embeddedSpeechHelperHash: hashFile(packagedSpeechHelper),
    signingFingerprint: signing.fingerprint,
    identityVersion,
  })}\n`);
  if (signing.identity === "-") {
    console.log("Wuu Dev uses ad-hoc signing; macOS may require Accessibility and Screen Recording permission again.");
  } else {
    console.log(`Wuu Dev signed with ${signing.name}; macOS permissions will persist across rebuilds.`);
  }
  return devApp;
}

function devHostIsCurrent(
  electronVersion,
  helperHash,
  builtHelperHash,
  builtPiPHelperHash,
  builtSpeechHelperHash,
  signingFingerprint,
) {
  if (!existsSync(devApp) || !existsSync(markerPath)) return false;
  try {
    const marker = JSON.parse(readFileSync(markerPath, "utf8"));
    if (
      marker.electronVersion !== electronVersion
      || marker.helperHash !== helperHash
      || marker.builtHelperHash !== builtHelperHash
      || marker.builtPiPHelperHash !== builtPiPHelperHash
      || marker.builtSpeechHelperHash !== builtSpeechHelperHash
      || marker.embeddedHelperHash !== hashFile(helperPathForApp(devApp))
      || marker.embeddedPiPHelperHash !== hashFile(pipHelperPathForApp(devApp))
      || marker.embeddedSpeechHelperHash !== hashFile(speechHelperPathForApp(devApp))
      || marker.signingFingerprint !== signingFingerprint
      || marker.identityVersion !== identityVersion
    ) {
      return false;
    }
    return spawnSync("codesign", ["--verify", "--deep", "--strict", devApp], {
      stdio: "ignore",
    }).status === 0;
  } catch {
    return false;
  }
}

function hashFile(path) {
  return createHash("sha256").update(readFileSync(path)).digest("hex");
}

function helperSourceHash() {
  try {
    return sourceHashFromBuildInfo(JSON.parse(readFileSync(helperBuildInfo, "utf8")), () => hashFile(builtHelper));
  } catch {
    // Builds made outside the development script predate the metadata file.
    return hashFile(builtHelper);
  }
}

function sourceHashFromBuildInfo(info, fallback) {
  if (typeof info?.sourceHash === "string" && /^[a-f0-9]{64}$/i.test(info.sourceHash)) {
    return info.sourceHash;
  }
  return fallback();
}

function helperPathForApp(appPath) {
  return join(appPath, "Contents", "Resources", "bin", "wuu-cua-mac");
}

function pipHelperPathForApp(appPath) {
  return join(appPath, "Contents", "Resources", "bin", "wuu-cua-mac-pip");
}

function speechHelperPathForApp(appPath) {
  return join(appPath, "Contents", "Resources", "bin", "wuu-speech-mac");
}

function setPlistString(info, key, value) {
  const set = spawnSync("/usr/libexec/PlistBuddy", ["-c", `Set :${key} ${value}`, info], {
    stdio: "ignore",
  });
  if (set.status === 0) return;
  run("/usr/libexec/PlistBuddy", ["-c", `Add :${key} string ${value}`, info]);
}

function run(command, args) {
  const result = spawnSync(command, args, { stdio: "inherit" });
  if (result.status !== 0) {
    throw new Error(`${command} failed with status ${result.status ?? "unknown"}`);
  }
}

module.exports = {
  helperPathForApp,
  pipHelperPathForApp,
  speechHelperPathForApp,
  prepareDevElectronApp,
  sourceHashFromBuildInfo,
};
