package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"eucli-deep-wiki-pack/internal/pack"
)

// 标准输出只留给最终结果：协议工具把整段标准输出当 JSON 解析，
// 任何多余字符都会让它读不到成品路径。
func main() {
	repoRoot := flag.String("repo", "", "应用根目录；留空时按当前目录上两级推断")
	outputDir := flag.String("out", "", "压缩包落点；留空时落在协议目录的 dist/ 下")
	flag.Parse()

	artifact, err := pack.Package(pack.Options{
		RepoRoot:  *repoRoot,
		OutputDir: *outputDir,
		LogWriter: os.Stderr,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "打包失败: %v\n", err)
		os.Exit(1)
	}

	encoder := json.NewEncoder(os.Stdout)
	if err := encoder.Encode(artifact); err != nil {
		fmt.Fprintf(os.Stderr, "输出结果失败: %v\n", err)
		os.Exit(1)
	}
}
