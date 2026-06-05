package tools

import (
	"testing"

	"github.com/blueberrycongee/wuu/internal/providers"
)

func TestToolkitToolDisplayFormatsBuiltInTools(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	tests := []struct {
		name string
		call providers.ToolCall
		want providers.ToolCallDisplay
	}{
		{
			name: "list root",
			call: providers.ToolCall{Name: "list_files", Arguments: `{"path":"."}`},
			want: providers.ToolCallDisplay{Kind: "read", Text: "查看项目目录"},
		},
		{
			name: "read file",
			call: providers.ToolCall{Name: "read_file", Arguments: `{"path":"internal/appserver/model.go"}`},
			want: providers.ToolCallDisplay{Kind: "read", Text: "读取 model.go"},
		},
		{
			name: "glob",
			call: providers.ToolCall{Name: "glob", Arguments: `{"pattern":"**/AGENTS.md","path":"."}`},
			want: providers.ToolCallDisplay{Kind: "search", Text: "搜索 AGENTS.md"},
		},
		{
			name: "git status",
			call: providers.ToolCall{Name: "git", Arguments: `{"subcommand":"status","args":[]}`},
			want: providers.ToolCallDisplay{Kind: "command", Text: "检查 Git 状态"},
		},
		{
			name: "shell",
			call: providers.ToolCall{Name: "run_shell", Arguments: `{"command":"npm run typecheck"}`},
			want: providers.ToolCallDisplay{Kind: "command", Text: "运行 npm run typecheck"},
		},
		{
			name: "create workflow",
			call: providers.ToolCall{Name: "create_workflow", Arguments: `{"definition_name":"feature-delivery"}`},
			want: providers.ToolCallDisplay{Kind: "workflow", Text: "创建工作流 feature-delivery"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := kit.ToolDisplay(tt.call)
			if !ok {
				t.Fatal("expected display metadata")
			}
			if got != tt.want {
				t.Fatalf("unexpected display: got %+v want %+v", got, tt.want)
			}
		})
	}
}

func TestToolkitToolDisplayLeavesMCPRaw(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if got, ok := kit.ToolDisplay(providers.ToolCall{Name: "mcp_docs_search", Arguments: `{"query":"abc"}`}); ok {
		t.Fatalf("MCP tools should not get built-in display metadata, got %+v", got)
	}
}
