# Read-only system events implementation plan

## Step 1: Remove action policy from the core protocol

- Update `internal/appserver/protocol.go`, `turn_error.go`, and notification construction.
- Update `packages/protocol/src/index.ts`.
- Rewrite focused Go tests so they assert diagnostic classification without action recommendations.
- Run the focused app-server error tests and protocol TypeScript checks.
- Commit as one core/protocol change.

## Step 2: Replace the renderer error/action model

- Simplify `UserFacingErrors.ts` to produce read-only display data.
- Keep user-facing title classification and diagnostic facts separate.
- Remove all action translation and debug-copy payload construction.
- Update focused classifier tests, including the 404 plus `internal_error` case.
- Commit as one renderer-model change.

## Step 3: Unify system event rendering

- Introduce one read-only system event primitive in `TurnNotice.tsx`.
- Route failure, missing reply, reconnect, compaction, and handoff displays through it.
- Remove `onNoticeAction` plumbing from App, cached panes, split panes, turn views, assistant shells, and item views.
- Remove action/code CSS and update the component gallery.
- Add DOM tests that reject buttons, links, visible machine codes, and multiple visible labels.
- Commit as one desktop UI change.

## Step 4: Verify the real product path

- Run focused renderer tests first, then the desktop test suite.
- Run relevant Go tests.
- Start or refresh the desktop development stack if required and inspect representative neutral, warning, progress, and error events.
- Confirm production debug-control rules are unaffected.
- Commit only any necessary verification-driven fixes as separate atomic changes.
