// Package config seeds the baseline Claude config (settings.json + CLAUDE.md)
// onto a blank stick. The templates are embedded so a single ccp.exe carries
// them.
//
// Two update policies, because a stuck stick must self-heal when a new ccp.exe
// ships a changed template (Seed runs on every launch):
//   - settings.json is CCP-MANAGED operational config (env flags, hooks,
//     statusLine command, permissions). It is overwritten from the template on
//     every launch so the on-disk copy always matches the running ccp version —
//     e.g. a stick seeded before the embedded statusline shipped keeps a stale
//     statusLine.command until this rewrites it. Customize it in the repo
//     template, not on the stick.
//   - CLAUDE.md is OPERATOR content (the portable rules); it is written only
//     when absent, never clobbering edits.
//
// Seed only ever writes files that exist in the template (settings.json,
// CLAUDE.md). Auth — .credentials.json and .claude.json — is NOT a template, so
// Seed never reads, writes, or deletes it: authorization is untouched by any
// reseed.
package config

import (
	"embed"
	"os"
	"path/filepath"
)

//go:embed template/*
var templates embed.FS

// managed lists template files ccp owns and overwrites on every Seed. Anything
// not listed is write-if-absent.
var managed = map[string]bool{"settings.json": true}

// Seed writes each embedded template into dir. Managed files (settings.json) are
// overwritten every call so they track the ccp version; unmanaged files
// (CLAUDE.md) are written only when absent so operator edits survive. Files not
// present in the template — including auth — are never touched. Returns the
// first real error; nil otherwise.
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
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
