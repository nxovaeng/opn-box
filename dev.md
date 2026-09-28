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

### 5. MosDNS 报错 `failed to init plugin #4 sync_to_pf, unable to decode plugin args: * '' has invalid keys: max_ttl, min_ttl, socket_path`
- **根因**：MosDNS 核心引擎在 `coremain/plugin.go` 中使用 `utils.WeakDecode()` 将 YAML 配置转换为插件结构体，其 mapstructure 解码器强制配置了 `TagName: "yaml"` 与 `ErrorUnused: true`。而 `pkg/plugin/pf_alias.go` 中的 `Args` 结构体字段仅声明了 `json:` tag，导致解码器无法识别带下划线的 `socket_path`、`min_ttl`、`max_ttl`，进而被视为非法未知参数并抛出 `FATAL`。
- **修复**：在 `pkg/plugin/pf_alias.go` 的 `Args` 字段中补全 `yaml:"..."` 标签，并重新为 FreeBSD amd64 编译了内嵌该插件的 `dist/bin/mosdns`。

### 6. Web 界面点击「全量更新规则库」提示异常被拦截，且终端日志被瞬时覆盖
- **根因**：
  1. `update_rules.sh` 原先默认 `MIRROR_ENABLED=0`，在未加 `--mirror` 参数时优先直连 GitHub。国内直连 GitHub 经常因阻断或重试导致耗时过长，进而超出 OPNsense 后台 configd 的默认通信超时，返回空或失败。
  2. `actions_netbox.conf` 与 `actions_mosdns.conf` 中的默认规则更新动作未显式指定 `--mirror`。
  3. `index.volt` 在动作执行完成后无条件执行 `setTimeout(refreshStatus, 800)`，`refreshStatus()` 强制用套件健康巡检输出覆写了 `#status-terminal`，将实际的规则下载进度日志秒级刷掉。
  4. 安装阶段出现 `Action not allowed or missing` 是由于 `package_repo.sh` 中 `rc.configure_plugins` 在 `service configd restart` 之前执行，此时 configd 尚未加载新插件的 actions 定义。
- **修复**：
  1. `update_rules.sh` 将 CDN 加速通道置为默认 (`MIRROR_ENABLED=1`)，支持 `--no-mirror`，调优连接超时。
  2. `actions_netbox.conf` 与 `actions_mosdns.conf` 中的所有规则更新指令均显式声明 `--mirror`。
  3. `index.volt` 重构 `refreshStatus(updateTerminal)`，在动作完成后传入 `false` 仅静默同步徽标状态，坚决保留控制台内原样输出；增加中文友好提示并将 Ajax 超时扩大至 300 秒。
### 7. MosDNS v5.3.4 sequence 解码崩溃 `* '[0].exec' expected type 'string', got unconvertible type '[]interface {}'`
- **根因**：历史示例配置与 `cmd/mosdns-controller/main.go` 沿用了 MosDNS v4 时代的语法（`args: exec: - if: ... exec: [...]`）。在 MosDNS v5.3.4 中，`sequence` 插件的数据结构被重构为扁平线性流水线（`type Args = []RuleArgs`，字段仅为 `matches: []string` 和 `exec: string`）。v5 中已彻底移除 `if: ...`、`_matches_domain`、`_return` 等 v4 插件，而是改用 `qname $direct_domain_set` 结合 `has_resp` 与 `return`。
- **修复**：全面对齐 MosDNS v5.3.4 规范：
  1. 将 `config.mosdns.example.yaml` 与 `cmd/mosdns-controller/main.go` 统一重构为标准 v5 sequence 语法；
  2. 将 `MosdnsPlugin.Args` 类型升级为 `interface{}` 以原生支持 slice 序列化；
  3. 增加全模式单测 `TestMosDNSv5AllModes` 100% 验证通过；
  4. 重新基于官方 `v5.3.4` 源码编译了内嵌 `pf_alias` 的全新 FreeBSD `dist/bin/mosdns`。

### 8. OPNsense 插件页面显示「配置错误」(misconfigured) 与 NetBox 下只有仪表盘菜单
- **根因**：
  1. OPNsense 插件列表中的「配置错误」（misconfigured）是由于该插件是通过命令行 `pkg install` 或自定义离线源安装的，系统在 `/conf/config.xml` 的备份/恢复跟踪列表中未找到对应记录。这属于 OPNsense 固件管理器的外观跟踪提示（Cosmetic tracking state），不影响插件的功能与系统服务的正常运行；
  2. `os-netbox` 原先的 `Menu.xml` 仅包含 `<Dashboard>` 节点，其余 3 个组件的菜单分散在各自子插件的 Menu.xml 中。若子插件未被缓存识别，左侧边栏则仅显示仪表盘。
- **修复**：
  1. 在 `src/os-netbox/src/opnsense/mvc/app/models/OPNsense/Netbox/Menu/Menu.xml` 与 `ACL.xml` 中直接汇总声明完整 4 项导航菜单（仪表盘、MosDNS 智能分流、Tun2Socks 虚拟网卡、Xray 代理核心）；
  2. 消除「配置错误」提示只需在 OPNsense 后台 **System > Firmware > Status** 中运行审计并点击 **“Reset all local conflicts”** 接受当前安装状态。

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
