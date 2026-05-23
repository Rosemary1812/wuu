const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { pathToFileURL } = require("node:url");
const { app, BrowserWindow } = require("electron");

const desktopRoot = path.resolve(__dirname, "..");
const repoRoot = path.resolve(desktopRoot, "..");
const mainBundle = path.join(desktopRoot, "out", "main", "index.js");
const home = fs.mkdtempSync(path.join(os.tmpdir(), "wuu-desktop-setup-"));
const userData = path.join(home, "userData");
const documents = path.join(home, "Documents");
const scenario = process.env.WUU_SETUP_E2E_SCENARIO || "missing-config";

fs.mkdirSync(userData, { recursive: true });
fs.mkdirSync(documents, { recursive: true });
seedScenario();

process.env.HOME = home;
process.env.WUU_SOURCE_ROOT = repoRoot;
process.env.OPENAI_API_KEY = "";
process.env.ANTHROPIC_API_KEY = "";
app.setPath("userData", userData);
app.setPath("documents", documents);
app.commandLine.appendSwitch("disable-gpu");
app.commandLine.appendSwitch("disable-software-rasterizer");

run().catch(fail);

async function run() {
  assert.ok(fs.existsSync(mainBundle), "Main build is missing. Run npm run build first.");

  await import(pathToFileURL(mainBundle).href);
  await app.whenReady();

  const win = await waitForWindow(10000);
  win.webContents.on("render-process-gone", (_event, details) => {
    fail(new Error(`Renderer process exited: ${details.reason}`));
  });
  win.webContents.on("console-message", (_event, level, message, line, sourceId) => {
    if (level >= 2) {
      console.error(`renderer console: ${message} (${sourceId}:${line})`);
    }
  });

  await waitFor(win, () => Boolean(document.querySelector(".runtime-setup")), 30000);
  const heading = await evaluate(win, () => document.querySelector(".runtime-setup-heading h2")?.textContent?.trim());
  assert.equal(heading, "连接模型", "Missing config should show the runtime setup form.");

  await setField(win, "api-key", "sk-desktop-setup-test");
  await waitFor(win, () => {
    const button = document.querySelector("[data-runtime-setup-submit]");
    return button instanceof HTMLButtonElement && !button.disabled;
  }, 1000);
  await evaluate(win, () => {
    const button = document.querySelector("[data-runtime-setup-submit]");
    if (!(button instanceof HTMLButtonElement)) {
      throw new Error("runtime setup submit button not found");
    }
    button.click();
  });

  await waitFor(win, () => Boolean(document.querySelector(".scroll-region")), 30000);
  assert.equal(
    await evaluate(win, () => Boolean(document.querySelector(".runtime-setup"))),
    false,
    "Runtime setup form should close after saving a usable config."
  );

  const configPath = path.join(home, ".config", "wuu", "config.json");
  const authPath = path.join(home, ".config", "wuu", "auth.json");
  const preferencesPath = path.join(home, ".config", "wuu", "preferences.json");
  const config = JSON.parse(fs.readFileSync(configPath, "utf8"));
  const auth = JSON.parse(fs.readFileSync(authPath, "utf8"));
  const preferences = JSON.parse(fs.readFileSync(preferencesPath, "utf8"));

  const expectedProvider = scenario === "missing-key" ? "anthropic" : "openai";
  const expectedType = scenario === "missing-key" ? "anthropic" : "openai-compatible";

  assert.equal(config.default_provider, expectedProvider);
  assert.equal(config.providers[expectedProvider].type, expectedType);
  assert.equal(config.providers[expectedProvider].api_key, undefined);
  assert.equal(auth.keys[expectedProvider], "sk-desktop-setup-test");
  assert.equal(preferences.has_completed_onboarding, true);

  console.log(`setup e2e passed: ${scenario}`);
  app.quit();
}

function seedScenario() {
  if (scenario === "missing-config") {
    return;
  }
  if (scenario !== "missing-key") {
    throw new Error(`unknown setup e2e scenario: ${scenario}`);
  }
  const configPath = path.join(home, ".config", "wuu", "config.json");
  fs.mkdirSync(path.dirname(configPath), { recursive: true });
  fs.writeFileSync(
    configPath,
    `${JSON.stringify(
      {
        default_provider: "anthropic",
        providers: {
          anthropic: {
            type: "anthropic",
            base_url: "https://api.anthropic.com",
            api_key_env: "ANTHROPIC_API_KEY",
            model: "claude-3-5-sonnet-latest"
          }
        },
        agent: {
          max_steps: 0,
          temperature: 0.2
        }
      },
      null,
      2
    )}\n`
  );
}

async function waitForWindow(timeoutMs) {
  const started = Date.now();
  while (Date.now() - started < timeoutMs) {
    const win = BrowserWindow.getAllWindows()[0];
    if (win && !win.isDestroyed()) {
      return win;
    }
    await delay(50);
  }
  throw new Error("Timed out waiting for main window.");
}

async function setField(win, field, value) {
  await evaluate(
    win,
    ({ field: fieldName, value: nextValue }) => {
      const input = document.querySelector(`[data-runtime-setup-field="${fieldName}"]`);
      if (!(input instanceof HTMLInputElement)) {
        throw new Error(`runtime setup field not found: ${fieldName}`);
      }
      const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
      setter?.call(input, nextValue);
      input.dispatchEvent(new Event("input", { bubbles: true }));
    },
    { field, value }
  );
}

async function waitFor(win, predicate, timeoutMs) {
  const started = Date.now();
  let lastValue;
  while (Date.now() - started < timeoutMs) {
    lastValue = await evaluate(win, predicate);
    if (lastValue) {
      return lastValue;
    }
    await delay(50);
  }
  throw new Error(`Timed out waiting for condition. Last value: ${JSON.stringify(lastValue)}`);
}

async function evaluate(win, fn, arg) {
  const source = `(${fn})(${JSON.stringify(arg)})`;
  return win.webContents.executeJavaScript(source, true);
}

function delay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function fail(error) {
  console.error(error);
  app.exit(1);
}
