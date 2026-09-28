<?php

namespace OPNsense\Netbox\Api;

use OPNsense\Base\ApiControllerBase;
use OPNsense\Core\Backend;

class ServiceController extends ApiControllerBase
{
    private function parseResponse($response)
    {
        $trim = trim($response ?? '');
        $lower = strtolower($trim);
        $isError = (
            strpos($lower, 'action not allowed') !== false ||
            strpos($lower, 'action not found') !== false ||
            strpos($lower, 'not permitted') !== false ||
            strpos($lower, 'error: configd') !== false
        );
        return [
            'status' => $isError ? 'failed' : 'ok',
            'output' => $trim
        ];
    }

    public function statusAction()
    {
        $backend = new Backend();
        $response = $backend->configdRun('netbox status');
        return $this->parseResponse($response);
    }

    public function startAction()
    {
        if ($this->request->isPost()) {
            $backend = new Backend();
            $response = $backend->configdRun('netbox start');
            return $this->parseResponse($response);
        }
        return ['status' => 'failed', 'output' => 'Invalid request method'];
    }

    public function stopAction()
    {
        if ($this->request->isPost()) {
            $backend = new Backend();
            $response = $backend->configdRun('netbox stop');
            return $this->parseResponse($response);
        }
        return ['status' => 'failed', 'output' => 'Invalid request method'];
    }

    public function restartAction()
    {
        if ($this->request->isPost()) {
            $backend = new Backend();
            $response = $backend->configdRun('netbox restart');
            return $this->parseResponse($response);
        }
        return ['status' => 'failed', 'output' => 'Invalid request method'];
    }

    public function updateRulesAction()
    {
        if ($this->request->isPost()) {
            $backend = new Backend();
            $response = $backend->configdRun('netbox rules.update');
            return $this->parseResponse($response);
        }
        return ['status' => 'failed', 'output' => 'Invalid request method'];
    }
}
