# 更新记录

## 2026-11-18

- CLI 取数能力补齐：`tree --depth N` / `--dirs-only`、`get --lines 362-520` / `--max-bytes` / 一次多个路径、`search --context N` 与超大文件清单、新增 `overview` 一条命令给出仓库概览。
- **行为变更**：`find` 默认改为精确匹配文件名；要「包含即可」需显式 `--substring`（含 `*`、`?`、`[` 时仍按通配处理）。HTTP 侧对应 `match=substring`。精确匹配落空时 CLI 会按「包含」口径再查一次并提示数量与示例（通配符 / 翻页 / 已传 `--substring` 时不查，附加查询失败不影响空结果）。
- `repos` 首屏带上次同步时间、`.git` 体积与 `BLOCKED` / `MISSING` / `SHALLOW` 标记，并支持 `--sort name|time|size`；服务端 `/v1/repos` 与 `/v1/repos/{name}` 直接合并同步事实，客户端不再单独查 `/v1/state`。
- 同步成功后记录 `lastSyncAt` 与当时测得的 `.git` 体积（`sync-state.json` 新增两个字段，旧文件可直接读取）。
- 输出约定统一：stdout 只放数据，截断 / 还有更多 / 跳过 / 参数按上限收敛等说明一律写 stderr。
- 文件接口新增行切段与字节限长，响应头补 `X-File-Total-Lines`、`X-File-First-Line`、`X-File-Last-Line`、`X-File-Size`、`X-File-Bytes`、`X-File-Truncated`。

## 2026-11-17

- 源码与部署分离：配置、运行数据与二进制移出源码仓库，部署目录位于源码仓库之外（如 `D:\eucli-deploy`）。
- `dataRoot` 支持绝对路径，宿主机与容器各自使用同一份配置结构。
- 编排改为全量环境变量插值，宿主机取值放在 `.env`；配置以文件形式只读挂载。
- 镜像不再内置配置，缺失挂载时直接启动失败。

## 2026-11-16

- 拆分服务配置与仓库索引：`configs/config.json` 只保留服务自身设置，仓库名单迁至 `data/repositories.json`。
- 运行数据统一收敛到 `data/`（参考仓库、笔记、索引），与源码分离。
- `configs/` 与 `data/` 移出版本管理，仅保留 `configs/config.json.example`。
- 容器编排改为只读挂载配置、读写挂载数据目录。

## 2026-06-22

- 新增 `markitdown` 参考仓库配置，对应 `https://github.com/microsoft/markitdown.git`。

## 2026-06-09

- 新增 `snow-shot` 参考仓库配置。
- 新增参考仓库添加工具，可直接通过一个或多个 Git 链接写入参考仓库配置。
- 优化参考仓库同步策略：本地参考库正确、干净且远端无变化时跳过整理；远端有变化或本地不干净时才执行同步整理。
- 补充同步策略测试，覆盖已最新跳过、远端变化更新、本地改动后重新整理三类关键行为。
