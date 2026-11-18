package vault

import (
	"errors"
	"fmt"
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
	Repo     string       `json:"repo"`
	Revision string       `json:"revision"`
	Pattern  string       `json:"pattern"`
	Path     string       `json:"path,omitempty"`
	Offset   int          `json:"offset"`
	Entries  []FoundEntry `json:"entries"`
	Scanned  int          `json:"scannedFiles"`
	// Truncated 表示还有未返回的命中。
	Truncated bool `json:"truncated"`
}

// NameMatch 决定文件名如何与模式比较；模式带通配符时始终按 glob 处理。
type NameMatch int

const (
	// NameMatchExact 要求文件名与模式完全一致（不区分大小写）。
	NameMatchExact NameMatch = iota
	// NameMatchSubstring 要求文件名包含模式（不区分大小写）。
	NameMatchSubstring
)

// FindOptions 描述一次文件名检索。
type FindOptions struct {
	Pattern string
	// Path 是检索范围：遍历从该目录开始，而不是在结果里过滤。
	Path   string
	Match  NameMatch
	Limit  int
	Offset int
}

const (
	// DefaultFindLimit 是请求未指定条数时使用的默认上限。
	DefaultFindLimit = 200
	// MaxFindLimit 是单次请求可以返回的结果数硬上限。
	MaxFindLimit = 10000
)

// NormalizeFindLimit 把请求的条数收敛到默认值与硬上限之间。
// HTTP 层与 Vault 共用这一规则，避免两处对 limit 的处理出现分歧。
func NormalizeFindLimit(limit int) int {
	if limit <= 0 {
		return DefaultFindLimit
	}
	if limit > MaxFindLimit {
		return MaxFindLimit
	}
	return limit
}

// Find 只按文件名匹配，不读文件内容，因此比内容检索快得多。
//
// 默认要求文件名与模式一致；Match 为 Substring 时改为包含即可。
// 模式里出现 * ? [ 时一律按 glob 匹配——通配符本身就是「不要精确」的意思。
func (v *Vault) Find(name string, options FindOptions) (FindResult, error) {
	pattern := strings.TrimSpace(options.Pattern)
	if pattern == "" {
		return FindResult{}, ErrInvalidName
	}

	rel, err := cleanPath(options.Path)
	if err != nil {
		return FindResult{}, err
	}

	matcher, err := buildNameMatcher(pattern, options.Match)
	if err != nil {
		return FindResult{}, err
	}

	limit := NormalizeFindLimit(options.Limit)

	root, err := v.open(name)
	if err != nil {
		return FindResult{}, err
	}
	defer root.Close()

	if _, err := root.Stat(rel); err != nil {
		return FindResult{}, describe(err)
	}

	offset := options.Offset
	if offset < 0 {
		offset = 0
	}

	result := FindResult{
		Repo:     name,
		Revision: readMetaRevision(name, v.repos[name]),
		Pattern:  pattern,
		Path:     rel,
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

// buildNameMatcher 按「通配符优先、其次显式模式」决定比较方式，统一按小写比较。
func buildNameMatcher(pattern string, mode NameMatch) (func(string) bool, error) {
	lowered := strings.ToLower(pattern)

	if strings.ContainsAny(lowered, "*?[") {
		// 语法错误在这里就报出来，避免「模式写错 → 一个都不匹配」的沉默。
		if _, err := path.Match(lowered, ""); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidName, err)
		}
		return func(candidate string) bool {
			matched, err := path.Match(lowered, strings.ToLower(candidate))
			return err == nil && matched
		}, nil
	}

	if mode == NameMatchSubstring {
		return func(candidate string) bool {
			return strings.Contains(strings.ToLower(candidate), lowered)
		}, nil
	}
	return func(candidate string) bool {
		return strings.ToLower(candidate) == lowered
	}, nil
}
