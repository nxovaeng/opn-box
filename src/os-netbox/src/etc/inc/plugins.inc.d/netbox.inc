<?php

/*
 * Copyright (C) 2026 OPN-Box Team
 * All rights reserved.
 */

function netbox_services()
{
    $services = array();

    // 1. NetBox 全套件统一纳管服务
    $services[] = array(
        'description' => gettext('NetBox Suite (MosDNS + Tun2Socks + Xray)'),
        'configd' => array(
            'restart' => array('netbox restart'),
            'start' => array('netbox start'),
            'stop' => array('netbox stop'),
        ),
        'name' => 'netbox',
        'pidfile' => '/var/run/hev-socks5-tunnel.pid',
    );

    // 2. MosDNS 动态分流服务
    $services[] = array(
        'description' => gettext('MosDNS Dynamic DNS Forwarder'),
        'configd' => array(
            'restart' => array('mosdns dns.restart'),
            'start' => array('mosdns dns.start'),
            'stop' => array('mosdns dns.stop'),
        ),
        'name' => 'mosdns',
        'pidfile' => '/var/run/mosdns.pid',
    );

    // 3. Tun2Socks (Hev-Socks5-Tunnel) 虚拟网卡代理
    $services[] = array(
        'description' => gettext('Tun2Socks High-Performance Bridge'),
        'configd' => array(
            'restart' => array('tun2socks tunnel.restart'),
            'start' => array('tun2socks tunnel.start'),
            'stop' => array('tun2socks tunnel.stop'),
        ),
        'name' => 'hev_socks5_tunnel',
        'pidfile' => '/var/run/hev-socks5-tunnel.pid',
    );

    // 4. Xray-core 官方代理内核
    $services[] = array(
        'description' => gettext('Xray-core Proxy Engine'),
        'configd' => array(
            'restart' => array('xray core.restart'),
            'start' => array('xray core.start'),
            'stop' => array('xray core.stop'),
        ),
        'name' => 'xray',
        'pidfile' => '/var/run/xray.pid',
    );

    return $services;
}
