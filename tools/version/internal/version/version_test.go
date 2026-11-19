package version

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseBumpAcceptsSupportedSteps(t *testing.T) {
	for _, raw := range []string{"patch", "minor", "major", " patch "} {
		if _, err := ParseBump(raw); err != nil {
			t.Fatalf("ParseBump(%q) error = %v", raw, err)
		}
	}
	if _, err := ParseBump("hotfix"); err == nil {
		t.Fatal("ParseBump() 应拒绝不支持的档位")
	}
}

func TestBumpKeepsTheOtherSegments(t *testing.T) {
	cases := []struct {
		from string
		step Bump
		want string
	}{
		{"1.2.3", BumpPatch, "1.2.4"},
		{"1.2.3", BumpMinor, "1.3.0"},
		{"1.2.3", BumpMajor, "2.0.0"},
	}
	for _, testCase := range cases {
		numbers, err := parse(testCase.from)
		if err != nil {
			t.Fatalf("parse(%q) error = %v", testCase.from, err)
		}
		if got := numbers.bump(testCase.step).String(); got != testCase.want {
			t.Fatalf("%s + %s = %s, want %s", testCase.from, testCase.step, got, testCase.want)
		}
	}
}

func TestParseRejectsBrokenVersions(t *testing.T) {
	for _, raw := range []string{"1.2", "1.2.3.4", "a.b.c", "1..3", "", "1.2."} {
		if _, err := parse(raw); err == nil {
			t.Fatalf("parse(%q) 应失败", raw)
		}
	}
}

func TestApplyWritesBackAndKeepsOtherFields(t *testing.T) {
	repo := t.TempDir()
	target := filepath.Join(repo, filepath.FromSlash(ReleaseFile))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	if err := os.WriteFile(target, []byte(`{"version":"0.1.0","channel":"stable"}`), 0o644); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}

	before, after, err := Apply(repo, BumpMinor)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if before != "0.1.0" || after != "0.2.0" {
		t.Fatalf("before = %q, after = %q, want 0.1.0 -> 0.2.0", before, after)
	}

	payload, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatalf("回读解析失败: %v", err)
	}
	if fields["version"] != "0.2.0" {
		t.Fatalf("写回的 version = %v", fields["version"])
	}
	if fields["channel"] != "stable" {
		t.Fatalf("其余字段被弄丢了: %v", fields)
	}
}

func TestCurrentReportsMissingFile(t *testing.T) {
	if _, err := Current(t.TempDir()); err == nil {
		t.Fatal("Current() 应对缺失文件报错")
	}
}

func TestCurrentRejectsBlankVersion(t *testing.T) {
	repo := t.TempDir()
	target := filepath.Join(repo, filepath.FromSlash(ReleaseFile))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	if err := os.WriteFile(target, []byte(`{"version":"   "}`), 0o644); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
	if _, err := Current(repo); err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("Current() error = %v, want 提到 version", err)
	}
}
