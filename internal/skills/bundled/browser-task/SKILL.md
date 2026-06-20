---
name: browser-task-removed
description: Browser task skill has been removed.
user-invocable: false
---

This skill was removed because it declared non-existent `browser` and
`screenshot` tools (audit P0-1). Drive browser automation directly through
`bash` — for example `npx playwright ...`, `curl`, or
`npx playwright screenshot` — instead of relying on a dedicated skill. The
file is kept as a tombstone so older references resolve to a clear removal
notice rather than silently disappearing.