# OPN-Box 开发与集成手册 (Developer Manual)

本文档记录 OPN-Box 套件在 OPNsense 26.x (FreeBSD 15:amd64) 下的开发运维、系统服务联动规范与关键修复记录。

---

## 一、服务架构与控制脚本

为确保在 OPNsense WebGUI、configd 守护进程以及系统 SSH 终端中均能统一、可靠地管理服务，套件提供了 4 个标准化的 POSIX `/bin/sh` 控制脚本，统一安装于 `/usr/local/sbin/`：

| 脚本路径 | 作用范围 | 支持动作 | 关联底层服务 (rc.d) |
|---|---|---|---|
| `/usr/local/sbin/opnbox-control` | NetBox 全套件统一联动调度 | `start`, `stop`, `restart`, `status` | `pf_aliasd`, `mosdns`, `mosdns_controller`, `hev_socks5_tunnel`, `hev_controller`, `xray`, `xray_controller` |
| `/usr/local/sbin/mosdns-control` | MosDNS 动态 DNS 与 PF 表注入模块 | `start`, `stop`, `restart`, `status` | `pf_aliasd`, `mosdns`, `mosdns_controller` |
| `/usr/local/sbin/tun2socks-control`| Tun2Socks 虚拟网卡隧道模块 | `start`, `stop`, `restart`, `status` | `hev_socks5_tunnel`, `hev_controller` |
| `/usr/local/sbin/xray-control` | Xray 代理内核与节点管理器 | `start`, `stop`, `restart`, `status` | `xray`, `xray_controller` |

### 关键规范：FreeBSD 服务 `one*` 命令机制
FreeBSD 下若服务在 `/etc/rc.conf` 中未设置 `_enable="YES"`，直接调用 `service <name> start` 会被系统拒绝。因此所有控制脚本及控制器内部调用一律采用 `onestart`、`onestop`、`onerestart`、`onestatus`。

---

## 二、Xray-core 官方包规范与多文件目录集成

OPNsense 官方源内置了 `security/xray-core`（如 `xray-core-26.7.28_1`）。为确保与官方内核完全兼容，本套件遵守以下架构规范：

1. **废弃自定义 `rc.d/xray`**：直接复用官方提供的 `/usr/local/etc/rc.d/xray`。
2. **多文件目录架构 (`-confdir`)**：
   官方启动命令使用 `/usr/local/bin/xray -confdir /usr/local/etc/xray-core`。
   `xray-controller` (:5384) 在点击"应用配置"时，按官方规范直接写入拆分的模块化配置文件：
   - `/usr/local/etc/xray-core/00_log.json`（日志配置）
   - `/usr/local/etc/xray-core/02_dns.json`（MosDNS 出海 DNS 接入 dokodemo-door :10853）
   - `/usr/local/etc/xray-core/03_routing.json`（AI / 流媒体 / 直连路由分流规则）
   - `/usr/local/etc/xray-core/05_inbounds.json`（tun2socks 专用 :10808 入站与局域网专用代理入站）
   - `/usr/local/etc/xray-core/06_outbounds.json`（订阅代理节点、活动主节点 `proxy-default`、直连自由出站）
3. **资产规则目录**：
   官方内核环境变量指定 `XRAY_LOCATION_ASSET=/usr/local/share/xray-core`。`update-opnbox-rules.sh` 会将 `geosite.dat` 与 `geoip.dat` 同步至该目录，并软链接/复制至 `/usr/local/share/xray` 以实现双向兼容。

---

## 三、重大问题根因排查与修复记录

### 1. 安装时日志显示 `Action not allowed or missing`
- **根因**：插件的 `post-install` 脚本曾调用 `/usr/local/opnsense/service/configd_ctl.py reload actions`，但该命令在当前 OPNsense 版本中失效，导致 configd 进程从未加载 `actions_netbox.conf` 等新动作定义。
- **修复**：在所有插件的 `post-install` 与 `pre-deinstall` 脚本中替换为 `service configd restart >/dev/null 2>&1 || true`。安装完成后必须重启 configd。

### 2. 5382 端口 hev-socks5-tunnel 转发配置无法编辑、很快被还原
- **根因**：`cmd/hev-controller/main.go` 中设置了 `setInterval(fetchStatus, 5000)`，该定时器每 5 秒无条件执行 `document.getElementById('cfgSocksAddr').value = ...`，在用户输入打字时被强行还原为旧值。此外，保存后后台调用 `service ... restart` 缺少 `one` 前缀导致重启失败。
- **修复**：
  1. 增加 `window.formInitialized` 标志位，表单只在初始加载和保存成功后同步，轮询刷新绝不覆盖正在编辑的输入框；
  2. 增加"↺ 重新载入当前配置"按钮；
  3. 服务调用统一升级为 `onerestart`。

### 3. WebGUI 表单修改很快被还原 (MVC Model 问题)
- **根因**：各插件的 Model XML 声明了 `<items><general><enabled>` 字段，但没有配套的 API 模型控制器（`ApiMutableModelControllerBase`）将其持久化到 `/conf/config.xml`。每次页面刷新都从 XML 默认值重绘，表现为"被还原"。
- **修复**：由于各组件的细粒度配置已完全托管在各自独立的 WebUI (:5380/:5382/:5384) 中，OPNsense MVC Model 精简为仅保留 `<mount>` 节点，移除无用字段，彻底杜绝表单还原现象。

### 4. 规则库更新失败与 MosDNS 初始化 Fatal 崩溃
- **根因**：
  1. 镜像源 `raw.gitmirror.com` 无法解析 DNS；
  2. 脚本使用 `set -e`，网络抖动导致直接静默退出；
  3. 若 `cn.txt` 或 `gfw.txt` 缺失，MosDNS 的 `domain_set` 插件初始化失败并触发 `FATAL` 导致进程终止。
- **修复**：
  1. 移除 `set -e`，改用多 CDN（jsDelivr CDN、ghproxy、GitHub 原源）轮询降级机制；
  2. 无论网络下载是否成功，自动为 `cn.txt`、`gfw.txt`、`custom-direct.txt`、`custom-proxy.txt` 生成合法的非空占位记录，保证 MosDNS 100% 正常启动。

---

## 四、常用终端调试与验证指令

```sh
# 1. 检查 configd 动作是否加载正常
configctl netbox status
configctl mosdns status
configctl tun2socks status
configctl xray status

# 2. 命令行手动启停全套件
opnbox-control start all
opnbox-control status all
opnbox-control stop all

# 3. 手动执行全量规则库更新
update-opnbox-rules.sh

# 4. 手动验证 Xray 9 模块目录配置语法
xray run -test -confdir /usr/local/etc/xray-core

# 5. 查看各独立 Controller 监听端口状态
sockstat -4 -l | grep -E '5380|5382|5384'
```
