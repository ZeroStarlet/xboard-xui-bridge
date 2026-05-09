package web

import (
	"net/http"
	"time"
)

// statusResponse 是 GET /api/status 的返回结构。
//
// 设计意图：仪表盘（M6 阶段引入）需要快速感知"引擎是否在跑、有几个 bridge
// 启用、当前监听地址是什么"。本结构提供这些信息的"汇总视图"，避免前端
// 反复调多个端点拼装。
//
// 不暴露：单 bridge 的最近一次同步错误 / 详细 worker 心跳。这些信息散
// 落在 sync.Engine 内部 worker 里，目前没有反查接口；M6+ 可考虑增加。
type statusResponse struct {
	// Running 引擎当前是否处于 Run 期间（即 supervisor.Run 已启动且未退出）。
	Running bool `json:"running"`

	// EnabledBridgeCount 当前 cfg 中 enable=true 的 bridge 数。配合
	// CredsComplete 让前端展示 "X 个桥接，Y 个未配置凭据"。
	EnabledBridgeCount int `json:"enabled_bridge_count"`
	TotalBridgeCount   int `json:"total_bridge_count"`

	// CredsComplete xboard / xui 凭据是否全部配置完整。运维一眼看到引擎
	// 是否真在做事——半填状态下凭据不全 + supervisor.applyCredsGuard 会清空
	// Bridges，导致 EnabledBridgeCount=0 但仍标 Running。
	CredsComplete bool `json:"creds_complete"`

	// ListenAddr 当前 Web 监听地址；显示在状态页便于运维快速找到自己访问的入口。
	ListenAddr string `json:"listen_addr"`

	// Now 服务器当前 UTC 时间，前端用来识别浏览器/服务器时区差异。
	Now string `json:"now"`
}

// handleGetStatus 处理 GET /api/status。
func (s *Server) handleGetStatus(w http.ResponseWriter, r *http.Request) {
	cfg := s.supervisor.Snapshot()
	resp := statusResponse{
		Running: s.supervisor.Running(),
		Now:     time.Now().UTC().Format(time.RFC3339),
	}
	if cfg != nil {
		resp.TotalBridgeCount = len(cfg.Bridges)
		for _, b := range cfg.Bridges {
			if b.Enable {
				resp.EnabledBridgeCount++
			}
		}
		resp.CredsComplete = cfg.Xboard.APIHost != "" && cfg.Xboard.Token != "" &&
			cfg.Xui.APIHost != "" && cfg.Xui.APIToken != ""
		resp.ListenAddr = cfg.Web.ListenAddr
	}
	s.writeJSON(w, http.StatusOK, resp)
}

// healthResponse 是 GET /api/health 的返回结构。极简——存活探针就需要"回个
// 200 + 一个固定字符串"。Kubernetes / Docker healthcheck 等只看状态码。
type healthResponse struct {
	Status string `json:"status"`
}

// handleHealth 处理 GET /api/health。无鉴权，仅用于存活探针。
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
}
