#!/usr/bin/env bash
# Walk the auth API end to end against a running lewnotes-core:
# setup → login → cookie-authed request → CSRF check → token creation →
# bearer-authed request → revoke. The token is never printed.
#
#   BASE=http://127.0.0.1:8080 PASSWORD='correct horse battery staple' \
#     server/testdata/scripts/auth.sh
#
# The server must allow cookies over plain http when BASE is http://, i.e. run
# it with LEWNOTES_COOKIE_SECURE=false for local smoke tests.
set -euo pipefail

BASE="${BASE:-http://127.0.0.1:8080}"
PASSWORD="${PASSWORD:-correct horse battery staple}"
JAR="$(mktemp)"
trap 'rm -f "$JAR"' EXIT

json_field() {
	python3 -c 'import json,sys; print(json.load(sys.stdin).get(sys.argv[1], ""))' "$1"
}

echo "→ auth status"
status=$(curl -fsS "$BASE/api/v1/auth/status")
setup_required=$(printf '%s' "$status" | json_field setup_required)

if [ "$setup_required" = "True" ]; then
	echo "→ setup (first run)"
	body=$(printf '{"password":"%s","instance_name":"smoke"}' "$PASSWORD")
	curl -fsS -c "$JAR" -H 'Content-Type: application/json' -d "$body" "$BASE/api/v1/auth/setup" >/dev/null
	echo "  owner created"
fi

echo "→ login"
body=$(printf '{"password":"%s"}' "$PASSWORD")
curl -fsS -c "$JAR" -b "$JAR" -H 'Content-Type: application/json' -d "$body" "$BASE/api/v1/auth/login" >/dev/null
me=$(curl -fsS -b "$JAR" "$BASE/api/v1/auth/me")
csrf=$(printf '%s' "$me" | json_field csrf_token)
[ -n "$csrf" ] || { echo "missing csrf token" >&2; exit 1; }
echo "  session ok"

echo "→ cookie-authed index status"
curl -fsS -b "$JAR" "$BASE/api/v1/index/status" >/dev/null

echo "→ CSRF is required for mutations"
code=$(curl -s -o /dev/null -w '%{http_code}' -b "$JAR" -X POST \
	-H 'Content-Type: application/json' -d '{"full":false}' "$BASE/api/v1/index/rescan")
[ "$code" = "403" ] || { echo "expected 403 without CSRF, got $code" >&2; exit 1; }

echo "→ create device token"
token_json=$(curl -fsS -b "$JAR" -H "X-Lewnotes-CSRF: $csrf" \
	-H 'Content-Type: application/json' -d '{"name":"smoke"}' "$BASE/api/v1/tokens")
token=$(printf '%s' "$token_json" | json_field token)
token_id=$(printf '%s' "$token_json" | json_field id)
[ -n "$token" ] || { echo "no token returned" >&2; exit 1; }

echo "→ bearer-authed index status"
curl -fsS -H "Authorization: Bearer $token" "$BASE/api/v1/index/status" >/dev/null

echo "→ revoke token"
curl -fsS -b "$JAR" -H "X-Lewnotes-CSRF: $csrf" -X DELETE "$BASE/api/v1/tokens/$token_id" >/dev/null
code=$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $token" "$BASE/api/v1/index/status")
[ "$code" = "401" ] || { echo "revoked token still accepted: $code" >&2; exit 1; }

echo "✓ auth flow ok (token never printed)"
