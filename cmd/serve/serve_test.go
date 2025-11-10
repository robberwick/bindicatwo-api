package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robberwick/bindicatwo-api/pkg/nhdc"
	"github.com/spf13/viper"
)

func TestCacheKey(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"12345", "12345"},
		{"  12345  ", "12345"},
		{"UPPER", "upper"},
		{"  MiXeD  ", "mixed"},
	}

	for _, tt := range tests {
		got := cacheKey(tt.input)
		if got != tt.want {
			t.Errorf("cacheKey(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestGetCached_Miss(t *testing.T) {
	cache = map[string]cacheEntry{}
	_, ok := getCached("nonexistent")
	if ok {
		t.Error("expected cache miss for nonexistent key")
	}
}

func TestGetCached_Hit(t *testing.T) {
	cache = map[string]cacheEntry{}
	testData := []nhdc.Item{
		{Type: "Test", Date: "2025-11-15"},
	}

	putCached("test-uprn", testData, 1*time.Hour)

	data, ok := getCached("test-uprn")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if len(data) != 1 {
		t.Fatalf("expected 1 item, got %d", len(data))
	}
	if data[0].Type != "Test" {
		t.Errorf("expected Type=Test, got %s", data[0].Type)
	}
}

func TestGetCached_Expired(t *testing.T) {
	cache = map[string]cacheEntry{}
	testData := []nhdc.Item{
		{Type: "Test", Date: "2025-11-15"},
	}

	cacheMu.Lock()
	cache[cacheKey("test")] = cacheEntry{
		data:      testData,
		expiresAt: time.Now().Add(-1 * time.Second),
	}
	cacheMu.Unlock()

	_, ok := getCached("test")
	if ok {
		t.Error("expected cache miss for expired entry")
	}
}

func TestGetCached_Empty(t *testing.T) {
	cache = map[string]cacheEntry{}

	putCached("test", []nhdc.Item{}, 1*time.Hour)

	_, ok := getCached("test")
	if ok {
		t.Error("expected cache miss for empty data")
	}
}

func TestPutCached_ZeroTTL(t *testing.T) {
	cache = map[string]cacheEntry{}
	testData := []nhdc.Item{{Type: "Test"}}

	putCached("test", testData, 0)

	_, ok := getCached("test")
	if ok {
		t.Error("should not cache with zero TTL")
	}
}

func TestTokenBucket_Wait(t *testing.T) {
	tb := newTokenBucket(10.0, 5)

	ctx := context.Background()

	err := tb.wait(ctx)
	if err != nil {
		t.Fatalf("first wait failed: %v", err)
	}
}

func TestTokenBucket_ContextCanceled(t *testing.T) {
	tb := newTokenBucket(0.1, 1)

	_ = tb.wait(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := tb.wait(ctx)
	if err == nil {
		t.Error("expected error for canceled context")
	}
}

func TestTokenBucket_Refill(t *testing.T) {
	tb := newTokenBucket(100.0, 2)

	_ = tb.wait(context.Background())
	_ = tb.wait(context.Background())

	time.Sleep(50 * time.Millisecond)

	err := tb.wait(context.Background())
	if err != nil {
		t.Errorf("expected success after refill, got %v", err)
	}
}

func TestNewTokenBucket_InvalidParams(t *testing.T) {
	tb := newTokenBucket(-1, -1)
	if tb.rate <= 0 {
		t.Error("rate should be positive")
	}
	if tb.burst <= 0 {
		t.Error("burst should be positive")
	}
}

func TestComputeListenAddr(t *testing.T) {
	tests := []struct {
		name string
		addr string
		port int
		want string
	}{
		{"default", "127.0.0.1", 8080, "127.0.0.1:8080"},
		{"custom port", "127.0.0.1", 9000, "127.0.0.1:9000"},
		{"zero port", "0.0.0.0", 0, "0.0.0.0:0"},
		{"localhost", "localhost", 8080, "localhost:8080"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			viper.Reset()
			viper.Set("addr", tt.addr)
			viper.Set("port", tt.port)

			got := computeListenAddr()
			if got != tt.want {
				t.Errorf("computeListenAddr() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestComputeListenAddr_EmptyAddr(t *testing.T) {
	viper.Reset()
	viper.Set("addr", "")
	viper.Set("port", 8080)

	got := computeListenAddr()
	if !strings.Contains(got, "127.0.0.1:8080") {
		t.Errorf("expected default 127.0.0.1:8080, got %q", got)
	}
}

func TestClientIP(t *testing.T) {
	tests := []struct {
		name          string
		remoteAddr    string
		xForwardedFor string
		xRealIP       string
		want          string
	}{
		{
			name:       "RemoteAddr only",
			remoteAddr: "192.168.1.1:12345",
			want:       "192.168.1.1",
		},
		{
			name:          "X-Forwarded-For",
			remoteAddr:    "127.0.0.1:12345",
			xForwardedFor: "10.0.0.1, 10.0.0.2",
			want:          "10.0.0.1",
		},
		{
			name:       "X-Real-IP",
			remoteAddr: "127.0.0.1:12345",
			xRealIP:    "10.0.0.1",
			want:       "10.0.0.1",
		},
		{
			name:          "X-Forwarded-For takes precedence",
			remoteAddr:    "127.0.0.1:12345",
			xForwardedFor: "10.0.0.1",
			xRealIP:       "10.0.0.2",
			want:          "10.0.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xForwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tt.xForwardedFor)
			}
			if tt.xRealIP != "" {
				req.Header.Set("X-Real-IP", tt.xRealIP)
			}

			got := clientIP(req)
			if got != tt.want {
				t.Errorf("clientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStatusRecorder(t *testing.T) {
	rec := httptest.NewRecorder()
	sr := &statusRecorder{ResponseWriter: rec}

	n, err := sr.Write([]byte("test"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != 4 {
		t.Errorf("expected 4 bytes written, got %d", n)
	}
	if sr.status != http.StatusOK {
		t.Errorf("expected status 200, got %d", sr.status)
	}
	if sr.size != 4 {
		t.Errorf("expected size 4, got %d", sr.size)
	}
}

func TestStatusRecorder_ExplicitStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	sr := &statusRecorder{ResponseWriter: rec}

	sr.WriteHeader(http.StatusNotFound)
	sr.Write([]byte("not found"))

	if sr.status != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", sr.status)
	}
}

func TestHealthHandler(t *testing.T) {
	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()

	healthHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var result map[string]string
	err := json.NewDecoder(rec.Body).Decode(&result)
	if err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result["status"] != "ok" {
		t.Errorf("expected status=ok, got %s", result["status"])
	}
}

func TestScheduleHandler_PathParameter(t *testing.T) {
	cache = map[string]cacheEntry{}
	upstreamLimiter = newTokenBucket(10.0, 5)
	srvClient = nhdc.NewClient()

	testData := []nhdc.Item{{Type: "Test", Date: "2025-11-15"}}
	putCached("12345", testData, 1*time.Hour)

	req := httptest.NewRequest("GET", "/schedule/12345", nil)
	rec := httptest.NewRecorder()

	scheduleHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var result []nhdc.Item
	err := json.NewDecoder(rec.Body).Decode(&result)
	if err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("expected 1 item, got %d", len(result))
	}
}

func TestScheduleHandler_QueryParameter(t *testing.T) {
	cache = map[string]cacheEntry{}
	upstreamLimiter = newTokenBucket(10.0, 5)
	srvClient = nhdc.NewClient()

	testData := []nhdc.Item{{Type: "Test", Date: "2025-11-15"}}
	putCached("67890", testData, 1*time.Hour)

	req := httptest.NewRequest("GET", "/schedule?uprn=67890", nil)
	rec := httptest.NewRecorder()

	scheduleHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestScheduleHandler_AliasParameters(t *testing.T) {
	cache = map[string]cacheEntry{}
	upstreamLimiter = newTokenBucket(10.0, 5)
	srvClient = nhdc.NewClient()

	tests := []struct {
		name  string
		query string
	}{
		{"u alias", "?u=12345"},
		{"id alias", "?id=12345"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testData := []nhdc.Item{{Type: "Test", Date: "2025-11-15"}}
			putCached("12345", testData, 1*time.Hour)

			req := httptest.NewRequest("GET", "/schedule"+tt.query, nil)
			rec := httptest.NewRecorder()

			scheduleHandler(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("expected status 200, got %d", rec.Code)
			}
		})
	}
}

func TestFirmwareVersionHandler_NotConfigured(t *testing.T) {
	viper.Reset()
	viper.Set("firmware_version", "")

	req := httptest.NewRequest("GET", "/firmware/version.txt", nil)
	rec := httptest.NewRecorder()

	firmwareVersionHandler(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rec.Code)
	}
}

func TestFirmwareVersionHandler_Configured(t *testing.T) {
	viper.Reset()
	viper.Set("firmware_version", "1.2.3")

	req := httptest.NewRequest("GET", "/firmware/version.txt", nil)
	rec := httptest.NewRecorder()

	firmwareVersionHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "1.2.3") {
		t.Errorf("expected version 1.2.3 in response, got %q", body)
	}

	if !strings.HasSuffix(body, "\n") {
		t.Error("expected trailing newline")
	}
}

func TestFirmwareBinaryHandler_NotConfigured(t *testing.T) {
	viper.Reset()
	viper.Set("firmware_file", "")

	req := httptest.NewRequest("GET", "/firmware/bindicatwo_firmware.bin", nil)
	rec := httptest.NewRecorder()

	firmwareBinaryHandler(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rec.Code)
	}
}

func TestFirmwareBinaryHandler_FileNotFound(t *testing.T) {
	viper.Reset()
	viper.Set("firmware_file", "/nonexistent/file.bin")

	req := httptest.NewRequest("GET", "/firmware/bindicatwo_firmware.bin", nil)
	rec := httptest.NewRecorder()

	firmwareBinaryHandler(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rec.Code)
	}
}

func TestFirmwareBinaryHandler_Success(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "firmware.bin")
	err := os.WriteFile(tmpFile, []byte("fake firmware"), 0644)
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	viper.Reset()
	viper.Set("firmware_file", tmpFile)

	req := httptest.NewRequest("GET", "/firmware/bindicatwo_firmware.bin", nil)
	rec := httptest.NewRecorder()

	firmwareBinaryHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	if rec.Header().Get("Content-Type") != "application/octet-stream" {
		t.Errorf("expected Content-Type application/octet-stream, got %s", rec.Header().Get("Content-Type"))
	}

	if !strings.Contains(rec.Header().Get("Content-Disposition"), "bindicatwo_firmware.bin") {
		t.Error("expected Content-Disposition with filename")
	}
}
