package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"eucli-deep-wiki/internal/apiclient"
)

// runStatus 打印服务端同步状态。
func runStatus(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("status", flag.ContinueOnError)
	flags.SetOutput(stderr)
	setupHelp(flags, "eucli-dw status [参数]")
	options := &clientFlags{}
	options.register(flags, true)

	if err := parseArgs(flags, args); err != nil {
		return parseExitCode(err)
	}

	client, err := options.client()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitFailure
	}

	raw, err := client.Status(context.Background())
	if err != nil {
		reportError(stderr, err, options.asJSON)
		return exitCodeFor(err)
	}

	if options.asJSON {
		if _, err := stdout.Write(raw); err != nil {
			fmt.Fprintf(stderr, "输出失败: %v\n", err)
			return exitFailure
		}
		fmt.Fprintln(stdout)
		return 0
	}

	code := renderStatus(stdout, stderr, raw)
	if code != 0 {
		return code
	}

	if states, err := client.State(context.Background()); err == nil {
		printBlocked(stdout, states)
	}
	return 0
}

func renderStatus(stdout io.Writer, stderr io.Writer, raw json.RawMessage) int {
	var snapshot struct {
		Running        bool   `json:"running"`
		Total          int    `json:"total"`
		LastStartedAt  string `json:"lastStartedAt"`
		LastFinishedAt string `json:"lastFinishedAt"`
		NextRunAt      string `json:"nextRunAt"`
		Results        []struct {
			Name   string `json:"name"`
			Action string `json:"action"`
			Error  string `json:"error"`
		} `json:"results"`
		SingleResults []struct {
			Name   string `json:"name"`
			Action string `json:"action"`
			Error  string `json:"error"`
		} `json:"singleResults"`
		ConfigLoadedAt  string   `json:"configLoadedAt"`
		IndexLoadedAt   string   `json:"indexLoadedAt"`
		RepositoryCount int      `json:"repositoryCount"`
		SyncInterval    string   `json:"syncInterval"`
		GCThresholdMB   int      `json:"gcThresholdMB"`
		NeedsRestart    []string `json:"needsRestart"`
		LastReload      struct {
			At             string `json:"at"`
			ConfigReloaded bool   `json:"configReloaded"`
			IndexReloaded  bool   `json:"indexReloaded"`
			Error          string `json:"error"`
		} `json:"lastReload"`
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		fmt.Fprintf(stderr, "解析状态失败: %v\n", err)
		return exitFailure
	}

	state := "空闲"
	if snapshot.Running {
		state = "运行中"
	}

	fmt.Fprintf(stdout, "状态:     %s\n", state)
	if snapshot.Running {
		fmt.Fprintf(stdout, "本轮开始: %s\n", orDash(snapshot.LastStartedAt))
		fmt.Fprintf(stdout, "本轮进度: %d/%d\n", len(snapshot.Results), snapshot.Total)
		fmt.Fprintf(stdout, "下轮预计: %s\n", orDash(snapshot.NextRunAt))
	} else {
		fmt.Fprintf(stdout, "上轮开始: %s\n", orDash(snapshot.LastStartedAt))
		fmt.Fprintf(stdout, "上轮结束: %s\n", orDash(snapshot.LastFinishedAt))
		fmt.Fprintf(stdout, "下轮预计: %s\n", orDash(snapshot.NextRunAt))
	}

	fmt.Fprintf(stdout, "仓库数:   %d\n", snapshot.RepositoryCount)
	fmt.Fprintf(stdout, "同步周期: %s\n", orDash(snapshot.SyncInterval))
	fmt.Fprintf(stdout, "配置加载: %s\n", orDash(snapshot.ConfigLoadedAt))
	fmt.Fprintf(stdout, "索引加载: %s\n", orDash(snapshot.IndexLoadedAt))

	counts := map[string]int{}
	failures := []string{}
	for _, result := range snapshot.Results {
		counts[result.Action]++
		if result.Error != "" {
			failures = append(failures, result.Name+": "+result.Error)
		}
	}

	if snapshot.Running {
		fmt.Fprintf(stdout, "已处理:   %d 个仓库 %s\n", len(snapshot.Results), formatCounts(counts))
	} else {
		fmt.Fprintf(stdout, "上轮结果: %d 个仓库 %s\n", len(snapshot.Results), formatCounts(counts))
	}

	if len(snapshot.SingleResults) > 0 {
		last := snapshot.SingleResults[len(snapshot.SingleResults)-1]
		fmt.Fprintf(stdout, "最近按需: %s %s\n", last.Name, last.Action)
		if last.Error != "" {
			failures = append(failures, last.Name+": "+last.Error)
		}
	}

	if len(failures) > 0 {
		fmt.Fprintln(stdout, "失败明细:")
		for _, line := range failures {
			fmt.Fprintf(stdout, "  %s\n", line)
		}
	}

	fmt.Fprintf(stdout, "最近重载: %s\n", formatReload(snapshot.LastReload.At, snapshot.LastReload.ConfigReloaded, snapshot.LastReload.IndexReloaded, snapshot.LastReload.Error))
	if len(snapshot.NeedsRestart) > 0 {
		fmt.Fprintf(stdout, "需要重启: %s（改在配置文件里，重启服务后才生效）\n", strings.Join(snapshot.NeedsRestart, ", "))
	}
	return 0
}

// formatReload 描述最近一次重载的结果；失败时把原因一并写出。
func formatReload(at string, configReloaded bool, indexReloaded bool, reloadError string) string {
	if reloadError != "" {
		return "失败（" + reloadError + "）"
	}
	if at == "" {
		return "-"
	}
	parts := make([]string, 0, 2)
	if configReloaded {
		parts = append(parts, "配置")
	}
	if indexReloaded {
		parts = append(parts, "索引")
	}
	if len(parts) == 0 {
		return "-"
	}
	return "成功（" + strings.Join(parts, "+") + "）"
}

// printBlocked 列出因体积保护停止同步的仓库。
func printBlocked(stdout io.Writer, states map[string]apiclient.RepoState) {
	names := make([]string, 0, len(states))
	for name, state := range states {
		if state.Blocked {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return
	}
	sort.Strings(names)

	fmt.Fprintf(stdout, "停止同步: %d 个仓库\n", len(names))
	for _, name := range names {
		fmt.Fprintf(stdout, "  %s (%s)\n", name, formatBytes(states[name].BlockedSize))
	}
}

func formatCounts(counts map[string]int) string {
	if len(counts) == 0 {
		return "(空)"
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, counts[key]))
	}
	return strings.Join(parts, " ")
}

func orDash(value string) string {
	if value == "" || strings.HasPrefix(value, "0001-01-01") {
		return "-"
	}
	return value
}
