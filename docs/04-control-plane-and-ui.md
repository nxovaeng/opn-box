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
| **MosDNS Controller** | `mosdns-controller` (`rc.d/mosdns-controller`) | `:5380` | • DNS 动态解析规则与黑白名单管理<br>• 上下游 DNS 解析组切换<br>• 客户端查询日志流排查 | `/usr/local/etc/mosdns/` |
| **Tun2Socks Manager** | `hev-controller` (`rc.d/hev_controller`) | `:5382` | • `tun0` 网卡运行状态、PID 与实时日志<br>• MTU、多队列、IPv4/IPv6 参数可视化调优<br>• 本地 SOCKS5 桥接配置与一键重启 | `/usr/local/etc/hev-socks5-tunnel/config.yaml` |
| **Xray Manager** | `xray-controller` (`rc.d/xray_controller`) | `:5384` | • 节点多协议订阅拉取与批量导入 (全支持 xhttp)<br>• 多并发 TCP Ping 节点时延测速<br>• 可视化 7 层域名与 GeoSite 路由规则编辑<br>• 局域网自定义入站 (SOCKS5/HTTP) 动态管理<br>• 自动语法预检 (`xray run -test`) 与热重载 | • 状态层: `controller_data.json`<br>• 运行时: `config.json` |

---

## 二、OPNsense 原生 MVC 菜单整合

通过 OPNsense 原生插件 `os-mosdns`（源码位于 [src/os-mosdns/src/](file:///home/yaofen/opn-box/src/os-mosdns/src/)），所有组件被统合至 OPNsense 侧边栏的 **`Services` -> `OPN-Box`** 菜单组下：

```xml
<!-- src/os-mosdns/src/opnsense/mvc/app/models/OPNsense/Mosdns/Menu/Menu.xml -->
<menu>
    <Services>
        <OPNBox VisibleName="OPN-Box" cssClass="fa fa-cubes fa-fw">
            <General VisibleName="MosDNS Settings" url="/ui/mosdns/general" Order="10"/>
            <Dashboard VisibleName="MosDNS Controller" url="http://{SERVER_ADDR}:5380" target="_blank" Order="20"/>
            <Tun2Socks VisibleName="Tun2Socks Manager" url="http://{SERVER_ADDR}:5382" target="_blank" Order="30"/>
            <Xray VisibleName="Xray Manager" url="http://{SERVER_ADDR}:5384" target="_blank" Order="35"/>
            <Log VisibleName="MosDNS Log" url="/ui/diagnostics/log/core/mosdns" Order="40"/>
        </OPNBox>
    </Services>
</menu>
```

- **`{SERVER_ADDR}` 动态解析**：OPNsense 前端模板会自动将 `{SERVER_ADDR}` 替换为当前管理员正在访问的路由 LAN IP 或域名，点击直接新标签页打开对应微服务面板，体验高度无缝。

---

## 三、OPNsense Configd 服务系统接入

OPNsense 采用 `configd` 守护进程解耦 Web 界面与底层系统命令。OPN-Box 在 [actions_mosdns.conf](file:///home/yaofen/opn-box/src/os-mosdns/src/opnsense/service/conf/actions.d/actions_mosdns.conf) 中完整注册了各项标准动作：

```ini
# MosDNS 与 pf-aliasd 核心管理
[start]
command:/usr/local/etc/rc.d/mosdns start && /usr/local/etc/rc.d/pf_aliasd start
parameters:
type:script
message:starting MosDNS and pf-aliasd services

[stop]
command:/usr/local/etc/rc.d/mosdns stop && /usr/local/etc/rc.d/pf_aliasd stop
parameters:
type:script
message:stopping MosDNS and pf-aliasd services

# Tun2Socks (hev-socks5-tunnel + hev-controller) 管理
[tun2socks.start]
command:/usr/local/etc/rc.d/hev_socks5_tunnel start && /usr/local/etc/rc.d/hev_controller start
parameters:
type:script
message:starting hev-socks5-tunnel and controller

# Xray (xray-core + xray-controller) 管理
[xray.start]
command:/usr/local/etc/rc.d/xray start && /usr/local/etc/rc.d/xray_controller start
parameters:
type:script
message:starting xray-core and xray-controller

# 规则库一键同步与更新 (支持定时任务挂载)
[rules.update]
command:/usr/local/sbin/update-opnbox-rules.sh
parameters:
type:script_output
message:updating MosDNS and Xray rule databases

[rules.update_mirror]
command:/usr/local/sbin/update-opnbox-rules.sh --mirror
parameters:
type:script_output
message:updating MosDNS and Xray rule databases via acceleration mirror
```

### 控制台直接调用（CLI）
管理员登录 SSH 或控制台后，可随时通过原生 `configctl` 发起调试：
```bash
configctl mosdns status             # 检查 MosDNS 与 pf-aliasd 运行状态
configctl mosdns tun2socks.status   # 检查 Tun2Socks 隧道状态
configctl mosdns xray.restart       # 重启 Xray 与管理面板
configctl mosdns rules.update       # 触发全套规则库自动同步
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
sysrc hev_socks5_tunnel_enable="YES"
sysrc hev_controller_enable="YES"
sysrc xray_enable="YES"
sysrc xray_controller_enable="YES"
```
一键批量启动：
```bash
service pf_aliasd start
service mosdns start
service hev_socks5_tunnel start
service hev_controller start
service xray start
service xray_controller start
```
