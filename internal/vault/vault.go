// Package vault 提供参考仓库的只读访问。
//
// 它只做三件事：把仓库名收敛成受控目录、拒绝越界路径、给读取加上体积上限。
// 所有文件操作都经由 os.Root，符号链接无法把访问带出仓库之外。
package vault

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

var (
	// ErrRepoNotFound 表示仓库不在索引中，或尚未同步到本地。
	ErrRepoNotFound = errors.New("repository not found")
	// ErrNotFound 表示仓库内不存在该路径。
	ErrNotFound = errors.New("path not found")
	// ErrInvalidPath 表示请求路径试图逃出仓库。
	ErrInvalidPath = errors.New("invalid path")
	// ErrNotRegular 表示目标不是普通文件。
	ErrNotRegular = errors.New("path is not a regular file")
	// ErrTooLarge 表示文件超过读取上限。
	ErrTooLarge = errors.New("file exceeds size limit")
	// ErrBinary 表示文件不是可读文本。
	ErrBinary = errors.New("file is not text")
	// ErrInvalidQuery 表示检索词为空。
	ErrInvalidQuery = errors.New("query is required")
	// ErrInvalidName 表示文件名模式为空或通配语法不合法。
	ErrInvalidName = errors.New("name is required")
	// ErrInvalidLineRange 表示行区间写法不合法。
	ErrInvalidLineRange = errors.New("invalid line range")
)

// Limits 控制单次请求的资源上限，避免一次请求扫穿整个仓库。
//
// SearchWorkers 是内容检索的并行度，其余字段都是「一口气最多处理多少」的上限。
type Limits struct {
	// MaxFileBytes 是单次读取的文件大小上限。读取可以按行切段、按字节限长，
	// 因此它约束的是「文件本身有多大」，而不是「这次返回多少」。
	MaxFileBytes int64
	// MaxSearchFileBytes 是内容检索时单个文件的上限：超过就跳过并在结果里列出来。
	// 它比 MaxFileBytes 小得多——检索要扫全仓，不能为一个大文件拖住整轮。
	MaxSearchFileBytes int64
	MaxTreeEntries     int
	MaxSearchFiles     int
	MaxSearchBytes     int64
	MaxSearchResults   int
	SearchWorkers      int
}

// DefaultLimits 是面向阅读场景的保守取值。
func DefaultLimits() Limits {
	return Limits{
		MaxFileBytes:       16 << 20,
		MaxSearchFileBytes: 1 << 20,
		MaxTreeEntries:     2000,
		MaxSearchFiles:     20000,
		MaxSearchBytes:     64 << 20,
		MaxSearchResults:   100,
		SearchWorkers:      4,
	}
}

// Vault 把仓库名映射到受控目录。
type Vault struct {
	repos         map[string]string
	limits        Limits
	archiveLimits ArchiveLimits
}

// New 基于参考仓库根目录与索引里的仓库名构建只读视图。
func New(rootDir string, names []string, limits Limits) (*Vault, error) {
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, err
	}

	repos := make(map[string]string, len(names))
	for _, name := range names {
		repos[name] = filepath.Join(absRoot, name)
	}
	return &Vault{repos: repos, limits: limits, archiveLimits: DefaultArchiveLimits()}, nil
}

// Repositories 返回已登记的仓库名，按字典序排列。
func (v *Vault) Repositories() []string {
	names := make([]string, 0, len(v.repos))
	for name := range v.repos {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Listing 返回全部登记仓库的元数据，按仓库名字典序排列。
//
// 尚未下载到本地的仓库只给出名字：它是合法的登记项，只是还没有内容可取。
// 列表因此不会因为「刚加进索引、还没同步」就把仓库藏起来。
func (v *Vault) Listing() []Meta {
	names := v.Repositories()
	metas := make([]Meta, 0, len(names))
	for _, name := range names {
		meta, err := v.Meta(name)
		if err != nil {
			meta = Meta{Name: name}
		}
		metas = append(metas, meta)
	}
	return metas
}

// Meta 描述一个仓库的当前状态。
type Meta struct {
	Name     string `json:"name"`
	Branch   string `json:"branch,omitempty"`
	Revision string `json:"revision,omitempty"`
	// Shallow 表示本地是浅克隆：只有最近一次提交的历史。
	Shallow bool `json:"shallow,omitempty"`
}

// Meta 读取仓库分支、提交号与历史深度。
func (v *Vault) Meta(name string) (Meta, error) {
	dir, ok := v.repos[name]
	if !ok {
		return Meta{}, ErrRepoNotFound
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return Meta{}, ErrRepoNotFound
	}

	branch, revision := readHead(dir)
	return Meta{Name: name, Branch: branch, Revision: revision, Shallow: isShallowDir(dir)}, nil
}

// isShallowDir 判断仓库是否是浅克隆：浅克隆会把被截断的提交记在 .git/shallow。
func isShallowDir(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git", "shallow"))
	return err == nil
}

// RepositoryDir 返回仓库在磁盘上的目录，仅供服务内部使用。
func (v *Vault) RepositoryDir(name string) (string, error) {
	dir, ok := v.repos[name]
	if !ok {
		return "", ErrRepoNotFound
	}
	return dir, nil
}

// open 返回受限于仓库目录的句柄，调用方负责关闭。
func (v *Vault) open(name string) (*os.Root, error) {
	dir, ok := v.repos[name]
	if !ok {
		return nil, ErrRepoNotFound
	}

	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, ErrRepoNotFound
	}
	return root, nil
}

// cleanPath 把请求路径规整成仓库内的相对路径。
func cleanPath(requested string) (string, error) {
	trimmed := strings.TrimSpace(requested)
	trimmed = strings.ReplaceAll(trimmed, `\`, "/")
	trimmed = strings.TrimPrefix(trimmed, "/")
	if trimmed == "" {
		return ".", nil
	}

	cleaned := path.Clean(trimmed)
	if cleaned == "." {
		return ".", nil
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", ErrInvalidPath
	}
	if !filepath.IsLocal(filepath.FromSlash(cleaned)) {
		return "", ErrInvalidPath
	}
	return cleaned, nil
}

// joinRel 拼接仓库内的相对路径；起点是仓库根时不留前缀。
func joinRel(base string, name string) string {
	if base == "." {
		return name
	}
	return base + "/" + name
}

// readMetaRevision 直接从 .git 里取提交号，不为一次查询启动 git 进程。
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

// readHead 从 .git 目录直接读出分支与提交号，避免为一次查询启动 git 进程。
func readHead(dir string) (string, string) {
	gitDir := filepath.Join(dir, ".git")

	head, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return "", ""
	}

	line := strings.TrimSpace(string(head))
	if !strings.HasPrefix(line, "ref: ") {
		return "", line
	}

	ref := strings.TrimPrefix(line, "ref: ")
	branch := strings.TrimPrefix(ref, "refs/heads/")

	if raw, err := os.ReadFile(filepath.Join(gitDir, filepath.FromSlash(ref))); err == nil {
		return branch, strings.TrimSpace(string(raw))
	}

	packed, err := os.ReadFile(filepath.Join(gitDir, "packed-refs"))
	if err != nil {
		return branch, ""
	}
	for _, line := range strings.Split(string(packed), "\n") {
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "^") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == ref {
			return branch, fields[0]
		}
	}
	return branch, ""
}
