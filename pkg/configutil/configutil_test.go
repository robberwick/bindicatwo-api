package configutil

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func withTempHomeAndCwd(t *testing.T) (tmp string) {
	t.Helper()
	tmp = t.TempDir()
	oldHome := os.Getenv("HOME")
	oldUser := os.Getenv("USERPROFILE")
	oldWd, _ := os.Getwd()
	// Point both HOME and USERPROFILE for cross-platform consistency
	_ = os.Setenv("HOME", tmp)
	_ = os.Setenv("USERPROFILE", tmp)
	_ = os.Chdir(tmp)
	t.Cleanup(func() {
		_ = os.Setenv("HOME", oldHome)
		_ = os.Setenv("USERPROFILE", oldUser)
		_ = os.Chdir(oldWd)
	})
	return tmp
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writefile: %v", err)
	}
}

func TestInit_CustomYAML_Success(t *testing.T) {
	viper.Reset()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "mycfg.yaml")
	writeFile(t, cfg, "uprn: '12345'\njson: true\n")
	if err := Init(cfg); err != nil {
		t.Fatalf("Init(custom yaml): %v", err)
	}
	if viper.GetString("uprn") != "12345" {
		t.Fatalf("uprn read mismatch, got %q", viper.GetString("uprn"))
	}
	if !viper.GetBool("json") {
		t.Fatalf("json should be true")
	}
}

func TestInit_CustomJSON_Success(t *testing.T) {
	viper.Reset()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.json")
	writeFile(t, cfg, `{"uprn":"abc","json":false}`)
	if err := Init(cfg); err != nil {
		t.Fatalf("Init(custom json): %v", err)
	}
	if viper.GetString("uprn") != "abc" {
		t.Fatalf("uprn mismatch: %q", viper.GetString("uprn"))
	}
	if viper.GetBool("json") {
		t.Fatalf("json should be false")
	}
}

func TestInit_CustomMissing_ReturnsError(t *testing.T) {
	viper.Reset()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "nope.yaml")
	if err := Init(cfg); err == nil {
		t.Fatalf("expected error when custom config file does not exist")
	}
}

func TestInit_CustomInvalidYAML_Error(t *testing.T) {
	viper.Reset()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "bad.yaml")
	writeFile(t, cfg, "invalid: [ [\n")
	err := Init(cfg)
	if err == nil {
		t.Fatalf("expected error for invalid yaml")
	}
	if !strings.Contains(err.Error(), "error reading config file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInit_Default_NoFile_OK_Env(t *testing.T) {
	viper.Reset()
	withTempHomeAndCwd(t)
	_ = os.Unsetenv("BINDICATWO_UPRN")
	_ = os.Setenv("BINDICATWO_UPRN", "env-uprn")
	t.Cleanup(func() { _ = os.Unsetenv("BINDICATWO_UPRN") })
	if err := Init(""); err != nil {
		t.Fatalf("Init(default no file): %v", err)
	}
	if viper.GetString("uprn") != "env-uprn" {
		t.Fatalf("env var not read, got %q", viper.GetString("uprn"))
	}
}

func TestInit_Default_CurrentDirYAML_Success(t *testing.T) {
	viper.Reset()
	withTempHomeAndCwd(t)
	writeFile(t, filepath.Join(".", "config.yaml"), "uprn: curr\njson: true\n")
	if err := Init(""); err != nil {
		t.Fatalf("Init(default cwd yaml): %v", err)
	}
	if viper.GetString("uprn") != "curr" || !viper.GetBool("json") {
		t.Fatalf("unexpected values: uprn=%q json=%v", viper.GetString("uprn"), viper.GetBool("json"))
	}
}

func TestInit_Default_HomeDirYAML_Success(t *testing.T) {
	viper.Reset()
	tmp := withTempHomeAndCwd(t)
	homeCfg := filepath.Join(tmp, ".config", "bindicatwo", "config.yaml")
	writeFile(t, homeCfg, "uprn: home\njson: false\n")
	if err := Init(""); err != nil {
		t.Fatalf("Init(default home yaml): %v", err)
	}
	if v := viper.GetString("uprn"); v != "home" {
		t.Fatalf("uprn mismatch: %q", v)
	}
}

func TestInit_Default_InvalidYAML_Error(t *testing.T) {
	viper.Reset()
	withTempHomeAndCwd(t)
	writeFile(t, filepath.Join(".", "config.yaml"), "bad: [ [\n")
	if err := Init(""); err == nil {
		t.Fatalf("expected parse error")
	}
}

func TestInit_UserHomeDirError(t *testing.T) {
	// Try to force os.UserHomeDir to fail by unsetting env; if it doesn't fail on this OS, skip.
	if runtime.GOOS == "windows" {
		// On Windows, empty USERPROFILE usually causes error.
	}
	viper.Reset()
	oldHome := os.Getenv("HOME")
	oldUser := os.Getenv("USERPROFILE")
	_ = os.Unsetenv("HOME")
	_ = os.Unsetenv("USERPROFILE")
	t.Cleanup(func() { _ = os.Setenv("HOME", oldHome); _ = os.Setenv("USERPROFILE", oldUser) })
	err := Init("")
	if err == nil {
		// If the platform resolved a home anyway, we can't reliably assert the error.
		t.Skip("os.UserHomeDir did not error on this platform with HOME/USERPROFILE unset")
	}
}
