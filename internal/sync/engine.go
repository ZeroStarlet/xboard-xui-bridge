// Package sync 负责把"周期性 IO 协议"具象成可运行的同步引擎。
//
// 核心模型：
//
//	每个 config.Bridge 对应一个 bridgeWorker；worker 内部启动四条
//	周期循环（用户 / 流量 / 在线 / 状态），共享一份 Xboard / 3x-ui 客户端
//	与本地状态库。
//
// 与上游错误的关系：
//
//	任意一次循环失败仅打印 warn 不退出；进程级退出只发生在 ctx 被取消
//	（main 收到 SIGINT/SIGTERM）的情况下。这是有意为之——网络抖动、面板
//	重启、token 临时失效都不应导致中间件主进程被杀，否则会把可恢复故障
//	放大成不可恢复故障。
//
// 日志 trace_id（v0.5.3 起）：
//
//	每次 runStep 调用都会生成一个 6 字节 base64url（8 字符）的短 trace_id，
//	通过 ctx 携带的 *slog.Logger 注入到本次 tick 的所有主入口日志中。运维
//	查日志时按 trace_id grep 能把"同一周期内"的多条日志（开始 / 进度 /
//	完成）串起来，避免多 bridge × 4 loop 重叠时排查困难。详见 runStep。
//	工具函数：genTraceID / contextWithLogger / loggerFromCtx。
package sync

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"strconv"
	stdsync "sync"
	"time"

	"github.com/xboard-bridge/xboard-xui-bridge/internal/config"
	"github.com/xboard-bridge/xboard-xui-bridge/internal/protocol"
	"github.com/xboard-bridge/xboard-xui-bridge/internal/store"
	"github.com/xboard-bridge/xboard-xui-bridge/internal/xboard"
	"github.com/xboard-bridge/xboard-xui-bridge/internal/xui"
)

// loggerCtxKey 是 ctx 上 *slog.Logger 的 key 类型。用 unexported 类型
// 避免外部包"碰巧用相同字面量"覆盖我们的值——这是 context 包文档明确推荐
// 的模式，与 internal/web/middleware.go 中的 ctxKey 同等设计。
type loggerCtxKey struct{}

// contextWithLogger 把 logger 挂在 ctx 上，供下游 sync 函数通过
// loggerFromCtx 取回。仅在 runStep 中调用一次（每 tick 注入一份新 logger，
// 已带 loop / trace_id 字段）。
func contextWithLogger(ctx context.Context, log *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerCtxKey{}, log)
}

// loggerFromCtx 从 ctx 取出 trace 化 logger；ctx 无注入时返回 fallback。
//
// 使用场景：syncUsers / syncTraffic 等主入口函数在开头调一次：
//
//	log := loggerFromCtx(ctx, w.log)
//
// 此后函数内部所有 log 调用都用 log 而非 w.log。子 helper 函数（如
// applyAdd / parseExistingManagedClients）当前无日志输出，故无需改动；
// 未来若新增子函数日志，按需把 logger 显式传参或经 ctx 取。
func loggerFromCtx(ctx context.Context, fallback *slog.Logger) *slog.Logger {
	if v, ok := ctx.Value(loggerCtxKey{}).(*slog.Logger); ok {
		return v
	}
	return fallback
}

// genTraceID 生成 6 字节 crypto/rand 的 base64url 编码（8 字符 ASCII）。
//
// 字符集 [A-Za-z0-9_-]，URL safe，可直接放进 JSON / 日志聚合查询。8 字符
// = 48 bit 熵，碰撞概率极低（~2.8 * 10^14 一对碰撞）；同一周期内的多条
// 日志通过 trace_id grep 即可串联。
//
// 极少见兜底：crypto/rand 失败（OS 熵池异常）时退化为 unix 纳秒的 36 进制
// 字符串。仍能保证字符串"几乎不重复"——纳秒分辨率下连续生成两次需要在
// 同一 ns 内才能撞 ID，对中间件运维日志查询完全够用。
func genTraceID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// Engine 是中间件的同步入口。
//
// 实例化后调用 Run(ctx) 启动；Run 阻塞直到 ctx 取消，期间所有 Bridge 并行运行。
type Engine struct {
	bridges []*bridgeWorker
	log     *slog.Logger
}

// New 把配置 + 客户端 + 状态库装配成 Engine。
//
// 失败原因：协议适配器构造失败（出现配置中未实现的协议名）。这种错误属于
// 启动期错误，应当让进程退出。
func New(cfg *config.Root, log *slog.Logger, xboardC *xboard.Client, xuiC *xui.Client, st store.Store) (*Engine, error) {
	workers := make([]*bridgeWorker, 0, len(cfg.Bridges))
	for i := range cfg.Bridges {
		b := cfg.Bridges[i]
		if !b.Enable {
			log.Info("Bridge 已禁用，跳过装配", "bridge", b.Name)
			continue
		}
		ad, err := protocol.For(b.Protocol)
		if err != nil {
			return nil, err
		}
		w := &bridgeWorker{
			cfg:       b,
			intervals: cfg.Intervals,
			reporting: cfg.Reporting,
			adapter:   ad,
			xboardC:   xboardC,
			xuiC:      xuiC,
			store:     st,
			log:       log.With("bridge", b.Name, "protocol", b.Protocol),
		}
		workers = append(workers, w)
	}
	if len(workers) == 0 {
		log.Warn("没有任何启用的 Bridge，引擎将立即返回")
	}
	return &Engine{bridges: workers, log: log}, nil
}

// Run 阻塞执行引擎；ctx 取消后等所有 worker 优雅返回再退出。
func (e *Engine) Run(ctx context.Context) {
	if len(e.bridges) == 0 {
		<-ctx.Done()
		return
	}
	var wg stdsync.WaitGroup
	wg.Add(len(e.bridges))
	for _, w := range e.bridges {
		w := w
		go func() {
			defer wg.Done()
			w.run(ctx)
		}()
	}
	wg.Wait()
	e.log.Info("所有 Bridge 已退出")
}

// bridgeWorker 是一个 Bridge 的运行实例。
//
// 字段在装配后只读；四个 goroutine 并行访问也无需加锁——store 内部
// 已经线程安全，httpClient 同理。
type bridgeWorker struct {
	cfg       config.Bridge
	intervals config.Intervals
	reporting config.Reporting

	adapter protocol.Adapter
	xboardC *xboard.Client
	xuiC    *xui.Client
	store   store.Store

	log *slog.Logger
}

// run 启动当前 worker 的四条同步循环并阻塞至 ctx 取消。
func (w *bridgeWorker) run(ctx context.Context) {
	w.log.Info("Bridge 启动",
		"xboard_node_id", w.cfg.XboardNodeID,
		"xui_inbound_id", w.cfg.XuiInboundID,
	)

	var wg stdsync.WaitGroup
	loops := []struct {
		name     string
		interval time.Duration
		fn       func(context.Context) error
		enabled  bool
	}{
		// 用户与流量循环始终启用——它们是中间件的核心职责。
		{name: "user_sync", interval: w.intervals.UserPullDuration(), fn: w.syncUsers, enabled: true},
		{name: "traffic_sync", interval: w.intervals.TrafficPushDuration(), fn: w.syncTraffic, enabled: true},
		// alive / status 是可选上报，按 reporting 配置开关。
		{name: "alive_sync", interval: w.intervals.AlivePushDuration(), fn: w.syncAlive, enabled: w.reporting.AliveEnabled},
		{name: "status_sync", interval: w.intervals.StatusPushDuration(), fn: w.syncStatus, enabled: w.reporting.StatusEnabled},
	}
	for _, l := range loops {
		if !l.enabled {
			w.log.Info("循环未启用，跳过", "loop", l.name)
			continue
		}
		l := l // 重新绑定，避免 goroutine 闭包陷阱
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.tickerLoop(ctx, l.name, l.interval, l.fn)
		}()
	}
	wg.Wait()
	w.log.Info("Bridge 已退出")
}

// tickerLoop 把"周期函数"包装为可被 ctx 中断的循环。
//
// 行为：
//
//	a) 启动后立即执行一次 fn（不等第一个 ticker），保证用户重启后立刻可观测；
//	b) 之后按 interval 触发；ctx 取消立即返回。
func (w *bridgeWorker) tickerLoop(ctx context.Context, name string, interval time.Duration, fn func(context.Context) error) {
	w.runStep(ctx, name, fn)

	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.runStep(ctx, name, fn)
		}
	}
}

// runStep 执行单次同步并把耗时 / 错误规范化打印。
//
// 不向上抛 error：上游不需要据此做决策；保留 panic 防御也不必要——standard
// http / sql 客户端在合规调用下不会 panic。
//
// trace_id 注入（v0.5.3 起）：每次进入 runStep 都生成一个新的短 trace_id,
// 通过 ctx 携带的 trace 化 logger 让本次 tick 的所有主入口日志（runStep
// 自身的"同步完成 / 中断 / 失败"+ fn 内部的细节日志）共享同一组 attrs：
// loop / trace_id。运维按 trace_id grep 即可把多 bridge × 4 loop 时段重叠
// 时本应同周期的多条日志串联起来。
func (w *bridgeWorker) runStep(ctx context.Context, name string, fn func(context.Context) error) {
	traceID := genTraceID()
	log := w.log.With("loop", name, "trace_id", traceID)
	ctx = contextWithLogger(ctx, log)

	start := time.Now()
	err := fn(ctx)
	elapsed := time.Since(start)
	switch {
	case err == nil:
		log.Debug("同步完成", "elapsed", elapsed)
	case ctx.Err() != nil:
		// 退出过程中触发的失败属于正常现象，按 info 级别记录即可。
		log.Info("同步因退出而中断", "elapsed", elapsed)
	default:
		log.Warn("同步失败", "elapsed", elapsed, "err", err)
	}
}
