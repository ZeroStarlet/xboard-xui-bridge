package sync

import (
	"context"
	"fmt"

	"github.com/xboard-bridge/xboard-xui-bridge/internal/protocol"
	"github.com/xboard-bridge/xboard-xui-bridge/internal/store"
	"github.com/xboard-bridge/xboard-xui-bridge/internal/xboard"
)

// syncTraffic 把 3x-ui inbound 各 client 的累计流量差量上报到 Xboard。
//
// 高可靠遥测保证（已通过审查 Codex 验证）：
//
//	a) 流量重置 + 上报失败叠加场景下不丢失：
//	   通过 store.RecordSeen 持久化 last_seen 与 pending；当
//	   cur < last_seen 时把 (last_seen - reported) 累加到 pending 并归零 reported；
//	   pending 在 push 失败时保留，下个周期 delta = (cur - reported) + pending 自动包含。
//
//	b) 上报失败下个周期重传：reported 仅在 push 成功后推进，
//	   失败则不动，差量天然单调递增。
//
// v0.5.1 关键修复（v0.5 之前真实流量从未抵达 Xboard 的根因）：
//
//	v0.5 心跳 push 让节点不再被标"无人使用或异常"，但 Xboard 的 已用流量 /
//	u/d 字段始终为 0——根因不在 traffic_sync 自身，而是 xui.Client 调错了
//	端点：旧版 GetClientTrafficsByInboundID 调用
//	    GET /panel/api/inbounds/getClientTrafficsById/:id
//	并把 inbound id 整数传进 :id；但 3x-ui 主线该端点的 :id **实际上是
//	client UUID 字符串**，内部 SQL 用 JSON_EXTRACT(client.value, '$.id') in (?)
//	做相等匹配，传 inbound id 永远 0 命中——返回空数组，traffic_sync 循环
//	0 次进入，所有 skip 计数器为 0，最终 reported_users=0 + heartbeat_only=true。
//
//	v0.5.1 把内部实现切到 GET /panel/api/inbounds/list（3x-ui 服务层
//	GetInbounds(userId) 显式 Preload("ClientStats") + enrichClientStats），
//	遍历返回的 inbounds 数组取 ID == 目标 inboundID 那条的 ClientStats——
//	这是 3x-ui 主线唯一保证 ClientStats 被装载的端点。/get/:id 看似更精确
//	但其服务层 GetInbound(id) 仅 db.First(inbound, id)，**不 Preload**——
//	返回的 inbound.clientStats 是 nil 切片，会让本修复无效。GORM HasMany
//	关联仅靠 struct 标签不会自动加载子记录，必须显式 Preload。
//
//	调用方契约保持不变。注意 alive_sync 同样基于此方法过滤 myEmails，
//	本次修复一并恢复其能力。详见 internal/xui/client.go
//	GetClientTrafficsByInboundID 注释。
//
// v0.6 关键变更：移除"心跳 [0,0] 占位 push"兜底
//
//	v0.5 时代为了避免节点在"5 分钟内无任何用户消费流量"时被 Xboard 端
//	getAvailableStatusAttribute 标记为 STATUS_ONLINE_NO_PUSH（前端展示
//	"无人使用或异常"），构造 {"<heartbeatUserID>": [0, 0]} 占位 push 维持
//	节点 STATUS_ONLINE。这是典型的"失败兜底" / "降级路径"——它让上层
//	观察者无法区分"节点真的有人在用 + 有差量"与"节点空闲 + 中间件伪装在
//	上报"两种状态，且额外消耗 Xboard 的 array_filter / Cache::put 开销。
//
//	v0.6 起严格遵守"单一正向路径"承诺：本周期无任何真实差量 → 不调
//	PushTraffic、直接返回 nil；让 Xboard 按其原生判定逻辑切换节点状态
//	（"在线但 5 分钟内无 push" → STATUS_ONLINE_NO_PUSH）。运维若关心节
//	点活跃显示，应通过：
//	  - Xboard 后台 status 上报（status_sync 维持 LAST_LOAD_AT 活跃）；
//	  - 真实用户产生流量（生产环境最自然的活跃信号）；
//	维持"在线"的展示，而不是依赖中间件构造伪 [0,0] 数据欺骗判定逻辑。
//
// 不可消除的边界（与 README "限制与边界" 一致）：
//
//	"Xboard 端 push 成功 + 本地 ApplyReportSuccess 写入失败" 的极小窗口下，
//	下次循环以未推进的 reported 重发同一笔 delta，导致单次重复计费。
//	该窗口的概率 ≈ 本地 SQLite 单次写失败率（几乎为 0）。
//	彻底消除需要 Xboard 端引入 idempotency key——非中间件单方所能实现，
//	属于客观限制。运维如果需要 0 重复，应在上层做对账（Xboard 端按 push 时间窗去重）。
//
//	另一个常见运维盲点：Xboard 端真正写库的是 TrafficFetchJob 异步队列任务，
//	**必须运行 `php artisan queue:work` 或 Horizon** 才会真正消费队列；
//	否则中间件 push 永远成功（200）但 Xboard 用户 u/d 字段永远 0。
//	该提示已在 README "限制与边界" 章节明确披露。
func (w *bridgeWorker) syncTraffic(ctx context.Context) error {
	// 取本次 tick 的 trace 化 logger（含 loop=traffic_sync + trace_id）；
	// 详见 engine.go runStep 中的 trace_id 注入逻辑。函数内所有 w.log
	// 调用均已替换为 log，让本周期所有 skipped_* WARN / 推进失败 WARN /
	// 完成 INFO 共享同一组 attrs。
	log := loggerFromCtx(ctx, w.log)

	traffics, err := w.xuiC.GetClientTrafficsByInboundID(ctx, w.cfg.XuiInboundID)
	if err != nil {
		return fmt.Errorf("拉取 3x-ui 流量：%w", err)
	}

	// 一次性加载本 Bridge 的所有 baseline 到内存 map，替代循环里逐 user
	// 调 GetBaseline。1000 user 节点单周期 DB 读路径从 N 次逐 user
	// GetBaseline 降到 1 次批量读取（仍有 N 次 RecordSeen 写事务，但读
	// 路径已无 N 次往返）。
	//
	// 快照与 RecordSeen 的关系：本 map 是循环开始时刻的快照；RecordSeen
	// 内部会在事务里重新读 + 写 baseline，因此"map 中数据过时"不影响
	// 重置补偿正确性。并发 user_sync 删除窗口仍由后续 bl.XboardUserID
	// <= 0 二次防御兜底（详见循环中 RecordSeen 后的注释）。
	//
	// 快照陈旧的可见窗口：T0（本次 ListBaselinesByBridge）之后 user_sync
	// 新增的 baseline 在本周期 map 中查不到 → 走 skippedBaselineMissing
	// 跳过该 user，下周期补；T0 之后 user_sync 删除的 baseline 仍在
	// map 中可见 → 仅当删除发生在 RecordSeen 之前才能被 RecordSeen 内部
	// 防御性零行插入 + XboardUserID<=0 二次防御过滤；若删除发生在
	// RecordSeen 返回之后，本周期可能仍发出最后一笔 stale delta；这与
	// "用 GetBaseline 单次读取"的旧行为完全等价，即并发删除窗口本就是
	// "最多一笔 stale 上报"，不会造成流量丢失或长期重复计费。
	baselines, err := w.store.ListBaselinesByBridge(ctx, w.cfg.Name)
	if err != nil {
		return fmt.Errorf("批量加载基线：%w", err)
	}
	baselineMap := make(map[string]store.Baseline, len(baselines))
	for _, bl := range baselines {
		baselineMap[bl.Email] = bl
	}

	// 步骤 1：构造 (pushPayload, pendingCommits) 两份数据：
	//   pushPayload    {uid_string: [up, down]} 直接发给 Xboard；
	//   pendingCommits 在上报成功后批量调 ApplyReportSuccess 推进 reported。
	type curStat struct {
		email string
		curUp int64
		curDn int64
	}
	pushPayload := xboard.PushTraffic{}
	var pending []curStat

	// 跳过原因细分计数器（v0.5 起从单一 skipped_other 拆出）。
	// 运维在日志里能直观看到"为什么 reported_users=0"——是 baseline 没建好、
	// node_id 配错、还是用户根本没产生流量。混在一起的旧日志极难诊断。
	skippedEmailInvalid := 0    // email 解析失败 / 非托管前缀
	skippedNodeMismatch := 0    // email 中的 node_id 与本 bridge 配置不一致（多 bridge 共享 inbound 的边界）
	skippedBaselineMissing := 0 // baseline 未建立（user_sync 还没跑过 / 失败）
	skippedUserIDInvalid := 0   // baseline.XboardUserID <= 0（防御性零行 / Xboard 端 user 已删但 baseline 未清理）
	skippedZero := 0            // delta=0（用户在 inbound 内但本周期未消费流量）

	for _, t := range traffics {
		if !protocol.IsManaged(t.Email) {
			skippedEmailInvalid++
			continue
		}
		_, nodeID, perr := protocol.ParseEmail(t.Email)
		if perr != nil {
			// email 已通过 IsManaged（"xboard_" 前缀）但 ParseEmail 失败——
			// 这是数据腐烂迹象：托管前缀但其余字段（uuid_segment / node_id /
			// 分隔符）异常。v0.5.x 的"WARN + continue 维持原状"已被删除：
			// 静默跳过会让流量永远不上报，掩盖真实根因（运维误改 email /
			// store 写入异常 / 协议升级未完成）。v0.6 起立即报错让运维显式
			// 可见，与 user_sync.parseExistingManagedClients 同等严格——
			// 两者对同一类异常输入的处理保持一致。
			return fmt.Errorf("流量上报：email %q 是托管前缀但 ParseEmail 失败：%w", t.Email, perr)
		}
		if nodeID != w.cfg.XboardNodeID {
			// 同 inbound 下出现属于其它 node 的 email——这是多 bridge 共享
			// 同一 inbound（不推荐但允许）或同一物理 inbound 被换绑过 node_id
			// 时的合法情形：跨 node_id 的 email 不属于本 bridge 的 diff 范围，
			// 必须 continue 避免把流量错记到本 node 的 user 上。这与
			// parseExistingManagedClients 的同类分支语义对齐。
			log.Warn("流量上报跳过：node_id 不匹配", "email", t.Email, "expect", w.cfg.XboardNodeID, "got", nodeID)
			skippedNodeMismatch++
			continue
		}

		// EnsureBaseline 由 user_sync 负责调用；这里不做防御创建（避免与 user_sync
		// 的"未注册"语义冲突）。若 baseline 缺失，本周期跳过该 client，下周期 user_sync
		// 写完即可恢复正常上报；3x-ui 端流量在 baseline 建立前累积的部分会在
		// RecordSeen 第一次执行时写入 last_seen，下次差量自然包含。
		//
		// map 查询替代逐 user GetBaseline DB 往返："key 不存在"等价于原
		// errors.Is(err, store.ErrNotFound) 分支语义。批量加载的统一错误
		// 处理已在函数顶部完成（ListBaselinesByBridge 失败即整个 syncTraffic
		// return），所以 map 路径下不再需要"读取基线失败"的逐条错误处理。
		blPre, ok := baselineMap[t.Email]
		if !ok {
			log.Debug("流量上报跳过：基线未建立（等待 user_sync）", "email", t.Email)
			skippedBaselineMissing++
			continue
		}

		// 早期防御性拦截：baseline.XboardUserID <= 0 不应出现——它意味着
		// RecordSeen 走过防御性零行插入分支，或者 user_sync 写入时 user.ID
		// 异常。无论何种情形，把 [up, down] 推给 Xboard 时 user_id="0"
		// 会导致 incrementEach(['u'=>delta]) 误更新一个不存在或脏的用户。
		// 这里 skip + 计数 + WARN，让运维可观测但不阻塞其它合法 user 的上报。
		// 注意：这是早期拦截，能避开后续 RecordSeen 的事务开销；但它不能
		// 覆盖"GetBaseline 后 user_sync 并发删除导致 RecordSeen 写零行"
		// 的窗口——那一情形由 RecordSeen 之后的二次防御兜底（详见下方）。
		if blPre.XboardUserID <= 0 {
			log.Warn("流量上报跳过：baseline.XboardUserID 非法（<=0），疑似防御性零行或脏数据",
				"email", t.Email, "xboard_user_id", blPre.XboardUserID)
			skippedUserIDInvalid++
			continue
		}

		// RecordSeen 是"重置补偿持久化"的关键步骤：
		//   a) 在事务内对比 cur vs last_seen，若发生重置则累加 pending 并归零 reported；
		//   b) 写入 last_seen = cur（无论是否重置都要更新）；
		//   c) 返回写入后的 baseline，用于本周期 delta 计算。
		// 任何步骤失败都中断本周期；store 已用事务保护，不会留下部分写入的中间状态。
		bl, err := w.store.RecordSeen(ctx, w.cfg.Name, t.Email, t.Up, t.Down)
		if err != nil {
			return fmt.Errorf("RecordSeen %s：%w", t.Email, err)
		}

		// 二次防御性拦截：上文 GetBaseline 后到此处之间，user_sync 可能并发
		// 调用 DeleteBaseline（Xboard 端用户被删 / 改 group / 改 plan 都会
		// 触发 user_sync 删基线），随后 RecordSeen 走"防御性零行插入"分支
		// 写出 xboard_user_id=0 的行。第一次防御只挡到了"读到时已是 0"的
		// 情形——本次再次校验 RecordSeen 返回的 bl.XboardUserID 必须 > 0，
		// 否则 push 时会发出 "0": [up, down] 污染计费。
		if bl.XboardUserID <= 0 {
			log.Warn("流量上报跳过：RecordSeen 后 baseline.XboardUserID 仍非法（疑似并发删除窗口）",
				"email", t.Email, "xboard_user_id", bl.XboardUserID)
			skippedUserIDInvalid++
			continue
		}

		// 计算未上报差量。reported 已经在 RecordSeen 中（必要时）置 0；
		// 因此正常路径下 t.Up >= bl.ReportedUp、t.Down >= bl.ReportedDown
		// 必然成立（cur < last_seen 的重置场景被 RecordSeen 捕获并把
		// reported 归零）。若到这里仍出现负值，意味着 store 层事务出现
		// 了与协议假设冲突的状态——v0.6 单一正向路径承诺要求立即报错让
		// 运维显式可见，而不是夹紧到 0 静默丢弃异常。
		baseUp := t.Up - bl.ReportedUp
		baseDown := t.Down - bl.ReportedDown
		if baseUp < 0 || baseDown < 0 {
			return fmt.Errorf("流量基线异常 %s：t.Up=%d t.Down=%d ReportedUp=%d ReportedDown=%d（baseUp=%d baseDown=%d 出现负值，store 层事务可能未正确归零 reported）",
				t.Email, t.Up, t.Down, bl.ReportedUp, bl.ReportedDown, baseUp, baseDown)
		}
		// 直接使用 raw delta（字节）。流量倍率由 Xboard 端 v2_server.rate
		// 在 TrafficFetchJob 中自动乘算（详见 Reporting 注释）；中间件不在
		// 此处叠加二次倍率，避免双重乘以及 floor 累积偏差。
		deltaUp := baseUp + bl.PendingUp
		deltaDown := baseDown + bl.PendingDown

		if deltaUp == 0 && deltaDown == 0 {
			skippedZero++
			continue
		}
		pushPayload.Set(bl.XboardUserID, deltaUp, deltaDown)
		pending = append(pending, curStat{email: t.Email, curUp: t.Up, curDn: t.Down})
	}

	// 步骤 2：仅在有真实差量时调 Xboard /push（v0.6 起移除"心跳 [0,0] 兜底"）。
	//
	// pushPayload 为空 → 直接返回 nil 跳过本次 push。Xboard 端会按其原生
	// 判定逻辑切换节点状态（5 分钟内无 push → STATUS_ONLINE_NO_PUSH）。
	// 这是符合"单一正向路径"的预期行为：节点空闲就让 Xboard 看到空闲，
	// 不替它构造虚假的 [0,0] 占位数据。
	if len(pushPayload) == 0 {
		log.Info("本周期无差量，跳过 push（v0.6 起不再发送心跳占位）",
			"skipped_email_invalid", skippedEmailInvalid,
			"skipped_node_mismatch", skippedNodeMismatch,
			"skipped_baseline_missing", skippedBaselineMissing,
			"skipped_user_id_invalid", skippedUserIDInvalid,
			"skipped_zero", skippedZero,
		)
		return nil
	}

	// 步骤 3：调 Xboard /push 上报真实差量。
	totalUp := int64(0)
	totalDown := int64(0)
	for _, v := range pushPayload {
		totalUp += v[0]
		totalDown += v[1]
	}
	log.Info("调 Xboard /push 上报流量",
		"user_count", len(pushPayload),
		"total_up_bytes", totalUp,
		"total_down_bytes", totalDown,
	)
	if err := w.xboardC.PushTraffic(ctx, w.cfg.XboardNodeID, w.cfg.XboardNodeType, pushPayload); err != nil {
		return fmt.Errorf("Xboard /push：%w", err)
	}

	// 步骤 4：上报成功，逐条推进 baseline。
	//
	// v0.6 单一正向路径承诺：尽量推进所有条目（不能因为第 1 条失败就放弃
	// 第 2..N 条——那会让大部分用户产生"已上报但 baseline 未推进"的重复
	// 计费窗口扩大），但只要存在任意一条推进失败，整轮 syncTraffic 必须
	// 返回错误让上层显式可见。v0.5.x 时代的"WARN 后继续 return nil"已被
	// 删除——那种行为把"已上报但本地未持久化，下个周期会重复计费一次"
	// 这一真实风险静默掩盖，违反"失败立即传播"承诺。
	var firstUpdateErr error
	updateErrors := 0
	for _, p := range pending {
		if err := w.store.ApplyReportSuccess(ctx, w.cfg.Name, p.email, p.curUp, p.curDn); err != nil {
			updateErrors++
			log.Warn("推进基线失败（已上报但本地未持久化，下个周期可能重复计费一次）",
				"email", p.email, "err", err)
			if firstUpdateErr == nil {
				firstUpdateErr = fmt.Errorf("推进基线 %s：%w", p.email, err)
			}
		}
	}

	// 完成日志：reported_users 即真实差量 push 中的用户数。v0.6 起移除
	// heartbeat_only 标志（不再有伪心跳路径）。
	log.Info("流量上报完成",
		"reported_users", len(pending),
		"total_up_bytes", totalUp,
		"total_down_bytes", totalDown,
		"skipped_zero", skippedZero,
		"skipped_email_invalid", skippedEmailInvalid,
		"skipped_node_mismatch", skippedNodeMismatch,
		"skipped_baseline_missing", skippedBaselineMissing,
		"skipped_user_id_invalid", skippedUserIDInvalid,
		"baseline_update_errors", updateErrors,
	)
	// 任意一条 baseline 推进失败都向上传播：上层 runStep 会以 WARN 显示
	// 完整错误链，运维立刻可见。
	return firstUpdateErr
}
