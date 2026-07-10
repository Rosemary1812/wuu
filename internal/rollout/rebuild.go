package rollout

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/blueberrycongee/wuu/internal/providers"
)

// Rebuild reads the JSONL rollout for threadID and returns the
// reconstructed ChatMessage slice. Malformed lines are skipped with no
// error — the alternative (returning an error) would block startup
// over a single bad write, which is worse than losing one item. An
// absent file returns (nil, nil).
//
// ToolCall, ToolResult, TurnMeta, and SessionMeta items are not part
// of the message history. They are consumed by piece 2 (turn_handlers
// integration) for retry dedup, finish status, and session metadata.
// This function only rebuilds the ChatMessage slice; the caller is
// responsible for parsing the rest of the file if it needs that data.
func Rebuild(threadID string) ([]providers.ChatMessage, error) {
	path, err := Path(threadID)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var messages []providers.ChatMessage
	scanner := bufio.NewScanner(f)
	// Allow up to 16 MB per line. Tool outputs (file dumps, large shell
	// results) can easily exceed the default 64 KB scanner buffer.
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var item RolloutItem
		if err := json.Unmarshal(line, &item); err != nil {
			// Skip malformed lines (truncated write, schema mismatch,
			// partial JSON). One bad item shouldn't block startup.
			continue
		}
		switch item.Type {
		case TypeUserMessage:
			if item.UserMessage != nil {
				messages = append(messages, providers.ChatMessage{
					Role:    "user",
					Content: item.UserMessage.Content,
				})
			}
		case TypeAssistantMessage:
			if item.AssistantMessage != nil {
				messages = append(messages, providers.ChatMessage{
					Role:    "assistant",
					Content: item.AssistantMessage.Content,
				})
			}
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return messages, err
	}
	return messages, nil
}
