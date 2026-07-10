# Sidebar Functional Groups Implementation Plan

> **Execution note:** Use `executing-plans` to implement this plan task-by-task, or another equivalent execution workflow supported by the current agent runtime.

**Goal:** Reorganize the desktop sidebar into fixed Apple-style functional groups with a consistent spacing rhythm.

**Architecture:** Keep the existing sidebar row and collapse components, but add top-level functional group wrappers in `AppSidebar`. Normalize persisted section order by group and reject cross-group drag reordering so customization remains available inside each group without weakening the information architecture.

**Tech Stack:** React 19, TypeScript, dnd-kit, CSS, Vitest, jsdom.

---

### Task 1: Lock the functional-group behavior with tests

**Files:**
- Modify: `desktop/src/renderer/AppSidebar.test.tsx`
- Modify: `desktop/src/renderer/AppSidebarSections.test.tsx`

**Step 1: Write the failing layout tests**

Add assertions that:

- primary actions render as `新对话`, `搜索会话`, `Skills`;
- an empty pinned section is absent;
- a non-empty pinned section appears before `协作`;
- `群聊` and `Agents` render inside `协作`;
- `对话` and real projects render inside `工作区`;
- the add-workspace button and menu live in the `工作区` header.

**Step 2: Write the failing order tests**

Add assertions that legacy stored orders are normalized into collaboration then
workspace groups while preserving relative order inside each group. Add a test
that `reorderSidebarSections` returns the original array for a cross-group drag.

**Step 3: Run the focused tests to verify failure**

Run:

```bash
cd desktop && npx vitest run src/renderer/AppSidebar.test.tsx src/renderer/AppSidebarSections.test.tsx
```

Expected: FAIL because the current sidebar is flat, always renders `置顶`, and
keeps add-workspace in the primary navigation.

### Task 2: Implement fixed functional groups

**Files:**
- Modify: `desktop/src/renderer/AppSidebar.tsx`

**Step 1: Normalize section order by functional group**

Update `reconcileSidebarSectionOrder` so `群聊` and `Agents` are always emitted
before `对话` and real projects, while preserving stored relative order inside
each group.

**Step 2: Block cross-group drag moves**

Classify section ids as collaboration or workspace. Return the original order
when drag source and destination belong to different groups.

**Step 3: Render the new hierarchy**

Render:

```text
primary-nav
sidebar-main
  pinned functional group, when non-empty
  collaboration functional group
  workspace functional group with add action
sidebar-settings
```

Move the existing add-workspace menu into the workspace group header. Keep all
existing section row contents and callbacks.

**Step 4: Run the focused tests**

Run:

```bash
cd desktop && npx vitest run src/renderer/AppSidebar.test.tsx src/renderer/AppSidebarSections.test.tsx
```

Expected: PASS.

### Task 3: Apply the spacing rhythm

**Files:**
- Modify: `desktop/src/renderer/styles/sidebar.css`
- Test: `desktop/src/renderer/AppSidebar.test.tsx`

**Step 1: Add failing CSS assertions**

Assert the functional group tokens and selectors use 4px internal gaps, 8px
header-to-content spacing, and 24px functional-group spacing.

**Step 2: Run the focused layout test to verify failure**

Run:

```bash
cd desktop && npx vitest run src/renderer/AppSidebar.test.tsx
```

Expected: FAIL because the group selectors and spacing tokens do not exist.

**Step 3: Implement the CSS**

Add quiet group headings, a compact icon-only workspace action, constrained menu
placement, and overrides that remove the old uniform 14px section margins inside
functional groups. Do not add separators or cards.

**Step 4: Run focused verification**

Run:

```bash
cd desktop && npx vitest run src/renderer/AppSidebar.test.tsx src/renderer/AppSidebarSections.test.tsx
```

Expected: PASS.

### Task 4: Verify the product path

**Files:**
- No source changes expected.

**Step 1: Run typecheck and the complete desktop test suite**

Run:

```bash
cd desktop && npm test
```

Expected: TypeScript and all Vitest tests pass.

**Step 2: Restart the Electron development stack if needed**

Because this change touches renderer code only, Vite should reload it. If the
running UI is stale or inconsistent, restart `electron-vite dev` before visual
verification.

**Step 3: Verify visually**

Check the open sidebar at normal and narrow window sizes. Confirm group labels,
spacing, scroll behavior, workspace menu placement, collapsed drawer behavior,
and text truncation.

**Step 4: Commit the implementation**

```bash
git add desktop/src/renderer/AppSidebar.tsx \
  desktop/src/renderer/AppSidebar.test.tsx \
  desktop/src/renderer/AppSidebarSections.test.tsx \
  desktop/src/renderer/styles/sidebar.css
git commit -m "feat(desktop): group sidebar by function"
```
