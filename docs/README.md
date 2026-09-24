# OPN-Box 官方技术文档中心

欢迎查阅 **OPN-Box** 策略路由与 DNS 动态分流套件的核心技术文档。

OPN-Box 是专为 **OPNsense 26.x (FreeBSD:15:amd64)** 打造的高性能全链路策略路由平台，基于 “DNS 粗粒度白名单直连 + 零竞态内核同步 + 极速纯 C 协程 TUN 桥接 + Xray 7层应用特征细分流” 的混合分层架构。

---

## 📑 文档章节目录

### 1. [核心设计哲学与架构演进](file:///home/wym/devcode/opn-box/docs/01-design-philosophy.md)
- 项目背景与为什么拒绝 Linux 移植方案
- 四大核心哲学：分层治理、确定性零竞态、极速内核交互、松耦合模块化
- 双轨制版本管理策略（CalVer 自编译 + 锁定上游稳定版）
- 历史架构演进路线图

### 2. [DNS 动态分流与零竞态内核同步引擎](file:///home/wym/devcode/opn-box/docs/02-dns-and-pf-engine.md)
- 首包竞态（First-Packet Race Condition）的本质与微秒级阻断消除原理
- 二进制 `SOCK_SEQPACKET` 进程间通信协议详解
- `/dev/pf` ioctl 长连接与 Min-Heap TTL 垃圾回收调度器
- OPNsense `External (advanced)` 别名与底层 `table persist` 机制
- DNS 监听接入方案（直接监听 53 端口 vs 5353 NAT 重定向及全网防漏流）
- 远程 DNS SOCKS5 代理通道（为什么 SOCKS5 查询绝对不过 TUN）

### 3. [数据平面：极速 TUN 桥接与 Xray 7层细分流](file:///home/wym/devcode/opn-box/docs/03-dataplane-tun-xray.md)
- 为什么将 TUN 虚拟网卡与代理出海核心解耦？
- 底座网卡代理：纯 C 协程 `hev-socks5-tunnel` 极致性能与参数调优
- 应用层网关：`xray-core` 官方稳定版内核整合与内置本地 DNS 服务 (`:10853`)
- 7 层 SNI 域名嗅探（Sniffing）与多出口（AI 专线 / 流媒体 / 主力节点）调度
- 局域网自定义入站（Custom Inbounds: SOCKS5 / HTTP）设计与免 TUN 旁路接入
- 单二进制控制器 `xray-controller` 与配置沙盒预检自愈机制

### 4. [控制平面与 OPNsense 深度集成](file:///home/wym/devcode/opn-box/docs/04-control-plane-and-ui.md)
- 微服务控制面板与端口规划矩阵（`:5380`, `:5382`, `:5384`）
- OPNsense 原生 MVC 模块化菜单整合（`Services -> NetBox`，解耦子插件）
- OPNsense `configd` 动作注册与 `configctl` 控制台交互
- FreeBSD 工业级 `rc.d` 守护进程与服务管理规范

### 5. [规则库生态、更新机制与维护指南](file:///home/wym/devcode/opn-box/docs/05-rules-and-maintenance.md)
- 双库协同目录规范：MosDNS 纯文本 Set 与 Xray Protobuf `geosite.dat` 特征库
- 自动同步更新工具：`update-opnbox-rules.sh`（原子替换、双下载引擎、镜像容灾）
- OPNsense 后台定时计划任务（Cron）全自动调度配置
- 生产环境诊断与排错指令工具箱（PF 表、套接字、日志）

### 6. [架构演进与全链路模块化集成总结](file:///home/wym/devcode/opn-box/docs/06-evolution-and-modular-integration.md)
- 从第三方控制端到自研原生 Go 控制器的重大演进历程
- 数据面、控制面与内核 PF 的全链路协同架构图
- OPNsense 四大插件模块化拆分（`os-netbox`, `os-mosdns`, `os-tun2socks`, `os-xray`）
- 配置文件与 FreeBSD rc.d 服务管理全景矩阵

---

## 快速导航速查表

| 组件服务 | 监听端口 | 核心技术栈 | 配置与数据路径 | 文档索引 |
| :--- | :---: | :--- | :--- | :--- |
| **pf-aliasd** | `/var/run/pf-aliasd.sock` | Go / C / `/dev/pf` ioctl / Min-Heap | `/var/log/pf-aliasd.log` | [02-dns-and-pf-engine.md](file:///home/wym/devcode/opn-box/docs/02-dns-and-pf-engine.md) |
| **MosDNS** | `:53` 或 `:5353` | Go / sequence / pf_alias / domain_set | `/usr/local/etc/mosdns/config.yaml` | [02-dns-and-pf-engine.md](file:///home/wym/devcode/opn-box/docs/02-dns-and-pf-engine.md) |
| **MosDNS Controller** | `:5380` | Go 静态单二进制 / 内嵌现代暗黑 SPA / 白名单、黑名单、混合分流 / 零外部依赖 | `/usr/local/etc/mosdns/controller.yaml` | [04-control-plane-and-ui.md](file:///home/wym/devcode/opn-box/docs/04-control-plane-and-ui.md) |
| **hev-socks5-tunnel** | `tun_box` | 纯 C / C-Coroutines / SOCKS5 | `/usr/local/etc/hev-socks5-tunnel/config.yaml` | [03-dataplane-tun-xray.md](file:///home/wym/devcode/opn-box/docs/03-dataplane-tun-xray.md) |
| **hev-controller** | `:5382` | Go / 内嵌 SPA / YAML 操作 | `/var/log/hev-controller.log` | [04-control-plane-and-ui.md](file:///home/wym/devcode/opn-box/docs/04-control-plane-and-ui.md) |
| **xray-core** | `:10808` (SOCKS5)<br/>`:10853` (内置 DNS) | Go / VLESS / xhttp / REALITY / Sniffing | `/usr/local/etc/xray/config.json` | [03-dataplane-tun-xray.md](file:///home/wym/devcode/opn-box/docs/03-dataplane-tun-xray.md) |
| **xray-controller** | `:5384` | Go / 内嵌 SPA / 订阅解析 / 路由合成 | `/usr/local/etc/xray/controller_data.json` | [04-control-plane-and-ui.md](file:///home/wym/devcode/opn-box/docs/04-control-plane-and-ui.md) |
| **os-netbox** | OPNsense WebGUI | OPNsense MVC / PHP / XML Menu | `Services -> NetBox` | [04-control-plane-and-ui.md](file:///home/wym/devcode/opn-box/docs/04-control-plane-and-ui.md) |
