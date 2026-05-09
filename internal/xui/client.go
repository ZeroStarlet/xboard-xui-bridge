package xui

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/xboard-bridge/xboard-xui-bridge/internal/config"
)

// Client 是 3x-ui 面板 API 客户端。
//
// 鉴权：仅支持 Bearer Token 模式（在 3x-ui 后台手动生成的 48 字符 API Token）。
// 历史上的 cookie 登录模式因 3x-ui 现代版本对浏览器路由启用 CSRF 校验而被
// 移除——详细回避动机见 config.XuiAuthModeToken 注释与 README "限制与边界"。
//
// 并发安全：本结构所有字段在构造后只读，*http.Client 自带连接池且天然线程安全；
// 因此一个 Client 可被多个 Bridge 同时使用，无需额外加锁。
type Client struct {
	baseURL    string // host + base_path（不含尾 /）
	apiToken   string
	httpClient *http.Client
}

// New 构造 Client。
//
// 不发起任何网络请求；构造失败仅可能源于 TLS 配置错误等本地资源初始化问题。
// 当前实现不返回错误，但保留 (Client, error) 签名以便未来扩展时不破坏调用方。
func New(cfg config.Xui) (*Client, error) {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: cfg.SkipTLSVerify, //nolint:gosec // 由运维配置显式启用
			MinVersion:         tls.VersionTLS12,
		},
		MaxIdleConns:        16,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     90 * time.Second,
	}
	c := &Client{
		baseURL:  cfg.APIHost + cfg.BasePath,
		apiToken: cfg.APIToken,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   time.Duration(cfg.TimeoutSec) * time.Second,
		},
	}
	return c, nil
}

// GetInbound 调用 GET /panel/api/inbounds/get/:id 获取单个 inbound 详情。
//
// 返回值的 ClientStats 字段已经被 3x-ui 后端用 enrichClientStats 填充
// （UUID、订阅 ID 等运行时字段），可直接用于差量计算。
func (c *Client) GetInbound(ctx context.Context, id int) (*Inbound, error) {
	endpoint := "/panel/api/inbounds/get/" + strconv.Itoa(id)
	raw, err := c.do(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	var ib Inbound
	if err := json.Unmarshal(raw, &ib); err != nil {
		return nil, fmt.Errorf("解码 %s 响应：%w", endpoint, err)
	}
	return &ib, nil
}

// GetClientTrafficsByInboundID 调用 GET /panel/api/inbounds/getClientTrafficsById/:id。
//
// 与 GetInbound 区别：
//
//	GetInbound                     返回 settings + clientStats，体积较大；
//	GetClientTrafficsByInboundID   仅返回 clientStats，差量计算用它最划算。
func (c *Client) GetClientTrafficsByInboundID(ctx context.Context, inboundID int) ([]ClientTraffic, error) {
	endpoint := "/panel/api/inbounds/getClientTrafficsById/" + strconv.Itoa(inboundID)
	raw, err := c.do(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	// 极端情况下 obj 可能是 null（inbound 无 client 时）。
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var out []ClientTraffic
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("解码 %s 响应：%w", endpoint, err)
	}
	return out, nil
}

// AddClient 调用 POST /panel/api/inbounds/addClient 新增一个或多个 client。
//
// settingsJSON 是预先构造好的 inbound.settings 形态字符串（仅 clients 数组），
// 由 protocol 适配器生成；本包不解析其字段，避免协议耦合。
func (c *Client) AddClient(ctx context.Context, inboundID int, settingsJSON string) error {
	body := AddClientReq{ID: inboundID, Settings: settingsJSON}
	endpoint := "/panel/api/inbounds/addClient"
	if _, err := c.do(ctx, http.MethodPost, endpoint, body); err != nil {
		return err
	}
	return nil
}

// UpdateClient 调用 POST /panel/api/inbounds/updateClient/:clientKey。
//
// clientKey 的取值由协议决定（详见 protocol.Adapter.KeyOf）：
//
//	vless / vmess           → client.id (UUID)
//	trojan                  → client.password
//	shadowsocks             → client.email
//	hysteria / hysteria2    → client.auth
func (c *Client) UpdateClient(ctx context.Context, clientKey string, inboundID int, settingsJSON string) error {
	body := UpdateClientReq{ID: inboundID, Settings: settingsJSON}
	endpoint := "/panel/api/inbounds/updateClient/" + url.PathEscape(clientKey)
	if _, err := c.do(ctx, http.MethodPost, endpoint, body); err != nil {
		return err
	}
	return nil
}

// DelClientByEmail 调用 POST /panel/api/inbounds/:id/delClientByEmail/:email
// 按 email 删除客户端。即使该 email 已经不存在也返回 nil（幂等性由调用方保障）。
func (c *Client) DelClientByEmail(ctx context.Context, inboundID int, email string) error {
	endpoint := fmt.Sprintf("/panel/api/inbounds/%d/delClientByEmail/%s",
		inboundID, url.PathEscape(email))
	if _, err := c.do(ctx, http.MethodPost, endpoint, nil); err != nil {
		return err
	}
	return nil
}

// GetClientIPs 调用 POST /panel/api/inbounds/clientIps/:email，
// 返回字符串数组形态的 IP 历史（含时间戳）。
//
// 中间件主要用 GetOnlines() 拿在线 email，再拿 GetClientIPs 找该 email 当下使用的 IP。
// 时间戳信息保留在原始字符串中（"1.2.3.4 (2024-01-02 15:04:05)"），调用方自行解析。
func (c *Client) GetClientIPs(ctx context.Context, email string) ([]string, error) {
	endpoint := "/panel/api/inbounds/clientIps/" + url.PathEscape(email)
	raw, err := c.do(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	// 3x-ui 端 obj 字段在"无 IP 记录"时可能返回字符串 "No IP Record" 而不是数组，
	// 需要双形态兼容；只在数组形态下解析。
	if raw[0] != '[' {
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("解码 %s 响应：%w", endpoint, err)
	}
	return out, nil
}

// GetOnlines 调用 POST /panel/api/inbounds/onlines；返回当前在线的 email 列表。
//
// 该数据是面板从本地 + 远程节点聚合得到，刷新周期约 10 秒。
func (c *Client) GetOnlines(ctx context.Context) ([]string, error) {
	endpoint := "/panel/api/inbounds/onlines"
	raw, err := c.do(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("解码 %s 响应：%w", endpoint, err)
	}
	return out, nil
}

// GetServerStatus 调用 GET /panel/api/server/status；返回 cpu / mem / netUp / netDown 等。
//
// 用于把节点负载上报回 Xboard。返回值为 RawMessage，由调用方按需解构——本包不
// 提前定义结构是为了避免 3x-ui 字段调整时本包跟着改版。
func (c *Client) GetServerStatus(ctx context.Context) (json.RawMessage, error) {
	return c.do(ctx, http.MethodGet, "/panel/api/server/status", nil)
}

// do 是所有面板 API 共享的传输层。
//
// 行为：
//
//  1. 拼接 baseURL + endpoint；
//  2. 序列化 body；
//  3. 注入 Authorization: Bearer <api_token>；
//  4. 解析 commonResp，仅在 success==true 时返回 obj 字段。
func (c *Client) do(ctx context.Context, method, endpoint string, body any) (json.RawMessage, error) {
	full := c.baseURL + endpoint

	var bodyReader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("序列化 %s body：%w", endpoint, err)
		}
		bodyReader = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, full, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("构造 %s 请求：%w", endpoint, err)
	}
	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, &Error{HTTPStatus: 0, Endpoint: endpoint, Msg: err.Error()}
	}
	defer drainAndClose(resp.Body)

	rawBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024)) // 4MB 兜底，避免巨型响应耗内存
	if resp.StatusCode/100 != 2 {
		return nil, &Error{
			HTTPStatus: resp.StatusCode,
			Endpoint:   endpoint,
			Msg:        sniffMsg(rawBody),
			RawBody:    truncate(string(rawBody), 512),
		}
	}

	// 解析 commonResp。3x-ui 全量端点都遵循该外壳。
	var cr commonResp
	if err := json.Unmarshal(rawBody, &cr); err != nil {
		return nil, &Error{
			HTTPStatus: resp.StatusCode,
			Endpoint:   endpoint,
			Msg:        "响应非 JSON",
			RawBody:    truncate(string(rawBody), 512),
		}
	}
	if !cr.Success {
		return nil, &Error{
			HTTPStatus: resp.StatusCode,
			Endpoint:   endpoint,
			Msg:        cr.Msg,
			RawBody:    truncate(string(rawBody), 512),
		}
	}
	return cr.Obj, nil
}

// truncate 把字符串截到最多 max 字节，超长部分用 "…" 标记，便于日志阅读。
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// sniffMsg 在错误响应中尽力提取 commonResp.msg；失败时返回原始 body 的前 64 字节。
func sniffMsg(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var probe struct {
		Msg string `json:"msg"`
	}
	if err := json.Unmarshal(raw, &probe); err == nil && probe.Msg != "" {
		return probe.Msg
	}
	return truncate(string(raw), 64)
}

// drainAndClose 与 xboard 包同名函数语义一致：吃完 body 让 keep-alive 复用。
func drainAndClose(rc io.ReadCloser) {
	_, _ = io.Copy(io.Discard, rc)
	_ = rc.Close()
}
