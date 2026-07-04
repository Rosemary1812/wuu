package agentcontrol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
)

// helpMeRecoveryDirName is the session harness subdirectory holding one
// small JSON snapshot per HelpMe helper. The files exist so an await after
// a process restart can still rebuild the joint compact from the resolved
// brief; when the runtime has no harness directory (ephemeral runs) the
// recovery state lives purely in memory and works the same within the
// process.
const helpMeRecoveryDirName = "helpme-recovery"

const helpMeRecoverySchemaVersion = "wuu/helpme-recovery/v1"

// HelpMeRecoveryBrief is the RESOLVED handoff brief for one HelpMe rescue:
// goal/ask/reason as the helpme tool actually resolved them (args first,
// history fallback, placeholder last), not the raw tool arguments. It is
// the parent-side half of the joint compact built when the helper's result
// is consumed.
type HelpMeRecoveryBrief struct {
	OriginalGoal         string   `json:"original_goal,omitempty"`
	Ask                  string   `json:"ask,omitempty"`
	Reason               string   `json:"reason,omitempty"`
	CurrentUnderstanding string   `json:"current_understanding,omitempty"`
	Constraints          []string `json:"constraints,omitempty"`
	FailedAttempts       []string `json:"failed_attempts,omitempty"`
	Evidence             []string `json:"evidence,omitempty"`
}

// HelpMeRecovery is the first-class state object for one HelpMe rescue.
// It is registered when the helper is spawned and applied at most once:
// the await-side history rewrite flips Applied, so a repeated await of the
// same helper returns the result without wiping post-recovery context.
type HelpMeRecovery struct {
	SchemaVersion string              `json:"schema_version,omitempty"`
	HelperID      string              `json:"helper_id"`
	ParentPath    string              `json:"parent_path,omitempty"`
	Brief         HelpMeRecoveryBrief `json:"brief"`
	Applied       bool                `json:"applied,omitempty"`
	CreatedAt     time.Time           `json:"created_at"`
}

// RegisterHelpMeRecovery records the resolved recovery state for a spawned
// HelpMe helper. Persistence is best-effort: without a harness directory
// the state stays in memory only, which is enough for the in-process
// helpme -> await loop.
func (c *AgentControl) RegisterHelpMeRecovery(rec HelpMeRecovery) {
	if c == nil {
		return
	}
	rec.HelperID = strings.TrimSpace(rec.HelperID)
	if rec.HelperID == "" {
		return
	}
	rec.SchemaVersion = helpMeRecoverySchemaVersion
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}
	c.helpMeRecoveryMu.Lock()
	if c.helpMeRecoveries == nil {
		c.helpMeRecoveries = make(map[string]HelpMeRecovery)
	}
	c.helpMeRecoveries[rec.HelperID] = rec
	c.helpMeRecoveryMu.Unlock()
	c.persistHelpMeRecovery(rec)
}

// HelpMeRecoveryForHelper returns the recovery state registered for a
// helper. On an in-memory miss it lazily rehydrates the snapshot persisted
// in the session harness directory, so awaits keep working across process
// restarts.
func (c *AgentControl) HelpMeRecoveryForHelper(helperID string) (HelpMeRecovery, bool) {
	if c == nil {
		return HelpMeRecovery{}, false
	}
	helperID = strings.TrimSpace(helperID)
	if helperID == "" {
		return HelpMeRecovery{}, false
	}
	c.helpMeRecoveryMu.Lock()
	rec, ok := c.helpMeRecoveries[helperID]
	c.helpMeRecoveryMu.Unlock()
	if ok {
		return rec, true
	}
	rec, ok = c.loadHelpMeRecovery(helperID)
	if !ok {
		return HelpMeRecovery{}, false
	}
	c.helpMeRecoveryMu.Lock()
	if cached, exists := c.helpMeRecoveries[helperID]; exists {
		// A concurrent register/mark beat the lazy load; the in-memory
		// state is newer than the disk snapshot we just read.
		rec = cached
	} else {
		if c.helpMeRecoveries == nil {
			c.helpMeRecoveries = make(map[string]HelpMeRecovery)
		}
		c.helpMeRecoveries[helperID] = rec
	}
	c.helpMeRecoveryMu.Unlock()
	return rec, true
}

// MarkHelpMeRecoveryApplied flips a helper's recovery to applied exactly
// once. It returns false when the recovery is unknown or already applied,
// which makes it usable as the one-shot gate for the history rewrite.
func (c *AgentControl) MarkHelpMeRecoveryApplied(helperID string) bool {
	if c == nil {
		return false
	}
	helperID = strings.TrimSpace(helperID)
	if helperID == "" {
		return false
	}
	// Rehydrate from disk first so a post-restart await still applies once.
	if _, ok := c.HelpMeRecoveryForHelper(helperID); !ok {
		return false
	}
	c.helpMeRecoveryMu.Lock()
	rec, ok := c.helpMeRecoveries[helperID]
	if !ok || rec.Applied {
		c.helpMeRecoveryMu.Unlock()
		return false
	}
	rec.Applied = true
	c.helpMeRecoveries[helperID] = rec
	c.helpMeRecoveryMu.Unlock()
	c.persistHelpMeRecovery(rec)
	return true
}

func (c *AgentControl) helpMeRecoveryPath(helperID string) string {
	if c == nil {
		return ""
	}
	dir := strings.TrimSpace(c.harnessDir)
	id := sanitizeHelpMeRecoveryID(helperID)
	if dir == "" || id == "" {
		return ""
	}
	return filepath.Join(dir, helpMeRecoveryDirName, id+".json")
}

func (c *AgentControl) persistHelpMeRecovery(rec HelpMeRecovery) {
	path := c.helpMeRecoveryPath(rec.HelperID)
	if path == "" {
		return
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		providers.DebugLogf("agentcontrol: encode helpme recovery %s: %v", rec.HelperID, err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		providers.DebugLogf("agentcontrol: create helpme recovery dir: %v", err)
		return
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		providers.DebugLogf("agentcontrol: write helpme recovery %s: %v", rec.HelperID, err)
	}
}

func (c *AgentControl) loadHelpMeRecovery(helperID string) (HelpMeRecovery, bool) {
	path := c.helpMeRecoveryPath(helperID)
	if path == "" {
		return HelpMeRecovery{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return HelpMeRecovery{}, false
	}
	var rec HelpMeRecovery
	if err := json.Unmarshal(data, &rec); err != nil {
		providers.DebugLogf("agentcontrol: decode helpme recovery %s: %v", helperID, err)
		return HelpMeRecovery{}, false
	}
	if strings.TrimSpace(rec.HelperID) != strings.TrimSpace(helperID) {
		return HelpMeRecovery{}, false
	}
	return rec, true
}

func sanitizeHelpMeRecoveryID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
		if b.Len() >= 96 {
			break
		}
	}
	return strings.Trim(b.String(), "_-")
}
