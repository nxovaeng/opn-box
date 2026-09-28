#!/bin/sh
# ==============================================================================
# xray-control - Xray-core & Controller 专用服务控制脚本
# 适配 FreeBSD 官方 security/xray-core 包 (服务名: xray, 路径: /usr/local/etc/xray-core)
# ==============================================================================

ACTION="$1"
SERVICES="xray xray_controller"

pre_check() {
    mkdir -p /var/log /var/run /usr/local/etc/xray-core /usr/local/share/xray-core /usr/local/share/xray
    [ -L /usr/local/etc/xray ] || [ -d /usr/local/etc/xray ] || ln -s /usr/local/etc/xray-core /usr/local/etc/xray 2>/dev/null || true

    [ -f /usr/local/etc/xray-core/controller_data.json ] || [ ! -f /usr/local/etc/xray-core/controller_data.json.sample ] || cp /usr/local/etc/xray-core/controller_data.json.sample /usr/local/etc/xray-core/controller_data.json

    # 首次启动前确保基础配置文件存在，防止官方 rc.d/xray 启动致命报错
    if [ ! -f /usr/local/etc/xray-core/config.json ] && [ ! -f /usr/local/etc/xray-core/00_log.json ]; then
        if [ -f /usr/local/etc/xray-core/config.json.sample ]; then
            cp /usr/local/etc/xray-core/config.json.sample /usr/local/etc/xray-core/config.json
        fi
    fi

    # 确保清理可能残留的失效 PID 锁文件
    for pf in /var/run/xray.pid /var/run/xray-controller.pid; do
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
        # 二次核验，确保守护进程彻底退出以释放端口 5384 与代理入站
        sleep 0.5
        for proc in xray-controller xray; do
            if pgrep -x "${proc}" >/dev/null 2>&1; then
                pkill -KILL -x "${proc}" 2>/dev/null || true
            fi
        done
        rm -f /var/run/xray.pid /var/run/xray-controller.pid 2>/dev/null || true
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
