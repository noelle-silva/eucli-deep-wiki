package cli

import (
	"fmt"
	"io"
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
