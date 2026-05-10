<div align="center">

# xboard-xui-bridge

**[Xboard](https://github.com/cedar2025/Xboard) 与 [3x-ui](https://github.com/MHSanaei/3x-ui) 的非侵入式中间件**

无需修改任何一方源码，无需在节点机部署 XrayR / V2bX——一个二进制即可让两套面板无缝对接。

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go)](go.mod)
[![Vue Version](https://img.shields.io/badge/Vue-3-4FC08D?logo=vue.js)](web/package.json)
[![Platform](https://img.shields.io/badge/platform-Linux%20%7C%20macOS%20%7C%20Windows-lightgrey)](#手工编译)

</div>

---

## 项目简介

`xboard-xui-bridge` 解决一个具体问题：你已经在用 **Xboard** 做销售面板（订阅、用户、套餐、计费），同时又选定 **3x-ui** 作为节点端控制面板（多协议、Reality、TLS 灵活）——但这两个面板原生不互通。

主流方案要么改 Xboard 源码，要么在节点机上跑额外的 XrayR/V2bX 守护让 Xboard 把节点当 V2bX 来管。前者破坏升级路径，后者增加节点机维护成本。本项目选择第三条路：**在中间机器（也可以与 Xboard 同机）跑一个翻译器进程**，通过两端公开的 HTTP API 双向同步——不动任何一方源码，不在节点机加进程。

自 v0.2 起内置 Web 控制面板，配置全部走浏览器，无需 yaml/json；自 v0.5 起一键脚本提供完整的安装 / 卸载 / 启停 / 日志 / 重置密码菜单，运维体验对齐主流面板项目。

---

## 核心特性

| 类别 | 能力 |
| --- | --- |
| **协议覆盖** | Shadowsocks / VMess / VLESS / Trojan / Hysteria v1 / Hysteria v2 |
| **零侵入** | 仅通过两边公开的 HTTP API 交互；不改任何源码、不在节点机部署额外进程 |
| **零 yaml** | 配置全部维护于 SQLite + Web GUI，浏览器登录即可增删桥接 |
| **运行期热重载** | Supervisor 在 settings/bridges 修改后全停全启同步引擎，无需重启进程 |
| **高可靠遥测** | 差量上报 + 本地 SQLite 持久化基线 + 失败重传 + 重置补偿；通过 `last_seen + pending` 在重置 / 失败叠加场景下避免流量丢失。v0.6 起严格遵守"单一正向路径"承诺：无真实差量则不 push，不再发送 `[0, 0]` 占位心跳维持节点状态——空闲节点交给 Xboard 端按其原生判定逻辑显示 `STATUS_ONLINE_NO_PUSH`，让运维看到的状态等于真实数据 |
| **多桥接** | 单实例可承载多组 (Xboard 节点 ↔ 3x-ui inbound) 映射，互不干扰 |
| **单二进制** | `go build` 后约 25 MB（含 Vue 前端），无 cgo / 无系统级 SQLite 依赖 |
| **Web 面板** | Vue 3 + Tailwind 单页应用，bcrypt + Session Cookie 鉴权，CSRF 防御内置 |
| **运维友好** | `xui-bridge` 快捷命令一键进入交互菜单（启停 / 日志 / 重置密码 / 改监听 / 卸载） |

---

## 工作原理

```text
                    ┌────────────────────┐
                    │   Xboard 面板      │
                    │ /api/v2/server/... │
                    └────────┬───────────┘
                             │  HTTP（server_token 鉴权）
                  ┌──────────┴───────────┐
                  │ xboard-xui-bridge    │ ← 进程
                  │ ┌──────────────────┐ │
                  │ │ Web 面板 :8787   │ │ Vue 3 SPA + bcrypt 鉴权
                  │ │ Supervisor       │ │ 引擎热重载控制器
                  │ │ ┌──────────────┐ │ │
                  │ │ │ user_sync    │ │ │ 拉用户 → diff → addClient/del
                  │ │ │ traffic_sync │ │ │ 拉流量 → 差量 → push（无差量不 push）
                  │ │ │ alive_sync   │ │ │ 拉在线 IP → push
                  │ │ │ status_sync  │ │ │ 拉节点状态 → push
                  │ │ └──────────────┘ │ │
                  │ └──────────────────┘ │
                  │   本地 SQLite        │ ← settings / bridges /
                  │                      │   admin_users / sessions /
                  │                      │   traffic_baseline
                  └──────────┬───────────┘
                             │  HTTP（Bearer API Token 鉴权）
                    ┌────────┴────────────┐
                    │   3x-ui 面板        │
                    │ /panel/api/...      │
                    └─────────────────────┘
```

### 适配模型

中间件假设 **管理员预先在 3x-ui 后台手工创建好 inbound**（含端口、协议、TLS、Reality 等敏感字段）。

中间件只对 inbound 内的 client 做 add/update/del；inbound 元数据由管理员维护。这种分工：

- **降低耦合**：协议字段（端口、证书、Reality 私钥）的真相源在 3x-ui，无需复杂跨面板字段映射。
- **职责清晰**：协议参数变更（如换证书）由 3x-ui 完成；用户管理（uuid、限速、设备数）由 Xboard 控制；中间件只做"把 Xboard 的用户清单 → 3x-ui 的 client 数组"的同步。

---

## 快速开始

### 一键安装（Linux）

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/ZeroStarlet/xboard-xui-bridge/main/install.sh)
```

脚本会自动完成：检测架构（amd64 / arm64 / armv7） → 下载最新 Release 二进制（**可用时**校验 `SHA256SUMS.txt`，校验不匹配立即中止；缺校验文件或缺 `sha256sum` 工具时仅 WARN 跳过）→ 安装到 `/usr/local/bin/xboard-xui-bridge` → 创建数据目录 `/usr/local/xboard-xui-bridge/data` → 写 systemd unit 并启动 → 注册 `xui-bridge` 快捷命令 → 输出首次随机生成的 admin 密码。

二次执行：未安装时进入安装流程，已安装时进入交互菜单。

### `xui-bridge` 管理命令

安装完成后，运维直接用 `xui-bridge` 进交互菜单管理服务，覆盖所有高频运维动作：

```text
============================================================
  xboard-xui-bridge 管理菜单
============================================================
  服务状态：● 运行中

   1. 安装 / 升级
   2. 启动服务
   3. 停止服务
   4. 重启服务
   5. 查看运行状态（systemctl status）
   6. 查看最近 200 行日志
   7. 实时跟踪日志（journalctl -f）
   8. 重置 admin 密码
   9. 修改 Web 监听地址
  10. 卸载（保留数据）
  11. 彻底清理（含数据，不可恢复）
   0. 退出
```

也可以子命令直跑（常用示例；完整子命令清单请运行 `xui-bridge help`）：

```bash
xui-bridge install              # 安装 / 升级到最新版（别名：update / upgrade）
xui-bridge start                # 启动服务
xui-bridge stop                 # 停止服务
xui-bridge restart              # 重启服务
xui-bridge status               # 查看 systemctl 状态
xui-bridge log                  # 打印最近 200 行日志（别名：logs）
xui-bridge follow               # 实时跟踪日志（别名：log-follow）
xui-bridge reset-password       # 忘密码兜底（别名：reset / password）
xui-bridge change-listen-addr   # 改监听地址（别名：change-listen / listen）
xui-bridge uninstall            # 卸载，保留 data/（别名：remove）
xui-bridge purge                # 完全清理，含 data/（别名：clean，要求输入 PURGE 确认）
xui-bridge menu                 # 显式进入交互菜单
xui-bridge help                 # 完整帮助
```

### 准备工作

| 系统 | 准备项 |
| --- | --- |
| **Xboard** | 在系统设置中拿到 `server_token`；为该节点分配数字 ID。 |
| **3x-ui** | **必须使用 v3.0.0 或更新版本**。在 3x-ui 后台「面板设置 → API 令牌」点击「重新生成」拿到 48 字符 API Token；在该节点机预创建 inbound 拿到数字 ID。v0.6 起本中间件仅支持 Bearer Token 鉴权，账号密码 / cookie / TOTP 路径已彻底移除——v3.0.0 之前的 3x-ui 版本不再兼容。 |
| **Xboard 队列** | **必须** 运行 `php artisan queue:work` 或 Horizon——Xboard 端真正写库的是 `TrafficFetchJob` 异步队列任务，不跑 worker 的话中间件 push 永远成功但用户 u/d 永远 0（详见"限制与边界"）。 |

### 首次登录

安装脚本会输出 admin 用户名 + **初始随机密码**（同时写入 `/usr/local/xboard-xui-bridge/data/initial_password.txt`）。浏览器打开 `http://<server-ip>:8787` 登录后：

1. 立即修改密码
2. 进入"运行参数"填 Xboard / 3x-ui 凭据
3. 进入"桥接管理"添加桥接（Xboard 节点 ↔ 3x-ui inbound 一对一映射）
4. 保存后引擎自动热重载

---

## 手工编译

### 1. 端到端编译

```bash
# 完整编译：先构建 Vue 前端 → 嵌入 Go 二进制
make build-all

# 仅编译 Go（要求 internal/web/dist 已是最新前端）
make build

# 交叉编译 Linux x86_64
make build-linux
```

二进制产出到 `dist/`。

### 2. 本地开发

```bash
# 终端 1：Vite dev server (5173)
cd web && npm run dev

# 终端 2：Go 后端 (8787)
make build && ./dist/xboard-xui-bridge
```

Vite 已配置代理 `/api → 127.0.0.1:8787`，前端调试体验流畅。

### 3. 启动

```bash
# 默认数据库路径 ./data/bridge.db
./xboard-xui-bridge

# 指定其它路径
./xboard-xui-bridge run --db /var/lib/xboard-bridge/state.db
```

首次启动行为：

1. 自动建库并执行 schema（settings / bridges / admin_users / sessions / traffic_baseline）
2. admin 表为空时生成默认账户，**24 字符随机密码** 同时写入：
   - 进程日志（INFO 级）
   - `data/initial_password.txt`（0600 权限）
3. 启动 Web 面板（默认 `127.0.0.1:8787`）+ 同步引擎
4. 浏览器登录后填凭据，引擎自动热重载

> **首次密码丢失** ：执行 `xboard-xui-bridge reset-password --user admin`（或菜单第 8 项）通过本地 shell 兜底重置——这与 redis-cli AUTH 重置同等"信任本地"模型。

### 4. 容器化

```bash
mkdir -p data && chmod 777 data

docker build -t xboard-xui-bridge:latest .
docker run -d --name bridge \
  -v "$(pwd)/data:/app/data" \
  -p 127.0.0.1:8787:8787 \
  xboard-xui-bridge:latest

# 拿初始 admin 密码
docker logs bridge | grep -A1 "已生成初始 admin 账户"
```

要点：

- **挂载 `/app/data` 必须**：数据库、首次密码文件、日志都集中在此；不挂载会让重启丢数据。
- **bind mount 权限**：容器内非 root 用户 `bridge`；host 目录需对该 uid 可写（最简：`chmod 777 ./data`，仅本机调试可接受；生产建议查询镜像 uid 后 `chown`）。
- **监听地址**：镜像默认 `BRIDGE_LISTEN_ADDR=:8787` 绑定全部网卡；`-p 127.0.0.1:8787:8787` 只暴露宿主 8787。外网访问请配 nginx 反代 + TLS（面板自动识别 `X-Forwarded-Proto: https`）。

---

## CLI 子命令

```text
xboard-xui-bridge                       默认（等价于 run）
xboard-xui-bridge run [--db <path>]     启动后台守护 + Web 面板
xboard-xui-bridge reset-password [--db <path>] [--user admin]
                                        本地 shell 兜底重置密码（两次确认）
xboard-xui-bridge version               打印版本号
xboard-xui-bridge --version             同上
xboard-xui-bridge help                  打印帮助
```

---

## 配置字段

字段语义、默认值、Settings 表的 key 命名见 [`internal/config/config.go`](internal/config/config.go) 中 `Setting*` 常量与 `Validate` 函数注释。Web 面板按字段分组展示（Xboard / 3x-ui / 同步周期 / 上报开关 / 日志 / Web）。

> **重启生效字段**：`log.*`（level / file / max_size_mb / max_backups / max_age_days）与 `web.*`（listen_addr / session_max_age_hours / absolute_max_lifetime_hours）这两组字段在进程启动时被 `logger.New` / `auth.NewService` / `http.Server` 一次性消费，**运行期不可热修改**——Web 面板会显式拒绝 PATCH 这些字段，并提示运维改 listen 用 `xui-bridge change-listen-addr`，改 log/session 字段用 `sqlite3 ./data/bridge.db` 直接编辑后重启进程。其余字段（Xboard / 3x-ui / 同步周期 / 上报开关 / bridges 全部 CRUD）支持运行期热重载。

---

## 设计取舍

### 为什么差量上报而非全量？

全量快照对比模型每次都把 3x-ui 端 cur 全量推给 Xboard，逻辑简单但有两个风险：

1. **重启漂移**：进程重启后无基线，第一次推送会把全部历史累计值再次上报，造成双重计费。
2. **重置歧义**：3x-ui 流量周期性重置时，全量上报无法区分"重置后的小数"是新数据还是回退；差量模型有 `cur < reported` 检测分支可以识别。

差量模型把 baseline 落到本地 SQLite，每周期单调递增，重启即恢复，已被广泛验证。

中间件采用三字段持久化模型：`reported_up/down`、`last_seen_up/down`、`pending_up/down`。任何一次同步循环都先在事务中 `RecordSeen`（写 last_seen，重置时把 `last_seen − reported` 累加进 pending 并归零 reported），然后构造 `delta = (cur − reported) + pending` 上报；上报成功才推进 reported = cur 并清零 pending。这保证 "push 失败 + 流量重置" 复合场景下完全无丢失。

### 为什么不直接读 SQLite？

参考的早期方案 `xboard2xui` 直接 sqlite 读写，性能尚可但有几个致命问题：

- **JSON 字段并发腐烂**：3x-ui inbound.settings 是 JSON blob，并发改动易导致数据丢失。
- **无事务保障**：库级争抢导致 `SQLITE_BUSY`，整轮同步失败。
- **大用户量面板卡死**：面板自身也持续读写同一数据库。

切换到 HTTP API 后，并发安全由 3x-ui 业务层保证，且天然可观测（HTTP 日志）、可灰度（不同实例对接不同节点）。

### 为什么从 yaml 转向 SQLite + Web GUI？

v0.1 通过 `config.yaml` 维护配置；v0.2 起完全废弃 yaml，迁移到 SQLite + Web GUI。原因：

- **降低部署门槛**：运维通过浏览器图形化操作即可完成桥接对接，不必学习 yaml 字段语义；
- **运行期热重载**：yaml 修改需要重启进程；SQLite 配合 `Supervisor.Reload` 可在无中断下切换桥接清单；
- **配置一致性**：yaml + Web GUI 双源真相会让运维困惑"我改了 yaml 为何 GUI 没变"；统一存储消除歧义；
- **向前兼容**：v0.1 用户的 traffic_baseline 数据在 v0.2 仍可用，配置需运维通过 Web 面板重新填写一次。

### 为什么 SS-2022 没有自动适配？

3x-ui 端 Shadowsocks 2022 模式要求 inbound 级 method/server_key + client 级 user_key（base64 16/32 字节）。Xboard `/user` 端点返回的是裸 uuid，不是已转换的 user_key。

完整支持需要中间件解析 inbound 协议级配置，违背了"协议字段真相源在 3x-ui"的约定。当前选择：让运维在使用 Shadowsocks 时**避开 2022 算法**（使用 aes-256-gcm 等），由 3x-ui 端配置 method、客户端 password 直接使用 uuid。后续若有需求，可在 `internal/protocol/protocols.go` 的 `shadowsocksAdapter` 中扩展。

### 为什么 v0.6 不再发送无差量的 [0,0] 心跳 push？

v0.5 时代为了避免节点在"5 分钟内无真实流量"时被 Xboard 端
`Server::getAvailableStatusAttribute` 判定为 `STATUS_ONLINE_NO_PUSH`（前端
展示为"无人使用或异常"），曾从 baseline 中任选一个有效 user_id 构造
`{"<id>": [0, 0]}` 占位 push 维持 `STATUS_ONLINE`。这是典型的"失败兜底" /
"降级路径"——它让上层观察者无法区分"节点真的有人在用 + 有差量"与"节点
空闲 + 中间件伪装在上报"两种状态，且额外消耗 Xboard 的 `array_filter` /
`Cache::put` 开销。

v0.6 起严格遵守"单一正向路径"承诺：本周期无任何真实差量 → 不调
`PushTraffic`、直接返回 nil；让 Xboard 按其原生判定逻辑切换节点状态。
对运维的影响：完全空闲（无用户 / 用户无流量）的节点会显示为
`STATUS_ONLINE_NO_PUSH`——这是**真实状态**而非异常。运维若关心节点活跃
显示，应通过：

- 启用 status 子循环（`reporting.status_enabled = true`）维持
  `LAST_LOAD_AT` 活跃（仅刷新负载条，与 `available_status` 判定无关，
  但运维肉眼能看到节点心跳）；
- 让节点真正有用户产生流量（生产环境最自然的活跃信号）；

详见 [`internal/sync/traffic_sync.go`](internal/sync/traffic_sync.go) 文件
头注释关于 v0.6 移除 [0,0] 心跳兜底的设计动机。

---

## 安全模型

- **Web 面板鉴权**：用户名 + 密码 (bcrypt cost=12) + Session Cookie。session token 是 32 字节 crypto/rand，HttpOnly + SameSite=Lax；TLS（直接或经 `X-Forwarded-Proto: https` 反代）下自动加 Secure。
- **CSRF 防御**：所有写操作（POST/PUT/PATCH/DELETE）校验 Origin / Referer 与 Host 同源，配合 SameSite=Lax cookie 双重防御；不引入额外 CSRF token 端点。
- **会话寿命**：滑动续期窗口 7 天 + 绝对寿命上限 30 天（默认）；改密强制下线该用户全部 session（store 层事务保证"密码已改 + session 已删"原子性，无安全窗口）。
- **首次密码生成**：crypto/rand 18 字节 base64url ≈ 24 字符（约 108 bits 熵），双通道输出（日志 + 文件 0600 权限）。
- **登录侧信道防御**：用户不存在分支也运行一次 bcrypt 校验，让"用户存在但密码错"与"用户不存在"耗时一致，避免侧信道枚举用户名。
- **Release 资产校验**：v0.5 起一键脚本在条件满足时校验 `SHA256SUMS.txt`（要求 release 含该文件 + 系统装有 `sha256sum`），校验不匹配立即中止安装；缺文件或缺工具时仅 WARN 跳过，不阻断安装路径。

---

## 限制与边界

- **节点协议级字段不同步**：端口、TLS、Reality 等由管理员在 3x-ui 维护；中间件只同步 client。
- **Xboard 队列 worker 必须运行**：Xboard 端 `UniProxyController::push` 把流量交给 `TrafficFetchJob` 异步队列任务真正写库——**必须运行 `php artisan queue:work` 或 Horizon** 才会消费队列；否则中间件 push 永远成功但 Xboard 用户 u/d 字段永远 0。这是 Xboard 部署侧硬性要求，不在中间件可控范围内。
- **`Push` 端点缺少 idempotency key**：Xboard `/api/v2/server/push` 当前不接受幂等键。极小概率下"push 成功但本地 baseline 写入失败"会导致单次重复计费；该窗口的概率约等于本地 SQLite 单次写失败率（接近 0）。彻底消除需要 Xboard 端在协议层引入 idempotency key，超出中间件单方可控的范围。**流量丢失方向已通过 `last_seen + pending` 字段得到根除**（重置 + push 失败叠加场景下也不会丢失）。
- **alive 上报性能**：`GetClientIPs` 是逐 email 串行接口；inbound 内在线用户多时延迟显著（已限并发 8 路）。
- **3x-ui 鉴权（v0.6 起仅 Bearer API Token 单通道，仅适配 3x-ui v3.0.0+）**：填 `xui.api_host` + `xui.api_token`，中间件每次面板 API 请求都注入 `Authorization: Bearer <token>` 头；3x-ui 内 `APIController.checkAPIAuth` 走 `MatchApiToken` 通道并让 `CSRFMiddleware` 短路。无登录、无 CSRF、无 cookie，与 cookie 寿命完全解耦。任何 4xx / 5xx / 网络错误立即向上传播，不在客户端层做重试 / 重登录 / 兜底——这是项目"单一正向路径"承诺的具体落实。**账号密码 / cookie / CSRF / TOTP 路径已彻底移除**：从 v0.4/v0.5 升级的运维必须先把 3x-ui 升级到 v3.0.0 或更新版本，并在 3x-ui 后台「面板设置 → API 令牌」生成 token 后填入本中间件 Web 面板。
- **API Token 安全注意事项**：a) Token 以明文存于 settings 表（admin 鉴权后可读），运维需保护好 `bridge.db`（默认 0600）。b) 在 3x-ui 后台点击「重新生成」会立即让旧 token 失效，运维必须同步更新到本中间件 Web 面板，否则下一次同步循环立刻报错（错误显式可见，没有"自动重试 + 假装在跑"的兜底）。
- **启动期定型字段**：`log.*` 与 `web.*` 在进程启动时被消费一次；运行期 PATCH 会被 Web Handler 显式拒绝。改监听地址用 `xui-bridge change-listen-addr` 写 systemd drop-in override；其它启动期字段需 `sqlite3 编辑 settings 表后重启`。
- **删除桥接的清理**：当前删除 bridge 时不会清理对应的 traffic_baseline 行（占用极少，<1KB / 用户）；后续版本计划增加 `store.DeleteBaselinesByBridge` 一次性清理。

---

## 项目结构

```text
.
├── cmd/bridge/main.go           # 进程入口 + CLI 子命令分发
├── internal/
│   ├── auth/                    # bcrypt + session 鉴权服务
│   ├── config/                  # 从 SQLite 加载配置 + Validate
│   ├── logger/                  # slog 包装
│   ├── protocol/                # 6 个协议适配器（VLESS / VMess / Trojan / SS / Hysteria v1/v2）
│   ├── store/                   # SQLite 持久层（5 张表：traffic_baseline / settings / bridges / admin_users / sessions）
│   ├── supervisor/              # 引擎热重载控制器（全停全启 + draining 槽位）
│   ├── sync/                    # 同步引擎 + 4 类周期 worker（v0.6 起单一正向路径，不再有心跳/降级兜底）
│   ├── web/                     # HTTP 路由 + handlers + go:embed dist
│   ├── xboard/                  # Xboard /api/v2/server/* 客户端
│   └── xui/                     # 3x-ui /panel/api/inbounds/* 客户端
├── web/                         # Vue 3 + Vite + Tailwind SPA 源码（dev 用）
│   └── src/
│       ├── views/               # 5 个页面（Login / Dashboard / Bridges / Settings / Account）
│       ├── components/          # 共享组件（AppNav 等）
│       ├── stores/auth.ts       # Pinia 鉴权状态
│       ├── api/client.ts        # fetch 封装 + 全局 401 handler
│       └── router/              # vue-router + 守卫
├── Dockerfile                   # 多阶段：node 构前端 → go 嵌入构后端 → alpine 运行
├── Makefile                     # build / build-all / web / build-linux / clean / ...
├── install.sh                   # 一键安装 + 管理脚本（含菜单 / 卸载 / 日志 / 重置密码）
├── README.md                    # 本文件
└── LICENSE                      # MIT
```

---

## 致谢

- [Xboard](https://github.com/cedar2025/Xboard) — Laravel 11 高性能销售面板
- [3x-ui](https://github.com/MHSanaei/3x-ui) — Xray 多协议网页面板
- [v2board](https://github.com/v2board/v2board) — Xboard 的上游 fork 起点

---

## 开源协议

本项目以 [MIT License](LICENSE) 发布。
