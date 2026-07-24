package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Seed writes both templates when the destination dir is empty. settings.json
// has its {{CCP}} token substituted with the ccpName; CLAUDE.md is byte-identical.
func TestSeedWritesWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	if err := Seed(dir, "ccp"); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	for _, name := range []string{"settings.json", "CLAUDE.md"} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		want, err := templates.ReadFile("template/" + name)
		if err != nil {
			t.Fatalf("embedded %s: %v", name, err)
		}
		if name == "settings.json" {
			want = bytes.ReplaceAll(want, []byte("{{CCP}}"), []byte("ccp"))
			if strings.Contains(string(got), "{{CCP}}") {
				t.Errorf("%s: {{CCP}} token not substituted", name)
			}
			if !strings.Contains(string(got), `"ccp statusline"`) {
				t.Errorf("%s: missing substituted statusLine command", name)
			}
		}
		if string(got) != string(want) {
			t.Errorf("%s: seeded bytes differ from expected template", name)
		}
	}
}

// settings.json is managed: Seed overwrites a stale on-disk copy with the
// template so a stick self-heals after a ccp update. CLAUDE.md is write-if-absent:
// an existing operator copy is preserved.
func TestSeedManagedVsPreserved(t *testing.T) {
	dir := t.TempDir()
	stale := []byte(`{"statusLine":{"command":"morgott-statusline"}}`)
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), stale, 0o644); err != nil {
		t.Fatal(err)
	}
	claudeEdit := []byte("# my edited rules\n")
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), claudeEdit, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Seed(dir, "ccp"); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	// settings.json overwritten to the current template (with {{CCP}} substituted).
	got, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	want, _ := templates.ReadFile("template/settings.json")
	want = bytes.ReplaceAll(want, []byte("{{CCP}}"), []byte("ccp"))
	if string(got) != string(want) {
		t.Errorf("settings.json not refreshed to template: got %q", got)
	}

	// CLAUDE.md preserved (operator content, write-if-absent).
	gotMd, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(gotMd) != string(claudeEdit) {
		t.Errorf("CLAUDE.md clobbered: got %q, want %q", gotMd, claudeEdit)
	}
}

// Seed must never touch auth files — .credentials.json / .claude.json are not
// templates, so a reseed leaves authorization intact.
func TestSeedLeavesAuthUntouched(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{".credentials.json", ".claude.json"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("AUTH-SENTINEL"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := Seed(dir, "ccp"); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	for _, f := range []string{".credentials.json", ".claude.json"} {
		got, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if string(got) != "AUTH-SENTINEL" {
			t.Errorf("%s modified by Seed: got %q", f, got)
		}
	}
}

// Seed substitutes the {{CCP}} token in settings.json with the given ccp
// basename so the statusLine command resolves to the real binary on PATH.
func TestSeedSubstitutesCCPName(t *testing.T) {
	dir := t.TempDir()
	if err := Seed(dir, "ccp_windows_amd64"); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"ccp_windows_amd64 statusline"`) {
		t.Errorf("settings.json missing substituted command: got %q", got)
	}
	if strings.Contains(string(got), "{{CCP}}") {
		t.Errorf("settings.json still contains unsubstituted {{CCP}} token")
	}
}
