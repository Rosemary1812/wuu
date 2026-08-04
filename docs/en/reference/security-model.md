# Security Model

Wuu is a local coding agent. It reads project context, sends selected context to
a model provider, and can run tools on the user's machine. That is useful
authority, so a repository opened in Wuu must be treated more like executable
code than a passive document.

This document describes the current boundary. It is not a claim that model
output is safe or that Wuu is an operating-system sandbox.

## Local authority

Wuu has three permission modes:

| Mode | File reach | Mutations |
|---|---|---|
| `standard` | Registered workspace roots | Allowed inside those roots |
| `read_only` | Registered workspace roots | Denied |
| `unconfined` | No Wuu workspace restrictions | Allowed wherever the current OS user has permission |

The default `standard` mode enforces paths and tool rules inside the Wuu
process. It is **not** a macOS sandbox, container, virtual machine, or separate
OS user. A permitted child process runs with the Wuu process's operating-system
identity, inherited environment, and network stack. The path boundary and hard
tool guards reduce mistakes, but they are not a security boundary against
malicious native code or a compromised dependency.

`unconfined` means that Wuu no longer applies its own path boundary. The agent
can read or modify anything available to the current OS user, including the
user's home directory, other repositories, and configuration files. Wuu's
dedicated file tools still refuse writes to known sensitive paths, and its
structured Git tools still refuse to stage or commit them. The same dedicated
tools block direct access to Wuu credential files under `~/.wuu` (`auth.json`,
`credentials.json`, `remote.json`, and `phone.json`). These are tool-specific
guards, not guarantees about every execution path: arbitrary shell commands,
scripts, and child processes run with the Wuu process's OS authority and can
bypass them. Common secret patterns are still masked in tool output in every
mode, including `unconfined`, but redaction cannot recognize every secret or
indirect disclosure.

`unconfined` does not grant permissions beyond the current OS user, so file
ownership and ACLs, macOS privacy controls, System Integrity Protection,
read-only filesystems, and any container or OS sandbox still apply. Treat it as
full local authority at the current user's privilege level and enable it only
for trusted tasks.

In `standard` and `read_only` modes, commands that expose the whole environment,
read common credential paths, use unsafe Git operations, or perform
package/network mutations receive extra classification or hard checks. Tool
output is redacted for common secret patterns. These checks are defense in
depth, not a guarantee that every secret format or indirect access path will be
recognized.

Use a disposable VM, container, or separate OS account when working with a
repository you do not trust. Do not rely on a permission label as a substitute
for OS isolation.

## Data sent to model providers

Depending on the task and enabled features, Wuu may send the configured model
provider:

- user messages and conversation history;
- system prompts and applicable instruction files;
- file contents, search results, diffs, command output, and tool errors;
- configured user and workspace memory;
- images or other attachments the user adds;
- MCP or hook output returned to the agent.

Wuu does not intentionally place provider API keys in prompts. Keys may still
be exposed if a command, hook, MCP server, file, or user message prints them in
an unrecognized form. Review the privacy and retention terms of the selected
provider. A custom OpenAI-compatible base URL receives the same prompt data as
the provider it replaces.

## Trusted configuration and project input

The user config at `~/.wuu/config.json` (or `WUU_HOME/config.json`) is the
trusted base. Normal startup filters project attempts to replace provider
destinations, credential environment names, outside-workspace memory paths,
model roles, or the permission mode. `wuu exec --config` and
`--ignore-user-config` explicitly trust the selected project configuration and
are intended for controlled automation.

The following project content can affect agent behavior and must be reviewed:

- `AGENTS.md` and other discovered instruction files;
- `.wuu.json`, `wuu.json`, `.wuu/settings.json`, and local settings;
- project skills and prompt additions;
- hooks and plugins;
- `.mcp.json` and native MCP server configuration.

Project MCP entries are not loaded until approved in local settings. Approval
means trusting that server or subprocess with the files, environment, network,
and credentials available to it; MCP output is also untrusted model input.
Hooks and local skills can carry prompt injection even when they do not execute
native code.

## Credentials and local state

- Provider keys are normally read from the environment named by
  `providers.<name>.api_key_env`.
- The macOS desktop stores managed OAuth credentials in Keychain and does not
  silently fall back to a plaintext file if Keychain fails.
- Headless flows can explicitly use `~/.wuu/auth.json` and
  `~/.wuu/credentials.json` (or the matching `WUU_HOME` paths). Wuu writes
  credential files with owner-only permissions, but their contents are not a
  replacement for an OS keychain.
- Remote identity and enrolled-device data live in `~/.wuu/remote.json`; phone
  credentials use `~/.wuu/phone.json`. These files are also owner-only and
  should be backed up and shared as secrets.
- Sessions, logs, tool results, memory, and workspace state live under
  `~/.wuu`. They can contain source code and conversation content even when
  common credential patterns have been redacted.

Do not include `~/.wuu`, environment files, logs, session exports, or diagnostic
bundles in bug reports without reviewing them first.

## App server and desktop boundary

`wuu app-server` uses a newline-delimited JSON request/response protocol over the
subprocess's standard input and output.
It does not open a network listener by itself. The Electron shell starts and
owns this process and exposes selected operations to the renderer through its
preload/IPC bridge. A renderer-to-main IPC bypass or an unsafe navigation is a
security issue.

## Remote and mobile control

`wuu relay` listens on `127.0.0.1:8787` by default. Binding it to another
address exposes it to that network and requires the operator to provide TLS and
deployment controls appropriate for the environment.

The remote host dials the relay; it does not open an inbound workspace server.
Phones enroll through a time-limited pairing link. Enrolled devices authenticate
with signed keys, and application frames are end-to-end encrypted between host
and phone. The relay routes opaque frames and content-free push hints, but it
still sees connection timing, device presence, pairing identifiers, and network
addresses. Anyone who gets a live pairing link or a phone credential file may
be able to enroll or act as that device.

Treat a paired phone as a remote controller with the same workspace authority
as the host session. Revoke devices that are lost or no longer trusted.

## Safe use checklist

1. Review a new repository's instructions, settings, hooks, skills, and MCP
   configuration before enabling tools.
2. Use `read_only` for inspection and a real OS sandbox for hostile code.
3. Keep provider endpoints and credential environment names in the user config.
4. Pair remote devices in private and use `wss://` for relays across untrusted
   networks.
5. Review diffs and command output before publishing them.
6. Report boundary bypasses privately using [SECURITY.md](../../../SECURITY.md).
