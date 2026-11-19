package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"time"
	"unicode/utf8"
)

// FileOptions 描述一次文件读取。
type FileOptions struct {
	// Path 是仓库内的相对路径。
	Path string
	// Lines 限定返回的行区间；零值表示整份文件。
	Lines LineRange
	// MaxBytes 限定返回的字节数；0 表示不额外限制。
	MaxBytes int64
}

// File 是一次文件读取的结果。
//
// TotalLines 与 FirstLine/LastLine 一起把「文件有多长、这次给的是哪一段」
// 说清楚；内容不完整时 Truncated 说明原因，调用方据此提示如何继续。
type File struct {
	Repo     string
	Path     string
	Revision string
	Content  []byte
	// Size 是文件总字节数。
	Size int64
	// TotalLines 是文件总行数。
	TotalLines int
	// FirstLine 与 LastLine 是本次内容覆盖的行范围（1 基，含）；空内容时为 0。
	FirstLine int
	LastLine  int
	// Truncated 说明内容为何不完整："bytes" 表示被 MaxBytes 截断。
	Truncated  string
	ModifiedAt time.Time
	ETag       string
}

// 文件内容被字节上限截断时的原因标记。
const truncatedByBytes = "bytes"

// File 读取仓库内的文本文件；二进制与超限文件会被明确拒绝。
func (v *Vault) File(name string, options FileOptions) (File, error) {
	rel, err := cleanPath(options.Path)
	if err != nil {
		return File{}, err
	}
	if rel == "." {
		return File{}, ErrNotRegular
	}

	root, err := v.open(name)
	if err != nil {
		return File{}, err
	}
	defer root.Close()

	handle, err := root.Open(rel)
	if err != nil {
		return File{}, describe(err)
	}
	defer handle.Close()

	info, err := handle.Stat()
	if err != nil {
		return File{}, describe(err)
	}
	if !info.Mode().IsRegular() {
		return File{}, ErrNotRegular
	}
	if info.Size() > v.limits.MaxFileBytes {
		return File{}, fmt.Errorf("%w: %d bytes", ErrTooLarge, info.Size())
	}

	content, err := io.ReadAll(io.LimitReader(handle, v.limits.MaxFileBytes+1))
	if err != nil {
		return File{}, describe(err)
	}
	if int64(len(content)) > v.limits.MaxFileBytes {
		return File{}, fmt.Errorf("%w: %d bytes", ErrTooLarge, info.Size())
	}
	if !isText(content) {
		return File{}, ErrBinary
	}

	file := File{
		Repo:       name,
		Path:       rel,
		Revision:   readMetaRevision(name, v.repos[name]),
		Size:       info.Size(),
		TotalLines: countLines(content),
		ModifiedAt: info.ModTime(),
		ETag:       digest(content),
	}

	if options.Lines.IsZero() {
		file.Content = content
		file.FirstLine = 1
		file.LastLine = file.TotalLines
	} else {
		file.Content, file.FirstLine, file.LastLine = sliceLines(content, options.Lines)
	}

	file.Content, file.LastLine, file.Truncated = clampContent(file.Content, file.FirstLine, file.LastLine, options.MaxBytes)
	return file, nil
}

// clampContent 应用字节上限，并把截断原因收敛成结果里的标记。
// 没有发生截断时原因留空，客户端据此判断要不要提示「还有后续」。
func clampContent(content []byte, first int, last int, limit int64) ([]byte, int, string) {
	if limit <= 0 {
		return content, last, ""
	}

	kept, newLast, truncated := clampToBytes(content, first, limit, last)
	if !truncated {
		return kept, newLast, ""
	}
	return kept, newLast, truncatedByBytes
}

// isText 用 UTF-8 合法性与 NUL 字节做粗判，避免把二进制塞进响应。
func isText(content []byte) bool {
	if !utf8.Valid(content) {
		return false
	}
	for _, b := range content {
		if b == 0 {
			return false
		}
	}
	return true
}

func digest(content []byte) string {
	sum := sha256.Sum256(content)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}
