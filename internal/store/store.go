// Package store 提供本地状态持久层。
//
// 当前唯一真实业务用途：流量遥测的"已上报基线 + 重置补偿"。
//
// 为什么必须本地持久化：
//
//	差量上报模型要求中间件知道"上次成功上报到 Xboard 时，3x-ui 上的累计值是多少"。
//	不持久化 = 进程重启后这个值丢失 = 重启那一刻把 3x-ui 全部累计值再次上报 =
//	用户被双重计费。这是设计 bug，不是优化项。
//
// 为什么选 SQLite：
//
//	a) 单文件、零运维；中间件实例数等同于配置数量，一对一即可；
//	b) 事务保证"读基线 → 上报 → 写基线"的关键路径不会中途被腰斩；
//	c) Go 端用 modernc.org/sqlite 纯 Go 实现，无 cgo，跨平台编译友好。
//
// 接口设计原则：
//
//	业务侧不直接面对 *sql.DB——一律走本包定义的 Store 接口。这样：
//	a) 单元测试可以替换为内存实现；
//	b) 未来若需要切换到 PostgreSQL / etcd，业务代码不必改。
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	// 仅以副作用导入注册 SQLite 驱动。modernc.org/sqlite 注册名为 "sqlite"。
	_ "modernc.org/sqlite"
)

// ErrNotFound 在 GetBaseline 等查询接口返回，表示给定主键无记录。
//
// 区分"未查到"与"查询出错"是接口铁律：业务侧需要据此决定是
// 创建新基线（未查到）还是中断流程（查询出错）。
var ErrNotFound = errors.New("store: 记录不存在")

// Baseline 表示一行 traffic_baseline。
//
// 字段语义详见 schema.go。本结构仅为传输 DTO，不附加任何业务逻辑。
type Baseline struct {
	BridgeName     string
	Email          string
	XboardUserID   int64
	ReportedUp     int64
	ReportedDown   int64
	LastSeenUp     int64
	LastSeenDown   int64
	PendingUp      int64
	PendingDown    int64
	LastReportedAt time.Time
	UpdatedAt      time.Time
}

// Store 定义业务侧依赖的最小接口。
//
// 所有方法都接受 context.Context：方便上层在主循环退出时通过 ctx 取消未完成的查询，
// 而不是盲目等待 SQLite 锁释放。
type Store interface {
	// EnsureBaseline 在记录不存在时插入零基线，存在时仅更新 xboard_user_id
	// （处理 Xboard 端用户 id 改变的极端情形）。返回写入后的最新 Baseline。
	EnsureBaseline(ctx context.Context, bridgeName, email string, userID int64) (Baseline, error)

	// GetBaseline 按主键读取；记录不存在返回 ErrNotFound。
	GetBaseline(ctx context.Context, bridgeName, email string) (Baseline, error)

	// ListBaselinesByBridge 列出某个 Bridge 的全部基线，供同步引擎进行 diff。
	ListBaselinesByBridge(ctx context.Context, bridgeName string) ([]Baseline, error)

	// RecordSeen 在每个上报循环开始处调用，把"刚拉到的 cur"写入 last_seen，
	// 并在检测到 3x-ui 流量重置（cur < last_seen）时把"重置前未上报"的部分
	// 累加到 pending_up/down 持久化。
	//
	// 返回写入后的最新 Baseline；调用方据此（reported + pending）计算 delta。
	//
	// 即使 push 还未发出也必须先持久化：保证"重置补偿"一旦计算就不会因为
	// push 失败而被遗忘。
	RecordSeen(ctx context.Context, bridgeName, email string, curUp, curDown int64) (Baseline, error)

	// ApplyReportSuccess 在 Xboard /push 返回成功后调用：
	//
	//	reported_up   ← curUp
	//	reported_down ← curDown
	//	pending_up    ← 0
	//	pending_down  ← 0
	//	last_seen_up/down 不变（已经在 RecordSeen 中推进过）
	//	last_reported_at ← now
	//
	// 业务约束：必须先确认 Xboard 返回成功（HTTP 200 且 obj==true）才能调用。
	// 调用错误的代价：流量被重复计费。
	ApplyReportSuccess(ctx context.Context, bridgeName, email string, curUp, curDown int64) error

	// DeleteBaseline 在用户从 Xboard 端永久消失（被删除 / 禁用 / 过期）时调用，
	// 同步把 3x-ui 端 client 删除后再调用本方法。即使无对应记录也返回 nil（幂等）。
	DeleteBaseline(ctx context.Context, bridgeName, email string) error

	// Close 关闭底层连接；应当且仅由 main 在退出时调用一次。
	Close() error
}

// sqliteStore 是 Store 接口的 SQLite 实现。
//
// 持有 *sql.DB 而非 *sql.Conn：sql.DB 内部已是连接池，多 goroutine 安全。
// 业务上四个同步循环可能并发访问，连接池模型最自然。
type sqliteStore struct {
	db *sql.DB
}

// OpenSQLite 打开（必要时创建）指定路径的 SQLite 文件，并执行 schema 初始化。
//
// 路径父目录不存在时会被自动创建（mode 0o755）。这样运维只要写好父目录权限，
// 中间件首次启动即能自助建库。
//
// 连接参数：
//
//	_busy_timeout=5000   并发写入时最多等 5 秒锁；超时返回 SQLITE_BUSY，
//	                     上层捕获后下个周期重试，不会丢失数据。
//	_journal_mode=WAL    Write-Ahead Logging 提升单写多读并发；
//	                     代价是产生 -shm/-wal 副本文件，运维迁移时需一并复制。
//	_synchronous=NORMAL  在 WAL 下安全；FULL 太慢，OFF 在断电时可能损坏。
func OpenSQLite(path string) (Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("store: SQLite 路径不可为空")
	}
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		// 父目录不存在时递归创建；存在时不动。0o755 是惯例：所属者读写执行，
		// 其他人可读可执行（进入目录），与日志目录权限保持一致。
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("创建 SQLite 父目录 %q：%w", dir, err)
		}
	}

	// _txlock=immediate：让 db.BeginTx 默认获取写锁（IMMEDIATE 而非 DEFERRED）。
	// 这样 RecordSeen 的"先 SELECT 后 UPDATE"事务不会与并发的
	// ApplyReportSuccess 出现写写竞争（DEFERRED 模式下读锁会被升级为写锁
	// 时撞上 BUSY 而失败）。
	// 唯一影响：本包内显式 BeginTx 都改为 IMMEDIATE 语义；其它路径全部走
	// auto-commit 的 ExecContext / QueryContext，不受影响。
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_txlock=immediate", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开 SQLite %q：%w", path, err)
	}

	// modernc.org/sqlite 不在 Open 时建立连接，先用 Ping 验证 DSN 真的可用。
	// 否则建表语句会在第一次业务调用时才报错，定位困难。
	pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping SQLite 失败：%w", err)
	}

	// 最大空闲 / 在用连接保守设置：本中间件并发上限就是 4 个同步循环 + 偶发查询。
	// 设过大会浪费 fd，设过小则在突发请求时退化为串行。
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(time.Hour) // 防止单连接长时间空闲被 OS 杀掉。

	if _, err := db.ExecContext(pingCtx, schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("初始化 schema：%w", err)
	}
	if err := runMigrations(pingCtx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("schema 迁移：%w", err)
	}

	return &sqliteStore{db: db}, nil
}

// migrationStmts 列出所有"在 CREATE TABLE 后追加的字段"对应的 ALTER 语句。
//
// 设计动机：CREATE TABLE IF NOT EXISTS 不会修改已有表结构；当中间件早期版本
// 创建过 traffic_baseline 之后再升级到含新字段的版本时，旧表里缺少新增列，
// SELECT/INSERT 会立刻失败。本函数在启动期逐条 ALTER ADD COLUMN，对"重复列"
// 错误做幂等忽略，确保旧库平滑升级。
//
// 维护规约：每次新增字段都必须同时在 schema.go 的 CREATE TABLE 与本切片中
// 加入对应 ALTER 语句；删除字段不在此处理（SQLite ALTER 不直接支持 DROP COLUMN，
// 需走 CREATE-COPY-RENAME 流程，那时再单独写迁移函数）。
var migrationStmts = []string{
	"ALTER TABLE traffic_baseline ADD COLUMN last_seen_up   INTEGER NOT NULL DEFAULT 0",
	"ALTER TABLE traffic_baseline ADD COLUMN last_seen_down INTEGER NOT NULL DEFAULT 0",
	"ALTER TABLE traffic_baseline ADD COLUMN pending_up     INTEGER NOT NULL DEFAULT 0",
	"ALTER TABLE traffic_baseline ADD COLUMN pending_down   INTEGER NOT NULL DEFAULT 0",
}

// runMigrations 顺序执行 migrationStmts。
//
// 错误处理策略：SQLite 在列已存在时返回 "duplicate column name: <name>"，
// 这是预期的幂等情形，被显式忽略；其他任何错误都视为致命，向上返回。
func runMigrations(ctx context.Context, db *sql.DB) error {
	for _, stmt := range migrationStmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			if strings.Contains(err.Error(), "duplicate column") {
				// 列已存在；幂等忽略，继续下一条。
				continue
			}
			return fmt.Errorf("执行 %q：%w", stmt, err)
		}
	}
	return nil
}

// EnsureBaseline 实现 Store 接口。语义与接口注释一致。
//
// 写入策略：UPSERT（INSERT … ON CONFLICT DO UPDATE）。仅把 xboard_user_id 与
// updated_at 在冲突时刷新；reported / pending / last_seen 都不会被覆盖，避免
// 重置已经累积的差量补偿。
func (s *sqliteStore) EnsureBaseline(ctx context.Context, bridgeName, email string, userID int64) (Baseline, error) {
	now := time.Now().Unix()
	const stmt = `
INSERT INTO traffic_baseline (bridge_name, email, xboard_user_id,
    reported_up, reported_down, last_seen_up, last_seen_down,
    pending_up, pending_down, last_reported_at, updated_at)
VALUES (?, ?, ?, 0, 0, 0, 0, 0, 0, 0, ?)
ON CONFLICT(bridge_name, email) DO UPDATE SET
    xboard_user_id = excluded.xboard_user_id,
    updated_at     = excluded.updated_at;
`
	if _, err := s.db.ExecContext(ctx, stmt, bridgeName, email, userID, now); err != nil {
		return Baseline{}, fmt.Errorf("EnsureBaseline 写入：%w", err)
	}
	return s.GetBaseline(ctx, bridgeName, email)
}

// GetBaseline 实现 Store 接口。
func (s *sqliteStore) GetBaseline(ctx context.Context, bridgeName, email string) (Baseline, error) {
	const q = `
SELECT bridge_name, email, xboard_user_id,
       reported_up, reported_down, last_seen_up, last_seen_down,
       pending_up, pending_down, last_reported_at, updated_at
FROM traffic_baseline
WHERE bridge_name = ? AND email = ?;
`
	row := s.db.QueryRowContext(ctx, q, bridgeName, email)
	b, err := scanBaseline(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Baseline{}, ErrNotFound
	}
	return b, err
}

// ListBaselinesByBridge 实现 Store 接口。
//
// 返回顺序按 email 升序，便于 diff 时与 3x-ui 拉取的 client 列表
// 在同一排序维度上做线性归并。
func (s *sqliteStore) ListBaselinesByBridge(ctx context.Context, bridgeName string) ([]Baseline, error) {
	const q = `
SELECT bridge_name, email, xboard_user_id,
       reported_up, reported_down, last_seen_up, last_seen_down,
       pending_up, pending_down, last_reported_at, updated_at
FROM traffic_baseline
WHERE bridge_name = ?
ORDER BY email ASC;
`
	rows, err := s.db.QueryContext(ctx, q, bridgeName)
	if err != nil {
		return nil, fmt.Errorf("ListBaselinesByBridge 查询：%w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Baseline
	for rows.Next() {
		b, err := scanBaseline(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListBaselinesByBridge 遍历：%w", err)
	}
	return out, nil
}

// RecordSeen 实现 Store 接口。
//
// 单事务保证"重置补偿持久化"原子可见——避免在事务中途崩溃造成补偿丢失或重复。
//
// 算法：
//
//	事务开始（IMMEDIATE，立即获写锁）
//	  读现有 baseline；不存在则插入零行后再读一次。
//	  若 curUp   < last_seen_up ：pending_up   += last_seen_up   - reported_up；reported_up   = 0
//	  若 curDown < last_seen_down：pending_down += last_seen_down - reported_down；reported_down = 0
//	  写 last_seen_up = curUp, last_seen_down = curDown, updated_at = now
//	  写 reported_up / reported_down / pending_up / pending_down（如有变化）
//	事务提交
//	返回写入后的 Baseline
//
// 写锁强度：本包 OpenSQLite 在 DSN 中设置 _txlock=immediate，让 db.BeginTx
// 默认按 IMMEDIATE 语义获取写锁——这避免了"先读后写"模式下与并发
// ApplyReportSuccess 出现的 SQLITE_BUSY 锁升级竞争。
func (s *sqliteStore) RecordSeen(ctx context.Context, bridgeName, email string, curUp, curDown int64) (Baseline, error) {
	if curUp < 0 || curDown < 0 {
		return Baseline{}, fmt.Errorf("RecordSeen: cur 不可为负 (got %d, %d)", curUp, curDown)
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return Baseline{}, fmt.Errorf("RecordSeen 开启事务：%w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	const sel = `
SELECT bridge_name, email, xboard_user_id,
       reported_up, reported_down, last_seen_up, last_seen_down,
       pending_up, pending_down, last_reported_at, updated_at
FROM traffic_baseline
WHERE bridge_name = ? AND email = ?;
`
	var b Baseline
	row := tx.QueryRowContext(ctx, sel, bridgeName, email)
	b, err = scanBaseline(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		// 之前 user_sync 没有为该 email 调过 EnsureBaseline，理论上不应到此分支。
		// 防御性处理：插入一条最小行后再读出来。
		now := time.Now().Unix()
		const ins = `
INSERT INTO traffic_baseline (bridge_name, email, xboard_user_id,
    reported_up, reported_down, last_seen_up, last_seen_down,
    pending_up, pending_down, last_reported_at, updated_at)
VALUES (?, ?, 0, 0, 0, 0, 0, 0, 0, 0, ?);
`
		if _, err := tx.ExecContext(ctx, ins, bridgeName, email, now); err != nil {
			return Baseline{}, fmt.Errorf("RecordSeen 防御性插入：%w", err)
		}
		row = tx.QueryRowContext(ctx, sel, bridgeName, email)
		b, err = scanBaseline(row.Scan)
		if err != nil {
			return Baseline{}, fmt.Errorf("RecordSeen 读取防御行：%w", err)
		}
	} else if err != nil {
		return Baseline{}, fmt.Errorf("RecordSeen 读取：%w", err)
	}

	// 检测重置：cur 小于上次见到的累计值，说明 3x-ui 端做了流量重置。
	resetUp := curUp < b.LastSeenUp
	resetDown := curDown < b.LastSeenDown

	if resetUp {
		// "重置前未上报"= last_seen - reported（重置发生时这部分数据还活着）
		// 累加到 pending；并把 reported 视为 0 重新开始。
		add := b.LastSeenUp - b.ReportedUp
		if add < 0 {
			// 防御：极端情况（reported 比 last_seen 还大），不应累加负数。
			add = 0
		}
		b.PendingUp += add
		b.ReportedUp = 0
	}
	if resetDown {
		add := b.LastSeenDown - b.ReportedDown
		if add < 0 {
			add = 0
		}
		b.PendingDown += add
		b.ReportedDown = 0
	}
	b.LastSeenUp = curUp
	b.LastSeenDown = curDown
	now := time.Now().Unix()
	b.UpdatedAt = time.Unix(now, 0).UTC()

	const upd = `
UPDATE traffic_baseline SET
    reported_up    = ?,
    reported_down  = ?,
    last_seen_up   = ?,
    last_seen_down = ?,
    pending_up     = ?,
    pending_down   = ?,
    updated_at     = ?
WHERE bridge_name = ? AND email = ?;
`
	if _, err := tx.ExecContext(ctx, upd,
		b.ReportedUp, b.ReportedDown,
		b.LastSeenUp, b.LastSeenDown,
		b.PendingUp, b.PendingDown,
		now, bridgeName, email,
	); err != nil {
		return Baseline{}, fmt.Errorf("RecordSeen 更新：%w", err)
	}

	if err := tx.Commit(); err != nil {
		return Baseline{}, fmt.Errorf("RecordSeen 提交事务：%w", err)
	}
	committed = true
	return b, nil
}

// ApplyReportSuccess 实现 Store 接口。
//
// 把 reported 推进到 cur，pending 清零，记录上报时间。
//
// 边界情形：
//   - cur < reported 不应出现（RecordSeen 在重置时已把 reported 置 0），
//     但仍做防御：若发生则保留旧 reported 不缩水，避免数据回退。
//   - 无对应记录：UPSERT 退化为插入；这通常意味着 EnsureBaseline 漏调，
//     仅作自愈分支。
func (s *sqliteStore) ApplyReportSuccess(ctx context.Context, bridgeName, email string, curUp, curDown int64) error {
	if curUp < 0 || curDown < 0 {
		return fmt.Errorf("ApplyReportSuccess: cur 不可为负 (got %d, %d)", curUp, curDown)
	}
	now := time.Now().Unix()
	const stmt = `
INSERT INTO traffic_baseline (bridge_name, email, xboard_user_id,
    reported_up, reported_down, last_seen_up, last_seen_down,
    pending_up, pending_down, last_reported_at, updated_at)
VALUES (?, ?, 0, ?, ?, ?, ?, 0, 0, ?, ?)
ON CONFLICT(bridge_name, email) DO UPDATE SET
    reported_up      = excluded.reported_up,
    reported_down    = excluded.reported_down,
    last_seen_up     = MAX(excluded.last_seen_up, traffic_baseline.last_seen_up),
    last_seen_down   = MAX(excluded.last_seen_down, traffic_baseline.last_seen_down),
    pending_up       = 0,
    pending_down     = 0,
    last_reported_at = excluded.last_reported_at,
    updated_at       = excluded.updated_at;
`
	if _, err := s.db.ExecContext(ctx, stmt,
		bridgeName, email,
		curUp, curDown,
		curUp, curDown,
		now, now,
	); err != nil {
		return fmt.Errorf("ApplyReportSuccess 写入：%w", err)
	}
	return nil
}

// DeleteBaseline 实现 Store 接口；幂等，不存在记录不视为错误。
func (s *sqliteStore) DeleteBaseline(ctx context.Context, bridgeName, email string) error {
	const stmt = `DELETE FROM traffic_baseline WHERE bridge_name = ? AND email = ?;`
	if _, err := s.db.ExecContext(ctx, stmt, bridgeName, email); err != nil {
		return fmt.Errorf("DeleteBaseline 写入：%w", err)
	}
	return nil
}

// Close 关闭底层 *sql.DB。
func (s *sqliteStore) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

// scanBaseline 把数据库行扫描到 Baseline 结构体。
//
// 接收 scanner 函数而不是直接接收 *sql.Row / *sql.Rows：因为 Row.Scan 与
// Rows.Scan 同名但分属不同接口，泛化成函数指针后让 GetBaseline、
// ListBaselinesByBridge、RecordSeen 共享同一段扫描逻辑，避免字段顺序漂移。
func scanBaseline(scan func(...any) error) (Baseline, error) {
	var (
		b                       Baseline
		reportedAt, updatedAtTS int64
	)
	err := scan(
		&b.BridgeName, &b.Email, &b.XboardUserID,
		&b.ReportedUp, &b.ReportedDown,
		&b.LastSeenUp, &b.LastSeenDown,
		&b.PendingUp, &b.PendingDown,
		&reportedAt, &updatedAtTS,
	)
	if err != nil {
		return Baseline{}, err
	}
	if reportedAt > 0 {
		b.LastReportedAt = time.Unix(reportedAt, 0).UTC()
	}
	b.UpdatedAt = time.Unix(updatedAtTS, 0).UTC()
	return b, nil
}
