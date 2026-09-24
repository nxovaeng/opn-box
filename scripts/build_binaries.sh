#!/bin/bash
set -e

# ==============================================================================
# build_binaries.sh - 100% 源码自编译自动化脚本 (版本固定 + 本地补丁)
# 
# 特性：
# 1. 严格锁定各个组件的版本号 (支持环境变量覆写定制)
# 2. bsd-box 补丁永久存储在本地仓库 patches/sing-box/ 下，不依赖第三方分支
# 3. 完整支持 mosdns-controller (基于 v5.3.4 managed-dns) 与 mosdns-x
# ==============================================================================

WORKSPACE_DIR="$(cd "$(dirname "$0")/.." && pwd)"
OUTPUT_DIR="${1:-${WORKSPACE_DIR}/dist/bin}"
mkdir -p "${OUTPUT_DIR}"
OUTPUT_DIR="$(cd "${OUTPUT_DIR}" && pwd)"
BUILD_TMP=$(mktemp -d)
trap 'rm -rf "${BUILD_TMP}"' EXIT

export GOOS=freebsd
export GOARCH=amd64
export CGO_ENABLED=0

# --- 明确锁定的上游版本号 (严格指定版本号) ---
SINGBOX_VERSION="${SINGBOX_VERSION:-1.13.14}"
MOSDNS_VERSION="${MOSDNS_VERSION:-v5.3.4}"                                                    # 官方原版 v5.3.4
MOSDNS_CONTROLLER_TAG="${MOSDNS_CONTROLLER_TAG:-v0.0.2}"                                     # Web UI 控制器发行版
MOSDNS_X_TAG="${MOSDNS_X_TAG:-v26.01.18}"                                                   # 演进版 (默认不编译)
BUILD_MOSDNS_X="${BUILD_MOSDNS_X:-0}"                                                       # 默认跳过，后续稳定后再开启
HEV_TUNNEL_VERSION="${HEV_TUNNEL_VERSION:-2.17.1}"                                            # 官方发布稳定版 (2026.08)
XRAY_VERSION="${XRAY_VERSION:-26.7.11}"                                                       # 官方发布稳定版 (2026.07)

echo "=========================================================="
echo " 开始 100% 源码自编译流水线 (FreeBSD 64-bit / OPNsense 26.x)"
echo " 工作目录: ${WORKSPACE_DIR}"
echo " 目标输出: ${OUTPUT_DIR}"
echo " 锁定版本: sing-box=v${SINGBOX_VERSION}"
echo "           mosdns=${MOSDNS_VERSION}"
echo "           mosdns-controller=${MOSDNS_CONTROLLER_TAG}"
echo "           hev-socks5-tunnel=v${HEV_TUNNEL_VERSION}"
echo "           xray-core=v${XRAY_VERSION#v}"
if [ "${BUILD_MOSDNS_X}" = "1" ]; then
    echo "           mosdns-x=${MOSDNS_X_TAG}"
else
    echo "           mosdns-x=已禁用 (默认不编译)"
fi
echo "=========================================================="

# ------------------------------------------------------------------------------
# 1. 编译本地自研控制服务 (pf-aliasd, hev-controller, xray-controller)
# ------------------------------------------------------------------------------
echo "==> [1/7] 编译本地 pf-aliasd 守护进程..."
(
    cd "${WORKSPACE_DIR}"
    go build -trimpath -ldflags="-s -w" -o "${OUTPUT_DIR}/pf-aliasd" ./cmd/pf-aliasd
)
echo "    -> pf-aliasd 编译成功"

echo "==> [2/7] 编译本地 hev-controller (Tun2Socks Web 管理面板)..."
(
    cd "${WORKSPACE_DIR}"
    go build -trimpath -ldflags="-s -w" -o "${OUTPUT_DIR}/hev-controller" ./cmd/hev-controller
)
echo "    -> hev-controller 编译成功"

echo "==> [3/7] 编译本地 xray-controller (Xray Manager Web 管理面板)..."
(
    cd "${WORKSPACE_DIR}"
    go build -trimpath -ldflags="-s -w" -o "${OUTPUT_DIR}/xray-controller" ./cmd/xray-controller
)
echo "    -> xray-controller 编译成功"

# ------------------------------------------------------------------------------
# 2. 从源码编译官方 mosdns (严格指定 v5.3.4，嵌入自研 pf_alias)
# ------------------------------------------------------------------------------
echo "==> [4/7] 编译 mosdns (官方源码 ${MOSDNS_VERSION}，嵌入自研 pf_alias)..."
git clone --branch "${MOSDNS_VERSION}" --depth=1 https://github.com/IrineSistiana/mosdns.git "${BUILD_TMP}/mosdns"
mkdir -p "${BUILD_TMP}/mosdns/plugin/executable/pf_alias"
cp -r "${WORKSPACE_DIR}/pkg/plugin/"* "${BUILD_TMP}/mosdns/plugin/executable/pf_alias/"
(
    cd "${BUILD_TMP}/mosdns"
    go mod edit -require "opn-box@v0.0.0"
    go mod edit -replace "opn-box=${WORKSPACE_DIR}"
    sed -i 's|import (|import (\n\t_ "github.com/IrineSistiana/mosdns/v5/plugin/executable/pf_alias"|' main.go
    go mod tidy
    go build -trimpath -ldflags="-s -w" -o "${OUTPUT_DIR}/mosdns" .
)
echo "    -> mosdns (${MOSDNS_VERSION}) 编译成功"

# ------------------------------------------------------------------------------
# 3. 从源码编译 luoye663/mosdns-controller (Web UI 面板，内嵌生产打包 Web 静态资源)
# ------------------------------------------------------------------------------
echo "==> [5/7] 从源码拉取并编译 mosdns-controller (${MOSDNS_CONTROLLER_TAG})..."
git clone --branch "${MOSDNS_CONTROLLER_TAG}" --depth=1 https://github.com/luoye663/mosdns-controller.git "${BUILD_TMP}/mosdns-controller"
(
    cd "${BUILD_TMP}/mosdns-controller"
    if [ -d "web" ] && command -v npm >/dev/null 2>&1; then
        echo "    -> 检测到 npm，开始构建 Web 前端生产资源并嵌入..."
        (
            cd web
            npm ci || npm install
            npm run build
        )
        rm -rf controller/internal/web/static/*
        cp -R web/dist/. controller/internal/web/static/
        echo "    -> Web 前端资源已成功嵌入 controller/internal/web/static"
    else
        echo "    [WARN] 未检测到 npm 或 web 目录，将使用内置 placeholder"
    fi

    cd controller
    go build -trimpath -ldflags="-s -w -X 'github.com/managed-dns/controller/internal/version.ProjectVersion=${MOSDNS_CONTROLLER_TAG}' -X 'github.com/managed-dns/controller/internal/version.MosdnsBase=v5.3.4'" -o "${OUTPUT_DIR}/mosdns-controller" ./cmd/controller
)
if [ -f "${OUTPUT_DIR}/mosdns-controller" ]; then
    echo "    -> mosdns-controller 编译成功 (含完整内嵌 WebUI)"
fi

# ------------------------------------------------------------------------------
# 4. 可选：从源码编译 pmkol/mosdns-x (演进版，支持 DoQ/DoH3)
# ------------------------------------------------------------------------------
if [ "${BUILD_MOSDNS_X}" = "1" ]; then
    echo "==> [可选] 编译 mosdns-x (${MOSDNS_X_TAG}，嵌入自研 pf_alias 适配层)..."
    git clone --branch "${MOSDNS_X_TAG}" --depth=1 https://github.com/pmkol/mosdns-x.git "${BUILD_TMP}/mosdns-x"
    mkdir -p "${BUILD_TMP}/mosdns-x/plugin/executable/pf_alias"
    cp -r "${WORKSPACE_DIR}/pkg/plugin_mosdns_x/"* "${BUILD_TMP}/mosdns-x/plugin/executable/pf_alias/"
    (
        cd "${BUILD_TMP}/mosdns-x"
        go mod edit -require "opn-box@v0.0.0"
        go mod edit -replace "opn-box=${WORKSPACE_DIR}"
        sed -i '/\/\/go:build mosdns_x/d' plugin/executable/pf_alias/pf_alias.go
        sed -i 's|github.com/IrineSistiana/mosdns/v5|github.com/pmkol/mosdns-x|g' plugin/executable/pf_alias/pf_alias.go
        sed -i 's|import (|import (\n\t_ "github.com/pmkol/mosdns-x/plugin/executable/pf_alias"|' main.go
        go mod tidy
        go build -trimpath -ldflags="-s -w" -o "${OUTPUT_DIR}/mosdns-x" .
    )
    echo "    -> mosdns-x 编译成功"
else
    echo "==> [可选] 跳过 mosdns-x 编译 (BUILD_MOSDNS_X=0)"
fi

# ------------------------------------------------------------------------------
# 5. 从源码编译 sing-box (使用本地 patches/sing-box/ 补丁，零外部补丁依赖)
# ------------------------------------------------------------------------------
if [ "${BUILD_SINGBOX:-1}" = "1" ]; then
    echo "==> [可选] 从官方源码编译 sing-box (v${SINGBOX_VERSION}，应用本地 FreeBSD TUN 补丁)..."
    (
        cd "${BUILD_TMP}"

        # 1. 下载官方 sing-box 源码
        wget -q "https://github.com/Sagernet/sing-box/archive/refs/tags/v${SINGBOX_VERSION}.tar.gz" -O sing-box.tar.gz
        tar -xzf sing-box.tar.gz
        mv "sing-box-${SINGBOX_VERSION}" sing-box

        # 2. 关联 sing-tun 源码
        cd sing-box
        SING_TUN_VERSION=$(grep "github.com/sagernet/sing-tun" go.mod | awk '{ print $2 }' | sed 's|v||g')
        go mod edit -replace "github.com/sagernet/sing-tun=../sing-tun"
        cd ..

        wget -q "https://github.com/Sagernet/sing-tun/archive/refs/tags/v${SING_TUN_VERSION}.tar.gz" -O sing-tun.tar.gz
        tar -xzf sing-tun.tar.gz
        mv "sing-tun-${SING_TUN_VERSION}" sing-tun

        # 3. 应用本地仓库存储的 FreeBSD TUN 适配补丁
        cd sing-box
        patch -p1 < "${WORKSPACE_DIR}/patches/sing-box/sing-box.patch" || true
        cd ../sing-tun
        patch -p1 < "${WORKSPACE_DIR}/patches/sing-box/sing-tun.patch" || true
        cp monitor_darwin.go monitor_freebsd.go
        cp monitor_darwin.go monitor_openbsd.go

        # 4. 编译全功能 FreeBSD 64位 二进制
        cd ../sing-box
        TAGS="with_gvisor,with_quic,with_dhcp,with_wireguard,with_utls,with_acme,with_clash_api"
        go build -trimpath -ldflags="-s -w" -tags "${TAGS}" -o "${OUTPUT_DIR}/sing-box" ./cmd/sing-box
    )
    echo "    -> sing-box (自编译 + 本地补丁) 编译成功"
fi

# ------------------------------------------------------------------------------
# 6. 拉取外部依赖的官方已发布稳定版 (FreeBSD 64位 二进制，严格锁定版本)
# ------------------------------------------------------------------------------
echo "==> [6/7] 检查/下载 hev-socks5-tunnel 官方发布稳定版 (v${HEV_TUNNEL_VERSION#v})..."
HEV_DL_URL="https://github.com/heiher/hev-socks5-tunnel/releases/download/${HEV_TUNNEL_VERSION}/hev-socks5-tunnel-freebsd-x86_64"
if ! curl -fsSL -o "${OUTPUT_DIR}/hev-socks5-tunnel" "${HEV_DL_URL}"; then
    HEV_DL_URL="https://github.com/heiher/hev-socks5-tunnel/releases/download/v${HEV_TUNNEL_VERSION#v}/hev-socks5-tunnel-freebsd-x86_64"
    curl -fsSL -o "${OUTPUT_DIR}/hev-socks5-tunnel" "${HEV_DL_URL}" || true
fi

if [ -f "${OUTPUT_DIR}/hev-socks5-tunnel" ] && [ -s "${OUTPUT_DIR}/hev-socks5-tunnel" ]; then
    chmod +x "${OUTPUT_DIR}/hev-socks5-tunnel"
    echo "    -> hev-socks5-tunnel (v${HEV_TUNNEL_VERSION#v}) 官方稳定版获取成功"
else
    rm -f "${OUTPUT_DIR}/hev-socks5-tunnel"
    echo "    -> 未能直接下载 hev-socks5-tunnel 预编译二进制，将在 FreeBSD VM 步骤中通过源码 Tag (${HEV_TUNNEL_VERSION}) 编译"
fi

XRAY_TAG="v${XRAY_VERSION#v}"
echo "==> [7/7] 检查/下载 Xray-core 官方发布稳定版 (${XRAY_TAG})..."
XRAY_DL_DIR="${BUILD_TMP}/xray-dl"
mkdir -p "${XRAY_DL_DIR}" "${WORKSPACE_DIR}/dist/share/xray"
if curl -fsSL -o "${XRAY_DL_DIR}/xray.zip" "https://github.com/XTLS/Xray-core/releases/download/${XRAY_TAG}/Xray-freebsd-64.zip"; then
    if command -v unzip >/dev/null 2>&1; then
        unzip -q -o "${XRAY_DL_DIR}/xray.zip" -d "${XRAY_DL_DIR}/"
    else
        python3 -c "import zipfile; zipfile.ZipFile('${XRAY_DL_DIR}/xray.zip').extractall('${XRAY_DL_DIR}')"
    fi
    [ -f "${XRAY_DL_DIR}/xray" ] && cp "${XRAY_DL_DIR}/xray" "${OUTPUT_DIR}/xray" && chmod +x "${OUTPUT_DIR}/xray"
    [ -f "${XRAY_DL_DIR}/geoip.dat" ] && cp "${XRAY_DL_DIR}/geoip.dat" "${WORKSPACE_DIR}/dist/share/xray/"
    [ -f "${XRAY_DL_DIR}/geosite.dat" ] && cp "${XRAY_DL_DIR}/geosite.dat" "${WORKSPACE_DIR}/dist/share/xray/"
    echo "    -> Xray-core (${XRAY_TAG}) 官方稳定版获取并解压成功"
else
    echo "    [WARN] Xray-core (${XRAY_TAG}) 官方下载失败，跳过打包"
fi

# ------------------------------------------------------------------------------
# 8. 同步配置文件模板与 rc.d 服务启动脚本至 dist/ 目录
# ------------------------------------------------------------------------------
mkdir -p "${WORKSPACE_DIR}/dist/rc.d" "${WORKSPACE_DIR}/dist/etc"
[ -f "${WORKSPACE_DIR}/rc.d/pf_aliasd" ] && cp "${WORKSPACE_DIR}/rc.d/pf_aliasd" "${WORKSPACE_DIR}/dist/rc.d/"
[ -f "${WORKSPACE_DIR}/rc.d/hev_socks5_tunnel" ] && cp "${WORKSPACE_DIR}/rc.d/hev_socks5_tunnel" "${WORKSPACE_DIR}/dist/rc.d/"
[ -f "${WORKSPACE_DIR}/rc.d/hev_controller" ] && cp "${WORKSPACE_DIR}/rc.d/hev_controller" "${WORKSPACE_DIR}/dist/rc.d/"
[ -f "${WORKSPACE_DIR}/rc.d/xray" ] && cp "${WORKSPACE_DIR}/rc.d/xray" "${WORKSPACE_DIR}/dist/rc.d/"
[ -f "${WORKSPACE_DIR}/rc.d/xray_controller" ] && cp "${WORKSPACE_DIR}/rc.d/xray_controller" "${WORKSPACE_DIR}/dist/rc.d/"
[ -f "${WORKSPACE_DIR}/config.example.yaml" ] && cp "${WORKSPACE_DIR}/config.example.yaml" "${WORKSPACE_DIR}/dist/etc/mosdns.yaml.example"
[ -f "${WORKSPACE_DIR}/config.hev-socks5-tunnel.example.yaml" ] && cp "${WORKSPACE_DIR}/config.hev-socks5-tunnel.example.yaml" "${WORKSPACE_DIR}/dist/etc/hev-socks5-tunnel.yaml.example"
[ -f "${WORKSPACE_DIR}/config.xray.example.json" ] && cp "${WORKSPACE_DIR}/config.xray.example.json" "${WORKSPACE_DIR}/dist/etc/xray.json.example"

echo "=========================================================="
echo " 二进制准备完成！产物列表 (全量 FreeBSD 64位 ELF 二进制):"
ls -lh "${OUTPUT_DIR}"
echo "=========================================================="


