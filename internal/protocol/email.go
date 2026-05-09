// Package protocol 把 Xboard 用户与 3x-ui inbound 中的 client 项做双向适配。
//
// email 命名约定：
//
//	xboard_<uuid>_<node_id>
//
// 例如 uuid="550e8400-...-000"、node_id=11 时：
//
//	xboard_550e8400-...-000_11
//
// 设计动机：
//
//  1. 前缀 xboard_ 把中间件托管的 client 与运维手工添加的 client 区分；
//  2. 中间嵌 uuid 让运维肉眼即可看出对应 Xboard 用户；
//  3. 末尾追加 node_id 让同一个用户在多节点（多 inbound）下不撞 email——
//     3x-ui 把 email 当作 inbound 内唯一键，故只需 inbound 内不撞即可，
//     但我们仍保留 node_id 后缀以防同一 inbound 跨 bridge 复用时冲突。
package protocol

import (
	"errors"
	"strconv"
	"strings"
)

// emailPrefix 是中间件自管 client 的统一前缀。
//
// 任何运维手工添加的 client（不带本前缀）会被中间件视为"非托管"——
// 同步时不会被 diff 删除，避免误伤。这一约束在 sync.diff 阶段实现。
const emailPrefix = "xboard_"

// MakeEmail 由 (uuid, nodeID) 组合出标准 email。
//
// 不做大小写转换：UUID 标准是小写，但部分用户库可能存大写；保留原样以方便
// 用 ParseEmail 直接还原；下游 3x-ui 也按字符串严格匹配。
func MakeEmail(uuid string, nodeID int) string {
	return emailPrefix + uuid + "_" + strconv.Itoa(nodeID)
}

// IsManaged 判断给定 email 是否由本中间件管理。
//
// 用于 diff 阶段判断是否可以安全删除 3x-ui 端不再属于 Xboard 的 client：
// 仅 IsManaged 为 true 的 client 才参与同步删除。
func IsManaged(email string) bool {
	return strings.HasPrefix(email, emailPrefix)
}

// ParseEmail 是 MakeEmail 的逆运算。
//
// 入参不符合格式时返回 ErrInvalidEmail；调用方据此把不可识别的 email 跳过。
func ParseEmail(email string) (uuid string, nodeID int, err error) {
	if !strings.HasPrefix(email, emailPrefix) {
		return "", 0, ErrInvalidEmail
	}
	body := email[len(emailPrefix):]
	// 从右往左找最后一个 "_"，因为 UUID 中本身包含连字符（-）但不含下划线，
	// 在罕见情况下若 UUID 实现含下划线，强制以最后一个 _ 为分割符也能保证 nodeID 取出正确。
	idx := strings.LastIndex(body, "_")
	if idx <= 0 || idx == len(body)-1 {
		return "", 0, ErrInvalidEmail
	}
	uuid = body[:idx]
	nodeID, perr := strconv.Atoi(body[idx+1:])
	if perr != nil {
		return "", 0, ErrInvalidEmail
	}
	return uuid, nodeID, nil
}

// ErrInvalidEmail 表示给定字符串不符合 MakeEmail 约定的格式。
var ErrInvalidEmail = errors.New("protocol: email 不是 xboard 中间件托管格式")
