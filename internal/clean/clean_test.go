package clean

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/UberMorgott/ClaudeCodePortable/internal/paths"
)

// write creates parent dirs and writes a file with sentinel bytes.
func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// layoutIn mirrors paths.Resolve's field relationships against a temp root.
func layoutIn(root string) paths.Layout {
	data := filepath.Join(root, "data")
	return paths.Layout{
		Root:      root,
		WGConfig:  filepath.Join(root, "wg-config"),
		Data:      data,
		Bin:       filepath.Join(data, "bin"),
		ClaudeCfg: filepath.Join(data, "claude-cfg"),
		Home:      filepath.Join(data, "home"),
		Run:       filepath.Join(data, "_run"),
	}
}

func TestClean(t *testing.T) {
	root := t.TempDir()
	l := layoutIn(root)

	claudeExe := filepath.Join(l.Home, ".local", "share", "claude", "versions", "2.1.218", "claude.exe")
	cache := filepath.Join(l.Data, "_cache")

	// Kept config/auth (with distinct sentinels).
	write(t, filepath.Join(l.ClaudeCfg, ".credentials.json"), "AUTH-CREDS")
	write(t, filepath.Join(l.ClaudeCfg, ".claude.json"), "AUTH-CLAUDE")
	write(t, filepath.Join(l.ClaudeCfg, "settings.json"), "SETTINGS")
	write(t, filepath.Join(l.ClaudeCfg, "CLAUDE.md"), "CLAUDEMD")
	// Junk in claude-cfg.
	write(t, filepath.Join(l.ClaudeCfg, "projects", "p", "sess.jsonl"), "x")
	write(t, filepath.Join(l.ClaudeCfg, "statsig", "x"), "x")
	write(t, filepath.Join(l.ClaudeCfg, "shell-snapshots", "s.ps1"), "x")
	write(t, filepath.Join(l.ClaudeCfg, "history.jsonl"), "x")
	write(t, filepath.Join(l.ClaudeCfg, "cache", "changelog.md"), "x")
	write(t, filepath.Join(l.ClaudeCfg, "stats-cache.json"), "x")

	// Home: keep the binary subtree, junk everywhere else.
	write(t, claudeExe, "BINARY")
	write(t, filepath.Join(l.Home, "AppData", "Roaming", "junk"), "x")
	write(t, filepath.Join(l.Home, ".claude", "x"), "x")
	write(t, filepath.Join(l.Home, ".local", "bin", "claude.exe"), "x")

	// Runtime + cache dirs.
	write(t, filepath.Join(l.Run, "wireproxy.pid"), "x")
	write(t, filepath.Join(cache, "blob"), "x")

	// Untouchable: bin + wg-config.
	write(t, filepath.Join(l.Bin, "claude.cmd"), "SHIM")
	write(t, filepath.Join(l.Bin, "claude.exe"), "BUNDLED")
	write(t, filepath.Join(l.WGConfig, "amnezia.vpn"), "VPN")

	if err := Clean(l); err != nil {
		t.Fatalf("Clean: %v", err)
	}

	// Kept config/auth survive with original bytes.
	for name, want := range map[string]string{
		".credentials.json": "AUTH-CREDS",
		".claude.json":      "AUTH-CLAUDE",
		"settings.json":     "SETTINGS",
		"CLAUDE.md":         "CLAUDEMD",
	} {
		got, err := os.ReadFile(filepath.Join(l.ClaudeCfg, name))
		if err != nil {
			t.Errorf("kept %s missing: %v", name, err)
		} else if string(got) != want {
			t.Errorf("kept %s = %q, want %q", name, got, want)
		}
	}

	// claude-cfg junk gone.
	for _, junk := range []string{"projects", "statsig", "shell-snapshots", "history.jsonl", "cache", "stats-cache.json"} {
		if exists(filepath.Join(l.ClaudeCfg, junk)) {
			t.Errorf("claude-cfg junk survived: %s", junk)
		}
	}

	// Binary survives with original bytes.
	if got, err := os.ReadFile(claudeExe); err != nil {
		t.Errorf("claude.exe missing: %v", err)
	} else if string(got) != "BINARY" {
		t.Errorf("claude.exe = %q, want BINARY", got)
	}

	// Home junk gone.
	for _, junk := range []string{"AppData", ".claude", filepath.Join(".local", "bin")} {
		if exists(filepath.Join(l.Home, junk)) {
			t.Errorf("home junk survived: %s", junk)
		}
	}

	// Runtime + cache gone.
	if exists(l.Run) {
		t.Errorf("_run survived")
	}
	if exists(cache) {
		t.Errorf("_cache survived")
	}

	// Untouchable survive.
	for path, want := range map[string]string{
		filepath.Join(l.Bin, "claude.cmd"):    "SHIM",
		filepath.Join(l.Bin, "claude.exe"):    "BUNDLED",
		filepath.Join(l.WGConfig, "amnezia.vpn"): "VPN",
	} {
		if got, err := os.ReadFile(path); err != nil {
			t.Errorf("untouchable %s missing: %v", path, err)
		} else if string(got) != want {
			t.Errorf("untouchable %s = %q, want %q", path, got, want)
		}
	}
}

func TestCleanEmptyLayout(t *testing.T) {
	// No dirs exist under a fresh root — Clean must not error on missing dirs.
	if err := Clean(layoutIn(t.TempDir())); err != nil {
		t.Fatalf("Clean on empty layout: %v", err)
	}
}
