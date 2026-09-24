# 规则库生态、更新机制与维护指南

## 一、双库分工与目录规范

在 OPN-Box 体系中，规则库根据承载引擎的不同分为两类，分别由不同的目录管理：

```
/usr/local/
├── etc/
│   └── mosdns/
│       └── rule/                         # MosDNS 纯文本 Set 规则目录
│           ├── cn.txt                    # 社区权威国内直连名单 (上游 direct-list，自动覆盖)
│           ├── gfw.txt                   # 社区权威出海防污染名单 (上游 gfw.txt，自动覆盖)
│           ├── custom-direct.txt         # 用户自定义直连白名单 (永久保留，绝不覆盖)
│           └── custom-proxy.txt          # 用户自定义代理黑名单 (永久保留，绝不覆盖)
└── share/
    └── xray/                             # Xray 7层二进制 Protobuf 特征库目录
        ├── geosite.dat                   # 7层站点特征库 (google, openai, netflix, cn 等)
        └── geoip.dat                     # 全球 IP 地理网段库
```

### 用户自定义规则格式说明
在 `custom-direct.txt` 和 `custom-proxy.txt` 中，每行填写一个域名匹配规则：
- `domain:example.com`：匹配 `example.com` 及其所有子域名（如 `sub.example.com`）；
- `full:api.example.com`：严格精确匹配完整主机名；
- `keyword:google`：包含关键词匹配；
- 纯域名 `example.org`：默认等同于 `domain:example.org`。

---

## 二、自动同步脚本：update-opnbox-rules.sh

源码位于 [scripts/update_rules.sh](file:///home/yaofen/opn-box/scripts/update_rules.sh)，在系统打包后自动部署至 `/usr/local/sbin/update-opnbox-rules.sh`。

### 脚本设计保障：
1. **原子性替换（Atomic Swap）**：所有文件下载至 `.tmp` 临时文件，严格执行大小与非空校验后，再通过原子 `mv -f` 覆盖，彻底避免网络中途中断导致空规则或文件破损。
2. **免外挂依赖的双下载引擎**：优先采用 `curl`；在精简安装的 FreeBSD 系统上自动回退至系统内置的 `/usr/bin/fetch`。
3. **主源与国内加速镜像容灾**：
   - 主源：`https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download`
   - 加速镜像源：`https://raw.gitmirror.com/Loyalsoldier/v2ray-rules-dat/release`
   - 主源网络波动时自动回退至镜像源重试。
4. **服务自愈热重载**：同步完成后，自动检测系统中处于运行状态的 `mosdns` 与 `xray` 进程，并依次发起平滑重启，让新规则立刻生效。

### 命令行使用示例：
```bash
# 1. 标准同步 (连接 GitHub 官方 Release)
update-opnbox-rules.sh

# 2. 加速同步 (在 GitHub 访问不畅时推荐)
update-opnbox-rules.sh --mirror

# 3. 仅下载文件，不自动重启服务
update-opnbox-rules.sh --no-restart

# 4. 指定自定义存放路径
update-opnbox-rules.sh --rule-dir /custom/path/rules --xray-dir /custom/path/xray
```

---

## 三、OPNsense 自动化计划任务（Cron）设置

强烈建议在 OPNsense 后台挂载每日自动同步任务，让分流规则库始终保持最新状态。

### 图形界面配置步骤：
1. 登录 OPNsense Web 管理界面；
2. 导航至 **System -> Settings -> Cron**；
3. 点击右下角 **`+`** 添加新的计划任务：
   - **Minutes**：`0`
   - **Hours**：`4` （建议选在凌晨业务低峰期，如 04:00）
   - **Days**：`*`
   - **Months**：`*`
   - **Days of week**：`*`
   - **Command**：从下拉列表中选择 **`MosDNS: update MosDNS and Xray rule databases`**（即 `configctl mosdns rules.update`）；
   - **Description**：`OPN-Box Daily Rule Sync`
4. 保存并点击 **Apply changes**。

---

## 四、日常排错与诊断工具箱

当分流遇到异常或特定网站行为不符合预期时，可依次通过以下命令排查：

### 1. 检查 PF 内核动态表状态
```bash
# 查看 GFW_Proxy 表中当前所有由 MosDNS 动态注入的活动 IP 列表
pfctl -t GFW_Proxy -T show

# 查看该表的匹配包计数与内存占用
pfctl -v -t GFW_Proxy -T show

# 测试特定 IP 是否已在表中 (存在返回 1，不存在返回 0)
pfctl -t GFW_Proxy -T test 142.250.190.46
```

### 2. 检查 pf-aliasd IPC 套接字与到期 GC 状态
```bash
# 确认 UNIX 域套接字正常存活
ls -la /var/run/pf-aliasd.sock

# 实时查看垃圾回收与入表日志
tail -f /var/log/pf-aliasd.log
```

### 3. 检查 Tun2Socks 虚拟网卡状态
```bash
# 查看 tun0 网卡是否存在并已正确配置 IP
ifconfig tun0

# 查看 hev-socks5-tunnel 实时日志
tail -f /var/log/hev-controller.log
```

### 4. 检查 Xray 节点路由与 7 层嗅探状态
```bash
# 检查 Xray 进程与内存占用
pgrep -fl xray

# 查看 Xray 实时分流访问日志 (观察出站 tag 是否匹配到预期节点)
tail -f /var/log/xray.log
```
