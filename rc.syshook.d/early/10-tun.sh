#!/bin/sh
# OPNsense early boot hook: Ensure tun kernel driver is loaded
kldstat -q -m if_tun || kldload if_tun 2>/dev/null || true
