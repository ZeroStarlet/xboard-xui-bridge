#!/usr/bin/env bash
# xboard-xui-bridge 一键安装 / 管理脚本（v0.5+）。
#
# 设计动机：
#
#   v0.4 时代仅 install + 升级两条路径，运维卸载、查看日志、重置密码都得
#   逐个 systemctl / sqlite3 操作；客户工单大量集中在"忘记密码怎么办""怎么
#   彻底清掉""怎么改监听地址"——这些都是高频低价值步骤，应当被脚本封装。
#
#   参考 MHSanaei/3x-ui 主线 install.sh 的"装完就给一个 helper 命令进菜单"
#   设计：用户运行 `bash <(curl ...)` 一次完成安装；后续在 SSH 上敲
#   `xui-bridge` 即可进入交互式管理菜单（启停 / 重启 / 状态 / 日志 / 卸载 /
#   重置密码 / 修改监听）；脚本本身也支持以子命令直跑（如 `xui-bridge log`）
#   方便运维写自动化。
#
# 用法：
#
#   bash <(curl -fsSL https://raw.githubusercontent.com/ZeroStarlet/xboard-xui-bridge/main/install.sh)
#       默认行为：未装 → 安装；已装 → 进菜单。一键命令运维直接复制粘贴即可。
#
#   xui-bridge [子命令]                          安装后可用的快捷别名
#     install / update / upgrade                安装或升级（保留 data/）
#     uninstall                                 卸载二进制 + service，**保留** data/
#     purge                                     完全清理（含 data/，**不可恢复**）
#     start | stop | restart                    服务启停
#     status                                    打印 systemctl status
#     log                                       打印最近 200 行日志
#     follow                                    实时跟踪日志（journalctl -f）
#     reset-password                            交互式重置 admin 密码
#     change-listen-addr                        交互式修改 Web 监听地址
#     menu                                      显式进入菜单（默认入口）
#     help | --help | -h                        打印帮助
#
# 风格约定（与项目其他 sh 保持一致）：
#
#   - 所有日志走 log_info / log_warn / log_error / log_step；禁止裸 echo。
#   - 任何破坏性操作（uninstall / purge / change-listen-addr）必须二次确认。
#   - set -euo pipefail：任何未捕获错误立即终止，避免"半装状态"。
#   - 全脚本不依赖 bashism 之外的工具（curl / tar / systemctl 必备；ss /
#     netstat 二选一即可，端口检测自动兜底）。
#
# 返回码语义：
#
#   0   成功
#   1   运行期错误（systemctl 失败 / 下载失败 / 用户取消等）
#   2   参数错误 / 前置检查失败（非 root / 不支持的 OS / 不支持的架构）

set -euo pipefail

# ---------------- 颜色与日志辅助 ----------------
# ANSI 转义；终端不支持彩色时（如 nohup 重定向到文件）会显示原始转义符——
# 但对一键安装场景影响不大；如需强制无色可在调用处 export NO_COLOR=1。
red='\033[0;31m'
green='\033[0;32m'
yellow='\033[0;33m'
blue='\033[0;34m'
cyan='\033[0;36m'
bold='\033[1m'
plain='\033[0m'

log_info()  { printf "${green}[INFO]${plain}  %s\n" "$*"; }
log_warn()  { printf "${yellow}[WARN]${plain}  %s\n" "$*" >&2; }
log_error() { printf "${red}[ERROR]${plain} %s\n" "$*" >&2; }
log_step()  { printf "${cyan}[STEP]${plain}  %s\n" "$*"; }
fail()      { log_error "$*"; exit 1; }

# ---------------- 常量 ----------------
GITHUB_REPO="ZeroStarlet/xboard-xui-bridge"
INSTALL_DIR="/usr/local/xboard-xui-bridge"
DATA_DIR="${INSTALL_DIR}/data"
BIN_PATH="/usr/local/bin/xboard-xui-bridge"
# HELPER_PATH 是装好后给运维敲的快捷命令；symlink 到 BIN_PATH 让
# `xui-bridge --version` 等子命令也能直接走。但**菜单 / 卸载等管理动作**
# 走 install.sh 自身——所以再单独维护一份 install.sh 副本到
# /usr/local/xboard-xui-bridge/install.sh，让 helper 用。
HELPER_PATH="/usr/local/bin/xui-bridge"
SCRIPT_COPY="${INSTALL_DIR}/install.sh"
SERVICE_NAME="xboard-xui-bridge"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
DEFAULT_PORT=8787

# ---------------- 前置检查 ----------------
require_root() {
    if [[ "${EUID}" -ne 0 ]]; then
        fail "请以 root 身份运行：sudo bash $0"
    fi
}

# detect_arch 把 uname -m 输出映射到本项目 Release 资产名约定。
#
# 项目当前发布 amd64 / arm64 / armv7 三种 Linux 架构；其他架构返回非零
# 让上层 fail。armv6 / 386 / s390x 等暂不发布——若运维有需要可手工编译。
detect_arch() {
    local arch
    arch=$(uname -m)
    case "${arch}" in
        x86_64|amd64)         echo "linux-amd64" ;;
        aarch64|arm64)        echo "linux-arm64" ;;
        armv7l|armv7|armhf)   echo "linux-armv7" ;;
        *) fail "暂不支持的架构：${arch}（仅支持 amd64 / arm64 / armv7；其他架构请手工 \`make build-linux\` 编译）" ;;
    esac
}

# detect_os 读取 /etc/os-release 的 ID 字段（小写 distro 标识）。
# 失败时返回 "unknown"；安装依赖时按 "unknown" 走通用提示而非中断。
detect_os() {
    if [[ -f /etc/os-release ]]; then
        # shellcheck disable=SC1091
        . /etc/os-release
        echo "${ID:-unknown}"
    else
        echo "unknown"
    fi
}

# is_installed 仅在二进制 + service 文件都存在时才视为"已装"。
# 部分残留（仅 service 文件 / 仅二进制）按"未装"处理，让 install 路径自愈。
is_installed() {
    [[ -f "${BIN_PATH}" ]] && [[ -f "${SERVICE_FILE}" ]]
}

# service_active 包装 systemctl is-active；对未装情况安全返回 1。
service_active() {
    systemctl is-active --quiet "${SERVICE_NAME}" 2>/dev/null
}

# port_in_use 检测指定 TCP 端口是否被占用——优先用 ss，其次 netstat，最后 lsof。
# 这三者在不同 minimal 镜像上的可用性各异；按可用性兜底而不强制安装额外工具。
port_in_use() {
    local port="$1"
    if command -v ss >/dev/null 2>&1; then
        ss -ltn 2>/dev/null | awk -v p=":${port}$" '$4 ~ p { found=1 } END { exit !found }'
        return
    fi
    if command -v netstat >/dev/null 2>&1; then
        netstat -lnt 2>/dev/null | awk -v p=":${port} " '$4 ~ p { found=1 } END { exit !found }'
        return
    fi
    if command -v lsof >/dev/null 2>&1; then
        lsof -nP -iTCP:"${port}" -sTCP:LISTEN >/dev/null 2>&1
        return
    fi
    # 完全无可用工具——保守返回 0（视为占用）但只是提示，不会阻断安装。
    return 1
}

# get_public_ip 尽力获取公网 IP，用于在安装完成提示中显示访问地址。
# 失败时返回 <server-ip> 占位符——运维通常知道自己机器的 IP，无需阻断。
get_public_ip() {
    local ip
    ip=$(curl -fsSL --max-time 5 https://api.ipify.org 2>/dev/null || true)
    if [[ -z "${ip}" ]]; then
        ip=$(curl -fsSL --max-time 5 https://ipv4.icanhazip.com 2>/dev/null || true)
    fi
    if [[ -z "${ip}" ]]; then
        ip="<server-ip>"
    fi
    echo "${ip}"
}

# confirm_yes 提示 [y/N] 二次确认；默认拒绝（防误操作）。
# 返回 0 = 用户输入 y/Y/yes/YES；其它（含 EOF）返回 1。
confirm_yes() {
    local prompt="$1"
    local reply
    read -rp "${prompt} [y/N]: " reply || return 1
    case "${reply}" in
        y|Y|yes|YES) return 0 ;;
        *) return 1 ;;
    esac
}

# ---------------- 依赖安装 ----------------
install_dependencies() {
    if command -v curl >/dev/null 2>&1 && command -v tar >/dev/null 2>&1; then
        log_info "依赖 curl / tar 已就绪，跳过安装"
        return
    fi
    local os
    os=$(detect_os)
    log_step "安装依赖（os=${os}）"
    case "${os}" in
        ubuntu|debian|armbian)
            apt-get update -y && apt-get install -y curl tar
            ;;
        centos|rhel|fedora|rocky|almalinux|ol|amzn)
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
        opensuse-tumbleweed|opensuse-leap)
            zypper -q install -y curl tar
            ;;
        *)
            log_warn "未识别的系统 ${os}，请手动确认 curl 与 tar 已安装"
            ;;
    esac
}

# ---------------- 版本与下载 ----------------
get_latest_version() {
    local api="https://api.github.com/repos/${GITHUB_REPO}/releases/latest"
    local tag
    tag=$(curl -fsSL "${api}" | grep -E '"tag_name"' | head -1 | sed -E 's/.*"([^"]+)".*/\1/')
    if [[ -z "${tag}" ]]; then
        fail "无法获取最新版本（GitHub API 限频或仓库无 Release）。请稍后重试或手工指定版本。"
    fi
    echo "${tag}"
}

# download_and_install 下载 release tarball、校验 SHA256、解压安装到 BIN_PATH。
# 二次执行（升级）会原地覆盖 BIN_PATH 不动 DATA_DIR。
#
# 实现细节：
#
#   函数体放在 subshell 里——bash 的 `trap '...' RETURN` 不会在 `fail/exit`
#   退出路径触发，会泄漏 mktemp 临时目录。改用 subshell + EXIT trap 后，
#   即使内部 fail 触发 exit 1，subshell 退出时 EXIT trap 一定会执行清理；
#   subshell 的 exit 1 会让外层函数返回非零，配合 set -e 自动中断主流程。
download_and_install() {
    local arch="$1"
    local version="$2"
    (
        local asset="xboard-xui-bridge-${arch}.tar.gz"
        local url="https://github.com/${GITHUB_REPO}/releases/download/${version}/${asset}"
        local sums_url="https://github.com/${GITHUB_REPO}/releases/download/${version}/SHA256SUMS.txt"
        local tmpdir
        tmpdir=$(mktemp -d)
        # subshell 范围内的 EXIT trap：subshell 退出（含 fail/exit 1）必触发。
        trap 'rm -rf "${tmpdir}"' EXIT

        log_step "下载 ${asset}"
        if ! curl -fL --retry 3 -o "${tmpdir}/${asset}" "${url}"; then
            fail "下载失败：${url}"
        fi

        # SHA256 校验是 v0.5 起新加的供应链防御——release 资产被替换的极小
        # 概率下让运维提前感知。校验失败要立刻 fail，绝不安装可疑二进制。
        log_step "校验 SHA256"
        if curl -fsSL --retry 3 -o "${tmpdir}/SHA256SUMS.txt" "${sums_url}"; then
            if command -v sha256sum >/dev/null 2>&1; then
                local expected actual
                # `|| true` 保护：当 SHA256SUMS.txt 中不含本资产时 grep
                # 会返回 1，配合 set -euo pipefail 会让脚本提前中断；这里
                # 是"找不到则跳过校验"的合法路径，必须吞掉非零退出。
                expected=$(grep " ${asset}\$" "${tmpdir}/SHA256SUMS.txt" | awk '{print $1}' || true)
                if [[ -z "${expected}" ]]; then
                    log_warn "SHA256SUMS.txt 中未找到 ${asset}，跳过校验"
                else
                    actual=$(sha256sum "${tmpdir}/${asset}" | awk '{print $1}')
                    if [[ "${expected}" != "${actual}" ]]; then
                        fail "SHA256 校验失败！expected=${expected} actual=${actual}"
                    fi
                    log_info "SHA256 校验通过"
                fi
            else
                log_warn "未安装 sha256sum，跳过校验（建议 apt install coreutils）"
            fi
        else
            log_warn "无法获取 SHA256SUMS.txt（旧版本 release 可能未发布该文件），跳过校验"
        fi

        log_step "解压并安装到 ${BIN_PATH}"
        tar -xzf "${tmpdir}/${asset}" -C "${tmpdir}"
        install -m 0755 "${tmpdir}/xboard-xui-bridge" "${BIN_PATH}"
    )
}

# ---------------- 目录与服务 ----------------
setup_directories() {
    log_step "准备数据目录 ${DATA_DIR}"
    mkdir -p "${DATA_DIR}"
    chmod 700 "${DATA_DIR}"
}

# install_helper 把 install.sh 拷贝到 INSTALL_DIR 并创建 xui-bridge 别名。
#
# 拷贝而不是 symlink 到 source URL：本脚本支持离线管理（卸载、改 listen 等
# 不需要联网），symlink 到一次性下载源会在 /tmp 清理后失效。把脚本固化在
# INSTALL_DIR 下，xui-bridge → 指向它，运维断网仍可管。
#
# 关键边界：通过 `xui-bridge install` 调起本函数时，$0 经 bash 解析后是
# helper symlink → SCRIPT_COPY 的真实路径——cp "$0" "$SCRIPT_COPY" 会
# 让 GNU cp 拒绝（"are the same file"）并退出非零，set -e 把整个安装流程
# 中止在 daemon-reload 之前。这里通过 readlink -f 比对真实路径，**自我
# 复制**（已固化）就直接跳过，让 helper 模式下重装 / 升级 100% 通畅。
install_helper() {
    log_step "安装 helper 命令 ${HELPER_PATH}"
    local self_real script_copy_real
    self_real=$(readlink -f "$0" 2>/dev/null || true)
    script_copy_real=$(readlink -f "${SCRIPT_COPY}" 2>/dev/null || true)

    # 三种调用源各自对应一种固化策略：
    #
    #   a) helper 模式（self_real == script_copy_real）：用户敲了 xui-bridge
    #      install 走升级。优先从 GitHub curl 一份新脚本——让 install.sh
    #      自身的逻辑也能随 release 演进（如本次 v0.5 加了 SHA256 校验、
    #      menu 等，旧版 helper 升级 binary 时若不刷脚本，运维感知不到）。
    #      curl 失败则保留旧脚本（offline-friendly：网络抖动不影响升级）。
    #
    #   b) 本地直跑（$0 是真实文件且 != SCRIPT_COPY）：cp $0 → SCRIPT_COPY。
    #      运维克隆仓库后跑 `bash install.sh install`，文件固化即可。
    #
    #   c) pipe 模式（$0 是 /dev/fd/63）：bash <(curl ...)，$0 不是真文件，
    #      必须 curl 拉一次脚本固化下来；curl 失败时仅 WARN 不阻断（核心
    #      功能仍可用，只是 xui-bridge helper 不可用）。
    if [[ -n "${self_real}" && "${self_real}" == "${script_copy_real}" ]]; then
        log_step "helper 模式：尝试从 GitHub 刷新 install.sh"
        if curl -fsSL "https://raw.githubusercontent.com/${GITHUB_REPO}/main/install.sh" -o "${SCRIPT_COPY}.new"; then
            mv -f "${SCRIPT_COPY}.new" "${SCRIPT_COPY}"
            log_info "已从 GitHub 刷新 install.sh"
        else
            rm -f "${SCRIPT_COPY}.new"
            log_warn "无法从 GitHub 拉取 install.sh，保留本地旧版本（离线模式可继续工作）"
        fi
    elif [[ -f "$0" ]] && [[ -r "$0" ]]; then
        cp "$0" "${SCRIPT_COPY}"
    else
        if ! curl -fsSL "https://raw.githubusercontent.com/${GITHUB_REPO}/main/install.sh" -o "${SCRIPT_COPY}"; then
            log_warn "无法从 GitHub 固化 install.sh（curl 失败），helper 将不可用"
            return 0
        fi
    fi
    chmod 0755 "${SCRIPT_COPY}"
    # symlink 而不是 cp：让 xui-bridge 永远跑最新 install.sh（升级覆盖时
    # SCRIPT_COPY 会被刷新，xui-bridge 自动跟进）。
    ln -sf "${SCRIPT_COPY}" "${HELPER_PATH}"
}

write_systemd_unit() {
    log_step "写入 systemd unit ${SERVICE_FILE}"
    cat > "${SERVICE_FILE}" <<EOF
[Unit]
Description=xboard-xui-bridge — 非侵入式 Xboard / 3x-ui 中间件
After=network.target

[Service]
Type=simple
User=root
# BRIDGE_LISTEN_ADDR=:${DEFAULT_PORT} 让 Web 面板绑定全部网卡——VPS 一键安装的
# 典型场景就是要从公网浏览器访问。安全模型靠 admin 鉴权 + bcrypt +
# CSRF 防御兜底；如需更高安全请配反代 + TLS。
#
# 不希望外部可达的运维可以执行：
#   sudo systemctl edit ${SERVICE_NAME}
# 然后写：
#   [Service]
#   Environment=BRIDGE_LISTEN_ADDR=127.0.0.1:${DEFAULT_PORT}
# 这会通过 drop-in override 覆盖以下默认值。
Environment=BRIDGE_LISTEN_ADDR=:${DEFAULT_PORT}
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
        log_step "重启服务"
        systemctl restart "${SERVICE_NAME}"
    else
        log_step "启用并启动服务"
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
    public_ip=$(get_public_ip)

    echo
    printf "${bold}${cyan}============================================================${plain}\n"
    printf "${bold}  xboard-xui-bridge 安装完成${plain}\n"
    printf "${bold}${cyan}============================================================${plain}\n"
    echo
    printf "  ${bold}Web 面板地址：${plain}  http://%s:%s\n" "${public_ip}" "${DEFAULT_PORT}"
    printf "  ${bold}默认用户名：${plain}    admin\n"
    if [[ -n "${pwd_line}" ]]; then
        printf "  ${bold}初始密码：${plain}      ${green}%s${plain}\n" "${pwd_line}"
        echo
        printf "  密码同时已写入文件：%s\n" "${pwd_file}"
        printf "  ${yellow}登录后请立即修改密码并妥善保管该文件。${plain}\n"
    else
        printf "  ${bold}初始密码：${plain}      （未读到 initial_password.txt——这通常意味着升级安装，沿用旧密码）\n"
    fi
    echo
    printf "  ${bold}防火墙放行（首次部署必做）：${plain}\n"
    printf "    ufw allow %s/tcp                                      # Ubuntu/Debian\n" "${DEFAULT_PORT}"
    printf "    firewall-cmd --add-port=%s/tcp --permanent && firewall-cmd --reload   # CentOS/RHEL\n" "${DEFAULT_PORT}"
    printf "    云厂商 VPS 还需到控制台安全组放行 TCP %s\n" "${DEFAULT_PORT}"
    echo
    printf "  ${bold}快捷管理命令（已注册到 \$PATH）：${plain}\n"
    printf "    xui-bridge                       打开管理菜单\n"
    printf "    xui-bridge status                查看运行状态\n"
    printf "    xui-bridge log                   查看最近日志\n"
    printf "    xui-bridge follow                实时跟踪日志\n"
    printf "    xui-bridge restart               重启服务\n"
    printf "    xui-bridge reset-password        重置 admin 密码\n"
    printf "    xui-bridge uninstall             卸载（保留 data/）\n"
    printf "    xui-bridge purge                 完全清理（含 data/）\n"
    printf "    xui-bridge help                  查看完整帮助\n"
    echo
    printf "${bold}${cyan}============================================================${plain}\n"
}

# ---------------- 子命令实现 ----------------
cmd_install() {
    install_dependencies

    local arch version
    arch=$(detect_arch)
    log_info "检测到架构：${arch}"

    version=$(get_latest_version)
    log_info "目标版本：${version}"

    # 端口冲突预警：装前检测 8787 是否已被占用——避免安装完启动即 fail
    # 让运维一脸懵。仅 WARN 不阻断（用户可能要换 listen_addr）。
    if port_in_use "${DEFAULT_PORT}"; then
        log_warn "端口 ${DEFAULT_PORT} 已被占用——若不是本中间件历史进程，"
        log_warn "建议安装后通过 \`xui-bridge change-listen-addr\` 修改监听端口。"
    fi

    download_and_install "${arch}" "${version}"
    setup_directories
    write_systemd_unit
    install_helper
    start_or_restart_service
    show_post_install
}

# cmd_uninstall 卸载二进制 + service + helper，**保留** data/。
# 适用于"想换机器迁移"或"暂时不用，未来还会装回来"的运维。
cmd_uninstall() {
    if ! is_installed; then
        log_warn "未检测到已安装实例，无需卸载"
        return 0
    fi
    echo
    log_warn "即将卸载 xboard-xui-bridge："
    printf "    - 停止并禁用 systemd 服务 %s\n" "${SERVICE_NAME}"
    printf "    - 删除二进制 %s\n" "${BIN_PATH}"
    printf "    - 删除 service 文件 %s\n" "${SERVICE_FILE}"
    printf "    - 删除 helper 命令 %s\n" "${HELPER_PATH}"
    printf "    - ${green}保留${plain} 数据目录 %s（含数据库、密码、日志）\n" "${DATA_DIR}"
    echo
    if ! confirm_yes "确定继续吗？"; then
        log_info "已取消"
        return 0
    fi
    log_step "停止并禁用服务"
    systemctl stop "${SERVICE_NAME}" 2>/dev/null || true
    systemctl disable "${SERVICE_NAME}" 2>/dev/null || true
    log_step "删除二进制 / service / drop-in / helper"
    rm -f "${BIN_PATH}" "${SERVICE_FILE}" "${HELPER_PATH}" "${SCRIPT_COPY}"
    # 删除 change-listen-addr 写过的 drop-in override 目录——不删的话
    # 重装后会从 listen.conf 继承上一次的监听地址，让"卸载重装恢复默认"
    # 这一直觉失效。
    rm -rf "/etc/systemd/system/${SERVICE_NAME}.service.d"
    systemctl daemon-reload
    log_info "卸载完成。data/ 仍保留在 ${DATA_DIR}；如需彻底清理请运行 xui-bridge purge。"
}

# cmd_purge 完全清理：相当于"装回原样"。包括 data/ 数据库。
# 危险操作，必须二次确认 + 输入 PURGE 字符串确认。
cmd_purge() {
    echo
    # 这里不能用 log_warn——log helper 把消息作为 %s 参数传给 printf，
    # 嵌入的 ${red} 等颜色变量会被原样输出为字面转义序列（无法着色）。
    # 直接用 printf + format string 是唯一干净的彩色多色嵌入方式。
    printf "${yellow}[WARN]${plain}  即将${red}彻底清理${plain} xboard-xui-bridge 全部数据：\n" >&2
    printf "    - 停止服务 + 删二进制 + 删 service + 删 helper（同 uninstall）\n"
    printf "    - ${red}并删除${plain} 数据目录 %s（含数据库、密码、日志）\n" "${DATA_DIR}"
    printf "    - ${red}此操作不可恢复${plain}：所有桥接配置、管理员账户、流量基线都会丢失\n"
    echo
    local reply
    read -rp "请输入 PURGE 确认（区分大小写）：" reply || return 1
    if [[ "${reply}" != "PURGE" ]]; then
        log_info "确认未通过，已取消"
        return 0
    fi
    log_step "停止并禁用服务"
    systemctl stop "${SERVICE_NAME}" 2>/dev/null || true
    systemctl disable "${SERVICE_NAME}" 2>/dev/null || true
    log_step "删除全部文件 + 数据目录 + drop-in"
    rm -f "${BIN_PATH}" "${SERVICE_FILE}" "${HELPER_PATH}"
    # 同 uninstall：清理 change-listen-addr 写过的 drop-in。purge 语义是
    # "回到刚装 OS 那一刻"，残留 drop-in override 会让重装时困惑。
    rm -rf "/etc/systemd/system/${SERVICE_NAME}.service.d"
    rm -rf "${INSTALL_DIR}"
    systemctl daemon-reload
    log_info "彻底清理完成"
}

cmd_start()   { systemctl start "${SERVICE_NAME}";   log_info "已启动 ${SERVICE_NAME}"; }
cmd_stop()    { systemctl stop "${SERVICE_NAME}";    log_info "已停止 ${SERVICE_NAME}"; }
cmd_restart() { systemctl restart "${SERVICE_NAME}"; log_info "已重启 ${SERVICE_NAME}"; }
cmd_status()  { systemctl status "${SERVICE_NAME}" --no-pager || true; }
cmd_log()     { journalctl -u "${SERVICE_NAME}" -n 200 --no-pager || true; }
cmd_log_follow() {
    log_info "实时跟踪日志中（Ctrl+C 退出）..."
    journalctl -u "${SERVICE_NAME}" -f
}

cmd_reset_password() {
    if ! is_installed; then
        fail "未检测到已安装实例，请先运行 install。"
    fi
    "${BIN_PATH}" reset-password --db "${DATA_DIR}/bridge.db"
}

# cmd_change_listen_addr 引导运维写 systemd drop-in override 修改 BRIDGE_LISTEN_ADDR。
#
# 不直接改 settings 表里的 web.listen_addr：那是"启动期定型字段"——运行
# 时 PATCH 会被 Web Handler 拒绝，且需要重启进程才生效；运维体验差。
# drop-in override 的好处：systemctl edit 自动管理 mtime + reload，重启
# 即生效，可重复修改。
cmd_change_listen_addr() {
    if ! is_installed; then
        fail "未检测到已安装实例，请先运行 install。"
    fi
    echo
    printf "${bold}修改 Web 监听地址${plain}\n"
    printf "  示例：\n"
    printf "    :8787              ← 绑定全部网卡（默认；公网可访问）\n"
    printf "    127.0.0.1:8787     ← 仅本机（配合 nginx 反代时推荐）\n"
    printf "    192.168.1.10:8787  ← 仅指定网卡（多 IP 场景）\n"
    echo
    local addr
    read -rp "请输入新的监听地址：" addr
    addr="${addr// /}"
    if [[ -z "${addr}" ]]; then
        log_warn "监听地址不可为空，已取消"
        return 1
    fi
    # 极简格式校验：必须含冒号 + 冒号后是 1-65535 的端口数字。
    if ! [[ "${addr}" =~ ^.*:[0-9]{1,5}$ ]]; then
        fail "格式不合法（应为 host:port 形式）：${addr}"
    fi
    local override_dir="/etc/systemd/system/${SERVICE_NAME}.service.d"
    local override_file="${override_dir}/listen.conf"
    mkdir -p "${override_dir}"
    cat > "${override_file}" <<EOF
[Service]
Environment=BRIDGE_LISTEN_ADDR=${addr}
EOF
    log_step "已写入 drop-in override：${override_file}"
    systemctl daemon-reload
    if confirm_yes "立即重启服务以应用？"; then
        systemctl restart "${SERVICE_NAME}"
        log_info "已重启；新监听地址：${addr}"
    else
        log_info "已保存配置，下次重启生效"
    fi
}

cmd_show_menu() {
    while true; do
        echo
        printf "${bold}${cyan}============================================================${plain}\n"
        printf "${bold}  xboard-xui-bridge 管理菜单${plain}\n"
        printf "${bold}${cyan}============================================================${plain}\n"
        local active_label
        if service_active; then
            active_label="${green}● 运行中${plain}"
        elif is_installed; then
            active_label="${red}● 已停止${plain}"
        else
            active_label="${yellow}● 未安装${plain}"
        fi
        printf "  服务状态：%b\n" "${active_label}"
        echo
        printf "   1. 安装 / 升级\n"
        printf "   2. 启动服务\n"
        printf "   3. 停止服务\n"
        printf "   4. 重启服务\n"
        printf "   5. 查看运行状态（systemctl status）\n"
        printf "   6. 查看最近 200 行日志\n"
        printf "   7. 实时跟踪日志（journalctl -f）\n"
        printf "   8. 重置 admin 密码\n"
        printf "   9. 修改 Web 监听地址\n"
        printf "  10. 卸载（保留数据）\n"
        printf "  11. ${red}彻底清理${plain}（含数据，不可恢复）\n"
        printf "   0. 退出\n"
        echo
        local choice
        read -rp "请选择 [0-11]: " choice || return 0
        case "${choice}" in
            1)  cmd_install ;;
            2)  cmd_start ;;
            3)  cmd_stop ;;
            4)  cmd_restart ;;
            5)  cmd_status ;;
            6)  cmd_log ;;
            7)  cmd_log_follow ;;
            8)  cmd_reset_password ;;
            9)  cmd_change_listen_addr ;;
            10) cmd_uninstall ;;
            11) cmd_purge ;;
            0)  log_info "再见"; return 0 ;;
            *)  log_warn "无效选项：${choice}" ;;
        esac
    done
}

cmd_help() {
    cat <<EOF
xboard-xui-bridge 安装 / 管理脚本

用法：
  bash <(curl -fsSL https://raw.githubusercontent.com/${GITHUB_REPO}/main/install.sh)
                                  默认行为：未装 → 安装；已装 → 进菜单

  xui-bridge [子命令]              安装后可用的快捷别名

子命令：
  install / update / upgrade      安装或升级（保留 data/）
  uninstall                       卸载二进制 + service，保留 data/
  purge                           完全清理（含 data/，不可恢复）
  start | stop | restart          服务启停
  status                          打印 systemctl status
  log                             打印最近 200 行日志
  follow                          实时跟踪日志
  reset-password                  交互式重置 admin 密码
  change-listen-addr              交互式修改 Web 监听地址
  menu                            显式进入菜单（默认入口）
  help | --help | -h              打印本帮助

示例：
  xui-bridge                                    # 进菜单
  xui-bridge install                            # 一键升级到最新版
  xui-bridge restart && xui-bridge follow       # 重启后跟日志
  xui-bridge reset-password                     # 忘密码兜底重置
EOF
}

# ---------------- 主流程 ----------------
main() {
    require_root
    case "${1:-default}" in
        install|update|upgrade)
            cmd_install
            ;;
        uninstall|remove)
            cmd_uninstall
            ;;
        purge|clean)
            cmd_purge
            ;;
        start)              cmd_start ;;
        stop)               cmd_stop ;;
        restart)            cmd_restart ;;
        status)             cmd_status ;;
        log|logs)           cmd_log ;;
        follow|log-follow)  cmd_log_follow ;;
        reset-password|reset|password)
            cmd_reset_password
            ;;
        change-listen-addr|change-listen|listen)
            cmd_change_listen_addr
            ;;
        menu)
            cmd_show_menu
            ;;
        help|--help|-h)
            cmd_help
            ;;
        default)
            # 一键命令默认行为：未装 → 安装；已装 → 菜单。
            # 这是用户从 README 复制粘贴的核心 UX 保证。
            if is_installed; then
                cmd_show_menu
            else
                cmd_install
            fi
            ;;
        *)
            log_error "未知子命令：$1"
            cmd_help
            exit 2
            ;;
    esac
}

main "$@"
