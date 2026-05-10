// Package supervisor 管理同步引擎（internal/sync.Engine）的生命周期，
// 提供"全停全启"式的运行期热重载能力。
//
// 为什么需要 Supervisor：
//
//	v0.1 时代配置在启动时一次性加载，运行期不可修改。v0.2 升级目标是让
//	Web GUI 在线增删 bridge / 修改凭据 / 调整间隔，因此必须在不重启进程
//	的前提下"切换引擎"。Supervisor 把"启动 / 停止 / 切换"封装成单一对象，
//	让 main.go 与未来的 Web Handler（M5）都通过同一个 API 操作引擎。
//
// 全停全启策略（vs 按字段精细化重启）：
//
//	a) 实现简单：cancel ctx → wg.Wait → 用新 cfg 装配 → 启动；不需要在 worker
//	   粒度做"哪个 bridge 改了什么字段"的 diff 算法；
//	b) 切换代价小：4 类同步循环全部停掉再起，过渡期 < 200ms（worker 用
//	   select ctx.Done 立即返回）；
//	c) 凭据变更最重的场景天然覆盖：Xboard / 3x-ui 客户端在 Reload 时整体
//	   重建，老连接被 *http.Client 自动回收。
//
// "先构造新引擎再切换旧引擎"原则：
//
//	Reload 内部先 Validate(newCfg) → 构造新客户端 → 调 sync.New 装配引擎；
//	若任一步失败，旧引擎仍在跑，Reload 返回错误让运维继续修配置。这样
//	避免"运维误改一个字段把所有同步停掉"的窘境。
//
// 并发模型：
//
//	所有对 *Supervisor 的操作都串行化通过 mu 保护；Reload 与 Snapshot
//	可被任意 goroutine 调用。Run 在外层 rootCtx 取消后才返回，期间不持锁
//	等待——所以 Reload 不会被 Run 永久阻塞。
package supervisor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	stdsync "sync"

	"github.com/xboard-bridge/xboard-xui-bridge/internal/config"
	"github.com/xboard-bridge/xboard-xui-bridge/internal/store"
	syncengine "github.com/xboard-bridge/xboard-xui-bridge/internal/sync"
	"github.com/xboard-bridge/xboard-xui-bridge/internal/xboard"
	"github.com/xboard-bridge/xboard-xui-bridge/internal/xui"
)

// Supervisor 是引擎的生命周期管理者。
//
// 字段不直接暴露：所有对 cfg / current 的访问都通过 mu 串行化。
type Supervisor struct {
	mu      stdsync.Mutex
	log     *slog.Logger
	store   store.Store

	// cfg 当前生效的配置；Reload 成功时被替换。Run 启动期间也是这个值。
	cfg *config.Root

	// current 当前正在运行的引擎句柄；nil 表示尚未启动或已停止。
	current *engineHandle

	// draining 槽位：保存上一次 stopCurrentLocked 超时后未来得及退出的旧句柄。
	//
	// 解决"超时后状态混乱"问题：stopCurrentLocked 超时返回错误后，旧引擎
	// 实际上已被 cancel——它会在某个稍后的瞬间真正退出（done channel 关闭）。
	// 把它移到 draining 槽位，让下次 Reload 进入时先 reap（已退出则清理；
	// 仍在退出中则拒绝新 Reload 直至清理完成）。这样保证：任何时刻最多
	// 只有 1 个新引擎在跑，已被 cancel 的旧引擎要么被清理、要么被显式追踪。
	draining *engineHandle

	// running 标记 Run 是否在执行；用于 Reload 在 Run 之前的合法性检查。
	running bool
}

// engineHandle 包装一份正在运行的 Engine 与其控制接口。
//
// 设计动机：sync.Engine.Run 是阻塞调用，Supervisor 需要持有对应的 cancel
// 与 done channel 才能"主动停止 + 等待退出"。把这三者一起持有避免散落
// 多个字段。
type engineHandle struct {
	engine *syncengine.Engine
	cancel context.CancelFunc
	// done 在启动 goroutine 退出时被 close；外部用 <-done 等待。
	done chan struct{}
}

// New 构造 Supervisor。
//
// 不启动任何 goroutine；调用方必须显式调 Run(ctx) 才会真正运行引擎。
// 这一拆分让 main 在 Run 前还能再做日志 / Web 服务等装配，避免装配未完
// 时引擎已经发起上游请求的怪异状态。
//
// cfg 必须已通过 config.Validate（LoadFromStore 出来的实例已经过校验）。
func New(cfg *config.Root, log *slog.Logger, st store.Store) (*Supervisor, error) {
	if cfg == nil {
		return nil, errors.New("supervisor: cfg 不可为 nil")
	}
	if log == nil {
		return nil, errors.New("supervisor: log 不可为 nil")
	}
	if st == nil {
		return nil, errors.New("supervisor: store 不可为 nil")
	}
	return &Supervisor{
		log:   log.With("component", "supervisor"),
		store: st,
		cfg:   cfg,
	}, nil
}

// Run 启动初始引擎并阻塞至 rootCtx 取消，然后优雅停止当前引擎。
//
// 退出路径：
//
//	a) rootCtx.Done()：正常退出（SIGINT/SIGTERM）；
//	b) 启动初始引擎失败：直接返回错误，Run 内部不会卡住。
//
// 优雅停止策略：等待当前引擎自然退出（worker 都是 select ctx.Done 立即
// 返回，至多等到一个 HTTP 调用完成）。Run 不设置硬超时——若上游 HTTP
// 卡死，靠 *http.Client.Timeout 兜底，进程整体超时后由 systemd 等管控
// 工具发送 SIGKILL 强制收尾。
func (s *Supervisor) Run(rootCtx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return errors.New("supervisor.Run 已经在运行中，禁止重复调用")
	}
	// 启动期凭据护栏：与 Reload 路径共享同一逻辑。
	guarded := applyCredsGuard(s.cfg, s.log)
	if err := s.startLocked(s.cfg); err != nil {
		s.mu.Unlock()
		return fmt.Errorf("启动初始引擎：%w", err)
	}
	s.running = true
	// 在持锁期间 capture 用于日志的字段，避免解锁后并发 Reload 替换 cfg
	// 造成数据竞争（go test -race 会报）。
	bridgeCount := len(s.cfg.Bridges)
	s.mu.Unlock()

	s.log.Info("引擎已启动，等待退出信号",
		"bridges", bridgeCount,
		"creds_guard_triggered", guarded,
	)

	<-rootCtx.Done()
	s.log.Info("收到退出信号，开始优雅停止")

	s.mu.Lock()
	s.running = false
	// 用 background ctx 等：rootCtx 已 Done，再用它做超时控制就立刻返回。
	if err := s.stopCurrentLocked(context.Background()); err != nil {
		s.log.Warn("停止当前引擎出错（非致命）", "err", err)
	}
	// 退出前最后一次 reap：尽力清理 draining 槽位，避免泄漏 goroutine。
	// 用非阻塞 reap：若仍未退出，goroutine 会在进程退出时随之结束。
	_ = s.reapDrainingLocked()
	s.mu.Unlock()
	return nil
}

// Reload 用 newCfg 替换当前引擎。
//
// ctx 用于"等待旧引擎退出"的超时控制；调用方应当传入有限超时（如 30s），
// 防止旧引擎卡死时 Reload 永久阻塞。新引擎装配（验证 + 构造客户端 +
// 构造 Engine）在切换前完成，失败时旧引擎仍在跑、Reload 返回错误。
//
// 副作用：
//
//	a) Reload 成功 → 当前引擎被新引擎替换，s.cfg 更新；
//	b) Reload 失败（在 stopCurrentLocked 前的任何步骤） → 旧引擎仍在跑，
//	   s.cfg 保留旧值；
//	c) Reload 失败（stopCurrentLocked 超时） → 旧引擎被 cancel 但尚未退出，
//	   句柄被搬到 draining 槽位；s.cfg 保留旧值；下次 Reload 入口会先
//	   reapDrainingLocked 检查 draining 是否清理完。
//
// 并发：内部加 mu，所以多个 goroutine 同时 Reload 会串行执行；最后一个
// 调用的 newCfg 胜出。
//
// 注意：调用 Reload 的 newCfg 应当来自最新一次 LoadFromStore 的结果——
// Reload 不会自己去重新读 settings/bridges。这层职责落在 Web Handler：
// 写完 settings 后立刻调 LoadFromStore + Reload。
func (s *Supervisor) Reload(ctx context.Context, newCfg *config.Root) error {
	if newCfg == nil {
		return errors.New("supervisor.Reload: newCfg 不可为 nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// 顺序：先做廉价的状态检查，再做 Validate（纯计算，但仍占持锁时间），
	// 最后做 reap + buildEngine。这样在 Run 之前 Reload 时能直接拒绝，
	// 不必走 Validate（也避免误导式错误"新配置非法"——其实是流程错误）。
	if !s.running {
		return errors.New("supervisor.Reload: 必须在 Run 期间调用")
	}

	// 处理上一次 Reload 留下的 draining 句柄：仍未清理 → 拒绝本次 Reload。
	if err := s.reapDrainingLocked(); err != nil {
		return err
	}

	if err := newCfg.Validate(); err != nil {
		return fmt.Errorf("Reload: 新配置非法：%w", err)
	}

	// 凭据完整性护栏：与 Run 启动期一致，避免半填配置触发无效请求。
	guarded := applyCredsGuard(newCfg, s.log)

	// 先构造新引擎（不启动）。任何构造失败都不影响旧引擎。
	newEngine, err := s.buildEngine(newCfg)
	if err != nil {
		return fmt.Errorf("Reload: 装配新引擎：%w", err)
	}

	// 停旧。等待退出超时由 ctx 控制。超时时旧句柄被搬到 draining。
	if err := s.stopCurrentLocked(ctx); err != nil {
		return fmt.Errorf("Reload: 停止旧引擎：%w", err)
	}

	// 启新。startEngineLocked 不返回错误（goroutine 启动总能成功）。
	s.startEngineLocked(newEngine)
	s.cfg = newCfg

	s.log.Info("引擎热重载完成",
		"bridges", len(newCfg.Bridges),
		"creds_guard_triggered", guarded,
	)
	return nil
}

// Snapshot 返回当前 cfg 的浅拷贝，便于 Web Handler 读取展示。
//
// 浅拷贝细节：
//
//	a) 顶层 *Root 单独 new，避免外部修改污染内部 cfg；
//	b) Bridges 切片单独 copy，让外部 append 不影响内部；
//	c) 其它字段（Log/Xboard/Xui/...）是值类型，已经被顶层拷贝复制。
//
// 调用方不应基于 Snapshot 修改后再写回——写回应当走 Web Handler 的
// SetSettings + Reload 闭环。
func (s *Supervisor) Snapshot() *config.Root {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg == nil {
		return nil
	}
	snap := *s.cfg
	snap.Bridges = make([]config.Bridge, len(s.cfg.Bridges))
	copy(snap.Bridges, s.cfg.Bridges)
	return &snap
}

// Running 返回 Supervisor 是否处于 Run 期间。Web Handler 在 Reload 之前
// 用它做合法性检查可以避免被 stopCurrentLocked 的"未启动"错误绕过。
func (s *Supervisor) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// startLocked 在持有 mu 时调用：构造引擎并启动 goroutine。
func (s *Supervisor) startLocked(cfg *config.Root) error {
	eng, err := s.buildEngine(cfg)
	if err != nil {
		return err
	}
	s.startEngineLocked(eng)
	return nil
}

// buildEngine 构造一份新引擎（含其依赖的 Xboard / 3x-ui 客户端）。
//
// 失败原因：
//
//	a) xui.New 返回错误（当前实现极少触发，但保留传播）；
//	b) syncengine.New 协议适配器找不到（cfg.Bridges 中含未实现协议）。
//
// 不在 buildEngine 内启动 goroutine：让构造与启动分两步，使得 Reload
// 可以"先构造，发现错误就立刻返回；构造成功再杀旧启新"。
func (s *Supervisor) buildEngine(cfg *config.Root) (*syncengine.Engine, error) {
	xboardC := xboard.New(cfg.Xboard)
	xuiC, err := xui.New(cfg.Xui)
	if err != nil {
		return nil, fmt.Errorf("初始化 3x-ui 客户端：%w", err)
	}
	eng, err := syncengine.New(cfg, s.log, xboardC, xuiC, s.store)
	if err != nil {
		return nil, fmt.Errorf("装配同步引擎：%w", err)
	}
	return eng, nil
}

// startEngineLocked 在持有 mu 时启动 engine.Run goroutine 并保存 handle。
//
// 调用方约束：mu 必须已锁，且 s.current 必须为 nil（要么从未启动，要么
// 已经被 stopCurrentLocked 清理过）。否则旧 handle 会被覆盖，旧 goroutine
// 永久泄漏。
func (s *Supervisor) startEngineLocked(eng *syncengine.Engine) {
	ctx, cancel := context.WithCancel(context.Background())
	handle := &engineHandle{
		engine: eng,
		cancel: cancel,
		done:   make(chan struct{}),
	}
	go func() {
		defer close(handle.done)
		eng.Run(ctx)
	}()
	s.current = handle
}

// applyCredsGuard 检查 Xboard / 3x-ui 凭据完整性，必要时清空 cfg.Bridges。
//
// 行为：当 enabled bridge 存在但凭据未配齐（xboard.api_host / xboard.token
// 任一为空，或 xui.api_host / xui.username / xui.password 任一为空）时，
// 强制把 cfg.Bridges 置为 nil，让引擎装配 0 worker，避免每周期产生大量
// "无效" 或 "未鉴权" 请求。
//
// 返回值：true 表示触发了清空；false 表示无需介入。便于调用方在日志里
// 区分"按用户配置正常运行"与"被护栏降级到空载"。
//
// 设计动机：M2 阶段把这一逻辑放在 main.go；但 Reload 路径同样需要它，
// 否则 Web Handler 写入半填配置后引擎会装配 worker 然后猛刷无效请求。
// 把 helper 放进 supervisor 包，让 Run 启动期与 Reload 路径共用同一份
// 决策，不会再出现两路逻辑不一致的隐患。
//
// 该函数原地修改 cfg.Bridges，由调用方自行决定是否需要二次拷贝。当前
// 用法是：cfg 来自 LoadFromStore（独立 *Root 实例），原地修改不会污染
// 持久层，调用方下次 LoadFromStore 仍能拿到 store 里的真实 bridges。
func applyCredsGuard(cfg *config.Root, log *slog.Logger) bool {
	// 委托给 cfg.{Xboard,Xui}.CredsComplete()——同一份函数在
	// internal/web/status_handler.go 也使用，杜绝运维看到的"凭据完整性"灯
	// 与引擎实际行为不一致。v0.4 起 xui 仅 cookie 登录模式，CredsComplete
	// 检查 APIHost + Username + Password 三必填。
	xboardOK := cfg.Xboard.CredsComplete()
	xuiOK := cfg.Xui.CredsComplete()
	if xboardOK && xuiOK {
		return false
	}
	enabledCount := 0
	for _, b := range cfg.Bridges {
		if b.Enable {
			enabledCount++
		}
	}
	if enabledCount == 0 {
		// 没有 enabled bridge：worker 数本来就是 0，无须清空。
		return false
	}
	log.Warn("检测到启用的 Bridge 但凭据未完整配置，引擎将以空载模式启动以避免无效上游请求",
		"enabled_bridge_count", enabledCount,
		"xboard_creds_ok", xboardOK,
		"xui_creds_ok", xuiOK,
	)
	cfg.Bridges = nil
	return true
}

// stopCurrentLocked 在持有 mu 时停止当前引擎并等待 goroutine 退出。
//
// 行为：
//
//	a) s.current 为 nil → 直接返回 nil（已经无引擎在跑）；
//	b) 取消 ctx → 等 done channel 关闭 → 清空 s.current 后返回 nil；
//	c) 等待期间 ctx 超时 → 把当前 handle 移到 s.draining 槽位（已被 cancel，
//	   会在稍后真正退出），清空 s.current 后返回错误。下次 Reload 入口会
//	   先 reapDrainingLocked 检查 draining 是否已退出。
//
// 不变量保证（重要）：
//
//	任何时刻 s.current 与 s.draining 中"处于运行的引擎"加起来 ≤ 1。
//	c 路径之后 s.current=nil，s.draining 是已被 cancel 的旧引擎；只要
//	下次 Reload 等到 draining 真退出后再启动新引擎，就保证不会有两个
//	活跃引擎同时跑。
func (s *Supervisor) stopCurrentLocked(ctx context.Context) error {
	if s.current == nil {
		return nil
	}
	s.current.cancel()
	select {
	case <-s.current.done:
		s.current = nil
		return nil
	case <-ctx.Done():
		// 超时：把已被 cancel 的旧句柄移到 draining 槽位，等下次 Reload 清理。
		s.draining = s.current
		s.current = nil
		return fmt.Errorf("等待引擎退出超时（已转入 draining 状态）：%w", ctx.Err())
	}
}

// reapDrainingLocked 检查 draining 槽位是否已退出。
//
// 返回值语义：
//
//	nil       槽位为空 / 已退出已清理；可以继续启动新引擎；
//	非 nil    槽位仍在退出中；调用方应当向上传播让运维稍后重试。
//
// 不阻塞：用 default 分支做"非阻塞 select"，让 Reload 不会因 draining
// 卡死而无限等待——若需要等到底，外层应当在新一次 Reload 调用上传更长
// ctx 让 stopCurrentLocked 自己等（但此时 current 已是新一轮的句柄，逻辑
// 干净）。
func (s *Supervisor) reapDrainingLocked() error {
	if s.draining == nil {
		return nil
	}
	select {
	case <-s.draining.done:
		s.draining = nil
		return nil
	default:
		return errors.New("supervisor: 上一次 Reload 留下的旧引擎仍在退出中，请稍后重试")
	}
}
