<?php

namespace OPNsense\Netbox\Api;

use OPNsense\Base\ApiControllerBase;
use OPNsense\Core\Backend;

class ServiceController extends ApiControllerBase
{
    public function statusAction()
    {
        $backend = new Backend();
        $response = $backend->configdRun('netbox status');
        return [
            'status' => 'ok',
            'output' => trim($response)
        ];
    }

    public function startAction()
    {
        if ($this->request->isPost()) {
            $backend = new Backend();
            $response = $backend->configdRun('netbox start');
            return ['status' => 'ok', 'output' => trim($response)];
        }
        return ['status' => 'failed'];
    }

    public function stopAction()
    {
        if ($this->request->isPost()) {
            $backend = new Backend();
            $response = $backend->configdRun('netbox stop');
            return ['status' => 'ok', 'output' => trim($response)];
        }
        return ['status' => 'failed'];
    }

    public function restartAction()
    {
        if ($this->request->isPost()) {
            $backend = new Backend();
            $response = $backend->configdRun('netbox restart');
            return ['status' => 'ok', 'output' => trim($response)];
        }
        return ['status' => 'failed'];
    }

    public function updateRulesAction()
    {
        if ($this->request->isPost()) {
            $backend = new Backend();
            $response = $backend->configdRun('netbox rules.update');
            return ['status' => 'ok', 'output' => trim($response)];
        }
        return ['status' => 'failed'];
    }
}
