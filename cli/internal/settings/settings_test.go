package settings

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePathPrefersExplicitValue(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	explicit := filepath.Join(dir, "custom.json")

	resolved, err := ResolvePath(explicit)
	if err != nil {
		t.Fatalf("ResolvePath() error = %v", err)
	}
	if resolved != explicit {
		t.Fatalf("ResolvePath() = %q, want %q", resolved, explicit)
	}
}

func TestResolvePathUsesEnvVariable(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvConfigPath, dir)

	resolved, err := ResolvePath("")
	if err != nil {
		t.Fatalf("ResolvePath() error = %v", err)
	}
	if resolved != filepath.Join(dir, FileName) {
		t.Fatalf("ResolvePath() = %q", resolved)
	}
}

func TestResolvePathUsesEnvFileDirectly(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "eucli.json")
	if err := os.WriteFile(file, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	t.Setenv(EnvConfigPath, file)

	resolved, err := ResolvePath("")
	if err != nil {
		t.Fatalf("ResolvePath() error = %v", err)
	}
	if resolved != file {
		t.Fatalf("ResolvePath() = %q, want %q", resolved, file)
	}
}

func TestResolvePathFallsBackToPlatformDefault(t *testing.T) {
	t.Setenv(EnvConfigPath, "")

	resolved, err := ResolvePath("")
	if err != nil {
		t.Fatalf("ResolvePath() error = %v", err)
	}
	if !filepath.IsAbs(resolved) {
		t.Fatalf("ResolvePath() = %q, want absolute path", resolved)
	}
	if filepath.Base(resolved) != FileName {
		t.Fatalf("ResolvePath() = %q, want %s", resolved, FileName)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nested", FileName)
	original := &Settings{Server: "http://127.0.0.1:9997", APIKey: "0123456789abcdef"}

	if err := original.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if *loaded != *original {
		t.Fatalf("loaded = %#v, want %#v", loaded, original)
	}
}

func TestLoadReportsMissingFile(t *testing.T) {
	t.Parallel()

	if _, err := Load(filepath.Join(t.TempDir(), FileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Load() error = %v, want os.ErrNotExist", err)
	}
}

func TestValidateServer(t *testing.T) {
	t.Parallel()

	valid := []string{"http://127.0.0.1:9997", "https://example.com"}
	for _, value := range valid {
		if err := ValidateServer(value); err != nil {
			t.Fatalf("ValidateServer(%q) error = %v", value, err)
		}
	}

	invalid := []string{"", "127.0.0.1:9997", "ftp://example.com", "http://"}
	for _, value := range invalid {
		if err := ValidateServer(value); err == nil {
			t.Fatalf("ValidateServer(%q) error = nil, want error", value)
		}
	}
}

func TestValidateAPIKey(t *testing.T) {
	t.Parallel()

	if err := ValidateAPIKey("0123456789abcdef"); err != nil {
		t.Fatalf("ValidateAPIKey() error = %v", err)
	}
	for _, value := range []string{"", "  key  ", "has space"} {
		if err := ValidateAPIKey(value); err == nil {
			t.Fatalf("ValidateAPIKey(%q) error = nil, want error", value)
		}
	}
}

func TestEnsureReadyRequiresBothFields(t *testing.T) {
	t.Parallel()

	partial := &Settings{Server: "http://127.0.0.1:9997"}
	if err := partial.EnsureReady(); err == nil {
		t.Fatal("EnsureReady() error = nil, want missing key error")
	}

	complete := &Settings{Server: "http://127.0.0.1:9997", APIKey: "0123456789abcdef"}
	if err := complete.EnsureReady(); err != nil {
		t.Fatalf("EnsureReady() error = %v", err)
	}
}
