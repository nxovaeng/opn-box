<?php

namespace OPNsense\Mosdns;

use OPNsense\Base\IndexController as BaseIndexController;

class IndexController extends BaseIndexController
{
    public function indexAction()
    {
        $this->view->title = gettext('MosDNS & Controller');
        $this->view->pick('OPNsense/Mosdns/index');
    }
}
