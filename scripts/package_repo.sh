#!/bin/sh
set -e

# ==============================================================================
# package_repo.sh - FreeBSD 原生打包与 OPNsense 软件源生成自动化脚本
#
# 特性：
# 1. 严格使用标准 POSIX /bin/sh 语法，可在 FreeBSD 原生环境及 VM 中无缝执行
# 2. 自动化将编译产物装载到 stage 目录并构建 FreeBSD 格式 .pkg 安装包
# 3. 补齐强制元数据（www、abi/arch 严格指定 FreeBSD:15:amd64），专供 FreeBSD 15 (OPNsense 26.x)
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
HEV_TUNNEL_VERSION="${HEV_TUNNEL_VERSION:-2.17.1}"
XRAY_VERSION="${XRAY_VERSION:-26.7.11}"

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
    --hev-version)
      HEV_TUNNEL_VERSION="$2"; shift 2 ;;
    --xray-version)
      XRAY_VERSION="$2"; shift 2 ;;
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
      echo "  --hev-version <ver>    hev-socks5-tunnel 固定版本号 (默认: ${HEV_TUNNEL_VERSION})"
      echo "  --xray-version <ver>   xray-core 固定版本号 (默认: ${XRAY_VERSION})"
      exit 0
      ;;
    *)
      echo "未知参数: $1" >&2
      exit 1
      ;;
  esac
done

HEV_TUNNEL_VERSION="${HEV_TUNNEL_VERSION#v}"
XRAY_VERSION="${XRAY_VERSION#v}"

# 计算对外访问 URL 与项目主页
if [ -n "${CUSTOM_DOMAIN}" ]; then
  CLIENT_REPO_URL="https://${CUSTOM_DOMAIN}"
else
  CLIENT_REPO_URL="https://${REPO_OWNER}.github.io/${REPO_NAME}"
fi
PROJECT_WEB_URL="https://github.com/${REPO_OWNER}/${REPO_NAME}"

# 严格指定目标系统 ABI 为 FreeBSD 15 (FreeBSD:15:amd64)，杜绝通配符
ABI="FreeBSD:15:amd64"
TARGET_PKG_DIR="${OUTPUT_DIR}/${ABI}"

echo "=========================================================="
echo " 开始生成 OPNsense 软件源仓库 (FreeBSD pkg repo)"
echo " 工作目录:   ${WORKSPACE_DIR}"
echo " 编译产物:   ${DIST_DIR}"
echo " 目标源目录: ${OUTPUT_DIR}"
echo " 目标ABI目录: ${TARGET_PKG_DIR}"
echo " 发布目录:   ${PUBLISH_DIR}"
echo " 当前系统ABI: ${ABI}"
echo " 仓库终端URL: ${CLIENT_REPO_URL}"
echo " 自编版本号:  ${BUILD_DATE} (构建日期 年.月.日)"
echo " 官源 hev 版本: v${HEV_TUNNEL_VERSION}"
echo " 官源 xray 版本: v${XRAY_VERSION}"
echo "=========================================================="

# 1. 准备 Staging 与 Output 目录结构
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

# 清理并创建确定的 ABI 目标目录（根目录不留多余 All 或 FreeBSD 14 目录）
rm -rf "${OUTPUT_DIR}"
mkdir -p "${TARGET_PKG_DIR}"

# 1.1 在 FreeBSD 原生环境中源码编译 hev-socks5-tunnel (若尚未下载到预编译稳定版)
if [ ! -f "${DIST_DIR}/bin/hev-socks5-tunnel" ]; then
  if command -v gmake >/dev/null 2>&1 && command -v git >/dev/null 2>&1; then
    echo "==> [FreeBSD 原生编译] 正在通过指定稳定标签 (${HEV_TUNNEL_VERSION}) 源码编译 hev-socks5-tunnel..."
    HEV_SRC_DIR="/tmp/hev-socks5-tunnel-src"
    rm -rf "${HEV_SRC_DIR}"
    git clone --branch "${HEV_TUNNEL_VERSION}" --recursive --depth 1 https://github.com/heiher/hev-socks5-tunnel.git "${HEV_SRC_DIR}" 2>/dev/null || \
      git clone --recursive --depth 1 https://github.com/heiher/hev-socks5-tunnel.git "${HEV_SRC_DIR}"
    (
      cd "${HEV_SRC_DIR}"
      git checkout "${HEV_TUNNEL_VERSION}" 2>/dev/null || git checkout "v${HEV_TUNNEL_VERSION}" 2>/dev/null || true
      git submodule update --init --recursive
      gmake
      mkdir -p "${DIST_DIR}/bin"
      cp bin/hev-socks5-tunnel "${DIST_DIR}/bin/"
    )
    rm -rf "${HEV_SRC_DIR}"
    echo "    -> hev-socks5-tunnel (${HEV_TUNNEL_VERSION}) 原生编译成功！"
  else
    echo "提示: 未检测到 gmake 或 git，跳过 hev-socks5-tunnel 原生源码编译"
  fi
fi

# 2. 复制二进制与运行配置到 staging 目录
if [ -f "${DIST_DIR}/bin/pf-aliasd" ]; then
  cp "${DIST_DIR}/bin/pf-aliasd" "${STAGE_DIR}/usr/local/sbin/"
fi
if [ -f "${WORKSPACE_DIR}/scripts/update_rules.sh" ]; then
  cp "${WORKSPACE_DIR}/scripts/update_rules.sh" "${STAGE_DIR}/usr/local/sbin/update-opnbox-rules.sh"
elif [ -f "${DIST_DIR}/sbin/update-opnbox-rules.sh" ]; then
  cp "${DIST_DIR}/sbin/update-opnbox-rules.sh" "${STAGE_DIR}/usr/local/sbin/update-opnbox-rules.sh"
fi
chmod +x "${STAGE_DIR}/usr/local/sbin/"* 2>/dev/null || true

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
[ -f "${DIST_DIR}/bin/xray-controller" ] && cp "${DIST_DIR}/bin/xray-controller" "${STAGE_DIR}/usr/local/bin/"

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

if [ -f "${WORKSPACE_DIR}/rc.d/xray_controller" ]; then
  cp "${WORKSPACE_DIR}/rc.d/xray_controller" "${STAGE_DIR}/usr/local/etc/rc.d/"
elif [ -f "${DIST_DIR}/rc.d/xray_controller" ]; then
  cp "${DIST_DIR}/rc.d/xray_controller" "${STAGE_DIR}/usr/local/etc/rc.d/"
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
abi: "FreeBSD:15:amd64"
arch: "FreeBSD:15:amd64"
EOF
  cat << EOF > /tmp/plist_pf_aliasd
sbin/pf-aliasd
etc/rc.d/pf_aliasd
EOF
  pkg create -M /tmp/manifest_pf_aliasd -p /tmp/plist_pf_aliasd -r "${STAGE_DIR}" -o "${TARGET_PKG_DIR}"
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
abi: "FreeBSD:15:amd64"
arch: "FreeBSD:15:amd64"
deps: {
  pf-aliasd: { version: "${BUILD_DATE}", origin: "net/pf-aliasd" }
}
EOF
  cat << EOF > /tmp/plist_mosdns
bin/mosdns
etc/mosdns/config.yaml.sample
EOF
  pkg create -M /tmp/manifest_mosdns -p /tmp/plist_mosdns -r "${STAGE_DIR}" -o "${TARGET_PKG_DIR}"
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
abi: "FreeBSD:15:amd64"
arch: "FreeBSD:15:amd64"
deps: {
  pf-aliasd: { version: "${BUILD_DATE}", origin: "net/pf-aliasd" }
}
EOF
  cat << EOF > /tmp/plist_mosdns_x
bin/mosdns-x
etc/mosdns/config.yaml.sample
EOF
  pkg create -M /tmp/manifest_mosdns_x -p /tmp/plist_mosdns_x -r "${STAGE_DIR}" -o "${TARGET_PKG_DIR}"
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
abi: "FreeBSD:15:amd64"
arch: "FreeBSD:15:amd64"
EOF
  cat << EOF > /tmp/plist_controller
bin/mosdns-controller
EOF
  pkg create -M /tmp/manifest_controller -p /tmp/plist_controller -r "${STAGE_DIR}" -o "${TARGET_PKG_DIR}"
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
abi: "FreeBSD:15:amd64"
arch: "FreeBSD:15:amd64"
EOF
  cat << EOF > /tmp/plist_singbox
bin/sing-box
EOF
  pkg create -M /tmp/manifest_singbox -p /tmp/plist_singbox -r "${STAGE_DIR}" -o "${TARGET_PKG_DIR}"
fi

# ------------------------------------------------------------------------------
# 3.6 Package: hev-socks5-tunnel (高性能 Tun2Socks 代理与 WebUI 管理器)
# ------------------------------------------------------------------------------
if [ -f "${STAGE_DIR}/usr/local/bin/hev-socks5-tunnel" ] || [ -f "${STAGE_DIR}/usr/local/bin/hev-controller" ]; then
  echo "==> 打包 hev-socks5-tunnel (Tun2Socks + Controller, v${HEV_TUNNEL_VERSION})..."
  cat << EOF > /tmp/manifest_hev
name: hev-socks5-tunnel
version: "${HEV_TUNNEL_VERSION}"
origin: net/hev-socks5-tunnel
comment: "High-performance Tun2Socks proxy bridge and Web UI manager"
desc: "hev-socks5-tunnel compiled from source with coroutines and hev-controller WebUI"
maintainer: "admin@opn-box.local"
www: "${PROJECT_WEB_URL}"
prefix: /usr/local
categories: [net]
abi: "FreeBSD:15:amd64"
arch: "FreeBSD:15:amd64"
EOF
  rm -f /tmp/plist_hev
  [ -f "${STAGE_DIR}/usr/local/bin/hev-socks5-tunnel" ] && echo "bin/hev-socks5-tunnel" >> /tmp/plist_hev
  [ -f "${STAGE_DIR}/usr/local/bin/hev-controller" ] && echo "bin/hev-controller" >> /tmp/plist_hev
  [ -f "${STAGE_DIR}/usr/local/etc/rc.d/hev_socks5_tunnel" ] && echo "etc/rc.d/hev_socks5_tunnel" >> /tmp/plist_hev
  [ -f "${STAGE_DIR}/usr/local/etc/rc.d/hev_controller" ] && echo "etc/rc.d/hev_controller" >> /tmp/plist_hev
  [ -f "${STAGE_DIR}/usr/local/etc/hev-socks5-tunnel/config.yaml.sample" ] && echo "etc/hev-socks5-tunnel/config.yaml.sample" >> /tmp/plist_hev

  pkg create -M /tmp/manifest_hev -p /tmp/plist_hev -r "${STAGE_DIR}" -o "${TARGET_PKG_DIR}"
fi

# ------------------------------------------------------------------------------
# 3.7 Package: xray-core (官方稳定版代理内核，原生支持 xhttp、VLESS 与 SNI 嗅探，内嵌 Web 管理器)
# ------------------------------------------------------------------------------
if [ -f "${STAGE_DIR}/usr/local/bin/xray" ] || [ -f "${STAGE_DIR}/usr/local/bin/xray-controller" ]; then
  echo "==> 打包 xray-core (官方稳定版 + Xray Manager, v${XRAY_VERSION})..."
  cat << EOF > /tmp/manifest_xray
name: xray-core
version: "${XRAY_VERSION}"
origin: security/xray-core
comment: "Xray-core proxy engine with Web UI manager, VLESS, xhttp and sniffing support"
desc: "Official Xray-core release packaged with xray-controller WebUI for OPNsense"
maintainer: "admin@opn-box.local"
www: "${PROJECT_WEB_URL}"
prefix: /usr/local
categories: [security]
abi: "FreeBSD:15:amd64"
arch: "FreeBSD:15:amd64"
EOF
  rm -f /tmp/plist_xray
  [ -f "${STAGE_DIR}/usr/local/bin/xray" ] && echo "bin/xray" >> /tmp/plist_xray
  [ -f "${STAGE_DIR}/usr/local/bin/xray-controller" ] && echo "bin/xray-controller" >> /tmp/plist_xray
  [ -f "${STAGE_DIR}/usr/local/share/xray/geoip.dat" ] && echo "share/xray/geoip.dat" >> /tmp/plist_xray
  [ -f "${STAGE_DIR}/usr/local/share/xray/geosite.dat" ] && echo "share/xray/geosite.dat" >> /tmp/plist_xray
  [ -f "${STAGE_DIR}/usr/local/etc/rc.d/xray" ] && echo "etc/rc.d/xray" >> /tmp/plist_xray
  [ -f "${STAGE_DIR}/usr/local/etc/rc.d/xray_controller" ] && echo "etc/rc.d/xray_controller" >> /tmp/plist_xray
  [ -f "${STAGE_DIR}/usr/local/etc/xray/config.json.sample" ] && echo "etc/xray/config.json.sample" >> /tmp/plist_xray

  pkg create -M /tmp/manifest_xray -p /tmp/plist_xray -r "${STAGE_DIR}" -o "${TARGET_PKG_DIR}"
fi

# ------------------------------------------------------------------------------
# 3.8 Package: os-mosdns (OPNsense WebGUI 插件，自编译)
# ------------------------------------------------------------------------------
if [ -d "${WORKSPACE_DIR}/src/os-mosdns/src" ]; then
  echo "==> 打包 os-mosdns (OPNsense UI 插件，自编: ${BUILD_DATE})..."
  mkdir -p "${STAGE_UI_DIR}/usr/local/sbin"
  cp -r "${WORKSPACE_DIR}/src/os-mosdns/src/"* "${STAGE_UI_DIR}/usr/local/"
  if [ -f "${WORKSPACE_DIR}/scripts/update_rules.sh" ]; then
    cp "${WORKSPACE_DIR}/scripts/update_rules.sh" "${STAGE_UI_DIR}/usr/local/sbin/update-opnbox-rules.sh"
  elif [ -f "${DIST_DIR}/sbin/update-opnbox-rules.sh" ]; then
    cp "${DIST_DIR}/sbin/update-opnbox-rules.sh" "${STAGE_UI_DIR}/usr/local/sbin/update-opnbox-rules.sh"
  fi
  chmod +x "${STAGE_UI_DIR}/usr/local/sbin/"* 2>/dev/null || true

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
abi: "FreeBSD:15:amd64"
arch: "FreeBSD:15:amd64"
deps: {
  pf-aliasd: { version: "${BUILD_DATE}", origin: "net/pf-aliasd" },
  mosdns: { version: "5.3.4", origin: "dns/mosdns" }
}
EOF
  cat << EOF > /tmp/plist_os_mosdns
sbin/update-opnbox-rules.sh
opnsense/service/conf/actions.d/actions_mosdns.conf
opnsense/mvc/app/models/OPNsense/Mosdns/Menu/Menu.xml
EOF
  pkg create -M /tmp/manifest_os_mosdns -p /tmp/plist_os_mosdns -r "${STAGE_UI_DIR}" -o "${TARGET_PKG_DIR}"
fi

# ------------------------------------------------------------------------------
# 4. 生成软件源元数据索引 (pkg repo)
# ------------------------------------------------------------------------------
echo "==> 在 ${TARGET_PKG_DIR} 生成 FreeBSD:15:amd64 pkg 索引与元数据..."
pkg repo "${TARGET_PKG_DIR}"

# ------------------------------------------------------------------------------
# 5. 生成客户端配置文件 opnbox.conf (根目录仅保留 opnbox.conf 及 ABI 子目录)
# ------------------------------------------------------------------------------
echo "==> 在根目录生成 OPNsense 客户端配置文件 opnbox.conf..."
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
  rm -rf "${PUBLISH_DIR}"
  mkdir -p "${PUBLISH_DIR}"
  cp -r "${OUTPUT_DIR}"/* "${PUBLISH_DIR}/"
fi

echo "=========================================================="
echo " OPNsense 软件源仓库生成完毕！"
echo " 根目录结构 (仅保留 opnbox.conf 与 ABI 目录):"
ls -lh "${OUTPUT_DIR}"
echo " FreeBSD:15:amd64 软件源包与元数据列表:"
ls -lh "${TARGET_PKG_DIR}"
echo "=========================================================="
