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
BUILD_DATE="${BUILD_DATE:-$(date +%Y.%m.%d)}"

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
    --build-date)
      BUILD_DATE="$2"; shift 2 ;;
    -h|--help)
      echo "用法: $0 [选项]"
      echo "选项:"
      echo "  --dist-dir <dir>       自编译产物所在目录 (默认: dist)"
      echo "  --output-dir <dir>     软件源生成输出目录 (默认: repo_output)"
      echo "  --publish-dir <dir>    最终对外发布目录 (默认: public)"
      echo "  --repo-owner <owner>   GitHub 仓库拥有者"
      echo "  --repo-name <name>     GitHub 仓库名称"
      echo "  --domain <domain>      自定义域名 (例如: opnbox.zro.qzz.io)"
      echo "  --build-date <date>    自编译包版本号 (默认: YYYY.MM.DD，例如: $(date +%Y.%m.%d))"
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
echo " 自编版本号:  ${BUILD_DATE} (构建日期 年.月.日)"
echo "=========================================================="

# 1. 准备 Staging 目录结构
STAGE_DIR="/tmp/stage"
STAGE_UI_DIR="/tmp/stage-ui"
rm -rf "${STAGE_DIR}" "${STAGE_UI_DIR}"
mkdir -p "${STAGE_DIR}/usr/local/sbin" \
         "${STAGE_DIR}/usr/local/bin" \
         "${STAGE_DIR}/usr/local/etc/rc.d" \
         "${STAGE_DIR}/usr/local/etc/mosdns" \
         "${STAGE_DIR}/usr/local/etc/hev-socks5-tunnel" \
         "${STAGE_DIR}/usr/local/share/xray" \
         "${STAGE_DIR}/usr/local/etc/xray" \
         "${STAGE_UI_DIR}/usr/local"

mkdir -p "${OUTPUT_DIR}/${ABI}" "${OUTPUT_DIR}/All"

# 1.1 在 FreeBSD 原生环境中源码编译 hev-socks5-tunnel (若尚未下载到预编译稳定版)
if [ ! -f "${DIST_DIR}/bin/hev-socks5-tunnel" ]; then
  if command -v gmake >/dev/null 2>&1 && command -v git >/dev/null 2>&1; then
    echo "==> [FreeBSD 原生编译] 正在通过稳定 Release Tag 源码编译 hev-socks5-tunnel..."
    HEV_SRC_DIR="/tmp/hev-socks5-tunnel-src"
    rm -rf "${HEV_SRC_DIR}"
    git clone --recursive https://github.com/heiher/hev-socks5-tunnel.git "${HEV_SRC_DIR}"
    (
      cd "${HEV_SRC_DIR}"
      LATEST_TAG=$(git describe --tags $(git rev-list --tags --max-count=1) 2>/dev/null || echo "")
      if [ -n "${LATEST_TAG}" ]; then
        echo "    -> 检出官方稳定版本标签: ${LATEST_TAG}"
        git checkout "${LATEST_TAG}" 2>/dev/null || true
        git submodule update --init --recursive
      fi
      gmake
      mkdir -p "${DIST_DIR}/bin"
      cp bin/hev-socks5-tunnel "${DIST_DIR}/bin/"
    )
    rm -rf "${HEV_SRC_DIR}"
    echo "    -> hev-socks5-tunnel 原生编译成功！"
  else
    echo "提示: 未检测到 gmake 或 git，跳过 hev-socks5-tunnel 原生源码编译"
  fi
fi

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
[ -f "${DIST_DIR}/bin/hev-socks5-tunnel" ] && cp "${DIST_DIR}/bin/hev-socks5-tunnel" "${STAGE_DIR}/usr/local/bin/"
[ -f "${DIST_DIR}/bin/hev-controller" ] && cp "${DIST_DIR}/bin/hev-controller" "${STAGE_DIR}/usr/local/bin/"
[ -f "${DIST_DIR}/bin/xray" ] && cp "${DIST_DIR}/bin/xray" "${STAGE_DIR}/usr/local/bin/"

if [ -d "${DIST_DIR}/share/xray" ]; then
  cp -r "${DIST_DIR}/share/xray/"* "${STAGE_DIR}/usr/local/share/xray/" 2>/dev/null || true
fi

if [ -f "${WORKSPACE_DIR}/rc.d/hev_socks5_tunnel" ]; then
  cp "${WORKSPACE_DIR}/rc.d/hev_socks5_tunnel" "${STAGE_DIR}/usr/local/etc/rc.d/"
elif [ -f "${DIST_DIR}/rc.d/hev_socks5_tunnel" ]; then
  cp "${DIST_DIR}/rc.d/hev_socks5_tunnel" "${STAGE_DIR}/usr/local/etc/rc.d/"
fi

if [ -f "${WORKSPACE_DIR}/rc.d/hev_controller" ]; then
  cp "${WORKSPACE_DIR}/rc.d/hev_controller" "${STAGE_DIR}/usr/local/etc/rc.d/"
elif [ -f "${DIST_DIR}/rc.d/hev_controller" ]; then
  cp "${DIST_DIR}/rc.d/hev_controller" "${STAGE_DIR}/usr/local/etc/rc.d/"
fi

if [ -f "${WORKSPACE_DIR}/rc.d/xray" ]; then
  cp "${WORKSPACE_DIR}/rc.d/xray" "${STAGE_DIR}/usr/local/etc/rc.d/"
elif [ -f "${DIST_DIR}/rc.d/xray" ]; then
  cp "${DIST_DIR}/rc.d/xray" "${STAGE_DIR}/usr/local/etc/rc.d/"
fi

if [ -f "${WORKSPACE_DIR}/config.example.yaml" ]; then
  cp "${WORKSPACE_DIR}/config.example.yaml" "${STAGE_DIR}/usr/local/etc/mosdns/config.yaml.sample"
elif [ -f "${DIST_DIR}/etc/mosdns.yaml.example" ]; then
  cp "${DIST_DIR}/etc/mosdns.yaml.example" "${STAGE_DIR}/usr/local/etc/mosdns/config.yaml.sample"
fi

if [ -f "${WORKSPACE_DIR}/config.hev-socks5-tunnel.example.yaml" ]; then
  cp "${WORKSPACE_DIR}/config.hev-socks5-tunnel.example.yaml" "${STAGE_DIR}/usr/local/etc/hev-socks5-tunnel/config.yaml.sample"
elif [ -f "${DIST_DIR}/etc/hev-socks5-tunnel.yaml.example" ]; then
  cp "${DIST_DIR}/etc/hev-socks5-tunnel.yaml.example" "${STAGE_DIR}/usr/local/etc/hev-socks5-tunnel/config.yaml.sample"
fi

if [ -f "${WORKSPACE_DIR}/config.xray.example.json" ]; then
  cp "${WORKSPACE_DIR}/config.xray.example.json" "${STAGE_DIR}/usr/local/etc/xray/config.json.sample"
elif [ -f "${DIST_DIR}/etc/xray.json.example" ]; then
  cp "${DIST_DIR}/etc/xray.json.example" "${STAGE_DIR}/usr/local/etc/xray/config.json.sample"
fi

chmod +x "${STAGE_DIR}/usr/local/sbin/"* "${STAGE_DIR}/usr/local/bin/"* "${STAGE_DIR}/usr/local/etc/rc.d/"* 2>/dev/null || true

# 3. 逐个生成 FreeBSD 格式软件包 (.pkg)
# ------------------------------------------------------------------------------
# 3.1 Package: pf-aliasd
# ------------------------------------------------------------------------------
if [ -f "${STAGE_DIR}/usr/local/sbin/pf-aliasd" ]; then
  echo "==> 打包 pf-aliasd (自编: ${BUILD_DATE})..."
  cat << EOF > /tmp/manifest_pf_aliasd
name: pf-aliasd
version: "${BUILD_DATE}"
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
  echo "==> 打包 mosdns (官方源码 v5.3.4 + pf_alias)..."
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
  pf-aliasd: { version: "${BUILD_DATE}", origin: "net/pf-aliasd" }
}
EOF
  cat << EOF > /tmp/plist_mosdns
bin/mosdns
etc/mosdns/config.yaml.sample
EOF
  pkg create -M /tmp/manifest_mosdns -p /tmp/plist_mosdns -r "${STAGE_DIR}" -o "${OUTPUT_DIR}/All"
fi

# ------------------------------------------------------------------------------
# 3.3 Package: mosdns-x (演进版，支持 DoQ/DoH3，自编译)
# ------------------------------------------------------------------------------
if [ -f "${STAGE_DIR}/usr/local/bin/mosdns-x" ]; then
  echo "==> 打包 mosdns-x (自编: ${BUILD_DATE})..."
  cat << EOF > /tmp/manifest_mosdns_x
name: mosdns-x
version: "${BUILD_DATE}"
origin: dns/mosdns-x
comment: "High-performance modular DNS forwarder with DoQ, DoH3 and pf_alias"
desc: "MosDNS-X engine compiled from source with pf_alias plugin for OPNsense"
maintainer: "admin@opn-box.local"
www: "${PROJECT_WEB_URL}"
prefix: /usr/local
categories: [dns]
arch: "FreeBSD:*:amd64"
deps: {
  pf-aliasd: { version: "${BUILD_DATE}", origin: "net/pf-aliasd" }
}
EOF
  cat << EOF > /tmp/plist_mosdns_x
bin/mosdns-x
etc/mosdns/config.yaml.sample
EOF
  pkg create -M /tmp/manifest_mosdns_x -p /tmp/plist_mosdns_x -r "${STAGE_DIR}" -o "${OUTPUT_DIR}/All"
fi

# ------------------------------------------------------------------------------
# 3.4 Package: mosdns-controller (Web UI 面板与控制器，自编译)
# ------------------------------------------------------------------------------
if [ -f "${STAGE_DIR}/usr/local/bin/mosdns-controller" ]; then
  echo "==> 打包 mosdns-controller (自编: ${BUILD_DATE})..."
  cat << EOF > /tmp/manifest_controller
name: mosdns-controller
version: "${BUILD_DATE}"
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
# 3.6 Package: hev-socks5-tunnel (高性能 Tun2Socks 代理与 WebUI 管理器)
# ------------------------------------------------------------------------------
if [ -f "${STAGE_DIR}/usr/local/bin/hev-socks5-tunnel" ] || [ -f "${STAGE_DIR}/usr/local/bin/hev-controller" ]; then
  echo "==> 打包 hev-socks5-tunnel (Tun2Socks + Controller)..."
  cat << EOF > /tmp/manifest_hev
name: hev-socks5-tunnel
version: "2.13.0"
origin: net/hev-socks5-tunnel
comment: "High-performance Tun2Socks proxy bridge and Web UI manager"
desc: "hev-socks5-tunnel compiled from source with coroutines and hev-controller WebUI"
maintainer: "admin@opn-box.local"
www: "${PROJECT_WEB_URL}"
prefix: /usr/local
categories: [net]
arch: "FreeBSD:*:amd64"
EOF
  rm -f /tmp/plist_hev
  [ -f "${STAGE_DIR}/usr/local/bin/hev-socks5-tunnel" ] && echo "bin/hev-socks5-tunnel" >> /tmp/plist_hev
  [ -f "${STAGE_DIR}/usr/local/bin/hev-controller" ] && echo "bin/hev-controller" >> /tmp/plist_hev
  [ -f "${STAGE_DIR}/usr/local/etc/rc.d/hev_socks5_tunnel" ] && echo "etc/rc.d/hev_socks5_tunnel" >> /tmp/plist_hev
  [ -f "${STAGE_DIR}/usr/local/etc/rc.d/hev_controller" ] && echo "etc/rc.d/hev_controller" >> /tmp/plist_hev
  [ -f "${STAGE_DIR}/usr/local/etc/hev-socks5-tunnel/config.yaml.sample" ] && echo "etc/hev-socks5-tunnel/config.yaml.sample" >> /tmp/plist_hev

  pkg create -M /tmp/manifest_hev -p /tmp/plist_hev -r "${STAGE_DIR}" -o "${OUTPUT_DIR}/All"
fi

# ------------------------------------------------------------------------------
# 3.7 Package: xray-core (官方稳定版代理内核，原生支持 xhttp、VLESS 与 SNI 嗅探)
# ------------------------------------------------------------------------------
if [ -f "${STAGE_DIR}/usr/local/bin/xray" ]; then
  echo "==> 打包 xray-core (官方稳定版)..."
  cat << EOF > /tmp/manifest_xray
name: xray-core
version: "25.1.30"
origin: security/xray-core
comment: "Xray-core proxy engine with VLESS, xhttp and sniffing support"
desc: "Official Xray-core pre-compiled release packaged for OPNsense"
maintainer: "admin@opn-box.local"
www: "${PROJECT_WEB_URL}"
prefix: /usr/local
categories: [security]
arch: "FreeBSD:*:amd64"
EOF
  rm -f /tmp/plist_xray
  [ -f "${STAGE_DIR}/usr/local/bin/xray" ] && echo "bin/xray" >> /tmp/plist_xray
  [ -f "${STAGE_DIR}/usr/local/share/xray/geoip.dat" ] && echo "share/xray/geoip.dat" >> /tmp/plist_xray
  [ -f "${STAGE_DIR}/usr/local/share/xray/geosite.dat" ] && echo "share/xray/geosite.dat" >> /tmp/plist_xray
  [ -f "${STAGE_DIR}/usr/local/etc/rc.d/xray" ] && echo "etc/rc.d/xray" >> /tmp/plist_xray
  [ -f "${STAGE_DIR}/usr/local/etc/xray/config.json.sample" ] && echo "etc/xray/config.json.sample" >> /tmp/plist_xray

  pkg create -M /tmp/manifest_xray -p /tmp/plist_xray -r "${STAGE_DIR}" -o "${OUTPUT_DIR}/All"
fi

# ------------------------------------------------------------------------------
# 3.8 Package: os-mosdns (OPNsense WebGUI 插件，自编译)
# ------------------------------------------------------------------------------
if [ -d "${WORKSPACE_DIR}/src/os-mosdns/src" ]; then
  echo "==> 打包 os-mosdns (OPNsense UI 插件，自编: ${BUILD_DATE})..."
  cp -r "${WORKSPACE_DIR}/src/os-mosdns/src/"* "${STAGE_UI_DIR}/usr/local/"
  cat << EOF > /tmp/manifest_os_mosdns
name: os-mosdns
version: "${BUILD_DATE}"
origin: opnsense/os-mosdns
comment: "MosDNS Dynamic Routing Engine with pf-aliasd & Controller UI"
desc: "OPNsense WebGUI plugin for MosDNS with pf-aliasd"
maintainer: "admin@opn-box.local"
www: "${PROJECT_WEB_URL}"
prefix: /usr/local
categories: [opnsense]
arch: "*"
deps: {
  pf-aliasd: { version: "${BUILD_DATE}", origin: "net/pf-aliasd" },
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
