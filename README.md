# Eucli Deep Wiki

一个用于维护本地参考代码库的轻量服务。它按配置把多个 GitHub 仓库同步到本地资料区，并让这些参考仓库始终保持干净、可更新的状态。

## 项目用途

本项目的目标是把多个外部仓库作为参考资料集中放置，并由服务定期更新。

参考仓库只用于查阅，不作为本地开发副本使用。同步时，服务会清理参考仓库里的本地改动、临时文件和不符合配置的残留目录，然后重新对齐远端内容。

## 源码与部署分离

源码仓库只描述怎么构建、怎么运行，不存放任何本机配置与运行数据。Go 编译产物是单文件二进制，运行时不需要源码树，因此部署目录独立于源码目录：

```text
源码仓库                            部署目录（示例）
├── cmd/                            D:\eucli-deploy\
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
- 宿主机直接跑二进制：`dataRoot` 填绝对路径，例如 `D:/eucli-deploy/data`。

`dataRoot` 支持绝对路径，也支持单层相对名字（按启动时的工作目录解析）；`repositoryRoot` 与 `repositoryIndex` 始终是单层名字，挂在 `dataRoot` 之下。

| 字段 | 含义 | 默认值 | 改完是否需要重启 |
| --- | --- | --- | --- |
| `dataRoot` | 运行数据根目录 | `data` | 是 |
| `repositoryRoot` | 参考仓库目录（相对 `dataRoot`） | `repository` | 是 |
| `repositoryIndex` | 仓库索引文件（相对 `dataRoot`） | `repositories.json` | 是 |
| `httpAddress` | 状态服务监听地址 | `127.0.0.1:9997` | 是 |
| `syncInterval` | 同步周期 | `6h` | 否 |
| `gitCommandTimeout` | 单条 git 命令超时 | `20m` | 否 |
| `maxParallelSyncs` | 并行同步数量 | `1` | 否 |
| `defaultHistory` | 默认历史档位：`shallow` / `full` | `shallow` | 否 |
| `gcThresholdMB` | `.git` 达到多少 MB 才考虑压缩 | `2048` | 否 |
| `gcGrowthMB` | 距上次压缩增长多少 MB 才真的压 | `300` | 否 |
| `sizeLimitMB` | `.git` 超过多少 MB 停止同步 | `5120` | 否 |

标“是”的四个字段在启动时被固化：监听地址由 HTTP 服务持有，数据根目录决定索引文件与同步状态文件的位置，仓库目录与索引文件名决定读写位置。改了它们不会静默忽略——`GET /status` 的 `needsRestart` 会点名，日志里也会留一条记录。其余字段改完即生效，见“运行期热加载”。

历史档位只有两档：`shallow` 只保留最新一个提交（默认，`.git` 很小），`full` 保留完整历史。单个仓库可以在索引里用 `"history"` 字段覆盖全局默认。不提供“无 Git”档——没有 `.git` 就无法增量拉取、也无法精确对齐到某个提交，代价高于收益。

切档不需要重新下载工作树：从 `full` 转 `shallow` 时就地瘦身——收窄 fetch 范围、删掉无关的远端分支与标签、清理不可达对象，工作树保持不变；只有就地转换失败才退回重建。也可以手动触发：

```sh
eucli-dw repo compact            # 全部
eucli-dw repo compact <仓库>      # 单个
```

体积保护：同步完成后检查 `.git` 体积。达到 `gcThresholdMB` 且比上次压缩后增长超过 `gcGrowthMB` 时跑一次 `git gc` 并记录新基准；超过 `sizeLimitMB` 则停止该仓库的自动同步，标记为 `BLOCKED`，人工处理后再 `repo unblock` 恢复。

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
eucli-dw repo add https://github.com/xxxx/xxxx.git
```

写盘成功后命令会顺带通知正在运行的服务重载一次，新仓库立刻出现在 `/v1/repos`，不需要重启。服务没在跑不算失败：命令照样返回 0，并提示「已写入索引，服务将在下一轮同步前自动加载」。只想写索引、不碰服务时加 `--no-reload`。

把某个仓库从索引里删掉同样即生效：`/v1/repos`、`/tree`、`/file`、`/find`、`/search`、`/archive`、`/bundle` 对它一律返回 `404`，但 `dataRoot/repository/<仓库>` 下的数据**不会**被删除——索引只决定“要不要对外提供”，不负责回收磁盘。

## 运行期热加载

服务不再只在启动时读一次配置文件与索引文件。以下时机都会比对两个文件的指纹（修改时间 + 大小），指纹变了才重新加载，没变时只花两次 `stat`：

- 每轮定时同步开始前；
- 每次按需同步（`POST /v1/repos/{name}/sync`）与远端探测前；
- 每个受保护的 HTTP 请求进入前（`/status` 与全部 `/v1/...`）。

重载后立即生效的有：鉴权用的 `apiKeys`（新增的 key 下一个请求就能用，删掉的立刻 401）、`syncInterval`（等待中的计时按新周期重新排）、`gitCommandTimeout`、`maxParallelSyncs`、`defaultHistory`、GC 与体积阈值、以及整份仓库清单（同步清单与只读视图一起换，不会出现“同步了但查不到”）。

三条保证：

- **原子替换**：新状态整份构建完成后才切换，并发请求与正在跑的同步拿到的是完整的一份，不会读到半新半旧；正在进行的同步仍用它开始时那份配置跑完。
- **失败保留旧值**：JSON 非法或校验不过时，旧配置与旧视图继续工作，服务不停摆，失败原因写进最近一次重载结果。
- **不重复踩坑**：同一份坏文件不会被每轮重复解析，改好（指纹变化）之后自动重试。

也可以显式触发一次，两个文件都会被重新解析：

```sh
eucli-dw reload                # 文本结果
eucli-dw reload --json         # 原始结果
curl -X POST -H "Authorization: Bearer <key>" http://127.0.0.1:9997/v1/reload
```

返回 `{configReloaded, indexReloaded, repositories, needsRestart[]}`（另带 `at`，失败时带 `error`）。两个布尔值的含义是「生效中的这份是否就是磁盘上的这份」：文件没变但上次加载成功时同样为 `true`；为 `false` 时 `error` 一定非空，`/status` 里也能看到。重载失败时接口仍返回 `200`（请求本身被正确处理），`eucli-dw reload` 此时退出码为 1。

需要重启才能生效的字段（`httpAddress`、`dataRoot`、`repositoryRoot`、`repositoryIndex`）一旦在配置文件里被改动，`/status` 的 `needsRestart` 会列出字段名，日志里也会留一条 `restart required for ...`，改回去提示随之消失。

## 同步策略

服务每轮都会检查索引中的参考仓库。若本地参考库不存在，会下载；若本地目录不是索引对应的正确参考库，会重建。

若本地参考库来源正确、没有本地改动，且远端没有新变化，服务会跳过整理动作。只有远端有变化，或本地参考库出现改动、临时文件等不干净状态时，才会重新整理并对齐远端。

同步日志和状态结果会区分 `skipped`、`updated`、`cloned`、`recreated` 等动作，便于判断每个参考仓库本轮实际发生了什么。

## 状态查看

```text
http://127.0.0.1:9997/status
```

`/status` 返回当前或上一轮的同步快照：`running` 表示是否有同步在跑，`results` 是已记录的结果，`summary` 按动作统计条数。运行中 `summary` 是已处理部分的汇总，结束后是全量汇总。

同一个响应里还带着运行期状态，用来确认“改的东西到底生效了没有”：

| 字段 | 含义 |
| --- | --- |
| `configLoadedAt` | 当前生效配置的加载时间 |
| `indexLoadedAt` | 当前生效仓库清单的加载时间 |
| `repositoryCount` | 当前生效的仓库数 |
| `syncInterval` / `gcThresholdMB` / `gcGrowthMB` / `sizeLimitMB` / `defaultHistory` | 当前生效的取值（改完配置后可直接对照） |
| `lastReload` | 最近一次重载结果：`at`、`configReloaded`、`indexReloaded`、`repositories`、`needsRestart`，失败时还有 `error` |
| `needsRestart` | 改了但必须重启才能生效的字段名列表，空数组表示没有 |

原有字段语义不变，新字段只加不改。

每个仓库的远端状态与体积保护记录在 `/v1/state`：

| 字段 | 含义 |
| --- | --- |
| `remoteMissing` | 远端已被判定不可用，后续同步直接跳过该仓库 |
| `remoteMissingAt` | 判定远端不可用的时间 |
| `consecutiveFailures` | 连续同步失败次数，成功一次即清零 |
| `lastError` | 最近一次同步失败的错误信息，成功后清空 |
| `blocked` / `blockedSize` | 体积保护：`.git` 超过 `sizeLimitMB` 后停止同步 |

远端不可用类失败会在连续失败计数达到 3 次时判定远端消失；其它失败只累加 `consecutiveFailures` 并记录 `lastError`，不会触发该标记。确认远端恢复后执行 `repo unblock`，会同时清掉 `remoteMissing`、失败计数与 `lastError`，下一轮同步重新尝试。

`GET /v1/state` 只列出“有状态”的仓库：返回空对象表示当前没有任何仓库处于失败、停止同步或体积保护状态；`consecutiveFailures` 从第一次失败起就会记录，因此即使还没到 3 次判定门槛，也能从这里看到哪个仓库在连续失败。

## 只读查询接口

服务对已同步的参考仓库提供只读 HTTP 接口，供人和 Agent 按需取用。

除 `/healthz` 外，所有接口都要求携带 key：

```sh
curl -H "Authorization: Bearer <key>" http://127.0.0.1:9997/v1/repos
```

key 配置在 `config.json` 的 `apiKeys` 数组里，可以放多个，便于给不同 Agent 分别发放、单独吊销。每个 key 至少 16 个字符，不允许重复或含空白。

| 方法与路径 | 说明 |
| --- | --- |
| `GET /v1/repos` | 仓库清单：分支、提交、浅克隆、上次同步时间、体积、保护状态 |
| `GET /v1/repos/{name}` | 单个仓库的元数据 |
| `GET /v1/repos/{name}/tree?path=&depth=&dirsOnly=` | 目录树，目录优先，隐藏 `.git`；`depth` 一次展开多层 |
| `GET /v1/repos/{name}/file?path=&lines=&maxBytes=` | 读取文本文件，可按行切段、按字节限长 |
| `GET /v1/repos/{name}/search?q=&path=&offset=&limit=&context=` | 按行子串检索：限定目录、翻页、带上下文行 |
| `GET /v1/repos/{name}/find?name=&path=&match=&limit=&offset=` | 按文件名查找：默认精确，`match=substring` 改为包含 |
| `GET /v1/repos/{name}/archive?path=&path=` | 快照打包，`path` 可重复，留空为整仓 |
| `GET /v1/archive?include=仓库:路径&include=…` | 跨仓库或多目标打包 |
| `GET /v1/repos/{name}/bundle` | 带完整历史的 `git bundle` |
| `POST /v1/repos/{name}/sync` | 立刻同步指定仓库，同步返回结果 |
| `POST /v1/sync` | 触发一轮全量同步，立即返回 `202` |
| `POST /v1/repos/{name}/check` | 探测远端是否可达，不改动本地仓库 |
| `POST /v1/reload` | 立即重载配置与索引，返回本次重载结果 |
| `GET /v1/state` | 每个仓库的远端状态与体积保护记录 |

按需同步用于「刚改完文档，不想等 6 小时」。这里的“只读”是针对仓库内容的：客户端永远不能写入、修改或删除仓库里的文件；触发同步是服务器自身的运维动作，只是把远端内容拉到本地，不改变任何远端仓库。已有同步在跑时返回 `409 sync_busy`，不排队。

```sh
eucli-dw sync <仓库>      # 立刻同步这一个
eucli-dw sync --all       # 立刻跑一轮全量
```

`find` 只按文件名匹配，不读文件内容。`name` 含 `*`、`?`、`[` 时按通配处理；否则默认要求文件名与 `name` **完全一致**（不区分大小写），要换成「包含即可」用 `match=substring`。`path` 是检索范围——遍历从该目录开始，不是在结果里过滤；`offset` 跳过前面的命中，`limit` 默认 200、单次上限 10000，超过上限按 10000 处理。结果里的 `entries` 每项含 `name`、`path`、`type`、`size`；`scannedFiles` 是遍历过的文件数，`truncated` 表示还有未返回的命中。

精确匹配落空时，`eucli-dw find` 会按「包含」口径再查一次：确实有包含匹配就在 stderr 报出数量与示例路径，提示加 `--substring`。通配符模式、正在翻页、或已经传了 `--substring` 时不做这次附加查询；附加查询失败也不影响原本的空结果。

```sh
curl "http://127.0.0.1:9997/v1/repos/claw-code/find?name=*.md&path=docs&limit=20"
```

`search` 结果里的 `scannedBytes` 是实际读取的字节数（跳过的二进制文件也计入），`scannedFiles` 是实际参与匹配的文本文件数。`context` 让每处命中带上前后各若干行（单处上限 10），命中项的 `before` / `after` 给出这些行的行号与原文。

超过单文件上限的文件不参与检索，它们会被列在 `oversizedFiles` 里，`oversizedFilesCount` 是总数（明细最多 20 条）。这样「大文件没搜」不会被误读成「大文件里没命中」。

`check` 直接对远端执行一次 `git ls-remote`，只判断远端是否还在，不读取、不改动本地仓库，也不改动同步状态。返回 `{"repo":"…","reachable":true}`；不可达时 `reachable` 为 `false`，并带上 `error`。仓库不在索引中时返回 `404 repo_not_found`。

```sh
curl -X POST -H "Authorization: Bearer <key>" http://127.0.0.1:9997/v1/repos/claw-code/check
```

打包默认输出 zip，加 `format=tar.gz` 可换格式；归档不包含 `.git`，总量上限 512 MiB、条目上限两万。bundle 按提交号缓存在 `dataRoot/cache` 下，同一提交只生成一次。

```sh
eucli-dw pull <仓库>[:路径] ... -o bundle.zip     # 快照
eucli-dw bundle <仓库> -o repo.bundle            # 带历史
```

```sh
curl http://127.0.0.1:9997/v1/repos
curl "http://127.0.0.1:9997/v1/repos/claw-code/tree?path=docs"
curl "http://127.0.0.1:9997/v1/repos/claw-code/file?path=README.md"
curl "http://127.0.0.1:9997/v1/repos/claw-code/search?q=subagent&limit=5"
```

文件响应的头分两类：一类判断内容有没有变化，一类说明「这次给的是哪一段」。

| 响应头 | 含义 |
| --- | --- |
| `ETag` | 整份内容的 SHA-256 摘要，请求带 `If-None-Match` 命中时返回 `304` |
| `Last-Modified` | 文件修改时间 |
| `X-Repo-Revision` | 该仓库当前的提交号 |
| `X-File-Total-Lines` | 文件总行数 |
| `X-File-First-Line` / `X-File-Last-Line` | 本次正文覆盖的行范围（1 基，含）；空正文时 `FirstLine > LastLine` |
| `X-File-Size` / `X-File-Bytes` | 文件总字节数 / 本次返回的字节数 |
| `X-File-Truncated` | 正文不完整的原因，目前只有 `bytes`（被 `maxBytes` 截断） |

`lines` 与 `maxBytes` 的写法：

| 写法 | 含义 |
| --- | --- |
| `lines=362-520` | 第 362 到 520 行 |
| `lines=362-` | 第 362 行到末尾 |
| `lines=-520` | 首行到第 520 行 |
| `lines=362` | 仅第 362 行 |
| `maxBytes=4096` | 最多 4096 字节，超出时退到最后一个完整行，绝不返回半行残句 |

`ETag` 描述的是整份文件，因此「同一文件的不同行区间」拿到的是同一个 `ETag`：它回答的是「文件变没变」，不是「这一段变没变」。

读取与检索对大文件的容忍度不同，这是两个独立的上限：单次读取允许到 16 MiB（配合 `lines` 能从几 MiB 的打包产物里取一段），内容检索的单文件上限是 1 MiB——检索要扫全仓，不能为一个大文件拖住整轮；被跳过的文件会列在 `oversizedFiles` 里。

边界处理都是明确报错，不做静默兜底：

| 情况 | 响应 |
| --- | --- |
| 路径越出仓库 | `400 invalid_path` |
| 仓库不存在或尚未同步 | `404 repo_not_found` |
| 路径不存在 | `404 path_not_found` |
| 目标不是普通文件 | `400 not_a_file` |
| 文件超过 16 MiB（单次读取上限，可用 `lines` 只取一段） | `413 file_too_large` |
| 文件不是 UTF-8 文本 | `415 not_text` |
| 检索词为空 | `400 query_required` |
| 文件名为空或通配语法不合法 | `400 name_required` |
| 行区间写法不合法 | `400 invalid_lines` |
| 布尔参数不是合法布尔值 | `400 invalid_flag` |
| `maxBytes` 不是非负整数 | `400 invalid_bytes` |
| `match` 不是 `exact` / `substring` | `400 invalid_match` |

所有路径都经 `os.Root` 收敛，符号链接无法把访问带出仓库；目录树、检索与查找都会跳过 `.git`。

## 命令行工具

整个项目的产物只有一个二进制 `eucli-dw`，服务与查询都走它。

```sh
eucli-dw serve [--service-config 路径] [--http-address 地址] [--once]
eucli-dw repo add [--service-config 路径] [--no-reload] <git-url>...
eucli-dw repo list [--service-config 路径] [--json]
eucli-dw repo check [--service-config 路径] [仓库]...
eucli-dw repo unblock [--service-config 路径] <仓库>

eucli-dw repos [--sort name|time|size] [--json]
eucli-dw overview [参数] <仓库>            目录骨架 + README 开头 + 清单文件
eucli-dw get [参数] <仓库> <路径>...       可多路径；--lines 切段，--max-bytes 限长
eucli-dw tree [参数] <仓库> [目录]         --depth 一次展开多层，--dirs-only 只看骨架
eucli-dw search [参数] <仓库> <关键词>     --path 限目录，--context 上下文行，--offset 翻页
eucli-dw find [参数] <仓库> <文件名>       默认精确；--substring 改为包含；支持 * 通配
eucli-dw status [--json]            查看同步状态
eucli-dw reload [--json]            让服务立刻重载配置与索引
eucli-dw sync [--json] <仓库> | sync --all [--json]
eucli-dw pull <仓库>[:路径] ... [-o 文件] [--progress] [--json]
eucli-dw bundle <仓库> [-o 文件] [--progress] [--json]

eucli-dw config set server <url>
eucli-dw config set key <key>
eucli-dw config show [--json]
eucli-dw config path
eucli-dw cache list | cache clean [--yes]
```

查询命令通用参数另有 `--timeout 秒数`，用来给大文件下载设置上限。

`repo check` 逐个探测索引里的仓库（也可以只传仓库名）：远端可达输出 `可达`，不可达输出 `不可达` 与错误信息，索引里不存在的名字输出 `未知仓库`，最后给出汇总行。全部可达返回退出码 0，只要出现不可达或未知就返回 1；`--json` 输出 `{checked, reachable, unreachable, unknown, results[]}`。探测并发进行，走本机服务，需要服务正在运行。

`repos` 一行一个仓库，列依次是：仓库名、分支、提交号前 12 位、上次同步时间、`.git` 体积，随后是状态标记（`BLOCKED(大小)` / `MISSING` / `SHALLOW`）。这些字段由服务端一次返回，不必再单独查状态接口。`--sort` 可以按名字（默认）、最近同步时间或体积排序，同值一律按名字升序，输出可复现。

`repo list` 是本地命令：直接读索引与状态文件，输出仓库名、分支、地址，`--sizes` 时附带 `.git` 体积，并追加 `MISSING` / `BLOCKED` 标记。它不需要服务在跑，服务没启动时也能用。

`overview` 是「先看仓库长什么样」的入口：一次请求拿到元数据与目录骨架，随后并发取回 README 开头与清单文件（`go.mod`、`package.json`、`Cargo.toml` 等），把原先需要 `tree` + 多次 `get` 的往返压成一条命令。`--depth`、`--readme-lines`、`--manifests`、`--manifest-lines` 控制展开规模。

`find --limit` 超过上限时按 10000 处理，文本模式的截断提示会写明该上限。

**查询命令的输出约定**：stdout 只放数据（条目、正文、清单行），关于这批数据的说明——截断、还有更多、跳过了哪些大文件、参数按上限收敛——一律写 stderr。因此 `eucli-dw tree repo --depth 2 | wc -l` 这类管道不会被提示行干扰。`--json` 把所有事实放进结构化字段，不依赖 stderr。

`sync <仓库> --json` 输出该仓库的 `name`、`action`、`durationMs`、`error`；`sync --all --json` 输出 `{"status":"started"}`。同步失败时退出码非零，错误同时以 `{"ok":false,...}` 写入 stderr。文本模式的输出保持不变。

**退出码**（便于脚本判断该重试还是放弃）：

| 码 | 含义 |
| --- | --- |
| 0 | 成功 |
| 1 | 其它失败 |
| 2 | 参数或用法错误 |
| 3 | 仓库或路径不存在 |
| 4 | 鉴权失败 |
| 5 | 网络或超时 |
| 6 | 已有同步任务在运行 |
| 7 | 被拒绝：文件过大、非文本 |

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
eucli-dw get claw-code README.md --json     # 带提交号与行范围的 JSON

# 一次看两三层的骨架，不必逐层剥
eucli-dw tree claw-code --depth 2 --dirs-only

# 只看某个函数附近，不需要整份 cli.js
eucli-dw get claw-code cli.js --lines 362-520

# 多个路径一次取回，按位置参数顺序输出
eucli-dw get claw-code README.md src/index.ts --max-bytes 4096

# 检索时带上下文，并把超大文件被跳过的事实说出来
eucli-dw search claw-code subagent --context 2 --limit 20

# 先看仓库长什么样
eucli-dw overview claw-code
```

`repos`、`status` 与 `reload` 是客户端命令，只走网络；`status` 至少给出运行状态、上轮结果统计、下一轮预计时间、仓库数、同步周期、配置与索引的加载时间、最近一次重载结果，以及需要重启才能生效的字段；`reload` 打印服务端的重载结果，失败时退出码为 1。

`config show` 只显示 key 的首尾各四位；查询命令都支持把参数写在位置参数之后，也支持 `--json` 输出原始结构，便于脚本与 Agent 消费。

任何一层都有帮助：`eucli-dw help`、`eucli-dw repo --help`、`eucli-dw get -h`，退出码统一为 0；未知命令返回 2 并打印用法。

## 开发资料目录

开发过程中的任务记录、设计决定与工作区约定统一放在 `.dev-management/`，该目录拥有独立的 Git 记录。
