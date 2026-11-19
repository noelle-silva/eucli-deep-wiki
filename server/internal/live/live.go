// Package live 维护运行期可替换的配置、仓库索引与只读视图。
//
// 服务启动时读到的配置文件与索引文件不再是「一次性」的：定时同步开始前、
// 按需同步前、每个受保护的 HTTP 请求进入前，都会比对两个文件的指纹
// （修改时间 + 大小），只有指纹变化才重新加载。
//
// 重载时先构建完整的新快照，再整体替换，读者拿到的永远是完整的一份，
// 既不会读到半新半旧，也不会被一次失败的解析拖垮：解析失败时旧配置
// 与旧视图继续生效，失败原因记录在最近一次重载结果里。
//
// 门牌号与口令（服务画像）是启动期事实：它在首次加载配置时定下，之后每次重载
// 都重新落到生效配置上，因此配置文件怎么改也换不掉运行期的端口与钥匙。
package live

import (
	"fmt"
	"log"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"eucli-deep-wiki-server/internal/config"
	"eucli-deep-wiki-server/internal/repoindex"
	"eucli-deep-wiki-server/internal/serviceprofile"
	"eucli-deep-wiki-server/internal/vault"
)

// Options 是打开运行期状态所需的参数。
type Options struct {
	// ConfigPath 是服务配置文件的位置，运行期不变。
	ConfigPath string
	// HTTPAddress 是命令行给出的监听地址。
	// 非空时它对每一次重载同样生效，配置文件里的值不再作数。
	HTTPAddress string
}

// State 持有运行期可替换的配置、仓库清单与只读视图。
//
// 读路径是原子指针加载，不加锁；重载由 reloadMu 串行化，
// 并发触发的重载不会重复解析同一份文件。
type State struct {
	configPath  string
	httpAddress string

	// profile 是启动时定下的门牌号与口令。它是这两个事实的权威：
	// 每次都随配置一起落到生效视图里，因此重载换不回配置文件里的旧值。
	profile serviceprofile.Profile

	// startup 是启动时确定的配置，用来判定哪些字段改了必须重启。
	startup *config.Config
	// repositoryDir 与 indexPath 在启动时固化：读写位置随重载漂移没有意义。
	repositoryDir string
	indexPath     string

	snapshot atomic.Pointer[Snapshot]

	reloadMu sync.Mutex

	changedMu sync.Mutex
	changed   chan struct{}
}

// Open 读取配置与索引，建立第一份生效快照。
//
// 首次加载失败属于启动期错误，直接返回给调用方；服务不该带着空配置起床。
func Open(options Options) (*State, error) {
	cfg, err := config.Load(options.ConfigPath)
	if err != nil {
		return nil, err
	}

	// 画像：读现成的，或按配置文件里的初值生成一份落盘。
	// 它是端口与口令的权威，启动时定下就不再变——要换门牌号或口令得改画像并重启。
	profile, err := serviceprofile.Ensure(cfg.DataRoot, serviceprofile.Seed{
		Port: cfg.ListenPort(),
		Key:  cfg.FirstAPIKey(),
	})
	if err != nil {
		return nil, err
	}
	if err := adopt(cfg, profile, options.HTTPAddress); err != nil {
		return nil, err
	}
	log.Printf("service profile %s: port %d", serviceprofile.Path(cfg.DataRoot), profile.Port)

	state := &State{
		configPath:    options.ConfigPath,
		httpAddress:   options.HTTPAddress,
		profile:       profile,
		startup:       cfg,
		repositoryDir: cfg.RepositoryDir(),
		indexPath:     cfg.RepositoryIndexPath(),
		changed:       make(chan struct{}),
	}

	index, err := repoindex.Load(state.indexPath)
	if err != nil {
		return nil, fmt.Errorf("加载仓库索引 %s: %w", state.indexPath, err)
	}
	view, err := newVault(state.repositoryDir, index.Repositories)
	if err != nil {
		return nil, fmt.Errorf("建立只读视图: %w", err)
	}

	loadedAt := time.Now()
	snapshot := &Snapshot{
		Config:         cfg,
		Repositories:   normalizeRepositories(index.Repositories),
		Vault:          view,
		RepositoryDir:  state.repositoryDir,
		IndexPath:      state.indexPath,
		ConfigLoadedAt: loadedAt,
		IndexLoadedAt:  loadedAt,
		configStamp:    readStamp(state.configPath),
		indexStamp:     readStamp(state.indexPath),
		configInSync:   true,
		indexInSync:    true,
	}
	initial := Result{At: loadedAt}
	state.settle(snapshot, initial)
	state.snapshot.Store(snapshot)
	return state, nil
}

// Snapshot 返回当前生效的完整视图；返回值只读。
func (s *State) Snapshot() Snapshot {
	return *s.snapshot.Load()
}

// APIKeys 返回当前生效的 key 集合。
func (s *State) APIKeys() []string {
	return s.snapshot.Load().Config.APIKeys
}

// Changed 返回一个在生效内容变化后关闭的通道。
// 每次变化都会换上新通道，调用方在下一次等待前必须重新获取。
func (s *State) Changed() <-chan struct{} {
	s.changedMu.Lock()
	defer s.changedMu.Unlock()
	return s.changed
}

// EnsureFresh 在配置文件或索引文件的指纹变化时重新加载。
//
// 文件没变时只花两次 stat，因此可以放在每个受保护请求的入口。
func (s *State) EnsureFresh() {
	current := s.snapshot.Load()
	if current.configStamp == readStamp(s.configPath) && current.indexStamp == readStamp(s.indexPath) {
		return
	}
	s.refresh(false)
}

// Reload 立即按磁盘内容重载配置与索引，返回本次结果。
//
// 与 EnsureFresh 不同，它不看指纹：显式重载就是要重新解析一遍。
func (s *State) Reload() Result {
	return s.refresh(true)
}

// refresh 是唯一的加载入口：按需重读配置与索引，构建新快照后整体替换。
//
// force 为 true 时无条件重读两个文件；否则只重读指纹变了的那个。
func (s *State) refresh(force bool) Result {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()

	previous := s.snapshot.Load()
	configStamp := readStamp(s.configPath)
	indexStamp := readStamp(s.indexPath)
	reloadConfig := force || configStamp != previous.configStamp
	reloadIndex := force || indexStamp != previous.indexStamp
	if !reloadConfig && !reloadIndex {
		// 抢占到锁时别的协程已经加载过同一份变化，直接沿用结果。
		return previous.LastReload
	}

	next := &Snapshot{
		Config:         previous.Config,
		Repositories:   previous.Repositories,
		Vault:          previous.Vault,
		RepositoryDir:  previous.RepositoryDir,
		IndexPath:      previous.IndexPath,
		ConfigLoadedAt: previous.ConfigLoadedAt,
		IndexLoadedAt:  previous.IndexLoadedAt,
		configStamp:    previous.configStamp,
		indexStamp:     previous.indexStamp,
		configInSync:   previous.configInSync,
		indexInSync:    previous.indexInSync,
	}

	result := Result{At: time.Now()}
	failures := make([]string, 0, 2)

	if reloadConfig {
		// 失败也记下这次看到的指纹：同一份坏文件不反复报错，改好后再重试。
		next.configStamp = configStamp
		cfg, err := s.loadConfig()
		next.configInSync = err == nil
		if err != nil {
			failures = append(failures, fmt.Sprintf("配置加载失败（%s）: %v", s.configPath, err))
		} else {
			next.Config = cfg
			next.ConfigLoadedAt = result.At
		}
	}

	if reloadIndex {
		next.indexStamp = indexStamp
		index, err := repoindex.Load(s.indexPath)
		if err == nil {
			view, viewErr := newVault(s.repositoryDir, index.Repositories)
			if viewErr != nil {
				err = viewErr
			} else {
				next.Repositories = normalizeRepositories(index.Repositories)
				next.Vault = view
				next.IndexLoadedAt = result.At
			}
		}
		next.indexInSync = err == nil
		if err != nil {
			failures = append(failures, fmt.Sprintf("索引加载失败（%s）: %v", s.indexPath, err))
		}
	}

	result.Error = strings.Join(failures, "; ")
	s.settle(next, result)
	s.snapshot.Store(next)
	// 结果只有快照里这一份：日志、/status 与接口返回值都读它，不会各拿一份。
	s.report(previous, next)

	// 只有生效内容真的变了才唤醒订阅者：改一次没变化的文件不该打断同步节奏。
	if !sameConfig(previous.Config, next.Config) || !slices.Equal(previous.Repositories, next.Repositories) {
		s.notifyChanged()
	}
	return next.LastReload
}

// report 把这次重载写进日志。
//
// 只在真的重读了文件时调用，因此不会随请求刷屏；需要重启才能生效的
// 字段变化会单独留一条记录，避免「改了没反应」变成沉默的意外。
func (s *State) report(previous *Snapshot, next *Snapshot) {
	result := next.LastReload
	if result.Error != "" {
		log.Printf("reload failed: %s", result.Error)
	} else {
		log.Printf("reload applied: config=%t index=%t repositories=%d",
			result.ConfigReloaded, result.IndexReloaded, result.Repositories)
	}

	if len(next.NeedsRestart) > 0 && !slices.Equal(previous.NeedsRestart, next.NeedsRestart) {
		log.Printf("restart required for %s: these fields are fixed at startup",
			strings.Join(next.NeedsRestart, ", "))
	}
}

// settle 补全派生字段：仓库数、需要重启的字段与最近一次重载结果。
func (s *State) settle(snapshot *Snapshot, result Result) {
	snapshot.NeedsRestart = startupChanges(s.startup, snapshot.Config)
	result.ConfigReloaded = snapshot.configInSync
	result.IndexReloaded = snapshot.indexInSync
	result.Repositories = len(snapshot.Repositories)
	result.NeedsRestart = append([]string{}, snapshot.NeedsRestart...)
	snapshot.LastReload = result
}

// notifyChanged 唤醒所有等待配置变化的订阅者（例如同步循环）。
func (s *State) notifyChanged() {
	s.changedMu.Lock()
	defer s.changedMu.Unlock()
	close(s.changed)
	s.changed = make(chan struct{})
}

// loadConfig 读取配置，并套用启动期定下的门牌号与口令。
//
// 首次加载与每一次重载都走这里，因此画像的权威不会被一次重载换掉。
func (s *State) loadConfig() (*config.Config, error) {
	cfg, err := config.Load(s.configPath)
	if err != nil {
		return nil, err
	}
	if err := adopt(cfg, s.profile, s.httpAddress); err != nil {
		return nil, err
	}
	return cfg, nil
}

// adopt 把启动期定下的门牌号与口令落到配置上。
//
// 顺序即优先级：画像接管端口与钥匙，命令行给出的监听地址再覆盖一次。
// 配置文件里的 httpAddress 只剩监听宿主与「首次生成画像」的端口初值。
func adopt(cfg *config.Config, profile serviceprofile.Profile, httpAddress string) error {
	if err := cfg.AdoptProfile(profile.Port, profile.Key); err != nil {
		return err
	}
	if httpAddress == "" {
		return nil
	}
	return cfg.SetHTTPAddress(httpAddress)
}

// newVault 按仓库清单建立只读视图。
func newVault(repositoryDir string, repositories []repoindex.Repository) (*vault.Vault, error) {
	names := make([]string, len(repositories))
	for index, repository := range repositories {
		names[index] = repository.Name
	}
	return vault.New(repositoryDir, names, vault.DefaultLimits())
}

// normalizeRepositories 让空清单也是可用作比较的空切片，而不是 nil。
func normalizeRepositories(repositories []repoindex.Repository) []repoindex.Repository {
	if repositories == nil {
		return []repoindex.Repository{}
	}
	return repositories
}

// sameConfig 判断两份配置是否等价。
func sameConfig(left *config.Config, right *config.Config) bool {
	return reflect.DeepEqual(left, right)
}
