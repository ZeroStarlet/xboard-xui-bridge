package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// 全部响应体的统一外壳。
//
// 设计动机：v0.1 时代直接 json.Marshal struct 让前端"猜测"返回字段名；
// 切到统一外壳后前端只需关心 "data" / "error" 两路即可，零心智成本。
type apiResponse struct {
	// Data 仅在成功路径上填值；任意 JSON 序列化能成功的类型都可。
	Data any `json:"data,omitempty"`
	// Error 仅在失败路径上填值；前端先检查 error 再处理 data。
	Error *apiError `json:"error,omitempty"`
}

// apiError 是统一错误负载。
//
// 选 code 字符串而非 HTTP status：HTTP status 已在 status code header 里，
// code 字符串让前端"按场景"做精细化处理（如 "session_expired" → 跳登录）。
type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// 错误码常量。前端依赖这些字符串做"场景识别"——更名要在前端同步改。
//
// 命名约定：
//   - 全小写 + 下划线；
//   - 用动词或形容词描述"什么不对"，而不是把 HTTP 状态翻译成中文。
const (
	errCodeBadRequest      = "bad_request"
	errCodeUnauthorized    = "unauthorized"
	errCodeForbidden       = "forbidden"
	errCodeNotFound        = "not_found"
	errCodeConflict        = "conflict"
	errCodeInternal        = "internal_error"
	errCodeMethodNotAllowed = "method_not_allowed"
)

// 请求体最大字节数：1 MiB。
//
// 目前最重的请求是 PATCH /api/settings（几十个字段），实际不超过 8 KB；
// 1 MiB 给未来扩展留充足余量，同时阻止恶意 client 用超大 body 拖垮服务。
const maxRequestBodyBytes = 1 << 20

// writeJSON 把数据写为 JSON 响应。
//
// 调用约定：
//   - status 是 HTTP 状态码（200/201/204/...）；
//   - data 可以是任意 JSON-serializable 类型，nil 时 body 为 `{}`；
//   - 写入失败仅打 ERROR 日志（连接已断不能再写）；不向调用方传播。
//
// 注意：本函数不会调用 w.WriteHeader 之外的任何 header 设置，调用方应
// 在 writeJSON 之前完成所有 header 修改（如 Set-Cookie）。
func (s *Server) writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	resp := apiResponse{Data: data}
	if data == nil {
		resp.Data = struct{}{}
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false) // 配置字段中可能含 < / >；不需要转义。
	if err := enc.Encode(resp); err != nil {
		s.log.Error("写入 JSON 响应失败", "err", err)
	}
}

// writeError 写一个统一外壳的错误响应。
//
// 调用约定：
//   - status 一般是 4xx / 5xx；
//   - code 用上面的 errCodeXxx 常量；
//   - message 中文，给运维看；不应包含敏感 token / 哈希等。
func (s *Server) writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	resp := apiResponse{Error: &apiError{Code: code, Message: message}}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.log.Error("写入错误响应失败", "err", err)
	}
}

// readJSON 把请求体反序列化到 dst。
//
// 行为：
//   - 限制 body 最大 maxRequestBodyBytes；超出返回错误（client 行为不端）。
//   - DisallowUnknownFields：拼错的字段会报错而不是被静默忽略——这与
//     v0.1 yaml 的 KnownFields(true) 同等护栏，避免运维以为字段已生效。
//   - body 为空（content-length=0）返回 io.EOF 包装错误；调用方按需视为
//     "无字段"或"必填缺失"。
//   - 拒绝多个顶层 JSON 值（如 `{}{}`）：第一次 Decode 后再 Decode 一次
//     占位结构，期望 io.EOF。json.Decoder.More() 仅检查"当前 array/object
//     内是否还有元素"，对顶层多值无效；必须用第二次 Decode 才能可靠拒绝。
//
// 失败时返回的 error 直接面向 handler——handler 用 errors.Is 区分超大 /
// 拼错 / EOF 三类常见错误并转成对应 HTTP 状态。
func readJSON(r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, maxRequestBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		// EOF 单独包装：让 handler 区分"完全空 body" vs "解析错误"。
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("readJSON: 请求体为空：%w", err)
		}
		return fmt.Errorf("readJSON: %w", err)
	}
	// 第二次 Decode 期望 io.EOF；若仍能解出值即"顶层多对象"，拒绝。
	// 用 json.RawMessage 接收：占位轻量，不需要任何字段定义。
	var trailing json.RawMessage
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		// err == nil 表示真的解出了第二个值；err != nil 但不是 EOF 则可能
		// 是"额外字节但又解不成 JSON"——同样视为客户端格式错误。
		return errors.New("readJSON: 请求体含多个顶层 JSON 值")
	}
	return nil
}
