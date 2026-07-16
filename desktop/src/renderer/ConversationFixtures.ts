import type { InitializeResult, Thread } from "../shared/protocol";
import { translateCurrent as t } from "./i18n";

export type ConversationFixtureKind =
  | "long"
  | "rich"
  | "running"
  | "compact"
  | "plan";

export function createAgentTreeDemo(
  cwd: string,
  initialized?: InitializeResult,
): { parent: Thread; children: Thread[] } {
  const base = Date.now();
  const at = (offsetMs: number): string =>
    new Date(base + offsetMs).toISOString();
  const parentID = `demo-agent-tree-${base}`;
  const inspectID = `${parentID}-inspect-sidebar`;
  const resumeID = `${parentID}-resume-child`;
  const provider = initialized?.provider ?? "demo-provider";
  const model = initialized?.model ?? "demo-model";
  const parentTurnID = `${parentID}-turn-0001`;
  const inspectTurnID = `${inspectID}-turn-0001`;
  const resumeTurnID = `${resumeID}-turn-0001`;

  const parent: Thread = {
    id: parentID,
    preview: t("fixture.agentTree.preview"),
    model_provider: provider,
    model,
    cwd,
    status: "idle",
    created_at: at(0),
    updated_at: at(4000),
    turns: [
      {
        id: parentTurnID,
        items_view: "full",
        status: "completed",
        items: [
          {
            id: `${parentTurnID}-item-1`,
            type: "user_message",
            status: "completed",
            role: "user",
            text: t("fixture.agentTree.user"),
          },
          {
            id: `${parentTurnID}-item-2`,
            type: "collab_agent_tool_call",
            status: "completed",
            name: "spawn_agent",
            arguments: JSON.stringify({ name: "inspect_sidebar", description: t("fixture.agentTree.inspectTask"), prompt: t("fixture.agentTree.inspectPrompt"), subagent_type: "general-purpose", run_in_background: true }),
            result: `{"agent_id":"${inspectID}","agent_path":"/root/inspect_sidebar","status":"completed"}`,
          },
          {
            id: `${parentTurnID}-item-3`,
            type: "collab_agent_tool_call",
            status: "completed",
            name: "spawn_agent",
            arguments: JSON.stringify({ name: "resume_child", description: t("fixture.agentTree.resumeTask"), prompt: t("fixture.agentTree.resumePrompt"), subagent_type: "general-purpose", run_in_background: true }),
            result: `{"agent_id":"${resumeID}","agent_path":"/root/resume_child","status":"running"}`,
          },
          {
            id: `${parentTurnID}-item-4`,
            type: "agent_message",
            status: "completed",
            role: "assistant",
            text: t("fixture.agentTree.answer"),
          },
        ],
      },
    ],
    child_agents: [
      {
        id: inspectID,
        type: "worker",
        task_name: t("fixture.agentTree.inspectTask"),
        agent_path: "/root/inspect-sidebar",
        parent_id: parentID,
        description: t("fixture.agentTree.inspectPrompt"),
        status: "completed",
        nested_count: 1,
        nested_running_count: 0,
        started_at: at(1000),
        completed_at: at(2500),
      },
      {
        id: resumeID,
        type: "worker",
        task_name: t("fixture.agentTree.resumeTask"),
        agent_path: "/root/resume-child",
        parent_id: parentID,
        description: t("fixture.agentTree.resumePrompt"),
        status: "running",
        nested_count: 1,
        nested_running_count: 1,
        started_at: at(1800),
      },
    ],
  };

  const children: Thread[] = [
    {
      id: inspectID,
      parent_id: parentID,
      agent_path: "/root/inspect-sidebar",
      preview: t("fixture.agentTree.inspectTask"),
      model_provider: provider,
      model,
      cwd,
      status: "idle",
      read_only: true,
      created_at: at(1000),
      updated_at: at(2500),
      turns: [
        {
          id: inspectTurnID,
          items_view: "full",
          status: "completed",
          items: [
            {
              id: `${inspectTurnID}-item-1`,
              type: "user_message",
              status: "completed",
              role: "user",
              text: t("fixture.agentTree.inspectUser"),
            },
            {
              id: `${inspectTurnID}-item-2`,
              type: "agent_message",
              status: "completed",
              role: "assistant",
              text: t("fixture.agentTree.inspectAnswer"),
            },
          ],
        },
      ],
    },
    {
      id: resumeID,
      parent_id: parentID,
      agent_path: "/root/resume-child",
      preview: t("fixture.agentTree.resumeTask"),
      model_provider: provider,
      model,
      cwd,
      status: "idle",
      read_only: true,
      created_at: at(1800),
      updated_at: at(4200),
      turns: [
        {
          id: resumeTurnID,
          items_view: "full",
          status: "completed",
          items: [
            {
              id: `${resumeTurnID}-item-1`,
              type: "user_message",
              status: "completed",
              role: "user",
              text: t("fixture.agentTree.resumeUser"),
            },
            {
              id: `${resumeTurnID}-item-2`,
              type: "agent_message",
              status: "completed",
              role: "assistant",
              text: t("fixture.agentTree.resumeAnswer"),
            },
          ],
        },
      ],
    },
  ];

  return { parent, children };
}

export function createConversationFixture(
  kind: ConversationFixtureKind,
  cwd: string,
  initialized?: InitializeResult,
): Thread {
  switch (kind) {
    case "rich":
      return createRichContentFixture(cwd, initialized);
    case "running":
      return createRunningFixture(cwd, initialized);
    case "compact":
      return createContextCompactionFixture(cwd, initialized);
    case "plan":
      return createPlanPanelFixture(cwd, initialized);
    default:
      return createLongReadingFixture(cwd, initialized);
  }
}

function fixtureRuntime(initialized?: InitializeResult): {
  provider: string;
  model: string;
} {
  return {
    provider: initialized?.provider ?? "demo-provider",
    model: initialized?.model ?? "demo-model",
  };
}

function createLongReadingFixture(
  cwd: string,
  initialized?: InitializeResult,
): Thread {
  const base = Date.now();
  const at = (offsetMs: number): string =>
    new Date(base + offsetMs).toISOString();
  const { provider, model } = fixtureRuntime(initialized);
  const threadID = `demo-long-reading-${base}`;
  const firstTurnID = `${threadID}-turn-0001`;
  const secondTurnID = `${threadID}-turn-0002`;
  const thirdTurnID = `${threadID}-turn-0003`;

  return {
    id: threadID,
    preview: t("fixture.long.preview"),
    model_provider: provider,
    model,
    cwd,
    status: "idle",
    created_at: at(-18_000),
    updated_at: at(4000),
    turns: [
      {
        id: firstTurnID,
        items_view: "full",
        status: "completed",
        started_at: at(-18_000),
        completed_at: at(-12_000),
        duration_ms: 6000,
        items: [
          {
            id: `${firstTurnID}-item-1`,
            type: "user_message",
            status: "completed",
            role: "user",
            text: t("fixture.long.user1"),
          },
          {
            id: `${firstTurnID}-item-2`,
            type: "agent_message",
            status: "completed",
            role: "assistant",
            text: t("fixture.long.answer1"),
          },
        ],
      },
      {
        id: secondTurnID,
        items_view: "full",
        status: "completed",
        started_at: at(-10_000),
        completed_at: at(-6000),
        duration_ms: 4000,
        items: [
          {
            id: `${secondTurnID}-item-1`,
            type: "user_message",
            status: "completed",
            role: "user",
            text: t("fixture.long.user2"),
          },
          {
            id: `${secondTurnID}-item-2`,
            type: "agent_message",
            status: "completed",
            role: "assistant",
            text: t("fixture.long.answer2"),
          },
        ],
      },
      {
        id: thirdTurnID,
        items_view: "full",
        status: "completed",
        started_at: at(-5000),
        completed_at: at(0),
        duration_ms: 5000,
        items: [
          {
            id: `${thirdTurnID}-item-1`,
            type: "user_message",
            status: "completed",
            role: "user",
            text: t("fixture.long.user3"),
          },
          {
            id: `${thirdTurnID}-item-2`,
            type: "agent_message",
            status: "completed",
            role: "assistant",
            text: t("fixture.long.answer3"),
          },
        ],
      },
    ],
  };
}

function createRichContentFixture(
  cwd: string,
  initialized?: InitializeResult,
): Thread {
  const base = Date.now();
  const at = (offsetMs: number): string =>
    new Date(base + offsetMs).toISOString();
  const { provider, model } = fixtureRuntime(initialized);
  const threadID = `demo-rich-content-${base}`;
  const turnID = `${threadID}-turn-0001`;

  return {
    id: threadID,
    preview: t("fixture.rich.preview"),
    model_provider: provider,
    model,
    cwd,
    status: "idle",
    created_at: at(-9000),
    updated_at: at(1000),
    turns: [
      {
        id: turnID,
        items_view: "full",
        status: "completed",
        started_at: at(-9000),
        completed_at: at(1000),
        duration_ms: 10_000,
        items: [
          {
            id: `${turnID}-item-1`,
            type: "user_message",
            status: "completed",
            role: "user",
            text: t("fixture.rich.user"),
          },
          {
            id: `${turnID}-item-2`,
            type: "reasoning",
            status: "completed",
            text: t("fixture.rich.answer"),
          },
          {
            id: `${turnID}-item-3`,
            type: "tool_call",
            status: "completed",
            name: "rg",
            arguments: `{"pattern":"conversation-width","path":"desktop/src/renderer/styles.css"}`,
            result: "desktop/src/renderer/styles.css:3244:.conversation-width",
          },
          {
            id: `${turnID}-item-4`,
            type: "tool_call",
            status: "completed",
            name: "apply_patch",
            arguments: JSON.stringify({ file: "desktop/src/renderer/styles.css", summary: t("fixture.rich.patchSummary") }),
            result: t("fixture.rich.patchApplied"),
          },
          {
            id: `${turnID}-item-5`,
            type: "context_compaction",
            status: "completed",
            text: t("fixture.rich.compacted"),
          },
          {
            id: `${turnID}-item-6`,
            type: "agent_message",
            status: "completed",
            role: "assistant",
            text: t("fixture.rich.checklist"),
          },
          {
            id: `${turnID}-item-7`,
            type: "error",
            status: "failed",
            error: t("fixture.rich.error"),
          },
        ],
      },
    ],
  };
}

function createPlanPanelFixture(
  cwd: string,
  initialized?: InitializeResult,
): Thread {
  const base = Date.now();
  const at = (offsetMs: number): string =>
    new Date(base + offsetMs).toISOString();
  const { provider, model } = fixtureRuntime(initialized);
  const threadID = `demo-plan-panel-${base}`;
  const turnID = `${threadID}-turn-0001`;

  return {
    id: threadID,
    preview: t("fixture.plan.preview"),
    model_provider: provider,
    model,
    cwd,
    status: "idle",
    created_at: at(-6000),
    updated_at: at(1000),
    turns: [
      {
        id: turnID,
        items_view: "full",
        status: "completed",
        started_at: at(-6000),
        completed_at: at(1000),
        duration_ms: 7000,
        items: [
          {
            id: `${turnID}-item-1`,
            type: "user_message",
            status: "completed",
            role: "user",
            text: t("fixture.plan.user"),
          },
          {
            id: `${turnID}-item-2`,
            type: "tool_call",
            status: "completed",
            name: "update_plan",
            arguments: JSON.stringify({
              plan: [
                {
                  step: t("fixture.plan.step1"),
                  status: "completed",
                },
                { step: t("fixture.plan.step2"), status: "completed" },
                { step: t("fixture.plan.step3"), status: "in_progress" },
                { step: t("fixture.plan.step4"), status: "pending" },
              ],
            }),
            result: `{"status":"updated"}`,
          },
          {
            id: `${turnID}-item-3`,
            type: "agent_message",
            status: "completed",
            role: "assistant",
            text: t("fixture.plan.answer"),
          },
        ],
      },
    ],
  };
}

function createContextCompactionFixture(
  cwd: string,
  initialized?: InitializeResult,
): Thread {
  const base = Date.now();
  const at = (offsetMs: number): string =>
    new Date(base + offsetMs).toISOString();
  const { provider, model } = fixtureRuntime(initialized);
  const threadID = `demo-context-compaction-${base}`;
  const beforeTurnID = `${threadID}-turn-0001`;
  const compactTurnID = `${threadID}-turn-0002`;

  return {
    id: threadID,
    preview: t("fixture.compact.preview"),
    model_provider: provider,
    model,
    cwd,
    status: "idle",
    created_at: at(-32_000),
    updated_at: at(2000),
    turns: [
      {
        id: beforeTurnID,
        items_view: "full",
        status: "completed",
        started_at: at(-32_000),
        completed_at: at(-24_000),
        duration_ms: 8000,
        items: [
          {
            id: `${beforeTurnID}-item-1`,
            type: "user_message",
            status: "completed",
            role: "user",
            text: t("fixture.compact.user1"),
          },
          {
            id: `${beforeTurnID}-item-2`,
            type: "agent_message",
            status: "completed",
            role: "assistant",
            text: t("fixture.compact.answer1"),
          },
        ],
      },
      {
        id: compactTurnID,
        items_view: "full",
        status: "completed",
        started_at: at(-20_000),
        completed_at: at(2000),
        duration_ms: 22_000,
        items: [
          {
            id: `${compactTurnID}-item-1`,
            type: "user_message",
            status: "completed",
            role: "user",
            text: t("fixture.compact.user2"),
          },
          {
            id: `${compactTurnID}-item-2`,
            type: "context_compaction",
            status: "completed",
            text: "✦ Compacted history: 18 → 5 messages (was ~12k tokens)",
          },
          {
            id: `${compactTurnID}-item-3`,
            type: "agent_message",
            status: "completed",
            role: "assistant",
            text: t("fixture.compact.answer2"),
          },
        ],
      },
    ],
  };
}

function createRunningFixture(
  cwd: string,
  initialized?: InitializeResult,
): Thread {
  const base = Date.now();
  const at = (offsetMs: number): string =>
    new Date(base + offsetMs).toISOString();
  const { provider, model } = fixtureRuntime(initialized);
  const threadID = `demo-running-${base}`;
  const turnID = `${threadID}-turn-0001`;

  return {
    id: threadID,
    preview: t("fixture.running.preview"),
    model_provider: provider,
    model,
    cwd,
    status: "in_progress",
    created_at: at(-45_000),
    updated_at: at(0),
    turns: [
      {
        id: turnID,
        items_view: "full",
        status: "in_progress",
        started_at: at(-45_000),
        items: [
          {
            id: `${turnID}-item-1`,
            type: "user_message",
            status: "completed",
            role: "user",
            text: t("fixture.running.user"),
          },
          {
            id: `${turnID}-item-2`,
            type: "reasoning",
            status: "in_progress",
            text: t("fixture.running.reasoning"),
          },
          {
            id: `${turnID}-item-3`,
            type: "tool_call",
            status: "in_progress",
            name: "npm run typecheck",
            arguments: `{"cwd":"desktop"}`,
            result: "",
          },
          {
            id: `${turnID}-item-4`,
            type: "agent_message",
            status: "in_progress",
            role: "assistant",
            text: t("fixture.running.answer"),
          },
        ],
      },
    ],
  };
}
