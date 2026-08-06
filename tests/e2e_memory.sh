#!/usr/bin/env bash

# 在不依赖 MySQL、Redis、RabbitMQ 或 Docker 的情况下验收商城 HTTP 主链路。
set -euo pipefail

for command_name in go curl jq; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    printf 'missing required command: %s\n' "$command_name" >&2
    exit 1
  fi
done

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
runtime_dir=$(mktemp -d /tmp/seckill-commerce-e2e.XXXXXX)
api_pid=''

cleanup() {
  if [[ -n "$api_pid" ]]; then
    kill -TERM "$api_pid" 2>/dev/null || true
    wait "$api_pid" 2>/dev/null || true
  fi
  rm -rf "$runtime_dir"
}
trap cleanup EXIT

export GOTMPDIR="$runtime_dir"
export GOCACHE="$runtime_dir/go-build-cache"
export GIN_MODE='release'
export SECKILL_COMMERCE_STORE='memory'
export SECKILL_COMMERCE_HTTP_ADDR="${SECKILL_E2E_HTTP_ADDR:-127.0.0.1:18082}"
export SECKILL_JWT_SECRET='e2e-jwt-secret-32-characters-minimum'
export SECKILL_MOCK_PAYMENT_SECRET='e2e-payment-secret-32-characters-min'
export SECKILL_ADMIN_EMAIL='admin@example.com'
export SECKILL_ADMIN_PASSWORD='admin-password'

cd "$repo_root"
go build -o "$runtime_dir/commerce-api" ./cmd/commerce-api
"$runtime_dir/commerce-api" >"$runtime_dir/commerce-api.log" 2>&1 &
api_pid=$!

base_url="http://$SECKILL_COMMERCE_HTTP_ADDR"
ready=''
for _ in {1..30}; do
  if curl -fsS "$base_url/readyz" >/dev/null 2>&1; then
    ready='yes'
    break
  fi
  sleep 1
done
if [[ "$ready" != 'yes' ]]; then
  tail -n 40 "$runtime_dir/commerce-api.log"
  exit 1
fi

registered=$(curl -fsS -X POST "$base_url/api/v1/auth/register" \
  -H 'Content-Type: application/json' \
  --data '{"email":"e2e-buyer@example.com","password":"password-123"}')
buyer_token=$(printf '%s' "$registered" | jq -er '.data.access_token')

address_response=$(curl -fsS -X POST "$base_url/api/v1/addresses" \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $buyer_token" \
  --data '{"recipient":"E2E Buyer","phone":"13800000000","province":"Zhejiang","city":"Hangzhou","district":"Xihu","detail":"Acceptance Road 1","is_default":true}')
address_id=$(printf '%s' "$address_response" | jq -er '.data.id')

cart_response=$(curl -fsS -X POST "$base_url/api/v1/cart/items" \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $buyer_token" \
  --data '{"sku_id":1,"quantity":1}')
printf '%s' "$cart_response" | jq -e '.data.sku_id == 1 and .data.quantity == 1' >/dev/null

preview_response=$(curl -fsS "$base_url/api/v1/cart/checkout-preview" \
  -H "Authorization: Bearer $buyer_token")
printf '%s' "$preview_response" | jq -e \
  '.data.available == true and .data.total_amount_cents == 699900' >/dev/null

order_response=$(curl -fsS -X POST "$base_url/api/v1/orders" \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $buyer_token" \
  -H 'Idempotency-Key: process-e2e-checkout' \
  --data "{\"address_id\":$address_id}")
order_id=$(printf '%s' "$order_response" | jq -er '.data.order_id')

duplicate_response=$(curl -fsS -X POST "$base_url/api/v1/orders" \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $buyer_token" \
  -H 'Idempotency-Key: process-e2e-checkout' \
  --data "{\"address_id\":$address_id}")
printf '%s' "$duplicate_response" | jq -e --arg order_id "$order_id" \
  '.data.order_id == $order_id' >/dev/null

payment_response=$(curl -fsS -X POST "$base_url/api/v1/payments/mock" \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $buyer_token" \
  --data "{\"order_id\":\"$order_id\"}")
payment_no=$(printf '%s' "$payment_response" | jq -er '.data.payment.payment_no')
signature=$(printf '%s' "$payment_response" | jq -er '.data.callback_signature')

paid_response=$(curl -fsS -X POST "$base_url/api/v1/payments/mock/callback" \
  -H 'Content-Type: application/json' \
  --data "{\"payment_no\":\"$payment_no\",\"callback_ref\":\"process-e2e-callback\",\"signature\":\"$signature\"}")
printf '%s' "$paid_response" | jq -e '.data.status == "paid"' >/dev/null

forbidden_status=$(curl -sS -o /dev/null -w '%{http_code}' \
  -X POST "$base_url/api/v1/admin/orders/$order_id/ship" \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $buyer_token" \
  --data '{"carrier":"SF","tracking_no":"SF-PROCESS-E2E"}')
[[ "$forbidden_status" == '403' ]]

admin_login=$(curl -fsS -X POST "$base_url/api/v1/auth/login" \
  -H 'Content-Type: application/json' \
  --data '{"email":"admin@example.com","password":"admin-password"}')
admin_token=$(printf '%s' "$admin_login" | jq -er '.data.access_token')

shipped_response=$(curl -fsS -X POST "$base_url/api/v1/admin/orders/$order_id/ship" \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $admin_token" \
  --data '{"carrier":"SF","tracking_no":"SF-PROCESS-E2E"}')
printf '%s' "$shipped_response" | jq -e '.data.status == "shipped"' >/dev/null

completed_response=$(curl -fsS -X POST "$base_url/api/v1/orders/$order_id/confirm" \
  -H "Authorization: Bearer $buyer_token")
printf '%s' "$completed_response" | jq -e '.data.status == "completed"' >/dev/null

printf 'process_e2e=PASS order_status=completed idempotency=PASS rbac_http_status=403 total_amount_cents=699900\n'
