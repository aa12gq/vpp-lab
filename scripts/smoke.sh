#!/bin/sh
set -eu

API_BASE="${API_BASE:-http://localhost:8080}"
PROM_BASE="${PROM_BASE:-http://localhost:9090}"
SITE_ID="${SITE_ID:-home-lab}"

fail() {
	echo "smoke failed: $*" >&2
	exit 1
}

need() {
	command -v "$1" >/dev/null 2>&1 || fail "$1 is required"
}

need curl
need grep
need wc

echo "checking healthz"
health="$(curl -fsS "$API_BASE/healthz")" || fail "healthz request failed"
echo "$health" | grep -q '"status":"ok"' || fail "healthz is not ok: $health"

echo "checking site summary"
summary="$(curl -fsS "$API_BASE/api/v1/sites/$SITE_ID/summary")" || fail "summary request failed"
echo "$summary" | grep -q "\"site_id\":\"$SITE_ID\"" || fail "summary site mismatch: $summary"

echo "checking device states"
states="$(curl -fsS "$API_BASE/api/v1/sites/$SITE_ID/device-states")" || fail "device-states request failed"
online_count="$(printf '%s' "$states" | grep -o '"online":true' | wc -l | tr -d ' ')"
if [ "$online_count" -lt 1 ]; then
	fail "no online devices in device-states: $states"
fi

echo "checking prometheus online devices"
prom="$(curl -fsS "$PROM_BASE/api/v1/query?query=sum%28vpp_device_online%29")" || fail "prometheus request failed"
echo "$prom" | grep -q '"status":"success"' || fail "prometheus query failed: $prom"

echo "smoke ok: $online_count online device(s)"
