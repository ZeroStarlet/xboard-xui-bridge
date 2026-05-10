package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"

	"github.com/xboard-bridge/xboard-xui-bridge/internal/xboard"
)

// syncStatus 把节点负载信息上报到 Xboard /status。
//
// 数据来源：
//
//	a) 优先从 3x-ui /panel/api/server/status 拿——它跑在节点真机上，CPU/内存数据准确。
//	b) 拉取或解析失败时退化为"中间件自身的 runtime stats"（仅 CPU 占用近似 0%、
//	   mem 仅含 Go runtime 用量）。这种降级模式让 Xboard 后台运维仍能看到节点
//	   有上报（SERVER_*_LAST_LOAD_AT 缓存被刷新 + 负载数据非空），不至于因一次
//	   status 失败把节点负载条标灰。
//
// **重要澄清（v0.5 修正）**：本上报仅刷新 SERVER_*_LAST_LOAD_AT 缓存，与节点
// "在线 / 异常"判定（SERVER_*_LAST_PUSH_AT，由 traffic_sync 维护）**完全无关**。
// v0.4 之前老注释一度写"心跳保活靠 status_sync"——那是错的。真正的节点活跃心跳
// 在 traffic_sync 中通过 [0, 0] 占位 push 维持，详见 traffic_sync.go 文件头注释。
//
// 为何没有 swap / disk 真实值：
//
//	3x-ui server/status 返回的字段集合不含 swap 与 disk；中间件没有 root 权限直接
//	读 /proc/swaps、/proc/diskstats（Windows 节点更不可能）。统一把 swap/disk 上报
//	为 0/0 即可——Xboard 校验只要求非负整数，业务上"未上报具体值"。
func (w *bridgeWorker) syncStatus(ctx context.Context) error {
	// 直接尝试从 3x-ui 拉 status；任何失败都退化为运行时心跳，不返回错误。
	rawJSON, err := w.xuiC.GetServerStatus(ctx)
	if err != nil {
		w.log.Warn("拉取 3x-ui server status 失败，降级为心跳模式", "err", err)
		return w.pushHeartbeat(ctx)
	}

	report, err := parseXuiStatus(rawJSON)
	if err != nil {
		w.log.Warn("解析 3x-ui server status 失败，降级为心跳模式", "err", err)
		return w.pushHeartbeat(ctx)
	}

	if err := w.xboardC.PushStatus(ctx, w.cfg.XboardNodeID, w.cfg.XboardNodeType, report); err != nil {
		return fmt.Errorf("Xboard /status：%w", err)
	}
	w.log.Debug("status 上报完成", "cpu", report.CPU, "mem_used", report.Mem.Used)
	return nil
}

// pushHeartbeat 在拉不到节点真实状态时，构造一份"中间件自身可观测"的最小状态上报。
//
// **命名澄清（v0.5）**：函数名中的 "heartbeat" 仅指 status 子循环的"降级上报"
// 语义——保证 Xboard 后台运维看到节点负载条非空——与节点"在线 / 异常"判定无关。
// 节点 STATUS_ONLINE / STATUS_ONLINE_NO_PUSH 切换由 traffic_sync 维护
// （详见 traffic_sync.go 文件头注释）；本函数与该判定完全解耦。
//
// 关键约束：所有字段必须非负，cpu ∈ [0, 100]——Xboard 校验失败会返回 422，
// 比"完全不报"更糟糕。这里宁可上报 0 也不省略字段。
func (w *bridgeWorker) pushHeartbeat(ctx context.Context) error {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	report := xboard.StatusReport{
		CPU: 0, // 中间件没有跨平台获取 CPU 占用率的轻量手段；保守上报 0
		Mem: xboard.StatusPair{
			// Sys 是 Go runtime 已向系统申请的总字节数；可作为非常粗略的"内存使用"代理。
			// total 字段无法准确知道整机内存——这里用 Sys * 4 作为"分母"占位，避免分子高于分母。
			Total: ms.Sys * 4,
			Used:  ms.Sys,
		},
		Swap: xboard.StatusPair{Total: 0, Used: 0},
		Disk: xboard.StatusPair{Total: 0, Used: 0},
	}
	if err := w.xboardC.PushStatus(ctx, w.cfg.XboardNodeID, w.cfg.XboardNodeType, report); err != nil {
		return fmt.Errorf("Xboard /status (heartbeat)：%w", err)
	}
	return nil
}

// parseXuiStatus 把 3x-ui /panel/api/server/status 的响应解码为 xboard.StatusReport。
//
// 3x-ui 端字段（以 v2.x 为准）：
//
//	{
//	  "cpu": 15.5,
//	  "cpuCores": 4,
//	  "logicalPro": 8,
//	  "cpuSpeedMhz": 3000,
//	  "mem":  { "current": ..., "total": ... },
//	  "swap": { "current": ..., "total": ... },
//	  "disk": { "current": ..., "total": ... },
//	  "loads": [...], "tcpCount": ..., "udpCount": ..., "uptime": ...
//	}
//
// 历史更早版本可能返回扁平字段（mem 直接是百分比浮点），本函数对两种形态都做尝试，
// 解码失败则返回错误由调用方降级处理。
func parseXuiStatus(raw json.RawMessage) (xboard.StatusReport, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return xboard.StatusReport{}, fmt.Errorf("空响应")
	}

	var probe struct {
		CPU  json.RawMessage `json:"cpu"`
		Mem  json.RawMessage `json:"mem"`
		Swap json.RawMessage `json:"swap"`
		Disk json.RawMessage `json:"disk"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return xboard.StatusReport{}, fmt.Errorf("解码顶层：%w", err)
	}

	cpu, _ := parseFloat(probe.CPU)
	if cpu < 0 {
		cpu = 0
	}
	if cpu > 100 {
		cpu = 100
	}

	return xboard.StatusReport{
		CPU:  cpu,
		Mem:  parseStatusPair(probe.Mem),
		Swap: parseStatusPair(probe.Swap),
		Disk: parseStatusPair(probe.Disk),
	}, nil
}

// parseStatusPair 兼容两种形态：
//
//	a) {"current": ..., "total": ...} (3x-ui v2.x 形态)
//	b) {"used":    ..., "total": ...} (Xboard 自身约定的形态，用于直通 fallback)
//
// 两者都不命中时返回零值；上层视为"该子项未上报"。
func parseStatusPair(raw json.RawMessage) xboard.StatusPair {
	if len(raw) == 0 || string(raw) == "null" {
		return xboard.StatusPair{}
	}
	var probe struct {
		Current uint64 `json:"current"`
		Used    uint64 `json:"used"`
		Total   uint64 `json:"total"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return xboard.StatusPair{}
	}
	used := probe.Used
	if used == 0 {
		used = probe.Current
	}
	if used > probe.Total && probe.Total > 0 {
		// 防御：3x-ui 偶发返回 used > total（百分比误用），夹紧到 total 以避免 Xboard 拒收。
		used = probe.Total
	}
	return xboard.StatusPair{Total: probe.Total, Used: used}
}

// parseFloat 把 json.RawMessage 解析为 float64。
//
// 兼容两种序列化：
//
//	a) 12.34         （number）
//	b) "12.34"       （string；某些版本 3x-ui 把百分比序列化为字符串）
func parseFloat(raw json.RawMessage) (float64, error) {
	if len(raw) == 0 {
		return 0, nil
	}
	// 先尝试 number。
	var n float64
	if err := json.Unmarshal(raw, &n); err == nil {
		return n, nil
	}
	// 再尝试 string。
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return parseFloatString(s)
	}
	return 0, fmt.Errorf("既非 number 也非 string：%s", string(raw))
}

// parseFloatString 是把字符串解析为 float64 的轻量版本，避免引入 strconv 在该
// 文件中的额外可读性负担——但实现还是用 strconv，因为手写浮点解析复杂度过高。
func parseFloatString(s string) (float64, error) {
	if s == "" {
		return 0, nil
	}
	// 直接借用 encoding/json：它能把字符串内部数字解出来。
	wrapped := []byte(s)
	if wrapped[0] != '-' && (wrapped[0] < '0' || wrapped[0] > '9') {
		// 非法首字符直接拒绝。
		return 0, fmt.Errorf("非数字字符串 %q", s)
	}
	var n float64
	if err := json.Unmarshal(wrapped, &n); err != nil {
		return 0, err
	}
	return n, nil
}
