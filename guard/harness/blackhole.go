package harness

import (
	"net"
	"sync"
)

// BlackHole 是一个「接了电话不说话」的 TCP 服务。
//
// 它只用于一件确定的事：让一次真实版本管理操作稳稳地卡在网络握手上，
// 从而把服务端的破坏性操作闸门占住。它自己不引入任何等待时长——什么时候放行
// 由调用方决定，Drop 之后那条命令立刻失败退出，不会留下悬着的子进程。
type BlackHole struct {
	listener net.Listener
	mu       sync.Mutex
	held     []net.Conn
	closed   bool
}

// NewBlackHole 在本机随机端口上开一个黑障。
func NewBlackHole() (*BlackHole, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	hole := &BlackHole{listener: listener}
	go hole.accept()
	return hole, nil
}

// accept 收下连接并一直握着，不回任何字节。
func (hole *BlackHole) accept() {
	for {
		connection, err := hole.listener.Accept()
		if err != nil {
			return
		}
		hole.mu.Lock()
		if hole.closed {
			hole.mu.Unlock()
			_ = connection.Close()
			return
		}
		hole.held = append(hole.held, connection)
		hole.mu.Unlock()
	}
}

// Address 是这个黑障的 host:port。
func (hole *BlackHole) Address() string {
	return hole.listener.Addr().String()
}

// HTTPSURL 是给仓库地址用的加密传输写法，末段就是仓库名。
func (hole *BlackHole) HTTPSURL(repository string) string {
	return "https://" + hole.Address() + "/" + repository + ".git"
}

// Drop 断掉所有被握住的连接，让卡在握手上的那条命令立刻失败。
func (hole *BlackHole) Drop() {
	hole.mu.Lock()
	held := hole.held
	hole.held = nil
	hole.closed = true
	hole.mu.Unlock()

	for _, connection := range held {
		_ = connection.Close()
	}
	_ = hole.listener.Close()
}
