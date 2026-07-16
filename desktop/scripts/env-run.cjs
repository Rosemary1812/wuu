#!/usr/bin/env node
// Cross-shell replacement for the POSIX `KEY=value command` prefix, which
// cmd.exe and PowerShell do not understand. Usage:
//   node scripts/env-run.cjs KEY=value [KEY2=value2 ...] command [args...]
// Leading KEY=value tokens land in the child's environment; the rest is the
// command line. npm has already prepended node_modules/.bin to PATH, so bare
// tool names (vitest, electron-vite) resolve on every platform.
const { spawn } = require("node:child_process");

const args = process.argv.slice(2);
const env = { ...process.env };
let index = 0;
while (index < args.length && /^[A-Za-z_][A-Za-z0-9_]*=/.test(args[index])) {
  const separator = args[index].indexOf("=");
  env[args[index].slice(0, separator)] = args[index].slice(separator + 1);
  index += 1;
}

const command = args[index];
if (!command) {
  console.error("usage: env-run.cjs KEY=value ... command [args...]");
  process.exit(2);
}

const child = spawn(command, args.slice(index + 1), {
  stdio: "inherit",
  env,
  // Windows tool shims are .cmd batch files; only a shell can start those.
  shell: process.platform === "win32",
});
child.on("error", (error) => {
  console.error(`env-run: ${error.message}`);
  process.exit(1);
});
child.on("exit", (code, signal) => {
  process.exit(code ?? (signal ? 1 : 0));
});
