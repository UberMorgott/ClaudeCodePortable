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

// Seed never overwrites an existing destination file, but does write absent ones.
func TestSeedNoClobber(t *testing.T) {
	dir := t.TempDir()
	sentinel := []byte("custom")
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), sentinel, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Seed(dir); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(sentinel) {
		t.Errorf("settings.json clobbered: got %q, want %q", got, sentinel)
	}
	if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); err != nil {
		t.Errorf("CLAUDE.md not seeded: %v", err)
	}
}
