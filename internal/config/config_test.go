package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAppliesDefaultsAndParsesDurations(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, `{}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.DataRoot != DefaultDataRoot {
		t.Fatalf("DataRoot = %q", cfg.DataRoot)
	}
	if cfg.RepositoryRoot != DefaultRepositoryRoot {
		t.Fatalf("RepositoryRoot = %q", cfg.RepositoryRoot)
	}
	if cfg.RepositoryIndex != DefaultRepositoryIndex {
		t.Fatalf("RepositoryIndex = %q", cfg.RepositoryIndex)
	}
	if cfg.SyncIntervalDuration == 0 {
		t.Fatal("SyncIntervalDuration was not parsed")
	}
	if cfg.GitCommandTimeoutDuration == 0 {
		t.Fatal("GitCommandTimeoutDuration was not parsed")
	}
	if cfg.HTTPAddress != DefaultHTTPAddress {
		t.Fatalf("HTTPAddress = %q", cfg.HTTPAddress)
	}
}

func TestDerivedPathsResolveUnderDataRoot(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, `{"dataRoot": "var", "repositoryRoot": "refs", "repositoryIndex": "index.json"}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got, want := cfg.RepositoryDir(), filepath.Join("var", "refs"); got != want {
		t.Fatalf("RepositoryDir() = %q, want %q", got, want)
	}
	if got, want := cfg.RepositoryIndexPath(), filepath.Join("var", "index.json"); got != want {
		t.Fatalf("RepositoryIndexPath() = %q, want %q", got, want)
	}
}

func TestLoadRejectsEscapingDataRoot(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, `{"dataRoot": "../outside"}`)

	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want unsafe dataRoot error")
	}
}

func TestLoadRejectsNestedRepositoryRoot(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, `{"repositoryRoot": "tmp/repository"}`)

	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want nested repositoryRoot error")
	}
}

func TestLoadRejectsNestedRepositoryIndex(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, `{"repositoryIndex": "nested/index.json"}`)

	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want nested repositoryIndex error")
	}
}

func TestLoadRejectsInvalidHTTPAddress(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, `{"httpAddress": "127.0.0.1:not-a-port"}`)

	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want invalid httpAddress error")
	}
}

func TestLoadRejectsNonPositiveSyncInterval(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, `{"syncInterval": "0s"}`)

	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want non-positive syncInterval error")
	}
}

func TestSetHTTPAddressOverridesValidatedAddress(t *testing.T) {
	t.Parallel()

	cfg := &Config{}
	if err := cfg.SetHTTPAddress("0.0.0.0:9997"); err != nil {
		t.Fatalf("SetHTTPAddress() error = %v", err)
	}
	if cfg.HTTPAddress != "0.0.0.0:9997" {
		t.Fatalf("HTTPAddress = %q", cfg.HTTPAddress)
	}
}

func TestSetHTTPAddressAcceptsPortOnlyListenAddress(t *testing.T) {
	t.Parallel()

	cfg := &Config{}
	if err := cfg.SetHTTPAddress(":9997"); err != nil {
		t.Fatalf("SetHTTPAddress() error = %v", err)
	}
	if cfg.HTTPAddress != ":9997" {
		t.Fatalf("HTTPAddress = %q", cfg.HTTPAddress)
	}
}

func TestSetHTTPAddressRejectsInvalidAddress(t *testing.T) {
	t.Parallel()

	cfg := &Config{}
	if err := cfg.SetHTTPAddress("9997"); err == nil {
		t.Fatal("SetHTTPAddress() error = nil, want invalid address error")
	}
}

func writeConfig(t *testing.T, data string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
