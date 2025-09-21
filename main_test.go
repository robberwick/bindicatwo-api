package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

func datePlus(days int) string {
	now := time.Now()
	d := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	d = d.AddDate(0, 0, days)
	return d.Format("2006-01-02")
}

func TestBetweenExtractsValueAndReturnsNextIndex(t *testing.T) {
	val, next := between("xx start [value] end", "[", "]", 0)
	if val != "value" {
		t.Fatalf("expected 'value', got %q", val)
	}
	if next <= 0 {
		t.Fatalf("expected next index > 0, got %d", next)
	}
}

func TestBetweenRespectsStartOffset(t *testing.T) {
	s := "<a>first</a><a>second</a>"
	// First match
	v1, n1 := between(s, "<a>", "</a>", 0)
	if v1 != "first" || n1 <= 0 {
		t.Fatalf("unexpected first: %q, %d", v1, n1)
	}
	// Start after first closing tag to get second
	v2, _ := between(s, "<a>", "</a>", n1)
	if v2 != "second" {
		t.Fatalf("expected 'second', got %q", v2)
	}
}

func TestBetweenReturnsMinusOneWhenMarkersMissing(t *testing.T) {
	if v, n := between("hello", "[", "]", 0); v != "" || n != -1 {
		t.Fatalf("expected empty and -1, got %q and %d", v, n)
	}
}

func TestIsDigitsReturnsTrueOnlyForNonEmptyNumeric(t *testing.T) {
	cases := map[string]bool{
		"":     false,
		"0":    true,
		"123":  true,
		"12a3": false,
		" 123": false,
		"123 ": false,
		"12.3": false,
	}
	for s, exp := range cases {
		if got := isDigits(s); got != exp {
			t.Fatalf("isDigits(%q) = %v, want %v", s, got, exp)
		}
	}
}

func TestStripTagsRemovesHTMLTagsAndTrims(t *testing.T) {
	in := "  <div>Hello <b>World</b> &amp; <i>Friends</i></div>  "
	got := stripTags(in)
	if got != "Hello World &amp; Friends" {
		t.Fatalf("stripTags got %q", got)
	}
}

func TestUnescapeEntitiesReplacesNbspAndAmp(t *testing.T) {
	in := "A&nbsp;&amp;&nbsp;B"
	got := unescapeEntities(in)
	if got != "A & B" {
		t.Fatalf("unescapeEntities got %q", got)
	}
}

func TestUnsuffixRemovesOrdinalSuffixesCaseInsensitive(t *testing.T) {
	cases := map[string]string{
		"1st":  "1",
		"2ND":  "2",
		"3rd":  "3",
		"4Th":  "4",
		"11":   "11",
		"21st": "21",
	}
	for in, exp := range cases {
		if got := unsuffix(in); got != exp {
			t.Fatalf("unsuffix(%q) = %q, want %q", in, got, exp)
		}
	}
}

func TestParseNextCollectionsExtractsItemsAndCanonicalizesAndPicksEarliest(t *testing.T) {
	html := strings.Join([]string{
		`<div>`,
		`<strong>Mixed recycling</strong>`,
		`Next collection<br>Tuesday 16th September 2025`,
		`</div>`,
		`<div>`,
		`<strong>Cardboard &amp; paper</strong>`,
		`Next collection<br>Friday 12th September 2025`,
		`</div>`,
		`<div>`,
		`<strong>Paper &amp; card</strong>`,
		`Next collection<br>Wednesday 10th September 2025`,
		`</div>`,
	}, "")

	items := parseNextCollections(html)
	if len(items) == 0 {
		t.Fatalf("expected some items")
	}

	// Build a map for easier assertions
	m := map[string]Item{}
	for _, it := range items {
		m[it.Type] = it
	}

	// Canonicalization: both paper variants should result in one canonical type with earliest date
	it, ok := m["Paper & card"]
	if !ok {
		t.Fatalf("expected Paper & card entry")
	}
	if it.Date != "2025-09-10" {
		t.Fatalf("expected earliest paper date 2025-09-10, got %s", it.Date)
	}
	if it.Sprite != "paper" {
		t.Fatalf("expected sprite 'paper', got %q", it.Sprite)
	}

	// Mixed recycling present
	it2, ok := m["Mixed recycling"]
	if !ok || it2.Date != "2025-09-16" {
		t.Fatalf("expected Mixed recycling on 2025-09-16, got %+v", it2)
	}
}

func TestParseNextCollectionsHandlesNbspMarker(t *testing.T) {
	html := `<div><strong>Refuse</strong>Next\u00a0collection<br>Monday 1st December 2025</div>`
	// Replace escape with actual NBSP to mimic byte content
	html = strings.ReplaceAll(html, "\\u00a0", "\u00a0")
	items := parseNextCollections(html)
	if len(items) != 1 || items[0].Type != "Refuse" || items[0].Date != "2025-12-01" {
		t.Fatalf("unexpected parse result: %+v", items)
	}
}

func TestParseNextCollectionsReturnsNilOnEmpty(t *testing.T) {
	if items := parseNextCollections(""); items != nil {
		t.Fatalf("expected nil for empty html, got %+v", items)
	}
}

func TestExtractAjaxURLHandlesVarAndFallback(t *testing.T) {
	varCase := "<script>var AJAX_URL = '/w/ajax?webpage_subpage_id=1&webpage_token=abc&auth=def&id=42'</script>"
	if u := extractAjaxURL(varCase); u != "/w/ajax?webpage_subpage_id=1&webpage_token=abc&auth=def&id=42" {
		t.Fatalf("unexpected ajax url: %q", u)
	}
	fallback := "... webpage_subpage_id=123&webpage_token=t0k&auth=au&id=77' ..."
	if u := extractAjaxURL(fallback); u != "/w/ajax?webpage_subpage_id=123&webpage_token=t0k&auth=au&id=77" {
		t.Fatalf("unexpected fallback url: %q", u)
	}
}

func TestDiscoverFromSubmitParsesJSONRedirectAndInlinePath(t *testing.T) {
	jsonResp := `{"redirect_url":"\/w\/webpage\/find-bin-collection-day-show-details?webpage_token=t&auth=a&id=99"}`
	tok, auth, id, err := discoverFromSubmit(jsonResp)
	if err != nil || tok != "t" || auth != "a" || id != "99" {
		t.Fatalf("unexpected redirect parse: %q %q %q %v", tok, auth, id, err)
	}
	inline := "...w/webpage/find-bin-collection-day-show-details?webpage_token=x&auth=y&id=5\"..."
	tok, auth, id, err = discoverFromSubmit(inline)
	if err != nil || tok != "x" || auth != "y" || id != "5" {
		t.Fatalf("unexpected inline parse: %q %q %q %v", tok, auth, id, err)
	}
}

func TestComputeRelativeFieldsSetsDaysUntilAndNextForSoonestFutureOrToday(t *testing.T) {
	items := []Item{
		{Type: "Refuse", Date: datePlus(2)},
		{Type: "Paper & card", Date: datePlus(0)},
		{Type: "Mixed recycling", Date: datePlus(5)},
	}
	computeRelativeFields(items)
	// The 'today' item should be Next, days_until 0
	var nextCount int
	for _, it := range items {
		if it.Date == datePlus(0) {
			if it.DaysUntil != 0 || !it.Next {
				t.Fatalf("today item not marked as next correctly: %+v", it)
			}
		}
		if it.Next {
			nextCount++
		}
	}
	if nextCount != 1 {
		t.Fatalf("expected exactly one Next item, got %d", nextCount)
	}
}

func TestComputeRelativeFieldsMarksAllItemsOnSoonestSameDate(t *testing.T) {
	same := datePlus(3)
	items := []Item{{Type: "A", Date: same}, {Type: "B", Date: same}, {Type: "C", Date: datePlus(5)}}
	computeRelativeFields(items)
	var nexts int
	for _, it := range items {
		if it.Date == same && it.Next {
			nexts++
		}
	}
	if nexts != 2 {
		t.Fatalf("expected both items on %s to be Next, got %d", same, nexts)
	}
}

func TestPrettyDateFormatsAndRelativeLabels(t *testing.T) {
	// Today
	d := datePlus(0)
	disp, rel := prettyDate(d)
	if disp != time.Now().Format("Mon 02 Jan 2006") || rel != "today" {
		t.Fatalf("unexpected prettyDate for today: %q, %q", disp, rel)
	}
	// Tomorrow
	d = datePlus(1)
	_, rel = prettyDate(d)
	if rel != "tomorrow" {
		t.Fatalf("unexpected rel for tomorrow: %q", rel)
	}
	// Future >1
	d = datePlus(4)
	_, rel = prettyDate(d)
	if rel != "in 4 days" {
		t.Fatalf("unexpected rel for +4: %q", rel)
	}
	// Past
	d = datePlus(-2)
	_, rel = prettyDate(d)
	if rel != "2 days ago" {
		t.Fatalf("unexpected rel for -2: %q", rel)
	}
}

func TestEmojiAndColorForSpriteMappings(t *testing.T) {
	if emojiForSprite("paper") == "" || colorForSprite("paper") == "" {
		t.Fatalf("expected mappings for 'paper'")
	}
	if emojiForSprite("unknown") == "" {
		t.Fatalf("expected default emoji for unknown sprite")
	}
	if colorForSprite("unknown") != "" {
		t.Fatalf("expected no color for unknown sprite")
	}
}

func TestColorizeRespectsNoColorEnv(t *testing.T) {
	old := os.Getenv("NO_COLOR")
	t.Cleanup(func() { _ = os.Setenv("NO_COLOR", old) })
	_ = os.Setenv("NO_COLOR", "1")
	out := colorize("hello", ansiGreen)
	if out != "hello" {
		t.Fatalf("expected no color when NO_COLOR is set, got %q", out)
	}
}
