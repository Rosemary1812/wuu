# Security Policy

This document explains how to report security vulnerabilities in wuu.

## Supported versions

Only the latest minor release line of wuu receives security fixes. Older
versions are not patched.

| Version | Supported          |
|---------|--------------------|
| latest  | :white_check_mark: |
| older   | :x:                |

## Reporting a vulnerability

Please do **not** open a public issue for security problems.

Report privately through one of the following channels:

1. **GitHub Security Advisories** (preferred): open a private advisory from
   the project's Security tab.
2. **Email**: see the maintainer address in the recent commit history.

Please include:

- A clear description of the issue and its impact
- Reproduction steps, ideally with a minimal repo or transcript
- The affected version (`wuu --version`)
- Any known mitigations or workarounds

We will acknowledge new reports within 5 business days and aim to ship a
fix or mitigation within 30 days for critical issues. We will coordinate
disclosure timing with you and credit reporters who request it.

## Scope

In scope:

- Provider API key handling, `.wuu.json` parsing, and secret persistence
- Tool execution sandboxing and command escaping
- Renderer / main process IPC in the desktop shell
- Network requests, TLS, and proxy handling

Out of scope:

- Issues in third-party model providers (report upstream)
- Issues that only reproduce with `--dangerously-skip-permissions` or other
  explicitly opt-in unsafe behavior
- Local CLI behavior around the user's own API keys (key hygiene is the
  user's responsibility)
