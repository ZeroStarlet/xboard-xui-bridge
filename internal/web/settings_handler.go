package web

import (
	"net/http"
	"strconv"

	"github.com/xboard-bridge/xboard-xui-bridge/internal/config"
)

// settingsResponse 是 GET /api/settings 的返回结构。
//
// 不直接返回 *config.Root：那是引擎内部模型，含 State.Database 等"运维
// 不应通过 GUI 修改"的字段。这里做投影，只暴露 Web 表单需要操作的字段。
//
// 字段命名采用 snake_case：与 settings 表 key 命名一致，让前端开发者
// 看到 JSON 字段就能联想到 settings 表 key。
type settingsResponse struct {
	Log struct {
		Level      string `json:"level"`
		File       string `json:"file"`
		MaxSizeMB  int    `json:"max_size_mb"`
		MaxBackups int    `json:"max_backups"`
		MaxAgeDays int    `json:"max_age_days"`
	} `json:"log"`
	Xboard struct {
		APIHost       string `json:"api_host"`
		Token         string `json:"token"`
		TimeoutSec    int    `json:"timeout_sec"`
		SkipTLSVerify bool   `json:"skip_tls_verify"`
		UserAgent     string `json:"user_agent"`
	} `json:"xboard"`
	Xui struct {
		APIHost  string `json:"api_host"`
		BasePath string `json:"base_path"`
		// v0.4 起仅 cookie 登录模式；admin 鉴权后明文回传 password / totp_secret，
		// 前端如需 mask 自行处理（与 v0.2/v0.3 的 api_token 行为一致）。
		Username      string `json:"username"`
		Password      string `json:"password"`
		TOTPSecret    string `json:"totp_secret"`
		TimeoutSec    int    `json:"timeout_sec"`
		SkipTLSVerify bool   `json:"skip_tls_verify"`
	} `json:"xui"`
	Intervals struct {
		UserPullSec    int `json:"user_pull_sec"`
		TrafficPushSec int `json:"traffic_push_sec"`
		AlivePushSec   int `json:"alive_push_sec"`
		StatusPushSec  int `json:"status_push_sec"`
	} `json:"intervals"`
	Reporting struct {
		AliveEnabled  bool `json:"alive_enabled"`
		StatusEnabled bool `json:"status_enabled"`
	} `json:"reporting"`
	Web struct {
		ListenAddr               string `json:"listen_addr"`
		SessionMaxAgeHours       int    `json:"session_max_age_hours"`
		AbsoluteMaxLifetimeHours int    `json:"absolute_max_lifetime_hours"`
	} `json:"web"`
}

// handleGetSettings 处理 GET /api/settings。
//
// 行为：
//   - 调 supervisor.Snapshot 拿到当前生效配置；
//   - 投影到 settingsResponse 结构（脱敏 + 字段过滤）；
//   - 返回 200。
//
// 关键：xboard.token / xui.password / xui.totp_secret 也在返回里——前端
// 需要在编辑页显示 "已设置 (****)" 还是空。完整明文回传是有意为之，因为
// Web 面板的访问者已经通过 admin 鉴权，他们本来就能看到一切凭据；前端
// 如果担心展示就自己 mask 即可（password / totp_secret 已用 type=password
// 默认隐藏字符）。
//
// 不直接读 store：supervisor.Snapshot 的好处是 cfg 已经过 LoadFromStore
// + Validate，时间戳和默认值都已经规范化。
func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	cfg := s.supervisor.Snapshot()
	if cfg == nil {
		s.writeError(w, http.StatusInternalServerError, errCodeInternal, "未取到当前配置")
		return
	}
	resp := projectSettings(cfg)
	s.writeJSON(w, http.StatusOK, resp)
}

// projectSettings 把 *config.Root 投影为 settingsResponse。
func projectSettings(cfg *config.Root) settingsResponse {
	var r settingsResponse
	r.Log.Level = cfg.Log.Level
	r.Log.File = cfg.Log.File
	r.Log.MaxSizeMB = cfg.Log.MaxSizeMB
	r.Log.MaxBackups = cfg.Log.MaxBackups
	r.Log.MaxAgeDays = cfg.Log.MaxAgeDays

	r.Xboard.APIHost = cfg.Xboard.APIHost
	r.Xboard.Token = cfg.Xboard.Token
	r.Xboard.TimeoutSec = cfg.Xboard.TimeoutSec
	r.Xboard.SkipTLSVerify = cfg.Xboard.SkipTLSVerify
	r.Xboard.UserAgent = cfg.Xboard.UserAgent

	r.Xui.APIHost = cfg.Xui.APIHost
	r.Xui.BasePath = cfg.Xui.BasePath
	r.Xui.Username = cfg.Xui.Username
	r.Xui.Password = cfg.Xui.Password
	r.Xui.TOTPSecret = cfg.Xui.TOTPSecret
	r.Xui.TimeoutSec = cfg.Xui.TimeoutSec
	r.Xui.SkipTLSVerify = cfg.Xui.SkipTLSVerify

	r.Intervals.UserPullSec = cfg.Intervals.UserPullSec
	r.Intervals.TrafficPushSec = cfg.Intervals.TrafficPushSec
	r.Intervals.AlivePushSec = cfg.Intervals.AlivePushSec
	r.Intervals.StatusPushSec = cfg.Intervals.StatusPushSec

	r.Reporting.AliveEnabled = cfg.Reporting.AliveEnabled
	r.Reporting.StatusEnabled = cfg.Reporting.StatusEnabled

	r.Web.ListenAddr = cfg.Web.ListenAddr
	r.Web.SessionMaxAgeHours = cfg.Web.SessionMaxAgeHours
	r.Web.AbsoluteMaxLifetimeHours = cfg.Web.AbsoluteMaxLifetimeHours

	return r
}

// settingsPatchRequest 是 PATCH /api/settings 的请求体。
//
// 所有字段都是 *T 指针：JSON 中"字段未提供"vs"字段提供了零值"必须区分——
// 前者表示"保持当前值不动"，后者表示"明确设为零值/空字符串"。指针 + 不
// 序列化 nil 字段是 Go 标准做法。
type settingsPatchRequest struct {
	Log *struct {
		Level      *string `json:"level,omitempty"`
		File       *string `json:"file,omitempty"`
		MaxSizeMB  *int    `json:"max_size_mb,omitempty"`
		MaxBackups *int    `json:"max_backups,omitempty"`
		MaxAgeDays *int    `json:"max_age_days,omitempty"`
	} `json:"log,omitempty"`
	Xboard *struct {
		APIHost       *string `json:"api_host,omitempty"`
		Token         *string `json:"token,omitempty"`
		TimeoutSec    *int    `json:"timeout_sec,omitempty"`
		SkipTLSVerify *bool   `json:"skip_tls_verify,omitempty"`
		UserAgent     *string `json:"user_agent,omitempty"`
	} `json:"xboard,omitempty"`
	Xui *struct {
		APIHost  *string `json:"api_host,omitempty"`
		BasePath *string `json:"base_path,omitempty"`
		// v0.4 起仅 cookie 登录模式；运行期可热重载，不在"重启生效"白名单内。
		Username      *string `json:"username,omitempty"`
		Password      *string `json:"password,omitempty"`
		TOTPSecret    *string `json:"totp_secret,omitempty"`
		TimeoutSec    *int    `json:"timeout_sec,omitempty"`
		SkipTLSVerify *bool   `json:"skip_tls_verify,omitempty"`
	} `json:"xui,omitempty"`
	Intervals *struct {
		UserPullSec    *int `json:"user_pull_sec,omitempty"`
		TrafficPushSec *int `json:"traffic_push_sec,omitempty"`
		AlivePushSec   *int `json:"alive_push_sec,omitempty"`
		StatusPushSec  *int `json:"status_push_sec,omitempty"`
	} `json:"intervals,omitempty"`
	Reporting *struct {
		AliveEnabled  *bool `json:"alive_enabled,omitempty"`
		StatusEnabled *bool `json:"status_enabled,omitempty"`
	} `json:"reporting,omitempty"`
	Web *struct {
		ListenAddr               *string `json:"listen_addr,omitempty"`
		SessionMaxAgeHours       *int    `json:"session_max_age_hours,omitempty"`
		AbsoluteMaxLifetimeHours *int    `json:"absolute_max_lifetime_hours,omitempty"`
	} `json:"web,omitempty"`
}

// handlePatchSettings 处理 PATCH /api/settings。
//
// 流程：
//
//   1) 解析 body；
//   2) 拒绝 log.* / web.* 字段（启动期定型，运行期不可热重载）；
//   3) 把剩余的可热重载非 nil 字段映射到 settings KV map；
//   4) 调 store.SetSettings 单事务批量写入；
//   5) 调 supervisor 重新 LoadFromStore + Reload。
//
// 失败处理：
//   - 解析失败 → 400
//   - 写库失败 → 500（store 保证事务，要么全成功要么全无）
//   - reload 失败 → 207-style 不太适合，本接口直接返回 5xx：写已成功
//     但引擎未切换。运维可以通过重启进程让新值生效。
//
// 注意：本 handler 不在 settings 写入前做 Validate——校验放在 reload
// 路径上的 LoadFromStore + config.Validate 里集中处理，避免维护两套校验
// 逻辑。这意味着写库后才发现非法字段时，store 内已落入"半生效"状态——
// 但下次 LoadFromStore 仍会按原始字符串读出来，Validate 报错让运维下次
// 改对值再重启。可接受的脆弱性。
func (s *Server) handlePatchSettings(w http.ResponseWriter, r *http.Request) {
	var req settingsPatchRequest
	if err := readJSON(r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, errCodeBadRequest, "请求体格式错误")
		return
	}
	// "重启生效"白名单：以下字段在 main.go 启动期被消费（logger.New / authSvc /
	// http.Server / web 监听地址），运行时改动不会真正切换。supervisor.Reload
	// 仅替换同步引擎的 Xboard / 3x-ui 客户端 + worker 集合，不重建 logger /
	// 不切换 listen 端口 / 不更新会话寿命策略。
	//
	// 如果接受 PATCH 会让"settings 与实际行为不一致"——运维以为改了 log.level
	// 立刻生效，但日志仍按旧 level 输出。统一显式拒绝并提示"需重启进程"。
	//
	// 重启生效字段集合：log.* / web.*
	if req.Log != nil {
		if req.Log.Level != nil || req.Log.File != nil || req.Log.MaxSizeMB != nil ||
			req.Log.MaxBackups != nil || req.Log.MaxAgeDays != nil {
			s.writeError(w, http.StatusBadRequest, errCodeBadRequest,
				"log.* 字段修改后需要重启进程才能生效；请通过 sqlite3 直接编辑 settings 表后重启")
			return
		}
	}
	if req.Web != nil {
		if req.Web.ListenAddr != nil || req.Web.SessionMaxAgeHours != nil || req.Web.AbsoluteMaxLifetimeHours != nil {
			s.writeError(w, http.StatusBadRequest, errCodeBadRequest,
				"web.listen_addr / web.session_max_age_hours / web.absolute_max_lifetime_hours 修改后需要重启进程才能生效；请通过 sqlite3 直接编辑 settings 表后重启")
			return
		}
	}
	kv := buildSettingsKV(req)
	if len(kv) == 0 {
		// 客户端提交了空 body 或全 nil 字段；本接口需要至少一个字段。
		s.writeError(w, http.StatusBadRequest, errCodeBadRequest, "未提供任何字段")
		return
	}
	if err := s.store.SetSettings(r.Context(), kv); err != nil {
		s.log.Error("SetSettings 失败", "err", err)
		s.writeError(w, http.StatusInternalServerError, errCodeInternal, "写入配置失败")
		return
	}
	if err := s.reloadFromStore(r.Context()); err != nil {
		// 把"已经写入但 reload 失败"的状态显式告诉前端：让运维知道需要
		// 重启进程才能让新值生效。
		s.log.Error("settings 写入成功但 supervisor.Reload 失败", "err", err)
		s.writeError(w, http.StatusInternalServerError, errCodeInternal, "配置已保存但引擎重启失败，请检查日志或重启进程")
		return
	}
	s.writeJSON(w, http.StatusOK, struct{}{})
}

// buildSettingsKV 把指针式 settingsPatchRequest 中"可热重载的非 nil 字段"
// 拍平为 KV map。
//
// 仅写入"字段非 nil"的项；nil 表示客户端没提交该字段、当前值保持不变。
//
// 注意：log.* / web.* 不在本函数处理范围内——它们是"启动期定型"字段，
// 已经在 handlePatchSettings 入口被显式拒绝。本函数只负责剩余的可热
// 重载字段（Xboard / 3x-ui / 同步周期 / 上报开关）。
//
// 字符串值不做 trim：Validate 会做。这里保持原样，让运维在 GUI 看到
// "我点了空格"等异常输入时能立刻发现问题。
func buildSettingsKV(req settingsPatchRequest) map[string]string {
	kv := make(map[string]string, 32)
	// 注意：req.Log 与 req.Web 的处理被 handlePatchSettings 在更早阶段
	// 显式拒绝（它们都是"重启生效"配置）；本函数不再处理 log.* / web.* ——
	// 任何路径走到这里时这两个字段已被验证为 nil 或字段全 nil。
	if req.Xboard != nil {
		setIfNotNil(kv, config.SettingXboardAPIHost, req.Xboard.APIHost)
		setIfNotNil(kv, config.SettingXboardToken, req.Xboard.Token)
		setIfNotNilInt(kv, config.SettingXboardTimeoutSec, req.Xboard.TimeoutSec)
		setIfNotNilBool(kv, config.SettingXboardSkipTLSVerify, req.Xboard.SkipTLSVerify)
		setIfNotNil(kv, config.SettingXboardUserAgent, req.Xboard.UserAgent)
	}
	if req.Xui != nil {
		setIfNotNil(kv, config.SettingXuiAPIHost, req.Xui.APIHost)
		setIfNotNil(kv, config.SettingXuiBasePath, req.Xui.BasePath)
		setIfNotNil(kv, config.SettingXuiUsername, req.Xui.Username)
		setIfNotNil(kv, config.SettingXuiPassword, req.Xui.Password)
		setIfNotNil(kv, config.SettingXuiTOTPSecret, req.Xui.TOTPSecret)
		setIfNotNilInt(kv, config.SettingXuiTimeoutSec, req.Xui.TimeoutSec)
		setIfNotNilBool(kv, config.SettingXuiSkipTLSVerify, req.Xui.SkipTLSVerify)
	}
	if req.Intervals != nil {
		setIfNotNilInt(kv, config.SettingIntervalsUserPullSec, req.Intervals.UserPullSec)
		setIfNotNilInt(kv, config.SettingIntervalsTrafficPushSec, req.Intervals.TrafficPushSec)
		setIfNotNilInt(kv, config.SettingIntervalsAlivePushSec, req.Intervals.AlivePushSec)
		setIfNotNilInt(kv, config.SettingIntervalsStatusPushSec, req.Intervals.StatusPushSec)
	}
	if req.Reporting != nil {
		setIfNotNilBool(kv, config.SettingReportingAliveEnabled, req.Reporting.AliveEnabled)
		setIfNotNilBool(kv, config.SettingReportingStatusEnabled, req.Reporting.StatusEnabled)
	}
	// 注意：req.Web 的三个字段被 handlePatchSettings 在更早阶段显式拒绝
	// （它们都是"重启生效"配置）；本函数不再处理 web.* —— 任何路径走到
	// 这里时 req.Web 已被验证为 nil 或字段全 nil。
	return kv
}

// setIfNotNil / setIfNotNilInt / setIfNotNilBool 是三组 helper：
// 仅在 *T 非 nil 时把值写入 map。
//
// 之所以拆成三个：Go 没有泛型 + 类型断言的组合可以同时优雅处理 string/
// int/bool 的"零值不为零的语义"，写三个轻量 helper 比一个泛型函数更易读。
func setIfNotNil(kv map[string]string, key string, val *string) {
	if val == nil {
		return
	}
	kv[key] = *val
}

func setIfNotNilInt(kv map[string]string, key string, val *int) {
	if val == nil {
		return
	}
	kv[key] = strconv.Itoa(*val)
}

func setIfNotNilBool(kv map[string]string, key string, val *bool) {
	if val == nil {
		return
	}
	kv[key] = strconv.FormatBool(*val)
}

