# 控制平面与 OPNsense 深度集成

## 一、控制平面体系与职责矩阵

为了给管理员带来极致直观的运维体验，OPN-Box 划分了严谨的端口规划与微服务控制面板架构。各控制器均采用零依赖的独立 Go 原生单二进制程序，内嵌现代化响应式 SPA Web 前端：

```
                      OPNsense 统一 WebGUI 导航
                               │
            ┌──────────────────┼──────────────────┐
            ▼                  ▼                  ▼
     [ 端口 :5380 ]     [ 端口 :5382 ]     [ 端口 :5384 ]
    MosDNS Controller  Tun2Socks Manager   Xray Manager
```

### 控制器详细功能矩阵

| 控制器名称 | 守护服务与启动脚本 | 监听端口 | 核心管理职责 | 状态与配置持久化文件 |
| :--- | :--- | :---: | :--- | :--- |
| **MosDNS Controller** | `mosdns_controller` (`rc.d/mosdns_controller`) | `:5380` | • 原生 Go 独立架构，内嵌现代化暗黑 SPA WebUI (零 Node/npm/SQLite 依赖)<br>• 3 种分流模式：白名单直连、黑名单代理、混合自定义模式<br>• 3 组规则库管理：直连 (`custom-direct.txt`)、代理 (`custom-proxy.txt`)、广告拦截 (`custom-block.txt`)<br>• 实时域名路由推演模拟器 (`/api/test-domain`)<br>• 远端 DNS SOCKS5 代理通道配置 (不过 TUN)<br>• 实时日志流 (`mosdns.log`, `pf-aliasd.log`) | `/usr/local/etc/mosdns/controller.yaml` |
| **Tun2Socks Manager** | `hev-controller` (`rc.d/hev_controller`) | `:5382` | • `tun0` 网卡运行状态、PID 与实时日志<br>• MTU、多队列、IPv4/IPv6 参数可视化调优<br>• 本地 SOCKS5 桥接配置与一键重启 | `/usr/local/etc/hev-socks5-tunnel/config.yaml` |
| **Xray Manager** | `xray-controller` (`rc.d/xray_controller`) | `:5384` | • 节点多协议订阅拉取与批量导入 (全支持 xhttp)<br>• 多并发 TCP Ping 节点时延测速<br>• 可视化 7 层域名与 GeoSite 路由规则编辑<br>• 局域网自定义入站 (SOCKS5/HTTP) 动态管理<br>• 自动语法预检 (`xray run -test`) 与热重载 | • 状态层: `controller_data.json`<br>• 运行时: `config.json` |

---

## 二、OPNsense 原生 MVC 菜单整合

为了保证系统的极度解耦与自由组合能力，OPN-Box 将 UI 拆分为高度独立的模块化插件（`os-mosdns`, `os-tun2socks`, `os-xray` 及总控套件 `os-netbox`）。各插件通过 OPNsense MVC Menu 的自动树节点合并机制，统一汇聚至侧边栏 **`Services` -> `NetBox`** 二级菜单下：

```xml
<!-- 各模块解耦挂载示例 -->
<!-- 1. os-mosdns (src/os-mosdns/src/opnsense/mvc/app/models/OPNsense/Mosdns/Menu/Menu.xml) -->
<menu>
    <Services>
        <NetBox VisibleName="NetBox" cssClass="fa fa-cubes fa-fw">
            <MosDNS VisibleName="MosDNS" Order="10">
                <Dashboard VisibleName="MosDNS Controller" url="http://{SERVER_ADDR}:5380" target="_blank" Order="10"/>
                <General VisibleName="MosDNS Settings" url="/ui/mosdns/general" Order="20"/>
                <Log VisibleName="MosDNS Log" url="/ui/diagnostics/log/core/mosdns" Order="30"/>
            </MosDNS>
        </NetBox>
    </Services>
</menu>

<!-- 2. os-tun2socks (src/os-tun2socks/src/opnsense/mvc/app/models/OPNsense/Tun2socks/Menu/Menu.xml) -->
<menu>
    <Services>
        <NetBox VisibleName="NetBox" cssClass="fa fa-cubes fa-fw">
            <Tun2Socks VisibleName="Tun2Socks" Order="20">
                <Dashboard VisibleName="Tun2Socks Manager" url="http://{SERVER_ADDR}:5382" target="_blank" Order="10"/>
            </Tun2Socks>
        </NetBox>
    </Services>
</menu>

<!-- 3. os-xray (src/os-xray/src/opnsense/mvc/app/models/OPNsense/Xray/Menu/Menu.xml) -->
<menu>
    <Services>
        <NetBox VisibleName="NetBox" cssClass="fa fa-cubes fa-fw">
            <Xray VisibleName="Xray" Order="30">
                <Dashboard VisibleName="Xray Manager" url="http://{SERVER_ADDR}:5384" target="_blank" Order="10"/>
            </Xray>
        </NetBox>
    </Services>
</menu>
```

- **`{SERVER_ADDR}` 动态解析**：OPNsense 前端模板会自动将 `{SERVER_ADDR}` 替换为当前管理员正在访问的路由 LAN IP 或域名，点击直接新标签页打开对应微服务面板，体验高度无缝。
- **即插即用动态合并**：管理员仅安装需要的插件即可。例如只装 `os-mosdns` 时，`NetBox` 菜单下仅显示 MosDNS 相关组件；组合安装后自动拼装成完整的分流面板。

---

## 三、OPNsense Configd 模块化服务动作接入

OPNsense 采用 `configd` 守护进程解耦 Web 界面与底层系统命令。OPN-Box 将动作按子系统职责彻底模块化，分别独立注册在各自插件的 `actions_<name>.conf` 中：

### 1. `os-mosdns` 动作规范 (`actions_mosdns.conf`)
负责 MosDNS 核心、`pf-aliasd` 零首包入表守护与 MosDNS Controller WebUI：
```bash
configctl mosdns start              # 联合启动 pf-aliasd, mosdns, controller
configctl mosdns stop               # 联合停止
configctl mosdns restart            # 联合重启
configctl mosdns status             # 检查运行状态

# 细粒度单组件控制:
configctl mosdns dns.status         # 检查 MosDNS 转发引擎
configctl mosdns pf_aliasd.status   # 检查 pf-aliasd 守护进程
configctl mosdns controller.status  # 检查 Controller WebUI
```

### 2. `os-tun2socks` 动作规范 (`actions_tun2socks.conf`)
负责 `hev-socks5-tunnel` 虚拟网卡桥接与 Tun2Socks Web 面板：
```bash
configctl tun2socks start           # 联合启动 tunnel 与 controller
configctl tun2socks stop            # 联合停止
configctl tun2socks restart         # 联合重启
configctl tun2socks status          # 检查运行状态
configctl tun2socks tunnel.status   # 检查底层 tun0 协程服务
configctl tun2socks controller.status # 检查 Tun2Socks 管理面板
```

### 3. `os-xray` 动作规范 (`actions_xray.conf`)
负责 `xray-core` 7层应用特征路由与 Xray Manager 控制器：
```bash
configctl xray start                # 联合启动 xray 与 controller
configctl xray stop                 # 联合停止
configctl xray restart              # 联合重启
configctl xray status               # 检查运行状态
configctl xray core.status          # 检查 Xray 内核状态
configctl xray controller.status    # 检查 Xray 管理面板
```

### 4. `os-netbox` 套件总控动作规范 (`actions_netbox.conf`)
负责全套件统一联动与规则库集中同步：
```bash
configctl netbox start              # 一键顺序启动全链路微服务矩阵
configctl netbox stop               # 一键优雅关闭全部服务
configctl netbox restart            # 一键重启全套服务
configctl netbox status             # 一键巡检全链路健康状态
configctl netbox rules.update       # 触发全套规则库自动同步 (MosDNS + Xray)
configctl netbox rules.update_mirror # 通过加速镜像触发规则库同步
```

---

## 四、FreeBSD rc.d 守护脚本规范

所有服务启动脚本均遵循 FreeBSD `/etc/rc.subr` 工业级标准规范，由 FreeBSD 原生 `/usr/sbin/daemon` 负责监督守护：

- **PID 隔离管理**：由 `daemon -p /var/run/<service>.pid` 自动接管，保障异常崩溃时 PID 文件的正确释放与追踪；
- **日志集中归档**：标准输出与错误重定向至 `/var/log/<service>.log`，杜绝终端会话断开导致的挂起；
- **预检函数（prestart）**：启动前自动通过 `mkdir -p` 补齐必要的日志与配置目录，创建日志空文件，防止首启失败。

在 `/etc/rc.conf` 中开启服务示例：
```bash
sysrc pf_aliasd_enable="YES"
sysrc mosdns_enable="YES"
sysrc mosdns_controller_enable="YES"
sysrc hev_socks5_tunnel_enable="YES"
sysrc hev_controller_enable="YES"
sysrc xray_enable="YES"
sysrc xray_controller_enable="YES"
```
一键批量启动：
```bash
service pf_aliasd start
service mosdns start
service mosdns_controller start
service hev_socks5_tunnel start
service hev_controller start
service xray start
service xray_controller start
```

