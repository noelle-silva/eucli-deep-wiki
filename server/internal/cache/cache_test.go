package cache

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"eucli-deep-wiki-server/internal/bundle"
)

const testRevision = "0123456789abcdef0123456789abcdef01234567"

func TestListMissingDirectoryIsEmpty(t *testing.T) {
	t.Parallel()

	result, err := Open(filepath.Join(t.TempDir(), "absent")).List(ListOptions{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(result.Files) != 0 || result.TotalBytes != 0 {
		t.Fatalf("result = %#v", result)
	}
	if result.Files == nil {
		t.Fatal("空清单也要给空数组，而不是 null")
	}
}

func TestListReportsArtifactsWithOrigin(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, fileName("example", testRevision), 128)
	writeFile(t, dir, fileName("my-repo", "abc1234"), 64)
	writeFile(t, dir, "notes.txt", 10)
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	result, err := Open(dir).List(ListOptions{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(result.Files) != 2 {
		t.Fatalf("files = %#v", result.Files)
	}

	byName := map[string]Entry{}
	for _, entry := range result.Files {
		byName[entry.Name] = entry
	}
	example, ok := byName[fileName("example", testRevision)]
	if !ok {
		t.Fatalf("缺少 example 的产物: %#v", result.Files)
	}
	if example.Repository != "example" || example.Revision != testRevision || example.Bytes != 128 {
		t.Fatalf("example = %#v", example)
	}
	if example.ModifiedAt.IsZero() {
		t.Fatalf("缺少修改时间: %#v", example)
	}
	if result.TotalBytes != 192 {
		t.Fatalf("TotalBytes = %d, want 192", result.TotalBytes)
	}
	// 仓库名里的连字符不能把归属切错。
	multi, ok := byName[fileName("my-repo", "abc1234")]
	if !ok || multi.Repository != "my-repo" || multi.Revision != "abc1234" {
		t.Fatalf("my-repo = %#v", multi)
	}
}

// 清单可以只列一个仓库的产物：合计也随之只算这些。
func TestListFiltersByRepository(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, fileName("example", testRevision), 16)
	writeFile(t, dir, fileName("other", testRevision), 24)

	result, err := Open(dir).List(ListOptions{Repository: "example"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(result.Files) != 1 || result.Files[0].Repository != "example" || result.TotalBytes != 16 {
		t.Fatalf("result = %#v", result)
	}
}

func TestParseFileNameRejectsUnrecognizedNames(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		repository string
		revision   string
		ok         bool
	}{
		{"example-" + testRevision + ".bundle", "example", testRevision, true},
		{"a-b-abc1234.bundle", "a-b", "abc1234", true},
		{"example.bundle", "", "", false},
		{"example-.bundle", "", "", false},
		{"example-zzzz.bundle", "", "", false},
		{"example-abc1234.tmp", "", "", false},
		{"notes.txt", "", "", false},
	}

	for _, testCase := range cases {
		repository, revision, ok := parseFileName(testCase.name)
		if ok != testCase.ok || repository != testCase.repository || revision != testCase.revision {
			t.Fatalf("parseFileName(%q) = %q, %q, %t", testCase.name, repository, revision, ok)
		}
	}
}

func TestEnsureReusesExistingArtifact(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, fileName("example", testRevision), 32)

	// 仓库目录不存在：如果这里真的去打包，它一定会失败；
	// 能正常返回就说明已有的产物被复用了。
	path, err := Open(dir).Ensure(context.Background(), "example", testRevision, filepath.Join(dir, "absent"), time.Second)
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if path != filepath.Join(dir, fileName("example", testRevision)) {
		t.Fatalf("path = %q", path)
	}
}

func TestEnsureGeneratesArtifact(t *testing.T) {
	t.Parallel()

	repository := newRepository(t)
	dir := filepath.Join(t.TempDir(), "cache")

	store := Open(dir)
	path, err := store.Ensure(context.Background(), "example", testRevision, repository, 30*time.Second)
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat artifact: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("产物不该是空文件")
	}

	result, err := store.List(ListOptions{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(result.Files) != 1 || result.Files[0].Repository != "example" {
		t.Fatalf("files = %#v", result.Files)
	}
}

func TestCleanRequiresConfirmation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, fileName("example", testRevision), 16)

	if _, err := Open(dir).Clean(CleanOptions{}); !errors.Is(err, ErrNotConfirmed) {
		t.Fatalf("Clean() error = %v, want ErrNotConfirmed", err)
	}
	if _, err := os.Stat(filepath.Join(dir, fileName("example", testRevision))); err != nil {
		t.Fatalf("没确认时不该删文件: %v", err)
	}
}

func TestCleanRemovesOnlyArtifacts(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	artifact := fileName("example", testRevision)
	writeFile(t, dir, artifact, 16)
	writeFile(t, dir, "notes.txt", 8)
	writeFile(t, dir, "example-"+testRevision+".bundle.tmp", 4)
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	result, err := Open(dir).Clean(CleanOptions{Confirm: true})
	if err != nil {
		t.Fatalf("Clean() error = %v", err)
	}

	if result.Removed != 1 || result.Bytes != 16 {
		t.Fatalf("result = %#v", result)
	}
	if _, err := os.Stat(filepath.Join(dir, artifact)); !os.IsNotExist(err) {
		t.Fatalf("产物应已删除: %v", err)
	}

	// 不是缓存产物的东西留在原地，并且逐条说明原因。
	for _, name := range []string{"notes.txt", "example-" + testRevision + ".bundle.tmp", "nested"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("%s 不该被删: %v", name, err)
		}
	}
	reasons := map[string]string{}
	for _, skipped := range result.Skipped {
		reasons[skipped.Name] = skipped.Reason
	}
	if len(reasons) != 3 {
		t.Fatalf("Skipped = %#v", result.Skipped)
	}
	for _, name := range []string{"notes.txt", "example-" + testRevision + ".bundle.tmp", "nested"} {
		if reasons[name] == "" {
			t.Fatalf("%s 缺少跳过原因: %#v", name, result.Skipped)
		}
	}
	if reasons["example-"+testRevision+".bundle.tmp"] != reasonTemporary {
		t.Fatalf("临时文件的跳过原因 = %q", reasons["example-"+testRevision+".bundle.tmp"])
	}
}

func TestCleanFiltersByRepository(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	keep := fileName("other", testRevision)
	writeFile(t, dir, fileName("example", testRevision), 16)
	writeFile(t, dir, keep, 24)

	result, err := Open(dir).Clean(CleanOptions{Confirm: true, Repository: "example"})
	if err != nil {
		t.Fatalf("Clean() error = %v", err)
	}
	if result.Removed != 1 || result.Bytes != 16 {
		t.Fatalf("result = %#v", result)
	}
	if _, err := os.Stat(filepath.Join(dir, keep)); err != nil {
		t.Fatalf("别的仓库的产物不该被删: %v", err)
	}
	if len(result.Skipped) != 0 {
		t.Fatalf("不在范围内的产物不算「被跳过」: %#v", result.Skipped)
	}
}

// 同一时刻只允许一份清理：另一份在进行时明确报告正忙，不去踩同一个目录。
func TestCleanReportsBusyWhileAnotherCleanRuns(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, fileName("example", testRevision), 16)

	store := Open(dir)
	store.mu.Lock()
	store.cleaning = true
	store.cleanDone = make(chan struct{})
	store.mu.Unlock()

	if _, err := store.Clean(CleanOptions{Confirm: true}); !errors.Is(err, ErrBusy) {
		t.Fatalf("Clean() error = %v, want ErrBusy", err)
	}
	if _, err := os.Stat(filepath.Join(dir, fileName("example", testRevision))); err != nil {
		t.Fatalf("正忙时不该删文件: %v", err)
	}
}

// 正在生成的产物不许删：它既不在成品名单里也不该被当成垃圾，逐条给出原因。
func TestCleanSkipsArtifactsBeingGenerated(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	artifact := fileName("example", testRevision)
	writeFile(t, dir, artifact, 16)
	writeFile(t, dir, artifact+bundle.TemporarySuffix, 4)
	writeFile(t, dir, fileName("other", testRevision), 8)

	store := Open(dir)
	store.mu.Lock()
	store.generating[artifact] = make(chan struct{})
	store.mu.Unlock()

	result, err := store.Clean(CleanOptions{Confirm: true})
	if err != nil {
		t.Fatalf("Clean() error = %v", err)
	}

	if result.Removed != 1 || result.Bytes != 8 {
		t.Fatalf("result = %#v", result)
	}
	if _, err := os.Stat(filepath.Join(dir, artifact)); err != nil {
		t.Fatalf("正在生成的产物不该被删: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, artifact+bundle.TemporarySuffix)); err != nil {
		t.Fatalf("正在生成的临时文件不该被删: %v", err)
	}

	reasons := map[string]string{}
	for _, skipped := range result.Skipped {
		reasons[skipped.Name] = skipped.Reason
	}
	if reasons[artifact] != reasonGenerating || reasons[artifact+bundle.TemporarySuffix] != reasonGenerating {
		t.Fatalf("Skipped = %#v", result.Skipped)
	}
}

// 清理进行期间不开始新的生成：等清理结束再判断产物是否已存在。
func TestEnsureWaitsForClean(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	artifact := fileName("example", testRevision)
	writeFile(t, dir, artifact, 16)

	store := Open(dir)
	store.mu.Lock()
	store.cleaning = true
	store.cleanDone = make(chan struct{})
	store.mu.Unlock()

	type outcome struct {
		path string
		err  error
	}
	finished := make(chan outcome, 1)
	go func() {
		path, err := store.Ensure(context.Background(), "example", testRevision, filepath.Join(dir, "absent"), time.Second)
		finished <- outcome{path: path, err: err}
	}()

	select {
	case early := <-finished:
		t.Fatalf("清理没结束就不该开始生成: %#v", early)
	case <-time.After(50 * time.Millisecond):
	}

	store.endClean()
	got := <-finished
	if got.err != nil || got.path != filepath.Join(dir, artifact) {
		t.Fatalf("Ensure() = %q, %v", got.path, got.err)
	}
}

func TestCleanMissingDirectoryIsNoop(t *testing.T) {
	t.Parallel()

	result, err := Open(filepath.Join(t.TempDir(), "absent")).Clean(CleanOptions{Confirm: true})
	if err != nil {
		t.Fatalf("Clean() error = %v", err)
	}
	if result.Removed != 0 || result.Bytes != 0 || len(result.Skipped) != 0 {
		t.Fatalf("result = %#v", result)
	}
}

// writeFile 在目录下写一个指定大小的普通文件。
func writeFile(t *testing.T, dir string, name string, size int) {
	t.Helper()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), make([]byte, size), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
