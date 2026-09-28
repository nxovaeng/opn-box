<?php

namespace OPNsense\Xray;

use OPNsense\Base\IndexController as BaseIndexController;

class IndexController extends BaseIndexController
{
    public function indexAction()
    {
        $this->view->title = gettext('Xray Manager');
        $this->view->pick('OPNsense/Xray/index');
    }
}
