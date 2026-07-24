package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Seed writes both templates when the destination dir is empty.
func TestSeedWritesWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	if err := Seed(dir); err != nil {
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
		if string(got) != string(want) {
			t.Errorf("%s: seeded bytes differ from embedded template", name)
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
	if err := Seed(dir); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	// settings.json overwritten to the current template.
	got, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	want, _ := templates.ReadFile("template/settings.json")
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
	if err := Seed(dir); err != nil {
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
