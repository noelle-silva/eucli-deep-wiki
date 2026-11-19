package httpapi

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"eucli-deep-wiki-server/internal/cache"
)

type cacheEntryPayload struct {
	Name       string `json:"name"`
	Repository string `json:"repo"`
	Revision   string `json:"revision"`
	Bytes      int64  `json:"bytes"`
	ModifiedAt string `json:"modifiedAt"`
}

type cacheListPayload struct {
	Files      []cacheEntryPayload `json:"files"`
	TotalBytes int64               `json:"totalBytes"`
}

type cacheCleanPayload struct {
	Removed int   `json:"removed"`
	Bytes   int64 `json:"bytes"`
	Skipped []struct {
		Name   string `json:"name"`
		Reason string `json:"reason"`
	} `json:"skipped"`
}

func TestListCacheReportsArtifacts(t *testing.T) {
	t.Parallel()

	environment := newManagementEnv(t)
	example := "example-" + testRevision + ".bundle"
	environment.writeCacheFile(t, example, 128)
	environment.writeCacheFile(t, "other-abc1234.bundle", 64)
	environment.writeCacheFile(t, "notes.txt", 10)

	recorder := environment.do(t, http.MethodGet, "/v1/cache", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	payload := decodeJSON[cacheListPayload](t, recorder)
	if len(payload.Files) != 2 {
		t.Fatalf("files = %#v", payload.Files)
	}
	if payload.TotalBytes != 192 {
		t.Fatalf("totalBytes = %d", payload.TotalBytes)
	}

	first := payload.Files[0]
	if first.Name != example || first.Repository != "example" || first.Revision != testRevision {
		t.Fatalf("first = %#v", first)
	}
	if first.Bytes != 128 || first.ModifiedAt == "" {
		t.Fatalf("first = %#v", first)
	}

	// 不返回服务端绝对路径：调用方要的是「缓存里有什么」，不是部署目录长什么样。
	if strings.Contains(recorder.Body.String(), environment.cacheDir) {
		t.Fatalf("响应里带了服务端路径: %s", recorder.Body.String())
	}
}

// 目录不存在等于空清单，不是错误；空清单也要是空数组。
func TestListCacheWithoutDirectoryIsEmpty(t *testing.T) {
	t.Parallel()

	environment := newManagementEnv(t)

	recorder := environment.do(t, http.MethodGet, "/v1/cache", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	payload := decodeJSON[cacheListPayload](t, recorder)
	if len(payload.Files) != 0 || payload.TotalBytes != 0 {
		t.Fatalf("payload = %#v", payload)
	}
	if !strings.Contains(recorder.Body.String(), `"files":[]`) {
		t.Fatalf("空清单应编码成空数组: %s", recorder.Body.String())
	}
}

func TestListCacheFiltersByRepository(t *testing.T) {
	t.Parallel()

	environment := newManagementEnv(t)
	environment.writeCacheFile(t, "example-"+testRevision+".bundle", 128)
	environment.writeCacheFile(t, "other-abc1234.bundle", 64)

	recorder := environment.do(t, http.MethodGet, "/v1/cache?repo=example", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	payload := decodeJSON[cacheListPayload](t, recorder)
	if len(payload.Files) != 1 || payload.Files[0].Repository != "example" || payload.TotalBytes != 128 {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestListCacheReportsFailure(t *testing.T) {
	t.Parallel()

	environment := newEnv(t, envOptions{Cache: &stubArtifacts{listErr: errors.New("readdir: access denied")}})

	recorder := environment.do(t, http.MethodGet, "/v1/cache", nil)
	requireFailure(t, recorder, http.StatusInternalServerError, "internal_error")
}

// 清理不可撤销：没确认就拒绝，而且一个文件都不动。
func TestCleanCacheRequiresConfirmation(t *testing.T) {
	t.Parallel()

	environment := newManagementEnv(t)
	artifact := "example-" + testRevision + ".bundle"
	environment.writeCacheFile(t, artifact, 16)

	recorder := environment.do(t, http.MethodDelete, "/v1/cache", nil)
	requireFailure(t, recorder, http.StatusBadRequest, "confirm_required")

	if _, err := os.Stat(filepath.Join(environment.cacheDir, artifact)); err != nil {
		t.Fatalf("没确认时不该删文件: %v", err)
	}
}

// 只删产物文件；目录与不认识的文件留在原地，并逐条说明原因。
func TestCleanCacheRemovesOnlyArtifacts(t *testing.T) {
	t.Parallel()

	environment := newManagementEnv(t)
	artifact := "example-" + testRevision + ".bundle"
	environment.writeCacheFile(t, artifact, 16)
	environment.writeCacheFile(t, "notes.txt", 8)
	if err := os.MkdirAll(filepath.Join(environment.cacheDir, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	recorder := environment.do(t, http.MethodDelete, "/v1/cache?confirm=1", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	payload := decodeJSON[cacheCleanPayload](t, recorder)
	if payload.Removed != 1 || payload.Bytes != 16 {
		t.Fatalf("payload = %#v", payload)
	}
	if _, err := os.Stat(filepath.Join(environment.cacheDir, artifact)); !os.IsNotExist(err) {
		t.Fatalf("产物应已删除: %v", err)
	}

	reasons := map[string]string{}
	for _, skipped := range payload.Skipped {
		reasons[skipped.Name] = skipped.Reason
	}
	if len(reasons) != 2 || reasons["notes.txt"] == "" || reasons["nested"] == "" {
		t.Fatalf("skipped = %#v", payload.Skipped)
	}
	for _, name := range []string{"notes.txt", "nested"} {
		if _, err := os.Stat(filepath.Join(environment.cacheDir, name)); err != nil {
			t.Fatalf("%s 不该被删: %v", name, err)
		}
	}
}

func TestCleanCacheFiltersByRepository(t *testing.T) {
	t.Parallel()

	environment := newManagementEnv(t)
	keep := "other-abc1234.bundle"
	environment.writeCacheFile(t, "example-"+testRevision+".bundle", 16)
	environment.writeCacheFile(t, keep, 24)

	recorder := environment.do(t, http.MethodDelete, "/v1/cache?repo=example&confirm=1", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	payload := decodeJSON[cacheCleanPayload](t, recorder)
	if payload.Removed != 1 || payload.Bytes != 16 || len(payload.Skipped) != 0 {
		t.Fatalf("payload = %#v", payload)
	}
	if _, err := os.Stat(filepath.Join(environment.cacheDir, keep)); err != nil {
		t.Fatalf("别的仓库的产物不该被删: %v", err)
	}
}

// 删除失败与「缓存正忙」是两件事：前者要人去看盘，后者只是时机不对。
func TestCleanCacheReportsEveryFailure(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{
			name:   "删除失败",
			err:    errors.New("remove: access denied"),
			status: http.StatusInternalServerError,
			code:   "remove_failed",
		},
		{
			name:   "缓存正忙",
			err:    cache.ErrBusy,
			status: http.StatusConflict,
			code:   "cache_busy",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			environment := newEnv(t, envOptions{Cache: &stubArtifacts{cleanErr: testCase.err}})

			recorder := environment.do(t, http.MethodDelete, "/v1/cache?confirm=1", nil)
			requireFailure(t, recorder, testCase.status, testCase.code)
		})
	}
}

func TestCleanCacheRejectsInvalidConfirmFlag(t *testing.T) {
	t.Parallel()

	environment := newManagementEnv(t)

	recorder := environment.do(t, http.MethodDelete, "/v1/cache?confirm=maybe", nil)
	requireFailure(t, recorder, http.StatusBadRequest, "invalid_flag")
}
