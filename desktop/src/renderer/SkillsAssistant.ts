import type { RuntimeContext, Thread, ThreadStartParams } from "../shared/protocol";

export type ManagementAssistantSurface = "skills" | "automations";

export type ManagementAssistantSession = {
  draft: string;
  status: string;
  threadID?: string;
};

export const EMPTY_MANAGEMENT_ASSISTANT_SESSION: ManagementAssistantSession = {
  draft: "",
  status: "",
};

export function managementAssistantThreadStartParams(
  surface: ManagementAssistantSurface,
): ThreadStartParams {
  return {
    ephemeral: true,
    ...(surface === "skills" ? { managementSurface: "skills" } : {}),
  };
}

export function retainOpenManagementAssistantSessions(
  sessions: Record<string, ManagementAssistantSession>,
  openTabIDs: Set<string>,
): Record<string, ManagementAssistantSession> {
  const retained = Object.fromEntries(
    Object.entries(sessions).filter(([tabID]) => openTabIDs.has(tabID)),
  );
  return Object.keys(retained).length === Object.keys(sessions).length
    ? sessions
    : retained;
}

export function managementAssistantRequestContext(
  surface: ManagementAssistantSurface,
  context: RuntimeContext,
): string {
  const surfaceContext = {
    surface: surface === "skills" ? "skills_catalog" : "automations_catalog",
    workspace_kind: context.kind,
    cwd: context.cwd,
    behavior:
      surface === "skills"
        ? [
            "Treat the user request as scoped to the Skills catalog.",
            "Inspect installed Skills and their source files when the request depends on current state.",
            "Create or edit Skill files directly when the user asks for a change.",
          ]
        : [
            "Treat the user request as scoped to managing scheduled automations.",
            "Use the cron tool to inspect current automations before answering requests that depend on current state.",
            "Use the cron tool to create, update, pause, resume, or remove automations when requested.",
          ],
  };
  return JSON.stringify(surfaceContext, null, 2);
}

export function userVisibleThreads(threads: Thread[]): Thread[] {
  return threads.filter((thread) => !thread.ephemeral);
}
