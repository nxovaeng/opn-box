#!/bin/sh
# ==============================================================================
# 95-netbox.sh - OPNsense Bootup Service Hook for NetBox Suite
# Triggered automatically after networking is fully configured on boot
# ==============================================================================

if [ -x /usr/local/sbin/opnbox-control ]; then
    /usr/local/sbin/opnbox-control start all >/dev/null 2>&1 || true
fi
