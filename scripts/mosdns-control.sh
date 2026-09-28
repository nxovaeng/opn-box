#!/bin/sh
# ==============================================================================
# mosdns-control - MosDNS, pf-aliasd & Controller 专用服务控制脚本
# ==============================================================================

ACTION="$1"
SERVICES="pf_aliasd mosdns mosdns_controller"

pre_check() {
    mkdir -p /var/log /var/run /usr/local/etc/mosdns/rule
    for r in cn.txt gfw.txt custom-direct.txt custom-proxy.txt; do
        fpath="/usr/local/etc/mosdns/rule/${r}"
        if [ ! -f "${fpath}" ] || [ ! -s "${fpath}" ]; then
            echo "# placeholder for ${r}" > "${fpath}"
            [ "${r}" = "cn.txt" ] && echo "domain:internal.lan" >> "${fpath}"
            [ "${r}" = "gfw.txt" ] && echo "domain:google.com" >> "${fpath}"
        fi
    done
    [ -f /usr/local/etc/mosdns/config.yaml ] || [ ! -f /usr/local/etc/mosdns/config.yaml.sample ] || cp /usr/local/etc/mosdns/config.yaml.sample /usr/local/etc/mosdns/config.yaml

    # 确保清理可能残留的失效 PID 锁文件
    for pf in /var/run/pf-aliasd.pid /var/run/mosdns.pid /var/run/mosdns-controller.pid; do
        if [ -f "$pf" ]; then
            p=$(cat "$pf" 2>/dev/null)
            if [ -n "$p" ] && ! kill -0 "$p" 2>/dev/null; then
                rm -f "$pf"
            fi
        fi
    done
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
        echo "==> 启动 MosDNS 相关服务..."
        for s in ${SERVICES}; do
            svc_cmd "${s}" start
        done
        ;;
    stop)
        echo "==> 停止 MosDNS 相关服务..."
        for s in mosdns_controller mosdns pf_aliasd; do
            svc_cmd "${s}" stop
        done
        # 二次核验，确保守护进程彻底退出以释放端口与 PF 套接字
        sleep 0.5
        for proc in mosdns-controller mosdns pf-aliasd; do
            if pgrep -x "${proc}" >/dev/null 2>&1; then
                pkill -KILL -x "${proc}" 2>/dev/null || true
            fi
        done
        rm -f /var/run/pf-aliasd.pid /var/run/mosdns.pid /var/run/mosdns-controller.pid 2>/dev/null || true
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
