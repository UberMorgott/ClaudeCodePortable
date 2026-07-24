package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/UberMorgott/ClaudeCodePortable/internal/paths"
)

// envMap splits a []string env into name->value (last wins, like the OS).
func envMap(env []string) map[string]string {
	m := map[string]string{}
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	return m
}

// countName returns how many entries have the given (exact) name.
func countName(env []string, name string) int {
	n := 0
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i >= 0 && kv[:i] == name {
			n++
		}
	}
	return n
}

func testLayout() paths.Layout {
	return paths.Layout{
		Bin:       `C:\stick\data\bin`,
		ClaudeCfg: `C:\stick\data\claude-cfg`,
		Home:      `C:\stick\data\home`,
	}
}

func TestBuildEnv_ScrubsHostAndPins(t *testing.T) {
	base := []string{
		"ANTHROPIC_API_KEY=secret",
		"anthropic_base_url=https://host", // case-insensitive
		"CLAUDE_CODE_FOO=1",
		"claude_code_bar=2",
		"HOME=C:\\Users\\host",
		"USERPROFILE=C:\\Users\\host",
		"APPDATA=C:\\Users\\host\\AppData\\Roaming",
		"LOCALAPPDATA=C:\\Users\\host\\AppData\\Local",
		"HTTPS_PROXY=http://host-proxy",
		"http_proxy=http://host-proxy2",
		"PATH=C:\\Windows;C:\\tools",
		"KEEPME=yes",
	}
	l := testLayout()
	env := buildEnv(base, LaunchOpts{Layout: l})
	m := envMap(env)

	// Host scrub: nothing ANTHROPIC_*/CLAUDE_CODE_* survives.
	for _, kv := range env {
		ln := strings.ToLower(kv)
		if strings.HasPrefix(ln, "anthropic_") || strings.HasPrefix(ln, "claude_code_") {
			t.Errorf("host var leaked: %q", kv)
		}
	}
	// Unrelated var preserved.
	if m["KEEPME"] != "yes" {
		t.Errorf("KEEPME not preserved: %q", m["KEEPME"])
	}
	// Per-user path dirs each overridden to the stick, none duplicated.
	for _, tc := range []struct{ name, want string }{
		{"HOME", l.Home},
		{"USERPROFILE", l.Home},
		{"APPDATA", filepath.Join(l.Home, "AppData", "Roaming")},
		{"LOCALAPPDATA", filepath.Join(l.Home, "AppData", "Local")},
	} {
		if m[tc.name] != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, m[tc.name], tc.want)
		}
		if c := countName(env, tc.name); c != 1 {
			t.Errorf("%s appears %d times, want 1", tc.name, c)
		}
	}
	// Config dir pinned.
	if m["CLAUDE_CONFIG_DIR"] != l.ClaudeCfg {
		t.Errorf("CLAUDE_CONFIG_DIR = %q", m["CLAUDE_CONFIG_DIR"])
	}
	// PATH prepended with Bin, original retained, single entry.
	if !strings.HasPrefix(m["PATH"], l.Bin+string(os.PathListSeparator)) {
		t.Errorf("PATH not prepended with Bin: %q", m["PATH"])
	}
	if !strings.Contains(m["PATH"], "C:\\Windows") {
		t.Errorf("PATH dropped original entries: %q", m["PATH"])
	}
	if c := countName(env, "PATH"); c != 1 {
		t.Errorf("PATH appears %d times, want 1", c)
	}
	// No proxy when UseProxy false.
	if _, ok := m["HTTPS_PROXY"]; ok {
		t.Errorf("HTTPS_PROXY set with UseProxy=false")
	}
	if _, ok := m["HTTP_PROXY"]; ok {
		t.Errorf("HTTP_PROXY set with UseProxy=false")
	}
}

func TestBuildEnv_ProxyWhenEnabled(t *testing.T) {
	l := testLayout()
	env := buildEnv([]string{"HTTPS_PROXY=http://host"}, LaunchOpts{
		Layout: l, UseProxy: true, ProxyURL: "http://127.0.0.1:25345",
	})
	m := envMap(env)
	if m["HTTPS_PROXY"] != "http://127.0.0.1:25345" {
		t.Errorf("HTTPS_PROXY = %q", m["HTTPS_PROXY"])
	}
	if m["HTTP_PROXY"] != "http://127.0.0.1:25345" {
		t.Errorf("HTTP_PROXY = %q", m["HTTP_PROXY"])
	}
	// The host HTTPS_PROXY must have been scrubbed, not duplicated.
	if c := countName(env, "HTTPS_PROXY"); c != 1 {
		t.Errorf("HTTPS_PROXY appears %d times, want 1", c)
	}
}

func TestBuildEnv_NoPathInBase(t *testing.T) {
	l := testLayout()
	env := buildEnv(nil, LaunchOpts{Layout: l})
	if envMap(env)["PATH"] != l.Bin {
		t.Errorf("PATH = %q, want %q", envMap(env)["PATH"], l.Bin)
	}
}

func TestBuildCmd_WorkDir(t *testing.T) {
	l := testLayout()

	// WorkDir set → cmd.Dir is that dir.
	cmd := buildCmd("claude.exe", LaunchOpts{Layout: l, WorkDir: `D:\some\project`})
	if cmd.Dir != `D:\some\project` {
		t.Errorf("cmd.Dir = %q, want %q", cmd.Dir, `D:\some\project`)
	}
	if len(cmd.Env) == 0 {
		t.Error("cmd.Env not populated")
	}

	// WorkDir empty → cmd.Dir stays "" (inherit ccp's cwd).
	cmd = buildCmd("claude.exe", LaunchOpts{Layout: l})
	if cmd.Dir != "" {
		t.Errorf("cmd.Dir = %q, want empty", cmd.Dir)
	}
}

func TestEnsureLayout(t *testing.T) {
	root := t.TempDir()
	l := paths.Layout{
		Bin:  filepath.Join(root, "data", "bin"),
		Home: filepath.Join(root, "data", "home"),
	}
	if err := os.MkdirAll(l.Bin, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(l.Bin, "claude.exe")
	if err := os.WriteFile(src, []byte("DUMMYEXE"), 0o755); err != nil {
		t.Fatal(err)
	}

	orig := probeVersion
	probeVersion = func(string) (string, error) { return "2.1.190", nil }
	defer func() { probeVersion = orig }()

	got, err := EnsureLayout(l)
	if err != nil {
		t.Fatal(err)
	}
	wantBin := filepath.Join(l.Home, ".local", "bin", "claude.exe")
	wantVer := filepath.Join(l.Home, ".local", "share", "claude", "versions", "2.1.190", "claude.exe")
	// EnsureLayout must return the versioned binary (run inline, no re-exec/new
	// window), not the .local\bin launcher.
	if got != wantVer {
		t.Errorf("returned %q, want %q", got, wantVer)
	}
	for _, p := range []string{wantBin, wantVer} {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("dest missing %q: %v", p, err)
			continue
		}
		if string(b) != "DUMMYEXE" {
			t.Errorf("dest %q content = %q", p, b)
		}
	}
}

func TestEnsureLayout_MissingSrc(t *testing.T) {
	l := paths.Layout{Bin: filepath.Join(t.TempDir(), "nope"), Home: t.TempDir()}
	if _, err := EnsureLayout(l); err == nil {
		t.Error("expected error for missing claude.exe, got nil")
	}
}
