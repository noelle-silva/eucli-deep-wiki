package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"eucli-deep-wiki-version/internal/version"
)

// 标准输出只留给结果：协议工具按 JSON 读它。
func main() {
	repoRoot := flag.String("repo", "", "应用根目录；留空时按当前目录上两级推断")
	bump := flag.String("bump", "", "提升档位：patch / minor / major；留空表示只查看")
	flag.Parse()

	repo, err := resolveRepo(*repoRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "定位应用根失败: %v\n", err)
		os.Exit(1)
	}

	before := ""
	after := ""
	if *bump == "" {
		current, err := version.Current(repo)
		if err != nil {
			fmt.Fprintf(os.Stderr, "读取版本失败: %v\n", err)
			os.Exit(1)
		}
		before, after = current, current
	} else {
		step, err := version.ParseBump(*bump)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		before, after, err = version.Apply(repo, step)
		if err != nil {
			fmt.Fprintf(os.Stderr, "提升版本失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "版本 %s -> %s（%s）\n", before, after, step)
	}

	payload := map[string]string{"before": before, "version": after, "file": version.ReleaseFile}
	encoder := json.NewEncoder(os.Stdout)
	if err := encoder.Encode(payload); err != nil {
		fmt.Fprintf(os.Stderr, "输出结果失败: %v\n", err)
		os.Exit(1)
	}
}

func resolveRepo(given string) (string, error) {
	repo := given
	if repo == "" {
		// 约定：在 tools/version 目录里运行，应用根是它的上两级。
		working, err := os.Getwd()
		if err != nil {
			return "", err
		}
		repo = filepath.Dir(filepath.Dir(working))
	}
	return filepath.Abs(repo)
}
