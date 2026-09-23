# FreeBSD sing-box & sing-tun 本地补丁集

本目录包含针对 FreeBSD 平台编译 `sing-box` (v1.13.x+) 与 `sing-tun` 所需的所有补丁与调试诊断工具。
所有补丁均保存在本地仓库中，自编译流水线完全脱离外部不可控的临时分支。

## 文件清单

| 文件 | 说明 |
| :--- | :--- |
| `sing-box.patch` | sing-box FreeBSD 核心补丁：增加 `searcher_freebsd.go`（基于 FreeBSD `KERN_PROC_FILEDESC` 与 `xinpcb` 实现进程名反查，匹配 PROCESS-NAME 路由规则）、支持 `FIBIndex` 多路由表路由分流等。 |
| `sing-tun.patch` | sing-tun FreeBSD 驱动适配补丁：增加 FreeBSD gVisor 端点驱动 `tun_freebsd_gvisor.go` 与系统路由事件监听器 `monitor_freebsd.go`。 |
| `get_offset.c` | FreeBSD 内核数据结构（`struct xinpcb`, `struct pfioc_natlook`, `struct pfsync_state` 等）字段偏移校验工具，用于适配 FreeBSD 13/14/15 内核版本差异。 |
| `natlookup.c` | 原生 `/dev/pf` 的 `DIOCNATLOOK` 端口还原与透明代理重定向验证工具。 |

## 应用方法

在编译流水线（如 `scripts/build_binaries.sh`）中：
```bash
# 1. 对 sing-box 应用补丁
cd sing-box
patch -p1 < patches/sing-box/sing-box.patch

# 2. 对 sing-tun 应用补丁并生成平台监听器
cd sing-tun
patch -p1 < patches/sing-box/sing-tun.patch
cp monitor_darwin.go monitor_freebsd.go
cp monitor_darwin.go monitor_openbsd.go
```
