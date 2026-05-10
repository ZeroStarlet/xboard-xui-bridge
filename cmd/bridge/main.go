// Package main 是 xboard-xui-bridge 中间件的进程入口。
//
// 中间件解决的核心问题：
//
//	Xboard（Laravel 11 面板）希望节点端按 V2 协议（/api/v2/server/...）
//	拉取用户、上报流量；而 3x-ui（Go 面板）只通过 /panel/api/inbounds
//	暴露 inbound/client/流量数据。两者协议不通、字段命名各异、流量
//	统计周期不同，本中间件作为"翻译器 + 调度器"在中间运转。
//
// 子命令路由：
//
//	xboard-xui-bridge                 默认（等价于 run）
//	xboard-xui-bridge run             启动后台守护 + Web 面板
//	xboard-xui-bridge reset-password  重置 admin 密码（本地 shell 兜底）
//	xboard-xui-bridge version         打印版本号
//	xboard-xui-bridge --version       同上（兼容 v0.1 风格）
//
// 退出码语义：
//
//	0  正常退出（收到 SIGINT/SIGTERM；或子命令成功完成）
//	1  运行期错误（子组件异常退出 / reset-password 失败）
//	2  启动期错误（持久层 / 配置 / 日志 / 客户端初始化失败）
//
// 高可靠遥测的关键约束（保持不变）：
//
//	进程崩溃 / 重启后，本地 SQLite 中的"已上报基线"
//	(email → last_reported_up/down) 必须保证流量既不丢失、也不重复计费。
//	因此差量计算流程严格分两步：先计算差量并尝试上报，仅当 HTTP 200 后
//	才推进基线（store.ApplyReportSuccess）。失败则保留旧基线，下个周期重试。
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/xboard-bridge/xboard-xui-bridge/internal/auth"
	"github.com/xboard-bridge/xboard-xui-bridge/internal/config"
	"github.com/xboard-bridge/xboard-xui-bridge/internal/logger"
	"github.com/xboard-bridge/xboard-xui-bridge/internal/store"
	"github.com/xboard-bridge/xboard-xui-bridge/internal/supervisor"
	"github.com/xboard-bridge/xboard-xui-bridge/internal/web"
)

// version 由编译期 `-ldflags "-X main.version=x.y.z"` 注入。
// 未注入时回落到 "dev"，便于本地开发识别。
var version = "dev"

// defaultDBPath 是 --db 未传入时的默认路径。
//
// 选择 ./data/bridge.db 是为了：
//
//	a) 与 Docker volume 惯例一致（容器内 /app/data 挂出宿主机数据目录）；
//	b) 与日志默认目录 ./data/logs/ 同根，运维只需挂载一个数据卷；
//	c) 工作目录相对路径让 systemd / 1Panel 等托管器更直观。
const defaultDBPath = "./data/bridge.db"

// main 仅做"子命令分发"——不直接持有业务逻辑。
//
// 子命令解析策略：把 os.Args[1] 与已知子命令字符串匹配；命中则把剩余
// 参数（os.Args[2:]）转给对应的 runXxx 函数。否则退化为默认 daemon
// 流程（让 `xboard-xui-bridge --db x.db` 这种 v0.1 风格的命令仍然可用）。
func main() {
	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "run":
			runDaemon(args[1:])
			return
		case "reset-password":
			runResetPassword(args[1:])
			return
		case "version", "--version", "-version":
			fmt.Println("xboard-xui-bridge", version)
			return
		case "help", "--help", "-h":
			printHelp()
			return
		}
	}
	// 没有匹配到子命令：默认 daemon 流程，args 全部交给 daemon flag 解析。
	runDaemon(args)
}

// printHelp 输出顶层 help。
//
// 极简实现：列出已知子命令与典型 flag。深入到子命令内部时由 daemon /
// reset-password 自身的 flag.FlagSet 输出 usage。
func printHelp() {
	const helpText = `xboard-xui-bridge — Xboard 与 3x-ui 的非侵入式中间件。

用法：
  xboard-xui-bridge [子命令] [参数...]

子命令：
  run                       启动后台守护 + Web 面板（默认）
  reset-password            重置 admin 密码
  version                   打印版本号
  help                      打印本帮助

常用参数（run 子命令）：
  --db <path>               SQLite 数据库路径（默认 ./data/bridge.db）
  --version                 打印版本号并退出

reset-password 参数：
  --db <path>               SQLite 数据库路径
  --user <username>         目标用户名（默认 admin）

例：
  xboard-xui-bridge --db ./data/bridge.db
  xboard-xui-bridge reset-password --user admin
`
	fmt.Print(helpText)
}

// runDaemon 是 v0.2 主流程：装配 store / config / logger / supervisor /
// auth / web 并阻塞至 SIGINT/SIGTERM。
//
// args 是已经去掉子命令名的剩余参数（用 flag.NewFlagSet 解析）。
func runDaemon(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	dbPath := fs.String("db", defaultDBPath, "SQLite 数据库路径（首次运行会自动创建父目录与表）")
	showVersion := fs.Bool("version", false, "打印版本号并退出")
	if err := fs.Parse(args); err != nil {
		// flag.ExitOnError 已经处理了 usage 打印；这里仅做防御式的兜底。
		os.Exit(2)
	}

	if *showVersion {
		fmt.Println("xboard-xui-bridge", version)
		return
	}

	// 启动顺序：
	//
	//	1. 打开 SQLite（必须最先；它是 settings 真相源）；
	//	2. 从 settings/bridges 表加载配置；
	//	3. 用配置初始化 logger；
	//	4. 装配 supervisor + auth + web；
	//	5. 并行运行同步引擎与 Web 面板，等 SIGINT/SIGTERM。
	//
	// 阶段 1/2 的失败只能写 stderr——logger 还没起来；阶段 3 之后用 logger。
	st, err := store.OpenSQLite(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[fatal] 打开 SQLite %q 失败: %v\n", *dbPath, err)
		os.Exit(2)
	}
	defer func() {
		if cerr := st.Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, "[warn] 关闭 SQLite 出错: %v\n", cerr)
		}
	}()

	// 加载配置使用 background ctx：启动期不参与信号取消，避免 ctrl+C
	// 在加载阶段触发模糊状态。
	cfg, err := config.LoadFromStore(context.Background(), st, *dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[fatal] 加载配置失败: %v\n", err)
		os.Exit(2)
	}

	// 环境变量覆盖：BRIDGE_LISTEN_ADDR 是给 Docker / systemd 等托管器的
	// "首次启动" 出口——容器默认通过 ENV BRIDGE_LISTEN_ADDR=:8787 让进程
	// 绑定全部网卡，外部 -p 8787:8787 才能可达。无需运维登入容器再修改
	// settings 表，避免"鸡生蛋"窘境。
	//
	// 设计取舍：环境变量仅覆盖 listen_addr 这一个启动期字段；其余字段
	// 仍走 settings 表 + Web GUI。如果未来要扩展更多 ENV 覆盖点，应新增
	// BRIDGE_XXX 风格变量，而不是引入复杂的优先级规则。
	if envAddr := strings.TrimSpace(os.Getenv("BRIDGE_LISTEN_ADDR")); envAddr != "" {
		cfg.Web.ListenAddr = envAddr
	}

	log, closeLog, err := logger.New(cfg.Log)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[fatal] 初始化日志失败: %v\n", err)
		os.Exit(2)
	}
	defer closeLog()

	// "首次启动半填" 友好提示：仅打 WARN，让运维一眼看到关键字段为空。
	// 凭据不全 + 有 enabled bridge 时的硬护栏在 supervisor.applyCredsGuard 实现。
	if cfg.Xboard.APIHost == "" || cfg.Xboard.Token == "" {
		log.Warn("Xboard 凭据未配置，请登录 Web 面板补齐后重启进程",
			"api_host_set", cfg.Xboard.APIHost != "",
			"token_set", cfg.Xboard.Token != "",
		)
	}
	// v0.6 起 xui 仅 Bearer API Token 单通道：必须 api_host + api_token 都
	// 填齐才视为完整。复用 config.Xui.CredsComplete 与 supervisor /
	// status_handler 共享同一份判定（不在此处手写 OR 表达式重蹈
	// "凭据完整性灯不一致" 覆辙）。
	if !cfg.Xui.CredsComplete() {
		log.Warn("3x-ui 凭据未配置，请登录 Web 面板补齐后重启进程",
			"api_host_set", cfg.Xui.APIHost != "",
			"api_token_set", cfg.Xui.APIToken != "",
		)
	}

	// 装配 supervisor。
	sup, err := supervisor.New(cfg, log, st)
	if err != nil {
		log.Error("装配 Supervisor 失败", "err", err)
		os.Exit(2)
	}

	// 装配 auth 服务并执行首次启动 admin 引导。
	//
	// 会话寿命取自 cfg.Web 的两个字段（已被 Validate 补默认）；
	// 修改这两个字段需要重启进程才能让 authSvc 看到新值——这与
	// 凭据修改一致，记录在 README 限制说明里。
	sessionMaxAge := time.Duration(cfg.Web.SessionMaxAgeHours) * time.Hour
	absoluteMaxAge := time.Duration(cfg.Web.AbsoluteMaxLifetimeHours) * time.Hour
	authSvc := auth.NewService(st, sessionMaxAge, absoluteMaxAge)

	// dataDir 是 dbPath 的父目录；首次密码导出文件、未来日志文件都放这里。
	dataDir := filepath.Dir(*dbPath)
	bootstrap, err := authSvc.EnsureBootstrapAdmin(context.Background(), dataDir)
	if err != nil {
		log.Error("初始化 admin 账户失败", "err", err)
		os.Exit(2)
	}
	auth.LogBootstrap(log, bootstrap)

	// 装配 Web 面板。
	webSrv, err := web.New(cfg, log, st, sup, authSvc)
	if err != nil {
		log.Error("装配 Web 面板失败", "err", err)
		os.Exit(2)
	}

	log.Info("xboard-xui-bridge 启动",
		"version", version,
		"db", *dbPath,
		"bridges", len(cfg.Bridges),
		"web", cfg.Web.ListenAddr,
	)

	// 信号联动：SIGINT/SIGTERM 触发 ctx.Done，supervisor 与 web 都监听。
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// 并行跑 supervisor + web。任一退出都 cancel 让另一个停。
	errCh := make(chan error, 2)
	go func() {
		errCh <- sup.Run(ctx)
	}()
	go func() {
		errCh <- webSrv.Run(ctx)
	}()

	firstErr := <-errCh
	cancel() // 让另一个尽快退出
	secondErr := <-errCh

	if firstErr != nil {
		log.Error("子组件退出（first）", "err", firstErr)
	}
	if secondErr != nil {
		log.Error("子组件退出（second）", "err", secondErr)
	}
	if firstErr != nil || secondErr != nil {
		os.Exit(1)
	}

	log.Info("xboard-xui-bridge 已退出")
}

// runResetPassword 处理 `reset-password` 子命令。
//
// 用途：当运维忘记 Web 面板密码时通过本地 shell 重置——这是最后的兜底，
// 因为面板自身要 admin 登录后才能改密。
//
// 流程：
//
//	1. 解析 --db / --user 参数；
//	2. 打开 SQLite；
//	3. 按 username 查找用户；
//	4. 交互式提示输入新密码（隐藏明文输入，确认两次）；
//	5. 调 auth.Service.ResetPassword（事务保证：改密 + 删全部 session）；
//	6. 打印成功提示并退出。
//
// 安全考虑：本子命令没有"二次鉴权"——一旦能在本地 shell 跑就视作受信任。
// 这与传统 Linux 服务（如 redis-cli AUTH 重置）一致，是合理的"信任本地"
// 设计。HTTP 层不暴露此路径（auth.Service.ResetPassword 不被 web handler
// 调用）。
func runResetPassword(args []string) {
	fs := flag.NewFlagSet("reset-password", flag.ExitOnError)
	dbPath := fs.String("db", defaultDBPath, "SQLite 数据库路径")
	username := fs.String("user", "admin", "目标用户名")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}

	st, err := store.OpenSQLite(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[fatal] 打开 SQLite %q 失败: %v\n", *dbPath, err)
		os.Exit(2)
	}
	defer func() { _ = st.Close() }()

	user, err := st.GetAdminUserByUsername(context.Background(), *username)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			fmt.Fprintf(os.Stderr, "[fatal] 用户 %q 不存在\n", *username)
		} else {
			fmt.Fprintf(os.Stderr, "[fatal] 查询用户失败: %v\n", err)
		}
		os.Exit(1)
	}

	// 交互输入：两次密码必须一致。
	pwd, err := readPasswordTwice("请输入用户 " + *username + " 的新密码（不少于 8 位）:")
	if err != nil {
		fmt.Fprintf(os.Stderr, "[fatal] 读取密码失败: %v\n", err)
		os.Exit(1)
	}

	// 用与 daemon 相同的 sessionMaxAge / absoluteMaxAge 默认即可——
	// reset-password 不会启动 web，本字段的值不影响行为，仅为通过 NewService
	// 的非 nil 校验。
	authSvc := auth.NewService(st, time.Hour, 24*time.Hour)
	if err := authSvc.ResetPassword(context.Background(), user.ID, pwd); err != nil {
		if errors.Is(err, auth.ErrPasswordTooShort) {
			fmt.Fprintln(os.Stderr, "[fatal] 新密码长度不足 8 位")
		} else {
			fmt.Fprintf(os.Stderr, "[fatal] 重置密码失败: %v\n", err)
		}
		os.Exit(1)
	}

	fmt.Printf("用户 %q 的密码已重置；该用户所有会话已被强制下线。\n", *username)
}

// readPasswordTwice 提示两次密码并比对一致；不一致重试至最多 3 次。
//
// 输入隐藏：用 golang.org/x/term.ReadPassword（兼容 Linux/macOS/Windows）。
// 非 TTY 场景下（如 docker exec 没有 -it 或 `printf "x\nx\n" | bridge ...`
// 的管道）会退化为普通行读取——这种情形下密码会回显，但能可靠按行
// 切割两次输入，方便自动化脚本测试。
//
// 共享 *bufio.Reader：非 TTY 路径下两次 readPasswordOnce 必须从同一个
// 缓冲流读取，否则首次 Read 可能一次性吞下两行（例如 stdin 是已经写满
// 的管道），让第二次 prompt 拿到 EOF 或错位字符串。bufio.Reader 在两次
// ReadString('\n') 之间维持读取位置，行切割语义稳定。
func readPasswordTwice(prompt string) (string, error) {
	const maxAttempts = 3
	// 仅在非 TTY 时分配 bufio.Reader——TTY 路径走 term.ReadPassword，
	// 后者直接读取 fd，不与 stdin 缓冲流共享状态。
	var stdinReader *bufio.Reader
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		stdinReader = bufio.NewReader(os.Stdin)
	}
	for i := 0; i < maxAttempts; i++ {
		fmt.Println(prompt)
		first, err := readPasswordOnce(stdinReader)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(first) == "" {
			fmt.Fprintln(os.Stderr, "密码不可为空。")
			continue
		}
		fmt.Println("再次输入新密码以确认:")
		second, err := readPasswordOnce(stdinReader)
		if err != nil {
			return "", err
		}
		if first != second {
			fmt.Fprintln(os.Stderr, "两次输入不一致，请重新输入。")
			continue
		}
		return first, nil
	}
	return "", errors.New("密码确认失败次数过多")
}

// readPasswordOnce 从标准输入读取一次密码。
//
// 路由策略：
//
//	a) TTY → term.ReadPassword：隐藏字符回显；reader 参数被忽略；
//	b) 非 TTY → bufio.Reader.ReadString('\n')：按行切割，确保多行管道
//	   输入能被两次调用按序消费（修复 P2 输入串扰问题）。
//
// 调用方约定：非 TTY 时必须传入持久化 *bufio.Reader（同一个 reader 在
// 两次调用间共享）；TTY 时可传 nil。readPasswordTwice 已经按规则装配。
func readPasswordOnce(reader *bufio.Reader) (string, error) {
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		buf, err := term.ReadPassword(fd)
		if err != nil {
			return "", fmt.Errorf("读取密码：%w", err)
		}
		// term.ReadPassword 不会自动换行；为视觉清晰显式打一行。
		fmt.Println()
		return string(buf), nil
	}
	if reader == nil {
		// 防御：调用方应当在非 TTY 路径传入 reader。
		return "", errors.New("readPasswordOnce: 非 TTY 路径必须传入 *bufio.Reader")
	}
	line, err := reader.ReadString('\n')
	if err != nil {
		// io.EOF：管道已关闭或数据已读完——视作"输入结束"，让上层报错。
		if errors.Is(err, io.EOF) && line == "" {
			return "", fmt.Errorf("读取 stdin：%w", err)
		}
		// 其它错误一律视为终止。注意：ReadString 在最后一行无 '\n' 但有
		// 内容时会返回 (line, io.EOF)；line 已经包含数据，可继续返回。
		if !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("读取 stdin：%w", err)
		}
	}
	return strings.TrimRight(line, "\r\n"), nil
}
