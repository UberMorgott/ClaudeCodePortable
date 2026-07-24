package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolve(t *testing.T) {
	l := Resolve(filepath.FromSlash("X:/stick/ccp.exe"))
	want := map[string]string{
		"root": filepath.FromSlash("X:/stick"),
		"wg":   filepath.FromSlash("X:/stick/wg-config"),
		"data": filepath.FromSlash("X:/stick/data"),
		"bin":  filepath.FromSlash("X:/stick/data/bin"),
		"cfg":  filepath.FromSlash("X:/stick/data/claude-cfg"),
		"home": filepath.FromSlash("X:/stick/data/home"),
		"run":  filepath.FromSlash("X:/stick/data/_run"),
	}
	if l.Root != want["root"] || l.WGConfig != want["wg"] || l.Bin != want["bin"] ||
		l.ClaudeCfg != want["cfg"] || l.Home != want["home"] || l.Run != want["run"] {
		t.Fatalf("got %+v", l)
	}
}

// TestEnsureRuntimeDirsCreatesWGConfig guards the fresh-stick fix: the operator
// needs wg-config/ to exist so they have somewhere to drop their .vpn file.
func TestEnsureRuntimeDirsCreatesWGConfig(t *testing.T) {
	l := Resolve(filepath.Join(t.TempDir(), "ccp.exe"))
	if err := l.EnsureRuntimeDirs(); err != nil {
		t.Fatalf("EnsureRuntimeDirs: %v", err)
	}
	fi, err := os.Stat(l.WGConfig)
	if err != nil {
		t.Fatalf("stat WGConfig: %v", err)
	}
	if !fi.IsDir() {
		t.Fatalf("WGConfig %q is not a directory", l.WGConfig)
	}
}
