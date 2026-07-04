import {
  afterEach,
  describe,
  expect,
  it,
} from "vitest";
import {
  mkdirSync,
  mkdtempSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import type { RuntimeContext } from "../shared/protocol";
import { WorkspaceFileService } from "./workspaceFiles";

const workspaces: string[] = [];

afterEach(() => {
  for (const workspace of workspaces.splice(0)) {
    rmSync(workspace, { recursive: true, force: true });
  }
});

function createWorkspace(): string {
  const workspace = mkdtempSync(join(tmpdir(), "wuu-workspace-files-"));
  workspaces.push(workspace);
  return workspace;
}

function writeWorkspaceFile(root: string, path: string): void {
  const absolutePath = join(root, path);
  mkdirSync(dirname(absolutePath), { recursive: true });
  writeFileSync(absolutePath, "ok\n");
}

function createService(root: string): WorkspaceFileService {
  const context: RuntimeContext = {
    kind: "no_project",
    cwd: root,
  };
  return new WorkspaceFileService(() => context);
}

describe("WorkspaceFileService file reference resolution", () => {
  it("resolves explicit workspace-relative file references", () => {
    const root = createWorkspace();
    writeWorkspaceFile(root, "internal/tools/tool_discovery.go");

    expect(createService(root).resolveFileReference("internal/tools/tool_discovery.go")).toMatchObject({
      root,
      reference: "internal/tools/tool_discovery.go",
      status: "resolved",
      path: "internal/tools/tool_discovery.go",
      absolute_path: join(root, "internal/tools/tool_discovery.go"),
    });
  });

  it("does not resolve missing bare filenames", () => {
    const root = createWorkspace();

    expect(createService(root).resolveFileReference("tool_search.go")).toMatchObject({
      root,
      reference: "tool_search.go",
      status: "missing",
    });
  });

  it("resolves bare filenames only when they are unique in the workspace", () => {
    const root = createWorkspace();
    writeWorkspaceFile(root, "internal/tools/tool_discovery.go");

    expect(createService(root).resolveFileReference("tool_discovery.go")).toMatchObject({
      root,
      reference: "tool_discovery.go",
      status: "resolved",
      path: "internal/tools/tool_discovery.go",
    });
  });

  it("does not guess when a bare filename has multiple matches", () => {
    const root = createWorkspace();
    writeWorkspaceFile(root, "app/README.md");
    writeWorkspaceFile(root, "docs/README.md");

    expect(createService(root).resolveFileReference("README.md")).toMatchObject({
      root,
      reference: "README.md",
      status: "ambiguous",
      matches: ["app/README.md", "docs/README.md"],
    });
  });

  it("strips line suffixes before resolving the file path", () => {
    const root = createWorkspace();
    writeWorkspaceFile(root, "README.md");

    expect(createService(root).resolveFileReference("README.md (line 19)")).toMatchObject({
      root,
      reference: "README.md (line 19)",
      status: "resolved",
      path: "README.md",
    });
  });

  it("keeps explicit root-relative references separate from ambiguous bare names", () => {
    const root = createWorkspace();
    writeWorkspaceFile(root, "README.md");
    writeWorkspaceFile(root, "docs/README.md");
    const service = createService(root);

    expect(service.resolveFileReference("README.md")).toMatchObject({
      status: "ambiguous",
    });
    expect(service.resolveFileReference("./README.md")).toMatchObject({
      status: "resolved",
      path: "README.md",
    });
  });
});

// Covers the worktree-fork panel-root gap: a thread's own cwd (e.g. a git
// worktree) can differ from the active project's runtime context. Callers
// pass that thread cwd as an explicit `root` override on each call; these
// tests prove the override actually redirects reads/listing there (and
// keeps its own containment checks) instead of silently falling back to
// the constructed context's cwd.
describe("WorkspaceFileService root override", () => {
  it("lists a directory rooted at the override, not the constructed context", () => {
    const defaultRoot = createWorkspace();
    writeWorkspaceFile(defaultRoot, "default-only.txt");
    const overrideRoot = createWorkspace();
    writeWorkspaceFile(overrideRoot, "override-only.txt");

    const service = createService(defaultRoot);
    const result = service.directoryList(undefined, overrideRoot);

    expect(result.root).toBe(overrideRoot);
    expect(result.entries.map((entry) => entry.name)).toEqual(["override-only.txt"]);
  });

  it("reads a file relative to the override root, not the constructed context", () => {
    const defaultRoot = createWorkspace();
    const overrideRoot = createWorkspace();
    writeWorkspaceFile(overrideRoot, "notes.txt");

    const service = createService(defaultRoot);
    const result = service.readFile("notes.txt", overrideRoot);

    expect(result.root).toBe(overrideRoot);
    expect(result.absolute_path).toBe(join(overrideRoot, "notes.txt"));
    expect(result.text).toBe("ok\n");
  });

  it("rejects a qualified file reference that escapes the override root even though it exists next to the constructed context", () => {
    const defaultRoot = createWorkspace();
    writeWorkspaceFile(defaultRoot, "secret.txt");
    // Nest the override root inside the default root so "../secret.txt"
    // from the override resolves to a real, readable file one directory
    // up — proving containment is enforced against the override root
    // itself, not merely "does the resolved path exist somewhere".
    const overrideRoot = join(defaultRoot, "worktree");
    mkdirSync(overrideRoot, { recursive: true });

    const service = createService(defaultRoot);

    expect(service.resolveFileReference("../secret.txt", overrideRoot)).toMatchObject({
      root: overrideRoot,
      status: "missing",
    });
  });

  it("resolves a file reference relative to the override root, not the constructed context", () => {
    const defaultRoot = createWorkspace();
    writeWorkspaceFile(defaultRoot, "shared-name.go");
    const overrideRoot = createWorkspace();
    writeWorkspaceFile(overrideRoot, "shared-name.go");

    const service = createService(defaultRoot);
    const result = service.resolveFileReference("shared-name.go", overrideRoot);

    expect(result).toMatchObject({
      root: overrideRoot,
      status: "resolved",
      path: "shared-name.go",
      absolute_path: join(overrideRoot, "shared-name.go"),
    });
  });

  it("falls back to the constructed context when no override root is given", () => {
    const defaultRoot = createWorkspace();
    writeWorkspaceFile(defaultRoot, "default-only.txt");

    const service = createService(defaultRoot);
    const result = service.directoryList();

    expect(result.root).toBe(defaultRoot);
    expect(result.entries.map((entry) => entry.name)).toEqual(["default-only.txt"]);
  });
});
