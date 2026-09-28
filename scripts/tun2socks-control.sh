#!/bin/sh
# ==============================================================================
# tun2socks-control - Hev-Socks5-Tunnel & Controller 专用服务控制脚本
# ==============================================================================

ACTION="$1"
SERVICES="hev_socks5_tunnel hev_controller"

pre_check() {
    mkdir -p /var/log /var/run /usr/local/etc/hev-socks5-tunnel
    [ -f /usr/local/etc/hev-socks5-tunnel/config.yaml ] || [ ! -f /usr/local/etc/hev-socks5-tunnel/config.yaml.sample ] || cp /usr/local/etc/hev-socks5-tunnel/config.yaml.sample /usr/local/etc/hev-socks5-tunnel/config.yaml
    kldstat -q -m if_tun || kldload if_tun 2>/dev/null || true
}

svc_cmd() {
    svc="$1"
    act="$2"
    if [ -x "/usr/local/etc/rc.d/${svc}" ]; then
        /usr/local/etc/rc.d/${svc} "one${act}" 2>&1
    elif service -e | grep -q "${svc}"; then
        service "${svc}" "one${act}" 2>&1
    else
        echo "${svc}: rc.d 脚本未安装"
    fi
}

case "${ACTION}" in
    start)
        pre_check
        echo "==> 启动 Tun2Socks 相关服务..."
        for s in ${SERVICES}; do
            svc_cmd "${s}" start
        done
        ;;
    stop)
        echo "==> 停止 Tun2Socks 相关服务..."
        for s in hev_controller hev_socks5_tunnel; do
            svc_cmd "${s}" stop
        done
        ;;
    restart)
        $0 stop
        sleep 1
        $0 start
        ;;
    status)
        for s in ${SERVICES}; do
            svc_cmd "${s}" status
        done
        ;;
    *)
        echo "用法: $0 {start|stop|restart|status}"
        exit 1
        ;;
esac
