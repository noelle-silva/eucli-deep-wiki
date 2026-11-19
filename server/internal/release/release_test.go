package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 内置的版本文件必须是一份可用的版本事实：改坏了要在测试里当场露出来。
func TestLoadReturnsFormalVersion(t *testing.T) {
	t.Parallel()

	info, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !formalVersion.MatchString(info.Version) {
		t.Fatalf("Version = %q, want 三段版本号", info.Version)
	}
}

func TestSourcePathPointsAtEmbeddedFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", filepath.FromSlash(SourcePath))
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("版本文件不在 %s: %v", SourcePath, err)
	}
}

func TestParseRejectsNonFormalVersion(t *testing.T) {
	t.Parallel()

	for _, body := range []string{
		`{}`,
		`{"version": ""}`,
		`{"version": "dev"}`,
		`{"version": "1.2"}`,
		`{"version": "1.2.3.4"}`,
		`{"version": "v1.2.3"}`,
		`{"version": "1.2.x"}`,
		`{"version": 123}`,
		`{"version": "1.2.3",`,
	} {
		body := body
		t.Run(body, func(t *testing.T) {
			t.Parallel()

			if _, err := parse([]byte(body)); err == nil {
				t.Fatalf("parse(%s) error = nil, want 版本号被拒绝", body)
			}
		})
	}
}

func TestParseAcceptsFormalVersion(t *testing.T) {
	t.Parallel()

	info, err := parse([]byte(`{"version": " 1.2.3 "}`))
	if err != nil {
		t.Fatalf("parse() error = %v", err)
	}
	if info.Version != "1.2.3" {
		t.Fatalf("Version = %q", info.Version)
	}
}

// 版本文件的报错必须点名文件，否则改坏了不知道去改哪儿。
func TestParseErrorsMentionSourcePath(t *testing.T) {
	t.Parallel()

	_, err := parse([]byte(`{"version": "dev"}`))
	if err == nil {
		t.Fatal("parse() error = nil, want 版本号无效")
	}
	if !strings.Contains(err.Error(), SourcePath) {
		t.Fatalf("错误里没有版本文件位置: %s", err.Error())
	}
}
