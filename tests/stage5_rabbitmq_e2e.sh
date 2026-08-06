#!/usr/bin/env bash
set -euo pipefail

if [[ "${SECKILL_RABBITMQ_INTEGRATION:-}" != "1" ]]; then
  echo "RabbitMQ integration skipped: set SECKILL_RABBITMQ_INTEGRATION=1"
  exit 0
fi

if ! command -v docker >/dev/null 2>&1 || ! docker info >/dev/null 2>&1; then
  echo "RabbitMQ integration skipped: Docker daemon is unavailable"
  exit 0
fi

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_root"
GOTMPDIR=${SECKILL_MALL_GO_TMPDIR:-/tmp/seckill-mall-go-build} go test ./common/messaging -count=1
