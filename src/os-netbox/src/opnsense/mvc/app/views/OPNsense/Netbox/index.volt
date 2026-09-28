{#
 # Copyright (C) 2026 OPN-Box Team
 # All rights reserved.
 #}

<div class="content-box" style="padding-bottom: 1.5em;">
    <div class="col-xs-12">
        <h2>{{ lang._('NetBox Suite - 策略路由与智能分流控制台') }}</h2>
        <p class="text-muted">
            {{ lang._('统一纳管 MosDNS (粗粒度分流/白名单)、Hev-Socks5-Tunnel (极速 TUN 网卡)、Xray-core (7层应用特征细分流) 与 pf-aliasd 内核零首包竞态同步。') }}
        </p>
        <hr/>
        
        <!-- 全局服务控制按钮条 -->
        <div class="btn-group" style="margin-bottom: 20px;">
            <button id="btn-start-all" class="btn btn-primary">
                <i class="fa fa-play fa-fw"></i> {{ lang._('启动全套件') }}
            </button>
            <button id="btn-restart-all" class="btn btn-warning">
                <i class="fa fa-refresh fa-fw"></i> {{ lang._('重启全套件') }}
            </button>
            <button id="btn-stop-all" class="btn btn-danger">
                <i class="fa fa-stop fa-fw"></i> {{ lang._('停止全套件') }}
            </button>
            <button id="btn-update-rules" class="btn btn-info">
                <i class="fa fa-cloud-download fa-fw"></i> {{ lang._('全量更新规则库') }}
            </button>
            <button id="btn-refresh-status" class="btn btn-default">
                <i class="fa fa-heartbeat fa-fw"></i> {{ lang._('刷新状态') }}
            </button>
        </div>

        <div id="action-alert" class="alert alert-info" style="display: none;"></div>

        <!-- 核心模块卡片区 -->
        <div class="row">
            <!-- 1. MosDNS Controller -->
            <div class="col-md-4 col-sm-12">
                <div class="panel panel-default" style="border-radius: 6px; box-shadow: 0 2px 4px rgba(0,0,0,0.08);">
                    <div class="panel-heading" style="font-weight: bold; background-color: #f8fafc;">
                        <i class="fa fa-sitemap fa-fw text-primary"></i> MosDNS Controller
                        <span id="badge-mosdns" class="badge pull-right" style="background-color: #94a3b8;">检测中...</span>
                    </div>
                    <div class="panel-body">
                        <p class="text-muted" style="min-height: 48px;">
                            {{ lang._('DNS 粗粒度智能分流与域名规则库管理。支持自定义白名单直连、黑名单拦截及实时 DNS 解析流审计。') }}
                        </p>
                        <div style="margin-bottom: 12px;">
                            <small class="text-muted">{{ lang._('管理端口:') }} <code>:5380</code></small>
                        </div>
                        <div class="btn-group btn-group-justified">
                            <a id="link-mosdns" href="#" target="_blank" class="btn btn-sm btn-primary">
                                <i class="fa fa-external-link fa-fw"></i> {{ lang._('打开管理面板') }}
                            </a>
                            <a href="javascript:void(0);" onclick="openEmbeddedView('link-mosdns', 'MosDNS Controller');" class="btn btn-sm btn-default">
                                <i class="fa fa-window-maximize fa-fw"></i> {{ lang._('内嵌查看') }}
                            </a>
                        </div>
                    </div>
                </div>
            </div>

            <!-- 2. Tun2Socks (Hev-Socks5-Tunnel) -->
            <div class="col-md-4 col-sm-12">
                <div class="panel panel-default" style="border-radius: 6px; box-shadow: 0 2px 4px rgba(0,0,0,0.08);">
                    <div class="panel-heading" style="font-weight: bold; background-color: #f8fafc;">
                        <i class="fa fa-exchange fa-fw text-success"></i> Tun2Socks Manager
                        <span id="badge-tun2socks" class="badge pull-right" style="background-color: #94a3b8;">检测中...</span>
                    </div>
                    <div class="panel-body">
                        <p class="text-muted" style="min-height: 48px;">
                            {{ lang._('纯 C 协程极速 TUN 虚拟网卡代理桥接。负责将 FreeBSD 内核命中的 GFW_Proxy 流量极速注入 Socks5 代理管道。') }}
                        </p>
                        <div style="margin-bottom: 12px;">
                            <small class="text-muted">{{ lang._('管理端口:') }} <code>:5382</code></small>
                        </div>
                        <div class="btn-group btn-group-justified">
                            <a id="link-tun2socks" href="#" target="_blank" class="btn btn-sm btn-success">
                                <i class="fa fa-external-link fa-fw"></i> {{ lang._('打开管理面板') }}
                            </a>
                            <a href="javascript:void(0);" onclick="openEmbeddedView('link-tun2socks', 'Tun2Socks Manager');" class="btn btn-sm btn-default">
                                <i class="fa fa-window-maximize fa-fw"></i> {{ lang._('内嵌查看') }}
                            </a>
                        </div>
                    </div>
                </div>
            </div>

            <!-- 3. Xray Manager -->
            <div class="col-md-4 col-sm-12">
                <div class="panel panel-default" style="border-radius: 6px; box-shadow: 0 2px 4px rgba(0,0,0,0.08);">
                    <div class="panel-heading" style="font-weight: bold; background-color: #f8fafc;">
                        <i class="fa fa-shield fa-fw text-danger"></i> Xray Manager
                        <span id="badge-xray" class="badge pull-right" style="background-color: #94a3b8;">检测中...</span>
                    </div>
                    <div class="panel-body">
                        <p class="text-muted" style="min-height: 48px;">
                            {{ lang._('Xray-core 官方代理内核管理面板。支持订阅导入、7层 SNI 嗅探规则编辑、VLESS/xhttp 分流与局域网专属接入配置。') }}
                        </p>
                        <div style="margin-bottom: 12px;">
                            <small class="text-muted">{{ lang._('管理端口:') }} <code>:5384</code></small>
                        </div>
                        <div class="btn-group btn-group-justified">
                            <a id="link-xray" href="#" target="_blank" class="btn btn-sm btn-danger">
                                <i class="fa fa-external-link fa-fw"></i> {{ lang._('打开管理面板') }}
                            </a>
                            <a href="javascript:void(0);" onclick="openEmbeddedView('link-xray', 'Xray Manager');" class="btn btn-sm btn-default">
                                <i class="fa fa-window-maximize fa-fw"></i> {{ lang._('内嵌查看') }}
                            </a>
                        </div>
                    </div>
                </div>
            </div>
        </div>

        <!-- 运行诊断与输出日志框 -->
        <div class="panel panel-default" style="margin-top: 15px;">
            <div class="panel-heading" style="font-weight: bold;">
                <i class="fa fa-terminal fa-fw"></i> {{ lang._('全套件健康与状态诊断信息') }}
            </div>
            <div class="panel-body">
                <pre id="status-terminal" style="background: #0f172a; color: #38bdf8; min-height: 120px; font-family: monospace; padding: 12px; border-radius: 6px;">加载中...</pre>
            </div>
        </div>

        <!-- 内嵌查看容器 (Iframe View) -->
        <div id="embedded-container" class="panel panel-default" style="display: none; margin-top: 20px;">
            <div class="panel-heading" style="font-weight: bold;">
                <span id="embedded-title">{{ lang._('内嵌控制台') }}</span>
                <button type="button" class="close" onclick="$('#embedded-container').slideUp();">&times;</button>
            </div>
            <div class="panel-body" style="padding: 0;">
                <iframe id="embedded-iframe" src="" style="width: 100%; height: 750px; border: none;"></iframe>
            </div>
        </div>
    </div>
</div>

<script>
$(document).ready(function() {
    var host = window.location.hostname;
    var proto = window.location.protocol;

    // 动态绑定第三方面板实际访问地址
    var mosdnsUrl = proto + '//' + host + ':5380';
    var tun2socksUrl = proto + '//' + host + ':5382';
    var xrayUrl = proto + '//' + host + ':5384';

    $('#link-mosdns').attr('href', mosdnsUrl);
    $('#link-tun2socks').attr('href', tun2socksUrl);
    $('#link-xray').attr('href', xrayUrl);

    function refreshStatus() {
        $.getJSON('/api/netbox/service/status', function(data) {
            if (data && data.output) {
                $('#status-terminal').text(data.output);
                
                // 根据输出更新状态 Badge
                updateBadge('mosdns', data.output.indexOf('mosdns is running') !== -1 || data.output.indexOf('mosdns_controller is running') !== -1);
                updateBadge('tun2socks', data.output.indexOf('hev_socks5_tunnel is running') !== -1);
                updateBadge('xray', data.output.indexOf('xray is running') !== -1 || data.output.indexOf('xray_controller is running') !== -1);
            }
        });
    }

    function updateBadge(id, isRunning) {
        var el = $('#badge-' + id);
        if (isRunning) {
            el.text('运行中').css('background-color', '#10b981');
        } else {
            el.text('已停止').css('background-color', '#ef4444');
        }
    }

    function runServiceAction(actionName, btn) {
        btn.prop('disabled', true);
        $('#action-alert').removeClass('alert-danger alert-info').addClass('alert-warning').text('正在执行: ' + actionName + '，请稍候...').show();
        $.post('/api/netbox/service/' + actionName, {}, function(res) {
            btn.prop('disabled', false);
            $('#action-alert').removeClass('alert-warning').addClass('alert-success').text('操作执行完毕！').delay(3000).fadeOut();
            refreshStatus();
        }).fail(function() {
            btn.prop('disabled', false);
            $('#action-alert').removeClass('alert-warning').addClass('alert-danger').text('操作执行失败，请检查系统日志。');
        });
    }

    $('#btn-start-all').click(function() { runServiceAction('start', $(this)); });
    $('#btn-stop-all').click(function() { runServiceAction('stop', $(this)); });
    $('#btn-restart-all').click(function() { runServiceAction('restart', $(this)); });
    $('#btn-update-rules').click(function() { runServiceAction('updateRules', $(this)); });
    $('#btn-refresh-status').click(function() { refreshStatus(); });

    window.openEmbeddedView = function(linkId, title) {
        var url = $('#' + linkId).attr('href');
        $('#embedded-title').text(title + ' - ' + url);
        $('#embedded-iframe').attr('src', url);
        $('#embedded-container').slideDown();
        $('html, body').animate({
            scrollTop: $('#embedded-container').offset().top - 60
        }, 500);
    };

    refreshStatus();
});
</script>
