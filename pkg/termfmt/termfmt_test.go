package termfmt

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/robberwick/bindicatwo-api/pkg/nhdc"
)

func withEnv(t *testing.T, key, val string) {
	old := os.Getenv(key)
	_ = os.Setenv(key, val)
	t.Cleanup(func() { _ = os.Setenv(key, old) })
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

func TestUseColor_Variants(t *testing.T) {
	// Color when NO_COLOR empty and TERM != dumb
	withEnv(t, "NO_COLOR", "")
	withEnv(t, "TERM", "xterm-256color")
	if !useColor() {
		t.Fatalf("expected useColor true")
	}
	// Disabled when NO_COLOR set
	withEnv(t, "NO_COLOR", "1")
	withEnv(t, "TERM", "xterm-256color")
	if useColor() {
		t.Fatalf("expected useColor false with NO_COLOR set")
	}
	// Disabled when TERM=dumb
	withEnv(t, "NO_COLOR", "")
	withEnv(t, "TERM", "dumb")
	if useColor() {
		t.Fatalf("expected useColor false with TERM=dumb")
	}
}

func TestColorize_Bold_Dim_EnabledDisabled(t *testing.T) {
	// Enabled
	withEnv(t, "NO_COLOR", "")
	withEnv(t, "TERM", "xterm")
	if got := Colorize("txt", ansiBlue); !strings.Contains(got, ansiBlue) || !strings.Contains(got, ansiReset) {
		t.Fatalf("Colorize should wrap when enabled: %q", got)
	}
	if got := Bold("x"); !strings.Contains(got, ansiBold) || !strings.Contains(got, ansiReset) {
		t.Fatalf("Bold should wrap when enabled: %q", got)
	}
	if got := Dim("x"); !strings.Contains(got, ansiDim) || !strings.Contains(got, ansiReset) {
		t.Fatalf("Dim should wrap when enabled: %q", got)
	}
	// Disabled
	withEnv(t, "NO_COLOR", "1")
	withEnv(t, "TERM", "xterm")
	if got := Colorize("txt", ansiBlue); got != "txt" {
		t.Fatalf("Colorize disabled should return input: %q", got)
	}
	if got := Bold("b"); got != "b" {
		t.Fatalf("Bold disabled should return input")
	}
	if got := Dim("d"); got != "d" {
		t.Fatalf("Dim disabled should return input")
	}
}

func TestEmojiForSprite_AllCases(t *testing.T) {
	cases := map[string]string{
		"paper":     "📄",
		"recycling": "♻️",
		"refuse":    "🗑️",
		"garden":    "🌿",
		"food":      "🍽️",
		"":          "🗓️",
		"unknown":   "🗓️",
	}
	for in, want := range cases {
		if got := EmojiForSprite(in); got != want {
			t.Fatalf("EmojiForSprite(%q)=%q want %q", in, got, want)
		}
	}
}

func TestColorForSprite_AllCases(t *testing.T) {
	cases := map[string]string{
		"paper":     ansiBlue,
		"recycling": ansiGreen,
		"refuse":    ansiMagenta,
		"garden":    ansiYellow,
		"food":      ansiYellow,
		"":          "",
		"unknown":   "",
	}
	for in, want := range cases {
		if got := ColorForSprite(in); got != want {
			t.Fatalf("ColorForSprite(%q)=%q want %q", in, got, want)
		}
	}
}

func TestANSIConstantExports(t *testing.T) {
	if ANSIReset != ansiReset || ANSIBold != ansiBold || ANSIDim != ansiDim || ANSICyan != ansiCyan || ANSIBlue != ansiBlue || ANSIGreen != ansiGreen || ANSIMagenta != ansiMagenta || ANSIYellow != ansiYellow {
		t.Fatalf("ANSI exported constants must match internal values")
	}
}

func TestPrettyDate_Invalid(t *testing.T) {
	d, rel := PrettyDate("not-a-date")
	if d != "not-a-date" || rel != "" {
		t.Fatalf("PrettyDate invalid should echo input and empty rel: %q %q", d, rel)
	}
}

func TestPrintHuman_Empty(t *testing.T) {
	withEnv(t, "NO_COLOR", "1")
	out := captureStdout(t, func() { PrintHuman(nil) })
	if !strings.Contains(out, "No upcoming collections found.") {
		t.Fatalf("expected empty message, got: %s", out)
	}
}

func TestPrintHuman_GroupingSortingAndEmojis(t *testing.T) {
	withEnv(t, "NO_COLOR", "1") // disable color for stable output
	items := []nhdc.Item{
		{Type: "Paper & card", Sprite: "paper", Bin: "blue lid bin", Date: "2025-01-10"},
		{Type: "Mixed recycling", Sprite: "recycling", Bin: "black lid bin", Date: "2025-01-05"},
		{Type: "Food waste", Sprite: "food", Bin: "brown caddy", Date: "2025-01-05"},
		{Type: "Refuse", Sprite: "refuse", Bin: "purple lid bin", Date: "2025-01-10"},
	}
	out := captureStdout(t, func() { PrintHuman(items) })
	// Header
	if !strings.Contains(out, "Upcoming collections") {
		t.Fatalf("missing header: %s", out)
	}
	// Emojis present
	for _, em := range []string{"♻️", "🍽️", "📄", "🗑️"} {
		if !strings.Contains(out, em) {
			t.Fatalf("missing emoji %s in output: %s", em, out)
		}
	}
	// Extras include both bin and sprite when present
	if !strings.Contains(out, "(black lid bin, recycling)") {
		t.Fatalf("missing extras: %s", out)
	}
	if !strings.Contains(out, "(blue lid bin, paper)") {
		t.Fatalf("missing extras: %s", out)
	}
	// Sorting by date asc and by type asc within date: for 2025-01-05, Food waste < Mixed recycling? Alphabetical: "Food" < "Mixed" so Food first
	idxFood := strings.Index(out, "Food waste")
	idxMixed := strings.Index(out, "Mixed recycling")
	if idxFood == -1 || idxMixed == -1 || idxFood > idxMixed {
		t.Fatalf("items on same date should be sorted by type: %d vs %d; out=%s", idxFood, idxMixed, out)
	}
}
