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
go run ./cmd/reference-sync -config <配置文件路径>   # 长期运行
go run ./cmd/reference-sync -config <配置文件路径> -once   # 只同步一轮
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

## 开发资料目录

开发过程中的任务记录、设计决定与工作区约定统一放在 `.dev-management/`，该目录拥有独立的 Git 记录。
