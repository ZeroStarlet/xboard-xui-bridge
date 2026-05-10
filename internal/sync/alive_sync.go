package sync

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
	stdsync "sync"

	"github.com/xboard-bridge/xboard-xui-bridge/internal/protocol"
	"github.com/xboard-bridge/xboard-xui-bridge/internal/store"
	"github.com/xboard-bridge/xboard-xui-bridge/internal/xboard"
)

// syncAlive 把 3x-ui 端"在线 client + 其使用的 IP"上报到 Xboard /alive。
//
// 算法：
//
//  1. 拉所有当前 inbound 的 client 流量列表（GetClientTrafficsByInboundID）取
//     该 inbound 内的 email 集合 myEmails。
//  2. 拉全局在线 email（GetOnlines）取 onlineAll。
//  3. onlineMine = onlineAll ∩ myEmails；逐个调 GetClientIPs 解析最近 IP。
//  4. 真实 IP 缺失时（详见下方 v0.5.2 关键修复）走 placeholder 兜底，
//     至少让 Xboard 端"在线设备"反映"该 user 在线"。
//  5. 把 (xboard_user_id → [ip...]) 形态写入 AliveMap，调 PushAlive 上报。
//
// 并发：GetClientIPs 是逐个 email 的串行接口，inbound 内在线用户多时延迟会显著。
//
//	采用受限并发（信号量限并发数 8）平衡 3x-ui 面板压力 vs 吞吐。
//
// 错误处理：
//
//	a) 某个 email 拉 IPs 失败：记录 warn，跳过该用户继续，不影响其他用户上报。
//	b) PushAlive 失败：返回错误中断本次循环；下个周期完整重做（在线 IP 是
//	   状态而非增量，重做幂等且不丢数据）。
//
// v0.5.2 关键修复：Xboard 端"在线设备"永远 0 的根因。
//
// 表现：v0.5.1 修复后 traffic_sync 已能正常上报真实流量，但 Xboard 用户管理
// 页"在线设备"列、节点详情"在线设备数"始终为 0；运维肉眼可见有用户在线，
// alive_sync 日志亦显示"alive 上报完成 user_count=N（N>0）"——但 user_count
// 其实是 targets 计数，alive map 实际上传的是 `{}`。
//
// 根因链路：
//
//  1. 3x-ui 主线 POST /panel/api/inbounds/clientIps/:email → 内部读
//     `inbound_client_ips` SQL 表（client_email → ips JSON）。本仓库
//     internal/xui/client.go GetClientIPs 同样用 POST，与 3x-ui controller
//     注册的 POST 路由一致。
//  2. 该表的填充由 web/job/check_client_ip_job.go 的 CheckClientIpJob.Run()
//     负责，触发条件四重门槛全部满足才进入 processLogFile → addInboundClientIps：
//     a) hasLimitIp() == true：至少有一个 client.LimitIP > 0；
//     b) isAccessLogAvailable == true：xray access log 路径可读；
//     c) Linux 下需安装 Fail2Ban；Windows 不需要；
//     d) 实际有流量经过 access log（否则正则匹不到）。
//  3. 中间件 ClientSettings.LimitIP = Xboard User.DeviceLimit（见
//     internal/protocol/protocols.go 各协议适配器），但多数 Xboard 部署
//     的用户 device_limit 默认 0（"不限设备数"），故 a) 通常不满足；即便
//     运维给少数用户配了 device_limit>0，xray access log 与 Linux 端
//     Fail2Ban 在常见部署中也未启用——条件 (b)/(c)/(d) 同样常常缺一；
//  4. → CheckClientIpJob 不进入 IP 解析分支 → 表对中间件 client 几乎永远空；
//  5. → GetClientIPs 永远返回 "No IP Record"（controller 的 ips=="" 分支），
//     extractIPs 结果为空切片；
//  6. → alive map 中所有 user_id 对应的 ips 都是 [] → 拼装阶段被
//     `len(r.ips) == 0` 跳过 → AliveMap 实际为空对象；
//  7. → POST /api/v2/server/alive {} → Xboard 端 processAlive 看到 $alive=[]，
//     foreach 不执行 → user_devices Redis hash 无更新 → online_count
//     字段不被刷新，永远停在 0；
//  8. → 用户管理页"在线设备"显示 0。
//
// 与 v0.5.1 的区别：v0.5.1 修的是 GetClientTrafficsByInboundID 端点签名
// 错误（错把 inbound id 当 client UUID 用），属于 traffic_sync / alive_sync
// 第 1 步 myEmails 计算阶段；本 bug 在第 3 步 GetClientIPs 阶段，traffic_sync
// 不涉及该端点（traffic 走 client_traffics 表，由 xray gRPC stats 直接写，
// 与 LimitIP / access log 完全无关），因此 v0.5.1 修复后 traffic 显示正常
// 而 alive 仍 0——两个独立 bug，必须分别修。
//
// v0.5.2 修复策略：placeholder IP 兜底
//
// 当 GetClientIPs 返回空（即 3x-ui 端未启用 LimitIP/access log，对中间件
// client 而言是常态而非异常）时，使用 RFC 5737 TEST-NET-2 文档段
// (198.51.100.0/24) 构造稳定 placeholder IP，仍调 PushAlive 上报。这保证：
//
//   - Xboard processAlive 写入 user_devices hash → notifyUpdate 立即更新
//     users.online_count → "在线设备 = 1" 反映"该 user 在线"；
//   - 选择 RFC 5737 段：IANA 永久保留为文档示例，公网 / 运营商内网均不会
//     使用，与真实 IP 0 冲突；
//   - placeholder = 198.51.100.<fnv32(email) mod 254 + 1>，同 email 始终
//     生成同一占位 IP，跨周期幂等，跨 inbound 同 user 共享 placeholder
//     被 array_unique 去重为 1 设备——这是 placeholder 路径的预期上限
//     语义（"知道在线"，但不能反映真实多设备数）。
//
// 已知局限：placeholder 路径下"在线设备"永远 ≤ 1，无法反映同一 user 在多
// 终端的真实并发数；运维若需真实计数，请按 3x-ui 文档启用 LimitIP +
// access log（Linux 还需 Fail2Ban）；后续版本会考虑提供"中间件托管 client
// 自动设 LimitIP=N"开关，让真实 IP 路径在中间件场景下可用，同时保留本
// placeholder 兜底作为运维零配置默认值。
func (w *bridgeWorker) syncAlive(ctx context.Context) error {
	// 取本次 tick 的 trace 化 logger（含 loop=alive_sync + trace_id）；
	// 详见 engine.go runStep 中的 trace_id 注入逻辑。下方 fan-out goroutine
	// 闭包捕获 log 引用，所有"alive 拉 IP 失败" WARN 与"alive 上报完成"
	// INFO 共享同一组 attrs。
	log := loggerFromCtx(ctx, w.log)

	traffics, err := w.xuiC.GetClientTrafficsByInboundID(ctx, w.cfg.XuiInboundID)
	if err != nil {
		return fmt.Errorf("拉取 inbound 流量列表（用于 alive 过滤）：%w", err)
	}
	myEmails := make(map[string]struct{}, len(traffics))
	for _, t := range traffics {
		if protocol.IsManaged(t.Email) {
			myEmails[t.Email] = struct{}{}
		}
	}

	online, err := w.xuiC.GetOnlines(ctx)
	if err != nil {
		return fmt.Errorf("拉取在线 email：%w", err)
	}

	// 求交集，并把 email 翻译成 (xboard_user_id, uuid)；过滤掉 baseline 缺失的。
	type liveTarget struct {
		email        string
		xboardUserID int64
	}
	var targets []liveTarget
	for _, email := range online {
		if _, ok := myEmails[email]; !ok {
			continue
		}
		bl, err := w.store.GetBaseline(ctx, w.cfg.Name, email)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return fmt.Errorf("读取 alive 基线 %s：%w", email, err)
		}
		targets = append(targets, liveTarget{email: email, xboardUserID: bl.XboardUserID})
	}

	// 受限并发地拉每个 email 的当前 IP。
	const maxConcurrency = 8
	sem := make(chan struct{}, maxConcurrency)

	type ipResult struct {
		userID int64
		ips    []string
	}
	results := make([]ipResult, len(targets))

	var wg stdsync.WaitGroup
	for i := range targets {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			t := targets[idx]
			raw, err := w.xuiC.GetClientIPs(ctx, t.email)
			if err != nil {
				log.Warn("alive 拉 IP 失败", "email", t.email, "err", err)
				return
			}
			ips := extractIPs(raw)
			if len(ips) == 0 {
				// 真实 IP 缺失（详见文件头部 v0.5.2 关键修复注释）：使用
				// RFC 5737 文档段 placeholder 兜底，让 Xboard 端"在线设备"
				// 至少反映"该 user 在线"。同 email 跨周期幂等同值。
				ips = []string{placeholderIP(t.email)}
			}
			results[idx] = ipResult{userID: t.xboardUserID, ips: ips}
		}(i)
	}
	wg.Wait()
	if ctx.Err() != nil {
		// 外部已取消，不再上报。
		return ctx.Err()
	}

	// 拼装 AliveMap：key 是 user_id 字符串。即使一个用户有多个 IP，也按数组上报。
	alive := xboard.AliveMap{}
	for _, r := range results {
		if r.userID == 0 || len(r.ips) == 0 {
			continue
		}
		key := strconv.FormatInt(r.userID, 10)
		alive[key] = mergeUnique(alive[key], r.ips)
	}

	if err := w.xboardC.PushAlive(ctx, w.cfg.XboardNodeID, w.cfg.XboardNodeType, alive); err != nil {
		return fmt.Errorf("Xboard /alive：%w", err)
	}
	log.Info("alive 上报完成", "user_count", len(alive))
	return nil
}

// extractIPs 把 3x-ui clientIps 的字符串数组解析为纯 IP 列表。
//
// 3x-ui 返回形如 ["1.2.3.4 (2024-01-02 15:04:05)", "5.6.7.8 (...)"]
// 本函数取每行首个空白前的 token 作为 IP；不做合法性校验——非法 IP 由
// Xboard 端处理（DeviceStateService 会过滤），中间件不重复校验。
func extractIPs(raw []string) []string {
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// 取首个 token：3x-ui 间隔可能用普通空格，也可能用其它空白；strings.Fields 兼容。
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		out = append(out, fields[0])
	}
	return out
}

// mergeUnique 把 b 中的元素合并到 a 中，去重；保留 a 原有顺序，b 中新元素追加在末尾。
//
// 不使用 map 是为了保证出现顺序确定（便于日志比对），且 IP 列表通常 < 16 个，O(n^2) 可接受。
func mergeUnique(a, b []string) []string {
	for _, v := range b {
		seen := false
		for _, x := range a {
			if x == v {
				seen = true
				break
			}
		}
		if !seen {
			a = append(a, v)
		}
	}
	return a
}

// placeholderIP 为"已确认在线但 3x-ui 端拿不到真实 IP"的中间件托管 user 构造
// 一个稳定占位 IP。真实 IP 缺失的根因详见文件头部 v0.5.2 关键修复注释——
// 简言之：3x-ui 主线 inbound_client_ips 表的填充强依赖 client.LimitIP > 0 +
// access log + (Linux 下) Fail2Ban；ClientSettings.LimitIP 虽然映射自
// Xboard User.DeviceLimit，但多数部署 device_limit=0 + access log/Fail2Ban
// 未启用，故对绝大多数中间件 client 而言 GetClientIPs 返回空是常态。
//
// 设计要点：
//
//  1. 段位选 RFC 5737 TEST-NET-2 (198.51.100.0/24)：IANA 永久保留为文档
//     示例段，互联网 / 运营商内网 / RFC 1918 私有段均不会使用，因此
//     placeholder 与任何真实 IP 都不会发生取值冲突，运维在 Xboard 端看到
//     该段 IP 即可立即识别为中间件占位（无需查表）。其他保留段如
//     192.0.2.0/24（TEST-NET-1）、203.0.113.0/24（TEST-NET-3）也满足同
//     等需求；选 TEST-NET-2 仅为字面更易识别（中间段、数字递增易记）。
//
//  2. 主机位由 fnv32a(email) mod 254 + 1 决定：octet 范围限定 1..254，
//     避开 .0（网络号语义）与 .255（广播位语义），即便部分 Xboard 部署对
//     特殊 IP 做了过滤也能稳过。fnv32a 选用理由：标准库自带、零分配、
//     非加密 hash 速度优于 sha/md5；对"email → octet"映射不需要密码学
//     强度——254 桶下天然存在碰撞（因为我们刻意避开了 .0 与 .255 两个
//     边缘地址），碰撞结果只是"不同 user 共享同一 placeholder"，但因
//     Xboard user_devices Redis 按 user_id 分桶
//     (`user_devices:{userId}` hash)，跨 user 占位 IP 重复不会互相污染。
//
//  3. 同 email 始终映射同一 placeholder：保证跨周期幂等——本周期上线、
//     下周期仍上线时，Xboard 端 setDevices 先 removeNodeDevices(nodeId,
//     userId) 清旧 entry，再写同一 placeholder，不产生抖动。
//
//  4. 同 user 跨多 inbound / 多 node 同时上线：每个 syncAlive 调用是一个
//     bridge 内独立计算，AliveMap key 为 xboard_user_id。Xboard hash field
//     是 {nodeId}:{ip}；同 placeholder 在不同 nodeId 下被算作不同 field，
//     但 getDeviceCount 用 array_unique($ips) 去重——所以"同一 user 跨 N 节点
//     上线"在 placeholder 路径下仍只算 1 设备。这是 placeholder 路径的
//     预期上限语义（不能反映真实并发设备数；运维若需真实数请配 LimitIP）。
//
// 不引入额外配置项的理由：本函数仅作为"无真实 IP 时的兜底"，其行为对所
// 有部署都安全且对公网真实 IP 0 干扰，因此默认启用比加 feature flag 更
// 直接，运维零配置即可看到非 0 的"在线设备"显示，更新体验是单调改善。
func placeholderIP(email string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(email))
	octet := int(h.Sum32()%254) + 1 // 1..254：避开 .0 网络号与 .255 广播位
	return fmt.Sprintf("198.51.100.%d", octet)
}
