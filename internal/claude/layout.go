package claude

import (
	"os"
	"path/filepath"

	"github.com/UberMorgott/ClaudeCodePortable/internal/paths"
	"github.com/UberMorgott/ClaudeCodePortable/internal/version"
)

// versionsDir is where the native claude layout keeps per-version binaries:
// <Home>\.local\share\claude\versions\<ver>\claude.exe.
func versionsDir(l paths.Layout) string {
	return filepath.Join(l.Home, ".local", "share", "claude", "versions")
}

// InstalledClaudeExe returns the path to the highest-semver installed
// claude.exe under the versions dir, or "" if none is installed. It is the
// single source of truth for "where does the one real claude.exe live".
func InstalledClaudeExe(l paths.Layout) string {
	base := versionsDir(l)
	entries, err := os.ReadDir(base)
	if err != nil {
		return ""
	}
	best := ""
	bestExe := ""
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		exe := filepath.Join(base, e.Name(), "claude.exe")
		if _, err := os.Stat(exe); err != nil {
			continue
		}
		if best == "" || version.Newer(best, e.Name()) {
			best = e.Name()
			bestExe = exe
		}
	}
	return bestExe
}
