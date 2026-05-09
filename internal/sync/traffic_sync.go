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
// 不可消除的边界（与 README "限制与边界" 一致）：
//
//	"Xboard 端 push 成功 + 本地 ApplyReportSuccess 写入失败" 的极小窗口下，
//	下次循环以未推进的 reported 重发同一笔 delta，导致单次重复计费。
//	该窗口的概率 ≈ 本地 SQLite 单次写失败率（几乎为 0）。
//	彻底消除需要 Xboard 端引入 idempotency key——非中间件单方所能实现，
//	属于客观限制。运维如果需要 0 重复，应在上层做对账（Xboard 端按 push 时间窗去重）。
func (w *bridgeWorker) syncTraffic(ctx context.Context) error {
	traffics, err := w.xuiC.GetClientTrafficsByInboundID(ctx, w.cfg.XuiInboundID)
	if err != nil {
		return fmt.Errorf("拉取 3x-ui 流量：%w", err)
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
	skippedNon := 0
	skippedZero := 0

	for _, t := range traffics {
		if !protocol.IsManaged(t.Email) {
			skippedNon++
			continue
		}
		_, nodeID, perr := protocol.ParseEmail(t.Email)
		if perr != nil {
			// email 形态不合规（运维改过、脏数据），既不上报也不报错。
			w.log.Warn("流量上报跳过：email 解析失败", "email", t.Email, "err", perr)
			skippedNon++
			continue
		}
		if nodeID != w.cfg.XboardNodeID {
			// 同 inbound 下出现属于其它 node 的 email——理论上不应发生。
			// 跳过避免把流量错误地记账到当前 node 的 user 上。
			w.log.Warn("流量上报跳过：node_id 不匹配", "email", t.Email, "expect", w.cfg.XboardNodeID, "got", nodeID)
			skippedNon++
			continue
		}

		// EnsureBaseline 由 user_sync 负责调用；这里不做防御创建（避免与 user_sync
		// 的"未注册"语义冲突）。若 baseline 缺失，本周期跳过该 client，下周期 user_sync
		// 写完即可恢复正常上报；3x-ui 端流量在 baseline 建立前累积的部分会在
		// RecordSeen 第一次执行时写入 last_seen，下次差量自然包含。
		_, err := w.store.GetBaseline(ctx, w.cfg.Name, t.Email)
		if errors.Is(err, store.ErrNotFound) {
			w.log.Debug("流量上报跳过：基线未建立（等待 user_sync）", "email", t.Email)
			skippedNon++
			continue
		}
		if err != nil {
			return fmt.Errorf("读取基线 %s：%w", t.Email, err)
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

	// 步骤 2：调 Xboard /push。无差量上报项时跳过：Xboard 端 processTraffic
	// 在 empty 时直接 return 不会触发任何缓存更新，发空请求毫无收益。
	// 心跳保活靠 status_sync 的 /status 上报刷新 LAST_LOAD_AT。
	if len(pushPayload) > 0 {
		if err := w.xboardC.PushTraffic(ctx, w.cfg.XboardNodeID, w.cfg.XboardNodeType, pushPayload); err != nil {
			return fmt.Errorf("Xboard /push：%w", err)
		}
	}

	// 步骤 3：上报成功，逐条推进 baseline。
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

	w.log.Info("流量上报完成",
		"reported_users", len(pushPayload),
		"skipped_zero", skippedZero,
		"skipped_other", skippedNon,
		"baseline_update_errors", updateErrors,
	)
	return nil
}

