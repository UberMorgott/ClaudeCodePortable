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
		Root:      `C:\stick`,
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
	// PATH prepended with Root then Bin, original retained, single entry.
	sep := string(os.PathListSeparator)
	if !strings.HasPrefix(m["PATH"], l.Root+sep+l.Bin+sep) {
		t.Errorf("PATH not prepended with Root+Bin: %q", m["PATH"])
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
	want := l.Root + string(os.PathListSeparator) + l.Bin
	if envMap(env)["PATH"] != want {
		t.Errorf("PATH = %q, want %q", envMap(env)["PATH"], want)
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

// layoutIn builds a Layout under a fresh temp root (Bin and Home siblings).
func layoutIn(t *testing.T) paths.Layout {
	t.Helper()
	root := t.TempDir()
	return paths.Layout{
		Bin:  filepath.Join(root, "data", "bin"),
		Home: filepath.Join(root, "data", "home"),
	}
}

// writeVerExe creates versions\<ver>\claude.exe with the given bytes.
func writeVerExe(t *testing.T, l paths.Layout, ver string, data string) string {
	t.Helper()
	p := filepath.Join(l.Home, ".local", "share", "claude", "versions", ver, "claude.exe")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestEnsureLayout(t *testing.T) {
	l := layoutIn(t)
	wantVer := writeVerExe(t, l, "2.1.190", "DUMMYEXE")

	got, err := EnsureLayout(l)
	if err != nil {
		t.Fatal(err)
	}
	// Returns the single versioned binary (run inline, no re-exec/new window).
	if got != wantVer {
		t.Errorf("returned %q, want %q", got, wantVer)
	}
	// Text shim points at the versioned binary.
	shim, err := os.ReadFile(filepath.Join(l.Bin, "claude.cmd"))
	if err != nil {
		t.Fatalf("claude.cmd missing: %v", err)
	}
	if !strings.Contains(string(shim), "2.1.190") || !strings.Contains(string(shim), "claude.exe") {
		t.Errorf("shim content = %q", shim)
	}
	// No legacy duplicate locations remain.
	if _, err := os.Stat(filepath.Join(l.Home, ".local", "bin")); !os.IsNotExist(err) {
		t.Errorf(".local\\bin should not exist: err=%v", err)
	}
	if _, err := os.Stat(l.BinPath("claude.exe")); !os.IsNotExist(err) {
		t.Errorf("bin\\claude.exe should not exist: err=%v", err)
	}
}

func TestEnsureLayoutMigratesLegacyBin(t *testing.T) {
	l := layoutIn(t)
	if err := os.MkdirAll(l.Bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.BinPath("claude.exe"), []byte("DUMMYEXE"), 0o755); err != nil {
		t.Fatal(err)
	}

	orig := probeVersion
	probeVersion = func(string) (string, error) { return "2.1.190", nil }
	defer func() { probeVersion = orig }()

	got, err := EnsureLayout(l)
	if err != nil {
		t.Fatal(err)
	}
	wantVer := filepath.Join(l.Home, ".local", "share", "claude", "versions", "2.1.190", "claude.exe")
	if got != wantVer {
		t.Errorf("returned %q, want %q", got, wantVer)
	}
	if _, err := os.Stat(wantVer); err != nil {
		t.Errorf("versioned exe missing: %v", err)
	}
	if _, err := os.Stat(l.BinPath("claude.exe")); !os.IsNotExist(err) {
		t.Errorf("legacy bin\\claude.exe should be gone: err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(l.Bin, "claude.cmd")); err != nil {
		t.Errorf("claude.cmd missing: %v", err)
	}
}

func TestEnsureLayoutPrunesOldVersions(t *testing.T) {
	l := layoutIn(t)
	writeVerExe(t, l, "2.1.100", "OLD")
	wantVer := writeVerExe(t, l, "2.1.190", "NEW")

	got, err := EnsureLayout(l)
	if err != nil {
		t.Fatal(err)
	}
	if got != wantVer {
		t.Errorf("returned %q, want %q", got, wantVer)
	}
	oldDir := filepath.Join(l.Home, ".local", "share", "claude", "versions", "2.1.100")
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Errorf("old version dir should be pruned: err=%v", err)
	}
}

func TestEnsureLayout_MissingSrc(t *testing.T) {
	l := paths.Layout{Bin: filepath.Join(t.TempDir(), "nope"), Home: t.TempDir()}
	if _, err := EnsureLayout(l); err == nil {
		t.Error("expected error for missing claude.exe, got nil")
	}
}
