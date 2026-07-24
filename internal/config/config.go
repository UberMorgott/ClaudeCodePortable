// Package config seeds the baseline Claude config (settings.json + CLAUDE.md)
// onto a blank stick. The templates are embedded so a single ccp.exe carries
// them; Seed writes each only when absent, never clobbering operator edits.
package config

import (
	"embed"
	"os"
	"path/filepath"
)

//go:embed template/*
var templates embed.FS

// Seed writes each embedded template into dir, but only when the destination
// file does not already exist — a re-launch must never overwrite the operator's
// edited config. Returns the first real error; nil on success or all-skipped.
func Seed(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	entries, err := templates.ReadDir("template")
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		dst := filepath.Join(dir, name)
		if _, err := os.Stat(dst); err == nil {
			continue // present — leave the operator's copy alone
		} else if !os.IsNotExist(err) {
			return err
		}
		data, err := templates.ReadFile("template/" + name)
		if err != nil {
			return err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
