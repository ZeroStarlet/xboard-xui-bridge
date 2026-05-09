package sync

import (
	"context"
	"errors"
	"fmt"
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
//  4. 把 (xboard_user_id → [ip...]) 形态写入 AliveMap，调 PushAlive 上报。
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
func (w *bridgeWorker) syncAlive(ctx context.Context) error {
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
				w.log.Warn("alive 拉 IP 失败", "email", t.email, "err", err)
				return
			}
			results[idx] = ipResult{userID: t.xboardUserID, ips: extractIPs(raw)}
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
	w.log.Info("alive 上报完成", "user_count", len(alive))
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
