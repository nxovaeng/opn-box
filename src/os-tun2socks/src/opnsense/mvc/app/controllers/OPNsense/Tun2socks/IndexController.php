<?php

namespace OPNsense\Tun2socks;

use OPNsense\Base\IndexController as BaseIndexController;

class IndexController extends BaseIndexController
{
    public function indexAction()
    {
        $this->view->title = gettext('Tun2Socks Manager');
        $this->view->pick('OPNsense/Tun2socks/index');
    }
}
