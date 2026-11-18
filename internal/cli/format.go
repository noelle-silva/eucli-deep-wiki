package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
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
