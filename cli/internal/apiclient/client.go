// Package apiclient 封装对服务端对外接口的调用。
//
// 只依赖标准库：本模块不引用服务端模块的任何包，接口形状在这里按契约
// 独立声明一遍。返回的结构直接对应线上 JSON。
//
// 连接信息只有两样：地址与 key。地址里的协议照用户写的用（明文与加密都直接连），
// 这里不补前缀、不改写主机名——命令行不需要知道服务端跑在哪台机器上。
package apiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// APIError 表示服务端返回的错误。
type APIError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *APIError) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("HTTP %d %s: %s", e.StatusCode, e.Code, e.Message)
}

// 等待预算：一次调用最多等多久。
//
// 普通查询与管理动作都很快；同步与就地瘦身是分钟级操作，各自有独立的预算。
// 不能共用默认值——否则用户会在一次正常的转换还没跑完时被判成超时。
const (
	requestBudget = 30 * time.Second
	syncBudget    = 15 * time.Minute
	compactBudget = 15 * time.Minute
)

// requestClass 区分一类操作，它决定这次调用能用多少等待预算。
type requestClass int

const (
	// classRequest 是一问一答的调用：查询与管理动作都走它。
	classRequest requestClass = iota
	// classSync 是同步类调用：单个大仓库可能要跑几分钟。
	classSync
	// classCompact 是就地瘦身：分钟级，且与同步互斥。
	classCompact
)

// budget 是这一类操作允许的等待上限。
func (class requestClass) budget() time.Duration {
	switch class {
	case classSync:
		return syncBudget
	case classCompact:
		return compactBudget
	default:
		return requestBudget
	}
}

// Client 是对外接口客户端。
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
	// budgetOverride 非零时覆盖所有类的预算，来自 --timeout。
	budgetOverride time.Duration
}

// New 构建客户端，地址末尾的斜杠会被去掉。
func New(baseURL string, apiKey string) (*Client, error) {
	trimmed := strings.TrimSpace(baseURL)
	if trimmed == "" {
		return nil, fmt.Errorf("server is required")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("server must start with http:// or https://")
	}

	return &Client{
		baseURL: strings.TrimRight(trimmed, "/"),
		apiKey:  apiKey,
		http:    &http.Client{},
	}, nil
}

// WithTimeout 返回一份把所有请求的等待上限都改成 d 的副本。
//
// 用户显式给了 --timeout 就按用户给的来，按类预算让位。
func (c *Client) WithTimeout(d time.Duration) *Client {
	copied := *c
	copied.budgetOverride = d
	return &copied
}

// budget 返回这次请求允许的等待上限。
func (c *Client) budget(class requestClass) time.Duration {
	if c.budgetOverride > 0 {
		return c.budgetOverride
	}
	return class.budget()
}

// request 描述一次调用：方法、路径、查询串、请求体与等待预算。
type request struct {
	method string
	path   string
	query  url.Values
	body   any
	class  requestClass
}

// response 是已经发出、还没有读完的响应；读完必须 close。
type response struct {
	*http.Response
	cancel context.CancelFunc
}

func (r *response) close() {
	_ = r.Body.Close()
	r.cancel()
}

// open 发一次请求；状态码不在 accept 里时转成 APIError。
// 没有指定 accept 时只接受 200。
func (c *Client) open(ctx context.Context, spec request, accept ...int) (*response, error) {
	endpoint := c.baseURL + spec.path
	if len(spec.query) > 0 {
		endpoint += "?" + spec.query.Encode()
	}

	var payload io.Reader
	if spec.body != nil {
		encoded, err := json.Marshal(spec.body)
		if err != nil {
			return nil, err
		}
		payload = bytes.NewReader(encoded)
	}

	budgeted, cancel := context.WithTimeout(ctx, c.budget(spec.class))
	built, err := http.NewRequestWithContext(budgeted, spec.method, endpoint, payload)
	if err != nil {
		cancel()
		return nil, err
	}
	built.Header.Set("Authorization", "Bearer "+c.apiKey)
	built.Header.Set("Accept", "application/json")
	if spec.body != nil {
		built.Header.Set("Content-Type", "application/json")
	}

	sent, err := c.http.Do(built)
	if err != nil {
		cancel()
		return nil, err
	}
	if !accepts(sent.StatusCode, accept) {
		body, _ := io.ReadAll(sent.Body)
		_ = sent.Body.Close()
		cancel()
		return nil, decodeError(sent.StatusCode, body)
	}
	return &response{Response: sent, cancel: cancel}, nil
}

// accepts 判断状态码是否在允许之列；没有指定时只接受 200。
func accepts(statusCode int, allowed []int) bool {
	if len(allowed) == 0 {
		return statusCode == http.StatusOK
	}
	for _, candidate := range allowed {
		if statusCode == candidate {
			return true
		}
	}
	return false
}

// call 发一次请求并把 JSON 响应读进 out；out 为 nil 时只要状态码。
func (c *Client) call(ctx context.Context, spec request, out any, accept ...int) error {
	opened, err := c.open(ctx, spec, accept...)
	if err != nil {
		return err
	}
	defer opened.close()

	if out == nil {
		_, _ = io.Copy(io.Discard, opened.Body)
		return nil
	}

	body, err := io.ReadAll(opened.Body)
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return nil
	}
	return json.Unmarshal(body, out)
}

// stream 发一次请求并把响应正文原样写进 w，用于下载类接口。
func (c *Client) stream(ctx context.Context, spec request, w io.Writer) error {
	opened, err := c.open(ctx, spec)
	if err != nil {
		return err
	}
	defer opened.close()

	_, err = io.Copy(w, opened.Body)
	return err
}

// headerInt 读一个整数响应头；缺失或非法都按 0 处理。
func headerInt(response *http.Response, key string) int {
	value, err := strconv.Atoi(response.Header.Get(key))
	if err != nil {
		return 0
	}
	return value
}

// headerInt64 读一个 64 位整数响应头；缺失或非法都按 0 处理。
func headerInt64(response *http.Response, key string) int64 {
	value, err := strconv.ParseInt(response.Header.Get(key), 10, 64)
	if err != nil {
		return 0
	}
	return value
}

// decodeError 把失败响应转成 APIError。
func decodeError(statusCode int, body []byte) error {
	var payload struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return &APIError{StatusCode: statusCode, Message: strings.TrimSpace(string(body))}
	}
	return &APIError{StatusCode: statusCode, Code: payload.Code, Message: payload.Message}
}
