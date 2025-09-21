package nhdc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	BASE         = "https://waste.nc.north-herts.gov.uk"
	INPUT_PATH   = "/w/webpage/find-bin-collection-day-input-address"
	DETAILS_PATH = "/w/webpage/find-bin-collection-day-show-details"
)

type Item struct {
	Type      string `json:"type"`
	Date      string `json:"date"`
	Sprite    string `json:"sprite,omitempty"`
	Bin       string `json:"bin,omitempty"`
	DaysUntil int    `json:"days_until"`
	Next      bool   `json:"next"`
}

var typeToSprite = map[string]string{
	"Food waste":            "food",
	"Garden waste":          "garden",
	"Mixed recycling":       "recycling",
	"Cardboard & paper":     "paper",
	"Non-recyclable refuse": "refuse",
	"Paper & card":          "paper",
	"Refuse":                "refuse",
}

// Bin identification (e.g., lid colors/caddy) per canonical type
var typeToBinID = map[string]string{
	"Paper & card":    "blue lid bin",
	"Mixed recycling": "black lid bin",
	"Refuse":          "purple lid bin",
	"Garden waste":    "brown lid bin",
	"Food waste":      "brown caddy",
}

// HTTP client with cookie jar
func NewClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Timeout: 30 * time.Second, Jar: jar}
}

func httpGet(c *http.Client, urlStr, accept string, extra map[string]string) (string, *http.Response, error) {
	req, _ := http.NewRequest("GET", urlStr, nil)
	req.Header.Set("User-Agent", "GoBindicatwo/1.0")
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		return "", nil, err
	}
	b, err := io.ReadAll(resp.Body)
	if cerr := resp.Body.Close(); err == nil && cerr != nil {
		err = cerr
	}
	if err != nil {
		return "", resp, err
	}
	if resp.StatusCode != 200 {
		return "", resp, fmt.Errorf("GET %s -> %d", urlStr, resp.StatusCode)
	}
	return string(b), resp, nil
}

func httpPostForm(c *http.Client, urlStr string, body string, referer string, accept string, extra map[string]string) (string, *http.Response, error) {
	req, _ := http.NewRequest("POST", urlStr, strings.NewReader(body))
	req.Header.Set("User-Agent", "GoBindicatwo/1.0")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	if accept == "" {
		accept = "application/json, text/javascript, */*; q=0.01"
	}
	req.Header.Set("Accept", accept)
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	req.Header.Set("Origin", BASE)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		return "", nil, err
	}
	b, readErr := io.ReadAll(resp.Body)
	if cerr := resp.Body.Close(); readErr == nil && cerr != nil {
		readErr = cerr
	}
	if readErr != nil {
		return "", resp, readErr
	}
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return "", resp, fmt.Errorf("POST %s -> %d", urlStr, resp.StatusCode)
	}
	return string(b), resp, nil
}

// Between finds substring between a and b starting at offset start.
func Between(s, a, b string, start int) (string, int) {
	i := strings.Index(s[start:], a)
	if i == -1 {
		return "", -1
	}
	i += start
	j := strings.Index(s[i+len(a):], b)
	if j == -1 {
		return "", -1
	}
	j += i + len(a)
	return s[i+len(a) : j], j + len(b)
}

// Step 1: read CSRF and page IDs
func readShellTokens(c *http.Client) (csrf, inputPageID, inputPageToken, html string, err error) {
	html, _, err = httpGet(c, BASE+INPUT_PATH, "text/html", nil)
	if err != nil {
		return
	}
	csrf, _ = Between(html, "var CSRF = '", "'", 0)
	if csrf == "" {
		low := strings.ToLower(html)
		csrf, _ = Between(low, "var csrf = '", "'", 0)
	}
	if csrf == "" {
		err = errors.New("CSRF not found in shell")
		return
	}
	pid, _ := Between(html, "webpage_subpage_id=", "&", 0)
	if pid == "" {
		pid, _ = Between(html, "data-page-id=\"", "\"", 0)
	}
	tok, _ := Between(html, "webpage_token=", "\"", 0)
	inputPageID, inputPageToken = pid, tok
	return
}

// Step 2: load fragment
func loadInputFragment(c *http.Client, pageID, pageToken, csrf string) (string, error) {
	qs := "?webpage_subpage_id=" + url.QueryEscape(pageID) + "&webpage_token=" + url.QueryEscape(pageToken)
	urlStr := BASE + INPUT_PATH + qs
	body := "_dummy=1&_update_page_content_request=1&form_check_ajax=" + url.QueryEscape(csrf)
	resp, _, err := httpPostForm(c, urlStr, body, BASE+INPUT_PATH, "application/json, text/javascript, */*; q=0.01", nil)
	if err != nil {
		return "", err
	}
	var j map[string]any
	if err := json.Unmarshal([]byte(resp), &j); err == nil {
		if d, ok := j["data"].(string); ok {
			return d, nil
		}
	}
	return resp, nil
}

// Step 2b: parse widget ids from fragment
func parseWidgetIDs(fragment string) (pwg, submissionToken, pcl, crow, pcfSelect, pcfSubmit, levels string) {
	html := fragment
	if pos := strings.Index(html, "name=\"submitted_widget_group_id\""); pos != -1 {
		v, _ := Between(html, "value=\"", "\"", pos)
		pwg = v
	}
	if pos := strings.Index(html, "name=\"submission_token\""); pos != -1 {
		v, _ := Between(html, "value=\"", "\"", pos)
		submissionToken = v
	}
	for _, q := range []string{"data-params=\"", "data-params='"} {
		pos := 0
		for {
			p := strings.Index(html[pos:], q)
			if p == -1 {
				break
			}
			p += pos
			payload, pos2 := Between(html, q, q[len(q)-1:], p)
			if payload != "" {
				payload = strings.ReplaceAll(payload, "&quot;", "\"")
				var obj map[string]any
				if json.Unmarshal([]byte(payload), &obj) == nil {
					if v, ok := obj["levels"].(string); ok {
						levels = v
						break
					}
					if v, ok := obj["level"].(string); ok {
						levels = v
						break
					}
				}
			}
			if pos2 == -1 {
				pos = p + 1
			} else {
				pos = pos2
			}
		}
		if levels != "" {
			break
		}
	}
	needle := "name=\"payload["
	pos := 0
	for {
		p := strings.Index(html[pos:], needle)
		if p == -1 {
			break
		}
		p += pos
		attr, pos2 := Between(html, needle, "\"", p)
		if attr == "" {
			if pos2 == -1 {
				pos = p + 1
			} else {
				pos = pos2
			}
			continue
		}
		if strings.Contains(attr, "[PCL") && strings.Contains(attr, "][formtable]") && strings.Contains(attr, "[C_") && strings.Contains(attr, "[PCF") {
			if i := strings.Index(attr, "[PCL"); i != -1 {
				if j := strings.Index(attr[i+1:], "]"); j != -1 {
					pcl = attr[i+1 : i+1+j]
				}
			}
			if i := strings.Index(attr, "[C_"); i != -1 {
				if j := strings.Index(attr[i+1:], "]"); j != -1 {
					crow = attr[i+1 : i+1+j]
				}
			}
			if i := strings.Index(attr, "[PCF"); i != -1 {
				if j := strings.Index(attr[i+1:], "]"); j != -1 {
					pcfSelect = attr[i+1 : i+1+j]
				}
			}
			break
		}
		if pos2 == -1 {
			pos = p + 1
		} else {
			pos = pos2
		}
	}
	pos = 0
	for {
		i := strings.Index(html[pos:], "<input")
		if i == -1 {
			break
		}
		i += pos
		tag, pos2 := Between(html, "<input", ">", i)
		full := "<input" + tag + ">"
		if strings.Contains(full, "type=\"submit\"") && strings.Contains(full, "name=\"payload[") {
			last := strings.LastIndex(full, "[PCF")
			if last != -1 {
				endb := strings.Index(full[last:], "]")
				if endb != -1 {
					pcfSubmit = full[last+1 : last+endb]
					break
				}
			}
		}
		if pos2 == -1 {
			pos = i + 1
		} else {
			pos = pos2
		}
	}
	return
}

// Step 3: typeahead
func typeahead(c *http.Client, pageID, pageToken, csrf, levels, search string) ([][2]string, error) {
	qs := "?webpage_subpage_id=" + url.QueryEscape(pageID) + "&webpage_token=" + url.QueryEscape(pageToken) + "&ajax_action=html_get_type_ahead_results"
	urlStr := BASE + "/w/ajax" + qs
	var b strings.Builder
	b.WriteString("levels=")
	b.WriteString(url.QueryEscape(levels))
	b.WriteString("&search_string=")
	b.WriteString(url.QueryEscape(search))
	b.WriteString("&display_limit=75")
	b.WriteString("&presenter_settings%5Brecords_limit%5D=75")
	b.WriteString("&presenter_settings%5Bload_more_records_label%5D=")
	b.WriteString(url.QueryEscape("Click here to load more addresses"))
	b.WriteString("&presenter_settings%5Bmin_characters%5D=3")
	b.WriteString("&presenter_settings%5Bexact_match_first%5D=0")
	b.WriteString("&settings%5Blabel%5D=")
	b.WriteString(url.QueryEscape("Search for an address. For example, 123 Test Road, or SG6 3JF. Postcodes must contain a space."))
	b.WriteString("&context_page_id=")
	b.WriteString(url.QueryEscape(pageID))
	b.WriteString("&form_check_ajax=")
	b.WriteString(url.QueryEscape(csrf))

	html, _, err := httpPostForm(c, urlStr, b.String(), BASE+INPUT_PATH, "text/html, */*; q=0.01", nil)
	if err != nil {
		return nil, err
	}
	options := make([][2]string, 0, 8)
	pos := 0
	for {
		li := strings.Index(html[pos:], "<li")
		if li == -1 {
			break
		}
		li += pos
		liEnd := strings.Index(html[li:], "</li>")
		if liEnd == -1 {
			break
		}
		liEnd += li
		tagEnd := strings.Index(html[li:], ">")
		if tagEnd == -1 || li+tagEnd > liEnd {
			pos = li + 3
			continue
		}
		tag := html[li : li+tagEnd+1]
		dataID := ""
		if a := strings.Index(tag, "data-id=\""); a != -1 {
			a1 := strings.Index(tag[a+len("data-id=\""):], "\"")
			if a1 != -1 {
				dataID = tag[a+len("data-id=\"") : a+len("data-id=\"")+a1]
			}
		}
		label := ""
		if bpos := strings.Index(tag, "aria-label=\""); bpos != -1 {
			b1 := strings.Index(tag[bpos+len("aria-label=\""):], "\"")
			if b1 != -1 {
				label = tag[bpos+len("aria-label=\"") : bpos+len("aria-label=\"")+b1]
			}
		}
		if label == "" {
			inner := html[li+tagEnd+1 : liEnd]
			label = StripTags(inner)
		}
		if dataID != "" && IsDigits(dataID) {
			options = append(options, [2]string{dataID, label})
		}
		pos = liEnd + 5
	}
	return options, nil
}

func IsDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}

func StripTags(s string) string {
	re := regexp.MustCompile(`<[^>]+>`) // simple tag matcher
	clean := re.ReplaceAllString(s, " ")
	fields := strings.Fields(clean)
	return strings.Join(fields, " ")
}

// Step 4: submit input
func submitInput(c *http.Client, pageID, pageToken, csrf, pwg, submissionToken, pcl, crow, pcfSelect, pcfSubmit, optionVal, optionLabel string) (string, error) {
	urlStr := BASE + INPUT_PATH + "?webpage_subpage_id=" + url.QueryEscape(pageID) + "&webpage_token=" + url.QueryEscape(pageToken)
	var b strings.Builder
	b.WriteString("form_check=")
	b.WriteString(url.QueryEscape(csrf))
	b.WriteString("&submitted_page_id=")
	b.WriteString(url.QueryEscape(pageID))
	b.WriteString("&submitted_widget_group_id=")
	b.WriteString(url.QueryEscape(pwg))
	b.WriteString("&submitted_widget_group_type=modify")
	b.WriteString("&submission_token=")
	b.WriteString(url.QueryEscape(submissionToken))
	_, err := fmt.Fprintf(&b, "&payload%%5B%s%%5D%%5B%s%%5D%%5B%s%%5D%%5Bformtable%%5D%%5B%s%%5D%%5B%s%%5D=%s", url.QueryEscape(pageID), url.QueryEscape(pwg), url.QueryEscape(pcl), url.QueryEscape(crow), url.QueryEscape(pcfSelect), url.QueryEscape(optionVal))
	if err != nil {
		return "", err
	}
	_, err = fmt.Fprintf(&b, "&payload%%5B%s%%5D%%5B%s%%5D%%5B%s%%5D%%5Bformtable%%5D%%5B%s%%5D%%5B%s%%5D=%s", url.QueryEscape(pageID), url.QueryEscape(pwg), url.QueryEscape(pcl), url.QueryEscape(crow), url.QueryEscape(pcfSubmit), url.QueryEscape(optionLabel))
	if err != nil {
		return "", err
	}
	b.WriteString("&submit_fragment_id=")
	b.WriteString(url.QueryEscape(pcfSubmit))
	b.WriteString("&_session_storage=%7B%22_global%22%3A%7B%22destination_stack%22%3A%5B%22w%2Fwebpage%2Ffind-bin-collection-day-input-address%22%5D%7D%7D")
	b.WriteString("&_update_page_content_request=1")
	b.WriteString("&form_check_ajax=")
	b.WriteString(url.QueryEscape(csrf))
	resp, _, err := httpPostForm(c, urlStr, b.String(), BASE+INPUT_PATH, "application/json, text/javascript, */*; q=0.01", nil)
	if err != nil {
		return "", err
	}
	return resp, nil
}

// discover details tokens from submit response
func DiscoverFromSubmit(respText string) (token, auth, id string, err error) {
	var j map[string]any
	if json.Unmarshal([]byte(respText), &j) == nil {
		if redir, ok := j["redirect_url"].(string); ok && redir != "" {
			redir = strings.ReplaceAll(redir, "\\/", "/")
			full := redir
			if strings.HasPrefix(redir, "/") {
				full = BASE + redir
			}
			token, _ = Between(full, "webpage_token=", "&", 0)
			auth, _ = Between(full, "auth=", "&", 0)
			id, _ = Between(full, "id=", "&", 0)
			if id == "" {
				if i := strings.Index(full, "id="); i != -1 {
					id = full[i+3:]
				}
			}
			if token != "" && auth != "" && id != "" {
				return
			}
		}
		if data, ok := j["data"].(string); ok {
			respText = data
		}
	}
	needle := "w/webpage/find-bin-collection-day-show-details?"
	pos := 0
	for {
		p := strings.Index(respText[pos:], needle)
		if p == -1 {
			break
		}
		p += pos
		end := p
		for end < len(respText) && !strings.ContainsRune("\"' <", rune(respText[end])) {
			end++
		}
		candidate := respText[p:end]
		if strings.Contains(candidate, "webpage_token=") && strings.Contains(candidate, "auth=") && strings.Contains(candidate, "id=") {
			path := candidate
			token, _ = Between(path, "webpage_token=", "&", 0)
			auth, _ = Between(path, "auth=", "&", 0)
			id, _ = Between(path, "id=", "&", 0)
			if id == "" {
				if i := strings.Index(path, "id="); i != -1 {
					id = path[i+3:]
				}
			}
			if token != "" && auth != "" && id != "" {
				return
			}
		}
		pos = end + 1
	}
	err = errors.New("Could not discover details URL (token/auth/id)")
	return
}

// Step 5: fetch details HTML
func fetchDetailsHTML(c *http.Client, token, auth, id, csrf string) (string, error) {
	params := "?webpage_token=" + url.QueryEscape(token) + "&auth=" + url.QueryEscape(auth) + "&id=" + url.QueryEscape(id)
	showURL := BASE + DETAILS_PATH + params
	body := "_dummy=1&_update_page_content_request=1&form_check_ajax=" + url.QueryEscape(csrf)
	jtxt, _, err := httpPostForm(c, showURL, body, BASE+INPUT_PATH, "application/json, text/javascript, */*; q=0.01", nil)
	if err != nil {
		return "", err
	}
	type ajaxResp struct {
		Content string `json:"content"`
		Data    string `json:"data"`
	}
	var j ajaxResp
	if json.Unmarshal([]byte(jtxt), &j) == nil {
		if strings.TrimSpace(j.Content) != "" {
			return j.Content, nil
		}
		if strings.TrimSpace(j.Data) != "" {
			data := j.Data
			if strings.Contains(data, "Next collection") || strings.Contains(data, "listing_template") || strings.Contains(data, "page_widget_group") {
				return data, nil
			}
			ajaxURL := ExtractAjaxURL(data)
			if ajaxURL != "" {
				return fetchAjaxContent(c, ajaxURL, showURL, body)
			}
		}
	}
	shell, _, err := httpGet(c, showURL, "text/html", nil)
	if err == nil {
		ajaxURL := ExtractAjaxURL(shell)
		if ajaxURL != "" {
			return fetchAjaxContent(c, ajaxURL, showURL, body)
		}
	}
	return "", errors.New("AJAX did not yield content")
}

func ExtractAjaxURL(html string) string {
	for _, marker := range []string{"var AJAX_URL =", "AJAX_URL ="} {
		if k := strings.Index(html, marker); k != -1 {
			q1 := strings.Index(html[k:], "'")
			if q1 != -1 {
				q1 += k
				q2 := strings.Index(html[q1+1:], "'")
				if q2 != -1 {
					q2 += q1 + 1
					return html[q1+1 : q2]
				}
			}
		}
	}
	get := func(a, b string) string { v, _ := Between(html, a, b, 0); return v }
	pid := get("webpage_subpage_id=", "&")
	tok := get("webpage_token=", "&")
	au := get("auth=", "&")
	var rid string
	if p := strings.Index(html, "&id="); p != -1 {
		p += len("&id=")
		end := p
		for end < len(html) {
			ch := html[end]
			if ch == '&' || ch == '"' || ch == '\'' || ch == '<' || ch == ' ' {
				break
			}
			end++
		}
		rid = html[p:end]
	}
	if rid == "" {
		if p := strings.Index(html, "id="); p != -1 {
			p += len("id=")
			end := p
			for end < len(html) {
				ch := html[end]
				if ch == '&' || ch == '"' || ch == '\'' || ch == '<' || ch == ' ' {
					break
				}
				end++
			}
			rid = html[p:end]
		}
	}
	if pid != "" && tok != "" && au != "" && rid != "" {
		return "/w/ajax?webpage_subpage_id=" + pid + "&webpage_token=" + tok + "&auth=" + au + "&id=" + rid
	}
	return ""
}

func fetchAjaxContent(c *http.Client, ajaxURL, referer, body string) (string, error) {
	full := ajaxURL
	if strings.HasPrefix(ajaxURL, "/") {
		full = BASE + ajaxURL
	}
	resp, _, err := httpPostForm(c, full, body, referer, "application/json, text/javascript, */*; q=0.01", nil)
	if err != nil {
		return "", err
	}
	type ajaxResp struct {
		Content string `json:"content"`
		Data    string `json:"data"`
	}
	var j ajaxResp
	if json.Unmarshal([]byte(resp), &j) == nil {
		if j.Content != "" {
			return j.Content, nil
		}
		if j.Data != "" {
			return j.Data, nil
		}
	}
	return "", errors.New("AJAX JSON did not contain content")
}

var months = map[string]int{
	"january": 1, "february": 2, "march": 3, "april": 4, "may": 5, "june": 6,
	"july": 7, "august": 8, "september": 9, "october": 10, "november": 11, "december": 12,
}

var canonMap = map[string]string{
	"paper & card":          "Paper & card",
	"paper &amp; card":      "Paper & card",
	"cardboard & paper":     "Paper & card",
	"cardboard &amp; paper": "Paper & card",
	"blue lid bin":          "Paper & card",
	"food waste":            "Food waste",
	"brown caddy":           "Food waste",
	"garden waste":          "Garden waste",
	"brown lid bin":         "Garden waste",
	"refuse":                "Refuse",
	"non-recyclable waste":  "Refuse",
	"purple lid bin":        "Refuse",
	"mixed recycling":       "Mixed recycling",
	"black lid bin":         "Mixed recycling",
}

var spriteMap = map[string]string{
	"Paper & card":    "paper",
	"Food waste":      "food",
	"Garden waste":    "garden",
	"Refuse":          "refuse",
	"Mixed recycling": "recycling",
}

func ParseNextCollections(html string) []Item {
	if html == "" {
		return nil
	}
	itemsByType := map[string]Item{}
	findNext := func(s string, start int) int {
		j := strings.Index(s[start:], "Next collection")
		if j == -1 {
			return strings.Index(s[start:], "Next\u00a0collection")
		}
		return j
	}
	i := 0
	for {
		j := findNext(html, i)
		if j < 0 {
			break
		}
		j += i
		br := strings.Index(html[j:], "<br")
		if br == -1 {
			i = j + 1
			continue
		}
		br += j
		brEnd := strings.Index(html[br:], ">")
		if brEnd == -1 {
			i = j + 1
			continue
		}
		brEnd += br
		ds := brEnd + 1
		de := strings.Index(html[ds:], "<")
		if de == -1 {
			i = j + 1
			continue
		}
		de += ds
		rawDate := StripTagsSmall(html[ds:de])
		parts := strings.Fields(rawDate)
		iso := ""
		if len(parts) >= 3 {
			day := Unsuffix(parts[len(parts)-3])
			mon := strings.ToLower(parts[len(parts)-2])
			year := parts[len(parts)-1]
			if d, err := strconv.Atoi(day); err == nil {
				if m := months[mon]; m > 0 {
					if y, err := strconv.Atoi(year); err == nil {
						iso = fmt.Sprintf("%04d-%02d-%02d", y, m, d)
					}
				}
			}
		}
		ctype := ""
		searchPos := j
		for k := 0; k < 16; k++ {
			ls := strings.LastIndex(html[:searchPos], "<strong")
			if ls < 0 {
				break
			}
			se := strings.Index(html[ls:], "</strong>")
			if se < 0 {
				searchPos = ls
				continue
			}
			se += ls
			gt := strings.Index(html[ls:], ">")
			if gt == -1 || ls+gt > se {
				searchPos = ls
				continue
			}
			txt := UnescapeEntities(StripTagsSmall(html[ls+gt+1 : se]))
			searchPos = ls
			low := strings.ToLower(txt)
			if low != "" && low != "next collection" && low != "collection cycle" {
				ctype = strings.TrimSpace(txt)
				break
			}
		}
		if iso != "" && ctype != "" {
			canon := ctype
			if v, ok := canonMap[strings.ToLower(ctype)]; ok {
				canon = v
			}
			if prev, ok := itemsByType[canon]; !ok || iso < prev.Date {
				itemsByType[canon] = Item{Type: canon, Date: iso, Sprite: spriteMap[canon]}
			}
		}
		i = de + 1
	}
	items := make([]Item, 0, len(itemsByType))
	for _, it := range itemsByType {
		items = append(items, it)
	}
	sort.Slice(items, func(a, b int) bool { return items[a].Date < items[b].Date })
	return items
}

func StripTagsSmall(s string) string { return StripTags(s) }

func UnescapeEntities(s string) string {
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "&amp;", "&")
	return s
}

func Unsuffix(day string) string {
	low := strings.ToLower(day)
	for _, suf := range []string{"st", "nd", "rd", "th"} {
		if strings.HasSuffix(low, suf) {
			return day[:len(day)-2]
		}
	}
	return day
}

// Public API
func GetSchedule(c *http.Client, search, preferContains string) ([]Item, error) {
	csrf, pageID, pageToken, _, err := readShellTokens(c)
	if err != nil {
		return nil, err
	}
	frag, err := loadInputFragment(c, pageID, pageToken, csrf)
	if err != nil {
		return nil, err
	}
	pwg, submissionToken, pcl, crow, pcfSelect, pcfSubmit, levels := parseWidgetIDs(frag)
	if pwg == "" || submissionToken == "" || pcl == "" || crow == "" || pcfSelect == "" || pcfSubmit == "" {
		return nil, errors.New("Could not parse required form IDs from input fragment")
	}
	opts, err := typeahead(c, pageID, pageToken, csrf, levels, search)
	if err != nil {
		return nil, err
	}
	if len(opts) == 0 {
		return nil, errors.New("Type-ahead returned no options; check 'search' or 'levels'")
	}
	pick := opts[0]
	if preferContains != "" {
		pref := strings.ToLower(preferContains)
		for _, o := range opts {
			if strings.Contains(strings.ToLower(o[1]), pref) {
				pick = o
				break
			}
		}
	} else {
		re := regexp.MustCompile(`\b(\d+)\b`)
		m := re.FindStringSubmatch(search)
		if len(m) > 1 {
			wanted := m[1]
			for _, o := range opts {
				low := strings.ToLower(strings.TrimLeft(o[1], " "))
				if strings.HasPrefix(low, wanted+" ") {
					pick = o
					break
				}
			}
		}
	}
	resp, err := submitInput(c, pageID, pageToken, csrf, pwg, submissionToken, pcl, crow, pcfSelect, pcfSubmit, pick[0], pick[1])
	if err != nil {
		return nil, err
	}
	tok, auth, id, err := DiscoverFromSubmit(resp)
	if err != nil {
		return nil, err
	}
	html, err := fetchDetailsHTML(c, tok, auth, id, csrf)
	if err != nil {
		return nil, err
	}
	items := ParseNextCollections(html)
	for i := range items {
		if sp, ok := typeToSprite[items[i].Type]; ok {
			items[i].Sprite = sp
		}
		if bid, ok := typeToBinID[items[i].Type]; ok {
			items[i].Bin = bid
		}
	}
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
