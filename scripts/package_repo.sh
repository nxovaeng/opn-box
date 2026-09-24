#!/bin/sh
set -e

# ==============================================================================
# package_repo.sh - FreeBSD 原生打包与 OPNsense 软件源生成自动化脚本
#
# 特性：
# 1. 严格使用标准 POSIX /bin/sh 语法，可在 FreeBSD 原生环境及 VM 中无缝执行
# 2. 自动化将编译产物装载到 stage 目录并构建 FreeBSD 格式 .pkg 安装包
# 3. 补齐强制元数据（www、arch 通配符），支持 FreeBSD 14 / 15 双 ABI 镜像分发
# 4. 支持参数化配置：自定义输出目录、自定义域名、发布目录等
# ==============================================================================

WORKSPACE_DIR="$(cd "$(dirname "$0")/.." && pwd)"

# 默认参数
DIST_DIR="${WORKSPACE_DIR}/dist"
OUTPUT_DIR="${WORKSPACE_DIR}/repo_output"
PUBLISH_DIR="${WORKSPACE_DIR}/public"
REPO_OWNER="${REPO_OWNER:-opn-box}"
REPO_NAME="${REPO_NAME:-opn-box}"
CUSTOM_DOMAIN="${CUSTOM_DOMAIN:-opnbox.zro.qzz.io}"

# 命令行参数解析
while [ $# -gt 0 ]; do
  case "$1" in
    --dist-dir)
      DIST_DIR="$2"; shift 2 ;;
    --output-dir)
      OUTPUT_DIR="$2"; shift 2 ;;
    --publish-dir)
      PUBLISH_DIR="$2"; shift 2 ;;
    --repo-owner)
      REPO_OWNER="$2"; shift 2 ;;
    --repo-name)
      REPO_NAME="$2"; shift 2 ;;
    --domain)
      CUSTOM_DOMAIN="$2"; shift 2 ;;
    -h|--help)
      echo "用法: $0 [选项]"
      echo "选项:"
      echo "  --dist-dir <dir>       自编译产物所在目录 (默认: dist)"
      echo "  --output-dir <dir>     软件源生成输出目录 (默认: repo_output)"
      echo "  --publish-dir <dir>    最终对外发布目录 (默认: public)"
      echo "  --repo-owner <owner>   GitHub 仓库拥有者"
      echo "  --repo-name <name>     GitHub 仓库名称"
      echo "  --domain <domain>      自定义域名 (例如: opnbox.zro.qzz.io)"
      exit 0
      ;;
    *)
      echo "未知参数: $1" >&2
      exit 1
      ;;
  esac
done

# 计算对外访问 URL 与项目主页
if [ -n "${CUSTOM_DOMAIN}" ]; then
  CLIENT_REPO_URL="https://${CUSTOM_DOMAIN}"
else
  CLIENT_REPO_URL="https://${REPO_OWNER}.github.io/${REPO_NAME}"
fi
PROJECT_WEB_URL="https://github.com/${REPO_OWNER}/${REPO_NAME}"

# 检测当前系统 ABI
if command -v pkg >/dev/null 2>&1; then
  ABI=$(pkg config abi 2>/dev/null || echo "FreeBSD:14:amd64")
else
  ABI="FreeBSD:14:amd64"
fi

echo "=========================================================="
echo " 开始生成 OPNsense 软件源仓库 (FreeBSD pkg repo)"
echo " 工作目录:   ${WORKSPACE_DIR}"
echo " 编译产物:   ${DIST_DIR}"
echo " 目标源目录: ${OUTPUT_DIR}"
echo " 发布目录:   ${PUBLISH_DIR}"
echo " 当前系统ABI: ${ABI}"
echo " 仓库终端URL: ${CLIENT_REPO_URL}"
echo "=========================================================="

# 1. 准备 Staging 目录结构
STAGE_DIR="/tmp/stage"
STAGE_UI_DIR="/tmp/stage-ui"
rm -rf "${STAGE_DIR}" "${STAGE_UI_DIR}"
mkdir -p "${STAGE_DIR}/usr/local/sbin" \
         "${STAGE_DIR}/usr/local/bin" \
         "${STAGE_DIR}/usr/local/etc/rc.d" \
         "${STAGE_DIR}/usr/local/etc/mosdns" \
         "${STAGE_UI_DIR}/usr/local"

mkdir -p "${OUTPUT_DIR}/${ABI}" "${OUTPUT_DIR}/All"

# 2. 复制二进制与运行配置到 staging 目录
if [ -f "${DIST_DIR}/bin/pf-aliasd" ]; then
  cp "${DIST_DIR}/bin/pf-aliasd" "${STAGE_DIR}/usr/local/sbin/"
fi
if [ -f "${WORKSPACE_DIR}/rc.d/pf_aliasd" ]; then
  cp "${WORKSPACE_DIR}/rc.d/pf_aliasd" "${STAGE_DIR}/usr/local/etc/rc.d/"
elif [ -f "${DIST_DIR}/rc.d/pf_aliasd" ]; then
  cp "${DIST_DIR}/rc.d/pf_aliasd" "${STAGE_DIR}/usr/local/etc/rc.d/"
fi

[ -f "${DIST_DIR}/bin/mosdns" ] && cp "${DIST_DIR}/bin/mosdns" "${STAGE_DIR}/usr/local/bin/"
[ -f "${DIST_DIR}/bin/mosdns-x" ] && cp "${DIST_DIR}/bin/mosdns-x" "${STAGE_DIR}/usr/local/bin/"
[ -f "${DIST_DIR}/bin/mosdns-controller" ] && cp "${DIST_DIR}/bin/mosdns-controller" "${STAGE_DIR}/usr/local/bin/"
[ -f "${DIST_DIR}/bin/sing-box" ] && cp "${DIST_DIR}/bin/sing-box" "${STAGE_DIR}/usr/local/bin/"

if [ -f "${WORKSPACE_DIR}/config.example.yaml" ]; then
  cp "${WORKSPACE_DIR}/config.example.yaml" "${STAGE_DIR}/usr/local/etc/mosdns/config.yaml.sample"
elif [ -f "${DIST_DIR}/etc/mosdns.yaml.example" ]; then
  cp "${DIST_DIR}/etc/mosdns.yaml.example" "${STAGE_DIR}/usr/local/etc/mosdns/config.yaml.sample"
fi

chmod +x "${STAGE_DIR}/usr/local/sbin/"* "${STAGE_DIR}/usr/local/bin/"* "${STAGE_DIR}/usr/local/etc/rc.d/"* 2>/dev/null || true

# 3. 逐个生成 FreeBSD 格式软件包 (.pkg)
# ------------------------------------------------------------------------------
# 3.1 Package: pf-aliasd
# ------------------------------------------------------------------------------
if [ -f "${STAGE_DIR}/usr/local/sbin/pf-aliasd" ]; then
  echo "==> 打包 pf-aliasd..."
  cat << EOF > /tmp/manifest_pf_aliasd
name: pf-aliasd
version: "1.0.0"
origin: net/pf-aliasd
comment: "Packet Filter Alias Daemon for OPNsense External Aliases"
desc: "Zero-race PF table sync daemon with min-heap TTL garbage collection"
maintainer: "admin@opn-box.local"
www: "${PROJECT_WEB_URL}"
prefix: /usr/local
categories: [net]
arch: "FreeBSD:*:amd64"
EOF
  cat << EOF > /tmp/plist_pf_aliasd
sbin/pf-aliasd
etc/rc.d/pf_aliasd
EOF
  pkg create -M /tmp/manifest_pf_aliasd -p /tmp/plist_pf_aliasd -r "${STAGE_DIR}" -o "${OUTPUT_DIR}/All"
fi

# ------------------------------------------------------------------------------
# 3.2 Package: mosdns (官方原版 v5.3.4，嵌入 pf_alias 插件)
# ------------------------------------------------------------------------------
if [ -f "${STAGE_DIR}/usr/local/bin/mosdns" ]; then
  echo "==> 打包 mosdns (v5.3.4 + pf_alias)..."
  cat << EOF > /tmp/manifest_mosdns
name: mosdns
version: "5.3.4"
origin: dns/mosdns
comment: "Official MosDNS v5.3.4 with pf_alias plugin"
desc: "Official MosDNS v5.3.4 compiled from source with pf_alias for OPNsense"
maintainer: "admin@opn-box.local"
www: "${PROJECT_WEB_URL}"
prefix: /usr/local
categories: [dns]
arch: "FreeBSD:*:amd64"
deps: {
  pf-aliasd: { version: "1.0.0", origin: "net/pf-aliasd" }
}
EOF
  cat << EOF > /tmp/plist_mosdns
bin/mosdns
etc/mosdns/config.yaml.sample
EOF
  pkg create -M /tmp/manifest_mosdns -p /tmp/plist_mosdns -r "${STAGE_DIR}" -o "${OUTPUT_DIR}/All"
fi

# ------------------------------------------------------------------------------
# 3.3 Package: mosdns-x (演进版，支持 DoQ/DoH3)
# ------------------------------------------------------------------------------
if [ -f "${STAGE_DIR}/usr/local/bin/mosdns-x" ]; then
  echo "==> 打包 mosdns-x..."
  cat << EOF > /tmp/manifest_mosdns_x
name: mosdns-x
version: "26.1.0"
origin: dns/mosdns-x
comment: "High-performance modular DNS forwarder with DoQ, DoH3 and pf_alias"
desc: "MosDNS-X engine compiled from source with pf_alias plugin for OPNsense"
maintainer: "admin@opn-box.local"
www: "${PROJECT_WEB_URL}"
prefix: /usr/local
categories: [dns]
arch: "FreeBSD:*:amd64"
deps: {
  pf-aliasd: { version: "1.0.0", origin: "net/pf-aliasd" }
}
EOF
  cat << EOF > /tmp/plist_mosdns_x
bin/mosdns-x
etc/mosdns/config.yaml.sample
EOF
  pkg create -M /tmp/manifest_mosdns_x -p /tmp/plist_mosdns_x -r "${STAGE_DIR}" -o "${OUTPUT_DIR}/All"
fi

# ------------------------------------------------------------------------------
# 3.4 Package: mosdns-controller (Web UI 面板与控制器)
# ------------------------------------------------------------------------------
if [ -f "${STAGE_DIR}/usr/local/bin/mosdns-controller" ]; then
  echo "==> 打包 mosdns-controller..."
  cat << EOF > /tmp/manifest_controller
name: mosdns-controller
version: "1.0.0"
origin: dns/mosdns-controller
comment: "Web UI and Dynamic Rule Management Controller for MosDNS"
desc: "mosdns-controller compiled from luoye663/mosdns-controller source for OPNsense"
maintainer: "admin@opn-box.local"
www: "${PROJECT_WEB_URL}"
prefix: /usr/local
categories: [dns]
arch: "FreeBSD:*:amd64"
EOF
  cat << EOF > /tmp/plist_controller
bin/mosdns-controller
EOF
  pkg create -M /tmp/manifest_controller -p /tmp/plist_controller -r "${STAGE_DIR}" -o "${OUTPUT_DIR}/All"
fi

# ------------------------------------------------------------------------------
# 3.5 Package: sing-box (全功能代理内核，应用 FreeBSD TUN 补丁)
# ------------------------------------------------------------------------------
if [ -f "${STAGE_DIR}/usr/local/bin/sing-box" ]; then
  echo "==> 打包 sing-box..."
  cat << EOF > /tmp/manifest_singbox
name: sing-box
version: "1.13.14"
origin: net/sing-box
comment: "Universal proxy platform with FreeBSD TUN patches"
desc: "sing-box compiled with local FreeBSD patches for FreeBSD/OPNsense"
maintainer: "admin@opn-box.local"
www: "${PROJECT_WEB_URL}"
prefix: /usr/local
categories: [net]
arch: "FreeBSD:*:amd64"
EOF
  cat << EOF > /tmp/plist_singbox
bin/sing-box
EOF
  pkg create -M /tmp/manifest_singbox -p /tmp/plist_singbox -r "${STAGE_DIR}" -o "${OUTPUT_DIR}/All"
fi

# ------------------------------------------------------------------------------
# 3.6 Package: os-mosdns (OPNsense WebGUI 插件)
# ------------------------------------------------------------------------------
if [ -d "${WORKSPACE_DIR}/src/os-mosdns/src" ]; then
  echo "==> 打包 os-mosdns (OPNsense UI 插件)..."
  cp -r "${WORKSPACE_DIR}/src/os-mosdns/src/"* "${STAGE_UI_DIR}/usr/local/"
  cat << EOF > /tmp/manifest_os_mosdns
name: os-mosdns
version: "1.0.0"
origin: opnsense/os-mosdns
comment: "MosDNS Dynamic Routing Engine with pf-aliasd & Controller UI"
desc: "OPNsense WebGUI plugin for MosDNS with pf-aliasd"
maintainer: "admin@opn-box.local"
www: "${PROJECT_WEB_URL}"
prefix: /usr/local
categories: [opnsense]
arch: "*"
deps: {
  pf-aliasd: { version: "1.0.0", origin: "net/pf-aliasd" },
  mosdns: { version: "5.3.4", origin: "dns/mosdns" }
}
EOF
  cat << EOF > /tmp/plist_os_mosdns
opnsense/service/conf/actions.d/actions_mosdns.conf
opnsense/mvc/app/models/OPNsense/Mosdns/Menu/Menu.xml
EOF
  pkg create -M /tmp/manifest_os_mosdns -p /tmp/plist_os_mosdns -r "${STAGE_UI_DIR}" -o "${OUTPUT_DIR}/All"
fi

# ------------------------------------------------------------------------------
# 4. 生成软件源元数据索引 (pkg repo) 与多 ABI 镜像兼容
# ------------------------------------------------------------------------------
echo "==> 生成 FreeBSD pkg 索引目录 (pkg repo)..."
pkg repo "${OUTPUT_DIR}/All"

echo "==> 同步生成双 ABI 镜像路径 (FreeBSD:14:amd64 & FreeBSD:15:amd64)..."
mkdir -p "${OUTPUT_DIR}/FreeBSD:14:amd64" "${OUTPUT_DIR}/FreeBSD:15:amd64"
cp "${OUTPUT_DIR}/All"/* "${OUTPUT_DIR}/${ABI}/"
cp "${OUTPUT_DIR}/All"/* "${OUTPUT_DIR}/FreeBSD:14:amd64/"
cp "${OUTPUT_DIR}/All"/* "${OUTPUT_DIR}/FreeBSD:15:amd64/"
cp "${OUTPUT_DIR}/All"/* "${OUTPUT_DIR}/"

# ------------------------------------------------------------------------------
# 5. 生成客户端配置文件 opnbox.conf
# ------------------------------------------------------------------------------
echo "==> 生成 OPNsense 客户端配置文件 opnbox.conf..."
cat << EOF > "${OUTPUT_DIR}/opnbox.conf"
opnbox: {
  url: "${CLIENT_REPO_URL}/\${ABI}",
  mirror_type: "http",
  signature_type: "none",
  priority: 10,
  enabled: yes
}
EOF

# ------------------------------------------------------------------------------
# 6. 同步至对外发布目录 (public)
# ------------------------------------------------------------------------------
if [ -n "${PUBLISH_DIR}" ]; then
  echo "==> 复制产物到发布目录: ${PUBLISH_DIR}..."
  mkdir -p "${PUBLISH_DIR}"
  cp -r "${OUTPUT_DIR}"/* "${PUBLISH_DIR}/"
fi

echo "=========================================================="
echo " OPNsense 软件源仓库生成完毕！"
echo " 产物列表:"
ls -lh "${OUTPUT_DIR}/All"
echo "=========================================================="
