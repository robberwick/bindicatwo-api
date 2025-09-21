package termfmt

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/robberwick/bindicatwo-api/pkg/nhdc"
)

const (
	ansiReset   = "\x1b[0m"
	ansiBold    = "\x1b[1m"
	ansiDim     = "\x1b[2m"
	ansiCyan    = "\x1b[36m"
	ansiBlue    = "\x1b[34m"
	ansiGreen   = "\x1b[32m"
	ansiMagenta = "\x1b[35m"
	ansiYellow  = "\x1b[33m"
)

// Exported aliases for tests in package main that expect ANSI* constants.
const (
	ANSIReset   = ansiReset
	ANSIBold    = ansiBold
	ANSIDim     = ansiDim
	ANSICyan    = ansiCyan
	ANSIBlue    = ansiBlue
	ANSIGreen   = ansiGreen
	ANSIMagenta = ansiMagenta
	ANSIYellow  = ansiYellow
)

func useColor() bool {
	return os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
}

func Colorize(s, color string) string {
	if !useColor() {
		return s
	}
	return color + s + ansiReset
}

func Dim(s string) string {
	if !useColor() {
		return s
	}
	return ansiDim + s + ansiReset
}

func Bold(s string) string {
	if !useColor() {
		return s
	}
	return ansiBold + s + ansiReset
}

func EmojiForSprite(sprite string) string {
	switch sprite {
	case "paper":
		return "📄"
	case "recycling":
		return "♻️"
	case "refuse":
		return "🗑️"
	case "garden":
		return "🌿"
	case "food":
		return "🍽️"
	default:
		return "🗓️"
	}
}

func ColorForSprite(sprite string) string {
	switch sprite {
	case "paper":
		return ansiBlue
	case "recycling":
		return ansiGreen
	case "refuse":
		return ansiMagenta
	case "garden":
		return ansiYellow
	case "food":
		return ansiYellow
	default:
		return ""
	}
}

func PrettyDate(d string) (string, string) { return nhdc.PrettyDate(d) }

func PrintHuman(items []nhdc.Item) {
	if len(items) == 0 {
		fmt.Println("No upcoming collections found.")
		return
	}
	byDate := map[string][]nhdc.Item{}
	dates := make([]string, 0)
	for _, it := range items {
		byDate[it.Date] = append(byDate[it.Date], it)
	}
	for d := range byDate {
		dates = append(dates, d)
	}
	sort.Strings(dates)

	header := Bold(Colorize("Upcoming collections", ansiCyan))
	fmt.Println(header)
	for _, d := range dates {
		pretty, rel := PrettyDate(d)
		sub := pretty
		if rel != "" {
			sub = fmt.Sprintf("%s %s", pretty, Dim("("+rel+")"))
		}
		fmt.Printf("  %s\n", sub)

		itemsForDate := append([]nhdc.Item(nil), byDate[d]...)
		sort.Slice(itemsForDate, func(i, j int) bool { return itemsForDate[i].Type < itemsForDate[j].Type })
		for _, it := range itemsForDate {
			em := EmojiForSprite(it.Sprite)
			clr := ColorForSprite(it.Sprite)
			tlabel := it.Type
			if clr != "" {
				tlabel = Colorize(tlabel, clr)
			}
			extraParts := []string{}
			if it.Bin != "" {
				extraParts = append(extraParts, it.Bin)
			}
			if it.Sprite != "" {
				extraParts = append(extraParts, it.Sprite)
			}
			extra := ""
			if len(extraParts) > 0 {
				extra = " " + Dim("("+strings.Join(extraParts, ", ")+")")
			}
			fmt.Printf("    • %s %s %s\n", em, tlabel, extra)
		}
	}
}
