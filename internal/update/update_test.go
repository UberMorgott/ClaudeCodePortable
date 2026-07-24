package update

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/UberMorgott/ClaudeCodePortable/internal/fetch"
	"github.com/UberMorgott/ClaudeCodePortable/internal/paths"
)

// fakeComp builds a Comp with fixed Current/Latest for wiring tests. A non-nil
// latestErr makes Latest fail (simulating a network error).
func fakeComp(name, current, latest string, latestErr error) Comp {
	return Comp{
		Name:    name,
		Current: func(paths.Layout) string { return current },
		Latest:  func(context.Context) (string, error) { return latest, latestErr },
		Install: func(context.Context, paths.Layout, string, fetch.Progress) error { return nil },
	}
}

func TestCheckAllWiring(t *testing.T) {
	comps := []Comp{
		fakeComp("has-update", "1.2.3", "1.2.4", nil),              // newer available
		fakeComp("up-to-date", "2.0.0", "2.0.0", nil),              // equal
		fakeComp("ahead", "3.5.0", "3.4.0", nil),                   // local newer than found
		fakeComp("net-fail", "1.0.0", "9.9.9", errors.New("boom")), // Latest errors
		fakeComp("fresh", "", "1.0.0", nil),                        // not installed
	}

	rows := checkAll(context.Background(), paths.Layout{}, comps)

	if len(rows) != len(comps) {
		t.Fatalf("got %d rows, want %d", len(rows), len(comps))
	}
	// Order must match input (index-keyed assembly, no drop/dupe).
	for i, c := range comps {
		if rows[i].Name != c.Name {
			t.Fatalf("row %d: name %q, want %q", i, rows[i].Name, c.Name)
		}
	}

	by := map[string]struct {
		found     string
		hasUpdate bool
	}{}
	for _, r := range rows {
		by[r.Name] = struct {
			found     string
			hasUpdate bool
		}{r.Found, r.HasUpdate}
	}

	cases := []struct {
		name      string
		found     string
		hasUpdate bool
	}{
		{"has-update", "1.2.4", true},
		{"up-to-date", "2.0.0", false},
		{"ahead", "3.4.0", false},
		{"net-fail", "", false}, // error => Found "", non-fatal, no update
		{"fresh", "1.0.0", true},
	}
	for _, c := range cases {
		got := by[c.name]
		if got.found != c.found || got.hasUpdate != c.hasUpdate {
			t.Errorf("%s: got Found=%q HasUpdate=%v, want Found=%q HasUpdate=%v",
				c.name, got.found, got.hasUpdate, c.found, c.hasUpdate)
		}
	}
}

// TestCheckAllNoDropUnderConcurrency runs many components to confirm the
// index-keyed goroutine assembly never drops or duplicates a result.
func TestCheckAllNoDropUnderConcurrency(t *testing.T) {
	const n = 200
	comps := make([]Comp, n)
	for i := range comps {
		comps[i] = fakeComp(string(rune('A'+i%26))+"-"+itoa(i), "1.0.0", "1.0.1", nil)
	}
	rows := checkAll(context.Background(), paths.Layout{}, comps)
	if len(rows) != n {
		t.Fatalf("got %d rows, want %d", len(rows), n)
	}
	names := make([]string, len(rows))
	for i, r := range rows {
		if !r.HasUpdate {
			t.Fatalf("row %d (%s): expected HasUpdate true", i, r.Name)
		}
		names[i] = r.Name
	}
	sort.Strings(names)
	for i := 1; i < len(names); i++ {
		if names[i] == names[i-1] {
			t.Fatalf("duplicate result name %q", names[i])
		}
	}
}

func TestComponentsShape(t *testing.T) {
	want := []string{"claude", "rtk", "wireproxy-awg"}
	got := Components()
	if len(got) != len(want) {
		t.Fatalf("Components() len %d, want %d", len(got), len(want))
	}
	for i, c := range got {
		if c.Name != want[i] {
			t.Errorf("Components()[%d].Name = %q, want %q", i, c.Name, want[i])
		}
		if c.Current == nil || c.Latest == nil || c.Install == nil {
			t.Errorf("%s: nil func field(s)", c.Name)
		}
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
