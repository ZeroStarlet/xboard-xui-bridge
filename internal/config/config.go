// Package config 定义 xboard-xui-bridge 的配置模型与加载流程。
//
// 设计原则：
//
//  1. 配置在进程启动时一次性加载并完成校验；运行期间不支持热加载，避免
//     "部分组件已切换到新配置、其他组件仍在旧配置"导致的状态不一致。
//  2. 默认值在 Validate 中显式补齐——绝不依赖 YAML 解析时的零值表达
//     "未指定"语义，因为零值与"显式写 0"无法区分（如 Interval=0 不能默认 60s）。
//  3. 任何新增配置项都必须同时：a) 写明 yaml tag；b) 在 Validate 中校验；
//     c) 在 configs/config.example.yaml 中给出注释样例。三处缺一会导致
//     用户配置静默失效，违反"显式优于隐式"的项目铁律。
//
// 不在本包做的事：
//
//	不做"读取 + 启动"的副作用（不创建文件、不打开网络连接），只做纯解析与
//	校验。这样测试只需构造 Root 结构体即可覆盖所有分支。
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
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

// 鉴权模式枚举：当前仅支持 Bearer Token。
//
// 历史背景：早期版本计划同时支持 cookie 模式（用 username/password 登录），
// 但现代 3x-ui 对浏览器路由（含 /login 与 /panel/api 的 cookie 调用）启用 CSRF
// 校验，需要先 GET /csrf-token 再在每次 POST 中带上 X-CSRF-Token，且 token
// 与 session 绑定，刷新策略复杂；维护风险显著高于 token 模式。
//
// 鉴于 3x-ui 在面板设置中可一键生成 48 字符 API Token（无状态、不参与 CSRF），
// 中间件统一收敛为 token 模式，在 README 中记录这一决定与回避动机。
const (
	XuiAuthModeToken = "token"
)

// Root 是配置文件的根节点。
//
// 嵌套结构而不是平铺：因为各部分互不耦合，嵌套能让每个组件只接收自己关心
// 的子配置（如 logger.New(cfg.Log)），保持单一职责。
type Root struct {
	Log       Log       `yaml:"log"`
	State     State     `yaml:"state"`
	Xboard    Xboard    `yaml:"xboard"`
	Xui       Xui       `yaml:"xui"`
	Bridges   []Bridge  `yaml:"bridges"`
	Intervals Intervals `yaml:"intervals"`
	Reporting Reporting `yaml:"reporting"`
}

// Log 描述日志输出策略。
type Log struct {
	// Level：debug / info / warn / error。大小写不敏感，规范化在 Validate 完成。
	Level string `yaml:"level"`
	// File：写入路径；空字符串表示 stdout。文件不存在时 logger 会自动创建。
	File string `yaml:"file"`
	// MaxSizeMB：单文件滚动阈值（MB），仅在 File 非空时生效。0 视为不滚动。
	MaxSizeMB int `yaml:"max_size_mb"`
	// MaxBackups：保留历史日志份数，超过即清理最旧的。
	MaxBackups int `yaml:"max_backups"`
	// MaxAgeDays：历史日志最大保留天数，超过即清理。
	MaxAgeDays int `yaml:"max_age_days"`
}

// State 描述本地状态持久层（流量基线、失败重传队列）。
type State struct {
	// Database：SQLite 文件路径；建议放在 /var/lib/xboard-bridge/bridge.db
	// 等持久卷下，避免随容器临时层销毁。
	Database string `yaml:"database"`
}

// Xboard 描述对 Xboard 节点 API 的访问参数。
type Xboard struct {
	// APIHost：Xboard 面板入口（含协议头），例如 https://panel.example.com，结尾不必带 /。
	APIHost string `yaml:"api_host"`
	// Token：admin_setting('server_token')，所有 V2 节点 API 鉴权使用同一个 token。
	Token string `yaml:"token"`
	// TimeoutSec：单次 HTTP 请求超时秒数，包括连接 + 读取 + 关闭。
	TimeoutSec int `yaml:"timeout_sec"`
	// SkipTLSVerify：仅自签证书的内网部署可设 true；公网部署严禁开启。
	SkipTLSVerify bool `yaml:"skip_tls_verify"`
	// UserAgent：请求头标识，默认 xboard-xui-bridge/<version>。便于 Xboard 侧访问日志识别。
	UserAgent string `yaml:"user_agent"`
}

// Xui 描述对 3x-ui 面板 API 的访问参数。
//
// 当前仅支持 Bearer Token 鉴权——对应面板设置中"API Token"。
// cookie 登录模式因 CSRF 复杂性已被移除，详见 XuiAuthModeToken 注释。
type Xui struct {
	// APIHost：3x-ui 面板根 URL（含协议头），例如 http://127.0.0.1:2053。
	APIHost string `yaml:"api_host"`
	// BasePath：3x-ui 的 webBasePath 设置；空串代表 /。
	// 中间件会在 APIHost 之后、/panel/api/... 之前拼接它。
	BasePath string `yaml:"base_path"`
	// AuthMode：保留字段；当前唯一合法取值为 "token"，写其他值会被
	// Validate 拒绝。保留是为了未来若再扩展鉴权方式时不破坏配置兼容。
	AuthMode string `yaml:"auth_mode"`
	// APIToken：3x-ui 后台生成的 48 字符 API Token。
	APIToken string `yaml:"api_token"`
	// TimeoutSec：单次 HTTP 请求超时秒数。
	TimeoutSec int `yaml:"timeout_sec"`
	// SkipTLSVerify：3x-ui 多数部署在 127.0.0.1，但少数会暴露到公网，
	// 内网用自签可放行，公网严禁。
	SkipTLSVerify bool `yaml:"skip_tls_verify"`
}

// Bridge 描述一对 (Xboard 节点, 3x-ui inbound) 的映射关系。
//
// 一个中间件实例可承载多个 Bridge：每个 Bridge 在引擎中独立运行四个同步循环，
// 互不干扰。Bridge 之间共享同一份 Xboard / Xui 客户端实例（连接复用）。
type Bridge struct {
	// Name：人类可读名称，用于日志区分；同一配置内必须唯一。
	Name string `yaml:"name"`
	// XboardNodeID：在 Xboard 后台为该节点分配的数字 ID。
	XboardNodeID int `yaml:"xboard_node_id"`
	// XboardNodeType：Xboard 端节点类型，用于 V2 API 的 node_type 参数。
	// 注意：当 Xboard 节点类型是 hysteria 时，本字段填 hysteria；
	// 至于实际是 v1 还是 v2，由 Protocol 字段决定（业务语义解耦）。
	XboardNodeType string `yaml:"xboard_node_type"`
	// XuiInboundID：在 3x-ui 侧已经创建好的 inbound 数字 ID（管理员预创建）。
	XuiInboundID int `yaml:"xui_inbound_id"`
	// Protocol：见 ProtocolXxx 常量；决定协议适配器选择。
	Protocol string `yaml:"protocol"`
	// Flow：仅 VLESS 协议生效，常见取值 xtls-rprx-vision；其他协议忽略。
	Flow string `yaml:"flow"`
	// Enable：false 时该 Bridge 不参与同步循环（用于灰度 / 临时停用）。
	Enable bool `yaml:"enable"`
}

// Intervals 描述四个同步循环的执行间隔（秒）。
//
// 默认全部 60s 与 Xboard 推荐一致；调高会降低面板压力但延长响应延迟，
// 调低则相反。所有 Bridge 共享同一组间隔，避免运维心智负担。
type Intervals struct {
	UserPullSec    int `yaml:"user_pull_sec"`
	TrafficPushSec int `yaml:"traffic_push_sec"`
	AlivePushSec   int `yaml:"alive_push_sec"`
	StatusPushSec  int `yaml:"status_push_sec"`
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
	AliveEnabled bool `yaml:"alive_enabled"`
	// StatusEnabled：是否上报节点 CPU / 内存 / 磁盘负载。
	StatusEnabled bool `yaml:"status_enabled"`
}

// LoadFromFile 读取并解析 YAML 文件，随后调用 Validate 完成校验。
//
// 任何错误都被包装上文件路径前缀，便于运维快速定位是哪个配置出问题——
// 在多实例部署中这能省下大量排查时间。
func LoadFromFile(path string) (*Root, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件 %q: %w", path, err)
	}

	var root Root
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	// KnownFields(true) 让 YAML 中的拼写错误（例如把 timeout_sec 写成 timout_sec）
	// 立即暴露为解析错误，而不是被静默忽略导致默认值生效——后者是非常危险的运维陷阱。
	dec.KnownFields(true)
	if err := dec.Decode(&root); err != nil {
		return nil, fmt.Errorf("解析配置文件 %q: %w", path, err)
	}

	if err := root.Validate(); err != nil {
		return nil, fmt.Errorf("校验配置 %q: %w", path, err)
	}
	return &root, nil
}

// Validate 同时承担"校验非法值"与"补齐默认值"两个职责。
//
// 把它们放在同一函数：因为补齐默认值后还需要再次校验（例如默认间隔 60s 之后
// 仍要确认其为正数），分两个函数会重复检查反而增加心智负担。
//
// 任何被修改字段都通过指针接收者写回 r，确保后续组件读取到的是已规范化的值。
func (r *Root) Validate() error {
	// ---------------- log ----------------
	switch strings.ToLower(strings.TrimSpace(r.Log.Level)) {
	case "":
		r.Log.Level = "info"
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
		return errors.New("state.database 必须指定本地 SQLite 文件路径")
	}

	// ---------------- xboard ----------------
	if err := validateHTTPHost("xboard.api_host", r.Xboard.APIHost); err != nil {
		return err
	}
	r.Xboard.APIHost = strings.TrimRight(r.Xboard.APIHost, "/")
	if strings.TrimSpace(r.Xboard.Token) == "" {
		return errors.New("xboard.token 不可为空")
	}
	if r.Xboard.TimeoutSec <= 0 {
		r.Xboard.TimeoutSec = 15
	}
	if strings.TrimSpace(r.Xboard.UserAgent) == "" {
		r.Xboard.UserAgent = "xboard-xui-bridge"
	}

	// ---------------- xui ----------------
	if err := validateHTTPHost("xui.api_host", r.Xui.APIHost); err != nil {
		return err
	}
	r.Xui.APIHost = strings.TrimRight(r.Xui.APIHost, "/")
	r.Xui.BasePath = normalizeBasePath(r.Xui.BasePath)
	switch strings.ToLower(strings.TrimSpace(r.Xui.AuthMode)) {
	case "", XuiAuthModeToken:
		r.Xui.AuthMode = XuiAuthModeToken
	default:
		return fmt.Errorf("xui.auth_mode 取值非法：%q（当前仅支持 token，cookie 模式已移除）", r.Xui.AuthMode)
	}
	if strings.TrimSpace(r.Xui.APIToken) == "" {
		return errors.New("xui.api_token 不可为空（请在 3x-ui 后台生成 API Token）")
	}
	if r.Xui.TimeoutSec <= 0 {
		r.Xui.TimeoutSec = 15
	}

	// ---------------- bridges ----------------
	if len(r.Bridges) == 0 {
		return errors.New("bridges 至少需要配置一项")
	}
	seenName := make(map[string]struct{}, len(r.Bridges))
	enabledCount := 0
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

		if b.Enable {
			enabledCount++
		}
	}
	// 防御漏写 enable: true 导致全部 Bridge 静默禁用。
	// YAML 中省略 enable 字段时，bool 默认 false——若所有 Bridge 都未显式 enable，
	// 引擎会启动后立刻挂起在 ctx.Done() 上，运维以为程序在跑实际什么都没做。
	if enabledCount == 0 {
		return errors.New("bridges 至少需要一项 enable: true（否则同步引擎不会进行任何工作）")
	}

	// ---------------- intervals（默认 60s，最低 5s 防止把面板打挂） ----------------
	if r.Intervals.UserPullSec <= 0 {
		r.Intervals.UserPullSec = 60
	}
	if r.Intervals.TrafficPushSec <= 0 {
		r.Intervals.TrafficPushSec = 60
	}
	if r.Intervals.AlivePushSec <= 0 {
		r.Intervals.AlivePushSec = 60
	}
	if r.Intervals.StatusPushSec <= 0 {
		r.Intervals.StatusPushSec = 60
	}
	for name, v := range map[string]int{
		"user_pull_sec":    r.Intervals.UserPullSec,
		"traffic_push_sec": r.Intervals.TrafficPushSec,
		"alive_push_sec":   r.Intervals.AlivePushSec,
		"status_push_sec":  r.Intervals.StatusPushSec,
	} {
		if v < 5 {
			return fmt.Errorf("intervals.%s 不可低于 5 秒（避免对上游 API 形成压测）", name)
		}
	}

	// ---------------- reporting ----------------
	// AliveEnabled / StatusEnabled 都是 bool，YAML 解析的零值即"关闭"，无需补默认。

	return nil
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

// Duration 是一组从配置秒数派生的便捷方法，避免下游业务代码每次都写 *time.Second。
// 不在 Validate 中预先转换为 time.Duration 字段：因为 yaml 序列化 time.Duration
// 体验差（"60s" vs 60000000000 各有歧义），直接保留 int 秒数最稳。

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
