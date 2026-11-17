package vault

import (
	"errors"
	"io/fs"
	"strings"
)

// Match 是一处命中。
type Match struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// SearchOptions 描述一次检索的边界。
type SearchOptions struct {
	Query  string
	Path   string
	Offset int
	Limit  int
}

// SearchResult 是一次检索的结果。
type SearchResult struct {
	Repo      string  `json:"repo"`
	Query     string  `json:"query"`
	Path      string  `json:"path,omitempty"`
	Offset    int     `json:"offset"`
	Matches   []Match `json:"matches"`
	Scanned   int     `json:"scannedFiles"`
	Truncated bool    `json:"truncated"`
}

// Search 在指定目录内按行做子串匹配；支持偏移量，便于翻页。
func (v *Vault) Search(name string, options SearchOptions) (SearchResult, error) {
	if strings.TrimSpace(options.Query) == "" {
		return SearchResult{}, ErrInvalidQuery
	}

	rel, err := cleanPath(options.Path)
	if err != nil {
		return SearchResult{}, err
	}

	limit := options.Limit
	if limit <= 0 || limit > v.limits.MaxSearchResults {
		limit = v.limits.MaxSearchResults
	}
	offset := options.Offset
	if offset < 0 {
		offset = 0
	}

	root, err := v.open(name)
	if err != nil {
		return SearchResult{}, err
	}
	defer root.Close()

	if _, err := root.Stat(rel); err != nil {
		return SearchResult{}, describe(err)
	}

	result := SearchResult{
		Repo:    name,
		Query:   options.Query,
		Path:    options.Path,
		Offset:  offset,
		Matches: []Match{},
	}
	// 多取一条用来判断后面还有没有。
	wanted := offset + limit + 1
	fsys := root.FS()

	walkErr := fs.WalkDir(fsys, rel, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}

		if result.Scanned >= v.limits.MaxSearchFiles {
			result.Truncated = true
			return fs.SkipAll
		}

		info, err := entry.Info()
		if err != nil || info.Size() > v.limits.MaxFileBytes {
			return nil
		}

		data, err := fs.ReadFile(fsys, current)
		if err != nil || !isText(data) {
			return nil
		}

		result.Scanned++
		for index, line := range strings.Split(string(data), "\n") {
			if !strings.Contains(line, options.Query) {
				continue
			}
			result.Matches = append(result.Matches, Match{
				Path: current,
				Line: index + 1,
				Text: trimText(line),
			})
			if len(result.Matches) >= wanted {
				result.Truncated = true
				return fs.SkipAll
			}
		}
		return nil
	})

	if walkErr != nil && !errors.Is(walkErr, fs.SkipAll) {
		return SearchResult{}, walkErr
	}

	if offset >= len(result.Matches) {
		result.Matches = []Match{}
		return result, nil
	}
	result.Matches = result.Matches[offset:]
	if len(result.Matches) > limit {
		result.Matches = result.Matches[:limit]
		result.Truncated = true
	}
	return result, nil
}

// trimText 把命中行压缩成适合阅读的一行摘要。
func trimText(line string) string {
	trimmed := strings.TrimSpace(line)
	const maxRunes = 200
	runes := []rune(trimmed)
	if len(runes) <= maxRunes {
		return trimmed
	}
	return string(runes[:maxRunes]) + "…"
}
