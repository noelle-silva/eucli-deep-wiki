package vault

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"sort"
	"strings"
	"sync"
)

// defaultSearchWorkers 是并行扫描的默认并行度。
const defaultSearchWorkers = 4

// MaxOversizedDetail 是结果里最多列出多少个被跳过的超大文件。
const MaxOversizedDetail = 20

// ContextLine 是命中行前后的一行原文。
type ContextLine struct {
	Line int    `json:"line"`
	Text string `json:"text"`
}

// Match 是一处命中。
type Match struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
	// Before 与 After 是紧邻命中行的上下文，按行号顺序排列。
	Before []ContextLine `json:"before,omitempty"`
	After  []ContextLine `json:"after,omitempty"`
}

// OversizedFile 是一个因为超过单文件上限而没有参与检索的文件。
type OversizedFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// SearchOptions 描述一次检索的边界。
type SearchOptions struct {
	Query  string
	Path   string
	Offset int
	Limit  int
	// Context 是每处命中带出的上下文行数（前后各这么多），0 表示不带。
	Context int
}

// SearchResult 是一次检索的结果。
type SearchResult struct {
	Repo     string  `json:"repo"`
	Revision string  `json:"revision"`
	Query    string  `json:"query"`
	Path     string  `json:"path,omitempty"`
	Offset   int     `json:"offset"`
	Matches  []Match `json:"matches"`
	// Scanned 是实际参与匹配的文本文件数。
	Scanned int `json:"scannedFiles"`
	// ScannedBytes 是实际读取的字节数，被跳过的二进制文件同样计入。
	ScannedBytes int64 `json:"scannedBytes"`
	// Oversized 列出被跳过的大文件明细（最多 MaxOversizedDetail 条）。
	Oversized []OversizedFile `json:"oversizedFiles,omitempty"`
	// OversizedCount 是被跳过的大文件总数，可能大于明细条数。
	OversizedCount int  `json:"oversizedFilesCount,omitempty"`
	Truncated      bool `json:"truncated"`
}

// NormalizeSearchContext 把请求的上下文行数收敛到硬上限。
func NormalizeSearchContext(lines int) int {
	if lines <= 0 {
		return 0
	}
	if lines > MaxSearchContext {
		return MaxSearchContext
	}
	return lines
}

// Search 在指定目录内按行做子串匹配；支持偏移量，便于翻页。
//
// 扫描分两步：先并行定位每个文件的命中行，再只为返回窗口内的命中读取行文本。
// 因此翻到很深的偏移时，窗口之前的命中只被计数，不会构造文本。
//
// offset/limit 按遍历顺序切页，页内的命中再按路径与行号排序：同样的查询
// 无论执行多少次，返回的命中都完全一致。
func (v *Vault) Search(name string, options SearchOptions) (SearchResult, error) {
	if strings.TrimSpace(options.Query) == "" {
		return SearchResult{}, ErrInvalidQuery
	}

	rel, err := cleanPath(options.Path)
	if err != nil {
		return SearchResult{}, err
	}

	root, err := v.open(name)
	if err != nil {
		return SearchResult{}, err
	}
	defer root.Close()

	if _, err := root.Stat(rel); err != nil {
		return SearchResult{}, describe(err)
	}

	scan := newSearchScan(v.limits, options)
	if err := scan.run(v.repos[name], root, rel); err != nil {
		return SearchResult{}, err
	}

	// 窗口多取一条只是为了判断「后面还有没有命中」，它不进入结果。
	window := scan.window
	if len(window) > scan.limit {
		window = window[:scan.limit]
	}

	result := SearchResult{
		Repo:           name,
		Revision:       readMetaRevision(name, v.repos[name]),
		Query:          options.Query,
		Path:           options.Path,
		Offset:         scan.offset,
		Matches:        buildMatches(root, window, NormalizeSearchContext(options.Context)),
		Scanned:        scan.scanned,
		ScannedBytes:   scan.scannedBytes,
		Oversized:      scan.oversized,
		OversizedCount: scan.oversizedCount,
		Truncated:      scan.truncated,
	}
	return result, nil
}

// matchRef 是待取文本的命中位置。
type matchRef struct {
	path string
	line int
}

// searchJob 是一个待扫描文件；index 对应遍历顺序。
type searchJob struct {
	index int
	path  string
}

// fileScan 是一个文件的扫描结果：只保留命中行号，不保留正文与文本。
type fileScan struct {
	index int
	path  string
	lines []int
	bytes int64
	text  bool
}

// searchScan 是一次检索的运行状态：扫描参数、累计计数与命中窗口。
type searchScan struct {
	query   string
	offset  int
	limit   int
	workers int

	maxFiles     int
	maxBytes     int64
	maxFileBytes int64

	scanned      int
	scannedBytes int64
	matched      int
	window       []matchRef
	enough       bool
	truncated    bool

	oversized      []OversizedFile
	oversizedCount int
}

func newSearchScan(limits Limits, options SearchOptions) *searchScan {
	limit := options.Limit
	if limit <= 0 || limit > limits.MaxSearchResults {
		limit = limits.MaxSearchResults
	}
	offset := options.Offset
	if offset < 0 {
		offset = 0
	}

	workers := limits.SearchWorkers
	if workers <= 0 {
		workers = defaultSearchWorkers
	}

	return &searchScan{
		query:        options.Query,
		offset:       offset,
		limit:        limit,
		workers:      workers,
		maxFiles:     limits.MaxSearchFiles,
		maxBytes:     limits.MaxSearchBytes,
		maxFileBytes: limits.MaxSearchFileBytes,
		window:       make([]matchRef, 0, limit+1),
	}
}

// run 并行扫描候选文件，并按遍历顺序汇总结果。
//
// 「按遍历顺序汇总」是结果可复现的前提：同样的仓库与查询，无论哪个 worker
// 先读完，进入窗口的命中都完全一致。
func (s *searchScan) run(dir string, walkRoot *os.Root, rel string) error {
	roots, err := openSearchRoots(dir, s.workers)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	var pipeline sync.WaitGroup
	defer func() {
		// 提前返回时也要等 worker 收尾，避免关句柄时还有读取在飞。
		cancel()
		pipeline.Wait()
		closeSearchRoots(roots)
	}()

	jobs := make(chan searchJob)
	results := make(chan fileScan)
	// 遍历错误交给主协程处理；提前结束时不再读取，缓冲一位足够。
	walkResult := make(chan error, 1)

	pipeline.Add(len(roots) + 1)
	go func() {
		defer pipeline.Done()
		defer close(jobs)
		walkResult <- s.enqueue(ctx, walkRoot, rel, jobs)
	}()
	for _, workerRoot := range roots {
		go func(root *os.Root) {
			defer pipeline.Done()
			s.scanFiles(ctx, root, jobs, results)
		}(workerRoot)
	}
	go func() {
		pipeline.Wait()
		close(results)
	}()

	pending := make(map[int]fileScan, len(roots))
	next := 0
	for outcome := range results {
		pending[outcome.index] = outcome
		for {
			current, ready := pending[next]
			if !ready {
				break
			}
			delete(pending, next)
			next++
			if s.consume(current) {
				return nil
			}
		}
	}

	if err := <-walkResult; err != nil && !errors.Is(err, fs.SkipAll) {
		return err
	}
	return nil
}

// enqueue 按遍历顺序派发候选文件；.git 与超过体积上限的文件直接跳过。
func (s *searchScan) enqueue(ctx context.Context, root *os.Root, rel string, jobs chan<- searchJob) error {
	index := 0
	return fs.WalkDir(root.FS(), rel, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return nil
		}
		if info.Size() > s.maxFileBytes {
			s.recordOversized(current, info.Size())
			return nil
		}

		job := searchJob{index: index, path: current}
		index++
		select {
		case jobs <- job:
			return nil
		case <-ctx.Done():
			return fs.SkipAll
		}
	})
}

// recordOversized 记下被跳过的超大文件：明细只留前若干条，总数照实累计。
// 这些文件不参与匹配，因此必须在结果里说出来，否则调用方会以为「没命中」。
func (s *searchScan) recordOversized(path string, size int64) {
	s.oversizedCount++
	if len(s.oversized) >= MaxOversizedDetail {
		return
	}
	s.oversized = append(s.oversized, OversizedFile{Path: path, Size: size})
}

// scanFiles 是单个 worker 的主循环：一个 worker 独占一个仓库句柄。
func (s *searchScan) scanFiles(ctx context.Context, root *os.Root, jobs <-chan searchJob, results chan<- fileScan) {
	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-jobs:
			if !ok {
				return
			}
			outcome := s.scanFile(root, job)
			select {
			case results <- outcome:
			case <-ctx.Done():
				return
			}
		}
	}
}

// scanFile 读取一个文件并记下命中行号；读取失败按「没有命中」处理，与逐个
// 读取时跳过坏文件的语义一致。
func (s *searchScan) scanFile(root *os.Root, job searchJob) fileScan {
	outcome := fileScan{index: job.index, path: job.path}

	data, err := root.ReadFile(job.path)
	if err != nil {
		return outcome
	}
	outcome.bytes = int64(len(data))
	if !isText(data) {
		return outcome
	}
	outcome.text = true

	for number, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, s.query) {
			outcome.lines = append(outcome.lines, number+1)
		}
	}
	return outcome
}

// consume 按遍历顺序累计命中，返回 true 表示可以结束扫描。
//
// 命中窗口之外的命中只参与计数，不占用内存；三道闸门（命中足够、文件数、
// 字节数）任意一道触到上限都置 truncated，表示结果可能不完整。
func (s *searchScan) consume(outcome fileScan) bool {
	s.scannedBytes += outcome.bytes
	if outcome.text {
		s.scanned++
		s.collectMatches(outcome)
	}

	if !s.reachedLimit() {
		return false
	}
	s.truncated = true
	return true
}

// reachedLimit 判断是否触到停下扫描的闸门。
func (s *searchScan) reachedLimit() bool {
	switch {
	case s.enough:
		return true
	case s.maxFiles > 0 && s.scanned >= s.maxFiles:
		return true
	case s.maxBytes > 0 && s.scannedBytes >= s.maxBytes:
		return true
	}
	return false
}

// collectMatches 把文件命中按遍历顺序放进窗口；偏移之前的命中只计数。
func (s *searchScan) collectMatches(outcome fileScan) {
	for _, line := range outcome.lines {
		global := s.matched
		s.matched++
		if global < s.offset {
			continue
		}

		s.window = append(s.window, matchRef{path: outcome.path, line: line})
		if len(s.window) > s.limit {
			s.enough = true
			return
		}
	}
}

// buildMatches 只为窗口内的命中读取行文本，并按需带出上下文。
//
// 命中行在两次读取之间消失时保留行号、文本留空：命中的位置信息仍然成立，
// 只是拿不到当时的原文。
func buildMatches(root *os.Root, window []matchRef, context int) []Match {
	matches := make([]Match, 0, len(window))

	currentPath := ""
	var lines []string
	for _, hit := range window {
		if hit.path != currentPath {
			currentPath = hit.path
			lines = readFileLines(root, hit.path)
		}

		text := ""
		if hit.line <= len(lines) {
			text = trimText(lines[hit.line-1])
		}
		match := Match{Path: hit.path, Line: hit.line, Text: text}
		if context > 0 && len(lines) > 0 {
			match.Before, match.After = contextAround(lines, hit.line, context)
		}
		matches = append(matches, match)
	}

	sort.Slice(matches, func(left int, right int) bool {
		if matches[left].Path != matches[right].Path {
			return matches[left].Path < matches[right].Path
		}
		return matches[left].Line < matches[right].Line
	})
	return matches
}

// contextAround 取出命中行前后各 context 行，越出文件边界就少给几行。
func contextAround(lines []string, line int, context int) ([]ContextLine, []ContextLine) {
	before := make([]ContextLine, 0, context)
	for number := max(line-context, 1); number < line; number++ {
		before = append(before, ContextLine{Line: number, Text: trimText(lines[number-1])})
	}

	after := make([]ContextLine, 0, context)
	for number := line + 1; number <= line+context && number <= len(lines); number++ {
		after = append(after, ContextLine{Line: number, Text: trimText(lines[number-1])})
	}
	return before, after
}

// readFileLines 读取取文本用的行；读不到时返回 nil，由调用方按「无文本」处理。
func readFileLines(root *os.Root, path string) []string {
	data, err := root.ReadFile(path)
	if err != nil {
		return nil
	}
	return splitFileLines(data)
}

// splitFileLines 按行切分；末尾换行不会凭空多出一个空行，
// 否则上下文行会把文件结尾的「幽灵行」也带出来。
func splitFileLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// openSearchRoots 为每个 worker 准备独立的仓库句柄，避免并发读同一个 *os.Root。
func openSearchRoots(dir string, workers int) ([]*os.Root, error) {
	roots := make([]*os.Root, 0, workers)
	for range workers {
		root, err := os.OpenRoot(dir)
		if err != nil {
			closeSearchRoots(roots)
			return nil, ErrRepoNotFound
		}
		roots = append(roots, root)
	}
	return roots, nil
}

func closeSearchRoots(roots []*os.Root) {
	for _, root := range roots {
		_ = root.Close()
	}
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
