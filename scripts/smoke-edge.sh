#!/bin/sh
set -eu

EDGE_BASE="${EDGE_BASE:-http://localhost:8081}"

fail() {
	echo "edge smoke failed: $*" >&2
	exit 1
}

need() {
	command -v "$1" >/dev/null 2>&1 || fail "$1 is required"
}

need curl
need grep

echo "checking edge healthz"
health="$(curl -fsS "$EDGE_BASE/healthz")" || fail "healthz request failed"
echo "$health" | grep -q '"status":"ok"' || fail "edge healthz is not ok: $health"
echo "$health" | grep -q '"local_mqtt":true' || fail "local mqtt is not connected: $health"

echo "checking edge cache stats"
stats="$(curl -fsS "$EDGE_BASE/api/v1/cache/stats")" || fail "cache stats request failed"
echo "$stats" | grep -q '"pending":' || fail "missing pending in cache stats: $stats"
echo "$stats" | grep -q '"total":' || fail "missing total in cache stats: $stats"

echo "checking edge metrics"
metrics="$(curl -fsS "$EDGE_BASE/metrics")" || fail "metrics request failed"
echo "$metrics" | grep -q 'vpp_edge_cache_messages{state="pending"}' || fail "missing pending cache metric"
echo "$metrics" | grep -q 'vpp_edge_cache_oldest_pending_age_seconds' || fail "missing oldest pending age metric"
echo "$metrics" | grep -q 'vpp_edge_mqtt_connected{side="local"} 1' || fail "missing local mqtt connected metric"

echo "checking edge local command"
command_resp="$(curl -fsS -X POST "$EDGE_BASE/api/v1/local-command" \
	-H 'Content-Type: application/json' \
	-d '{"device_type":"load","device_id":"load_02","action":"set_relay","params":{"on":true},"reason":"edge smoke"}')" || fail "local command request failed"
echo "$command_resp" | grep -q '"topic":"vpp/home-lab/load/load_02/command"' || fail "unexpected local command response: $command_resp"
echo "$command_resp" | grep -q '"command_id":' || fail "local command missing command id: $command_resp"

echo "edge smoke ok"
