package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"eucli-deep-wiki-contract-guard/contract"
)

// 标准现场的内容全部是确定的字面量：用例按行号、按字节、按条数断言，
// 因此现场内容本身也是契约的一部分，改这里等于改用例的期望值。
const (
	// ReadmeContent 是 alpha 的 README：第 4 行与第 7 行各有一处 needle。
	ReadmeContent = `# alpha 示例仓库
第二行：这一行是普通内容
第三行
第四行 needle 出现在这里
第五行
第六行
第七行 needle 第二次出现
第八行
`
	// GuideContent 是 alpha 的文档。
	GuideContent = "# 指南\n\n这里是 alpha 的文档。\n"
	// NoteContent 是深层目录里的文件。
	NoteContent = "深层文件\n"
	// MainContent 是 alpha 的源码文件。
	MainContent = "package main\n\nfunc main() {}\n"
	// GammaContent 是 gamma 的源码文件。
	GammaContent = "gamma\n"

	// AlphaMainRevision 是 alpha 的提交号：用例按它核对分支与提交的回显。
	AlphaMainRevision = "3f786850e387550fdab836ed7e6dc881de23001b"
	// GammaMainRevision 是 gamma 的提交号。
	GammaMainRevision = "9c1185a5c5e9fc54612808977ee8f548b2258d31"
	// ReadmeLines 是 alpha README 的行数。
	ReadmeLines = 8
	// AlphaTextFiles 是标准现场里 alpha 可参与检索的文本文件数。
	AlphaTextFiles = 6
	alphaReadme    = "README.md"
	alphaGuide     = "docs/guide.md"
	alphaNote      = "docs/deep/nested/note.txt"
	alphaMain      = "src/main.go"
	// alphaDependency 是「依赖目录」里的文件：目录名在排除名单里，内部还有一层。
	alphaDependency = "node_modules/left-pad/index.js"
	gammaSource     = "src/gamma.txt"
	// ExcludedDirName 是标准现场里那个「按名字排除」的目录名：用例拿它当参数。
	ExcludedDirName = "node_modules"
	// SameNameFilePath 是与该目录同名、但不在同一层的普通文件：排除只作用于目录。
	SameNameFilePath = "src/node_modules"
	// ContextProbe 是「上下文行数上限」用例要用的文件：命中在中间，两侧各 14 行。
	ContextProbe  = "context/around.md"
	IndexFilePerm = 0o644
	DirectoryPerm = 0o755
	// seedHTTPAddress 只用于配置校验：真实监听地址由 -http-address 给出。
	seedHTTPAddress = "127.0.0.1:19997"
)

// AlphaTreePaths 是标准现场 alpha 在 depth 3 时的完整骨架，按展开顺序排列。
//
// 它是「不传 exclude 时目录接口行为不变」的比对基准：现场里有哪些条目、以什么
// 顺序展开，都钉在这一份里，因此改动前后多一项、少一项、换一处顺序都会露出来。
var AlphaTreePaths = []string{
	"docs", "docs/deep", "docs/deep/nested", "docs/guide.md",
	"node_modules", "node_modules/left-pad", "node_modules/left-pad/index.js",
	"src", "src/main.go", "src/node_modules",
	"README.md",
}

// seed 按现场种类铺磁盘。
func (world *World) seed() error {
	if err := os.MkdirAll(world.RepoDir, DirectoryPerm); err != nil {
		return fmt.Errorf("建立仓库根: %w", err)
	}
	if err := os.MkdirAll(world.CacheDir, DirectoryPerm); err != nil {
		return fmt.Errorf("建立缓存目录: %w", err)
	}
	if err := os.MkdirAll(world.DownloadDir, DirectoryPerm); err != nil {
		return fmt.Errorf("建立下载目录: %w", err)
	}
	if err := os.MkdirAll(world.DataRoot, DirectoryPerm); err != nil {
		return fmt.Errorf("建立数据根: %w", err)
	}

	switch world.Kind {
	case KindEmpty:
		return world.seedEmpty()
	case KindBulk:
		return world.seedBulk()
	case KindGit:
		return world.seedGit()
	case KindGitBusy:
		return world.seedGitBusy()
	default:
		return world.seedStandard()
	}
}

// seedStandard 是标准现场：三种仓库角色 + 缓存产物 + 同步状态。
func (world *World) seedStandard() error {
	if err := world.writeConfig("30m"); err != nil {
		return err
	}
	if err := world.planIndex([]repositorySeed{
		{Name: contract.RepoOnDisk, URL: "https://example.com/alpha.git", Branch: "main", History: contract.HistoryShallow},
		{Name: contract.RepoDeclaredOnly, URL: "https://example.com/beta.git"},
		{Name: contract.RepoFullHistory, URL: "https://example.com/gamma.git", History: contract.HistoryFull},
	}); err != nil {
		return err
	}
	if err := world.writeState(); err != nil {
		return err
	}
	if err := world.writeCache(); err != nil {
		return err
	}
	if err := world.writeAlpha(); err != nil {
		return err
	}
	return world.writeGamma()
}

// seedEmpty 是空现场：名单、缓存、状态都为空。
func (world *World) seedEmpty() error {
	if err := world.writeConfig("30m"); err != nil {
		return err
	}
	return world.planIndex(nil)
}

// seedBulk 在标准现场上再铺几百个文件与足够多的命中。
func (world *World) seedBulk() error {
	if err := world.seedStandard(); err != nil {
		return err
	}

	for index := 0; index < 220; index++ {
		path := filepath.Join(world.RepoDir, contract.RepoOnDisk, "bulk", fmt.Sprintf("file-%03d.txt", index))
		if err := writeFile(path, "bulk\n"); err != nil {
			return err
		}
	}
	for index := 0; index < 150; index++ {
		path := filepath.Join(world.RepoDir, contract.RepoOnDisk, "matches", fmt.Sprintf("line-%03d.txt", index))
		if err := writeFile(path, "needle\n"); err != nil {
			return err
		}
	}

	if err := writeFile(filepath.Join(world.RepoDir, contract.RepoOnDisk, ContextProbe), contextProbeContent()); err != nil {
		return err
	}

	long := make([]string, 0, 30)
	for line := 1; line <= 30; line++ {
		if line == 15 {
			long = append(long, "needle 出现在中间")
			continue
		}
		long = append(long, fmt.Sprintf("第 %d 行", line))
	}
	return writeFile(filepath.Join(world.RepoDir, contract.RepoOnDisk, "long.md"), strings.Join(long, "\n")+"\n")
}

// repositorySeed 是名单里的一条。
type repositorySeed struct {
	Name    string
	URL     string
	Branch  string
	History string
}

// planIndex 记下这一轮要生效的名单，并把名单文件先写成空清单。
//
// 为什么不一上来就写全量：服务端启动后会先跑一轮同步，那一轮会去访问远端、
// 会动仓库目录、会写同步状态。验证要的是「名单已经生效、但没有同步在跑」，
// 因此名单在服务端起好之后再经 /v1/reload 生效，现场数据不会被启动那轮改动。
func (world *World) planIndex(repositories []repositorySeed) error {
	world.pendingIndex = repositories
	return world.writeIndexFile(nil)
}

// ApplyIndex 让记下的名单在服务端生效。
func (world *World) ApplyIndex() error {
	if len(world.pendingIndex) == 0 {
		return nil
	}
	if err := world.writeIndexFile(world.pendingIndex); err != nil {
		return err
	}
	message := world.Post("/v1/reload", nil)
	if message.Status != 200 {
		return fmt.Errorf("让名单生效失败：%s", message.Describe())
	}
	if reloaded, ok := message.BoolField("indexReloaded"); !ok || !reloaded {
		return fmt.Errorf("让名单生效失败：%s", message.Describe())
	}
	return nil
}

// writeIndexFile 写名单文件。
func (world *World) writeIndexFile(repositories []repositorySeed) error {
	entries := make([]string, 0, len(repositories))
	for _, repository := range repositories {
		fields := []string{fmt.Sprintf("    \"name\": %q", repository.Name), fmt.Sprintf("    \"url\": %q", repository.URL)}
		if repository.Branch != "" {
			fields = append(fields, fmt.Sprintf("    \"branch\": %q", repository.Branch))
		}
		if repository.History != "" {
			fields = append(fields, fmt.Sprintf("    \"history\": %q", repository.History))
		}
		entries = append(entries, "  {\n"+strings.Join(fields, ",\n")+"\n  }")
	}

	content := "{\n  \"repositories\": [\n" + strings.Join(entries, ",\n") + "\n  ]\n}\n"
	if len(entries) == 0 {
		content = "{\n  \"repositories\": []\n}\n"
	}
	return writeFile(world.IndexPath, content)
}

// writeState 写同步状态：alpha 被体积保护拦住，gamma 只留同步成功的事实。
func (world *World) writeState() error {
	content := `{
  "repositories": {
    "alpha": {
      "lastSyncAt": "2024-05-01T10:00:00Z",
      "gitBytes": 1234,
      "blocked": true,
      "blockedAt": "2024-05-01T10:05:00Z",
      "blockedSize": 4096,
      "consecutiveFailures": 3,
      "lastError": "远端超时"
    },
    "gamma": {
      "lastSyncAt": "2024-06-01T08:00:00Z",
      "gitBytes": 5678
    }
  }
}
`
	return writeFile(world.StatePath, content)
}

// writeCache 写缓存目录：两份真产物、一份非产物文件、一个子目录。
func (world *World) writeCache() error {
	artifacts := map[string]string{
		"alpha-" + AlphaMainRevision + contract.CacheArtifactSuffix: "alpha-bundle",
		"gamma-" + AlphaMainRevision + contract.CacheArtifactSuffix: "gamma-bundle-content",
		"beta-" + AlphaMainRevision + contract.CacheArtifactSuffix:  "beta-bundle",
		"notes.txt": "不是缓存产物",
		"alpha-" + AlphaMainRevision + contract.CacheArtifactSuffix + ".tmp": "半成品",
	}
	for name, content := range artifacts {
		if err := writeFile(filepath.Join(world.CacheDir, name), content); err != nil {
			return err
		}
	}
	return os.MkdirAll(filepath.Join(world.CacheDir, "sub"), DirectoryPerm)
}

// writeAlpha 写 alpha：一个真实目录 + 一个看起来像仓库的 .git。
func (world *World) writeAlpha() error {
	dir := filepath.Join(world.RepoDir, contract.RepoOnDisk)
	files := map[string]string{
		alphaReadme: ReadmeContent,
		alphaGuide:  GuideContent,
		alphaNote:   NoteContent,
		alphaMain:   MainContent,
		// 依赖目录：名字在排除清单里，内部还有一层，用来验证「不返回也不进入遍历」。
		alphaDependency: "module.exports = {};\n",
		// 与依赖目录同名、但不在同一层的普通文件：排除只作用于目录。
		SameNameFilePath: "不是一个目录\n",
		".git/HEAD":      "ref: refs/heads/main\n",
	}
	files[".git/refs/heads/main"] = AlphaMainRevision + "\n"
	files[".git/shallow"] = AlphaMainRevision + "\n"
	files[".git/pack/meta"] = "0123456789"
	for rel, content := range files {
		if err := writeFile(filepath.Join(dir, rel), content); err != nil {
			return err
		}
	}
	return nil
}

// contextProbeContent 是一份「命中在正中间、两侧各 14 行」的文件。
func contextProbeContent() string {
	lines := make([]string, 0, 29)
	for number := 1; number <= 29; number++ {
		if number == 15 {
			lines = append(lines, "needle 在正中间")
			continue
		}
		lines = append(lines, fmt.Sprintf("第 %d 行", number))
	}
	return strings.Join(lines, "\n") + "\n"
}

// writeGamma 写 gamma：完整历史（没有 .git/shallow），分支 dev。
func (world *World) writeGamma() error {
	dir := filepath.Join(world.RepoDir, contract.RepoFullHistory)
	files := map[string]string{
		gammaSource:           GammaContent,
		".git/HEAD":           "ref: refs/heads/dev\n",
		".git/refs/heads/dev": "9c1185a5c5e9fc54612808977ee8f548b2258d31\n",
		".git/pack/meta":      "0123456789abcdef",
	}
	for rel, content := range files {
		if err := writeFile(filepath.Join(dir, rel), content); err != nil {
			return err
		}
	}
	return nil
}

// writeConfig 写服务端配置。dataRoot 用绝对路径，验证现场不落进工作区。
func (world *World) writeConfig(gitTimeout string) error {
	content := fmt.Sprintf(`{
  "dataRoot": %q,
  "repositoryRoot": %q,
  "repositoryIndex": %q,
  "syncInterval": "6h",
  "gitCommandTimeout": %q,
  "maxParallelSyncs": 1,
  "httpAddress": %q,
  "apiKeys": [%q],
  "defaultHistory": "shallow",
  "gcThresholdMB": 2048,
  "gcGrowthMB": 300,
  "sizeLimitMB": 5120
}
`, world.DataRoot, repositoryName, indexFileName, gitTimeout, seedHTTPAddress, APIKey)
	return writeFile(world.ConfigPath, content)
}

// writeFile 建目录并写文件。
func writeFile(path string, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), DirectoryPerm); err != nil {
		return fmt.Errorf("建立目录 %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), IndexFilePerm); err != nil {
		return fmt.Errorf("写文件 %s: %w", path, err)
	}
	return nil
}
