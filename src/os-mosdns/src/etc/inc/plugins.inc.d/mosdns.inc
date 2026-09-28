<?php

/*
 * Copyright (C) 2026 OPN-Box Team
 * All rights reserved.
 */

function mosdns_services()
{
    $services = array();
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
    return $services;
}
