# Sidebar Functional Groups Design

## Goal

Make the desktop sidebar easier to scan by separating commands, pinned work,
collaboration, workspaces, and settings with a stable spacing rhythm.

## Structure

The sidebar uses fixed functional boundaries:

1. Primary actions: `新对话`, `搜索会话`, `Skills`.
2. `置顶`: shown only when at least one pinned conversation exists.
3. `协作`: contains `群聊` and `Agents`.
4. `工作区`: contains `对话` and real workspaces. Its trailing `+` opens the
   existing add-workspace menu.
5. `设置`: remains anchored at the bottom as a low-frequency utility.

The functional groups do not move. Users may reorder items within `协作` and
within `工作区`, but a drag cannot move an item across those boundaries.

## Spacing Rhythm

Use a 4px base rhythm:

- 4px between rows in the same group.
- 8px between a group label and its first row.
- 24px between different functional groups.
- 12px horizontal sidebar padding.
- 32px navigation and section-row height.

Whitespace provides the primary grouping signal. Avoid adding divider lines or
card containers. Group labels are small, quiet, and aligned with row text.

## Behavior

- Hide the entire `置顶` section when it has no rows.
- Keep existing section expansion, unread state, running state, context menus,
  and nested conversation behavior.
- Preserve existing stored relative order within each functional group while
  normalizing legacy cross-group orders into the new fixed group order.
- Keep the existing collapsed-sidebar drawer and responsive behavior.

## Validation

- Unit-test functional group DOM order, empty pinned behavior, add-workspace
  placement, and cross-group reorder rejection.
- Run the focused sidebar test files and the desktop typecheck.
- Verify the running Electron app at normal and narrow window sizes.

