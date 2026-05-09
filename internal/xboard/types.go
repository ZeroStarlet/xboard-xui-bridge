// Package xboard 实现对 Xboard /api/v2/server/* 节点 API 的客户端。
//
// 设计要点：
//
//  1. Client 仅持有"全局参数"（baseURL、token、HTTP 实例），不绑定任何
//     单一 NodeID。每次调用 API 由调用方传入 NodeID + NodeType，从而
//     一个 Client 实例可服务多个 Bridge，节省连接、统一观测。
//
//  2. 所有上行数据均以 application/json 提交；token 与 node_id / node_type 放
//     query string，避免与业务 body（如 /push 的 traffic map）字段冲突。
//
//  3. 错误使用 *Error 包装：Xboard 返回 HTTP 200 但 body 含 message 时
//     视为业务错误；HTTP 非 200 视为传输层错误。两者统一由本包暴露，
//     业务层可用 errors.As 精细判断。
package xboard

import (
	"encoding/json"
	"strconv"
)

// User 是 GET /api/v2/server/user 单个返回项。
//
// 字段映射 ServerService.getAvailableUsers 的输出（Xboard 端只暴露这四列）：
//
//	id          : 用户主键，上报流量时作为 traffic map 的 key（字符串化）。
//	uuid        : 客户端密码（除 SS-2022 外所有协议直接拿它当 password）。
//	speed_limit : bps；0 表示不限速。
//	device_limit: 最大设备数；0 表示不限。
type User struct {
	ID          int64  `json:"id"`
	UUID        string `json:"uuid"`
	SpeedLimit  int    `json:"speed_limit"`
	DeviceLimit int    `json:"device_limit"`
}

// UserListResp 是 GET /api/v2/server/user 的响应包装。
//
// Xboard 同时发 ETag 头；本中间件目前不使用 304 优化，但保留 ETag 字段
// 以备未来引入差量缓存（设计点见 README "未来方向"）。
type UserListResp struct {
	Users []User `json:"users"`
	ETag  string `json:"-"`
}

// PushTraffic 是 POST /api/v2/server/push 的"上行 / 下行"数据载体。
//
// Xboard 端 ServerService::processTraffic 期望的 JSON 形态：
//
//	{
//	  "<user_id>": [up_bytes, down_bytes],
//	  "<user_id>": [up_bytes, down_bytes],
//	  ...
//	}
//
// 注意：key 必须是数字字符串（PHP array_filter 默认保留 key），value 是
// 长度为 2 的数字数组（is_array && count==2 && is_numeric），不符合条件
// 的项会被 array_filter 直接丢弃，不会有任何错误反馈给中间件——所以严格
// 遵守该形态是计费正确的前提。
type PushTraffic map[string][2]int64

// MarshalJSON 把 PushTraffic 序列化为对象形态。
//
// 即使为空，也输出 {} 而不是 null：Xboard 端 array_filter 对 null 会抛
// TypeError，输出 {} 视为 noop。
func (p PushTraffic) MarshalJSON() ([]byte, error) {
	if len(p) == 0 {
		return []byte("{}"), nil
	}
	// 用 map[string][2]int64 别名走标准 marshal 即可。
	type aliased map[string][2]int64
	return json.Marshal(aliased(p))
}

// Set 是构造 PushTraffic 的便捷方法：把 (uid, up, down) 写入 map。
//
// 若 uid 重复出现，后写入的覆盖前者——业务侧通常按 email 唯一聚合上报，
// 不应出现重复 uid，本方法不显式拒绝以保持简单。
func (p PushTraffic) Set(userID int64, upBytes, downBytes int64) {
	p[strconv.FormatInt(userID, 10)] = [2]int64{upBytes, downBytes}
}

// AliveMap 是 alive 上报的字段；key 为 user_id 字符串，value 为该用户当前在线 IP 数组。
//
// 例：{"1": ["1.2.3.4", "5.6.7.8"], "2": ["9.10.11.12"]}
//
// 用 string key 是因为 JSON 对象 key 必须是字符串，这是 Xboard ServerService
// processAlive 端的硬性要求；不做 int 转换会导致 PHP 解析失败。
type AliveMap map[string][]string

// AliveListResp 是 GET /api/v2/server/alivelist 的响应。
// alive 字段含义与 AliveMap 一致：受设备限制的用户当前已被记录的 IP 列表。
type AliveListResp struct {
	Alive AliveMap `json:"alive"`
}

// StatusReport 是 POST /api/v2/server/status 的请求体。
//
// 注意：Xboard 校验规则要求 cpu 在 [0,100]，total/used 必须为非负整数。
// 业务层在调用前应当确保字段已填充；零值字段也会被序列化提交。
type StatusReport struct {
	CPU  float64    `json:"cpu"`
	Mem  StatusPair `json:"mem"`
	Swap StatusPair `json:"swap"`
	Disk StatusPair `json:"disk"`
}

// StatusPair 描述 total / used 两元组（单位：字节）。
type StatusPair struct {
	Total uint64 `json:"total"`
	Used  uint64 `json:"used"`
}

// Error 是本包对外暴露的统一错误类型，承载 HTTP 状态码与 Xboard 端 message。
//
// 使用方式：
//
//	if err := client.PushTraffic(...); err != nil {
//	    var xerr *xboard.Error
//	    if errors.As(err, &xerr) && xerr.HTTPStatus == http.StatusUnauthorized {
//	        // 鉴权过期处理
//	    }
//	}
type Error struct {
	HTTPStatus int    // HTTP 状态码
	Endpoint   string // 出错的 API 路径
	Message    string // Xboard 端 message 字段（若有）
	RawBody    string // 截断后的原始响应（最多 512 字节，便于日志追踪）
}

// Error 实现 error 接口；输出尽量信息密集，便于 grep。
func (e *Error) Error() string {
	if e.Message != "" {
		return e.Endpoint + " HTTP " + httpStatusText(e.HTTPStatus) + ": " + e.Message
	}
	return e.Endpoint + " HTTP " + httpStatusText(e.HTTPStatus) + ": " + e.RawBody
}

// httpStatusText 将状态码格式化为 "200" / "401" 等字符串。
// 不引入 net/http.StatusText 避免给非 HTTP 错误流派的调用者造成歧义。
func httpStatusText(code int) string {
	if code <= 0 {
		return "<n/a>"
	}
	return strconv.Itoa(code)
}
