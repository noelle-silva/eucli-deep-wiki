package pack

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadVersionReadsTheSingleSourceOfTruth(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, filepath.FromSlash(versionFile)), `{"version":"1.2.3"}`)

	version, err := readVersion(repo)
	if err != nil {
		t.Fatalf("readVersion() error = %v", err)
	}
	if version != "1.2.3" {
		t.Fatalf("version = %q, want 1.2.3", version)
	}
}

func TestReadVersionRejectsBrokenFacts(t *testing.T) {
	cases := map[string]string{
		"缺少 version": `{}`,
		"空 version":  `{"version":"  "}`,
		"不是结构化数据":    `not json`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			repo := t.TempDir()
			writeFile(t, filepath.Join(repo, filepath.FromSlash(versionFile)), payload)
			if _, err := readVersion(repo); err == nil {
				t.Fatal("readVersion() error = nil, want 失败")
			}
		})
	}
}

func TestReadVersionReportsMissingFile(t *testing.T) {
	if _, err := readVersion(t.TempDir()); err == nil {
		t.Fatal("readVersion() error = nil, want 失败")
	}
}

func TestWriteZipKeepsExactlyTheDeclaredEntries(t *testing.T) {
	work := t.TempDir()
	binary := filepath.Join(work, "source.exe")
	config := filepath.Join(work, "source.json")
	writeFile(t, binary, "binary-payload")
	writeFile(t, config, `{"apiKeys":["k"]}`)

	target := filepath.Join(work, "out.zip")
	entries := []bundleEntry{
		{source: binary, name: ExecutableName},
		{source: config, name: bundleConfig},
	}
	if err := writeZip(target, entries); err != nil {
		t.Fatalf("writeZip() error = %v", err)
	}

	reader, err := zip.OpenReader(target)
	if err != nil {
		t.Fatalf("打开压缩包失败: %v", err)
	}
	defer reader.Close()

	if len(reader.File) != len(entries) {
		t.Fatalf("包内条目数 = %d, want %d", len(reader.File), len(entries))
	}
	for index, file := range reader.File {
		if file.Name != entries[index].name {
			t.Fatalf("包内第 %d 项 = %q, want %q", index, file.Name, entries[index].name)
		}
	}
}

func TestFileDigestMatchesContent(t *testing.T) {
	work := t.TempDir()
	target := filepath.Join(work, "file.bin")
	writeFile(t, target, "abc")

	digest, err := fileDigest(target)
	if err != nil {
		t.Fatalf("fileDigest() error = %v", err)
	}
	// "abc" 的 sha256，取固定值防止算法被无意改掉。
	const want = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if digest != want {
		t.Fatalf("digest = %q, want %q", digest, want)
	}
}

func TestResolveRepoRejectsDirectoryWithoutServer(t *testing.T) {
	if _, err := resolveRepo(t.TempDir()); err == nil {
		t.Fatal("resolveRepo() error = nil, want 失败")
	}
}

func TestArchLabelUsesStoreNaming(t *testing.T) {
	label := archLabel()
	if label == "" {
		t.Fatal("archLabel() 不能为空")
	}
	if label == "amd64" {
		t.Fatal("archLabel() 应使用商店既有叫法 x64，而不是 Go 的内部名")
	}
}

// Artifact 的字段名就是协议动作里 artifact 的取值路径，改名会让商店化读不到成品。
func TestArtifactFieldNamesMatchProtocolPaths(t *testing.T) {
	payload, err := json.Marshal(Artifact{})
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	for _, field := range []string{`"zipPath"`, `"zipName"`, `"sha256"`} {
		if !strings.Contains(string(payload), field) {
			t.Fatalf("Artifact 缺少字段 %s：%s", field, payload)
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
}
