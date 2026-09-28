{#
 # Copyright (C) 2026 OPN-Box Team
 # All rights reserved.
 #}

<div class="content-box">
    <div class="col-xs-12">
        <h2>{{ lang._('Xray Manager 管理控制台') }}</h2>
        <p class="text-muted">
            {{ lang._('Xray-core 官方代理内核管理面板 (监听端口 :5384)。支持订阅导入、7层 SNI 嗅探规则编辑、VLESS/xhttp 分流与局域网专属接入。') }}
        </p>
        <hr/>

        <div style="margin-bottom: 15px;">
            <a id="ext-link-xray" href="#" target="_blank" class="btn btn-danger">
                <i class="fa fa-external-link fa-fw"></i> {{ lang._('新窗口打开独立 WebUI (:5384)') }}
            </a>
            <button class="btn btn-default" onclick="location.reload();">
                <i class="fa fa-refresh fa-fw"></i> {{ lang._('刷新界面') }}
            </button>
        </div>

        <div class="panel panel-default" style="border-radius: 6px; overflow: hidden; margin-top: 15px;">
            <div class="panel-heading" style="font-weight: bold;">
                <i class="fa fa-desktop fa-fw"></i> {{ lang._('内嵌管理视图 (Port :5384)') }}
            </div>
            <div class="panel-body" style="padding: 0;">
                <iframe id="xray-frame" src="" style="width: 100%; height: 800px; border: none;"></iframe>
            </div>
        </div>
    </div>
</div>

<script>
$(document).ready(function() {
    var host = window.location.hostname;
    var proto = window.location.protocol;
    var targetUrl = proto + '//' + host + ':5384';
    $('#ext-link-xray').attr('href', targetUrl);
    $('#xray-frame').attr('src', targetUrl);
});
</script>
