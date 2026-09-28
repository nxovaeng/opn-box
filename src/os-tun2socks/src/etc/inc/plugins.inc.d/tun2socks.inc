<?php

/*
 * Copyright (C) 2026 OPN-Box Team
 * All rights reserved.
 */

function tun2socks_services()
{
    $services = array();
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
    return $services;
}
