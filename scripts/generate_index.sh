#!/bin/sh
set -e

# ==============================================================================
# generate_index.sh - 生成 OPNsense 软件源仓库与技术文档中心门户 (index.html / MkDocs)
# ==============================================================================

WORKSPACE_DIR="$(cd "$(dirname "$0")/.." && pwd)"
PUBLIC_DIR="${1:-${WORKSPACE_DIR}/public}"
REPO_OWNER="${2:-opn-box}"
REPO_NAME="${3:-opn-box}"
CUSTOM_DOMAIN="${4:-${CUSTOM_DOMAIN:-opnbox.zro.qzz.io}}"

mkdir -p "${PUBLIC_DIR}"
touch "${PUBLIC_DIR}/.nojekyll"

# 确定对外基础 URL 与 CNAME 域名配置
if [ -n "${CUSTOM_DOMAIN}" ]; then
  BASE_URL="https://${CUSTOM_DOMAIN}"
  echo "${CUSTOM_DOMAIN}" > "${PUBLIC_DIR}/CNAME"
else
  BASE_URL="https://${REPO_OWNER}.github.io/${REPO_NAME}"
fi

# ------------------------------------------------------------------------------
# 1. 优先检测 MkDocs：若存在则基于 docs/ Markdown 文档构建全套静态文档站点
# ------------------------------------------------------------------------------
if command -v mkdocs >/dev/null 2>&1 && [ -f "${WORKSPACE_DIR}/mkdocs.yml" ]; then
  echo "==> 检测到 mkdocs 命令，正在通过 MkDocs 将 docs/ 编译为现代化技术文档与软件源门户..."
  
  # 使用 --dirty 选项构建，避免覆盖清理掉已存在的 FreeBSD:15:amd64 软件源及 opnbox.conf
  cd "${WORKSPACE_DIR}"
  mkdocs build --site-dir "${PUBLIC_DIR}" --dirty

  # 重新确认 CNAME 与 .nojekyll 存在
  [ -n "${CUSTOM_DOMAIN}" ] && echo "${CUSTOM_DOMAIN}" > "${PUBLIC_DIR}/CNAME"
  touch "${PUBLIC_DIR}/.nojekyll"

  echo "==> MkDocs 文档站点构建完成！输出目录: ${PUBLIC_DIR}"
  exit 0
fi

# ------------------------------------------------------------------------------
# 2. 回退机制：若无 MkDocs，生成现代化暗黑风格原生落地页 (index.html)
# ------------------------------------------------------------------------------
echo "==> 未检测到 mkdocs 命令，生成现代化独立落地页 (index.html)..."
INDEX_FILE="${PUBLIC_DIR}/index.html"

cat << EOF > "${INDEX_FILE}"
<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>OPNsense 26.x 专属软件源与技术文档 - opn-box</title>
  <style>
    :root {
      --bg-main: #0b1120;
      --card-bg: #1e293b;
      --card-border: #334155;
      --text-main: #f8fafc;
      --text-muted: #94a3b8;
      --primary: #38bdf8;
      --primary-hover: #0284c7;
      --accent: #f97316;
      --code-bg: #030712;
      --success: #10b981;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      padding: 2.5rem 1rem;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
      background-color: var(--bg-main);
      color: var(--text-main);
      line-height: 1.6;
    }
    .container { max-width: 960px; margin: 0 auto; }
    header {
      margin-bottom: 2rem;
      border-bottom: 1px solid var(--card-border);
      padding-bottom: 1.5rem;
    }
    h1 {
      margin: 0 0 0.5rem 0;
      font-size: 2.2rem;
      color: var(--primary);
      display: flex;
      align-items: center;
      gap: 0.5rem;
    }
    .subtitle {
      color: var(--text-muted);
      margin: 0.5rem 0 0 0;
      font-size: 1.05rem;
    }
    .badges { display: flex; gap: 0.5rem; flex-wrap: wrap; margin-top: 1rem; }
    .badge {
      padding: 0.3rem 0.75rem;
      font-size: 0.75rem;
      font-weight: 600;
      border-radius: 9999px;
      background: rgba(56, 189, 248, 0.12);
      color: var(--primary);
      border: 1px solid rgba(56, 189, 248, 0.28);
    }
    .badge-accent {
      background: rgba(249, 115, 22, 0.12);
      color: var(--accent);
      border-color: rgba(249, 115, 22, 0.28);
    }
    .card {
      background: var(--card-bg);
      border: 1px solid var(--card-border);
      border-radius: 12px;
      padding: 1.5rem;
      margin-bottom: 1.5rem;
      box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.2);
    }
    .card h2 {
      margin-top: 0;
      font-size: 1.3rem;
      color: var(--text-main);
      display: flex;
      align-items: center;
      gap: 0.5rem;
    }
    .code-container {
      position: relative;
      margin-top: 0.75rem;
    }
    pre {
      background: var(--code-bg);
      padding: 1.1rem 1rem;
      border-radius: 8px;
      overflow-x: auto;
      color: #e2e8f0;
      border: 1px solid var(--card-border);
      font-size: 0.9rem;
      margin: 0;
    }
    code { font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; }
    .copy-btn {
      position: absolute;
      top: 0.6rem;
      right: 0.6rem;
      background: #334155;
      color: #f8fafc;
      border: 1px solid #475569;
      border-radius: 6px;
      padding: 0.35rem 0.7rem;
      font-size: 0.75rem;
      cursor: pointer;
      transition: all 0.2s ease;
    }
    .copy-btn:hover { background: var(--primary); color: #0f172a; }
    table { width: 100%; border-collapse: collapse; margin-top: 0.75rem; font-size: 0.9rem; }
    th, td {
      padding: 0.75rem 0.85rem;
      text-align: left;
      border-bottom: 1px solid var(--card-border);
    }
    th { color: var(--text-muted); font-size: 0.8rem; text-transform: uppercase; letter-spacing: 0.05em; }
    a { color: var(--primary); text-decoration: none; font-weight: 500; }
    a:hover { text-decoration: underline; color: #7dd3fc; }
    .docs-grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
      gap: 1rem;
      margin-top: 0.75rem;
    }
    .doc-item {
      background: #0f172a;
      border: 1px solid var(--card-border);
      border-radius: 8px;
      padding: 1rem;
      transition: transform 0.15s ease, border-color 0.15s ease;
    }
    .doc-item:hover {
      transform: translateY(-2px);
      border-color: var(--primary);
    }
    .doc-item h3 {
      margin: 0 0 0.35rem 0;
      font-size: 1rem;
    }
    .doc-item p {
      margin: 0;
      font-size: 0.825rem;
      color: var(--text-muted);
      line-height: 1.45;
    }
    .footer {
      text-align: center;
      margin-top: 3rem;
      color: var(--text-muted);
      font-size: 0.875rem;
      border-top: 1px solid var(--card-border);
      padding-top: 1.5rem;
    }
  </style>
</head>
<body>
  <div class="container">
    <header>
      <h1>📦 opn-box 专属软件源</h1>
      <p class="subtitle">
        专为 <strong>OPNsense 26.x (FreeBSD:15:amd64)</strong> 打造的高性能策略路由与 DNS 动态分流套件
      </p>
      <div class="badges">
        <span class="badge">FreeBSD 15 专版 (ABI)</span>
        <span class="badge">100% 源码自编译</span>
        <span class="badge">零首包竞态 (pf-aliasd)</span>
        <span class="badge badge-accent">自研 MosDNS Controller</span>
        <span class="badge">纯 C 协程 hev-socks5-tunnel</span>
        <span class="badge">Xray 7层细分流</span>
      </div>
    </header>

    <div class="card">
      <h2>⚡ OPNsense 终端一键安装</h2>
      <p style="color: var(--text-muted); margin: 0 0 0.5rem 0;">登录 OPNsense SSH 或系统控制台（Shell）执行以下命令，一键导入并安装全套组件：</p>
      <div class="code-container">
        <button class="copy-btn" onclick="copyInstallCmd()">一键复制命令</button>
        <pre><code id="installCmd"># 1. 抓取并写入仓库配置文件
fetch -o /usr/local/etc/pkg/repos/opnbox.conf ${BASE_URL}/opnbox.conf

# 2. 更新软件源索引
pkg update -r opnbox

# 3. 一键安装 NetBox 模块化插件与全套底层组件
pkg install -y os-netbox os-mosdns os-tun2socks os-xray \
               pf-aliasd mosdns mosdns-controller \
               hev-socks5-tunnel xray-core</code></pre>
      </div>
    </div>

    <div class="card">
      <h2>📦 包含组件与软件包矩阵</h2>
      <table>
        <thead>
          <tr>
            <th>组件包</th>
            <th>架构 / ABI</th>
            <th>版本策略</th>
            <th>功能职责与特性说明</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td><strong>os-netbox</strong></td>
            <td><code>FreeBSD:15:amd64</code></td>
            <td><code>YYYY.MM.DD</code> (自编)</td>
            <td>OPNsense <code>Services -> NetBox</code> 总菜单、全链路一键联动与全套规则自动同步</td>
          </tr>
          <tr>
            <td><strong>os-mosdns</strong></td>
            <td><code>FreeBSD:15:amd64</code></td>
            <td><code>YYYY.MM.DD</code> (自编)</td>
            <td>MosDNS OPNsense 插件模型、<code>actions_mosdns.conf</code> 动作与后台控制</td>
          </tr>
          <tr>
            <td><strong>os-tun2socks</strong></td>
            <td><code>FreeBSD:15:amd64</code></td>
            <td><code>YYYY.MM.DD</code> (自编)</td>
            <td>Tun2Socks OPNsense 插件模型、<code>actions_tun2socks.conf</code> 动作与后台控制</td>
          </tr>
          <tr>
            <td><strong>os-xray</strong></td>
            <td><code>FreeBSD:15:amd64</code></td>
            <td><code>YYYY.MM.DD</code> (自编)</td>
            <td>Xray OPNsense 插件模型、<code>actions_xray.conf</code> 动作与后台控制</td>
          </tr>
          <tr>
            <td><strong>pf-aliasd</strong></td>
            <td><code>FreeBSD:15:amd64</code></td>
            <td><code>YYYY.MM.DD</code> (自编)</td>
            <td>常驻 <code>/dev/pf</code> ioctl 同步守护进程，消除首包竞态，内置 Min-Heap TTL 垃圾回收</td>
          </tr>
          <tr>
            <td><strong>mosdns</strong></td>
            <td><code>FreeBSD:15:amd64</code></td>
            <td><code>v5.3.4</code> (源码构建)</td>
            <td>官方源码集成自研 <code>pf_alias</code> 内核同步插件，实现微秒级外部别名入表</td>
          </tr>
          <tr>
            <td><strong>mosdns-controller</strong></td>
            <td><code>FreeBSD:15:amd64</code></td>
            <td><code>YYYY.MM.DD</code> (自编)</td>
            <td>自研原生 Go 单二进制控制端，内嵌现代暗黑 SPA WebUI，支持 3 种分流模式与实时路由推演 (端口 <code>:5380</code>)</td>
          </tr>
          <tr>
            <td><strong>hev-socks5-tunnel</strong></td>
            <td><code>FreeBSD:15:amd64</code></td>
            <td><code>v2.17.1</code> (锁定稳定版)</td>
            <td>纯 C 协程极致性能 Tun2Socks 虚拟网卡代理，内嵌 <code>hev-controller</code> Web 面板 (端口 <code>:5382</code>)</td>
          </tr>
          <tr>
            <td><strong>xray-core</strong></td>
            <td><code>FreeBSD:15:amd64</code></td>
            <td><code>v26.7.11</code> (锁定稳定版)</td>
            <td>官方稳定版 Xray 代理网关，支持 VLESS、xhttp、SNI 嗅探，内置 DNS (<code>:10853</code>)，内嵌 <code>xray-controller</code> (端口 <code>:5384</code>)</td>
          </tr>
        </tbody>
      </table>
    </div>

    <div class="card">
      <h2>📚 官方技术文档中心</h2>
      <div class="docs-grid">
        <div class="doc-item">
          <h3><a href="https://github.com/${REPO_OWNER}/${REPO_NAME}/blob/main/docs/01-design-philosophy.md" target="_blank">01. 核心设计哲学</a></h3>
          <p>分层治理、确定性零首包竞态、极速内核交互与松耦合模块化哲学。</p>
        </div>
        <div class="doc-item">
          <h3><a href="https://github.com/${REPO_OWNER}/${REPO_NAME}/blob/main/docs/02-dns-and-pf-engine.md" target="_blank">02. DNS分流与PF引擎</a></h3>
          <p>首包阻断消除机制、Min-Heap TTL 堆调度、DNS 53/5353 接入与 SOCKS5 远端解析。</p>
        </div>
        <div class="doc-item">
          <h3><a href="https://github.com/${REPO_OWNER}/${REPO_NAME}/blob/main/docs/03-dataplane-tun-xray.md" target="_blank">03. 数据平面(TUN与Xray)</a></h3>
          <p>纯 C 协程 TUN 桥接、7层 SNI 嗅探、局域网自定义入站及 MosDNS 接入 Xray 10853。</p>
        </div>
        <div class="doc-item">
          <h3><a href="https://github.com/${REPO_OWNER}/${REPO_NAME}/blob/main/docs/04-control-plane-and-ui.md" target="_blank">04. 控制平面与OPNsense</a></h3>
          <p>微服务控制面板矩阵 (:5380/:5382/:5384)、OPNsense MVC 模块化菜单与 rc.d 规范。</p>
        </div>
        <div class="doc-item">
          <h3><a href="https://github.com/${REPO_OWNER}/${REPO_NAME}/blob/main/docs/05-rules-and-maintenance.md" target="_blank">05. 规则库与维护指南</a></h3>
          <p>双库原子同步工具 (update_rules.sh)、Cron 计划任务与生产排错指令箱。</p>
        </div>
        <div class="doc-item">
          <h3><a href="https://github.com/${REPO_OWNER}/${REPO_NAME}/blob/main/docs/06-evolution-and-modular-integration.md" target="_blank">06. 架构演进集成总结</a></h3>
          <p>从第三方控制端走向自研原生 Go 控制器的演进历程与 4 大解耦子插件全景。</p>
        </div>
      </div>
    </div>

    <div class="card">
      <h2>🔗 软件源配置详情 (opnbox.conf)</h2>
      <pre><code>opnbox: {
  url: "${BASE_URL}/\${ABI}",
  mirror_type: "http",
  signature_type: "none",
  priority: 10,
  enabled: yes
}</code></pre>
    </div>

    <div class="footer">
      <p>
        开源项目：<a href="https://github.com/${REPO_OWNER}/${REPO_NAME}" target="_blank">GitHub: ${REPO_OWNER}/${REPO_NAME}</a>
        &nbsp;|&nbsp; 运行平台：FreeBSD 15-CURRENT / OPNsense 26.x (amd64)
      </p>
    </div>
  </div>

  <script>
    function copyInstallCmd() {
      const text = document.getElementById('installCmd').innerText;
      navigator.clipboard.writeText(text).then(() => {
        const btn = document.querySelector('.copy-btn');
        btn.innerText = '已复制到剪贴板！';
        btn.style.background = '#10b981';
        btn.style.color = '#ffffff';
        setTimeout(() => {
          btn.innerText = '一键复制命令';
          btn.style.background = '#334155';
          btn.style.color = '#f8fafc';
        }, 2000);
      });
    }
  </script>
</body>
</html>
EOF

echo "==> index.html, CNAME (${CUSTOM_DOMAIN}) and .nojekyll generated successfully at ${PUBLIC_DIR}"
