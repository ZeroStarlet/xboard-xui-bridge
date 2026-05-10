<div align="center">

# xboard-xui-bridge

**[Xboard](https://github.com/cedar2025/Xboard) 与 [3x-ui](https://github.com/MHSanaei/3x-ui) 的非侵入式中间件**

不改源码、不在节点机部署 XrayR / V2bX 只需一个二进制让两套面板无缝对接。

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go)](go.mod)
[![Vue Version](https://img.shields.io/badge/Vue-3-4FC08D?logo=vue.js)](web/package.json)
[![Platform](https://img.shields.io/badge/platform-Linux%20%7C%20macOS%20%7C%20Windows-lightgrey)](#手工编译)

</div>

> [!IMPORTANT]
> 本项目仅用于个人使用和通信，请勿将其用于非法目的，请勿在生产环境中使用。

---

## 赞助商

如果本项目帮你节省了运维时间，欢迎以赞助形式支持持续开发与维护。

### 当前赞助商

> [CCTQ-中转站](https://code.b886.top/)    [星科支付](https://pay.egoyun.com/)

<a href="https://code.b886.top/" target="_blank">
<img src="https://cos.b886.top/cctq/logo.gif" alt="CCTQ" style="height: 70px !important;width: 277px !important;" >
</a>

支持者将以名字 + 链接（或匿名）展示在此。如有意向，请通过 GitHub Issue 联系。

---

## 快速入门

### 一键安装（Linux）

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/ZeroStarlet/xboard-xui-bridge/main/install.sh)
```

### 准备工作

| 系统 | 准备项 |
| --- | --- |
| Xboard | 拿到 `通讯密钥`；为节点分配数字 ID。 |
| 3x-ui | **必须 v3.0.0+**。后台「面板设置 → API 令牌」生成 48 字符 Token；预创建 inbound 拿到 ID。 |
| Xboard 队列 | **必须**运行 `php artisan queue:work` 或 Horizon——否则中间件 push 永远成功但用户 u/d 永远 0。 |

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

## 致谢

- [Xboard](https://github.com/cedar2025/Xboard) — Laravel 11 高性能销售面板
- [3x-ui](https://github.com/MHSanaei/3x-ui) — Xray 多协议网页面板
- [v2board](https://github.com/v2board/v2board) — Xboard 上游 fork 起点

## 协议

本项目以 [MIT License](LICENSE) 发布。