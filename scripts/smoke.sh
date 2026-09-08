#!/usr/bin/env bash
set -euo pipefail

api_url="${API_URL:-http://127.0.0.1:8080}"
ready=false
for _ in {1..60}; do
  if curl --fail --silent --max-time 2 "$api_url/readyz" > /dev/null; then
    ready=true
    break
  fi
  sleep 1
done
if [[ "$ready" != true ]]; then
  echo "FAIL: API not ready" >&2
  exit 1
fi

created=$(curl --fail --silent --show-error --max-time 5 \
  -H 'Content-Type: application/json' -d '{"sku":"STEAM-TOPUP-500"}' "$api_url/orders")
order_id=$(jq -er '.order_id' <<< "$created")
payment=$(jq -n --arg id "$order_id" \
  '{event_id:("evt_"+$id),order_id:$id,status:"paid",amount:500,currency:"RUB",created_at:"2026-09-07T12:00:00Z"}')
for _ in 1 2; do
  curl --fail --silent --show-error --max-time 5 \
    -H 'Content-Type: application/json' -d "$payment" "$api_url/webhooks/payment" > /dev/null
done
for _ in {1..30}; do
  result=$(curl --fail --silent --show-error --max-time 5 "$api_url/orders/$order_id")
  if jq -e '.status == "delivered" and (.code | length > 0)' <<< "$result" > /dev/null; then
    echo "PASS: created -> paid -> delivered, duplicate webhook accepted"
    exit 0
  fi
  sleep 1
done
echo "FAIL: order not delivered within 30 attempts" >&2
exit 1
