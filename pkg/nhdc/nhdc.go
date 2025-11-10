package nhdc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/cookiejar"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	MOBILE_API_BASE = "https://apps.cloud9technologies.com/northherts/citizenmobile/mobileapi"
)

type Item struct {
	Type      string `json:"type"`
	Date      string `json:"date"`
	Sprite    string `json:"sprite,omitempty"`
	Bin       string `json:"bin,omitempty"`
	DaysUntil int    `json:"days_until"`
	Next      bool   `json:"next"`
}

// Mobile API response structures
type MobileAPIResponse struct {
	WasteCollectionDates WasteCollectionDates `json:"wasteCollectionDates"`
}

type WasteCollectionDates struct {
	UPRN                        string            `json:"uprn"`
	CacheKey                    string            `json:"cacheKey"`
	Container1CollectionDetails *ContainerDetails `json:"container1CollectionDetails"`
	Container2CollectionDetails *ContainerDetails `json:"container2CollectionDetails"`
	Container3CollectionDetails *ContainerDetails `json:"container3CollectionDetails"`
	Container4CollectionDetails *ContainerDetails `json:"container4CollectionDetails"`
	Container5CollectionDetails *ContainerDetails `json:"container5CollectionDetails"`
	Container6CollectionDetails *ContainerDetails `json:"container6CollectionDetails"`
	Container7CollectionDetails *ContainerDetails `json:"container7CollectionDetails"`
	Container8CollectionDetails *ContainerDetails `json:"container8CollectionDetails"`
}

type ContainerDetails struct {
	ContainerID          int     `json:"containerID"`
	ContainerDescription string  `json:"containerDescription"`
	Order                int     `json:"order"`
	CollectionDate       string  `json:"collectionDate"`
	CollectionWeek       *string `json:"collectionWeek"`
	DateChanged          bool    `json:"dateChanged"`
	DateChangeReason     string  `json:"dateChangeReason"`
	Image                string  `json:"image"`
	ImageURL             string  `json:"imageURL"`
}

var typeToSprite = map[string]string{
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

// Bin identification (e.g., lid colors/caddy) per canonical type
var typeToBinID = map[string]string{
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

// HTTP client with cookie jar
func NewClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Timeout: 30 * time.Second, Jar: jar}
}

// fetchMobileAPI calls the Cloud9 mobile API to get waste collection data
func fetchMobileAPI(c *http.Client, uprn string) (*MobileAPIResponse, error) {
	urlStr := fmt.Sprintf("%s/wastecollections/%s", MOBILE_API_BASE, uprn)
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return nil, err
	}

	// Set required headers
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Basic Y2xvdWQ5OmlkQmNWNGJvcjU=")
	req.Header.Set("X-Api-Version", "2")
	req.Header.Set("X-App-Version", "3.0.56")
	req.Header.Set("X-Platform", "android")
	req.Header.Set("User-Agent", "GoBindicatwo/1.0")

	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("mobile API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var apiResp MobileAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse mobile API response: %w", err)
	}

	return &apiResp, nil
}

// convertMobileAPIToItems converts the mobile API response to our Item format
func convertMobileAPIToItems(apiResp *MobileAPIResponse) []Item {
	items := make([]Item, 0, 8)

	containers := []*ContainerDetails{
		apiResp.WasteCollectionDates.Container1CollectionDetails,
		apiResp.WasteCollectionDates.Container2CollectionDetails,
		apiResp.WasteCollectionDates.Container3CollectionDetails,
		apiResp.WasteCollectionDates.Container4CollectionDetails,
		apiResp.WasteCollectionDates.Container5CollectionDetails,
		apiResp.WasteCollectionDates.Container6CollectionDetails,
		apiResp.WasteCollectionDates.Container7CollectionDetails,
		apiResp.WasteCollectionDates.Container8CollectionDetails,
	}

	for _, container := range containers {
		if container == nil {
			continue
		}

		// Skip empty collection dates
		if container.CollectionDate == "" {
			continue
		}

		// Parse the collection date (format: "2025-11-25T00:00:00")
		dateStr := container.CollectionDate
		if len(dateStr) >= 10 {
			dateStr = dateStr[:10] // Extract YYYY-MM-DD
		}

		item := Item{
			Type:   container.ContainerDescription,
			Date:   dateStr,
			Sprite: typeToSprite[container.ContainerDescription],
			Bin:    typeToBinID[container.ContainerDescription],
		}

		items = append(items, item)
	}

	// Sort by date
	sort.Slice(items, func(a, b int) bool {
		return items[a].Date < items[b].Date
	})

	return items
}

// Public API
// GetSchedule fetches the bin collection schedule for a given UPRN
// The search parameter should be a UPRN (Unique Property Reference Number)
func GetSchedule(c *http.Client, search, preferContains string) ([]Item, error) {
	// The search parameter is expected to be a UPRN
	uprn := strings.TrimSpace(search)
	if uprn == "" {
		return nil, errors.New("UPRN is required")
	}

	// Call the mobile API with the UPRN
	apiResp, err := fetchMobileAPI(c, uprn)
	if err != nil {
		return nil, fmt.Errorf("mobile API error: %w", err)
	}

	// Convert to Item format
	items := convertMobileAPIToItems(apiResp)

	return items, nil
}

// ComputeRelativeFields sets DaysUntil and Next flags.
func ComputeRelativeFields(items []Item) {
	now := time.Now().In(time.Local)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	minNonNeg := -1
	for i := range items {
		y, m, d := 0, 0, 0
		if len(items[i].Date) == 10 {
			if yy, err := strconv.Atoi(items[i].Date[0:4]); err == nil {
				y = yy
			}
			if mm, err := strconv.Atoi(items[i].Date[5:7]); err == nil {
				m = mm
			}
			if dd, err := strconv.Atoi(items[i].Date[8:10]); err == nil {
				d = dd
			}
		}
		t := time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.Local)
		days := int(t.Sub(today).Hours() / 24)
		items[i].DaysUntil = days
		items[i].Next = false
		if days >= 0 {
			if minNonNeg == -1 || days < minNonNeg {
				minNonNeg = days
			}
		}
	}
	if minNonNeg != -1 {
		for i := range items {
			if items[i].DaysUntil == minNonNeg {
				items[i].Next = true
			}
		}
	}
}

// PrettyDate returns formatted and relative label.
func PrettyDate(d string) (string, string) {
	t, err := time.Parse("2006-01-02", d)
	if err != nil {
		return d, ""
	}
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	dh := t.Sub(today).Hours() / 24.0
	diff := int(math.Floor(dh))
	if math.Abs(dh-float64(diff)) < 1e-9 {
		diff = int(dh)
	}
	rel := ""
	switch diff {
	case 0:
		rel = "today"
	case 1:
		rel = "tomorrow"
	default:
		if diff > 1 {
			rel = fmt.Sprintf("in %d days", diff)
		} else if diff < 0 {
			rel = fmt.Sprintf("%d days ago", -diff)
		}
	}
	return t.Format("Mon 02 Jan 2006"), rel
}
