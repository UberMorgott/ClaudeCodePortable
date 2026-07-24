// Package statusline renders a Claude Code statusline (line 1) from the JSON
// Claude Code pipes on stdin. Pure stdlib: no Node, no network, no git.
//
// It is the "Source A" happy path of the original Node morgott-statusline:
// when stdin carries rate_limits, every bar comes straight from stdin and
// nothing external is touched. Source-B (OAuth/HTTP usage API, cache, lock,
// history, trend/sparkline/ETA), line-2 segments, and the git segment are all
// out of scope here — see the `ponytail:` seams below.
package statusline

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

// Config is the MVP subset of claude-limitline.json behavior we honor. Unknown
// JSON keys are ignored (json.Unmarshal drops extras), so a richer real config
// loads without error.
type Config struct {
	Display      Display  `json:"display"`
	SegmentOrder []string `json:"segmentOrder"`
	Directory    Toggle   `json:"directory"`
	Model        Toggle   `json:"model"`
	Context      Bar      `json:"context"`
	Block        Block    `json:"block"`
	Weekly       Bar      `json:"weekly"`
}

// Display holds display-wide toggles.
type Display struct {
	UseNerdFonts bool   `json:"useNerdFonts"`
	CompactMode  string `json:"compactMode"` // "auto" | "always" | "never"
	CompactWidth int    `json:"compactWidth"`
}

// Toggle is a segment with only an on/off switch.
type Toggle struct {
	Enabled bool `json:"enabled"`
}

// Bar is a bar segment (context, weekly).
type Bar struct {
	Enabled  bool `json:"enabled"`
	BarWidth int  `json:"barWidth"`
}

// Block is the 5-hour block segment (bar + remaining-time suffix).
type Block struct {
	Enabled           bool `json:"enabled"`
	BarWidth          int  `json:"barWidth"`
	ShowTimeRemaining bool `json:"showTimeRemaining"`
}

// DefaultConfig returns the built-in defaults (matching the original's
// DEFAULT_CONFIG / config-example.json for the fields we support).
func DefaultConfig() Config {
	return Config{
		Display: Display{
			UseNerdFonts: true,
			CompactMode:  "auto",
			CompactWidth: 100,
		},
		SegmentOrder: []string{"directory", "model", "context", "block", "weekly"},
		Directory:    Toggle{Enabled: false}, // off by default: portable stick's cwd is the stick itself; re-enable via .claude-limitline.json
		Model:        Toggle{Enabled: true},
		Context:      Bar{Enabled: true, BarWidth: 10},
		Block:        Block{Enabled: true, BarWidth: 10, ShowTimeRemaining: true},
		Weekly:       Bar{Enabled: true, BarWidth: 10},
	}
}

// LoadConfig reads <cwd>/.claude-limitline.json then
// <home>/.claude/claude-limitline.json (first that exists), unmarshaled over
// DefaultConfig. json.Unmarshal only assigns keys present in the file, so this
// is exactly the original's one-level deep-merge over defaults. A missing or
// invalid file yields DefaultConfig — a broken config must never break the
// statusline, so no error is surfaced.
func LoadConfig(cwd, home string) Config {
	cfg := DefaultConfig()
	for _, path := range []string{
		filepath.Join(cwd, ".claude-limitline.json"),
		filepath.Join(home, ".claude", "claude-limitline.json"),
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue // absent/unreadable — try next
		}
		merged := cfg
		if json.Unmarshal(data, &merged) == nil {
			return merged
		}
		// invalid JSON: fall through to defaults rather than a partial merge
		return cfg
	}
	return cfg
}

// ---------------------------------------------------------------------------
// stdin contract
// ---------------------------------------------------------------------------

type input struct {
	Workspace struct {
		ProjectDir string `json:"project_dir"`
		CurrentDir string `json:"current_dir"`
	} `json:"workspace"`
	Cwd   string `json:"cwd"`
	Model struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"model"`
	ContextWindow struct {
		// pointer so absent/null omits the segment (never renders 0%)
		UsedPercentage    *float64 `json:"used_percentage"`
		ContextWindowSize int      `json:"context_window_size"`
	} `json:"context_window"`
	RateLimits struct {
		FiveHour *rateLimit `json:"five_hour"`
		SevenDay *rateLimit `json:"seven_day"`
	} `json:"rate_limits"`
}

type rateLimit struct {
	UsedPercentage float64 `json:"used_percentage"`
	ResetsAt       int64   `json:"resets_at"` // epoch SECONDS (stdin), not ISO
}

// ---------------------------------------------------------------------------
// Render
// ---------------------------------------------------------------------------

// Render parses the stdin JSON and returns the line-1 statusline fitted to cols
// columns (no trailing newline). cols<=0 uses a sane default width.
func Render(stdinJSON []byte, cols int, cfg Config) string {
	return renderAt(stdinJSON, cols, cfg, time.Now())
}

// renderAt is Render with an injected "now" so time-dependent output is
// deterministic under test. Render is the only caller that reads the wall clock.
func renderAt(stdinJSON []byte, cols int, cfg Config, now time.Time) string {
	var in input
	_ = json.Unmarshal(stdinJSON, &in) // malformed stdin -> zero value, degrade gracefully

	if cols <= 0 {
		cols = 120
	}

	dir := dirName(firstNonEmpty(in.Workspace.ProjectDir, in.Workspace.CurrentDir, in.Cwd))

	// Fit: try the largest bar width that fits, with the time suffix; if even
	// the minimum bar overflows, drop the time suffix; then truncate the
	// directory from the left. MVP guarantee: never exceed cols, never panic.
	budget := cols - 1
	if budget < 1 {
		budget = 1
	}

	maxBar := 3
	for _, w := range []int{cfg.Context.BarWidth, cfg.Block.BarWidth, cfg.Weekly.BarWidth} {
		if w > maxBar {
			maxBar = w
		}
	}
	if maxBar > 60 {
		maxBar = 60
	}

	forceCompact := cfg.Display.CompactMode == "always" ||
		(cfg.Display.CompactMode == "auto" && cols < cfg.Display.CompactWidth)
	neverCompact := cfg.Display.CompactMode == "never"
	baseTime := cfg.Block.ShowTimeRemaining && (neverCompact || !forceCompact)

	build := func(barW int, showTime bool, dirBudget int) string {
		return assemble(in, cfg, now, dir, barW, showTime, dirBudget)
	}

	// pass 1: shrink the bar, keep time (unless forced compact)
	for w := maxBar; w >= 3; w-- {
		s := build(w, baseTime, 0)
		if visibleWidth(s) <= budget {
			return s
		}
	}
	// pass 2: drop the time suffix (unless "never" compact)
	if !neverCompact {
		for w := maxBar; w >= 1; w-- {
			s := build(w, false, 0)
			if visibleWidth(s) <= budget {
				return s
			}
		}
	}
	// pass 3: truncate directory from the left until it fits
	for dirBudget := len([]rune(dir)); dirBudget >= 1; dirBudget-- {
		s := build(1, false, dirBudget)
		if visibleWidth(s) <= budget {
			return s
		}
	}
	// pass 4: still over on a very narrow terminal — drop trailing segments
	// (weekly, then block, ...) until it fits, keeping at least the directory.
	segs := buildSegments(in, cfg, now, dir, 1, false, 1)
	for len(segs) > 1 {
		segs = segs[:len(segs)-1]
		s := joinSegments(segs, cfg.Display.UseNerdFonts)
		if visibleWidth(s) <= budget {
			return s
		}
	}
	// last resort: the single first segment, hard-truncated to the budget.
	if len(segs) == 1 {
		return truncateToWidth(segs[0], budget)
	}
	return ""
}

// buildSegments returns the rendered, enabled segments in order.
func buildSegments(in input, cfg Config, now time.Time, dir string, barW int, showTime bool, dirBudget int) []string {
	var segs []string
	for _, name := range cfg.SegmentOrder {
		if s, ok := segment(name, in, cfg, now, dir, barW, showTime, dirBudget); ok {
			segs = append(segs, s)
		}
	}
	return segs
}

// assemble builds the joined statusline for one candidate sizing.
func assemble(in input, cfg Config, now time.Time, dir string, barW int, showTime bool, dirBudget int) string {
	return joinSegments(buildSegments(in, cfg, now, dir, barW, showTime, dirBudget), cfg.Display.UseNerdFonts)
}

// segment renders a single named segment. ok=false means "omit" (disabled or no
// data). ponytail: git segment is intentionally not implemented (would shell out
// to git); add a "git" case + exec here if the wired binary ever needs it.
func segment(name string, in input, cfg Config, now time.Time, dir string, barW int, showTime bool, dirBudget int) (string, bool) {
	switch name {
	case "directory":
		if !cfg.Directory.Enabled || dir == "" {
			return "", false
		}
		d := dir
		if dirBudget > 0 {
			d = truncateLeft(d, dirBudget)
		}
		return pad(colorize(d, accentDir, cfg.Display.UseNerdFonts)), true

	case "model":
		if !cfg.Model.Enabled {
			return "", false
		}
		name := formatModelName(in.Model.ID, in.Model.DisplayName)
		if name == "" {
			return "", false
		}
		return pad(colorize("✱ "+name, accentModel, cfg.Display.UseNerdFonts)), true

	case "context":
		if !cfg.Context.Enabled || in.ContextWindow.UsedPercentage == nil {
			return "", false
		}
		return pad("🧠 " + barWithPct(*in.ContextWindow.UsedPercentage, barW)), true

	case "block":
		if !cfg.Block.Enabled || in.RateLimits.FiveHour == nil {
			return "", false
		}
		fh := in.RateLimits.FiveHour
		s := "⏱️ " + barWithPct(fh.UsedPercentage, barW)
		if showTime && fh.ResetsAt > 0 {
			remaining := time.Unix(fh.ResetsAt, 0).Sub(now)
			s += " (" + formatTimeRemaining(remaining) + ")"
		}
		return pad(s), true

	case "weekly":
		if !cfg.Weekly.Enabled || in.RateLimits.SevenDay == nil {
			return "", false
		}
		return pad("📅 " + barWithPct(in.RateLimits.SevenDay.UsedPercentage, barW)), true
	}
	return "", false
}

// ---------------------------------------------------------------------------
// rendering primitives
// ---------------------------------------------------------------------------

const reset = "\x1b[0m"

// dark-palette accent foregrounds (ponytail: only the default dark theme is
// ported; other themes are out of scope).
var (
	accentDir   = rgb{130, 180, 255}
	accentModel = rgb{200, 160, 255}
	greyEmpty   = rgb{110, 110, 110}
	wedgeColor  = rgb{90, 90, 90}
)

type rgb struct{ r, g, b int }

func fg(c rgb) string { return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", c.r, c.g, c.b) }

func colorize(s string, c rgb, _ bool) string { return fg(c) + s + reset }

func pad(s string) string { return " " + s + " " }

// joinSegments joins colored segment strings. With nerd fonts, a powerline
// wedge (U+E0B0) separates them; otherwise a plain ">".
// ponytail: powerline background fills (the classic filled "islands" where the
// wedge inherits adjacent backgrounds) are not reproduced — segments are
// foreground-colored only. Upgrade to per-segment bg + neighbor-aware wedge
// coloring if the wired statusline needs the solid-block look.
func joinSegments(segs []string, nerd bool) string {
	sep := " > "
	if nerd {
		sep = fg(wedgeColor) + "" + reset
	}
	return strings.Join(segs, sep)
}

// barWithPct renders "<bar> N%": filled cells in a green->red gradient, empty
// cells grey, then the rounded percent.
func barWithPct(pct float64, width int) string {
	if width < 1 {
		width = 1
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := int(math.Round(pct / 100 * float64(width)))
	if filled > width {
		filled = width
	}
	var b strings.Builder
	b.WriteString(fg(barColor(pct)))
	b.WriteString(strings.Repeat("█", filled))
	b.WriteString(fg(greyEmpty))
	b.WriteString(strings.Repeat("░", width-filled))
	b.WriteString(reset)
	fmt.Fprintf(&b, " %d%%", int(math.Round(pct)))
	return b.String()
}

// barColor: r ramps 0->255 over 0-50%, g ramps 255->0 over 50-100% (green->red).
func barColor(pct float64) rgb {
	if pct <= 50 {
		return rgb{int(pct / 50 * 255), 255, 0}
	}
	return rgb{255, int((100 - pct) / 50 * 255), 0}
}

// formatModelName parses model.id ("claude-opus-4-8" -> "Opus 4.8",
// legacy "claude-3-5-sonnet" -> "Sonnet 3.5"). Falls back to display_name with
// the leading "Claude " and any "(...)" suffix stripped.
func formatModelName(id, display string) string {
	lid := strings.ToLower(id)
	family := ""
	for _, f := range []string{"opus", "sonnet", "haiku"} {
		if strings.Contains(lid, f) {
			family = f
			break
		}
	}
	if family == "" {
		return stripDisplay(display)
	}
	var nums []string
	for _, p := range strings.Split(lid, "-") {
		if p == "" || p == "claude" || p == family {
			continue
		}
		if isDigits(p) {
			nums = append(nums, p)
		}
	}
	name := map[string]string{"opus": "Opus", "sonnet": "Sonnet", "haiku": "Haiku"}[family]
	switch {
	case len(nums) >= 2:
		return name + " " + nums[0] + "." + nums[1]
	case len(nums) == 1:
		return name + " " + nums[0]
	default:
		return name
	}
}

func stripDisplay(d string) string {
	d = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(d), "Claude "))
	if i := strings.IndexByte(d, '('); i >= 0 {
		d = strings.TrimSpace(d[:i])
	}
	return d
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// formatTimeRemaining renders a Russian short duration: "6д18ч", "3ч17м", "45м".
func formatTimeRemaining(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d.Minutes())
	days := total / 1440
	hours := (total % 1440) / 60
	mins := total % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dд%dч", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dч%dм", hours, mins)
	default:
		return fmt.Sprintf("%dм", mins)
	}
}

// dirName returns the last path element, handling both / and \ separators.
func dirName(p string) string {
	p = strings.TrimRight(p, `/\`)
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

func truncateLeft(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return "…"
	}
	return "…" + string(r[len(r)-(max-1):])
}

// truncateToWidth cuts s (dropping color) to at most max visible columns. Last
// resort for a nonsensically narrow terminal; keeping it colorless keeps it simple.
func truncateToWidth(s string, max int) string {
	if max < 0 {
		max = 0
	}
	runes := []rune(stripANSI(s))
	w, out := 0, make([]rune, 0, len(runes))
	for i := 0; i < len(runes); i++ {
		rw := runeWidth(runes[i])
		if i+1 < len(runes) && runes[i+1] == 0xFE0F {
			rw = 2
		}
		if w+rw > max {
			break
		}
		out = append(out, runes[i])
		if i+1 < len(runes) && runes[i+1] == 0xFE0F {
			out = append(out, runes[i+1])
			i++
		}
		w += rw
	}
	return string(out)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// visibleWidth is the terminal column width of s, ignoring ANSI escapes and
// counting wide/emoji runes as 2. A rune followed by VS16 (U+FE0F) counts as 2
// (emoji presentation) and the selector as 0.
//
// ponytail: this is a compact width heuristic, NOT the JS custom wide-char
// table — minor wrapping drift vs the original is accepted (brief allows it).
// Upgrade to golang.org/x/text/width or an exact table only if visible
// misalignment is reported.
func visibleWidth(s string) int {
	stripped := stripANSI(s)
	runes := []rune(stripped)
	w := 0
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if i+1 < len(runes) && runes[i+1] == 0xFE0F {
			w += 2 // emoji presentation
			i++    // consume VS16
			continue
		}
		w += runeWidth(r)
	}
	return w
}

func runeWidth(r rune) int {
	switch {
	case r == 0xFE0F: // stray variation selector
		return 0
	case r >= 0xE000 && r <= 0xF8FF: // private use (powerline glyphs) -> width 1
		return 1
	case r >= 0x1100 && r <= 0x115F, // Hangul Jamo
		r >= 0x2E80 && r <= 0x303E, // CJK radicals/symbols
		r >= 0x3041 && r <= 0x33FF, // Hiragana..CJK compat
		r >= 0x3400 && r <= 0x4DBF, // CJK ext A
		r >= 0x4E00 && r <= 0x9FFF, // CJK unified
		r >= 0xA000 && r <= 0xA4CF, // Yi
		r >= 0xAC00 && r <= 0xD7A3, // Hangul syllables
		r >= 0xF900 && r <= 0xFAFF, // CJK compat ideographs
		r >= 0xFE30 && r <= 0xFE4F, // CJK compat forms
		r >= 0xFF00 && r <= 0xFF60, // fullwidth forms
		r >= 0xFFE0 && r <= 0xFFE6,
		r >= 0x1F300 && r <= 0x1FAFF, // emoji & symbols (🧠 📅 …)
		r >= 0x20000 && r <= 0x3FFFD: // CJK ext B+
		return 2
	default:
		return 1
	}
}

// stripANSI removes CSI escape sequences (\x1b[ ... letter).
func stripANSI(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] == 0x1b && i+1 < len(runes) && runes[i+1] == '[' {
			i += 2
			for i < len(runes) && !(runes[i] >= '@' && runes[i] <= '~') {
				i++
			}
			continue // skip the final byte too
		}
		b.WriteRune(runes[i])
	}
	return b.String()
}
