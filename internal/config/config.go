// Package config seeds the baseline Claude config (settings.json + CLAUDE.md)
// onto a blank stick. The templates are embedded so a single ccp.exe carries
// them.
//
// Both templates are CCP-MANAGED, so a stuck stick self-heals when a new ccp.exe
// ships changed content (Seed runs on every launch and overwrites them):
//   - settings.json is operational config (env flags, hooks, statusLine command,
//     permissions). It is overwritten from the template every launch so the
//     on-disk copy always matches the running ccp version — e.g. a stick seeded
//     before the embedded statusline shipped keeps a stale statusLine.command
//     until this rewrites it.
//   - CLAUDE.md holds the portable rules — central content authored in the repo,
//     not per-machine. It is likewise overwritten every launch so a single ccp
//     self-update refreshes binary, config, and rules together. Customize both
//     in the repo template, not on the stick.
//
// Seed only ever writes files that exist in the template (settings.json,
// CLAUDE.md). Auth — .credentials.json and .claude.json — is NOT a template, so
// Seed never reads, writes, or deletes it: authorization is untouched by any
// reseed.
package config

import (
	"bytes"
	"embed"
	"os"
	"path/filepath"
)

//go:embed template/*
var templates embed.FS

// managed lists template files ccp owns and overwrites on every Seed. Anything
// not listed is write-if-absent.
var managed = map[string]bool{"settings.json": true, "CLAUDE.md": true}

// Seed writes each embedded template into dir. Managed files (settings.json and
// CLAUDE.md) are overwritten every call so they track the ccp version; any
// unmanaged file would be written only when absent. Files not present in the
// template — including auth — are never touched. In settings.json the {{CCP}}
// token is replaced with ccpName (ccp's own basename) so the statusLine command
// resolves to the actual binary name on PATH. Returns the first real error; nil
// otherwise.
func Seed(dir, ccpName string) error {
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
		if !managed[name] {
			// write-if-absent: leave the operator's copy alone when present.
			if _, err := os.Stat(dst); err == nil {
				continue
			} else if !os.IsNotExist(err) {
				return err
			}
		}
		data, err := templates.ReadFile("template/" + name)
		if err != nil {
			return err
		}
		if name == "settings.json" {
			data = bytes.ReplaceAll(data, []byte("{{CCP}}"), []byte(ccpName))
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
