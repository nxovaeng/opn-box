# OPN-Box: OPNsense 高性能策略路由与动态分流套件

基于 **FreeBSD 原生内核 /dev/pf ioctl + MosDNS 零竞态同步 + hev-socks5-tunnel 极速 TUN + Xray-core 7层应用特征细分流** 的 OPNsense 现代化策略路由平台。

---

## 📚 官方技术文档中心

系统完整设计哲学、底层协议规范与生产运维指南已整理至 `docs/` 目录：

- 📖 **[01. 核心设计哲学与架构演进](docs/01-design-philosophy.md)**：分层治理（粗分流+细分流）、零首包竞态、双轨制版本策略与演进历程
- 📖 **[02. DNS 动态分流与零竞态内核同步引擎](docs/02-dns-and-pf-engine.md)**：`SOCK_SEQPACKET` 二进制协议、Min-Heap TTL 到期调度、`/dev/pf` ioctl 机制
- 📖 **[03. 数据平面：极速 TUN 桥接与 Xray 7层细分流](docs/03-dataplane-tun-xray.md)**：纯 C 协程 `hev-socks5-tunnel`、`xray-core` (xhttp/REALITY)、7层 SNI 嗅探与局域网自定义入站
- 📖 **[04. 控制平面与 OPNsense 深度集成](docs/04-control-plane-and-ui.md)**：微服务面板矩阵 (:5380, :5382, :5384)、OPNsense MVC 菜单、`configd` 动作与 rc.d 守护进程
- 📖 **[05. 规则库生态、更新机制与维护指南](docs/05-rules-and-maintenance.md)**：双库规范、`update-opnbox-rules.sh` 自动同步、Cron 定时任务与排错工具箱

---

## 核心特性

1. **零首包漏流（Zero Race Condition）**：MosDNS 插件以微秒级（< 0.1ms）同步等待 `pf-aliasd` 注入内核 PF 表完成后才向客户端返回 DNS 响应，彻底消灭“客户端已收到 IP 发起 TCP SYN，而防火墙尚未入表走直连”的竞态漏流问题。
2. **基于 TTL 的自动定时清理（GC）**：
   - 守护进程内置 **最小堆（Min-Heap）到期调度器**，支持动态续期与精准毫秒级淘汰。
   - 周期性后台 GC 协程自动批量聚合到期 IP，单次 `DIOCRDELADDRS` 系统调用批量剔除，防止表无限膨胀触碰 OPNsense `table-entries` 上限。
3. **分层分流治理架构**：
   - **前端 MosDNS 粗分流**：国内白名单直连解析，未明确与出海走防污染 DoH 并推入 PF 代理表；
   - **后端 Xray 7层细分流**：借助 SNI 嗅探与 `geosite.dat`，实现 AI 专线、海外流媒体与主力节点的精准按需出口路由。
4. **极速数据平面与现代协议支持**：
   - 底层采用纯 C 协程驱动的 `hev-socks5-tunnel`，千兆吞吐 CPU 占用极低；
   - 官方稳定版 `xray-core` 原生支持 CDN 承载的 `xhttp` (SplitHTTP) 与 REALITY。
5. **OPNsense 原生无缝集成与全 WebUI 控制**：
   - 插件直接集成进 OPNsense 菜单组 `Services -> OPN-Box`；
   - 独立微服务面板：`:5380` (MosDNS), `:5382` (Tun2Socks), `:5384` (Xray Manager)。

---

## 目录结构

```text
opn-box/
├── cmd/
│   ├── pf-aliasd/         # 零竞态内核 PF 表常驻守护进程 (Go)
│   ├── hev-controller/    # Tun2Socks 虚拟网卡 Web 管理面板 (:5382)
│   ├── xray-controller/   # Xray 订阅导入/7层路由/自定义入站 Web 面板 (:5384)
│   └── c-daemon/          # 纯 C 语言实现的轻量守护进程 (可选)
├── docs/                  # 官方技术文档中心
│   ├── 01-design-philosophy.md
│   ├── 02-dns-and-pf-engine.md
│   ├── 03-dataplane-tun-xray.md
│   ├── 04-control-plane-and-ui.md
│   ├── 05-rules-and-maintenance.md
│   └── README.md
├── pkg/
│   ├── protocol/          # 二进制 SOCK_SEQPACKET 通信协议 (ADD, DEL, FLUSH, ACK)
│   ├── ttlcleaner/        # 核心 TTL 到期调度器 (Min-Heap 优先队列)
│   ├── pf/                # /dev/pf ioctl 批量操作抽象 (FreeBSD 原生系统调用)
│   └── plugin/            # MosDNS v5 官方兼容 pf_alias 同步插件
├── rc.d/                  # FreeBSD 原生开机自启守护脚本 (pf_aliasd, xray, hev 等)
├── scripts/               # 编译流水线、软件源打包与规则库自动更新脚本
├── src/os-mosdns/         # OPNsense 原生 WebGUI 插件模型、菜单与服务配置
├── config.example.yaml    # MosDNS 粗粒度智能分流配置模板
└── README.md
```

---

## 编译指南

### 1. 编译 `pf-aliasd` 守护进程

可以在 Linux / macOS 上直接交叉编译生成 FreeBSD 静态二进制文件：

```bash
# 交叉编译为 64 位 FreeBSD 二进制（适用于 OPNsense amd64）
GOOS=freebsd GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o pf-aliasd-freebsd ./cmd/pf-aliasd

# 或在 OPNsense 本地直接用 C 编译器编译纯 C 版本：
cc -O2 -Wall -o pf-aliasd cmd/c-daemon/pf-aliasd.c -lpfctl
```

### 2. 编译集成插件的 MosDNS

将 `pkg/plugin/pf_alias.go` 源码引入 MosDNS 编译工程中，在 MosDNS 的 `main.go` 中隐式导入该包即可注册：

```go
import (
    _ "opn-box/pkg/plugin" // 自动触发 init() 注册 pf_alias 插件
)
```

---

## OPNsense 部署步骤

### 1. 配置 OPNsense External Alias

1. 打开 OPNsense Web 管理界面：**Firewall -> Aliases**。
2. 点击右上角 **`+`** 新增别名：
   - **Enabled**：勾选
   - **Name**：`GFW_Proxy`（需与 MosDNS 配置中的 `table` 名称一致）
   - **Type**：选择 **`External (advanced)`**
   - **Description**：`MosDNS Dynamic Policy Routing Table`
3. 保存并点击 **Apply**。
   > **说明**：OPNsense 会在 `/tmp/rules.debug` 中生成 `table <GFW_Proxy> persist`，允许外部工具管理其 IP，且在 Filter Reload 时不会被清空。

### 2. 部署 `pf-aliasd` 服务

1. 将编译好的 `pf-aliasd-freebsd` 复制到 OPNsense 的 `/usr/local/sbin/pf-aliasd`：
   ```bash
   chmod +x /usr/local/sbin/pf-aliasd
   ```
2. 将 `rc.d/pf_aliasd` 复制到 `/usr/local/etc/rc.d/pf_aliasd`：
   ```bash
   chmod +x /usr/local/etc/rc.d/pf_aliasd
   ```
3. 在 `/etc/rc.conf` 中启用服务：
   ```bash
   sysrc pf_aliasd_enable="YES"
   service pf_aliasd start
   ```
4. 验证服务运行状态：
   ```bash
   ls -la /var/run/pf-aliasd.sock
   tail -n 20 /var/log/pf-aliasd.log
   ```

### 3. 配置 OPNsense 策略路由规则

1. 进入 **Firewall -> Rules -> LAN**。
2. 添加一条通行规则：
   - **Interface**：LAN
   - **Direction**：in
   - **TCP/IP Version**：IPv4+IPv6
   - **Protocol**：Any
   - **Destination**：选择刚刚创建的别名 `GFW_Proxy`
   - **Gateway**：选择你的出境分流网关（如 WireGuard、OpenVPN、V2Ray/Xray 虚拟网关等）
3. 保存并应用规则。

### 4. 实时查看与调试

在 OPNsense 的 **Firewall -> Diagnostics -> Aliases** 页面中，选择 `GFW_Proxy` 表，即可实时看到由 MosDNS 解析后自动注入、且在到期后自动消失的动态 IP 列表。也可以在控制台执行：

```bash
pfctl -t GFW_Proxy -T show
```
