package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"runtime/debug"
	"sort"

	"eucli-deep-wiki/internal/apiclient"
	"strings"
)

// runRepos 列出服务器上的仓库，这就是客户端唯一需要的“清单入口”。
func runRepos(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("repos", flag.ContinueOnError)
	flags.SetOutput(stderr)
	setupHelp(flags, "eucli-dw repos [参数]")
	options := &clientFlags{}
	options.register(flags, true)

	if err := parseArgs(flags, args); err != nil {
		return parseExitCode(err)
	}

	client, err := options.client()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	repositories, err := client.Repositories(context.Background())
	if err != nil {
		reportError(stderr, err, options.asJSON)
		return exitCodeFor(err)
	}

	states, stateErr := client.State(context.Background())
	if stateErr != nil {
		states = map[string]apiclient.RepoState{}
	}

	if options.asJSON {
		return writeJSON(stdout, stderr, map[string]any{
			"repositories": repositories,
			"states":       states,
		})
	}

	for _, repository := range repositories {
		branch := repository.Branch
		if branch == "" {
			branch = "-"
		}
		revision := repository.Revision
		if len(revision) > 12 {
			revision = revision[:12]
		}
		if revision == "" {
			revision = "-"
		}

		marker := ""
		if state, ok := states[repository.Name]; ok && state.Blocked {
			marker = fmt.Sprintf("\tBLOCKED(%d MB)", state.BlockedSize>>20)
		}
		fmt.Fprintf(stdout, "%s\t%s\t%s%s\n", repository.Name, branch, revision, marker)
	}
	return 0
}

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
		return 1
	}

	raw, err := client.Status(context.Background())
	if err != nil {
		reportError(stderr, err, options.asJSON)
		return exitCodeFor(err)
	}

	if options.asJSON {
		if _, err := stdout.Write(raw); err != nil {
			fmt.Fprintf(stderr, "输出失败: %v\n", err)
			return 1
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
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		fmt.Fprintf(stderr, "解析状态失败: %v\n", err)
		return 1
	}

	state := "空闲"
	if snapshot.Running {
		state = "运行中"
	}

	fmt.Fprintf(stdout, "状态:     %s\n", state)
	fmt.Fprintf(stdout, "上轮开始: %s\n", orDash(snapshot.LastStartedAt))
	fmt.Fprintf(stdout, "上轮结束: %s\n", orDash(snapshot.LastFinishedAt))
	fmt.Fprintf(stdout, "下轮预计: %s\n", orDash(snapshot.NextRunAt))

	counts := map[string]int{}
	failures := []string{}
	for _, result := range snapshot.Results {
		counts[result.Action]++
		if result.Error != "" {
			failures = append(failures, result.Name+": "+result.Error)
		}
	}
	fmt.Fprintf(stdout, "上轮结果: %d 个仓库 %s\n", len(snapshot.Results), formatCounts(counts))

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
	return 0
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
		fmt.Fprintf(stdout, "  %s (%d MB)\n", name, states[name].BlockedSize>>20)
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

// runVersion 打印构建信息，用于确认线上跑的是哪个版本。
func runVersion(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("version", flag.ContinueOnError)
	flags.SetOutput(stderr)
	setupHelp(flags, "eucli-dw version")
	asJSON := flags.Bool("json", false, "输出 JSON")

	if err := parseArgs(flags, args); err != nil {
		return parseExitCode(err)
	}

	info := buildInfo()
	if *asJSON {
		return writeJSON(stdout, stderr, info)
	}

	fmt.Fprintf(stdout, "eucli-dw %s\n", info["version"])
	fmt.Fprintf(stdout, "go:     %s\n", info["go"])
	if info["revision"] != "" {
		fmt.Fprintf(stdout, "commit: %s\n", info["revision"])
	}
	if info["time"] != "" {
		fmt.Fprintf(stdout, "built:  %s\n", info["time"])
	}
	return 0
}

func buildInfo() map[string]string {
	result := map[string]string{
		"version": "dev",
		"go":      "unknown",
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return result
	}
	result["go"] = info.GoVersion
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		result["version"] = info.Main.Version
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			result["revision"] = setting.Value
		case "vcs.time":
			result["time"] = setting.Value
		}
	}
	return result
}
