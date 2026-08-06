#!/usr/bin/env bash
set -euo pipefail

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
task_tmp=${SECKILL_MALL_GO_TMPDIR:-/tmp/seckill-mall-go-build}
task_cache=${SECKILL_MALL_GO_CACHE:-/tmp/seckill-mall-go-cache}
mkdir -p "$task_tmp" "$task_cache"
cd "$repo_root"

GOTMPDIR="$task_tmp" GOCACHE="$task_cache" go test \
  ./common/contracts ./common/messaging ./inventory_service ./internal/order \
  ./payment_service ./fulfillment_service ./api_gateway -count=1
