package web

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/xboard-bridge/xboard-xui-bridge/internal/store"
)

// bridgeResponse 是 GET /api/bridges 与单条返回共用的对外结构。
type bridgeResponse struct {
	Name           string `json:"name"`
	XboardNodeID   int    `json:"xboard_node_id"`
	XboardNodeType string `json:"xboard_node_type"`
	XuiInboundID   int    `json:"xui_inbound_id"`
	Protocol       string `json:"protocol"`
	Flow           string `json:"flow,omitempty"`
	Enable         bool   `json:"enable"`
	CreatedAt      string `json:"created_at,omitempty"`
	UpdatedAt      string `json:"updated_at,omitempty"`
}

// bridgeRequest 是 POST / PUT 共用的请求体。
//
// 字段命名 snake_case：保持与 settings 一致；前端开发者一眼能联想到
// SQL 列名与 store.BridgeRow 字段。
type bridgeRequest struct {
	Name           string `json:"name"`
	XboardNodeID   int    `json:"xboard_node_id"`
	XboardNodeType string `json:"xboard_node_type"`
	XuiInboundID   int    `json:"xui_inbound_id"`
	Protocol       string `json:"protocol"`
	Flow           string `json:"flow"`
	Enable         bool   `json:"enable"`
}

// handleListBridges 处理 GET /api/bridges。
func (s *Server) handleListBridges(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ListBridges(r.Context())
	if err != nil {
		s.log.Error("ListBridges 失败", "err", err)
		s.writeError(w, http.StatusInternalServerError, errCodeInternal, "查询桥接列表失败")
		return
	}
	out := make([]bridgeResponse, 0, len(rows))
	for i := range rows {
		out = append(out, marshalBridge(&rows[i]))
	}
	s.writeJSON(w, http.StatusOK, out)
}

// handleCreateBridge 处理 POST /api/bridges。
//
// 行为：
//
//   1) 解析 body；
//   2) 调 store.CreateBridge（store 内部已做 name/protocol 非空校验 +
//      唯一约束映射）；
//   3) 调 reload；
//   4) 返回新建项。
//
// 注意：不在本 handler 做 protocol 取值合法性校验——store 层用 NOT NULL
// 仅保 name/protocol 非空，真正的"是否在 allowedProtocols 内"由
// LoadFromStore 后的 Validate 处理。如果用户提交 "qq-protocol" 这种
// 非法值，会先成功落库，然后 reload 触发 Validate 失败，handler 返回 5xx
// 让运维知道"已写入但引擎未切换"。可接受。
//
// 进一步的 v0.3 优化：在写库前直接调一次 cfg.Validate(stub *Root)
// 校验 protocol 合法性。M5 阶段先不做，避免维护两套校验逻辑。
func (s *Server) handleCreateBridge(w http.ResponseWriter, r *http.Request) {
	var req bridgeRequest
	if err := readJSON(r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, errCodeBadRequest, "请求体格式错误")
		return
	}
	row := bridgeFromRequest(req)
	if err := s.store.CreateBridge(r.Context(), row); err != nil {
		switch {
		case errors.Is(err, store.ErrAlreadyExists):
			s.writeError(w, http.StatusConflict, errCodeConflict, "桥接名已存在："+row.Name)
		default:
			// store 层已做 name/protocol 非空 trim 校验；其余错误（DB 故障 / SQLite BUSY）
			// 一律 500。
			s.log.Error("CreateBridge 失败", "name", row.Name, "err", err)
			s.writeError(w, http.StatusInternalServerError, errCodeInternal, "创建桥接失败")
		}
		return
	}
	if err := s.reloadFromStore(r.Context()); err != nil {
		s.log.Error("CreateBridge 后 reload 失败", "name", row.Name, "err", err)
		s.writeError(w, http.StatusInternalServerError, errCodeInternal, "桥接已保存但引擎重载失败，请检查日志")
		return
	}
	// 取一次最新行回传（带 created_at / updated_at）。
	saved, err := s.store.GetBridge(r.Context(), row.Name)
	if err != nil {
		// 极小概率：刚 create 完立刻 get 没拿到——不影响成功结果。
		s.writeJSON(w, http.StatusCreated, marshalBridge(&row))
		return
	}
	s.writeJSON(w, http.StatusCreated, marshalBridge(&saved))
}

// handleUpdateBridge 处理 PUT /api/bridges/{name}。
//
// 路径参数 {name} 必须与 body.name 一致——避免 URL 拼写漂移。
func (s *Server) handleUpdateBridge(w http.ResponseWriter, r *http.Request) {
	pathName := strings.TrimSpace(r.PathValue("name"))
	if pathName == "" {
		s.writeError(w, http.StatusBadRequest, errCodeBadRequest, "URL 缺少 name")
		return
	}
	var req bridgeRequest
	if err := readJSON(r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, errCodeBadRequest, "请求体格式错误")
		return
	}
	if strings.TrimSpace(req.Name) != pathName {
		s.writeError(w, http.StatusBadRequest, errCodeBadRequest, "URL 中的 name 与 body 中的 name 不一致")
		return
	}
	row := bridgeFromRequest(req)
	if err := s.store.UpdateBridge(r.Context(), row); err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			s.writeError(w, http.StatusNotFound, errCodeNotFound, "桥接不存在："+row.Name)
		default:
			s.log.Error("UpdateBridge 失败", "name", row.Name, "err", err)
			s.writeError(w, http.StatusInternalServerError, errCodeInternal, "更新桥接失败")
		}
		return
	}
	if err := s.reloadFromStore(r.Context()); err != nil {
		s.log.Error("UpdateBridge 后 reload 失败", "name", row.Name, "err", err)
		s.writeError(w, http.StatusInternalServerError, errCodeInternal, "桥接已保存但引擎重载失败，请检查日志")
		return
	}
	saved, err := s.store.GetBridge(r.Context(), row.Name)
	if err != nil {
		s.writeJSON(w, http.StatusOK, marshalBridge(&row))
		return
	}
	s.writeJSON(w, http.StatusOK, marshalBridge(&saved))
}

// handleDeleteBridge 处理 DELETE /api/bridges/{name}。
//
// store.DeleteBridge 是幂等的——本 handler 不区分"原本就不存在"与
// "成功删除"，统一返回 200 + 空 envelope。这是删除接口的常见行为：
// 客户端"删除我已删过的资源" 不应感知差异。
//
// 选 200 而非 204：本项目所有 API 走统一 envelope（apiResponse），204
// 不带 body 与该约定冲突；若某些前端代码无差别 JSON.parse 响应会因 204
// 空 body 报错。返回 `{}` envelope 让前端代码路径完全统一。
//
// 留给 v0.3 的清理项：当前 M5 阶段 traffic_baseline 中以该 bridge_name
// 为主键的所有行不会被清理——store 没有"按 bridge_name 批量删除"的接口；
// user_sync 仅在用户被禁/删时单条删 baseline。删除桥接后这部分数据成为
// 无主行，占用极少（< 1KB / user）。v0.3 计划新增
// store.DeleteBaselinesByBridge 一次性清理。
func (s *Server) handleDeleteBridge(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		s.writeError(w, http.StatusBadRequest, errCodeBadRequest, "URL 缺少 name")
		return
	}
	if err := s.store.DeleteBridge(r.Context(), name); err != nil {
		s.log.Error("DeleteBridge 失败", "name", name, "err", err)
		s.writeError(w, http.StatusInternalServerError, errCodeInternal, "删除桥接失败")
		return
	}
	if err := s.reloadFromStore(r.Context()); err != nil {
		s.log.Error("DeleteBridge 后 reload 失败", "name", name, "err", err)
		s.writeError(w, http.StatusInternalServerError, errCodeInternal, "桥接已删除但引擎重载失败，请检查日志")
		return
	}
	s.writeJSON(w, http.StatusOK, struct{}{})
}

// bridgeFromRequest 把请求 DTO 转换为 store.BridgeRow。
//
// 不做 trim / lower：这些规范化在 LoadFromStore 后的 Validate 中执行。
// 直接保留原值落库，让运维下次 GET 看到自己输入了什么——便于排错"为什么
// 我的字段被悄悄改了"。
func bridgeFromRequest(req bridgeRequest) store.BridgeRow {
	return store.BridgeRow{
		Name:           req.Name,
		XboardNodeID:   req.XboardNodeID,
		XboardNodeType: req.XboardNodeType,
		XuiInboundID:   req.XuiInboundID,
		Protocol:       req.Protocol,
		Flow:           req.Flow,
		Enable:         req.Enable,
	}
}

// marshalBridge 把 store.BridgeRow 投影到 bridgeResponse。
//
// 时间字段用 RFC3339；零值时间不输出（让前端判断"暂未发生"）。
func marshalBridge(row *store.BridgeRow) bridgeResponse {
	resp := bridgeResponse{
		Name:           row.Name,
		XboardNodeID:   row.XboardNodeID,
		XboardNodeType: row.XboardNodeType,
		XuiInboundID:   row.XuiInboundID,
		Protocol:       row.Protocol,
		Flow:           row.Flow,
		Enable:         row.Enable,
	}
	if !row.CreatedAt.IsZero() {
		resp.CreatedAt = row.CreatedAt.UTC().Format(time.RFC3339)
	}
	if !row.UpdatedAt.IsZero() {
		resp.UpdatedAt = row.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return resp
}
