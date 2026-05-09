#!/usr/bin/env bash
# xboard-xui-bridge 一键安装脚本。
#
# 用法：
#   bash <(curl -fsSL https://raw.githubusercontent.com/ZeroStarlet/xboard-xui-bridge/main/install.sh)
#
# 行为：
#   1. 自检 root 权限与系统架构（amd64 / arm64 / armv7）；
#   2. 安装基础依赖（curl / tar）——若已存在则跳过；
#   3. 从 GitHub Release 拉取最新二进制并安装到 /usr/local/bin；
#   4. 创建 /usr/local/xboard-xui-bridge/data 数据目录；
#   5. 写 systemd unit 并启动；
#   6. 输出首次随机生成的 admin 密码。
#
# 二次执行（已安装情况下）会自动进入"升级"模式：
#   保留 data/，下载新二进制覆盖 /usr/local/bin，重启服务。

set -euo pipefail

# ---------------- 颜色与日志辅助 ----------------
red='\033[0;31m'
green='\033[0;32m'
yellow='\033[0;33m'
plain='\033[0m'

log_info()  { echo -e "${green}[INFO]${plain} $*"; }
log_warn()  { echo -e "${yellow}[WARN]${plain} $*"; }
log_error() { echo -e "${red}[ERROR]${plain} $*" >&2; }
fail()      { log_error "$*"; exit 1; }

# ---------------- 常量 ----------------
GITHUB_REPO="ZeroStarlet/xboard-xui-bridge"
INSTALL_DIR="/usr/local/xboard-xui-bridge"
DATA_DIR="${INSTALL_DIR}/data"
BIN_PATH="/usr/local/bin/xboard-xui-bridge"
SERVICE_NAME="xboard-xui-bridge"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"

# ---------------- 前置检查 ----------------
require_root() {
    if [[ "${EUID}" -ne 0 ]]; then
        fail "请以 root 身份运行：sudo bash $0"
    fi
}

detect_arch() {
    local arch
    arch=$(uname -m)
    case "${arch}" in
        x86_64|amd64)         echo "linux-amd64" ;;
        aarch64|arm64)        echo "linux-arm64" ;;
        armv7l|armv7|armhf)   echo "linux-armv7" ;;
        *) fail "暂不支持的架构：${arch}（仅支持 amd64 / arm64 / armv7）" ;;
    esac
}

detect_os() {
    if [[ -f /etc/os-release ]]; then
        # shellcheck disable=SC1091
        . /etc/os-release
        echo "${ID:-unknown}"
    else
        echo "unknown"
    fi
}

# ---------------- 依赖安装 ----------------
install_dependencies() {
    if command -v curl >/dev/null 2>&1 && command -v tar >/dev/null 2>&1; then
        log_info "依赖 curl / tar 已就绪，跳过安装"
        return
    fi
    local os
    os=$(detect_os)
    log_info "安装依赖（os=${os}）"
    case "${os}" in
        ubuntu|debian)
            apt-get update -y
            apt-get install -y curl tar
            ;;
        centos|rhel|fedora|rocky|almalinux|ol)
            if command -v dnf >/dev/null 2>&1; then
                dnf install -y curl tar
            else
                yum install -y curl tar
            fi
            ;;
        alpine)
            apk add --no-cache curl tar
            ;;
        arch|manjaro)
            pacman -Sy --noconfirm curl tar
            ;;
        *)
            log_warn "未识别的系统 ${os}，请手动确认 curl 与 tar 已安装。"
            ;;
    esac
}

# ---------------- 版本与下载 ----------------
get_latest_version() {
    # GitHub API：返回最新 release 的 tag_name（含 v 前缀）。
    local api="https://api.github.com/repos/${GITHUB_REPO}/releases/latest"
    local tag
    tag=$(curl -fsSL "${api}" | grep -E '"tag_name"' | head -1 | sed -E 's/.*"([^"]+)".*/\1/')
    if [[ -z "${tag}" ]]; then
        fail "无法获取最新版本（API 限频或仓库无 Release）。"
    fi
    echo "${tag}"
}

download_and_install() {
    local arch="$1"
    local version="$2"
    local asset="xboard-xui-bridge-${arch}.tar.gz"
    local url="https://github.com/${GITHUB_REPO}/releases/download/${version}/${asset}"
    local tmpdir
    tmpdir=$(mktemp -d)

    log_info "下载 ${asset}"
    if ! curl -fL --retry 3 -o "${tmpdir}/${asset}" "${url}"; then
        rm -rf "${tmpdir}"
        fail "下载失败：${url}"
    fi

    log_info "解压并安装到 ${BIN_PATH}"
    tar -xzf "${tmpdir}/${asset}" -C "${tmpdir}"
    install -m 0755 "${tmpdir}/xboard-xui-bridge" "${BIN_PATH}"
    rm -rf "${tmpdir}"
}

# ---------------- 目录与服务 ----------------
setup_directories() {
    log_info "准备数据目录 ${DATA_DIR}"
    mkdir -p "${DATA_DIR}"
    chmod 700 "${DATA_DIR}"
}

write_systemd_unit() {
    log_info "写入 systemd unit ${SERVICE_FILE}"
    cat > "${SERVICE_FILE}" <<EOF
[Unit]
Description=xboard-xui-bridge — 非侵入式 Xboard / 3x-ui 中间件
After=network.target

[Service]
Type=simple
User=root
# BRIDGE_LISTEN_ADDR=:8787 让 Web 面板绑定全部网卡——VPS 一键安装的
# 典型场景就是要从公网浏览器访问。安全模型靠 admin 鉴权 + bcrypt +
# CSRF 防御兜底；如需更高安全请配反代 + TLS。
#
# 不希望外部可达的运维可以执行：
#   sudo systemctl edit xboard-xui-bridge
# 然后写：
#   [Service]
#   Environment=BRIDGE_LISTEN_ADDR=127.0.0.1:8787
# 这会通过 drop-in override 覆盖以下默认值。
Environment=BRIDGE_LISTEN_ADDR=:8787
ExecStart=${BIN_PATH} run --db ${DATA_DIR}/bridge.db
Restart=on-failure
RestartSec=5s
LimitNOFILE=65535
WorkingDirectory=${INSTALL_DIR}

[Install]
WantedBy=multi-user.target
EOF
    systemctl daemon-reload
}

start_or_restart_service() {
    if systemctl is-active --quiet "${SERVICE_NAME}"; then
        log_info "重启服务"
        systemctl restart "${SERVICE_NAME}"
    else
        log_info "启用并启动服务"
        systemctl enable "${SERVICE_NAME}"
        systemctl start "${SERVICE_NAME}"
    fi
}

# ---------------- 完成提示 ----------------
show_post_install() {
    log_info "等待 ~3 秒让首次启动完成..."
    sleep 3

    local pwd_file="${DATA_DIR}/initial_password.txt"
    local pwd_line=""
    if [[ -f "${pwd_file}" ]]; then
        pwd_line=$(cat "${pwd_file}" 2>/dev/null || true)
    fi

    local public_ip
    public_ip=$(curl -fsSL --max-time 5 https://api.ipify.org 2>/dev/null || echo "<server-ip>")

    echo ""
    echo "============================================================"
    echo "  xboard-xui-bridge 安装完成"
    echo "============================================================"
    echo ""
    echo "  Web 面板地址：  http://${public_ip}:8787"
    echo "  默认用户名：    admin"
    if [[ -n "${pwd_line}" ]]; then
        echo "  初始密码：      ${pwd_line}"
        echo ""
        echo "  密码同时已写入文件：${pwd_file}"
        echo "  登录后请立即修改密码并妥善保管该文件。"
    else
        echo "  初始密码：      （未读到 initial_password.txt——这通常意味着升级安装，沿用旧密码）"
    fi
    echo ""
    echo "  防火墙放行（首次部署必做）："
    echo "    ufw allow 8787/tcp                                                # Ubuntu/Debian"
    echo "    firewall-cmd --add-port=8787/tcp --permanent && firewall-cmd --reload   # CentOS/RHEL"
    echo "    云厂商 VPS 还需到控制台安全组放行 TCP 8787。"
    echo ""
    echo "  服务管理命令："
    echo "    systemctl status ${SERVICE_NAME}    查看运行状态"
    echo "    systemctl restart ${SERVICE_NAME}   重启服务"
    echo "    systemctl stop ${SERVICE_NAME}      停止服务"
    echo "    journalctl -u ${SERVICE_NAME} -f    实时查看日志"
    echo ""
    echo "  CLI 工具："
    echo "    xboard-xui-bridge reset-password    本地重置 admin 密码"
    echo "    xboard-xui-bridge version           打印版本"
    echo ""
    echo "============================================================"
}

# ---------------- 主流程 ----------------
main() {
    require_root
    install_dependencies

    local arch version
    arch=$(detect_arch)
    log_info "检测到架构：${arch}"

    version=$(get_latest_version)
    log_info "目标版本：${version}"

    download_and_install "${arch}" "${version}"
    setup_directories
    write_systemd_unit
    start_or_restart_service
    show_post_install
}

main "$@"
