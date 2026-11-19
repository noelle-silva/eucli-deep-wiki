# Eucli Deep Wiki

一套维护「本地参考代码库」的服务：服务端按名单把多个 Git 仓库同步到资料区，并让它们始终干净、可更新；命令行工具在任意一台机器上通过网络读取这些内容。

## 两个程序

交付物是**两个互不相干的程序**，不是一个程序扮演两种角色：

| 程序 | 可执行文件 | 职责 | 需要什么 | 装在哪 |
| --- | --- | --- | --- | --- |
| 服务端 | `eucli-deep-wiki-server` | 持有参考仓库目录、跑 Git、维护仓库名单与同步状态、对外提供内容与管理接口 | 自己那份服务端配置（数据根、监听地址、`apiKeys` 等） | 跑同步的那台机器：部署目录或容器 |
| 命令行 | `eucli-dw` | 把请求发出去、把结果格式化输出、把下载产物写到本地 | 自己那份连接配置（服务端地址 + 一把 key） | 任意一台能连到服务端的机器，与服务端不同机也照常可用 |

两侧的边界是硬约束，不是约定：

1. **零共享代码**。两边源码各自是一个独立模块（`server/`、`cli/`），互不引用；构建或测试任何一侧都不需要另一侧的源码存在。两侧共用的只有对外契约（接口地址、参数名、字段名、错误标识、状态码），没有共用代码。
2. **命令行只认地址与 key**。它读自己的连接配置；不读服务端配置、不猜服务端部署目录、不把服务端监听地址改写成回环地址、不取别人配置里的钥匙。
3. **命令行不碰服务端机器上的任何文件**。加仓库、摘名单、解封、就地瘦身、列缓存、清缓存这些动作都由服务端执行，命令行只发请求并显示结果；它在本地只写自己产出的下载文件与自己的配置文件。

两条使用边界也顺带说清：

- 服务端是**长期运行的进程**，一次启动一直跑，按周期同步；它的生命周期由部署方式（容器、任务计划程序、Windows 服务）决定。
- 命令行是**每次查阅临时执行的程序**，跑完就退出；它不缓存任何仓库内容，也不保留服务端状态。

本文档第「服务端」章讲服务端怎么构建、怎么配、怎么部署、暴露什么接口；第「命令行」章讲命令行怎么装、怎么配、有哪些命令。两章各自独立，互不要求对方的前提。

## 项目用途

本项目的目标是把多个外部仓库作为参考资料集中放置，并由服务定期更新。

参考仓库只用于查阅，不作为本地开发副本使用。同步时，服务会清理参考仓库里的本地改动、临时文件和不符合配置的残留目录，然后重新对齐远端内容。

## 代码布局与构建

源码仓库只描述怎么构建、怎么运行，不存放任何本机配置与运行数据。

```text
源码仓库
├── go.work                             # 把两个模块组合起来，本地可同时构建与测试
├── server/                             # 服务端模块（module eucli-deep-wiki-server）
│   ├── cmd/eucli-deep-wiki-server/     # 服务端入口
│   └── internal/                       # 配置、接口层、热加载、同步调度、Git 操作、只读访问…
├── cli/                                # 命令行模块（module eucli-dw）
│   ├── cmd/eucli-dw/                   # 命令行入口
│   └── internal/                       # 命令分发、连接调用、连接配置、参数上限
├── Dockerfile                          # 构建服务端镜像
├── docker-compose.yml                  # 服务端容器编排
├── configs/config.json.example         # 服务端配置样例
├── .env.example                        # 容器部署取值样例
└── scripts/windows/                    # 服务端在 Windows 上的启动与服务注册脚本族
```

两个程序分别构建，各出各的可执行文件：

```sh
cd server && go build -o eucli-deep-wiki-server ./cmd/eucli-deep-wiki-server
cd cli    && go build -o eucli-dw ./cmd/eucli-dw
```

两个都是单文件二进制，运行时不需要源码树。产物去向不同：

```text
跑服务端的机器（部署目录，示例）
D:\eucli-deploy\
├── eucli-deep-wiki-server.exe
├── config\config.json
├── data\
│   ├── repositories.json
│   ├── repository\
│   └── cache\
└── logs\

需要查阅的机器（位置随意）
C:\tools\eucli-dw\eucli-dw.exe          # 放进 PATH 即可
%USERPROFILE%\.eucli-dw\config.json     # 它自己的连接配置
<当前目录>\<用 -o 指定的文件名>          # 它自己的下载产物
```

命令行不需要部署目录，也不需要知道部署目录在哪台机器上：它只有地址与 key 两项输入。

| 文件 | 归属 | 是否入库 |
| --- | --- | --- |
| `server/`、`cli/`、`go.work` | 源码 | 是 |
| `Dockerfile`、`docker-compose.yml` | 构建与运行描述 | 是 |
| `configs/config.json.example`、`.env.example`、`scripts/windows/deploy.env.example` | 样例 | 是 |
| `configs/config.json`、`.env`、`docker-compose.override.yml`、`scripts/windows/deploy.env` | 本机取值 | 否 |
| 部署目录、两个可执行文件、`data/` | 运行数据与产物 | 否 |

## 服务端

服务端是长期运行的进程：读自己的配置、定时同步名单里的仓库、对外提供接口。它没有子命令，也不分发任何命令行职责。

### 启动

```sh
cd server && go build -o eucli-deep-wiki-server ./cmd/eucli-deep-wiki-server   # 产物，单文件

./eucli-deep-wiki-server -config <配置文件路径>
./eucli-deep-wiki-server -config <配置文件路径> -http-address 127.0.0.1:9997
./eucli-deep-wiki-server -config <配置文件路径> -once      # 只同步一轮后退出，不启动接口
```

| 参数 | 含义 | 缺省 |
| --- | --- | --- |
| `-config` | 服务端配置文件路径 | `configs/config.json`（相对启动时的工作目录） |
| `-http-address` | 覆盖配置里的 `httpAddress`，校验规则与配置文件相同 | 空，即用配置里的值 |
| `-once` | 只同步一轮后退出，不启动接口 | 关 |

服务端不接受位置参数：启动方式只有「程序 + 自己的参数」，没有第二种入口。容器之外也一样，直接带参数启动即可，容器只负责把程序与配置放进一个目录。

### 配置

服务端配置只有一个文件、一套字段，只影响服务端自己。命令行不读它，也不需要它的任何字段；反过来，服务端也不读命令行的连接配置。

字段全集如下（配置文件是服务端唯一的外部输入：它不读任何环境变量，也不靠约定去猜路径）：

| 字段 | 含义 | 默认值 | 改完是否需要重启 |
| --- | --- | --- | --- |
| `dataRoot` | 运行数据根目录，支持绝对路径或单层相对名字（相对启动时的工作目录） | `data` | 是 |
| `repositoryRoot` | 参考仓库目录（相对 `dataRoot` 的单层名字） | `repository` | 是 |
| `repositoryIndex` | 仓库名单文件（相对 `dataRoot` 的单层名字） | `repositories.json` | 是 |
| `httpAddress` | 接口监听地址 | `127.0.0.1:9997` | 是 |
| `apiKeys` | 接口 key 列表，`Authorization: Bearer <key>` 用 | 无，必须自己填 | 否 |
| `syncInterval` | 定时同步周期 | `6h` | 否 |
| `gitCommandTimeout` | 单条 Git 命令超时 | `20m` | 否 |
| `maxParallelSyncs` | 并行同步数量 | `1` | 否 |
| `defaultHistory` | 默认历史档位：`shallow` / `full` | `shallow` | 否 |
| `gcThresholdMB` | `.git` 达到多少 MB 才考虑压缩 | `2048` | 否 |
| `gcGrowthMB` | 距上次压缩增长多少 MB 才真的压 | `300` | 否 |
| `sizeLimitMB` | `.git` 超过多少 MB 停止同步 | `5120` | 否 |

标「是」的四个字段在启动时被固化：监听地址由 HTTP 服务持有，数据根决定名单文件与同步状态文件的位置，仓库目录与名单文件名决定读写位置。改了它们不会静默忽略——`GET /status` 的 `needsRestart` 会点名，日志里也会留一条 `restart required for ...`。其余字段改完即生效，见「运行期热加载」。

`apiKeys` 是唯一没有默认值的字段：至少一项，每项至少 16 个字符，不许重复、不许含空白字符。校验不过服务端直接启动失败——宁可起不来，也不要裸奔到网上。

### 历史档位与体积保护

历史档位只有两档：`shallow` 只保留最新一个提交（默认，`.git` 很小），`full` 保留完整历史。单个仓库可以在名单里用 `"history"` 字段覆盖全局默认。不提供「无 Git」档——没有 `.git` 就无法增量拉取、也无法精确对齐到某个提交，代价高于收益。

切档不需要重新下载工作树：从 `full` 转 `shallow` 时就地瘦身——收窄 fetch 范围、删掉无关的远端分支与标签、清理不可达对象，工作树保持不变；只有就地转换失败才退回重建。这条动作由服务端执行，从命令行触发：

```sh
eucli-dw repo compact --dry-run          # 预演：只看会做什么
eucli-dw repo compact                    # 全部仓库执行
eucli-dw repo compact claw-code          # 单个仓库执行
```

体积保护：同步完成后检查 `.git` 体积。达到 `gcThresholdMB` 且比上次压缩后增长超过 `gcGrowthMB` 时跑一次 `git gc` 并记录新基准；超过 `sizeLimitMB` 则停止该仓库的自动同步，标记为 `BLOCKED`，人工处理后再 `repo unblock` 恢复。

### 容器部署

镜像**只承载服务端**：

| 项 | 值 |
| --- | --- |
| 镜像名 | `eucli-deep-wiki-server:latest` |
| 编排里的服务键 | `eucli-deep-wiki-server` |
| 容器内程序 | `/usr/local/bin/eucli-deep-wiki-server` |
| 容器启动参数 | `-config /app/config.json` |
| 运行层依赖 | 证书包、Git、加密传输客户端、时区数据 |

命令行产物不在镜像里：它是另一个程序，需要单独构建、单独分发到要查阅的机器上。

```sh
cp .env.example .env      # 改成自己的部署目录
docker compose up -d --build
docker compose logs -f
docker compose down
```

编排把宿主机上的服务端配置文件只读挂载为 `/app/config.json`，数据目录读写挂载为 `/app/data`。宿主机差异全部放在 `.env`，compose 文件本身不含任何绝对路径：

| 变量 | 含义 |
| --- | --- |
| `EUCLI_CONFIG_FILE` | 宿主机上的服务端配置文件，只读挂载为 `/app/config.json` |
| `EUCLI_DATA_DIR` | 宿主机上的运行数据目录，读写挂载为 `/app/data` |
| `EUCLI_BIND_ADDRESS` | 宿主机侧监听地址，映射到容器内 9997 |

`.env` 只服务 compose；Windows 脚本族用另一份 `scripts/windows/deploy.env`，两者互不影响。健康检查走 `/healthz`，每 30 秒一次；`restart: unless-stopped` 让容器退出后自动拉起。

需要追加挂载、限制资源或补环境变量时，复制 `docker-compose.override.example.yml` 为 `docker-compose.override.yml` 再改；它与基础文件同目录，`docker compose up -d` 会自动合并。取值类的调整请改 `.env`，不要写进覆盖文件。

### Windows 启动脚本

Windows 侧由 `scripts/windows/` 下的一组脚本负责拉起服务端，脚本本身不含任何绝对路径，取值全部来自同目录的 `deploy.env`：

```text
scripts/windows/
├── start.bat                 # 拉起的唯一逻辑出口：读取值、判重、启动、重定向日志
├── start-hidden.vbs          # 只是壳：隐藏窗口调用 start.bat
├── service.bat               # 注册入口：校验参数、分发路线
├── run-foreground.bat        # 执行体：前台运行服务端并落日志，供下面两条路线复用
├── lib/load-env.bat          # 公共：读取并校验 deploy.env
├── impl/schtasks.bat         # 路线一：任务计划程序
├── impl/nssm.bat             # 路线二：NSSM 包装成 Windows 服务
├── deploy.env.example        # 取值样例（入库）
└── deploy.env                # 本机取值（不入库）
```

七个批处理 / VBScript 文件加一份取值样例；`deploy.env` 由样例复制而来：

```sh
cp scripts/windows/deploy.env.example scripts/windows/deploy.env
```

| 取值 | 含义 |
| --- | --- |
| `EUCLI_DEPLOY_DIR` | 部署目录，服务端程序、配置与数据所在处 |
| `EUCLI_SERVER_EXE` | 服务端程序文件名，纯文件名；脚本在部署目录下找它，进程判重也只用它 |
| `EUCLI_CONFIG` | 配置文件，可相对部署目录，也可绝对路径 |
| `EUCLI_LOG_DIR` / `EUCLI_LOG_FILE` | 日志目录与文件名 |
| `EUCLI_SERVICE_NAME` | 注册成任务或服务时使用的名字，只描述服务端 |
| `EUCLI_NSSM` | `nssm.exe` 位置；留空则从 `PATH` 查找，只有 NSSM 路线用到 |

两层分工：`start.bat` 是唯一逻辑出口，直接双击能看到每一步提示；`start-hidden.vbs` 只有一句 `Run`，不重复任何判断。开机自启把快捷方式指向 `start-hidden.vbs`，放进 `shell:startup` 即可。

日志形如：

```text
[eucli] 启动 eucli-deep-wiki-server.exe
[eucli] 配置 E:\eucli-deploy\config\config.json
[eucli] 日志 E:\eucli-deploy\logs\sync.log
```

重复执行会被判重拦下，输出 `已在运行，跳过启动`。判重对象只能是服务端程序本身——命令行是一次性进程，不参与也不影响这套判断。

### 注册为开机任务或系统服务

不想依赖登录与快捷方式时，用同一个入口注册：

```sh
service.bat install schtasks            # 开机以 SYSTEM 运行（管理员）
service.bat install schtasks --user     # 登录时以当前用户运行（同样需要管理员）
service.bat install nssm                # 注册为 Windows 服务，需已安装 nssm
service.bat uninstall schtasks          # 移除任务，幂等
service.bat uninstall nssm              # 移除服务，幂等
service.bat status                      # 查看注册与运行状态
```

两条路线共用同一个入口 `service.bat` 与同一份取值校验（`lib/load-env.bat`），只在注册命令上分叉：`impl/schtasks.bat` 走任务计划程序，`impl/nssm.bat` 走 NSSM 服务。两条路线的执行体都是 `run-foreground.bat`（它把工作目录固定在部署目录，再启动服务端程序），因此进程生命周期与任务或服务的状态一致，日志也不会丢。

`onstart` 与 `onlogon` 触发器都要求管理员权限，`--user` 只改变运行身份。以 SYSTEM 运行时任务不依赖任何用户登录。

`nssm` 路线额外设置了 `AppExit Default Restart`，进程崩溃会被自动拉起；`deploy.env` 里的 `EUCLI_NSSM` 可指定 `nssm.exe` 位置，留空则从 PATH 查找。

### 仓库名单

`repositories.json` 记录参考仓库名单，位于 `dataRoot` 之下，是服务端的数据。名单里的地址必须是加密传输地址（`https://`）——服务端既有校验只接受这一种，明文地址会被拒为 `400 invalid_url`。改动方式有三种，都会让服务端在下一个合适时机采用新名单：

```sh
eucli-dw repo add https://github.com/xxxx/xxxx.git     # 加入（可一次多条，整批原子）
eucli-dw repo add --branch main --history full https://github.com/xxxx/xxxx.git
eucli-dw repo rm xxxx                                  # 只摘名单，磁盘数据保留（默认）
eucli-dw repo rm xxxx --drop-data                      # 连磁盘数据一起删
eucli-dw repo list                                     # 看名单：声明的地址与档位、实际生效档位、是否已落盘
```

- 加入走 `POST /v1/repos`：默认让新名单立即生效（`--no-reload` 则只写名单，等下一轮同步前自动加载）。整批原子：任何一条地址不合法，整批都不生效。
- 移出走 `DELETE /v1/repos/{name}`：缺省只从名单摘除，`dataRoot/repository/<仓库>` 下的数据保留。命令行默认也是这个安全动作，只有显式 `--drop-data`（映射为服务端 `keepData=false`）才连数据一起删。
- 直接手改文件也可以，服务端按「文件指纹（修改时间 + 大小）」发现变化后重新加载。

名单只决定「要不要对外提供、要不要同步」，不负责回收磁盘：摘出名单后 `GET /v1/repos/{name}`、`/tree`、`/file`、`/find`、`/search`、`/archive`、`/bundle` 对它一律返回 `404 repo_not_found`，磁盘上的目录仍然在，需要删数据要显式要求。

### 运行期热加载

服务不只会在启动时读一次配置文件与名单文件。以下时机都会比对两个文件的指纹，指纹变了才重新加载，没变时只花两次 `stat`：

- 每轮定时同步开始前；
- 每次按需同步（`POST /v1/repos/{name}/sync`）与远端探测前；
- 每个受保护的 HTTP 请求进入前（`/status` 与全部 `/v1/...`）。

重载后立即生效的有：鉴权用的 `apiKeys`（新增的 key 下一个请求就能用，删掉的立刻 401）、`syncInterval`（等待中的计时按新周期重新排）、`gitCommandTimeout`、`maxParallelSyncs`、`defaultHistory`、GC 与体积阈值、以及整份仓库名单（同步清单与只读视图一起换，不会出现「同步了但查不到」）。

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

### 同步策略

服务每轮都会检查名单中的参考仓库。若本地参考库不存在，会下载；若本地目录不是名单对应的正确参考库，会重建。

若本地参考库来源正确、没有本地改动，且远端没有新变化，服务会跳过整理动作。只有远端有变化，或本地参考库出现改动、临时文件等不干净状态时，才会重新整理并对齐远端。

同步日志和状态结果会区分 `skipped`、`updated`、`cloned`、`recreated` 等动作，便于判断每个参考仓库本轮实际发生了什么。

定时同步之外，也可以按需触发：`POST /v1/repos/{name}/sync` 立刻同步一个仓库并同步返回结果（已有同步在跑时返回 `409 sync_busy`，不排队）；`POST /v1/sync` 触发一轮全量同步，立即返回 `202`。命令行的对应命令是 `eucli-dw sync <仓库>` 与 `eucli-dw sync --all`。

### 状态与运行期事实

```text
http://127.0.0.1:9997/status
```

`/status` 返回当前或上一轮的同步快照：`running` 表示是否有同步在跑，`results` 是已记录的结果，`summary` 按动作统计条数。运行中 `summary` 是已处理部分的汇总，结束后是全量汇总。

同一个响应里还带着运行期状态，用来确认「改的东西到底生效了没有」：

| 字段 | 含义 |
| --- | --- |
| `configLoadedAt` | 当前生效配置的加载时间 |
| `indexLoadedAt` | 当前生效仓库名单的加载时间 |
| `repositoryCount` | 当前生效的仓库数 |
| `syncInterval` / `gcThresholdMB` / `gcGrowthMB` / `sizeLimitMB` / `defaultHistory` | 当前生效的取值（改完配置后可直接对照） |
| `lastReload` | 最近一次重载结果：`at`、`configReloaded`、`indexReloaded`、`repositories`、`needsRestart`，失败时还有 `error` |
| `needsRestart` | 改了但必须重启才能生效的字段名列表，空数组表示没有 |

每个仓库的远端状态与体积保护记录在 `/v1/state`：

| 字段 | 含义 |
| --- | --- |
| `remoteMissing` | 远端已被判定不可用，后续同步直接跳过该仓库 |
| `remoteMissingAt` | 判定远端不可用的时间 |
| `consecutiveFailures` | 连续同步失败次数，成功一次即清零 |
| `lastError` | 最近一次同步失败的错误信息，成功后清空 |
| `blocked` / `blockedSize` | 体积保护：`.git` 超过 `sizeLimitMB` 后停止同步 |

远端不可用类失败会在连续失败计数达到 3 次时判定远端消失；其它失败只累加 `consecutiveFailures` 并记录 `lastError`，不会触发该标记。确认远端恢复后执行 `eucli-dw repo unblock <仓库>`，会同时清掉 `remoteMissing`、失败计数与 `lastError`，下一轮同步重新尝试。这个动作作用在服务端自己的运行态上，不会被下一次状态写回覆盖。

`GET /v1/state` 只列出「有状态」的仓库：返回空对象表示当前没有任何仓库处于失败、停止同步或体积保护状态；`consecutiveFailures` 从第一次失败起就会记录，因此即使还没到 3 次判定门槛，也能从这里看到哪个仓库在连续失败。

### 接口

服务对已同步的参考仓库提供只读内容接口，另外提供一组管理能力（名单、解封、瘦身、缓存）。内容类接口永远不能写入、修改或删除仓库里的文件；管理动作只作用于服务端自己的数据，不改变任何远端仓库。

除 `/healthz` 外，所有接口都要求携带 key：

```sh
curl -H "Authorization: Bearer <key>" http://127.0.0.1:9997/v1/repos
```

key 配置在服务端配置的 `apiKeys` 数组里，可以放多个，便于给不同 Agent 分别发放、单独吊销。鉴权是 Bearer 头加摘要常量时间比较；改配置即热更新，不需要重启。

| 方法与路径 | 说明 |
| --- | --- |
| `GET /healthz` | 健康检查，唯一不需要 key 的接口 |
| `GET /status` | 同步状态与运行期状态 |
| `GET /v1/repos` | 名单总览：分支、提交、浅克隆、上次同步时间、体积、保护状态，以及名单声明的事实 |
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
| `POST /v1/reload` | 立即重载配置与名单，返回本次重载结果 |
| `GET /v1/state` | 每个仓库的远端状态与体积保护记录 |
| `POST /v1/repos` | 把一批地址加入名单（整批原子，可要求立即生效） |
| `DELETE /v1/repos/{name}?keepData=` | 从名单移除（缺省保留磁盘数据） |
| `POST /v1/repos/{name}/unblock` | 解除停止同步状态；本来就未被拦也算成功 |
| `POST /v1/repos/{name}/compact?dryRun=` | 就地瘦身（可预演），与同步互斥 |
| `GET /v1/cache?repo=` | 缓存产物清单，目录不存在等于空清单 |
| `DELETE /v1/cache?repo=&confirm=1` | 清理缓存产物，必须显式确认 |

#### 名单总览与名单变更

`GET /v1/repos` 每个仓库除了同步事实，还带四个名单事实与一个可选实测值：

| 字段 | 含义 |
| --- | --- |
| `url` | 名单里声明的地址 |
| `history` | 名单里声明的历史档位，取值 `shallow` 或 `full`，未声明时为空 |
| `effectiveHistory` | 实际生效的档位（声明值或全局默认） |
| `onDisk` | 磁盘上是否已落盘 |
| `measuredBytes` | 只在 `measure=1` 时出现：本次**现测**的元数据体积 |

`measure` 缺省为假。`gitBytes` 的语义固定为「上次同步成功时记录的体积」，不会被现测值覆盖；两者意义不同，别混用。

`POST /v1/repos` 请求体：

```json
{"urls": ["https://…", "https://…"], "branch": "", "history": "", "reload": true}
```

`urls` 必填且至少一项，`branch`、`history` 可选，`reload` 缺省为真。`urls` 里的地址必须是加密传输地址（`https://`）——服务端既有校验只接受这一种。成功返回 `{added: [{name, url, branch, history, effectiveHistory}], repositories: N, reloaded: {configReloaded, indexReloaded, repositories, error}}`；`reload` 为假时 `reloaded` 不出现。失败区分 `400 invalid_url`（不是加密传输地址、带查询串或锚点、取不出名字）、`400 invalid_name`、`400 invalid_history`、`400 invalid_branch`、`400 invalid_body`（请求体不是合法结构化数据或字段类型不对）、`409 duplicate_name`、`500 index_write_failed`。

`DELETE /v1/repos/{name}` 的 `keepData` 缺省为真：只摘名单、保留 `dataRoot/repository/<仓库>` 下的数据；要连数据一起删得显式传 `keepData=false`，命令行的对应开关是 `--drop-data`（默认不加就是保留）。成功返回 `{name, removed, dataExisted, dataKept}`：`dataExisted` 说的是「操作之前磁盘上有没有副本」，`dataKept` 说的是「操作之后还留着没有」，两个都要看，才不会把「本来就没有副本」错说成「数据被删了」。失败区分 `404 repo_not_found`、`500 index_write_failed`。

#### 解封、瘦身与缓存

`POST /v1/repos/{name}/unblock` 请求体可选 `{"clearFailures": true}`（缺省为真，连带清零失败计数与最近错误）。成功返回 `{name, cleared, previous: {blocked, blockedSize, remoteMissing, consecutiveFailures, lastError}}`——`previous` 用来原样回显「动手之前是什么状态」。本来没有被拦是成功：`cleared` 为假、状态码仍是 `200`。

`POST /v1/repos/{name}/compact?dryRun=1` 返回 `{name, action, branch, before: {shallow, gitBytes}, after: {shallow, gitBytes}, durationMs}`。`action` 有四种，**全部是成功**：

| `action` | 含义 |
| --- | --- |
| `compacted` | 已执行转换 |
| `would_compact` | 预演认为会转换 |
| `already_shallow` | 本来就是浅克隆 |
| `skipped_full_history` | 名单要求完整历史，跳过 |

失败区分 `404 repo_not_found`、`409 sync_busy`、`400 detached_head`、`424 remote_unreachable`、`500 git_failed`。

`GET /v1/cache` 返回 `{files: [{name, repo, revision, bytes, modifiedAt}], totalBytes}`，不返回服务端绝对路径。`DELETE /v1/cache` 必须带 `confirm=1`（否则 `400 confirm_required`），返回 `{removed, bytes, skipped: [{name, reason}]}`；只删缓存产物文件，正在生成的进 `skipped` 而不是硬删，缓存正忙时 `409 cache_busy`。

#### 查询参数与上限

| 参数 | 适用 | 缺省 | 上限 / 规则 |
| --- | --- | --- | --- |
| `depth` | `tree`、`overview` | `tree` 为 1，`overview` 为 2 | 8，超过按 8 处理 |
| `dirsOnly` | `tree` | 假 | 布尔 |
| `lines` | `file` | 整份 | 四种写法，见下 |
| `maxBytes` | `file` | 不限 | 超出时退到最后一个完整行，绝不返回半行残句 |
| `limit` | `search` | 100 | 100 |
| `context` | `search` | 0 | 每处命中前后各 10 行 |
| `offset` | `search`、`find` | 0 | 非负 |
| `limit` | `find` | 200 | 10000，超过按 10000 处理 |
| `match` | `find` | `exact` | `exact` 或 `substring` |
| `measure` | `repos` | 假 | 布尔 |

`find` 只按文件名匹配，不读文件内容。`name` 含 `*`、`?`、`[` 时按通配处理；否则默认要求文件名与 `name` **完全一致**（不区分大小写），要换成「包含即可」用 `match=substring`。`path` 是检索范围——遍历从该目录开始，不是在结果里过滤。结果里的 `entries` 每项含 `name`、`path`、`type`、`size`；`scannedFiles` 是遍历过的文件数，`truncated` 表示还有未返回的命中。

`search` 结果里的 `scannedBytes` 是实际读取的字节数（跳过的二进制文件也计入），`scannedFiles` 是实际参与匹配的文本文件数。`context` 让每处命中带上前后各若干行，命中项的 `before` / `after` 给出这些行的行号与原文。

超过单文件上限的文件不参与检索，它们会被列在 `oversizedFiles` 里，`oversizedFilesCount` 是总数（明细最多 20 条）。这样「大文件没搜」不会被误读成「大文件里没命中」。

#### 文件响应头

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

打包默认输出 zip，加 `format=tar.gz` 可换格式；归档不包含 `.git`，总量上限 512 MiB、条目上限两万。bundle 按提交号缓存在服务端 `dataRoot/cache` 下，同一提交只生成一次。

#### 错误与状态码

边界处理都是明确报错，不做静默兜底：

| 情况 | 响应 |
| --- | --- |
| 路径越出仓库 | `400 invalid_path` |
| 仓库不在名单、尚未同步或已摘出名单 | `404 repo_not_found` |
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
| 加入名单时地址不是加密传输地址（服务端只接受 `https://`）、带查询串或锚点、取不出仓库名 | `400 invalid_url` |
| 加入名单时推导出的名字不是合法单层目录名 | `400 invalid_name` |
| 加入名单时历史档位不是 `shallow` / `full` | `400 invalid_history` |
| 加入名单时分支名不合法 | `400 invalid_branch` |
| 加入名单时请求体不是合法结构化数据，或字段类型不对 | `400 invalid_body` |
| 加入名单时与已有项重名 | `409 duplicate_name` |
| 名单写盘失败 | `500 index_write_failed` |
| 状态写盘失败 | `500 state_write_failed` |
| 同步正在跑（互斥动作撞上） | `409 sync_busy` |
| 缓存正在生成（互斥动作撞上） | `409 cache_busy` |
| 瘦身时仓库处于游离头指针 | `400 detached_head` |
| 瘦身时远端不可达 | `424 remote_unreachable` |
| 瘦身时 Git 命令失败 | `500 git_failed` |
| 清缓存时没带确认 | `400 confirm_required` |
| 删缓存文件失败 | `500 remove_failed` |

所有路径都经 `os.Root` 收敛，符号链接无法把访问带出仓库；目录树、检索与查找都会跳过 `.git`。

#### 部署安全

服务端只跑明文 HTTP，没有权限分级、没有速率限制、没有审计日志。绑在 `127.0.0.1` 上时只服务本机；要跨机访问，请在前面加一层做加密与访问控制的反向代理，不要把明文端口直接暴露到公网。

## 命令行

命令行是另一个程序：装到要查阅的机器上，填好地址与 key 就能用。它不读服务端配置，不知道服务端目录怎么摆，也不接触服务端机器上的任何文件。

### 安装与自检

```sh
cd cli && go build -o eucli-dw ./cmd/eucli-dw
eucli-dw version        # 打印构建信息：版本、Go 版本、提交号（有的话）
```

把 `eucli-dw` 放进 `PATH`，或者用绝对路径调用。要分发到别的机器，复制这一个文件即可——它不需要 `data/`、不需要部署目录，也不需要服务端的配置文件。

### 连接配置

命令行配置只有两个字段：

```json
{
  "server": "http://127.0.0.1:9997",
  "apiKey": "0123456789abcdef"
}
```

`server` 填服务端的地址；`apiKey` 填服务端 `apiKeys` 里的任意一把（服务端要求每把至少 16 个字符）。命令行这一侧只检查「非空、不含空白」这类基本形态，真正有效的判定在服务端：填错了就是 401，退出码 4。

查找顺序是固定的：

```text
--config 参数  →  EUCLI_DW_CONFIG 环境变量（可以指向目录）  →  平台默认位置
```

```text
Windows: %USERPROFILE%\.eucli-dw\config.json
Linux:   $XDG_CONFIG_HOME/eucli-dw/config.json  →  ~/.config/eucli-dw/config.json
```

```sh
eucli-dw config set server http://127.0.0.1:9997
eucli-dw config set key 0123456789abcdef
eucli-dw config show          # key 只显示首尾各四位
eucli-dw config path          # 打印配置文件位置
```

`--server`、`--key` 只覆盖这一次调用，不写回文件。`server` 是**命令行连服务端的地址**，支持 `http://` 与 `https://` 两种协议，只写协议与主机端口，不要带路径；它和加入名单时的仓库地址不是一回事——仓库地址只接受加密传输地址（`https://`）。

**服务端配置和命令行配置有什么区别**——两份文件、两套字段、两个归属，互不读取：

| | 服务端配置 | 命令行配置 |
| --- | --- | --- |
| 谁读它 | `eucli-deep-wiki-server` 进程 | `eucli-dw` 进程 |
| 有哪些字段 | `dataRoot`、`httpAddress`、`apiKeys`、同步与体积阈值等 12 项 | 只有 `server`（地址）与 `apiKey`（一把 key） |
| 放在哪 | 部署目录里的 `config/config.json`（容器内 `/app/config.json`） | 平台默认位置，或 `EUCLI_DW_CONFIG` 指向的位置 |
| 环境变量 | 一个都不读 | 只用 `EUCLI_DW_CONFIG` 定位文件本身 |
| 数量关系 | 一台服务端一份 | 每台要查阅的机器各一份，可以访问同一台服务端 |

两者不是同一份文件的不同叫法：命令行配置里没有数据根、没有仓库目录，服务端配置里也没有「客户端连谁」。不要让命令行去指向服务端那份文件——它读不出地址与 key，也不该读。

### 命令清单

命令分四组。第一组只看内容，第二组看服务端状态或让它做事，第三组改名单与缓存（同样全部由服务端执行），第四组是命令自身的功能。

**读取内容**

| 命令 | 作用 | 走哪个接口 |
| --- | --- | --- |
| `repos [--sort name\|time\|size] [--json]` | 仓库清单：分支、提交、上次同步、体积、状态标记 | `GET /v1/repos` |
| `overview [参数] <仓库>` | 目录骨架 + README 开头 + 清单文件 | `GET /v1/repos/{name}` + `/tree` + `/file` |
| `tree [参数] <仓库> [目录]` | 列目录，可一次展开多层 | `GET /v1/repos/{name}/tree` |
| `get [参数] <仓库> <路径>...` | 读文件，可多路径、可切段、可限长 | `GET /v1/repos/{name}/file` |
| `search [参数] <仓库> <关键词>` | 按内容检索 | `GET /v1/repos/{name}/search` |
| `find [参数] <仓库> <文件名>` | 按文件名查找 | `GET /v1/repos/{name}/find` |

**服务端状态与动作**

| 命令 | 作用 | 走哪个接口 |
| --- | --- | --- |
| `status [--json]` | 同步状态与运行期状态 | `GET /status` |
| `reload [--json]` | 让服务端重载配置与名单 | `POST /v1/reload` |
| `sync [--json] <仓库>` | 立刻同步一个仓库 | `POST /v1/repos/{name}/sync` |
| `sync --all [--json]` | 触发一轮全量同步 | `POST /v1/sync` |
| `pull <仓库>[:路径] ... [-o 文件] [--format zip\|tar.gz] [--progress] [--json]` | 下载快照包 | `GET /v1/repos/{name}/archive`、`GET /v1/archive` |
| `bundle <仓库> [-o 文件] [--progress] [--json]` | 下载带完整历史的 bundle | `GET /v1/repos/{name}/bundle` |

**名单与缓存**

| 命令 | 作用 | 走哪个接口 |
| --- | --- | --- |
| `repo add [--branch B] [--history shallow\|full] [--no-reload] <地址>...` | 把一批仓库加入名单 | `POST /v1/repos` |
| `repo list [--sizes] [--json]` | 看名单：声明地址、声明档位、生效档位、是否落盘 | `GET /v1/repos?measure=` |
| `repo rm <仓库> [--drop-data]` | 从名单移除（默认保留磁盘数据） | `DELETE /v1/repos/{name}?keepData=` |
| `repo unblock <仓库>` | 解除停止同步状态 | `POST /v1/repos/{name}/unblock` |
| `repo compact [--dry-run] [仓库]...` | 就地瘦身，可预演 | `POST /v1/repos/{name}/compact` |
| `repo check [仓库]... [--json]` | 探测远端是否可达 | `GET /v1/repos` + 并发 `POST /v1/repos/{name}/check` |
| `cache list [--json]` | 缓存产物清单 | `GET /v1/cache` |
| `cache clean [--yes] [仓库]` | 清理缓存产物 | `DELETE /v1/cache` |

**命令自身**

| 命令 | 作用 |
| --- | --- |
| `config set server\|key <值>` / `config show` / `config path` | 管理自己的连接配置 |
| `version [--json]` | 打印构建信息 |
| `help` | 打印帮助 |

查询类命令的通用参数：`--config 路径`、`--server 地址`、`--key key`、`--json`、`--timeout 秒数`（普通请求的超时，缺省 30 秒；同步与瘦身这类长动作不走这个值，见下）。

### 读取类命令

`repos` 一行一个仓库，列依次是：仓库名、分支、提交号前 12 位、上次同步时间、`.git` 体积，随后是状态标记（`BLOCKED(大小)` / `MISSING` / `SHALLOW`）。这些字段由服务端一次返回，不必再单独查状态接口。`--sort` 可以按名字（默认）、最近同步时间或体积排序，同值一律按名字升序，输出可复现。

`overview` 是「先看仓库长什么样」的入口：一次请求拿到元数据与目录骨架，随后并发取回 README 开头与清单文件（`go.mod`、`package.json`、`Cargo.toml` 等），把原先需要 `tree` + 多次 `get` 的往返压成一条命令。`--depth`（缺省 2，服务端上限 8）、`--readme-lines`（缺省 40，0 表示不展示）、`--manifests`（缺省 3，0 表示不展示）、`--manifest-lines`（缺省 40）控制展开规模。

`tree` 列出目录内容，目录优先、隐藏 `.git`。`--depth` 一次展开多层（缺省 1，上限 8，超过按 8 处理）；`--dirs-only` 只列目录，用来看骨架。

`get` 读文本文件，可一次给多个路径，按位置参数顺序输出。`--lines` 按行切段（写法见服务端章节的行区间表），`--max-bytes` 限长（超出退到最后一个完整行）。

```sh
eucli-dw tree claw-code --depth 2 --dirs-only
eucli-dw get claw-code cli.js --lines 362-520
eucli-dw get claw-code README.md src/index.ts --max-bytes 4096
```

`search` 在仓库内按行做子串检索。`--path` 限定目录，`--context` 让每处命中带上下文（上限 10），`--offset` 翻页，`--limit` 单次最多 100 条（缺省 100）。被跳过的超大文件会在 stderr 里说出来。

`find` 只按文件名匹配，不读文件内容。`--limit` 缺省 200、上限 10000，超过上限按 10000 处理，文本模式的截断提示会写明该上限；`--offset` 翻页；`--substring` 把「完全一致」换成「包含即可」；`name` 含 `*`、`?`、`[` 时按通配处理。

精确匹配落空时，`eucli-dw find` 会按「包含」口径再查一次：确实有包含匹配就在 stderr 报出数量与示例路径，提示加 `--substring`。通配符模式、正在翻页、或已经传了 `--substring` 时不做这次附加查询；附加查询失败也不影响原本的空结果。

```sh
eucli-dw search claw-code subagent --context 2 --limit 20
eucli-dw find claw-code "index.ts" --path src
eucli-dw find claw-code "*.md" --path docs --substring
```

### 状态与同步类命令

`status` 至少给出运行状态、上轮结果统计、下一轮预计时间、仓库数、同步周期、配置与名单的加载时间、最近一次重载结果，以及需要重启才能生效的字段。

`reload` 打印服务端的重载结果，失败时退出码为 1。

`sync` 立刻同步：`sync <仓库>` 等这一个跑完并返回结果，`sync --all` 只触发一轮全量、立刻返回。同步失败时退出码非零，错误同时以 `{"ok":false,...}` 写入 stderr。同步类请求自带 15 分钟预算，不受 `--timeout` 影响。

```sh
eucli-dw sync claw-code
eucli-dw sync --all --json
```

`pull` 下载快照包（`-o` 指定输出文件，`--format` 选 `zip` 或 `tar.gz`），`bundle` 下载带完整历史的 bundle。两者都先写 `*.part`，成功后才改名成目标文件，失败不留半截产物；`--progress` 把进度写 stderr，stdout 只留结果。

```sh
eucli-dw pull claw-code[:路径] ... -o bundle.zip
eucli-dw bundle claw-code -o repo.bundle
```

### 名单与缓存类命令

这八条命令都只发请求：名单文件、同步状态、仓库目录、缓存目录都在服务端那边，命令行不知道它们的路径，也不需要知道。

`repo add` 一次可给多个地址，**整批原子**——任何一条地址不合法，整批都不生效。地址必须是加密传输地址（`https://`）：服务端既有校验只接受这一种，明文地址、带查询串或锚点、取不出仓库名的都会被拒（`400 invalid_url`）。`--branch` 指定分支（不合法为 `400 invalid_branch`），`--history shallow|full` 指定历史档位，`--no-reload` 表示只写名单、不让它立即生效（等下一轮同步前自动加载）。成功时输出新增项（含推导出的仓库名与生效档位）与名单总数。

`repo list` 是**名单视角**：仓库名、名单声明的地址、声明的档位、实际生效的档位、是否已落盘。`--sizes` 会让服务端对每个已落盘仓库现测体积（`measuredBytes`）——这项会真的去扫元数据目录，比不带时慢。

`repos` 与 `repo list` 的区别：`repos` 看的是仓库**当前状态**（提交号、上次同步时间、体积、BLOCKED/MISSING 标记，可排序）；`repo list` 看的是**名单声明**与落盘情况。两者数据都来自 `GET /v1/repos`，只是关注点不同。

`repo rm` 默认是**安全动作**：只把仓库从名单里摘除，`dataRoot/repository/<仓库>` 下的数据原样保留——摘名单不等于删数据，这件事不该靠猜。要连磁盘数据一起删，必须显式加 `--drop-data`（命令行把它映射成服务端的 `keepData=false`）；不加这个开关，磁盘上什么都不会被删。

`repo unblock` 解除停止同步状态。本来就没有被拦也算成功，退出码 0。

`repo compact` 就地瘦身：给了仓库名就只处理这几个，不给就处理名单里的全部；`--dry-run` 只预演不落地。四种结论（已转换 / 会转换 / 本来就是浅克隆 / 因要求完整历史而跳过）全部按成功处理，输出 `action` 说明到底发生了什么。瘦身是分钟级操作，命令行给它单独的等待预算，不沿用普通请求的 30 秒超时；同步正在跑时服务端返回 `409`，命令行退出码 6，不排队等待。

`repo check` 先取名单，再对名单里的仓库并发探测远端（也可只传仓库名来缩小范围）：远端可达输出 `可达`，不可达输出 `不可达` 与错误信息，不在名单里的名字输出 `未知仓库`，最后给出汇总行。「不在名单」与「不可达」是两件事，不会混成一个失败。全部可达返回 0，只要出现不可达或未知就返回 1；`--json` 输出 `{checked, reachable, unreachable, unknown, results[]}`。

```sh
eucli-dw repo add https://github.com/xxxx/xxxx.git
eucli-dw repo add --history full https://github.com/xxxx/xxxx.git
eucli-dw repo list --sizes
eucli-dw repo rm claw-code                             # 只摘名单，磁盘数据保留
eucli-dw repo rm claw-code --drop-data                 # 连磁盘数据一起删
eucli-dw repo check claw-code
eucli-dw repo unblock claw-code
eucli-dw repo compact --dry-run
```

`cache list` 列出服务端缓存产物的文件名、归属仓库、对应提交、字节数与修改时间，并给合计。接口不返回服务端绝对路径，因此输出里也不会有服务端目录结构。

`cache clean` 必须带 `--yes`：不带就算用法错误、退出码 2（服务端对「没带确认」的拒绝是 `400 confirm_required`），可给一个仓库名把清理范围收窄到该仓库。删除的是缓存产物文件，目录里其他东西不动；正在生成的条目会被跳过并在结果里给出原因。

```sh
eucli-dw cache list
eucli-dw cache clean --yes
eucli-dw cache clean --yes claw-code
```

### 输出约定与退出码

**stdout 只放数据**（条目、正文、清单行），关于这批数据的说明——截断、还有更多、跳过了哪些大文件、参数按上限收敛——一律写 stderr。因此 `eucli-dw tree repo --depth 2 | wc -l` 这类管道不会被提示行干扰。`--json` 把所有事实放进结构化字段，不依赖 stderr。

**退出码**（便于脚本判断该重试还是放弃）：

| 码 | 含义 | 典型来源 |
| --- | --- | --- |
| 0 | 成功 | 全部正常完成，含「本来就没被拦」「本来就是浅克隆」这类无事可做的成功 |
| 1 | 其它失败 | `500` 类写入或 Git 失败、重载失败、其它未归类失败 |
| 2 | 参数或用法错误 | 本地参数不对、未知命令；服务端的 `400`（`invalid_url`、`invalid_name`、`invalid_history`、`invalid_branch`、`invalid_body`、`confirm_required`、`detached_head` 等） |
| 3 | 仓库或路径不存在 | `404` |
| 4 | 鉴权失败 | `401` / `403` |
| 5 | 网络或超时 | 连不上、超时；服务端的 `424 remote_unreachable` |
| 6 | 任务冲突 | `409`（`sync_busy`、`cache_busy`、`duplicate_name`） |
| 7 | 被拒绝 | `413` 过大、`415` 非文本 |

任何一层都有帮助：`eucli-dw help`、`eucli-dw repo --help`、`eucli-dw get -h`，退出码统一为 0；未知命令返回 2 并打印用法。

## 开发资料目录

开发过程中的任务记录、设计决定与工作区约定统一放在 `.dev-management/`，该目录拥有独立的 Git 记录。
