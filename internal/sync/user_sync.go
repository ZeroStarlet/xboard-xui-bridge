package sync

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/xboard-bridge/xboard-xui-bridge/internal/protocol"
	"github.com/xboard-bridge/xboard-xui-bridge/internal/xboard"
	"github.com/xboard-bridge/xboard-xui-bridge/internal/xui"
)

// syncUsers 把 Xboard 端"应该有的用户清单"同步到 3x-ui inbound。
//
// 算法：
//
//  1. 拉 Xboard /user → 期望状态 wanted (email → ClientSettings)
//  2. 拉 3x-ui /inbounds/get/:id → 现状 existing (email → managedRawClient)
//     仅识别 IsManaged(email) 的 client，避免误删运维手工添加的 client。
//  3. diff：
//     toAdd     = wanted - existing
//     toDelete  = existing - wanted
//     toUpdate  = existing ∩ wanted（按字段差异）
//  4. 调 3x-ui addClient（批量）/ updateClient（逐个）/ delClientByEmail（逐个）
//  5. 与 store 同步：toAdd/toUpdate 调 EnsureBaseline；toDelete 调 DeleteBaseline。
//
// 错误策略：
//
//	任一 3x-ui 操作失败立即返回错误并中断本次循环；下一次循环会重新 diff
//	并仅处理仍然不一致的部分，无需补偿动作。这避免了"多步部分成功"导致
//	的中间状态——下一周期重做即可幂等。
func (w *bridgeWorker) syncUsers(ctx context.Context) error {
	// 取本次 tick 的 trace 化 logger（含 loop=user_sync + trace_id）；详见
	// engine.go runStep 中的 trace_id 注入逻辑。子函数 buildWantedClients /
	// parseExistingManagedClients / applyXxx 当前不打日志，故只在主入口
	// 替换 logger；任何错误会通过 fmt.Errorf 链返回 runStep，runStep 层
	// 的 WARN 输出带 trace_id 自然贯穿。
	log := loggerFromCtx(ctx, w.log)

	users, err := w.xboardC.FetchUsers(ctx, w.cfg.XboardNodeID, w.cfg.XboardNodeType)
	if err != nil {
		return fmt.Errorf("拉取 Xboard 用户：%w", err)
	}

	inbound, err := w.xuiC.GetInbound(ctx, w.cfg.XuiInboundID)
	if err != nil {
		return fmt.Errorf("拉取 3x-ui inbound：%w", err)
	}

	wanted, err := w.buildWantedClients(users.Users)
	if err != nil {
		return err
	}
	existing, err := w.parseExistingManagedClients(inbound.RawSettings)
	if err != nil {
		return fmt.Errorf("解析 inbound.settings：%w", err)
	}

	toAdd, toUpdate, toDelete := diffClients(wanted, existing)

	log.Info("用户 diff 完成",
		"xboard_users", len(users.Users),
		"xui_managed", len(existing),
		"add", len(toAdd),
		"update", len(toUpdate),
		"delete", len(toDelete),
	)

	if err := w.applyAdd(ctx, toAdd); err != nil {
		return err
	}
	if err := w.applyUpdate(ctx, toUpdate); err != nil {
		return err
	}
	if err := w.applyDelete(ctx, toDelete); err != nil {
		return err
	}

	// store 侧同步基线：先把不再存在的清掉（避免"无主"基线占空间），再为新增 / 已存在的用户登记。
	for email := range toDelete {
		if err := w.store.DeleteBaseline(ctx, w.cfg.Name, email); err != nil {
			return fmt.Errorf("删除基线 %s：%w", email, err)
		}
	}
	for _, u := range users.Users {
		email := protocol.MakeEmail(u.UUID, w.cfg.XboardNodeID)
		if _, err := w.store.EnsureBaseline(ctx, w.cfg.Name, email, u.ID); err != nil {
			return fmt.Errorf("登记基线 %s：%w", email, err)
		}
	}

	return nil
}

// buildWantedClients 把 Xboard 用户列表通过协议适配器转成 ClientSettings。
//
// 同 email 出现两次会返回错误——这通常意味着 Xboard 数据异常，与其静默
// 覆盖一份不如把问题暴露给运维。
func (w *bridgeWorker) buildWantedClients(users []xboard.User) (map[string]xui.ClientSettings, error) {
	out := make(map[string]xui.ClientSettings, len(users))
	for _, u := range users {
		c := w.adapter.BuildClient(u, w.cfg)
		if _, dup := out[c.Email]; dup {
			return nil, fmt.Errorf("Xboard 返回重复用户 email=%s（uuid=%s）", c.Email, u.UUID)
		}
		out[c.Email] = c
	}
	return out, nil
}

// rawSettings 是为了把 inbound.settings 的二次 JSON 解出来用的最小结构。
// 只关心 clients 数组，其它字段透传由 3x-ui 自行管理。
type rawSettings struct {
	Clients []json.RawMessage `json:"clients"`
}

// parseExistingManagedClients 解析 inbound.RawSettings，
// 抽出"由当前 Bridge 管理"的 client 子集转为 ClientSettings 形态。
//
// "由当前 Bridge 管理"判定条件：
//
//   - email 以 xboard_ 前缀打头（IsManaged）；
//   - email 经 ParseEmail 解析得到的 node_id 与 bridge 配置中的 XboardNodeID 一致。
//
// 第二条判定至关重要：当多个 Bridge 共享同一个 inbound（不推荐但允许）或
// 同一物理 inbound 被换绑过 node_id 时，"看起来托管"的 email 实际属于其他
// bridge；只有 node_id 完全匹配才能纳入 diff，否则可能误删其他 bridge 的 client。
//
// 解析失败 = 配置异常，向上返回错误中断本次循环。
func (w *bridgeWorker) parseExistingManagedClients(raw json.RawMessage) (map[string]xui.ClientSettings, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]xui.ClientSettings{}, nil
	}
	// 3x-ui 的 settings 字段是"双层 JSON"——外层是 string，里层是 object。
	// 但 GetInbound 中我们已声明为 json.RawMessage，期望它直接是 object 字节流。
	// 两种形态都要兼容：
	settingsBytes := stripJSONStringQuotes(raw)

	var s rawSettings
	if err := json.Unmarshal(settingsBytes, &s); err != nil {
		return nil, err
	}

	out := make(map[string]xui.ClientSettings, len(s.Clients))
	for _, raw := range s.Clients {
		var c xui.ClientSettings
		if err := json.Unmarshal(raw, &c); err != nil {
			// 单条解码失败不阻断全量；只是这条 client 不会被识别成托管对象，
			// 不会被中间件改动，最坏退化为"维持原状"，安全。
			continue
		}
		if !protocol.IsManaged(c.Email) {
			continue
		}
		// node_id 不匹配的 "xboard_*" email 视为他属，不纳入本 Bridge 的 diff。
		_, nodeID, perr := protocol.ParseEmail(c.Email)
		if perr != nil || nodeID != w.cfg.XboardNodeID {
			continue
		}
		out[c.Email] = c
	}
	return out, nil
}

// stripJSONStringQuotes 处理 3x-ui inbound.settings 的"双层 JSON"。
//
// 实测两种形态都可能出现：
//
//	a) 外层包了一对引号且内部转义："\"{\\\"clients\\\":[...]}\""
//	b) 直接是对象：{"clients":[...]}
//
// 情形 a 会被 json.Unmarshal 解为字符串再解二次，本函数把它解开成裸对象字节流。
// 情形 b 直接返回原 bytes。
func stripJSONStringQuotes(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return raw
	}
	if raw[0] != '"' {
		return raw
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return raw
	}
	return []byte(s)
}

// diffClients 计算 add / update / delete 三集合。
//
// update 集合的判断策略：仅当字段实质变化时纳入；否则跳过以减少 3x-ui 写入压力。
//
// "字段实质变化" 的判定：把字段集合按 reflect 比较过于宽松（会把数据库回填的字段
// 也算进来），所以这里逐字段对照——业务层关心的字段已经全部列出，足够覆盖。
func diffClients(wanted, existing map[string]xui.ClientSettings) (toAdd, toUpdate map[string]xui.ClientSettings, toDelete map[string]struct{}) {
	toAdd = make(map[string]xui.ClientSettings)
	toUpdate = make(map[string]xui.ClientSettings)
	toDelete = make(map[string]struct{})

	for email, w := range wanted {
		ex, ok := existing[email]
		if !ok {
			toAdd[email] = w
			continue
		}
		if needsUpdate(ex, w) {
			toUpdate[email] = w
		}
	}
	for email := range existing {
		if _, ok := wanted[email]; !ok {
			toDelete[email] = struct{}{}
		}
	}
	return
}

// needsUpdate 判断现有 client 是否需要被覆盖更新。
//
// 仅比较中间件控制的字段；3x-ui 后端可能填入的运行时字段（例如 stat 关联）
// 不在此处比较——把那些纳入会导致每个周期都"看似"变化触发无效更新。
func needsUpdate(ex, want xui.ClientSettings) bool {
	if ex.Enable != want.Enable ||
		ex.LimitIP != want.LimitIP ||
		ex.ID != want.ID ||
		ex.Password != want.Password ||
		ex.Auth != want.Auth ||
		ex.Flow != want.Flow ||
		ex.SubID != want.SubID ||
		ex.ExpiryTime != want.ExpiryTime ||
		ex.TotalGB != want.TotalGB ||
		ex.Reset != want.Reset ||
		ex.Method != want.Method {
		return true
	}
	return false
}

// applyAdd 把 toAdd 中所有 client 一次性追加到 inbound。
// 3x-ui addClient 端点支持单次提交多个 client（settings.clients 是数组）。
func (w *bridgeWorker) applyAdd(ctx context.Context, toAdd map[string]xui.ClientSettings) error {
	if len(toAdd) == 0 {
		return nil
	}
	// 把 map 转成有序切片，避免不同执行序列产生不必要的差异（便于排查日志）。
	list := make([]xui.ClientSettings, 0, len(toAdd))
	for _, c := range toAdd {
		list = append(list, c)
	}
	settings, err := protocol.EncodeAddSettings(list)
	if err != nil {
		return err
	}
	if err := w.xuiC.AddClient(ctx, w.cfg.XuiInboundID, settings); err != nil {
		return fmt.Errorf("addClient：%w", err)
	}
	return nil
}

// applyUpdate 逐个对 toUpdate 中的 client 调 updateClient。
// 3x-ui 不支持批量更新，必须逐个调用。
func (w *bridgeWorker) applyUpdate(ctx context.Context, toUpdate map[string]xui.ClientSettings) error {
	if len(toUpdate) == 0 {
		return nil
	}
	for email, c := range toUpdate {
		key := w.adapter.KeyOf(c)
		if key == "" {
			// 协议适配器返回空 key 通常意味着字段缺失（如 trojan 缺 password）。
			// 拒绝继续：这种 client 本就不该出现在 wanted 集合里。
			return fmt.Errorf("updateClient：%s 的协议主键为空，请检查协议适配器输出", email)
		}
		settings, err := protocol.EncodeSingleSettings(c)
		if err != nil {
			return err
		}
		if err := w.xuiC.UpdateClient(ctx, key, w.cfg.XuiInboundID, settings); err != nil {
			return fmt.Errorf("updateClient %s：%w", email, err)
		}
	}
	return nil
}

// applyDelete 逐个调 delClientByEmail。
//
// 即使 email 已不存在 3x-ui 仍返回 success=true（幂等），所以不需要额外的"先查再删"。
func (w *bridgeWorker) applyDelete(ctx context.Context, toDelete map[string]struct{}) error {
	if len(toDelete) == 0 {
		return nil
	}
	for email := range toDelete {
		if err := w.xuiC.DelClientByEmail(ctx, w.cfg.XuiInboundID, email); err != nil {
			return fmt.Errorf("delClientByEmail %s：%w", email, err)
		}
	}
	return nil
}
