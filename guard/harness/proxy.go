package harness

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
)

// Request 是经过转发探针的一个请求与它的响应摘要。
//
// 探针只记录、不改写：请求照原样转给真实服务端，响应照原样回到调用方，
// 因此记录下来的就是命令行与探针真正发到线上的东西。
type Request struct {
	Method string
	Path   string
	Query  url.Values
	Body   []byte
	// Status 是服务端给出的状态码。
	Status int
	// ResponseCode 是响应体里的错误标识；成功响应或正文不是 JSON 时为空。
	ResponseCode string
}

// HasQuery 判断这次请求是否带了某个查询参数（含空值）。
func (request Request) HasQuery(name string) bool {
	_, ok := request.Query[name]
	return ok
}

// BodyField 判断请求体（JSON 对象）里是否出现了某个字段。
func (request Request) BodyField(name string) bool {
	_, ok := request.BodyValue(name)
	return ok
}

// BodyValue 取请求体（JSON 对象）里某个字段的值。
func (request Request) BodyValue(name string) (any, bool) {
	if len(request.Body) == 0 {
		return nil, false
	}
	var decoded map[string]any
	if err := json.Unmarshal(request.Body, &decoded); err != nil {
		return nil, false
	}
	value, ok := decoded[name]
	return value, ok
}

// Describe 把这次请求压成一行。
func (request Request) Describe() string {
	text := request.Method + " " + request.Path
	if len(request.Query) > 0 {
		text += "?" + request.Query.Encode()
	}
	if len(request.Body) > 0 {
		body := string(request.Body)
		if len(body) > 160 {
			body = body[:160] + "…"
		}
		text += " 正文=" + body
	}
	return text
}

// Recorder 是挡在真实服务端前面的转发探针。
type Recorder struct {
	mu      sync.Mutex
	records []Request
	proxy   *httputil.ReverseProxy
}

// NewRecorder 建立一个指向 target 的转发探针。
func NewRecorder(target *url.URL) *Recorder {
	recorder := &Recorder{}
	recorder.proxy = &httputil.ReverseProxy{
		// 只换目标主机，路径与查询串照原样带过去。
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(target)
		},
		Transport: recorder,
		ErrorHandler: func(writer http.ResponseWriter, _ *http.Request, err error) {
			http.Error(writer, "转发失败: "+err.Error(), http.StatusBadGateway)
		},
	}
	return recorder
}

// ServeHTTP 让探针自己就是一个处理器。
func (recorder *Recorder) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	recorder.proxy.ServeHTTP(writer, request)
}

// RoundTrip 是探针的记录点：请求出去之前记一笔，响应回来之后补上结果。
func (recorder *Recorder) RoundTrip(request *http.Request) (*http.Response, error) {
	body := []byte(nil)
	if request.Body != nil {
		read, err := io.ReadAll(request.Body)
		if err == nil {
			body = read
		}
		_ = request.Body.Close()
		request.Body = io.NopCloser(bytes.NewReader(body))
	}

	record := Request{
		Method: request.Method,
		Path:   request.URL.Path,
		Query:  request.URL.Query(),
		Body:   body,
	}

	response, err := http.DefaultTransport.RoundTrip(request)
	if err != nil {
		recorder.append(record)
		return nil, err
	}

	record.Status = response.StatusCode
	if strings.HasPrefix(response.Header.Get("Content-Type"), "application/json") {
		record.ResponseCode = captureCode(response)
	}
	recorder.append(record)
	return response, nil
}

// capturedBodyLimit 是探针最多读多少响应体来识别错误标识。
const capturedBodyLimit = 8 << 10

// captureCode 偷看响应体里的 code 字段，并把正文原样拼回去。
func captureCode(response *http.Response) string {
	captured, err := io.ReadAll(io.LimitReader(response.Body, capturedBodyLimit))
	if err != nil {
		return ""
	}
	remaining := response.Body
	response.Body = readCloser{
		Reader: io.MultiReader(bytes.NewReader(captured), remaining),
		Closer: remaining,
	}
	return codeOf(captured)
}

// readCloser 把「已经读出来的部分 + 还没读的部分」拼成一个可关闭的正文。
type readCloser struct {
	io.Reader
	io.Closer
}

// append 记一笔。
func (recorder *Recorder) append(record Request) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.records = append(recorder.records, record)
}

// Reset 清空记录，通常在一次命令行调用之前调用。
func (recorder *Recorder) Reset() {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.records = nil
}

// Requests 返回到目前为止的全部记录副本。
func (recorder *Recorder) Requests() []Request {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	copied := make([]Request, len(recorder.records))
	copy(copied, recorder.records)
	return copied
}

// codeOf 从响应体里取出错误标识。
func codeOf(body []byte) string {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return ""
	}
	var payload struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(trimmed, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.Code)
}
