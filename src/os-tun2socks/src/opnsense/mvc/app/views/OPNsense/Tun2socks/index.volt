{#
 # Copyright (C) 2026 OPN-Box Team
 # All rights reserved.
 #}

<div class="content-box">
    <div class="col-xs-12">
        <h2>{{ lang._('Tun2Socks (Hev-Socks5-Tunnel) 管理控制台') }}</h2>
        <p class="text-muted">
            {{ lang._('基于纯 C 协程的极致性能 Tun2Socks 虚拟网卡代理与 Web 管理面板 (监听端口 :5382)。') }}
        </p>
        <hr/>

        <div style="margin-bottom: 15px;">
            <a id="ext-link-tun2socks" href="#" target="_blank" class="btn btn-success">
                <i class="fa fa-external-link fa-fw"></i> {{ lang._('新窗口打开独立 WebUI (:5382)') }}
            </a>
            <button class="btn btn-default" onclick="location.reload();">
                <i class="fa fa-refresh fa-fw"></i> {{ lang._('刷新界面') }}
            </button>
        </div>

        <div class="panel panel-default" style="border-radius: 6px; overflow: hidden; margin-top: 15px;">
            <div class="panel-heading" style="font-weight: bold;">
                <i class="fa fa-desktop fa-fw"></i> {{ lang._('内嵌管理视图 (Port :5382)') }}
            </div>
            <div class="panel-body" style="padding: 0;">
                <iframe id="tun2socks-frame" src="" style="width: 100%; height: 800px; border: none;"></iframe>
            </div>
        </div>
    </div>
</div>

<script>
$(document).ready(function() {
    var host = window.location.hostname;
    var proto = window.location.protocol;
    var targetUrl = proto + '//' + host + ':5382';
    $('#ext-link-tun2socks').attr('href', targetUrl);
    $('#tun2socks-frame').attr('src', targetUrl);
});
</script>
