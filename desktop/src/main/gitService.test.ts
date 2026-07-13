import { execFileSync } from "node:child_process";
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import type { RuntimeContext } from "../shared/protocol";
import { GitService } from "./gitService";

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

function serviceFor(root: string): GitService {
  const context: RuntimeContext = { kind: "no_project", cwd: root };
  return new GitService(() => context);
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
