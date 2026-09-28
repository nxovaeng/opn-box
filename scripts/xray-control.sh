#!/bin/sh
# ==============================================================================
# xray-control - Xray-core & Controller 专用服务控制脚本
# 适配 FreeBSD 官方 security/xray-core 包 (服务名: xray, 路径: /usr/local/etc/xray-core)
# ==============================================================================

ACTION="$1"
SERVICES="xray xray_controller"

pre_check() {
    mkdir -p /var/log /var/run /usr/local/etc/xray-core /usr/local/share/xray-core /usr/local/share/xray
    [ -f /usr/local/etc/xray-core/controller_data.json ] || [ ! -f /usr/local/etc/xray-core/controller_data.json.sample ] || cp /usr/local/etc/xray-core/controller_data.json.sample /usr/local/etc/xray-core/controller_data.json
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
        echo "==> 启动 Xray 相关服务..."
        for s in ${SERVICES}; do
            svc_cmd "${s}" start
        done
        ;;
    stop)
        echo "==> 停止 Xray 相关服务..."
        for s in xray_controller xray; do
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
