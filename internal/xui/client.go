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
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/xboard-bridge/xboard-xui-bridge/internal/config"
)

// Client 是 3x-ui 面板 API 客户端，仅支持 Bearer API Token 单通道。
//
// 鉴权机制（v0.6 起，仅适配 3x-ui v3.0.0+，详见 internal/config/config.go
// 文件头"3x-ui 鉴权说明"注释块）：
//
//	每次面板 API 请求都在请求头注入 "Authorization: Bearer <APIToken>"；
//	3x-ui v3.0.0 内 APIController.checkAPIAuth 走 SettingService.MatchApiToken
//	（constant-time compare）通道，校验通过后 c.Set("api_authed", true)，
//	让 CSRFMiddleware 立即短路——token 模式下连 mutating（POST）请求都
//	无需 X-CSRF-Token 头。
//
// 单一正向路径承诺：
//
//	a) 无登录、无 CSRF、无 cookie，请求路径只有"构造请求 → 注入 Bearer
//	   头 → 发送 → 解码 commonResp"四步；
//	b) 任何 4xx / 5xx / 网络错误都立即向上传播为 *Error，不在客户端层做
//	   隐式重试 / 重登录 / 兜底响应；
//	c) Token 错误（典型 401 / 404）= 运维在 3x-ui 面板重新生成 token 后
//	   未同步到本中间件——立即报错让运维显式介入。
//
// 并发安全：
//
//	a) httpClient 自带连接池天然线程安全；本 Client 不持有 cookiejar，
//	   也不需要登录串行化锁，构造后所有字段（baseURL / apiToken）皆为
//	   只读，多 goroutine 并发请求无 race。
//	b) listMu 保护 /list 端点的周期内合并缓存（v0.5.3 起引入），仅保护
//	   短暂的 cache 状态读写，与请求路径无嵌套。
type Client struct {
	baseURL    string // host + base_path（不含尾 /）
	apiToken   string // Bearer 头注入值；构造后不可变
	httpClient *http.Client

	// /panel/api/inbounds/list 端点的周期内合并缓存（v0.5.3）。
	//
	// 背景：traffic_sync 与 alive_sync 都通过 GetClientTrafficsByInboundID
	// 间接拉 /list；多 Bridge 部署下每周期对同一 host 重复发相同请求。
	// 单周期内多次调用走缓存命中；多 goroutine 并发撞上过期 → singleflight
	// 合并成一次 HTTP，等待者通过 done channel 拿同一份结果。
	//
	// listCacheTTL 取 5 秒：远小于默认同步周期 60 秒，避免"上一周期数据
	// 跨周期使用"导致 user_sync 刚加的新 client 流量被推迟到下周期上报；
	// 同时仍能让 traffic_sync + alive_sync 在同一周期内（典型间隔 < 1s）
	// 共享一次拉取。
	//
	// 缓存仅作用于"已经鉴权通过的成功响应"，本身不绕过鉴权——任何
	// fetchInbounds 失败都不会污染 cache。
	listMu       sync.Mutex
	listCache    []Inbound          // 上次成功拉取的 inbounds 快照（nil 与空切片均可能为合法成功结果，例如 obj=null）
	listExpireAt time.Time          // 缓存有效性的真相源；零值或已过 = 缓存无效。listCache 自身不能用作"是否有缓存"的判定依据
	listInflight *listFetchInflight // 非 nil 表示有 goroutine 正在拉取，等待者订阅 done
}

// listCacheTTL 是 /list 端点缓存的有效期。详见 Client.listMu 字段注释。
const listCacheTTL = 5 * time.Second

// listFetchInflight 用于 singleflight：当一个 goroutine 正在拉 /list 时，
// 后续 caller 阻塞在 done channel 上，全部拿到同一份 result。
//
// done 关闭后，result 字段被视为"已经写入完毕"——所有 reader 都安全读取。
// 写入端必须在 close(done) 之前完成 result 的赋值；本约束由
// fetchInboundsCached 内部的代码顺序保证（写 result → close(done)）。
type listFetchInflight struct {
	done   chan struct{}
	result listFetchResult
}

// listFetchResult 是一次 /list 拉取的成功 / 失败状态二选一。
//
// 设计成简单结构体而非 (slice, error) 二元组：方便在 listFetchInflight 中
// 一次性赋值后让 close(done) 之后的读取者拿到一致快照，避免"slice 已写
// 但 err 还未写"的撕裂窗口。
type listFetchResult struct {
	inbounds []Inbound
	err      error
}

// New 构造 Client。
//
// 不发起任何网络请求——token 模式无需登录，请求注入仅在每次 do 时执行。
// 这让"半填配置"场景仍能完成 Client 构造，引擎后续 Reload 即可恢复。
//
// 构造失败原因仅可能源于 TLS 配置异常——但 *http.Transport 把 TLS 错误
// 推迟到首次请求时报告，本函数不主动检测。
func New(cfg config.Xui) (*Client, error) {
	transport := &http.Transport{
		// 不直接复用 http.DefaultTransport 的两个原因：
		//  1) SkipTLSVerify 必须按 cfg 决定；复用 default 会污染全局；
		//  2) default 的 ResponseHeaderTimeout=0（无限等响应头），3x-ui 在
		//     xray 重启 / inbound 重载窗口内可能延迟返回 header，单次请求
		//     会撑满整个 httpClient.Timeout（默认 15s）。本 transport 在
		//     IO 各阶段设独立超时，让"读 header 前卡死""TLS 握手卡死"
		//     "100-Continue 卡死"三类异常各自尽快返回；端到端总上限仍由
		//     httpClient.Timeout 兜底（含 DNS/TCP dial / 响应体读取阶段，
		//     本 transport 不再额外约束）。
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: cfg.SkipTLSVerify, //nolint:gosec // 由运维配置显式启用
			MinVersion:         tls.VersionTLS12,
		},
		// 连接池：MaxIdleConns 是全局闲置上限；MaxIdleConnsPerHost 与
		// MaxConnsPerHost 是按目标 host 细化限制——3x-ui 通常只有一个
		// host，但中间件在多 bridge 部署下会出现请求叠加：4 bridges × 4
		// 同步循环 = 16 基础并发，再加 alive_sync 内部 GetClientIPs 的
		// fan-out（每 bridge 信号量 8）——单周期最坏可达 4 × 8 = 32 个
		// 同时挂起的 GetClientIPs 请求。MaxConnsPerHost=32 给典型部署留
		// 头空间，避免单条卡住的请求把后续所有 RPC 排队等到
		// httpClient.Timeout（队列等待计入端到端超时会让本周期连环 fail）。
		// 显式锁定上限同时阻断"连接风暴 → ephemeral 端口耗尽"边角失败
		// （极端下新连接被 EADDRINUSE 拒绝）。
		MaxIdleConns:        16,
		MaxIdleConnsPerHost: 8,
		MaxConnsPerHost:     32,
		IdleConnTimeout:     90 * time.Second,
		// 细粒度超时：仅约束 TLS 握手 / 服务端读完 header / 100-Continue
		// 三个 IO 子阶段。10s / 10s / 1s 远高于正常 3x-ui 响应时间
		// （< 200ms），远低于默认 client.Timeout=15s。
		// 注意：这些超时不覆盖 DNS/TCP dial 与响应体读取——后两者仍由
		// httpClient.Timeout 端到端兜底。
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

	httpClient := &http.Client{
		Transport: transport,
		Timeout:   time.Duration(cfg.TimeoutSec) * time.Second,
	}

	c := &Client{
		baseURL:    cfg.APIHost + cfg.BasePath,
		apiToken:   cfg.APIToken,
		httpClient: httpClient,
	}
	return c, nil
}

// GetInbound 调用 GET /panel/api/inbounds/get/:id 获取单个 inbound 详情。
//
// 返回值用途：本中间件仅在 user_sync 中使用其 RawSettings 字段（解析
// settings.clients[*] 做托管 client diff），不读 ClientStats。
//
// 重要陷阱（v0.5.1 文档化）：3x-ui 主线 service.GetInbound(id) 的实现仅
// 是 db.First(inbound, id)，**不调用 Preload("ClientStats") 也不调
// enrichClientStats**——所以本端点返回的 JSON 里 clientStats 字段是 nil
// 切片，UUID/SubID 也未被 enrich。如需"按 inbound 拉所有 client traffics"
// 应改用 GetClientTrafficsByInboundID（v0.5.1 起内部走 /list 端点）。
//
// 老版本（v0.5 之前）注释一度写"ClientStats 已被 enrichClientStats 填充"，
// 那是错的——只有 /list 端点对应的 GetInbounds(userId) / GetAllInbounds()
// 才 Preload + enrich，单条 GetInbound(id) 一直没这个待遇。user_sync 只
// 用 RawSettings 没暴露这个 bug；traffic_sync 与 alive_sync 之前调的是
// /getClientTrafficsById/:id（且把 inbound id 当 client UUID 传错），所以
// 也没踩到 GetInbound 的 ClientStats nil 陷阱——直到 v0.5.1 重新审视才发现。
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

// ErrInboundNotFound 表示通过 GET /panel/api/inbounds/list 没找到目标 inbound。
//
// 调用方拿到本错误应理解为"3x-ui 端确实没有该 inbound"——可能根因是 inbound
// 被运维删除、token 模式下 SetAPIAuthUser(GetFirstUser) 拿到的账户不拥有
// 该 inbound（多用户面板下的隐性差异）、或中间件 bridge 配置中的 XuiInboundID
// 写错。返回明确 sentinel 错误而非 nil 切片，是项目"单一正向路径"承诺的具体
// 落实——v0.5.x 时代用"返回 nil 当作无 client"的兜底语义已被移除：找不到
// 就是找不到，调用方显式失败让运维立即可见。
var ErrInboundNotFound = errors.New("xui: /list 中未找到目标 inbound（请检查 inbound 是否被删除 / bridge 的 xui_inbound_id 是否正确 / token 对应的账户是否拥有该 inbound）")

// GetClientTrafficsByInboundID 拉取指定 inbound 下所有 client 的流量统计。
//
// v0.5.1 关键修复（v0.5 及之前一直误用，导致 Xboard 端流量永远 0）：
//
//	3x-ui 主线 GET /panel/api/inbounds/getClientTrafficsById/:id 的 :id
//	**实际上是 client UUID（即 inbound.settings.clients[*].id 字段，UUID
//	字符串）**，并不是 inbound id。3x-ui InboundService.GetClientTrafficByID
//	内部 SQL（v3 主线 web/service/inbound.go）是：
//
//	    SELECT ... FROM client_traffics WHERE email IN (
//	        SELECT JSON_EXTRACT(client.value, '$.email') AS email
//	        FROM inbounds, JSON_EACH(JSON_EXTRACT(inbounds.settings, '$.clients')) AS client
//	        WHERE JSON_EXTRACT(client.value, '$.id') in (?)
//	    )
//
//	把 inbound id（如整数 1）以字符串 "1" 传进去，会与 settings.clients[*].id
//	（UUID 字符串如 "550e8400-e29b-41d4-a716-446655440000"）做相等匹配——
//	**永远匹配不到任何 client**，端点返回空数组。表现为 traffic_sync 循环
//	zero 次进入、alive_sync myEmails 永远为空——直接造成 Xboard 用户 u/d
//	永远 0 与 alive 永远空 push 这两个综合症状。
//
// 正确做法（v0.5.1 切换至 /list 端点）：
//
//	调 GET /panel/api/inbounds/list 走 3x-ui InboundService.GetInbounds(userId)；
//	该方法用 db.Preload("ClientStats") 装载 HasMany 子记录，且后续 enrichClientStats
//	会把每个 ClientStats 的 UUID/SubID 字段从 settings.clients 里反查填充——
//	返回的每条 inbound.ClientStats 已经是"完全可用"状态。注意三件事：
//
//	  a) 不能用 GET /panel/api/inbounds/get/:id：3x-ui 主线 GetInbound(id)
//	     仅 db.First(inbound, id)，**不 Preload ClientStats**。GORM 的
//	     HasMany 关联仅靠 struct 标签不会自动加载子记录——必须显式 Preload。
//	     该端点返回的 inbound.clientStats 始终是 nil 切片（user_sync 只读
//	     RawSettings 没暴露这一陷阱，直到 v0.5.1 修复 traffic 流量 bug 时
//	     被 Codex 审查发现）。详见 GetInbound 注释。
//
//	  b) /list 走 GetInbounds(userId) 用 WHERE user_id = ? **严格过滤**——
//	     登录账户只能看到自己 user_id 下的 inbound。token 模式下 3x-ui v3.0.0
//	     APIController.checkAPIAuth 通过 MatchApiToken 后调用
//	     session.SetAPIAuthUser(c, GetFirstUser())——所以 token 模式实际看到
//	     的是"第一个用户"的 inbound 集合。多用户面板下若运维不是 user_id=1
//	     的管理员、或存在多个 admin 账户，目标 inbound 必须属于 GetFirstUser
//	     才能被 /list 看到。本函数拿到不到目标 inbound 时显式返回
//	     ErrInboundNotFound，配合 v0.6 移除 sync 层心跳兜底，运维在
//	     traffic_sync 失败日志里能立刻看到根因。
//
//	  c) 体积权衡：拉所有 inbound 比单条 inbound 多带 N-1 倍 settings JSON。
//	     3x-ui 一个面板典型 inbound 数 < 10、单 inbound client 数 < 数千，
//	     /list 完整 JSON 通常 < 数百 KB；30s 一次的 traffic_sync 周期完全
//	     可承受。如未来出现极端规模（万级 client），下个版本可换"先拉
//	     /get/:id 取 settings.clients[*].email，再批量调 /getClientTraffics/:email"
//	     的 N+1 路径——但当前阶段无此需求。
//
// 调用方契约（v0.6 起调整）：
//   - 找到目标 inbound 且其 ClientStats 非空 → 真实流量列表
//   - 找到目标 inbound 但 ClientStats 为空 → nil 切片。注意 3x-ui 在
//     AddInboundClient 时通常会一并创建对应的 client_traffics 行（即使
//     up/down 都是 0），所以正常运行下 ClientStats 不应为空；如果观察到
//     非 0 用户数 inbound 仍返回空 stats，更可能是 inbound 刚建尚无 client、
//     运维手动重置过 stats 表、或 /list 端 Preload 失败等异常情形。
//   - /list 响应中找不到目标 inbound id → 返回 ErrInboundNotFound（v0.6
//     起从"返回 nil 切片"改为显式错误；理由详见 ErrInboundNotFound 注释）。
//   - 网络 / 鉴权 / 5xx 错误 → 走 c.do 的标准错误链，向调用方传播
func (c *Client) GetClientTrafficsByInboundID(ctx context.Context, inboundID int) ([]ClientTraffic, error) {
	// v0.5.3 起改走 fetchInboundsCached：单 Client 实例下，多 Bridge 在同
	// 一同步周期内对同一 host 的多次调用合并为一次 HTTP，降低对 3x-ui
	// 面板的压力。详见 Client.listMu 字段与 fetchInboundsCached 注释。
	inbounds, err := c.fetchInboundsCached(ctx)
	if err != nil {
		return nil, err
	}
	for _, ib := range inbounds {
		if ib.ID == inboundID {
			// 找到目标 inbound：返回其 ClientStats（已经被 Preload + enrich）。
			// 切片可能 nil（无 client）——调用方按 nil/empty 处理即可。
			return ib.ClientStats, nil
		}
	}
	// /list 不含目标 inbound id：v0.6 起返回 ErrInboundNotFound，让上层
	// sync 循环在日志里显式打出该错误根因（不再被静默兜底为"无在线 / 无
	// 流量"）。
	return nil, fmt.Errorf("inbound_id=%d：%w", inboundID, ErrInboundNotFound)
}

// fetchInbounds 直接发 GET /panel/api/inbounds/list 并解码 obj 字段。
//
// 调用方一般通过 fetchInboundsCached 间接调本函数，由后者负责 TTL 控制
// 与 singleflight 合并。本函数本身无锁、无缓存——是 fetchInboundsCached
// 的纯 IO 子步骤。
//
// 返回值约定：
//
//	a) 服务端 obj=null 或 body 空 → 返回 (nil, nil)；
//	b) 解码成功 → 返回 inbounds 切片（可能为空切片）；
//	c) 网络 / 鉴权 / 5xx 错误 → 走 c.do 的标准错误链，向调用方传播。
func (c *Client) fetchInbounds(ctx context.Context) ([]Inbound, error) {
	endpoint := "/panel/api/inbounds/list"
	raw, err := c.do(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	// 3x-ui 在用户没有 inbound 时返回 obj=null。统一收口为"无可用数据"
	// 而非解码错误——这不是失败兜底，而是协议合法响应（obj=null 是
	// commonResp 在"该用户名下无 inbound"场景的正常输出形式）。
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var inbounds []Inbound
	if err := json.Unmarshal(raw, &inbounds); err != nil {
		return nil, fmt.Errorf("解码 %s 响应：%w", endpoint, err)
	}
	return inbounds, nil
}

// fetchInboundsCached 是带 TTL + singleflight 的 fetchInbounds 包装。
//
// 行为分支：
//
//	a) listCache 仍在 TTL 内 → 直接返回缓存切片（同一切片在 reader 间共享，
//	   调用方不应原地 mutate；当前调用点都是只读遍历，安全）；
//	b) 已有 goroutine 在拉（listInflight 非 nil）→ 阻塞在其 done channel
//	   上，等 fetcher 关闭后读取共享 result。等待期间响应 ctx 取消，避免
//	   引擎退出时被卡住；
//	c) 否则当前 goroutine 触发新一轮拉取：
//	    1) 在 listMu 内创建 inflight 句柄、赋给 c.listInflight；
//	    2) 释放 listMu 后调 fetchInbounds——不持锁，避免阻塞其它 caller；
//	    3) 拉完后重新拿 listMu，更新 cache（仅 err==nil 时）+ 清 inflight，
//	       最后写 inflight.result 并 close(done)。
//
// 失败语义：fetch 失败时 listCache / listExpireAt 都不更新——保持上一轮
// 成功结果在剩余 TTL 内仍可命中，下一个 caller 看到 expireAt 已过会触发
// 新一轮 fetch。这避免了"瞬时网络抖动让缓存被脏数据替换"。
//
// ctx 选择：fetcher 用本次"首个 caller"的 ctx。该 caller 提前取消会让
// fetch 失败、所有 waiter 一起失败——这与"同一引擎所有 worker 共用 ctx
// 树"的现实一致；引擎退出时所有 worker 也都将退出，无需独立 detach ctx。
//
// 并发不变量：c.listInflight 仅在持有 listMu 时被读 / 写；从 nil → 非 nil
// 与 非 nil → nil 都在 listMu 临界区内完成。fetcher 释放 listMu 期间其它
// goroutine 看到非 nil inflight 并订阅 done channel；fetcher 拿回 listMu
// 把 inflight 清空之后再 close(done)——任何已订阅的 waiter 都能正常醒来。
func (c *Client) fetchInboundsCached(ctx context.Context) ([]Inbound, error) {
	c.listMu.Lock()
	if !c.listExpireAt.IsZero() && time.Now().Before(c.listExpireAt) {
		cached := c.listCache
		c.listMu.Unlock()
		return cached, nil
	}
	if c.listInflight != nil {
		inflight := c.listInflight
		c.listMu.Unlock()
		select {
		case <-inflight.done:
			return inflight.result.inbounds, inflight.result.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	// 当前 goroutine 触发拉取。
	inflight := &listFetchInflight{done: make(chan struct{})}
	c.listInflight = inflight
	c.listMu.Unlock()

	inbounds, err := c.fetchInbounds(ctx)

	c.listMu.Lock()
	if err == nil {
		c.listCache = inbounds
		c.listExpireAt = time.Now().Add(listCacheTTL)
	}
	c.listInflight = nil
	c.listMu.Unlock()

	// 写 result 必须在 close(done) 之前完成；close 之后 reader 可立即读 result。
	inflight.result = listFetchResult{inbounds: inbounds, err: err}
	close(inflight.done)

	return inbounds, err
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
//
// 协议合法响应形态（仅以下三种被视为成功，全部映射为 (nil, nil) 或数组）：
//
//	a) obj=null 或空 body                  → (nil, nil)（IP 表无该 email 行）
//	b) obj 为字符串 "No IP Record"          → (nil, nil)（3x-ui 主线 controller
//	                                          在 inboundService.GetInboundClientIps
//	                                          返回空 / err 时显式输出该字面值）
//	c) obj 为 JSON 数组（可空）              → 转切片返回
//
// 任何其它形态（数字 / 对象 / 其它字符串等）一律视为协议错误向上传播——
// 这不是失败兜底，是"无数据 vs 协议异常"语义边界的显式拦截，避免静默
// 把异常响应映射成空切片导致 alive_sync 少报用户。
func (c *Client) GetClientIPs(ctx context.Context, email string) ([]string, error) {
	endpoint := "/panel/api/inbounds/clientIps/" + url.PathEscape(email)
	raw, err := c.do(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	// 数组形态：直接解码返回。
	if raw[0] == '[' {
		var out []string
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, fmt.Errorf("解码 %s 响应：%w", endpoint, err)
		}
		return out, nil
	}
	// 字符串形态：仅接受 "No IP Record" 字面值；其它字符串一律报错。
	// 不接受任何其它字符串字面值（如未来 fork 改文案）：让协议变化时立刻
	// 显式失败，由维护者更新本判断而不是静默吞掉，与"单一正向路径"承诺
	// 一致。
	if raw[0] == '"' {
		var msg string
		if err := json.Unmarshal(raw, &msg); err != nil {
			return nil, fmt.Errorf("解码 %s 响应（字符串形态）：%w", endpoint, err)
		}
		if msg == "No IP Record" {
			return nil, nil
		}
		return nil, fmt.Errorf("解码 %s 响应：obj 为意外字符串 %q（仅接受数组 / null / \"No IP Record\"）", endpoint, msg)
	}
	// 数字 / 对象 / 布尔等其它类型：协议异常，立即报错。
	return nil, fmt.Errorf("解码 %s 响应：obj 既非数组也非 \"No IP Record\"：%s", endpoint, truncate(string(raw), 128))
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
// 单一正向路径：
//
//	1. 序列化 body（如有）；
//	2. 构造 *http.Request 并注入 Authorization: Bearer 头；
//	3. 发送一次；
//	4. 读响应 → 解码 commonResp → 返回 obj 或 *Error。
//
// 任意一步失败立即返回 *Error；不做重试 / 重登录 / 兜底响应——这是项目
// "单一正向路径"承诺的具体落实。
func (c *Client) do(ctx context.Context, method, endpoint string, body any) (json.RawMessage, error) {
	full := c.baseURL + endpoint

	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("序列化 %s body：%w", endpoint, err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, full, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("构造 %s 请求：%w", endpoint, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	// Bearer 头：每次请求都注入；3x-ui v3.0.0 APIController.checkAPIAuth
	// 在该头部前缀匹配 "Bearer " 后调 SettingService.MatchApiToken（constant
	// time compare），通过即 c.Set("api_authed", true)，CSRFMiddleware 短路。
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
