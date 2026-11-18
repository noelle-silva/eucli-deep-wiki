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
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"`
	Size int64  `json:"size"`
	// Depth 是相对展开起点的层级：直接子项为 1。
	Depth      int       `json:"depth"`
	ModifiedAt time.Time `json:"modifiedAt,omitempty"`
}

// TreeOptions 描述一次目录展开。
type TreeOptions struct {
	// Path 是展开的起点，空串表示仓库根。
	Path string
	// Depth 是要展开的层数；1 表示只列直接子项。
	Depth int
	// DirsOnly 只返回目录，用于快速看骨架。
	DirsOnly bool
}

// Tree 是一次目录展开的结果。
type Tree struct {
	Repo     string `json:"repo"`
	Revision string `json:"revision"`
	Path     string `json:"path"`
	// Depth 是本次实际展开的层数，便于调用方确认参数是否被收敛。
	Depth     int     `json:"depth"`
	Entries   []Entry `json:"entries"`
	Truncated bool    `json:"truncated"`
}

const (
	// DefaultTreeDepth 是未指定层数时展开的层数，与历史行为一致。
	DefaultTreeDepth = 1
	// MaxTreeDepth 是单次请求允许展开的最大层数。
	MaxTreeDepth = 8
)

// NormalizeTreeDepth 把请求的层数收敛到默认值与硬上限之间。
func NormalizeTreeDepth(depth int) int {
	if depth <= 0 {
		return DefaultTreeDepth
	}
	if depth > MaxTreeDepth {
		return MaxTreeDepth
	}
	return depth
}

// Tree 展开仓库内的某个目录：先列本级，再按目录深度优先向下。
//
// 条目总数受 MaxTreeEntries 约束，超出即截断；截断可能发生在任何一层，
// 因此 Truncated 为真时不能把结果当成完整骨架。
func (v *Vault) Tree(name string, options TreeOptions) (Tree, error) {
	rel, err := cleanPath(options.Path)
	if err != nil {
		return Tree{}, err
	}

	root, err := v.open(name)
	if err != nil {
		return Tree{}, err
	}
	defer root.Close()

	info, err := root.Stat(rel)
	if err != nil {
		return Tree{}, describe(err)
	}
	if !info.IsDir() {
		return Tree{}, ErrNotRegular
	}

	depth := NormalizeTreeDepth(options.Depth)
	walk := &treeWalker{
		root:     root,
		budget:   v.limits.MaxTreeEntries,
		dirsOnly: options.DirsOnly,
	}
	if err := walk.expand(rel, 1, depth); err != nil {
		return Tree{}, err
	}

	return Tree{
		Repo:      name,
		Path:      rel,
		Revision:  readMetaRevision(name, v.repos[name]),
		Depth:     depth,
		Entries:   walk.entries,
		Truncated: walk.truncated,
	}, nil
}

// treeWalker 是一次展开过程的累计状态。
type treeWalker struct {
	root      *os.Root
	budget    int
	dirsOnly  bool
	entries   []Entry
	truncated bool
}

// expand 展开第 level 层，然后对每个子目录继续向下；maxLevel 之外只看不展开。
func (w *treeWalker) expand(rel string, level int, maxLevel int) error {
	if level > maxLevel || w.truncated {
		return nil
	}

	remaining := w.budget - len(w.entries)
	if remaining <= 0 {
		w.truncated = true
		return nil
	}

	children, err := w.readChildren(rel, level, remaining)
	if err != nil {
		return err
	}

	for _, child := range children {
		if len(w.entries) >= w.budget {
			w.truncated = true
			return nil
		}

		w.entries = append(w.entries, child)
		if child.Type != "dir" {
			continue
		}
		if err := w.expand(child.Path, level+1, maxLevel); err != nil {
			return err
		}
	}
	return nil
}

// readChildren 读出一层的子项并排序；目录在前，同级按名字升序。
func (w *treeWalker) readChildren(rel string, level int, remaining int) ([]Entry, error) {
	dir, err := w.root.Open(rel)
	if err != nil {
		return nil, describe(err)
	}
	defer dir.Close()

	items, err := dir.ReadDir(remaining + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, describe(err)
	}
	// 多读的一项只用来判断「还有更多」，它本身不进结果。
	if len(items) > remaining {
		w.truncated = true
		items = items[:remaining]
	}

	children := make([]Entry, 0, len(items))
	for _, item := range items {
		if item.Name() == ".git" {
			continue
		}

		child := Entry{
			Name:  item.Name(),
			Path:  joinRel(rel, item.Name()),
			Type:  "file",
			Depth: level,
		}
		if item.IsDir() {
			child.Type = "dir"
		} else if w.dirsOnly {
			continue
		}
		if info, infoErr := item.Info(); infoErr == nil {
			child.ModifiedAt = info.ModTime()
			if child.Type == "file" {
				child.Size = info.Size()
			}
		}
		children = append(children, child)
	}

	sort.Slice(children, func(left int, right int) bool {
		if children[left].Type != children[right].Type {
			return children[left].Type == "dir"
		}
		return children[left].Name < children[right].Name
	})
	return children, nil
}
