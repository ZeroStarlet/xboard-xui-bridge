package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/xboard-bridge/xboard-xui-bridge/internal/auth"
	"github.com/xboard-bridge/xboard-xui-bridge/internal/store"
)

// ctxKey 是 context.WithValue 的 key 类型。
//
// 用 unexported 类型避免外部包"碰巧用相同字面量"覆盖我们的值——这是
// context 包文档明确推荐的模式。
type ctxKey int

const (
	// ctxKeyUser 把当前已认证的 *store.AdminUserRow 挂在 ctx 上，让 handler
	// 不必每次都重新解析 cookie / 查 session。
	ctxKeyUser ctxKey = iota
	// ctxKeySessionToken 把当前 session token 挂在 ctx 上，便于 logout
	// handler 拿到要删的 token。
	ctxKeySessionToken
)

// CurrentUser 从 ctx 取出已认证用户；未登录时返回 nil（理论上 authMiddleware
// 不应让此情形通过，但防御编程仍要兜底）。
func CurrentUser(ctx context.Context) *store.AdminUserRow {
	v, _ := ctx.Value(ctxKeyUser).(*store.AdminUserRow)
	return v
}

// CurrentSessionToken 从 ctx 取出 session token；同上。
func CurrentSessionToken(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeySessionToken).(string)
	return v
}

// securityHeadersMiddleware 给所有响应注入一组保守的浏览器侧安全响应头。
//
// 头部含义：
//
//	X-Content-Type-Options: nosniff
//	    禁止浏览器对 Content-Type 做"猜测"——即使 handler 误把 JSON 写成
//	    text/plain，浏览器也不会按 HTML 执行其中可能的脚本片段。配合 Vue
//	    构建的 SPA + JSON API，本头部对正常业务零影响。
//
//	X-Frame-Options: DENY
//	    禁止任何域把本面板嵌进 iframe，杜绝 clickjacking 攻击向量。运维
//	    若需要把面板嵌入其它系统，应通过反向代理改写本头部——不在中间件
//	    内部留 allow-list，避免误配。
//
//	Referrer-Policy: same-origin
//	    跳转到外部链接时只发送同源 referer；点击 GitHub 链接等外部跳转
//	    时不会泄漏 /api/auth/login 等内部路径。
//
// 设计选择：
//
//	a) 不写 Content-Security-Policy：本面板的前端含 Vite 注入的 inline
//	   style，CSP 严格策略需要 hash / nonce 配合，会让首版 SPA 调试复杂。
//	   留给未来版本（roadmap 已记录）。
//	b) 不写 Strict-Transport-Security：HTTPS 终结大概率在反代而非中间件本身，
//	   HSTS 应当由反代统一签发，避免双方各发一份引发浏览器困惑。
//	c) 中间件不读 ResponseWriter 的写入状态——这些头部在 next(w, r) 之前
//	   注入，handler 仍然可以覆盖（例如 spaHandler 自己设 Content-Type）。
//	   若 handler 已 Write，net/http 会忽略后续 Set，不影响正确性。
func (s *Server) securityHeadersMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		next(w, r)
	}
}

// recoverMiddleware 捕获下游 handler 的 panic，返回 500 而不是让进程崩溃。
//
// 设计动机：v0.1 没有 Web 层时进程 panic 等于"流量同步停摆"——靠 systemd
// 重启即可。v0.2 加 Web 后单次请求的 panic 不应让整个面板与同步引擎一起
// 重启。recover 后把 panic stack 写到 ERROR 日志，便于后续排查。
func (s *Server) recoverMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("HTTP handler panic",
					"path", r.URL.Path,
					"method", r.Method,
					"panic", rec,
					"stack", string(debug.Stack()),
				)
				// 这时候 ResponseWriter 状态未知；尽力写一个 500，失败也只能算了。
				s.writeError(w, http.StatusInternalServerError, errCodeInternal, "服务器内部错误")
			}
		}()
		next(w, r)
	}
}

// loggingMiddleware 把每次请求的方法 / 路径 / 状态 / 耗时打到 INFO 级日志。
//
// 通过 statusRecorder 截获 WriteHeader 调用，记录最终状态码——直接读
// http.ResponseWriter 拿不到 status。
func (s *Server) loggingMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next(rec, r)
		s.log.Info("HTTP",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"elapsed", time.Since(start),
			"remote", clientIP(r),
		)
	}
}

// statusRecorder 包装 ResponseWriter 截获 status code。
//
// 必要性：标准 ResponseWriter 接口没有 Status() 方法，只能在 WriteHeader
// 被调时记录。如果 handler 从未显式调 WriteHeader（典型于 200 OK 路径上
// 直接 Write），net/http 会在第一次 Write 时 implicit 调 WriteHeader(200)，
// 我们也照样能截获——statusRecorder.WriteHeader 只在第一次有效，第二次
// 的覆盖会被 http 包自动忽略并 ERROR 警告。
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

// WriteHeader 拦截状态码写入。
func (sr *statusRecorder) WriteHeader(code int) {
	if !sr.wroteHeader {
		sr.status = code
		sr.wroteHeader = true
	}
	sr.ResponseWriter.WriteHeader(code)
}

// Write 截获隐式 WriteHeader：handler 直接 Write 不调 WriteHeader 时，
// 标准库会按 200 触发，这里同步记录。
func (sr *statusRecorder) Write(p []byte) (int, error) {
	if !sr.wroteHeader {
		sr.status = http.StatusOK
		sr.wroteHeader = true
	}
	return sr.ResponseWriter.Write(p)
}

// authMiddleware 校验 session cookie 并把当前用户写入 ctx。
//
// 流程：
//
//	1. 从 cookie 读 bridge_session token；缺失 → 401
//	2. authSvc.VerifySession(token)
//	3. 成功 → ctx WithValue(currentUser, sessionToken) → next
//	4. 失败 → 清 cookie + 401
//
// 把"清 cookie"动作放在 401 路径里：让浏览器主动丢失效凭证，避免下次
// 还带过期 token 来撞同样的 401。
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := readSessionCookie(r)
		if token == "" {
			s.writeError(w, http.StatusUnauthorized, errCodeUnauthorized, "未登录")
			return
		}
		user, err := s.authSvc.VerifySession(r.Context(), token)
		if err != nil {
			if errors.Is(err, auth.ErrSessionExpired) {
				clearSessionCookie(w, r)
				s.writeError(w, http.StatusUnauthorized, errCodeUnauthorized, "会话已过期，请重新登录")
				return
			}
			// 内部错误：日志 ERROR，对外 500（不暴露细节）。
			s.log.Error("校验会话失败", "err", err)
			s.writeError(w, http.StatusInternalServerError, errCodeInternal, "服务器内部错误")
			return
		}
		ctx := context.WithValue(r.Context(), ctxKeyUser, user)
		ctx = context.WithValue(ctx, ctxKeySessionToken, token)
		next(w, r.WithContext(ctx))
	}
}

// csrfMiddleware 对所有写操作做 Origin / Referer 同源校验。
//
// 策略：
//
//	GET / HEAD → 不校验（浏览器跨站发起 GET 不算 CSRF——除非 handler
//	    写副作用，本项目所有写操作走 POST/PUT/PATCH/DELETE）。
//	其它方法 → Origin 优先；缺失退到 Referer；都缺失也拒绝。
//
// 不再额外签发 CSRF token：因为我们已经对 cookie 用 SameSite=Lax，
// 跨站 POST 不会自动带 cookie；Origin/Referer 检查作为第二层防御
// 足够覆盖 cookie 被攻击者诱发自动发送的边角场景。
//
// 同源判定：scheme + host 严格相等（host 会带端口）。如果 listenAddr
// 是 ":8787"（绑定全部网卡），上层应当通过反代落地一个固定外部域名
// 给浏览器，本中间件按浏览器实际访问的 Host 做判定。
func (s *Server) csrfMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next(w, r)
			return
		}
		if !isSameOrigin(r) {
			s.log.Warn("CSRF 校验失败：拒绝跨站写操作",
				"method", r.Method,
				"path", r.URL.Path,
				"origin", r.Header.Get("Origin"),
				"referer", r.Header.Get("Referer"),
				"host", r.Host,
			)
			s.writeError(w, http.StatusForbidden, errCodeForbidden, "跨站写操作已被拒绝")
			return
		}
		next(w, r)
	}
}

// isSameOrigin 判断请求的 Origin / Referer 与 r.Host 是否同源。
//
// Go 标准库不提供该函数，自己写一个：
//   - 优先看 Origin；浏览器在 fetch / XHR 写操作时一律带；
//   - Origin 为 "null"（file:// 等）也视为不同源——拒之；
//   - Origin 缺失 → 退到 Referer（少数 client 不发 Origin）；
//   - 都没有 → 视为不同源（防御 GET 之外的 client）。
//
// host 比较包含端口：浏览器把不同端口视为不同 origin，本函数遵循。
func isSameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin != "" {
		// "null" 是 file:// 等场景，标准浏览器在 redirect 等情况下也会发；
		// 一律拒绝。
		if origin == "null" {
			return false
		}
		return originHost(origin) == r.Host
	}
	referer := r.Header.Get("Referer")
	if referer != "" {
		return originHost(referer) == r.Host
	}
	return false
}

// originHost 从 Origin / Referer 字符串提取 host (含端口)。
// 解析失败返回空串，调用方按"不同源"处理。
func originHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}

// clientIP 返回客户端 IP 字符串。
//
// 来源优先级：
//   1. X-Real-IP / X-Forwarded-For（仅当部署在反代后；裸跑要忽略以防伪造）
//   2. r.RemoteAddr 的 host 部分
//
// 当前实现仅用 RemoteAddr：本中间件不知道用户是否部署反代；要安全地信任
// X-Forwarded-For 必须配置"信任的反代列表"——M5 阶段先不引入，留给运维
// 通过 nginx 自己写日志拿真实 IP。
func clientIP(r *http.Request) string {
	host, _, err := splitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// splitHostPort 是 net.SplitHostPort 的薄包装。
//
// 包装动机：可能 RemoteAddr 是空字符串或纯 host（极罕见），net.SplitHostPort
// 会报错。本函数把错误返回出去，让 clientIP 回退到原值。
func splitHostPort(addr string) (host, port string, err error) {
	if addr == "" {
		return "", "", errors.New("addr 为空")
	}
	// 复用标准库实现：net.SplitHostPort 处理 ipv6 [::1]:8787 等格式比手写
	// 字符串 split 稳。
	idx := strings.LastIndex(addr, ":")
	if idx < 0 {
		return addr, "", nil
	}
	host = addr[:idx]
	port = addr[idx+1:]
	// 处理 ipv6 形式 [::1]
	host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	return host, port, nil
}

// readSessionCookie 从请求里取 bridge_session cookie 的值。
// 不存在或空字符串时返回 ""。
func readSessionCookie(r *http.Request) string {
	c, err := r.Cookie(SessionCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

// setSessionCookie 写入 session cookie。
//
// Cookie 属性：
//   - HttpOnly：JS 不可读，防 XSS 偷 token；
//   - SameSite=Lax：浏览器跨站 GET 仍会带（如登录后跳转），但跨站 POST
//     不会带——配合 csrfMiddleware 双保险；
//   - Secure：详见 isHTTPSRequest；同时识别 r.TLS 与 X-Forwarded-Proto
//     头，让反代场景下也能正确标 Secure；
//   - Path=/：覆盖整站；
//   - MaxAge：与 sessionMaxAge 对齐，浏览器主动丢失效 cookie。
func setSessionCookie(w http.ResponseWriter, r *http.Request, token string, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isHTTPSRequest(r),
		MaxAge:   int(maxAge.Seconds()),
	})
}

// clearSessionCookie 立刻把 cookie 置空 + MaxAge<0 让浏览器删除。
func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isHTTPSRequest(r),
		MaxAge:   -1,
	})
}

// isHTTPSRequest 判断请求是否走在 HTTPS（用于决定 Cookie 的 Secure 属性）。
//
// 判定来源：
//
//	a) r.TLS != nil  → 进程直接监听 TLS（Go 的 ServeTLS）；
//	b) X-Forwarded-Proto == "https"  → 反代终结 TLS 后转发 plain HTTP。
//
// 反代场景的安全前提：运维必须确保只有受信任的反代能注入该头部。
// 这与 net/http 设计哲学一致——头部信任由部署架构决定，库不替运维做
// "默认信任 / 默认不信任"的强假设。
//
// 不识别 X-Forwarded-Ssl / Forwarded 等其它形态：扩展支持需要正交化的
// "信任头部"白名单配置；M5 阶段先收敛到一个最常见的头部，运维若用了
// 其它代理（如 Apache 默认转发 X-Forwarded-Ssl）应当配置反代统一发
// X-Forwarded-Proto。
func isHTTPSRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return true
	}
	return false
}

// requireMethod 在 handler 内部做"当前路径仅接受这一种方法"的兜底校验。
//
// Go 1.22 ServeMux 的方法路由（"POST /api/auth/login"）已经处理了 405，
// 但万一有人改了路由忘了同步方法，本函数能让 handler 自身仍可在错误的
// 方法上回 405 + 一致的 JSON 外壳。
func (s *Server) requireMethod(w http.ResponseWriter, r *http.Request, allowed ...string) bool {
	for _, m := range allowed {
		if r.Method == m {
			return true
		}
	}
	w.Header().Set("Allow", strings.Join(allowed, ", "))
	s.writeError(w, http.StatusMethodNotAllowed, errCodeMethodNotAllowed, fmt.Sprintf("仅支持 %s", strings.Join(allowed, ", ")))
	return false
}

// 登录失败次数限流（v0.5.3 引入）。
//
// 防御目标：在 bcrypt cost=12（单次 ~150ms）之外再加一道"按 IP 计数"门槛，
// 让一对凭据 + 一个 IP 的暴力破解最多 5 次/5 分钟内拿到尝试机会。bcrypt
// 已让单 IP 单线程每秒 ~7 次尝试，但若攻击者用并发 + 多源 IP 仍可压力
// 测试；限流让"单 IP 内卷"变得不经济，与 fail2ban 等 OS 层防御互补
// （OS 层覆盖跨进程持久状态，本中间件覆盖进程内即时计数）。
//
// 算法：固定窗口计数器 + 预占名额（reserve-then-rollback）。每个 IP 维护一个
// (count, windowStart)；请求落在 windowStart + window 内 → 在 reserve 阶段
// count++；超出窗口 → 重置 count=1，windowStart=now。count >= 上限时拒新
// reserve（与代码 e.count >= loginRateLimitMaxAttempts 严格对齐）。
//
// 关键：reserve 必须与 count 检查在同一临界区——否则并发 burst 会让多个
// 请求都看到 count=0、全部通过、随后再各自递增。
//
// 失败 / 成功语义：
//
//	入口 reserveAttempt(ip) → 预占一次名额；超限即返 429；
//	登录成功 → recordSuccess(ip) 直接删除该 IP entry（让正常用户偶发输错
//	几次后正常登录回到干净计数）；
//	凭据错失败 → 不需要额外动作，预占已经计入；
//	非凭据类错误（body 解析 / 服务端 5xx） → rollbackAttempt(ip) 撤销预占，
//	避免真正用户因为服务端故障被白消耗名额。
//
// 状态泄漏控制：软上限 + 惰性清扫。entries 长度超 softMax 时清掉所有
// 已过期 entry；即使大规模攻击让 map 在某个时刻接近 softMax，单次清扫
// 是 O(N) 一次性付出，摊销到攻击周期内 < 单次 bcrypt 成本。clean 后若
// 仍超 softMax，说明攻击规模超出中间件设计上限——按正常逻辑继续工作，
// OS 层（iptables / fail2ban / cloudflare）应当并行兜底。
const (
	loginRateLimitWindow      = 5 * time.Minute
	loginRateLimitMaxAttempts = 5
	// loginRateLimitSoftMax 是 entries map 的软上限。超过时本次调用先清扫
	// 已过期 entry。10000 是基于"正常运维场景下不可能积累这么多 IP"选的
	// 经验值——若典型部署需要更高/更低，应通过 cfg 暴露而不是在此调参。
	loginRateLimitSoftMax = 10000
)

// loginAttemptEntry 是单个 IP 的失败计数槽位。
type loginAttemptEntry struct {
	count       int
	windowStart time.Time
}

// loginRateLimiter 是 IP → 失败计数的并发安全 map。
//
// 锁粒度：单一 sync.Mutex 包整 map。临界区只做 hash 查询 + 整型递增 + 时
// 间比较，O(1) 极快；多 IP 并发登录失败的扩展性无压力。若未来攻击者能让
// 锁竞争成为瓶颈，可拆分为 sharded mutex；当前 5 次/5 分钟的设计阈值下
// 完全过设计。
type loginRateLimiter struct {
	mu      sync.Mutex
	entries map[string]*loginAttemptEntry
}

// newLoginRateLimiter 构造空 limiter。Server.New 调用一次。
func newLoginRateLimiter() *loginRateLimiter {
	return &loginRateLimiter{entries: make(map[string]*loginAttemptEntry)}
}

// reserveAttempt 在限流允许时预占一次名额，返回 true；已超限返回 false。
//
// 调用约定：
//
//	handleLogin 入口：if !s.loginLimiter.reserveAttempt(ip) → 429
//	登录成功 → recordSuccess(ip)（清空 entry，预占自然作废）
//	登录凭据错失败 → 不需要额外动作，预占已计入
//	登录非凭据类错（body 解析 / 5xx） → rollbackAttempt(ip)（撤销预占）
//
// 关键：count 检查与 count++ 必须在同一临界区，避免并发 burst 让多个请求
// 都看到旧 count、全部通过 reserve、再各自递增——v0.5.3 修复 Codex 审查
// 发现的"allow → bcrypt → record" 旧模型并发漏洞。
func (l *loginRateLimiter) reserveAttempt(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 软上限触发清扫：超过阈值时一次性清掉所有已过期 entry。
	// 攻击规模未到 softMax 时本路径不进，长期开销可忽略。
	if len(l.entries) > loginRateLimitSoftMax {
		l.pruneExpiredLocked()
	}

	now := time.Now()
	e, ok := l.entries[ip]
	if !ok || now.Sub(e.windowStart) >= loginRateLimitWindow {
		// 新 IP 或窗口已过：建立新 entry，count=1（本次预占已计入）。
		l.entries[ip] = &loginAttemptEntry{count: 1, windowStart: now}
		return true
	}
	if e.count >= loginRateLimitMaxAttempts {
		return false
	}
	e.count++
	return true
}

// rollbackAttempt 撤销 reserveAttempt 预占的一次名额。仅在"非凭据类错误"
// （body 解析失败 / 服务端 5xx）时调用，让真正的用户不因服务端故障被白
// 消耗名额。
//
// 行为：count > 0 时 count--；entry 不删除——下次同 IP 来仍在同一窗口内，
// 保留 windowStart 让"窗口期内的真实尝试时长"被准确记录（即使 rollback
// 把 count 回到 0，也不应当让 IP 假装"窗口刚开始"）。
func (l *loginRateLimiter) rollbackAttempt(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[ip]
	if !ok {
		// 极少见：rollback 调用之间该 entry 被并发清扫或 recordSuccess
		// 删掉。无 entry 直接返回——预占已经"消失"，目标达成。
		return
	}
	if e.count > 0 {
		e.count--
	}
}

// recordSuccess 在登录成功时调用，删除该 IP 的 entry——让正常用户偶发
// 输错几次密码后成功登录不被自己历史污点限流，下次再来时是干净计数。
func (l *loginRateLimiter) recordSuccess(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, ip)
}

// pruneExpiredLocked 清扫所有已过期 entry。调用方必须已持有 l.mu。
//
// 单次 O(N)；摊销到 softMax 触发频率上极低，对正常请求路径无感。Go map
// 在迭代中删除是安全的——这是 stdlib 文档明确允许的用法。
func (l *loginRateLimiter) pruneExpiredLocked() {
	now := time.Now()
	for ip, e := range l.entries {
		if now.Sub(e.windowStart) >= loginRateLimitWindow {
			delete(l.entries, ip)
		}
	}
}
