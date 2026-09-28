#!/bin/sh
# ==============================================================================
# tun2socks-control - Hev-Socks5-Tunnel & Controller 专用服务控制脚本
# ==============================================================================

ACTION="$1"
SERVICES="hev_socks5_tunnel hev_controller"

pre_check() {
    mkdir -p /var/log /var/run /usr/local/etc/hev-socks5-tunnel
    [ -f /usr/local/etc/hev-socks5-tunnel/config.yaml ] || [ ! -f /usr/local/etc/hev-socks5-tunnel/config.yaml.sample ] || cp /usr/local/etc/hev-socks5-tunnel/config.yaml.sample /usr/local/etc/hev-socks5-tunnel/config.yaml
    
    # 确保清理可能残留的失效 PID 锁文件
    for pf in /var/run/hev-socks5-tunnel.pid /var/run/hev-controller.pid; do
        if [ -f "$pf" ]; then
            p=$(cat "$pf" 2>/dev/null)
            if [ -n "$p" ] && ! kill -0 "$p" 2>/dev/null; then
                rm -f "$pf"
            fi
        fi
    done

    # 载入 FreeBSD if_tun 驱动模块
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
        # 二次核验，确保守护进程彻底退出以释放端口与 tun 网卡
        sleep 0.5
        for proc in hev-controller hev-socks5-tunnel; do
            if pgrep -x "${proc}" >/dev/null 2>&1; then
                pkill -KILL -x "${proc}" 2>/dev/null || true
            fi
        done
        rm -f /var/run/hev-socks5-tunnel.pid /var/run/hev-controller.pid 2>/dev/null || true
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
