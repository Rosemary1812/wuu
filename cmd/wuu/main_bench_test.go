package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/blueberrycongee/wuu/internal/config"
	"github.com/blueberrycongee/wuu/internal/memory"
	"github.com/blueberrycongee/wuu/internal/skills"
)

func BenchmarkStartup_MemoryDiscovery(b *testing.B) {
	rootDir := "/Users/blueberrycongee/wuu"
	homeDir := os.Getenv("HOME")
	opts := memory.DefaultOptions()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = memory.Discover(rootDir, homeDir, opts)
	}
}

func BenchmarkStartup_SkillsDiscovery(b *testing.B) {
	rootDir := "/Users/blueberrycongee/wuu"
	projectSkillsDir := filepath.Join(rootDir, ".claude", "skills")
	userSkillsDir := ""
	if home := os.Getenv("HOME"); home != "" {
		userSkillsDir = filepath.Join(home, ".claude", "skills")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = skills.Discover(projectSkillsDir, userSkillsDir)
	}
}

func BenchmarkStartup_ConfigLoad(b *testing.B) {
	rootDir := "/Users/blueberrycongee/wuu"
	homeDir := os.Getenv("HOME")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = config.LoadFrom(rootDir, homeDir)
	}
}
