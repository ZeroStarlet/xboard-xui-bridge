// Package xui 实现对 3x-ui 面板 /panel/api/inbounds/* API 的客户端。
//
// 3x-ui 的 API 风格特征：
//
//  1. 所有响应包装在 {success:bool, msg:string, obj:any}；success=false 即业务错误。
//  2. inbound 的 settings / streamSettings / sniffing 是嵌入对象的 JSON 字符串
//     （二次 JSON），增删 client 时必须解码 → 改 → 重新编码。
//  3. 鉴权 v0.6 起仅 Bearer API Token 单通道，仅适配 3x-ui v3.0.0+：
//     每次请求都在 Authorization 头注入 "Bearer <APIToken>"；3x-ui 主线
//     APIController.checkAPIAuth 走 MatchApiToken 通道并让 CSRFMiddleware
//     短路。账号密码 / cookie / CSRF / TOTP 路径已彻底移除——详见
//     internal/config/config.go 文件头"3x-ui 鉴权说明"注释块。
//  4. 路径前缀含可配置的 webBasePath，所有 API 实际路径为
//     {host}{basePath}/panel/api/...，本包会自动拼接。
package xui

import (
	"encoding/json"
)

// commonResp 是 3x-ui 所有 API 响应的统一外壳。
//
// 业务错误（success=false）由本包抽出 msg 转成 *Error；调用方拿到的 obj
// 已经是经过 success 校验后的具体结构，无需再判断 success 字段。
type commonResp struct {
	Success bool            `json:"success"`
	Msg     string          `json:"msg"`
	Obj     json.RawMessage `json:"obj"`
}

// Inbound 是 GET /panel/api/inbounds/get/:id 返回的单条记录。
//
// 字段映射 3x-ui database/model.Inbound（部分），仅暴露中间件需要的字段。
// 未暴露字段会在 RawSettings 中保留原貌，便于调用方按需扩展。
type Inbound struct {
	ID             int             `json:"id"`
	Remark         string          `json:"remark"`
	Listen         string          `json:"listen"`
	Port           int             `json:"port"`
	Protocol       string          `json:"protocol"`        // vless / vmess / trojan / shadowsocks / hysteria2 ...
	Tag            string          `json:"tag"`             // inbound-<port>，xray 内部唯一标识
	Enable         bool            `json:"enable"`
	RawSettings    json.RawMessage `json:"settings"`        // 仍是 JSON 字符串而非对象
	StreamSettings json.RawMessage `json:"streamSettings"`  // 同上
	Sniffing       json.RawMessage `json:"sniffing"`        // 同上
	// ClientStats 仅在 /panel/api/inbounds/list 返回的 inbound 中可用——
	// 3x-ui 主线 InboundService.GetInbounds(userId) 显式 Preload("ClientStats")
	// 并跑 enrichClientStats 填充每条 stats 的 UUID/SubId 字段。
	// /panel/api/inbounds/get/:id 走的 GetInbound(id) 仅 db.First(inbound, id)，
	// **不 Preload**——该端点返回的 inbound JSON 里 clientStats 永远为 nil。
	// 中间件按 inbound id 拉所有 client traffics 必须走 /list（详见
	// client.go GetClientTrafficsByInboundID 注释及 v0.5.1 修复历史）。
	ClientStats []ClientTraffic `json:"clientStats"`
}

// ClientTraffic 映射 3x-ui xray.ClientTraffic 模型，是流量统计的源数据。
//
// 注意：在 GET inbound 返回的 clientStats 中，UUID/SubID 是 3x-ui 后端从
// inbound.settings.clients[*] 动态填充的，不直接落库。中间件读取它来
// 把 email 与上游 Xboard user_id 做反查。
type ClientTraffic struct {
	ID         int    `json:"id"`
	InboundID  int    `json:"inboundId"`
	Email      string `json:"email"`
	UUID       string `json:"uuid"`
	Up         int64  `json:"up"`
	Down       int64  `json:"down"`
	Total      int64  `json:"total"`
	ExpiryTime int64  `json:"expiryTime"`
	Enable     bool   `json:"enable"`
	Reset      int    `json:"reset"`
	LastOnline int64  `json:"lastOnline"`
}

// AddClientReq 是 POST /panel/api/inbounds/addClient 的请求体。
//
// 关键约定（来自 3x-ui controller/inbound.go addInboundClient）：
//
//   - 必须填 ID = 目标 inbound id；后端按此找到 inbound 并 merge clients。
//   - Settings 是一个新构造的 settings JSON 字符串，仅包含本次新增的 clients 数组。
//   - 后端不要求 streamSettings / sniffing / port / protocol，但若填写也不会报错。
//
// 一次可以批量新增多个 client：把它们都塞进 Settings.clients。
type AddClientReq struct {
	ID       int    `json:"id"`
	Settings string `json:"settings"`
}

// UpdateClientReq 是 POST /panel/api/inbounds/updateClient/:clientKey 的请求体。
//
// :clientKey 由协议适配器决定（详见 protocol.Adapter.KeyOf 注释）：
//
//	vless / vmess          → client.ID (UUID)
//	trojan                 → client.password
//	shadowsocks            → client.email
//	hysteria / hysteria2   → client.auth
//
// 调用方（xui.Client.UpdateClient）只负责把 KeyOf 输出做 url.PathEscape 后拼到 URL，
// 不再做任何协议判断。
type UpdateClientReq struct {
	ID       int    `json:"id"`       // 目标 inbound id
	Settings string `json:"settings"` // 单个 client 的 settings JSON（仅 clients 数组）
}

// ClientSettings 是 inbound.settings.clients 数组中单个对象的中性表达。
//
// 不直接使用 *json.RawMessage 是因为字段名因协议差异巨大：
//
//	vless / vmess         → id        + email        + flow            + …
//	trojan                → password  + email        + flow            + …
//	shadowsocks (clients) → password  + method/email + …
//	hysteria2             → password / auth + email + …
//
// 协议适配器会把 ClientSettings 转换为对应协议的 JSON 形态，本结构仅作为
// 中间桥接 DTO，避免 xui 包内部塞满协议分支。
type ClientSettings struct {
	Email      string `json:"email"`
	Enable     bool   `json:"enable"`
	ExpiryTime int64  `json:"expiryTime,omitempty"`
	TotalGB    int64  `json:"totalGB,omitempty"`
	LimitIP    int    `json:"limitIp,omitempty"`
	Reset      int    `json:"reset,omitempty"`
	SubID      string `json:"subId,omitempty"`

	// ID：vless/vmess 用作 UUID。
	ID string `json:"id,omitempty"`
	// Flow：vless 专用。
	Flow string `json:"flow,omitempty"`
	// Password：trojan / shadowsocks 等协议的密码字段。
	Password string `json:"password,omitempty"`
	// Auth：hysteria / hysteria2 协议的鉴权字段。3x-ui 在 inbound.protocol
	// 为 hysteria / hysteria2 时按 client.auth 查找，与 password 字段语义相互独立。
	Auth string `json:"auth,omitempty"`
	// Method：仅 shadowsocks per-user 模式有意义。
	Method string `json:"method,omitempty"`
}

// Error 是本包对外暴露的统一错误类型。
//
// 错误来源有三种：
//
//	a) 网络层错误（HTTPStatus=0）：连接失败 / 超时；
//	b) HTTP 非 2xx：HTTPStatus=具体码，Body 截断保留；
//	c) success=false 业务错误：HTTPStatus=200，Msg 取 commonResp.Msg。
//
// 调用方可用 errors.As 判别后再决定终止 / 上报告警。v0.6 起本包及上层
// sync 循环都不做"鉴权失效自动重试 / 重登录"——任何错误都直接传播，由
// 上层日志路径显式呈现，符合"单一正向路径"承诺。
type Error struct {
	HTTPStatus int
	Endpoint   string
	Msg        string
	RawBody    string
}

// Error 实现 error 接口；尽量信息密集，方便日志检索。
func (e *Error) Error() string {
	return e.Endpoint + " status=" + statusStr(e.HTTPStatus) + " msg=" + quoted(e.Msg) + " body=" + quoted(e.RawBody)
}

// statusStr 把 HTTP 状态码格式化为可读字符串；0 表示传输层错误。
func statusStr(code int) string {
	if code == 0 {
		return "<network>"
	}
	const digits = "0123456789"
	if code < 1000 && code >= 0 {
		return string([]byte{digits[code/100], digits[(code/10)%10], digits[code%10]})
	}
	// 极少出现 4 位以上状态码。
	out := []byte{}
	for code > 0 {
		out = append([]byte{digits[code%10]}, out...)
		code /= 10
	}
	return string(out)
}

// quoted 把字符串裹上引号便于日志定位边界；同时把内部双引号转义。
// 不使用 strconv.Quote 是为了避免对中文字符做 \u 转义降低可读性。
func quoted(s string) string {
	out := make([]byte, 0, len(s)+2)
	out = append(out, '"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' || c == '\\' {
			out = append(out, '\\', c)
			continue
		}
		out = append(out, c)
	}
	out = append(out, '"')
	return string(out)
}
