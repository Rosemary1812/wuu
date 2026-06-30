import {
  useEffect,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
} from "react";
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

function networkMethod(preview: string): string | undefined {
  const match = preview.match(/\b(GET|POST|PUT|DELETE|PATCH|HEAD)\b/);
  return match ? (match[1] ?? "").toUpperCase() : undefined;
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
  const capabilityLine = [capability, capabilityAction].filter(Boolean).join(" · ");

  const danger = isDangerApproval(approval);
  const command = isCommandCapability(capability);
  const network = isNetworkCapability(capability);
  const method = network && preview ? networkMethod(preview) : undefined;

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

  return (
    <div
      className={className}
      role="group"
      aria-label="审批操作"
      onKeyDown={handleKeyDown}
    >
      <header className="tool-approval-card-header">
        <span className="tool-approval-card-capability">
          {capabilityLine || approval.tool_name}
        </span>
      </header>
      <p className="tool-approval-card-reason">{reason}</p>
      {object ? (
        <div className="tool-approval-card-object">
          <span className="tool-approval-card-object-label">对象</span>
          <code className="tool-approval-card-object-value">{object}</code>
        </div>
      ) : null}
      {visiblePreview ? (
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
              <span
                className={`tool-approval-card-method-badge ${method}`}
              >
                {method}
              </span>
            ) : null}
            <pre className="tool-approval-card-preview-text">{visiblePreview}</pre>
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
      ) : null}
      {rule ? (
        <details className="tool-approval-card-rule">
          <summary>规则</summary>
          <code>{rule}</code>
        </details>
      ) : null}
      <footer className="tool-approval-card-actions">
        <button type="button" onClick={onDeny}>
          拒绝
        </button>
        <button type="button" onClick={onApproveForSession}>
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
      </footer>
    </div>
  );
}