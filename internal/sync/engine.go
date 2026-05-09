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
package sync

import (
	"context"
	"log/slog"
	stdsync "sync"
	"time"

	"github.com/xboard-bridge/xboard-xui-bridge/internal/config"
	"github.com/xboard-bridge/xboard-xui-bridge/internal/protocol"
	"github.com/xboard-bridge/xboard-xui-bridge/internal/store"
	"github.com/xboard-bridge/xboard-xui-bridge/internal/xboard"
	"github.com/xboard-bridge/xboard-xui-bridge/internal/xui"
)

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
func (w *bridgeWorker) runStep(ctx context.Context, name string, fn func(context.Context) error) {
	start := time.Now()
	err := fn(ctx)
	elapsed := time.Since(start)
	switch {
	case err == nil:
		w.log.Debug("同步完成", "loop", name, "elapsed", elapsed)
	case ctx.Err() != nil:
		// 退出过程中触发的失败属于正常现象，按 info 级别记录即可。
		w.log.Info("同步因退出而中断", "loop", name, "elapsed", elapsed)
	default:
		w.log.Warn("同步失败", "loop", name, "elapsed", elapsed, "err", err)
	}
}
