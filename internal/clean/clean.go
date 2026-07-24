// Package clean wipes regenerable runtime/session/cache/junk from the portable
// stick's data dir, so no traces (chat transcripts, caches, telemetry residue)
// are left behind after using the stick on someone else's PC.
//
// Keep/delete contract:
//   - claude-cfg: keep ONLY .credentials.json, .claude.json (AUTH — never
//     deleted), settings.json, CLAUDE.md (config). Everything else goes.
//   - home: keep ONLY the .local\share\claude\versions subtree (the single
//     claude.exe). Everything else under home goes.
//   - _run and _cache under data are removed entirely.
//   - bin (bundled binaries), wg-config (VPN config), and anything outside
//     data are never touched.
package clean

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/UberMorgott/ClaudeCodePortable/internal/paths"
)

// Clean removes all regenerable session/cache/junk data, keeping auth, config,
// and the bundled claude binary. See package doc for the exact contract.
func Clean(l paths.Layout) error {
	if err := cleanDirKeeping(l.ClaudeCfg, map[string]bool{
		".credentials.json": true, // AUTH — never delete
		".claude.json":      true, // AUTH — never delete
		"settings.json":     true,
		"CLAUDE.md":         true,
	}); err != nil {
		return err
	}
	if err := removeAllExcept(l.Home, filepath.Join(l.Home, ".local", "share", "claude", "versions")); err != nil {
		return err
	}
	if err := os.RemoveAll(l.Run); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(l.Data, "_cache"))
}

// cleanDirKeeping removes every top-level entry of dir whose name is not in
// keep. A missing dir is not an error.
func cleanDirKeeping(dir string, keep map[string]bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if keep[e.Name()] {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// removeAllExcept removes everything under root except the subtree at keepAbs.
// Directories that are ancestors of keepAbs are preserved and recursed into so
// their unrelated children are removed; keepAbs itself is left intact. A
// missing root is not an error.
func removeAllExcept(root, keepAbs string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		p := filepath.Join(root, e.Name())
		switch {
		case p == keepAbs:
			// keep this subtree wholesale
		case strings.HasPrefix(keepAbs, p+string(os.PathSeparator)):
			if err := removeAllExcept(p, keepAbs); err != nil {
				return err
			}
		default:
			if err := os.RemoveAll(p); err != nil {
				return err
			}
		}
	}
	return nil
}
