#!/usr/bin/env bash
set -euo pipefail

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
task_tmp=${SECKILL_MALL_GO_TMPDIR:-/tmp/seckill-mall-go-build}
task_cache=${SECKILL_MALL_GO_CACHE:-/tmp/seckill-mall-go-cache}
mkdir -p "$task_tmp" "$task_cache"
cd "$repo_root"

GOTMPDIR="$task_tmp" GOCACHE="$task_cache" go test \
  ./shared/contracts ./shared/platform/messaging \
  ./services/inventory/internal/app ./services/order/internal/app \
  ./services/payment/internal/app ./services/fulfillment/internal/app \
  ./services/gateway/internal/app -count=1
