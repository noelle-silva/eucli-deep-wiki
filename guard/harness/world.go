package harness

import (
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"eucli-deep-wiki-contract-guard/contract"
)

// Kind 是现场的种类：一条用例需要哪一种磁盘与依赖，就开哪一种。
type Kind string

const (
	// KindStandard 是标准现场。
	KindStandard Kind = "standard"
	// KindEmpty 是空现场。
	KindEmpty Kind = "empty"
	// KindBulk 是批量现场。
	KindBulk Kind = "bulk"
	// KindGit 是真实版本管理现场。
	KindGit Kind = "git"
	// KindBrokenIndex 是名单文件被占住的现场。
	KindBrokenIndex Kind = "broken-index"
	// KindBrokenState 是状态文件被占住的现场。
	KindBrokenState Kind = "broken-state"
	// KindGitBusy 是「同步真的卡在网上」的版本管理现场。
	KindGitBusy Kind = "git-busy"
)

// Kinds 是契约世界名到现场种类的一一对应。
//
// 键直接用契约里的常量：两侧各写一份字符串的话，改一处就会静默错位。
var Kinds = map[string]Kind{
	contract.WorldStandard:    KindStandard,
	contract.WorldEmpty:       KindEmpty,
	contract.WorldBulk:        KindBulk,
	contract.WorldGit:         KindGit,
	contract.WorldGitBusy:     KindGitBusy,
	contract.WorldBrokenIndex: KindBrokenIndex,
	contract.WorldBrokenState: KindBrokenState,
}

// APIKey 是现场里唯一的钥匙：≥ 服务端的长度下限，因此两侧都直接用它。
const APIKey = "contract-guard-key-0123456789"

// 现场里各条路径，全部落在一次性的临时根目录下。
const (
	dataRootName    = "data"
	repositoryName  = "repository"
	indexFileName   = "repositories.json"
	configFileName  = "config.json"
	cliConfigName   = "cli-config.json"
	downloadDirName = "downloads"
)

// World 是一次隔离验证的全部现场。
type World struct {
	// Kind 是这个现场的种类。
	Kind Kind
	// Root 是这次验证的根目录。
	Root string

	// DataRoot 是服务端的数据根。
	DataRoot string
	// RepoDir 是仓库落盘根。
	RepoDir string
	// IndexPath 是名单文件。
	IndexPath string
	// StatePath 是同步状态文件。
	StatePath string
	// CacheDir 是缓存目录。
	CacheDir string
	// ConfigPath 是服务端配置。
	ConfigPath string
	// DownloadDir 是下载类命令的落点，避免往工作区写文件。
	DownloadDir string

	// Server 是真实运行起来的服务端进程。
	Server *Server
	// Hole 是这个现场的黑障；只有需要「卡住一条真实网络操作」的现场才有。
	Hole *BlackHole
	// Recorder 是挡在服务端前面的转发探针，命令行连的就是它。
	Recorder *Recorder
	// Proxy 是对外提供记录的地址。
	Proxy *httptest.Server
	// Command 是驱动真实命令行的入口。
	Command *Command

	// mu 保护 Close 与用例之间共享的可关闭资源。
	mu          sync.Mutex
	heldHandles []*os.File

	// pendingIndex 是等 servic 起好之后再让它生效的名单。
	pendingIndex []repositorySeed

	ownsRoot bool
}

// Options 是开一个现场所需的参数。
type Options struct {
	// Root 是指定根目录；为空时新建临时目录并在 Close 时删除。
	Root string
	// Kind 是现场种类。
	Kind Kind
}

// Open 铺好磁盘、构建并起真实服务端、建立命令行驱动器。
func Open(options Options) (*World, error) {
	binaries, err := Build()
	if err != nil {
		return nil, err
	}

	root := options.Root
	ownsRoot := false
	if root == "" {
		created, tempErr := os.MkdirTemp("", "eucli-contract-guard-")
		if tempErr != nil {
			return nil, fmt.Errorf("建立临时现场: %w", tempErr)
		}
		root = created
		ownsRoot = true
	}

	world := &World{
		Kind:        options.Kind,
		Root:        root,
		DataRoot:    filepath.Join(root, dataRootName),
		RepoDir:     filepath.Join(root, dataRootName, repositoryName),
		IndexPath:   filepath.Join(root, dataRootName, indexFileName),
		StatePath:   filepath.Join(root, dataRootName, "sync-state.json"),
		CacheDir:    filepath.Join(root, dataRootName, "cache"),
		ConfigPath:  filepath.Join(root, configFileName),
		DownloadDir: filepath.Join(root, downloadDirName),
		ownsRoot:    ownsRoot,
	}

	if err := world.seed(); err != nil {
		world.Close()
		return nil, err
	}

	server, err := startServer(binaries.Server, world.ConfigPath)
	if err != nil {
		world.Close()
		return nil, err
	}
	world.Server = server

	target, err := url.Parse(server.URL())
	if err != nil {
		world.Close()
		return nil, fmt.Errorf("解析服务端地址: %w", err)
	}
	world.Recorder = NewRecorder(target)
	world.Proxy = httptest.NewServer(world.Recorder)

	if err := world.ApplyIndex(); err != nil {
		world.Close()
		return nil, err
	}
	// 服务端启动后会先跑一轮同步；那一轮会动仓库目录与同步状态。
	// 现场必须等它收场，否则用例看到的会是一个「同步正在运行」的世界。
	if err := world.WaitIdle(); err != nil {
		world.Close()
		return nil, err
	}

	if err := world.writeCLIConfig(); err != nil {
		world.Close()
		return nil, err
	}

	command, err := newCommand(binaries.Client, filepath.Join(root, cliConfigName), world.DownloadDir)
	if err != nil {
		world.Close()
		return nil, err
	}
	command.recorder = world.Recorder
	world.Command = command
	return world, nil
}

// WaitIdle 等到服务端既不在同步、也已经跑完过至少一轮。
func (world *World) WaitIdle() error {
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		message := world.Get("/status", nil)
		if message.Status == 200 && message.JSON != nil {
			running, hasRunning := message.BoolField("running")
			_, finished := message.StringField("lastFinishedAt")
			if hasRunning && !running && finished {
				return nil
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("服务端在 3 分钟内没有进入空闲状态；服务端日志：%s", world.Server.Logs())
}

// URL 是调用方应该用的地址：命令行与探针都走这里，因此请求会被记录下来。
func (world *World) URL() string {
	return world.Proxy.URL
}

// writeCLIConfig 写命令行自己的连接配置：只有地址与钥匙，与服务端配置无关。
func (world *World) writeCLIConfig() error {
	content := fmt.Sprintf("{\n  \"server\": %q,\n  \"apiKey\": %q\n}\n", world.URL(), APIKey)
	return writeFile(filepath.Join(world.Root, cliConfigName), content)
}

// CLIConfigPath 是命令行自己的配置文件位置。
func (world *World) CLIConfigPath() string {
	return filepath.Join(world.Root, cliConfigName)
}

// RepoPath 返回现场里某个仓库的落盘目录。
func (world *World) RepoPath(name string) string {
	return filepath.Join(world.RepoDir, name)
}

// ReadIndex 读回磁盘上的名单文件内容，用于核对「整批原子」。
func (world *World) ReadIndex() (string, error) {
	data, err := os.ReadFile(world.IndexPath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// BreakIndex 占住名单写入必然经过的那个临时路径，此后任何一次写名单都会真实失败。
//
// 为什么占临时文件而不是正式文件：两个写盘实现都是「先写 <路径>.tmp，再改名」。
// 把正式文件换成目录只挡住了改名那一步，而 Windows 允许把文件改名到一个**空目录**
// 上，于是失败与否取决于时机，不是确定事实。占住临时路径则卡在「打开准备写」这一步，
// 两种平台、任何时候都必然失败。
func (world *World) BreakIndex() error {
	return occupy(world.IndexPath + indexTemporarySuffix)
}

// BreakState 占住状态写入必然经过的那个临时路径。
func (world *World) BreakState() error {
	return occupy(world.StatePath + indexTemporarySuffix)
}

// indexTemporarySuffix 与两个写盘实现用的临时后缀一致。
const indexTemporarySuffix = ".tmp"

// occupy 让一个路径无法被当作文件打开写：把一个目录放在那里。
func occupy(path string) error {
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	return os.Mkdir(path, DirectoryPerm)
}

// Close 结束服务端进程、关掉探针与仍然按住的句柄，并清掉自己建的临时目录。
func (world *World) Close() {
	world.mu.Lock()
	handles := world.heldHandles
	world.heldHandles = nil
	world.mu.Unlock()
	for _, handle := range handles {
		_ = handle.Close()
	}

	if world.Server != nil {
		world.Server.Stop()
		world.Server = nil
	}
	if world.Proxy != nil {
		world.Proxy.Close()
		world.Proxy = nil
	}
	if world.Hole != nil {
		world.Hole.Drop()
		world.Hole = nil
	}
	if world.ownsRoot {
		_ = os.RemoveAll(world.Root)
	}
}
