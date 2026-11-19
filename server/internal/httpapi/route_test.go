package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// existingEndpoints 是拆分前就对外提供的 16 个接口：地址、方法与这次改动前一字不差。
var existingEndpoints = []struct {
	method string
	target string
}{
	{http.MethodGet, "/healthz"},
	{http.MethodPost, "/v1/reload"},
	{http.MethodGet, "/status"},
	{http.MethodGet, "/v1/repos"},
	{http.MethodGet, "/v1/repos/example"},
	{http.MethodGet, "/v1/repos/example/tree"},
	{http.MethodGet, "/v1/repos/example/file?path=docs/guide.md"},
	{http.MethodGet, "/v1/repos/example/search?q=line"},
	{http.MethodGet, "/v1/repos/example/find?name=guide.md"},
	{http.MethodGet, "/v1/repos/example/archive"},
	{http.MethodGet, "/v1/repos/example/bundle"},
	{http.MethodGet, "/v1/archive?include=example"},
	{http.MethodPost, "/v1/repos/example/sync"},
	{http.MethodPost, "/v1/sync"},
	{http.MethodPost, "/v1/repos/example/check"},
	{http.MethodGet, "/v1/state"},
}

// managedEndpoints 是这次新增的 6 个管理接口。
var managedEndpoints = []struct {
	method string
	target string
}{
	{http.MethodPost, "/v1/repos"},
	{http.MethodDelete, "/v1/repos/example"},
	{http.MethodPost, "/v1/repos/example/unblock"},
	{http.MethodPost, "/v1/repos/example/compact"},
	{http.MethodGet, "/v1/cache"},
	{http.MethodDelete, "/v1/cache"},
}

// 全部接口都还在，而且除健康检查外一律要钥匙：漏了鉴权或路线掉了都会露出来。
func TestRouterKeepsEveryEndpointAndAuth(t *testing.T) {
	t.Parallel()

	for _, endpoint := range append(append([]struct {
		method string
		target string
	}{}, existingEndpoints...), managedEndpoints...) {
		endpoint := endpoint
		t.Run(endpoint.method+" "+endpoint.target, func(t *testing.T) {
			t.Parallel()

			environment := newEnv(t, envOptions{Sync: &stubSync{}, Remote: &stubRemote{}})

			anonymous := httptest.NewRecorder()
			environment.router.ServeHTTP(anonymous, httptest.NewRequest(endpoint.method, endpoint.target, nil))
			if anonymous.Code == http.StatusNotFound || anonymous.Code == http.StatusMethodNotAllowed {
				t.Fatalf("接口不存在: status = %d", anonymous.Code)
			}
			if endpoint.target == "/healthz" {
				if anonymous.Code != http.StatusOK {
					t.Fatalf("健康检查应保持匿名: status = %d", anonymous.Code)
				}
				return
			}
			if anonymous.Code != http.StatusUnauthorized {
				t.Fatalf("缺少钥匙时应 401: status = %d", anonymous.Code)
			}
		})
	}
}

// 每个原有接口仍然能被真实请求打到，并且没有被改成「找不到」或「方法不允许」。
func TestRouterKeepsExistingEndpointsWorking(t *testing.T) {
	t.Parallel()

	for _, endpoint := range existingEndpoints {
		endpoint := endpoint
		t.Run(endpoint.method+" "+endpoint.target, func(t *testing.T) {
			t.Parallel()

			environment := newEnv(t, envOptions{Sync: &stubSync{}, Remote: &stubRemote{}})

			recorder := environment.do(t, endpoint.method, endpoint.target, nil)
			if recorder.Code == http.StatusNotFound || recorder.Code == http.StatusMethodNotAllowed {
				t.Fatalf("接口不存在或方法不对: status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}
