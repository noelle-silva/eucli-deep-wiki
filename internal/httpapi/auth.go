package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
)

const bearerPrefix = "Bearer "

// Authenticator 校验请求携带的 Bearer key。
// 只保存摘要，比较时按固定长度做常量时间比对，避免长度与内容泄漏。
type Authenticator struct {
	digests [][sha256.Size]byte
}

// NewAuthenticator 由配置里的多个 key 构建校验器。
func NewAuthenticator(keys []string) (*Authenticator, error) {
	if len(keys) == 0 {
		return nil, errors.New("at least one api key is required")
	}

	digests := make([][sha256.Size]byte, 0, len(keys))
	for _, key := range keys {
		digests = append(digests, sha256.Sum256([]byte(key)))
	}
	return &Authenticator{digests: digests}, nil
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
	for _, digest := range a.digests {
		matched |= subtle.ConstantTimeCompare(candidate[:], digest[:])
	}
	return matched == 1
}
