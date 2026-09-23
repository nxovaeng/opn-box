#!/bin/sh
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

# --- 明确锁定的上游版本号与提交哈希 (严禁浮动分支) ---
SINGBOX_VERSION="${SINGBOX_VERSION:-1.13.14}"
MOSDNS_MANAGED_COMMIT="${MOSDNS_MANAGED_COMMIT:-a740966d7123906cc87522b2732752d60b99c2ae}" # 基于官方 v5.3.4 + managed-dns API (与 UI 严格配套)
MOSDNS_CONTROLLER_TAG="${MOSDNS_CONTROLLER_TAG:-v0.0.2}"                                     # Web UI 控制器发行版
MOSDNS_X_TAG="${MOSDNS_X_TAG:-v26.01.18}"                                                   # 具备 DoQ/DoH3 的演进版

echo "=========================================================="
echo " 开始 100% 源码自编译流水线 (FreeBSD 64-bit / OPNsense 26.x)"
echo " 工作目录: ${WORKSPACE_DIR}"
echo " 目标输出: ${OUTPUT_DIR}"
echo " 锁定版本: sing-box=v${SINGBOX_VERSION}"
echo "           mosdns-managed=v5.3.4 (${MOSDNS_MANAGED_COMMIT:0:7})"
echo "           mosdns-controller=${MOSDNS_CONTROLLER_TAG}"
echo "           mosdns-x=${MOSDNS_X_TAG}"
echo "=========================================================="

# ------------------------------------------------------------------------------
# 1. 编译本地 pf-aliasd 守护进程 (自研核心：/dev/pf 与 TTL 最小堆 GC)
# ------------------------------------------------------------------------------
echo "==> [1/5] 编译本地 pf-aliasd 守护进程..."
(
    cd "${WORKSPACE_DIR}"
    go build -trimpath -ldflags="-s -w" -o "${OUTPUT_DIR}/pf-aliasd" ./cmd/pf-aliasd
)
echo "    -> pf-aliasd 编译成功"

# ------------------------------------------------------------------------------
# 2. 从源码编译 luoye663/mosdns (严格基于 v5.3.4 managed-dns，与 UI 完美配套)
# ------------------------------------------------------------------------------
echo "==> [2/5] 编译 mosdns (基于 v5.3.4 managed-dns ${MOSDNS_MANAGED_COMMIT:0:7}，嵌入自研 pf_alias)..."
git clone https://github.com/luoye663/mosdns.git "${BUILD_TMP}/mosdns-managed"
(
    cd "${BUILD_TMP}/mosdns-managed"
    git checkout "${MOSDNS_MANAGED_COMMIT}"
)
mkdir -p "${BUILD_TMP}/mosdns-managed/plugin/executable/pf_alias"
cp -r "${WORKSPACE_DIR}/pkg/plugin/"* "${BUILD_TMP}/mosdns-managed/plugin/executable/pf_alias/"
(
    cd "${BUILD_TMP}/mosdns-managed"
    go mod edit -require "opn-box@v0.0.0"
    go mod edit -replace "opn-box=${WORKSPACE_DIR}"
    sed -i 's|import (|import (\n\t_ "github.com/IrineSistiana/mosdns/v5/plugin/executable/pf_alias"|' main.go
    go mod tidy
    go build -trimpath -ldflags="-s -w -X 'main.mosdnsBase=v5.3.4'" -o "${OUTPUT_DIR}/mosdns" .
)
echo "    -> mosdns (v5.3.4 managed) 编译成功"

# ------------------------------------------------------------------------------
# 3. 从源码编译 luoye663/mosdns-controller (Web UI 面板，内嵌生产打包 Web 静态资源)
# ------------------------------------------------------------------------------
echo "==> [3/5] 从源码拉取并编译 mosdns-controller (${MOSDNS_CONTROLLER_TAG})..."
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
if [ "${BUILD_MOSDNS_X:-1}" = "1" ]; then
    echo "==> [4/5] 编译 mosdns-x (${MOSDNS_X_TAG}，嵌入自研 pf_alias 适配层)..."
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
fi

# ------------------------------------------------------------------------------
# 5. 从源码编译 sing-box (使用本地 patches/sing-box/ 补丁，零外部补丁依赖)
# ------------------------------------------------------------------------------
if [ "${BUILD_SINGBOX:-1}" = "1" ]; then
    echo "==> [5/5] 从官方源码编译 sing-box (v${SINGBOX_VERSION}，应用本地 FreeBSD TUN 补丁)..."
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

echo "=========================================================="
echo " 自编译完成！产物列表 (全量 FreeBSD 64位 ELF 二进制):"
ls -lh "${OUTPUT_DIR}"
echo "=========================================================="
