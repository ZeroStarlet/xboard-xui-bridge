# xboard-xui-bridge

非侵入式中间件：让 [Xboard](https://github.com/cedar2025/Xboard) 面板与 [3x-ui](https://github.com/MHSanaei/3x-ui) 无缝对接，**无需修改任何一方源码**，亦无需在节点机部署单独的"原生"后端（XrayR / V2bX）。

## 特性

- **协议覆盖**：Shadowsocks / VMess / VLESS / Trojan / Hysteria v1 / Hysteria v2。
- **高可靠遥测**：差量上报 + 本地 SQLite 持久化基线 + 失败重传 + 重置补偿。**流量不会丢失**；进程重启 + 流量重置 + push 失败任意叠加场景都能恢复。**重复计费仅在"push 成功 + 本地 SQLite 写入失败"这一极小窗口内可能发生一次**（Xboard `/push` 协议层无幂等键，工程上无法在中间件单方消除——详见"限制与边界"）。
- **多桥接**：单实例可承载多组 (Xboard 节点 ↔ 3x-ui inbound) 映射，互不干扰。
- **运维友好**：单二进制（`go build`），无 cgo / 系统级 SQLite 依赖。
- **零侵入**：仅通过两边公开的 HTTP API 交互，无需登录数据库、无需修改面板代码。

## 工作原理

```
                    ┌────────────────────┐
                    │   Xboard 面板      │
                    │ /api/v2/server/... │
                    └────────┬───────────┘
                             │  HTTP（token 鉴权）
                  ┌──────────┴──────────┐
                  │ xboard-xui-bridge   │ ← 进程
                  │ ┌─────────────────┐ │
                  │ │ user_sync       │ │ 拉用户 → diff → addClient/updateClient/del
                  │ │ traffic_sync    │ │ 拉流量 → 差量 → push（失败重传）
                  │ │ alive_sync      │ │ 拉在线 IP → push
                  │ │ status_sync     │ │ 拉节点状态 → push
                  │ └─────────────────┘ │
                  │   本地 SQLite       │ ← 流量基线持久化
                  └──────────┬──────────┘
                             │  HTTP（Bearer Token 鉴权）
                    ┌────────┴───────────┐
                    │   3x-ui 面板       │
                    │ /panel/api/...     │
                    └────────────────────┘
```

## 适配模型

中间件假设 **管理员预先在 3x-ui 后台手工创建好 inbound**（含端口、协议、TLS、Reality 等敏感字段）。

中间件只对 inbound 内的 client 做 add/update/del；inbound 元数据由管理员维护。这种分工：

- **降低耦合**：协议字段（端口、证书、Reality 私钥）的真相源在 3x-ui，无需复杂跨面板字段映射。
- **职责清晰**：协议参数变更（如换证书）由 3x-ui 完成；用户管理（uuid、限速、设备数）由 Xboard 控制；中间件只做"把 Xboard 的用户清单 → 3x-ui 的 client 数组"的同步。

## 快速开始

### 1. 准备工作

| 系统 | 准备项 |
| --- | --- |
| Xboard | 在系统设置中拿到 `server_token`；为该节点分配数字 ID。 |
| 3x-ui  | 在面板设置中生成 API Token（推荐）或准备 admin 凭据；在该节点机预创建 inbound 拿到数字 ID。 |

### 2. 下载或编译

```bash
# 编译当前平台
make build

# 交叉编译 Linux x86_64
make build-linux
```

二进制产出到 `dist/`。

### 3. 配置

复制 `configs/config.example.yaml` 为 `config.yaml` 并按注释填写。最小可运行配置：

```yaml
state:
  database: ./bridge.db

xboard:
  api_host: https://panel.example.com
  token: your-server-token

xui:
  api_host: http://127.0.0.1:2053
  auth_mode: token
  api_token: your-48-char-api-token

bridges:
  - name: hk-vless-01
    xboard_node_id: 11
    xui_inbound_id: 1
    protocol: vless
    flow: xtls-rprx-vision
    enable: true

reporting:
  alive_enabled: true
  status_enabled: true
```

> **流量倍率说明**：中间件不再提供 `traffic_rate` 配置项；倍率统一在 Xboard 后台节点配置 `v2_server.rate` 中维护，由 Xboard `TrafficFetchJob` 自动乘算。

### 4. 运行

```bash
./xboard-xui-bridge --config ./config.yaml
```

或通过 systemd / Docker / 1Panel 等托管。Docker 用法：

```bash
docker build -t xboard-xui-bridge:latest .
docker run -d --name bridge \
  -v "$(pwd)/config.yaml:/app/config.yaml:ro" \
  -v "$(pwd)/data:/app/data" \
  xboard-xui-bridge:latest
```

## 配置参数

完整字段说明请见 [`configs/config.example.yaml`](configs/config.example.yaml)。每个字段的语义、默认值、合法取值都在示例文件注释中标注。

YAML 解析启用 `KnownFields(true)`：拼写错误会让中间件**启动失败**而非静默回落默认值，便于运维快速定位问题。

## 设计取舍

### 为什么差量上报而非全量？

全量快照对比模型每次都把 3x-ui 端 cur 全量推给 Xboard，逻辑简单但有两个风险：

1. **重启漂移**：进程重启后无基线，第一次推送会把全部历史累计值再次上报，造成双重计费。
2. **重置歧义**：3x-ui 流量周期性重置时，全量上报无法区分"重置后的小数"是新数据还是回退；差量模型有 `cur < reported` 检测分支可以识别。

差量模型把 baseline 落到本地 SQLite，每周期单调递增，重启即恢复，已被广泛验证。

中间件采用三字段持久化模型：`reported_up/down`、`last_seen_up/down`、`pending_up/down`。任何一次同步循环都先在事务中 `RecordSeen`（写 last_seen，重置时把 `last_seen − reported` 累加进 pending 并归零 reported），然后构造 `delta = (cur − reported) + pending` 上报；上报成功才推进 reported = cur 并清零 pending。这保证 "push 失败 + 流量重置" 复合场景下完全无丢失。

### 为什么不直接读 SQLite？

参考的早期方案 [xboard2xui](../xboard2xui) 直接 sqlite 读写，性能尚可但有几个致命问题：

- **JSON 字段并发腐烂**：3x-ui inbound.settings 是 JSON blob，并发改动易导致数据丢失。
- **无事务保障**：库级争抢导致 `SQLITE_BUSY`，整轮同步失败。
- **大用户量面板卡死**：面板自身也持续读写同一数据库。

切换到 HTTP API 后，并发安全由 3x-ui 业务层保证，且天然可观测（HTTP 日志）、可灰度（不同实例上对接不同节点）。

### 为什么 SS-2022 没有自动适配？

3x-ui 端 Shadowsocks 2022 模式要求 inbound 级 method/server_key + client 级 user_key（base64 16/32 字节）。Xboard `/user` 端点返回的是裸 uuid，不是已转换的 user_key。

完整支持需要：

1. 中间件读取 inbound.method 判断是否 2022 模式；
2. 把 uuid 经 `uuidToBase64` 转换为 user_key。

但这要求中间件解析 inbound 协议级配置，违背了"协议字段真相源在 3x-ui"的约定。当前选择：让运维在使用 Shadowsocks 时**避开 2022 算法**（使用 aes-256-gcm 等），由 3x-ui 端配置 method、客户端 password 直接使用 uuid。后续若有需求，可在 `internal/protocol/protocols.go` 的 `shadowsocksAdapter` 中扩展。

## 限制与边界

- **节点协议级字段不同步**：端口、TLS、Reality 等由管理员在 3x-ui 维护；中间件只同步 client。
- **`Push` 端点缺少 idempotency key**：Xboard `/api/v2/server/push` 当前不接受幂等键。极小概率下"push 成功但本地 baseline 写入失败"会导致单次重复计费；该窗口的概率 ≈ 本地 SQLite 单次写失败率（接近 0）。彻底消除需要 Xboard 端在协议层引入 idempotency key，超出中间件单方可控的范围。**流量丢失方向已通过 `last_seen + pending` 字段得到根除**（重置 + push 失败叠加场景下也不会丢失）。
- **alive 上报性能**：`GetClientIPs` 是逐 email 串行接口；inbound 内在线用户多时延迟会显著（已限并发 8 路）。
- **3x-ui 鉴权仅支持 Bearer Token**：早期版本计划支持 cookie 登录，但现代 3x-ui 对浏览器路由（含 `/login` 与 `/panel/api` 的 cookie 调用）启用 CSRF 校验，需要先 `GET /csrf-token` 再带 `X-CSRF-Token`，且 token 与 session 绑定，刷新策略复杂；权衡后统一收敛为 token 模式。请在 3x-ui 后台一键生成 48 字符 API Token 并填入 `xui.api_token`。

## 开源协议

本项目以 MIT License 发布；详见 `LICENSE`。
