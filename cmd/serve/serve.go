package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/robberwick/bindicatwo-api/pkg/nhdc"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// AddServeSubcommand adds the `serve` subcommand which runs an HTTP API server
// exposing the schedule as JSON.
func AddServeSubcommand(root *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run HTTP server exposing the schedule API",
		Long:  "Start a small HTTP server that serves JSON schedule data at the root path (/) and a health check at /healthz.",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Bind flags used by this command
			_ = viper.BindPFlag("addr", cmd.Flags().Lookup("addr"))
			_ = viper.BindPFlag("port", cmd.Flags().Lookup("port"))
			_ = viper.BindPFlag("cache_ttl", cmd.Flags().Lookup("cache-ttl"))
			_ = viper.BindPFlag("cors", cmd.Flags().Lookup("cors"))
			_ = viper.BindPFlag("rate_rps", cmd.Flags().Lookup("rate-rps"))
			_ = viper.BindPFlag("rate_burst", cmd.Flags().Lookup("rate-burst"))

			// Bind env vars for convenience
			_ = viper.BindEnv("addr", "ADDR")
			_ = viper.BindEnv("port", "PORT")
			_ = viper.BindEnv("cache_ttl", "CACHE_TTL")
			_ = viper.BindEnv("cors", "CORS_ORIGINS")
			_ = viper.BindEnv("rate_rps", "RATE_RPS")
			_ = viper.BindEnv("rate_burst", "RATE_BURST")
			// OTA firmware envs
			_ = viper.BindEnv("firmware_enabled", "FIRMWARE_ENABLE")
			_ = viper.BindEnv("firmware_version", "FIRMWARE_VERSION")
			_ = viper.BindEnv("firmware_file", "FIRMWARE_FILE")

			return runServer()
		},
	}

	// Flags
	cmd.Flags().String("addr", "127.0.0.1", "Listen address/host (ADDR)")
	cmd.Flags().Int("port", 8080, "Listen port (PORT). Use 0 for any free port.")
	cmd.Flags().Duration("cache-ttl", 15*time.Minute, "Cache TTL for schedule responses (CACHE_TTL)")
	cmd.Flags().String("cors", "", "CORS allowed origins: '*' or CSV (CORS_ORIGINS)")
	cmd.Flags().Float64("rate-rps", 1.0, "Max upstream requests per second (RATE_RPS)")
	cmd.Flags().Int("rate-burst", 5, "Burst capacity for upstream requests (RATE_BURST)")
	// OTA firmware flags
	cmd.Flags().Bool("firmware-enable", false, "Enable firmware OTA endpoints (FIRMWARE_ENABLE)")
	cmd.Flags().String("firmware-version", "", "Firmware version string served at /firmware/version.txt (FIRMWARE_VERSION)")
	cmd.Flags().String("firmware-file", "", "Path to firmware binary served at /firmware/bindicatwo_firmware.bin (FIRMWARE_FILE)")
	_ = viper.BindPFlag("firmware_enabled", cmd.Flags().Lookup("firmware-enable"))
	_ = viper.BindPFlag("firmware_version", cmd.Flags().Lookup("firmware-version"))
	_ = viper.BindPFlag("firmware_file", cmd.Flags().Lookup("firmware-file"))

	root.AddCommand(cmd)
	return cmd
}

// --- Server state ---

var srvClient *http.Client

// cache entry with expiry

type cacheEntry struct {
	data      []nhdc.Item
	expiresAt time.Time
}

var (
	cacheMu sync.Mutex
	cache   = map[string]cacheEntry{}
)

func cacheKey(search, prefer string) string {
	return strings.ToLower(strings.TrimSpace(search)) + "\n" + strings.ToLower(strings.TrimSpace(prefer))
}

func getCached(search, prefer string) ([]nhdc.Item, bool) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	k := cacheKey(search, prefer)
	ce, ok := cache[k]
	if !ok || time.Now().After(ce.expiresAt) {
		return nil, false
	}
	// Treat empty cached data as a miss to avoid persisting bad/empty upstream states
	if len(ce.data) == 0 {
		return nil, false
	}
	// Return a shallow copy to avoid mutation
	out := append([]nhdc.Item(nil), ce.data...)
	return out, true
}

func putCached(search, prefer string, data []nhdc.Item, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	// Do not cache empty schedules to avoid returning persistent empty arrays when upstream is flaky
	if len(data) == 0 {
		return
	}
	cacheMu.Lock()
	defer cacheMu.Unlock()
	k := cacheKey(search, prefer)
	cache[k] = cacheEntry{data: append([]nhdc.Item(nil), data...), expiresAt: time.Now().Add(ttl)}
}

// --- Simple token-bucket rate limiter (no external deps) ---

type tokenBucket struct {
	mu     sync.Mutex
	rate   float64   // tokens per second
	burst  int       // max tokens
	tokens float64   // current tokens
	last   time.Time // last refill
}

func newTokenBucket(rate float64, burst int) *tokenBucket {
	if rate <= 0 {
		rate = 1
	}
	if burst <= 0 {
		burst = 1
	}
	return &tokenBucket{rate: rate, burst: burst, tokens: float64(burst), last: time.Now()}
}

// wait blocks until there is a token available.
func (tb *tokenBucket) wait(ctx context.Context) error {
	for {
		tb.mu.Lock()
		now := time.Now()
		dt := now.Sub(tb.last).Seconds()
		if dt > 0 {
			tb.tokens += dt * tb.rate
			if tb.tokens > float64(tb.burst) {
				tb.tokens = float64(tb.burst)
			}
			tb.last = now
		}
		if tb.tokens >= 1 {
			tb.tokens -= 1
			tb.mu.Unlock()
			return nil
		}
		// Need to wait for more tokens
		// Compute time to next token
		need := 1 - tb.tokens
		sec := need / tb.rate
		tb.mu.Unlock()

		select {
		case <-time.After(time.Duration(sec*1000) * time.Millisecond):
			// loop
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

var upstreamLimiter *tokenBucket

// --- Address helpers ---

func computeListenAddr() string {
	addr := strings.TrimSpace(viper.GetString("addr"))
	if addr == "" {
		addr = "127.0.0.1"
	}
	port := viper.GetInt("port")
	if port < 0 {
		port = 0
	}
	// If addr already has a port, override it when port is provided
	if h, _, err := net.SplitHostPort(addr); err == nil {
		addr = h
	}
	return net.JoinHostPort(addr, strconv.Itoa(port))
}

// --- HTTP handlers ---

type statusRecorder struct {
	http.ResponseWriter
	status int
	size   int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

func (sr *statusRecorder) Write(b []byte) (int, error) {
	if sr.status == 0 {
		// Default status is 200 if Write is called first
		sr.status = http.StatusOK
	}
	n, err := sr.ResponseWriter.Write(b)
	sr.size += n
	return n, err
}

func clientIP(r *http.Request) string {
	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		// Take first IP
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xr := strings.TrimSpace(r.Header.Get("X-Real-IP")); xr != "" {
		return xr
	}
	// Fallback RemoteAddr host part
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return h
	}
	return r.RemoteAddr
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		dur := time.Since(start)
		log.Printf("%s %s %d %dB ip=%s ua=%q dur=%s", r.Method, r.URL.RequestURI(), rec.status, rec.size, clientIP(r), r.UserAgent(), dur)
	})
}

func runServer() error {
	// Shared HTTP client
	srvClient = nhdc.NewClient()

	// Rate limiter
	rps := viper.GetFloat64("rate_rps")
	burst := viper.GetInt("rate_burst")
	upstreamLimiter = newTokenBucket(rps, burst)

	mux := http.NewServeMux()
	// Protect schedule endpoints with API key auth (if configured)
	mux.Handle("/", withAuth(http.HandlerFunc(scheduleHandler)))
	mux.Handle("/schedule", withAuth(http.HandlerFunc(scheduleHandler)))
	// Health is public
	mux.HandleFunc("/healthz", healthHandler)

	// OTA firmware endpoints (public)
	if viper.GetBool("firmware_enabled") {
		mux.HandleFunc("/firmware/version.txt", firmwareVersionHandler)
		mux.HandleFunc("/firmware/bindicatwo_firmware.bin", firmwareBinaryHandler)
	}

	h := withLogging(withCORS(mux))

	srv := &http.Server{
		Handler:      h,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Resolve address and bind explicitly so we can print actual chosen port and improve errors
	addr := computeListenAddr()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s failed: %w. Try a different --port (e.g. 18080) or set ADDR/PORT.", addr, err)
	}
	defer func(ln net.Listener) {
		err := ln.Close()
		if err != nil {
			_, err := fmt.Fprintf(os.Stderr, "error closing listener: %v\n", err)
			if err != nil {
				return
			}
		}
	}(ln)

	// Graceful shutdown
	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
		sig := <-ch
		log.Printf("shutdown signal received: %v", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("graceful shutdown error: %v", err)
		}
	}()

	log.Printf("Listening on %s", ln.Addr().String())
	return srv.Serve(ln)
}

func scheduleHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	ctx := r.Context()
	q := r.URL.Query()
	// Accept a few aliases for convenience
	search := strings.TrimSpace(q.Get("search"))
	if search == "" {
		for _, k := range []string{"q", "address", "addr", "postcode", "pc", "s"} {
			if v := strings.TrimSpace(q.Get(k)); v != "" {
				search = v
				break
			}
		}
	}
	prefer := strings.TrimSpace(q.Get("prefer"))
	if prefer == "" {
		for _, k := range []string{"contains", "prefer_contains", "preferContains"} {
			if v := strings.TrimSpace(q.Get(k)); v != "" {
				prefer = v
				break
			}
		}
	}
	if search == "" {
		log.Printf("schedule: missing search param ip=%s", clientIP(r))
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "missing 'search' query parameter"})
		return
	}

	if data, ok := getCached(search, prefer); ok {
		log.Printf("schedule: cache hit search=%q prefer=%q", search, prefer)
		items := append([]nhdc.Item(nil), data...)
		nhdc.ComputeRelativeFields(items)
		_ = json.NewEncoder(w).Encode(items)
		return
	}
	log.Printf("schedule: cache miss search=%q prefer=%q", search, prefer)

	// Throttle upstream calls
	if err := upstreamLimiter.wait(ctx); err != nil {
		log.Printf("schedule: rate limited search=%q prefer=%q err=%v", search, prefer, err)
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "rate limited", "detail": err.Error()})
		return
	}

	start := time.Now()
	items, err := nhdc.GetSchedule(srvClient, search, prefer)
	if err != nil {
		log.Printf("schedule: upstream error search=%q prefer=%q dur=%s err=%v", search, prefer, time.Since(start), err)
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "upstream error", "detail": err.Error()})
		return
	}
	log.Printf("schedule: upstream ok search=%q prefer=%q dur=%s items=%d", search, prefer, time.Since(start), len(items))
	nhdc.ComputeRelativeFields(items)
	putCached(search, prefer, items, viper.GetDuration("cache_ttl"))
	_ = json.NewEncoder(w).Encode(items)
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
	log.Printf("healthz ok ip=%s", clientIP(r))
}

// --- OTA Handlers ---

func firmwareVersionHandler(w http.ResponseWriter, r *http.Request) {
	ver := strings.TrimSpace(viper.GetString("firmware_version"))
	if ver == "" {
		log.Printf("firmware version requested but not configured ip=%s", clientIP(r))
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("version not configured\n"))
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	// Ensure trailing newline per docs example
	if !strings.HasSuffix(ver, "\n") {
		ver += "\n"
	}
	_, _ = w.Write([]byte(ver))
	log.Printf("firmware version served ip=%s", clientIP(r))
}

func firmwareBinaryHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(viper.GetString("firmware_file"))
	if path == "" {
		log.Printf("firmware binary requested but not configured ip=%s", clientIP(r))
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("firmware file not configured\n"))
		return
	}
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		log.Printf("firmware binary not found path=%q ip=%s", path, clientIP(r))
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("firmware not found\n"))
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=\"bindicatwo_firmware.bin\"")
	w.Header().Set("Cache-Control", "public, max-age=60")
	log.Printf("serving firmware binary path=%q size=%d ip=%s", path, fi.Size(), clientIP(r))
	http.ServeFile(w, r, path)
}

// --- Auth middleware ---

func getConfiguredAPIKeys() []string {
	keys := []string{}
	// Gather from list
	if v := viper.Get("api_keys"); v != nil {
		switch t := v.(type) {
		case []string:
			for _, k := range t {
				k = strings.TrimSpace(k)
				if k != "" {
					keys = append(keys, k)
				}
			}
		case []any:
			for _, it := range t {
				if s, ok := it.(string); ok {
					s = strings.TrimSpace(s)
					if s != "" {
						keys = append(keys, s)
					}
				}
			}
		case string:
			// Support CSV in env var API_KEYS
			csv := strings.TrimSpace(t)
			if csv != "" {
				for _, part := range strings.Split(csv, ",") {
					p := strings.TrimSpace(part)
					if p != "" {
						keys = append(keys, p)
					}
				}
			}
		}
	}
	// Single api_key
	if s := strings.TrimSpace(viper.GetString("api_key")); s != "" {
		keys = append(keys, s)
	}
	// Deduplicate
	if len(keys) > 1 {
		seen := map[string]struct{}{}
		out := make([]string, 0, len(keys))
		for _, k := range keys {
			if _, ok := seen[k]; !ok {
				seen[k] = struct{}{}
				out = append(out, k)
			}
		}
		keys = out
	}
	return keys
}

func withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keys := getConfiguredAPIKeys()
		if len(keys) == 0 {
			// Auth disabled when no keys configured
			log.Printf("auth: disabled; allowing %s %s ip=%s", r.Method, r.URL.Path, clientIP(r))
			next.ServeHTTP(w, r)
			return
		}
		// Extract key from headers
		key := strings.TrimSpace(r.Header.Get("X-API-Key"))
		if key == "" {
			// Also support Authorization: Bearer <key>
			authz := strings.TrimSpace(r.Header.Get("Authorization"))
			const pref = "Bearer "
			if strings.HasPrefix(strings.ToLower(authz), strings.ToLower(pref)) {
				key = strings.TrimSpace(authz[len(pref):])
			}
		}
		if key == "" {
			// Also support query parameters: api_key or apikey
			q := r.URL.Query()
			key = strings.TrimSpace(q.Get("api_key"))
			if key == "" {
				key = strings.TrimSpace(q.Get("apikey"))
			}
		}
		if key == "" {
			log.Printf("auth: missing API key ip=%s path=%s", clientIP(r), r.URL.Path)
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "missing API key", "hint": "provide X-API-Key header, Authorization: Bearer <key>, or ?api_key=..."})
			return
		}
		for _, k := range keys {
			if subtleConstantTimeEq(key, k) {
				log.Printf("auth: ok ip=%s path=%s", clientIP(r), r.URL.Path)
				next.ServeHTTP(w, r)
				return
			}
		}
		log.Printf("auth: invalid API key ip=%s path=%s", clientIP(r), r.URL.Path)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid API key"})
	})
}

// constant-time string equality to avoid timing leaks (simple)
func subtleConstantTimeEq(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

func withCORS(next http.Handler) http.Handler {
	allowed := strings.TrimSpace(viper.GetString("cors"))
	if allowed == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Vary", "Origin")
		origin := r.Header.Get("Origin")
		if allowed == "*" && origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		} else if origin != "" {
			// CSV list
			ok := false
			parts := strings.Split(allowed, ",")
			for i := range parts {
				parts[i] = strings.TrimSpace(parts[i])
			}
			sort.Strings(parts)
			for _, p := range parts {
				if strings.EqualFold(p, origin) {
					ok = true
					break
				}
			}
			if ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
		}
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
