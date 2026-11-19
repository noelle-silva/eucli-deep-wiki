// Package cache 管理服务端的产物缓存：命名规则、生成互斥、清单与清理。
//
// 缓存里的每一项都是随时可以重建的产物，但「重建」与「删除」不能同时发生。
// 因此生成与清理共用一份登记：生成前登记自己要写的那份产物，清理时跳过被登记
// 的产物并逐条说明原因；清理进行期间新的生成会等它结束再开始，不会往正在清理
// 的目录里塞文件。两种情况都不会互相破坏，也就不需要拿整份缓存当锁。
//
// 缓存目录布局只在这里定义一次：接口层生成产物、运维能力看清单与清理，
// 都从这里取，不各自拼一份文件名。
package cache

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"eucli-deep-wiki-server/internal/bundle"
)

// fileExtension 是缓存产物的后缀。
const fileExtension = ".bundle"

// 跳过原因：每一个没删的目录项都要能说清楚为什么。
const (
	reasonDirectory   = "目录不删"
	reasonNotArtifact = "不是缓存产物"
	reasonTemporary   = "生成中断留下的临时文件，不是缓存产物"
	reasonGenerating  = "正在生成"
)

var (
	// ErrNotConfirmed 表示清理请求缺少显式确认。
	ErrNotConfirmed = errors.New("cache clean requires confirmation")
	// ErrBusy 表示另一次清理正在进行，此刻不并发清理同一份缓存。
	ErrBusy = errors.New("cache is being cleaned")
)

// nameReplacer 把仓库名里不能出现在文件名中的字符替换掉。
//
// 缓存文件名必须是单层文件名：仓库名本身已经过校验，这里只是防止它被拼成路径。
var nameReplacer = strings.NewReplacer("/", "_", "\\", "_", ":", "_", "..", "_")

// Entry 是缓存清单里的一项。
type Entry struct {
	// Name 是缓存文件名。
	Name string `json:"name"`
	// Repository 与 Revision 从文件名还原；文件名不合规则时都为空。
	Repository string    `json:"repo,omitempty"`
	Revision   string    `json:"revision,omitempty"`
	Bytes      int64     `json:"bytes"`
	ModifiedAt time.Time `json:"modifiedAt"`
}

// ListOptions 是缓存清单的查询条件。
type ListOptions struct {
	// Repository 非空时只列这个仓库的产物；为空表示全部。
	Repository string
}

// ListResult 是缓存清单：逐条产物加一份合计。
type ListResult struct {
	Files []Entry `json:"files"`
	// TotalBytes 是本次列出产物的字节数合计。
	TotalBytes int64 `json:"totalBytes"`
}

// CleanOptions 是一次清理请求。
type CleanOptions struct {
	// Repository 非空时只清理这个仓库的产物；为空表示全部。
	Repository string
	// Confirm 为假时拒绝执行：清理不可撤销，必须显式确认。
	Confirm bool
}

// Skipped 是一个没有被删除的目录项及原因。
type Skipped struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// CleanResult 是一次清理的结果。
type CleanResult struct {
	// Removed 是已删除的产物数量，Bytes 是它们占用的字节数。
	Removed int   `json:"removed"`
	Bytes   int64 `json:"bytes"`
	// Skipped 逐条说明哪些目录项为什么没删。
	Skipped []Skipped `json:"skipped"`
}

// Store 是缓存目录的读写入口。
//
// 生成与清理的协调只有这几种状态：谁在生成哪份产物、有没有清理在进行。
// 两者都在 mu 里读写，判断与登记在同一把锁内完成，不存在「看到空闲、登记时已被占」。
type Store struct {
	dir string

	mu sync.Mutex
	// generating 记录正在生成的产物文件名，值是「完成后关闭」的通道。
	generating map[string]chan struct{}
	// cleaning 表示有清理正在进行，cleanDone 在它结束时关闭。
	cleaning  bool
	cleanDone chan struct{}
}

// Open 指向一个缓存目录。目录按需创建，不必预先存在。
func Open(dir string) *Store {
	return &Store{
		dir:        dir,
		generating: make(map[string]chan struct{}),
	}
}

// Dir 返回缓存目录的位置。
func (s *Store) Dir() string {
	return s.dir
}

// List 列出当前缓存产物；目录不存在等于空清单，不是错误。
func (s *Store) List(options ListOptions) (ListResult, error) {
	directoryEntries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// 空清单与「有缓存但一个产物都没有」在对外表达上是同一件事，
			// 因此仍然给一个空数组，而不是 null。
			return ListResult{Files: []Entry{}}, nil
		}
		return ListResult{}, err
	}

	result := ListResult{Files: make([]Entry, 0, len(directoryEntries))}
	for _, directoryEntry := range directoryEntries {
		if directoryEntry.IsDir() {
			continue
		}
		repository, revision, ok := parseFileName(directoryEntry.Name())
		if !ok {
			continue
		}
		if options.Repository != "" && repository != options.Repository {
			continue
		}
		info, err := directoryEntry.Info()
		if err != nil {
			continue
		}
		result.Files = append(result.Files, Entry{
			Name:       directoryEntry.Name(),
			Repository: repository,
			Revision:   revision,
			Bytes:      info.Size(),
			ModifiedAt: info.ModTime(),
		})
		result.TotalBytes += info.Size()
	}
	return result, nil
}

// Ensure 保证某个提交的产物已经生成，返回可下发的文件路径。
//
// 已存在就直接复用；不存在时在「这份产物」的生成权内生成，同一份产物任何时刻
// 只有一个生成过程。生成过程不受调用方请求的生命周期影响由调用方决定：
// 这里收到的 ctx 就是打包过程使用的 ctx。
func (s *Store) Ensure(ctx context.Context, repository string, revision string, repositoryDir string, timeout time.Duration) (string, error) {
	name := fileName(repository, revision)
	path := filepath.Join(s.dir, name)

	if err := s.waitTurn(ctx, name); err != nil {
		return "", err
	}
	defer s.release(name)

	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	if err := bundle.Create(ctx, repositoryDir, path, timeout); err != nil {
		return "", err
	}
	return path, nil
}

// waitTurn 等到可以开始生成：清理进行中时等它结束，同一份产物已有生成者时
// 等它完成。等待期间 ctx 取消即返回，不把调用方一直挂着。
func (s *Store) waitTurn(ctx context.Context, name string) error {
	for {
		s.mu.Lock()
		switch {
		case s.cleaning:
			waiter := s.cleanDone
			s.mu.Unlock()
			if err := waitFor(ctx, waiter); err != nil {
				return err
			}
		default:
			if waiter, busy := s.generating[name]; busy {
				s.mu.Unlock()
				if err := waitFor(ctx, waiter); err != nil {
					return err
				}
				continue
			}
			s.generating[name] = make(chan struct{})
			s.mu.Unlock()
			return nil
		}
	}
}

// waitFor 等到 done 关闭；ctx 先结束则放弃等待。
func waitFor(ctx context.Context, done <-chan struct{}) error {
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// release 交还生成权，并唤醒等同一份产物的调用方。
func (s *Store) release(name string) {
	s.mu.Lock()
	waiter := s.generating[name]
	delete(s.generating, name)
	s.mu.Unlock()

	close(waiter)
}

// Clean 删除缓存产物。
//
// 默认只删缓存产物文件：目录、正在生成的产物、以及不认识的文件都会留在原地，
// 并逐条说明原因——「为什么这个没删」必须能查到。
func (s *Store) Clean(options CleanOptions) (CleanResult, error) {
	if !options.Confirm {
		return CleanResult{}, ErrNotConfirmed
	}

	inProgress, err := s.beginClean()
	if err != nil {
		return CleanResult{}, err
	}
	defer s.endClean()

	directoryEntries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return CleanResult{Removed: 0, Bytes: 0, Skipped: []Skipped{}}, nil
		}
		return CleanResult{}, err
	}

	result := CleanResult{Skipped: []Skipped{}}
	for _, directoryEntry := range directoryEntries {
		name := directoryEntry.Name()

		if directoryEntry.IsDir() {
			result.Skipped = append(result.Skipped, Skipped{Name: name, Reason: reasonDirectory})
			continue
		}

		// 临时文件算它将要变成的那份产物：归属与「正在生成」都按产物名判断，
		// 这样按仓库清理时不会把别人的半成品也算进跳过名单。但它永远不删——
		// 它不是成品，而生成方下一次开始时会自己清掉残留。
		artifact := artifactName(name)
		temporary := artifact != name
		repository, _, isArtifact := parseFileName(artifact)
		if isArtifact && options.Repository != "" && repository != options.Repository {
			// 不在本次清理范围内，不是「被跳过」，不必解释。
			continue
		}
		if _, generating := inProgress[artifact]; generating {
			result.Skipped = append(result.Skipped, Skipped{Name: name, Reason: reasonGenerating})
			continue
		}
		if !isArtifact || temporary {
			result.Skipped = append(result.Skipped, Skipped{Name: name, Reason: notArtifactReason(name)})
			continue
		}

		info, err := directoryEntry.Info()
		if err != nil {
			result.Skipped = append(result.Skipped, Skipped{Name: name, Reason: err.Error()})
			continue
		}
		if err := os.Remove(filepath.Join(s.dir, name)); err != nil {
			result.Skipped = append(result.Skipped, Skipped{Name: name, Reason: err.Error()})
			continue
		}
		result.Removed++
		result.Bytes += info.Size()
	}
	return result, nil
}

// beginClean 占住「清理」这一职责，并抄一份此刻正在生成的产物名单。
//
// 另一份清理正在进行时返回 ErrBusy：宁可让调用方稍后重试，也不让两次清理
// 同时改同一个目录。
func (s *Store) beginClean() (map[string]struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cleaning {
		return nil, ErrBusy
	}
	s.cleaning = true
	s.cleanDone = make(chan struct{})

	inProgress := make(map[string]struct{}, len(s.generating))
	for name := range s.generating {
		inProgress[name] = struct{}{}
	}
	return inProgress, nil
}

// endClean 交还「清理」这一职责，并唤醒等它结束的生成方。
func (s *Store) endClean() {
	s.mu.Lock()
	s.cleaning = false
	done := s.cleanDone
	s.cleanDone = nil
	s.mu.Unlock()

	close(done)
}

// fileName 是缓存产物的命名规则：<仓库名>-<提交>.bundle。
func fileName(repository string, revision string) string {
	return nameReplacer.Replace(repository) + "-" + revision + fileExtension
}

// artifactName 把一个目录项还原成它所属的产物文件名：临时文件算它将要变成的那份。
func artifactName(name string) string {
	return strings.TrimSuffix(name, bundle.TemporarySuffix)
}

// notArtifactReason 说明一个不是产物的目录项为什么没删。
func notArtifactReason(name string) string {
	if strings.HasSuffix(name, bundle.TemporarySuffix) {
		return reasonTemporary
	}
	return reasonNotArtifact
}

// parseFileName 从缓存文件名还原归属仓库与对应提交。
//
// 仓库名里可能有连字符，因此从右侧切：最后一段必须像一个提交号（十六进制），
// 否则判定为「不是缓存产物」——宁可把它留在原地，也不猜它的归属。
func parseFileName(name string) (string, string, bool) {
	base, ok := strings.CutSuffix(name, fileExtension)
	if !ok {
		return "", "", false
	}
	separator := strings.LastIndex(base, "-")
	if separator <= 0 {
		return "", "", false
	}
	repository := base[:separator]
	revision := base[separator+1:]
	if !isRevision(revision) {
		return "", "", false
	}
	return repository, revision, true
}

// isRevision 判断一段文本是否像一个提交号：七到六十四位十六进制。
func isRevision(value string) bool {
	const shortest = 7
	const longest = 64
	if len(value) < shortest || len(value) > longest {
		return false
	}
	for _, character := range value {
		switch {
		case character >= '0' && character <= '9':
		case character >= 'a' && character <= 'f':
		case character >= 'A' && character <= 'F':
		default:
			return false
		}
	}
	return true
}
