package agentcontrol

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/securefs"
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
	// ParentExecutionJournal is the machine-extracted decision journal of
	// the parent's run at rescue time (goal, key decisions, paths taken
	// with outcomes). When present it is the primary parent-side handoff
	// carrier and the self-reported Brief is supplementary. Empty when the
	// extraction was skipped, timed out, or failed.
	ParentExecutionJournal string    `json:"parent_execution_journal,omitempty"`
	Applied                bool      `json:"applied,omitempty"`
	CreatedAt              time.Time `json:"created_at"`
}

// RegisterHelpMeRecovery records the resolved recovery state for a spawned
// HelpMe helper. A configured harness directory is committed atomically before
// the recovery becomes visible in memory; ephemeral runtimes without one keep
// the same in-process behavior.
func (c *AgentControl) RegisterHelpMeRecovery(rec HelpMeRecovery) error {
	if c == nil {
		return errors.New("agent control is required")
	}
	rec.HelperID = strings.TrimSpace(rec.HelperID)
	if rec.HelperID == "" {
		return errors.New("helpme helper id is required")
	}
	rec.SchemaVersion = helpMeRecoverySchemaVersion
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}
	c.helpMeRecoveryMu.Lock()
	defer c.helpMeRecoveryMu.Unlock()
	if err := c.persistHelpMeRecovery(rec); err != nil {
		return err
	}
	if c.helpMeRecoveries == nil {
		c.helpMeRecoveries = make(map[string]HelpMeRecovery)
	}
	c.helpMeRecoveries[rec.HelperID] = rec
	return nil
}

// RemoveHelpMeRecovery rolls back recovery state prepared for a worker that
// never became runnable. Disk is removed before memory so a failed cleanup
// remains visible and can be retried without creating split state.
func (c *AgentControl) RemoveHelpMeRecovery(helperID string) error {
	if c == nil {
		return errors.New("agent control is required")
	}
	helperID = strings.TrimSpace(helperID)
	if helperID == "" {
		return errors.New("helpme helper id is required")
	}
	c.helpMeRecoveryMu.Lock()
	defer c.helpMeRecoveryMu.Unlock()
	if path := c.helpMeRecoveryPath(helperID); path != "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove helpme recovery %s: %w", helperID, err)
		}
	}
	delete(c.helpMeRecoveries, helperID)
	return nil
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
	defer c.helpMeRecoveryMu.Unlock()
	rec, ok := c.helpMeRecoveries[helperID]
	if ok {
		return rec, true
	}
	// Keep the lazy load serialized with registration, marking, and removal.
	// Otherwise a read that started before RemoveHelpMeRecovery deleted the
	// durable snapshot could repopulate the in-memory cache after removal.
	rec, ok = c.loadHelpMeRecovery(helperID)
	if !ok {
		return HelpMeRecovery{}, false
	}
	if c.helpMeRecoveries == nil {
		c.helpMeRecoveries = make(map[string]HelpMeRecovery)
	}
	c.helpMeRecoveries[helperID] = rec
	return rec, true
}

// MarkHelpMeRecoveryApplied flips a helper's recovery to applied exactly
// once. The applied marker is written atomically before the in-memory state is
// advanced, so callers can retry a failed durable commit without losing it.
func (c *AgentControl) MarkHelpMeRecoveryApplied(helperID string) (bool, error) {
	if c == nil {
		return false, errors.New("agent control is required")
	}
	helperID = strings.TrimSpace(helperID)
	if helperID == "" {
		return false, errors.New("helpme helper id is required")
	}
	// Rehydrate from disk first so a post-restart await still applies once.
	if _, ok := c.HelpMeRecoveryForHelper(helperID); !ok {
		return false, nil
	}
	c.helpMeRecoveryMu.Lock()
	rec, ok := c.helpMeRecoveries[helperID]
	if !ok || rec.Applied {
		c.helpMeRecoveryMu.Unlock()
		return false, nil
	}
	rec.Applied = true
	if err := c.persistHelpMeRecovery(rec); err != nil {
		c.helpMeRecoveryMu.Unlock()
		return false, err
	}
	c.helpMeRecoveries[helperID] = rec
	c.helpMeRecoveryMu.Unlock()
	return true, nil
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

func (c *AgentControl) persistHelpMeRecovery(rec HelpMeRecovery) error {
	path := c.helpMeRecoveryPath(rec.HelperID)
	if path == "" {
		return nil
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("encode helpme recovery %s: %w", rec.HelperID, err)
	}
	if err := securefs.WriteFileAtomic(path, data); err != nil {
		return fmt.Errorf("write helpme recovery %s: %w", rec.HelperID, err)
	}
	return nil
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
