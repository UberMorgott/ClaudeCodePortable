package statusline

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fixedNow is the reference "now" all time-dependent tests inject.
var fixedNow = time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)

// proStdin is a realistic hook payload for a Pro/Max subscriber after the first
// API response: model id, workspace, context %, and both rate-limit blocks.
// five_hour resets 3h17m after fixedNow (epoch seconds).
func proStdin(t *testing.T) []byte {
	t.Helper()
	reset := strconv.FormatInt(fixedNow.Add(3*time.Hour+17*time.Minute).Unix(), 10)
	return []byte(`{
		"model": {"id": "claude-opus-4-8", "display_name": "Claude Opus 4.8 (preview)"},
		"workspace": {"project_dir": "D:\\MorgDEV\\ClaudeCodePortable", "current_dir": "D:\\MorgDEV\\ClaudeCodePortable\\internal"},
		"cwd": "D:\\MorgDEV\\ClaudeCodePortable",
		"context_window": {"used_percentage": 42, "context_window_size": 200000},
		"rate_limits": {
			"five_hour": {"used_percentage": 63, "resets_at": ` + reset + `},
			"seven_day": {"used_percentage": 18}
		}
	}`)
}

func TestRenderProSubscriber(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Directory.Enabled = true // directory is off by default; enable to verify it renders
	out := renderAt(proStdin(t), 200, cfg, fixedNow)

	for _, want := range []string{
		"ClaudeCodePortable", // directory name
		"Opus 4.8",           // parsed model
		"42%",                // context
		"63%",                // five-hour block
		"18%",                // weekly
		"3ч17м",              // russian remaining
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}
	if !strings.ContainsAny(out, "█░") {
		t.Errorf("output missing bar glyphs\n---\n%s", out)
	}
	if strings.Contains(out, "undefined") || strings.Contains(out, "NaN") {
		t.Errorf("output leaked undefined/NaN\n---\n%s", out)
	}
}

func TestDirectoryHiddenByDefault(t *testing.T) {
	out := renderAt(proStdin(t), 200, DefaultConfig(), fixedNow)
	if strings.Contains(out, "ClaudeCodePortable") {
		t.Errorf("directory should be hidden by default\n%s", out)
	}
	// only the directory is gone — other segments still render
	if !strings.Contains(out, "Opus 4.8") {
		t.Errorf("non-directory segment missing\n%s", out)
	}
}

func TestRenderMissingRateLimits(t *testing.T) {
	in := []byte(`{
		"model": {"id": "claude-3-5-sonnet"},
		"workspace": {"project_dir": "/home/u/proj"},
		"context_window": {"used_percentage": 30}
	}`)
	cfg := DefaultConfig()
	cfg.Directory.Enabled = true // directory is off by default; enable to verify it renders
	out := renderAt(in, 200, cfg, fixedNow)

	if !strings.Contains(out, "proj") {
		t.Errorf("missing dir\n%s", out)
	}
	if !strings.Contains(out, "Sonnet 3.5") {
		t.Errorf("legacy model ordering wrong\n%s", out)
	}
	if !strings.Contains(out, "30%") {
		t.Errorf("missing context\n%s", out)
	}
	// block + weekly must be absent, no crash. Their icons:
	if strings.Contains(out, "⏱") || strings.Contains(out, "📅") {
		t.Errorf("block/weekly rendered without rate_limits\n%s", out)
	}
}

func TestRenderNullContext(t *testing.T) {
	in := []byte(`{
		"model": {"id": "claude-opus-4-8"},
		"workspace": {"project_dir": "/x/y"},
		"context_window": {"used_percentage": null}
	}`)
	out := renderAt(in, 200, DefaultConfig(), fixedNow)
	if strings.Contains(out, "🧠") {
		t.Errorf("context segment rendered for null used_percentage\n%s", out)
	}
}

func TestFormatTimeRemaining(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{3*time.Hour + 17*time.Minute, "3ч17м"},
		{6*24*time.Hour + 18*time.Hour, "6д18ч"},
		{45 * time.Minute, "45м"},
		{-5 * time.Minute, "0м"},
	}
	for _, c := range cases {
		if got := formatTimeRemaining(c.d); got != c.want {
			t.Errorf("formatTimeRemaining(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestFormatModelName(t *testing.T) {
	cases := []struct{ id, disp, want string }{
		{"claude-opus-4-8", "", "Opus 4.8"},
		{"claude-3-5-sonnet", "", "Sonnet 3.5"},
		{"claude-sonnet-4-5", "", "Sonnet 4.5"},
		{"claude-3-haiku", "", "Haiku 3"},
		{"", "Claude Opus 4.8 (preview)", "Opus 4.8"},
		{"weird-model", "Claude Sonnet 4.5", "Sonnet 4.5"},
	}
	for _, c := range cases {
		if got := formatModelName(c.id, c.disp); got != c.want {
			t.Errorf("formatModelName(%q,%q) = %q, want %q", c.id, c.disp, got, c.want)
		}
	}
}

func TestWidthFit(t *testing.T) {
	for _, cols := range []int{40, 20, 10, 3} {
		out := renderAt(proStdin(t), cols, DefaultConfig(), fixedNow)
		if w := visibleWidth(out); w > cols {
			t.Errorf("cols=%d: visibleWidth=%d exceeds budget\n%s", cols, w, out)
		}
	}
}

func TestNerdFontsToggle(t *testing.T) {
	cfg := DefaultConfig()
	on := renderAt(proStdin(t), 200, cfg, fixedNow)
	if !strings.Contains(on, "\ue0b0") {
		t.Errorf("nerd-font mode should contain powerline wedge U+E0B0\n%s", on)
	}

	cfg.Display.UseNerdFonts = false
	off := renderAt(proStdin(t), 200, cfg, fixedNow)
	if strings.Contains(off, "\ue0b0") {
		t.Errorf("ASCII mode leaked U+E0B0\n%s", off)
	}
	if !strings.Contains(off, ">") {
		t.Errorf("ASCII mode missing fallback separator\n%s", off)
	}
}

func TestSegmentDisabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Weekly.Enabled = false
	out := renderAt(proStdin(t), 200, cfg, fixedNow)
	if strings.Contains(out, "📅") {
		t.Errorf("disabled weekly still rendered\n%s", out)
	}
}

func TestLoadConfig(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()

	// home config: nerd fonts off
	homeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(homeDir, "claude-limitline.json"), `{"display":{"useNerdFonts":false}}`)

	// cwd config wins: sets compactWidth, leaves nerd fonts default (true)
	writeFile(t, filepath.Join(cwd, ".claude-limitline.json"), `{"display":{"compactWidth":77}}`)

	got := LoadConfig(cwd, home)
	if got.Display.CompactWidth != 77 {
		t.Errorf("cwd config not applied: compactWidth=%d", got.Display.CompactWidth)
	}
	if got.Display.UseNerdFonts != true {
		t.Errorf("cwd config should win over home; useNerdFonts=%v want default true", got.Display.UseNerdFonts)
	}

	// missing files -> defaults
	empty := LoadConfig(t.TempDir(), t.TempDir())
	if empty.Display.UseNerdFonts != true || len(empty.SegmentOrder) == 0 {
		t.Errorf("missing files should yield DefaultConfig, got %+v", empty)
	}

	// invalid file -> defaults, no panic
	bad := t.TempDir()
	writeFile(t, filepath.Join(bad, ".claude-limitline.json"), `{not json`)
	if LoadConfig(bad, t.TempDir()).Display.CompactMode != "auto" {
		t.Errorf("invalid config should fall back to defaults")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
