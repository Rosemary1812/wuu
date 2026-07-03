import {
  useEffect,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
} from "react";
import {
  FileEdit,
  FileText,
  Globe,
  ShieldAlert,
  Terminal,
  type LucideIcon,
} from "lucide-react";
import type { PendingToolApproval } from "../shared/protocol";

const PREVIEW_LINE_LIMIT = 6;

type ToolApprovalCardProps = {
  approval: PendingToolApproval;
  onApprove: () => void;
  onApproveForSession: () => void;
  onDeny: () => void;
};

/**
 * Whether this approval deserves the danger visual. Two-tier is intentional:
 * the user only sees the red accent on actions that are actually destructive
 * or non-idempotent. Everything else uses the neutral surface.
 *
 * `destructive=true` is the primary trigger (set by the tool's risk
 * classification). Network calls with non-idempotent HTTP methods
 * (POST/PUT/PATCH/DELETE) also qualify — they can mutate remote state.
 */
function isDangerApproval(approval: PendingToolApproval): boolean {
  if (approval.destructive === true) {
    return true;
  }
  if (approval.read_only === true) {
    return false;
  }
  const capability = approval.capability ?? "";
  if (capability.startsWith("network.")) {
    const preview = approval.arguments_preview ?? "";
    if (/\b(POST|PUT|DELETE|PATCH)\b/i.test(preview)) {
      return true;
    }
  }
  return false;
}

function isCommandCapability(capability: string | undefined): boolean {
  return Boolean(capability?.startsWith("command."));
}

function isNetworkCapability(capability: string | undefined): boolean {
  return Boolean(capability?.startsWith("network."));
}

function isFileCapability(capability: string | undefined): boolean {
  return Boolean(
    capability?.startsWith("file.") || capability?.startsWith("fs."),
  );
}

function networkMethod(preview: string): string | undefined {
  const match = preview.match(/\b(GET|POST|PUT|DELETE|PATCH|HEAD)\b/);
  return match ? (match[1] ?? "").toUpperCase() : undefined;
}

function iconForCapability(
  capability: string | undefined,
  danger: boolean,
): LucideIcon {
  if (danger) {
    return ShieldAlert;
  }
  if (isCommandCapability(capability)) {
    return Terminal;
  }
  if (isNetworkCapability(capability)) {
    return Globe;
  }
  if (isFileCapability(capability)) {
    return FileEdit;
  }
  return FileText;
}

function capabilityLabel(capability: string | undefined): string {
  if (!capability) {
    return "需要批准";
  }
  if (isCommandCapability(capability)) {
    return "运行命令";
  }
  if (isNetworkCapability(capability)) {
    return "访问网络";
  }
  if (isFileCapability(capability)) {
    return "操作文件";
  }
  return "使用工具";
}

function compactPreviewTarget({
  preview,
  object,
}: {
  preview: string | undefined;
  object: string | undefined;
}): string | undefined {
  if (object) {
    return object;
  }
  if (!preview) {
    return undefined;
  }
  const firstLine = preview.split("\n").find((line) => line.trim().length > 0);
  return firstLine?.trim();
}

/**
 * Inline approval card that lives inside the assistant turn. The rest of
 * the conversation stays interactive (the user can scroll, read other
 * turns, send a new message) while a decision is pending; only this card
 * holds the buttons that resolve it.
 *
 * No onClose: there is no "close without deciding" path. The user must
 * pick deny / approve-for-session / approve-once. Esc on the card fires
 * deny — that's a real decision, not a dismissal.
 *
 * Focus: the primary button (approve-once) takes focus on mount. Enter on
 * a focused button fires its native click. Enter on a non-button target
 * inside the card also fires approve, so the user doesn't have to mouse
 * back to the button after reading the preview.
 */
export function ToolApprovalCard({
  approval,
  onApprove,
  onApproveForSession,
  onDeny,
}: ToolApprovalCardProps): JSX.Element {
  const preview = approval.arguments_preview?.trim();
  const reason =
    approval.policy_reason?.trim() ||
    approval.classification_reason?.trim() ||
    "这个操作需要你的确认才能继续。";
  const rule = approval.capability_rule?.trim() || approval.permission_rule?.trim();
  const object = approval.capability_object?.trim();
  const capability = approval.capability?.trim();
  const danger = isDangerApproval(approval);
  const command = isCommandCapability(capability);
  const network = isNetworkCapability(capability);
  const method = network && preview ? networkMethod(preview) : undefined;
  const Icon = iconForCapability(capability, danger);
  const target = compactPreviewTarget({ preview, object });

  const previewLines = preview ? preview.split("\n") : [];
  const isLongPreview = previewLines.length > PREVIEW_LINE_LIMIT;
  const [expanded, setExpanded] = useState(false);
  const visiblePreview = isLongPreview && !expanded
    ? previewLines.slice(0, PREVIEW_LINE_LIMIT).join("\n")
    : preview;

  const primaryButtonRef = useRef<HTMLButtonElement | null>(null);
  useEffect(() => {
    primaryButtonRef.current?.focus();
  }, []);

  function handleKeyDown(event: ReactKeyboardEvent<HTMLDivElement>): void {
    if (event.key === "Escape") {
      event.preventDefault();
      onDeny();
      return;
    }
    if (event.key === "Enter" && !(event.target instanceof HTMLButtonElement)) {
      event.preventDefault();
      onApprove();
    }
  }

  const className = `tool-approval-card${danger ? " danger" : ""}`;
  const showRawPreview = Boolean(preview && preview !== target);

  return (
    <div
      className={className}
      role="group"
      aria-label="审批操作"
      onKeyDown={handleKeyDown}
    >
      <header className="tool-approval-card-header">
        <div
          className={`tool-approval-card-icon${danger ? " danger" : ""}`}
          aria-hidden="true"
        >
          <Icon className="icon" strokeWidth={1.75} />
        </div>
        <div className="tool-approval-card-heading">
          <div className="tool-approval-card-title-row">
            <span className="tool-approval-card-title">
              {capabilityLabel(capability)}
            </span>
            {danger ? (
              <span className="tool-approval-card-risk-pill">高风险</span>
            ) : null}
          </div>
          <p className="tool-approval-card-reason">{reason}</p>
        </div>
      </header>

      {target ? (
        <div className="tool-approval-card-target">
          {command ? (
            <span className="tool-approval-card-preview-prompt" aria-hidden="true">
              $_
            </span>
          ) : null}
          {network && method ? (
            <span className={`tool-approval-card-method-badge ${method}`}>
              {method}
            </span>
          ) : null}
          <code title={target}>{target}</code>
        </div>
      ) : null}

      {showRawPreview || rule ? (
        <details className="tool-approval-card-rule">
          <summary>查看参数</summary>
          {showRawPreview ? (
            <div className="tool-approval-card-preview">
              <pre className="tool-approval-card-preview-text">{visiblePreview}</pre>
              {isLongPreview ? (
                <button
                  type="button"
                  className="tool-approval-card-expand"
                  onClick={() => setExpanded((current) => !current)}
                >
                  {expanded
                    ? "收起"
                    : `展开完整参数 (${previewLines.length} 行)`}
                </button>
              ) : null}
            </div>
          ) : null}
          {rule ? <code>{rule}</code> : null}
        </details>
      ) : null}

      <footer className="tool-approval-card-actions">
        <button type="button" className="deny" onClick={onDeny}>
          拒绝
        </button>
        <div className="tool-approval-card-actions-right">
          <button
            type="button"
            className="session"
            onClick={onApproveForSession}
          >
            本会话批准
          </button>
          <button
            ref={primaryButtonRef}
            type="button"
            className="primary"
            onClick={onApprove}
          >
            批准一次
          </button>
        </div>
      </footer>
    </div>
  );
}
