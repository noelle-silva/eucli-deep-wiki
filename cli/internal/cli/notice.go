package cli

import (
	"fmt"
	"io"
	"os"
)

// noteClamped 说明某个参数已被按上限处理。
//
// 请求本身是成功的，被收敛这件事必须说出来：不说的话，「为什么只给了这么多」
// 就只能靠猜。上限来自命令行自己的 params 包，与服务端实际生效的值一致。
// 没有超过上限时不提示——正常范围内的请求不需要解释。
func noteClamped(stderr io.Writer, name string, requested int, limit int) {
	if requested <= limit {
		return
	}
	fmt.Fprintf(stderr, "（--%s 已按上限 %d 处理）\n", name, limit)
}

// noteOnce 是把「这条说明只说一次」落地的唯一实现。
//
// 有些说明是在讲一个长期有效的默认规矩（例如「默认不列出依赖目录」），而不是
// 这一次请求的结果。批量探查时同一条规矩每次调用都重复一遍，就变成了噪声：
// 连着跑四个 tree，屏幕上刷四遍同一份清单，还恰好挤在标题与正文之间。
//
// 所以口径按「有没有人在读屏幕」分两种：
//
//   - 终端交互：说一次就够。进程内记下已经说过的内容，同一段文字不再重复。
//   - 输出被重定向或管道接走：一个字都不说。这时候对面多半是脚本或程序，
//     它不读这些说明，而每一个字都会进入别人的日志与文件。
//
// 「是不是终端」由 caller 给，不在这里猜：判定依赖 stdio 的形态，属于命令入口
// 的职责，收在这一层会让单元测试无法构造两种情形。
type noticeLog struct {
	seen map[string]struct{}
}

// noteOnce 在终端交互时把 message 播报一次；非交互时始终静默。
func (l *noticeLog) noteOnce(stderr io.Writer, interactive bool, message string) {
	if !interactive || message == "" {
		return
	}
	if l.seen == nil {
		l.seen = make(map[string]struct{})
	}
	if _, exists := l.seen[message]; exists {
		return
	}
	l.seen[message] = struct{}{}
	fmt.Fprintln(stderr, message)
}

// isTerminal 判断数据出口是不是一个交互终端。
//
// 只看标准输出：数据从它出去，它接的是屏幕还是管道，决定这次调用有没有人在读
// 屏幕。判定基于传进来的那个流而不是进程的 os.Stdout，这样两种情形都能被测试
// 构造出来——传一个内存缓冲区就是管道，传真正的终端文件就是交互。
func isTerminal(out io.Writer) bool {
	file, ok := out.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	// 字符设备即终端；普通文件与管道都不是。
	return info.Mode()&os.ModeCharDevice != 0
}
