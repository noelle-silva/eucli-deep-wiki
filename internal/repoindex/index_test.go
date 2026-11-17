package repoindex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMissingFileReturnsEmptyIndex(t *testing.T) {
	t.Parallel()

	index, err := Load(filepath.Join(t.TempDir(), "repositories.json"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(index.Repositories) != 0 {
		t.Fatalf("Repositories = %#v", index.Repositories)
	}
}

func TestLoadRejectsUnsafeRepositoryNames(t *testing.T) {
	t.Parallel()

	path := writeIndex(t, `{
		"repositories": [
			{"name": "../outside", "url": "https://github.com/example/example.git"}
		]
	}`)

	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want unsafe name error")
	}
}

func TestLoadRejectsDuplicateNamesCaseInsensitive(t *testing.T) {
	t.Parallel()

	path := writeIndex(t, `{
		"repositories": [
			{"name": "Example", "url": "https://github.com/example/one.git"},
			{"name": "example", "url": "https://github.com/example/two.git"}
		]
	}`)

	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want duplicate name error")
	}
}

func TestLoadRejectsNonHTTPSURL(t *testing.T) {
	t.Parallel()

	path := writeIndex(t, `{
		"repositories": [
			{"name": "example", "url": "http://github.com/example/example.git"}
		]
	}`)

	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want non-https url error")
	}
}

func TestFromURLDerivesName(t *testing.T) {
	t.Parallel()

	repo, err := FromURL(" https://github.com/CherryHQ/cherry-studio.git ")
	if err != nil {
		t.Fatalf("FromURL() error = %v", err)
	}
	if repo.Name != "cherry-studio" {
		t.Fatalf("Name = %q, want cherry-studio", repo.Name)
	}
	if repo.URL != "https://github.com/CherryHQ/cherry-studio.git" {
		t.Fatalf("URL = %q", repo.URL)
	}
}

func TestFromURLRejectsUnsupportedURL(t *testing.T) {
	t.Parallel()

	if _, err := FromURL("git@github.com:CherryHQ/cherry-studio.git"); err == nil {
		t.Fatal("FromURL() error = nil, want unsupported URL error")
	}
}

func TestFromURLRejectsQuery(t *testing.T) {
	t.Parallel()

	if _, err := FromURL("https://github.com/CherryHQ/cherry-studio.git?ref=main"); err == nil {
		t.Fatal("FromURL() error = nil, want query error")
	}
}

func TestAddURLsAppendsAndValidates(t *testing.T) {
	t.Parallel()

	index := &Index{Repositories: []Repository{
		{Name: "example", URL: "https://github.com/example/example.git"},
	}}

	added, err := index.AddURLs([]string{"https://github.com/CherryHQ/cherry-studio.git"})
	if err != nil {
		t.Fatalf("AddURLs() error = %v", err)
	}
	if len(added) != 1 || added[0].Name != "cherry-studio" {
		t.Fatalf("added = %#v", added)
	}
	if len(index.Repositories) != 2 || index.Repositories[1].Name != "cherry-studio" {
		t.Fatalf("Repositories = %#v", index.Repositories)
	}
}

func TestAddURLsRejectsDuplicateWithoutMutating(t *testing.T) {
	t.Parallel()

	index := &Index{Repositories: []Repository{
		{Name: "cherry-studio", URL: "https://github.com/CherryHQ/cherry-studio.git"},
	}}

	if _, err := index.AddURLs([]string{"https://github.com/other/cherry-studio.git"}); err == nil {
		t.Fatal("AddURLs() error = nil, want duplicate error")
	}
	if len(index.Repositories) != 1 || index.Repositories[0].URL != "https://github.com/CherryHQ/cherry-studio.git" {
		t.Fatalf("Repositories mutated: %#v", index.Repositories)
	}
}

func TestSaveWritesLoadableIndex(t *testing.T) {
	t.Parallel()

	index := &Index{Repositories: []Repository{
		{Name: "example", URL: "https://github.com/example/example.git"},
	}}
	if _, err := index.AddURLs([]string{"https://github.com/CherryHQ/cherry-studio.git"}); err != nil {
		t.Fatalf("AddURLs() error = %v", err)
	}

	path := filepath.Join(t.TempDir(), "repositories.json")
	if err := index.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved index: %v", err)
	}
	if strings.Contains(string(data), "dataRoot") || strings.Contains(string(data), "httpAddress") {
		t.Fatalf("saved index contains server settings: %s", data)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load(saved) error = %v", err)
	}
	if len(loaded.Repositories) != 2 || loaded.Repositories[1].Name != "cherry-studio" {
		t.Fatalf("loaded repositories = %#v", loaded.Repositories)
	}
}

func TestSaveWritesEmptyRepositoryList(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "repositories.json")
	if err := (&Index{}).Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved index: %v", err)
	}
	if strings.Contains(string(data), "null") {
		t.Fatalf("saved index encodes null repositories: %s", data)
	}
}

func writeIndex(t *testing.T, data string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "repositories.json")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write index: %v", err)
	}
	return path
}

func TestLoadRejectsUnknownHistoryMode(t *testing.T) {
	t.Parallel()

	path := writeIndex(t, `{
		"repositories": [
			{"name": "example", "url": "https://github.com/example/example.git", "history": "partial"}
		]
	}`)

	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want unknown history error")
	}
}

func TestEffectiveHistoryFallsBackToDefault(t *testing.T) {
	t.Parallel()

	plain := Repository{Name: "a"}
	if got := plain.EffectiveHistory("shallow"); got != HistoryShallow {
		t.Fatalf("EffectiveHistory() = %q", got)
	}

	declared := Repository{Name: "b", History: HistoryFull}
	if got := declared.EffectiveHistory(HistoryShallow); got != HistoryFull {
		t.Fatalf("EffectiveHistory() = %q", got)
	}
}
