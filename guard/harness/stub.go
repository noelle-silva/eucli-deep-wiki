package harness

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
)

// Stub 是一个只回答固定正文的极小服务端。
//
// 它只用于一件事：把契约表里「服务端必然给出这个状态码与标识」的输入定向喂给
// 命令行，核对命令行是否把它映射成契约规定的退出码。它不是服务端的替身：
// 服务端自己能不能给出这些标识，由打真实路由的用例单独负责。
type Stub struct {
	server *httptest.Server
}

// NewStub 建立一个对任何请求都回答 status + payload 的服务端。
func NewStub(status int, payload any) *Stub {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(payload)
	})
	return &Stub{server: httptest.NewServer(handler)}
}

// URL 是这个服务端的地址。
func (stub *Stub) URL() string {
	return stub.server.URL
}

// Close 关掉它。
func (stub *Stub) Close() {
	stub.server.Close()
}
