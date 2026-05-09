# xboard-xui-bridge

非侵入式中间件：让 [Xboard](https://github.com/cedar2025/Xboard) 面板与 [3x-ui](https://github.com/MHSanaei/3x-ui) 无缝对接，**无需修改任何一方源码**，亦无需在节点机部署单独的"原生"后端（XrayR / V2bX）。v0.2 起内置 Web GUI 控制面板，运维通过浏览器即可完成参数配置与桥接管理。

## 特性

- **协议覆盖**：Shadowsocks / VMess / VLESS / Trojan / Hysteria v1 / Hysteria v2。
- **内置 Web 面板**：Vue 3 + Tailwind 单页应用，通过 `go:embed` 打入二进制；登录后即可维护 Xboard / 3x-ui 凭据、桥接清单、运行参数。
- **运行期热重载**：`Supervisor` 在 settings / bridges 修改后全停全启同步引擎，无需重启进程。Xboard / 3x-ui 凭据 / 同步周期 / 上报开关与桥接清单修改即时生效；启动期参数 `log.*` 与 `web.*`（监听地址、会话寿命、日志级别等）需重启进程——Web 面板会显式拒绝这些字段的 PATCH 并提示运维。
- **高可靠遥测**：差量上报 + 本地 SQLite 持久化基线 + 失败重传 + 重置补偿。**流量不会丢失**；进程重启 + 流量重置 + push 失败任意叠加场景都能恢复。**重复计费仅在"push 成功 + 本地 SQLite 写入失败"这一极小窗口内可能发生一次**（Xboard `/push` 协议层无幂等键，工程上无法在中间件单方消除——详见"限制与边界"）。
- **多桥接**：单实例可承载多组 (Xboard 节点 ↔ 3x-ui inbound) 映射，互不干扰。
- **运维友好**：单二进制（`go build` 后 ~25 MB，含前端），无 cgo / 系统级 SQLite 依赖。
- **零侵入 + 零 yaml**：仅通过两边公开的 HTTP API 交互；自身配置全部在 SQLite + Web GUI 中维护，不需要任何 yaml/json 配置文件。

## 工作原理

```
                    ┌────────────────────┐
                    │   Xboard 面板      │
                    │ /api/v2/server/... │
                    └────────┬───────────┘
                             │  HTTP（token 鉴权）
                  ┌──────────┴───────────┐
                  │ xboard-xui-bridge    │ ← 进程
                  │ ┌──────────────────┐ │
                  │ │ Web 面板 :8787   │ │ Vue 3 SPA + bcrypt 鉴权
                  │ │ Supervisor       │ │ 引擎热重载控制器
                  │ │ ┌──────────────┐ │ │
                  │ │ │ user_sync    │ │ │ 拉用户 → diff → addClient/del
                  │ │ │ traffic_sync │ │ │ 拉流量 → 差量 → push
                  │ │ │ alive_sync   │ │ │ 拉在线 IP → push
                  │ │ │ status_sync  │ │ │ 拉节点状态 → push
                  │ │ └──────────────┘ │ │
                  │ └──────────────────┘ │
                  │   本地 SQLite        │ ← settings / bridges /
                  │                      │   admin_users / sessions /
                  │                      │   traffic_baseline
                  └──────────┬───────────┘
                             │  HTTP（Bearer Token 鉴权）
                    ┌────────┴────────────┐
                    │   3x-ui 面板        │
                    │ /panel/api/...      │
                    └─────────────────────┘
```

## 适配模型

中间件假设 **管理员预先在 3x-ui 后台手工创建好 inbound**（含端口、协议、TLS、Reality 等敏感字段）。

中间件只对 inbound 内的 client 做 add/update/del；inbound 元数据由管理员维护。这种分工：

- **降低耦合**：协议字段（端口、证书、Reality 私钥）的真相源在 3x-ui，无需复杂跨面板字段映射。
- **职责清晰**：协议参数变更（如换证书）由 3x-ui 完成；用户管理（uuid、限速、设备数）由 Xboard 控制；中间件只做"把 Xboard 的用户清单 → 3x-ui 的 client 数组"的同步。

## 一键安装（Linux）

`install.sh` 会自动完成：检测架构（amd64 / arm64 / armv7）→ 下载最新 Release 二进制 → 安装到 `/usr/local/bin/xboard-xui-bridge` → 创建数据目录 `/usr/local/xboard-xui-bridge/data` → 写 systemd unit 并启动 → 输出首次随机生成的 admin 密码。

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/ZeroStarlet/xboard-xui-bridge/main/install.sh)
```

二次执行视作"升级"——保留 `data/`，覆盖 `/usr/local/bin/xboard-xui-bridge`，重启服务。

服务管理：

```bash
systemctl status xboard-xui-bridge      # 查看运行状态
systemctl restart xboard-xui-bridge     # 重启服务
systemctl stop xboard-xui-bridge        # 停止服务
journalctl -u xboard-xui-bridge -f      # 实时查看日志
```

完整卸载：

```bash
systemctl stop xboard-xui-bridge
systemctl disable xboard-xui-bridge
rm -f /etc/systemd/system/xboard-xui-bridge.service
rm -f /usr/local/bin/xboard-xui-bridge
rm -rf /usr/local/xboard-xui-bridge   # 含数据库；如需保留请先备份
systemctl daemon-reload
```

## 手工编译

### 1. 准备工作

| 系统 | 准备项 |
| --- | --- |
| Xboard | 在系统设置中拿到 `server_token`；为该节点分配数字 ID。 |
| 3x-ui  | 在面板设置中生成 API Token（48 字符）；在该节点机预创建 inbound 拿到数字 ID。 |

### 2. 下载或编译

```bash
# 端到端编译：先构建 Vue 前端，再把 dist 嵌入到 Go 二进制
make build-all

# 或仅编译 Go（如果 internal/web/dist 已经是最新前端）
make build

# 交叉编译 Linux x86_64
make build-linux
```

二进制产出到 `dist/`。

> **本地开发** 用 `cd web && npm run dev`（Vite dev server 5173 端口）+ 另一终端 `make build && ./dist/xboard-xui-bridge`（Go 后端 8787 端口）。Vite 已配置代理 `/api → 127.0.0.1:8787`，前端调试体验流畅。

### 3. 启动

```bash
# 默认数据库路径 ./data/bridge.db；不存在时进程会自动创建父目录与表
./xboard-xui-bridge

# 指定其它路径
./xboard-xui-bridge run --db /var/lib/xboard-bridge/state.db
```

首次启动会：

1. 自动建库并执行 schema（settings / bridges / admin_users / sessions / traffic_baseline）；
2. 检测到 admin_users 表为空时生成默认 admin 账户，**随机 24 字符密码同时写入**：
   - 进程日志（INFO 级，可在 stdout / log 文件查看）；
   - `data/initial_password.txt`（0600 权限，导出后建议手工删除）；
3. 启动 Web 面板（默认 `127.0.0.1:8787`）+ 同步引擎（首次无 bridge 时空载等待）；
4. 浏览器打开 `http://<host>:8787` 用 admin + 上述随机密码登录，进入"运行参数"填 Xboard / 3x-ui 凭据，再到"桥接管理"添加桥接——保存后引擎自动热重载。

> **首次密码丢失**：`xboard-xui-bridge reset-password --user admin` 通过本地 shell 兜底重置。需要本地 shell 访问权限——这是有意为之的"本地信任"边界，与 redis-cli AUTH 重置同等级。

### 4. 容器化

```bash
# 准备 host 数据目录并赋写入权限（容器内非 root 用户 bridge 需要写入）
mkdir -p data && chmod 777 data

docker build -t xboard-xui-bridge:latest .
docker run -d --name bridge \
  -v "$(pwd)/data:/app/data" \
  -p 127.0.0.1:8787:8787 \
  xboard-xui-bridge:latest

# 首次启动后查看日志拿 admin 密码
docker logs bridge | grep -A1 "已生成初始 admin 账户"
```

要点：

- **挂载 `/app/data` 必须**：数据库、首次密码文件、未来日志都集中在该目录；不挂载会让容器重启丢数据。
- **bind mount 权限**：容器内进程以非 root 用户 `bridge` 运行；host 目录需对该 uid 可写。最简单的方式是 `chmod 777 ./data`（仅本机调试可接受）；生产建议查询镜像中 `bridge` 的 uid（`docker run --rm xboard-xui-bridge:latest id`）后 `chown` 给对应 uid。
- **监听地址**：镜像通过 `ENV BRIDGE_LISTEN_ADDR=:8787` 让进程默认绑定全部网卡；`-p 127.0.0.1:8787:8787` 把面板暴露到宿主 8787——仅宿主可访问。要外网访问请配合 nginx 反代 + TLS 终结（Web 面板会自动识别 `X-Forwarded-Proto: https` 设置 cookie Secure）。
- **裸跑（非容器）**：默认监听 `127.0.0.1:8787`（更安全），无需该环境变量；如需绑定全部网卡请通过 `BRIDGE_LISTEN_ADDR=:8787 ./xboard-xui-bridge` 启动，或在 settings 表中修改 `web.listen_addr` 后重启。

## CLI 子命令

```
xboard-xui-bridge                       默认（等价于 run）
xboard-xui-bridge run [--db <path>]     启动后台守护 + Web 面板
xboard-xui-bridge reset-password [--db <path>] [--user admin]
                                        本地 shell 兜底重置密码（两次确认）
xboard-xui-bridge version               打印版本号
xboard-xui-bridge --version             同上
xboard-xui-bridge help                  打印帮助
```

## 配置字段

字段语义、默认值、Settings 表的 key 命名见 [`internal/config/config.go`](internal/config/config.go) 中 `Setting*` 常量与 `Validate` 函数注释。Web 面板按字段分组展示（Xboard / 3x-ui / 同步周期 / 上报开关 / 日志 / Web）。

> **重启生效字段**：`log.*`（level / file / max_size_mb / max_backups / max_age_days）与 `web.*`（listen_addr / session_max_age_hours / absolute_max_lifetime_hours）这两组字段在进程启动时被 `logger.New` / `auth.NewService` / `http.Server` 一次性消费，运行期不可热修改——Web 面板会显式拒绝 PATCH 这些字段，并提示运维通过 `sqlite3 ./data/bridge.db` 直接编辑 settings 表后重启进程。其余字段（Xboard / 3x-ui / 同步周期 / 上报开关 / bridges 全部 CRUD）都支持运行期热重载。

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

### 为什么从 yaml 转向 SQLite + Web GUI？

v0.1 通过 `config.yaml` 维护配置；v0.2 起完全废弃 yaml，迁移到 SQLite + Web GUI。原因：

- **降低部署门槛**：运维通过浏览器图形化操作即可完成桥接对接，不必学习 yaml 字段语义；
- **运行期热重载**：yaml 修改需要重启进程；SQLite 配合 `Supervisor.Reload` 可在无中断下切换桥接清单；
- **配置一致性**：yaml + Web GUI 双源真相会让运维困惑"我改了 yaml 为何 GUI 没变"；统一存储消除歧义；
- **向前兼容**：v0.1 用户的 traffic_baseline 数据在 v0.2 仍可用，配置需运维通过 Web 面板重新填写一次。

### 为什么 SS-2022 没有自动适配？

3x-ui 端 Shadowsocks 2022 模式要求 inbound 级 method/server_key + client 级 user_key（base64 16/32 字节）。Xboard `/user` 端点返回的是裸 uuid，不是已转换的 user_key。

完整支持需要：

1. 中间件读取 inbound.method 判断是否 2022 模式；
2. 把 uuid 经 `uuidToBase64` 转换为 user_key。

但这要求中间件解析 inbound 协议级配置，违背了"协议字段真相源在 3x-ui"的约定。当前选择：让运维在使用 Shadowsocks 时**避开 2022 算法**（使用 aes-256-gcm 等），由 3x-ui 端配置 method、客户端 password 直接使用 uuid。后续若有需求，可在 `internal/protocol/protocols.go` 的 `shadowsocksAdapter` 中扩展。

## 安全模型

- **Web 面板鉴权**：用户名 + 密码 (bcrypt cost=12) + Session Cookie。session token 是 32 字节 crypto/rand，HttpOnly + SameSite=Lax；TLS（直接或经 `X-Forwarded-Proto: https` 反代）下自动加 Secure。
- **CSRF 防御**：所有写操作（POST/PUT/PATCH/DELETE）校验 Origin / Referer 与 Host 同源，配合 SameSite=Lax cookie 双重防御；不引入额外 CSRF token 端点。
- **会话寿命**：滑动续期窗口 7 天 + 绝对寿命上限 30 天（默认）；改密强制下线该用户全部 session（store 层事务保证"密码已改 + session 已删"原子性，无安全窗口）。
- **首次密码生成**：crypto/rand 18 字节 base64url ≈ 24 字符（~108 bits 熵），双通道输出（日志 + 文件 0600 权限）。
- **登录侧信道防御**：用户不存在分支也运行一次 bcrypt 校验，让"用户存在但密码错"与"用户不存在"耗时一致，避免侧信道枚举用户名。

## 限制与边界

- **节点协议级字段不同步**：端口、TLS、Reality 等由管理员在 3x-ui 维护；中间件只同步 client。
- **`Push` 端点缺少 idempotency key**：Xboard `/api/v2/server/push` 当前不接受幂等键。极小概率下"push 成功但本地 baseline 写入失败"会导致单次重复计费；该窗口的概率 ≈ 本地 SQLite 单次写失败率（接近 0）。彻底消除需要 Xboard 端在协议层引入 idempotency key，超出中间件单方可控的范围。**流量丢失方向已通过 `last_seen + pending` 字段得到根除**（重置 + push 失败叠加场景下也不会丢失）。
- **alive 上报性能**：`GetClientIPs` 是逐 email 串行接口；inbound 内在线用户多时延迟会显著（已限并发 8 路）。
- **3x-ui 鉴权仅支持 Bearer Token**：早期版本计划支持 cookie 登录，但现代 3x-ui 对浏览器路由（含 `/login` 与 `/panel/api` 的 cookie 调用）启用 CSRF 校验，需要先 `GET /csrf-token` 再带 `X-CSRF-Token`，且 token 与 session 绑定，刷新策略复杂；权衡后统一收敛为 token 模式。请在 3x-ui 后台一键生成 48 字符 API Token 并填入 `xui.api_token`。
- **启动期定型字段**：`log.*`（level / file / max_size_mb / max_backups / max_age_days）与 `web.*`（listen_addr / session_max_age_hours / absolute_max_lifetime_hours）这两组在进程启动时被消费一次；运行期 PATCH 会被 Web Handler 显式拒绝并提示运维 `sqlite3 编辑 settings 表后重启`。其余字段（Xboard / 3x-ui / 同步周期 / 上报开关 / bridges 全部 CRUD）支持热重载。
- **删除桥接的清理**：当前删除 bridge 时不会清理对应的 traffic_baseline 行（占用极少，<1KB / 用户）；v0.3 计划增加 `store.DeleteBaselinesByBridge` 一次性清理。

## 项目结构

```
.
├── cmd/bridge/main.go           # 进程入口 + CLI 子命令分发
├── internal/
│   ├── auth/                    # bcrypt + session 鉴权服务
│   ├── config/                  # 从 SQLite 加载配置 + Validate
│   ├── logger/                  # slog 包装
│   ├── protocol/                # 6 个协议适配器（VLESS / VMess / Trojan / SS / Hysteria v1/v2）
│   ├── store/                   # SQLite 持久层（5 张表：traffic_baseline / settings / bridges / admin_users / sessions）
│   ├── supervisor/              # 引擎热重载控制器（全停全启 + draining 槽位）
│   ├── sync/                    # 同步引擎 + 4 类周期 worker
│   ├── web/                     # HTTP 路由 + handlers + go:embed dist
│   ├── xboard/                  # Xboard /api/v2/server/* 客户端
│   └── xui/                     # 3x-ui /panel/api/inbounds/* 客户端
├── web/                         # Vue 3 + Vite + Tailwind SPA 源码（dev 用）
│   ├── src/
│   │   ├── views/               # 5 个页面（Login / Dashboard / Bridges / Settings / Account）
│   │   ├── components/          # 共享组件（AppNav 等）
│   │   ├── stores/auth.ts       # Pinia 鉴权状态
│   │   ├── api/client.ts        # fetch 封装 + 全局 401 handler
│   │   └── router/              # vue-router + 守卫
│   └── dist/                    # 本地 npm run build 输出（不入仓）
├── Dockerfile                   # 多阶段：node 构前端 → go 嵌入构后端 → alpine 运行
├── Makefile                     # build / build-all / web / build-linux / clean / ...
├── README.md                    # 本文件
├── CONTRIBUTING.md              # 贡献规范
└── LICENSE                      # MIT
```

## 开源协议

本项目以 MIT License 发布；详见 `LICENSE`。
