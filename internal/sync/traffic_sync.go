package sync

import (
	"context"
	"errors"
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
// 心跳 push（v0.5 起，修复"无人使用或异常"标黄 bug）：
//
//	Xboard 端 Server 模型 getAvailableStatusAttribute 的判定铁律：
//	  - last_check_at 在 5 分钟内 + last_push_at 在 5 分钟内 → STATUS_ONLINE
//	  - last_check_at 在 5 分钟内 + last_push_at 超时   → STATUS_ONLINE_NO_PUSH
//	  - last_check_at 超时                              → STATUS_OFFLINE
//	last_push_at 缓存只在 ServerService::processTraffic 主流程被刷新；
//	而 processTraffic 在 array_filter 后 empty($data) 时直接 return，**不更新缓存**。
//	即"中间件发空 push（{}）等于没发"。
//
//	老版本（v0.4 之前）的 traffic_sync 在 pushPayload 为空时跳过整个 PushTraffic
//	调用，节点 5 分钟内若无任何用户产生流量就会被 Xboard 标记为
//	STATUS_ONLINE_NO_PUSH——前端展示为"无人使用或异常"，与"节点真死了"难以区分。
//	老注释一度写"心跳保活靠 status_sync 的 /status 上报刷新 LAST_LOAD_AT"——
//	**这是错的**：LAST_LOAD_AT 与节点 available_status 判定无关，status 端点
//	缓存的是节点负载快照，与心跳无关。
//
//	v0.5 修复：当 pushPayload 为空时，从 baseline 中任选一个有效 user_id
//	（XboardUserID > 0）构造 {"<id>": [0, 0]} 占位项发出去——
//	  a) Xboard 端 array_filter 接受 [0, 0]（is_numeric(0) 通过）；
//	  b) processTraffic 走完缓存更新分支，刷新 LAST_PUSH_AT；
//	  c) 后续 incrementEach(['u'=>0,'d'=>0]) 在 SQL 层等价 noop——不污染计费。
//	心跳目标查找两级兜底：
//	  1. 优先用循环里 RecordSeen 后的 bl.XboardUserID（最新事务快照）；
//	  2. 循环结束仍未拿到（3x-ui 此刻 inbound 为空 / 全部走 skip 分支）→
//	     回退查 ListBaselinesByBridge 挑首个 XboardUserID > 0 的行。
//	仅当 baseline 表无任何 XboardUserID > 0 的有效行（即 Xboard 没给该节点
//	分配任何合法 user）时才跳过；这种情况下 Xboard 视角下"节点无可服务
//	用户"，标记 STATUS_ONLINE_NO_PUSH 也合理。
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
	traffics, err := w.xuiC.GetClientTrafficsByInboundID(ctx, w.cfg.XuiInboundID)
	if err != nil {
		return fmt.Errorf("拉取 3x-ui 流量：%w", err)
	}

	// 步骤 1：构造 (pushPayload, pendingCommits) 两份数据：
	//   pushPayload    {uid_string: [up, down]} 直接发给 Xboard；
	//   pendingCommits 在上报成功后批量调 ApplyReportSuccess 推进 reported。
	//
	// 同时记录 heartbeatUserID：循环中遇到的第一个有效 XboardUserID（>0），
	// 用于"无任何真实差量"时构造 [0, 0] 心跳 push 维持节点 STATUS_ONLINE。
	type curStat struct {
		email string
		curUp int64
		curDn int64
	}
	pushPayload := xboard.PushTraffic{}
	var pending []curStat
	var heartbeatUserID int64

	// 跳过原因细分计数器（v0.5 起从单一 skipped_other 拆出）。
	// 运维在日志里能直观看到"为什么 reported_users=0"——是 baseline 没建好、
	// node_id 配错、还是用户根本没产生流量。混在一起的旧日志极难诊断，
	// 这是 v0.4 版"流量永远 0 + 提示无人使用或异常"工单的主要排查瓶颈。
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
			// email 形态不合规（运维改过、脏数据），既不上报也不报错。
			w.log.Warn("流量上报跳过：email 解析失败", "email", t.Email, "err", perr)
			skippedEmailInvalid++
			continue
		}
		if nodeID != w.cfg.XboardNodeID {
			// 同 inbound 下出现属于其它 node 的 email——理论上不应发生。
			// 跳过避免把流量错误地记账到当前 node 的 user 上。
			w.log.Warn("流量上报跳过：node_id 不匹配", "email", t.Email, "expect", w.cfg.XboardNodeID, "got", nodeID)
			skippedNodeMismatch++
			continue
		}

		// EnsureBaseline 由 user_sync 负责调用；这里不做防御创建（避免与 user_sync
		// 的"未注册"语义冲突）。若 baseline 缺失，本周期跳过该 client，下周期 user_sync
		// 写完即可恢复正常上报；3x-ui 端流量在 baseline 建立前累积的部分会在
		// RecordSeen 第一次执行时写入 last_seen，下次差量自然包含。
		blPre, err := w.store.GetBaseline(ctx, w.cfg.Name, t.Email)
		if errors.Is(err, store.ErrNotFound) {
			w.log.Debug("流量上报跳过：基线未建立（等待 user_sync）", "email", t.Email)
			skippedBaselineMissing++
			continue
		}
		if err != nil {
			return fmt.Errorf("读取基线 %s：%w", t.Email, err)
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
			w.log.Warn("流量上报跳过：baseline.XboardUserID 非法（<=0），疑似防御性零行或脏数据",
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
		// 同时把 heartbeatUserID 切换到从 bl（而非 blPre）取——避免读到的旧
		// 快照在并发删除窗口内成为 push 目标。
		if bl.XboardUserID <= 0 {
			w.log.Warn("流量上报跳过：RecordSeen 后 baseline.XboardUserID 仍非法（疑似并发删除窗口）",
				"email", t.Email, "xboard_user_id", bl.XboardUserID)
			skippedUserIDInvalid++
			continue
		}
		// 用 bl（post-RecordSeen 快照）刷新 heartbeatUserID——这是当前事务
		// 已确认存在且 user_id>0 的最新值，比 blPre 更可靠。
		if heartbeatUserID == 0 {
			heartbeatUserID = bl.XboardUserID
		}

		// 计算未上报差量。reported 已经在 RecordSeen 中（必要时）置 0。
		baseUp := t.Up - bl.ReportedUp
		baseDown := t.Down - bl.ReportedDown
		if baseUp < 0 {
			baseUp = 0
		}
		if baseDown < 0 {
			baseDown = 0
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

	// 步骤 2：决定本周期是发"真实差量 push"还是"心跳 [0,0] push"。
	//
	// 真实差量优先：pushPayload 非空时直接走真实路径，无需心跳——真实数据
	// 已经能让 Xboard processTraffic 主流程刷新 LAST_PUSH_AT。
	//
	// 无真实差量时心跳兜底：构造 {"<heartbeatUserID>": [0, 0]} 让 Xboard 端：
	//   a) array_filter 接受（is_numeric(0) 为 true）；
	//   b) Cache::put SERVER_*_LAST_PUSH_AT 触发（节点 STATUS_ONLINE）；
	//   c) Cache::put SERVER_*_ONLINE_USER 设为 1（"伪在线 1 人"——可接受的
	//      展示折损：换取节点不显示"异常"）；
	//   d) UserService::trafficFetch 走 incrementEach(['u'=>0,'d'=>0])——
	//      Laravel 在 SQL 层把它优化为 noop UPDATE，不影响计费。
	//
	// heartbeatUserID==0 兜底：循环里只有"3x-ui 端确实存在的 client + 已建立
	// baseline 且 user_id>0"才会被记录到 heartbeatUserID。如果 3x-ui 端
	// 这一刻 inbound 被清空 / clientStats 全是非托管 / 全部走 baseline_missing
	// 跳过分支，循环结束 heartbeatUserID 仍为 0。但 baseline 表里可能仍有
	// Xboard 端分配过的 user 行——回退查 ListBaselinesByBridge 再挑一个。
	// 这是 v0.5 修复的关键：3x-ui 临时清空（运维换 inbound、面板重启）期间，
	// 节点不应被 Xboard 标"异常"。
	//
	// 仍找不到（baseline 表无任何 XboardUserID > 0 的有效行）时直接跳过
	// push——此时 Xboard 端该节点本身就没分配任何合法 user，标
	// STATUS_ONLINE_NO_PUSH 是合理展示。
	heartbeatOnly := false
	if len(pushPayload) == 0 {
		if heartbeatUserID == 0 {
			baselines, err := w.store.ListBaselinesByBridge(ctx, w.cfg.Name)
			if err != nil {
				return fmt.Errorf("回退查询 baseline 列表（用于心跳兜底）：%w", err)
			}
			for _, bl := range baselines {
				// 与循环内防御一致：仅挑 XboardUserID>0 的行作为心跳目标，
				// 避免脏的零行污染计费。
				if bl.XboardUserID > 0 {
					heartbeatUserID = bl.XboardUserID
					break
				}
			}
		}
		if heartbeatUserID > 0 {
			pushPayload.Set(heartbeatUserID, 0, 0)
			heartbeatOnly = true
		} else {
			w.log.Info("无差量也无可用 baseline，跳过本周期 push（Xboard 端无可服务用户）",
				"skipped_email_invalid", skippedEmailInvalid,
				"skipped_node_mismatch", skippedNodeMismatch,
				"skipped_baseline_missing", skippedBaselineMissing,
				"skipped_user_id_invalid", skippedUserIDInvalid,
				"skipped_zero", skippedZero,
			)
			return nil
		}
	}

	// 步骤 3：调 Xboard /push。
	//
	// 真实差量路径：附带 total_up_bytes / total_down_bytes 让运维确认推送量。
	// 心跳路径：标记 heartbeat=true，total_bytes 必为 0。
	totalUp := int64(0)
	totalDown := int64(0)
	for _, v := range pushPayload {
		totalUp += v[0]
		totalDown += v[1]
	}
	w.log.Info("调 Xboard /push 上报流量",
		"heartbeat", heartbeatOnly,
		"user_count", len(pushPayload),
		"total_up_bytes", totalUp,
		"total_down_bytes", totalDown,
	)
	if err := w.xboardC.PushTraffic(ctx, w.cfg.XboardNodeID, w.cfg.XboardNodeType, pushPayload); err != nil {
		return fmt.Errorf("Xboard /push：%w", err)
	}

	// 步骤 4：上报成功，逐条推进 baseline。
	// 仅推进 pending 中的真实差量项；心跳 [0,0] 不在 pending 中（步骤 1
	// 仅 deltaUp/deltaDown 非零时才 append），ApplyReportSuccess 不会被对
	// 心跳 user_id 误调，避免污染该 user 的 reported_*。
	//
	// 失败一条不能阻断全部——剩余条目仍要写入，否则会产生"已上报但 baseline 未推进"
	// 导致下次重复计费。这种情况下记 warn 但不返回错误。
	updateErrors := 0
	for _, p := range pending {
		if err := w.store.ApplyReportSuccess(ctx, w.cfg.Name, p.email, p.curUp, p.curDn); err != nil {
			updateErrors++
			w.log.Warn("推进基线失败（已上报但本地未持久化，下个周期可能重复计费一次）",
				"email", p.email, "err", err)
		}
	}

	// 完成日志：reported_users 仅计真实差量项（不含心跳）；heartbeat_only
	// 标志让运维一眼看出本周期是否承担了"维持节点活跃"职责而非真实计费。
	w.log.Info("流量上报完成",
		"reported_users", len(pending),
		"heartbeat_only", heartbeatOnly,
		"total_up_bytes", totalUp,
		"total_down_bytes", totalDown,
		"skipped_zero", skippedZero,
		"skipped_email_invalid", skippedEmailInvalid,
		"skipped_node_mismatch", skippedNodeMismatch,
		"skipped_baseline_missing", skippedBaselineMissing,
		"skipped_user_id_invalid", skippedUserIDInvalid,
		"baseline_update_errors", updateErrors,
	)
	return nil
}
