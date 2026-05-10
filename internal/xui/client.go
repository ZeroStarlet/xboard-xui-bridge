package xui

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xboard-bridge/xboard-xui-bridge/internal/config"
)

// Client 是 3x-ui 面板 API 客户端。
//
// 鉴权（v0.4 起仅 cookie 登录模式）：
//
//	首次调用时 POST /login 拿 session cookie 存进 cookiejar，后续请求由
//	net/http 自动带 cookie。鉴权失效（401 / 302 / HTML 登录页 / JSON
//	success=false 含登录关键字）时自动重登录并重试一次；二次仍失败即返回
//	*Error 让上层 sync 循环走 WARN 路径，下个周期再试。
//
//	若 3x-ui 后台启用了 TOTP 2FA，构造时传 cfg.TOTPSecret（base32），每次
//	/login 自动算 RFC 6238 6 位码塞 twoFactorCode 字段；未启用 2FA 留空即可。
//
// 并发安全：
//
//	a) httpClient 自带连接池天然线程安全；cookiejar.Jar 内部用 sync.Mutex
//	   保护 SetCookies/Cookies，并发读写无 race。
//	b) doLogin 由 loginMu 串行化，防止多个 Bridge worker 同时触发 /login
//	   造成"5 个 worker 各自登录 5 次"的请求风暴。
//	c) loggedIn 字段同样由 loginMu 保护——仅 ensureLoggedIn / invalidateSession
//	   两处读写，访问点都已持锁。
//	d) baseURL / username / password / totpSecret 等构造后不可变字段无锁；
//	   它们的读取行为对所有 goroutine 一致。
type Client struct {
	baseURL    string // host + base_path（不含尾 /）
	username   string
	password   string
	totpSecret string // 3x-ui 启用 2FA 时使用；空串表示未启用
	httpClient *http.Client

	// loginMu 同时保护 loggedIn 与"是否正在 /login 中"。两处临界区不在同一
	// 调用栈互相嵌套，所以一把互斥锁足够，无需 RWMutex。
	loginMu sync.Mutex
	// loggedIn 表示"此 Client 上一次成功 login 之后尚未被 invalidate"——
	// 不代表服务端 cookie 一定还有效（cookie 失效仍要靠 sendOnce 探测，
	// 然后 invalidateSession 把它翻回 false 触发重登录）。
	loggedIn bool
}

// New 构造 Client。
//
// 不发起任何网络请求（/login 推迟到首次业务调用时由 ensureLoggedIn 触发），
// 便于"半填配置"场景仍能完成 Client 构造，引擎后续 Reload 即可恢复。
//
// 构造失败仅可能源于 cookiejar.New 返回错误——当前 Go 标准库对 nil options
// 一定返回 nil error，但仍保留错误传递路径以适配未来 std 库变更。TLS 配置
// 错误会被 *http.Transport 推迟到首次请求时报告，本函数不主动检测。
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

	// 3x-ui 用 gin sessions 默认 cookie 名 "session" 且 HttpOnly + Path=/，
	// 标准 cookiejar 自动处理域 / 路径作用域，无需特殊配置。
	// 使用 nil options：默认 PublicSuffixList=nil 不做公共后缀过滤，对 IP 直连
	// 与本地局域网域名都友好。
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("初始化 cookie jar：%w", err)
	}

	httpClient := &http.Client{
		Transport: transport,
		Timeout:   time.Duration(cfg.TimeoutSec) * time.Second,
		Jar:       jar,
	}

	c := &Client{
		baseURL:    cfg.APIHost + cfg.BasePath,
		username:   cfg.Username,
		password:   cfg.Password,
		totpSecret: cfg.TOTPSecret,
		httpClient: httpClient,
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
// v0.4 起的语义（仅 cookie 模式）：
//
//	1. ensureLoggedIn 确保 cookiejar 中有有效 session cookie；登录失败立刻
//	   返回 *Error，不发出"必然 401"的无效业务请求；
//	2. sendOnce 发一次请求；若返回 authStale=true（looksLikeAuthFailed 识别）
//	   则 invalidateSession + ensureLoggedIn + sendOnce 第二次；
//	3. 第二次仍 stale → 立刻报错，绝不返回 (nil, nil) 让 mutating 端点
//	   （AddClient / UpdateClient 等）误判"已写入"。
//
// body 重读保护：把序列化结果保留在 bodyBytes 中，sendOnce 内部用
// bytes.NewReader 重新构造 io.Reader——确保重试时 body 不会因为已被
// http.Client.Do 消费而 EOF（io.Reader 不可重读 pitfall）。
func (c *Client) do(ctx context.Context, method, endpoint string, body any) (json.RawMessage, error) {
	full := c.baseURL + endpoint

	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("序列化 %s body：%w", endpoint, err)
		}
	}

	if err := c.ensureLoggedIn(ctx); err != nil {
		return nil, fmt.Errorf("登录 3x-ui：%w", err)
	}

	raw, authStale, err := c.sendOnce(ctx, method, full, endpoint, bodyBytes)
	if !authStale {
		return raw, err
	}

	// 重登录 + 重试一次。第二次仍 stale → 立刻报错，避免返回 (nil, nil) 让上层
	// 误判"业务成功但 obj 为 null"——AddClient / UpdateClient / DelClientByEmail
	// 等 mutating 端点拿到 err==nil 会直接进入"已写入"状态机，破坏一致性。
	c.invalidateSession()
	if err := c.ensureLoggedIn(ctx); err != nil {
		return nil, fmt.Errorf("重登录 3x-ui：%w", err)
	}
	retriedRaw, retriedStale, retriedErr := c.sendOnce(ctx, method, full, endpoint, bodyBytes)
	if retriedStale {
		// 重登录后第二次请求仍被判鉴权失效——常见根因：
		//   a) 运维在 3x-ui 后台改了密码但中间件 settings 未同步；
		//   b) 3x-ui 启用了暴力破解保护，连续 /login 触发临时锁定；
		//   c) 3x-ui 启用了 CSRF middleware（v2.4+ 主线 fork 行为可能不同），
		//      cookie 登录后还需 X-CSRF-Token——v0.4 暂不实现，遇到时再扩展。
		// 这里返回明确 *Error 而不是 (nil, nil)，确保 mutating 调用（AddClient
		// 等）不会把"未写入"误当作"已写入"。
		return nil, &Error{
			HTTPStatus: 0,
			Endpoint:   endpoint,
			Msg:        "重登录后仍被判鉴权失效（凭据可能变更 / 服务端限流 / 启用了 CSRF 校验）",
		}
	}
	return retriedRaw, retriedErr
}

// sendOnce 发一次 HTTP 请求并解析响应。
//
// 返回三元组（authStale × err 二维区分）：
//
//	(obj, false, nil)  → 成功，obj 为 commonResp.obj 字段（可能为 null）；
//	(nil, false, err)  → 普通错误（网络 / 4xx 非鉴权 / 5xx / success=false 业务错）；
//	(nil, true, nil)   → 检测到鉴权失效，调用方应触发重登录 + 重试。
//
// 不让"鉴权失效"作为 err 的特例，是为了让上层用 if authStale 判别比 errors.Is
// 更直观；err 永远表达"无法继续"语义，调用方拿到 err 直接返回即可。
//
// 入参 endpoint 仅用于 *Error 的诊断标签——与 fullURL 解耦，便于日志中既能
// 看到完整 URL 又能保留语义化的端点路径。
//
// 鉴权策略：cookie 由 httpClient.Jar 自动注入；本函数不手动 Set("Cookie", ...)
// 是为了让 jar 的"路径作用域 / 过期处理"语义完整生效。
func (c *Client) sendOnce(ctx context.Context, method, fullURL, endpoint string, bodyBytes []byte) (json.RawMessage, bool, error) {
	var bodyReader io.Reader
	if bodyBytes != nil {
		bodyReader = bytes.NewReader(bodyBytes)
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return nil, false, fmt.Errorf("构造 %s 请求：%w", endpoint, err)
	}
	if bodyBytes != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, false, &Error{HTTPStatus: 0, Endpoint: endpoint, Msg: err.Error()}
	}
	defer drainAndClose(resp.Body)

	rawBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024)) // 4MB 兜底，避免巨型响应耗内存

	// 识别"鉴权失效"——上层 do() 收到 authStale=true 会触发重登录 + 重试一次。
	if c.looksLikeAuthFailed(resp, rawBody) {
		return nil, true, nil
	}

	if resp.StatusCode/100 != 2 {
		return nil, false, &Error{
			HTTPStatus: resp.StatusCode,
			Endpoint:   endpoint,
			Msg:        sniffMsg(rawBody),
			RawBody:    truncate(string(rawBody), 512),
		}
	}

	// 解析 commonResp。3x-ui 全量端点都遵循该外壳。
	var cr commonResp
	if err := json.Unmarshal(rawBody, &cr); err != nil {
		return nil, false, &Error{
			HTTPStatus: resp.StatusCode,
			Endpoint:   endpoint,
			Msg:        "响应非 JSON",
			RawBody:    truncate(string(rawBody), 512),
		}
	}
	if !cr.Success {
		return nil, false, &Error{
			HTTPStatus: resp.StatusCode,
			Endpoint:   endpoint,
			Msg:        cr.Msg,
			RawBody:    truncate(string(rawBody), 512),
		}
	}
	return cr.Obj, false, nil
}

// ensureLoggedIn 确保 cookie 模式下当前 Client 已经持有有效 cookie。
//
// 行为：
//
//	a) 已 loggedIn → 立即返回 nil（仍走一次锁，防止"doLogin 进行中其他 goroutine
//	   误判已登录直接发请求"导致的乱序）；
//	b) 未 loggedIn → 调用 doLogin 一次，成功后置 loggedIn=true 返回 nil；
//	c) doLogin 失败 → 不动 loggedIn，原样返回错误（错误链含原因）。
//
// 仅 cookie 模式调用此函数；token 模式无登录概念。调用方负责模式判别。
func (c *Client) ensureLoggedIn(ctx context.Context) error {
	c.loginMu.Lock()
	defer c.loginMu.Unlock()
	if c.loggedIn {
		return nil
	}
	if err := c.doLogin(ctx); err != nil {
		return err
	}
	c.loggedIn = true
	return nil
}

// invalidateSession 把 loggedIn 翻成 false，强制下一次 ensureLoggedIn 重发 /login。
//
// 不主动清空 cookie jar：3x-ui 服务端在重新 /login 成功时返回的 Set-Cookie
// 会用同名 cookie 覆盖 jar 中的旧条目（gin sessions 默认 cookie 名 "session"），
// 所以"旧 cookie 还在 jar 里"仅在两次 ensureLoggedIn 之间的极短窗口存在，
// 且因 loggedIn=false 此时不会有请求被发出。手动清 jar 反而需要替换
// httpClient.Jar 字段，与 client.Do 的并发读取构成 race，得不偿失。
func (c *Client) invalidateSession() {
	c.loginMu.Lock()
	defer c.loginMu.Unlock()
	c.loggedIn = false
}

// looksLikeAuthFailed 判定一次 cookie 模式响应是否属于"鉴权失效"。
//
// 3x-ui 主线 + 多种 fork 在 cookie 失效时的兜底响应不一致，按观察到的几种形态
// 兜底匹配：
//
//	a) HTTP 401 / 403：标准错误码；
//	b) HTTP 200 + body 以 < 开头：被默认 redirect-follow 引到了 /login 的 HTML 页；
//	c) HTTP 200 + JSON {"success":false,"msg":"login session timeout"} 等：
//	   按 msg 关键字识别（覆盖中英文 + fork 扩展词）。
//
// 误判风险：业务接口（非 /login）返回 success=false 且 msg 含"login"等字眼
// 的概率极低（3x-ui inbound API 的错误信息不会出现"login""session"）；
// 即使误判，最坏后果也只是"多发一次 /login + 重试本次请求"——没有数据正确
// 性风险，性能损耗可忽略。
func (c *Client) looksLikeAuthFailed(resp *http.Response, body []byte) bool {
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return true
	}
	if resp.StatusCode != http.StatusOK || len(body) == 0 {
		return false
	}
	// HTML 探测：去除前导空白后首字符 '<' 即视作 HTML。3x-ui 的合法 JSON 响应
	// 永远以 '{' 或 '[' 开头，不与此冲突。
	head := bytes.TrimLeft(body, " \t\r\n")
	if len(head) > 0 && head[0] == '<' {
		return true
	}
	// 解析 commonResp 探测 success=false + msg 关键字。
	var probe struct {
		Success bool   `json:"success"`
		Msg     string `json:"msg"`
	}
	if err := json.Unmarshal(body, &probe); err != nil || probe.Success {
		return false
	}
	msgLower := strings.ToLower(probe.Msg)
	// 中英文双语关键字穷举：覆盖主线 + 已知 fork 的常见输出。
	// 注意 "login" 会同时匹配 "please login""login required""login session timeout"
	// 等等；"session" 兜住"session timeout""session expired"。中文 "登录" / "登入"
	// 兼容大陆 / 港台不同译法。
	for _, kw := range []string{"login", "session", "未登录", "未登入", "登录已过期", "登入已过期", "请登录", "请登入"} {
		if strings.Contains(msgLower, kw) || strings.Contains(probe.Msg, kw) {
			return true
		}
	}
	return false
}

// doLogin 调用 3x-ui /login 拿 session cookie。
//
// 请求：
//
//	POST {baseURL}/login
//	Content-Type: application/json
//	Accept: application/json
//	{"username":"...","password":"...","twoFactorCode":"..."}
//
// 关于 body 格式：3x-ui 主线 web/controller/index.go 用 c.ShouldBind 自动按
// Content-Type 选 binding，JSON / form-urlencoded 两种都接受。本中间件统一
// 走 JSON——既现代化、与本包其它请求一致，又对 fork 中可能的"严格 form
// binding"具有更好的语义稳定性。
//
// 响应：
//
//	HTTP 200 + Set-Cookie: session=... + body {"success":true,...} → 登录成功；
//	HTTP 200                            + body {"success":false,...} → 业务失败，
//	                                                                按 msg 关键字
//	                                                                分流为 ErrTOTPRequired
//	                                                                或 ErrInvalidCredentials；
//	非 2xx                                                          → *Error 抛出。
//
// TOTP 处理：若 totpSecret 非空，调 generateTOTP 算"此刻"6 位码塞 twoFactorCode；
// 否则发空串（3x-ui 的 LoginForm 字段允许缺失/为空，未启用 2FA 的部署不会因此
// 失败）。
//
// 不在此函数内重试：HTTP 层失败 / 凭据错误 / TOTP 错误都需要运维介入，自动重试
// 既无意义又可能触发服务端账户锁定。
//
// 调用契约：调用方必须持 loginMu（由 ensureLoggedIn 保证）。doLogin 自身不
// 二次加锁——重入锁会让 ensureLoggedIn 的语义复杂化得不偿失。
func (c *Client) doLogin(ctx context.Context) error {
	var twoFactorCode string
	if c.totpSecret != "" {
		code, err := generateTOTP(c.totpSecret)
		if err != nil {
			return fmt.Errorf("计算 TOTP 码：%w", err)
		}
		twoFactorCode = code
	}

	payload := struct {
		Username      string `json:"username"`
		Password      string `json:"password"`
		TwoFactorCode string `json:"twoFactorCode"`
	}{c.username, c.password, twoFactorCode}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("序列化 /login body：%w", err)
	}

	full := c.baseURL + "/login"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, full, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("构造 /login 请求：%w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return &Error{HTTPStatus: 0, Endpoint: "/login", Msg: err.Error()}
	}
	defer drainAndClose(resp.Body)

	rawBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))

	if resp.StatusCode/100 != 2 {
		return &Error{
			HTTPStatus: resp.StatusCode,
			Endpoint:   "/login",
			Msg:        sniffMsg(rawBody),
			RawBody:    truncate(string(rawBody), 512),
		}
	}

	var cr commonResp
	if err := json.Unmarshal(rawBody, &cr); err != nil {
		return &Error{
			HTTPStatus: resp.StatusCode,
			Endpoint:   "/login",
			Msg:        "响应非 JSON",
			RawBody:    truncate(string(rawBody), 512),
		}
	}
	if !cr.Success {
		// 关键字识别：覆盖主线 + 中英文 fork 的"需要 2FA"措辞。
		// strings.ToLower 仅对 ASCII 字母有效，中文关键字直接对原 Msg 匹配。
		msgLower := strings.ToLower(cr.Msg)
		for _, kw := range []string{"two factor", "2fa"} {
			if strings.Contains(msgLower, kw) {
				return ErrTOTPRequired
			}
		}
		for _, kw := range []string{"二次验证", "二步验证", "动态密码", "双重认证"} {
			if strings.Contains(cr.Msg, kw) {
				return ErrTOTPRequired
			}
		}
		return ErrInvalidCredentials
	}

	// 防御性校验：success=true 但服务端没真的下发 cookie——3x-ui 主线不会发生，
	// 但 fork 的实现细节可能有差异。这种情况下"loggedIn=true 但 jar 是空"会让
	// 业务请求被服务端再次判定为失效，进而触发"重登录—又登录—又失败"循环。
	// 直接判 fail 让运维立刻看到错误根因。
	if c.httpClient.Jar != nil {
		u, parseErr := url.Parse(c.baseURL + "/")
		if parseErr == nil && len(c.httpClient.Jar.Cookies(u)) == 0 {
			return errors.New("/login 返回 success=true 但未下发 cookie；3x-ui 服务端行为异常")
		}
	}

	return nil
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
