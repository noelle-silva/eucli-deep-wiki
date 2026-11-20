package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"eucli-dw/internal/apiclient"
)

// writeJSON 输出缩进的 JSON，供脚本与 Agent 直接消费。
func writeJSON(stdout io.Writer, stderr io.Writer, value any) int {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintf(stderr, "输出失败: %v\n", err)
		return exitFailure
	}
	return 0
}

// writeEntries 输出目录项：目录带尾斜杠，文件带字节数。
//
// tree 与 overview 的骨架用的是同一份口径，两处不各写一遍。
func writeEntries(stdout io.Writer, entries []apiclient.Entry) {
	for _, entry := range entries {
		if entry.Type == "dir" {
			fmt.Fprintf(stdout, "%s/\n", entry.Path)
			continue
		}
		fmt.Fprintf(stdout, "%s\t%d\n", entry.Path, entry.Size)
	}
}

// formatBytes 把字节数写成便于阅读的单位。
func formatBytes(size int64) string {
	if size < 0 {
		return "-"
	}

	const step = 1024
	if size < step {
		return fmt.Sprintf("%d B", size)
	}

	suffixes := []string{"KB", "MB", "GB", "TB"}
	value := float64(size)
	unit := -1
	for value >= step && unit < len(suffixes)-1 {
		value /= step
		unit++
	}
	return fmt.Sprintf("%.1f %s", value, suffixes[unit])
}

// formatTime 输出本地时间；零值写成短横线，避免「0001-01-01」这种东西出现。
func formatTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.Local().Format("2006-01-02 15:04")
}
