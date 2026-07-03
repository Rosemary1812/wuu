/**
 * Design-system catalog of every tool-approval card variant the renderer
 * can produce.
 *
 * Two sections:
 *   1. Gallery: each approval kind rendered in isolation with its
 *      `danger`/read-only state, capability and the human description.
 *      Built by mounting the real `ToolApprovalCard` with hand-crafted
 *      `PendingToolApproval` fixtures, so what you see here is what
 *      the user sees inside an assistant turn.
 *   2. In-Context: representative approvals framed inside a mock
 *      conversation slice (user message bubble + assistant activity
 *      text + the approval card) so the card's real spacing, focus
 *      ring, and surroundings are visible.
 *
 * Gated behind the debug-controls switch in Settings. The switch is
 * itself only visible in development builds, so this catalog never
 * reaches production. See AGENTS.md "Desktop Debug Controls".
 */
import { ShieldCheck, X } from "lucide-react";
import { type JSX } from "react";
import type { PendingToolApproval } from "../shared/protocol";
import { ToolApprovalCard } from "./ToolApprovalCard";

export function ApprovalGalleryPanel({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}): JSX.Element | null {
  if (!open) {
    return null;
  }
  return (
    <div
      className="approval-gallery-backdrop"
      role="presentation"
      onClick={onClose}
    >
      <div
        className="approval-gallery-panel"
        role="dialog"
        aria-label="审批图鉴"
        aria-modal="true"
        onClick={(event) => event.stopPropagation()}
      >
        <header className="approval-gallery-header">
          <div>
            <h2>审批图鉴</h2>
            <p>
              Tool approval 卡片的设计系统图鉴。每一个变体都通过真实的{" "}
              <code>ToolApprovalCard</code> 渲染,跟 conversation 流里看到的
              完全一致。点击背景关闭。
            </p>
          </div>
          <button
            className="icon-button"
            type="button"
            aria-label="关闭"
            onClick={onClose}
          >
            <X className="icon" />
          </button>
        </header>
        <div className="approval-gallery-body">
          <ApprovalGalleryGallery />
          <ApprovalGalleryInContext />
        </div>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Gallery section: every approval variant in isolation
// ---------------------------------------------------------------------------

type GalleryEntry = {
  /** Short label shown next to the card. */
  label: string;
  /** `capability · action` — drives the visual contract documentation. */
  capability: string;
  /** One-line description of when this card fires. */
  description: string;
  /** The fixture rendered through the real `ToolApprovalCard`. */
  approval: () => PendingToolApproval;
};

const APPROVAL_FIXTURES: GalleryEntry[] = [
  // -----------------------------------------------------------------------
  // Read-only / neutral operations
  // -----------------------------------------------------------------------
  {
    label: "Shell · 读",
    capability: "command.bash · execute",
    description: "只读命令,默认中性样式,无 risk 标签",
    approval: () => readOnlyShell(),
  },
  {
    label: "Network · GET",
    capability: "network.fetch · GET",
    description: "GET 请求,只读,保持中性",
    approval: () => networkGet(),
  },
  {
    label: "File · read",
    capability: "file.read · read",
    description: "读取文件,无破坏性",
    approval: () => fileRead(),
  },

  // -----------------------------------------------------------------------
  // Destructive / non-idempotent operations
  // -----------------------------------------------------------------------
  {
    label: "Shell · 删",
    capability: "command.bash · execute",
    description: "rm -rf 等破坏性命令,触发 .danger",
    approval: () => destructiveShell(),
  },
  {
    label: "Shell · git push",
    capability: "command.bash · execute",
    description: "git push 写入远程仓库,触发 .danger",
    approval: () => gitPush(),
  },
  {
    label: "Network · POST",
    capability: "network.fetch · POST",
    description: "写入远端 API,自动判定为 danger",
    approval: () => networkPost(),
  },
  {
    label: "Network · DELETE",
    capability: "network.fetch · DELETE",
    description: "删除远端资源,自动判定为 danger",
    approval: () => networkDelete(),
  },
  {
    label: "File · write",
    capability: "file.write · write",
    description: "写文件,带 destructive=true",
    approval: () => fileWrite(),
  },

  // -----------------------------------------------------------------------
  // With rule / long preview
  // -----------------------------------------------------------------------
  {
    label: "With matched rule",
    capability: "command.bash · execute",
    description: "命中授权规则,可展开查看 rule",
    approval: () => withRule(),
  },
  {
    label: "Long arguments",
    capability: "command.bash · execute",
    description: "参数 > 6 行,默认折叠,可展开",
    approval: () => longArguments(),
  },
];

function readOnlyShell(): PendingToolApproval {
  return {
    server_request_id: "gallery-r",
    id: "gallery-read-only-shell",
    tool_name: "run_shell",
    call_id: "call-r-1",
    capability: "command.bash",
    capability_action: "execute",
    capability_object: "ls -la /Users/me/projects",
    arguments_preview: '{\n  "command": "ls -la /Users/me/projects",\n  "cwd": "/Users/me"\n}',
    policy_reason: "需要审批",
    read_only: true,
  };
}

function destructiveShell(): PendingToolApproval {
  return {
    server_request_id: "gallery-d",
    id: "gallery-destructive-shell",
    tool_name: "run_shell",
    call_id: "call-d-1",
    capability: "command.bash",
    capability_action: "execute",
    capability_object: "rm -rf /Users/me/projects/wuu/node_modules",
    arguments_preview:
      '{\n  "command": "rm -rf /Users/me/projects/wuu/node_modules",\n  "cwd": "/Users/me/projects/wuu"\n}',
    policy_reason: "需要审批",
    destructive: true,
  };
}

function gitPush(): PendingToolApproval {
  return {
    server_request_id: "gallery-p",
    id: "gallery-git-push",
    tool_name: "run_shell",
    call_id: "call-p-1",
    capability: "command.bash",
    capability_action: "execute",
    capability_object: "git push origin main --force-with-lease",
    arguments_preview:
      '{\n  "command": "git push origin main --force-with-lease",\n  "cwd": "/Users/me/projects/wuu"\n}',
    policy_reason: "需要审批",
    destructive: true,
  };
}

function networkGet(): PendingToolApproval {
  return {
    server_request_id: "gallery-net-get",
    id: "gallery-net-get",
    tool_name: "fetch",
    call_id: "call-net-get",
    capability: "network.fetch",
    capability_action: "GET",
    capability_object: "https://api.github.com/users/octocat",
    arguments_preview:
      '{\n  "method": "GET",\n  "url": "https://api.github.com/users/octocat",\n  "headers": { "Accept": "application/json" }\n}',
    policy_reason: "需要审批",
  };
}

function networkPost(): PendingToolApproval {
  return {
    server_request_id: "gallery-net-post",
    id: "gallery-net-post",
    tool_name: "fetch",
    call_id: "call-net-post",
    capability: "network.fetch",
    capability_action: "POST",
    capability_object: "https://api.example.com/v1/items",
    arguments_preview:
      '{\n  "method": "POST",\n  "url": "https://api.example.com/v1/items",\n  "headers": { "Authorization": "Bearer ***" },\n  "body": { "name": "New item", "tags": ["demo"] }\n}',
    policy_reason: "需要审批",
  };
}

function networkDelete(): PendingToolApproval {
  return {
    server_request_id: "gallery-net-delete",
    id: "gallery-net-delete",
    tool_name: "fetch",
    call_id: "call-net-delete",
    capability: "network.fetch",
    capability_action: "DELETE",
    capability_object: "https://api.example.com/v1/items/42",
    arguments_preview:
      '{\n  "method": "DELETE",\n  "url": "https://api.example.com/v1/items/42"\n}',
    policy_reason: "需要审批",
  };
}

function fileRead(): PendingToolApproval {
  return {
    server_request_id: "gallery-file-read",
    id: "gallery-file-read",
    tool_name: "read_file",
    call_id: "call-file-read",
    capability: "file.read",
    capability_action: "read",
    capability_object: "/Users/me/projects/wuu/README.md",
    arguments_preview:
      '{\n  "path": "/Users/me/projects/wuu/README.md"\n}',
    policy_reason: "需要审批",
    read_only: true,
  };
}

function fileWrite(): PendingToolApproval {
  return {
    server_request_id: "gallery-file-write",
    id: "gallery-file-write",
    tool_name: "write_file",
    call_id: "call-file-write",
    capability: "file.write",
    capability_action: "write",
    capability_object: "/Users/me/projects/wuu/src/foo.ts",
    arguments_preview:
      '{\n  "path": "/Users/me/projects/wuu/src/foo.ts",\n  "content": "export const hello = (name: string) => `hello ${name}`;\\n"\n}',
    policy_reason: "需要审批",
    destructive: true,
  };
}

function withRule(): PendingToolApproval {
  return {
    server_request_id: "gallery-rule",
    id: "gallery-with-rule",
    tool_name: "run_shell",
    call_id: "call-rule",
    capability: "command.bash",
    capability_action: "execute",
    capability_object: "git push origin main",
    arguments_preview:
      '{\n  "command": "git push origin main",\n  "cwd": "/Users/me/projects/wuu"\n}',
    policy_reason: "需要审批",
    destructive: true,
    capability_rule: 'allow git:*  // permitted by .wuu/permissions.toml',
  };
}

function longArguments(): PendingToolApproval {
  return {
    server_request_id: "gallery-long",
    id: "gallery-long-args",
    tool_name: "run_shell",
    call_id: "call-long",
    capability: "command.bash",
    capability_action: "execute",
    capability_object: "docker compose up -d",
    arguments_preview: [
      '{',
      '  "command": "docker compose up -d",',
      '  "env": {',
      '    "POSTGRES_USER": "app",',
      '    "POSTGRES_PASSWORD": "***",',
      '    "POSTGRES_DB": "wuu_dev",',
      '    "REDIS_URL": "redis://redis:6379"',
      '  },',
      '  "services": ["api", "worker", "redis", "postgres"],',
      '  "detach": true',
      '}',
    ].join("\n"),
    policy_reason: "需要审批",
  };
}

function noopHandlers(): {
  onApprove: () => void;
  onApproveForSession: () => void;
  onDeny: () => void;
} {
  return {
    onApprove: () => undefined,
    onApproveForSession: () => undefined,
    onDeny: () => undefined,
  };
}

function ApprovalGalleryGallery(): JSX.Element {
  return (
    <section className="approval-gallery-section">
      <header>
        <h3>Gallery</h3>
        <p>
          按 <code>capability · action</code> 分组,每行独立展示一个审批卡片。
          Hover 卡片看完整字段;点击按钮不会真的处理(展厅里都是 noop)。
        </p>
      </header>
      <ul className="approval-gallery-entries">
        {APPROVAL_FIXTURES.map((entry, index) => {
          const handlers = noopHandlers();
          return (
            <li
              key={`${entry.label}-${index}`}
              className="approval-gallery-entry"
              data-kind={entry.capability}
            >
              <div className="approval-gallery-entry-card">
                <ToolApprovalCard
                  approval={entry.approval()}
                  {...handlers}
                />
              </div>
              <div className="approval-gallery-entry-meta">
                <strong className="approval-gallery-entry-label">
                  {entry.label}
                </strong>
                <code className="approval-gallery-entry-kind">
                  {entry.capability}
                </code>
                <span className="approval-gallery-entry-description">
                  {entry.description}
                </span>
              </div>
            </li>
          );
        })}
      </ul>
    </section>
  );
}

// ---------------------------------------------------------------------------
// In-Context section: approval cards in a representative conversation slice
// ---------------------------------------------------------------------------

type ContextSlice = {
  heading: string;
  userMessage: string;
  assistantNarration: string;
  approval: PendingToolApproval;
};

const CONTEXT_SLICES: ContextSlice[] = [
  {
    heading: "中性命令(读)",
    userMessage: "看一下当前项目的目录结构",
    assistantNarration: "我先用 ls 列一下根目录,确认整体结构再继续。",
    approval: readOnlyShell(),
  },
  {
    heading: "破坏性命令(rm)",
    userMessage: "把 node_modules 清掉重装依赖",
    assistantNarration: "清理依赖前需要你确认,这会删除整个 node_modules 目录。",
    approval: destructiveShell(),
  },
  {
    heading: "远端写入(POST)",
    userMessage: "把这条记录写到远端 API",
    assistantNarration: "提交之前我会先把请求体准备好,需要你确认后才会发出。",
    approval: networkPost(),
  },
  {
    heading: "带规则匹配",
    userMessage: "把本地提交推送到 origin",
    assistantNarration: "我已经匹配到一条授权规则,会一并展示出来供你检查。",
    approval: withRule(),
  },
];

function ApprovalGalleryInContext(): JSX.Element {
  return (
    <section className="approval-gallery-section">
      <header>
        <h3>In-Context</h3>
        <p>
          把卡片塞进一个最小化的对话切片里,展示它在 user 消息 + assistant
          narration 之间的真实间距和排版。
        </p>
      </header>
      <ul className="approval-gallery-context">
        {CONTEXT_SLICES.map((slice, index) => {
          const handlers = noopHandlers();
          return (
            <li
              key={`${slice.heading}-${index}`}
              className="approval-gallery-context-item"
              data-heading={slice.heading}
            >
              <h4>
                <ShieldCheck className="icon" aria-hidden="true" />
                {slice.heading}
              </h4>
              <div className="approval-gallery-context-frame">
                <div className="approval-gallery-context-user">
                  {slice.userMessage}
                </div>
                <div className="approval-gallery-context-assistant">
                  {slice.assistantNarration}
                </div>
                <ToolApprovalCard
                  approval={slice.approval}
                  {...handlers}
                />
              </div>
            </li>
          );
        })}
      </ul>
    </section>
  );
}
