package cli

import (
	"errors"
	"flag"
	"fmt"
	"strings"
)

// parseArgs 允许参数与位置参数任意穿插。
//
// 标准库的 flag 只接受「参数全部写在位置参数之前」，而实际调用时
// 人们和 Agent 都习惯把 --server 之类的参数写在末尾，这里先重排再解析。
func parseArgs(flags *flag.FlagSet, args []string) error {
	options := make([]string, 0, len(args))
	positional := make([]string, 0, len(args))

	for index := 0; index < len(args); index++ {
		arg := args[index]

		if arg == "--" {
			positional = append(positional, args[index+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positional = append(positional, arg)
			continue
		}

		options = append(options, arg)

		name := strings.TrimLeft(arg, "-")
		if strings.Contains(name, "=") {
			continue
		}
		defined := flags.Lookup(name)
		if defined == nil || isBoolFlag(defined) {
			continue
		}
		if index+1 < len(args) {
			index++
			options = append(options, args[index])
		}
	}

	err := flags.Parse(append(options, positional...))
	if errors.Is(err, flag.ErrHelp) {
		return errHelpRequested
	}
	return err
}

// errHelpRequested 表示用户主动要帮助，不算失败。
var errHelpRequested = errors.New("help requested")

// parseExitCode 把解析结果换算成退出码。
func parseExitCode(err error) int {
	if errors.Is(err, errHelpRequested) {
		return 0
	}
	return 2
}

// setupHelp 统一子命令的帮助输出。
func setupHelp(flags *flag.FlagSet, synopsis string) {
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "用法: %s\n", synopsis)
		flags.PrintDefaults()
	}
}

// wantsHelp 判断参数里是否带有帮助开关。
func wantsHelp(args []string) bool {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" || arg == "help" {
			return true
		}
	}
	return false
}

func isBoolFlag(defined *flag.Flag) bool {
	boolean, ok := defined.Value.(interface{ IsBoolFlag() bool })
	return ok && boolean.IsBoolFlag()
}
