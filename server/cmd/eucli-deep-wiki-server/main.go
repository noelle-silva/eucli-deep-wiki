// eucli-deep-wiki-server 是参考仓库服务端：持有仓库目录、跑版本管理命令、
// 维护名单与同步状态，并通过接口对外提供内容与管理能力。
//
// 它只做服务端的事：读自己的配置、启动服务、同步仓库。
package main

import (
	"os"

	"eucli-deep-wiki-server/internal/service"
)

func main() {
	os.Exit(service.Main(os.Args[1:], os.Stderr))
}
