package cli

import (
	"flag"
	"fmt"
	"io"
	"runtime/debug"
)

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
