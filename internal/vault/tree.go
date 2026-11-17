package vault

import (
	"errors"
	"io"
	"os"
	"sort"
	"time"
)

// Entry 是目录树里的一个节点。
type Entry struct {
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	Type       string    `json:"type"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modifiedAt,omitempty"`
}

// Tree 是一层目录的展开结果。
type Tree struct {
	Repo      string  `json:"repo"`
	Revision  string  `json:"revision"`
	Path      string  `json:"path"`
	Entries   []Entry `json:"entries"`
	Truncated bool    `json:"truncated"`
}

// Tree 列出仓库内某个目录的直接子项。
func (v *Vault) Tree(name string, requested string) (Tree, error) {
	rel, err := cleanPath(requested)
	if err != nil {
		return Tree{}, err
	}

	root, err := v.open(name)
	if err != nil {
		return Tree{}, err
	}
	defer root.Close()

	dir, err := root.Open(rel)
	if err != nil {
		return Tree{}, describe(err)
	}
	defer dir.Close()

	info, err := dir.Stat()
	if err != nil {
		return Tree{}, describe(err)
	}
	if !info.IsDir() {
		return Tree{}, ErrNotRegular
	}

	entries, err := dir.ReadDir(v.limits.MaxTreeEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return Tree{}, describe(err)
	}

	truncated := len(entries) > v.limits.MaxTreeEntries
	if truncated {
		entries = entries[:v.limits.MaxTreeEntries]
	}

	tree := Tree{
		Repo:      name,
		Path:      rel,
		Revision:  readMetaRevision(name, v.repos[name]),
		Truncated: truncated,
		Entries:   make([]Entry, 0, len(entries)),
	}
	for _, entry := range entries {
		if entry.Name() == ".git" {
			continue
		}

		item := Entry{
			Name: entry.Name(),
			Path: joinRel(rel, entry.Name()),
			Type: "file",
		}
		if entry.IsDir() {
			item.Type = "dir"
		}
		if info, err := entry.Info(); err == nil {
			item.ModifiedAt = info.ModTime()
			if item.Type == "file" {
				item.Size = info.Size()
			}
		}
		tree.Entries = append(tree.Entries, item)
	}

	sort.Slice(tree.Entries, func(i, j int) bool {
		if tree.Entries[i].Type != tree.Entries[j].Type {
			return tree.Entries[i].Type == "dir"
		}
		return tree.Entries[i].Name < tree.Entries[j].Name
	})
	return tree, nil
}

func joinRel(base string, name string) string {
	if base == "." {
		return name
	}
	return base + "/" + name
}

func readMetaRevision(name string, dir string) string {
	_, revision := readHead(dir)
	return revision
}

// describe 把文件系统错误收敛成可判定的业务错误。
func describe(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	return err
}
