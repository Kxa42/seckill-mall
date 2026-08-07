#!/usr/bin/env bash
set -euo pipefail

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_root"

failed=0

while IFS=: read -r file line import_line; do
  source_service=${file#services/}
  source_service=${source_service%%/*}
  target_service=$(sed -E 's#.*seckill-mall/services/([^/]+)/internal/.*#\1#' <<<"$import_line")
  if [[ "$source_service" != "$target_service" ]]; then
    printf '跨服务 internal 依赖: %s:%s %s\n' "$file" "$line" "$import_line"
    failed=1
  fi
done < <(rg -n '"seckill-mall/services/[^/]+/internal/' services --glob '*.go' || true)

if rg -n '"seckill-mall/services/' shared --glob '*.go'; then
  printf 'shared 包不得反向依赖 services\n'
  failed=1
fi

if rg -n '"seckill-mall/services/[^/]+/testkit"' services shared tools \
  --glob '*.go' --glob '!*_test.go' --glob '!services/*/testkit/**'; then
  printf '生产代码不得依赖 testkit\n'
  failed=1
fi

exit "$failed"
