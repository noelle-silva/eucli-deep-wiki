package repoindex

import (
	"errors"
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

	if _, err := Load(path); !errors.Is(err, ErrInvalidURL) {
		t.Fatalf("Load() error = %v, want ErrInvalidURL", err)
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

	if _, err := FromURL("https://github.com/CherryHQ/cherry-studio.git?ref=main"); !errors.Is(err, ErrInvalidURL) {
		t.Fatalf("FromURL() error = %v, want ErrInvalidURL", err)
	}
}

// 地址、名字、分支、档位四类不合法各有自己的标识：调用方据此区分「哪里不对」。
func TestValidationFailuresHaveOwnIdentity(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		index    Index
		expected error
	}{
		{
			name:     "地址不是 https",
			index:    Index{Repositories: []Repository{{Name: "example", URL: "ssh://github.com/example/example.git"}}},
			expected: ErrInvalidURL,
		},
		{
			name:     "名字不是单层目录名",
			index:    Index{Repositories: []Repository{{Name: "../outside", URL: "https://github.com/example/example.git"}}},
			expected: ErrInvalidName,
		},
		{
			name:     "分支带空白",
			index:    Index{Repositories: []Repository{{Name: "example", URL: "https://github.com/example/example.git", Branch: " main"}}},
			expected: ErrInvalidBranch,
		},
		{
			name:     "档位不认识",
			index:    Index{Repositories: []Repository{{Name: "example", URL: "https://github.com/example/example.git", History: "deep"}}},
			expected: ErrInvalidHistory,
		},
		{
			name: "名字重复",
			index: Index{Repositories: []Repository{
				{Name: "example", URL: "https://github.com/example/one.git"},
				{Name: "Example", URL: "https://github.com/example/two.git"},
			}},
			expected: ErrDuplicateName,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "repositories.json")
			if err := testCase.index.Save(path); !errors.Is(err, testCase.expected) {
				t.Fatalf("Save() error = %v, want %v", err, testCase.expected)
			}

			// 校验失败的那一份一个字都不落盘：旧名单保持原样。
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("校验失败不该写出文件: %v", err)
			}
		})
	}
}

func TestSaveWritesLoadableIndex(t *testing.T) {
	t.Parallel()

	index := &Index{Repositories: []Repository{
		{Name: "example", URL: "https://github.com/example/example.git"},
		{Name: "cherry-studio", URL: "https://github.com/CherryHQ/cherry-studio.git", Branch: "main", History: HistoryFull},
	}}

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

func TestLoadAcceptsUTF8BOM(t *testing.T) {
	t.Parallel()

	body := append([]byte{0xEF, 0xBB, 0xBF}, []byte(`{"repositories":[{"name":"example","url":"https://github.com/example/example.git"}]}`)...)
	path := filepath.Join(t.TempDir(), "repositories.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write index: %v", err)
	}

	index, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(index.Repositories) != 1 {
		t.Fatalf("Repositories = %#v", index.Repositories)
	}
}
