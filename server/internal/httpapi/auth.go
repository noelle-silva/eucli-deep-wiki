package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
)

const bearerPrefix = "Bearer "

// Authenticator 校验请求携带的 Bearer key。
//
// key 集合从可替换的状态里现取，因此改配置里的 apiKeys 不需要重启：
// 新增的 key 立刻可用，删掉的 key 立刻失效。
//
// 摘要按请求现算：几条短字符串的 SHA-256 是纳秒级开销，换来的是
// 没有缓存失效问题的一致性；比较仍然是常量时间。
type Authenticator struct {
	source KeySource
}

// NewAuthenticator 由可替换的 key 集合构建校验器。
func NewAuthenticator(source KeySource) *Authenticator {
	return &Authenticator{source: source}
}

// Wrap 返回受保护的处理器。
func (a *Authenticator) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.authorized(r) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="eucli-deep-wiki"`)
			writeJSON(w, http.StatusUnauthorized, apiError{
				Code:    "unauthorized",
				Message: "缺少或无效的 Authorization: Bearer <key>",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *Authenticator) authorized(r *http.Request) bool {
	header := r.Header.Get("Authorization")
	if len(header) <= len(bearerPrefix) || !strings.EqualFold(header[:len(bearerPrefix)], bearerPrefix) {
		return false
	}

	token := strings.TrimSpace(header[len(bearerPrefix):])
	if token == "" {
		return false
	}

	candidate := sha256.Sum256([]byte(token))
	matched := 0
	for _, key := range a.source.APIKeys() {
		digest := sha256.Sum256([]byte(key))
		matched |= subtle.ConstantTimeCompare(candidate[:], digest[:])
	}
	return matched == 1
}
