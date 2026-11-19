package harness

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"sync"
	"time"
)

// Server 是一个真实的 eucli-deep-wiki-server 进程。
type Server struct {
	command *exec.Cmd
	url     string
	logs    *lockedBuffer
	stopOne sync.Once
	done    chan struct{}
	waitErr error
}

// lockedBuffer 收集进程输出，供失败时回显。
type lockedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (locked *lockedBuffer) Write(data []byte) (int, error) {
	locked.mu.Lock()
	defer locked.mu.Unlock()
	return locked.buffer.Write(data)
}

func (locked *lockedBuffer) String() string {
	locked.mu.Lock()
	defer locked.mu.Unlock()
	return locked.buffer.String()
}

// startServer 起一个真实服务端进程：真实入口、真实参数、真实监听。
func startServer(binary string, configPath string) (*Server, error) {
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	address := fmt.Sprintf("127.0.0.1:%d", port)

	logs := &lockedBuffer{}
	command := exec.Command(binary, "-config", configPath, "-http-address", address)
	command.Stdout = logs
	command.Stderr = logs
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("启动服务端: %w", err)
	}

	server := &Server{
		command: command,
		url:     "http://" + address,
		logs:    logs,
		done:    make(chan struct{}),
	}
	go func() {
		server.waitErr = command.Wait()
		close(server.done)
	}()

	if err := server.waitReady(15 * time.Second); err != nil {
		server.Stop()
		return nil, err
	}
	return server, nil
}

// waitReady 轮询健康检查，直到服务端能回答或超时。
func (server *Server) waitReady(within time.Duration) error {
	deadline := time.Now().Add(within)
	client := &http.Client{Timeout: time.Second}
	for time.Now().Before(deadline) {
		select {
		case <-server.done:
			return fmt.Errorf("服务端提前退出：%v\n%s", server.waitErr, server.logs.String())
		default:
		}

		response, err := client.Get(server.url + "/healthz")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("服务端在 %s 内没有就绪\n%s", within, server.logs.String())
}

// URL 是服务端自己的地址。
func (server *Server) URL() string {
	return server.url
}

// Logs 是服务端进程到目前为止的输出。
func (server *Server) Logs() string {
	return server.logs.String()
}

// Stop 结束这个进程（它是本进程拉起来的子进程，不是机器上已有的进程）。
func (server *Server) Stop() {
	server.stopOne.Do(func() {
		if server.command.Process != nil {
			_ = server.command.Process.Kill()
		}
		<-server.done
	})
}

// freePort 要一个空闲端口给这次验证用。
func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("申请空闲端口: %w", err)
	}
	defer func() { _ = listener.Close() }()
	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("申请空闲端口: 地址类型不支持")
	}
	return address.Port, nil
}
