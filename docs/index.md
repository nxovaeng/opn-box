# OPN-Box 专属软件源与技术文档中心

欢迎查阅 **OPN-Box** 策略路由与 DNS 动态分流套件官方技术文档与软件源门户。

OPN-Box 是专为 **OPNsense 26.x (FreeBSD:15:amd64)** 打造的高性能全链路策略路由平台，基于 **“DNS 粗粒度白名单直连 + 零首包竞态内核同步 + 极速纯 C 协程 TUN 桥接 + Xray 7层应用特征细分流”** 的分层协同架构。

---

## ⚡ OPNsense 终端一键安装

登录 OPNsense SSH 或在系统控制台（Shell）执行以下命令，即可一键导入软件源并安装全套组件：

```bash
# 1. 抓取并写入仓库配置文件
fetch -o /usr/local/etc/pkg/repos/opnbox.conf https://opnbox.zro.qzz.io/opnbox.conf

# 2. 更新软件源索引
pkg update -r opnbox

# 3. 安装 NetBox 模块化插件与全套底层核心组件
pkg install -y os-netbox os-mosdns os-tun2socks os-xray \
               pf-aliasd mosdns mosdns-controller \
               hev-socks5-tunnel xray-core
```

---

## 📦 包含组件与软件包矩阵

本软件源所发布的全部软件包均针对 **`FreeBSD:15:amd64`** 架构严格构建，无任何第三方预编译黑盒：

| 软件包名称 | 架构 / ABI | 版本策略 | 核心功能与职责说明 |
| :--- | :--- | :--- | :--- |
| **`os-netbox`** | `FreeBSD:15:amd64` | `YYYY.MM.DD` (自编) | OPNsense `Services -> NetBox` 顶级菜单总控、全套规则自动同步工具 (`update-opnbox-rules.sh`) |
| **`os-mosdns`** | `FreeBSD:15:amd64` | `YYYY.MM.DD` (自编) | MosDNS OPNsense 插件模型、服务动作 (`actions_mosdns.conf`) 与菜单挂载 |
| **`os-tun2socks`** | `FreeBSD:15:amd64` | `YYYY.MM.DD` (自编) | Tun2Socks OPNsense 插件模型、服务动作 (`actions_tun2socks.conf`) 与菜单挂载 |
| **`os-xray`** | `FreeBSD:15:amd64` | `YYYY.MM.DD` (自编) | Xray OPNsense 插件模型、服务动作 (`actions_xray.conf`) 与菜单挂载 |
| **`pf-aliasd`** | `FreeBSD:15:amd64` | `YYYY.MM.DD` (自编) | 常驻 `/dev/pf` ioctl 毫秒级同步守护进程，内置 Min-Heap TTL 垃圾回收调度器 |
| **`mosdns`** | `FreeBSD:15:amd64` | `v5.3.4` (自编) | 官方 v5.3.4 源码构建，内置自研 `pf_alias` 零竞态同步插件 |
| **`mosdns-controller`** | `FreeBSD:15:amd64` | `YYYY.MM.DD` (自编) | 自研原生 Go 单二进制控制端，内嵌现代暗黑 SPA WebUI，支持 3 种分流模式与实时路由推演 (端口 `:5380`) |
| **`hev-socks5-tunnel`** | `FreeBSD:15:amd64` | `v2.17.1` (锁定稳定版) | 纯 C 协程极致吞吐 TUN 虚拟网卡代理，内嵌 `hev-controller` Web 管理面板 (端口 `:5382`) |
| **`xray-core`** | `FreeBSD:15:amd64` | `v26.7.11` (锁定稳定版) | 官方稳定版 Xray 内核，支持 VLESS、xhttp (SplitHTTP)、SNI 嗅探，内置本地 DNS (`:10853`)，内嵌 `xray-controller` (端口 `:5384`) |

---

## 🔗 软件源配置详情 (`opnbox.conf`)

仓库由 GitHub Actions 全自动构建并托管在 GitHub Pages / 自定义 CDN 域名上：

```ini
opnbox: {
  url: "https://opnbox.zro.qzz.io/${ABI}",
  mirror_type: "http",
  signature_type: "none",
  priority: 10,
  enabled: yes
}
```

---

## 📑 官方技术文档快速导航

- [**01. 核心设计哲学与架构演进**](01-design-philosophy.md)：分层治理、确定性零竞态、极速内核交互与松耦合模块化哲学。
- [**02. DNS 动态分流与零竞态内核同步引擎**](02-dns-and-pf-engine.md)：首包竞态消除机制、二进制 `SOCK_SEQPACKET` 协议、Min-Heap TTL 调度器、DNS 53/5353 监听模式及 SOCKS5 远端解析原理。
- [**03. 数据平面：极速 TUN 桥接与 Xray 7层细分流**](03-dataplane-tun-xray.md)：纯 C 协程 `hev-socks5-tunnel`、Xray 7 层 SNI 嗅探、局域网自定义入站与 MosDNS 接入 Xray 内置 DNS (`:10853`) 闭环。
- [**04. 控制平面与 OPNsense 深度集成**](04-control-plane-and-ui.md)：微服务控制面板矩阵（`:5380`, `:5382`, `:5384`）、OPNsense MVC 模块化菜单与 FreeBSD `rc.d` 规范。
- [**05. 规则库生态、更新机制与维护指南**](05-rules-and-maintenance.md)：双库协同更新工具 (`update-opnbox-rules.sh`)、Cron 自动化调度与故障排查工具箱。
- [**06. 架构演进与全链路模块化集成总结**](06-evolution-and-modular-integration.md)：自研原生 Go 控制器演进历程与 4 大解耦子插件全景。
