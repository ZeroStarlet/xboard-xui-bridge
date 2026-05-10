<div align="center">

# xboard-xui-bridge

**[Xboard](https://github.com/cedar2025/Xboard) 与 [3x-ui](https://github.com/MHSanaei/3x-ui) 的非侵入式中间件**

不改源码、不在节点机部署 XrayR / V2bX——一个二进制让两套面板无缝对接。

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go)](go.mod)
[![Vue Version](https://img.shields.io/badge/Vue-3-4FC08D?logo=vue.js)](web/package.json)
[![Platform](https://img.shields.io/badge/platform-Linux%20%7C%20macOS%20%7C%20Windows-lightgrey)](#手工编译)

</div>

---

## 特性

| 类别 | 能力 |
| --- | --- |
| 协议覆盖 | Shadowsocks / VMess / VLESS / Trojan / Hysteria v1 / Hysteria v2 |
| 零侵入 | 仅通过两边公开 HTTP API 交互；不改源码、不在节点机加进程 |
| 零 yaml | 配置维护于 SQLite + Web GUI，浏览器登录即可增删桥接 |
| 热重载 | settings / bridges 修改后引擎全停全启同步，无需重启进程 |
| 高可靠遥测 | 差量上报 + 本地 baseline + `last_seen + pending` 三字段消除流量丢失 |
| 多桥接 | 单实例承载多组 (Xboard 节点 ↔ 3x-ui inbound) 映射 |
| 单二进制 | `go build` 后约 25 MB（含 Vue 前端），无 cgo |
| 运维友好 | `xui-bridge` 一键交互菜单（启停 / 日志 / 改密 / 改监听 / 卸载） |

## 工作原理

```text
┌────────────────────┐                       ┌─────────────────┐
│   Xboard 面板      │  ◄─ server_token ──►  │   xboard-xui-   │
│ /api/v2/server/... │                       │      bridge     │
└────────────────────┘                       │                 │
                                             │  Web 面板 :8787 │
                                             │  Supervisor     │
                                             │  user/traffic/  │
                                             │  alive/status   │
                                             │  本地 SQLite    │
                                             │                 │
┌────────────────────┐                       │                 │
│   3x-ui 面板       │  ◄─ Bearer Token ──►  │                 │
│ /panel/api/...     │                       └─────────────────┘
└────────────────────┘
```

中间件假设管理员预先在 3x-ui 后台手工创建好 inbound（端口 / 协议 / TLS / Reality 等），只对 inbound 内的 client 做 add/update/del——协议字段的真相源在 3x-ui，无跨面板字段映射。

---

## 快速开始

### 一键安装（Linux）

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/ZeroStarlet/xboard-xui-bridge/main/install.sh)
```

脚本自动：检测架构 → 下载最新 Release → 校验 SHA256 → 安装到 `/usr/local/bin/xboard-xui-bridge` → 写 systemd unit → 注册 `xui-bridge` 快捷命令 → 输出首次随机 admin 密码。

二次执行进入交互菜单。

### 准备工作

| 系统 | 准备项 |
| --- | --- |
| Xboard | 拿到 `server_token`；为节点分配数字 ID。 |
| 3x-ui | **必须 v3.0.0+**。后台「面板设置 → API 令牌」生成 48 字符 Token；预创建 inbound 拿到 ID。 |
| Xboard 队列 | **必须**运行 `php artisan queue:work` 或 Horizon——否则中间件 push 永远成功但用户 u/d 永远 0。 |

### 首次登录

安装脚本输出 admin 用户名 + 初始密码（同时写入 `/usr/local/xboard-xui-bridge/data/initial_password.txt`）。浏览器打开 `http://<server-ip>:8787`：

1. 改密码
2. "运行参数"填 Xboard / 3x-ui 凭据
3. "桥接管理"添加桥接（Xboard 节点 ↔ 3x-ui inbound 一对一）
4. 保存后引擎自动热重载

### `xui-bridge` 常用命令

```bash
xui-bridge install              # 安装 / 升级到最新版
xui-bridge start | stop | restart
xui-bridge status               # systemctl 状态
xui-bridge log | follow         # 日志 / 实时跟踪
xui-bridge reset-password       # 忘密码兜底
xui-bridge change-listen-addr   # 改监听地址
xui-bridge uninstall | purge    # 卸载（保留数据 / 含数据）
xui-bridge menu                 # 交互菜单（默认）
xui-bridge help
```

---

## 手工编译

```bash
make build-all           # 端到端：构建 Vue 前端 → 嵌入 Go 二进制
make build               # 仅 Go（要求 internal/web/dist 已最新）
make build-linux         # 交叉编译 Linux x86_64
```

二进制输出到 `dist/`。

### 本地开发

```bash
# 终端 1：Vite dev server
cd web && npm run dev

# 终端 2：Go 后端
make build && ./dist/xboard-xui-bridge
```

Vite 已配置代理 `/api → 127.0.0.1:8787`。

### 容器化

```bash
mkdir -p data && chmod 777 data
docker build -t xboard-xui-bridge:latest .
docker run -d --name bridge \
  -v "$(pwd)/data:/app/data" \
  -p 127.0.0.1:8787:8787 \
  xboard-xui-bridge:latest
docker logs bridge | grep -A1 "已生成初始 admin 账户"
```

挂载 `/app/data` 必须；非 root 用户 `bridge` 需对挂载点有写权限。

---

## 配置字段

字段语义、默认值、key 命名见 [`internal/config/config.go`](internal/config/config.go)。Web 面板按字段分组（Xboard / 3x-ui / 同步周期 / 上报开关 / 日志 / Web）。

> **重启生效字段**：`log.*` / `web.*` 在启动时一次性消费，运行期 PATCH 会被显式拒绝。改监听用 `xui-bridge change-listen-addr`；其它启动期字段需 `sqlite3 ./data/bridge.db` 编辑后重启。

---

## 安全模型

- bcrypt cost=12 + Session Cookie（HttpOnly + SameSite=Lax，TLS 下加 Secure）
- CSRF 防御：写操作校验 Origin / Referer 与 Host 同源
- 滑动续期 7 天 + 绝对寿命 30 天（默认）；改密强制下线该用户全部 session
- 首次密码 crypto/rand 18 字节 base64url（≈ 108 bits 熵），双通道输出
- 登录侧信道防御：用户不存在分支也跑一次 bcrypt
- Release 资产 SHA256 校验，不匹配立即中止安装

---

## 限制

- 节点协议级字段（端口 / TLS / Reality）由管理员在 3x-ui 维护，中间件只同步 client。
- Xboard 队列 worker 必须运行（详见上方表格）。
- alive 上报是逐 email 串行接口，已限并发 8 路。
- 3x-ui 鉴权 v0.6 起仅 Bearer API Token 单通道，**v3.0.0 之前不再兼容**；账号密码 / cookie / CSRF / TOTP 路径已彻底移除。
- API Token 明文存 settings 表，需保护好 `bridge.db`（默认 0600）。
- 项目恪守"单一正向路径"：任何 4xx / 5xx / 网络错误立即向上传播，无重试 / 重登录 / 降级兜底。

---

## 项目结构

```text
.
├── cmd/bridge/main.go           # 进程入口 + CLI 分发
├── internal/
│   ├── auth/                    # bcrypt + session 鉴权
│   ├── config/                  # SQLite 配置加载
│   ├── protocol/                # 6 个协议适配器
│   ├── store/                   # SQLite 持久层
│   ├── supervisor/              # 引擎热重载控制器
│   ├── sync/                    # 4 类周期 worker
│   ├── web/                     # HTTP + go:embed dist
│   ├── xboard/                  # Xboard 客户端
│   └── xui/                     # 3x-ui 客户端
├── web/                         # Vue 3 + Vite + Tailwind SPA
├── Dockerfile
├── Makefile
└── install.sh
```

---

## 赞助商

如果本项目帮你节省了运维时间，欢迎以赞助形式支持持续开发与维护。

### 当前赞助商

> 暂无赞助商——欢迎成为第一位 ❤️

支持者将以名字 + 链接（或匿名）展示在此。如有意向，请通过 GitHub Issue 联系。

---

## 致谢

- [Xboard](https://github.com/cedar2025/Xboard) — Laravel 11 高性能销售面板
- [3x-ui](https://github.com/MHSanaei/3x-ui) — Xray 多协议网页面板
- [v2board](https://github.com/v2board/v2board) — Xboard 上游 fork 起点

## 协议

本项目以 [MIT License](LICENSE) 发布。
