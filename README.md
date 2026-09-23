# OPNsense MosDNS pf_alias 动态分流套件

基于 **MosDNS v5 插件 + Unix SOCK_SEQPACKET + 常驻 pf-aliasd + FreeBSD /dev/pf ioctl (libpfctl)** 的 OPNsense 极速策略路由动态表同步方案。

---

## 核心特性

1. **零首包漏流（Zero Race Condition）**：MosDNS 插件以微秒级（< 0.1ms）同步等待 `pf-aliasd` 注入内核 PF 表完成后才向客户端返回 DNS 响应，彻底消灭“客户端已收到 IP 发起 TCP SYN，而防火墙尚未入表走直连”的竞态漏流问题。
2. **基于 TTL 的自动定时清理（GC）**：
   - 守护进程内置 **最小堆（Min-Heap）到期调度器**，支持动态续期与精准毫秒级淘汰。
   - 周期性后台 GC 协程自动批量聚合到期 IP，单次 `DIOCRDELADDRS` 系统调用批量剔除，防止表无限膨胀触碰 OPNsense `table-entries` 上限。
3. **消息边界保障（SOCK_SEQPACKET）**：
   - 采用 `AF_UNIX` + `SOCK_SEQPACKET`，既拥有面向连接的反压与保序可靠性，又天然保留独立数据报文边界，零分包粘包解析开销。
4. **长驻 /dev/pf 描述符**：
   - 避免每次运行 Shell 调用 `/sbin/pfctl` 产生 Fork/Exec/Linker 的巨大 CPU 开销（性能提升 100~500 倍）。
5. **OPNsense 原生无缝集成**：
   - 目标直接对接 OPNsense 的 `External (advanced)` 别名（底层为 `table <name> persist`）。
   - OPNsense 在执行常规任务或 Filter Reload 时不会清空该表，与防火墙规则完全兼容。

---

## 目录结构

```text
opn-box/
├── cmd/
│   ├── pf-aliasd/         # Go 语言实现的守护进程（支持跨平台开发与 FreeBSD 原生编译）
│   │   └── main.go
│   └── c-daemon/          # 纯 C 语言实现的轻量级单文件守护进程（可选，供嵌入式环境直接用 cc 编译）
│       └── pf-aliasd.c
├── pkg/
│   ├── protocol/          # 二进制 SOCK_SEQPACKET 通信协议（ADD, DEL, FLUSH, ACK）
│   ├── ttlcleaner/        # 核心 TTL 到期调度器（基于优先队列 Min-Heap，支持批量弹出与续期）
│   ├── pf/                # /dev/pf ioctl 批量操作抽象（FreeBSD 原生系统调用 + Linux Mock）
│   └── plugin/            # MosDNS v5 官方兼容插件（实现 sequence.Executable，支持本地热点去重）
├── rc.d/
│   └── pf_aliasd          # FreeBSD / OPNsense rc.d 系统服务开机启动脚本
├── config.example.yaml    # MosDNS v5 配置集成示例
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
