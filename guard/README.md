# 契约守护者 `eucli-deep-wiki-contract-guard`

这个模块不属于任何一侧：它谁也不被谁依赖，却同时认识两侧，用**真实的服务端程序**
与**真实的命令行程序**核对同一份契约。

本模块在 `contract/` 包中维护可执行的契约定义，两侧的实现必须满足它。

## 怎么跑

在本目录里：

```bash
go run ./cmd/contract-guard              # 跑全部用例，打印通过/失败汇总
go run ./cmd/contract-guard -coverage    # 只打印「每条契约事实由哪几条用例守着」
go run ./cmd/contract-guard -filter 数字  # 只跑标识里含这段文字的用例
go test ./...                            # 等价于第一条，并把每条用例变成一个子测试
```

退出码：`0` 全绿，`1` 有偏移。

## 它为什么能站在两侧之外

`harness/` **不 import 任何一侧的包**，`go.mod` 里也**不 require 任何一侧**：
两侧在它眼里就是两个可执行文件——`eucli-deep-wiki-server` 与 `eucli-dw`。
它按路径找到源码根，用 `GOWORK=off` 自己 `go build` 出这两个程序（各按各自的模块
定义独立构建），起一个真实服务端进程，再用真实命令行进程发真实请求。
中间的 `proxy.go` 只是挡在服务端前面的转发探针：请求与响应原样穿过，它只记一笔。

因此：

- 两侧的源码与模块定义里都没有它的名字（`ownership.no_reference` 守）；
- 两侧单独构建（`GOWORK=off`）不需要它存在（`ownership.independent_build` 守）；
- 镜像构建只 `COPY server/go.mod`、`server/cmd`、`server/internal` 三处，
  从来不会把 `guard/` 带进去——`GOWORK=off` 也保证它不会去找工作区文件。

证据（在本目录执行，三者都必须成立）：

```bash
GOWORK=off go list -m all              # 只有 eucli-deep-wiki-contract-guard 自己
cd ../server && GOWORK=off go build ./...   # 不看工作区、不看 cli、不看 guard
cd ../cli    && GOWORK=off go build ./...   # 同上
```

## 目录

```
guard/
├── contract/          # 契约事实的唯一出处：接口、字段、错误、退出码、数字、取值集合
│   ├── contract.go    # 接口表（22 个）、参数名、响应字段、正文形态
│   ├── errors.go      # 17 条错误标识：状态码 + 命令行退出码 + 怎么真实诱发
│   ├── numbers.go     # 六个两侧各持一份的数字、行区间写法表
│   ├── shapes.go      # 嵌套对象字段名、响应头名、动作与格式取值
│   ├── commands.go    # 命令行 → 接口的对应关系（26 行）
│   ├── roles.go       # 现场里固定角色的仓库名
│   ├── coverage.go    # 覆盖门禁：契约有、用例没声明守住的事实一律变红
│   └── route.go       # 路由模板 → 正则（用来核对命令行实际打了哪个路径）
├── harness/           # 真实现场
│   ├── build.go       # 用 GOWORK=off 分别构建两侧程序
│   ├── server.go      # 起/停真实服务端进程，等它就绪
│   ├── proxy.go       # 转发探针：记录请求与响应，不改一个字节
│   ├── command.go     # 驱动真实命令行进程，收集退出码与输出
│   ├── world.go       # 一次隔离验证的全部现场（铺盘 → 起服务 → 等空闲 → 拆）
│   ├── seed.go        # 各现场的磁盘内容（名单、状态、缓存、仓库目录）
│   ├── gitfixture.go  # 用真实 git 命令做出完整/浅/游离/坏远端/锁配置的仓库
│   ├── blackhole.go   # 「接了电话不说话」的 TCP 服务：稳稳占住一次网络操作
│   ├── gate.go        # 用真实同步占住服务端闸门（触发 ->sync_busy）
│   ├── lock.go        # 让「删仓库目录」真的失败（触发 remove_failed）
│   ├── configprobe.go # 用真实服务端程序判断一份配置能不能通过校验
│   ├── probe.go       # 直接打接口的探针（服务端侧断言用它）
│   └── stub.go        # 只回答固定错误的极小服务端（喂命令行退出码映射）
├── check.go           # 用例模型：偏移只说「哪一侧、哪个事实、期望什么、实际什么」
├── suite.go           # 用例清单与运行器；一条用例一份现场
├── report.go          # 通过/失败汇总与覆盖清单
├── ownership.go       # 归属一族：不被引用、独立构建、工作区组合
├── api_read.go        # 接口一族：读类接口的地址、字段与取值
├── api_manage.go      # 接口一族：写类接口（加入/移出/解封/瘦身/缓存）
├── body.go            # 正文与响应头一族：纯文本、截断标记、下载头
├── numbers.go         # 数字一族：上限、缺省值与真收敛
├── errors.go          # 错误一族：状态码 + 标识 + 退出码
├── command.go         # 命令行一族：命令走哪个接口、带哪个参数
├── parsing.go         # 命令行一族：回包字段真的被解析成可用事实
└── cmd/contract-guard # 一条命令跑完全部验证
```

## 一条用例怎么工作

1. `suite.go` 按 `Check.World` 开一份**独立现场**（临时目录、自己的数据根与服务端端口）。
2. 用例用两种方式取证：
   - `world.Get / Post / PostJSON / Delete` —— 直接打接口，断言「服务端说什么」；
   - `world.Command.Run(...)` —— 跑真实命令行，断言「命令行怎么退、发了什么请求、解析出什么」。
3. 任何一处不符合契约，`Reporter` 记一条 `Deviation`：`[哪一侧] 哪个事实：期望 X，实际 Y`。
4. 用例只声明 `Guards`（它守哪些契约事实），覆盖门禁再把契约表与全部声明对账。
5. 一条用例一个现场，因此**一处偏移只让相关的那几条变红**。

## 已说明的缺口

覆盖门禁允许「写明原因的缺口」，但不允许沉默的空白。当前有 3 条，报告里会单独列出：

| 缺口 | 原因 |
|---|---|
| `cache_busy`（服务端） | 需要两次清理真的并发；为此造一个「大到以毫秒计仍在删除」的缓存目录，等于把确定性断言换成时序竞赛。命令行的退出码映射仍被守住。 |
| `invalid_body`（命令行） | 命令行的调用层永远自己序列化请求体，用户没有入口让它发出坏正文；这条标识只可能由服务端产出。命令行的退出码映射仍被守住。 |
| `MaxFindLimit`（服务端） | 服务端只有在匹配数超过一万时才体现这条上限；铺一万个文件的现场实测要 6 秒以上。命令行的同一个数与缺省 200 的真收敛仍被守住。 |

`sync_busy` 曾经也在这张表里，现在不是：`harness/blackhole.go` 能造一个
「接了电话不说话」的远端，那一轮真实同步会稳稳卡在握手上，闸门因此被真实占住；
`error.sync_busy` 就在这个状态下让瘦身走完整条服务端路径。
