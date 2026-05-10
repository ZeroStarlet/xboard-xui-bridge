// Package web 实现 xboard-xui-bridge v0.2 的内置 Web 面板后端。
//
// 职责边界：
//
//   - HTTP 路由、中间件、请求/响应序列化、SPA 静态资源服务；
//   - 不直接读写数据库——所有业务逻辑通过 internal/auth.Service +
//     internal/config.LoadFromStore + internal/supervisor.Reload 走；
//   - 不构造 store / supervisor 自身，由 main.go 注入。
//
// 路由结构：
//
//	GET  /                              SPA 主页（index.html）
//	GET  /assets/...                    Vue 构建的静态资源
//	POST /api/auth/login                登录
//	POST /api/auth/logout               登出
//	GET  /api/auth/me                   获取当前登录用户信息
//	PUT  /api/account/password          修改自己的密码
//	GET  /api/settings                  获取全部运行参数（脱敏）
//	PATCH /api/settings                 部分更新运行参数（触发 supervisor.Reload）
//	GET  /api/bridges                   列出全部桥接
//	POST /api/bridges                   新增桥接
//	PUT  /api/bridges/{name}            更新桥接
//	DELETE /api/bridges/{name}          删除桥接
//	GET  /api/status                    引擎运行状态摘要
//	GET  /api/health                    存活探针（无需鉴权）
//
// 中间件链（从外到内）：
//
//	recover → logging → csrf（写操作） → auth（除登录 / 健康外） → handler
//
// 鉴权契约：
//
//	所有 /api/* 端点（除 /api/auth/login 与 /api/health）都要求带有效的
//	bridge_session cookie；缺失或失效返回 401。
package web

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/xboard-bridge/xboard-xui-bridge/internal/auth"
	"github.com/xboard-bridge/xboard-xui-bridge/internal/config"
	"github.com/xboard-bridge/xboard-xui-bridge/internal/store"
	"github.com/xboard-bridge/xboard-xui-bridge/internal/supervisor"
)

// 常量集中定义。Cookie 名 / 路径 / 关闭超时一类的字面量都在这里——
// 散落到各处会让"全局常量改一处"的维护变成"全局 grep 修十处"。
const (
	// SessionCookieName 是浏览器持有的 session token cookie 名。
	// 选 bridge_session 避免与 3x-ui / Xboard 自身的 cookie 冲突。
	SessionCookieName = "bridge_session"

	// shutdownTimeout 是 Server.Run 在 ctx 取消时给 http.Server.Shutdown 的
	// 最大宽限时间。与 supervisor 退出超时叠加共同决定进程整体退出延迟。
	shutdownTimeout = 10 * time.Second

	// reloadTimeout 是写操作后调 supervisor.Reload 等待旧引擎退出的超时。
	// 短于 30 秒：M5 的 Web 调用方期望"提交按钮按下后几秒内"得到响应；
	// 旧引擎卡死时 supervisor 会把句柄移到 draining 槽位让下次 Reload 处理。
	reloadTimeout = 15 * time.Second

	// readHeaderTimeout 防御 slowloris 攻击：限制 client 发送 header 的最大
	// 时长。10 秒对正常浏览器绰绰有余；恶意 client 慢慢吐字节会立刻被踹掉。
	readHeaderTimeout = 10 * time.Second
)

// Server 是 web 包对外暴露的主对象。
//
// 字段：
//
//	listenAddr     HTTP 监听地址；运行期不可修改（修改要通过重启进程，
//	               与"凭据修改要重启"同等级运维操作）。
//	log            日志接口；本包内的所有 log 都加 component=web 前缀。
//	store          持久层；用于 settings / bridges / admin_users / sessions CRUD。
//	supervisor     引擎热重载控制器；写操作完成后调 Reload 同步引擎。
//	authSvc        鉴权服务；middleware 与 auth handler 共享。
//	httpServer     底层 *http.Server；Run 启动后填充，Shutdown 时使用。
//	dbPath         数据库路径——LoadFromStore 需要它来填回 *Root.State.Database。
type Server struct {
	listenAddr string
	log        *slog.Logger
	store      store.Store
	supervisor *supervisor.Supervisor
	authSvc    *auth.Service
	dbPath     string

	httpServer *http.Server

	// loginLimiter 是 v0.5.3 起引入的"每 IP 登录失败计数器"——bcrypt
	// cost=12 已是慢路径，但限流让单 IP 暴力破解的可探测密码空间在窗口
	// 期内被强制压缩到上限次数（详见 middleware.go 中的 loginRateLimiter
	// 注释段）。Server 持有单实例，整个进程生命周期内共享状态。
	loginLimiter *loginRateLimiter
}

// New 构造 Server。
//
// 参数都是必填；任一为 nil/空 立即返回错误，避免在请求处理路径上
// 撞上 nil pointer。listenAddr 必须是 net.Listen 能接受的形态（如
// ":8787" / "127.0.0.1:8787"），由 config.Validate 已经规范化。
func New(
	cfg *config.Root,
	log *slog.Logger,
	st store.Store,
	sup *supervisor.Supervisor,
	authSvc *auth.Service,
) (*Server, error) {
	if cfg == nil {
		return nil, errors.New("web.New: cfg 不可为 nil")
	}
	if log == nil {
		return nil, errors.New("web.New: log 不可为 nil")
	}
	if st == nil {
		return nil, errors.New("web.New: store 不可为 nil")
	}
	if sup == nil {
		return nil, errors.New("web.New: supervisor 不可为 nil")
	}
	if authSvc == nil {
		return nil, errors.New("web.New: authSvc 不可为 nil")
	}
	if cfg.Web.ListenAddr == "" {
		return nil, errors.New("web.New: cfg.Web.ListenAddr 不可为空")
	}
	if cfg.State.Database == "" {
		return nil, errors.New("web.New: cfg.State.Database 不可为空")
	}
	return &Server{
		listenAddr:   cfg.Web.ListenAddr,
		log:          log.With("component", "web"),
		store:        st,
		supervisor:   sup,
		authSvc:      authSvc,
		dbPath:       cfg.State.Database,
		loginLimiter: newLoginRateLimiter(),
	}, nil
}

// Run 启动 HTTP 服务并阻塞至 ctx 取消，然后优雅关闭。
//
// 退出路径：
//
//	a) ctx.Done() → http.Server.Shutdown(shutdownTimeout 内)
//	b) ListenAndServe 返回错误 → 立即向上传 → main 退出
//
// 优雅关闭语义：拒绝新请求，让正在处理的请求走完（最多 shutdownTimeout）；
// 与 supervisor 的"等引擎退出"是两条独立的优雅链路，main 协调两者顺序
// （通常先 Web Shutdown 再 Supervisor Shutdown）。
func (s *Server) Run(ctx context.Context) error {
	mux := s.buildMux()

	s.httpServer = &http.Server{
		Addr:              s.listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
		// Read / Write timeout 不设置：API 调用普遍很快，但 settings PATCH 触发
		// supervisor.Reload 可能等几秒，硬限会让大配置切换误超时。
	}

	listener, err := net.Listen("tcp", s.listenAddr)
	if err != nil {
		return fmt.Errorf("监听 %s 失败：%w", s.listenAddr, err)
	}
	s.log.Info("Web 面板已启动", "listen", s.listenAddr)

	// ListenAndServe 阻塞；用单独 goroutine 跑，主流程等 ctx 取消信号。
	errCh := make(chan error, 1)
	go func() {
		// http.Server.Serve 在 Shutdown 调用后会返回 http.ErrServerClosed，
		// 这是正常退出，不向上抛错。
		if err := s.httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		s.log.Info("收到退出信号，开始优雅关闭 Web 面板")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			s.log.Warn("Web 面板优雅关闭超时（非致命）", "err", err)
		}
		// 等 Serve goroutine 真正退出（防止泄漏）。
		<-errCh
		return nil
	case err := <-errCh:
		// Serve 期间出错：通常是端口被占用或被 OS 拔了；向上传让 main 决定。
		return fmt.Errorf("HTTP Serve：%w", err)
	}
}

// buildMux 构造 http.ServeMux 并挂载所有路由。
//
// 路由顺序遵循"具体优先"：明确的 /api/* 端点先注册；最后注册的 "/" 是
// SPA fallback，匹配所有未命中前缀的请求。Go 1.22+ ServeMux 已经按
// 最长前缀匹配，但保持注册顺序与心智模型一致便于审计。
//
// 中间件链：每个 handler 通过包装函数串成链。匿名 closure 让依赖注入
// 简洁——不需要 Server 类型实现 http.Handler 接口。
func (s *Server) buildMux() *http.ServeMux {
	mux := http.NewServeMux()

	// 通用中间件链（外层依次：recover → logging → securityHeaders → handler）。
	// auth / csrf 是按需追加，不放入此链。
	//
	// securityHeaders 放在最内层（直接调 handler 前）：在 handler 写 body 前
	// 头部就已 set；即使 handler 自己 Set 同名 header（如 spaHandler 显式
	// 设 Content-Type）也是直接覆盖，行为一致。
	//
	// 放在最外层在功能上等价——只要 securityHeaders 在 handler 调用前执行，
	// panic 触发的 recoverMiddleware writeError 仍会写出已 set 的安全头
	// （除非 ResponseWriter 已 committed）。选最内层是为了让职责更紧贴
	// handler，未来若想为某个路由单独豁免安全头，包装函数改写更直观。
	commonChain := func(h http.HandlerFunc) http.HandlerFunc {
		return s.recoverMiddleware(s.loggingMiddleware(s.securityHeadersMiddleware(h)))
	}

	// 鉴权要求链（追加 auth）。
	authChain := func(h http.HandlerFunc) http.HandlerFunc {
		return commonChain(s.authMiddleware(h))
	}

	// 写操作链（追加 csrf 后再 auth）。CSRF 校验放 auth 前面：
	// 即使提供了合法 cookie，跨站发起的请求也会因 Origin/Referer 不匹配
	// 被先一步拦下，杜绝 CSRF。
	writeChain := func(h http.HandlerFunc) http.HandlerFunc {
		return commonChain(s.csrfMiddleware(s.authMiddleware(h)))
	}

	// 1. 健康探针：无鉴权，仅暴露最基本存活信息。
	mux.HandleFunc("GET /api/health", commonChain(s.handleHealth))

	// 2. 鉴权端点：自身不需要 auth middleware（login / logout 行为自携带）。
	mux.HandleFunc("POST /api/auth/login", commonChain(s.csrfMiddleware(s.handleLogin)))
	mux.HandleFunc("POST /api/auth/logout", writeChain(s.handleLogout))
	mux.HandleFunc("GET /api/auth/me", authChain(s.handleAuthMe))

	// 3. 账户操作。
	mux.HandleFunc("PUT /api/account/password", writeChain(s.handleChangePassword))

	// 4. 配置管理。
	mux.HandleFunc("GET /api/settings", authChain(s.handleGetSettings))
	mux.HandleFunc("PATCH /api/settings", writeChain(s.handlePatchSettings))

	// 5. 桥接管理。
	mux.HandleFunc("GET /api/bridges", authChain(s.handleListBridges))
	mux.HandleFunc("POST /api/bridges", writeChain(s.handleCreateBridge))
	mux.HandleFunc("PUT /api/bridges/{name}", writeChain(s.handleUpdateBridge))
	mux.HandleFunc("DELETE /api/bridges/{name}", writeChain(s.handleDeleteBridge))

	// 6. 引擎状态。
	mux.HandleFunc("GET /api/status", authChain(s.handleGetStatus))

	// 7. SPA fallback：捕获所有未命中 /api/ 的 GET 请求。
	mux.Handle("/", commonChain(s.spaHandler()))

	return mux
}

// reloadCtx 返回一个用于 supervisor.Reload 的 ctx——派生自请求 ctx，叠加
// reloadTimeout 短超时。这样客户端 cancel 请求时也能让 reload 早退。
func (s *Server) reloadCtx(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, reloadTimeout)
}

// reloadFromStore 加载最新配置并触发 supervisor.Reload。
//
// 调用顺序：
//
//	1) Web Handler 已写完 store；
//	2) 本函数 LoadFromStore → 拿到包含最新 settings/bridges 的 *Root；
//	3) supervisor.Reload(ctx, newCfg)。
//
// 失败原因：
//
//	a) LoadFromStore 失败：通常是数据库故障，写已成功但引擎没切换；
//	   handler 需要返回 5xx 让前端提示"配置已保存但引擎未切换"。
//	b) Reload 失败：旧引擎卡死等；同上 5xx。
//
// 调用方 reload 失败时不应回滚 store 写入——store 才是真相源，引擎
// 重启后会重新加载并对齐。
func (s *Server) reloadFromStore(ctx context.Context) error {
	newCfg, err := config.LoadFromStore(ctx, s.store, s.dbPath)
	if err != nil {
		return fmt.Errorf("从 store 加载新配置：%w", err)
	}
	rctx, cancel := s.reloadCtx(ctx)
	defer cancel()
	if err := s.supervisor.Reload(rctx, newCfg); err != nil {
		return fmt.Errorf("supervisor.Reload：%w", err)
	}
	return nil
}
