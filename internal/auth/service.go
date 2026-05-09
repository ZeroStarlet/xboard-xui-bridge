// Package auth 提供 Web 面板的鉴权服务：密码哈希、会话生命周期、首次启动
// admin 引导。
//
// 设计原则：
//
//  1. 不依赖 HTTP 层：本包只面向 store.Store 与 config 抽象工作，便于
//     单元测试无需起 HTTP server。Web Handler（M5）薄薄一层把 HTTP 请求
//     翻译为 Service 调用。
//  2. 防用户枚举：登录失败一律返回 ErrInvalidCredentials，不区分"用户名
//     不存在"与"密码不对"——避免攻击者通过响应差异枚举存在的用户名。
//  3. 滑动续期 + 绝对寿命双约束：每次 VerifySession 把会话过期时间推到
//     min(now + slidingWindow, createdAt + absoluteMaxLifetime)。即使
//     攻击者拿到 token 一直续，token 总寿命也不会超过绝对上限。
//  4. 改密强制下线：ChangePassword 成功后清除该用户所有会话——攻击者
//     若已挟持过的 token 立即失效；缺点是用户自己也得重新登录，但这是
//     标准安全权衡。
//  5. 首次启动 admin 引导走"双通道"输出明文密码：日志（INFO 级）+
//     文件（data/initial_password.txt，0600 权限）。日志被滚动 / 文件被
//     误删任一通道仍可找回——这与 README 的"首次启动" 描述一致。
//
// bcrypt cost 选择：12 是 2025 年常见 Web 后台的合理上限。验证一次 ~150ms，
// 对登录页延迟可感但仍可接受；攻击者每秒 ~7 次 bcrypt 校验，对穷举密码
// 极不友好。生产环境若觉得 150ms 过慢可降到 10（~40ms / 校验），但低于
// 10 不推荐——bcrypt 设计就是"可校准慢"。
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	stdsync "sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/xboard-bridge/xboard-xui-bridge/internal/store"
)

// 错误常量。所有"业务侧可识别的错误"都用 errors.Is 比较。
//
// 不在错误消息里区分"用户名不存在"与"密码不对"——防御用户枚举。
var (
	// ErrInvalidCredentials 登录用户名或密码错误。
	ErrInvalidCredentials = errors.New("auth: 用户名或密码错误")

	// ErrSessionExpired token 不存在 / 已过期 / 撞到绝对寿命上限。
	// Web 中间件遇到该错误应当 401 + 清除浏览器 cookie。
	ErrSessionExpired = errors.New("auth: 会话已过期或不存在")

	// ErrPasswordTooShort 改密 / 创建用户时密码长度不达标。
	ErrPasswordTooShort = errors.New("auth: 密码长度未达最低要求")

	// ErrPasswordReuse 改密时新密码与旧密码相同——避免"改了等于没改"。
	ErrPasswordReuse = errors.New("auth: 新密码不可与旧密码相同")
)

// 默认参数常量。Service 构造时未指定的字段会落到这些值。
//
// MinPasswordLength 选 8：与 NIST SP 800-63B 的建议一致；对中间件 admin
// 这个低频登录场景已足够，太长反而让运维倾向"123456 + 123" 这种弱拼接。
const (
	defaultMinPasswordLength = 8
	defaultBcryptCost        = 12
	bootstrapPasswordBytes   = 18 // 18 字节 base64url ≈ 24 字符
	sessionTokenBytes        = 32 // 32 字节 base64url ≈ 43 字符
	initialPasswordFileName  = "initial_password.txt"
	initialPasswordFileMode  = 0o600 // 仅文件所有者可读写
	initialPasswordDirMode   = 0o755
	defaultAdminUsername     = "admin"
)

// dummyBcryptHash 是固定的 bcrypt 哈希字符串，用于 Login 路径"用户不存在"
// 分支的时间侧信道防御：让"用户不存在"与"密码错"耗时一致。
//
// 用 sync.Once 懒加载：避免 init 时为所有 import 本包的 binary 都付出
// 一次 bcrypt 计算（~150ms 启动延迟），仅在第一次 Login 调用时生成。
// 内容是固定明文 "invalid-account-placeholder" 的 cost=12 哈希——明文
// 不需要保密，这只是个不可能匹配真实用户提交的密码的占位串。
var (
	dummyBcryptHash     []byte
	dummyBcryptHashOnce stdsync.Once
)

// runDummyBcrypt 执行一次 bcrypt 校验（against dummy hash）以平衡时间侧信道。
//
// 第一次调用时 sync.Once 生成 dummy hash（~150ms）；后续调用复用。结果
// 一律丢弃——dummy hash 不可能匹配上任意攻击者输入。
func runDummyBcrypt(password string) {
	dummyBcryptHashOnce.Do(func() {
		// 失败极不可能（bcrypt cost 在 [4, 31] 内合法，且明文短）；如果失败
		// 留 dummyBcryptHash 为 nil，后续 CompareHashAndPassword 会立刻返回，
		// 无法平衡时间但也不会 panic——可观察到的危害仅是"侧信道防御退化为
		// 与 v0 等价的有/无差异"，这与历史版本一致。
		h, err := bcrypt.GenerateFromPassword([]byte("invalid-account-placeholder"), defaultBcryptCost)
		if err == nil {
			dummyBcryptHash = h
		}
	})
	if dummyBcryptHash != nil {
		_ = bcrypt.CompareHashAndPassword(dummyBcryptHash, []byte(password))
	}
}

// BootstrapResult 是 EnsureBootstrapAdmin 的结构化返回值。
//
// 设计动机：Codex 审查指出"err != nil 时 plain 仍有意义"违反 Go 惯例
// （调用方常忽略 err 时的其它返回值）。把"创建结果"与"文件写入子错误"
// 分离到独立字段，让 err == nil 路径表达"账户已创建"清晰可读。
type BootstrapResult struct {
	// Created 为 true 表示本次确实创建了新 admin 账户；为 false 表示
	// admin_users 表非空，无需引导。
	Created bool

	// Plain 是新 admin 的明文密码；仅在 Created=true 时有效，调用方
	// 应当立即通过日志或屏幕输出展示给运维。
	Plain string

	// PasswordFile 是写入了明文密码的文件路径（dataDir 非空时填）；
	// 空字符串表示运维未指定 dataDir，仅日志通道可用。
	//
	// 路径形态：构造时尝试 filepath.Abs 解析为绝对路径，失败则回退到原
	// 相对路径——后者发生概率极低（仅在系统级 Getwd 失败时）。运维拿到
	// 该字段一律按"打开它能读到密码"的契约即可，不必关心是否绝对。
	PasswordFile string

	// FileWriteErr 文件写入子错误（非致命）。Plain 已经在返回值里提供，
	// 即使文件写入失败仍能通过日志通道找回密码；但运维可能希望感知
	// 文件落盘失败以便手工干预，所以单独保留该字段。
	FileWriteErr error
}

// Service 是鉴权服务对外门面。
//
// 字段说明：
//
//	store           底层持久层；所有 SQL 都经过它，不直接接 *sql.DB。
//	sessionMaxAge   滑动窗口：每次校验通过把过期时间推到 now + 这个值。
//	absoluteMaxAge  绝对寿命：从 created_at 起的总寿命上限，永远不能被
//	                续期突破。两者关系详见 internal/store/schema.go 的
//	                sessions 表注释。
//	minPasswordLen  最低密码长度。
//	bcryptCost      bcrypt 强度参数；详见包注释中的 "cost 选择"。
//	nowFn           获取当前时间的注入点；测试可替换以模拟时间流逝。
type Service struct {
	store store.Store

	sessionMaxAge  time.Duration
	absoluteMaxAge time.Duration

	minPasswordLen int
	bcryptCost     int
	nowFn          func() time.Time
}

// NewService 构造 Service。
//
// 参数：
//
//	st              非 nil 的 store.Store；nil 会立刻 panic 防御编程错误。
//	sessionMaxAge   滑动窗口；<=0 时业务侧应当传 config.Web 已校验后的值。
//	                本函数不再二次校验，避免与 config.Validate 重复职责。
//	absoluteMaxAge  绝对寿命；同上，由调用方保证。
//
// 默认字段（minPasswordLen=8 / bcryptCost=12 / nowFn=time.Now）由本函数
// 写入；测试可在构造后用 SetXxx 替换（M5 引入对应 setter）。
func NewService(st store.Store, sessionMaxAge, absoluteMaxAge time.Duration) *Service {
	if st == nil {
		panic("auth.NewService: store 不可为 nil")
	}
	return &Service{
		store:          st,
		sessionMaxAge:  sessionMaxAge,
		absoluteMaxAge: absoluteMaxAge,
		minPasswordLen: defaultMinPasswordLength,
		bcryptCost:     defaultBcryptCost,
		nowFn:          time.Now,
	}
}

// HashPassword 用 bcrypt 哈希明文密码。
//
// 失败原因：bcrypt cost 超出 [4, 31] 范围（本包内固定 12，不会触发）；
// 或 plain 长度 > 72 字节（bcrypt 上限）。
func (s *Service) HashPassword(plain string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(plain), s.bcryptCost)
	if err != nil {
		return "", fmt.Errorf("bcrypt 哈希：%w", err)
	}
	return string(h), nil
}

// VerifyPassword 校验明文与哈希是否匹配。
//
// 用 bool 而非 error 返回：调用方典型用法 `if !VerifyPassword(...) { ... }`，
// 比 `if errors.Is(...) ...` 更直观。bcrypt.CompareHashAndPassword 内部对
// "不匹配"返回固定 *bcrypt.ErrMismatchedHashAndPassword，不在乎其它 error
// 类型——所以只要不是 nil 一律视为不匹配（保守判定）。
func VerifyPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// GenerateToken 生成 32 字节 crypto/rand 随机数的 base64url 编码。
//
// 输出长度恒定 43 字符，不带 "="。base64url 而非标准 base64：URL safe
// 字符集 [A-Za-z0-9_-]，可以无转义地放进 cookie / URL / JSON 字符串里。
//
// 失败仅可能源于 OS rand 池失败，本函数把它包装成中文错误消息。
func GenerateToken() (string, error) {
	buf := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成 session token 时随机数源失败：%w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// EnsureBootstrapAdmin 在 admin_users 表为空时创建默认 admin 账户。
//
// 返回值约定：
//
//   - err 非 nil → 创建失败，BootstrapResult 应被忽略；
//   - err 为 nil + Created=false → 表非空，无需引导；
//   - err 为 nil + Created=true → 账户已创建；Plain 必有值，FileWriteErr
//     可能非空（文件写入失败但账户已建好；调用方仍能从 Plain 字段获取
//     明文密码并通过日志通道输出，FileWriteErr 仅作 WARN 兜底）。
//
// 这种结构化返回值替代了"err 非 nil 时 plain 仍有效"的 footgun。
//
// 安全考虑：
//
//   - 密码 18 字节 base64url ≈ 24 字符（~108 bits 熵），远超暴力破解阈值；
//   - 文件 mode 0o600：仅所属用户可读，避免同一宿主上其他用户看到；
//   - 父目录 0o755：标准目录权限，与 store 创建数据库父目录一致；
//   - dataDir 为空字符串时仅日志通道，文件不创建——某些场景（如测试 /
//     stdin-only 部署）不需要文件落盘。
//
// 调用方约定：拿到 Plain 后必须立刻 log.Info 输出（双通道）。本函数不
// 直接调 logger 是为了让"何时打日志"由调用方决定（避免在测试中污染输出）。
func (s *Service) EnsureBootstrapAdmin(ctx context.Context, dataDir string) (BootstrapResult, error) {
	n, err := s.store.CountAdminUsers(ctx)
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("查询管理员数量：%w", err)
	}
	if n > 0 {
		return BootstrapResult{Created: false}, nil
	}

	plain, err := generateRandomPassword()
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("生成初始密码：%w", err)
	}
	hash, err := s.HashPassword(plain)
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("哈希初始密码：%w", err)
	}
	if _, err := s.store.CreateAdminUser(ctx, defaultAdminUsername, hash); err != nil {
		return BootstrapResult{}, fmt.Errorf("创建初始 admin：%w", err)
	}

	res := BootstrapResult{
		Created: true,
		Plain:   plain,
	}
	if dataDir != "" {
		path := filepath.Join(dataDir, initialPasswordFileName)
		if werr := writePasswordFile(dataDir, plain); werr != nil {
			// 非致命：账户已建好，明文也在 Plain 里，调用方走日志通道仍能看到。
			res.FileWriteErr = fmt.Errorf("写入 %s：%w", path, werr)
		} else {
			// 尝试解析为绝对路径，让运维拿到"在哪里都能直接打开"的串。
			// Abs 在系统级 Getwd 失败时才会出错（极罕见），失败回退到原
			// 相对路径——契约是"路径有效"，绝对/相对仅是表面差异。
			if abs, aerr := filepath.Abs(path); aerr == nil {
				res.PasswordFile = abs
			} else {
				res.PasswordFile = path
			}
		}
	}
	return res, nil
}

// Login 校验用户名密码并创建一个新会话。
//
// 成功返回 session token 字符串（调用方写入 cookie）；失败返回
// ErrInvalidCredentials（防止用户枚举）或具体的内部错误。
//
// 时间侧信道防御：用户不存在分支也运行一次 bcrypt 校验（against dummy
// hash），让两条路径的总耗时近似一致。否则攻击者用纳秒级响应差就能
// 区分"用户存在但密码错" vs "用户不存在"，绕过 ErrInvalidCredentials
// 的统一错误消息防御。
//
// ip / userAgent 仅作为审计字段写入 sessions 表，不参与校验——本中间件
// 的会话不绑定到 IP / UA，因为运维场景常有 NAT / 移动办公等导致 IP 变化。
// 如果未来要做 IP 锁定，可在 Web 中间件层独立判断。
func (s *Service) Login(ctx context.Context, username, password, ip, userAgent string) (string, error) {
	u, err := s.store.GetAdminUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// 跑一次 bcrypt 让响应时间与"用户存在但密码错"路径相近。
			// 结果一律丢弃；不可能匹配上 dummy hash，攻击者无法据此推断什么。
			runDummyBcrypt(password)
			return "", ErrInvalidCredentials
		}
		return "", fmt.Errorf("查询用户 %q：%w", username, err)
	}
	if !VerifyPassword(u.PasswordHash, password) {
		return "", ErrInvalidCredentials
	}

	token, err := GenerateToken()
	if err != nil {
		return "", err
	}
	now := s.nowFn().UTC()
	if err := s.store.CreateSession(ctx, store.SessionRow{
		Token:     token,
		UserID:    u.ID,
		CreatedAt: now,
		ExpiresAt: now.Add(s.sessionMaxAge),
		LastSeen:  now,
		IP:        ip,
		UserAgent: userAgent,
	}); err != nil {
		return "", fmt.Errorf("创建会话：%w", err)
	}

	// 更新 last_login_at；失败不影响登录结果，仅作 WARN 兜底。
	// 这里用 best-effort 风格：返回 token 让调用方知道登录成功，但同时
	// 包装错误让 caller 能选择记录 WARN（避免静默吞掉数据库写失败）。
	if terr := s.store.TouchAdminLogin(ctx, u.ID, now); terr != nil {
		return token, fmt.Errorf("更新 last_login_at（非致命）：%w", terr)
	}
	return token, nil
}

// Logout 删除指定 session token。
//
// 幂等：token 不存在时不报错（store.DeleteSession 自身幂等）。
func (s *Service) Logout(ctx context.Context, token string) error {
	return s.store.DeleteSession(ctx, token)
}

// VerifySession 校验 token 是否有效，并执行滑动续期；返回当前用户。
//
// 校验流程：
//
//	a) token 为空 → ErrSessionExpired
//	b) GetSession 返回 ErrNotFound → ErrSessionExpired
//	c) 已过期（ExpiresAt <= now） → 删除 session + ErrSessionExpired
//	d) 计算 newExpiresAt = min(now + sessionMaxAge, createdAt + absoluteMaxAge)
//	e) newExpiresAt <= now（撞到绝对上限） → 删除 session + ErrSessionExpired
//	f) TouchSession(now, newExpiresAt) → GetAdminUserByID → 返回用户
//
// 任意 a..e 路径都不返回非业务错误，避免泄漏内部状态给攻击者。
func (s *Service) VerifySession(ctx context.Context, token string) (*store.AdminUserRow, error) {
	if token == "" {
		return nil, ErrSessionExpired
	}
	ses, err := s.store.GetSession(ctx, token)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrSessionExpired
		}
		return nil, fmt.Errorf("查询会话：%w", err)
	}

	now := s.nowFn().UTC()
	if !ses.ExpiresAt.After(now) {
		_ = s.store.DeleteSession(ctx, token)
		return nil, ErrSessionExpired
	}

	// 滑动续期 + 绝对寿命 cap。
	newExpiresAt := now.Add(s.sessionMaxAge)
	absoluteCap := ses.CreatedAt.Add(s.absoluteMaxAge)
	if newExpiresAt.After(absoluteCap) {
		newExpiresAt = absoluteCap
	}
	if !newExpiresAt.After(now) {
		// 已撞到绝对上限：强制下线。
		_ = s.store.DeleteSession(ctx, token)
		return nil, ErrSessionExpired
	}

	if err := s.store.TouchSession(ctx, token, now, newExpiresAt); err != nil {
		// 极端情况：另一个 goroutine 在 GetSession 与 TouchSession 之间
		// Logout / ChangePassword 把这个 session 删掉。映射成 ErrSessionExpired
		// 让 Web 中间件按"未登录"处理，而不是返回内部错误。
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrSessionExpired
		}
		return nil, fmt.Errorf("续期会话：%w", err)
	}

	user, err := s.store.GetAdminUserByID(ctx, ses.UserID)
	if err != nil {
		// 用户被删但会话还在——理论不应发生（DeleteAdminUser 调 DeleteSessionsForUser
		// 是约定俗成）。仍做防御。
		if errors.Is(err, store.ErrNotFound) {
			_ = s.store.DeleteSession(ctx, token)
			return nil, ErrSessionExpired
		}
		return nil, fmt.Errorf("查询用户 id=%d：%w", ses.UserID, err)
	}
	return &user, nil
}

// ChangePassword 修改密码并强制下线该用户的所有会话。
//
// 校验：
//
//	a) 新密码长度 < minPasswordLen → ErrPasswordTooShort
//	b) 新旧密码相同 → ErrPasswordReuse（避免"改了等于没改"）
//	c) 旧密码不正确 → ErrInvalidCredentials
//
// 副作用：成功时立即 DeleteSessionsForUser——攻击者若已挟持过 token
// 立即失效。代价是当前操作者的会话也被删，调用方（HTTP handler）若想
// 保留当前请求会话需要在 ChangePassword 后立即重新 Login。
//
// 不对 newPass 做"必须含字母数字符号"等复杂正则——NIST SP 800-63B
// 已不再推荐这种规则，长度才是关键。运维若误设极弱密码（如 "12345678"），
// 后续 bcrypt 校验仍能正确防护，且本函数会因 ErrPasswordReuse 拦住
// "改密改成原值" 的退化场景。
func (s *Service) ChangePassword(ctx context.Context, userID int64, oldPass, newPass string) error {
	if len(newPass) < s.minPasswordLen {
		return ErrPasswordTooShort
	}
	if oldPass == newPass {
		return ErrPasswordReuse
	}

	u, err := s.store.GetAdminUserByID(ctx, userID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrInvalidCredentials
		}
		return fmt.Errorf("查询用户 id=%d：%w", userID, err)
	}
	if !VerifyPassword(u.PasswordHash, oldPass) {
		return ErrInvalidCredentials
	}

	newHash, err := s.HashPassword(newPass)
	if err != nil {
		return fmt.Errorf("哈希新密码：%w", err)
	}
	// 走单事务：把"更新密码"与"删该用户全部会话"绑成原子操作，避免
	// "密码已改但旧 session 仍能续期"这一安全窗口。
	if _, err := s.store.UpdateAdminPasswordAndPurgeSessions(ctx, userID, newHash); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// 用户在校验后被并发删除——映射成 ErrInvalidCredentials，
			// 与"找不到用户"路径保持一致的对外错误形态。
			return ErrInvalidCredentials
		}
		return fmt.Errorf("改密事务：%w", err)
	}
	return nil
}

// ResetPassword 直接重设指定用户密码（不校验旧密码）。
//
// 用途：CLI 子命令 `xboard-xui-bridge reset-password`（M7 阶段引入）—
// 当运维忘记 admin 密码时通过本地 shell 重置。HTTP 层不应该暴露该路径，
// 因为没有"权限"概念可以信任（任何能调 HTTP 的人都不应能任意重置密码）。
//
// 副作用：与 ChangePassword 一致，强制下线该用户所有会话。
func (s *Service) ResetPassword(ctx context.Context, userID int64, newPass string) error {
	if len(newPass) < s.minPasswordLen {
		return ErrPasswordTooShort
	}
	newHash, err := s.HashPassword(newPass)
	if err != nil {
		return fmt.Errorf("哈希新密码：%w", err)
	}
	// 同 ChangePassword：单事务把"改密 + 强制下线"绑成原子操作。
	if _, err := s.store.UpdateAdminPasswordAndPurgeSessions(ctx, userID, newHash); err != nil {
		return fmt.Errorf("重置密码事务：%w", err)
	}
	return nil
}

// PurgeExpiredSessions 由后台定时任务调用清理过期会话。
//
// 不是事务关键路径，失败仅打 WARN；调用方约定每 1 小时跑一次即可，
// 高频清理对持久层没有意义（VerifySession 路径已经会按需删除单条）。
func (s *Service) PurgeExpiredSessions(ctx context.Context) (int64, error) {
	return s.store.PurgeExpiredSessions(ctx, s.nowFn().UTC())
}

// LogBootstrap 是 EnsureBootstrapAdmin 配套的日志辅助：把首次密码同时打到
// 日志的 INFO 级 + WARN 级（INFO 给"成功"事实，WARN 给"请立即妥善保管"
// 的提醒），让运维不会在大量 INFO 中错过这一关键信息。
//
// res.Created 为 false 时直接 no-op；调用方无需在外层重复判断。
// res.FileWriteErr 非 nil 时打 WARN 提示运维文件落盘失败但账户已建好。
func LogBootstrap(log *slog.Logger, res BootstrapResult) {
	if !res.Created {
		return
	}
	attrs := []any{
		"username", defaultAdminUsername,
		"password", res.Plain,
	}
	if res.PasswordFile != "" {
		attrs = append(attrs, "file", res.PasswordFile)
	}
	log.Info("已生成初始 admin 账户", attrs...)
	if res.FileWriteErr != nil {
		log.Warn("初始密码文件写入失败（不影响账户创建，密码已通过日志输出）",
			"err", res.FileWriteErr,
		)
	}
	log.Warn("⚠️ 请立即用此密码登录 Web 面板并修改；建议导出后删除 initial_password.txt 文件")
}

// generateRandomPassword 生成 18 字节 crypto/rand 随机数的 base64url 编码。
// 输出 ~24 字符纯 ASCII（[A-Za-z0-9_-]），可直接作为密码使用。
func generateRandomPassword() (string, error) {
	buf := make([]byte, bootstrapPasswordBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("crypto/rand 读取失败：%w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// writePasswordFile 把明文密码写入 dataDir/initial_password.txt。
//
// 写入策略：
//
//   - dataDir 不存在时按 0o755 递归创建（与 store 创建数据库目录一致）；
//   - 文件 0o600 权限，避免同宿主其它用户读到；
//   - 内容 = 明文密码 + 换行符；末尾换行让 cat 输出更友好（大多数终端
//     在最后没换行时会显示 "%" 或拼接到下一行 prompt，影响阅读）。
//
// 已存在的 initial_password.txt 会被覆盖——这是有意为之：本函数只在
// EnsureBootstrapAdmin 检测 admin_users 为空时调用，存在旧文件说明
// 上一次启动留下的密码已经过时，覆盖不会丢失有效信息。
func writePasswordFile(dataDir, plain string) error {
	if err := os.MkdirAll(dataDir, initialPasswordDirMode); err != nil {
		return fmt.Errorf("创建 dataDir %q：%w", dataDir, err)
	}
	path := filepath.Join(dataDir, initialPasswordFileName)
	if err := os.WriteFile(path, []byte(plain+"\n"), initialPasswordFileMode); err != nil {
		return fmt.Errorf("写入 %q：%w", path, err)
	}
	return nil
}

