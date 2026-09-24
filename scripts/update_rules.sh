#!/bin/sh
set -e

# ==============================================================================
# update_rules.sh - OPN-Box 规则库一键同步与更新脚本 (MosDNS + Xray)
#
# 功能说明：
# 1. 同步 MosDNS 粗粒度分流规则集合至 /usr/local/etc/mosdns/rule/
#    - cn.txt (国内直连域名大库，来源于 Loyalsoldier direct-list)
#    - gfw.txt (权威出海代理名单)
#    - 自动初始化用户自定义白名单/代理名单 (custom-direct.txt / custom-proxy.txt)
# 2. 同步 Xray 7 层精细分流规则库至 /usr/local/share/xray/
#    - geosite.dat (Protobuf 7层应用特征库: openai, netflix, google, cn 等)
#    - geoip.dat (全球与中国 IP 分段库)
# 3. 采用原子下载机制 (下载至 .tmp 校验后替换)，避免断网或残损
# 4. 支持 FreeBSD fetch 与 curl 双下载器，内置 GitHub / CDN 备用镜像通道
# 5. 更新成功后自动优雅重载/重启运行中的 mosdns 与 xray 服务
# ==============================================================================

RULE_DIR="/usr/local/etc/mosdns/rule"
XRAY_DIR="/usr/local/share/xray"
RESTART_SERVICES=1
MIRROR_ENABLED=0

# 上游主源与备用加速镜像源 (Loyalsoldier v2ray-rules-dat)
PRIMARY_BASE="https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download"
MIRROR_BASE="https://raw.gitmirror.com/Loyalsoldier/v2ray-rules-dat/release"

usage() {
    echo "用法: $0 [选项]"
    echo "选项:"
    echo "  --rule-dir <dir>   MosDNS 规则存放目录 (默认: ${RULE_DIR})"
    echo "  --xray-dir <dir>   Xray 规则存放目录 (默认: ${XRAY_DIR})"
    echo "  --mirror           使用国内加速镜像源同步"
    echo "  --no-restart       更新后不自动重启相关服务"
    echo "  -h, --help         显示帮助信息"
    exit 0
}

while [ $# -gt 0 ]; do
    case "$1" in
        --rule-dir) RULE_DIR="$2"; shift 2 ;;
        --xray-dir) XRAY_DIR="$2"; shift 2 ;;
        --mirror) MIRROR_ENABLED=1; shift 1 ;;
        --no-restart) RESTART_SERVICES=0; shift 1 ;;
        -h|--help) usage ;;
        *) echo "未知参数: $1" >&2; exit 1 ;;
    esac
done

if [ "${MIRROR_ENABLED}" = "1" ]; then
    BASE_URL="${MIRROR_BASE}"
else
    BASE_URL="${PRIMARY_BASE}"
fi

download_file() {
    file_name="$1"
    dest_path="$2"
    url="${BASE_URL}/${file_name}"
    tmp_path="${dest_path}.tmp"

    printf "  -> 正在获取 %-16s ... " "${file_name}"

    download_ok=0
    if command -v curl >/dev/null 2>&1; then
        if curl -fsSL --connect-timeout 10 -m 180 -o "${tmp_path}" "${url}" >/dev/null 2>&1; then
            download_ok=1
        fi
    elif command -v fetch >/dev/null 2>&1; then
        if fetch -q -T 15 -o "${tmp_path}" "${url}" >/dev/null 2>&1; then
            download_ok=1
        fi
    fi

    # 如果主源失败且尚未尝试备用镜像，尝试备用镜像重试一次
    if [ ${download_ok} -eq 0 ] && [ "${BASE_URL}" != "${MIRROR_BASE}" ]; then
        alt_url="${MIRROR_BASE}/${file_name}"
        if command -v curl >/dev/null 2>&1; then
            curl -fsSL --connect-timeout 10 -m 180 -o "${tmp_path}" "${alt_url}" >/dev/null 2>&1 && download_ok=1
        elif command -v fetch >/dev/null 2>&1; then
            fetch -q -T 15 -o "${tmp_path}" "${alt_url}" >/dev/null 2>&1 && download_ok=1
        fi
    fi

    if [ ${download_ok} -eq 1 ] && [ -s "${tmp_path}" ]; then
        mv -f "${tmp_path}" "${dest_path}"
        echo "[成功]"
    else
        rm -f "${tmp_path}"
        echo "[失败]"
        return 1
    fi
}

echo "=========================================================="
echo " 开始同步 OPN-Box 规则库 (MosDNS 粗分流 + Xray 7层细分流)"
echo " MosDNS 目标目录: ${RULE_DIR}"
echo " Xray 目标目录:   ${XRAY_DIR}"
echo " 主源基址:        ${BASE_URL}"
echo "=========================================================="

mkdir -p "${RULE_DIR}" "${XRAY_DIR}"

echo "==> [1/2] 更新 MosDNS 纯文本 Set 集合..."
download_file "direct-list.txt" "${RULE_DIR}/cn.txt" || true
download_file "gfw.txt" "${RULE_DIR}/gfw.txt" || true

# 初始化自定义列表模板文件 (若不存在则创建，永久保留用户编辑)
if [ ! -f "${RULE_DIR}/custom-direct.txt" ]; then
    cat << 'EOF' > "${RULE_DIR}/custom-direct.txt"
# 自定义国内/直连白名单域名 (每行一个，支持 domain: / full:)
# 示例:
# domain:myprivate.lan
# domain:internal-company.cn
EOF
    echo "  -> 已初始化 ${RULE_DIR}/custom-direct.txt"
fi

if [ ! -f "${RULE_DIR}/custom-proxy.txt" ]; then
    cat << 'EOF' > "${RULE_DIR}/custom-proxy.txt"
# 自定义出海代理黑名单域名 (每行一个，支持 domain: / full:)
# 示例:
# domain:myprivatevps.com
# domain:specific-foreign-site.org
EOF
    echo "  -> 已初始化 ${RULE_DIR}/custom-proxy.txt"
fi

echo "==> [2/2] 更新 Xray-core 二进制 Protobuf 特征库..."
download_file "geosite.dat" "${XRAY_DIR}/geosite.dat" || true
download_file "geoip.dat" "${XRAY_DIR}/geoip.dat" || true

# 检查服务状态并平滑重载
if [ "${RESTART_SERVICES}" = "1" ]; then
    echo "==> [3/3] 检查并重载运行中服务..."
    if pgrep -x "mosdns" >/dev/null 2>&1; then
        echo "  -> 正在平滑重启 mosdns 服务..."
        service mosdns restart >/dev/null 2>&1 || /usr/local/etc/rc.d/mosdns restart >/dev/null 2>&1 || true
    fi
    if pgrep -x "xray" >/dev/null 2>&1; then
        echo "  -> 正在平滑重启 xray 服务..."
        service xray restart >/dev/null 2>&1 || /usr/local/etc/rc.d/xray restart >/dev/null 2>&1 || true
    fi
fi

echo "=========================================================="
echo " 规则库同步完成！"
echo " MosDNS 规则: $(ls -1 ${RULE_DIR}/*.txt 2>/dev/null | tr '\n' ' ')"
echo " Xray 规则:   $(ls -1 ${XRAY_DIR}/*.dat 2>/dev/null | tr '\n' ' ')"
echo "=========================================================="
