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
		// 不直接复用 http.DefaultTransport 的两个原因：
		//  1) SkipTLSVerify 必须按 cfg 决定；复用 default 会污染全局；
		//  2) default 的 ResponseHeaderTimeout=0（无限等响应头），上游 PHP-FPM
		//     / 反代 worker 在读完 header 前卡死时，单次请求能撑满整个
		//     httpClient.Timeout（默认 15s）。本 transport 在 IO 各阶段
		//     设独立超时，让"读 header 前卡死""TLS 握手卡死""100-Continue
		//     卡死"三类异常各自尽快返回；端到端总上限仍由 httpClient.Timeout
		//     兜底（含 DNS/TCP dial / 响应体读取阶段，本 transport 不再
		//     额外约束）。
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: cfg.SkipTLSVerify, //nolint:gosec // 配置允许时由运维显式启用
			MinVersion:         tls.VersionTLS12,
		},
		// 连接池：MaxIdleConns 是全局闲置上限；MaxIdleConnsPerHost 与
		// MaxConnsPerHost 是按目标 host 细化限制——Xboard 通常只有一个
		// host，但中间件在多 bridge 部署下会并发把 4 类同步循环对齐到该
		// host（4 bridges × 4 loops = 16 并发），叠加同周期偶发的运维操作
		// （Web Reload 触发的 LoadFromStore 不走该 transport，但 alive 端
		// 上报与 traffic 差量 push 仍可能瞬时叠加）。MaxConnsPerHost=32
		// 给典型部署留 2× 头空间，避免任意单条卡住的请求把后续所有 RPC
		// 排队等到 httpClient.Timeout——队列等待计入端到端超时，会让本
		// 周期连环 fail。显式锁定上限同时阻断"连接风暴 → ephemeral 端口
		// 耗尽"边角失败（极端时新连接被 EADDRINUSE 拒绝）。
		MaxIdleConns:        16,
		MaxIdleConnsPerHost: 8,
		MaxConnsPerHost:     32,
		IdleConnTimeout:     90 * time.Second,
		// 细粒度超时：仅约束 TLS 握手 / 服务端读完 header / 100-Continue
		// 三个 IO 子阶段。10s / 10s / 1s 远高于正常 Xboard 响应时间
		// （< 500ms），远低于默认 client.Timeout=15s。注意：这些超时不
		// 覆盖 DNS/TCP dial 与响应体读取——后两者仍由 httpClient.Timeout
		// 端到端兜底。
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		// 显式启用 HTTP/2：当 Transport 设了自定义 TLSClientConfig 时，
		// Go net/http 会保守地禁用 HTTP/2 自动协商（除非显式置
		// ForceAttemptHTTP2=true），原因是 stdlib 担心调用方意图绕开
		// 默认 ALPN 行为。本中间件需要的是"自签证书放行"而非"禁用 h2"，
		// 故必须显式置 true 才能在 nginx + h2 反代场景下复用单 TCP 流，
		// 显著降低连接池压力（多个并发 RPC 共享一个 TCP）。
		ForceAttemptHTTP2: true,
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
//     array_filter 后 empty($data) 时直接 return **不更新任何缓存**，等价于
//     "请求白发"。所以发空 traffic 既无业务效果也不维持 LAST_PUSH_AT。
//     v0.6 起 traffic_sync 在无差量时**不会调用本函数**——空 push 既然
//     无副作用，调用本身就是冗余流量；详见 internal/sync/traffic_sync.go
//     "v0.6 移除 [0, 0] 心跳兜底"段落。
//   - 节点 STATUS_ONLINE / STATUS_ONLINE_NO_PUSH 切换完全交给 Xboard 端
//     原生判定逻辑：5 分钟内无真实流量即 STATUS_ONLINE_NO_PUSH。中间件
//     不再构造伪 [0, 0] 占位项欺骗判定。status_sync 的 /status 端点只
//     刷新 LAST_LOAD_AT 缓存（节点负载条），与 available_status 判定无关
//     ——v0.4 之前老注释一度写"心跳保活靠 status_sync"，那是错的，已在
//     v0.5 修正；v0.6 进一步移除 traffic_sync 的占位心跳路径，让节点状态
//     完全反映真实数据。
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
