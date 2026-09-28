<?php

/*
 * Copyright (C) 2026 OPN-Box Team
 * All rights reserved.
 */

function xray_services()
{
    $services = array();
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
