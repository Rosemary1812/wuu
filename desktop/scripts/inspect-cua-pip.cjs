#!/usr/bin/env node
"use strict";

// Dev-only diagnostic for the native CUA picture-in-picture (PiP) window.
//
// The PiP is not part of the Electron window: the Electron main process spawns
// a separate `wuu-cua-mac-pip --native-pip` helper, and that helper owns an
// independent NSPanel. So "the PiP is not visible in Wuu Dev" is not enough to
// tell whether the helper never started, started but has no panel, has a panel
// that is hidden/offscreen, or has a visible panel with no real content.
//
// This script correlates all three observation planes:
//   A. Electron main process (who spawned the helper)
//   B. the helper process + its arguments (target, window identity, parent pid)
//   C. the real NSPanel via CGWindowList, a window capture, and the visible
//      screen region the user actually sees
//
// It writes artifacts/cua-pip/<timestamp>/{diagnostic.json,pip-<id>.png} and
// prints a classification of why the PiP is (not) visible. It never runs in
// production and draws nothing into the PiP itself.

const { execFileSync } = require("node:child_process");
const { createHash } = require("node:crypto");
const { existsSync, mkdirSync, readFileSync, realpathSync, statSync, writeFileSync } = require("node:fs");
const { basename, dirname, join, resolve } = require("node:path");

function fail(message) {
  process.stderr.write(`inspect-cua-pip: ${message}\n`);
  process.exit(1);
}

if (process.platform !== "darwin") {
  fail("the native CUA PiP only exists on macOS; run this on the machine hosting Wuu Dev");
}

function run(cmd, args, opts = {}) {
  try {
    return { ok: true, out: execFileSync(cmd, args, { encoding: "utf8", maxBuffer: 256 * 1024 * 1024, ...opts }) };
  } catch (error) {
    return { ok: false, out: (error.stdout || "").toString(), err: (error.stderr || error.message || "").toString() };
  }
}

function pidAlive(pid) {
  if (!Number.isInteger(pid) || pid <= 0) return false;
  try {
    process.kill(pid, 0);
    return true;
  } catch (error) {
    return error.code === "EPERM";
  }
}

function sha256(path) {
  try {
    return createHash("sha256").update(readFileSync(path)).digest("hex");
  } catch {
    return undefined;
  }
}

function samePhysicalFile(first, second) {
  try {
    if (realpathSync(first) === realpathSync(second)) return true;
    const firstStat = statSync(first);
    const secondStat = statSync(second);
    return firstStat.dev === secondStat.dev && firstStat.ino === secondStat.ino;
  } catch {
    return false;
  }
}

// ---- Plane A/B: processes -------------------------------------------------

function listProcesses() {
  const { ok, out } = run("ps", ["-axww", "-o", "pid=,ppid=,etime=,args="]);
  if (!ok) return [];
  return out
    .split("\n")
    .map((line) => {
      const m = line.match(/^\s*(\d+)\s+(\d+)\s+(\S+)\s+(.*)$/);
      if (!m) return undefined;
      return { pid: Number(m[1]), ppid: Number(m[2]), etime: m[3], args: m[4] };
    })
    .filter(Boolean);
}

// argv for the helper is:
//   <path> --native-pip <activityID> <target...> <x> <y> <w> <h> <pid> <winID> <parentPID>
// The target may contain spaces, and the executable path may too ("Wuu Dev.app"),
// so parse the fixed head/tail and treat the middle as the target.
function parseHelper(proc) {
  const marker = "--native-pip";
  const at = proc.args.indexOf(marker);
  if (at < 0) return undefined;
  const binaryPath = proc.args.slice(0, at).trim();
  const tail = proc.args
    .slice(at + marker.length)
    .trim()
    .split(/\s+/)
    .filter(Boolean);
  if (tail.length < 8) return undefined;
  const parentPID = Number(tail[tail.length - 1]);
  const targetWindowID = Number(tail[tail.length - 2]);
  const targetProcessID = Number(tail[tail.length - 3]);
  const frame = {
    x: Number(tail[tail.length - 7]),
    y: Number(tail[tail.length - 6]),
    width: Number(tail[tail.length - 5]),
    height: Number(tail[tail.length - 4]),
  };
  const activityID = tail[0];
  const target = tail.slice(1, tail.length - 7).join(" ");
  return {
    pid: proc.pid,
    ppid: proc.ppid,
    etime: proc.etime,
    binaryPath,
    activityID,
    target,
    targetProcessID: targetProcessID > 0 ? targetProcessID : undefined,
    targetWindowID: targetWindowID > 0 ? targetWindowID : undefined,
    parentProcessID: parentPID > 0 ? parentPID : undefined,
    parentAlive: parentPID > 0 ? pidAlive(parentPID) : undefined,
    orphan: parentPID > 0 ? !pidAlive(parentPID) : false,
  };
}

// ---- Plane C: window server ------------------------------------------------

const WINDOW_QUERY = `
import CoreGraphics
import Foundation
let list = CGWindowListCopyWindowInfo([.optionAll], kCGNullWindowID) as? [[String: Any]] ?? []
var out: [[String: Any]] = []
let ownerNames: Set<String> = ["wuu-cua-mac-pip", "wuu-cua-mac"]
for w in list where ownerNames.contains(w[kCGWindowOwnerName as String] as? String ?? "") {
    var e: [String: Any] = [:]
    e["windowID"] = w[kCGWindowNumber as String] ?? 0
    e["ownerPID"] = w[kCGWindowOwnerPID as String] ?? 0
    e["layer"] = w[kCGWindowLayer as String] ?? 0
    e["alpha"] = w[kCGWindowAlpha as String] ?? 0
    e["onscreen"] = w[kCGWindowIsOnscreen as String] ?? false
    e["memory"] = w[kCGWindowMemoryUsage as String] ?? 0
    if let b = w[kCGWindowBounds as String] as? [String: Any] { e["bounds"] = b }
    out.append(e)
}
if let data = try? JSONSerialization.data(withJSONObject: out, options: []) {
    FileHandle.standardOutput.write(data)
}
`;

function listPiPWindows() {
  const { ok, out, err } = run("swift", ["-e", WINDOW_QUERY]);
  if (!ok) return { available: false, windows: [], reason: err.trim() || "swift unavailable" };
  try {
    return { available: true, windows: JSON.parse(out.trim() || "[]") };
  } catch (error) {
    return { available: false, windows: [], reason: `unparsable swift output: ${error.message}` };
  }
}

function preflightScreenRecording() {
  // NOTE: this reports the *swift* process's Screen Recording grant, which is a
  // proxy only. TCC is per-binary, so the PiP helper has its own grant.
  // Use this as a hint, and confirm the helper's own entry in System Settings.
  const { ok, out } = run("swift", ["-e", "import CoreGraphics; print(CGPreflightScreenCaptureAccess())"]);
  if (!ok) return undefined;
  return out.trim() === "true";
}

// ---- correlate + classify --------------------------------------------------

function repoBuiltHelper() {
  const roots = [process.env.WUU_SOURCE_ROOT, resolve(__dirname, ".."), resolve(__dirname, "..", "..")];
  for (const root of roots) {
    if (!root) continue;
    const candidate = join(root, "desktop", "build", "bin", "wuu-cua-mac-pip");
    if (existsSync(candidate)) return candidate;
    const nested = join(root, "build", "bin", "wuu-cua-mac-pip");
    if (existsSync(nested)) return nested;
  }
  return undefined;
}

function classify({ helpers, windows, windowPlane }) {
  if (helpers.length === 0) {
    return {
      stage: "helper_not_started",
      likely:
        "The Electron coordinator never spawned a helper for the active session. " +
        "Either the activity did not reach the coordinator, the active thread does not match the CUA thread, " +
        "the session was dismissed, or resolveCUAFrameHelper() found no built helper binary.",
    };
  }
  const live = helpers.filter((h) => !h.orphan);
  if (live.length === 0) {
    return { stage: "orphan_only", likely: "Only orphaned helpers (parent Electron gone) are running. Reap them and restart Wuu Dev." };
  }
  const pathCollisions = live.filter((helper) => !helper.matchesExpectedRole || helper.sharesMCPExecutable);
  if (pathCollisions.length > 0) {
    return {
      stage: "helper_path_collision",
      likely:
        "A PiP process is not running from the dedicated wuu-cua-mac-pip executable identity. " +
        "Hash equality only proves the version; this path/physical-file check prevents PiP from sharing replayd identity with MCP.",
    };
  }
  if (!windowPlane.available) {
    return { stage: "window_plane_unavailable", likely: `Could not enumerate windows (${windowPlane.reason}). Cannot confirm the panel; check the helper stderr in the Electron dev log.` };
  }
  const livePIDs = new Set(live.map((h) => h.pid));
  const panels = windows.filter((w) => livePIDs.has(Number(w.ownerPID)));
  if (panels.length === 0) {
    return {
      stage: "helper_started_no_panel",
      likely:
        "A live helper exists but owns no discoverable NSPanel. It may have failed before creating the controller, " +
        "or the WindowServer query may not recognize the helper owner name. Check the helper stderr and process path.",
    };
  }
  const visible = panels.filter((w) => Boolean(w.onscreen) && Number(w.alpha) > 0.01);
  if (visible.length === 0) {
    return {
      stage: "panel_hidden",
      likely:
        "A panel exists but is offscreen or alpha 0. Either coordinator visibility (active thread / dismissed) is keeping it hidden, " +
        "or saved user bounds place it offscreen. Check bounds vs your displays.",
    };
  }
  return {
    stage: "panel_visible_unverified",
    likely:
      "A panel is on-screen with alpha > 0, but this does not prove live capture: the frosted app-icon placeholder is also visible. " +
      "Inspect the captured PNG and confirm real target pixels (and a changing value) before calling the PiP live.",
  };
}

// ---- main ------------------------------------------------------------------

const stamp = new Date().toISOString().replace(/[:.]/g, "-");
const outDir = join(resolve(__dirname, ".."), "..", "artifacts", "cua-pip", stamp);
mkdirSync(outDir, { recursive: true });

const procs = listProcesses();
const helpers = procs.map(parseHelper).filter(Boolean);
const builtHelper = repoBuiltHelper();
const builtHash = builtHelper ? sha256(builtHelper) : undefined;
for (const helper of helpers) {
  const runningHash = sha256(helper.binaryPath);
  const colocatedMCPHelper = join(dirname(helper.binaryPath), "wuu-cua-mac");
  helper.runningBinaryHash = runningHash;
  helper.matchesBuiltHelper = builtHash && runningHash ? builtHash === runningHash : undefined;
  helper.matchesExpectedRole = basename(helper.binaryPath) === "wuu-cua-mac-pip";
  helper.sharesMCPExecutable = existsSync(colocatedMCPHelper)
    ? samePhysicalFile(helper.binaryPath, colocatedMCPHelper)
    : undefined;
}

const electronMains = new Map();
for (const helper of helpers) {
  if (!helper.parentProcessID) continue;
  const parent = procs.find((p) => p.pid === helper.parentProcessID);
  if (parent) electronMains.set(parent.pid, { pid: parent.pid, args: parent.args, etime: parent.etime });
}

const windowPlane = listPiPWindows();
const windows = windowPlane.windows || [];

// Capture each on-screen panel so the content can be inspected by eye.
const captures = [];
for (const w of windows) {
  if (!w.onscreen) continue;
  const png = join(outDir, `pip-${w.windowID}.png`);
  const result = run("screencapture", ["-x", "-o", "-l", String(w.windowID), png]);
  const size = existsSync(png) ? statSync(png).size : 0;
  const visiblePng = join(outDir, `pip-visible-${w.windowID}.png`);
  const bounds = w.bounds || {};
  const region = [bounds.X, bounds.Y, bounds.Width, bounds.Height].map(Number);
  const visibleResult = region.every(Number.isFinite) && region[2] > 0 && region[3] > 0
    ? run("screencapture", ["-x", `-R${region.map(Math.round).join(",")}`, visiblePng])
    : { ok: false };
  const visibleSize = existsSync(visiblePng) ? statSync(visiblePng).size : 0;
  captures.push({
    windowID: w.windowID,
    path: png,
    bytes: size,
    captured: result.ok && size > 0,
    visiblePath: visiblePng,
    visibleBytes: visibleSize,
    visibleCaptured: visibleResult.ok && visibleSize > 0,
    // A tiny PNG for a 260x170 window is a weak hint of an all-black / empty frame.
    blackHint: size > 0 && size < 3000,
  });
}

const diagnostic = {
  generated_at: stamp,
  platform: `${process.platform} ${process.arch}`,
  screen_recording_preflight_swift_proxy: preflightScreenRecording(),
  built_helper: builtHelper ? { path: builtHelper, sha256: builtHash } : undefined,
  electron_mains: [...electronMains.values()],
  helpers,
  window_plane: windowPlane.available ? { available: true, count: windows.length } : windowPlane,
  windows,
  captures,
  classification: classify({ helpers, windows, windowPlane }),
};

const jsonPath = join(outDir, "diagnostic.json");
writeFileSync(jsonPath, `${JSON.stringify(diagnostic, null, 2)}\n`);

// ---- human summary ---------------------------------------------------------

const c = diagnostic.classification;
const lines = [];
lines.push("");
lines.push(`CUA PiP diagnostic  (${stamp})`);
lines.push(`  artifacts: ${outDir}`);
lines.push("");
lines.push(`STAGE: ${c.stage}`);
lines.push(`  ${c.likely}`);
lines.push("");
lines.push(`helpers running: ${helpers.length}`);
for (const h of helpers) {
  lines.push(`  pid ${h.pid}  target=${h.target}  win=${h.targetWindowID ?? "-"}  parent=${h.parentProcessID ?? "-"}${h.orphan ? "  [ORPHAN]" : ""}`);
  if (h.matchesBuiltHelper === false) {
    lines.push(`    ! running binary differs from desktop/build/bin/wuu-cua-mac-pip (stale helper — rebuild + restart)`);
  }
  if (!h.matchesExpectedRole) {
    lines.push(`    ! running path is not the dedicated wuu-cua-mac-pip role (full Electron restart required)`);
  }
  if (h.sharesMCPExecutable) {
    lines.push(`    ! PiP and MCP resolve to the same physical executable (SCStream connection collision)`);
  }
}
lines.push(`electron mains: ${diagnostic.electron_mains.map((e) => e.pid).join(", ") || "(none correlated)"}`);
lines.push(`pip windows: ${windows.length}`);
for (const w of windows) {
  lines.push(`  id ${w.windowID}  owner ${w.ownerPID}  layer ${w.layer}  alpha ${w.alpha}  onscreen ${w.onscreen}  bounds ${JSON.stringify(w.bounds)}`);
}
for (const cap of captures) {
  lines.push(`window capture: ${cap.path}  (${cap.bytes} bytes${cap.blackHint ? ", tiny — possibly black/empty" : ""})`);
  lines.push(`visible pixels: ${cap.visiblePath}  (${cap.visibleBytes} bytes${cap.visibleCaptured ? "" : ", capture failed"})`);
}
if (diagnostic.screen_recording_preflight_swift_proxy === false) {
  lines.push("");
  lines.push("! Screen Recording preflight (swift proxy) is FALSE. Confirm the Wuu Dev entry in");
  lines.push("  System Settings > Privacy & Security > Screen Recording. A rebuild/re-sign can reset this grant.");
}
lines.push("");
lines.push(`full JSON: ${jsonPath}`);
lines.push("");

process.stdout.write(lines.join("\n"));
