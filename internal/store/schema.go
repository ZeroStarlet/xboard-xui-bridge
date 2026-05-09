// 本文件集中维护 SQLite 的 DDL。
//
// 拆出来单独成文件的原因：
//
//	a) 让阅读者一眼看到全部表结构，无需在查询代码间穿梭；
//	b) 未来若引入 schema 版本号，迁移脚本天然就近于此处管理。
//
// 字段语义（traffic_baseline，下文给出 SQL）：
//
//	reported_up/down    最近一次成功上报到 Xboard 时所"承认"的累计值；
//	                    push 成功后才会推进，是计费真相源。
//	last_seen_up/down   最近一次拉到的 3x-ui 累计值；每次循环无条件刷新，
//	                    用于检测 3x-ui 流量重置（cur < last_seen ⇒ 发生重置）。
//	pending_up/down     检测到重置时把 (last_seen - reported) 累加到这里，
//	                    持久化"重置前未上报的差量"。push 成功后清零；
//	                    push 失败时保留，下个周期 delta = (cur - reported) + pending
//	                    自动包含。
//	last_reported_at    最近一次"成功上报"的 unix 秒，用于运维观测。
//	updated_at          该记录最近一次写入的 unix 秒。
//
// 差量计算流程（详见 store.RecordSeen + Store.ApplyReportSuccess）：
//
//	1) 拉到 cur_up / cur_down；
//	2) 若 cur_* < last_seen_*：检测到重置，pending_* += (last_seen_* - reported_*)，
//	   reported_* 视为 0；持久化新 pending 与 reported；
//	3) last_seen_* = cur_*；
//	4) delta_up   = (cur_up   - reported_up)   + pending_up
//	   delta_down = (cur_down - reported_down) + pending_down；
//	5) 调 Xboard /push；
//	6) 成功 → ApplyReportSuccess(cur_up, cur_down)
//	          推进 reported = cur，pending 清零；
//	   失败 → 不动，下次循环重新计算（pending 已经持久化，不会因失败而丢）。
//
// 不可消除的边界（详见 traffic_sync 注释）：
//
//	push 成功但本地 ApplyReportSuccess 写入失败（极小概率，仅在 SQLite
//	写出错时触发），下次循环会以"未推进的 reported"再次构造同一笔 delta，
//	导致单次重复计费。Xboard /push 当前不接受幂等键，工程上无法消除该窗口；
//	该 trade-off 的合理性已在 README "限制与边界" 章节明确披露。
package store

const schemaSQL = `
CREATE TABLE IF NOT EXISTS traffic_baseline (
    bridge_name        TEXT    NOT NULL,
    email              TEXT    NOT NULL,
    xboard_user_id     INTEGER NOT NULL,
    reported_up        INTEGER NOT NULL DEFAULT 0,
    reported_down      INTEGER NOT NULL DEFAULT 0,
    last_seen_up       INTEGER NOT NULL DEFAULT 0,
    last_seen_down     INTEGER NOT NULL DEFAULT 0,
    pending_up         INTEGER NOT NULL DEFAULT 0,
    pending_down       INTEGER NOT NULL DEFAULT 0,
    last_reported_at   INTEGER NOT NULL DEFAULT 0,
    updated_at         INTEGER NOT NULL,
    PRIMARY KEY (bridge_name, email)
);

CREATE INDEX IF NOT EXISTS idx_traffic_baseline_user
    ON traffic_baseline (bridge_name, xboard_user_id);
`
