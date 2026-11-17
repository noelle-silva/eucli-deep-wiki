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

// SearchResult 是一次检索的结果。
type SearchResult struct {
	Repo      string  `json:"repo"`
	Query     string  `json:"query"`
	Matches   []Match `json:"matches"`
	Scanned   int     `json:"scannedFiles"`
	Truncated bool    `json:"truncated"`
}

// Search 在仓库内按行做子串匹配，遇到目录边界与体积上限即停。
func (v *Vault) Search(name string, query string, limit int) (SearchResult, error) {
	if strings.TrimSpace(query) == "" {
		return SearchResult{}, ErrInvalidQuery
	}
	if limit <= 0 || limit > v.limits.MaxSearchResults {
		limit = v.limits.MaxSearchResults
	}

	root, err := v.open(name)
	if err != nil {
		return SearchResult{}, err
	}
	defer root.Close()

	result := SearchResult{
		Repo:    name,
		Query:   query,
		Matches: []Match{},
	}
	fsys := root.FS()

	walkErr := fs.WalkDir(fsys, ".", func(current string, entry fs.DirEntry, walkErr error) error {
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
			if !strings.Contains(line, query) {
				continue
			}
			result.Matches = append(result.Matches, Match{
				Path: current,
				Line: index + 1,
				Text: trimText(line),
			})
			if len(result.Matches) >= limit {
				result.Truncated = true
				return fs.SkipAll
			}
		}
		return nil
	})

	if walkErr != nil && !errors.Is(walkErr, fs.SkipAll) {
		return SearchResult{}, walkErr
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
