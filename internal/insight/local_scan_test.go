package insight

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalScanSkillUsage(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	sessDir := filepath.Join(home, ".wuu", "sessions")
	scan, err := CollectUsageScan(sessDir)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("token rows: %d\n", len(scan.TokenRows))
	fmt.Printf("skills: %d\n", len(scan.Skills))
	for _, s := range scan.Skills {
		fmt.Printf("  %s: %d\n", s.Name, s.Count)
	}
}
