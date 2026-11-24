# eucli-deep-wiki 适配器工具

实现 `eucli-code-factory` 契约 3（`.dev-tools/adapter.toml`）所需的 7 条命令，零第三方依赖：

- `deps`：准备共享区 Go 依赖缓存与分支依赖目录，并在各子模块执行 `go mod download`。
- `build`：以 `GOWORK=off CGO_ENABLED=0` 分别正式构建 `eucli-deep-wiki-server` 与 `eucli-dw`，写入 `EUCLI_CF_BUILD_DIR`。
- `start`：校验注入的实例身份后缀与端口，在 `EUCLI_CF_DATA_ROOT` 生成隔离配置并在后台拉起服务端，日志落盘到 `EUCLI_CF_DATA_ROOT/server.log`。
- `health`：探测 `/healthz` 并确认 `service-profile.json` 已就绪。
- `stop`：幂等结束当前实例进程并等待端口释放。
- `seed`：本服务无需外部基线数据，按契约返回 `not-applicable`。
- `test`：先对运行中的正式实例执行 HTTP 与 `eucli-dw` 命令行联调校验，再依次运行 `server`、`cli`、`guard`、`tools/pack`、`tools/version` 的全部自动化测试。

## 调用方式

由 `eucli-cfd` 通过 `.dev-tools/adapter.toml` 调用，所有输入均来自 `EUCLI_CF_*` 环境变量；标准输出仅输出单个契约 JSON，日志全部写往标准错误。
