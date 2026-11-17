package vault

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"time"
)

// ArchiveLimits 控制单次导出的体积与条目数。
type ArchiveLimits struct {
	MaxBytes   int64
	MaxEntries int
}

// DefaultArchiveLimits 是导出功能的默认上限。
func DefaultArchiveLimits() ArchiveLimits {
	return ArchiveLimits{MaxBytes: 512 << 20, MaxEntries: 20000}
}

// 支持的打包格式。
const (
	FormatZip   = "zip"
	FormatTarGz = "tar.gz"
)

// ArchiveTarget 指定要导出的仓库与路径，Path 为空表示整个仓库。
type ArchiveTarget struct {
	Repo string
	Path string
}

// ErrArchiveTooLarge 表示导出内容超过上限。
var ErrArchiveTooLarge = fmt.Errorf("archive exceeds size limit")

// WriteArchive 把若干目标边读边写成压缩包。
func (v *Vault) WriteArchive(targets []ArchiveTarget, format string, w io.Writer) error {
	if len(targets) == 0 {
		return ErrInvalidPath
	}

	switch format {
	case FormatTarGz:
		return v.writeTarGz(targets, w)
	default:
		return v.writeZip(targets, w)
	}
}

type archiveEntry struct {
	name    string
	modTime time.Time
	content []byte
}

// collect 遍历目标，按统一规则产出条目；超出上限即停止。
func (v *Vault) collect(targets []ArchiveTarget, handle func(archiveEntry) error) error {
	seen := make(map[string]struct{})
	var totalBytes int64
	var totalEntries int
	limits := v.archiveLimits

	for _, target := range targets {
		rel, err := cleanPath(target.Path)
		if err != nil {
			return err
		}

		root, err := v.open(target.Repo)
		if err != nil {
			return err
		}

		info, err := root.Stat(rel)
		if err != nil {
			root.Close()
			return describe(err)
		}

		if !info.IsDir() {
			content, err := v.readWithinRoot(root, rel)
			root.Close()
			if err != nil {
				return err
			}
			totalBytes += int64(len(content))
			if totalBytes > limits.MaxBytes {
				return ErrArchiveTooLarge
			}
			name := archiveName(target.Repo, rel)
			if _, exists := seen[name]; exists {
				continue
			}
			seen[name] = struct{}{}
			totalEntries++
			if err := handle(archiveEntry{name: name, modTime: info.ModTime(), content: content}); err != nil {
				return err
			}
			continue
		}

		walkErr := fs.WalkDir(root.FS(), rel, func(current string, entry fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if entry.IsDir() {
				if entry.Name() == ".git" {
					return fs.SkipDir
				}
				return nil
			}
			if entry.Name() == ".git" {
				return nil
			}

			if totalEntries >= limits.MaxEntries || totalBytes >= limits.MaxBytes {
				return ErrArchiveTooLarge
			}

			content, err := v.readWithinRoot(root, current)
			if err != nil {
				return nil
			}

			totalBytes += int64(len(content))
			if totalBytes > limits.MaxBytes {
				return ErrArchiveTooLarge
			}

			name := archiveName(target.Repo, current)
			if _, exists := seen[name]; exists {
				return nil
			}
			seen[name] = struct{}{}
			totalEntries++

			info, err := entry.Info()
			if err != nil {
				info = nil
			}
			modTime := time.Time{}
			if info != nil {
				modTime = info.ModTime()
			}
			return handle(archiveEntry{name: name, modTime: modTime, content: content})
		})
		root.Close()
		if walkErr != nil {
			return walkErr
		}
	}
	return nil
}

func archiveName(repo string, rel string) string {
	cleaned := strings.TrimPrefix(path.Clean(rel), "./")
	if cleaned == "." || cleaned == "" {
		return repo + "/"
	}
	return repo + "/" + cleaned
}

func (v *Vault) writeZip(targets []ArchiveTarget, w io.Writer) error {
	writer := zip.NewWriter(w)
	err := v.collect(targets, func(entry archiveEntry) error {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		if !entry.modTime.IsZero() {
			header.Modified = entry.modTime
		}
		file, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		_, err = file.Write(entry.content)
		return err
	})
	if err != nil {
		return err
	}
	return writer.Close()
}

func (v *Vault) writeTarGz(targets []ArchiveTarget, w io.Writer) error {
	compressed := gzip.NewWriter(w)
	writer := tar.NewWriter(compressed)

	err := v.collect(targets, func(entry archiveEntry) error {
		header := &tar.Header{
			Name: entry.name,
			Mode: 0o644,
			Size: int64(len(entry.content)),
		}
		if !entry.modTime.IsZero() {
			header.ModTime = entry.modTime
		}
		if err := writer.WriteHeader(header); err != nil {
			return err
		}
		_, err := writer.Write(entry.content)
		return err
	})
	if err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return compressed.Close()
}

// readWithinRoot 在受控根目录内读取整个文件。
func (v *Vault) readWithinRoot(root *os.Root, rel string) ([]byte, error) {
	handle, err := root.Open(rel)
	if err != nil {
		return nil, describe(err)
	}
	defer handle.Close()

	info, err := handle.Stat()
	if err != nil {
		return nil, describe(err)
	}
	if !info.Mode().IsRegular() {
		return nil, ErrNotRegular
	}
	return io.ReadAll(io.LimitReader(handle, v.limits.MaxFileBytes))
}
