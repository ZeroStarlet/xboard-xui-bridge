package web

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/xboard-bridge/xboard-xui-bridge/internal/auth"
	"github.com/xboard-bridge/xboard-xui-bridge/internal/store"
)

// loginRequest 是 POST /api/auth/login 的请求体。
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// userResponse 是 GET /api/auth/me 与 login 成功响应共用的用户信息形态。
//
// 不含 password_hash —— 那是绝对不能回传客户端的。
// LastLoginAt 用 RFC3339 字符串：JS 自带 Date.parse 兼容，时区明确。
type userResponse struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	LastLoginAt string `json:"last_login_at,omitempty"`
}

// handleLogin 处理 POST /api/auth/login。
//
// 行为：
//
//   - 入口先做"每 IP 失败次数"限流预占——reserveAttempt 在临界区内同时
//     检查 count + 递增，避免并发 burst 突破上限（详见 middleware.go 的
//     loginRateLimiter 注释段）；超出 5 次/5 分钟即返 429；
//   - 解析 body → 调 auth.Login → 写 Set-Cookie → 返回用户信息；
//   - 凭据错失败 → 401 + 模糊文案；预占已经计入，无需额外动作；
//   - 登录成功 → recordSuccess 清该 IP entry，让正常用户偶发输错后回到
//     干净计数；
//   - 非凭据类错误（body 解析 / 5xx 内部错） → rollbackAttempt 撤销预占，
//     避免真用户因为服务端故障被白消耗名额。
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !s.loginLimiter.reserveAttempt(ip) {
		// 仅在限流命中时打 WARN——让运维能在日志里发现暴力破解尝试，
		// 但不打 ERROR 避免被运维监控系统误判为故障。
		s.log.Warn("登录限流命中：IP 在窗口期内失败次数超出上限",
			"ip", ip,
			"max_attempts", loginRateLimitMaxAttempts,
			"window", loginRateLimitWindow.String(),
		)
		s.writeError(w, http.StatusTooManyRequests, errCodeTooManyRequests,
			"登录失败次数过多，请稍后再试")
		return
	}

	var req loginRequest
	if err := readJSON(r, &req); err != nil {
		// body 解析错与暴力破解信号无关——撤销预占，避免真用户偶发坏请求被罚。
		s.loginLimiter.rollbackAttempt(ip)
		s.writeError(w, http.StatusBadRequest, errCodeBadRequest, "请求体格式错误")
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" || req.Password == "" {
		// 字段空也属于客户端误用，与凭据破解无关——撤销预占。
		s.loginLimiter.rollbackAttempt(ip)
		s.writeError(w, http.StatusBadRequest, errCodeBadRequest, "用户名与密码不可为空")
		return
	}

	token, err := s.authSvc.Login(r.Context(), username, req.Password, ip, r.UserAgent())
	switch {
	case err != nil && errors.Is(err, auth.ErrInvalidCredentials):
		// 用户名或密码错——预占已经计入，无需额外 record。
		s.writeError(w, http.StatusUnauthorized, errCodeUnauthorized, "用户名或密码错误")
		return
	case err != nil && token != "":
		// 非致命错误：例如 last_login_at 更新失败但 session 已落库（auth.Login 契约）。
		// 仅 WARN 提示运维，登录路径继续走完——把 token 还给用户避免一次"虚假失败"。
		s.log.Warn("Login 部分成功（非致命）", "username", username, "err", err)
	case err != nil:
		// 致命：未拿到 token，登录确实失败。撤销预占——5xx 与暴力破解无关，
		// 真用户不该因服务端故障被白消耗名额。
		s.loginLimiter.rollbackAttempt(ip)
		s.log.Error("Login 失败", "username", username, "err", err)
		s.writeError(w, http.StatusInternalServerError, errCodeInternal, "服务器内部错误")
		return
	}

	// 走到这里 = 登录成功（含"部分成功"路径）：删除该 IP entry，让该 IP
	// 后续的输错回到干净计数。
	s.loginLimiter.recordSuccess(ip)

	// 计算 cookie maxAge：用 supervisor.Snapshot 当前生效的 cfg.Web.SessionMaxAgeHours。
	// 不直接持有 cfg：Web 的运行时参数（如 sessionMaxAge）是由 supervisor 持有
	// 的最新值，避免 Server 自己保存一份过期副本。
	maxAge := s.currentSessionMaxAge()
	setSessionCookie(w, r, token, maxAge)

	user := s.lookupUserByName(r, username)
	if user == nil {
		// 极端情况：刚 Login 成功，立刻 GetAdminUserByUsername 找不到。
		// 几乎不可能发生（中间没有 DELETE）；保守路径仍返回 200 + 仅 username。
		s.writeJSON(w, http.StatusOK, userResponse{Username: username})
		return
	}
	s.writeJSON(w, http.StatusOK, user)
}

// handleLogout 处理 POST /api/auth/logout。
//
// 行为：删除当前 session（幂等），清 cookie，返回 204。
// 即使 token 已无效（中间件已踹掉）也通过——客户端调 logout 不应再收 401。
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	token := CurrentSessionToken(r.Context())
	if token != "" {
		if err := s.authSvc.Logout(r.Context(), token); err != nil {
			// Logout 失败仅记录；不影响清 cookie 的体验。
			s.log.Warn("删除 session 失败（不影响登出）", "err", err)
		}
	}
	clearSessionCookie(w, r)
	s.writeJSON(w, http.StatusOK, struct{}{})
}

// handleAuthMe 处理 GET /api/auth/me。
//
// 返回当前已认证用户（authMiddleware 已经把 user 挂在 ctx 上）。
func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	user := CurrentUser(r.Context())
	if user == nil {
		// 防御：authMiddleware 不应让 nil user 走到这里。
		s.writeError(w, http.StatusInternalServerError, errCodeInternal, "未取到当前用户")
		return
	}
	s.writeJSON(w, http.StatusOK, marshalUser(user))
}

// changePasswordRequest 是 PUT /api/account/password 的请求体。
type changePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

// handleChangePassword 处理 PUT /api/account/password。
//
// 副作用：成功后该用户所有 session 被删除（auth.ChangePassword 内部事务保证）。
// 当前请求的 cookie 失效；前端应当在 200 后立刻跳转登录页。
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	user := CurrentUser(r.Context())
	if user == nil {
		s.writeError(w, http.StatusUnauthorized, errCodeUnauthorized, "未登录")
		return
	}

	var req changePasswordRequest
	if err := readJSON(r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, errCodeBadRequest, "请求体格式错误")
		return
	}
	if req.OldPassword == "" || req.NewPassword == "" {
		s.writeError(w, http.StatusBadRequest, errCodeBadRequest, "旧密码与新密码不可为空")
		return
	}

	if err := s.authSvc.ChangePassword(r.Context(), user.ID, req.OldPassword, req.NewPassword); err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials):
			s.writeError(w, http.StatusUnauthorized, errCodeUnauthorized, "旧密码不正确")
		case errors.Is(err, auth.ErrPasswordTooShort):
			s.writeError(w, http.StatusBadRequest, errCodeBadRequest, "新密码长度不足")
		case errors.Is(err, auth.ErrPasswordReuse):
			s.writeError(w, http.StatusBadRequest, errCodeBadRequest, "新密码不可与旧密码相同")
		default:
			s.log.Error("ChangePassword 失败", "user_id", user.ID, "err", err)
			s.writeError(w, http.StatusInternalServerError, errCodeInternal, "服务器内部错误")
		}
		return
	}
	// 改密成功：sessions 已被批量删除，清当前 cookie 让浏览器彻底丢凭证。
	clearSessionCookie(w, r)
	s.writeJSON(w, http.StatusOK, struct{}{})
}

// currentSessionMaxAge 取 supervisor 当前 cfg 的 SessionMaxAgeHours，转为 Duration。
//
// 注意：session_max_age_hours 是"启动期定型"字段——PATCH /api/settings
// 已显式拒绝运行时修改（详见 handlePatchSettings 的 web.* 拒绝逻辑），
// 所以这里通过 Snapshot 读到的值实质上等价于启动期值。仍然走 Snapshot
// 是为了让"未来若把该字段升级为热重载支持"时本函数无需修改——保持
// 调用路径稳定，未来扩展只需放开 PATCH 端的拒绝即可。
func (s *Server) currentSessionMaxAge() time.Duration {
	cfg := s.supervisor.Snapshot()
	if cfg == nil || cfg.Web.SessionMaxAgeHours <= 0 {
		// 防御：cfg 缺失时按 7 天兜底，与 config.defaultWebSessionMaxHours 一致。
		return 7 * 24 * time.Hour
	}
	return time.Duration(cfg.Web.SessionMaxAgeHours) * time.Hour
}

// lookupUserByName 用 username 反查 *userResponse；失败返回 nil。
//
// 抽函数复用：login handler 在登录成功后需要回传当前用户信息；
// 直接查比"再回头解析 token"简单。
func (s *Server) lookupUserByName(r *http.Request, username string) *userResponse {
	u, err := s.store.GetAdminUserByUsername(r.Context(), username)
	if err != nil {
		return nil
	}
	resp := marshalUser(&u)
	return &resp
}

// marshalUser 把 store.AdminUserRow 转为对外的 userResponse（脱敏 + 时间格式化）。
//
// password_hash 等敏感字段绝不出现在返回结构里——这是面向客户端的
// "可读资料"投影。
func marshalUser(u *store.AdminUserRow) userResponse {
	if u == nil {
		return userResponse{}
	}
	resp := userResponse{
		ID:       u.ID,
		Username: u.Username,
	}
	if !u.LastLoginAt.IsZero() {
		resp.LastLoginAt = u.LastLoginAt.UTC().Format(time.RFC3339)
	}
	return resp
}
