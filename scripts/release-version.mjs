#!/usr/bin/env node

import { readFileSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(fileURLToPath(new URL("..", import.meta.url)));
const versionFile = resolve(repoRoot, "VERSION");
const desktopManifest = resolve(repoRoot, "desktop/package.json");
const desktopLock = resolve(repoRoot, "desktop/package-lock.json");
const changelogFile = resolve(repoRoot, "CHANGELOG.md");
const semverPattern = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$/;

const [command = "check", rawVersion] = process.argv.slice(2);

switch (command) {
  case "check":
    checkVersions(rawVersion);
    break;
  case "prepare":
    prepare(rawVersion);
    break;
  case "notes":
    process.stdout.write(`${releaseNotes(requireVersion(rawVersion))}\n`);
    break;
  default:
    fail("usage: release-version.mjs check [v<version>] | prepare <version> | notes <version>");
}

function checkVersions(expectedTag) {
  const versions = currentVersions();
  requireVersion(versions.root);
  for (const [source, version] of Object.entries(versions)) {
    if (version !== versions.root) {
      fail(`release versions differ: VERSION=${versions.root} ${source}=${version}`);
    }
  }
  if (expectedTag) {
    const expected = requireVersion(expectedTag);
    if (versions.root !== expected) {
      fail(`release version ${versions.root} does not match tag v${expected}`);
    }
    releaseNotes(expected);
  }
  console.log(`release versions match: ${versions.root}`);
}

function prepare(rawNextVersion) {
  const nextVersion = requireVersion(rawNextVersion);
  const current = currentVersions().root;
  requireVersion(current);
  if (compareVersions(nextVersion, current) <= 0) {
    fail(`new version ${nextVersion} must be greater than current version ${current}`);
  }

  const changelog = readFileSync(changelogFile, "utf8");
  const marker = "## [Unreleased]";
  const markerStart = changelog.indexOf(marker);
  if (markerStart < 0) fail("CHANGELOG.md is missing an [Unreleased] section");
  const bodyStart = markerStart + marker.length;
  const nextHeadingOffset = changelog.slice(bodyStart).search(/^## \[/m);
  const bodyEnd = nextHeadingOffset < 0 ? changelog.length : bodyStart + nextHeadingOffset;
  const unreleased = changelog.slice(bodyStart, bodyEnd).trim();
  if (!unreleased) fail("CHANGELOG.md [Unreleased] is empty; document user-visible changes first");

  writeFileSync(versionFile, `${nextVersion}\n`);
  updateJSON(desktopManifest, (json) => { json.version = nextVersion; });
  updateJSON(desktopLock, (json) => {
    json.version = nextVersion;
    json.packages[""].version = nextVersion;
  });
  const date = new Date().toISOString().slice(0, 10);
  const released = `${marker}\n\n## [${nextVersion}] - ${date}\n\n${unreleased}\n\n`;
  writeFileSync(changelogFile, changelog.slice(0, markerStart) + released + changelog.slice(bodyEnd));
  checkVersions();
  console.log(`prepared release ${nextVersion}`);
}

function releaseNotes(rawVersion) {
  const version = requireVersion(rawVersion);
  const changelog = readFileSync(changelogFile, "utf8");
  const heading = `## [${version}]`;
  const start = changelog.indexOf(heading);
  if (start < 0) fail(`CHANGELOG.md has no section for ${version}`);
  const contentStart = changelog.indexOf("\n", start);
  const nextHeadingOffset = changelog.slice(contentStart + 1).search(/^## \[/m);
  const end = nextHeadingOffset < 0 ? changelog.length : contentStart + 1 + nextHeadingOffset;
  const body = changelog.slice(contentStart + 1, end).trim();
  if (!body) fail(`CHANGELOG.md section for ${version} is empty`);
  return `# wuu v${version}\n\n${body}`;
}

function currentVersions() {
  const desktop = readJSON(desktopManifest);
  const lock = readJSON(desktopLock);
  return {
    root: readFileSync(versionFile, "utf8").trim(),
    desktop: desktop.version,
    desktopLock: lock.version,
    desktopLockRoot: lock.packages?.[""]?.version,
  };
}

function requireVersion(rawVersion) {
  const version = String(rawVersion ?? "").trim().replace(/^v/, "");
  if (!semverPattern.test(version)) fail(`invalid semantic version: ${rawVersion ?? ""}`);
  return version;
}

function compareVersions(left, right) {
  const a = semverPattern.exec(left);
  const b = semverPattern.exec(right);
  for (let index = 1; index <= 3; index += 1) {
    const difference = Number(a[index]) - Number(b[index]);
    if (difference !== 0) return difference;
  }
  if (a[4] === b[4]) return 0;
  if (!a[4]) return 1;
  if (!b[4]) return -1;
  return a[4].localeCompare(b[4], "en", { numeric: true });
}

function readJSON(path) {
  return JSON.parse(readFileSync(path, "utf8"));
}

function updateJSON(path, update) {
  const json = readJSON(path);
  update(json);
  writeFileSync(path, `${JSON.stringify(json, null, 2)}\n`);
}

function fail(message) {
  console.error(message);
  process.exit(1);
}
