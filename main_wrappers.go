package main

import (
	"github.com/robberwick/bindicatwo-api/pkg/nhdc"
	"github.com/robberwick/bindicatwo-api/pkg/termfmt"
)

// Keep Item type name available in package main for tests.
type Item = nhdc.Item

// The following thin wrappers preserve backward compatibility with tests that
// referenced helper functions in package main. They delegate to modular
// packages (nhdc, termfmt).

func between(s, a, b string, start int) (string, int)             { return nhdc.Between(s, a, b, start) }
func isDigits(s string) bool                                      { return nhdc.IsDigits(s) }
func stripTags(s string) string                                   { return nhdc.StripTags(s) }
func stripTagsSmall(s string) string                              { return nhdc.StripTagsSmall(s) }
func unescapeEntities(s string) string                            { return nhdc.UnescapeEntities(s) }
func unsuffix(day string) string                                  { return nhdc.Unsuffix(day) }
func parseNextCollections(html string) []Item                     { return nhdc.ParseNextCollections(html) }
func extractAjaxURL(html string) string                           { return nhdc.ExtractAjaxURL(html) }
func discoverFromSubmit(s string) (string, string, string, error) { return nhdc.DiscoverFromSubmit(s) }
func computeRelativeFields(items []Item)                          { nhdc.ComputeRelativeFields(items) }
func prettyDate(d string) (string, string)                        { return nhdc.PrettyDate(d) }

// Pretty output wrappers
const (
	ansiReset   = termfmt.ANSIReset
	ansiBold    = termfmt.ANSIBold
	ansiDim     = termfmt.ANSIDim
	ansiCyan    = termfmt.ANSICyan
	ansiBlue    = termfmt.ANSIBlue
	ansiGreen   = termfmt.ANSIGreen
	ansiMagenta = termfmt.ANSIMagenta
	ansiYellow  = termfmt.ANSIYellow
)

func colorize(s, color string) string { return termfmt.Colorize(s, color) }
func dim(s string) string             { return termfmt.Dim(s) }
func bold(s string) string            { return termfmt.Bold(s) }
func emojiForSprite(s string) string  { return termfmt.EmojiForSprite(s) }
func colorForSprite(s string) string  { return termfmt.ColorForSprite(s) }
func printHuman(items []Item)         { termfmt.PrintHuman(items) }
