package config

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// helper to set a temporary HOME / USERPROFILE so defaultConfigDir points inside temp
func withTempHome(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	oldHome := os.Getenv("HOME")
	oldUser := os.Getenv("USERPROFILE")
	os.Setenv("HOME", d)
	os.Setenv("USERPROFILE", d)
	t.Cleanup(func() {
		os.Setenv("HOME", oldHome)
		os.Setenv("USERPROFILE", oldUser)
	})
	return d
}

// helper to replace stdin with provided input using a temporary *os.File
func withStdin(t *testing.T, input string) {
	f, err := os.CreateTemp(t.TempDir(), "stdin-*.txt")
	if err != nil {
		t.Fatalf("create temp stdin file: %v", err)
	}
	if _, err = f.WriteString(input); err != nil {
		t.Fatalf("write temp stdin file: %v", err)
	}
	if _, err = f.Seek(0, 0); err != nil {
		t.Fatalf("seek temp stdin file: %v", err)
	}
	old := os.Stdin
	os.Stdin = f
	// restore
	t.Cleanup(func() {
		os.Stdin = old
		f.Close()
	})
}

func TestDefaultConfigDir(t *testing.T) {
	withTempHome(t)
	d := defaultConfigDir()
	if !strings.Contains(d, "bindicatwo") {
		t.Fatalf("expected bindicatwo in path, got %s", d)
	}
}

func TestDefaultConfigBaseName(t *testing.T) {
	if defaultConfigBaseName() != "config" {
		t.Fatalf("expected config")
	}
}

func TestDefaultConfigPathWithExt(t *testing.T) {
	withTempHome(t)
	p := defaultConfigPathWithExt("json")
	if !strings.HasSuffix(p, ".json") || !strings.Contains(p, "config") {
		t.Fatalf("unexpected path: %s", p)
	}
	p2 := defaultConfigPathWithExt("")
	if !strings.HasSuffix(p2, ".yaml") {
		t.Fatalf("empty ext should default to yaml: %s", p2)
	}
}

func TestConfigTypeFromExt(t *testing.T) {
	cases := map[string]string{
		"/x/a.yaml": "yaml",
		"/x/a.yml":  "yaml",
		"/x/a.toml": "toml",
		"/x/a.json": "json",
		"/x/a":      "yaml",
	}
	for path, want := range cases {
		if got := configTypeFromExt(path); got != want {
			t.Fatalf("configTypeFromExt(%s)=%s want %s", path, got, want)
		}
	}
}

func TestExtFromFormat(t *testing.T) {
	cases := map[string]string{
		"yaml": "yaml",
		"yml":  "yaml",
		"json": "json",
		"toml": "toml",
		"":     "yaml",
		"WHAT": "yaml",
	}
	for f, want := range cases {
		if got := extFromFormat(f); got != want {
			t.Fatalf("extFromFormat(%s)=%s want %s", f, got, want)
		}
	}
}

func TestFileExists(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a.txt")
	if fileExists(f) { // should be false
		t.Fatalf("expected false before write")
	}
	if err := os.WriteFile(f, []byte("hi"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !fileExists(f) {
		t.Fatalf("expected true after write")
	}
	if fileExists("") {
		t.Fatalf("empty path should be false")
	}
}

func TestNormalizeKey(t *testing.T) {
	if normalizeKey("UPRN") != "uprn" {
		t.Fatal("UPRN -> uprn")
	}
	if normalizeKey(" json ") != "json" {
		t.Fatal("json trim")
	}
	if normalizeKey("firmware_version") != "firmware_version" {
		t.Fatal("firmware_version")
	}
}

func TestGenerateAPIKey(t *testing.T) {
	k, err := generateAPIKey(16)
	if err != nil || len(k) != 32 {
		t.Fatalf("unexpected key %s err=%v", k, err)
	}
	k2, _ := generateAPIKey(16)
	if k == k2 {
		t.Fatalf("keys should differ")
	}
}

func TestGetAPIKeysFromViperVariants(t *testing.T) {
	viper.Reset()
	viper.Set("api_key", "solo")
	viper.Set("api_keys", []string{"a", "b", "a"})
	keys := getAPIKeysFromViper()
	// Expect dedup plus solo -> a,b,solo (order stable-ish but not guaranteed; check presence & length)
	if len(keys) != 3 {
		t.Fatalf("want 3 keys, got %v", keys)
	}
	seen := map[string]bool{}
	for _, k := range keys {
		seen[k] = true
	}
	for _, want := range []string{"a", "b", "solo"} {
		if !seen[want] {
			t.Fatalf("missing %s", want)
		}
	}
}

func TestSetAPIKeys(t *testing.T) {
	viper.Reset()
	// point config file to temp path so writeBack works
	cfg := filepath.Join(t.TempDir(), "config.yaml")
	viper.SetConfigFile(cfg)
	viper.SetConfigType("yaml")
	if err := viper.WriteConfigAs(cfg); err != nil {
		t.Fatalf("init cfg: %v", err)
	}
	if err := setAPIKeys([]string{"x", "", "y"}); err != nil {
		t.Fatalf("setAPIKeys: %v", err)
	}
	keys := getAPIKeysFromViper()
	if len(keys) != 2 {
		t.Fatalf("expected 2 clean keys, got %v", keys)
	}
	if viper.GetString("api_key") != "" {
		t.Fatalf("api_key should be cleared")
	}
}

func TestEnsureConfigTargetAndConfigPath(t *testing.T) {
	withTempHome(t)
	viper.Reset()
	if err := ensureConfigTarget("yaml"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	p := configPath()
	if !strings.HasSuffix(p, ".yaml") {
		t.Fatalf("configPath suffix: %s", p)
	}
}

func TestEnsureConfigTargetInvalid(t *testing.T) {
	viper.Reset()
	if err := ensureConfigTarget("badfmt"); err == nil {
		t.Fatalf("expected error for badfmt")
	}
}

func TestWriteBackAndRequireConfigFile(t *testing.T) {
	withTempHome(t)
	viper.Reset()
	// require creates file
	if err := requireConfigFile(); err != nil {
		t.Fatalf("require: %v", err)
	}
	p := viper.ConfigFileUsed()
	if p == "" {
		t.Fatalf("ConfigFileUsed empty")
	}
	viper.Set("uprn", "123")
	if err := writeBack(); err != nil {
		t.Fatalf("writeBack: %v", err)
	}
	v := viper.GetString("uprn")
	if v != "123" {
		t.Fatalf("uprn mismatch got %s", v)
	}
}

func TestDeleteKey(t *testing.T) {
	viper.Reset()
	viper.Set("uprn", "abc")
	viper.Set("json", true)
	if err := deleteKey("json"); err != nil {
		t.Fatalf("deleteKey: %v", err)
	}
	// Key may still appear in AllSettings but value should not be true
	if viper.GetBool("json") {
		t.Fatalf("json should not be true after delete")
	}
}

func TestDeleteKeyEmpty(t *testing.T) {
	if err := deleteKey(""); err == nil {
		t.Fatalf("expected error")
	}
}

// --- Cobra command integration tests (non-interactive except init simulated) ---

func buildRoot() *cobra.Command {
	root := &cobra.Command{Use: "bindicatwo"}
	AddConfigSubcommand(root)
	return root
}

// capture runs fn while capturing stdout/stderr (fmt.Printf + cobra output)
func capture(t *testing.T, fn func()) string {
	t.Helper()
	oldOut := os.Stdout
	oldErr := os.Stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr
	fn()
	wOut.Close()
	wErr.Close()
	os.Stdout = oldOut
	os.Stderr = oldErr
	var buf bytes.Buffer
	io.Copy(&buf, rOut)
	io.Copy(&buf, rErr)
	return buf.String()
}

func TestConfigInitInteractive(t *testing.T) {
	withTempHome(t)
	viper.Reset()
	root := buildRoot()
	// Use flags to bypass interactive prompts in tests
	root.SetArgs([]string{"config", "init", "--force", "--uprn", "999999999", "--json", "--firmware-enable=false", "--firmware-version", "", "--firmware-file", ""})
	err := root.Execute()
	if err != nil {
		t.Fatalf("config init failed: %v", err)
	}
	if viper.GetString("uprn") != "999999999" {
		t.Fatalf("uprn not set, got %s", viper.GetString("uprn"))
	}
	if !viper.GetBool("json") {
		t.Fatalf("json flag not set")
	}
	// firmware_enabled should be false (not set via flag)
	if viper.GetBool("firmware_enabled") {
		t.Fatalf("firmware_enabled should be false")
	}
}

func TestConfigSetGetUnsetPath(t *testing.T) {
	withTempHome(t)
	viper.Reset()
	root := buildRoot()
	// Use flags to avoid interactive prompts
	root.SetArgs([]string{"config", "init", "--force", "--uprn", "", "--json=false", "--firmware-enable=false", "--firmware-version", "", "--firmware-file", ""})
	_ = root.Execute()
	// set
	root.SetArgs([]string{"config", "set", "uprn", "ABC"})
	if err := root.Execute(); err != nil {
		t.Fatalf("set uprn: %v", err)
	}
	// get
	buf := capture(t, func() {
		root.SetArgs([]string{"config", "get", "uprn"})
		_ = root.Execute()
	})
	if !strings.Contains(buf, "ABC") {
		t.Fatalf("expected uprn ABC, got %s", buf)
	}
	// unset
	root.SetArgs([]string{"config", "unset", "uprn"})
	if err := root.Execute(); err != nil {
		t.Fatalf("unset: %v", err)
	}
	// path
	pOut := capture(t, func() { root.SetArgs([]string{"config", "path"}); _ = root.Execute() })
	if !strings.Contains(pOut, "config.yaml") {
		t.Fatalf("expected path output got %s", pOut)
	}
}

func TestConfigAPIKeysSubcommands(t *testing.T) {
	withTempHome(t)
	viper.Reset()
	root := buildRoot()
	// Use flags to avoid interactive prompts
	root.SetArgs([]string{"config", "init", "--force", "--uprn", "", "--json=false", "--firmware-enable=false", "--firmware-version", "", "--firmware-file", ""})
	_ = root.Execute()
	root.SetArgs([]string{"config", "api-keys", "add", "k1"})
	if err := root.Execute(); err != nil {
		t.Fatalf("add k1: %v", err)
	}
	root.SetArgs([]string{"config", "api-keys", "add", "k2"})
	_ = root.Execute()
	listOut := capture(t, func() { root.SetArgs([]string{"config", "api-keys", "list"}); _ = root.Execute() })
	if !strings.Contains(listOut, "k1") || !strings.Contains(listOut, "k2") {
		t.Fatalf("list missing keys: %s", listOut)
	}
	root.SetArgs([]string{"config", "api-keys", "remove", "k1"})
	if err := root.Execute(); err != nil {
		t.Fatalf("remove k1: %v", err)
	}
	listOut2 := capture(t, func() { root.SetArgs([]string{"config", "api-keys", "list"}); _ = root.Execute() })
	if strings.Contains(listOut2, "k1") {
		t.Fatalf("k1 should be removed: %s", listOut2)
	}
	genOut := capture(t, func() { root.SetArgs([]string{"config", "api-keys", "generate", "--length", "8"}); _ = root.Execute() })
	genOut = strings.TrimSpace(genOut)
	if len(genOut) != 16 {
		t.Fatalf("expected 16 hex chars for length 8 bytes, got %s", genOut)
	}
}
