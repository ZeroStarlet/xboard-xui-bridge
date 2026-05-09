// Package main 是 xboard-xui-bridge 中间件的进程入口。
//
// 中间件解决的核心问题：
//
//	Xboard（Laravel 11 面板）希望节点端按 V2 协议（/api/v2/server/...）
//	拉取用户、上报流量；而 3x-ui（Go 面板）只通过 /panel/api/inbounds
//	暴露 inbound/client/流量数据。两者协议不通、字段命名各异、流量
//	统计周期不同，本中间件作为"翻译器 + 调度器"在中间运转。
//
// 进程职责（在不修改双方任何源码的前提下完成）：
//
//	1. 周期性从 Xboard 拉取该节点的"在用用户"清单（含 uuid、限速、设备数）。
//	2. 把用户以 client 形式 diff 同步进 3x-ui 已有 inbound（按 email 锚点）。
//	3. 周期性从 3x-ui 拉取每个 client 的累计流量，按差量上报回 Xboard。
//	4. 周期性把在线 IP（来源于 3x-ui clientIps）与节点状态上报回 Xboard。
//
// 高可靠遥测的关键约束：
//
//	进程崩溃 / 重启后，本地 SQLite 中的"已上报基线"
//	(email → last_reported_up/down) 必须保证流量既不丢失、也不重复计费。
//	因此差量计算流程严格分两步：先计算差量并尝试上报，仅当 HTTP 200 后
//	才推进基线（store.UpdateAfterReport）。失败则保留旧基线，下个周期重试。
//
// 命令行：
//
//	xboard-xui-bridge --config /etc/bridge/config.yaml
//	xboard-xui-bridge --version
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/xboard-bridge/xboard-xui-bridge/internal/config"
	"github.com/xboard-bridge/xboard-xui-bridge/internal/logger"
	"github.com/xboard-bridge/xboard-xui-bridge/internal/store"
	syncengine "github.com/xboard-bridge/xboard-xui-bridge/internal/sync"
	"github.com/xboard-bridge/xboard-xui-bridge/internal/xboard"
	"github.com/xboard-bridge/xboard-xui-bridge/internal/xui"
)

// version 由编译期 `-ldflags "-X main.version=x.y.z"` 注入。
// 未注入时回落到 "dev"，便于本地开发识别。
var version = "dev"

// main 不直接持有业务逻辑，只做"装配 + 信号联动"，保证测试时可以
// 绕过 main 单独构造组件。退出码语义：
//
//	0  正常退出（收到 SIGINT/SIGTERM）
//	1  运行期错误（保留供未来引擎主动退出使用）
//	2  启动期错误（配置 / 日志 / 持久层 / 客户端初始化失败）
func main() {
	configPath := flag.String("config", "config.yaml", "YAML 配置文件路径")
	showVersion := flag.Bool("version", false, "打印版本号并退出")
	flag.Parse()

	if *showVersion {
		fmt.Println("xboard-xui-bridge", version)
		return
	}

	cfg, err := config.LoadFromFile(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[fatal] 加载配置失败: %v\n", err)
		os.Exit(2)
	}

	log, closeLog, err := logger.New(cfg.Log)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[fatal] 初始化日志失败: %v\n", err)
		os.Exit(2)
	}
	defer closeLog()

	st, err := store.OpenSQLite(cfg.State.Database)
	if err != nil {
		log.Error("打开本地状态数据库失败", "path", cfg.State.Database, "err", err)
		os.Exit(2)
	}
	defer func() {
		if cerr := st.Close(); cerr != nil {
			log.Warn("关闭本地状态数据库出错", "err", cerr)
		}
	}()

	// 客户端构造。Xboard / 3x-ui 客户端均不在构造期发起网络请求；
	// 因此构造失败仅可能源于配置类错误（cookiejar 初始化等），属于不可恢复故障，进程退出。
	xboardC := xboard.New(cfg.Xboard)
	xuiC, err := xui.New(cfg.Xui)
	if err != nil {
		log.Error("初始化 3x-ui 客户端失败", "err", err)
		os.Exit(2)
	}

	engine, err := syncengine.New(cfg, log, xboardC, xuiC, st)
	if err != nil {
		log.Error("装配同步引擎失败", "err", err)
		os.Exit(2)
	}

	log.Info("xboard-xui-bridge 启动",
		"version", version,
		"config", *configPath,
		"bridges", len(cfg.Bridges),
		"state_db", cfg.State.Database,
	)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	engine.Run(ctx)

	log.Info("xboard-xui-bridge 已退出")
}
