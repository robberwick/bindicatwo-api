package nhdc

import (
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	client := NewClient()
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
	if client.Timeout != 30*time.Second {
		t.Errorf("expected timeout 30s, got %v", client.Timeout)
	}
	if client.Jar == nil {
		t.Error("expected cookie jar to be set")
	}
}

func TestConvertMobileAPIToItems(t *testing.T) {
	apiResp := &MobileAPIResponse{
		WasteCollectionDates: WasteCollectionDates{
			UPRN: "100080795976",
			Container1CollectionDetails: &ContainerDetails{
				ContainerID:          1,
				ContainerDescription: "Mixed recycling",
				CollectionDate:       "2025-11-15T00:00:00",
			},
			Container2CollectionDetails: &ContainerDetails{
				ContainerID:          2,
				ContainerDescription: "Paper & card",
				CollectionDate:       "2025-11-20T00:00:00",
			},
			Container3CollectionDetails: &ContainerDetails{
				ContainerID:          3,
				ContainerDescription: "Food waste",
				CollectionDate:       "2025-11-12T00:00:00",
			},
			Container4CollectionDetails: nil,
			Container5CollectionDetails: &ContainerDetails{
				ContainerID:    5,
				CollectionDate: "",
			},
		},
	}

	items := convertMobileAPIToItems(apiResp)

	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}

	if items[0].Date != "2025-11-12" {
		t.Errorf("expected first item date 2025-11-12, got %s", items[0].Date)
	}
	if items[1].Date != "2025-11-15" {
		t.Errorf("expected second item date 2025-11-15, got %s", items[1].Date)
	}
	if items[2].Date != "2025-11-20" {
		t.Errorf("expected third item date 2025-11-20, got %s", items[2].Date)
	}

	if items[0].Type != "Food waste" {
		t.Errorf("expected Food waste, got %s", items[0].Type)
	}
	if items[0].Sprite != "food" {
		t.Errorf("expected sprite food, got %s", items[0].Sprite)
	}
	if items[0].Bin != "brown caddy" {
		t.Errorf("expected bin brown caddy, got %s", items[0].Bin)
	}

	if items[2].Type != "Paper & card" {
		t.Errorf("expected Paper & card, got %s", items[2].Type)
	}
	if items[2].Sprite != "paper" {
		t.Errorf("expected sprite paper, got %s", items[2].Sprite)
	}
	if items[2].Bin != "blue lid bin" {
		t.Errorf("expected bin blue lid bin, got %s", items[2].Bin)
	}
}

func TestConvertMobileAPIToItems_AllTypes(t *testing.T) {
	testCases := []struct {
		desc   string
		sprite string
		bin    string
	}{
		{"Food waste", "food", "brown caddy"},
		{"Food Caddy", "food", "brown caddy"},
		{"Garden waste", "garden", "brown lid bin"},
		{"Garden waste bin", "garden", "brown lid bin"},
		{"Mixed recycling", "recycling", "black lid bin"},
		{"Mixed recycling bin", "recycling", "black lid bin"},
		{"Cardboard & paper", "paper", "blue lid bin"},
		{"Cardboard & paper bin", "paper", "blue lid bin"},
		{"Non-recyclable refuse", "refuse", "purple lid bin"},
		{"Non-recyclable refuse bin", "refuse", "purple lid bin"},
		{"Paper & card", "paper", "blue lid bin"},
		{"Refuse", "refuse", "purple lid bin"},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			apiResp := &MobileAPIResponse{
				WasteCollectionDates: WasteCollectionDates{
					Container1CollectionDetails: &ContainerDetails{
						ContainerDescription: tc.desc,
						CollectionDate:       "2025-11-15T00:00:00",
					},
				},
			}
			items := convertMobileAPIToItems(apiResp)
			if len(items) != 1 {
				t.Fatalf("expected 1 item, got %d", len(items))
			}
			if items[0].Sprite != tc.sprite {
				t.Errorf("expected sprite %s, got %s", tc.sprite, items[0].Sprite)
			}
			if items[0].Bin != tc.bin {
				t.Errorf("expected bin %s, got %s", tc.bin, items[0].Bin)
			}
		})
	}
}

func TestGetSchedule_EmptyUPRN(t *testing.T) {
	client := NewClient()
	_, err := GetSchedule(client, "")
	if err == nil {
		t.Fatal("expected error for empty UPRN")
	}
	if err.Error() != "UPRN is required" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestGetSchedule_WhitespaceUPRN(t *testing.T) {
	client := NewClient()
	_, err := GetSchedule(client, "   \t\n  ")
	if err == nil {
		t.Fatal("expected error for whitespace-only UPRN")
	}
}

func TestComputeRelativeFields(t *testing.T) {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)

	dateStr := func(days int) string {
		d := today.AddDate(0, 0, days)
		return d.Format("2006-01-02")
	}

	items := []Item{
		{Type: "Past", Date: dateStr(-2)},
		{Type: "Today", Date: dateStr(0)},
		{Type: "Tomorrow", Date: dateStr(1)},
		{Type: "Future 1", Date: dateStr(5)},
		{Type: "Future 2", Date: dateStr(5)},
		{Type: "Far Future", Date: dateStr(10)},
	}

	ComputeRelativeFields(items)

	if items[0].DaysUntil != -2 {
		t.Errorf("expected DaysUntil -2 for past, got %d", items[0].DaysUntil)
	}
	if items[1].DaysUntil != 0 {
		t.Errorf("expected DaysUntil 0 for today, got %d", items[1].DaysUntil)
	}
	if items[2].DaysUntil != 1 {
		t.Errorf("expected DaysUntil 1 for tomorrow, got %d", items[2].DaysUntil)
	}
	if items[3].DaysUntil != 5 {
		t.Errorf("expected DaysUntil 5, got %d", items[3].DaysUntil)
	}

	if !items[1].Next {
		t.Error("expected Today to have Next=true")
	}

	if items[0].Next {
		t.Error("expected past item to have Next=false")
	}

	if items[2].Next {
		t.Error("expected tomorrow to have Next=false when today exists")
	}
}

func TestComputeRelativeFields_MultipleOnSameDay(t *testing.T) {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	future := today.AddDate(0, 0, 3).Format("2006-01-02")

	items := []Item{
		{Type: "A", Date: future},
		{Type: "B", Date: future},
		{Type: "C", Date: today.AddDate(0, 0, 5).Format("2006-01-02")},
	}

	ComputeRelativeFields(items)

	if !items[0].Next {
		t.Error("expected first item on soonest day to have Next=true")
	}
	if !items[1].Next {
		t.Error("expected second item on soonest day to have Next=true")
	}
	if items[2].Next {
		t.Error("expected later item to have Next=false")
	}
}

func TestComputeRelativeFields_AllPast(t *testing.T) {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)

	items := []Item{
		{Type: "Past 1", Date: today.AddDate(0, 0, -5).Format("2006-01-02")},
		{Type: "Past 2", Date: today.AddDate(0, 0, -2).Format("2006-01-02")},
	}

	ComputeRelativeFields(items)

	for i, item := range items {
		if item.Next {
			t.Errorf("expected all past items to have Next=false, but item %d has Next=true", i)
		}
	}
}

func TestComputeRelativeFields_InvalidDate(t *testing.T) {
	items := []Item{
		{Type: "Invalid", Date: "invalid-date"},
		{Type: "Short", Date: "2025"},
	}

	ComputeRelativeFields(items)
}

func TestPrettyDate(t *testing.T) {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	tests := []struct {
		name        string
		date        string
		wantRel     string
		wantDispFmt string
	}{
		{
			name:        "today",
			date:        today.Format("2006-01-02"),
			wantRel:     "today",
			wantDispFmt: "Mon 02 Jan 2006",
		},
		{
			name:        "tomorrow",
			date:        today.AddDate(0, 0, 1).Format("2006-01-02"),
			wantRel:     "tomorrow",
			wantDispFmt: "Mon 02 Jan 2006",
		},
		{
			name:        "in 4 days",
			date:        today.AddDate(0, 0, 4).Format("2006-01-02"),
			wantRel:     "in 4 days",
			wantDispFmt: "Mon 02 Jan 2006",
		},
		{
			name:        "2 days ago",
			date:        today.AddDate(0, 0, -2).Format("2006-01-02"),
			wantRel:     "2 days ago",
			wantDispFmt: "Mon 02 Jan 2006",
		},
		{
			name:        "invalid date",
			date:        "invalid",
			wantRel:     "",
			wantDispFmt: "invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			disp, rel := PrettyDate(tt.date)
			if rel != tt.wantRel {
				t.Errorf("PrettyDate(%q) rel = %q, want %q", tt.date, rel, tt.wantRel)
			}
			if tt.wantRel != "" {
				if len(disp) != len(tt.wantDispFmt) {
					t.Errorf("PrettyDate(%q) disp format unexpected: got %q", tt.date, disp)
				}
			} else {
				if disp != tt.date {
					t.Errorf("PrettyDate(%q) should return input for invalid date, got %q", tt.date, disp)
				}
			}
		})
	}
}

func TestTypeToSprite_Coverage(t *testing.T) {
	expectedMappings := map[string]string{
		"Food waste":                "food",
		"Food Caddy":                "food",
		"Garden waste":              "garden",
		"Garden waste bin":          "garden",
		"Mixed recycling":           "recycling",
		"Mixed recycling bin":       "recycling",
		"Cardboard & paper":         "paper",
		"Cardboard & paper bin":     "paper",
		"Non-recyclable refuse":     "refuse",
		"Non-recyclable refuse bin": "refuse",
		"Paper & card":              "paper",
		"Refuse":                    "refuse",
	}

	for wasteType, expectedSprite := range expectedMappings {
		sprite := typeToSprite[wasteType]
		if sprite != expectedSprite {
			t.Errorf("typeToSprite[%q] = %q, want %q", wasteType, sprite, expectedSprite)
		}
	}

	unknown := typeToSprite["Unknown Type"]
	if unknown != "" {
		t.Errorf("expected empty sprite for unknown type, got %q", unknown)
	}
}

func TestTypeToBinID_Coverage(t *testing.T) {
	expectedMappings := map[string]string{
		"Paper & card":              "blue lid bin",
		"Cardboard & paper":         "blue lid bin",
		"Cardboard & paper bin":     "blue lid bin",
		"Mixed recycling":           "black lid bin",
		"Mixed recycling bin":       "black lid bin",
		"Refuse":                    "purple lid bin",
		"Non-recyclable refuse":     "purple lid bin",
		"Non-recyclable refuse bin": "purple lid bin",
		"Garden waste":              "brown lid bin",
		"Garden waste bin":          "brown lid bin",
		"Food waste":                "brown caddy",
		"Food Caddy":                "brown caddy",
	}

	for wasteType, expectedBin := range expectedMappings {
		bin := typeToBinID[wasteType]
		if bin != expectedBin {
			t.Errorf("typeToBinID[%q] = %q, want %q", wasteType, bin, expectedBin)
		}
	}

	unknown := typeToBinID["Unknown Type"]
	if unknown != "" {
		t.Errorf("expected empty bin for unknown type, got %q", unknown)
	}
}
