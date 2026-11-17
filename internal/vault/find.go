package vault

import (
	"errors"
	"io/fs"
	"path"
	"strings"
)

// FoundEntry 是一个按文件名命中的文件。
type FoundEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"`
	Size int64  `json:"size"`
}

// FindResult 是一次文件名检索的结果。
type FindResult struct {
	Repo      string       `json:"repo"`
	Revision  string       `json:"revision"`
	Pattern   string       `json:"pattern"`
	Path      string       `json:"path,omitempty"`
	Offset    int          `json:"offset"`
	Entries   []FoundEntry `json:"entries"`
	Scanned   int          `json:"scannedFiles"`
	Truncated bool         `json:"truncated"`
}

// FindOptions 描述一次文件名检索。
type FindOptions struct {
	Pattern string
	Path    string
	Limit   int
	Offset  int
}

const (
	defaultFindLimit = 200
	maxFindLimit     = 1000
)

// Find 只按文件名匹配，不读文件内容，因此比内容检索快得多。
// 带通配符（* ? [）时按 glob 匹配，否则按子串匹配；两者都不区分大小写。
func (v *Vault) Find(name string, options FindOptions) (FindResult, error) {
	pattern := strings.TrimSpace(options.Pattern)
	if pattern == "" {
		return FindResult{}, ErrInvalidName
	}

	rel, err := cleanPath(options.Path)
	if err != nil {
		return FindResult{}, err
	}

	limit := options.Limit
	if limit <= 0 {
		limit = defaultFindLimit
	}
	if limit > maxFindLimit {
		limit = maxFindLimit
	}

	root, err := v.open(name)
	if err != nil {
		return FindResult{}, err
	}
	defer root.Close()

	if _, err := root.Stat(rel); err != nil {
		return FindResult{}, describe(err)
	}

	matcher := buildNameMatcher(pattern)
	offset := options.Offset
	if offset < 0 {
		offset = 0
	}

	result := FindResult{
		Repo:     name,
		Revision: readMetaRevision(name, v.repos[name]),
		Pattern:  pattern,
		Path:     options.Path,
		Offset:   offset,
		Entries:  []FoundEntry{},
	}

	matched := 0
	walkErr := fs.WalkDir(root.FS(), rel, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}

		result.Scanned++
		if !matcher(entry.Name()) {
			return nil
		}

		matched++
		if matched <= offset {
			return nil
		}
		if len(result.Entries) >= limit {
			result.Truncated = true
			return fs.SkipAll
		}

		info, err := entry.Info()
		if err != nil {
			return nil
		}
		result.Entries = append(result.Entries, FoundEntry{
			Name: entry.Name(),
			Path: current,
			Type: "file",
			Size: info.Size(),
		})
		return nil
	})

	if walkErr != nil && !errors.Is(walkErr, fs.SkipAll) {
		return FindResult{}, walkErr
	}
	return result, nil
}

// buildNameMatcher 根据是否含通配符选择匹配方式，统一按小写比较。
func buildNameMatcher(pattern string) func(string) bool {
	lowered := strings.ToLower(pattern)
	if strings.ContainsAny(lowered, "*?[") {
		return func(candidate string) bool {
			matched, err := path.Match(lowered, strings.ToLower(candidate))
			return err == nil && matched
		}
	}
	return func(candidate string) bool {
		return strings.Contains(strings.ToLower(candidate), lowered)
	}
}
