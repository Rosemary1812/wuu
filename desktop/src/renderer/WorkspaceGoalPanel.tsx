import { useEffect, useMemo, useState } from "react";
import {
  Activity,
  AlertTriangle,
  CheckCircle2,
  CircleDot,
  FileText,
  GitBranch,
  ListChecks,
  RefreshCw,
  Users,
} from "lucide-react";
import { OverlayScrollbarsComponent } from "overlayscrollbars-react";
import type {
  GoalAttentionItem,
  GoalApprovalSnapshot,
  GoalHarnessReportSnapshot,
  GoalHarnessTaskSnapshot,
  GoalSystemSnapshot,
  GoalWorkflowSnapshot,
  RuntimeContext,
} from "../shared/protocol";
import { OVERLAY_SCROLLBAR_OPTIONS } from "./ScrollbarOptions";

type GoalPanelState =
  | { status: "idle" | "loading"; snapshot?: GoalSystemSnapshot; error?: undefined }
  | { status: "ready"; snapshot: GoalSystemSnapshot; error?: undefined }
  | { status: "error"; snapshot?: GoalSystemSnapshot; error: string };

export function WorkspaceGoalPanel({
  activeContext,
  threadId,
  open,
}: {
  activeContext?: RuntimeContext;
  threadId?: string;
  open: boolean;
}): JSX.Element {
  const [state, setState] = useState<GoalPanelState>({ status: "idle" });
  const [refreshKey, setRefreshKey] = useState(0);

  useEffect(() => {
    if (!open || !activeContext) {
      return;
    }
    let cancelled = false;
    setState((current) => ({
      status: "loading",
      snapshot: current.snapshot,
    }));
    window.wuu
      .getGoalSnapshot(threadId)
      .then((result) => {
        if (!cancelled) {
          setState({ status: "ready", snapshot: result.snapshot });
        }
      })
      .catch((error: unknown) => {
        if (!cancelled) {
          setState((current) => ({
            status: "error",
            snapshot: current.snapshot,
            error: error instanceof Error ? error.message : String(error),
          }));
        }
      });
    return () => {
      cancelled = true;
    };
  }, [activeContext, open, refreshKey, threadId]);

  if (!activeContext) {
    return (
      <div className="workspace-panel-empty">
        <span className="workspace-panel-empty-icon" aria-hidden="true">
          <Activity className="icon-xl" />
        </span>
        <strong>Goal</strong>
        <span>选择工作区后显示目标状态</span>
      </div>
    );
  }

  const snapshot = state.snapshot;
  const loading = state.status === "loading";

  return (
    <section className="workspace-goal-panel" aria-label="Goal 状态">
      <div className="workspace-goal-toolbar">
        <div>
          <strong>Goal</strong>
          <span>{snapshot?.generated_at ? formatTimestamp(snapshot.generated_at) : "未同步"}</span>
        </div>
        <button
          className="icon-button workspace-goal-refresh"
          type="button"
          aria-label="刷新 Goal 状态"
          disabled={loading}
          onClick={() => setRefreshKey((value) => value + 1)}
        >
          <RefreshCw className="icon" />
        </button>
      </div>
      {state.status === "error" ? (
        <div className="workspace-goal-error" role="status">
          <AlertTriangle className="icon" />
          <span>{state.error}</span>
        </div>
      ) : null}
      {snapshot ? (
        <GoalSnapshotView snapshot={snapshot} loading={loading} />
      ) : (
        <div className="workspace-panel-empty">
          <span className="workspace-panel-empty-icon" aria-hidden="true">
            <CircleDot className="icon-xl" />
          </span>
          <strong>{loading ? "读取中" : "暂无 Goal"}</strong>
          <span>{loading ? "正在同步工作区状态" : "当前工作区还没有持久化目标"}</span>
        </div>
      )}
    </section>
  );
}

function GoalSnapshotView({
  snapshot,
  loading,
}: {
  snapshot: GoalSystemSnapshot;
  loading: boolean;
}): JSX.Element {
  const summary = useMemo(() => summarizeGoalSnapshot(snapshot), [snapshot]);
  return (
    <OverlayScrollbarsComponent
      className={`workspace-goal-scroll${loading ? " loading" : ""}`}
      data-overlayscrollbars-initialize
      defer
      options={OVERLAY_SCROLLBAR_OPTIONS}
    >
      <div className="workspace-goal-summary" aria-label="Goal 汇总">
        <GoalMetric icon={<GitBranch className="icon" />} label="运行" value={summary.workflowCount} />
        <GoalMetric icon={<Activity className="icon" />} label="活跃" value={summary.activeWorkflowCount} />
        <GoalMetric icon={<AlertTriangle className="icon" />} label="关注" value={summary.attentionCount} />
        <GoalMetric icon={<Users className="icon" />} label="任务" value={summary.taskCount} />
      </div>
      <GoalSection title="关注项" count={snapshot.attention?.length ?? 0}>
        {snapshot.attention?.length ? (
          <div className="workspace-goal-list">
            {snapshot.attention.map((item, index) => (
              <AttentionRow key={`${item.source}-${item.id ?? index}-${item.status ?? ""}`} item={item} />
            ))}
          </div>
        ) : (
          <GoalEmpty text="没有需要处理的失败或冲突" />
        )}
      </GoalSection>
      <GoalSection title="Approvals" count={snapshot.approvals?.length ?? 0}>
        {snapshot.approvals?.length ? (
          <div className="workspace-goal-list">
            {snapshot.approvals.map((approval) => (
              <ApprovalRow key={`${approval.goal_id ?? ""}-${approval.id}`} approval={approval} />
            ))}
          </div>
        ) : (
          <GoalEmpty text="没有待处理审批" />
        )}
      </GoalSection>
      <GoalSection title="Workflow" count={snapshot.workflows?.length ?? 0}>
        {snapshot.workflows?.length ? (
          <div className="workspace-goal-list">
            {snapshot.workflows.map((workflow) => (
              <WorkflowCard key={workflow.id} workflow={workflow} />
            ))}
          </div>
        ) : (
          <GoalEmpty text="没有 workflow 运行" />
        )}
      </GoalSection>
      <GoalSection title="Agent Tasks" count={snapshot.harness?.tasks?.length ?? 0}>
        {snapshot.harness?.tasks?.length ? (
          <div className="workspace-goal-list">
            {snapshot.harness.tasks.map((task) => (
              <HarnessTaskRow key={task.id} task={task} />
            ))}
          </div>
        ) : (
          <GoalEmpty text="当前 thread 没有 agent task" />
        )}
      </GoalSection>
      <GoalSection title="Reports" count={snapshot.harness?.reports?.length ?? 0}>
        {snapshot.harness?.reports?.length ? (
          <div className="workspace-goal-list">
            {snapshot.harness.reports.map((report) => (
              <HarnessReportRow key={report.id} report={report} />
            ))}
          </div>
        ) : (
          <GoalEmpty text="还没有 agent report" />
        )}
      </GoalSection>
      {snapshot.warnings?.length ? (
        <GoalSection title="Warnings" count={snapshot.warnings.length}>
          <div className="workspace-goal-list">
            {snapshot.warnings.map((warning, index) => (
              <div className="workspace-goal-row warning" key={`${warning}-${index}`}>
                <AlertTriangle className="icon" />
                <span>{warning}</span>
              </div>
            ))}
          </div>
        </GoalSection>
      ) : null}
    </OverlayScrollbarsComponent>
  );
}

function GoalMetric({
  icon,
  label,
  value,
}: {
  icon: JSX.Element;
  label: string;
  value: number;
}): JSX.Element {
  return (
    <div className="workspace-goal-metric">
      <span aria-hidden="true">{icon}</span>
      <strong>{value}</strong>
      <small>{label}</small>
    </div>
  );
}

function GoalSection({
  title,
  count,
  children,
}: {
  title: string;
  count: number;
  children: JSX.Element;
}): JSX.Element {
  return (
    <section className="workspace-goal-section">
      <header>
        <h3>{title}</h3>
        <span>{count}</span>
      </header>
      {children}
    </section>
  );
}

function AttentionRow({ item }: { item: GoalAttentionItem }): JSX.Element {
  return (
    <div className="workspace-goal-row attention">
      <AlertTriangle className="icon" />
      <div>
        <strong>{firstText(item.message, item.id, item.source)}</strong>
        <span>
          {item.source}
          {item.status ? ` / ${item.status}` : ""}
        </span>
      </div>
    </div>
  );
}

function ApprovalRow({ approval }: { approval: GoalApprovalSnapshot }): JSX.Element {
  return (
    <div className="workspace-goal-row attention">
      <AlertTriangle className="icon" />
      <div>
        <strong>{firstText(approval.title, approval.id)}</strong>
        <span>
          {firstText(approval.goal_id, approval.source, "goal")}
          {approval.requested_action ? ` / ${approval.requested_action}` : ""}
        </span>
      </div>
      <StatusPill status={approval.status} />
    </div>
  );
}

function WorkflowCard({ workflow }: { workflow: GoalWorkflowSnapshot }): JSX.Element {
  const phaseCount = workflow.phases?.length ?? 0;
  const agentCount = workflow.agent_runs?.length ?? 0;
  const memberCount = workflow.team?.members?.length ?? 0;
  return (
    <article className="workspace-goal-card">
      <div className="workspace-goal-card-header">
        <div>
          <strong>{firstText(workflow.definition_name, workflow.id)}</strong>
          <span>{workflow.goal_id || workflow.id}</span>
        </div>
        <StatusPill status={workflow.status} />
      </div>
      <div className="workspace-goal-card-grid">
        <GoalFact icon={<ListChecks className="icon-sm" />} label="phases" value={phaseCount} />
        <GoalFact icon={<Users className="icon-sm" />} label="agents" value={agentCount} />
        <GoalFact icon={<FileText className="icon-sm" />} label="events" value={workflow.event_count ?? 0} />
        <GoalFact icon={<Activity className="icon-sm" />} label="team" value={memberCount} />
      </div>
      {workflow.arbitration?.next_actions?.length ? (
        <div className="workspace-goal-next">
          {workflow.arbitration.next_actions.slice(0, 2).map((action) => (
            <span key={action}>{action}</span>
          ))}
        </div>
      ) : null}
    </article>
  );
}

function HarnessTaskRow({ task }: { task: GoalHarnessTaskSnapshot }): JSX.Element {
  return (
    <div className="workspace-goal-row">
      <Users className="icon" />
      <div>
        <strong>{firstText(task.name, task.id)}</strong>
        <span>
          {firstText(task.role, "agent")}
          {task.goal_id ? ` / ${task.goal_id}` : ""}
        </span>
      </div>
      <StatusPill status={task.status} />
    </div>
  );
}

function HarnessReportRow({ report }: { report: GoalHarnessReportSnapshot }): JSX.Element {
  return (
    <div className="workspace-goal-row">
      <CheckCircle2 className="icon" />
      <div>
        <strong>{firstText(report.summary, report.task_id)}</strong>
        <span>
          {report.outcome}
          {report.verification?.length ? ` / ${report.verification.length} checks` : ""}
        </span>
      </div>
    </div>
  );
}

function GoalFact({
  icon,
  label,
  value,
}: {
  icon: JSX.Element;
  label: string;
  value: number;
}): JSX.Element {
  return (
    <span className="workspace-goal-fact">
      {icon}
      <span>{label}</span>
      <strong>{value}</strong>
    </span>
  );
}

function GoalEmpty({ text }: { text: string }): JSX.Element {
  return <div className="workspace-goal-empty">{text}</div>;
}

function StatusPill({ status }: { status: string }): JSX.Element {
  return <span className={`workspace-goal-status ${statusClass(status)}`}>{status || "unknown"}</span>;
}

function summarizeGoalSnapshot(snapshot: GoalSystemSnapshot): {
  workflowCount: number;
  activeWorkflowCount: number;
  attentionCount: number;
  taskCount: number;
} {
  const workflows = snapshot.workflows ?? [];
  return {
    workflowCount: workflows.length,
    activeWorkflowCount: workflows.filter((workflow) => !terminalStatus(workflow.status)).length,
    attentionCount: snapshot.attention?.length ?? 0,
    taskCount: snapshot.harness?.tasks?.length ?? 0,
  };
}

function terminalStatus(status: string): boolean {
  switch (status) {
    case "completed":
    case "failed":
    case "cancelled":
      return true;
    default:
      return false;
  }
}

function statusClass(status: string): string {
  switch (status) {
    case "completed":
      return "completed";
    case "failed":
    case "cancelled":
      return "failed";
    case "running":
    case "awaiting_report":
      return "running";
    default:
      return "pending";
  }
}

function firstText(...values: Array<string | undefined>): string {
  for (const value of values) {
    const text = value?.trim();
    if (text) {
      return text;
    }
  }
  return "unknown";
}

function formatTimestamp(value: string): string {
  const time = new Date(value);
  if (Number.isNaN(time.getTime())) {
    return value;
  }
  return time.toLocaleString(undefined, {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}
