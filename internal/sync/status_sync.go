package sync

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/xboard-bridge/xboard-xui-bridge/internal/xboard"
)

// syncStatus 把节点负载信息上报到 Xboard /status。
//
// 数据来源：仅 3x-ui /panel/api/server/status——它跑在节点真机上，cpu /
// mem / swap / disk 数据准确。
//
// 单一正向路径承诺（v0.6 起）：拉不到 / 解码失败 / 上报失败 任一步直接返
// 回 error 让上层 sync 循环走 WARN 日志路径，下个周期再尝试。v0.5.x 时代
// 的"拉不到 status 就 pushHeartbeat 上报中间件自身 runtime 占位负载"已被
// 移除——那是典型的"失败兜底"，会让运维看到的"节点负载条非空"与"3x-ui
// 端口确实通"两件事产生伪相关，掩盖真正的根因（面板宕机 / 鉴权失效 /
// 网络分区等）。改为显式失败后，运维看到 WARN 立即可知"3x-ui server
// status 拿不到"——这才是 status 上报循环应当传达的信号。
//
// **重要澄清（v0.5 修正，v0.6 沿用）**：本上报仅刷新 SERVER_*_LAST_LOAD_AT
// 缓存，与节点"在线 / 异常"判定（SERVER_*_LAST_PUSH_AT，由 traffic_sync
// 维护）**完全无关**。v0.4 之前老注释一度写"心跳保活靠 status_sync"——
// 那是错的。真正的节点活跃心跳在 traffic_sync 中通过实际差量 push 维持，
// 详见 traffic_sync.go 文件头注释。
//
// 为何没有 swap / disk 真实值：
//
//	3x-ui server/status 返回的字段集合包含 mem / swap / disk 三对 current /
//	total，本函数严格按这三对解码。中间件没有 root 权限直接读 /proc/swaps、
//	/proc/diskstats（Windows 节点更不可能），所以 swap / disk 完全依赖 3x-ui
//	提供。3x-ui v3.0.0 主线 server.go GetStatus 仍按上述三对返回；任意子项
//	缺失 / 解析失败均向上返回 error——v0.5.x 时代"零值兜底继续推送"的降级
//	行为已被移除，这是项目"单一正向路径"承诺的具体落实。
func (w *bridgeWorker) syncStatus(ctx context.Context) error {
	// 取本次 tick 的 trace 化 logger（含 loop=status_sync + trace_id）；
	// 详见 engine.go runStep 中的 trace_id 注入逻辑。本函数所有 INFO /
	// DEBUG 共享同一组 attrs，便于按 trace_id 串联本周期所有日志。
	log := loggerFromCtx(ctx, w.log)

	rawJSON, err := w.xuiC.GetServerStatus(ctx)
	if err != nil {
		return fmt.Errorf("拉取 3x-ui server status：%w", err)
	}

	report, err := parseXuiStatus(rawJSON)
	if err != nil {
		return fmt.Errorf("解析 3x-ui server status：%w", err)
	}

	if err := w.xboardC.PushStatus(ctx, w.cfg.XboardNodeID, w.cfg.XboardNodeType, report); err != nil {
		return fmt.Errorf("Xboard /status：%w", err)
	}
	log.Debug("status 上报完成", "cpu", report.CPU, "mem_used", report.Mem.Used)
	return nil
}

// parseXuiStatus 把 3x-ui /panel/api/server/status 的响应解码为 xboard.StatusReport。
//
// 3x-ui v3.0.0 端字段（与 v2.x 兼容）：
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
// CPU 字段双形态兼容（数字 / 字符串）：3x-ui 历史版本曾把百分比序列化为
// 字符串；v3.0.0 主线已统一为数字，但 fork / 旧补丁可能仍走字符串路径。
// 这是协议合法多形态而非"失败兜底"——两种形态都是合法成功响应。
//
// 失败语义（v0.6 单一正向路径承诺）：响应非 JSON / 顶层结构异常 / cpu /
// mem / swap / disk 任一字段解析失败 → 返回 error 让上层显式失败，绝不
// 返回零值兜底继续推送。
func parseXuiStatus(raw json.RawMessage) (xboard.StatusReport, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return xboard.StatusReport{}, fmt.Errorf("响应 obj 为空（3x-ui server status 不应返回 null）")
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

	cpu, err := parseFloat(probe.CPU)
	if err != nil {
		return xboard.StatusReport{}, fmt.Errorf("解析 cpu 字段：%w", err)
	}
	// CPU 越界严格化（v0.6 单一正向路径承诺）：
	// v0.5.x 时代会把 cpu < 0 / >100 静默夹紧后继续推送——掩盖 3x-ui 端
	// 真实故障（伪百分比 / 字段语义错位 / fork 字符集差异）。v0.6 起
	// fail-fast：让运维立即看到"3x-ui 上报的 cpu 字段超出 [0,100]"是协议
	// 异常而不是中间件需要兜底的"小概率脏数据"。允许 0 与 100 边界值。
	if cpu < 0 || cpu > 100 {
		return xboard.StatusReport{}, fmt.Errorf("解析 cpu 字段：值 %v 超出 [0, 100]（3x-ui 上报异常）", cpu)
	}

	mem, err := parseStatusPair("mem", probe.Mem)
	if err != nil {
		return xboard.StatusReport{}, err
	}
	swap, err := parseStatusPair("swap", probe.Swap)
	if err != nil {
		return xboard.StatusReport{}, err
	}
	disk, err := parseStatusPair("disk", probe.Disk)
	if err != nil {
		return xboard.StatusReport{}, err
	}

	return xboard.StatusReport{
		CPU:  cpu,
		Mem:  mem,
		Swap: swap,
		Disk: disk,
	}, nil
}

// parseStatusPair 解析 3x-ui mem / swap / disk 子对象为 xboard.StatusPair。
//
// 协议形态（与 3x-ui v3.0.0 主线 server.go GetStatus 输出一致）：
//
//	{"current": <uint64>, "total": <uint64>}
//
// 历史 fork 偶现 {"used": <uint64>, "total": <uint64>}（与 Xboard 自身约定
// 的字段名相同）；本函数用指针字段同时探测两个键，二选一——这是"兼容
// 多形态合法字段名"，不是兜底。
//
// 失败语义（v0.6 单一正向路径承诺）：
//
//	a) obj=null / 空 body              → (零值, nil)。这是协议显式允许的
//	   "该子项未上报"形态（如无 swap 的节点 swap=null 是合法的）。
//	b) JSON 解码失败                    → error 向上传播。
//	c) total / current 任一为指针非 nil 且对端键存在 → 校验：
//	     - current 与 used 同时出现（同 fork 双键并存）→ error，避免歧义；
//	     - total 缺失但 current/used 存在（半填）       → error；
//	     - current/used 缺失但 total 存在（半填）       → error；
//	     - used > total（数据自相矛盾）                 → error，**不再
//	       静默夹紧**——v0.5.x 的 "used = total" 兜底已删除，让 3x-ui
//	       端真实异常显式可见。
//	d) 全部为 0（{"current":0,"total":0}）→ (零值, nil)。等价于子项未上报；
//	   不是错误。
//
// fieldName 用于错误信息上下文，便于运维快速定位是哪个字段炸了。
func parseStatusPair(fieldName string, raw json.RawMessage) (xboard.StatusPair, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return xboard.StatusPair{}, nil
	}
	// 用指针字段区分"键存在但值为 0"与"键根本未提供"。Go encoding/json
	// 对 *uint64 的语义：JSON 中无该键 → 保持 nil；JSON 中显式 null 或
	// 数字 → 分配一个 *uint64 并填值（null 时仍 nil）。本函数借此实现
	// 严格"半填检测"。
	var probe struct {
		Current *uint64 `json:"current"`
		Used    *uint64 `json:"used"`
		Total   *uint64 `json:"total"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return xboard.StatusPair{}, fmt.Errorf("解析 %s 字段：%w", fieldName, err)
	}

	// 双键并存（current 与 used 同时出现）→ 协议歧义，立即报错。
	if probe.Current != nil && probe.Used != nil {
		return xboard.StatusPair{}, fmt.Errorf("解析 %s 字段：current 与 used 同时存在（3x-ui 协议歧义）", fieldName)
	}

	// 取实际"已用"指针：current 优先；其后再看 used；若都没提供则 hasUsed=false。
	var (
		usedVal  uint64
		hasUsed  bool
	)
	switch {
	case probe.Current != nil:
		usedVal = *probe.Current
		hasUsed = true
	case probe.Used != nil:
		usedVal = *probe.Used
		hasUsed = true
	}

	hasTotal := probe.Total != nil
	totalVal := uint64(0)
	if hasTotal {
		totalVal = *probe.Total
	}

	// 全部缺失（{} 或仅有无关字段）→ 视为"该子项未上报"，与 null 等价。
	if !hasUsed && !hasTotal {
		return xboard.StatusPair{}, nil
	}
	// 半填（仅一边）→ 严格报错。3x-ui 主线两键成对出现，半填 = 协议异常。
	if !hasUsed || !hasTotal {
		return xboard.StatusPair{}, fmt.Errorf("解析 %s 字段：current/used 与 total 必须成对出现（缺其一）", fieldName)
	}

	// used > total：数据自相矛盾，立即报错（v0.6 起不再夹紧到 total）。
	if usedVal > totalVal {
		return xboard.StatusPair{}, fmt.Errorf("解析 %s 字段：used=%d > total=%d（3x-ui 上报数据自相矛盾）", fieldName, usedVal, totalVal)
	}

	return xboard.StatusPair{Total: totalVal, Used: usedVal}, nil
}

// parseFloat 把 json.RawMessage 解析为 float64。
//
// 兼容两种合法序列化形态：
//
//	a) 12.34         （number）— v3.0.0 主线
//	b) "12.34"       （string）— 部分旧 fork 把百分比序列化为字符串
//
// 失败语义（v0.6 单一正向路径承诺）：
//   - 空 RawMessage / null → 返回 error（cpu 字段不应缺失，缺失视为协议异常）
//   - 既非 number 也非 string → 返回 error
//   - string 内容非数字 → 返回 error
//
// 调用方按 syncStatus 主路径整体失败处理——不再降级为零值。
func parseFloat(raw json.RawMessage) (float64, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, fmt.Errorf("字段缺失或为 null")
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

// parseFloatString 把字符串解析为 float64。
//
// 失败语义：空串 / 非数字字符串均返回 error——上层 parseFloat 把错误向上
// 传播让 syncStatus 整体失败。v0.5.x 的"空串静默归 0"已被删除（对 cpu
// 字段而言空串是异常而非合法零值）。
func parseFloatString(s string) (float64, error) {
	if s == "" {
		return 0, fmt.Errorf("空字符串不是合法数字")
	}
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
