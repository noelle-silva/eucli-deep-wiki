// Package repoindex 维护参考仓库清单：名称、远端地址与固定分支。
//
// 清单是可重建的运行数据，保存在 data 目录下，不参与源码版本管理。
// 索引文件缺失视为空清单，这样首次启动无需人工预置文件。
package repoindex

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Repository 是一条参考仓库记录。
type Repository struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Branch  string `json:"branch,omitempty"`
	History string `json:"history,omitempty"`
}

// 允许的历史档位。
const (
	HistoryShallow = "shallow"
	HistoryFull    = "full"
)

// EffectiveHistory 返回该仓库实际采用的历史档位。
func (r Repository) EffectiveHistory(defaultMode string) string {
	if r.History != "" {
		return r.History
	}
	if defaultMode != "" {
		return defaultMode
	}
	return HistoryShallow
}

// Index 是仓库清单的持久化形态。
type Index struct {
	Repositories []Repository `json:"repositories"`
}

// Load 读取索引文件；文件不存在时返回空清单。
func Load(path string) (*Index, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Index{}, nil
	}
	if err != nil {
		return nil, err
	}

	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, err
	}
	if err := idx.validate(); err != nil {
		return nil, err
	}
	return &idx, nil
}

// Save 校验后写回索引文件。
func (i *Index) Save(path string) error {
	if i == nil {
		return errors.New("index is required")
	}
	if err := i.validate(); err != nil {
		return err
	}

	payload := *i
	if payload.Repositories == nil {
		payload.Repositories = []Repository{}
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

// AddURLs 追加若干 Git 链接，任一条不合法时整体不生效。
func (i *Index) AddURLs(rawURLs []string) ([]Repository, error) {
	if i == nil {
		return nil, errors.New("index is required")
	}
	if len(rawURLs) == 0 {
		return nil, errors.New("at least one repository URL is required")
	}

	additions := make([]Repository, 0, len(rawURLs))
	for _, rawURL := range rawURLs {
		repo, err := FromURL(rawURL)
		if err != nil {
			return nil, err
		}
		additions = append(additions, repo)
	}

	next := Index{Repositories: append(append([]Repository(nil), i.Repositories...), additions...)}
	if err := next.validate(); err != nil {
		return nil, err
	}
	*i = next
	return additions, nil
}

// FromURL 由 Git 链接推导出仓库记录，名称取链接末段的仓库名。
func FromURL(rawURL string) (Repository, error) {
	cleanURL := strings.TrimSpace(rawURL)
	if cleanURL == "" {
		return Repository{}, errors.New("url is required")
	}

	name, err := nameFromURL(cleanURL)
	if err != nil {
		return Repository{}, err
	}
	repo := Repository{Name: name, URL: cleanURL}
	if err := validateRepository(repo); err != nil {
		return Repository{}, err
	}
	return repo, nil
}

func nameFromURL(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("url is invalid: %w", err)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("url %q must not contain query or fragment", rawURL)
	}

	name := path.Base(strings.TrimSuffix(parsed.Path, "/"))
	name = strings.TrimSuffix(name, ".git")
	if name == "." || name == "/" || name == "" {
		return "", fmt.Errorf("url %q does not contain a repository name", rawURL)
	}
	return name, nil
}

func (i *Index) validate() error {
	seen := make(map[string]struct{}, len(i.Repositories))
	for idx, repo := range i.Repositories {
		if err := validateRepository(repo); err != nil {
			return fmt.Errorf("repositories[%d]: %w", idx, err)
		}
		key := strings.ToLower(repo.Name)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("repositories[%d]: duplicate name %q", idx, repo.Name)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validateRepository(repo Repository) error {
	name := strings.TrimSpace(repo.Name)
	if name == "" {
		return errors.New("name is required")
	}
	if !filepath.IsLocal(name) || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("name %q must be a single local directory name", repo.Name)
	}

	parsed, err := url.Parse(repo.URL)
	if err != nil {
		return fmt.Errorf("url is invalid: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("url %q must be an https URL", repo.URL)
	}

	if strings.TrimSpace(repo.Branch) != repo.Branch {
		return fmt.Errorf("branch %q must not have surrounding whitespace", repo.Branch)
	}
	if strings.ContainsAny(repo.Branch, " \t\r\n") {
		return fmt.Errorf("branch %q must not contain whitespace", repo.Branch)
	}

	switch repo.History {
	case "", HistoryShallow, HistoryFull:
	default:
		return fmt.Errorf("history %q must be %q or %q", repo.History, HistoryShallow, HistoryFull)
	}
	return nil
}
