# Eucli Deep Wiki

一个用于维护本地参考代码库的轻量服务。它按配置把多个 GitHub 仓库同步到本地资料区，并让这些参考仓库始终保持干净、可更新的状态。

## 项目用途

本项目的目标是把多个外部仓库作为参考资料集中放置，并由服务定期更新。

参考仓库只用于查阅，不作为本地开发副本使用。同步时，服务会清理参考仓库里的本地改动、临时文件和不符合配置的残留目录，然后重新对齐远端内容。

## 目录约定

仓库根目录只存放源码与配置样例，运行数据全部收敛到 `data/` 下：

```text
configs/
  config.json            # 本机服务配置，不入库
  config.json.example    # 配置样例，随源码提交
data/                    # 运行数据，不入库
  repositories.json      # 参考仓库索引
  repository/            # 参考仓库落盘位置
  wiki/                  # 由参考仓库整理出的笔记
```

服务配置与仓库索引分开维护：前者是机器相关的设置，后者是可重建的数据清单。`data/` 与 `configs/config.json` 都不参与版本管理。

## 快速开始

```sh
cp configs/config.json.example configs/config.json

# 执行一次同步后退出
go run ./cmd/reference-sync -once

# 作为长期服务运行
go run ./cmd/reference-sync
```

## 配置

`configs/config.json` 中的路径字段都是单层名字，统一解析到 `dataRoot` 之下：

| 字段 | 含义 | 默认值 |
| --- | --- | --- |
| `dataRoot` | 运行数据根目录 | `data` |
| `repositoryRoot` | 参考仓库目录（相对 `dataRoot`） | `repository` |
| `repositoryIndex` | 仓库索引文件（相对 `dataRoot`） | `repositories.json` |
| `syncInterval` | 同步周期 | `6h` |
| `gitCommandTimeout` | 单条 git 命令超时 | `20m` |
| `maxParallelSyncs` | 并行同步数量 | `1` |
| `httpAddress` | 状态服务监听地址 | `127.0.0.1:9997` |

## 仓库索引

`data/repositories.json` 记录参考仓库名单。可以直接用 Git 链接追加，工具会从链接末尾推导本地目录名，并在写入前统一校验：

```sh
go run ./cmd/reference-add https://github.com/xxxx/xxxx.git
```

也可以一次传入多个链接。添加完成后，如果长期服务已经在运行，需要重启服务让新索引生效。

## 同步策略

服务每轮都会检查索引中的参考仓库。若本地参考库不存在，会下载；若本地目录不是索引对应的正确参考库，会重建。

若本地参考库来源正确、没有本地改动，且远端没有新变化，服务会跳过整理动作。只有远端有变化，或本地参考库出现改动、临时文件等不干净状态时，才会重新整理并对齐远端。

同步日志和状态结果会区分 `skipped`、`updated`、`cloned`、`recreated` 等动作，便于判断每个参考仓库本轮实际发生了什么。

## 状态查看

服务长期运行时，可以通过下面的地址查看当前状态：

```text
http://127.0.0.1:9997/status
```

## 容器运行

```sh
docker compose up --build
docker compose down
```

编排会只读挂载 `configs/`、读写挂载 `data/`，容器内不保存任何配置与数据。

## 开发资料目录

开发过程中的任务记录、设计决定、功能笔记和工作区约定统一放在 `.dev-management/`。该目录拥有独立的 Git 记录，不会被主项目记录。
