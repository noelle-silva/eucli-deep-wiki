package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var suffixPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type serverConfig struct {
	DataRoot          string `json:"dataRoot"`
	RepositoryRoot    string `json:"repositoryRoot"`
	RepositoryIndex   string `json:"repositoryIndex"`
	SyncInterval      string `json:"syncInterval"`
	GitCommandTimeout string `json:"gitCommandTimeout"`
	MaxParallelSyncs  int    `json:"maxParallelSyncs"`
	HTTPAddress       string `json:"httpAddress"`
	DefaultHistory    string `json:"defaultHistory"`
	GCThresholdMB     int    `json:"gcThresholdMB"`
	GCGrowthMB        int    `json:"gcGrowthMB"`
	SizeLimitMB       int    `json:"sizeLimitMB"`
}

type serviceProfile struct {
	Port int    `json:"port"`
	Key  string `json:"key"`
}

func main() {
	if len(os.Args) < 2 {
		fail("缺少适配器子命令")
	}
	switch os.Args[1] {
	case "deps":
		must(cmdDeps())
		ok(nil)
	case "build":
		data, err := cmdBuild()
		must(err)
		ok(data)
	case "start":
		data, err := cmdStart()
		must(err)
		ok(data)
	case "health":
		must(cmdHealth())
		ok(nil)
	case "stop":
		must(cmdStop())
		ok(nil)
	case "seed":
		emit(map[string]any{"status": "not-applicable"}, 0)
	case "test":
		must(cmdTest())
		ok(nil)
	default:
		fail("未知适配器子命令: " + os.Args[1])
	}
}

func emit(payload map[string]any, code int) {
	_ = json.NewEncoder(os.Stdout).Encode(payload)
	os.Exit(code)
}

func ok(data any) {
	out := map[string]any{"status": "ok"}
	if data != nil {
		out["data"] = data
	}
	emit(out, 0)
}

func fail(reason string) {
	emit(map[string]any{"status": "failed", "reason": reason}, 1)
}

func must(err error) {
	if err != nil {
		fail(err.Error())
	}
}

func requireEnv(name string) (string, error) {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return "", fmt.Errorf("缺少必需环境变量 %s", name)
	}
	return v, nil
}

func requireSuffix() (string, error) {
	v, err := requireEnv("EUCLI_CF_INSTANCE_SUFFIX")
	if err != nil {
		return "", err
	}
	if !suffixPattern.MatchString(v) {
		return "", fmt.Errorf("实例身份后缀 %q 非法，拒绝启动", v)
	}
	return v, nil
}

func requirePort() (int, error) {
	raw, err := requireEnv("EUCLI_CF_PORT_BASE")
	if err != nil {
		return 0, err
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("端口基址 %q 非法", raw)
	}
	return port, nil
}

func goEnv(depsStore string, goworkOff bool) []string {
	env := append([]string(nil), os.Environ()...)
	if depsStore != "" {
		env = append(env,
			"GOMODCACHE="+filepath.Join(depsStore, "go", "pkg", "mod"),
			"GOCACHE="+filepath.Join(depsStore, "go", "build-cache"),
		)
	}
	if goworkOff {
		env = append(env, "GOWORK=off")
	}
	return env
}

func runGo(dir string, env []string, args ...string) error {
	fmt.Fprintf(os.Stderr, "[adapter] (%s) go %s\n", dir, strings.Join(args, " "))
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("在 %s 执行 go %s 失败: %w", dir, strings.Join(args, " "), err)
	}
	return nil
}

func exeName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

func cmdDeps() error {
	worktree, err := requireEnv("EUCLI_CF_WORKTREE")
	if err != nil {
		return err
	}
	depsDir, err := requireEnv("EUCLI_CF_DEPS_DIR")
	if err != nil {
		return err
	}
	depsStore, err := requireEnv("EUCLI_CF_DEPS_STORE")
	if err != nil {
		return err
	}
	for _, dir := range []string{
		depsDir,
		filepath.Join(depsStore, "go", "pkg", "mod"),
		filepath.Join(depsStore, "go", "build-cache"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("创建依赖目录 %s 失败: %w", dir, err)
		}
	}
	env := goEnv(depsStore, true)
	for _, mod := range []string{"server", "cli", "guard", filepath.Join("tools", "pack"), filepath.Join("tools", "version")} {
		if err := runGo(filepath.Join(worktree, mod), env, "mod", "download"); err != nil {
			return err
		}
	}
	return nil
}

func cmdBuild() (map[string]any, error) {
	worktree, err := requireEnv("EUCLI_CF_WORKTREE")
	if err != nil {
		return nil, err
	}
	buildDir, err := requireEnv("EUCLI_CF_BUILD_DIR")
	if err != nil {
		return nil, err
	}
	depsStore, err := requireEnv("EUCLI_CF_DEPS_STORE")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建构建目录失败: %w", err)
	}

	env := append(goEnv(depsStore, true), "CGO_ENABLED=0")
	serverOut := filepath.Join(buildDir, exeName("eucli-deep-wiki-server"))
	cliOut := filepath.Join(buildDir, exeName("eucli-dw"))

	if err := runGo(filepath.Join(worktree, "server"), env,
		"build", "-trimpath", "-ldflags=-s -w", "-o", serverOut, "./cmd/eucli-deep-wiki-server"); err != nil {
		return nil, err
	}
	if err := runGo(filepath.Join(worktree, "cli"), env,
		"build", "-trimpath", "-ldflags=-s -w", "-o", cliOut, "./cmd/eucli-dw"); err != nil {
		return nil, err
	}
	return map[string]any{
		"server": serverOut,
		"cli":    cliOut,
	}, nil
}

func pidFile(dataRoot string) string {
	return filepath.Join(dataRoot, "server.pid")
}

func cmdStart() (map[string]any, error) {
	suffix, err := requireSuffix()
	if err != nil {
		return nil, err
	}
	port, err := requirePort()
	if err != nil {
		return nil, err
	}
	buildDir, err := requireEnv("EUCLI_CF_BUILD_DIR")
	if err != nil {
		return nil, err
	}
	dataRoot, err := requireEnv("EUCLI_CF_DATA_ROOT")
	if err != nil {
		return nil, err
	}
	serverBin := filepath.Join(buildDir, exeName("eucli-deep-wiki-server"))
	if _, err := os.Stat(serverBin); err != nil {
		return nil, fmt.Errorf("正式构建产物不存在 (%s): %w", serverBin, err)
	}

	instanceDataDir := filepath.Join(dataRoot, "data")
	if err := os.MkdirAll(instanceDataDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建实例数据目录失败: %w", err)
	}

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	cfg := serverConfig{
		DataRoot:          instanceDataDir,
		RepositoryRoot:    "repository",
		RepositoryIndex:   "repositories.json",
		SyncInterval:      "6h",
		GitCommandTimeout: "5m",
		MaxParallelSyncs:  1,
		HTTPAddress:       addr,
		DefaultHistory:    "shallow",
		GCThresholdMB:     2048,
		GCGrowthMB:        300,
		SizeLimitMB:       5120,
	}
	cfgBytes, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("序列化实例配置失败: %w", err)
	}
	cfgPath := filepath.Join(dataRoot, "config.json")
	if err := os.WriteFile(cfgPath, append(cfgBytes, '\n'), 0o644); err != nil {
		return nil, fmt.Errorf("写入实例配置失败: %w", err)
	}

	logPath := filepath.Join(dataRoot, "server.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("打开实例日志失败: %w", err)
	}
	defer logFile.Close()

	cmd := exec.Command(serverBin, "-config", cfgPath, "-http-address", addr)
	cmd.Dir = dataRoot
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动服务端失败: %w", err)
	}

	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	if err := os.WriteFile(pidFile(dataRoot), []byte(strconv.Itoa(pid)), 0o644); err != nil {
		return nil, fmt.Errorf("写入 PID 文件失败: %w", err)
	}
	fmt.Fprintf(os.Stderr, "[adapter] started eucli-deep-wiki (%s) pid=%d addr=%s\n", suffix, pid, addr)
	return map[string]any{
		"pid":    pid,
		"port":   port,
		"suffix": suffix,
		"config": cfgPath,
		"log":    logPath,
	}, nil
}

func probeHealthz(port int) error {
	client := http.Client{Timeout: time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", port))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("/healthz 返回状态码 %d", resp.StatusCode)
	}
	return nil
}

func cmdHealth() error {
	port, err := requirePort()
	if err != nil {
		return err
	}
	dataRoot, err := requireEnv("EUCLI_CF_DATA_ROOT")
	if err != nil {
		return err
	}
	if err := probeHealthz(port); err != nil {
		return fmt.Errorf("服务尚未就绪: %w", err)
	}
	profilePath := filepath.Join(dataRoot, "data", "service-profile.json")
	if _, err := os.Stat(profilePath); err != nil {
		return fmt.Errorf("服务画像尚未生成: %w", err)
	}
	return nil
}

func cmdStop() error {
	port, err := requirePort()
	if err != nil {
		return err
	}
	dataRoot, err := requireEnv("EUCLI_CF_DATA_ROOT")
	if err != nil {
		return err
	}
	pFile := pidFile(dataRoot)
	raw, err := os.ReadFile(pFile)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取 PID 文件失败: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		_ = os.Remove(pFile)
		return nil
	}
	if proc, findErr := os.FindProcess(pid); findErr == nil {
		_ = proc.Kill()
	}
	if runtime.GOOS == "windows" {
		_ = exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid)).Run()
	}
	for i := 0; i < 50; i++ {
		if probeHealthz(port) != nil {
			_ = os.Remove(pFile)
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("实例 pid=%d 在超时内未停止（端口 %d 仍有响应）", pid, port)
}

func cmdTest() error {
	worktree, err := requireEnv("EUCLI_CF_WORKTREE")
	if err != nil {
		return err
	}
	buildDir, err := requireEnv("EUCLI_CF_BUILD_DIR")
	if err != nil {
		return err
	}
	dataRoot, err := requireEnv("EUCLI_CF_DATA_ROOT")
	if err != nil {
		return err
	}
	depsStore, err := requireEnv("EUCLI_CF_DEPS_STORE")
	if err != nil {
		return err
	}
	port, err := requirePort()
	if err != nil {
		return err
	}

	// 1. 针对运行中的正式实例执行服务画像 + HTTP + CLI 联调验证
	profilePath := filepath.Join(dataRoot, "data", "service-profile.json")
	profileBytes, err := os.ReadFile(profilePath)
	if err != nil {
		return fmt.Errorf("读取实例服务画像失败: %w", err)
	}
	var prof serviceProfile
	if err := json.Unmarshal(profileBytes, &prof); err != nil {
		return fmt.Errorf("解析实例服务画像失败: %w", err)
	}
	if strings.TrimSpace(prof.Key) == "" {
		return errors.New("实例服务画像中的 key 为空")
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	req, err := http.NewRequest(http.MethodGet, baseURL+"/status", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+prof.Key)
	client := http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("访问运行中实例 /status 失败: %w", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("运行中实例 /status 返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	cliBin := filepath.Join(buildDir, exeName("eucli-dw"))
	cliCfg := filepath.Join(dataRoot, "test-cli-config.json")
	for _, args := range [][]string{
		{"status", "--config", cliCfg, "--server", baseURL, "--key", prof.Key, "--json"},
		{"repo", "list", "--config", cliCfg, "--server", baseURL, "--key", prof.Key, "--json"},
	} {
		fmt.Fprintf(os.Stderr, "[adapter] smoke: eucli-dw %s\n", strings.Join(args[:2], " "))
		cmd := exec.Command(cliBin, args...)
		cmd.Dir = dataRoot
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("命令行联调失败 (%s): %w", strings.Join(args[:2], " "), err)
		}
	}

	// 2. 运行本仓库全部正式回归测试套件（server / cli / guard / tools）
	isolatedEnv := goEnv(depsStore, true)
	for _, mod := range []string{"server", "cli", filepath.Join("tools", "pack"), filepath.Join("tools", "version")} {
		if err := runGo(filepath.Join(worktree, mod), isolatedEnv, "test", "./..."); err != nil {
			return err
		}
	}
	// guard 依赖工作区定位与独立构建检查，按其自身约定运行
	if err := runGo(filepath.Join(worktree, "guard"), goEnv(depsStore, false), "test", "./..."); err != nil {
		return err
	}
	return nil
}
