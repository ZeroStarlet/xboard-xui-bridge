// Package config 定义 xboard-xui-bridge 的配置模型与加载流程。
//
// 设计原则（v0.2 起，"零 yaml" 阶段）：
//
//  1. 配置真相源是 SQLite 中的 settings / bridges 两张表（详见
//     internal/store/schema.go）；不再读取任何 YAML 文件——v0.1 时代的
//     config.yaml 完全废弃。
//  2. 进程启动时一次性从 store 加载并完成校验；运行期间通过
//     internal/supervisor 协调"全停全启"式重载，避免"部分组件已切换到新
//     配置、其他组件仍在旧配置"导致的状态不一致。
//  3. 默认值在 Validate 中显式补齐——绝不依赖结构体零值表达"未指定"语义，
//     因为零值与"显式写 0"无法区分（如 Interval=0 不能默认 60s）。
//  4. "首次启动 / 半填" 友好：xboard / xui 关键字段允许为空，引擎空载运行
//     等待 GUI 填写完成后调用 Supervisor.Reload；非空字段仍按原规则严格
//     校验格式（避免"填错一半"被静默接受）。
//  5. 任何新增配置项都必须同时：a) 声明 SettingXxx 常量；b) 在
//     LoadFromStore 中映射；c) 在 Validate 中校验或补默认；d) 在 Web
//     Handler 表单层补上"必填"校验。四处缺一会导致用户配置静默失效，
//     违反"显式优于隐式"的项目铁律。
//
// 不在本包做的事：
//
//	不创建数据库、不打开网络连接，仅做"从 store 读取 → 解码 → 校验 → 返回"
//	纯计算流程。这样测试只需构造内存 store 即可覆盖所有分支。
package config

import (
	"context"
	"encoding/base32"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/xboard-bridge/xboard-xui-bridge/internal/store"
)

// 协议常量。中间件内部一律使用这套小写字符串作为协议标识，避免大小写歧义。
// Hysteria 单独区分 v1 / v2，因为 3x-ui 的 inbound 协议字段对它们也是分开的
// （hysteria / hysteria2），Xboard 端则在 protocol_settings.version 中用 1 / 2 区分。
const (
	ProtocolShadowsocks = "shadowsocks"
	ProtocolVMess       = "vmess"
	ProtocolVLESS       = "vless"
	ProtocolTrojan      = "trojan"
	ProtocolHysteria    = "hysteria"  // Hysteria v1
	ProtocolHysteria2   = "hysteria2" // Hysteria v2
)

// allowedProtocols 列出本中间件已实现适配器的协议清单。
// 任何配置中出现该集合之外的协议名都应当被 Validate 拦截。
var allowedProtocols = map[string]struct{}{
	ProtocolShadowsocks: {},
	ProtocolVMess:       {},
	ProtocolVLESS:       {},
	ProtocolTrojan:      {},
	ProtocolHysteria:    {},
	ProtocolHysteria2:   {},
}

// 3x-ui 鉴权说明：v0.4 起统一为 cookie 登录模式（用户名 + 密码 + 可选 TOTP）。
//
// 背景演进：
//
//	v0.2  仅支持 Bearer Token——主线 3x-ui v2.4+ 在面板设置中可生成 48 字符
//	      API Token，中间件注入 Authorization 头即可。
//	v0.3  增设 cookie 模式覆盖 fork 版本不实现 apiToken 中间件的部署
//	      （`strings $(which x-ui) | grep -ic apitoken == 0`）。
//	v0.4  彻底砍掉 token 模式：主线与 fork 在浏览器后台都同等支持
//	      `/login` 表单 + session cookie 路径，统一为唯一鉴权方式让代码体积、
//	      Web 表单分支、运维心智三方面都简化。Bearer Token 不再支持——
//	      旧 v0.2/v0.3 token 用户升级 v0.4 后必须重新填写 3x-ui 后台用户名
//	      / 密码（settings 表里残留的 xui.api_token / xui.auth_mode 行无害
//	      保留但被 LoadFromStore 忽略；如需清理可手工 sqlite3 删除）。
//
// 鉴权流程（详见 internal/xui/client.go）：
//
//	1. 首次业务调用前 POST /login 拿 session cookie 存进 cookiejar；
//	2. 后续 panel API 调用由 net/http 自动带 cookie；
//	3. 遇到失效信号（401 / 302 / HTML 登录页 / success=false 含登录关键字）
//	   自动重登录并重试一次；仍失败即返回 *Error 让上层 sync 循环走 WARN
//	   路径，下个周期再试。
//	4. 若 3x-ui 后台启用了 TOTP 2FA（v2.4+ 主线 LoginForm.twoFactorCode
//	   字段），填 xui.totp_secret（base32），中间件按 RFC 6238 算 6 位码
//	   塞 twoFactorCode；未启用 2FA 时该字段留空。

// settings 表 key 常量。
//
// 命名约定 "<group>.<field>"，与 v0.1 时代的 yaml 字段名一一对应；这样
// 阅读旧文档与新数据库时心智映射零成本。集中声明的目的是避免散落字符串
// 拼写漂移——M2 / M5 / M7 三处都引用这些常量，缺一处都可能让用户写入的
// 字段被误读成 "未设置"。
const (
	// log 组
	SettingLogLevel      = "log.level"
	SettingLogFile       = "log.file"
	SettingLogMaxSizeMB  = "log.max_size_mb"
	SettingLogMaxBackups = "log.max_backups"
	SettingLogMaxAgeDays = "log.max_age_days"

	// xboard 组
	SettingXboardAPIHost       = "xboard.api_host"
	SettingXboardToken         = "xboard.token"
	SettingXboardTimeoutSec    = "xboard.timeout_sec"
	SettingXboardSkipTLSVerify = "xboard.skip_tls_verify"
	SettingXboardUserAgent     = "xboard.user_agent"

	// xui 组（v0.4 起仅 cookie 模式；token 相关 SettingXuiAuthMode /
	// SettingXuiAPIToken 已废弃并从代码中移除——旧 settings 表里残留的
	// 这两行 KV 无害保留但被 LoadFromStore 忽略）。
	SettingXuiAPIHost  = "xui.api_host"
	SettingXuiBasePath = "xui.base_path"
	// Username / Password 必须同时填写或同时留空——半填会被 Validate 拒绝，
	// 防止"只配了一半凭据"导致中间件持续 login 失败却看似在跑。
	SettingXuiUsername = "xui.username"
	SettingXuiPassword = "xui.password"
	// TOTPSecret 始终可选：仅在 3x-ui 后台启用了 TOTP 2FA 时填写。
	// 接受大小写不敏感的 base32 字符串（带或不带 = padding 都可）。
	SettingXuiTOTPSecret    = "xui.totp_secret"
	SettingXuiTimeoutSec    = "xui.timeout_sec"
	SettingXuiSkipTLSVerify = "xui.skip_tls_verify"

	// intervals 组
	SettingIntervalsUserPullSec    = "intervals.user_pull_sec"
	SettingIntervalsTrafficPushSec = "intervals.traffic_push_sec"
	SettingIntervalsAlivePushSec   = "intervals.alive_push_sec"
	SettingIntervalsStatusPushSec  = "intervals.status_push_sec"

	// reporting 组
	SettingReportingAliveEnabled  = "reporting.alive_enabled"
	SettingReportingStatusEnabled = "reporting.status_enabled"

	// web 组
	SettingWebListenAddr               = "web.listen_addr"
	SettingWebSessionMaxAgeHours       = "web.session_max_age_hours"
	SettingWebAbsoluteMaxLifetimeHours = "web.absolute_max_lifetime_hours"
)

// 默认值常量。值的选择动机详见 Validate 中各分支注释。
const (
	defaultLogLevel             = "info"
	defaultXboardTimeoutSec     = 15
	defaultXboardUserAgent      = "xboard-xui-bridge"
	defaultXuiTimeoutSec        = 15
	defaultIntervalSec          = 60 // 与 Xboard 推荐周期一致
	minIntervalSec              = 5  // 防止配置错把面板打挂
	defaultWebListenAddr        = "127.0.0.1:8787"
	defaultWebSessionMaxHours   = 24 * 7  // 7 天滑动窗口
	defaultWebAbsoluteMaxHours  = 24 * 30 // 30 天绝对上限
)

// Root 是配置文件的根节点。
//
// 嵌套结构而不是平铺：因为各部分互不耦合，嵌套能让每个组件只接收自己关心
// 的子配置（如 logger.New(cfg.Log)），保持单一职责。
type Root struct {
	Log       Log
	State     State
	Xboard    Xboard
	Xui       Xui
	Bridges   []Bridge
	Intervals Intervals
	Reporting Reporting
	Web       Web
}

// Log 描述日志输出策略。
type Log struct {
	// Level：debug / info / warn / error。大小写不敏感，规范化在 Validate 完成。
	Level string
	// File：写入路径；空字符串表示 stdout。文件不存在时 logger 会自动创建。
	File string
	// MaxSizeMB：单文件滚动阈值（MB），仅在 File 非空时生效。0 视为不滚动。
	MaxSizeMB int
	// MaxBackups：保留历史日志份数，超过即清理最旧的。
	MaxBackups int
	// MaxAgeDays：历史日志最大保留天数，超过即清理。
	MaxAgeDays int
}

// State 描述本地状态持久层（流量基线、settings、bridges、admin、sessions）。
//
// 注意：Database 字段不通过 settings 表传递——它是打开 SQLite 时就需要
// 的"鸡生蛋"前置参数，由 main.go 通过命令行 flag / 环境变量决定后传给
// store.OpenSQLite，并在 LoadFromStore 时回填到本字段，便于其它组件
// 通过统一接口取得。
type State struct {
	Database string
}

// Xboard 描述对 Xboard 节点 API 的访问参数。
type Xboard struct {
	// APIHost：Xboard 面板入口（含协议头），例如 https://panel.example.com，结尾不必带 /。
	APIHost string
	// Token：admin_setting('server_token')，所有 V2 节点 API 鉴权使用同一个 token。
	Token string
	// TimeoutSec：单次 HTTP 请求超时秒数，包括连接 + 读取 + 关闭。
	TimeoutSec int
	// SkipTLSVerify：仅自签证书的内网部署可设 true；公网部署严禁开启。
	SkipTLSVerify bool
	// UserAgent：请求头标识，默认 xboard-xui-bridge。便于 Xboard 侧访问日志识别。
	UserAgent string
}

// Xui 描述对 3x-ui 面板 API 的访问参数。
//
// v0.4 起仅支持 cookie 登录模式：中间件 POST /login 拿 session cookie，
// 后续 panel API 调用由 net/http cookiejar 自动带 cookie。Bearer Token
// 模式（v0.2/v0.3）已彻底移除——详见文件顶部"3x-ui 鉴权说明"注释块。
type Xui struct {
	// APIHost：3x-ui 面板根 URL（含协议头），例如 http://127.0.0.1:2053。
	APIHost string
	// BasePath：3x-ui 的 webBasePath 设置；空串代表 /。
	// 中间件会在 APIHost 之后、/panel/api/... 之前拼接它。
	BasePath string
	// Username：3x-ui 后台登录用户名。
	// 必须与 Password 同时填写或同时留空（半填触发 Validate 报错）；
	// 全空允许 = 首次启动空载等待 Web 面板填配置。
	Username string
	// Password：3x-ui 后台登录密码。
	// 明文存于 settings 表，admin 鉴权后可通过 Web 面板查看——
	// 运维需保护好 bridge.db（默认 0600）。
	Password string
	// TOTPSecret：3x-ui 后台账户绑定的 TOTP base32 secret；仅在 3x-ui
	// 启用了 2FA 时填写，留空表示未启用 2FA（默认情形）。
	// 中间件每次 /login 时按 RFC 6238 算当前 6 位码塞进 twoFactorCode 字段。
	TOTPSecret string
	// TimeoutSec：单次 HTTP 请求超时秒数。
	TimeoutSec int
	// SkipTLSVerify：3x-ui 多数部署在 127.0.0.1，但少数会暴露到公网，
	// 内网用自签可放行，公网严禁。
	SkipTLSVerify bool
}

// Bridge 描述一对 (Xboard 节点, 3x-ui inbound) 的映射关系。
//
// 一个中间件实例可承载多个 Bridge：每个 Bridge 在引擎中独立运行四个同步循环，
// 互不干扰。Bridge 之间共享同一份 Xboard / Xui 客户端实例（连接复用）。
type Bridge struct {
	// Name：人类可读名称，用于日志区分；同一配置内必须唯一。
	Name string
	// XboardNodeID：在 Xboard 后台为该节点分配的数字 ID。
	XboardNodeID int
	// XboardNodeType：Xboard 端节点类型，用于 V2 API 的 node_type 参数。
	// 注意：当 Xboard 节点类型是 hysteria 时，本字段填 hysteria；
	// 至于实际是 v1 还是 v2，由 Protocol 字段决定（业务语义解耦）。
	XboardNodeType string
	// XuiInboundID：在 3x-ui 侧已经创建好的 inbound 数字 ID（管理员预创建）。
	XuiInboundID int
	// Protocol：见 ProtocolXxx 常量；决定协议适配器选择。
	Protocol string
	// Flow：仅 VLESS 协议生效，常见取值 xtls-rprx-vision；其他协议忽略。
	Flow string
	// Enable：false 时该 Bridge 不参与同步循环（用于灰度 / 临时停用）。
	Enable bool
}

// Intervals 描述四个同步循环的执行间隔（秒）。
//
// 默认全部 60s 与 Xboard 推荐一致；调高会降低面板压力但延长响应延迟，
// 调低则相反。所有 Bridge 共享同一组间隔，避免运维心智负担。
type Intervals struct {
	UserPullSec    int
	TrafficPushSec int
	AlivePushSec   int
	StatusPushSec  int
}

// Reporting 描述上报开关。
//
// 设计澄清：中间件不再提供本地"流量倍率"配置。
//
//	早期版本暴露过 TrafficRate 字段，让运维做"流量打折 / 翻倍"。但 Xboard 端
//	`TrafficFetchJob` 已经按 `v2_server.rate` 节点倍率自动乘算（见 Xboard
//	`app/Jobs/TrafficFetchJob.php:40-41`）；中间件再叠一层 rate 会双重乘，
//	且非整数 rate 在中间件做 floor 会随时间产生小数累积偏差。
//	因此倍率统一在 Xboard 后台节点配置中维护，中间件只上报 raw delta。
type Reporting struct {
	// AliveEnabled：是否上报在线 IP（用于 Xboard 设备数限制）。
	AliveEnabled bool
	// StatusEnabled：是否上报节点 CPU / 内存 / 磁盘负载。
	StatusEnabled bool
}

// Web 描述 Web 面板自身的运行参数。
//
// 注：v0.2 起新增；v0.1 没有 Web 面板时无该字段。
type Web struct {
	// ListenAddr：HTTP 监听地址，形如 "127.0.0.1:8787"（默认）或 ":8787"
	// （绑定全部网卡）。后者仅推荐在配合反向代理 + TLS 时使用。
	ListenAddr string
	// SessionMaxAgeHours：滑动续期窗口（小时）。每次有效请求把过期时间推到
	// "now + SessionMaxAgeHours"。默认 168（7 天）。
	SessionMaxAgeHours int
	// AbsoluteMaxLifetimeHours：会话从 created_at 起的绝对寿命上限（小时）。
	// 默认 720（30 天）。即使滑动续期一直命中，token 总寿命也不会超过该值。
	AbsoluteMaxLifetimeHours int
}

// LoadFromStore 从 SQLite 持久层加载完整配置。
//
// 调用前必须先 store.OpenSQLite（schema 已就绪）。dbPath 是调用方在打开
// SQLite 时已经持有的路径，本函数把它回填到 Root.State.Database，便于
// 其它组件按统一接口取数据库路径。
//
// 失败原因：
//
//	a) ListSettings / ListBridges 返回数据库错误；
//	b) Validate 在补默认值后仍发现"非法格式"——如 protocol 取值非法、
//	   interval 低于 minIntervalSec、bridges 内重复 name 等。
//
// "首次启动" 场景：settings 与 bridges 都为空时仍能成功返回——这是有意
// 设计，让首次部署的运维即使没填任何字段也能进入 Web 面板配置。校验
// 仅确保 dbPath 非空（"鸡生蛋"必填项）。
func LoadFromStore(ctx context.Context, st store.Store, dbPath string) (*Root, error) {
	if st == nil {
		return nil, errors.New("LoadFromStore: store 不可为 nil")
	}
	settings, err := st.ListSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("加载 settings：%w", err)
	}
	bridgeRows, err := st.ListBridges(ctx)
	if err != nil {
		return nil, fmt.Errorf("加载 bridges：%w", err)
	}

	root := &Root{
		Log: Log{
			Level:      settings[SettingLogLevel],
			File:       settings[SettingLogFile],
			MaxSizeMB:  parseSettingInt(settings, SettingLogMaxSizeMB, 0),
			MaxBackups: parseSettingInt(settings, SettingLogMaxBackups, 0),
			MaxAgeDays: parseSettingInt(settings, SettingLogMaxAgeDays, 0),
		},
		State: State{Database: dbPath},
		Xboard: Xboard{
			APIHost:       settings[SettingXboardAPIHost],
			Token:         settings[SettingXboardToken],
			TimeoutSec:    parseSettingInt(settings, SettingXboardTimeoutSec, 0),
			SkipTLSVerify: parseSettingBool(settings, SettingXboardSkipTLSVerify, false),
			UserAgent:     settings[SettingXboardUserAgent],
		},
		Xui: Xui{
			// 注：旧 v0.2/v0.3 settings 表里残留的 xui.auth_mode /
			// xui.api_token 行此处不再读——v0.4 已彻底移除 token 模式；
			// 残留 KV 无害保留但被 LoadFromStore 忽略。运维若想清理可
			// 手工执行 sqlite3 ./data/bridge.db "DELETE FROM settings
			// WHERE key IN ('xui.auth_mode','xui.api_token');"
			APIHost:       settings[SettingXuiAPIHost],
			BasePath:      settings[SettingXuiBasePath],
			Username:      settings[SettingXuiUsername],
			Password:      settings[SettingXuiPassword],
			TOTPSecret:    settings[SettingXuiTOTPSecret],
			TimeoutSec:    parseSettingInt(settings, SettingXuiTimeoutSec, 0),
			SkipTLSVerify: parseSettingBool(settings, SettingXuiSkipTLSVerify, false),
		},
		Intervals: Intervals{
			UserPullSec:    parseSettingInt(settings, SettingIntervalsUserPullSec, 0),
			TrafficPushSec: parseSettingInt(settings, SettingIntervalsTrafficPushSec, 0),
			AlivePushSec:   parseSettingInt(settings, SettingIntervalsAlivePushSec, 0),
			StatusPushSec:  parseSettingInt(settings, SettingIntervalsStatusPushSec, 0),
		},
		Reporting: Reporting{
			AliveEnabled:  parseSettingBool(settings, SettingReportingAliveEnabled, false),
			StatusEnabled: parseSettingBool(settings, SettingReportingStatusEnabled, false),
		},
		Web: Web{
			ListenAddr:               settings[SettingWebListenAddr],
			SessionMaxAgeHours:       parseSettingInt(settings, SettingWebSessionMaxAgeHours, 0),
			AbsoluteMaxLifetimeHours: parseSettingInt(settings, SettingWebAbsoluteMaxLifetimeHours, 0),
		},
	}

	// bridges 行表 → []Bridge：纯字段转换。Validate 后续会再做规范化（lower / trim 等）。
	root.Bridges = make([]Bridge, 0, len(bridgeRows))
	for _, br := range bridgeRows {
		root.Bridges = append(root.Bridges, Bridge{
			Name:           br.Name,
			XboardNodeID:   br.XboardNodeID,
			XboardNodeType: br.XboardNodeType,
			XuiInboundID:   br.XuiInboundID,
			Protocol:       br.Protocol,
			Flow:           br.Flow,
			Enable:         br.Enable,
		})
	}

	if err := root.Validate(); err != nil {
		return nil, fmt.Errorf("校验配置：%w", err)
	}
	return root, nil
}

// Validate 同时承担"校验非法值"与"补齐默认值"两个职责。
//
// 把它们放在同一函数：因为补齐默认值后还需要再次校验（例如默认间隔 60s 之后
// 仍要确认其为正数），分两个函数会重复检查反而增加心智负担。
//
// 任何被修改字段都通过指针接收者写回 r，确保后续组件读取到的是已规范化的值。
//
// 与 v0.1 yaml 时代的语义放宽点：
//
//   - bridges 数量允许为 0：首次启动 / GUI 还未配置时合法；引擎装配出空集合
//     并阻塞等待 ctx，Supervisor.Reload 收到新 bridges 后再装配 worker。
//   - 至少 1 项 enabled 不再强制：运维可临时禁用所有桥接做维护。
//   - xboard.api_host / xboard.token / xui.api_host / xui.username / xui.password
//     允许为空：首次启动 GUI 还未填写时合法；非空时仍按原规则校验格式。
//     xui.username 与 xui.password 必须同时填写或同时留空（半填触发报错），
//     防止"只配了一半凭据"导致中间件持续登录失败。
//
// Web Handler 表单提交路径会做更严格的"必填"校验，这与本函数的宽松校验
// 互补——前者让用户立刻知道半填错误，后者让运维重启后即使配置不完整也
// 能进入 Web 面板修复。
func (r *Root) Validate() error {
	// ---------------- log ----------------
	switch strings.ToLower(strings.TrimSpace(r.Log.Level)) {
	case "":
		r.Log.Level = defaultLogLevel
	case "debug", "info", "warn", "error":
		r.Log.Level = strings.ToLower(r.Log.Level)
	default:
		return fmt.Errorf("log.level 取值非法：%q（支持 debug / info / warn / error）", r.Log.Level)
	}
	if r.Log.MaxSizeMB < 0 || r.Log.MaxBackups < 0 || r.Log.MaxAgeDays < 0 {
		return errors.New("log.max_size_mb / max_backups / max_age_days 不可为负")
	}

	// ---------------- state ----------------
	if strings.TrimSpace(r.State.Database) == "" {
		return errors.New("state.database 必须由调用方在打开 SQLite 时传入（默认 ./data/bridge.db）")
	}

	// ---------------- xboard ----------------
	// 空字符串允许：首次启动场景。非空时仍按原规则严格校验。
	r.Xboard.APIHost = strings.TrimSpace(r.Xboard.APIHost)
	if r.Xboard.APIHost != "" {
		if err := validateHTTPHost("xboard.api_host", r.Xboard.APIHost); err != nil {
			return err
		}
		r.Xboard.APIHost = strings.TrimRight(r.Xboard.APIHost, "/")
	}
	r.Xboard.Token = strings.TrimSpace(r.Xboard.Token)
	if r.Xboard.TimeoutSec <= 0 {
		r.Xboard.TimeoutSec = defaultXboardTimeoutSec
	}
	if strings.TrimSpace(r.Xboard.UserAgent) == "" {
		r.Xboard.UserAgent = defaultXboardUserAgent
	}

	// ---------------- xui ----------------
	r.Xui.APIHost = strings.TrimSpace(r.Xui.APIHost)
	if r.Xui.APIHost != "" {
		if err := validateHTTPHost("xui.api_host", r.Xui.APIHost); err != nil {
			return err
		}
		r.Xui.APIHost = strings.TrimRight(r.Xui.APIHost, "/")
	}
	r.Xui.BasePath = normalizeBasePath(r.Xui.BasePath)

	// 所有凭据字段统一 trim：保留 trim 后的值供持久层回写时不带空格脏数据。
	r.Xui.Username = strings.TrimSpace(r.Xui.Username)
	r.Xui.Password = strings.TrimSpace(r.Xui.Password)
	r.Xui.TOTPSecret = strings.TrimSpace(r.Xui.TOTPSecret)

	// username + password 必须"同时填写"或"同时留空"。半填（仅 username
	// 或仅 password）会让中间件持续登录失败却看似在跑——必须 fail-fast
	// 拦截，比静默写库后才在运行期 WARN 友好得多。
	// 全空允许 = 首次启动空载等待 Web 面板填配置；全填 = 完整凭据。
	if (r.Xui.Username == "") != (r.Xui.Password == "") {
		return errors.New("xui.username 与 xui.password 必须同时填写或同时留空（半填会让中间件无法登录 3x-ui）")
	}
	// TOTPSecret 始终可选（多数部署不开 2FA）；非空时仅做 base32 合法性
	// 轻量校验，防止运维把 hex / 普通密码误塞到这里。Google Authenticator
	// 给的 secret 通常无 = padding，所以走 NoPadding 解码并先剥末尾 =。
	// ToUpper 是因为 base32 标准编码全大写，但用户从 QR 码下方文本复制时
	// 可能是小写。
	if r.Xui.TOTPSecret != "" {
		cleaned := strings.ToUpper(strings.TrimRight(r.Xui.TOTPSecret, "="))
		// 边界保护：仅含 = 填充符（如 "===="）的输入剥完会变空串，
		// 而 base32.DecodeString("") 会静默成功——但这显然是误填。
		// 这里 fail-fast 给出清晰错误，避免运行期才报"算不出 TOTP"。
		if cleaned == "" {
			return errors.New("xui.totp_secret 仅含 = 填充符，不是合法的 base32 secret（请填账户绑定的真实 secret）")
		}
		if _, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(cleaned); err != nil {
			return fmt.Errorf("xui.totp_secret 不是合法的 base32 字符串（请填账户绑定的 base32 secret）：%w", err)
		}
	}
	if r.Xui.TimeoutSec <= 0 {
		r.Xui.TimeoutSec = defaultXuiTimeoutSec
	}

	// ---------------- bridges ----------------
	// 不再强制"至少 1 项"或"至少 1 项 enabled"。每条 bridge 自身字段仍严格校验，
	// 防止存了一行半填数据进数据库。
	seenName := make(map[string]struct{}, len(r.Bridges))
	for i := range r.Bridges {
		b := &r.Bridges[i]
		// 用指针写回是为了让后续业务代码读取到规范化（trim、小写）后的值。
		b.Name = strings.TrimSpace(b.Name)
		if b.Name == "" {
			return fmt.Errorf("bridges[%d].name 不可为空", i)
		}
		if _, dup := seenName[b.Name]; dup {
			return fmt.Errorf("bridges[%d].name 重复：%q", i, b.Name)
		}
		seenName[b.Name] = struct{}{}

		if b.XboardNodeID <= 0 {
			return fmt.Errorf("bridges[%s].xboard_node_id 必须为正整数", b.Name)
		}
		if b.XuiInboundID <= 0 {
			return fmt.Errorf("bridges[%s].xui_inbound_id 必须为正整数", b.Name)
		}

		b.Protocol = strings.ToLower(strings.TrimSpace(b.Protocol))
		if _, ok := allowedProtocols[b.Protocol]; !ok {
			return fmt.Errorf("bridges[%s].protocol 取值非法：%q（支持 %s）",
				b.Name, b.Protocol, strings.Join(sortedKeys(allowedProtocols), " / "))
		}

		// XboardNodeType 默认与 Protocol 一致；显式指定时尊重用户值。
		// Hysteria v1/v2 在 Xboard 端的 node_type 都是 "hysteria"。
		b.XboardNodeType = strings.ToLower(strings.TrimSpace(b.XboardNodeType))
		if b.XboardNodeType == "" {
			if b.Protocol == ProtocolHysteria2 {
				b.XboardNodeType = ProtocolHysteria
			} else {
				b.XboardNodeType = b.Protocol
			}
		}

		// flow 仅对 vless 有意义；其它协议若误填则置空，避免误用。
		if b.Protocol != ProtocolVLESS {
			b.Flow = ""
		} else {
			b.Flow = strings.TrimSpace(b.Flow)
		}
	}

	// ---------------- intervals（默认 60s，最低 minIntervalSec 防止把面板打挂） ----------------
	if r.Intervals.UserPullSec <= 0 {
		r.Intervals.UserPullSec = defaultIntervalSec
	}
	if r.Intervals.TrafficPushSec <= 0 {
		r.Intervals.TrafficPushSec = defaultIntervalSec
	}
	if r.Intervals.AlivePushSec <= 0 {
		r.Intervals.AlivePushSec = defaultIntervalSec
	}
	if r.Intervals.StatusPushSec <= 0 {
		r.Intervals.StatusPushSec = defaultIntervalSec
	}
	for name, v := range map[string]int{
		"user_pull_sec":    r.Intervals.UserPullSec,
		"traffic_push_sec": r.Intervals.TrafficPushSec,
		"alive_push_sec":   r.Intervals.AlivePushSec,
		"status_push_sec":  r.Intervals.StatusPushSec,
	} {
		if v < minIntervalSec {
			return fmt.Errorf("intervals.%s 不可低于 %d 秒（避免对上游 API 形成压测）", name, minIntervalSec)
		}
	}

	// ---------------- reporting ----------------
	// AliveEnabled / StatusEnabled 都是 bool，零值即"关闭"，无需补默认。

	// ---------------- web ----------------
	r.Web.ListenAddr = strings.TrimSpace(r.Web.ListenAddr)
	if r.Web.ListenAddr == "" {
		r.Web.ListenAddr = defaultWebListenAddr
	}
	if r.Web.SessionMaxAgeHours <= 0 {
		r.Web.SessionMaxAgeHours = defaultWebSessionMaxHours
	}
	if r.Web.AbsoluteMaxLifetimeHours <= 0 {
		r.Web.AbsoluteMaxLifetimeHours = defaultWebAbsoluteMaxHours
	}
	if r.Web.SessionMaxAgeHours > r.Web.AbsoluteMaxLifetimeHours {
		// 滑动窗口超过绝对寿命没意义——后者无效。运维明显误配，应当 fail-fast。
		return fmt.Errorf("web.session_max_age_hours (%d) 不可超过 absolute_max_lifetime_hours (%d)",
			r.Web.SessionMaxAgeHours, r.Web.AbsoluteMaxLifetimeHours)
	}

	return nil
}

// CredsComplete 判断 Xboard 鉴权所需的凭据是否填齐。
//
// 仅检查"必填字段非空"语义；不验证字段格式（格式由 Validate 负责）。
// 与 Xui.CredsComplete 同名同位置，让调用方写出对称的 cfg.Xboard.CredsComplete()
// 与 cfg.Xui.CredsComplete() 表达式。
//
// 调用方：supervisor.applyCredsGuard（半填时强制空载避免无效请求风暴）
// 与 web.handleStatus（Dashboard "凭据完整性" 灯状态）。两者共用同一函数
// 杜绝"supervisor 觉得 OK 但 UI 显示未完整"或反向不一致的运维心智负担。
func (x Xboard) CredsComplete() bool {
	return x.APIHost != "" && x.Token != ""
}

// CredsComplete 判断 3x-ui 鉴权所需的凭据是否填齐。
//
// v0.4 起仅 cookie 登录模式：APIHost + Username + Password 任一为空即视为
// 未配齐。TOTPSecret 不参与判定——多数部署未启用 2FA，强行要求该字段会让
// "未开 2FA 的部署"永远显示未完整；启用 2FA 但 secret 暂未填的部署，配置
// 层仍合法（半填严格规则在 Validate 中按 Username/Password 判定，与
// TOTPSecret 解耦）。
//
// 仅检查"必填字段非空"语义；与 Validate 不同，本函数不做半填严格 / base32
// 等格式校验——那些校验已在配置加载阶段完成；运行期判定只关心"够不够发请求"。
func (x Xui) CredsComplete() bool {
	return x.APIHost != "" && x.Username != "" && x.Password != ""
}

// validateHTTPHost 检查传入字符串是否为含 scheme 的合法 HTTP/HTTPS URL。
// 不允许 path 以外的尾部内容（query / fragment），因为这些字段会与
// 后续拼接的 API 路径冲突，不应在配置中出现。
func validateHTTPHost(field, raw string) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("%s 不可为空", field)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s 解析失败：%w", field, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%s 必须以 http:// 或 https:// 开头", field)
	}
	if u.Host == "" {
		return fmt.Errorf("%s 缺少主机名", field)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("%s 不应包含 query 或 fragment", field)
	}
	return nil
}

// normalizeBasePath 把空字符串、"/"、"abc"、"/abc/" 一律归一化为 "" 或 "/abc"，
// 便于后续 url.Path = APIHost + BasePath + "/panel/api/..." 的拼接保持一致。
func normalizeBasePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" || p == "/" {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return strings.TrimRight(p, "/")
}

// sortedKeys 仅用于错误消息中输出协议清单，保持稳定顺序便于人眼识别。
// 使用 map+循环而非排序库，因协议数量极少（<10），手写排序得不偿失。
func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		// 维护一个简单的有序插入：从尾部冒泡到正确位置。
		// 复杂度 O(n^2) 但 n<=10，远低于使用 sort.Strings 的开销。
		out = append(out, k)
		for i := len(out) - 1; i > 0 && out[i] < out[i-1]; i-- {
			out[i], out[i-1] = out[i-1], out[i]
		}
	}
	return out
}

// parseSettingInt 从 settings map 取 key 对应字符串并转 int。
//
// 缺失或解析失败一律返回 fallback——v0.2 的"半填友好"原则：从持久层
// 读到的脏数据不应导致进程崩溃，而是退化到默认值并由 Web 表单层在用户
// 下次写入时立即提示。
func parseSettingInt(m map[string]string, key string, fallback int) int {
	raw, ok := m[key]
	if !ok {
		return fallback
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return n
}

// parseSettingBool 从 settings map 取 key 对应字符串并转 bool。语义同
// parseSettingInt。
//
// strconv.ParseBool 能处理 "1"/"0"/"true"/"false"/"True" 等十种常见写法，
// 不需要在这里做大小写归一。
func parseSettingBool(m map[string]string, key string, fallback bool) bool {
	raw, ok := m[key]
	if !ok {
		return fallback
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return b
}

// Duration 是一组从配置秒数派生的便捷方法，避免下游业务代码每次都写 *time.Second。
//
// 不在 Validate 中预先转换为 time.Duration 字段：因为 settings 表存的是
// 字符串（详见 schema.go 的 settings 表注释），最自然的反序列化目标是
// "纯整数秒"——既能被 strconv.Atoi 一行解析，也方便运维直接用
// `sqlite3 ./data/bridge.db` 命令行手工核对当前生效值。预先转 Duration
// 反而把"600000000000ns 还是 60s"的歧义引入 settings 字符串。

// UserPullDuration 返回用户拉取循环的间隔。
func (i Intervals) UserPullDuration() time.Duration { return secs(i.UserPullSec) }

// TrafficPushDuration 返回流量上报循环的间隔。
func (i Intervals) TrafficPushDuration() time.Duration { return secs(i.TrafficPushSec) }

// AlivePushDuration 返回在线 IP 上报循环的间隔。
func (i Intervals) AlivePushDuration() time.Duration { return secs(i.AlivePushSec) }

// StatusPushDuration 返回节点状态上报循环的间隔。
func (i Intervals) StatusPushDuration() time.Duration { return secs(i.StatusPushSec) }

// secs 把秒数 n 转成 time.Duration，单独抽函数仅为可读性。
func secs(n int) time.Duration { return time.Duration(n) * time.Second }
