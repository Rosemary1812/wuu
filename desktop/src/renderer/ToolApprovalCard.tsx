import {
  useEffect,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
} from "react";
import {
  AlertTriangle,
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
    return "tool call";
  }
  if (isCommandCapability(capability)) {
    return "Shell command";
  }
  if (isNetworkCapability(capability)) {
    return "Network request";
  }
  if (isFileCapability(capability)) {
    return "File operation";
  }
  return "Tool call";
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
  const capabilityAction = approval.capability_action?.trim();

  const danger = isDangerApproval(approval);
  const command = isCommandCapability(capability);
  const network = isNetworkCapability(capability);
  const method = network && preview ? networkMethod(preview) : undefined;
  const readOnly = approval.read_only === true;
  const Icon = iconForCapability(capability, danger);

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

  const className = `tool-approval-card${danger ? " danger" : ""}${
    readOnly ? " read-only" : ""
  }`;

  const riskLabel = danger
    ? readOnly
      ? "Read-only"
      : "Destructive"
    : null;

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
            {riskLabel ? (
              <span
                className={`tool-approval-card-risk-pill${
                  danger ? " danger" : ""
                }`}
              >
                {danger ? (
                  <AlertTriangle
                    className="icon"
                    strokeWidth={2}
                    aria-hidden="true"
                  />
                ) : null}
                {riskLabel}
              </span>
            ) : null}
          </div>
          <div className="tool-approval-card-subtitle">
            {capability ? <code className="mono">{capability}</code> : null}
            {capabilityAction ? (
              <>
                <span className="dot" aria-hidden="true">
                  ·
                </span>
                <span>{capabilityAction}</span>
              </>
            ) : null}
            {object ? (
              <>
                <span className="dot" aria-hidden="true">
                  ·
                </span>
                <span className="tool-approval-card-subtitle-object" title={object}>
                  {object}
                </span>
              </>
            ) : null}
          </div>
        </div>
      </header>

      <section className="tool-approval-card-section">
        <div className="tool-approval-card-section-label">Why this is asking</div>
        <p className="tool-approval-card-reason">{reason}</p>
      </section>

      {visiblePreview ? (
        <section className="tool-approval-card-section">
          <div className="tool-approval-card-section-label">About to run</div>
          <div className="tool-approval-card-preview">
            <div className="tool-approval-card-preview-line">
              {command ? (
                <span
                  className="tool-approval-card-preview-prompt"
                  aria-hidden="true"
                >
                  $_
                </span>
              ) : null}
              {network && method ? (
                <span className={`tool-approval-card-method-badge ${method}`}>
                  {method}
                </span>
              ) : null}
              <pre className="tool-approval-card-preview-text">
                {visiblePreview}
              </pre>
            </div>
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
        </section>
      ) : null}

      {rule ? (
        <details className="tool-approval-card-rule">
          <summary>Show matched rule</summary>
          <code>{rule}</code>
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
