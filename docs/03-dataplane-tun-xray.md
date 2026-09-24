# 数据平面：极速 TUN 桥接与 Xray 7层细分流

## 一、为什么需要数据平面解耦？

在传统的透明代理方案中，通常由单个软件同时承担 **TUN 虚拟网卡接管** 与 **复杂的出海代理协议传输**（例如直接使用 sing-box TUN 模式）。

但在生产级软路由场景下，这种“全包揽”方案在 FreeBSD / OPNsense 上会遇到显著挑战：
1. **协议滞后**：很多自建节点依赖 CDN 承载的现代化协议（如 `xhttp` SplitHTTP、`mode=packet-up`、REALITY），而通用 TUN 方案往往更新缓慢或不支持。
2. **多核性能与系统稳定性**：Go 语言实现的 TUN 堆栈在面对千兆高并发下吞吐和 GC 压力较大；且一旦代理内核因配置或节点异常崩溃，整个网关的 TUN 虚拟接口和路由表也会连带崩溃。

**OPN-Box 的解耦数据平面**采用 **双引擎协作架构**：
- **底座网卡桥接**：纯 C 语言协程实现的 **`hev-socks5-tunnel`**（极致吞吐、死锁免疫、只专注于把 `tun` 流量转换为标准本地 SOCKS5）。
- **上层出海网关**：原生官方稳定版 **`xray-core`**（业界最强现代协议兼容库、7 层域名嗅探与多节点动态调度）。

```
           [ 客户端出海 IP 数据包 ]
                      │
                      ▼ (PF 策略路由 route-to)
          ┌───────────────────────────┐
          │  FreeBSD tun0 虚拟网卡     │ (IPv4: 198.18.0.1/15)
          └─────────────┬─────────────┘
                        │
                        ▼ (微线程协程捕获)
          ┌───────────────────────────┐
          │    hev-socks5-tunnel      │ (纯 C 实现，零 GC 损耗)
          └─────────────┬─────────────┘
                        │ 本地高效回环 SOCKS5 转发
                        ▼ (127.0.0.1:10808)
          ┌─────────────────────────────────────────────────────────────┐
          │                      xray-core                              │
          │                                                             │
          │   1. 内置入站 (127.0.0.1:10808, 自动开启 Sniffing 嗅探)     │
          │   2. 局域网自定义入站 (0.0.0.0:10809 / 10810, 供特定设备免TUN)│
          │                                                             │
          │   3. 7 层路由判定 (基于原生 geosite.dat):                    │
          │      ├── 命中 geosite:openai  ──> [ 美国专线节点 ]          │
          │      ├── 命中 geosite:netflix ──> [ 新加坡流媒体节点 ]       │
          │      └── 默认所有海外流量     ──> [ 主力高速节点 ]          │
          └─────────────────────────────────────────────────────────────┘
```

---

## 二、底层网卡代理：hev-socks5-tunnel

`hev-socks5-tunnel` 是目前业界性能最顶尖的轻量级 TUN2SOCKS 桥接工具。

### 核心特性优势
1. **纯 C 语言微线程架构 (hev-task)**：
   采用类似 Go 协程的上下文切换机制，但完全基于纯 C 编写，内存占用通常低于 **5MB**，单核心轻松跑满千兆带宽。
2. **FreeBSD 原生系统适配**：
   天然支持 FreeBSD 的 `/dev/tun` 设备及特定 ioctl，无需通过 Linux 兼容层运行。
3. **极简配置规范**（`/usr/local/etc/hev-socks5-tunnel/config.yaml`）：

```yaml
tunnel:
  name: tun0
  mtu: 8500              # 支持巨帧，降低 CPU 中断频率
  ipv4: 198.18.0.1
  multi-queue: true

socks5:
  address: 127.0.0.1     # 指向本地 Xray 核心入站
  port: 10808
  udp: 'tcp'             # 支持 UDP-over-TCP 穿透

misc:
  task-stack-size: 81920
  connect-timeout: 5000
  read-write-timeout: 60000
  log-level: warn
```

---

## 三、应用层网关：Xray-core 与 7层精细分流

### 1. 为什么上层选用官方预编译 Xray-core？
- **零编译风险**：官方每个稳定版均提供高度优化的 `Xray-freebsd-64.zip`，经过全球海量用户生产验证；
- **全协议制霸**：原生完整支持 **`xhttp` (SplitHTTP / packet-up)**、**VLESS**、**VMess**、**Trojan**、**Shadowsocks**、**REALITY**、**gRPC** 与 **WebSocket**；
- **免二次自编维护**：避免因 Go 编译器版本迭代、上游隐式依赖变动而导致软路由构建断流。

---

### 2. 7 层域名嗅探（Sniffing）的魔法
当数据包穿过 `tun0` 到达 Xray 时，目标地址原本只是一个 IP（例如 `104.18.x.x`）。

Xray 开启了入站嗅探：
```json
"sniffing": {
  "enabled": true,
  "destOverride": ["http", "tls", "quic"],
  "metadataOnly": false
}
```
当 TLS 客户端发起 `ClientHello` 握手时，Xray 实时抓取其 **SNI 扩展字段**，将 IP 还原为真正的域名（如 `chatgpt.com` 或 `api.openai.com`）。

随后，Xray 的路由模块即可无缝应用 `geosite.dat` 特征库进行二次调度：
- **`geosite:openai` / `geosite:anthropic`** -> 调度至针对 AI 优化的节点（防封号/家宽）；
- **`geosite:netflix` / `geosite:disney`** -> 调度至具备解锁能力的节点；
- **`geosite:cn`** -> 即使有极少部分国内 IP 意外滑入 TUN，Xray 也能在 7 层直接判为 `direct` 直连出局，形成双重防呆保护；
- 其余未命中规则的境外域名 -> 统一走 `proxy-default` 主力节点。

---

### 3. 局域网自定义入站（Custom Inbounds）
在真实家庭或企业内网中，经常存在**免 TUN 透明代理**的特定设备需求：
- 如特定的开发机希望通过设置 `http_proxy=http://192.168.1.1:10810` 抓包排查；
- 部分智能电视或旧款设备对 TUN 策略路由兼容性不佳，更适合直接在 WiFi 设置中配置 SOCKS5 代理。

在 `xray-controller` 面板中，管理员可任意创建并启用独立的自定义入站：
- **协议**：SOCKS5 或 HTTP；
- **监听地址**：`0.0.0.0` 或指定局域网网卡 IP（如 `192.168.1.1`）；
- **独立端口**：如 `10809`、`10810`；
- **嗅探支持**：同样享受 7 层 SNI 嗅探与分流路由体系。

---

## 四、单二进制管理核心：xray-controller

为了在 FreeBSD 上提供媲美 Linux Web 面板的操作体验，OPN-Box 自研了纯 Go 编写的单二进制管理控制器 `xray-controller`（运行于端口 `:5384`）。

### 核心运作机制：
1. **多协议订阅解析**：
   - 抓取 Base64 订阅源，自动解析各类复杂 URL 编码参数；
   - 完美解析 `xhttp` 传输参数（模式、下载路径、Host 等）；
   - 支持批量手动导入。
2. **多并发 TCP Ping 测速**：
   - 使用异步协程对所有节点进行 TCP 往返时延（RTT）探测，并在界面用绿色/黄色/超时徽章直观标出。
3. **合成与沙盒自愈校验（Dry-Run）**：
   - 任何改动不会直接写入生产配置；
   - 后端先在 `/tmp/xray_test.json` 生成测试配置，并调用 `xray run -test -c /tmp/xray_test.json` 进行语法与证书有效性检验；
   - 检验 100% 通过后，才原子替换 `/usr/local/etc/xray/config.json` 并平滑重载服务。

