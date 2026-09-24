#!/bin/sh
set -e

# ==============================================================================
# generate_index.sh - 生成 OPNsense 软件源仓库的 Web 导航落地页 (index.html)
# ==============================================================================

PUBLIC_DIR="${1:-public}"
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

INDEX_FILE="${PUBLIC_DIR}/index.html"

cat << EOF > "${INDEX_FILE}"
<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>OPNsense Package Repository - opn-box</title>
  <style>
    :root {
      --bg-main: #0f172a;
      --card-bg: #1e293b;
      --card-border: #334155;
      --text-main: #f8fafc;
      --text-muted: #94a3b8;
      --primary: #38bdf8;
      --code-bg: #0b1120;
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
    .container { max-width: 880px; margin: 0 auto; }
    header {
      margin-bottom: 2rem;
      border-bottom: 1px solid var(--card-border);
      padding-bottom: 1.5rem;
    }
    h1 {
      margin: 0 0 0.5rem 0;
      font-size: 2rem;
      color: var(--primary);
    }
    .badges { display: flex; gap: 0.5rem; flex-wrap: wrap; margin-top: 0.8rem; }
    .badge {
      padding: 0.25rem 0.65rem;
      font-size: 0.75rem;
      font-weight: 600;
      border-radius: 9999px;
      background: rgba(56, 189, 248, 0.15);
      color: var(--primary);
      border: 1px solid rgba(56, 189, 248, 0.3);
    }
    .card {
      background: var(--card-bg);
      border: 1px solid var(--card-border);
      border-radius: 10px;
      padding: 1.5rem;
      margin-bottom: 1.5rem;
    }
    .card h2 {
      margin-top: 0;
      font-size: 1.25rem;
      color: var(--text-main);
    }
    pre {
      background: var(--code-bg);
      padding: 1rem;
      border-radius: 8px;
      overflow-x: auto;
      color: #e2e8f0;
      border: 1px solid var(--card-border);
      font-size: 0.9rem;
    }
    code { font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; }
    table { width: 100%; border-collapse: collapse; margin-top: 0.5rem; }
    th, td {
      padding: 0.75rem 1rem;
      text-align: left;
      border-bottom: 1px solid var(--card-border);
    }
    th { color: var(--text-muted); font-size: 0.85rem; text-transform: uppercase; }
    a { color: var(--primary); text-decoration: none; }
    a:hover { text-decoration: underline; }
    .footer {
      text-align: center;
      margin-top: 3rem;
      color: var(--text-muted);
      font-size: 0.875rem;
    }
  </style>
</head>
<body>
  <div class="container">
    <header>
      <h1>📦 opn-box 专属软件源</h1>
      <p style="color: var(--text-muted); margin: 0.5rem 0 0 0;">
        适用于 <strong>OPNsense 25.x / 26.x (FreeBSD 14 & FreeBSD 15 amd64)</strong> 的高性能策略路由与 DNS 分流套件
      </p>
      <div class="badges">
        <span class="badge">FreeBSD 14 & 15 通用</span>
        <span class="badge">100% 源码自编译</span>
        <span class="badge">零首包竞态</span>
        <span class="badge">Min-Heap TTL GC</span>
        <span class="badge">hev-socks5-tunnel 极速 TUN</span>
      </div>
    </header>

    <div class="card">
      <h2>⚡ OPNsense 终端一键安装</h2>
      <p>登录 OPNsense SSH 或在控制台执行以下命令导入并启用本仓库：</p>
      <pre><code># 1. 抓取并写入仓库配置文件
fetch -o /usr/local/etc/pkg/repos/opnbox.conf ${BASE_URL}/opnbox.conf

# 2. 更新软件源索引
pkg update -r opnbox

# 3. 安装 os-mosdns 插件及核心套件 (含 sing-box, hev-socks5-tunnel 与 xray-core)
pkg install -y os-mosdns sing-box hev-socks5-tunnel xray-core</code></pre>
    </div>

    <div class="card">
      <h2>📦 包含组件与二进制包</h2>
      <table>
        <thead>
          <tr>
            <th>组件包</th>
            <th>架构 / ABI</th>
            <th>版本策略 / 锁定版本</th>
            <th>功能说明</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td><strong>os-mosdns</strong></td>
            <td><code>all</code></td>
            <td><code>YYYY.MM.DD</code> (自编构建日期)</td>
            <td>OPNsense WebGUI 插件模型、服务动作、菜单与后台控制</td>
          </tr>
          <tr>
            <td><strong>pf-aliasd</strong></td>
            <td><code>amd64</code></td>
            <td><code>YYYY.MM.DD</code> (自编构建日期)</td>
            <td>常驻 /dev/pf ioctl 同步守护进程，内置 Min-Heap TTL 垃圾回收</td>
          </tr>
          <tr>
            <td><strong>mosdns</strong></td>
            <td><code>amd64</code></td>
            <td><code>v5.3.4</code> (官方源码+插件)</td>
            <td>官方原版 v5.3.4，内置自研 pf_alias 插件，零首包竞态保障</td>
          </tr>
          <tr>
            <td><strong>mosdns-controller</strong></td>
            <td><code>amd64</code></td>
            <td><code>YYYY.MM.DD</code> (自编构建日期)</td>
            <td>WebUI 规则管理面板与动态控制器 (端口 :5380)</td>
          </tr>
          <tr>
            <td><strong>sing-box</strong></td>
            <td><code>amd64</code></td>
            <td><code>v1.13.14</code> (官方源码+补丁)</td>
            <td>集成 FreeBSD TUN 本地适配补丁的全功能代理内核</td>
          </tr>
          <tr>
            <td><strong>hev-socks5-tunnel</strong></td>
            <td><code>amd64</code></td>
            <td><code>v2.17.1</code> (官方锁定稳定版)</td>
            <td>纯 C 协程极致性能 Tun2Socks 虚拟网卡代理，内嵌 hev-controller Web 管理面板 (端口 :5382)</td>
          </tr>
          <tr>
            <td><strong>xray-core</strong></td>
            <td><code>amd64</code></td>
            <td><code>v26.7.11</code> (官方锁定稳定版)</td>
            <td>官方稳定版 Xray 代理内核，原生支持 VLESS、xhttp (SplitHTTP) 与 SNI 嗅探，内嵌 xray-controller Web 管理面板 (端口 :5384) 支持订阅导入、可视化 7 层路由分流与局域网自定义入站</td>
          </tr>
        </tbody>
      </table>
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
        源码仓库与使用指南：<a href="https://github.com/${REPO_OWNER}/${REPO_NAME}" target="_blank">GitHub: ${REPO_OWNER}/${REPO_NAME}</a>
      </p>
    </div>
  </div>
</body>
</html>
EOF

echo "==> index.html, CNAME (${CUSTOM_DOMAIN}) and .nojekyll generated successfully at ${PUBLIC_DIR}"
