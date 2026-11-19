package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Call 是一次对服务端的原始请求。
type Call struct {
	Method string
	Path   string
	Query  url.Values
	// Body 是原始正文；为空表示没有正文。
	Body []byte
	// ContentType 是正文类型；有正文且未指定时按 JSON 处理。
	ContentType string
	// Anonymous 为真时不带 Authorization 头。
	Anonymous bool
	// Headers 是额外的请求头。
	Headers map[string]string
}

// Message 是一次响应：状态码、头、原始正文与解析后的 JSON。
type Message struct {
	Status int
	Header http.Header
	Body   []byte
	JSON   map[string]any
}

var probeClient = &http.Client{Timeout: 2 * time.Minute}

// Do 发一次请求并读完整份响应。
func (world *World) Do(call Call) Message {
	endpoint := world.URL() + call.Path
	if len(call.Query) > 0 {
		endpoint += "?" + call.Query.Encode()
	}

	var body io.Reader
	if len(call.Body) > 0 {
		body = bytes.NewReader(call.Body)
	}
	built, err := http.NewRequest(call.Method, endpoint, body)
	if err != nil {
		return Message{Status: 0, Body: []byte("构造请求失败: " + err.Error())}
	}
	if !call.Anonymous {
		built.Header.Set("Authorization", "Bearer "+APIKey)
	}
	for name, value := range call.Headers {
		built.Header.Set(name, value)
	}
	if len(call.Body) > 0 {
		contentType := call.ContentType
		if contentType == "" {
			contentType = "application/json"
		}
		built.Header.Set("Content-Type", contentType)
	}

	sent, err := probeClient.Do(built)
	if err != nil {
		return Message{Status: 0, Body: []byte("请求失败: " + err.Error())}
	}
	defer func() { _ = sent.Body.Close() }()

	raw, readErr := io.ReadAll(sent.Body)
	message := Message{Status: sent.StatusCode, Header: sent.Header, Body: raw}
	if readErr != nil {
		message.Body = []byte("读取响应失败: " + readErr.Error())
		return message
	}
	if strings.HasPrefix(sent.Header.Get("Content-Type"), "application/json") {
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err == nil {
			message.JSON = decoded
		}
	}
	return message
}

// Get 发一次带鉴权的 GET。
func (world *World) Get(path string, query url.Values) Message {
	return world.Do(Call{Method: http.MethodGet, Path: path, Query: query})
}

// GetAnonymous 发一次不带鉴权的 GET。
func (world *World) GetAnonymous(path string, query url.Values) Message {
	return world.Do(Call{Method: http.MethodGet, Path: path, Query: query, Anonymous: true})
}

// Post 发一次带鉴权、没有正文的 POST。
func (world *World) Post(path string, query url.Values) Message {
	return world.Do(Call{Method: http.MethodPost, Path: path, Query: query})
}

// PostJSON 发一次带 JSON 正文的 POST。
func (world *World) PostJSON(path string, query url.Values, payload any) Message {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return Message{Status: 0, Body: []byte("编码请求体失败: " + err.Error())}
	}
	return world.Do(Call{Method: http.MethodPost, Path: path, Query: query, Body: encoded})
}

// PostRaw 发一次给定原始正文的 POST，用于验证「请求体不是合法结构化数据」。
func (world *World) PostRaw(path string, query url.Values, raw string) Message {
	return world.Do(Call{
		Method: http.MethodPost, Path: path, Query: query,
		Body: []byte(raw), ContentType: "application/json",
	})
}

// Delete 发一次带鉴权的 DELETE。
func (world *World) Delete(path string, query url.Values) Message {
	return world.Do(Call{Method: http.MethodDelete, Path: path, Query: query})
}

// ContentType 是响应的正文类型。
func (m Message) ContentType() string {
	return m.Header.Get("Content-Type")
}

// Code 读响应体里的错误标识；没有时为「（无 code）」。
func (m Message) Code() string {
	if m.JSON == nil {
		return "（无 code）"
	}
	if code, ok := m.JSON["code"].(string); ok {
		return code
	}
	return "（无 code）"
}

// Field 取一个顶层字段。
func (m Message) Field(name string) (any, bool) {
	if m.JSON == nil {
		return nil, false
	}
	value, ok := m.JSON[name]
	return value, ok
}

// StringField 取一个字符串字段。
func (m Message) StringField(name string) (string, bool) {
	value, ok := m.Field(name)
	if !ok {
		return "", false
	}
	text, ok := value.(string)
	return text, ok
}

// BoolField 取一个布尔字段。
func (m Message) BoolField(name string) (bool, bool) {
	value, ok := m.Field(name)
	if !ok {
		return false, false
	}
	flag, ok := value.(bool)
	return flag, ok
}

// NumberField 取一个数字字段。
func (m Message) NumberField(name string) (float64, bool) {
	value, ok := m.Field(name)
	if !ok {
		return 0, false
	}
	number, ok := value.(float64)
	return number, ok
}

// ObjectField 取一个对象字段。
func (m Message) ObjectField(name string) (map[string]any, bool) {
	value, ok := m.Field(name)
	if !ok {
		return nil, false
	}
	object, ok := value.(map[string]any)
	return object, ok
}

// ArrayField 取一个数组字段。
func (m Message) ArrayField(name string) ([]any, bool) {
	value, ok := m.Field(name)
	if !ok {
		return nil, false
	}
	items, ok := value.([]any)
	return items, ok
}

// IsMuxDefault404 判断响应是不是「路由根本没注册」的默认 404。
//
// 业务上的「仓库不存在」是 JSON 且带 code；路由缺失是纯文本的 404 page not found。
// 两者混为一谈会让「接口还在不在」这件事失去判断依据。
func (m Message) IsMuxDefault404() bool {
	return m.Status == http.StatusNotFound &&
		strings.HasPrefix(m.ContentType(), "text/plain") &&
		strings.Contains(string(m.Body), "404 page not found")
}

// Describe 把响应压成一行，放进失败说明里。
func (m Message) Describe() string {
	body := strings.TrimSpace(string(m.Body))
	if len(body) > 240 {
		body = body[:240] + "…"
	}
	return fmt.Sprintf("HTTP %d（%s）%s", m.Status, m.ContentType(), body)
}

// Query 是构造查询串的小工具：一次给出若干「键, 值」对。
func Query(pairs ...string) url.Values {
	values := url.Values{}
	for index := 0; index+1 < len(pairs); index += 2 {
		values.Set(pairs[index], pairs[index+1])
	}
	return values
}
