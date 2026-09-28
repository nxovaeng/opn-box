#!/bin/sh
# ==============================================================================
# opnbox-control - NetBox Suite 全套件统一控制与巡检脚本
# 作用: 供 OPNsense configd (actions_netbox.conf) 及系统管理员命令行调用
# 语法: opnbox-control {start|stop|restart|status} [all|mosdns|tun2socks|xray]
# ==============================================================================

ACTION="$1"
TARGET="${2:-all}"

START_ORDER="pf_aliasd mosdns mosdns_controller hev_socks5_tunnel hev_controller xray xray_controller"
STOP_ORDER="xray_controller xray hev_controller hev_socks5_tunnel mosdns_controller mosdns pf_aliasd"

pre_check() {
    # 1. 确保日志与运行目录存在
    mkdir -p /var/log /var/run \
             /usr/local/etc/mosdns/rule \
             /usr/local/etc/hev-socks5-tunnel \
             /usr/local/etc/xray-core \
             /usr/local/share/xray-core \
             /usr/local/share/xray 2>/dev/null || true

    # 2. 兼容软链接 /usr/local/share/xray -> /usr/local/share/xray-core
    if [ ! -d /usr/local/share/xray-core ]; then
        mkdir -p /usr/local/share/xray-core
    fi

    # 3. 确保 MosDNS 所需的基础规则文件存在且非空，防止 MosDNS 初始化 domain_set 致命报错
    for r in cn.txt gfw.txt custom-direct.txt custom-proxy.txt; do
        fpath="/usr/local/etc/mosdns/rule/${r}"
        if [ ! -f "${fpath}" ] || [ ! -s "${fpath}" ]; then
            echo "# placeholder for ${r}" > "${fpath}"
            if [ "${r}" = "cn.txt" ]; then
                echo "domain:internal.lan" >> "${fpath}"
            elif [ "${r}" = "gfw.txt" ]; then
                echo "domain:google.com" >> "${fpath}"
            fi
        fi
    done

    # 4. 自动从 .sample 补齐缺失的主配置文件
    [ -f /usr/local/etc/mosdns/config.yaml ] || [ ! -f /usr/local/etc/mosdns/config.yaml.sample ] || cp /usr/local/etc/mosdns/config.yaml.sample /usr/local/etc/mosdns/config.yaml
    [ -f /usr/local/etc/mosdns/controller_settings.json ] || echo '{}' > /usr/local/etc/mosdns/controller_settings.json
    [ -f /usr/local/etc/hev-socks5-tunnel/config.yaml ] || [ ! -f /usr/local/etc/hev-socks5-tunnel/config.yaml.sample ] || cp /usr/local/etc/hev-socks5-tunnel/config.yaml.sample /usr/local/etc/hev-socks5-tunnel/config.yaml
    [ -f /usr/local/etc/xray-core/controller_data.json ] || [ ! -f /usr/local/etc/xray-core/controller_data.json.sample ] || cp /usr/local/etc/xray-core/controller_data.json.sample /usr/local/etc/xray-core/controller_data.json

    # 5. 确保 tun 虚拟网卡驱动内核模块已载入
    kldstat -q -m if_tun || kldload if_tun 2>/dev/null || true
}

svc_cmd() {
    svc="$1"
    act="$2"
    if [ -x "/usr/local/etc/rc.d/${svc}" ]; then
        /usr/local/etc/rc.d/${svc} "one${act}" 2>&1
    elif [ -x "/etc/rc.d/${svc}" ]; then
        /etc/rc.d/${svc} "one${act}" 2>&1
    else
        service "${svc}" "one${act}" 2>&1
    fi
}

do_start() {
    pre_check
    case "$1" in
        all)
            echo "==> [NetBox] 正在启动全套件服务..."
            for s in ${START_ORDER}; do
                printf "  -> 启动 %-22s ... " "${s}"
                res=$(svc_cmd "${s}" start)
                echo "${res}"
            done
            echo "==> 启动完毕，当前全套件健康状态："
            do_status all
            ;;
        mosdns)
            for s in pf_aliasd mosdns mosdns_controller; do
                svc_cmd "${s}" start
            done
            ;;
        tun2socks|hev)
            for s in hev_socks5_tunnel hev_controller; do
                svc_cmd "${s}" start
            done
            ;;
        xray)
            for s in xray xray_controller; do
                svc_cmd "${s}" start
            done
            ;;
        *)
            svc_cmd "$1" start
            ;;
    esac
}

do_stop() {
    case "$1" in
        all)
            echo "==> [NetBox] 正在停止全套件服务..."
            for s in ${STOP_ORDER}; do
                printf "  -> 停止 %-22s ... " "${s}"
                res=$(svc_cmd "${s}" stop)
                echo "${res}"
            done
            echo "==> 停止完毕，当前全套件健康状态："
            do_status all
            ;;
        mosdns)
            for s in mosdns_controller mosdns pf_aliasd; do
                svc_cmd "${s}" stop
            done
            ;;
        tun2socks|hev)
            for s in hev_controller hev_socks5_tunnel; do
                svc_cmd "${s}" stop
            done
            ;;
        xray)
            for s in xray_controller xray; do
                svc_cmd "${s}" stop
            done
            ;;
        *)
            svc_cmd "$1" stop
            ;;
    esac
}

do_restart() {
    do_stop "$1"
    sleep 1
    do_start "$1"
}

do_status() {
    case "$1" in
        all)
            echo "--- NetBox 套件运行状态 ---"
            for s in ${START_ORDER}; do
                svc_cmd "${s}" status
            done
            ;;
        mosdns)
            for s in pf_aliasd mosdns mosdns_controller; do
                svc_cmd "${s}" status
            done
            ;;
        tun2socks|hev)
            for s in hev_socks5_tunnel hev_controller; do
                svc_cmd "${s}" status
            done
            ;;
        xray)
            for s in xray xray_controller; do
                svc_cmd "${s}" status
            done
            ;;
        *)
            svc_cmd "$1" status
            ;;
    esac
}

case "${ACTION}" in
    start)
        do_start "${TARGET}"
        ;;
    stop)
        do_stop "${TARGET}"
        ;;
    restart)
        do_restart "${TARGET}"
        ;;
    status)
        do_status "${TARGET}"
        ;;
    *)
        echo "用法: $0 {start|stop|restart|status} [all|mosdns|tun2socks|xray|<service>]"
        exit 1
        ;;
esac
