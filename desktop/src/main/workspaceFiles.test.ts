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
