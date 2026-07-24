package update

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/UberMorgott/ClaudeCodePortable/internal/paths"
)

// TestClaudeInstallRejectsPathInjection verifies the strictSemver guard rejects a
// ver carrying path separators/".." before any network call, so the value can
// never steer the versions-dir write outside its tree.
func TestClaudeInstallRejectsPathInjection(t *testing.T) {
	var claude Comp
	for _, c := range Components() {
		if c.Name == "claude" {
			claude = c
			break
		}
	}
	if claude.Install == nil {
		t.Fatal("claude component not found")
	}

	home := t.TempDir()
	l := paths.Layout{Home: home}
	// nil fetch.Progress is a valid zero value (Progress is a func type).
	err := claude.Install(context.Background(), l, "2.1.190/../evil", nil)
	if err == nil {
		t.Fatal("expected error for path-injection ver, got nil")
	}
	if !strings.Contains(err.Error(), "unsafe") && !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("expected guard error, got: %v", err)
	}
	// Guard runs before any mkdir/HTTP: no stray dir escaped the versions tree.
	if _, statErr := os.Stat(filepath.Join(home, ".local", "share", "claude", "versions", "2.1.190")); !os.IsNotExist(statErr) {
		t.Fatalf("versions dir should not have been created: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".local", "share", "claude", "evil")); !os.IsNotExist(statErr) {
		t.Fatalf("escaped 'evil' dir should not exist: %v", statErr)
	}
}
