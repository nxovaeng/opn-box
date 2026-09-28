#!/bin/sh
# ==============================================================================
# 90-mosdns.sh - OPNsense Bootup Service Hook for MosDNS
# ==============================================================================

if [ -x /usr/local/sbin/mosdns-control ]; then
    /usr/local/sbin/mosdns-control start >/dev/null 2>&1 || true
fi
