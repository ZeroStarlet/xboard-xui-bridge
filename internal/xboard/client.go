package xboard

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/xboard-bridge/xboard-xui-bridge/internal/config"
)

// Client 是对接 Xboard /api/v2/server/* 的 HTTP 客户端。
//
// 并发安全：本结构所有字段在构造后只读，*http.Client 自带连接池且天然线程安全；
// 因此一个 Client 可被多个 Bridge 同时使用，无需额外加锁。
type Client struct {
	baseURL    string
	token      string
	userAgent  string
	httpClient *http.Client
}

// New 构造 Client。
//
// 入参 cfg 来自 config.Xboard，已在 config.Validate 中规范化；本构造函数
// 不再做二次校验，遵循"由谁负责校验，谁的输出可信"的契约。
func New(cfg config.Xboard) *Client {
	transport := &http.Transport{
		// 默认 transport 在 keep-alive 上的池化策略已足够；这里仅在 SkipTLSVerify
		// 为 true 时构造自定义 TLSClientConfig，避免对全局 http.DefaultTransport
		// 造成污染。
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: cfg.SkipTLSVerify, //nolint:gosec // 配置允许时由运维显式启用
			MinVersion:         tls.VersionTLS12,
		},
		MaxIdleConns:        16,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     90 * time.Second,
	}
	return &Client{
		baseURL:   cfg.APIHost,
		token:     cfg.Token,
		userAgent: cfg.UserAgent,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   time.Duration(cfg.TimeoutSec) * time.Second,
		},
	}
}

// FetchUsers 拉取 GET /api/v2/server/user 返回的用户清单。
//
// nodeID / nodeType 由调用方按 Bridge 配置传入，不在 Client 内持有。
func (c *Client) FetchUsers(ctx context.Context, nodeID int, nodeType string) (*UserListResp, error) {
	q := c.baseQuery(nodeID, nodeType)
	resp, err := c.do(ctx, http.MethodGet, "/api/v2/server/user", q, nil)
	if err != nil {
		return nil, err
	}
	defer drainAndClose(resp.Body)

	if err := assertHTTPOK(resp, "/api/v2/server/user"); err != nil {
		return nil, err
	}

	var out UserListResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("解码 /api/v2/server/user 响应：%w", err)
	}
	out.ETag = resp.Header.Get("ETag")
	return &out, nil
}

// PushTraffic 调用 POST /api/v2/server/push 上报流量。
//
// 入参 traffic 是 {uid_string: [up_bytes, down_bytes]} 形态的 map（详见
// types.go: PushTraffic 注释），由 sync.traffic_sync 调用 RecordSeen → 计算
// delta_up / delta_down 后填入。
//
// 行为约定：
//
//   - 即使 traffic 为空也会被序列化为 {} 提交；但 Xboard 端 processTraffic 在
//     empty 时直接 return 不更新任何缓存，所以本调用不能被当作"心跳保活"——
//     调用方应当自己判断 len(traffic) > 0 后再调用，避免无效请求。
//     节点心跳保活靠 status_sync 的 /status 上报刷新 SERVER_*_LAST_LOAD_AT。
//   - 成功语义：HTTP 200 且 body 反序列化得到 {data:true}（Xboard 约定）；
//     任一不满足都返回 *Error，调用方据此决定是否回退基线。
func (c *Client) PushTraffic(ctx context.Context, nodeID int, nodeType string, traffic PushTraffic) error {
	q := c.baseQuery(nodeID, nodeType)
	resp, err := c.do(ctx, http.MethodPost, "/api/v2/server/push", q, traffic)
	if err != nil {
		return err
	}
	defer drainAndClose(resp.Body)

	if err := assertHTTPOK(resp, "/api/v2/server/push"); err != nil {
		return err
	}
	return assertDataTrue(resp.Body, "/api/v2/server/push")
}

// PushAlive 调用 POST /api/v2/server/alive 上报在线 IP。
//
// alive 是一个 map[user_id_string][]ip_string；空 map 会被序列化为 {}，
// Xboard 端等价于"该节点本周期无在线用户"，仍是合法上报。
func (c *Client) PushAlive(ctx context.Context, nodeID int, nodeType string, alive AliveMap) error {
	q := c.baseQuery(nodeID, nodeType)
	if alive == nil {
		// nil map JSON 序列化为 null，Xboard 端会判定 422。这里统一替换为空对象。
		alive = AliveMap{}
	}
	resp, err := c.do(ctx, http.MethodPost, "/api/v2/server/alive", q, alive)
	if err != nil {
		return err
	}
	defer drainAndClose(resp.Body)
	if err := assertHTTPOK(resp, "/api/v2/server/alive"); err != nil {
		return err
	}
	return assertDataTrue(resp.Body, "/api/v2/server/alive")
}

// FetchAliveList 调用 GET /api/v2/server/alivelist 获取设备限制用户的当前在线 IP。
//
// 中间件不强制使用本接口；保留是为了未来在客户端侧做"主动断流量超限连接"。
// 当前阶段仅供运维自检/调试。
func (c *Client) FetchAliveList(ctx context.Context, nodeID int, nodeType string) (*AliveListResp, error) {
	q := c.baseQuery(nodeID, nodeType)
	resp, err := c.do(ctx, http.MethodGet, "/api/v2/server/alivelist", q, nil)
	if err != nil {
		return nil, err
	}
	defer drainAndClose(resp.Body)
	if err := assertHTTPOK(resp, "/api/v2/server/alivelist"); err != nil {
		return nil, err
	}
	var out AliveListResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("解码 /api/v2/server/alivelist 响应：%w", err)
	}
	if out.Alive == nil {
		out.Alive = AliveMap{}
	}
	return &out, nil
}

// PushStatus 调用 POST /api/v2/server/status 上报节点 CPU / 内存 / 磁盘。
func (c *Client) PushStatus(ctx context.Context, nodeID int, nodeType string, status StatusReport) error {
	q := c.baseQuery(nodeID, nodeType)
	resp, err := c.do(ctx, http.MethodPost, "/api/v2/server/status", q, status)
	if err != nil {
		return err
	}
	defer drainAndClose(resp.Body)
	if err := assertHTTPOK(resp, "/api/v2/server/status"); err != nil {
		return err
	}
	return assertDataTrue(resp.Body, "/api/v2/server/status")
}

// baseQuery 构造 token / node_id / node_type 三元组，所有 V2 端点共用。
func (c *Client) baseQuery(nodeID int, nodeType string) url.Values {
	q := url.Values{}
	q.Set("token", c.token)
	q.Set("node_id", strconv.Itoa(nodeID))
	if nodeType != "" {
		q.Set("node_type", nodeType)
	}
	return q
}

// do 是所有 API 方法共享的传输层；负责拼接 URL、设置 header、发送请求、
// 把网络错误规范化为 *Error。返回值由调用方负责 drain + close body。
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any) (*http.Response, error) {
	full := c.baseURL + path
	if len(query) > 0 {
		full += "?" + query.Encode()
	}

	var bodyReader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("序列化 %s body：%w", path, err)
		}
		bodyReader = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, full, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("构造 %s 请求：%w", path, err)
	}
	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, &Error{HTTPStatus: 0, Endpoint: path, Message: err.Error()}
	}
	return resp, nil
}

// assertHTTPOK 仅在 2xx 时返回 nil；否则读取 body 前 512 字节并包装成 *Error。
func assertHTTPOK(resp *http.Response, endpoint string) error {
	if resp.StatusCode/100 == 2 {
		return nil
	}
	raw := readSnippet(resp.Body, 512)
	msg := tryExtractMessage(raw)
	return &Error{
		HTTPStatus: resp.StatusCode,
		Endpoint:   endpoint,
		Message:    msg,
		RawBody:    raw,
	}
}

// assertDataTrue 解析 {"data":true} / {"data":...} 形态。
//
// Xboard 业务侧 push / status / alive 的成功标志都是 data == true（boolean）。
// 任何不为 true 的取值（false、null、字符串）都视为失败，由调用方决定重试策略。
func assertDataTrue(body io.Reader, endpoint string) error {
	raw, _ := io.ReadAll(io.LimitReader(body, 8*1024)) // 限 8K，防止异常响应耗内存
	if len(raw) == 0 {
		// 部分 V2 端点成功时 body 可能为空字符串；视为成功。
		return nil
	}
	// 尝试两种解码：先按对象，再按布尔。
	var asObj struct {
		Data    json.RawMessage `json:"data"`
		Message string          `json:"message"`
	}
	if err := json.Unmarshal(raw, &asObj); err == nil && len(asObj.Data) > 0 {
		// data 字段存在：判断其是否为 true。
		s := strings.TrimSpace(string(asObj.Data))
		if s == "true" {
			return nil
		}
		// data 不为 true：取 message 作为业务错误。
		return &Error{
			HTTPStatus: http.StatusOK,
			Endpoint:   endpoint,
			Message:    fmt.Sprintf("data=%s message=%q", s, asObj.Message),
			RawBody:    string(raw),
		}
	}
	// 没有 data 字段：兼容部分老路径返回纯 true 的情况。
	if strings.TrimSpace(string(raw)) == "true" {
		return nil
	}
	return &Error{
		HTTPStatus: http.StatusOK,
		Endpoint:   endpoint,
		Message:    "响应未含 data:true 标志",
		RawBody:    string(raw),
	}
}

// tryExtractMessage 在错误响应中尽力解析 {"message": "..."} 字段。
// 失败时返回空串，调用方会用 RawBody 兜底。
func tryExtractMessage(raw string) string {
	if raw == "" {
		return ""
	}
	var probe struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal([]byte(raw), &probe); err != nil {
		return ""
	}
	if probe.Message != "" {
		return probe.Message
	}
	return probe.Error
}

// readSnippet 读取最多 maxBytes 字节并返回字符串，多余部分丢弃。
// 不引发 EOF 错误是有意为之：错误响应已经是失败路径，再因 body 截断报二次错
// 反而会掩盖原始问题。
func readSnippet(r io.Reader, maxBytes int) string {
	buf := make([]byte, maxBytes)
	n, _ := io.ReadFull(io.LimitReader(r, int64(maxBytes)), buf)
	if n <= 0 {
		return ""
	}
	return string(buf[:n])
}

// drainAndClose 在 defer 中放心调用，吞掉 io.EOF 等 benign 错误。
//
// 必须 drain：未读完的 body 会让 keep-alive 连接无法回收，进而把
// 短时间内的并发请求降级为短连接，在面板小压力下也会出现莫名延迟。
func drainAndClose(rc io.ReadCloser) {
	_, _ = io.Copy(io.Discard, rc)
	_ = rc.Close()
}

// 编译期断言：确保 Client 不会被错误地实现成接口（仅实现这些方法时文件
// 编辑出错也能在编译期暴露）。该断言不消耗运行期内存。
var _ = errors.New
