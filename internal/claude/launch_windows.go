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

// EnsureLayout places data/bin/claude.exe into the self-managing layout the
// native binary expects: <Home>\.local\bin\claude.exe and
// <Home>\.local\share\claude\versions\<ver>\claude.exe. Populating both
// suppresses the "run claude install" re-exec that otherwise drops
// CLAUDE_CONFIG_DIR. Returns the versions\<ver>\claude.exe path to exec:
// launching the .local\bin launcher re-execs into the canonical versioned
// binary in a NEW console window (and leaves ccp blocked on the launcher, so
// wireproxy never tears down). Exec'ing the versioned binary directly runs
// claude inline as ccp's own child — one window, clean teardown on exit.
func EnsureLayout(l paths.Layout) (string, error) {
	src := l.BinPath("claude.exe")
	if _, err := os.Stat(src); err != nil {
		return "", fmt.Errorf("bundled claude.exe: %w", err)
	}
	ver, err := probeVersion(src)
	if err != nil {
		return "", err
	}

	binExe := filepath.Join(l.Home, ".local", "bin", "claude.exe")
	verExe := filepath.Join(l.Home, ".local", "share", "claude", "versions", ver, "claude.exe")
	for _, dst := range []string{binExe, verExe} {
		if err := placeExe(src, dst); err != nil {
			return "", err
		}
	}
	return verExe, nil
}

// placeExe puts src at dst (same NTFS volume): hardlink first, copy on failure.
// Any stale dst is removed so the link/copy is fresh.
func placeExe(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	_ = os.Remove(dst)
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	return copyFile(src, dst)
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
// Layout.Bin is prepended to PATH. Pinning all per-user dirs — not just HOME —
// mirrors the old shell/profile.ps1 host-isolation, so child tools and
// os.UserHomeDir resolve under the stick, never the host's AppData. Kept
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
			out = append(out, name+"="+l.Bin+sep+val)
			pathSet = true
		default:
			out = append(out, kv)
		}
	}
	if !pathSet {
		out = append(out, "PATH="+l.Bin)
	}
	out = append(out,
		"HOME="+l.Home,
		"USERPROFILE="+l.Home,
		"APPDATA="+filepath.Join(l.Home, "AppData", "Roaming"),
		"LOCALAPPDATA="+filepath.Join(l.Home, "AppData", "Local"),
		"CLAUDE_CONFIG_DIR="+l.ClaudeCfg,
	)
	if opts.UseProxy {
		out = append(out,
			"HTTPS_PROXY="+opts.ProxyURL,
			"HTTP_PROXY="+opts.ProxyURL,
		)
	}
	return out
}
