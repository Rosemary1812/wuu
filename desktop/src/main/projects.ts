import { app } from "electron";
import { mkdirSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { basename, dirname, join, resolve } from "node:path";
import type {
  DesktopProject,
  ProjectListResult,
  RuntimeContext,
} from "../shared/protocol";

type ProjectStore = {
  projects: DesktopProject[];
  active_context?: RuntimeContext;
};

export class ProjectManager {
  private store: ProjectStore = { projects: [] };

  load(): void {
    this.store = loadProjectStore();
  }

  list(): ProjectListResult {
    this.load();
    const context = this.ensureRuntimeContext();
    return {
      // Annotate (without persisting) each project with whether its directory
      // still exists, so the sidebar can grey out and lock a workspace whose
      // folder was moved away or deleted.
      projects: this.store.projects.map((project) => ({
        ...project,
        missing: !isDirectory(project.path),
      })),
      active_context: context,
      active_project_id:
        context.kind === "project" ? context.project_id : undefined,
    };
  }

  activeWorkdir(): string | undefined {
    return this.store.active_context
      ? resolve(this.store.active_context.cwd)
      : undefined;
  }

  ensureRuntimeContext(): RuntimeContext {
    const active = this.store.active_context;
    if (active?.kind === "project") {
      const project = this.store.projects.find(
        (candidate) => candidate.id === active.project_id,
      );
      if (project && isDirectory(project.path)) {
        const context: RuntimeContext = {
          kind: "project",
          project_id: project.id,
          cwd: project.path,
        };
        if (active.cwd !== project.path) {
          this.store.active_context = context;
          this.save();
        }
        return context;
      }
    }
    if (active?.kind === "no_project") {
      return active;
    }
    const fallback = createNoProjectContext();
    this.store.active_context = fallback;
    this.save();
    return fallback;
  }

  add(projectPath: string): ProjectListResult {
    this.load();
    const resolvedPath = resolve(projectPath);
    if (!isDirectory(resolvedPath)) {
      throw new Error("selected project is not a directory");
    }
    const now = new Date().toISOString();
    const id = projectID(resolvedPath);
    const existingIndex = this.store.projects.findIndex(
      (project) => project.id === id,
    );
    const project: DesktopProject = {
      id,
      name: projectName(resolvedPath),
      path: resolvedPath,
      created_at:
        existingIndex >= 0 ? this.store.projects[existingIndex].created_at : now,
      updated_at: now,
    };
    if (existingIndex >= 0) {
      this.store.projects[existingIndex] = project;
    } else {
      this.store.projects = [project, ...this.store.projects];
    }
    this.store.active_context = {
      kind: "project",
      project_id: id,
      cwd: resolvedPath,
    };
    this.save();
    return this.list();
  }

  select(projectIDToSelect: string): ProjectListResult {
    this.load();
    const project = this.store.projects.find(
      (candidate) => candidate.id === projectIDToSelect,
    );
    if (!project) {
      throw new Error("project not found");
    }
    this.store.active_context = {
      kind: "project",
      project_id: project.id,
      cwd: project.path,
    };
    this.save();
    return this.list();
  }

  remove(projectIDToRemove: string): ProjectListResult {
    this.load();
    this.store.projects = this.store.projects.filter(
      (project) => project.id !== projectIDToRemove,
    );
    // If the removed project was the active context, ensureRuntimeContext
    // (called by list()) reconciles the now-dangling project_id down to the
    // shared 对话 workspace. This is an explicit user removal, not a silent
    // fallback masking a missing directory. The project's threads stay in the
    // sessions DB (they simply stop matching any registered project), so
    // re-adding the folder restores them.
    this.save();
    return this.list();
  }

  selectNoProject(fresh: boolean, cwd?: string): ProjectListResult {
    this.load();
    if (!fresh && cwd) {
      const resolvedCwd = resolve(cwd);
      mkdirSync(resolvedCwd, { recursive: true });
      this.store.active_context = { kind: "no_project", cwd: resolvedCwd };
    } else if (fresh || this.store.active_context?.kind !== "no_project") {
      this.store.active_context = createNoProjectContext();
    }
    this.save();
    return this.list();
  }

  private save(): void {
    writeProjectStoreFile(projectStorePath(), this.store);
  }
}

function projectStorePath(): string {
  return join(wuuHomePath(), "projects.json");
}

function legacyProjectStorePath(): string {
  return join(app.getPath("userData"), "projects.json");
}

export function wuuHomePath(): string {
  const override = process.env.WUU_HOME?.trim();
  if (override) {
    return resolve(override);
  }
  return join(homedir(), ".wuu");
}

function loadProjectStore(): ProjectStore {
  const loaded = readProjectStoreFile(projectStorePath());
  const legacy = readProjectStoreFile(legacyProjectStorePath());
  const { store, changed } = mergeProjectStores(
    loaded ?? { projects: [] },
    legacy,
  );
  if (!loaded || changed) {
    writeProjectStoreFile(projectStorePath(), store);
  }
  return store;
}

function readProjectStoreFile(path: string): ProjectStore | undefined {
  try {
    const parsed = JSON.parse(
      readFileSync(path, "utf8"),
    ) as Partial<ProjectStore> & {
      active_project_id?: unknown;
    };
    const projects = Array.isArray(parsed.projects)
      ? parsed.projects.filter((project): project is DesktopProject =>
          isDesktopProject(project),
        )
      : [];
    const activeContext =
      normalizeRuntimeContext(parsed.active_context, projects) ??
      legacyProjectContext(parsed.active_project_id, projects);
    return {
      projects,
      active_context: activeContext,
    };
  } catch {
    return undefined;
  }
}

function mergeProjectStores(
  base: ProjectStore,
  incoming: ProjectStore | undefined,
): { store: ProjectStore; changed: boolean } {
  if (!incoming) {
    return { store: base, changed: false };
  }

  let changed = false;
  const projects = [...base.projects];
  for (const project of incoming.projects) {
    if (projects.some((candidate) => candidate.id === project.id)) {
      continue;
    }
    projects.push(project);
    changed = true;
  }

  let activeContext = base.active_context;
  if (!activeContext && incoming.active_context) {
    activeContext = normalizeRuntimeContext(incoming.active_context, projects);
    changed = Boolean(activeContext);
  }

  return { store: { projects, active_context: activeContext }, changed };
}

function isDesktopProject(value: unknown): value is DesktopProject {
  if (!value || typeof value !== "object") {
    return false;
  }
  const project = value as Partial<DesktopProject>;
  return (
    typeof project.id === "string" &&
    typeof project.name === "string" &&
    typeof project.path === "string" &&
    typeof project.created_at === "string" &&
    typeof project.updated_at === "string"
  );
}

function normalizeRuntimeContext(
  value: unknown,
  projects: DesktopProject[],
): RuntimeContext | undefined {
  if (!value || typeof value !== "object") {
    return undefined;
  }
  const context = value as Partial<RuntimeContext>;
  if (context.kind === "project" && typeof context.project_id === "string") {
    const project = projects.find(
      (candidate) => candidate.id === context.project_id,
    );
    return project
      ? { kind: "project", project_id: project.id, cwd: project.path }
      : undefined;
  }
  if (context.kind === "no_project" && typeof context.cwd === "string") {
    const cwd = resolve(context.cwd);
    mkdirSync(cwd, { recursive: true });
    return { kind: "no_project", cwd };
  }
  return undefined;
}

function legacyProjectContext(
  value: unknown,
  projects: DesktopProject[],
): RuntimeContext | undefined {
  if (typeof value !== "string") {
    return undefined;
  }
  const project = projects.find((candidate) => candidate.id === value);
  return project
    ? { kind: "project", project_id: project.id, cwd: project.path }
    : undefined;
}

function writeProjectStoreFile(path: string, store: ProjectStore): void {
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, `${JSON.stringify(store, null, 2)}\n`);
}

function createNoProjectContext(): RuntimeContext {
  return { kind: "no_project", cwd: allocateNoProjectCwd() };
}

// SCRATCH_WORKSPACE_DIRNAME is the single, stable directory backing the "对话"
// workspace. Every no-project conversation shares this one cwd — one workspace,
// many sessions — rather than getting its own dated directory. This keeps the
// "对话" workspace always present (it can never be moved/deleted, so it is never
// "unavailable"), and lets the draft-tab-per-context dedup collapse repeated
// "新建对话" clicks onto a single shared draft. Legacy per-date scratch threads
// keep their own recorded cwd and still list under 对话 via the isScratchThread
// cwd check.
const SCRATCH_WORKSPACE_DIRNAME = "default";

function allocateNoProjectCwd(): string {
  const dir = join(wuuHomePath(), "scratch", SCRATCH_WORKSPACE_DIRNAME);
  mkdirSync(dir, { recursive: true });
  return dir;
}

function projectID(projectPath: string): string {
  return Buffer.from(resolve(projectPath)).toString("base64url");
}

function projectName(projectPath: string): string {
  return basename(projectPath) || projectPath;
}

function isDirectory(projectPath: string): boolean {
  try {
    return statSync(projectPath).isDirectory();
  } catch {
    return false;
  }
}
