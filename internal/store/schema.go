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
//
// ============================================================================
// Web GUI 升级新增的 4 张表（settings / bridges / admin_users / sessions）
// ============================================================================
//
// 自 v0.2 起，xboard-xui-bridge 的"运行配置"与"管理员账户"全部落库 SQLite，
// 不再读取 config.yaml。下列表负责承载原本写在 yaml 里的所有字段以及 Web 面板
// 自身需要的鉴权 / 会话状态。
//
//   settings        分组式 KV，存放 xboard.* / xui.* / log.* / intervals.* /
//                   reporting.* / web.* 等运行参数；key 以"分组.字段"形式组织
//                   （例如 "xboard.api_host"），value 一律为字符串——业务侧
//                   按需自行 strconv 转换；这种"扁平 KV"在 SQLite 上 IO 路径
//                   最短，且未来追加字段不需要修改 schema。
//   bridges         按 name 主键存储桥接配置，逐行映射到 config.Bridge 结构体；
//                   独立成表的目的是支持 Web GUI 上"按行 CRUD"，并保留各项
//                   created_at / updated_at 用于运维观测。
//   admin_users     Web 面板管理员账户表；password_hash 存储 bcrypt 输出；
//                   首次启动时由进程创建一个随机密码的 admin 账户（密码同时
//                   写入日志与 data/initial_password.txt 双通道，防止日志滚动
//                   后无从找回）。
//   sessions        登录后的浏览器会话凭证；token 是 base64url 编码的 32 字节
//                   随机数；服务端校验时按 token 主键查找；过期或登出时删除。
//                   保留 last_seen 字段为"滑动续期"算法服务（每次活动把
//                   expires_at 推到 last_seen + 7d）。注意：token 总寿命的
//                   绝对上限由调用方（auth 中间件）按 created_at +
//                   absoluteMaxLifetime 计算并 cap 后再写回，本表不强制；
//                   详见 sessions 表注释与 SessionRow 字段说明。
//
// 为何 admin_users / sessions 单独成表而不是继续 KV：
//
//   a) 多账户预留：未来允许"只读访客 + 管理员"分级时不需要重写存储层；
//   b) 索引可选：sessions 按 expires_at 索引以加速过期清理；KV 表无法这样做；
//   c) 字段类型语义化：bcrypt hash / 时间戳列直接放对应类型，便于 SQL 调试。
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

-- ============ 配置 KV ============
-- key 命名约定 "<group>.<field>"，例如 "xboard.api_host" / "intervals.user_pull_sec"；
-- value 一律字符串：bool 用 "true"/"false"，int 用十进制字符串。SQLite 没有
-- 严格类型校验，业务侧的 strconv 才是真正的 schema。
CREATE TABLE IF NOT EXISTS settings (
    key        TEXT    NOT NULL PRIMARY KEY,
    value      TEXT    NOT NULL DEFAULT '',
    updated_at INTEGER NOT NULL
);

-- ============ 桥接配置 ============
-- 与 config.Bridge 一一对应。enable 用 0/1（SQLite 没有原生 BOOL）。
-- xboard_node_type 允许空串，为空时由业务侧按 protocol 推断（同 config.Validate 逻辑）。
CREATE TABLE IF NOT EXISTS bridges (
    name             TEXT    NOT NULL PRIMARY KEY,
    xboard_node_id   INTEGER NOT NULL,
    xboard_node_type TEXT    NOT NULL DEFAULT '',
    xui_inbound_id   INTEGER NOT NULL,
    protocol         TEXT    NOT NULL,
    flow             TEXT    NOT NULL DEFAULT '',
    enable           INTEGER NOT NULL DEFAULT 1,
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL
);

-- ============ Web 面板管理员 ============
-- username 唯一；password_hash 是完整 bcrypt 串（含算法标识 + cost + salt + hash）。
-- last_login_at 仅做运维观测，0 表示从未登录。
CREATE TABLE IF NOT EXISTS admin_users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT    NOT NULL UNIQUE,
    password_hash TEXT    NOT NULL,
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL,
    last_login_at INTEGER NOT NULL DEFAULT 0
);

-- ============ 会话凭证 ============
-- token 是 base64url(32 bytes) 即 43 字符；user_id 外键性质（无显式 FK 约束，
-- 删除用户时由业务侧调用 DeleteSessionsForUser 主动清理）。
-- last_seen + expires_at 联合实现"滑动续期"：每次校验通过都把 expires_at
-- 推到 last_seen + 7d。
--
-- 注意：本表不存储"绝对过期上限"。token 总寿命的上限由调用方（auth 中间件）
-- 在每次 TouchSession 前自行用 created_at + maxLifetime 计算并 cap：
--     newExpiresAt = min(now + slidingWindow, created_at + absoluteMaxLifetime)
-- GetSession 已返回 created_at，调用方天然持有该字段，无需在持久层冗余存储
-- 另一列；这样在不增加 schema 复杂度的前提下保证总寿命可控。
CREATE TABLE IF NOT EXISTS sessions (
    token       TEXT    NOT NULL PRIMARY KEY,
    user_id     INTEGER NOT NULL,
    created_at  INTEGER NOT NULL,
    expires_at  INTEGER NOT NULL,
    last_seen   INTEGER NOT NULL,
    ip          TEXT    NOT NULL DEFAULT '',
    user_agent  TEXT    NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_sessions_user
    ON sessions (user_id);

CREATE INDEX IF NOT EXISTS idx_sessions_expires
    ON sessions (expires_at);
`
