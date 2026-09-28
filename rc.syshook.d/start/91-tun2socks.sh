#!/bin/sh
# ==============================================================================
# 91-tun2socks.sh - OPNsense Bootup Service Hook for Tun2Socks
# ==============================================================================

if [ -x /usr/local/sbin/tun2socks-control ]; then
    /usr/local/sbin/tun2socks-control start >/dev/null 2>&1 || true
fi
