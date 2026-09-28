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
            empty($trim) ||
            strpos($lower, 'action not allowed') !== false ||
            strpos($lower, 'action not found') !== false ||
            strpos($lower, 'not permitted') !== false ||
            strpos($lower, 'error: configd') !== false
        );
        return [
            'status' => $isError ? 'failed' : 'ok',
            'output' => !empty($trim) ? $trim : '执行失败：后台调度服务 (configd) 未返回任何输出，请检查 actions 配置或服务状态'
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
            $candidates = [
                'netbox update_rules',
                'netbox rules.update_mirror',
                'netbox rules.update',
                'netbox rules',
                'netbox rules_update'
            ];
            $lastResponse = '';
            foreach ($candidates as $cmd) {
                try {
                    $response = $backend->configdRun($cmd, false, 300);
                    $res = $this->parseResponse($response);
                    if ($res['status'] === 'ok' && !empty($res['output'])) {
                        return $res;
                    }
                    $lastResponse = $response;
                } catch (\Exception $e) {
                    $lastResponse = $e->getMessage();
                }
            }
            return $this->parseResponse($lastResponse);
        }
        return ['status' => 'failed', 'output' => 'Invalid request method'];
    }
}
