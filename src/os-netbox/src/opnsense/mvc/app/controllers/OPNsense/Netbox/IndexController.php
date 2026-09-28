<?php

namespace OPNsense\Netbox;

use OPNsense\Base\IndexController as BaseIndexController;

class IndexController extends BaseIndexController
{
    public function indexAction()
    {
        $this->view->title = gettext('NetBox Suite Dashboard');
        $this->view->pick('OPNsense/Netbox/index');
    }
}
