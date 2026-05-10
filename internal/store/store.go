// Package store 提供本地状态持久层。
//
// 当前业务用途：
//
//	a) 流量遥测的"已上报基线 + 重置补偿"（traffic_baseline 表，自 v0.1 起存在）；
//	b) Web GUI 升级所需的运行配置 KV、桥接配置行表、管理员账户、会话凭证
//	   （settings / bridges / admin_users / sessions 四张表，自 v0.2 起新增）。
//
// 为什么必须本地持久化：
//
//	差量上报模型要求中间件知道"上次成功上报到 Xboard 时，3x-ui 上的累计值是多少"。
//	不持久化 = 进程重启后这个值丢失 = 重启那一刻把 3x-ui 全部累计值再次上报 =
//	用户被双重计费。这是设计 bug，不是优化项。
//
//	v0.2 起把"运行参数 / 桥接清单 / 管理员账户"也搬入同一个 SQLite，是为了
//	"零 yaml" 部署目标——配置全部由 Web 面板增删改，进程内同步引擎从同一份
//	库里读源真相，避免文件 + GUI 双源真相导致的一致性问题。
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
//	所有方法都接受 context.Context：方便上层在主循环退出时通过 ctx 取消未完成的
//	查询，而不是盲目等待 SQLite 锁释放。
package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
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
//
// 同一错误也用于 settings / bridges / admin_users / sessions 四张表的
// 单行查询接口，调用方据此区分"主键不存在"与真正的查询故障。
var ErrNotFound = errors.New("store: 记录不存在")

// ErrAlreadyExists 表示写入时主键 / 唯一约束冲突。
//
// 触发位置：
//
//	CreateBridge      bridges.name 主键已存在
//	CreateAdminUser   admin_users.username 唯一索引冲突
//
// 设计动机：让 Web Handler 层能稳定地把"重名提交"映射为 HTTP 409，
// 而不必依赖匹配 SQLite 驱动错误字符串（驱动版本变化时易碎）。
// 调用方通过 errors.Is(err, ErrAlreadyExists) 判定。
var ErrAlreadyExists = errors.New("store: 主键或唯一约束冲突")

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

// BridgeRow 是 bridges 表的行 DTO。
//
// 字段与 config.Bridge 一一对应；多出的 CreatedAt / UpdatedAt 仅供运维观测。
//
// 注意：本结构是"持久层 → 业务层"的搬运 DTO，不做任何字段校验；
// Web Handler 在写入前必须先调用 config.Validate（或新引入的等价校验）。
type BridgeRow struct {
	Name           string
	XboardNodeID   int
	XboardNodeType string
	XuiInboundID   int
	Protocol       string
	Flow           string
	Enable         bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// AdminUserRow 是 admin_users 表的行 DTO。
//
// PasswordHash 是完整的 bcrypt 输出串（形如 "$2a$12$..."），不是裸密码。
// LastLoginAt 在用户从未登录时为 zero time（time.Time{}），调用方按
// IsZero() 判断展示策略。
type AdminUserRow struct {
	ID           int64
	Username     string
	PasswordHash string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	LastLoginAt  time.Time
}

// SessionRow 是 sessions 表的行 DTO。
//
// Token 是 base64url 编码的 32 字节随机数（约 43 字符），由 internal/auth
// 在登录成功时生成；本包不参与生成，仅做存储。
//
// 时间字段语义：
//
//	CreatedAt 不变，是 token 寿命的"起点"；
//	LastSeen  最近一次校验通过的时间（每次活动刷新）；
//	ExpiresAt 当前生效的过期时间，由 auth 中间件按"滑动续期 + 绝对寿命 cap"
//	          算法计算后传入 TouchSession 写回；本包不强制其与 CreatedAt
//	          的绝对差值上限——cap 是业务层职责，详见 schema.go 的 sessions
//	          表注释。
type SessionRow struct {
	Token     string
	UserID    int64
	CreatedAt time.Time
	ExpiresAt time.Time
	LastSeen  time.Time
	IP        string
	UserAgent string
}

// Store 定义业务侧依赖的最小接口。
//
// 所有方法都接受 context.Context：方便上层在主循环退出时通过 ctx 取消未完成的查询，
// 而不是盲目等待 SQLite 锁释放。
type Store interface {
	// ============ traffic_baseline ============

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

	// ============ settings（KV）============
	//
	// key 命名约定 "<group>.<field>"；value 一律字符串，业务侧自行 strconv 转换。

	// GetSetting 读取单个 key；exists=false 表示该 key 未设置（与 value=="" 区分）。
	// 仅查询出错才返回 err，"不存在"按 (zero, false, nil) 形式返回，是有意为之——
	// 业务侧 90% 的调用会立刻配合默认值 fallback，写两次 nil-check 太累赘。
	GetSetting(ctx context.Context, key string) (value string, exists bool, err error)

	// SetSetting 单 key 写入（UPSERT 语义）。
	SetSetting(ctx context.Context, key, value string) error

	// SetSettings 批量写入；在单事务内执行，保证一致性快照。
	// 任意一条失败即回滚所有变更。
	SetSettings(ctx context.Context, kv map[string]string) error

	// ListSettings 列出全部 KV，主要供 Web Handler 一次性返回前端编辑表单。
	// 返回的 map 永远非 nil（即使无任何记录）。
	ListSettings(ctx context.Context) (map[string]string, error)

	// DeleteSetting 按 key 删除；幂等，不存在不视为错误。
	DeleteSetting(ctx context.Context, key string) error

	// ============ bridges ============

	// ListBridges 列出所有桥接配置，按 name 升序，供 Web 列表展示与
	// supervisor 装配引擎共用同一份数据视图。
	ListBridges(ctx context.Context) ([]BridgeRow, error)

	// GetBridge 按 name 主键读取；不存在返回 ErrNotFound。
	GetBridge(ctx context.Context, name string) (BridgeRow, error)

	// CreateBridge 写入新桥接；name 已存在时返回 ErrAlreadyExists 包装错误，
	// 调用方用 errors.Is(err, ErrAlreadyExists) 判定即可（错误消息中保留原始
	// name 标识，方便日志定位）。
	// CreatedAt / UpdatedAt 由本方法自动填入当前时间，调用方传入的对应字段被忽略。
	CreateBridge(ctx context.Context, b BridgeRow) error

	// UpdateBridge 按 name 更新所有字段；不存在返回 ErrNotFound。
	// UpdatedAt 自动刷新，CreatedAt 不变。
	UpdateBridge(ctx context.Context, b BridgeRow) error

	// DeleteBridge 按 name 删除；幂等，不存在不视为错误。
	DeleteBridge(ctx context.Context, name string) error

	// ============ admin_users ============

	// ListAdminUsers 列出所有管理员（不含 password_hash 的脱敏版本由
	// Web Handler 自行裁剪）；按 id 升序。
	ListAdminUsers(ctx context.Context) ([]AdminUserRow, error)

	// GetAdminUserByUsername 按用户名查找，不存在返回 ErrNotFound。
	GetAdminUserByUsername(ctx context.Context, username string) (AdminUserRow, error)

	// GetAdminUserByID 按主键 id 查找，不存在返回 ErrNotFound。
	GetAdminUserByID(ctx context.Context, id int64) (AdminUserRow, error)

	// CreateAdminUser 创建管理员；username 唯一冲突返回 ErrAlreadyExists 包装错误，
	// 调用方用 errors.Is(err, ErrAlreadyExists) 判定（错误消息保留 username 标识）。
	// 返回新插入行的自增 id。
	CreateAdminUser(ctx context.Context, username, passwordHash string) (int64, error)

	// UpdateAdminPassword 更新指定 id 用户的密码哈希；不存在返回 ErrNotFound。
	// UpdatedAt 自动刷新。
	//
	// 注意：单独使用本方法不会清除该用户已有 session——若需要"改密 +
	// 强制下线"的原子语义，应当改用 UpdateAdminPasswordAndPurgeSessions。
	UpdateAdminPassword(ctx context.Context, id int64, passwordHash string) error

	// UpdateAdminPasswordAndPurgeSessions 在单事务内更新指定用户的密码哈希
	// 并删除该用户的所有 session。
	//
	// 设计动机：原本 auth.Service.ChangePassword 调 UpdateAdminPassword +
	// DeleteSessionsForUser 两步操作，若第二步失败则"密码已改但旧 session
	// 仍能续期"——攻击者一旦持有挟持的 session，改密后仍可继续访问。
	// 把两步合到一个事务里彻底消除这个时间窗口。
	//
	// 行为：
	//   a) 用户不存在返回 ErrNotFound；事务自动回滚，不做任何修改；
	//   b) 用户存在但无 session：返回 (0, nil)；
	//   c) 正常路径返回 (deletedSessionsCount, nil)；
	//   d) 任何 SQL 失败 → 事务回滚 → 返回包装错误。
	//
	// 用途：auth.Service.ChangePassword 与 auth.Service.ResetPassword。
	UpdateAdminPasswordAndPurgeSessions(ctx context.Context, id int64, passwordHash string) (int64, error)

	// TouchAdminLogin 把 last_login_at 更新到 at；不存在返回 ErrNotFound。
	TouchAdminLogin(ctx context.Context, id int64, at time.Time) error

	// CountAdminUsers 返回当前管理员总数；首次启动时用于判断是否需要引导
	// 创建初始账户。
	CountAdminUsers(ctx context.Context) (int64, error)

	// ============ sessions ============

	// CreateSession 写入新会话；token 主键冲突返回错误（理论上不应发生，
	// 因为 token 是 32 字节随机数）。
	CreateSession(ctx context.Context, s SessionRow) error

	// GetSession 按 token 读取；不存在返回 ErrNotFound。
	// 注意：本方法不校验 ExpiresAt——是否过期由调用方（auth 中间件）判断，
	// 这样可在校验失败时统一删除过期 token。
	GetSession(ctx context.Context, token string) (SessionRow, error)

	// TouchSession 更新会话的 last_seen 与 expires_at（滑动续期）；
	// 不存在返回 ErrNotFound。
	TouchSession(ctx context.Context, token string, lastSeen, expiresAt time.Time) error

	// DeleteSession 按 token 删除；幂等，不存在不视为错误。
	DeleteSession(ctx context.Context, token string) error

	// DeleteSessionsForUser 删除某用户的全部会话（用于改密 / 强制下线）。
	// 返回被删除的行数，便于审计日志。
	DeleteSessionsForUser(ctx context.Context, userID int64) (int64, error)

	// PurgeExpiredSessions 删除 expires_at <= before 的所有会话。
	// 返回被删除的行数；调用方一般在后台定时任务里调用。
	PurgeExpiredSessions(ctx context.Context, before time.Time) (int64, error)

	// ============ 生命周期 ============

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
	// 空闲连接回收：默认 0 表示永不回收（直到 ConnMaxLifetime 到点）。本中间件
	// 多数时间四个同步循环就足够撑满 MaxIdleConns=4 的池；但同步周期之间的
	// 静默期可能长达 10 秒以上，期间空闲连接占着 fd 没必要。10 分钟回收阈值
	// 比 1 小时的 lifetime 更激进——让长期闲置的连接尽快释放，同时仍远高于
	// 60 秒同步周期，连接复用率不受影响。
	db.SetConnMaxIdleTime(10 * time.Minute)

	if _, err := db.ExecContext(pingCtx, schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("初始化 schema：%w", err)
	}
	if err := runMigrations(pingCtx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("schema 迁移：%w", err)
	}

	// 文件权限收紧（v0.5.3 起）：SQLite 默认按进程 umask 创建文件，常见
	// umask=022 会让 .db 落地为 0o644——同主机其它本地用户可读。对于
	// 存放管理员 bcrypt hash + Xboard token + 3x-ui 密码 + session token
	// 的库，这个权限过宽。建表完成后立刻 chmod 0o600（仅文件所有者可读写）
	// 把面对本机旁路的暴露面收敛到与 initial_password.txt 一致。
	//
	// WAL 模式下 SQLite 通常会动态创建 ${path}-wal 与 ${path}-shm 两个副本
	// （它们包含尚未写入主库的事务数据，泄漏价值与主库等同）；建表过程已
	// 触发 WAL 写入，两个副本通常已存在——若不存在（极少边角，如纯只读 /
	// 已 checkpoint 关闭等），helper 内部按 ENOENT 跳过即可。
	//
	// 边界声明：本调用是"打开时一次性硬化"，不是持续监控。SQLite 在
	// checkpoint / 连接关闭等阶段可能重建 -wal / -shm；典型 Unix 部署下
	// 重建会按主库现有权限（已被本调用收紧到 0o600）继承，但不构成强保证。
	// 若运维需要更强保障，应在文件系统层面用 ACL / SELinux 兜底。
	hardenSQLiteFilePerms(path)

	return &sqliteStore{db: db}, nil
}

// hardenSQLiteFilePerms 把主库与可能存在的 -wal/-shm 副本权限收紧到 0o600。
//
// 三个文件分别 chmod；任意一个失败仅 best-effort（不返回错误），原因是：
//
//	a) Windows 下 modernc.org/sqlite 也工作但 chmod 语义被 OS 忽略——失败属于
//	   预期行为，没必要把进程启动卡在文件权限上；
//	b) 副本文件可能尚未存在（极少数边角：首次启动 + 同步建表 + WAL 关闭等），
//	   不存在时 os.Chmod 返回 ENOENT，跳过即可；
//	c) 业务功能不依赖该权限收紧——把它当作"加固而非必备"，避免误把环境差异
//	   升级为启动失败。
//
// 失败仅写 stderr WARN（与 logger.cleanupArchives 同等待级），不阻断进程启动。
func hardenSQLiteFilePerms(path string) {
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Chmod(p, 0o600); err != nil && !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "[warn] 收紧 SQLite 文件 %q 权限失败（非致命）：%v\n", p, err)
		}
	}
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
//
// 注意：四张 v0.2 新增表（settings / bridges / admin_users / sessions）当前
// 不在本切片中——它们一开始就以完整列定义存在；将来若再加列，须在此处追加
// 对应 ALTER 语句。
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

// ============================================================================
// settings KV 实现
// ============================================================================

// GetSetting 实现 Store 接口。
//
// 双返回值（value, exists）的设计动机详见接口注释。注意：value 字段
// 在 schema.go 中声明 NOT NULL DEFAULT ''，因此 exists=true 时 value 永远
// 是字符串（可能为空字符串），不会出现 NULL 扫描错误。
func (s *sqliteStore) GetSetting(ctx context.Context, key string) (string, bool, error) {
	const q = `SELECT value FROM settings WHERE key = ?;`
	var value string
	err := s.db.QueryRowContext(ctx, q, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("GetSetting %q：%w", key, err)
	}
	return value, true, nil
}

// SetSetting 实现 Store 接口；UPSERT 语义。
func (s *sqliteStore) SetSetting(ctx context.Context, key, value string) error {
	now := time.Now().Unix()
	const stmt = `
INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
ON CONFLICT(key) DO UPDATE SET
    value      = excluded.value,
    updated_at = excluded.updated_at;
`
	if _, err := s.db.ExecContext(ctx, stmt, key, value, now); err != nil {
		return fmt.Errorf("SetSetting %q：%w", key, err)
	}
	return nil
}

// SetSettings 实现 Store 接口；批量原子写入。
//
// 事务保证：要么 kv 中所有 key 都成功写入，要么任何一条都不变。
// 这对 Web 表单提交特别重要——半成功状态会让前后端的"已保存"提示与
// 实际行为不一致。
func (s *sqliteStore) SetSettings(ctx context.Context, kv map[string]string) error {
	if len(kv) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return fmt.Errorf("SetSettings 开启事务：%w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	now := time.Now().Unix()
	const stmt = `
INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
ON CONFLICT(key) DO UPDATE SET
    value      = excluded.value,
    updated_at = excluded.updated_at;
`
	for k, v := range kv {
		if _, err := tx.ExecContext(ctx, stmt, k, v, now); err != nil {
			return fmt.Errorf("SetSettings 写入 %q：%w", k, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("SetSettings 提交事务：%w", err)
	}
	committed = true
	return nil
}

// ListSettings 实现 Store 接口；返回 map 永远非 nil。
func (s *sqliteStore) ListSettings(ctx context.Context) (map[string]string, error) {
	const q = `SELECT key, value FROM settings;`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("ListSettings 查询：%w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make(map[string]string, 32)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("ListSettings 扫描：%w", err)
		}
		out[k] = v
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListSettings 遍历：%w", err)
	}
	return out, nil
}

// DeleteSetting 实现 Store 接口；幂等。
func (s *sqliteStore) DeleteSetting(ctx context.Context, key string) error {
	const stmt = `DELETE FROM settings WHERE key = ?;`
	if _, err := s.db.ExecContext(ctx, stmt, key); err != nil {
		return fmt.Errorf("DeleteSetting %q：%w", key, err)
	}
	return nil
}

// ============================================================================
// bridges 实现
// ============================================================================

// ListBridges 实现 Store 接口。
func (s *sqliteStore) ListBridges(ctx context.Context) ([]BridgeRow, error) {
	const q = `
SELECT name, xboard_node_id, xboard_node_type, xui_inbound_id,
       protocol, flow, enable, created_at, updated_at
FROM bridges
ORDER BY name ASC;
`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("ListBridges 查询：%w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []BridgeRow
	for rows.Next() {
		b, err := scanBridge(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListBridges 遍历：%w", err)
	}
	return out, nil
}

// GetBridge 实现 Store 接口。
func (s *sqliteStore) GetBridge(ctx context.Context, name string) (BridgeRow, error) {
	const q = `
SELECT name, xboard_node_id, xboard_node_type, xui_inbound_id,
       protocol, flow, enable, created_at, updated_at
FROM bridges
WHERE name = ?;
`
	row := s.db.QueryRowContext(ctx, q, name)
	b, err := scanBridge(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return BridgeRow{}, ErrNotFound
	}
	return b, err
}

// CreateBridge 实现 Store 接口。
//
// 写入策略：纯 INSERT，依赖 PRIMARY KEY 唯一约束在 name 冲突时报错。
// 命中 SQLite UNIQUE 约束失败被映射为 ErrAlreadyExists，方便 Web Handler
// 把它对应到 HTTP 409。其它错误带上 name 包装后向上抛。
//
// 防御性校验：name / protocol 经 TrimSpace 后不可为空。校验放在持久层是
// 为了让"GUI 表单 → 持久层"的链路在每一段都拒绝非法输入，避免因前端逻辑
// 漏校验导致脏数据落库。
func (s *sqliteStore) CreateBridge(ctx context.Context, b BridgeRow) error {
	if strings.TrimSpace(b.Name) == "" {
		return errors.New("CreateBridge: name 不可为空")
	}
	if strings.TrimSpace(b.Protocol) == "" {
		return errors.New("CreateBridge: protocol 不可为空")
	}
	now := time.Now().Unix()
	const stmt = `
INSERT INTO bridges (name, xboard_node_id, xboard_node_type, xui_inbound_id,
    protocol, flow, enable, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);
`
	if _, err := s.db.ExecContext(ctx, stmt,
		b.Name, b.XboardNodeID, b.XboardNodeType, b.XuiInboundID,
		b.Protocol, b.Flow, boolToInt(b.Enable), now, now,
	); err != nil {
		if isUniqueConstraintError(err) {
			// 把 name 信息保留在外层 message，同时让 errors.Is(err, ErrAlreadyExists)
			// 仍然为 true——双层 wrap 通过 %w 实现链式 unwrap。
			return fmt.Errorf("CreateBridge %q：%w", b.Name, ErrAlreadyExists)
		}
		return fmt.Errorf("CreateBridge %q：%w", b.Name, err)
	}
	return nil
}

// UpdateBridge 实现 Store 接口。
//
// 行不存在时返回 ErrNotFound——通过 RowsAffected 判定。CreatedAt 不参与更新，
// 由数据库保留原值；UpdatedAt 自动刷新。
//
// 防御性校验：与 CreateBridge 一致，name / protocol 经 TrimSpace 后不可为空。
func (s *sqliteStore) UpdateBridge(ctx context.Context, b BridgeRow) error {
	if strings.TrimSpace(b.Name) == "" {
		return errors.New("UpdateBridge: name 不可为空")
	}
	if strings.TrimSpace(b.Protocol) == "" {
		return errors.New("UpdateBridge: protocol 不可为空")
	}
	now := time.Now().Unix()
	const stmt = `
UPDATE bridges SET
    xboard_node_id   = ?,
    xboard_node_type = ?,
    xui_inbound_id   = ?,
    protocol         = ?,
    flow             = ?,
    enable           = ?,
    updated_at       = ?
WHERE name = ?;
`
	res, err := s.db.ExecContext(ctx, stmt,
		b.XboardNodeID, b.XboardNodeType, b.XuiInboundID,
		b.Protocol, b.Flow, boolToInt(b.Enable),
		now, b.Name,
	)
	if err != nil {
		return fmt.Errorf("UpdateBridge %q：%w", b.Name, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("UpdateBridge %q 取受影响行数：%w", b.Name, err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteBridge 实现 Store 接口；幂等。
func (s *sqliteStore) DeleteBridge(ctx context.Context, name string) error {
	const stmt = `DELETE FROM bridges WHERE name = ?;`
	if _, err := s.db.ExecContext(ctx, stmt, name); err != nil {
		return fmt.Errorf("DeleteBridge %q：%w", name, err)
	}
	return nil
}

// ============================================================================
// admin_users 实现
// ============================================================================

// ListAdminUsers 实现 Store 接口。
func (s *sqliteStore) ListAdminUsers(ctx context.Context) ([]AdminUserRow, error) {
	const q = `
SELECT id, username, password_hash, created_at, updated_at, last_login_at
FROM admin_users
ORDER BY id ASC;
`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("ListAdminUsers 查询：%w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []AdminUserRow
	for rows.Next() {
		u, err := scanAdminUser(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListAdminUsers 遍历：%w", err)
	}
	return out, nil
}

// GetAdminUserByUsername 实现 Store 接口。
func (s *sqliteStore) GetAdminUserByUsername(ctx context.Context, username string) (AdminUserRow, error) {
	const q = `
SELECT id, username, password_hash, created_at, updated_at, last_login_at
FROM admin_users
WHERE username = ?;
`
	row := s.db.QueryRowContext(ctx, q, username)
	u, err := scanAdminUser(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return AdminUserRow{}, ErrNotFound
	}
	return u, err
}

// GetAdminUserByID 实现 Store 接口。
func (s *sqliteStore) GetAdminUserByID(ctx context.Context, id int64) (AdminUserRow, error) {
	const q = `
SELECT id, username, password_hash, created_at, updated_at, last_login_at
FROM admin_users
WHERE id = ?;
`
	row := s.db.QueryRowContext(ctx, q, id)
	u, err := scanAdminUser(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return AdminUserRow{}, ErrNotFound
	}
	return u, err
}

// CreateAdminUser 实现 Store 接口；返回新插入行的自增 id。
//
// username 与已有账户冲突时映射为 ErrAlreadyExists（与 CreateBridge 一致），
// 方便 Web Handler 把"用户名已存在"映射成 HTTP 409。
func (s *sqliteStore) CreateAdminUser(ctx context.Context, username, passwordHash string) (int64, error) {
	now := time.Now().Unix()
	const stmt = `
INSERT INTO admin_users (username, password_hash, created_at, updated_at, last_login_at)
VALUES (?, ?, ?, ?, 0);
`
	res, err := s.db.ExecContext(ctx, stmt, username, passwordHash, now, now)
	if err != nil {
		if isUniqueConstraintError(err) {
			// 同 CreateBridge：保留 username 信息的同时让 errors.Is(err, ErrAlreadyExists) 成立。
			return 0, fmt.Errorf("CreateAdminUser %q：%w", username, ErrAlreadyExists)
		}
		return 0, fmt.Errorf("CreateAdminUser %q：%w", username, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("CreateAdminUser %q 取自增 id：%w", username, err)
	}
	return id, nil
}

// UpdateAdminPassword 实现 Store 接口。
func (s *sqliteStore) UpdateAdminPassword(ctx context.Context, id int64, passwordHash string) error {
	now := time.Now().Unix()
	const stmt = `UPDATE admin_users SET password_hash = ?, updated_at = ? WHERE id = ?;`
	res, err := s.db.ExecContext(ctx, stmt, passwordHash, now, id)
	if err != nil {
		return fmt.Errorf("UpdateAdminPassword id=%d：%w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("UpdateAdminPassword id=%d 取受影响行数：%w", id, err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateAdminPasswordAndPurgeSessions 实现 Store 接口。
//
// 单事务保证"密码已更新 + 全部 session 已删除"或"两者都未发生"。
// 详见接口注释中的设计动机。
func (s *sqliteStore) UpdateAdminPasswordAndPurgeSessions(ctx context.Context, id int64, passwordHash string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("UpdateAdminPasswordAndPurgeSessions id=%d 开启事务：%w", id, err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	now := time.Now().Unix()
	const updPwd = `UPDATE admin_users SET password_hash = ?, updated_at = ? WHERE id = ?;`
	updRes, err := tx.ExecContext(ctx, updPwd, passwordHash, now, id)
	if err != nil {
		return 0, fmt.Errorf("UpdateAdminPasswordAndPurgeSessions id=%d 更新密码：%w", id, err)
	}
	n, err := updRes.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("UpdateAdminPasswordAndPurgeSessions id=%d 取更新行数：%w", id, err)
	}
	if n == 0 {
		// 注意：这里返回 ErrNotFound，事务在 defer 中被回滚——保证用户不
		// 存在时不会顺手把别人的 session 删掉。
		return 0, ErrNotFound
	}

	const delSes = `DELETE FROM sessions WHERE user_id = ?;`
	delRes, err := tx.ExecContext(ctx, delSes, id)
	if err != nil {
		return 0, fmt.Errorf("UpdateAdminPasswordAndPurgeSessions id=%d 删除会话：%w", id, err)
	}
	deleted, err := delRes.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("UpdateAdminPasswordAndPurgeSessions id=%d 取删除行数：%w", id, err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("UpdateAdminPasswordAndPurgeSessions id=%d 提交事务：%w", id, err)
	}
	committed = true
	return deleted, nil
}

// TouchAdminLogin 实现 Store 接口。
func (s *sqliteStore) TouchAdminLogin(ctx context.Context, id int64, at time.Time) error {
	const stmt = `UPDATE admin_users SET last_login_at = ? WHERE id = ?;`
	res, err := s.db.ExecContext(ctx, stmt, timeToUnix(at), id)
	if err != nil {
		return fmt.Errorf("TouchAdminLogin id=%d：%w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("TouchAdminLogin id=%d 取受影响行数：%w", id, err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// CountAdminUsers 实现 Store 接口。
func (s *sqliteStore) CountAdminUsers(ctx context.Context) (int64, error) {
	const q = `SELECT COUNT(*) FROM admin_users;`
	var n int64
	if err := s.db.QueryRowContext(ctx, q).Scan(&n); err != nil {
		return 0, fmt.Errorf("CountAdminUsers：%w", err)
	}
	return n, nil
}

// ============================================================================
// sessions 实现
// ============================================================================

// CreateSession 实现 Store 接口。
//
// 防御性默认：CreatedAt / LastSeen 为零时填入当前时间，便于审计与续期算法
// 在调用方偶发漏填时不会读到 zero time。ExpiresAt 必须由调用方显式给出
// （过期策略属于业务决策，此处不替业务挑默认值）；零值会被拒绝。
func (s *sqliteStore) CreateSession(ctx context.Context, ses SessionRow) error {
	if ses.ExpiresAt.IsZero() {
		return errors.New("CreateSession: ExpiresAt 必须由调用方显式指定")
	}
	now := time.Now().UTC()
	if ses.CreatedAt.IsZero() {
		ses.CreatedAt = now
	}
	if ses.LastSeen.IsZero() {
		ses.LastSeen = now
	}
	const stmt = `
INSERT INTO sessions (token, user_id, created_at, expires_at, last_seen, ip, user_agent)
VALUES (?, ?, ?, ?, ?, ?, ?);
`
	if _, err := s.db.ExecContext(ctx, stmt,
		ses.Token, ses.UserID,
		timeToUnix(ses.CreatedAt), timeToUnix(ses.ExpiresAt), timeToUnix(ses.LastSeen),
		ses.IP, ses.UserAgent,
	); err != nil {
		return fmt.Errorf("CreateSession user_id=%d token=%s：%w", ses.UserID, sessionTokenHash(ses.Token), err)
	}
	return nil
}

// GetSession 实现 Store 接口。
//
// 错误信息会带上 token 的稳定 hash 短前缀（详见 sessionTokenHash），不包含
// 完整 token，避免日志成为旁路凭证泄露源。
func (s *sqliteStore) GetSession(ctx context.Context, token string) (SessionRow, error) {
	const q = `
SELECT token, user_id, created_at, expires_at, last_seen, ip, user_agent
FROM sessions
WHERE token = ?;
`
	row := s.db.QueryRowContext(ctx, q, token)
	ses, err := scanSession(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionRow{}, ErrNotFound
	}
	if err != nil {
		return SessionRow{}, fmt.Errorf("GetSession token=%s：%w", sessionTokenHash(token), err)
	}
	return ses, nil
}

// TouchSession 实现 Store 接口。
//
// 调用方语义：在 GetSession 拿到 created_at 后，按
//
//	newExpiresAt = min(now + slidingWindow, created_at + absoluteMaxLifetime)
//
// 计算并传入；本方法只负责持久化，不再做边界 cap。
func (s *sqliteStore) TouchSession(ctx context.Context, token string, lastSeen, expiresAt time.Time) error {
	const stmt = `UPDATE sessions SET last_seen = ?, expires_at = ? WHERE token = ?;`
	res, err := s.db.ExecContext(ctx, stmt, timeToUnix(lastSeen), timeToUnix(expiresAt), token)
	if err != nil {
		return fmt.Errorf("TouchSession token=%s：%w", sessionTokenHash(token), err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("TouchSession token=%s 取受影响行数：%w", sessionTokenHash(token), err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteSession 实现 Store 接口；幂等。
func (s *sqliteStore) DeleteSession(ctx context.Context, token string) error {
	const stmt = `DELETE FROM sessions WHERE token = ?;`
	if _, err := s.db.ExecContext(ctx, stmt, token); err != nil {
		return fmt.Errorf("DeleteSession token=%s：%w", sessionTokenHash(token), err)
	}
	return nil
}

// DeleteSessionsForUser 实现 Store 接口；返回被删除的行数。
//
// 用途场景：
//
//	a) 改密后强制下线该用户已有所有会话；
//	b) 删除管理员账户时清理残留 session。
//
// 幂等：用户没有任何 session 时返回 (0, nil)。
func (s *sqliteStore) DeleteSessionsForUser(ctx context.Context, userID int64) (int64, error) {
	const stmt = `DELETE FROM sessions WHERE user_id = ?;`
	res, err := s.db.ExecContext(ctx, stmt, userID)
	if err != nil {
		return 0, fmt.Errorf("DeleteSessionsForUser user_id=%d：%w", userID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("DeleteSessionsForUser user_id=%d 取受影响行数：%w", userID, err)
	}
	return n, nil
}

// PurgeExpiredSessions 实现 Store 接口；返回受影响行数。
//
// 错误信息中带上 before 的 RFC3339 表示，方便定位是哪一轮清理任务出问题。
func (s *sqliteStore) PurgeExpiredSessions(ctx context.Context, before time.Time) (int64, error) {
	const stmt = `DELETE FROM sessions WHERE expires_at <= ?;`
	res, err := s.db.ExecContext(ctx, stmt, timeToUnix(before))
	if err != nil {
		return 0, fmt.Errorf("PurgeExpiredSessions before=%s：%w", before.UTC().Format(time.RFC3339), err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("PurgeExpiredSessions before=%s 取受影响行数：%w", before.UTC().Format(time.RFC3339), err)
	}
	return n, nil
}

// Close 关闭底层 *sql.DB。
func (s *sqliteStore) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

// ============================================================================
// 扫描辅助函数
// ============================================================================

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

// scanBridge 把数据库行扫描到 BridgeRow。
//
// 字段顺序必须与 ListBridges / GetBridge 中 SELECT 子句的列序严格一致；
// 任何 SELECT 列序变更都需要同步修改本函数的扫描指针顺序。
func scanBridge(scan func(...any) error) (BridgeRow, error) {
	var (
		b                BridgeRow
		enableInt        int64
		createdAtTS, updatedAtTS int64
	)
	err := scan(
		&b.Name, &b.XboardNodeID, &b.XboardNodeType, &b.XuiInboundID,
		&b.Protocol, &b.Flow, &enableInt,
		&createdAtTS, &updatedAtTS,
	)
	if err != nil {
		return BridgeRow{}, err
	}
	b.Enable = enableInt != 0
	b.CreatedAt = unixToTime(createdAtTS)
	b.UpdatedAt = unixToTime(updatedAtTS)
	return b, nil
}

// scanAdminUser 把数据库行扫描到 AdminUserRow。
//
// last_login_at 为 0 时映射到 zero time（time.Time{}），调用方按 IsZero()
// 判断展示策略（"从未登录"等）。
func scanAdminUser(scan func(...any) error) (AdminUserRow, error) {
	var (
		u                                            AdminUserRow
		createdAtTS, updatedAtTS, lastLoginAtTS int64
	)
	err := scan(
		&u.ID, &u.Username, &u.PasswordHash,
		&createdAtTS, &updatedAtTS, &lastLoginAtTS,
	)
	if err != nil {
		return AdminUserRow{}, err
	}
	u.CreatedAt = unixToTime(createdAtTS)
	u.UpdatedAt = unixToTime(updatedAtTS)
	u.LastLoginAt = unixToTime(lastLoginAtTS)
	return u, nil
}

// scanSession 把数据库行扫描到 SessionRow。
func scanSession(scan func(...any) error) (SessionRow, error) {
	var (
		s                                                SessionRow
		createdAtTS, expiresAtTS, lastSeenTS int64
	)
	err := scan(
		&s.Token, &s.UserID,
		&createdAtTS, &expiresAtTS, &lastSeenTS,
		&s.IP, &s.UserAgent,
	)
	if err != nil {
		return SessionRow{}, err
	}
	s.CreatedAt = unixToTime(createdAtTS)
	s.ExpiresAt = unixToTime(expiresAtTS)
	s.LastSeen = unixToTime(lastSeenTS)
	return s, nil
}

// boolToInt 把 Go bool 编码为 SQLite 存储值（0/1）。
//
// SQLite 没有原生 BOOL 类型，约定列声明为 INTEGER 时用 0/1 表达——
// 与官方 sqlite3 文档一致，也与 3x-ui 自身代码保持一致风格，便于
// 第三方运维工具读库时一眼识别。
func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// unixToTime 把 unix 秒映射为 time.Time；ts<=0 视为 zero time。
//
// 与 scanBaseline 中 last_reported_at 的处理策略保持一致：
// "0 表示从未发生"在持久化层用 0 表达，业务层用 IsZero() 判断。
func unixToTime(ts int64) time.Time {
	if ts <= 0 {
		return time.Time{}
	}
	return time.Unix(ts, 0).UTC()
}

// timeToUnix 把 time.Time 编码为 unix 秒；零值时间映射为 0。
//
// 反函数 unixToTime；二者必须保持对偶——任何修改都需同步更新另一个。
func timeToUnix(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

// isUniqueConstraintError 判断 SQLite 写入返回的错误是否为 UNIQUE 约束冲突。
//
// 实现策略：modernc.org/sqlite 把驱动错误格式化为可读字符串（其中含
// "UNIQUE constraint failed"），不同版本驱动这一片段保持稳定；
// 因此用 strings.Contains 判定即可，无需引入驱动私有错误码常量。
//
// 如果将来切换到 cgo 版 mattn/go-sqlite3 也可识别（同样字符串）；切换到
// PostgreSQL 等外部存储时本函数自然作废，调用方届时改用对应方言判定。
func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// sessionTokenHash 返回 token 的 SHA-256 前 8 字符 hex，用于日志短标识。
//
// 设计动机：直接把完整 token 写入日志会让日志成为旁路凭证泄露源——任何
// 拿到日志的人都可以挟该 token 假冒登录。这里用单向 hash 截断 8 字符，
// 既能在日志里追踪同一 token 的多次操作，又彻底无法反推原文。
//
// 截断到 8 字符（32 bits）的代价：碰撞概率约 1/4G，对于会话总量在百万
// 级以下的中间件场景足够区分。空 token 返回空字符串。
func sessionTokenHash(token string) string {
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:4])
}
