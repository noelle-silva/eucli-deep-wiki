# Eucli Deep Wiki

一个用于维护本地参考代码库的轻量服务。它按配置把多个 GitHub 仓库同步到本地资料区，并让这些参考仓库始终保持干净、可更新的状态。

## 项目用途

本项目的目标是把多个外部仓库作为参考资料集中放置，并由服务定期更新。

参考仓库只用于查阅，不作为本地开发副本使用。同步时，服务会清理参考仓库里的本地改动、临时文件和不符合配置的残留目录，然后重新对齐远端内容。

## 源码与部署分离

源码仓库只描述怎么构建、怎么运行，不存放任何本机配置与运行数据。Go 编译产物是单文件二进制，运行时不需要源码树，因此部署目录独立于源码目录：

```text
源码仓库                            部署目录（示例）
├── cmd/                            E:\TOOOOOLSbox\eucli-deep-wiki-deploy\
├── internal/                       ├── config\config.json
├── Dockerfile                      └── data\
├── docker-compose.yml                  ├── repositories.json
├── configs/config.json.example         ├── repository\
└── .env.example                        └── wiki\
```

| 文件 | 归属 | 是否入库 |
| --- | --- | --- |
| `Dockerfile`、`docker-compose.yml` | 构建与运行描述 | 是 |
| `configs/config.json.example`、`.env.example` | 样例 | 是 |
| `configs/config.json`、`.env`、覆盖文件 | 本机取值 | 否 |
| `data/`、二进制 | 运行数据 | 否 |

## 配置

配置文件分两处使用，内容只差在路径上：

- 容器内：`dataRoot` 填挂载点 `/app/data`，`httpAddress` 填 `0.0.0.0:9997`。
- 宿主机直接跑二进制：`dataRoot` 填绝对路径，例如 `E:/TOOOOOLSbox/eucli-deep-wiki-deploy/data`。

`dataRoot` 支持绝对路径，也支持单层相对名字（按启动时的工作目录解析）；`repositoryRoot` 与 `repositoryIndex` 始终是单层名字，挂在 `dataRoot` 之下。

| 字段 | 含义 | 默认值 |
| --- | --- | --- |
| `dataRoot` | 运行数据根目录 | `data` |
| `repositoryRoot` | 参考仓库目录（相对 `dataRoot`） | `repository` |
| `repositoryIndex` | 仓库索引文件（相对 `dataRoot`） | `repositories.json` |
| `syncInterval` | 同步周期 | `6h` |
| `gitCommandTimeout` | 单条 git 命令超时 | `20m` |
| `maxParallelSyncs` | 并行同步数量 | `1` |
| `httpAddress` | 状态服务监听地址 | `127.0.0.1:9997` |

## 用容器部署

```sh
cp .env.example .env      # 改成自己的部署目录
docker compose up -d --build
docker compose logs -f
docker compose down
```

编排把宿主机上的配置文件只读挂载为 `/app/config.json`，数据目录读写挂载为 `/app/data`。宿主机差异全部放在 `.env`，compose 文件本身不含任何绝对路径。

需要追加挂载、限制资源或补环境变量时，复制 `docker-compose.override.example.yml` 为 `docker-compose.override.yml` 再改；它与基础文件同目录，`docker compose up -d` 会自动合并。取值类的调整请改 `.env`，不要写进覆盖文件。

## 直接跑二进制

```sh
go run ./cmd/eucli-dw serve --service-config <配置文件路径>          # 长期运行
go run ./cmd/eucli-dw serve --service-config <配置文件路径> --once   # 只同步一轮
```

## Windows 启动脚本

Windows 侧由 `scripts/windows/` 下的两个脚本负责拉起，脚本本身不含任何绝对路径，取值全部来自同目录的 `deploy.env`：

```text
scripts/windows/
├── start.bat                 # 干活的：读配置、判重、启动、重定向日志
├── start-hidden.vbs          # 只是壳：隐藏窗口调用 start.bat
├── deploy.env.example        # 取值样例（入库）
└── deploy.env                # 本机取值（不入库）
```

```sh
cp scripts/windows/deploy.env.example scripts/windows/deploy.env
```

| 取值 | 含义 |
| --- | --- |
| `EUCLI_DEPLOY_DIR` | 部署目录，二进制与数据所在处 |
| `EUCLI_EXE` | 二进制文件名，仅用于进程判重 |
| `EUCLI_CONFIG` | 配置文件，可相对部署目录，也可绝对路径 |
| `EUCLI_LOG_DIR` / `EUCLI_LOG_FILE` | 日志目录与文件名 |

两层分工：`start.bat` 是唯一逻辑出口，直接双击能看到每一步提示；`start-hidden.vbs` 只有一句 `Run`，不重复任何判断。开机自启把快捷方式指向 `start-hidden.vbs`，放进 `shell:startup` 即可。

日志形如：

```text
[eucli] 启动 reference-sync.exe
[eucli] 配置 E:\...\config\config.json
[eucli] 日志 E:\...\logs\sync.log
```

重复执行会被判重拦下，输出 `已在运行，跳过启动`。

## 注册为开机任务或系统服务

不想依赖登录与快捷方式时，用同一个入口注册：

```sh
service.bat install schtasks            # 开机以 SYSTEM 运行（管理员）
service.bat install schtasks --user     # 登录时以当前用户运行（同样需要管理员）
service.bat install nssm                # 注册为 Windows 服务，需已安装 nssm
service.bat uninstall schtasks          # 移除任务，幂等
service.bat uninstall nssm              # 移除服务，幂等
service.bat status                      # 查看注册与运行状态
```

脚本结构：

```text
scripts/windows/
├── service.bat          入口：校验参数、分发路线
├── lib/load-env.bat     公共：读取并校验 deploy.env
├── impl/schtasks.bat    路线一：任务计划程序
├── impl/nssm.bat        路线二：NSSM 包装成服务
└── run-foreground.bat   执行体：前台运行并落日志，供上面两条路线复用
```

两条路线共用同一份取值校验，只在注册命令上分叉。执行体统一是 `run-foreground.bat`，因此进程生命周期与任务或服务的状态一致，日志也不会丢。

`onstart` 与 `onlogon` 触发器都要求管理员权限，`--user` 只改变运行身份。以 SYSTEM 运行时任务不依赖任何用户登录。

`nssm` 路线额外设置了 `AppExit Default Restart`，进程崩溃会被自动拉起；`deploy.env` 里的 `EUCLI_NSSM` 可指定 `nssm.exe` 位置，留空则从 PATH 查找。

## 仓库索引

`repositories.json` 记录参考仓库名单，位于 `dataRoot` 之下。可以直接用 Git 链接追加，工具会从链接末尾推导本地目录名，并在写入前统一校验：

```sh
go run ./cmd/reference-add -config <配置文件路径> https://github.com/xxxx/xxxx.git
```

添加完成后，若长期服务正在运行，需要重启让新索引生效。

## 同步策略

服务每轮都会检查索引中的参考仓库。若本地参考库不存在，会下载；若本地目录不是索引对应的正确参考库，会重建。

若本地参考库来源正确、没有本地改动，且远端没有新变化，服务会跳过整理动作。只有远端有变化，或本地参考库出现改动、临时文件等不干净状态时，才会重新整理并对齐远端。

同步日志和状态结果会区分 `skipped`、`updated`、`cloned`、`recreated` 等动作，便于判断每个参考仓库本轮实际发生了什么。

## 状态查看

```text
http://127.0.0.1:9997/status
```

## 只读查询接口

服务对已同步的参考仓库提供只读 HTTP 接口，供人和 Agent 按需取用。

除 `/healthz` 外，所有接口都要求携带 key：

```sh
curl -H "Authorization: Bearer <key>" http://127.0.0.1:9997/v1/repos
```

key 配置在 `config.json` 的 `apiKeys` 数组里，可以放多个，便于给不同 Agent 分别发放、单独吊销。每个 key 至少 16 个字符，不允许重复或含空白。

| 方法与路径 | 说明 |
| --- | --- |
| `GET /v1/repos` | 仓库清单，含分支与当前提交号 |
| `GET /v1/repos/{name}` | 单个仓库的元数据 |
| `GET /v1/repos/{name}/tree?path=` | 目录树，目录优先，隐藏 `.git` |
| `GET /v1/repos/{name}/file?path=` | 读取文本文件 |
| `GET /v1/repos/{name}/search?q=&limit=` | 按行子串检索 |

```sh
curl http://127.0.0.1:9997/v1/repos
curl "http://127.0.0.1:9997/v1/repos/claw-code/tree?path=docs"
curl "http://127.0.0.1:9997/v1/repos/claw-code/file?path=README.md"
curl "http://127.0.0.1:9997/v1/repos/claw-code/search?q=subagent&limit=5"
```

文件响应带三个头，便于判断内容是否变化：

| 响应头 | 含义 |
| --- | --- |
| `ETag` | 内容的 SHA-256 摘要，请求带 `If-None-Match` 命中时返回 `304` |
| `Last-Modified` | 文件修改时间 |
| `X-Repo-Revision` | 该仓库当前的提交号 |

边界处理都是明确报错，不做静默兜底：

| 情况 | 响应 |
| --- | --- |
| 路径越出仓库 | `400 invalid_path` |
| 仓库不存在或尚未同步 | `404 repo_not_found` |
| 目标不是普通文件 | `400 not_a_file` |
| 文件超过 1 MiB | `413 file_too_large` |
| 文件不是 UTF-8 文本 | `415 not_text` |
| 检索词为空 | `400 query_required` |

所有路径都经 `os.Root` 收敛，符号链接无法把访问带出仓库；目录树与检索都会跳过 `.git`。

## 命令行工具

整个项目的产物只有一个二进制 `eucli-dw`，服务与查询都走它。

```sh
eucli-dw serve [--service-config 路径] [--http-address 地址] [--once]
eucli-dw repo add [--service-config 路径] <git-url>...
eucli-dw repo list [--service-config 路径] [--json]

eucli-dw get [参数] <仓库> <路径>
eucli-dw tree [参数] <仓库> [目录]
eucli-dw search [参数] <仓库> <关键词>

eucli-dw config set server <url>
eucli-dw config set key <key>
eucli-dw config show [--json]
eucli-dw config path
```

**本地命令**（`serve`、`repo`）直接操作部署目录，只需 `--service-config`；**查询命令**只走网络，不读本地仓库。

查询命令的连接信息按以下顺序确定：

```text
--server / --key 参数  →  客户端配置文件  →  报错并提示如何配置
```

客户端配置文件的查找顺序：

```text
--config 参数
EUCLI_DW_CONFIG 环境变量（可以指向目录）
Windows: %USERPROFILE%\.eucli-dw\config.json
Linux:   $XDG_CONFIG_HOME/eucli-dw/config.json → ~/.config/eucli-dw/config.json
```

工具只认这套平台默认值，本机要把配置放到别处（例如统一的工具目录）时，用 `EUCLI_DW_CONFIG` 指过去即可，无需改代码。

```sh
eucli-dw config set server http://127.0.0.1:9997
eucli-dw config set key 0123456789abcdef

eucli-dw get claw-code README.md
eucli-dw tree claw-code docs
eucli-dw search claw-code subagent --limit 5
eucli-dw get claw-code README.md --json     # 带提交号的 JSON
```

`config show` 只显示 key 的首尾各四位；查询命令都支持把参数写在位置参数之后，也支持 `--json` 输出原始结构，便于脚本与 Agent 消费。

## 开发资料目录

开发过程中的任务记录、设计决定与工作区约定统一放在 `.dev-management/`，该目录拥有独立的 Git 记录。
