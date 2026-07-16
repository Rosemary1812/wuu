---
name: commit
description: Create a well-structured git commit from staged or unstaged changes
user-invocable: true
trigger-condition: When the user asks to commit changes, create a commit, or save their work.
allowed-tools: [bash]
required-context: [current git status, intended files for the commit, commit message focus]
examples: [commit a staged bug fix, atomic commit before opening a pull request, save work-in-progress]
verification-checklist: [git status --short shows only intended files, git log -1 shows the commit, no unrelated user changes included]
progressive-disclosure: Inspect git status and the current diff first; only stage files that belong to the same logical change.
---

Create a git commit with normal non-interactive shell git commands:

1. Use `bash` to inspect `git status --short`, `git diff`, `git diff --cached` when staged files exist, and `git log --oneline -5`.
2. Identify exactly which files belong in the commit. Do not include unrelated user work.
3. Stage only intended files with explicit paths, for example `git add src/file.ts tests/file_test.ts`. Never use `git add .`, `git add -A`, root/current-directory pathspecs, wildcards, pathspec magic, or sensitive credential paths.
4. If unrelated files are already staged, unstage them with explicit paths, for example `git restore --staged path/to/file`.
5. Draft a concise commit message focusing on the "why".
6. Prefer one short shell transaction for tightly coupled staging and committing, for example `git add path/to/file && git commit -m "message"`, after you have inspected the diff. This keeps the shared Git index window small when multiple sessions use the same repo.
7. Run `git status --short` after the commit to verify the result.
8. Do not push unless the user explicitly asked for a remote write.
9. Preserve existing `Co-authored-by` trailers, including trailers written by other tools. WUU adds its own trailer through the Git execution layer when attribution is enabled; never change author or committer Git configuration to add attribution.

Never update git config, skip hooks (`--no-verify`, `--no-gpg-sign`, etc.), amend, force push, run destructive git commands (`reset --hard`, `clean -f`, broad `checkout`/`restore`), or use interactive/editor-driven git flows unless the user explicitly requested that exact action and the runtime permits it. If a requested commit would require staging a sensitive path, stop and ask for explicit secret handling.
