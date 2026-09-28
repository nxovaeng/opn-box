#!/bin/sh
# ==============================================================================
# 92-xray.sh - OPNsense Bootup Service Hook for Xray
# ==============================================================================

if [ -x /usr/local/sbin/xray-control ]; then
    /usr/local/sbin/xray-control start >/dev/null 2>&1 || true
fi
