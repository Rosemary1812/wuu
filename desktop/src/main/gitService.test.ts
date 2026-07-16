import { execFileSync } from "node:child_process";
import {
  mkdtempSync,
  mkdirSync,
  realpathSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import type { RuntimeContext } from "../shared/protocol";
import { GitService, gitWorkingTreeBusy } from "./gitService";

const roots: string[] = [];

function makeRepository(): string {
  const root = mkdtempSync(join(tmpdir(), "wuu-git-service-"));
  roots.push(root);
  execFileSync("git", ["init", "-q", root]);
  execFileSync("git", ["-C", root, "config", "core.hooksPath", "/dev/null"]);
  execFileSync("git", ["-C", root, "config", "user.email", "test@example.com"]);
  execFileSync("git", ["-C", root, "config", "user.name", "Wuu Test"]);
  execFileSync("git", ["-C", root, "config", "commit.gpgsign", "false"]);
  writeFileSync(join(root, "README.md"), "workspace\n");
  execFileSync("git", ["-C", root, "add", "README.md"]);
  execFileSync("git", ["-C", root, "commit", "-qm", "initial"]);
  return root;
}

function serviceFor(root: string, runningThreadCwds: string[] = []): GitService {
  const context: RuntimeContext = { kind: "no_project", cwd: root };
  return new GitService(() => context, () => runningThreadCwds);
}

afterEach(() => {
  for (const root of roots.splice(0)) {
    rmSync(root, { recursive: true, force: true });
  }
});

describe("GitService file previews", () => {
  it("returns ignored text files as complete new-file previews", () => {
    const root = makeRepository();
    writeFileSync(join(root, ".gitignore"), "/docs/plans/*\n");
    mkdirSync(join(root, "docs/plans"), { recursive: true });
    writeFileSync(join(root, "docs/plans/brief.md"), "# Brief\n\nBody\n");

    const result = serviceFor(root).fileDiff("docs/plans/brief.md");

    expect(result.status).toBe("ignored");
    expect(result.additions).toBe(3);
    expect(result.patch).toContain("new file mode 100644");
    expect(result.patch).toContain("+# Brief");
    expect(result.patch).toContain("+Body");
  });

  it("keeps ignored files out of the workspace Git change list", () => {
    const root = makeRepository();
    writeFileSync(join(root, ".gitignore"), "ignored.txt\n");
    writeFileSync(join(root, "ignored.txt"), "private draft\n");

    expect(
      serviceFor(root)
        .changes()
        .files.map((file) => file.path),
    ).not.toContain("ignored.txt");
  });
});

describe("GitService worktree roots", () => {
  it("returns the same root for sibling directories in one working tree", () => {
    const root = makeRepository();
    const frontend = join(root, "frontend");
    const backend = join(root, "backend");
    mkdirSync(frontend);
    mkdirSync(backend);
    const service = serviceFor(root);

    const frontendRoot = service.worktreeRoot(frontend);
    expect(service.worktreeRoot(backend)).toBe(frontendRoot);
    expect(realpathSync(frontendRoot)).toBe(realpathSync(root));
  });

  it("treats a non-Git running cwd as unresolved", () => {
    const repository = makeRepository();
    const root = mkdtempSync(join(tmpdir(), "wuu-non-git-service-"));
    roots.push(root);

    expect(() => serviceFor(root).worktreeRoot(root)).toThrow();
    expect(gitWorkingTreeBusy(repository, [root])).toBe(true);
  });

  it("stays locked when the target root cannot be resolved", () => {
    const root = mkdtempSync(join(tmpdir(), "wuu-non-git-service-"));
    roots.push(root);

    expect(gitWorkingTreeBusy(root, [])).toBe(true);
  });

  it("matches symlinked sibling paths to the same working tree", () => {
    const root = makeRepository();
    const frontend = join(root, "frontend");
    const backend = join(root, "backend");
    const alias = mkdtempSync(join(tmpdir(), "wuu-git-alias-"));
    rmSync(alias, { recursive: true, force: true });
    roots.push(alias);
    mkdirSync(frontend);
    mkdirSync(backend);
    symlinkSync(root, alias, "dir");

    expect(gitWorkingTreeBusy(frontend, [join(alias, "backend")])).toBe(true);
  });

  it("allows a running thread in an independent linked worktree", () => {
    const root = makeRepository();
    const linked = mkdtempSync(join(tmpdir(), "wuu-linked-worktree-"));
    rmSync(linked, { recursive: true, force: true });
    roots.push(linked);
    execFileSync("git", ["-C", root, "worktree", "add", "-qb", "linked", linked]);

    expect(gitWorkingTreeBusy(root, [linked])).toBe(false);
  });

  it("rechecks the authoritative running cwd before checkout", () => {
    const root = makeRepository();
    const frontend = join(root, "frontend");
    const backend = join(root, "backend");
    mkdirSync(frontend);
    mkdirSync(backend);
    execFileSync("git", ["-C", root, "branch", "feature"]);

    expect(() => serviceFor(frontend, [backend]).checkoutBranch("feature")).toThrow(
      "cannot run Git actions while a thread is running in this working tree",
    );
  });
});
