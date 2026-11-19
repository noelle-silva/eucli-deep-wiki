// 命令 contract-guard 是契约守护者的一次独立验证运行。
//
// 跑法（在 guard 模块目录里）：
//
//	go run ./cmd/contract-guard              # 跑全部用例并给出汇总
//	go run ./cmd/contract-guard -coverage    # 只输出「每条契约事实由哪几条用例守着」
//	go run ./cmd/contract-guard -filter 数字  # 只跑标识里含这段文字的用例
//
// 退出码：0 全绿，1 有偏移。它只在本地验证阶段运行，不进任何一侧的产物。
package main

import (
	"flag"
	"fmt"
	"os"

	contractguard "eucli-deep-wiki-contract-guard"
)

func main() {
	filter := flag.String("filter", "", "只跑标识里包含这段文字的用例（此时不跑契约覆盖门禁）")
	coverage := flag.Bool("coverage", false, "只输出「每条契约事实由哪几条用例守着」的对照，不跑用例")
	flag.Parse()

	if *coverage {
		if err := contractguard.WriteCoverage(os.Stdout, contractguard.Checks()); err != nil {
			fmt.Fprintf(os.Stderr, "输出覆盖清单失败: %v\n", err)
			os.Exit(1)
		}
		return
	}

	summary := contractguard.Run(contractguard.RunOptions{Filter: *filter})
	if err := contractguard.WriteReport(os.Stdout, summary); err != nil {
		fmt.Fprintf(os.Stderr, "输出汇总失败: %v\n", err)
		os.Exit(1)
	}
	if !summary.Passed() {
		os.Exit(1)
	}
}
