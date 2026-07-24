// Package claude launches the bundled claude.exe with a pinned, host-scrubbed
// environment so the portable stick's config is used instead of the host's.
package claude

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/UberMorgott/ClaudeCodePortable/internal/paths"
	"github.com/UberMorgott/ClaudeCodePortable/internal/version"
)

// LaunchOpts configures a claude launch.
type LaunchOpts struct {
	Layout   paths.Layout
	UseProxy bool   // set HTTPS_PROXY/HTTP_PROXY when true
	ProxyURL string // e.g. "http://127.0.0.1:25345"
	WorkDir  string // working dir for claude; empty = inherit ccp's cwd
}

// probeVersion returns claude's version string ("x.y.z") from `exe --version`.
// Package var so tests can inject a fixed version without a real claude.exe.
var probeVersion = func(exe string) (string, error) {
	out, err := exec.Command(exe, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("claude --version: %w", err)
	}
	v := version.ParseClaude(string(out))
	if v == "" {
		return "", fmt.Errorf("claude --version: unparseable output %q", out)
	}
	return v, nil
}

// EnsureLayout returns the single canonical claude.exe to exec —
// <Home>\.local\share\claude\versions\<ver>\claude.exe — creating exactly one
// physical copy on any filesystem (no hardlink/symlink needed). It migrates a
// legacy bin\claude.exe staging copy into the versions layout, prunes older
// version dirs and legacy duplicate locations (.local\bin, stale bin\claude.exe),
// and writes a text shim bin\claude.cmd so `claude` on PATH resolves to the
// versioned binary in-session. Exec'ing the versioned binary directly (as Run
// does) runs claude inline as ccp's child — one window, clean teardown.
func EnsureLayout(l paths.Layout) (string, error) {
	verExe := InstalledClaudeExe(l)
	if verExe == "" {
		// Migrate a legacy staging copy (older sticks / pre-migration) into the
		// versions layout via move, so no second physical copy is created.
		src := l.BinPath("claude.exe")
		if _, err := os.Stat(src); err != nil {
			return "", fmt.Errorf("claude.exe not installed (run update): %w", err)
		}
		ver, err := probeVersion(src)
		if err != nil {
			return "", err
		}
		verExe = filepath.Join(versionsDir(l), ver, "claude.exe")
		if err := os.MkdirAll(filepath.Dir(verExe), 0o755); err != nil {
			return "", err
		}
		if err := moveFile(src, verExe); err != nil {
			return "", err
		}
	}
	// Single-copy hygiene (best-effort; must not block launch):
	keep := filepath.Base(filepath.Dir(verExe))
	pruneOldVersions(l, keep)
	_ = os.RemoveAll(filepath.Join(l.Home, ".local", "bin")) // legacy launcher copy
	_ = os.Remove(l.BinPath("claude.exe"))                    // legacy staging copy
	if err := writeShim(l, verExe); err != nil {
		return "", err
	}
	return verExe, nil
}

// moveFile renames src to dst (atomic on the same volume — the stick case);
// on a cross-volume error it falls back to copy + remove.
func moveFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyFile(src, dst); err != nil {
		return err
	}
	return os.Remove(src)
}

// pruneOldVersions removes every versions\<ver> dir except keep, so stale
// binaries never accumulate. Best-effort.
func pruneOldVersions(l paths.Layout, keep string) {
	base := versionsDir(l)
	entries, err := os.ReadDir(base)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() && e.Name() != keep {
			_ = os.RemoveAll(filepath.Join(base, e.Name()))
		}
	}
}

// writeShim writes bin\claude.cmd, a text launcher so `claude` on PATH resolves
// to the versioned binary (a real .exe copy in bin would defeat single-copy).
// The path is relative to the shim (%~dp0 = bin\) so it survives drive-letter
// changes on the portable stick.
func writeShim(l paths.Layout, verExe string) error {
	rel, err := filepath.Rel(l.Bin, verExe)
	if err != nil {
		rel = verExe // absolute fallback
	}
	content := "@\"%~dp0" + rel + "\" %*\r\n"
	if err := os.MkdirAll(l.Bin, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(l.Bin, "claude.cmd"), []byte(content), 0o644)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// Run execs claudeExe, handing it this console (stdin/out/err), with the pinned
// env, and blocks until it exits. Returns claude's exit code.
func Run(opts LaunchOpts) (int, error) {
	claudeExe, err := EnsureLayout(opts.Layout)
	if err != nil {
		return 1, err
	}
	cmd := buildCmd(claudeExe, opts)
	if err := cmd.Run(); err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			return 1, err // failed to start / other error, not a clean exit code
		}
	}
	return cmd.ProcessState.ExitCode(), nil
}

// buildCmd constructs the claude *exec.Cmd (console handles, pinned env, and
// working dir) without running it, so it can be inspected in tests.
func buildCmd(claudeExe string, opts LaunchOpts) *exec.Cmd {
	cmd := exec.Command(claudeExe)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = buildEnv(os.Environ(), opts)
	if opts.WorkDir != "" {
		cmd.Dir = opts.WorkDir
	}
	return cmd
}

// buildEnv derives the pinned launch env from base (an os.Environ-style slice).
// Host ANTHROPIC_*/CLAUDE_CODE_* and any per-user path vars (HOME, USERPROFILE,
// APPDATA, LOCALAPPDATA) plus HTTPS_PROXY/HTTP_PROXY are scrubbed
// (case-insensitive name match); the stick's values are then pinned and
// Layout.Root+Bin are prepended to PATH (Root so `ccp statusline` resolves).
// Pinning all per-user dirs — not just HOME —
// mirrors the old shell/profile.ps1 host-isolation, so child tools and
// os.UserHomeDir resolve under the stick, never the host's AppData.
// DISABLE_UPDATES=1 fully suppresses claude's self-relocation/reinstall (the
// settings.json DISABLE_AUTOUPDATER only stops the background check). Kept
// pure/param-driven for testing.
func buildEnv(base []string, opts LaunchOpts) []string {
	l := opts.Layout
	sep := string(os.PathListSeparator)
	out := make([]string, 0, len(base)+6)
	pathSet := false

	for _, kv := range base {
		name := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			name = kv[:i]
		}
		lname := strings.ToLower(name)
		switch {
		case strings.HasPrefix(lname, "anthropic_"), strings.HasPrefix(lname, "claude_code_"):
			continue
		case lname == "home", lname == "userprofile", lname == "appdata", lname == "localappdata",
			lname == "https_proxy", lname == "http_proxy":
			continue
		case lname == "path":
			val := ""
			if i := strings.IndexByte(kv, '='); i >= 0 {
				val = kv[i+1:]
			}
			// Prepend Root (where ccp.exe lives, so `ccp statusline` resolves)
			// then Bin (bundled claude/rtk/wireproxy).
			out = append(out, name+"="+l.Root+sep+l.Bin+sep+val)
			pathSet = true
		default:
			out = append(out, kv)
		}
	}
	if !pathSet {
		out = append(out, "PATH="+l.Root+sep+l.Bin)
	}
	out = append(out,
		"HOME="+l.Home,
		"USERPROFILE="+l.Home,
		"APPDATA="+filepath.Join(l.Home, "AppData", "Roaming"),
		"LOCALAPPDATA="+filepath.Join(l.Home, "AppData", "Local"),
		"CLAUDE_CONFIG_DIR="+l.ClaudeCfg,
		"DISABLE_UPDATES=1",
	)
	if opts.UseProxy {
		out = append(out,
			"HTTPS_PROXY="+opts.ProxyURL,
			"HTTP_PROXY="+opts.ProxyURL,
		)
	}
	return out
}
