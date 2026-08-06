#!/usr/bin/env bash
set -euo pipefail

# 无 Docker 时使用 bufconn 将 Gateway、Order、Catalog、Identity 和 Inventory 串联验收。
repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
task_tmp=${SECKILL_MALL_GO_TMPDIR:-/tmp/seckill-mall-go-build}
task_cache=${SECKILL_MALL_GO_CACHE:-/tmp/seckill-mall-go-cache}
mkdir -p "$task_tmp" "$task_cache"
cd "$repo_root"
GOTMPDIR="$task_tmp" GOCACHE="$task_cache" go test ./api_gateway -run TestOrderRoutesUseOrderGRPCWithoutGatewayInventoryCall -count=1
