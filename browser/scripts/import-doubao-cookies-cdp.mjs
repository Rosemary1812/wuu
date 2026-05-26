#!/usr/bin/env node
import { spawnSync } from "node:child_process";
import crypto from "node:crypto";
import os from "node:os";
import path from "node:path";

const CHROME_EPOCH_DELTA = 11644473600000000n;

const usage = `Usage: browser/scripts/import-doubao-cookies-cdp.mjs [--dry-run] [--apply] [--source-profile DIR] [--cdp-url URL]

Import non-expired Doubao cookies into a running Wuu Browser through the local
Chrome DevTools Protocol endpoint. Cookie values are decrypted locally and sent
to Wuu Browser over loopback CDP; values are never printed.

Options:
  --dry-run             Count decryptable cookies without writing to Wuu Browser.
                        This is the default.
  --apply               Write cookies to the running Wuu Browser.
  --source-profile DIR  Doubao profile root. Defaults to
                        ~/Library/Application Support/Doubao.
  --cdp-url URL         Wuu Browser CDP URL. Defaults to http://127.0.0.1:9100.
`;

let mode = "dry-run";
let sourceProfile = path.join(os.homedir(), "Library/Application Support/Doubao");
let cdpUrl = "http://127.0.0.1:9100";
const sourceService = "Doubao Safe Storage";
const sourceAccount = "Doubao";

for (let i = 2; i < process.argv.length; i += 1) {
  const arg = process.argv[i];
  if (arg === "--dry-run") {
    mode = "dry-run";
    continue;
  }
  if (arg === "--apply") {
    mode = "apply";
    continue;
  }
  if (arg === "--source-profile") {
    sourceProfile = process.argv[i + 1] || "";
    i += 1;
    continue;
  }
  if (arg === "--cdp-url") {
    cdpUrl = process.argv[i + 1] || "";
    i += 1;
    continue;
  }
  if (arg === "-h" || arg === "--help") {
    console.log(usage);
    process.exit(0);
  }
  console.error(`Unknown argument: ${arg}`);
  console.error(usage);
  process.exit(2);
}

if (!sourceProfile || !cdpUrl) {
  console.error("--source-profile and --cdp-url cannot be empty");
  process.exit(2);
}

function run(command, args, options = {}) {
  const result = spawnSync(command, args, {
    encoding: options.encoding ?? "utf8",
    maxBuffer: 200 * 1024 * 1024,
  });
  if (result.status !== 0) {
    const stderr = Buffer.isBuffer(result.stderr)
      ? result.stderr.toString("utf8")
      : result.stderr || "";
    throw new Error(`${command} failed: ${stderr.trim()}`);
  }
  return result.stdout;
}

function readKeychainPassword(service, account) {
  const result = spawnSync(
    "security",
    ["find-generic-password", "-w", "-s", service, "-a", account],
    { encoding: "buffer" },
  );
  if (result.status !== 0) {
    throw new Error(`failed to read keychain item: ${service}`);
  }
  let password = result.stdout;
  if (password[password.length - 1] === 0x0a) {
    password = password.subarray(0, password.length - 1);
  }
  return password;
}

function decryptCookieValue(encryptedValue, host, key) {
  if (encryptedValue.length < 4 || encryptedValue.subarray(0, 3).toString("ascii") !== "v10") {
    throw new Error("unsupported encrypted cookie prefix");
  }

  const iv = Buffer.alloc(16, 0x20);
  const decipher = crypto.createDecipheriv("aes-128-cbc", key, iv);
  const plaintext = Buffer.concat([
    decipher.update(encryptedValue.subarray(3)),
    decipher.final(),
  ]);

  const hostHash = crypto.createHash("sha256").update(host).digest();
  if (plaintext.length >= hostHash.length && plaintext.subarray(0, hostHash.length).equals(hostHash)) {
    return plaintext.subarray(hostHash.length);
  }
  return plaintext;
}

function sameSiteFromDb(value) {
  if (value === 0) return "None";
  if (value === 1) return "Lax";
  if (value === 2) return "Strict";
  return undefined;
}

function chromeTimeToUnixSeconds(chromeTime) {
  return Number((chromeTime - CHROME_EPOCH_DELTA) / 1000000n);
}

function buildUrlForHostCookie(host, sourceScheme, secure) {
  const scheme = sourceScheme === 2 || secure ? "https" : "http";
  return `${scheme}://${host}/`;
}

function readCookies() {
  const db = path.join(sourceProfile, "Default/Cookies");
  const nowChrome = BigInt(Date.now()) * 1000n + CHROME_EPOCH_DELTA;
  const password = readKeychainPassword(sourceService, sourceAccount);
  const key = crypto.pbkdf2Sync(password, "saltysalt", 1003, 16, "sha1");

  const sql = `
select
  hex(host_key),
  hex(name),
  hex(encrypted_value),
  hex(path),
  expires_utc,
  is_secure,
  is_httponly,
  samesite,
  source_scheme
from cookies
where length(encrypted_value) > 0
`;

  const rows = run("/usr/bin/sqlite3", ["-readonly", db, sql]);
  const cookies = [];
  let total = 0;
  let skippedExpired = 0;
  let decryptFailed = 0;

  for (const line of rows.split("\n")) {
    if (!line) continue;
    total += 1;

    const [
      hostHex,
      nameHex,
      encryptedHex,
      pathHex,
      expiresText,
      secureText,
      httpOnlyText,
      sameSiteText,
      sourceSchemeText,
    ] = line.split("|");

    const expiresChrome = BigInt(expiresText || "0");
    if (expiresChrome !== 0n && expiresChrome <= nowChrome) {
      skippedExpired += 1;
      continue;
    }

    const host = Buffer.from(hostHex, "hex").toString("utf8");
    const name = Buffer.from(nameHex, "hex").toString("utf8");
    const cookiePath = Buffer.from(pathHex, "hex").toString("utf8") || "/";
    const encryptedValue = Buffer.from(encryptedHex, "hex");
    const secure = secureText === "1";
    const sourceScheme = Number(sourceSchemeText);

    try {
      const value = decryptCookieValue(encryptedValue, host, key).toString("utf8");
      const cookie = {
        name,
        value,
        path: cookiePath,
        secure,
        httpOnly: httpOnlyText === "1",
      };

      if (host.startsWith(".")) {
        cookie.domain = host;
      } else {
        cookie.url = buildUrlForHostCookie(host, sourceScheme, secure);
      }

      if (expiresChrome !== 0n) {
        cookie.expires = chromeTimeToUnixSeconds(expiresChrome);
      }

      const sameSite = sameSiteFromDb(Number(sameSiteText));
      if (sameSite) {
        cookie.sameSite = sameSite;
      }

      cookies.push(cookie);
    } catch {
      decryptFailed += 1;
    }
  }

  return { cookies, total, skippedExpired, decryptFailed };
}

function countByDomain(cookies, patterns) {
  return cookies.filter((cookie) => {
    const domain = cookie.domain || "";
    const urlHost = cookie.url ? new URL(cookie.url).hostname : "";
    return patterns.some((pattern) => domain.includes(pattern) || urlHost.includes(pattern));
  }).length;
}

async function connectCdp() {
  if (typeof fetch !== "function" || typeof WebSocket !== "function") {
    throw new Error("Node.js with fetch and WebSocket support is required");
  }

  const version = await fetch(`${cdpUrl.replace(/\/$/, "")}/json/version`).then((response) => {
    if (!response.ok) {
      throw new Error(`CDP version request failed: ${response.status}`);
    }
    return response.json();
  });

  const ws = new WebSocket(version.webSocketDebuggerUrl);
  await new Promise((resolve, reject) => {
    ws.addEventListener("open", resolve, { once: true });
    ws.addEventListener("error", reject, { once: true });
  });

  let id = 0;
  const pending = new Map();
  ws.addEventListener("message", (event) => {
    const message = JSON.parse(event.data);
    if (!message.id || !pending.has(message.id)) {
      return;
    }
    const { resolve, reject } = pending.get(message.id);
    pending.delete(message.id);
    if (message.error) {
      reject(new Error(message.error.message));
    } else {
      resolve(message.result);
    }
  });

  return {
    async send(method, params = {}) {
      const messageId = ++id;
      ws.send(JSON.stringify({ id: messageId, method, params }));
      return new Promise((resolve, reject) => {
        pending.set(messageId, { resolve, reject });
      });
    },
    close() {
      ws.close();
    },
  };
}

async function applyCookies(cookies) {
  const cdp = await connectCdp();
  let setOk = 0;
  let setFailed = 0;

  try {
    for (let i = 0; i < cookies.length; i += 100) {
      const batch = cookies.slice(i, i + 100);
      try {
        await cdp.send("Storage.setCookies", { cookies: batch });
        setOk += batch.length;
      } catch {
        for (const cookie of batch) {
          try {
            await cdp.send("Storage.setCookies", { cookies: [cookie] });
            setOk += 1;
          } catch {
            setFailed += 1;
          }
        }
      }
    }

    const stored = await cdp.send("Storage.getCookies");
    return {
      setOk,
      setFailed,
      browserCookies: stored.cookies || [],
    };
  } finally {
    cdp.close();
  }
}

try {
  const { cookies, total, skippedExpired, decryptFailed } = readCookies();
  console.log("Wuu Browser Doubao cookie CDP import");
  console.log(`  mode:              ${mode}`);
  console.log(`  source:            ${sourceProfile}`);
  console.log(`  source cookies:    ${total}`);
  console.log(`  importable:        ${cookies.length}`);
  console.log(`  skipped expired:   ${skippedExpired}`);
  console.log(`  decrypt failed:    ${decryptFailed}`);
  console.log(`  twitter/x:         ${countByDomain(cookies, ["twitter.com", "x.com"])}`);
  console.log(`  bilibili:          ${countByDomain(cookies, ["bilibili.com"])}`);

  if (mode === "dry-run") {
    console.log("Dry run only. Re-run with --apply after Wuu Browser is running.");
  } else {
    const result = await applyCookies(cookies);
    console.log(`  set accepted:      ${result.setOk}`);
    console.log(`  set failed:        ${result.setFailed}`);
    console.log(`  browser cookies:   ${result.browserCookies.length}`);
    console.log(`  browser twitter/x: ${countByDomain(result.browserCookies, ["twitter.com", "x.com"])}`);
    console.log(`  browser bilibili:  ${countByDomain(result.browserCookies, ["bilibili.com"])}`);
  }
} catch (error) {
  console.error(error.message);
  process.exit(1);
}
