#!/bin/sh
# ==============================================================================
# update_rules.sh - OPN-Box 规则库一键同步与更新脚本 (MosDNS + Xray)
#
# 功能说明：
# 1. 同步 MosDNS 粗粒度分流规则集合至 /usr/local/etc/mosdns/rule/
#    - cn.txt (国内直连域名大库)
#    - gfw.txt (出海代理名单)
#    - 自动补全缺省占位文件，杜绝 MosDNS 启动因文件不存在而 FATAL 崩溃
# 2. 同步 Xray-core 官方规则资产库至 /usr/local/share/xray-core/
#    - geosite.dat (Protobuf 7层应用特征库)
#    - geoip.dat (全球与中国 IP 分段库)
#    - 软链接兼容 /usr/local/share/xray
# 3. 采用临时文件原子替换，多 CDN 备用镜像轮询重试
# 4. 更新后平滑重载 mosdns 与 xray 服务 (使用 onerestart)
# ==============================================================================

RULE_DIR="/usr/local/etc/mosdns/rule"
XRAY_DIR="/usr/local/share/xray-core"
RESTART_SERVICES=1
MIRROR_ENABLED=0

# 多镜像通道
PRIMARY_BASE="https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download"
MIRROR_CDN="https://fastly.jsdelivr.net/gh/Loyalsoldier/v2ray-rules-dat@release"
MIRROR_GHPROXY="https://ghfast.top/https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download"

usage() {
    echo "用法: $0 [选项]"
    echo "选项:"
    echo "  --rule-dir <dir>   MosDNS 规则存放目录 (默认: ${RULE_DIR})"
    echo "  --xray-dir <dir>   Xray 规则存放目录 (默认: ${XRAY_DIR})"
    echo "  --mirror           优先使用国内加速镜像源同步"
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

fetch_url() {
    url="$1"
    output="$2"
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL --connect-timeout 8 -m 120 -o "${output}" "${url}" >/dev/null 2>&1
        return $?
    elif command -v fetch >/dev/null 2>&1; then
        fetch -q -T 10 -o "${output}" "${url}" >/dev/null 2>&1
        return $?
    fi
    return 1
}

download_file() {
    file_name="$1"
    dest_path="$2"
    tmp_path="${dest_path}.tmp"

    printf "  -> 正在获取 %-16s ... " "${file_name}"

    if [ "${MIRROR_ENABLED}" = "1" ]; then
        URLS="${MIRROR_CDN}/${file_name} ${MIRROR_GHPROXY}/${file_name} ${PRIMARY_BASE}/${file_name}"
    else
        URLS="${PRIMARY_BASE}/${file_name} ${MIRROR_CDN}/${file_name} ${MIRROR_GHPROXY}/${file_name}"
    fi

    success=0
    for u in ${URLS}; do
        if fetch_url "${u}" "${tmp_path}" && [ -s "${tmp_path}" ]; then
            success=1
            break
        fi
        rm -f "${tmp_path}" 2>/dev/null
    done

    if [ ${success} -eq 1 ] && [ -s "${tmp_path}" ]; then
        mv -f "${tmp_path}" "${dest_path}"
        echo "[成功: $(du -h "${dest_path}" | awk '{print $1}')]"
        return 0
    else
        rm -f "${tmp_path}" 2>/dev/null
        echo "[跳过/下载失败]"
        return 1
    fi
}

echo "=========================================================="
echo " 开始同步 OPN-Box 规则库 (MosDNS 粗分流 + Xray 7层细分流)"
echo " MosDNS 目标目录: ${RULE_DIR}"
echo " Xray 目标目录:   ${XRAY_DIR}"
echo "=========================================================="

mkdir -p "${RULE_DIR}" "${XRAY_DIR}" /usr/local/share/xray 2>/dev/null || true

echo "==> [1/2] 更新 MosDNS 纯文本 Set 集合..."
download_file "direct-list.txt" "${RULE_DIR}/cn.txt" || true
download_file "gfw.txt" "${RULE_DIR}/gfw.txt" || true

# 确保核心规则文件非空占位，防止 MosDNS domain_set fatal 退出
for r in cn.txt gfw.txt; do
    fpath="${RULE_DIR}/${r}"
    if [ ! -f "${fpath}" ] || [ ! -s "${fpath}" ]; then
        echo "# placeholder generated $(date)" > "${fpath}"
        if [ "${r}" = "cn.txt" ]; then
            echo "domain:internal.lan" >> "${fpath}"
        else
            echo "domain:google.com" >> "${fpath}"
        fi
        echo "  -> [保底] 为缺失的 ${r} 创建了非空占位记录"
    fi
done

# 初始化用户自定义列表
for c in custom-direct.txt custom-proxy.txt; do
    cpath="${RULE_DIR}/${c}"
    if [ ! -f "${cpath}" ]; then
        echo "# 自定义域名分流列表 (${c})" > "${cpath}"
        echo "  -> 已初始化用户自定义列表: ${c}"
    fi
done

echo "==> [2/2] 更新 Xray-core 二进制 Protobuf 特征库..."
download_file "geosite.dat" "${XRAY_DIR}/geosite.dat" || true
download_file "geoip.dat" "${XRAY_DIR}/geoip.dat" || true

# 保持 /usr/local/share/xray 与 /usr/local/share/xray-core 同步
for f in geosite.dat geoip.dat; do
    if [ -f "${XRAY_DIR}/${f}" ]; then
        cp -f "${XRAY_DIR}/${f}" "/usr/local/share/xray/${f}" 2>/dev/null || true
    fi
done

# 检查服务状态并平滑重载
if [ "${RESTART_SERVICES}" = "1" ]; then
    echo "==> [3/3] 检查并平滑重载运行中服务..."
    if pgrep -x "mosdns" >/dev/null 2>&1; then
        echo "  -> 重启 mosdns 服务..."
        service mosdns onerestart >/dev/null 2>&1 || /usr/local/etc/rc.d/mosdns onerestart >/dev/null 2>&1 || true
    fi
    if pgrep -x "xray" >/dev/null 2>&1; then
        echo "  -> 重启 xray 服务..."
        service xray onerestart >/dev/null 2>&1 || /usr/local/etc/rc.d/xray onerestart >/dev/null 2>&1 || true
    fi
fi

echo "=========================================================="
echo " 规则库同步完成！"
echo " MosDNS 规则列表: $(ls -1 ${RULE_DIR}/*.txt 2>/dev/null | xargs -n1 basename | tr '\n' ' ')"
echo " Xray 规则列表:   $(ls -1 ${XRAY_DIR}/*.dat 2>/dev/null | xargs -n1 basename | tr '\n' ' ')"
echo "=========================================================="
exit 0
