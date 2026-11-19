// Package repoindex 维护参考仓库清单：名称、远端地址与固定分支。
//
// 清单是可重建的运行数据，保存在 data 目录下，不参与源码版本管理。
// 索引文件缺失视为空清单，这样首次启动无需人工预置文件。
package repoindex

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// 名单校验的四类失败各有自己的标识。
//
// 名单是「名单里声明的地址行不行」这件事的唯一裁判：改配置、走接口、写文件
// 最终都落到这几个判断上。调用方按这些标识区分「哪里不对」，而不是解析文字；
// 对外用哪个错误码由接口层决定，本包只回答「哪一类不合法」。
var (
	// ErrInvalidURL 表示地址本身不可用：不是 https 地址、带查询串或锚点、
	// 取不出仓库名。
	ErrInvalidURL = errors.New("repository url is invalid")
	// ErrInvalidName 表示由地址推导出的名字不是合法的单层目录名。
	ErrInvalidName = errors.New("repository name is invalid")
	// ErrInvalidBranch 表示分支名带空白字符。
	ErrInvalidBranch = errors.New("repository branch is invalid")
	// ErrInvalidHistory 表示历史档位不是 shallow 或 full。
	ErrInvalidHistory = errors.New("repository history mode is invalid")
	// ErrDuplicateName 表示与名单里已有的项重名（忽略大小写）。
	ErrDuplicateName = errors.New("repository name is duplicated")
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
	if err := json.Unmarshal(stripBOM(data), &idx); err != nil {
		return nil, err
	}
	if err := idx.validate(); err != nil {
		return nil, err
	}
	return &idx, nil
}

// stripBOM 去掉 UTF-8 BOM，避免带 BOM 的编辑结果导致解析失败。
func stripBOM(data []byte) []byte {
	return bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
}

// Save 校验后写回索引文件。
//
// 先写临时文件再改名：半份名单比没有名单更糟——它会让服务端把「还没写进去的
// 仓库」当成「已经从名单里删掉」。落盘顺序与状态文件保持一致。
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

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o644); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

// FromURL 由 Git 链接推导出仓库记录，名称取链接末段的仓库名。
func FromURL(rawURL string) (Repository, error) {
	cleanURL := strings.TrimSpace(rawURL)
	if cleanURL == "" {
		return Repository{}, fmt.Errorf("%w: url is required", ErrInvalidURL)
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
		return "", fmt.Errorf("%w: %q 不是合法的地址: %v", ErrInvalidURL, rawURL, err)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("%w: %q 不得带查询串或锚点", ErrInvalidURL, rawURL)
	}

	name := path.Base(strings.TrimSuffix(parsed.Path, "/"))
	name = strings.TrimSuffix(name, ".git")
	if name == "." || name == "/" || name == "" {
		return "", fmt.Errorf("%w: %q 里取不出仓库名", ErrInvalidURL, rawURL)
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
			return fmt.Errorf("%w: repositories[%d]: duplicate name %q", ErrDuplicateName, idx, repo.Name)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validateRepository(repo Repository) error {
	name := strings.TrimSpace(repo.Name)
	if name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidName)
	}
	if !filepath.IsLocal(name) || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("%w: name %q must be a single local directory name", ErrInvalidName, repo.Name)
	}

	parsed, err := url.Parse(repo.URL)
	if err != nil {
		return fmt.Errorf("%w: %q is not a valid url: %v", ErrInvalidURL, repo.URL, err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("%w: url %q must be an https URL", ErrInvalidURL, repo.URL)
	}

	if strings.TrimSpace(repo.Branch) != repo.Branch {
		return fmt.Errorf("%w: branch %q must not have surrounding whitespace", ErrInvalidBranch, repo.Branch)
	}
	if strings.ContainsAny(repo.Branch, " \t\r\n") {
		return fmt.Errorf("%w: branch %q must not contain whitespace", ErrInvalidBranch, repo.Branch)
	}

	switch repo.History {
	case "", HistoryShallow, HistoryFull:
	default:
		return fmt.Errorf("%w: history %q must be %q or %q", ErrInvalidHistory, repo.History, HistoryShallow, HistoryFull)
	}
	return nil
}
