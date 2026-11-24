# 应用载体目录

平台按这个目录认应用：卡片、图标、平台托管时用的最小配置都放在这里。

| 文件 | 作用 |
|---|---|
| `fw-app.json` | 自我介绍卡：应用身份、入口文件、服务三段（启动参数 / 就绪 / 停止）的唯一出处 |
| `assets/icon.svg` | 卡片 `package.icon` 指向的图标 |
| `config.json` | 平台托管时的最小配置样例：只写没有默认值的字段。**钥匙不用写**——首次启动会由服务画像自己生成一把并落盘 |
| `fast-window-dev-protocol.json` | 动作字典：本应用对外提供哪些开发动作、哪个动作产成品 |
| `fast-window-dev-tool.mjs` | 协议工具本体：每个应用自带一份，与平台上其他应用同构 |

## 上架的包里有什么

包的内容就是「包根」这一层，压缩包解开后直接是下面这个布局，不要把仓库目录结构一起打进去（真套了一层外壳目录时，平台按清单所在那层裁掉前缀，但没必要绕这一步）：

```
fw-app.json                  平台清单：必须恰好一份，且位于包根
<package.windowsExecutable>  卡片声明的可执行文件
config.json                  本目录 config.json 原样复制
assets/icon.svg              本目录 assets/icon.svg 原样复制
```

平台把它解压到 `<安装根>/<id>/package/`：包根就是那层 `package/`。

两条来自平台解压与校验规则的硬约束：

- 压缩包里只能有一份 `fw-app.json`，多一份就被拒（`压缩包内存在多个 fw-app.json，拒绝安装`）。所以打包时不要在包根之外再放一份卡片副本。
- `package.icon` 在包里按相对路径校验，指向的文件必须真实存在，否则拒绝安装。

## 程序从哪里读配置与数据

平台启动进程时，工作目录取可执行文件所在目录（`src-tauri/src/app_lifecycle/process_owner.rs` 的 `current_dir` 就是 `executable.parent()`），也就是包根。于是包内的相对路径都从这里长出来：

| 读什么 | 怎么定位 | 落在包里的位置 |
|---|---|---|
| 配置 | 卡片给的启动参数 `-config config.json`，相对工作目录解析 | `<包根>/config.json` |
| 数据 | 配置里的数据根；样例没写 `dataRoot`，取默认值 `data`，同样相对工作目录 | `<包根>/data/` |
| 门牌号与口令 | 服务端把画像写在数据根下的 `service-profile.json` | `<包根>/data/service-profile.json` |

由此固定三点：

- `dataRoot` 必须留在默认值 `data`。平台的画像路径是固定相对路径（`src-tauri/src/app_service_profile.rs` 的 `data/service-profile.json`），改数据根平台就读不到画像。样例故意不写 `dataRoot`：默认值就是要留在平台读得到的那一格，多写一份只会多一处会漂的事实。
- 配置里的监听地址与钥匙只是画像首次生成时的初值；画像一旦存在就是权威（设计约定），端口与钥匙在平台界面上还能改。**样例刻意不写 `apiKeys`**：不写就由画像当场随机生成一把 64 位十六进制的口令，装完直接启动即可，不必先让人去编一把钥匙。
- `config.json` 必须随包提供：服务端启动第一件事就是读它，缺了直接退出。

## 上架清单与本目录卡片的差别

卡片是「源卡」，不直接进包。平台清单结构里 `version` 是必填的 `x.y.z`（三段纯数字），卡片里没有这个字段（版本事实在 `versionSource` 指向的文件里），所以上架清单在卡片基础上要补两处：

1. `version`：从 `versionSource` 指向的文件取 `version` 值写入；
2. `package.icon`：从仓库内路径改成包内路径 `assets/icon.svg`（即包里那一份）。

其余字段原样带过去；空的 `commands` 可以不带（平台按空数组处理）。`versionSource` 本身不进包：平台清单结构里没有这个字段，打包工具也不写它。

包根不限制只放这几个文件：上架包根层也可附带说明文档，平台只认清单与清单指向的两个相对路径。

打包前置条件：`versionSource` 指向的文件必须存在，且含非空的 `version`。

## 怎么打出一个上架包

在这个目录里执行一个动作：

```sh
node fast-window-dev-tool.mjs package
```

它会做三件事，一条命令走完：

1. 调 `tools/pack` 把服务端单独编译出来（`GOWORK=off`，不读另一侧源码），连同本目录的 `config.json` 压成一个基础成品包；
2. 解开基础包、写入补好 `version` 的清单、把图标放成 `assets/<文件名>`，重新打成商店包；
3. 落到 `dist/`，并在回执里给出包路径、包名与校验值。

回执独占一行、以 `FAST-WINDOW-DEV-RECEIPT: ` 开头，里面的 `data.artifact` 就是可以拿去上架的三个事实。

其余动作（`go -C` 开头的都是各自模块的自检）：

```sh
node fast-window-dev-tool.mjs server-test
node fast-window-dev-tool.mjs cli-test
node fast-window-dev-tool.mjs guard-test
```

`dist/` 已被版本管理忽略：产物是本机构建结果，不进仓库。

## 边界

这里放「交付给平台的事实」与产出成品的入口；产物一律落在 `dist/`，不散在别处。
