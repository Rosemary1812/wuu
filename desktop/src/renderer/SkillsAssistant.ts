import type { RuntimeContext, Thread } from "../shared/protocol";

export function skillsAssistantPrompt(
  query: string,
  context: RuntimeContext,
): string {
  const surfaceContext = {
    surface: "skills_catalog",
    workspace_kind: context.kind,
    cwd: context.cwd,
    behavior: [
      "Treat the user request as scoped to the Skills catalog.",
      "Inspect the installed Skills and their source files when the request depends on current state.",
      "Create or edit Skill files directly when the user asks for a change.",
    ],
  };
  return [
    "<surface_context>",
    JSON.stringify(surfaceContext, null, 2),
    "</surface_context>",
    "",
    "<user_query>",
    query.trim(),
    "</user_query>",
  ].join("\n");
}

export function userVisibleThreads(threads: Thread[]): Thread[] {
  return threads.filter((thread) => !thread.ephemeral);
}
