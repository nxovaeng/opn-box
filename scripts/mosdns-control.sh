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
