package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"time"
	"unicode/utf8"
)

// File 是一个文件的读取结果。
type File struct {
	Repo       string
	Path       string
	Revision   string
	Content    []byte
	Size       int64
	ModifiedAt time.Time
	ETag       string
}

// File 读取仓库内的文本文件；二进制与超限文件会被明确拒绝。
func (v *Vault) File(name string, requested string) (File, error) {
	rel, err := cleanPath(requested)
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

	return File{
		Repo:       name,
		Path:       rel,
		Revision:   readMetaRevision(name, v.repos[name]),
		Content:    content,
		Size:       info.Size(),
		ModifiedAt: info.ModTime(),
		ETag:       digest(content),
	}, nil
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
