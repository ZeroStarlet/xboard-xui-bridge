package protocol

import (
	"github.com/xboard-bridge/xboard-xui-bridge/internal/config"
	"github.com/xboard-bridge/xboard-xui-bridge/internal/xboard"
	"github.com/xboard-bridge/xboard-xui-bridge/internal/xui"
)

// 本文件集中实现六种协议的 Adapter。
//
// 风格统一约定（不允许跨协议偏离）：
//
//   - 适配器结构体名 = <协议>Adapter（lowerCamel；包外不需要直接构造，通过 For() 取）。
//   - 字段填充顺序与 ClientSettings 字段声明顺序一致，便于 diff 比较。
//   - 不在 BuildClient 内做 IO，纯函数。
//   - KeyOf 仅读取 ClientSettings，绝不依赖外部状态——若依赖会破坏单元测试可重放性。
//
// KeyOf 的语义：
//
//	返回值用于拼接 POST /panel/api/inbounds/updateClient/:clientKey 的 URL 参数。
//	3x-ui 后端按 inbound.protocol 不同，用不同字段在 settings.clients 中查找：
//	  vless / vmess              按 client.id (UUID)
//	  trojan                     按 client.password
//	  shadowsocks                按 client.email（注意：不是 password！）
//	  hysteria / hysteria2       按 client.auth
//	因此各协议适配器必须返回与之匹配的字段。
//
// 共同填充字段：
//   Email      由 MakeEmail(uuid, node_id) 生成，跨协议一致。
//   Enable     固定 true；用户被禁用时由 Xboard /user 端点直接从列表移除，
//              故中间件无需在 client 层维护 enable=false 状态。
//   ExpiryTime 0（不过期，由 Xboard 控制生命周期）。
//   TotalGB    0（不限额，由 Xboard 控制）。
//   LimitIP    Xboard.User.DeviceLimit；3x-ui 端 limitIp=0 表示不限。
//   Reset      0（流量重置由 Xboard 主导，3x-ui 端不参与计费）。

// vlessAdapter 适配 VLESS 协议。
type vlessAdapter struct{}

func (vlessAdapter) Protocol() string { return config.ProtocolVLESS }

func (vlessAdapter) BuildClient(user xboard.User, bridge config.Bridge) xui.ClientSettings {
	return xui.ClientSettings{
		ID:         user.UUID,
		Email:      MakeEmail(user.UUID, bridge.XboardNodeID),
		Enable:     true,
		Flow:       bridge.Flow,
		ExpiryTime: expiryFromUser(user),
		TotalGB:    totalGBFromTransfer(user),
		LimitIP:    user.DeviceLimit,
	}
}

func (vlessAdapter) KeyOf(client xui.ClientSettings) string { return client.ID }

// vmessAdapter 适配 VMess 协议。
//
// 注意：3x-ui 不在 client 上单独存 alterId，alterId 在 inbound.settings.default 中。
// 中间件不修改 inbound 元数据，故无需关心 alterId。
type vmessAdapter struct{}

func (vmessAdapter) Protocol() string { return config.ProtocolVMess }

func (vmessAdapter) BuildClient(user xboard.User, bridge config.Bridge) xui.ClientSettings {
	return xui.ClientSettings{
		ID:         user.UUID,
		Email:      MakeEmail(user.UUID, bridge.XboardNodeID),
		Enable:     true,
		ExpiryTime: expiryFromUser(user),
		TotalGB:    totalGBFromTransfer(user),
		LimitIP:    user.DeviceLimit,
	}
}

func (vmessAdapter) KeyOf(client xui.ClientSettings) string { return client.ID }

// trojanAdapter 适配 Trojan 协议。
//
// 3x-ui trojan client 关键字段是 password；中间件直接把 uuid 当作 password 使用。
type trojanAdapter struct{}

func (trojanAdapter) Protocol() string { return config.ProtocolTrojan }

func (trojanAdapter) BuildClient(user xboard.User, bridge config.Bridge) xui.ClientSettings {
	return xui.ClientSettings{
		Email:      MakeEmail(user.UUID, bridge.XboardNodeID),
		Enable:     true,
		Password:   user.UUID,
		ExpiryTime: expiryFromUser(user),
		TotalGB:    totalGBFromTransfer(user),
		LimitIP:    user.DeviceLimit,
	}
}

func (trojanAdapter) KeyOf(client xui.ClientSettings) string { return client.Password }

// shadowsocksAdapter 适配 Shadowsocks 协议。
//
// 关键差异：3x-ui 对 SS client 的更新查找以 email 为主键（详见
// web/controller/inbound.go updateInboundClient 与 SS client model）。
// 因此 KeyOf 必须返回 email，而非 password；这一点与 Trojan / Hysteria 不同。
//
// 兼容性边界：
//
//	旧式 method（aes-256-gcm 等）：每 client password 直接用 uuid。
//	Shadowsocks 2022（method=2022-blake3-*）：3x-ui 期望 client.password 是 user_key
//	（uuidToBase64 后的 16/32 字节 base64），inbound 级配置 method + server_key。
//	当前阶段不做自动转换，详见 README "限制与边界"。
type shadowsocksAdapter struct{}

func (shadowsocksAdapter) Protocol() string { return config.ProtocolShadowsocks }

func (shadowsocksAdapter) BuildClient(user xboard.User, bridge config.Bridge) xui.ClientSettings {
	return xui.ClientSettings{
		Email:      MakeEmail(user.UUID, bridge.XboardNodeID),
		Enable:     true,
		Password:   user.UUID,
		ExpiryTime: expiryFromUser(user),
		TotalGB:    totalGBFromTransfer(user),
		LimitIP:    user.DeviceLimit,
	}
}

func (shadowsocksAdapter) KeyOf(client xui.ClientSettings) string { return client.Email }

// hysteriaV1Adapter 适配 Hysteria v1 协议（3x-ui 端 protocol="hysteria"）。
//
// Hysteria v1 客户端配置使用 auth_str（即 client.auth）字段；与 Trojan / SS 不同，
// 这里的密码不写在 password 字段里。3x-ui updateClient 端按 client.auth 查找。
type hysteriaV1Adapter struct{}

func (hysteriaV1Adapter) Protocol() string { return config.ProtocolHysteria }

func (hysteriaV1Adapter) BuildClient(user xboard.User, bridge config.Bridge) xui.ClientSettings {
	return xui.ClientSettings{
		Email:      MakeEmail(user.UUID, bridge.XboardNodeID),
		Enable:     true,
		Auth:       user.UUID,
		ExpiryTime: expiryFromUser(user),
		TotalGB:    totalGBFromTransfer(user),
		LimitIP:    user.DeviceLimit,
	}
}

func (hysteriaV1Adapter) KeyOf(client xui.ClientSettings) string { return client.Auth }

// hysteriaV2Adapter 适配 Hysteria v2 协议（3x-ui 端 protocol="hysteria2"）。
//
// 与 v1 一致：使用 auth 字段；保留独立 adapter 是为了未来 v2 引入差异化字段
// （如 obfs.password 透传）时可分支独立演进，避免 if/else 链。
type hysteriaV2Adapter struct{}

func (hysteriaV2Adapter) Protocol() string { return config.ProtocolHysteria2 }

func (hysteriaV2Adapter) BuildClient(user xboard.User, bridge config.Bridge) xui.ClientSettings {
	return xui.ClientSettings{
		Email:      MakeEmail(user.UUID, bridge.XboardNodeID),
		Enable:     true,
		Auth:       user.UUID,
		ExpiryTime: expiryFromUser(user),
		TotalGB:    totalGBFromTransfer(user),
		LimitIP:    user.DeviceLimit,
	}
}

func (hysteriaV2Adapter) KeyOf(client xui.ClientSettings) string { return client.Auth }
