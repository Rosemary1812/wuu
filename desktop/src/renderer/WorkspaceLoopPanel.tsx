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
  LoopAttentionItem,
  LoopApprovalSnapshot,
  LoopHarnessReportSnapshot,
  LoopHarnessTaskSnapshot,
  LoopSystemSnapshot,
  LoopWorkflowSnapshot,
  RuntimeContext,
} from "../shared/protocol";
import { OVERLAY_SCROLLBAR_OPTIONS } from "./ScrollbarOptions";

type LoopPanelState =
  | { status: "idle" | "loading"; snapshot?: LoopSystemSnapshot; error?: undefined }
  | { status: "ready"; snapshot: LoopSystemSnapshot; error?: undefined }
  | { status: "error"; snapshot?: LoopSystemSnapshot; error: string };

export function WorkspaceLoopPanel({
  activeContext,
  threadId,
  open,
}: {
  activeContext?: RuntimeContext;
  threadId?: string;
  open: boolean;
}): JSX.Element {
  const [state, setState] = useState<LoopPanelState>({ status: "idle" });
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
      .getLoopSnapshot(threadId)
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
          <Activity size={21} />
        </span>
        <strong>Loop</strong>
        <span>选择工作区后显示运行状态</span>
      </div>
    );
  }

  const snapshot = state.snapshot;
  const loading = state.status === "loading";

  return (
    <section className="workspace-loop-panel" aria-label="Loop 状态">
      <div className="workspace-loop-toolbar">
        <div>
          <strong>Loop</strong>
          <span>{snapshot?.generated_at ? formatTimestamp(snapshot.generated_at) : "未同步"}</span>
        </div>
        <button
          className="icon-button workspace-loop-refresh"
          type="button"
          aria-label="刷新 Loop 状态"
          disabled={loading}
          onClick={() => setRefreshKey((value) => value + 1)}
        >
          <RefreshCw size={16} />
        </button>
      </div>
      {state.status === "error" ? (
        <div className="workspace-loop-error" role="status">
          <AlertTriangle size={15} />
          <span>{state.error}</span>
        </div>
      ) : null}
      {snapshot ? (
        <LoopSnapshotView snapshot={snapshot} loading={loading} />
      ) : (
        <div className="workspace-panel-empty">
          <span className="workspace-panel-empty-icon" aria-hidden="true">
            <CircleDot size={21} />
          </span>
          <strong>{loading ? "读取中" : "暂无 Loop"}</strong>
          <span>{loading ? "正在同步工作区状态" : "当前工作区还没有持久化 loop 运行"}</span>
        </div>
      )}
    </section>
  );
}

function LoopSnapshotView({
  snapshot,
  loading,
}: {
  snapshot: LoopSystemSnapshot;
  loading: boolean;
}): JSX.Element {
  const summary = useMemo(() => summarizeLoopSnapshot(snapshot), [snapshot]);
  return (
    <OverlayScrollbarsComponent
      className={`workspace-loop-scroll${loading ? " loading" : ""}`}
      data-overlayscrollbars-initialize
      defer
      options={OVERLAY_SCROLLBAR_OPTIONS}
    >
      <div className="workspace-loop-summary" aria-label="Loop 汇总">
        <LoopMetric icon={<GitBranch size={16} />} label="运行" value={summary.workflowCount} />
        <LoopMetric icon={<Activity size={16} />} label="活跃" value={summary.activeWorkflowCount} />
        <LoopMetric icon={<AlertTriangle size={16} />} label="关注" value={summary.attentionCount} />
        <LoopMetric icon={<Users size={16} />} label="任务" value={summary.taskCount} />
      </div>
      <LoopSection title="关注项" count={snapshot.attention?.length ?? 0}>
        {snapshot.attention?.length ? (
          <div className="workspace-loop-list">
            {snapshot.attention.map((item, index) => (
              <AttentionRow key={`${item.source}-${item.id ?? index}-${item.status ?? ""}`} item={item} />
            ))}
          </div>
        ) : (
          <LoopEmpty text="没有需要处理的失败或冲突" />
        )}
      </LoopSection>
      <LoopSection title="Approvals" count={snapshot.approvals?.length ?? 0}>
        {snapshot.approvals?.length ? (
          <div className="workspace-loop-list">
            {snapshot.approvals.map((approval) => (
              <ApprovalRow key={`${approval.loop_id ?? ""}-${approval.id}`} approval={approval} />
            ))}
          </div>
        ) : (
          <LoopEmpty text="没有待处理审批" />
        )}
      </LoopSection>
      <LoopSection title="Workflow" count={snapshot.workflows?.length ?? 0}>
        {snapshot.workflows?.length ? (
          <div className="workspace-loop-list">
            {snapshot.workflows.map((workflow) => (
              <WorkflowCard key={workflow.id} workflow={workflow} />
            ))}
          </div>
        ) : (
          <LoopEmpty text="没有 workflow 运行" />
        )}
      </LoopSection>
      <LoopSection title="Agent Tasks" count={snapshot.harness?.tasks?.length ?? 0}>
        {snapshot.harness?.tasks?.length ? (
          <div className="workspace-loop-list">
            {snapshot.harness.tasks.map((task) => (
              <HarnessTaskRow key={task.id} task={task} />
            ))}
          </div>
        ) : (
          <LoopEmpty text="当前 thread 没有 agent task" />
        )}
      </LoopSection>
      <LoopSection title="Reports" count={snapshot.harness?.reports?.length ?? 0}>
        {snapshot.harness?.reports?.length ? (
          <div className="workspace-loop-list">
            {snapshot.harness.reports.map((report) => (
              <HarnessReportRow key={report.id} report={report} />
            ))}
          </div>
        ) : (
          <LoopEmpty text="还没有 agent report" />
        )}
      </LoopSection>
      {snapshot.warnings?.length ? (
        <LoopSection title="Warnings" count={snapshot.warnings.length}>
          <div className="workspace-loop-list">
            {snapshot.warnings.map((warning, index) => (
              <div className="workspace-loop-row warning" key={`${warning}-${index}`}>
                <AlertTriangle size={15} />
                <span>{warning}</span>
              </div>
            ))}
          </div>
        </LoopSection>
      ) : null}
    </OverlayScrollbarsComponent>
  );
}

function LoopMetric({
  icon,
  label,
  value,
}: {
  icon: JSX.Element;
  label: string;
  value: number;
}): JSX.Element {
  return (
    <div className="workspace-loop-metric">
      <span aria-hidden="true">{icon}</span>
      <strong>{value}</strong>
      <small>{label}</small>
    </div>
  );
}

function LoopSection({
  title,
  count,
  children,
}: {
  title: string;
  count: number;
  children: JSX.Element;
}): JSX.Element {
  return (
    <section className="workspace-loop-section">
      <header>
        <h3>{title}</h3>
        <span>{count}</span>
      </header>
      {children}
    </section>
  );
}

function AttentionRow({ item }: { item: LoopAttentionItem }): JSX.Element {
  return (
    <div className="workspace-loop-row attention">
      <AlertTriangle size={15} />
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

function ApprovalRow({ approval }: { approval: LoopApprovalSnapshot }): JSX.Element {
  return (
    <div className="workspace-loop-row attention">
      <AlertTriangle size={15} />
      <div>
        <strong>{firstText(approval.title, approval.id)}</strong>
        <span>
          {firstText(approval.loop_id, approval.source, "loop")}
          {approval.requested_action ? ` / ${approval.requested_action}` : ""}
        </span>
      </div>
      <StatusPill status={approval.status} />
    </div>
  );
}

function WorkflowCard({ workflow }: { workflow: LoopWorkflowSnapshot }): JSX.Element {
  const phaseCount = workflow.phases?.length ?? 0;
  const agentCount = workflow.agent_runs?.length ?? 0;
  const memberCount = workflow.team?.members?.length ?? 0;
  return (
    <article className="workspace-loop-card">
      <div className="workspace-loop-card-header">
        <div>
          <strong>{firstText(workflow.definition_name, workflow.id)}</strong>
          <span>{workflow.loop_id || workflow.id}</span>
        </div>
        <StatusPill status={workflow.status} />
      </div>
      <div className="workspace-loop-card-grid">
        <LoopFact icon={<ListChecks size={14} />} label="phases" value={phaseCount} />
        <LoopFact icon={<Users size={14} />} label="agents" value={agentCount} />
        <LoopFact icon={<FileText size={14} />} label="events" value={workflow.event_count ?? 0} />
        <LoopFact icon={<Activity size={14} />} label="team" value={memberCount} />
      </div>
      {workflow.arbitration?.next_actions?.length ? (
        <div className="workspace-loop-next">
          {workflow.arbitration.next_actions.slice(0, 2).map((action) => (
            <span key={action}>{action}</span>
          ))}
        </div>
      ) : null}
    </article>
  );
}

function HarnessTaskRow({ task }: { task: LoopHarnessTaskSnapshot }): JSX.Element {
  return (
    <div className="workspace-loop-row">
      <Users size={15} />
      <div>
        <strong>{firstText(task.name, task.id)}</strong>
        <span>
          {firstText(task.role, "agent")}
          {task.loop_id ? ` / ${task.loop_id}` : ""}
        </span>
      </div>
      <StatusPill status={task.status} />
    </div>
  );
}

function HarnessReportRow({ report }: { report: LoopHarnessReportSnapshot }): JSX.Element {
  return (
    <div className="workspace-loop-row">
      <CheckCircle2 size={15} />
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

function LoopFact({
  icon,
  label,
  value,
}: {
  icon: JSX.Element;
  label: string;
  value: number;
}): JSX.Element {
  return (
    <span className="workspace-loop-fact">
      {icon}
      <span>{label}</span>
      <strong>{value}</strong>
    </span>
  );
}

function LoopEmpty({ text }: { text: string }): JSX.Element {
  return <div className="workspace-loop-empty">{text}</div>;
}

function StatusPill({ status }: { status: string }): JSX.Element {
  return <span className={`workspace-loop-status ${statusClass(status)}`}>{status || "unknown"}</span>;
}

function summarizeLoopSnapshot(snapshot: LoopSystemSnapshot): {
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
