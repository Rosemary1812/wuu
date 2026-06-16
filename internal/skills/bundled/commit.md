---
name: commit
description: Create a well-structured git commit from staged or unstaged changes
user-invocable: true
when_to_use: When the user asks to commit changes, create a commit, or save their work
---

Create a git commit using the structured git tool, not shell git commands:

1. Call `git` with `subcommand="status"` to see staged, unstaged, and untracked changes.
2. Call `git` with `subcommand="diff"` and, when staged files exist, `args=["--cached"]` to understand the changes.
3. Call `git` with `subcommand="log"` and `args=["--oneline", "-5"]` to match the repository's commit style.
4. Stage only intended files by calling `git` with `subcommand="add"`, explicit workspace-relative paths from status, and `confirm_user_approved=true` after the user has asked to create the commit.
5. If unrelated files are staged, call `git` with `subcommand="restore --staged"` and explicit paths before committing.
6. Draft a concise commit message focusing on the "why".
7. Call `git` with `subcommand="commit"`, `args=["-m", "..."]`, and `confirm_user_approved=true`.
8. Do not push unless the user explicitly asked for a remote write; when pushing, call `git` with `subcommand="push"`, `confirm_user_approved=true`, and `confirm_remote_write=true`.

Never stage root/current-directory pathspecs, wildcards, pathspec magic, or sensitive credential paths. If a requested commit would require staging a sensitive path, stop and ask for explicit secret handling.
